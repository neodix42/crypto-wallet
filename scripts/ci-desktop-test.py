#!/usr/bin/env python3
"""Offline CI AppArmor regressions; never load policy or invoke sudo/aa-exec."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('tested_ci_desktop', ROOT / 'scripts/ci-desktop.py')
ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci)


class CIDesktop(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-ci-desktop-unit-')
        self.addCleanup(self.temporary.cleanup)
        self.folder = pathlib.Path(self.temporary.name)
        self.workspace = self.folder / 'workspace'
        self.workspace.mkdir()
        self.profiles = self.folder / 'profiles'
        self.profiles.write_text('unrelated-profile (enforce)\n')
        self.enabled = self.folder / 'enabled'
        self.enabled.write_text('Y\n')
        self.environment = {'GITHUB_ACTIONS': 'true', 'RUNNER_ENVIRONMENT': 'github-hosted',
                            'GITHUB_WORKSPACE': str(self.workspace)}
        for patch in [mock.patch.object(ci, 'ROOT', self.workspace),
                      mock.patch.object(ci, 'PROFILES', self.profiles),
                      mock.patch.object(ci, 'ENABLED', self.enabled),
                      mock.patch.object(ci.platform, 'system', return_value='Linux'),
                      mock.patch.object(ci.os, 'geteuid', return_value=self.workspace.stat().st_uid),
                      mock.patch.object(ci.pathlib.Path, 'cwd', return_value=self.workspace),
                      mock.patch.object(ci, 'read_profiles', side_effect=lambda: self.profiles.read_text()),
                      mock.patch.dict(os.environ, self.environment, clear=True)]:
            patch.start()
            self.addCleanup(patch.stop)
        self.events = []
        self.commands = mock.patch.object(ci.subprocess, 'run', side_effect=self.subprocess)
        self.run = self.commands.start()
        self.addCleanup(self.commands.stop)

    def mark_loaded(self, mode='unconfined'):
        self.profiles.write_text('unrelated-profile (enforce)\n' + ci.profile_name() + ' (' + mode + ')\n')

    def subprocess(self, command, **kwargs):
        # Every privileged call is intercepted, including malformed calls.
        self.assertEqual(kwargs['cwd'], self.workspace)
        self.events.append((command[:], kwargs.copy()))
        if command[0] == ci.SUDO:
            self.assertEqual(command[:3], [ci.SUDO, '-n', ci.PARSER])
            self.assertEqual(command[4], '-K')
            self.assertTrue(kwargs['check'])
            policy = pathlib.Path(command[5])
            self.assertEqual(stat.S_IMODE(policy.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(policy.parent.stat().st_mode), 0o700)
            self.assertEqual(policy.read_text(), ci.policy_text())
            self.assertTrue(policy.is_relative_to(pathlib.Path(tempfile.gettempdir())))
            self.assertNotEqual(policy.parent, self.workspace)
            if command[3] == '-r':
                self.mark_loaded()
            else:
                self.assertEqual(command[3], '-R')
                self.profiles.write_text('unrelated-profile (enforce)\n')
        else:
            self.assertEqual(command[:4], [ci.AA_EXEC, '-p', ci.profile_name(), '--'])
            self.assertFalse(kwargs['check'])
        return subprocess.CompletedProcess(command, 0)

    def test_policy_has_only_a_named_userns_permission_and_no_executable_attachment(self):
        name = ci.profile_name()
        self.assertRegex(name, r'^localwallet-ci-desktop-[a-f0-9]{24}$')
        self.assertEqual(ci.policy_text(), 'abi <abi/4.0>,\nprofile ' + name + ' flags=(unconfined) {\n  userns,\n}\n')
        self.assertNotIn('/', ci.policy_text().splitlines()[1])
        self.assertNotIn('*', ci.policy_text())
        self.assertNotIn('capability', ci.policy_text())
        self.assertNotIn('change_profile', ci.policy_text())

    def test_profile_name_is_workspace_specific_and_cannot_interpret_path_as_policy(self):
        original = ci.profile_name()
        for name in ['other', 'path with spaces', 'quote"{userns,}']:
            with mock.patch.object(ci, 'ROOT', self.folder / name):
                self.assertNotEqual(ci.profile_name(), original)
                self.assertRegex(ci.profile_name(), r'^localwallet-ci-desktop-[a-f0-9]{24}$')
        self.assertEqual(ci.profile_name(), original)

    def test_actual_parser_compiles_the_profile_offline_without_loading_or_cache_writes(self):
        if not pathlib.Path(ci.PARSER).is_file():
            raise RuntimeError('BLOCKED: apparmor_parser required for offline policy verification')
        policy = self.folder / 'compile-only-profile'
        policy.write_text(ci.policy_text())
        # Recover the real subprocess function solely for -Q -K: no kernel load,
        # sudo, aa-exec, system configuration write or profile cache is allowed.
        self.commands.stop()
        try:
            result = subprocess.run([ci.PARSER, '-Q', '-K', str(policy)],
                                    check=False, text=True, capture_output=True)
        finally:
            self.commands.start()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, '')

    def test_every_entry_point_rejects_non_hosted_non_ci_non_linux_and_root_before_subprocesses(self):
        variants = [{}, {'GITHUB_ACTIONS': 'false'}, {'GITHUB_ACTIONS': '1'},
                    {'RUNNER_ENVIRONMENT': 'self-hosted'}, {'RUNNER_ENVIRONMENT': ''}]
        for change in variants:
            env = {} if not change else {**self.environment, **change}
            with mock.patch.dict(os.environ, env, clear=True):
                for action in [ci.setup, ci.cleanup, lambda: ci.run(['make', 'test-packaged'])]:
                    with self.assertRaisesRegex(RuntimeError, 'GitHub-hosted'):
                        action()
        for patch in [mock.patch.object(ci.platform, 'system', return_value='Darwin'),
                      mock.patch.object(ci.os, 'geteuid', return_value=0)]:
            with patch:
                for action in [ci.setup, ci.cleanup, lambda: ci.run(['make', 'test-packaged'])]:
                    with self.assertRaisesRegex(RuntimeError, 'GitHub-hosted'):
                        action()
        self.run.assert_not_called()

    def test_workspace_must_be_absolute_owned_and_match_both_repository_and_working_directory(self):
        for value in ['', '.', str(self.folder), str(self.folder / 'missing')]:
            with mock.patch.dict(os.environ, {'GITHUB_WORKSPACE': value}):
                with self.assertRaises((RuntimeError, FileNotFoundError)):
                    ci.setup()
        with mock.patch.object(ci.pathlib.Path, 'cwd', return_value=self.folder):
            with self.assertRaisesRegex(RuntimeError, 'workspace'):
                ci.setup()
        with mock.patch.object(ci.os, 'geteuid', return_value=self.workspace.stat().st_uid + 1):
            with self.assertRaisesRegex(RuntimeError, 'workspace'):
                ci.setup()
        self.run.assert_not_called()

    def test_apparmor_absence_fails_closed(self):
        self.enabled.write_text('N\n')
        for action in [ci.setup, ci.cleanup, lambda: ci.run(['make', 'test-packaged'])]:
            with self.assertRaisesRegex(RuntimeError, 'AppArmor is unavailable'):
                action()
        self.enabled.unlink()
        with self.assertRaises(FileNotFoundError):
            ci.setup()
        self.run.assert_not_called()

    def test_setup_loads_exact_temporary_private_profile_without_persistent_policy_or_cache(self):
        with contextlib.redirect_stdout(io.StringIO()):
            ci.setup()
        self.assertTrue(ci.loaded())
        self.assertEqual(len(self.events), 1)
        command = self.events[0][0]
        self.assertEqual(command[:5], [ci.SUDO, '-n', ci.PARSER, '-r', '-K'])
        self.assertFalse(pathlib.Path(command[5]).exists())
        self.assertFalse(pathlib.Path(command[5]).parent.exists())
        self.assertEqual(list(self.workspace.iterdir()), [])
        with contextlib.redirect_stdout(io.StringIO()):
            ci.setup()
        self.assertEqual(len(self.events), 1)

    def test_failed_sudo_or_parser_does_not_claim_setup_success(self):
        self.run.side_effect = subprocess.CalledProcessError(1, [ci.SUDO, '-n', ci.PARSER])
        output = io.StringIO()
        with contextlib.redirect_stdout(output), self.assertRaises(subprocess.CalledProcessError):
            ci.setup()
        self.assertEqual(output.getvalue(), '')
        self.assertFalse(ci.loaded())
        self.assertFalse(pathlib.Path(self.run.call_args.args[0][-1]).exists())

    def test_successful_parser_exit_without_kernel_profile_is_a_failure(self):
        self.run.side_effect = None
        with self.assertRaisesRegex(RuntimeError, 'was not loaded'):
            ci.setup()

    def test_unexpected_existing_profile_mode_is_never_replaced_run_or_removed(self):
        for mode in ['enforce', 'complain', 'mixed']:
            self.mark_loaded(mode)
            for action in [ci.setup, ci.cleanup, lambda: ci.run(['make', 'test-packaged'])]:
                with self.assertRaisesRegex(RuntimeError, 'Unexpected AppArmor mode'):
                    action()
        self.run.assert_not_called()

    def test_run_requires_a_loaded_profile_with_no_fallback(self):
        with self.assertRaisesRegex(RuntimeError, 'missing; run setup first'):
            ci.run(['make', 'test-packaged'])
        self.run.assert_not_called()

    def test_allowed_checks_use_exact_aa_exec_argv_and_propagate_check_failure(self):
        self.mark_loaded()
        for command in sorted(ci.COMMANDS):
            self.assertEqual(ci.run(list(command)), 0)
            self.assertEqual(self.events[-1][0], [ci.AA_EXEC, '-p', ci.profile_name(), '--', *command])
        self.run.return_value = subprocess.CompletedProcess([], 23)
        self.run.side_effect = None
        self.assertEqual(ci.run(['make', 'test-packaged']), 23)
        self.run.side_effect = FileNotFoundError(ci.AA_EXEC)
        with self.assertRaises(FileNotFoundError):
            ci.run(['make', 'test-packaged'])
        self.assertEqual(self.run.call_count, len(ci.COMMANDS) + 2)

    def test_helper_rejects_arbitrary_shells_commands_and_sandbox_bypass_arguments(self):
        self.mark_loaded()
        for command in [[], ['/bin/sh'], ['make', 'test'], ['make', 'test-packaged', '--no-sandbox'],
                        ['sudo', 'make', 'test-packaged'], ['make test-packaged'],
                        ['./scripts/with-toolchain.sh', 'python3', 'scripts/packaged-send.py', '--no-sandbox']]:
            with self.assertRaisesRegex(RuntimeError, 'Only explicit packaged'):
                ci.run(command)
        self.run.assert_not_called()

    def test_cleanup_removes_only_this_profile_and_is_idempotent(self):
        self.mark_loaded()
        with contextlib.redirect_stdout(io.StringIO()):
            ci.cleanup()
            ci.cleanup()
        self.assertFalse(ci.loaded())
        self.assertEqual(self.profiles.read_text(), 'unrelated-profile (enforce)\n')
        self.assertEqual(len(self.events), 1)
        self.assertEqual(self.events[0][0][:5], [ci.SUDO, '-n', ci.PARSER, '-R', '-K'])
        self.assertFalse(pathlib.Path(self.events[0][0][-1]).parent.exists())

    def test_cleanup_parser_failure_or_retained_kernel_profile_is_not_reported_as_removed(self):
        self.mark_loaded()
        self.run.side_effect = subprocess.CalledProcessError(1, [ci.SUDO, '-n', ci.PARSER])
        with self.assertRaises(subprocess.CalledProcessError):
            ci.cleanup()
        self.run.side_effect = None
        with self.assertRaisesRegex(RuntimeError, 'was not removed'):
            ci.cleanup()

    def test_cli_routes_actions_and_preserves_errors_and_subprocess_exit_status(self):
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(ci.main(['setup']), 0)
            self.assertEqual(ci.main(['run', '--', 'make', 'test-packaged']), 0)
            self.assertEqual(ci.main(['run', 'make', 'test-packaged']), 0)
            self.assertEqual(ci.main(['cleanup']), 0)
        output = io.StringIO()
        with contextlib.redirect_stderr(output):
            self.assertEqual(ci.main(['run', '--', 'make', 'test-packaged']), 1)
        self.assertIn('profile is missing', output.getvalue())
        self.mark_loaded()
        self.run.side_effect = None
        self.run.return_value = subprocess.CompletedProcess([], 19)
        self.assertEqual(ci.main(['run', '--', 'make', 'test-packaged']), 19)
        for arguments in [[], ['unknown'], ['setup', '--no-sandbox']]:
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                ci.main(arguments)

    def test_workflows_install_utils_wrap_both_checks_and_cleanup_even_on_failure(self):
        for name in ['ci.yml']:
            text = (ROOT / '.github/workflows' / name).read_text()
            setup = text.index('run: python3 scripts/ci-desktop.py setup')
            packaged = text.index('run: python3 scripts/ci-desktop.py run -- make test-packaged')
            send = text.index('run: python3 scripts/ci-desktop.py run -- ./scripts/with-toolchain.sh python3 scripts/packaged-send.py')
            cleanup = text.index('run: python3 scripts/ci-desktop.py cleanup')
            upload = text.index('uses: actions/upload-artifact@')
            self.assertLess(text.index('util-linux apparmor-utils'), setup)
            self.assertLess(setup, packaged)
            self.assertLess(packaged, send)
            self.assertLess(send, cleanup)
            self.assertLess(cleanup, upload)
            self.assertIn('if: always()', text[text.rfind('      - name:', 0, cleanup):cleanup])
            self.assertNotIn('run: make test-packaged', text)
            self.assertNotIn('run: ./scripts/with-toolchain.sh python3 scripts/packaged-send.py', text)
            for forbidden in ['--no-sandbox', 'chmod 4755', 'sysctl ', 'apparmor_restrict_unprivileged_userns=0']:
                self.assertNotIn(forbidden, text)
        release = (ROOT / '.github/workflows/release.yml').read_text()
        for test_only in ['scripts/ci-desktop.py', 'make test-packaged', 'scripts/packaged-send.py']:
            self.assertNotIn(test_only, release)
        self.assertIn('\tpython3 scripts/ci-desktop-test.py\n', (ROOT / 'Makefile').read_text())


class ProfileCapture(unittest.TestCase):
    def setUp(self):
        self.children = []
        self.real_popen = subprocess.Popen
        self.folder = tempfile.TemporaryDirectory(prefix='wallet-ci-profile-capture-')
        self.addCleanup(self.folder.cleanup)
        self.directory = pathlib.Path(self.folder.name)

    def capture(self, program, **overrides):
        def synthetic_child(command, **kwargs):
            self.assertEqual(command, [ci.SUDO, '-n', '/usr/bin/cat', str(ci.PROFILES)])
            self.assertEqual(kwargs, {'cwd': ci.ROOT, 'stdout': subprocess.PIPE, 'stderr': subprocess.DEVNULL})
            child = self.real_popen([sys.executable, '-c', program], **kwargs)
            self.children.append(child)
            return child
        with mock.patch.object(ci.subprocess, 'Popen', side_effect=synthetic_child), contextlib.ExitStack() as stack:
            for key, value in overrides.items():
                stack.enter_context(mock.patch.object(ci, key, value))
            return ci.read_profiles()

    def tearDown(self):
        for child in self.children:
            self.assertIsNotNone(child.poll(), 'lookup left a subprocess running')
            self.assertTrue(child.stdout.closed)

    def test_fixed_sudo_lookup_captures_public_names_without_nonroot_securityfs_read(self):
        self.assertEqual(self.capture("import sys;sys.stdout.write('profile (unconfined)\\n')"), 'profile (unconfined)\n')

    def test_lookup_rejects_oversized_invalid_or_failed_output_and_reaps_child(self):
        for program, message in [
                ("import sys;sys.stdout.buffer.write(b'x'*4097)", 'exceeds its bound'),
                ("import sys;sys.stdout.buffer.write(bytes([255]))", 'Invalid CI'),
                ("import sys;sys.stdout.write('profile (unconfined)');sys.exit(7)", 'could not be read')]:
            with self.assertRaisesRegex(RuntimeError, message):
                self.capture(program, PROFILE_LIMIT=4096)

    def test_lookup_timeout_has_no_fallback_and_kills_owned_subprocess(self):
        with self.assertRaisesRegex(RuntimeError, 'timed out'):
            self.capture('import time;time.sleep(10)', PROFILE_TIMEOUT=.05)


if __name__ == '__main__':
    suite = unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(case)
                               for case in [CIDesktop, ProfileCapture])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    sources = ['scripts/ci-desktop.py', 'scripts/ci-desktop-test.py', 'Makefile',
               '.github/workflows/ci.yml', '.github/workflows/release.yml']
    report = {'status': 'PASS' if result.wasSuccessful() else 'FAIL',
              'scope': 'Offline policy compilation, CI-only guards, exact intercepted privileged commands and workflow wiring; no host policy was loaded',
              'testsRun': result.testsRun, 'failures': len(result.failures),
              'errors': len(result.errors), 'skipped': len(result.skipped),
              'kernelProfileLoad': 'NOT_RUN: never change host policy in these offline tests',
              'githubHostedExecution': 'NOT_RUN: requires executing the changed workflows on a hosted runner',
              'sourceSHA256': {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in sources}}
    reports = ROOT / 'reports'
    reports.mkdir(exist_ok=True)
    (reports / 'ci-desktop-tools.json').write_text(json.dumps(report, indent=2) + '\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
