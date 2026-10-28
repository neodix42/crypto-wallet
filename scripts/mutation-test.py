#!/usr/bin/env python3
"""Mutation-driver controls: no Go, live-source edits or real reports."""
import importlib.util
import json
import os
import pathlib
import select
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
SPEC = importlib.util.spec_from_file_location('wallet_mutation', ROOT / 'scripts/mutation.py')
mutation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mutation)


class MutationReportIsolation(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='wallet-mutation-cli-')
        self.addCleanup(self.temporary.cleanup)
        self.root = pathlib.Path(self.temporary.name)
        (self.root / 'scripts').mkdir()
        (self.root / 'reports').mkdir()
        for name in ('mutation.py', 'evidence_binding.py'):
            shutil.copy2(ROOT / 'scripts' / name, self.root / 'scripts' / name)
        self.aggregate = self.root / 'reports/mutation.json'
        self.aggregate.write_bytes(b'{"status":"PUBLIC EXISTING AGGREGATE"}\n')

    def reject(self, arguments, message):
        before = self.aggregate.read_bytes()
        process = subprocess.run([sys.executable, str(self.root / 'scripts/mutation.py'), *arguments],
                                 cwd=self.root, text=True, capture_output=True, timeout=5)
        self.assertEqual(process.returncode, 2, process.stderr)
        self.assertIn(message, process.stderr)
        self.assertEqual(process.stdout, '')
        self.assertEqual(self.aggregate.read_bytes(), before)
        self.assertEqual(sorted(p.name for p in (self.root / 'reports').iterdir()), ['mutation.json'])

    def test_selected_runs_reject_all_normalized_aggregate_path_aliases(self):
        for name in ['reports/mutation.json', 'reports//mutation.json', 'reports/./mutation.json',
                     './reports/mutation.json', 'reports///./mutation.json']:
            with self.subTest(report=name):
                self.reject(['--only', 'backup-explicit-confirmation', '--report', name],
                            'Selected runs must not overwrite the full mutation report')

    def test_report_must_remain_a_direct_relative_json_under_reports(self):
        for name in ['/tmp/mutation.json', '../reports/mutation.json', 'reports/nested/mutation.json',
                     'reports/../mutation.json', 'mutation.json', 'reports/mutation.txt']:
            with self.subTest(report=name):
                self.reject(['--only', 'backup-explicit-confirmation', '--report', name],
                            '--report must be a direct repository-relative reports/*.json path')

    def test_unknown_mutant_id_is_rejected_before_any_report_or_source_access(self):
        self.reject(['--only', 'backup-not-an-exact-id'], 'invalid choice')


