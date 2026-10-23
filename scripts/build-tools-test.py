#!/usr/bin/env python3
"""Hermetic build/install regression tests; tiny public fixture ELFs only.

No wallet binary, dependency downloader, remote publisher or real profile is run.
The ASAR fixture reader is a test double; package acceptance tests exercise the
actual Electron archive separately. Git, cc and dpkg-deb operate in temp dirs.
"""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import runpy
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / filename)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


version = module('tested_version', 'version.py')
next_release = module('tested_next_release', 'next-version.py')
installer = module('tested_installer', 'install.py')
verifier = module('tested_verifier', 'verify-version.py')


def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


class BuildTools(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        for command in ('cc', 'git', 'dpkg-deb'):
            if not shutil.which(command):
                raise RuntimeError('BLOCKED: fixture prerequisite unavailable: ' + command)
        cls.fixtures = tempfile.TemporaryDirectory(prefix='wallet-build-tool-elf-')
        cls.addClassCleanup(cls.fixtures.cleanup)
        cls.elf = {}
        for value in ('0.1.0', '0.1.1', '0.1.10'):
            folder = pathlib.Path(cls.fixtures.name) / value
            folder.mkdir()
            source = folder / 'version.c'
            source.write_text('#include <stdio.h>\n#include <string.h>\nint main(int n,char **v){if(n==1||(n==2&&!strcmp(v[1],"--version"))){puts("' + value + '");return 0;}return 2;}\n')
            binary = folder / 'fixture'
            subprocess.run(['cc', '-O0', '-o', str(binary), str(source)], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            cls.elf[value] = binary

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-build-tool-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        self.addCleanup(mock.patch.stopall)
        mock.patch.dict(os.environ, {}, clear=False).start()
        os.environ.pop('WALLET_VERSION', None)
        for key in ('GIT_DIR', 'GIT_WORK_TREE', 'GIT_COMMON_DIR', 'GIT_INDEX_FILE', 'GIT_OBJECT_DIRECTORY', 'GIT_ALTERNATE_OBJECT_DIRECTORIES', 'GIT_CONFIG_COUNT'):
            os.environ.pop(key, None)
        os.environ['GIT_CONFIG_GLOBAL'] = '/dev/null'
        os.environ['GIT_CONFIG_NOSYSTEM'] = '1'
        os.environ['GIT_TEMPLATE_DIR'] = str(self.root / 'empty-git-template')
        (self.root / 'empty-git-template').mkdir()

    def app(self, value='0.1.0', folder='source'):
        app = self.root / folder
        (app / 'resources/bin').mkdir(parents=True)
        for name in ('wallet-core', 'wallet-signer', 'wallet-launcher'):
            shutil.copy2(self.elf[value], app / 'resources/bin' / name)
        shutil.copy2(self.elf[value], app / 'wallet-launcher')
        shutil.copy2(self.elf[value], app / 'LocalWallet')
        (app / 'resources/app.asar').write_text(json.dumps({'version': value}))
        (app / 'resources/public-fixture.txt').write_text('Complete application resource for ' + value)
        manifest = {name: digest(app / 'resources/bin' / name) for name in ('wallet-core', 'wallet-signer', 'wallet-launcher')}
        (app / 'resources/bin/manifest.json').write_text(json.dumps(manifest))
        return app

    def destination(self):
        destination = self.root / 'destination'
        destination.mkdir(mode=0o700)
        return destination

    def install(self, app, destination):
        with contextlib.redirect_stdout(io.StringIO()):
            installer.install(app, destination)

    def runtime_version(self, destination):
        return subprocess.check_output([str(destination / 'wallet')], text=True).strip()

    def project(self, metadata='0.1.1', binaries=None):
        binaries = binaries or metadata
        app = self.app(binaries, 'dist/LocalWallet-linux-x64')
        (app / 'resources/app.asar').write_text(json.dumps({'version': metadata}))
        source = self.root / 'bin'
        shutil.copytree(app / 'resources/bin', source)
        (self.root / 'VERSION').write_text(metadata + '\n')
        stage = self.root / 'deb-stage'
        (stage / 'DEBIAN').mkdir(parents=True)
        (stage / 'DEBIAN/control').write_text('Package: localwallet-development\nVersion: ' + metadata + '\nArchitecture: amd64\nMaintainer: Public Test <fixture@example.invalid>\nDescription: public fixture\n')
        shutil.copytree(app, stage / 'opt/localwallet')
        deb = self.root / 'dist' / f'localwallet-development_{metadata}_amd64.deb'
        subprocess.run(['dpkg-deb', '--root-owner-group', '--build', str(stage), str(deb)], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        portable = self.root / 'dist' / f'localwallet-{metadata}-linux-amd64.tar.gz'
        with tarfile.open(portable, 'w:gz') as archive:
            archive.add(app, arcname='LocalWallet-linux-x64')
        manifest = {'version': metadata, 'package': deb.name, 'sha256': digest(deb), 'binaryManifest': json.loads((source / 'manifest.json').read_text()), 'portable': {'file': portable.name, 'sha256': digest(portable)}}
        (self.root / 'dist/build-manifest.json').write_text(json.dumps(manifest))
        return app, manifest

    def verify(self):
        # Only the tiny fixture ASAR reader is substituted. Debian metadata,
        # artifact hashes and all seven actual ELF --version calls are real.
        with mock.patch.object(verifier, '_electron_version', lambda _base, app: json.loads((app / 'resources/app.asar').read_text())['version']), contextlib.redirect_stdout(io.StringIO()):
            verifier.verify_version(self.root)

    def test_numeric_semver_maximum_is_not_lexical(self):
        self.assertEqual(next_release.next_version('0.1.0', ['v1.9.99', 'v1.10.2', 'v1.2.999', 'v0.99.999']), '1.10.3')
        self.assertEqual(next_release.next_version('2.0.0', ['v1.99.999']), '2.0.1')
        self.assertEqual(next_release.next_version('0.1.0', []), '0.1.1')

    def test_invalid_and_out_of_policy_tags_are_ignored(self):
        tags = ['v01.2.3', '1.2.3', 'v1.2', 'v1.2.3-beta', 'v1.2.3+build', 'v1.2.3\nINJECT=1', 'v2147483648.0.0', 'v0.2147483648.0', 'v0.0.' + '9' * 5000, 'v-1.0.0', None, 1]
        self.assertEqual(next_release.next_version('0.1.0', tags), '0.1.1')
        for base in ('01.1.0', '1.2', '1.2.3;touch injected', '2147483648.0.0'):
            with self.subTest(base=base), self.assertRaises(ValueError):
                next_release.next_version(base, [])

    def test_component_and_patch_exhaustion_bounds(self):
        self.assertEqual(version.validate_version('2147483647.2147483647.2147483647'), '2147483647.2147483647.2147483647')
        self.assertEqual(next_release.next_version('0.0.0', ['v1.2.2147483646']), '1.2.2147483647')
        with self.assertRaises(ValueError):
            next_release.next_version('0.0.0', ['v1.2.2147483647'])
        self.assertEqual(next_release.next_version('2.0.0', ['v1.2.2147483647']), '2.0.1')

    def test_version_override_rejects_shell_and_github_environment_injection(self):
        sentinel = self.root / 'MUST-NOT-EXIST'
        invalid = ['0.1.1\nGH_TOKEN=canary', '0.1.1;touch ' + str(sentinel), '$(touch ' + str(sentinel) + ')', '`touch ' + str(sentinel) + '`', '../1.2.3', '-X injected', '1.2.03', '', ' 1.2.3', '١.٢.٣', '9' * 5000 + '.0.0']
        for value in invalid:
            with self.subTest(value=value[:80]):
                result = subprocess.run([sys.executable, str(ROOT / 'scripts/version.py')], env={**os.environ, 'WALLET_VERSION': value}, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, '')
                self.assertFalse(sentinel.exists())
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/version.py')], env={**os.environ, 'WALLET_VERSION': '1.2.3'}, text=True, capture_output=True, check=True)
        self.assertEqual(result.stdout, '1.2.3\n')

    def test_next_version_reads_git_tags_without_creating_or_mutating_them(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        for name in ('next-version.py', 'version.py'):
            shutil.copy2(ROOT / 'scripts' / name, scripts / name)
        (self.root / 'VERSION').write_text('0.1.0\n')
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=Public Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-q', '--allow-empty', '-m', 'public fixture'], check=True)
        for tag in ('v1.9.9', 'v1.10.1', 'v01.99.99', 'v3.0.0-beta'):
            subprocess.run(['git', '-C', str(self.root), 'tag', tag], check=True)
        before = subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list'])
        result = subprocess.check_output([sys.executable, str(scripts / 'next-version.py')], text=True)
        self.assertEqual(result, '1.10.2\n')
        self.assertEqual(subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list']), before)

    def test_install_preserves_unrelated_wallet_file(self):
        app, destination = self.app(), self.destination()
        target = destination / 'wallet'
        target.write_text('public foreign-file sentinel')
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(target.read_text(), 'public foreign-file sentinel')
        self.assertFalse((destination / 'wallet-runtime').exists())

    def test_install_preserves_unrelated_runtime_directory(self):
        app, destination = self.app(), self.destination()
        runtime = destination / 'wallet-runtime'
        runtime.mkdir()
        sentinel = runtime / 'foreign-file'
        sentinel.write_bytes(b'public foreign runtime')
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(list(runtime.iterdir()), [sentinel])
        self.assertFalse((destination / 'wallet').exists())

    def test_install_refuses_runtime_symlink_even_with_external_managed_marker(self):
        app, destination = self.app(), self.destination()
        outside = self.root / 'outside'
        outside.mkdir(mode=0o700)
        (outside / '.localwallet-managed').touch(mode=0o600)
        (outside / 'foreign').write_bytes(b'public external sentinel')
        before = sorted(path.name for path in outside.iterdir())
        (destination / 'wallet-runtime').symlink_to(outside, target_is_directory=True)
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(sorted(path.name for path in outside.iterdir()), before)
        self.assertEqual((outside / 'foreign').read_bytes(), b'public external sentinel')
        self.assertFalse((destination / 'wallet').exists())

    def test_install_refuses_traversal_launcher_and_symlinked_marker(self):
        app, destination = self.app(), self.destination()
        runtime = destination / 'wallet-runtime'
        runtime.mkdir(mode=0o700)
        marker = runtime / '.localwallet-managed'
        marker.touch(mode=0o600)
        target = destination / 'wallet'
        original = 'wallet-runtime/../../foreign/wallet-launcher'
        target.symlink_to(original)
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(os.readlink(target), original)
        target.unlink()
        marker.unlink()
        outside = self.root / 'foreign-marker'
        outside.write_bytes(b'public external marker')
        marker.symlink_to(outside)
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(outside.read_bytes(), b'public external marker')

    def test_manifest_mismatch_never_switches_existing_install(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'wallet')
        new = self.app('0.1.1', 'new')
        (new / 'resources/bin/wallet-core').write_bytes(b'public tampered executable')
        with self.assertRaises(RuntimeError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'wallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')

    def test_incomplete_runtime_or_ambiguous_manifest_never_creates_launcher(self):
        app, destination = self.app(), self.destination()
        asar = app / 'resources/app.asar'
        asar.unlink()
        with self.assertRaisesRegex(RuntimeError, 'Incomplete packaged runtime'):
            self.install(app, destination)
        self.assertFalse((destination / 'wallet-runtime').exists())
        asar.write_text('{"version":"0.1.0"}')
        manifest = app / 'resources/bin/manifest.json'
        original = manifest.read_text()
        manifest.write_text(original[:-1] + ',"wallet-core":"' + digest(app / 'resources/bin/wallet-core') + '"}')
        with self.assertRaisesRegex(RuntimeError, 'Duplicate binary manifest member'):
            self.install(app, destination)
        self.assertFalse((destination / 'wallet').exists())

    def test_copy_failure_preserves_selected_version_and_profile_files(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'wallet')
        profile = destination / 'public-profile-sentinel'
        profile.write_bytes(b'not an actual profile or secret')
        new = self.app('0.1.1', 'new')
        with mock.patch.object(installer.shutil, 'copytree', side_effect=OSError('public injected copy failure')), self.assertRaises(OSError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'wallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')
        self.assertEqual(profile.read_bytes(), b'not an actual profile or secret')
        self.assertFalse(list((destination / 'wallet-runtime').glob('.staging-*')))

    def test_completed_copy_is_reverified_before_commit(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'wallet')
        new = self.app('0.1.1', 'new')
        copy = shutil.copytree
        def corrupt(source, target, *args, **kwargs):
            result = copy(source, target, *args, **kwargs)
            if pathlib.Path(source) == new:
                (pathlib.Path(target) / 'resources/bin/wallet-signer').write_bytes(b'public copied-byte corruption')
            return result
        with mock.patch.object(installer.shutil, 'copytree', side_effect=corrupt), self.assertRaises(RuntimeError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'wallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')

    def test_upgrade_switches_one_complete_version_atomically_and_retains_old_generation(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'wallet')
        new = self.app('0.1.1', 'new')
        replace = pathlib.Path.replace
        seen = []
        def commit(link, target):
            if link.name.startswith('.wallet-link-'):
                self.assertEqual(os.readlink(destination / 'wallet'), original)
                self.assertEqual(self.runtime_version(destination), '0.1.0')
                staged = link.resolve().parent
                self.assertEqual((staged / 'resources/public-fixture.txt').read_text(), 'Complete application resource for 0.1.1')
                self.assertEqual(subprocess.check_output([str(staged / 'resources/bin/wallet-signer'), '--version'], text=True), '0.1.1\n')
                installer.verify_binaries(staged)
                seen.append(True)
            return replace(link, target)
        with mock.patch.object(pathlib.Path, 'replace', commit):
            self.install(new, destination)
        self.assertEqual(seen, [True])
        self.assertNotEqual(os.readlink(destination / 'wallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.1')
        self.assertTrue((destination / original).is_file())
        self.assertEqual(subprocess.check_output([str(destination / original)], text=True), '0.1.0\n')
        self.assertFalse(list(destination.glob('.wallet-link-*')))

    def test_failed_atomic_link_switch_preserves_previous_runtime(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'wallet')
        new = self.app('0.1.1', 'new')
        with mock.patch.object(pathlib.Path, 'replace', side_effect=OSError('public injected commit failure')), self.assertRaises(OSError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'wallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')
        self.assertFalse(list(destination.glob('.wallet-link-*')))

    def test_actual_elf_versions_match_metadata_and_all_binary_hashes(self):
        self.project()
        self.verify()

    def test_runtime_version_match_is_exact_not_prefix(self):
        self.project(metadata='0.1.1', binaries='0.1.10')
        with self.assertRaisesRegex(RuntimeError, 'Go runtime version differs'):
            self.verify()

    def test_stale_packaged_binary_is_not_hidden_by_current_source_bin(self):
        app, _ = self.project()
        shutil.copy2(self.elf['0.1.0'], app / 'resources/bin/wallet-core')
        with self.assertRaisesRegex(RuntimeError, 'manifest mismatch'):
            self.verify()

    def test_tampered_debian_and_portable_artifacts_fail_closed(self):
        _, manifest = self.project()
        deb = self.root / 'dist' / manifest['package']
        original = deb.read_bytes()
        deb.write_bytes(original + b'public tampering')
        with self.assertRaisesRegex(RuntimeError, 'Debian artifact hash differs'):
            self.verify()
        deb.write_bytes(original)
        portable = self.root / 'dist' / manifest['portable']['file']
        portable.write_bytes(portable.read_bytes() + b'public tampering')
        with self.assertRaisesRegex(RuntimeError, 'Portable artifact identity differs'):
            self.verify()

    def test_release_publish_step_follows_mandatory_gates_and_version_check(self):
        text = (ROOT / '.github/workflows/release.yml').read_text()
        publish = text.index('gh release create ')
        gate = next(line.split('run: ', 1)[1] for line in text[:publish].splitlines() if 'run: make test ' in line)
        self.assertTrue(set(('test', 'test-security', 'test-race', 'test-interop', 'test-integration', 'coverage', 'check-deps')).issubset(shlex.split(gate)))
        self.assertLess(text.index('scripts/packaged-recovery.py'), publish)
        self.assertLess(text.index('scripts/verify-version.py'), publish)
        self.assertNotIn('git tag ', text[:publish])
        self.assertNotIn('git push ', text[:publish])
        self.assertIn('--prerelease', text[publish:])
        self.assertIn('--target "$GITHUB_SHA"', text[publish:])
        self.assertIn('cancel-in-progress: false', text)

    def test_unsupported_platform_is_rejected_before_toolchain_or_bootstrap(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        shutil.copy2(ROOT / 'scripts/with-toolchain.sh', scripts / 'with-toolchain.sh')
        fakebin = self.root / 'fake-bin'
        fakebin.mkdir()
        marker = self.root / 'go-must-not-run'
        for name, script in {
            'uname': '#!/bin/sh\ncase "$1" in -s) echo Linux;; -m) echo aarch64;; esac\n',
            'go': '#!/bin/sh\ntouch ' + shlex.quote(str(marker)) + '\necho go version go1.27.3\n',
        }.items():
            executable = fakebin / name
            executable.write_text(script)
            executable.chmod(0o755)
        result = subprocess.run(['/bin/sh', str(scripts / 'with-toolchain.sh'), '/bin/true'], env={**os.environ, 'PATH': str(fakebin) + ':/usr/bin:/bin'}, text=True, capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Linux amd64', result.stderr)
        self.assertFalse(marker.exists())

    def test_node_archive_hash_mismatch_never_extracts_or_installs(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        copied = scripts / 'bootstrap-node.py'
        shutil.copy2(ROOT / 'scripts/bootstrap-node.py', copied)
        with mock.patch('urllib.request.urlopen', return_value=io.BytesIO(b'public corrupt node archive')) as request:
            with self.assertRaisesRegex(SystemExit, 'SHA256 mismatch'):
                runpy.run_path(str(copied), run_name='__main__')
        self.assertEqual(request.call_args.args[0], 'https://nodejs.org/dist/v24.19.0/node-v24.19.0-linux-x64.tar.xz')
        self.assertEqual(list((self.root / '.tools').iterdir()), [])

    def test_sbom_marks_shipped_electron_required_and_never_reads_external_symlink_targets(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        for name in ('sbom.py', 'version.py'):
            shutil.copy2(ROOT / 'scripts' / name, scripts / name)
        (self.root / 'VERSION').write_text('0.1.1\n')
        desktop = self.root / 'desktop'
        desktop.mkdir()
        (desktop / 'package-lock.json').write_text(json.dumps({'packages': {'': {}, 'node_modules/electron': {'version': '45.0.0', 'dev': True}, 'node_modules/typescript': {'version': '7.0.2', 'dev': True}}}))
        source = self.root / 'public-source.go'
        source.write_text('package publicfixture\n')
        external = self.root / 'external-public-sentinel'
        external.write_bytes(b'PUBLIC-content-must-not-be-read')
        link = self.root / 'public-source-link'
        link.symlink_to(external)
        original = subprocess.check_output
        def public_tools(args, **kwargs):
            if args[:2] == ['go', 'list']:
                return json.dumps({'Main': True, 'Path': 'publicfixture'})
            if args[:2] == ['git', 'status']:
                return ' M public-source.go\n'
            if args[:2] == ['git', 'ls-files']:
                return b'public-source.go\0public-source-link\0'
            if args[:2] == ['git', 'rev-parse']:
                return 'a' * 40 + '\n'
            if args[:2] == ['python3', 'scripts/version.py']:
                return original(args, **kwargs)
            self.fail('Unexpected build-tool capability: ' + repr(args))
        with mock.patch.object(subprocess, 'check_output', side_effect=public_tools), contextlib.redirect_stdout(io.StringIO()):
            runpy.run_path(str(scripts / 'sbom.py'), run_name='__main__')
        sbom = json.loads((self.root / 'dist/sbom.cdx.json').read_text())
        components = {entry['name']: entry for entry in sbom['components']}
        self.assertEqual(components['electron']['scope'], 'required')
        self.assertEqual(components['typescript']['scope'], 'excluded')
        manifest = json.loads((self.root / 'dist/source-manifest.json').read_text())
        self.assertEqual(manifest['workingTree'], 'dirty')
        self.assertEqual(manifest['sha256'][link.name], hashlib.sha256(os.readlink(link).encode()).hexdigest())
        self.assertNotEqual(manifest['sha256'][link.name], digest(external))


class EvidenceResult(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.outcomes = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.outcomes.append({'test': str(test), 'status': 'PASS'})

    def addError(self, test, error):
        super().addError(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL', 'detail': str(error[1])})

    def addFailure(self, test, error):
        super().addFailure(test, error)
        self.outcomes.append({'test': str(test), 'status': 'FAIL', 'detail': str(error[1])})

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.outcomes.append({'test': str(test), 'status': 'NOT_RUN', 'detail': reason})


class EvidenceRunner(unittest.TextTestRunner):
    resultclass = EvidenceResult

    def run(self, test):
        result = super().run(test)
        filenames = ['scripts/version.py', 'scripts/next-version.py', 'scripts/install.py', 'scripts/verify-version.py', 'scripts/with-toolchain.sh', 'scripts/bootstrap-node.py', 'scripts/sbom.py', 'scripts/build-tools-test.py', '.github/workflows/release.yml']
        report = {
            'status': 'PASS' if result.wasSuccessful() and not result.skipped else 'FAIL' if not result.wasSuccessful() else 'NOT_RUN',
            'command': [sys.executable, 'scripts/build-tools-test.py'],
            'tests': result.outcomes,
            'scope': 'Hermetic public fixture ELFs, real temporary Debian files and Git repository; ASAR fixture reader and SBOM inventory tool outputs are substituted. No wallet profiles, network, funding or remote publication.',
            'sourceSHA256': {name: digest(ROOT / name) for name in filenames},
        }
        (ROOT / 'reports').mkdir(exist_ok=True)
        (ROOT / 'reports/build-tools.json').write_text(json.dumps(report, indent=2) + '\n')
        return result


if __name__ == '__main__':
    unittest.main(verbosity=2, testRunner=EvidenceRunner)
