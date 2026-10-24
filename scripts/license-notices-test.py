#!/usr/bin/env python3
"""Notice provenance, packaging identity and filesystem boundary regressions."""
import base64
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

specification = importlib.util.spec_from_file_location('license_notices', Path(__file__).with_name('license-notices.py'))
notices = importlib.util.module_from_spec(specification)
specification.loader.exec_module(notices)
INTEGRITY = 'sha512-' + base64.b64encode(bytes(64)).decode()


class NoticeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='localwallet-license-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def put(self, relative, data=b'Public synthetic license text.\n'):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def module(self):
        folder = self.root / 'cache/example.org/library@v1.2.3'
        self.put('cache/example.org/library@v1.2.3/LICENSE')
        return dict(Path='example.org/library', Version='v1.2.3', Dir=str(folder),
                    Sum='h1:' + base64.b64encode(bytes(32)).decode())

    def go_tool(self, module):
        self.put('toolchain/LICENSE')
        self.put('toolchain/PATENTS', b'Public synthetic patent grant.\n')
        self.put('go.mod', b'module test\n')
        self.put('go.sum', b'public pinned test digest\n')
        calls = []
        def run(root, *arguments):
            self.assertEqual(root, self.root)
            calls.append(arguments)
            if arguments == ('go', 'mod', 'verify'):
                return 'all modules verified\n'
            if arguments == ('go', 'env', 'GOMODCACHE'):
                return str(self.root / 'cache') + '\n'
            if arguments == ('go', 'env', 'GOROOT'):
                return str(self.root / 'toolchain') + '\n'
            if arguments == ('go', 'version'):
                return 'go version go1.27.3 linux/amd64\n'
            if arguments[:4] == ('go', 'list', '-deps', '-json'):
                # Actual production package closure deliberately excludes an
                # unshipped graph-only module with unavailable license source.
                return json.dumps(dict(Module=module)) + '\n' + json.dumps(dict(Module=module))
            self.fail('Unexpected tool invocation')
        return run, calls

    def desktop(self, production=False):
        runtime = self.root / 'runtime'
        self.put('runtime/LICENSE', b'Public Electron copyright and license.\n')
        self.put('runtime/LICENSES.chromium.html', b'<html>Public Chromium redistribution notices</html>\n')
        self.put('runtime/LocalWallet', b'\x00\xffsynthetic executable bytes')
        self.put('runtime/resources/app.asar', b'\x00\xffsynthetic ASAR bytes')
        packages = {'': dict(name='localwallet-test', version='1.0.0'),
                    'node_modules/electron': dict(version='45.0.0', dev=True, integrity=INTEGRITY),
                    'node_modules/dev-only': dict(version='1.0.0', dev=True, integrity=INTEGRITY)}
        files = ['/app/main.js', '/package.json']
        hashes = {}
        if production:
            location = 'node_modules/@example/library'
            packages[location] = dict(version='1.2.3', integrity=INTEGRITY)
            package = self.put('desktop/' + location + '/package.json', json.dumps(dict(name='@example/library', version='1.2.3')).encode())
            license = self.put('desktop/' + location + '/LICENSE')
            notice = self.put('desktop/' + location + '/NOTICE', b'Additional public attribution.\n')
            files += ['/node_modules', '/node_modules/@example', '/' + location,
                      '/' + location + '/package.json', '/' + location + '/LICENSE', '/' + location + '/NOTICE']
            hashes = {location + '/' + path.name: notices.sha256(path.read_bytes()) for path in (package, license, notice)}
        self.put('desktop/package-lock.json', json.dumps(dict(lockfileVersion=3, packages=packages)).encode())
        return runtime, dict(files=files, hashes=hashes)

    def inspect(self, result):
        def run(arguments, **keywords):
            self.assertEqual(arguments[:3], ['node', '--input-type=module', '-e'])
            self.assertEqual(keywords['cwd'], self.root)
            return json.dumps(result)
        return run

    def test_nested_verbatim_notice_texts_and_source_files(self):
        self.put('module/LICENSE', b'Copyright test\r\nExact upstream text.\r\n')
        self.put('module/lib/NOTICE')
        self.put('module/lib/COPYING.LESSER')
        self.put('module/PATENTS')
        self.put('module/license_test.go', b'package synthetic\n')
        rows = notices.collect_texts(self.root / 'module')
        self.assertEqual([path for path, _ in rows], ['LICENSE', 'PATENTS', 'lib/COPYING.LESSER', 'lib/NOTICE'])
        self.assertIn(b'\r\n', rows[0][1])

    def test_missing_primary_license_is_not_silently_omitted(self):
        self.put('module/NOTICE')
        with self.assertRaises(notices.NoticeError):
            notices.collect_texts(self.root / 'module')

    def test_rejects_linked_and_outside_license_sources(self):
        outside = self.put('outside/LICENSE')
        module = self.root / 'module'
        module.mkdir()
        (module / 'LICENSE').symlink_to(outside)
        with self.assertRaises(notices.NoticeError):
            notices.collect_texts(module)
        (module / 'LICENSE').unlink()
        os.link(outside, module / 'LICENSE')
        with self.assertRaises(notices.NoticeError):
            notices.collect_texts(module)
        (module / 'LICENSE').unlink()
        self.put('module/LICENSE')
        (module / 'linked-directory').symlink_to(outside.parent, target_is_directory=True)
        with self.assertRaises(notices.NoticeError):
            notices.collect_texts(module)
        (self.root / 'alias').symlink_to(module, target_is_directory=True)
        with self.assertRaises(notices.NoticeError):
            notices.read_file(self.root / 'alias', 'LICENSE')

    def test_rejects_unsafe_relative_paths_and_bounded_nontext_files(self):
        self.put('LICENSE')
        for path in ('../LICENSE', '/LICENSE', 'part/../LICENSE', './LICENSE', 'part//LICENSE', 'part\\LICENSE', 'LICENSE\n', ''):
            with self.subTest(path=repr(path)), self.assertRaises(notices.NoticeError):
                notices.read_file(self.root, path)
        for data in (b'', b'\xff', b'a\x00b', b'12345'):
            self.put('LICENSE', data)
            with self.subTest(size=len(data)), self.assertRaises(notices.NoticeError):
                notices.read_file(self.root, 'LICENSE', maximum=4)
        os.mkfifo(self.root / 'COPYING')
        with self.assertRaises(notices.NoticeError):
            notices.read_file(self.root, 'COPYING')

    def test_deterministic_render_preserves_bytes_and_review_status(self):
        components, texts = [], []
        notices.add_component(components, texts, 'public:b', dict(version='2'), [('NOTICE', b'Exact public attribution.\r\n')])
        notices.add_component(components, texts, 'public:a', dict(version='1'), [('LICENSE', b'Exact public license.\n')])
        first = notices.render(components, texts, 'Synthetic technical inventory', dict(sha256='public'))
        second = notices.render(list(reversed(components)), list(reversed(texts)), 'Synthetic technical inventory', dict(sha256='public'))
        self.assertEqual(first, second)
        self.assertIn(b'Exact public attribution.\r\n', first[0])
        metadata = json.loads(first[1])
        self.assertEqual(metadata['noticeSHA256'], notices.sha256(first[0]))
        self.assertTrue(metadata['review'].startswith('NOT_RUN'))
        self.assertNotIn(str(self.root).encode(), first[1])
        with self.assertRaises(notices.NoticeError):
            notices.add_component(components, texts, 'public:a', {}, [('LICENSE', b'x')])

    def test_actual_go_closure_is_pinned_and_verified_without_node(self):
        module = self.module()
        fake, calls = self.go_tool(module)
        components, texts = [], []
        with mock.patch.object(notices, 'run', fake):
            provenance = notices.collect_go(self.root, components, texts)
        self.assertEqual(len(components), 2)
        self.assertEqual(len(texts), 3)
        self.assertIn(('go', 'mod', 'verify'), calls)
        self.assertFalse(any('node' in command for command in calls))
        self.assertEqual(provenance['commands'], list(notices.COMMANDS))
        self.assertEqual(components[0]['provenance']['goModuleDirectorySum'], module['Sum'])

    def test_unpinned_replaced_and_outside_go_modules_fail(self):
        good = self.module()
        for mutate in (lambda m: m.update(Replace={}), lambda m: m.pop('Sum'),
                       lambda m: m.update(Sum='h1:not-a-digest'), lambda m: m.update(Version=''),
                       lambda m: m.update(Dir=str(self.root))):
            bad = copy.deepcopy(good)
            mutate(bad)
            fake, _ = self.go_tool(bad)
            with mock.patch.object(notices, 'run', fake), self.assertRaises(notices.NoticeError):
                notices.collect_go(self.root, [], [])

    def test_checksum_syntax_is_not_treated_as_provenance(self):
        self.assertTrue(notices.valid_integrity(INTEGRITY))
        for value in (None, 'sha512-missing', 'unknown-' + base64.b64encode(bytes(64)).decode(), 'sha512-' + base64.b64encode(bytes(32)).decode()):
            self.assertFalse(notices.valid_integrity(value))

    def test_complete_electron_texts_and_zero_production_npm(self):
        runtime, inspected = self.desktop()
        components, texts = [], []
        with mock.patch.object(notices.subprocess, 'check_output', self.inspect(inspected)):
            provenance = notices.collect_desktop(self.root, runtime, components, texts)
        self.assertEqual(provenance['productionNpmPackages'], 0)
        self.assertEqual([path for _, path, _ in texts], ['LICENSE', 'LICENSES.chromium.html'])
        self.assertEqual(components[0]['provenance']['runtimeSHA256'], notices.sha256((runtime / 'LocalWallet').read_bytes()))
        (runtime / 'LICENSES.chromium.html').unlink()
        with mock.patch.object(notices.subprocess, 'check_output', self.inspect(inspected)), self.assertRaises(notices.NoticeError):
            notices.collect_desktop(self.root, runtime, [], [])

    def test_production_npm_requires_exact_bundled_notices_and_metadata(self):
        runtime, inspected = self.desktop(production=True)
        components, texts = [], []
        with mock.patch.object(notices.subprocess, 'check_output', self.inspect(inspected)):
            provenance = notices.collect_desktop(self.root, runtime, components, texts)
        self.assertEqual(provenance['productionNpmPackages'], 1)
        self.assertEqual(len(components), 2)
        for filename in ('LICENSE', 'NOTICE', 'package.json'):
            bad = copy.deepcopy(inspected)
            bad['hashes']['node_modules/@example/library/' + filename] = '0' * 64
            with self.subTest(filename=filename), mock.patch.object(notices.subprocess, 'check_output', self.inspect(bad)), self.assertRaises(notices.NoticeError):
                notices.collect_desktop(self.root, runtime, [], [])

    def test_unknown_npm_packages_and_nested_injections_fail(self):
        locations = ['node_modules/@scope/one', 'node_modules/@scope/one/node_modules/two']
        notices.verify_npm_paths(['/node_modules', '/node_modules/@scope', '/node_modules/@scope/one/index.js',
                                 '/node_modules/@scope/one/node_modules', '/node_modules/@scope/one/node_modules/two/LICENSE'], locations)
        for files in (['/node_modules/evil/index.js'], ['/node_modules/@scope/one/node_modules/evil/LICENSE'],
                      ['/node_modules/@foreign'], ['/node_modules'], ['/node_modulesX/evil']):
            with self.subTest(files=files), self.assertRaises(notices.NoticeError):
                notices.verify_npm_paths(files, [] if files == ['/node_modules'] else locations)

    def test_atomic_outputs_reject_links_and_copy_into_runtime(self):
        folder = self.root / 'dist'
        with self.assertRaises(notices.NoticeError):
            notices.write_output(folder, '../outside.txt', b'never write outside')
        notices.write_output(folder, 'NOTICE.txt', b'first')
        notices.write_output(folder, 'NOTICE.txt', b'second')
        self.assertEqual((folder / 'NOTICE.txt').read_bytes(), b'second')
        self.assertFalse(list(folder.glob('.notice-*')))
        outside = self.put('outside.txt')
        (folder / 'linked.txt').symlink_to(outside)
        with self.assertRaises(notices.NoticeError):
            notices.write_output(folder, 'linked.txt', b'never overwrite outside')
        os.link(outside, folder / 'hardlinked.txt')
        with self.assertRaises(notices.NoticeError):
            notices.write_output(folder, 'hardlinked.txt', b'never overwrite outside')
        self.assertEqual(outside.read_bytes(), b'Public synthetic license text.\n')

    def test_go_only_generation_never_reads_desktop(self):
        module = self.module()
        fake, _ = self.go_tool(module)
        with mock.patch.object(notices, 'run', fake), mock.patch.object(notices, 'collect_desktop', side_effect=AssertionError('Desktop accessed')):
            first = notices.generate(self.root, go_only=True)
            before = (self.root / 'dist/go-THIRD-PARTY-NOTICES.txt').read_bytes()
            second = notices.generate(self.root, go_only=True)
        self.assertEqual(first, second)
        self.assertEqual(before, (self.root / 'dist/go-THIRD-PARTY-NOTICES.txt').read_bytes())
        self.assertFalse((self.root / 'dist/THIRD-PARTY-NOTICES.txt').exists())


if __name__ == '__main__':
    unittest.main()
