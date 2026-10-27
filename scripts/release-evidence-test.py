#!/usr/bin/env python3
"""Hermetic stale/tamper tests of release evidence, using public toy files only.

Fixture PASS rows test verifier control flow; they are never release acceptance,
OS/network evidence or a substitute for executing the wallet's actual suites.
"""
import contextlib
import importlib.util
import io
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import evidence_binding as binding


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


verifier = module('tested_release_verifier', 'verify-release.py')
frontend = module('tested_frontend_measurement', 'frontend-coverage.py')


class ReleaseEvidence(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-release-evidence-')
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.write('go.mod', 'module localwallet\n')
        self.write('go.sum', 'Public module checksum fixture\n')
        self.write('internal/vault/vault.go', 'package vault\nfunc publicFixture() {}\n')
        self.write('internal/vault/vault_test.go', 'package vault\n')
        for name in binding.FRONTEND_PINS:
            self.write(name, 'Public source/pin fixture: ' + name)
        for name in (*binding.AUTH_INPUTS, *binding.EVIDENCE_INPUTS):
            self.write(name, 'Public verification source fixture: ' + name)
        for name in verifier.PACKAGED_SEND_INPUTS:
            self.write(name, 'Public local Send source fixture: ' + name)
        self.write('desktop/src/main.ts', '// Public TypeScript fixture\n')
        self.write('desktop/tests/public.test.cjs', '// Public test fixture\n')
        for name in binding.PRODUCTION_FILES:
            self.write('desktop/' + name, '// Public production fixture: ' + name)
        self.write_json('docs/traceability.json', {'requirements': [{'id': name, 'status': 'PASS'} for name in ('TP050-decred', 'TP050-public-testnets', 'TP080-independent-review')]})
        for name in verifier.REQUIRED_REPORTS:
            self.write_json('reports/' + name + '.json', {'status': 'PASS', 'scope': 'Synthetic control-flow fixture only'})
        for name, inputs in (('release-auth', binding.AUTH_INPUTS), ('release-evidence', binding.EVIDENCE_INPUTS)):
            self.write_json('reports/' + name + '.json', {'status': 'PASS', 'sourceSHA256': binding.hash_names(self.root, inputs)})
        hashes = {}
        for name in binding.COMPONENTS:
            content = 'Public executable byte fixture: ' + name
            self.write('bin/' + name, content)
            self.write('dist/LocalWallet-linux-x64/resources/bin/' + name, content)
            hashes[name] = binding.sha256(self.root / 'bin' / name)
        self.write('dist/LocalWallet-linux-x64/cryptowallet', 'Public executable byte fixture: wallet-launcher')
        self.write('dist/LocalWallet-linux-x64/cryptowallet-ui', 'Public Electron byte fixture')
        self.write('dist/LocalWallet-linux-x64/resources/app.asar', 'Public opaque archive byte fixture; not an actual ASAR')
        self.write_json('dist/LocalWallet-linux-x64/resources/bin/manifest.json', hashes)
        self.write_json('dist/build-manifest.json', {'version': '1.2.3', 'binaryManifest': hashes})
        self.current = binding.candidate(self.root)
        sources = binding.hash_names(self.root, ['desktop/' + name for name in binding.PRODUCTION_FILES])
        self.write_json('reports/artifact-security.json', {
            'status': 'PASS', 'artifact': self.current,
            'productionSourceSHA256': sources,
            'payloadSHA256': {name: sources['desktop/' + name] for name in binding.PRODUCTION_FILES},
            'productionPayloadMatchesSource': True, 'productionFilesCompared': 12,
            'files': ['/app', '/ui', '/package.json', *('/' + name for name in binding.PRODUCTION_FILES)],
            'fuses': {'version': '1', '0': 48, '1': 49, '2': 48, '3': 48, '5': 49}})
        self.write_json('reports/packaged-recovery.json', {'status': 'PASS',
            'artifact': {key: value for key, value in self.current.items() if key != 'electronSHA256'},
            'steps': [{'name': 'Synthetic public fixture only', 'status': 'PASS'}]})
        amount, fee = 12345678912345678, 21000 * 10 ** 9
        txid = '0x' + 'a' * 64
        self.write_json('reports/packaged-send.json', {'status': 'PASS',
            'sourceSHA256': binding.hash_names(self.root, verifier.PACKAGED_SEND_INPUTS),
            'artifact': {key: value for key, value in self.current.items() if key != 'electronSHA256'},
            'steps': [{'name': name, 'status': 'PASS'} for name in verifier.PACKAGED_SEND_STEPS],
            'networkIsolation': {'uid': 1000, 'interfaces': ['lo'], 'externalRoutes': False, 'networkNamespace': 'net:[12345]'},
            'publicNetworkTests': 'NOT_RUN', 'mainnetSpending': 'NOT_APPLICABLE: no outside network route',
            'osMatrix': 'NOT_RUN: synthetic control-flow fixture only',
            'node': {'chainId': 11155111,
                'image': 'ethereum/client-go:v1.17.9@sha256:1ae3571b074ccda207808fd228ade023f62ebdeb1dab6b393093dcc8b5293109',
                'binarySHA256': '75da01386ed074c086f18e52e034b79cfc6974232f196ca1c72bb16ca169e3e2'},
            'approval': {'networkVisible': True, 'atomicAmountVisible': True, 'atomicAmount': str(amount)},
            'transfer': {'identity': txid, 'source': '0x9858EfFD232B4033E47d90003D41EC34EcaEda94',
                'recipient': '0x1111111111111111111111111111111111111111', 'chainId': 11155111,
                'atomicAmount': str(amount), 'nativeRecipientBefore': '0', 'nativeRecipientAfter': str(amount),
                'sourceBefore': str(1000 * 10 ** 18), 'sourceAfter': str(1000 * 10 ** 18 - amount - fee),
                'actualFee': str(fee), 'sourcePendingNonceAfter': 1,
                'receipt': {'transactionHash': txid, 'status': '0x1', 'type': '0x2', 'gasUsed': hex(21000), 'effectiveGasPrice': hex(10 ** 9),
                    'from': '0x9858EfFD232B4033E47d90003D41EC34EcaEda94', 'to': '0x1111111111111111111111111111111111111111'}}})
        summary = {str(self.root / name): {'statements': {'pct': 100}, 'branches': {'pct': 100}}
                   for name in binding.frontend_runtime(self.root)}
        self.write_json('reports/desktop/coverage-summary.json', summary)
        self.write_json('reports/frontend-coverage.json', {'status': 'PASS', 'sourceStable': True,
            'sourceSHA256': binding.frontend_sources(self.root), 'runtimeSHA256': binding.frontend_runtime(self.root),
            'coverageSummarySHA256': binding.sha256(self.root / 'reports/desktop/coverage-summary.json'),
            'files': binding.frontend_rows(self.root, summary)})
        profile = 'mode: atomic\nlocalwallet/internal/vault/vault.go:2.1,2.30 1 1\n'
        for name in ('go-base.cover', 'go-native.cover', 'go.cover'):
            self.write('reports/' + name, profile)
        self.write_json('reports/go-coverage.json', {'status': 'PASS', 'floors': 'PASS',
            'sourceSHA256': binding.go_sources(self.root),
            'gates': [{'gate': name, 'status': 'PASS'} for name in ('decred-simnet', 'ton-v5r1-emulator', 'usdt-evm-execution')],
            'profiles': [{'file': 'reports/' + name, 'sha256': binding.sha256(self.root / 'reports' / name)} for name in ('go-base.cover', 'go-native.cover')]})
        self.write_json('reports/coverage.json', {'packages': binding.go_rows(self.root / 'reports/go.cover')})

    def write(self, name, content):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def write_json(self, name, value):
        self.write(name, json.dumps(value) + '\n')

    def report(self, name):
        return binding.read_json(self.root, 'reports/' + name + '.json')

    def failures(self):
        # Tiny fixture files are never executed. Production main always enables
        # the separate existing runtime/archive/version verifier.
        return verifier.verify(self.root, check_version=False)

    def assert_blocked(self, part):
        self.assertTrue(any(part in value for value in self.failures()), self.failures())

    def test_consistent_complete_bound_fixture_passes(self):
        self.assertEqual(self.failures(), [])

    def test_every_missing_required_report_blocks(self):
        for name in verifier.REQUIRED_REPORTS:
            with self.subTest(report=name):
                path = self.root / 'reports' / (name + '.json')
                original = path.read_bytes()
                path.unlink()
                self.assert_blocked(name)
                path.write_bytes(original)

    def test_original_decred_testnet_review_and_soak_gates_remain_mandatory(self):
        for name in ('soak', 'desktop-e2e', 'public-testnets', 'independent-review', 'integration'):
            path = self.root / 'reports' / (name + '.json')
            original = path.read_bytes()
            self.write_json('reports/' + name + '.json', {'status': 'NOT_RUN'})
            self.assert_blocked(name)
            path.write_bytes(original)
        trace = binding.read_json(self.root, 'docs/traceability.json')
        for row in trace['requirements']:
            original = row['status']
            row['status'] = 'BLOCKED'
            self.write_json('docs/traceability.json', trace)
            self.assert_blocked(row['id'])
            row['status'] = original

    def test_go_snapshot_rejects_added_deleted_and_changed_source_or_pins(self):
        for name in ('internal/vault/vault.go', 'internal/vault/vault_test.go', 'go.mod', 'go.sum'):
            path = self.root / name
            original = path.read_bytes()
            path.write_bytes(original + b'public change')
            self.assert_blocked('go-coverage-stale-or-incomplete-source-set')
            path.unlink()
            self.assert_blocked('go-coverage-binding')
            path.write_bytes(original)
        self.write('internal/new-package/added.go', 'package newpackage\n')
        self.assert_blocked('go-coverage-stale-or-incomplete-source-set')

    def test_go_snapshot_subset_is_not_accepted(self):
        report = self.report('go-coverage')
        del report['sourceSHA256']['internal/vault/vault_test.go']
        self.write_json('reports/go-coverage.json', report)
        self.assert_blocked('go-coverage-stale-or-incomplete-source-set')

    def test_go_raw_profile_hashes_and_exact_profile_set_are_required(self):
        report = self.report('go-coverage')
        self.write('reports/go-base.cover', 'mode: atomic\n')
        self.assert_blocked('go-coverage-raw-profile-mismatch')
        report['profiles'] = report['profiles'][:1]
        self.write_json('reports/go-coverage.json', report)
        self.assert_blocked('go-coverage-raw-profiles-incomplete')

    def test_go_merged_profile_cannot_forge_frozen_input_counters(self):
        self.write('reports/go.cover', 'mode: atomic\nlocalwallet/internal/vault/vault.go:2.1,2.30 100 1\n')
        self.assert_blocked('go-coverage-union-profile-mismatch')

    def test_go_package_floor_report_is_recomputed(self):
        report = self.report('coverage')
        report['packages'][0]['covered'] = 999
        self.write_json('reports/coverage.json', report)
        self.assert_blocked('go-coverage-floors-or-measurement-mismatch')

    def test_decred_and_ton_local_coverage_gates_cannot_disappear(self):
        report = self.report('go-coverage')
        report['gates'] = [row for row in report['gates'] if row['gate'] != 'decred-simnet']
        self.write_json('reports/go-coverage.json', report)
        self.assert_blocked('go-coverage-original-local-gates-incomplete')

    def test_usdt_execution_gate_cannot_disappear_or_be_replaced_by_duplicate(self):
        report = self.report('go-coverage')
        report['gates'] = [row for row in report['gates'] if row['gate'] != 'usdt-evm-execution']
        self.write_json('reports/go-coverage.json', report)
        self.assert_blocked('go-coverage-original-local-gates-incomplete')
        report['gates'].append({'gate': 'ton-v5r1-emulator', 'status': 'PASS'})
        self.write_json('reports/go-coverage.json', report)
        self.assert_blocked('go-coverage-original-local-gates-incomplete')

    def test_frontend_snapshot_rejects_source_ui_test_and_package_pin_changes(self):
        for name in ('desktop/src/main.ts', 'desktop/ui/style.css', 'desktop/tests/public.test.cjs', *binding.FRONTEND_PINS):
            path = self.root / name
            original = path.read_bytes()
            path.write_bytes(original + b'public edit')
            self.assert_blocked('frontend-coverage-stale-source')
            path.write_bytes(original)

    def test_frontend_snapshot_rejects_added_deleted_and_subset_sources(self):
        added = self.root / 'desktop/src/added.ts'
        added.write_text('// Public addition')
        self.assert_blocked('frontend-coverage-stale-source')
        added.unlink()
        path = self.root / 'desktop/tests/public.test.cjs'
        original = path.read_bytes()
        path.unlink()
        self.assert_blocked('frontend-coverage-stale-source')
        path.write_bytes(original)
        report = self.report('frontend-coverage')
        del report['sourceSHA256']['desktop/tests/public.test.cjs']
        self.write_json('reports/frontend-coverage.json', report)
        self.assert_blocked('frontend-coverage-stale-source')

    def test_frontend_compiled_js_changes_and_additions_invalidate_measurement(self):
        path = self.root / 'desktop/app/main.js'
        original = path.read_bytes()
        path.write_bytes(original + b'public compiled change')
        self.assert_blocked('frontend-coverage-stale-runtime')
        path.write_bytes(original)
        self.write('desktop/app/added.js', '// Public unmeasured addition')
        self.assert_blocked('frontend-coverage-stale-runtime')

    def test_frontend_raw_summary_and_measured_floors_are_checked(self):
        path = self.root / 'reports/desktop/coverage-summary.json'
        summary = json.loads(path.read_text())
        summary[str(self.root / 'desktop/app/main.js')]['branches']['pct'] = 0
        self.write_json('reports/desktop/coverage-summary.json', summary)
        self.assert_blocked('frontend-coverage-raw-summary-mismatch')
        report = self.report('frontend-coverage')
        report['coverageSummarySHA256'] = binding.sha256(path)
        self.write_json('reports/frontend-coverage.json', report)
        self.assert_blocked('frontend-coverage-floors-or-measurement-mismatch')

    def test_frontend_unstable_measurement_cannot_pass(self):
        report = self.report('frontend-coverage')
        report['sourceStable'] = False
        self.write_json('reports/frontend-coverage.json', report)
        self.assert_blocked('frontend-coverage-missing-or-unstable')

    def test_recovery_from_another_asar_or_binary_candidate_is_rejected(self):
        self.write('dist/LocalWallet-linux-x64/resources/app.asar', 'Public different ASAR')
        self.assert_blocked('packaged-recovery-not-bound-to-candidate')
        self.assert_blocked('artifact-security-not-bound-to-candidate')
        self.assert_blocked('packaged-send-binding')

    def test_consistent_new_go_binaries_cannot_reuse_old_packaged_recovery(self):
        name = 'wallet-core'
        for prefix in ('bin/', 'dist/LocalWallet-linux-x64/resources/bin/'):
            self.write(prefix + name, 'Public newly built core')
        hashes = dict(self.current['binaryManifest'])
        hashes[name] = binding.sha256(self.root / 'bin' / name)
        self.write_json('dist/LocalWallet-linux-x64/resources/bin/manifest.json', hashes)
        self.write_json('dist/build-manifest.json', {'version': '1.2.3', 'binaryManifest': hashes})
        self.assert_blocked('packaged-recovery-not-bound-to-candidate')

    def test_electron_binary_or_manifest_tampering_blocks(self):
        self.write('dist/LocalWallet-linux-x64/cryptowallet-ui', 'Public changed Electron')
        self.assert_blocked('artifact-security-not-bound-to-candidate')
        self.write('bin/wallet-signer', 'Public changed signer')
        self.assert_blocked('candidate-binary-hash-mismatch')

    def test_all_twelve_production_source_payload_bindings_are_required(self):
        for name in binding.PRODUCTION_FILES:
            path = self.root / 'desktop' / name
            original = path.read_bytes()
            path.write_bytes(original + b'public change')
            self.assert_blocked('artifact-security-stale-source')
            path.write_bytes(original)
        report = self.report('artifact-security')
        del report['payloadSHA256']['ui/index.html']
        self.write_json('reports/artifact-security.json', report)
        self.assert_blocked('artifact-security-payload-mismatch')

    def test_missing_fuse_inventory_or_recovery_steps_cannot_pass(self):
        artifact = self.report('artifact-security')
        artifact['fuses']['0'] = 49
        self.write_json('reports/artifact-security.json', artifact)
        self.assert_blocked('artifact-security-fuses-incomplete')
        recovery = self.report('packaged-recovery')
        recovery['steps'] = []
        self.write_json('reports/packaged-recovery.json', recovery)
        self.assert_blocked('packaged-recovery-steps-incomplete')

    def test_report_symlinks_duplicates_and_secret_like_errors_fail_safely(self):
        secret = 'SYNTHETIC-SECRET-MUST-NOT-APPEAR-IN-ERROR'
        path = self.root / 'reports/stress.json'
        path.write_text('{"status":"PASS","status":"' + secret + '"}')
        result = self.failures()
        self.assertTrue(any('duplicate-evidence-member' in line for line in result))
        self.assertNotIn(secret, '\n'.join(result))
        path.unlink()
        path.symlink_to(self.root / 'reports/soak.json')
        self.assert_blocked('symlinked-evidence-file')
        trace = {'requirements': [{'id': secret + '\n' + secret, 'status': 'FAIL'}]}
        self.write_json('docs/traceability.json', trace)
        self.assertNotIn(secret, '\n'.join(self.failures()))

    def test_failed_measurement_overwrites_old_pass_and_does_not_reseal_it(self):
        with mock.patch.object(frontend.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, ['npm'])), contextlib.redirect_stdout(io.StringIO()):
            result = frontend.measure(self.root)
        self.assertEqual(result, 1)
        report = self.report('frontend-coverage')
        self.assertEqual(report['status'], 'FAIL')
        self.assertFalse(report['sourceStable'])
        self.assertNotIn('sourceSHA256', report)

    def test_source_changed_during_measurement_fails_without_current_hash_stamping(self):
        count = 0
        def run(*args, **kwargs):
            nonlocal count
            count += 1
            if count == 2:
                self.write('desktop/src/main.ts', '// Public concurrent modification')
            return subprocess.CompletedProcess(args, 0)
        with mock.patch.object(frontend.subprocess, 'run', side_effect=run), contextlib.redirect_stdout(io.StringIO()):
            result = frontend.measure(self.root)
        self.assertEqual(result, 1)
        report = self.report('frontend-coverage')
        self.assertEqual(report['reason'], 'frontend-source-or-runtime-changed-during-measurement')
        self.assertNotIn('sourceSHA256', report)

    def test_version_check_failure_does_not_echo_subprocess_output(self):
        canary = b'SYNTHETIC-SECRET-CLI-OUTPUT'
        with mock.patch.object(verifier.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, canary)):
            result = verifier.verify(self.root)
        self.assertTrue(any('artifact-version-integrity-failed' in line for line in result))
        self.assertNotIn(canary.decode(), '\n'.join(result))

    def test_release_workflow_and_verifier_changes_invalidate_their_tests(self):
        for name in ('.github/workflows/release.yml', 'scripts/release_assets.py', 'scripts/verify-artifact.cjs', 'scripts/verify-release.py'):
            path = self.root / name
            original = path.read_bytes()
            path.write_bytes(original + b'public change')
            self.assert_blocked('source-bound-report-stale-or-incomplete')
            path.write_bytes(original)

    def test_packaged_send_requires_all_seven_steps_and_unchanged_driver_sources(self):
        original = self.report('packaged-send')
        for index in range(7):
            report = json.loads(json.dumps(original))
            del report['steps'][index]
            self.write_json('reports/packaged-send.json', report)
            self.assert_blocked('packaged-send-seven-required-steps-incomplete')
        self.write_json('reports/packaged-send.json', original)
        self.write('tests/e2e/local_geth.py', 'Public changed node fixture')
        self.assert_blocked('packaged-send-binding: source-bound-report-stale-or-incomplete')

    def test_packaged_send_cannot_claim_public_acceptance_or_skip_real_execution(self):
        original = self.report('packaged-send')
        changes = [('publicNetworkTests', 'PASS'), ('networkIsolation', {'uid': 0, 'interfaces': ['eth0']}),
                   ('node', {'chainId': 1}), ('approval', {}),
                   ('approval', {'networkVisible': 1, 'atomicAmountVisible': True, 'atomicAmount': '12345678912345678'}), ('transfer', {})]
        for key, value in changes:
            report = json.loads(json.dumps(original))
            report[key] = value
            self.write_json('reports/packaged-send.json', report)
            self.assert_blocked('packaged-send-binding')

    def test_packaged_send_receipt_balance_fee_and_no_duplicate_nonce_are_verified(self):
        original = self.report('packaged-send')
        for field, value in (('nativeRecipientAfter', '0'), ('sourceAfter', '0'), ('actualFee', '0'),
                             ('sourcePendingNonceAfter', 2), ('atomicAmount', '1'), ('identity', 'invalid')):
            report = json.loads(json.dumps(original))
            report['transfer'][field] = value
            self.write_json('reports/packaged-send.json', report)
            self.assert_blocked('packaged-send-binding')
        report = json.loads(json.dumps(original))
        report['transfer']['receipt']['status'] = '0x0'
        self.write_json('reports/packaged-send.json', report)
        self.assert_blocked('packaged-send-successful-receipt-missing')


class EvidenceResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.outcomes = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.outcomes.append({'test': str(test), 'status': 'PASS'})

    def addFailure(self, test, error):
        super().addFailure(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL'})

    def addError(self, test, error):
        super().addError(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL'})


if __name__ == '__main__':
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2, resultclass=EvidenceResult).run(suite)
    names = ('scripts/evidence_binding.py', 'scripts/verify-release.py', 'scripts/frontend-coverage.py',
             'scripts/verify-artifact.cjs', 'scripts/release-evidence-test.py')
    report = {'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'testsRun': result.testsRun,
              'tests': result.outcomes, 'sourceSHA256': {name: binding.sha256(ROOT / name) for name in names},
              'scope': 'Hermetic candidate binding, complete source sets, raw coverage integrity and stale/tampered/missing evidence rejection using public toy files. No OS/network/recovery acceptance is fabricated.'}
    (ROOT / 'reports').mkdir(exist_ok=True)
    (ROOT / 'reports/release-evidence.json').write_text(json.dumps(report, indent=2) + '\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
