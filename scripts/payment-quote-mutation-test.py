#!/usr/bin/env python3
"""Offline quote mutation declarations/classification; no Go or wallet execution."""
import importlib.util
import json
import pathlib
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('payment_quote_mutations', ROOT / 'scripts/payment-quote-mutations.py')
campaign = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(campaign)


class PaymentQuoteMutationControls(unittest.TestCase):
    def test_unique_guards_create_only_disposable_overlays_and_keep_live_bytes(self):
        self.assertEqual(len(campaign.MUTANTS), 3)
        self.assertEqual(len({item['id'] for item in campaign.MUTANTS}), 3)
        with tempfile.TemporaryDirectory() as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / 'source'
            snapshot.mkdir()
            for index, item in enumerate(campaign.MUTANTS):
                original = (ROOT / item['path']).read_bytes()
                target = snapshot / item['path']
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(original)
                overlay, digest = campaign.helpers.overlay_mutant(snapshot, temporary, index, item)
                mapping = json.loads(overlay.read_text())['Replace']
                self.assertEqual(list(mapping), [str(target)])
                changed = pathlib.Path(mapping[str(target)])
                self.assertTrue(changed.is_relative_to(temporary))
                self.assertNotEqual(changed.read_bytes(), original)
                self.assertEqual(target.read_bytes(), original)
                self.assertEqual((ROOT / item['path']).read_bytes(), original)
                self.assertEqual(campaign.helpers.sha(changed), digest)

    def test_only_exact_non_skipped_runtime_assertion_counts_as_a_kill(self):
        for item in campaign.MUTANTS:
            expected, assertion = item['failedTest'], item['assertion']
            events = [{'Action': 'output', 'Test': expected, 'Output': assertion}, {'Action': 'fail', 'Test': expected}]
            classify = lambda values, code=1, **options: campaign.helpers.classify_execution(code, '\n'.join(json.dumps(row) for row in values), expected, assertion, **options)
            self.assertEqual(classify(events)['status'], 'KILLED')
            self.assertEqual(classify(events, control=True)['status'], 'INVALID')
            self.assertEqual(classify(events, timed_out=True)['status'], 'INVALID')
            self.assertEqual(classify(events + [{'Action': 'build-fail'}])['status'], 'INVALID')
            self.assertEqual(classify(events + [{'Action': 'skip', 'Test': 'Sibling'}])['status'], 'INVALID')
            self.assertEqual(classify(events + [{'Action': 'fail', 'Test': 'Unrelated'}])['status'], 'INVALID')
            self.assertEqual(classify(events[1:])['status'], 'INVALID')
            passed = [{'Action': 'pass', 'Test': expected}]
            self.assertEqual(classify(passed, 0, control=True)['status'], 'PASS')
            self.assertEqual(classify(passed, 0)['status'], 'SURVIVED')


if __name__ == '__main__':
    unittest.main()
