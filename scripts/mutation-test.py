#!/usr/bin/env python3
"""CLI isolation checks: never run Go, mutate live source or touch real reports."""
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


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


if __name__ == '__main__':
    unittest.main(verbosity=2)
