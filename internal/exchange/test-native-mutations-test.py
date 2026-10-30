#!/usr/bin/env python3
"""Guard the native mutation harness against false passing evidence."""
import importlib.util
import json
import pathlib
import tempfile
import unittest

PATH = pathlib.Path(__file__).resolve().with_name("test-native-mutations.py")
spec = importlib.util.spec_from_file_location("native_exchange_mutations", PATH)
harness = importlib.util.module_from_spec(spec)
spec.loader.exec_module(harness)


class NativeMutationIntegrity(unittest.TestCase):
    def events(self, test, assertion, action="fail"):
        return "\n".join(json.dumps(event) for event in [
            {"Action": "run", "Test": test},
            {"Action": "output", "Test": test, "Output": assertion + "\n"},
            {"Action": action, "Test": test},
        ])

    def test_six_distinct_guards_target_unique_current_source_blocks(self):
        self.assertEqual(len(harness.MUTANTS), 6)
        self.assertEqual(len({item["id"] for item in harness.MUTANTS}), 6)
        for item in harness.MUTANTS:
            with self.subTest(guard=item["id"]):
                source = (harness.ROOT / item["path"]).read_text()
                for original, changed in item["changes"]:
                    self.assertEqual(source.count(original), 1)
                    self.assertNotEqual(original, changed)
                    source = source.replace(original, changed, 1)
                self.assertTrue(item["assertion"])

    def test_only_designated_runtime_assertion_can_kill(self):
        item = harness.MUTANTS[0]
        output = self.events(item["failedTest"], item["assertion"])
        self.assertEqual(harness.runtime.classify_execution(1, output, item["failedTest"], item["assertion"])["status"], "KILLED")
        for exit_code, stream, timeout in [
            (1, self.events("WrongTest", item["assertion"]), False),
            (1, self.events(item["failedTest"], "unrelated assertion"), False),
            (1, self.events(item["failedTest"], "panic: " + item["assertion"]), False),
            (1, self.events(item["failedTest"], "[build failed] " + item["assertion"]), False),
            (1, output, True), (-9, output, False), (1, "not json", False),
        ]:
            with self.subTest(exit=exit_code, output=stream):
                self.assertEqual(harness.runtime.classify_execution(exit_code, stream, item["failedTest"], item["assertion"], timed_out=timeout)["status"], "INVALID")

    def test_unchanged_control_must_really_pass_and_survivor_is_not_killed(self):
        item = harness.MUTANTS[0]
        output = self.events(item["failedTest"], "", action="pass")
        classify = harness.runtime.classify_execution
        self.assertEqual(classify(0, output, item["failedTest"], item["assertion"], control=True)["status"], "PASS")
        self.assertEqual(classify(0, output, item["failedTest"], item["assertion"])["status"], "SURVIVED")
        self.assertEqual(classify(0, "", item["failedTest"], item["assertion"], control=True)["status"], "INVALID")

    def test_overlay_changes_snapshot_replacement_without_touching_source(self):
        with tempfile.TemporaryDirectory() as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / "source"
            source = snapshot / "guard.go"
            source.parent.mkdir()
            source.write_text("original unique guard")
            item = {"path": "guard.go", "changes": [("unique guard", "mutated guard")]}
            overlay, _ = harness.runtime.overlay_mutant(snapshot, temporary, 1, item)
            self.assertEqual(source.read_text(), "original unique guard")
            replacement = pathlib.Path(json.loads(overlay.read_text())["Replace"][str(source)])
            self.assertEqual(replacement.read_text(), "original mutated guard")
            item["changes"] = [("missing guard", "different")]
            with self.assertRaisesRegex(ValueError, "unique mutation"):
                harness.runtime.overlay_mutant(snapshot, temporary, 2, item)


if __name__ == "__main__":
    unittest.main()
