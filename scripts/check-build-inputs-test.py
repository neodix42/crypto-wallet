#!/usr/bin/env python3
"""Real disposable Git indexes exercise missing checkout and cache inputs."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('tested_build_inputs', ROOT / 'scripts/check-build-inputs.py')
inputs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inputs)
LOCK = 'internal/webui/frontend/package-lock.json'
FORK_SOURCE = 'third_party/wails/wails.go'


class BuildInputs(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture = tempfile.TemporaryDirectory(prefix='wallet-build-input-source-')
        cls.addClassCleanup(cls.fixture.cleanup)
        cls.template = Path(cls.fixture.name)
        for name in inputs.REQUIRED:
            source, target = ROOT / name, cls.template / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
        for name in inputs.SOURCE_TREES:
            shutil.copytree(ROOT / name, cls.template / name, dirs_exist_ok=True,
                            ignore=lambda folder, names: [name for name in names
                                if inputs.generated((Path(folder) / name).relative_to(ROOT).as_posix())])

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-build-input-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        shutil.copytree(self.template, self.root, dirs_exist_ok=True)
        self.env = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
        self.env.update(GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_COUNT='0')
        environment = mock.patch.dict(os.environ, self.env, clear=True)
        environment.start()
        self.addCleanup(environment.stop)
        self.git('init', '--quiet', '--template=')
        self.git('add', '--all')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], env=self.env, stderr=subprocess.PIPE)

    def fails(self, text):
        with self.assertRaisesRegex((inputs.BuildInputError, ValueError), text):
            inputs.verify(self.root)

    def test_complete_staged_checkout_passes_without_a_commit(self):
        report = inputs.verify(self.root)
        self.assertEqual(report['status'], 'PASS')
        self.assertEqual(report['wails']['finalFiles'], 976)
        self.assertGreater(report['trackedBuildInputs'], 1000)

    def test_missing_lockfile_fails(self):
        (self.root / LOCK).unlink()
        self.fails('Missing or linked build input: ' + LOCK)

    def test_untracked_existing_lockfile_fails(self):
        self.git('rm', '--cached', '--', LOCK)
        self.assertTrue((self.root / LOCK).is_file())
        self.fails('Untracked build input.*' + LOCK)

    def test_missing_fork_source_fails(self):
        (self.root / FORK_SOURCE).unlink()
        self.fails('Missing or linked build input: ' + FORK_SOURCE)

    def test_untracked_existing_fork_source_fails(self):
        self.git('rm', '--cached', '--', FORK_SOURCE)
        self.assertTrue((self.root / FORK_SOURCE).is_file())
        self.fails('Untracked build input.*' + FORK_SOURCE)

    def test_changed_fork_source_fails_full_inventory_verification(self):
        with (self.root / FORK_SOURCE).open('a') as stream:
            stream.write('\n// Test-only unreviewed change.\n')
        self.fails('Unlisted or changed Wails source: wails.go')

    def test_untracked_frontend_source_fails_even_when_gitignored(self):
        name = 'internal/webui/frontend/src/uncommitted.ts'
        (self.root / name).write_text('export const fixture = true;\n')
        (self.root / '.git/info').mkdir()
        (self.root / '.git/info/exclude').write_text(name + '\n')
        self.fails('Untracked build input.*' + name)

    def test_missing_tracked_frontend_source_fails(self):
        name = 'internal/webui/frontend/src/PublicApp.tsx'
        (self.root / name).unlink()
        self.fails('Missing or linked build input: ' + name)

    def test_generated_bundle_and_npm_install_are_not_commit_requirements(self):
        for name in ('internal/webui/frontend/dist/index.html',
                     'internal/webui/frontend/node_modules/react/package.json'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('Disposable generated fixture.\n')
        self.assertEqual(inputs.verify(self.root)['status'], 'PASS')

    def test_linked_build_helper_fails(self):
        target = self.root / 'scripts/build-frontend.sh'
        target.unlink()
        target.symlink_to('build.sh')
        self.fails('Missing or linked build input: scripts/build-frontend.sh')

    def test_frontend_dependency_lock_drift_fails(self):
        path = self.root / LOCK
        lock = json.loads(path.read_text())
        lock['packages']['']['dependencies']['react'] = '0.0.0-test-fixture'
        path.write_text(json.dumps(lock))
        self.fails('Frontend package/lock mismatch: dependencies')

    def test_workflows_check_inputs_before_compiler_setup(self):
        release = (ROOT / '.github/workflows/release.yml').read_text().split('  release:', 1)[0]
        self.assertLess(release.index('python3 scripts/check-build-inputs.py'), release.index('actions/setup-go@'))
        self.assertLess(release.index('python3 scripts/check-build-inputs.py'), release.index('actions/setup-node@'))
        self.assertIn('cache-dependency-path: ' + LOCK, release)
        ci = (ROOT / '.github/workflows/ci.yml').read_text()
        for job in (ci.split('  wails-wallet:', 1)[1].split('  checks:', 1)[0], ci.split('  checks:', 1)[1]):
            self.assertLess(job.index('python3 scripts/check-build-inputs.py'), job.index('actions/setup-go@'))
            self.assertLess(job.index('python3 scripts/check-build-inputs.py'), job.index('actions/setup-node@'))
        self.assertIn('python3 scripts/check-build-inputs-test.py', ci)


if __name__ == '__main__':
    unittest.main(verbosity=2)
