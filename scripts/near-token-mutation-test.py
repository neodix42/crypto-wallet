#!/usr/bin/env python3
"""Mutation harness controls only: no Go builds, wallet runs or real reports."""
import importlib.util
import json
import pathlib
import signal
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("near_token_mutation", ROOT / "scripts/near-token-mutation.py")
mutation = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mutation)


class RuntimeClassification(unittest.TestCase):
    expected = "SecurityInvariant/registration"
    assertion = "unapproved token registration signed"

    def classify(self, events, exit_code=1, **options):
        return mutation.classify_execution(exit_code, "\n".join(json.dumps(event) for event in events), self.expected, self.assertion, **options)

    def failure(self):
        return [{"Action": "output", "Test": self.expected, "Output": self.assertion},
                {"Action": "fail", "Test": self.expected}, {"Action": "fail", "Test": "SecurityInvariant"}]

    def test_only_exact_runtime_assertion_and_its_parent_aggregate_kill(self):
        result = self.classify(self.failure())
        self.assertEqual(result["status"], "KILLED")
        self.assertTrue(result["expectedAssertionObserved"])

    def test_missing_or_wrong_assertion_and_unrelated_failure_are_invalid(self):
        wrong = self.failure()
        wrong[0]["Test"] = "SecurityInvariant/sibling"
        unrelated = self.failure() + [{"Action": "fail", "Test": "AnotherInvariant"}]
        for events in (wrong, self.failure()[1:], unrelated):
            with self.subTest(events=events):self.assertEqual(self.classify(events)["status"], "INVALID")

    def test_passing_controls_and_survivors_must_execute_the_exact_case(self):
        passed = [{"Action": "pass", "Test": self.expected}]
        self.assertEqual(self.classify(passed, 0, control=True)["status"], "PASS")
        self.assertEqual(self.classify(passed, 0)["status"], "SURVIVED")
        self.assertEqual(self.classify(self.failure(), control=True)["status"], "INVALID")
        for events in ([], [{"Action": "pass", "Test": "SecurityInvariant/sibling"}], [{"Action": "skip", "Test": self.expected}]):
            for control in (False, True):
                with self.subTest(events=events, control=control):
                    self.assertEqual(self.classify(events, 0, control=control)["status"], "INVALID")

    def test_skip_build_panic_race_deadline_signal_and_non_json_never_kill(self):
        invalid = [{"Action": "build-fail"}, {"Action": "skip", "Test": "SecurityInvariant/sibling"}]
        invalid += [{"Action": "output", "Output": marker} for marker in ("[build failed]", "[setup failed]", "panic: unexpected", "fatal error: unexpected", "WARNING: DATA RACE")]
        for event in invalid:
            with self.subTest(event=event):self.assertEqual(self.classify(self.failure()+[event])["status"], "INVALID")
        self.assertEqual(self.classify(self.failure(), timed_out=True)["status"], "INVALID")
        self.assertEqual(self.classify(self.failure(), -9)["status"], "INVALID")
        for raw in ("compiler error", "{}\nnot JSON", "[]", "true"):
            with self.subTest(raw=raw):self.assertEqual(mutation.classify_execution(1,raw,self.expected,self.assertion)["status"], "INVALID")


class OverlayAndDeadlineControls(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="near-token-harness-controls-")
        self.addCleanup(self.temporary.cleanup)
        self.folder = pathlib.Path(self.temporary.name)
        self.snapshot = self.folder / "source"
        self.snapshot.mkdir()

    def test_all_declared_policies_have_unique_source_replacements_without_live_writes(self):
        self.assertEqual(len(mutation.MUTANTS),13)
        self.assertEqual(len({item['id'] for item in mutation.MUTANTS}),13)
        for index, item in enumerate(mutation.MUTANTS):
            with self.subTest(mutant=item['id']):
                target = self.snapshot / item['path']
                target.parent.mkdir(parents=True,exist_ok=True)
                original = (ROOT / item['path']).read_bytes()
                target.write_bytes(original)
                overlay,digest = mutation.overlay_mutant(self.snapshot,self.folder,index,item)
                mapping = json.loads(overlay.read_text())["Replace"]
                self.assertEqual(list(mapping),[str(target)])
                replacement = pathlib.Path(mapping[str(target)])
                self.assertTrue(replacement.is_relative_to(self.folder))
                self.assertNotEqual(replacement.read_bytes(),original)
                self.assertEqual(target.read_bytes(),original)
                self.assertEqual((ROOT / item['path']).read_bytes(),original)
                self.assertEqual(digest,mutation.sha(replacement))

    def test_missing_or_ambiguous_guard_cannot_create_a_mutant(self):
        item = {"path":"guard.go","changes":[("exact guard","bypass")]}
        target = self.snapshot / "guard.go"
        for raw in ("missing guard", "exact guard exact guard"):
            target.write_text(raw)
            with self.subTest(raw=raw),self.assertRaisesRegex(ValueError,"unique mutation source"):
                mutation.overlay_mutant(self.snapshot,self.folder,0,item)
            self.assertEqual(target.read_text(),raw)
        self.assertFalse((self.folder / "0.go").exists())

    def test_go_deadline_and_owned_process_group_cleanup_are_independent(self):
        item = {"package":"./fixture","selector":"^SecurityInvariant$/^registration$", "failedTest":RuntimeClassification.expected,"assertion":RuntimeClassification.assertion}
        process = mock.Mock(pid=12345,returncode=-9)
        process.communicate.side_effect=[subprocess.TimeoutExpired("owned Go fixture",120),("",None)]
        with mock.patch.object(mutation.subprocess,"Popen",return_value=process) as popen, mock.patch.object(mutation.os,"killpg",side_effect=ProcessLookupError()) as kill:
            result = mutation.run(self.snapshot,item,{},self.folder / "overlay.json")
        self.assertEqual(result["status"],"INVALID")
        self.assertEqual(result["invalidReason"],"test deadline exceeded")
        kill.assert_called_once_with(12345,signal.SIGKILL)
        self.assertEqual(process.communicate.call_count,2)
        command = popen.call_args.args[0]
        self.assertIn("-timeout=110s",command)
        self.assertIn("-overlay="+str(self.folder / "overlay.json"),command)
        self.assertIn("-count=1",command)
        self.assertTrue(popen.call_args.kwargs["start_new_session"])


if __name__ == "__main__":
    unittest.main()
