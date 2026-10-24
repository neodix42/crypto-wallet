#!/usr/bin/env python3
"""Reject short, stale, wrong-scope or unreconciled long-campaign claims."""
import copy
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location('wallet_soak_runner', pathlib.Path(__file__).with_name('run-soak.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
fuzz_spec = importlib.util.spec_from_file_location('wallet_fuzz_runner', pathlib.Path(__file__).with_name('fuzz.py'))
fuzzer = importlib.util.module_from_spec(fuzz_spec)
fuzz_spec.loader.exec_module(fuzzer)


class CampaignEvidence(unittest.TestCase):
    def test_zero_exit_without_actual_fuzzing_cannot_pass(self):
        for text in ('testing: warning: no fuzz tests to fuzz\nPASS\n',
                     'fuzz: elapsed: 0s, gathering baseline coverage: 1/1 completed\nPASS\n',
                     'now fuzzing with 2 workers\nfuzz: elapsed: 3s, execs: 0 (0/sec)\nPASS\n'):
            self.assertFalse(fuzzer.executed_fuzz(text))
        self.assertTrue(fuzzer.executed_fuzz('now fuzzing with 2 workers\nfuzz: elapsed: 3s, execs: 42 (14/sec)\nPASS\n'))

    def test_each_configured_fuzz_target_exists_exactly_once(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        import re
        for package, target in fuzzer.TARGETS:
            source = '\n'.join(p.read_text() for p in (root / 'internal' / package).glob('*_test.go'))
            self.assertEqual(len(re.findall(r'^func ' + re.escape(target) + r'\(', source, re.MULTILINE)), 1, target)

    def result(self, seconds=86400):
        return dict(executionStatus='PASS', status='NOT_RUN', requestedSeconds=seconds,
            elapsedSeconds=seconds + 1, campaign24hStatus='NOT_RUN',
            excluded=['Decred work deferred by owner; no public testnet activity'],
            scopedCampaign24hStatus='PASS' if seconds >= 86400 else 'NOT_RUN',
            workloads=dict(cycles=2, productionSignedJournalReconciled=2),
            finalState=dict(lifecycleConfirmed=2, lifecycleCancelledBeforeSignature=2,
                            lifecycleFakeLedgerPayments=2, lifecycleUnresolved=0))

    def test_scoped_24h_cannot_be_confused_with_full_gate(self):
        runner.validate_workload(self.result(), 86400)
        for updates in (dict(status='PASS'), dict(campaign24hStatus='PASS'), dict(excluded=[])):
            result = self.result()
            result.update(updates)
            with self.assertRaises(ValueError):
                runner.validate_workload(result, 86400)

    def test_duration_mismatch_short_nan_and_infinite_evidence_are_rejected(self):
        for value in (0, 86399, float('nan'), float('inf'), '86401', True):
            result = self.result()
            result['elapsedSeconds'] = value
            with self.assertRaises(ValueError):
                runner.validate_workload(result, 86400)
        with self.assertRaises(ValueError):
            runner.validate_workload(self.result(1), 86400)
        result = self.result(1)
        runner.validate_workload(result, 1)
        result['scopedCampaign24hStatus'] = 'PASS'
        with self.assertRaises(ValueError):
            runner.validate_workload(result, 1)
        result = self.result(1)
        result['requestedSeconds'] = True
        with self.assertRaises(ValueError):
            runner.validate_workload(result, 1)

    def test_lost_duplicate_or_unresolved_payments_never_count_as_completed(self):
        for key, value in (('lifecycleConfirmed', 1), ('lifecycleCancelledBeforeSignature', 0),
                           ('lifecycleFakeLedgerPayments', 3), ('lifecycleUnresolved', 1)):
            result = self.result()
            result['finalState'][key] = value
            with self.assertRaises(ValueError):
                runner.validate_workload(result, 86400)
        result = self.result()
        result['finalState']['lifecycleUnresolved'] = False
        with self.assertRaises(ValueError):
            runner.validate_workload(result, 86400)
        for change in (dict(cycles=0), dict(cycles=True), dict(productionSignedJournalReconciled=1),
                       dict(DCRAccountDiscoveryScans=1)):
            result = copy.deepcopy(self.result())
            result['workloads'].update(change)
            with self.assertRaises(ValueError):
                runner.validate_workload(result, 86400)


if __name__ == '__main__':
    unittest.main()
