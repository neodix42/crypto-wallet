#!/usr/bin/env python3
"""Current Go package routing checks; no compiler or wallet process is invoked."""
import concurrent.futures
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('current_ci_go', Path(__file__).with_name('ci-go.py'))
ci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci)


class CurrentGoRouting(unittest.TestCase):
    def listing(self, extra=()):
        return sorted(ci.REQUIRED | {'localwallet/internal/uiprompt', ci.LEGACY_PACKAGE, *extra})

    def test_only_legacy_launcher_is_removed_and_new_current_packages_remain(self):
        listed = self.listing({'localwallet/internal/newchain', ci.LEGACY_PACKAGE + '-successor'})
        with mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(listed)) as discover:
            self.assertEqual(ci.packages(), [name for name in listed if name != ci.LEGACY_PACKAGE])
        discover.assert_called_once_with(['go', 'list', './...'], cwd=ci.ROOT, text=True)

    def test_ordinary_tests_and_vet_keep_the_complete_selected_package_set(self):
        listed = self.listing({'localwallet/internal/newchain'})
        for action, prefix in [('test', ['go', 'test']), ('vet', ['go', 'vet'])]:
            with self.subTest(action=action), mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(listed)), \
                    mock.patch.object(ci.subprocess, 'run', return_value=subprocess.CompletedProcess([], 17)) as execute:
                self.assertEqual(ci.run(action), 17)
                execute.assert_called_once_with(prefix + [name for name in listed if name != ci.LEGACY_PACKAGE],
                                                cwd=ci.ROOT, check=False)

    def test_race_runs_all_other_packages_before_the_complete_uncached_stress_package(self):
        listed = self.listing({'localwallet/internal/newchain', ci.STRESS_PACKAGE + '/newhelper'})
        with mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(listed)), \
                mock.patch.object(ci.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0)) as execute:
            self.assertEqual(ci.run('race'), 0)
        expected = [name for name in listed if name not in (ci.LEGACY_PACKAGE, ci.STRESS_PACKAGE)]
        self.assertEqual(execute.call_args_list, [
            mock.call(['go', 'test', '-race', '-p', '1', '-timeout=30m', *expected], cwd=ci.ROOT, check=False),
            mock.call(['go', 'test', '-race', '-p', '1', '-timeout=30m', '-count=1', ci.STRESS_PACKAGE], cwd=ci.ROOT, check=False),
        ])
        # Dynamic discovery remains the authority: no package is omitted or
        # repeated, and a future stress subpackage still runs in the first batch.
        routed = [name for call in execute.call_args_list for name in call.args[0]
                  if name.startswith('localwallet/')]
        self.assertEqual(sorted(routed), sorted(name for name in listed if name != ci.LEGACY_PACKAGE))
        self.assertEqual(len(routed), len(set(routed)))

    def test_race_retains_both_phases_and_propagates_any_failure(self):
        for exits, expected in [((0, 0), 0), ((1, 0), 1), ((17, 0), 17), ((0, 23), 23),
                                ((17, 23), 17), ((-9, 0), -9)]:
            with self.subTest(exits=exits), \
                    mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(self.listing())), \
                    mock.patch.object(ci.subprocess, 'run', side_effect=[
                        subprocess.CompletedProcess([], code) for code in exits]) as execute:
                self.assertEqual(ci.run('race'), expected)
                self.assertEqual(execute.call_count, 2, 'a failed first batch must not omit stress')

    def test_subprocess_timeout_failure_preserves_every_package_and_fails_the_route(self):
        # Stand-in Go exercises real process execution, discovery and argv
        # boundaries without a compiler or wallet. Go package timeouts return
        # exit 1; a later successful stress phase must not turn that into PASS.
        listed = self.listing({'localwallet/internal/newchain', ci.STRESS_PACKAGE + '/newhelper'})
        with tempfile.TemporaryDirectory(prefix='wallet-ci-go-routing-') as temporary:
            folder = Path(temporary)
            executable = folder / 'go'
            executable.write_text('#!' + sys.executable + '\n' +
                'import json, pathlib, sys\n' +
                'args = sys.argv[1:]\n' +
                'with pathlib.Path(__file__).with_name("calls.jsonl").open("a") as log: log.write(json.dumps(args) + "\\n")\n' +
                'if args == ["list", "./..."]:\n' +
                ' print("\\n".join(' + repr(listed) + '))\n' +
                'elif args[:2] == ["test", "-race"]:\n' +
                ' sys.exit(0 if args[-1] == ' + repr(ci.STRESS_PACKAGE) + ' else 1)\n' +
                'else: sys.exit(99)\n')
            executable.chmod(0o755)
            with mock.patch.dict(os.environ, {'PATH': str(folder) + os.pathsep + os.environ['PATH']}):
                self.assertEqual(ci.run('race', root=folder), 1)
            calls = [json.loads(line) for line in (folder / 'calls.jsonl').read_text().splitlines()]
        self.assertEqual(calls[0], ['list', './...'])
        self.assertEqual(len(calls), 3, 'all race phases must execute even after exit 1')
        for command in calls[1:]:
            self.assertEqual(command[:5], ['test', '-race', '-p', '1', '-timeout=30m'])
            self.assertFalse(any(arg in ('-run', '-skip', '-short', '-parallel') for arg in command))
        routed = [name for command in calls[1:] for name in command if name.startswith('localwallet/')]
        self.assertEqual(sorted(routed), sorted(name for name in listed if name != ci.LEGACY_PACKAGE))
        self.assertEqual(len(routed), len(set(routed)))
        self.assertEqual(calls[-1][-2:], ['-count=1', ci.STRESS_PACKAGE])

    def test_stress_cannot_start_while_the_first_race_batch_is_running(self):
        first_started = threading.Event()
        release_first = threading.Event()
        first_finished = threading.Event()
        stress_started = threading.Event()

        def execute(command, **_):
            if command[-1] == ci.STRESS_PACKAGE:
                stress_started.set()
                self.assertTrue(first_finished.is_set(), 'stress overlapped the other race packages')
            else:
                first_started.set()
                self.assertTrue(release_first.wait(5), 'synthetic first batch was never released')
                first_finished.set()
            return subprocess.CompletedProcess(command, 0)

        with mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(self.listing())), \
                mock.patch.object(ci.subprocess, 'run', side_effect=execute), \
                concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(ci.run, 'race')
            try:
                self.assertTrue(first_started.wait(3))
                self.assertFalse(stress_started.wait(.1), 'stress started before the first batch completed')
            finally:
                release_first.set()
            self.assertEqual(future.result(timeout=3), 0)
        self.assertTrue(stress_started.is_set())

    def test_discovery_requires_stress_before_any_phase_can_start(self):
        listed = [name for name in self.listing() if name != ci.STRESS_PACKAGE]
        for action in ('test', 'race', 'vet'):
            with self.subTest(action=action), \
                    mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(listed)), \
                    mock.patch.object(ci.subprocess, 'run') as execute, self.assertRaises(ValueError):
                ci.run(action)
            execute.assert_not_called()

    def test_failed_or_malformed_discovery_cannot_run_an_empty_or_reduced_suite(self):
        invalid = ['', '\n'.join(self.listing()[1:]), '\n'.join(self.listing() * 2),
                   '\n'.join(self.listing() + ['-coverprofile=foreign']),
                   '\n'.join(self.listing() + ['foreign.example/package'])]
        for output in invalid:
            with self.subTest(output=output), mock.patch.object(ci.subprocess, 'check_output', return_value=output), \
                    mock.patch.object(ci.subprocess, 'run') as execute, self.assertRaises(ValueError):
                ci.run('test')
            execute.assert_not_called()
        with mock.patch.object(ci.subprocess, 'check_output', side_effect=subprocess.CalledProcessError(1, ['go'])), \
                mock.patch.object(ci.subprocess, 'run') as execute, self.assertRaises(subprocess.CalledProcessError):
            ci.run('test')
        execute.assert_not_called()


if __name__ == '__main__':
    unittest.main(verbosity=2)
