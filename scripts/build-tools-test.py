#!/usr/bin/env python3
"""Hermetic build/install regression tests; tiny public fixture ELFs only.

No wallet binary, dependency downloader, remote publisher or real profile is run.
The ASAR fixture reader is a test double; package acceptance tests exercise the
actual Electron archive separately. Git, cc and portable archives operate in temp dirs.
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
        for command in ('cc', 'git'):
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
        shutil.copy2(self.elf[value], app / 'cryptowallet')
        shutil.copy2(self.elf[value], app / 'cryptowallet-ui')
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
        return subprocess.check_output([str(destination / 'cryptowallet')], text=True).strip()

    def orphaned_install(self):
        app, destination = self.app(), self.destination()
        self.install(app, destination)
        original = os.readlink(destination / 'cryptowallet')
        shutil.rmtree(destination / 'wallet-runtime')
        self.assertTrue((destination / 'cryptowallet').is_symlink())
        self.assertFalse((destination / 'cryptowallet').exists())
        return destination, original

    def project(self, metadata='0.1.1', binaries=None):
        binaries = binaries or metadata
        app = self.app(binaries, 'dist/CryptoWallet-linux-x64')
        (app / 'resources/app.asar').write_text(json.dumps({'version': metadata}))
        source = self.root / 'bin'
        shutil.copytree(app / 'resources/bin', source)
        (self.root / 'VERSION').write_text(metadata + '\n')
        portable = self.root / 'dist' / f'cryptowallet-{metadata}-linux-amd64.tar.gz'
        with tarfile.open(portable, 'w:gz') as archive:
            archive.add(app, arcname='CryptoWallet-linux-x64')
        manifest = {'format': 'portable-desktop-v1', 'version': metadata, 'binaryManifest': json.loads((source / 'manifest.json').read_text()), 'portable': {'file': portable.name, 'sha256': digest(portable)}}
        (self.root / 'dist/build-manifest.json').write_text(json.dumps(manifest))
        return app, manifest

    def verify(self):
        # Only the tiny fixture ASAR reader is substituted. Portable payloads,
        # artifact hashes and all seven actual ELF --version calls are real.
        with mock.patch.object(verifier, '_electron_version', lambda _base, app: json.loads((app / 'resources/app.asar').read_text())['version']), contextlib.redirect_stdout(io.StringIO()):
            verifier.verify_version(self.root)

    def go_project(self, value='0.1.1', packaged=False, binaries=None):
        source = self.root / 'bin'
        source.mkdir()
        for name in verifier._COMPONENTS:
            shutil.copy2(self.elf[binaries or value], source / name)
        hashes = {name: digest(source / name) for name in verifier._COMPONENTS}
        (source / 'manifest.json').write_text(json.dumps(hashes))
        manifest = {'schemaVersion': 1, 'kind': 'go-components', 'version': value, 'status': 'DEVELOPMENT_UNSIGNED', 'source': 'a' * 40, 'dirty': False, 'go': 'go version fixture', 'platform': 'linux-amd64', 'binaryManifest': hashes}
        (source / 'go-build-manifest.json').write_text(json.dumps(manifest))
        (self.root / 'VERSION').write_text(value + '\n')
        if packaged:
            dist = self.root / 'dist'
            dist.mkdir()
            for name in verifier._GO_INVENTORIES:
                data = b'Public upstream redistribution license text.\n' if name.endswith('.txt') else b'[]' if name == 'go-dependency-licenses.json' else b'{"publicFixture":true}'
                (dist / name).write_bytes(data)
            (dist / 'go-THIRD-PARTY-NOTICES.json').write_text(json.dumps({'noticeSHA256': digest(dist / 'go-THIRD-PARTY-NOTICES.txt'), 'review': 'NOT_RUN: legal review'}))
            manifest['inventorySHA256'] = {name: digest(dist / name) for name in verifier._GO_INVENTORIES}
            (source / 'go-build-manifest.json').write_text(json.dumps(manifest))
            folder = self.root / f'cryptowallet-go-{value}-linux-amd64'
            shutil.copytree(source, folder)
            for name in verifier._GO_INVENTORIES:
                shutil.copy2(dist / name, folder / name)
            (folder / 'README.md').write_text('Public component fixture; not a standalone desktop wallet.')
            artifact = dist / (folder.name + '.tar.gz')
            with tarfile.open(artifact, 'w:gz') as archive:
                archive.add(folder, arcname=folder.name)
            manifest['archive'] = {'file': artifact.name, 'sha256': digest(artifact)}
            for name in (*verifier._COMPONENTS, 'manifest.json'):
                shutil.copy2(source / name, dist / name)
            (dist / 'go-build-manifest.json').write_text(json.dumps(manifest))
            (source / 'go-build-manifest.json').write_text(json.dumps(manifest))
        return source, manifest

    def verify_go(self):
        # Actual version subprocesses and archive reads run, without Node,
        # Electron metadata readers or any native wallet initialization.
        with mock.patch.object(verifier, '_electron_version', side_effect=AssertionError('Go-only must not read Electron')), contextlib.redirect_stdout(io.StringIO()):
            verifier.verify_go_only(self.root)

    def rewrite_go_archive(self, manifest, transform):
        path = self.root / 'dist' / manifest['archive']['file']
        with tarfile.open(path, 'r:gz') as archive:
            entries = [(member, archive.extractfile(member).read() if member.isfile() else None) for member in archive]
        with tarfile.open(path, 'w:gz') as archive:
            for member, content in transform(entries):
                if content is not None:
                    member.size = len(content)
                archive.addfile(member, io.BytesIO(content) if content is not None else None)
        manifest['archive']['sha256'] = digest(path)
        for folder in ('bin', 'dist'):
            (self.root / folder / 'go-build-manifest.json').write_text(json.dumps(manifest))

    def test_numeric_semver_maximum_is_not_lexical(self):
        self.assertEqual(next_release.next_version('0.1.1', ['1.9.99', '1.10.2', '1.2.999', '0.99.999']), '1.10.3')
        self.assertEqual(next_release.next_version('2.0.0', ['1.99.999']), '2.0.0')
        self.assertEqual(next_release.next_version('0.1.1', []), '0.1.1')
        self.assertEqual(next_release.next_version('0.1.1', ['0.1.1']), '0.1.2')
        self.assertEqual(next_release.next_version('0.1.1', ['v0.1.1']), '0.1.2')
        self.assertEqual(next_release.next_version('0.1.1', ['1.9.99', 'v1.10.2', 'v1.2.999']), '1.10.3')

    def test_old_and_new_tag_formats_share_one_incrementing_history(self):
        tags = ['v0.1.1', '0.1.1']
        for expected in ('0.1.2', '0.1.3', '0.1.4'):
            actual = next_release.next_version('0.1.1', tags)
            self.assertEqual(actual, expected)
            tags.append(actual)

    def test_invalid_and_out_of_policy_tags_are_ignored(self):
        tags = ['01.2.3', 'vv1.2.3', 'v01.2.3', 'V1.2.3', 'v1.2.3-beta', 'v1.2.3+build', 'v1.2.3\nINJECT=1', '1.2', '1.2.3-beta', '1.2.3+build', '1.2.3\nINJECT=1', '2147483648.0.0', 'v2147483648.0.0', '0.2147483648.0', '0.0.' + '9' * 5000, '-1.0.0', None, 1]
        self.assertEqual(next_release.next_version('0.1.1', tags), '0.1.1')
        for base in ('01.1.0', '1.2', '1.2.3;touch injected', '2147483648.0.0'):
            with self.subTest(base=base), self.assertRaises(ValueError):
                next_release.next_version(base, [])

    def test_component_and_patch_exhaustion_bounds(self):
        self.assertEqual(version.validate_version('2147483647.2147483647.2147483647'), '2147483647.2147483647.2147483647')
        self.assertEqual(next_release.next_version('0.0.0', ['1.2.2147483646']), '1.2.2147483647')
        with self.assertRaises(ValueError):
            next_release.next_version('0.0.0', ['1.2.2147483647'])
        with self.assertRaises(ValueError):
            next_release.next_version('0.0.0', ['v1.2.2147483647'])
        self.assertEqual(next_release.next_version('2.0.0', ['1.2.2147483647']), '2.0.0')

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
        for tag in ('1.9.9', '1.10.1', 'v1.10.2', '01.99.99', '3.0.0-beta'):
            subprocess.run(['git', '-C', str(self.root), 'tag', tag], check=True)
        before = subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list'])
        result = subprocess.check_output([sys.executable, str(scripts / 'next-version.py')], text=True)
        self.assertEqual(result, '1.10.3\n')
        self.assertEqual(subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list']), before)

    def test_consecutive_cli_selections_from_real_mixed_git_tags_do_not_repeat(self):
        scripts = self.root / 'scripts'
        scripts.mkdir()
        for name in ('next-version.py', 'version.py'):
            shutil.copy2(ROOT / 'scripts' / name, scripts / name)
        (self.root / 'VERSION').write_text('0.1.1\n')
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True)
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=Public Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-q', '--allow-empty', '-m', 'public fixture'], check=True)
        for tag in ('v0.1.1', '0.1.1'):
            subprocess.run(['git', '-C', str(self.root), 'tag', tag], check=True)
        for expected in ('0.1.2', '0.1.3', '0.1.4'):
            before = subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list'])
            result = subprocess.check_output([sys.executable, str(scripts / 'next-version.py')], text=True).strip()
            self.assertEqual(result, expected)
            self.assertEqual(subprocess.check_output(['git', '-C', str(self.root), 'tag', '--list']), before)
            subprocess.run(['git', '-C', str(self.root), 'tag', result], check=True)

    def test_install_preserves_unrelated_wallet_file(self):
        app, destination = self.app(), self.destination()
        target = destination / 'cryptowallet'
        target.write_text('public foreign-file sentinel')
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(target.read_text(), 'public foreign-file sentinel')
        self.assertFalse((destination / 'wallet-runtime').exists())

    def test_install_recovers_managed_launcher_after_runtime_cleanup(self):
        destination, original = self.orphaned_install()
        profile = destination / 'public-profile-sentinel'
        profile.write_bytes(b'not an actual profile or secret')
        app = self.app('0.1.1', 'new')
        self.install(app, destination)
        self.assertNotEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.1')
        runtime = (destination / 'cryptowallet').resolve().parent
        installer.verify_binaries(runtime)
        self.assertEqual((runtime / 'resources/public-fixture.txt').read_text(), 'Complete application resource for 0.1.1')
        self.assertTrue((destination / 'wallet-runtime/.localwallet-managed').is_file())
        self.assertEqual(profile.read_bytes(), b'not an actual profile or secret')

    def test_orphan_recovery_copy_failure_preserves_link_and_allows_retry(self):
        destination, original = self.orphaned_install()
        app = self.app('0.1.1', 'new')
        with mock.patch.object(installer.shutil, 'copytree', side_effect=OSError('public injected copy failure')), self.assertRaises(OSError):
            self.install(app, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertFalse(list((destination / 'wallet-runtime').glob('.staging-*')))
        self.install(app, destination)
        self.assertEqual(self.runtime_version(destination), '0.1.1')

    def test_orphan_recovery_commit_failure_preserves_link_and_allows_retry(self):
        destination, original = self.orphaned_install()
        app = self.app('0.1.1', 'new')
        with mock.patch.object(pathlib.Path, 'replace', side_effect=OSError('public injected atomic switch failure')), self.assertRaises(OSError):
            self.install(app, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertFalse(list(destination.glob('.wallet-link-*')))
        self.assertFalse(list((destination / 'wallet-runtime').glob('.staging-*')))
        self.install(app, destination)
        self.assertEqual(self.runtime_version(destination), '0.1.1')

    def test_install_refuses_unrelated_dangling_launcher_without_runtime(self):
        app, destination = self.app(), self.destination()
        target = destination / 'cryptowallet'
        for link in ('unrelated/cryptowallet', 'wallet-runtime/../../foreign/cryptowallet', 'wallet-runtime/app-not-an-id/cryptowallet', '/wallet-runtime/app-' + '0' * 32 + '/cryptowallet'):
            with self.subTest(link=link):
                target.symlink_to(link)
                with self.assertRaises(RuntimeError):
                    self.install(app, destination)
                self.assertEqual(os.readlink(target), link)
                self.assertFalse((destination / 'wallet-runtime').exists())
                target.unlink()

    def test_orphan_recovery_refuses_unsafe_runtime_paths(self):
        app, destination = self.app(), self.destination()
        target = destination / 'cryptowallet'
        original = 'wallet-runtime/app-' + '0' * 32 + '/cryptowallet'
        target.symlink_to(original)
        runtime = destination / 'wallet-runtime'
        outside = self.root / 'outside'
        outside.mkdir(mode=0o700)
        (outside / '.localwallet-managed').touch(mode=0o600)
        sentinel = outside / 'foreign'
        sentinel.write_bytes(b'public external sentinel')
        for shape in ('symlink', 'dangling-symlink', 'file', 'unmarked-directory', 'unsafe-directory'):
            with self.subTest(shape=shape):
                if shape.endswith('symlink'):
                    runtime.symlink_to(outside if shape == 'symlink' else self.root / 'missing', target_is_directory=True)
                elif shape == 'file':
                    runtime.write_bytes(b'public unrelated runtime file')
                else:
                    runtime.mkdir(mode=0o700)
                    if shape == 'unsafe-directory':
                        (runtime / '.localwallet-managed').touch(mode=0o600)
                        runtime.chmod(0o777)
                with self.assertRaises(RuntimeError):
                    self.install(app, destination)
                self.assertEqual(os.readlink(target), original)
                self.assertEqual(sentinel.read_bytes(), b'public external sentinel')
                if runtime.is_symlink() or runtime.is_file():
                    runtime.unlink()
                else:
                    shutil.rmtree(runtime)

    def test_install_preserves_unrelated_runtime_directory(self):
        app, destination = self.app(), self.destination()
        runtime = destination / 'wallet-runtime'
        runtime.mkdir()
        sentinel = runtime / 'foreign-file'
        sentinel.write_bytes(b'public foreign runtime')
        with self.assertRaises(RuntimeError):
            self.install(app, destination)
        self.assertEqual(list(runtime.iterdir()), [sentinel])
        self.assertFalse((destination / 'cryptowallet').exists())

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
        self.assertFalse((destination / 'cryptowallet').exists())

    def test_install_refuses_traversal_launcher_and_symlinked_marker(self):
        app, destination = self.app(), self.destination()
        runtime = destination / 'wallet-runtime'
        runtime.mkdir(mode=0o700)
        marker = runtime / '.localwallet-managed'
        marker.touch(mode=0o600)
        target = destination / 'cryptowallet'
        original = 'wallet-runtime/../../foreign/cryptowallet'
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
        original = os.readlink(destination / 'cryptowallet')
        new = self.app('0.1.1', 'new')
        (new / 'resources/bin/wallet-core').write_bytes(b'public tampered executable')
        with self.assertRaises(RuntimeError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
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
        self.assertFalse((destination / 'cryptowallet').exists())

    def test_copy_failure_preserves_selected_version_and_profile_files(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'cryptowallet')
        profile = destination / 'public-profile-sentinel'
        profile.write_bytes(b'not an actual profile or secret')
        new = self.app('0.1.1', 'new')
        with mock.patch.object(installer.shutil, 'copytree', side_effect=OSError('public injected copy failure')), self.assertRaises(OSError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')
        self.assertEqual(profile.read_bytes(), b'not an actual profile or secret')
        self.assertFalse(list((destination / 'wallet-runtime').glob('.staging-*')))

    def test_completed_copy_is_reverified_before_commit(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'cryptowallet')
        new = self.app('0.1.1', 'new')
        copy = shutil.copytree
        def corrupt(source, target, *args, **kwargs):
            result = copy(source, target, *args, **kwargs)
            if pathlib.Path(source) == new:
                (pathlib.Path(target) / 'resources/bin/wallet-signer').write_bytes(b'public copied-byte corruption')
            return result
        with mock.patch.object(installer.shutil, 'copytree', side_effect=corrupt), self.assertRaises(RuntimeError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.0')

    def test_upgrade_switches_one_complete_version_atomically_and_retains_old_generation(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'cryptowallet')
        new = self.app('0.1.1', 'new')
        replace = pathlib.Path.replace
        seen = []
        def commit(link, target):
            if link.name.startswith('.wallet-link-'):
                self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
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
        self.assertNotEqual(os.readlink(destination / 'cryptowallet'), original)
        self.assertEqual(self.runtime_version(destination), '0.1.1')
        self.assertTrue((destination / original).is_file())
        self.assertEqual(subprocess.check_output([str(destination / original)], text=True), '0.1.0\n')
        self.assertFalse(list(destination.glob('.wallet-link-*')))

    def test_failed_atomic_link_switch_preserves_previous_runtime(self):
        old, destination = self.app(), self.destination()
        self.install(old, destination)
        original = os.readlink(destination / 'cryptowallet')
        new = self.app('0.1.1', 'new')
        with mock.patch.object(pathlib.Path, 'replace', side_effect=OSError('public injected commit failure')), self.assertRaises(OSError):
            self.install(new, destination)
        self.assertEqual(os.readlink(destination / 'cryptowallet'), original)
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

    def test_legacy_archive_name_is_rejected_even_with_matching_hash(self):
        _, manifest = self.project()
        portable = self.root / 'dist' / manifest['portable']['file']
        legacy = portable.with_name('localwallet-0.1.1-linux-amd64.tar.gz')
        portable.rename(legacy)
        manifest['portable']['file'] = legacy.name
        manifest['portable']['sha256'] = digest(legacy)
        (self.root / 'dist/build-manifest.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, 'Portable artifact identity differs'):
            self.verify()

    def test_portable_artifact_identity_and_payload_fail_closed(self):
        app, manifest = self.project()
        portable = self.root / 'dist' / manifest['portable']['file']
        original = portable.read_bytes()
        portable.write_bytes(original + b'public tampering')
        with self.assertRaisesRegex(RuntimeError, 'Portable artifact identity differs'):
            self.verify()
        portable.write_bytes(original)
        # A self-consistent archive digest must still bind the actual runtime.
        (app / 'cryptowallet-ui').write_bytes(b'PUBLIC wrong archived Electron runtime')
        with tarfile.open(portable, 'w:gz') as archive:
            archive.add(app, arcname='CryptoWallet-linux-x64')
        shutil.copy2(self.elf['0.1.1'], app / 'cryptowallet-ui')
        manifest['portable']['sha256'] = digest(portable)
        (self.root / 'dist/build-manifest.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, 'Portable runtime payload differs'):
            self.verify()

    def test_go_only_build_has_actual_exact_versions_without_desktop_dependencies(self):
        self.go_project()
        self.verify_go()
        self.assertFalse((self.root / 'dist').exists())
        self.assertFalse((self.root / 'desktop').exists())

    def test_go_only_archive_raw_payload_and_base_manifests_agree(self):
        self.go_project(packaged=True)
        self.verify_go()

    def test_go_only_requires_each_exact_distributed_inventory(self):
        self.go_project(packaged=True)
        for name in verifier._GO_INVENTORIES:
            path = self.root / 'dist' / name
            original = path.read_bytes()
            with self.subTest(file=name, case='missing'):
                path.unlink()
                with self.assertRaisesRegex(RuntimeError, 'Distributed Go inventory hash differs'):
                    self.verify_go()
            with self.subTest(file=name, case='tampered'):
                path.write_bytes(original + b'PUBLIC tamper')
                with self.assertRaisesRegex(RuntimeError, 'Distributed Go inventory hash differs'):
                    self.verify_go()
            path.write_bytes(original)

    def test_go_only_archive_requires_and_binds_all_five_inventory_payloads(self):
        _, manifest = self.go_project(packaged=True)
        path = self.root / 'dist' / manifest['archive']['file']
        original = path.read_bytes()
        for name in verifier._GO_INVENTORIES:
            for missing in (True, False):
                with self.subTest(file=name, case='missing' if missing else 'substitution'):
                    path.write_bytes(original)
                    def transform(entries):
                        for member, content in entries:
                            if member.name.endswith('/' + name):
                                if missing:
                                    continue
                                content = b'X' + content[1:]
                            yield member, content
                    self.rewrite_go_archive(manifest, transform)
                    with self.assertRaisesRegex(RuntimeError, 'Incomplete Go component archive' if missing else 'Archived Go payload differs'):
                        self.verify_go()

    def test_go_only_inventory_manifest_is_an_exact_hash_allowlist(self):
        source, manifest = self.go_project(packaged=True)
        good = manifest['inventorySHA256']
        for bad in (None, {}, {key: value for key, value in good.items() if key != 'go-THIRD-PARTY-NOTICES.txt'},
                    {**good, 'unlisted.json': 'a' * 64}, {**good, 'go-THIRD-PARTY-NOTICES.json': 'h1:not-SHA256'}):
            changed = {**manifest, 'inventorySHA256': bad}
            (source / 'go-build-manifest.json').write_text(json.dumps(changed))
            (self.root / 'dist/go-build-manifest.json').write_text(json.dumps(changed))
            with self.subTest(value=bad), self.assertRaisesRegex(RuntimeError, 'Go release inventory manifest|Go inventory hash differs'):
                self.verify_go()

    def test_go_only_notice_pair_and_inventory_json_are_unambiguous(self):
        source, manifest = self.go_project(packaged=True)
        dist = self.root / 'dist'
        for name, content, reason in (
            ('go-THIRD-PARTY-NOTICES.json', json.dumps({'noticeSHA256': '0' * 64}).encode(), 'Go redistribution notice pair differs'),
            ('go-dependency-licenses.json', b'[{"module":"public-a","module":"public-b"}]', 'Duplicate manifest member'),
        ):
            path = dist / name
            original = path.read_bytes()
            expected = manifest['inventorySHA256'][name]
            path.write_bytes(content)
            manifest['inventorySHA256'][name] = digest(path)
            for folder in (source, dist):
                (folder / 'go-build-manifest.json').write_text(json.dumps(manifest))
            with self.subTest(file=name), self.assertRaisesRegex(RuntimeError, reason):
                self.verify_go()
            path.write_bytes(original)
            manifest['inventorySHA256'][name] = expected

    def test_go_only_distributed_inventory_links_are_refused(self):
        self.go_project(packaged=True)
        path = self.root / 'dist/go-THIRD-PARTY-NOTICES.txt'
        original = path.read_bytes()
        outside = self.root / 'PUBLIC-license-target.txt'
        outside.write_bytes(original)
        path.unlink()
        path.symlink_to(outside)
        with self.assertRaisesRegex(RuntimeError, 'Distributed Go inventory hash differs'):
            self.verify_go()
        path.unlink()
        os.link(outside, path)
        with self.assertRaisesRegex(RuntimeError, 'Distributed Go inventory hash differs'):
            self.verify_go()
        self.assertEqual(outside.read_bytes(), original)

    def test_go_only_rejects_runtime_version_prefix_despite_consistent_hashes(self):
        self.go_project(binaries='0.1.10')
        with self.assertRaisesRegex(RuntimeError, 'Go runtime version differs'):
            self.verify_go()

    def test_go_only_rejects_source_tampering_before_running_binary(self):
        source, _ = self.go_project()
        (source / 'wallet-core').write_bytes(b'PUBLIC broken ELF')
        with mock.patch.object(verifier.subprocess, 'check_output', side_effect=AssertionError('tampered bytes must not run')), self.assertRaisesRegex(RuntimeError, 'binary hash differs'):
            self.verify_go()

    def test_go_only_archive_hash_mismatch_and_distributed_payload_mismatch_fail(self):
        _, manifest = self.go_project(packaged=True)
        path = self.root / 'dist' / manifest['archive']['file']
        original = path.read_bytes()
        path.write_bytes(original + b'public tampering')
        with self.assertRaisesRegex(RuntimeError, 'archive identity differs'):
            self.verify_go()
        path.write_bytes(original)
        shutil.copy2(self.elf['0.1.10'], self.root / 'dist/wallet-signer')
        with self.assertRaisesRegex(RuntimeError, 'Distributed Go binary hash differs'):
            self.verify_go()

    def test_go_only_self_consistent_archive_hash_cannot_hide_stale_binary(self):
        _, manifest = self.go_project(packaged=True)
        def stale(entries):
            return [(member, self.elf['0.1.0'].read_bytes() if member.name.endswith('/wallet-core') else content) for member, content in entries]
        self.rewrite_go_archive(manifest, stale)
        with self.assertRaisesRegex(RuntimeError, 'Archived Go payload (size )?differs'):
            self.verify_go()

    def test_go_only_archive_rejects_traversal_duplicate_links_and_metadata_substitution(self):
        _, manifest = self.go_project(packaged=True)
        path = self.root / 'dist' / manifest['archive']['file']
        original = path.read_bytes()
        def restore():
            path.write_bytes(original)
        def traversal(entries):
            item = tarfile.TarInfo('../PUBLIC-must-not-extract')
            return entries + [(item, b'public fixture')]
        def duplicate(entries):
            return entries + [next(entry for entry in entries if entry[0].name.endswith('/wallet-core'))]
        def link(entries):
            for member, content in entries:
                if member.name.endswith('/wallet-core'):
                    member.type, member.linkname, content = tarfile.SYMTYPE, '/external/public', None
                yield member, content
        def metadata(entries):
            return [(member, b'{"schemaVersion":1}' if member.name.endswith('/go-build-manifest.json') else content) for member, content in entries]
        for change, reason in ((traversal, 'Unsafe Go archive path'), (duplicate, 'ambiguous Go archive'), (link, 'ambiguous Go archive'), (metadata, 'Archived Go build identity differs')):
            with self.subTest(case=change.__name__):
                restore()
                self.rewrite_go_archive(manifest, change)
                with self.assertRaisesRegex(RuntimeError, reason):
                    self.verify_go()
        self.assertFalse((self.root.parent / 'PUBLIC-must-not-extract').exists())

    def test_go_only_manifest_schema_duplicates_and_archive_path_fail_closed(self):
        source, manifest = self.go_project()
        filename = source / 'go-build-manifest.json'
        for value in ([], {**manifest, 'schemaVersion': True}, {**manifest, 'status': 'PRODUCTION'}, {**manifest, 'version': '0.1.10'}, {**manifest, 'archive': {'file': '../public', 'sha256': '0' * 64}}):
            filename.write_text(json.dumps(value))
            with self.subTest(value=value), self.assertRaises(RuntimeError):
                self.verify_go()
        filename.write_text(json.dumps(manifest)[:-1] + ',"version":"0.1.1"}')
        with self.assertRaisesRegex(RuntimeError, 'Duplicate manifest member'):
            self.verify_go()

    def test_optional_desktop_packager_only_produces_portable_desktop(self):
        text = (ROOT / 'scripts/package.cjs').read_text()
        for forbidden in ('dpkg-deb', '.deb', 'DEBIAN', 'apparmor', 'stagecontrol'):
            self.assertNotIn(forbidden, text.lower() if forbidden == 'apparmor' else text)
        self.assertRegex(text, r"format:\s*'portable-desktop-v1'")
        self.assertIn('tar.gz', text)
        self.assertIn('sha256', text)

    def test_release_build_and_publish_are_separate_from_complete_ci_tests(self):
        text = (ROOT / '.github/workflows/release.yml').read_text()
        tests = (ROOT / '.github/workflows/ci.yml').read_text()
        publish = text.index('gh release create ')
        gate = ' '.join(line.split('run: ', 1)[1] for line in tests.splitlines() if 'run: make ' in line)
        self.assertTrue(set(('test', 'test-security', 'test-race', 'test-interop', 'test-integration', 'test-fuzz', 'coverage', 'check-deps')).issubset(shlex.split(gate)))
        for required in ('make test-packaged', 'scripts/packaged-send.py', 'make test-recovery', 'make test-crash', 'WALLET_USDT_EVM=1'):
            self.assertIn(required, tests)
        for forbidden in ('make test', 'scripts/provision-tests.py', 'scripts/ci-desktop.py', 'scripts/record-evidence.py', 'test-security', 'test-race', 'xvfb', 'tesseract'):
            self.assertNotIn(forbidden, text)
        build, release = text.split('  release:\n', 1)
        self.assertIn('fetch-depth: 0', build)
        self.assertIn('fetch-tags: true', build)
        self.assertEqual(build.count('make package-desktop'), 1)
        self.assertIn('needs: build', release)
        self.assertIn('artifact-id: ${{ steps.payload.outputs.artifact-id }}', build)
        self.assertIn('artifact-ids: ${{ needs.build.outputs.artifact-id }}', release)
        self.assertIn('digest-mismatch: error', release)
        self.assertIn('compression-level: 0', build)
        self.assertIn('WALLET_VERSION: ${{ needs.build.outputs.version }}', release)
        self.assertNotIn('make ', release)
        self.assertNotIn('contents: write', build)
        self.assertIn("'tests':'NOT_RUN: independent .github/workflows/ci.yml workflow'", build)
        self.assertIn('scripts/release_assets.py check', release)
        self.assertLess(release.index('scripts/release_assets.py check'), release.index('node .github/provenance/attest.mjs'))
        self.assertLess(text.index('scripts/verify-version.py'), publish)
        self.assertIn('scripts/verify-version.py\n', text[:publish])
        self.assertNotIn('package-go', text)
        self.assertIn('make package-desktop', text[:publish])
        self.assertLess(text.index('scripts/release_assets.py verify'), publish)
        self.assertLess(text.index('node .github/provenance/attest.mjs'), publish)
        self.assertIn('npm --prefix .github/provenance ci --ignore-scripts', text[:publish])
        self.assertIn('id-token: write', text[:publish])
        self.assertNotIn('attestations: write', text)
        self.assertNotIn('actions/attest-build-provenance@', text)
        self.assertIn('dist/cryptowallet-$RELEASE_VERSION-linux-amd64.tar.gz', text[publish:])
        publication = text[publish:].split('      - uses:', 1)[0]
        self.assertIn('gh release create "$RELEASE_VERSION"', publication)
        self.assertIn('--title "$RELEASE_VERSION"', publication)
        for forbidden in ('dist/wallet-', 'dist/test-evidence', 'dist/THIRD-PARTY', 'dist/release.json', 'dist/release-provenance', 'dist/RELEASE-SHA256SUMS', 'cryptowallet-go-'):
            self.assertNotIn(forbidden, publication)
        self.assertIn('name: wallet-release-verification-${{ needs.build.outputs.version }}', text[:publish])
        self.assertIn('retention-days: 90', text[:publish])
        self.assertNotIn('.deb', text[publish:])
        self.assertNotIn('git tag ', text[:publish])
        self.assertNotIn('git push ', text[:publish])
        self.assertNotIn('--prerelease', text[publish:])
        self.assertNotIn('--draft', text[publish:])
        self.assertIn('--target "$GITHUB_SHA"', text[publish:])
        self.assertIn('cancel-in-progress: false', text)

    def test_release_build_metadata_marks_tests_not_run_for_exact_source(self):
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        block = workflow.split("          python3 - <<'PY'\n", 1)[1].split('\n          PY', 1)[0]
        program = '\n'.join(line[10:] if line.startswith('          ') else line for line in block.splitlines())
        (self.root / 'dist').mkdir()
        commit = 'a' * 40
        subprocess.run([sys.executable, '-c', program], cwd=self.root, check=True,
                       env={'WALLET_VERSION': '0.1.10', 'RELEASE_REPOSITORY': 'fixture/wallet', 'GITHUB_SHA': commit, 'GITHUB_RUN_ID': '123456'},
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        report = json.loads((self.root / 'reports/release-build.json').read_text())
        self.assertEqual(report['status'], 'BUILD_ONLY')
        self.assertEqual(report['version'], '0.1.10')
        self.assertEqual(report['sourceCommit'], commit)
        self.assertTrue(report['tests'].startswith('NOT_RUN:'))
        self.assertEqual(report['productionReadiness'], 'NOT_RUN')
        self.assertEqual(report['checksURL'], f'https://github.com/fixture/wallet/commit/{commit}/checks')
        notes = (self.root / 'dist/RELEASE-NOTES.md').read_text()
        self.assertIn('CryptoWallet 0.1.10', notes)
        self.assertIn('cryptowallet-0.1.10-linux-amd64.tar.gz', notes)
        self.assertIn('CryptoWallet-linux-x64/cryptowallet', notes)
        self.assertNotIn('LocalWallet', notes)
        self.assertNotIn('localwallet-', notes)
        self.assertIn(report['checksURL'], notes)
        self.assertIn('NOT_RUN', notes)
        self.assertIn('historical', notes)
        self.assertIn('wallet-release-verification-0.1.10', notes)
        self.assertIn('https://github.com/fixture/wallet/actions/runs/123456', notes)

    def test_publication_command_uploads_only_archive_and_uses_exact_version(self):
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        block = workflow.split('          gh release create ', 1)[1].split('      - uses:', 1)[0]
        command = 'gh release create ' + block.replace('            ', '').strip()
        fakebin = self.root / 'fake-bin'
        fakebin.mkdir()
        capture = self.root / 'arguments.json'
        transport = fakebin / 'gh'
        transport.write_text('#!' + sys.executable + '\nimport json,os,sys\nopen(os.environ["CAPTURE"],"w").write(json.dumps(sys.argv[1:]))\n')
        transport.chmod(0o755)
        tags = ['v0.1.1', '0.1.1']
        for expected in ('0.1.2', '0.1.3'):
            selected = next_release.next_version('0.1.1', tags)
            self.assertEqual(selected, expected)
            subprocess.run(['bash', '-eu', '-c', command], cwd=self.root, check=True,
                           env={**os.environ, 'PATH': str(fakebin) + os.pathsep + os.environ['PATH'], 'CAPTURE': str(capture), 'RELEASE_VERSION': selected, 'GITHUB_SHA': 'a' * 40})
            self.assertEqual(json.loads(capture.read_text()), ['release', 'create', expected, '--target', 'a' * 40, '--title', expected, '--notes-file', 'dist/RELEASE-NOTES.md', f'dist/cryptowallet-{expected}-linux-amd64.tar.gz'])
            embedded = subprocess.check_output([sys.executable, str(ROOT / 'scripts/version.py')], env={**os.environ, 'WALLET_VERSION': selected}, text=True)
            self.assertEqual(embedded, expected + '\n')
            tags.append(selected)

    def test_new_install_preserves_legacy_wallet_entry_and_existing_data(self):
        destination = self.destination()
        legacy = destination / 'wallet'
        legacy.symlink_to('wallet-runtime/app-' + 'a' * 32 + '/wallet-launcher')
        sentinel = destination / 'public-existing-data'
        sentinel.write_text('public fixture data')
        self.install(self.app('0.1.1'), destination)
        self.assertEqual(self.runtime_version(destination), '0.1.1')
        self.assertEqual(os.readlink(legacy), 'wallet-runtime/app-' + 'a' * 32 + '/wallet-launcher')
        self.assertEqual(sentinel.read_text(), 'public fixture data')

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
        go_dependency = self.root / 'public-go-dependency'
        go_dependency.mkdir()
        (go_dependency / 'LICENSE').write_text('Public license fixture; not a legal opinion.')
        original = subprocess.check_output
        def public_tools(args, **kwargs):
            if args[:2] == ['go', 'list']:
                return json.dumps({'Main': True, 'Path': 'publicfixture'}) + json.dumps({'Path': 'example.invalid/public-dependency', 'Version': 'v1.0.0', 'Dir': str(go_dependency), 'Sum': 'h1:PUBLIC'})
            if args[:2] == ['git', 'status']:
                return ' M public-source.go\n'
            if args[:2] == ['git', 'ls-files']:
                return b'public-source.go\0public-source-link\0'
            if args[:2] == ['git', 'rev-parse']:
                return 'a' * 40 + '\n'
            if args[:2] == ['python3', 'scripts/version.py']:
                return original(args, **kwargs)
            self.fail('Unexpected build-tool capability: ' + repr(args))
        with mock.patch.object(sys, 'argv', [str(scripts / 'sbom.py')]), mock.patch.object(subprocess, 'check_output', side_effect=public_tools), contextlib.redirect_stdout(io.StringIO()):
            runpy.run_path(str(scripts / 'sbom.py'), run_name='__main__')
        sbom = json.loads((self.root / 'dist/sbom.cdx.json').read_text())
        components = {entry['name']: entry for entry in sbom['components']}
        self.assertEqual(components['electron']['scope'], 'required')
        self.assertEqual(components['typescript']['scope'], 'excluded')
        manifest = json.loads((self.root / 'dist/source-manifest.json').read_text())
        self.assertEqual(manifest['workingTree'], 'dirty')
        self.assertEqual(manifest['sha256'][link.name], hashlib.sha256(os.readlink(link).encode()).hexdigest())
        self.assertNotEqual(manifest['sha256'][link.name], digest(external))
        # Go-only mode never reads a package lock or invokes an npm/Node tool.
        (desktop / 'package-lock.json').unlink()
        with mock.patch.object(sys, 'argv', [str(scripts / 'sbom.py'), '--go-only']), mock.patch.object(subprocess, 'check_output', side_effect=public_tools), contextlib.redirect_stdout(io.StringIO()):
            runpy.run_path(str(scripts / 'sbom.py'), run_name='__main__')
        go_sbom = json.loads((self.root / 'dist/go-sbom.cdx.json').read_text())
        self.assertEqual([component['name'] for component in go_sbom['components']], ['example.invalid/public-dependency'])
        self.assertTrue(all(component['purl'].startswith('pkg:golang/') for component in go_sbom['components']))
        self.assertEqual(go_sbom['metadata']['component']['version'], '0.1.1')
        licenses = json.loads((self.root / 'dist/go-dependency-licenses.json').read_text())
        self.assertEqual(licenses[0]['licenseSHA256'], {'LICENSE': digest(go_dependency / 'LICENSE')})
        self.assertIn('NOT_RUN', licenses[0]['review'])
        go_source = json.loads((self.root / 'dist/go-source-manifest.json').read_text())
        self.assertEqual(go_source['sha256'], manifest['sha256'])


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
        filenames = ['scripts/version.py', 'scripts/next-version.py', 'scripts/install.py', 'scripts/verify-version.py', 'scripts/package.cjs', 'scripts/package-go.py', 'scripts/with-toolchain.sh', 'scripts/bootstrap-node.py', 'scripts/sbom.py', 'scripts/build-tools-test.py', 'Makefile', '.github/workflows/ci.yml', '.github/workflows/release.yml']
        report = {
            'status': 'PASS' if result.wasSuccessful() and not result.skipped else 'FAIL' if not result.wasSuccessful() else 'NOT_RUN',
            'command': [sys.executable, 'scripts/build-tools-test.py'],
            'tests': result.outcomes,
            'scope': 'Hermetic public fixture ELFs, real temporary portable archives and Git repository; ASAR fixture reader and SBOM inventory tool outputs are substituted. No wallet profiles, network, funding or remote publication.',
            'sourceSHA256': {name: digest(ROOT / name) for name in filenames},
        }
        (ROOT / 'reports').mkdir(exist_ok=True)
        (ROOT / 'reports/build-tools.json').write_text(json.dumps(report, indent=2) + '\n')
        return result


if __name__ == '__main__':
    unittest.main(verbosity=2, testRunner=EvidenceRunner)