class RuntimeEvidence(unittest.TestCase):
    expected = 'SecurityInvariant/exact'
    assertion = 'unauthorized replacement accepted'

    def execute(self, exit_code, events, timed_out=False):
        result = {'exit': exit_code, 'elapsedSeconds': .01, 'timedOut': timed_out}
        output = '\n'.join(json.dumps(event) for event in events)
        with mock.patch.object(mutation, 'execute_command', return_value=(result, output)) as tool:
            value = mutation.backup_execution(pathlib.Path('/synthetic'), './synthetic',
                                              '^SecurityInvariant$/^exact$', self.expected,
                                              self.assertion)
        command, _ = tool.call_args.args
        self.assertIn('-timeout=110s', command)
        self.assertIn('-count=1', command)
        return value

    def failure_events(self):
        return [{'Action': 'output', 'Test': self.expected, 'Output': self.assertion},
                {'Action': 'fail', 'Test': self.expected}]

    def test_exact_test_and_assertion_kill_the_mutant(self):
        result = self.execute(1, self.failure_events())
        self.assertEqual(result['status'], 'KILLED')
        self.assertTrue(result['expectedAssertionObserved'])

    def test_passing_control_must_execute_the_exact_test(self):
        self.assertEqual(self.execute(0, [{'Action': 'pass', 'Test': self.expected}])['status'], 'PASS')
        for events in [[], [{'Action': 'pass', 'Test': 'Unrelated'}],
                       [{'Action': 'skip', 'Test': self.expected}]]:
            with self.subTest(events=events):
                self.assertEqual(self.execute(0, events)['status'], 'INVALID')

    def test_wrong_test_assertion_skip_panic_build_failure_or_deadline_is_invalid(self):
        wrong_test = [{'Action': 'output', 'Test': 'SecurityInvariant/sibling',
                       'Output': self.assertion}, {'Action': 'fail', 'Test': self.expected}]
        for events in [wrong_test, [{'Action': 'fail', 'Test': self.expected}],
                       [{'Action': 'output', 'Test': self.expected, 'Output': 'an unrelated failure'},
                        {'Action': 'fail', 'Test': self.expected}],
                       self.failure_events() + [{'Action': 'skip', 'Test': 'SecurityInvariant/sibling'}],
                       self.failure_events() + [{'Action': 'output', 'Output': 'panic: unexpected runtime fault'}],
                       self.failure_events() + [{'Action': 'build-fail'}]]:
            with self.subTest(events=events):
                self.assertEqual(self.execute(1, events)['status'], 'INVALID')
        self.assertEqual(self.execute(-9, self.failure_events(), timed_out=True)['status'], 'INVALID')

    def test_historical_compiler_and_go_deadline_failures_remain_invalid(self):
        for output in ['FAIL synthetic [build failed]', 'FAIL synthetic [setup failed]',
                       'panic: test timed out after 1m50s']:
            with self.subTest(output=output):
                with mock.patch.object(mutation, 'execute_command', return_value=(
                        {'exit': 1, 'timedOut': False}, output)):
                    self.assertEqual(mutation.historical_execution('/synthetic', './synthetic')['status'], 'INVALID')

    def test_outer_deadline_terminates_the_owned_test_descendant(self):
        # A surviving child would keep the captured stdout pipe open and write
        # this marker after its parent is killed. These are synthetic processes,
        # never a Go test or a wallet role.
        with tempfile.TemporaryDirectory(prefix='wallet-mutant-timeout-') as directory:
            marker = pathlib.Path(directory) / 'leaked-child'
            child = ('import pathlib,time;print("child-ready",flush=True);'
                     'time.sleep(.6);pathlib.Path(%r).write_text("leaked")') % str(marker)
            parent = ('import subprocess,sys;'
                      'subprocess.Popen([sys.executable,"-c",%r]);'
                      'import time;time.sleep(30)') % child
            popen = subprocess.Popen
            def await_child(*args, **kwargs):
                process = popen(*args, **kwargs)
                try:
                    self.assertTrue(select.select([process.stdout], [], [], 5)[0], 'synthetic child did not start')
                    self.assertEqual(process.stdout.readline(), 'child-ready\n')
                    return process
                except BaseException:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.communicate()
                    raise
            with mock.patch.object(mutation.subprocess, 'Popen', side_effect=await_child):
                result, _ = mutation.execute_command([sys.executable, '-c', parent], directory, timeout=.2)
            self.assertTrue(result['timedOut'])
            time.sleep(.7)
            self.assertFalse(marker.exists(), 'timed-out test child escaped process cleanup')


class SourceAndControlIsolation(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='wallet-mutant-source-')
        self.addCleanup(temporary.cleanup)
        self.copy = pathlib.Path(temporary.name)
        self.target = self.copy / 'synthetic.go'
        self.original = 'synthetic security guard'
        self.target.write_text(self.original)
        self.mutant = ('nonce-reservation', 'synthetic.go', self.original, 'synthetic bypass', './synthetic')

    def test_reservation_requires_passing_controls_and_both_exact_failures(self):
        for final, expected in [('KILLED', 'KILLED'), ('SURVIVED', 'SURVIVED'), ('INVALID', 'INVALID')]:
            with self.subTest(final=final):
                statuses = iter(['PASS', 'PASS', 'KILLED', final])
                def execute(*_):
                    status = next(statuses)
                    self.assertEqual(self.target.read_text(), self.original if status == 'PASS' else 'synthetic bypass')
                    return {'status': status}
                with mock.patch.object(mutation, 'backup_execution', side_effect=execute) as control:
                    row = mutation.run_mutant(self.copy, self.mutant)
                self.assertEqual(row['status'], expected)
                self.assertEqual(control.call_count, 4)
                self.assertEqual(self.target.read_text(), self.original)
                self.assertEqual(len(row['controls']), 2)

    def test_failed_unmodified_control_never_applies_the_mutation(self):
        with mock.patch.object(mutation, 'backup_execution', return_value={'status': 'SURVIVED'}) as control:
            row = mutation.run_mutant(self.copy, self.mutant)
        self.assertEqual(row['status'], 'INVALID')
        self.assertEqual(control.call_count, 1)
        self.assertEqual(self.target.read_text(), self.original)

    def test_tool_failure_always_restores_the_isolated_source(self):
        with mock.patch.object(mutation, 'backup_execution', side_effect=[
                {'status': 'PASS'}, {'status': 'PASS'}, RuntimeError('synthetic tool failure')]):
            with self.assertRaisesRegex(RuntimeError, 'synthetic tool failure'):
                mutation.run_mutant(self.copy, self.mutant)
        self.assertEqual(self.target.read_text(), self.original)


if __name__ == '__main__':
    unittest.main(verbosity=2)
