#!/usr/bin/env python3
"""Current Go package routing checks; no compiler or wallet process is invoked."""
import importlib.util
from pathlib import Path
import subprocess
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

    def test_all_actions_use_exact_selected_packages_without_coverage_flags(self):
        listed = self.listing()
        for action, prefix in [('test', ['go', 'test']), ('race', ['go', 'test', '-race', '-p', '2']),
                               ('vet', ['go', 'vet'])]:
            with self.subTest(action=action), mock.patch.object(ci.subprocess, 'check_output', return_value='\n'.join(listed)), \
                    mock.patch.object(ci.subprocess, 'run', return_value=subprocess.CompletedProcess([], 17)) as execute:
                self.assertEqual(ci.run(action), 17)
                execute.assert_called_once_with(prefix + [name for name in listed if name != ci.LEGACY_PACKAGE],
                                                cwd=ci.ROOT, check=False)

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
