#!/usr/bin/env python3
"""Evidence-integrity regressions; synthetic counters, no wallet secrets."""
import importlib.util
import pathlib
import subprocess
import tempfile
import unittest

source = pathlib.Path(__file__).resolve().with_name("go-coverage.py")
spec = importlib.util.spec_from_file_location("wallet_go_coverage", source)
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)
binding_spec = importlib.util.spec_from_file_location("wallet_test_evidence_binding", source.with_name("evidence_binding.py"))
binding = importlib.util.module_from_spec(binding_spec)
binding_spec.loader.exec_module(binding)


class CoverageEvidence(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        coverage.root = pathlib.Path(self.temporary.name)
        coverage.reports = coverage.root / "reports"
        coverage.reports.mkdir()
        coverage.report = {"profiles": [], "exclusions": []}

    def profile(self, name, rows, mode="atomic"):
        path = coverage.reports / name
        path.write_text("mode: " + mode + "\n" + "".join(row + "\n" for row in rows))
        return path

    def test_union_does_not_multiply_denominator_or_invent_hits(self):
        first = self.profile("first.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 4 0", "localwallet/internal/vault/vault.go:3.1,4.1 2 10"])
        second = self.profile("second.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 4 1", "localwallet/internal/vault/vault.go:3.1,4.1 2 7", "localwallet/internal/vault/vault.go:5.1,6.1 3 0"])
        merged = coverage.merge_profiles([first, second]).read_text().splitlines()[1:]
        self.assertEqual(len(merged), 3)
        self.assertEqual(sum(int(line.rsplit(" ", 2)[1]) for line in merged), 9)
        self.assertIn("localwallet/internal/vault/vault.go:3.1,4.1 2 10", merged)
        self.assertIn("localwallet/internal/vault/vault.go:5.1,6.1 3 0", merged)

    def test_only_test_fixture_generators_are_excluded_explicitly(self):
        profile = self.profile("input.cover", ["localwallet/internal/secureui/native.go:1.1,2.1 1 1", "localwallet/internal/secureui/automation.go:1.1,2.1 1 0", "localwallet/internal/secureui/recovery_automation.go:1.1,2.1 1 0", "localwallet/internal/secureui/other_automation.go:1.1,2.1 1 1", "localwallet/tests/oracles/decred/main.go:1.1,2.1 1 0", "localwallet/internal/nativeui/automation.go:1.1,2.1 1 0", "localwallet/internal/nativeui/native.go:1.1,2.1 1 0"])
        merged = coverage.merge_profiles([profile]).read_text()
        self.assertIn("secureui/native.go", merged)
        self.assertNotIn("secureui/automation.go:", merged)
        self.assertNotIn("secureui/recovery_automation.go:", merged)
        self.assertIn("secureui/other_automation.go:", merged)
        self.assertNotIn("tests/oracles", merged)
        self.assertEqual(len(coverage.report["exclusions"]), 4)
        self.assertIn("nativeui/native.go", merged)
        self.assertNotIn("nativeui/automation.go:", merged)

    def test_mixed_modes_and_changed_block_shapes_are_refused(self):
        first = self.profile("first.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 4 1"])
        changed = self.profile("changed.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 5 1"])
        with self.assertRaisesRegex(RuntimeError, "Mismatched block"):
            coverage.merge_profiles([first, changed])
        other_mode = self.profile("count.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 4 1"], "count")
        with self.assertRaisesRegex(RuntimeError, "atomic"):
            coverage.merge_profiles([first, other_mode])
        negative = self.profile("negative.cover", ["localwallet/internal/vault/vault.go:1.1,2.1 4 -1"])
        with self.assertRaisesRegex(RuntimeError, "Negative"):
            coverage.merge_profiles([negative])

    def test_missing_shipped_coverage_is_not_passed(self):
        profile = self.profile("empty.cover", [])
        with self.assertRaisesRegex(RuntimeError, "No shipped"):
            coverage.merge_profiles([profile])
        result = subprocess.run(["python3", str(source.with_name("coverage.py")), str(profile)], cwd=coverage.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.assertNotEqual(result.returncode, 0)

    def test_critical_security_packages_enforce_95_percent(self):
        for name in ("internal/vault", "internal/secureui", "internal/uiprompt", "internal/supervisor",
                     "cmd/wallet-core", "cmd/wallet-signer", "internal/walletcore", "internal/walletsigner", "internal/nativeui", "internal/nativewallet", "internal/sessionwatch"):
            profile = self.profile("critical.cover", [f"localwallet/{name}/sample.go:1.1,2.1 94 1", f"localwallet/{name}/sample.go:3.1,4.1 6 0"])
            result = subprocess.run(["python3", str(source.with_name("coverage.py")), str(profile)], cwd=coverage.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            self.assertNotEqual(result.returncode, 0, name)
            self.assertIn("95%", result.stdout)
            self.assertEqual(binding.go_rows(profile)[0]["floor"], 95, name)
            self.assertEqual(binding.go_rows(profile)[0]["status"], "FAIL", name)


if __name__ == "__main__":
    unittest.main()
