#!/usr/bin/env python3
"""Offline release inventory/policy tests and real frozen Sigstore verification.

No new wallet/publisher signature is made. The upstream public certificate and
artifact are frozen independent crypto fixtures, never a wallet release identity.
"""
import base64
import contextlib
import hashlib
import io
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import release_assets as release

REPO = 'wallet-owner/wallet'
REPO_ID = '123456789'
REF = 'refs/heads/main'
COMMIT = 'a' * 40
VERSION = '1.2.3'
FIXTURES = ROOT / 'tests/release-auth'


class InventoryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-release-auth-')
        self.addCleanup(self.temporary.cleanup)
        self.dist = pathlib.Path(self.temporary.name)
        for name in release.expected_assets(VERSION):
            if name != release.METADATA:
                (self.dist / name).write_text('Public release fixture: ' + name)
        release.prepare(self.dist, VERSION, REPO, REF, COMMIT, REPO_ID)

    def check(self, **options):
        return release.check(self.dist, options.get('version', VERSION), options.get('repo', REPO), options.get('ref', REF), options.get('commit', COMMIT), options.get('repo_id', REPO_ID))

    def rewrite_manifest(self):
        (self.dist / release.MANIFEST).write_text(''.join(release.digest(self.dist / name) + '  ' + name + '\n' for name in release.expected_assets(VERSION)))

    def test_portable_and_detached_verification_inventory_is_required(self):
        entries = self.check()
        self.assertEqual(set(entries), set(release.expected_assets(VERSION)))
        self.assertIn(f'cryptowallet-{VERSION}-linux-amd64.tar.gz', entries)
        self.assertEqual(len(entries), 3)
        self.assertEqual(json.loads((self.dist / release.METADATA).read_text())['tag'], VERSION)

    def test_every_missing_or_tampered_payload_fails(self):
        for name in release.expected_assets(VERSION):
            with self.subTest(asset=name):
                path = self.dist / name
                content = path.read_bytes()
                path.unlink()
                with self.assertRaises(ValueError):
                    self.check()
                path.write_bytes(content + b'public tampering')
                with self.assertRaises(ValueError):
                    self.check()
                path.write_bytes(content)

    def test_prepare_refuses_incomplete_desktop_without_overwriting_manifest(self):
        manifest = (self.dist / release.MANIFEST).read_bytes()
        (self.dist / f'cryptowallet-{VERSION}-linux-amd64.tar.gz').unlink()
        with self.assertRaises(ValueError):
            release.prepare(self.dist, VERSION, REPO, REF, COMMIT, REPO_ID)
        self.assertEqual((self.dist / release.MANIFEST).read_bytes(), manifest)

    def test_duplicate_traversal_extra_and_incomplete_manifests_fail(self):
        path = self.dist / release.MANIFEST
        original = path.read_text()
        for content in (original + original.splitlines()[0] + '\n',
                        original + '0' * 64 + '  ../outside\n',
                        original + '0' * 64 + '  surprise.exe\n',
                        '\n'.join(original.splitlines()[1:]) + '\n',
                        original.replace('  release.json\n', ' *release.json\n')):
            with self.subTest(content=content[-100:]):
                path.write_text(content)
                with self.assertRaises(ValueError):
                    self.check()

    def test_symlink_and_hardlink_assets_are_not_accepted(self):
        path = self.dist / f'cryptowallet-{VERSION}-linux-amd64.tar.gz'
        path.unlink()
        outside = self.dist / 'public-outside'
        outside.write_text('Public fixture')
        path.symlink_to(outside)
        with self.assertRaises(ValueError):
            self.check()
        path.unlink()
        os.link(outside, path)
        with self.assertRaises(ValueError):
            self.check()

    def test_signed_identity_and_version_must_match_consumer_policy(self):
        for options in ({'repo': 'different/publisher'}, {'ref': 'refs/heads/master'},
                        {'commit': 'b' * 40}, {'version': '1.2.4'}, {'repo_id': '999'}):
            with self.subTest(options=options), self.assertRaises(ValueError):
                self.check(**options)
        path = self.dist / release.METADATA
        original = path.read_text()
        for value in (original[:-2] + ',"schemaVersion":1}\n',
                      original.replace('"schemaVersion": 1', '"schemaVersion": true')):
            path.write_text(value)
            self.rewrite_manifest()
            with self.assertRaises(ValueError):
                self.check()

    def test_identity_input_cannot_inject_flags_environment_or_shell(self):
        for repo, ref, commit in (('--repo=bad', REF, COMMIT), (REPO + '\nX=1', REF, COMMIT),
                                 (REPO, 'refs/pull/1/merge', COMMIT), (REPO, REF, 'a' * 39),
                                 (REPO, REF, COMMIT + ';false')):
            with self.subTest(repo=repo, ref=ref, commit=commit), self.assertRaises(ValueError):
                release.identity(repo, ref, commit)

    def test_missing_bundle_cannot_be_reported_authenticated(self):
        with mock.patch.object(release.subprocess, 'run', side_effect=AssertionError('No tool should run')), self.assertRaises(ValueError):
            release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)

    def test_cryptographic_tool_failure_and_old_cli_fail_closed(self):
        (self.dist / release.BUNDLE).write_text('{}')
        with mock.patch.object(release.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, ['gh'])):
            with self.assertRaises(subprocess.CalledProcessError):
                release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)
        for output in ('gh version 2.46.0 (fixture)\n', 'gh version 2.96.0\n', 'not gh\n'):
            with mock.patch.object(release.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, output)), self.assertRaises(ValueError):
                release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)

    def test_verifier_uses_exact_certificate_source_and_hosted_runner_policy(self):
        (self.dist / release.BUNDLE).write_text('{}')
        verified = json.dumps([{'verificationResult': {'signature': {'certificate': {'sourceRepositoryIdentifier': REPO_ID}}}}])
        responses = [subprocess.CompletedProcess([], 0, 'gh version 2.103.2 (fixture)\n'), subprocess.CompletedProcess([], 0, verified)]
        with mock.patch.object(release.subprocess, 'run', side_effect=responses) as run:
            release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)
        command = run.call_args_list[1].args[0]
        for option, value in (('--repo', REPO), ('--source-ref', REF), ('--source-digest', COMMIT),
                              ('--cert-identity', f'https://github.com/{REPO}/{release.WORKFLOW}@{REF}'),
                              ('--cert-oidc-issuer', 'https://token.actions.githubusercontent.com'),
                              ('--predicate-type', 'https://slsa.dev/provenance/v1'), ('--digest-alg', 'sha256')):
            self.assertEqual(command[command.index(option) + 1], value)
        self.assertIn('--deny-self-hosted-runners', command)
        self.assertNotIn('--cert-identity-regex', command)

    def test_manifest_changed_during_authentication_fails(self):
        (self.dist / release.BUNDLE).write_text('{}')
        def change(*args, **kwargs):
            (self.dist / release.MANIFEST).write_text('public substitution')
        with mock.patch.object(release, 'verify_provenance', side_effect=change), self.assertRaisesRegex(ValueError, 'changed during authentication'):
            release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)

    def test_verification_requires_independent_immutable_repository_id(self):
        with self.assertRaisesRegex(ValueError, 'repository ID is required'):
            release.verify(self.dist, VERSION, REPO, REF, COMMIT)
        (self.dist / release.BUNDLE).write_text('{}')
        wrong = json.dumps([{'verificationResult': {'signature': {'certificate': {'sourceRepositoryIdentifier': '999'}}}}])
        responses = [subprocess.CompletedProcess([], 0, 'gh version 2.103.2\n'), subprocess.CompletedProcess([], 0, wrong)]
        with mock.patch.object(release.subprocess, 'run', side_effect=responses), self.assertRaisesRegex(ValueError, 'repository ID differs'):
            release.verify(self.dist, VERSION, REPO, REF, COMMIT, repo_id=REPO_ID)

    def test_workflow_pins_actions_and_never_publishes_without_authentication(self):
        text = (ROOT / '.github/workflows/release.yml').read_text()
        uses = re.findall(r'uses:\s+([^\s#]+)', text)
        self.assertGreaterEqual(len(uses), 5)
        self.assertTrue(all(re.fullmatch(r'actions/[A-Za-z0-9-]+@[0-9a-f]{40}', item) for item in uses))
        publish = text.index('gh release create ')
        for check in ('scripts/release_assets.py prepare', 'node .github/provenance/attest.mjs', 'scripts/release_assets.py verify'):
            self.assertLess(text.index(check), publish)
        self.assertNotIn('continue-on-error', text)
        self.assertNotIn('--no-sandbox', text)
        self.assertIn('persist-credentials: false', text)
        self.assertNotIn('attestations: write', text)
        self.assertNotIn('ATTESTATION_BUNDLE:', text)
        self.assertNotIn('actions/attest-build-provenance@', text)
        self.assertIn('npm --prefix .github/provenance ci --ignore-scripts', text)


class RealSigstoreTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.gh = ROOT / '.tools/gh-2.103.2/bin/gh'
        if not cls.gh.is_file():
            candidate = shutil.which('gh')
            if candidate:
                result = subprocess.run([candidate, 'version'], text=True, capture_output=True)
                match = re.match(r'gh version ([0-9]+)\.([0-9]+)\.([0-9]+)', result.stdout)
                if match and tuple(map(int, match.groups())) >= release.MIN_GH:
                    cls.gh = pathlib.Path(candidate)
        if not cls.gh.is_file() or str(cls.gh) == '.':
            raise unittest.SkipTest('BLOCKED: run python3 scripts/bootstrap-gh.py to provision pinned verifier')
        provenance = json.loads((FIXTURES / 'provenance.json').read_text())
        for name, checksum in provenance['sha256'].items():
            if release.digest(FIXTURES / name) != checksum:
                raise RuntimeError('Independent Sigstore fixture changed: ' + name)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-real-sigstore-')
        self.addCleanup(self.temporary.cleanup)
        self.work = pathlib.Path(self.temporary.name)
        for name in ('sigstore-js-2.1.0.tgz', 'sigstore-js-2.1.0-bundle.json', 'trusted_root.json'):
            shutil.copy2(FIXTURES / name, self.work / name)
        shutil.copytree(FIXTURES / 'private-timestamp', self.work / 'private-timestamp')

    def verify(self, repo='sigstore/sigstore-js', ref=REF, commit='26d16513386ffaa790b1c32f927544f1322e4194', repo_id='495574555'):
        # Capture CLI output (which contains no secrets). Cryptography is real,
        # with a frozen independent CA/transparency root and no network lookup.
        original = subprocess.run
        def captured(command, **kwargs):
            kwargs.pop('capture_output', None)
            kwargs.update(stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            return original(command, **kwargs)
        with mock.patch.object(release.subprocess, 'run', side_effect=captured):
            release.verify_provenance(self.work / 'sigstore-js-2.1.0.tgz', self.work / 'sigstore-js-2.1.0-bundle.json',
                                      repo, ref, commit, self.gh, self.work / 'trusted_root.json', digest_algorithm='sha512', repo_id=repo_id)

    def test_real_independent_certificate_signature_digest_and_transparency_pass(self):
        self.verify()

    def test_real_wrong_publisher_branch_and_commit_fail(self):
        for policy in ({'repo': 'sigstore/wrong-publisher'}, {'ref': 'refs/heads/master'}, {'commit': 'b' * 40}):
            with self.subTest(policy=policy), self.assertRaises(subprocess.CalledProcessError):
                self.verify(**policy)

    def test_real_valid_signature_with_wrong_immutable_repository_id_fails(self):
        with self.assertRaisesRegex(ValueError, 'repository ID differs'):
            self.verify(repo_id='999')

    def test_real_tampered_artifact_and_bundle_signatures_fail(self):
        artifact = self.work / 'sigstore-js-2.1.0.tgz'
        original = artifact.read_bytes()
        artifact.write_bytes(original + b'public tampering')
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify()
        artifact.write_bytes(original)
        path = self.work / 'sigstore-js-2.1.0-bundle.json'
        bundle = json.loads(path.read_text())
        signature = bytearray(base64.b64decode(bundle['dsseEnvelope']['signatures'][0]['sig']))
        signature[-1] ^= 1
        bundle['dsseEnvelope']['signatures'][0]['sig'] = base64.b64encode(signature).decode()
        path.write_text(json.dumps(bundle))
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify()

    def test_real_wrong_transparency_verification_key_fails(self):
        if not shutil.which('openssl'):
            self.fail('BLOCKED: openssl is required for ephemeral wrong-key fixture')
        key = self.work / 'ephemeral-test-only-key.pem'
        public = self.work / 'ephemeral-test-only-public.der'
        subprocess.run(['openssl', 'genpkey', '-algorithm', 'EC', '-pkeyopt', 'ec_paramgen_curve:P-256', '-out', str(key)], check=True, capture_output=True)
        subprocess.run(['openssl', 'pkey', '-in', str(key), '-pubout', '-outform', 'DER', '-out', str(public)], check=True, capture_output=True)
        path = self.work / 'trusted_root.json'
        data = json.loads(path.read_text())
        data['tlogs'][0]['publicKey']['rawBytes'] = base64.b64encode(public.read_bytes()).decode()
        path.write_text(json.dumps(data))
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify()

    def verify_private_timestamp(self, repo='actions/attest-demo', trusted_root=None):
        folder = self.work / 'private-timestamp'
        # The independent fixture's workflow differs from CryptoWallet's. Only
        # this test selects that known public publisher; production stays fixed.
        with mock.patch.object(release, 'WORKFLOW', '.github/workflows/build-python.yml'):
            release.verify_provenance(folder / 'artifact.whl', folder / 'bundle.json', repo, REF,
                                      'a6c23b9806c593664f68637c8f9d45dfcf98b2db', self.gh,
                                      trusted_root or folder / 'trusted-root.jsonl', repo_id='763287532')

    def test_real_private_sigstore_timestamp_bundle_verifies_without_a_stored_record(self):
        bundle = json.loads((self.work / 'private-timestamp/bundle.json').read_text())
        self.assertTrue(bundle['verificationMaterial']['timestampVerificationData']['rfc3161Timestamps'])
        self.assertFalse(bundle['verificationMaterial'].get('tlogEntries'))
        self.verify_private_timestamp()

    def test_real_private_timestamp_and_artifact_tampering_fail(self):
        folder = self.work / 'private-timestamp'
        artifact = folder / 'artifact.whl'
        original = artifact.read_bytes();artifact.write_bytes(original + b'public fixture tampering')
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify_private_timestamp()
        artifact.write_bytes(original)
        bundle_path = folder / 'bundle.json';bundle = json.loads(bundle_path.read_text())
        timestamp = bundle['verificationMaterial']['timestampVerificationData']['rfc3161Timestamps'][0]
        signature = bytearray(base64.b64decode(timestamp['signedTimestamp']));signature[-1] ^= 1
        timestamp['signedTimestamp'] = base64.b64encode(signature).decode()
        bundle_path.write_text(json.dumps(bundle))
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify_private_timestamp()

    def test_real_private_timestamp_wrong_root_and_publisher_fail(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify_private_timestamp(repo='actions/wrong-publisher')
        with self.assertRaises(subprocess.CalledProcessError):
            self.verify_private_timestamp(trusted_root=self.work / 'trusted_root.json')


class EvidenceResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.outcomes = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.outcomes.append({'test': str(test), 'status': 'PASS'})

    def addFailure(self, test, error):
        super().addFailure(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL', 'reason': str(error[1])})

    def addError(self, test, error):
        super().addError(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL', 'reason': str(error[1])})

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.outcomes.append({'test': str(test), 'status': 'BLOCKED', 'reason': reason})


if __name__ == '__main__':
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2, resultclass=EvidenceResult).run(suite)
    from evidence_binding import AUTH_INPUTS
    names = AUTH_INPUTS
    report = {'status': 'FAIL' if not result.wasSuccessful() else 'BLOCKED' if result.skipped else 'PASS',
              'tests': result.outcomes, 'testsRun': result.testsRun,
              'sourceSHA256': {name: release.digest(ROOT / name) for name in names},
              'scope': 'Hermetic complete-release inventories, exact publisher policy and real frozen upstream Sigstore/certificate/transparency verification; ephemeral wrong-key test only. No signing of CryptoWallet or remote workflow/publication.',
              'publisherSigning': 'NOT_RUN: no workflow was triggered and no CryptoWallet publisher attestation was fabricated',
              'remotePublication': 'NOT_RUN'}
    (ROOT / 'reports').mkdir(exist_ok=True)
    (ROOT / 'reports/release-auth.json').write_text(json.dumps(report, indent=2) + '\n')
    sys.exit(0 if result.wasSuccessful() and not result.skipped else 1)
