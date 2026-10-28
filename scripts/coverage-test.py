#!/usr/bin/env python3
"""Evidence-integrity regressions; synthetic counters, no wallet secrets."""
import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

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

    def test_instrumentation_uses_exact_owned_packages_including_glue_and_oracles(self):
        rows = ["localwallet/internal/webui\t" + str(coverage.root / "internal/webui") + "\tlocalwallet",
                "localwallet/cmd/cryptowallet\t" + str(coverage.root / "cmd/cryptowallet") + "\tlocalwallet",
                "localwallet/tests/oracles/decred\t" + str(coverage.root / "tests/oracles/decred") + "\tlocalwallet"]
        with mock.patch.object(coverage.subprocess, 'check_output', return_value='\n'.join(rows)) as listed:
            packages = coverage.first_party_packages({})
        self.assertEqual(packages, sorted(row.split('\t')[0] for row in rows))
        self.assertEqual(coverage.report['instrumentedPackages'], packages)
        self.assertEqual(listed.call_args.args[0][-1], './...')
        self.assertNotIn('-deps', listed.call_args.args[0])
        process = mock.Mock(stdout=[], wait=mock.Mock(return_value=0), poll=mock.Mock(return_value=0))
        coverage.report['gates'] = []
        with mock.patch.object(coverage.subprocess, 'check_output', return_value='\n'.join(rows)), mock.patch.object(coverage.subprocess, 'Popen', return_value=process) as started:
            self.assertEqual(coverage.all_packages({}), 0)
        command = started.call_args.args[0]
        self.assertIn('-coverpkg=' + ','.join(packages), command)
        self.assertNotIn('-coverpkg=./...', command)
        self.assertEqual(command[-1], './...')

    def test_foreign_replacement_paths_cannot_become_first_party_coverage(self):
        rows = [
            'github.com/wailsapp/wails/v2\t' + str(coverage.root / 'third_party/wails') + '\tgithub.com/wailsapp/wails/v2',
            'localwallet/internal/copied\t' + str(coverage.root / 'third_party/copied') + '\tlocalwallet',
            'localwallet/internal/foreign\t' + str(coverage.root.parent / 'foreign') + '\tlocalwallet',
            'localwallet/internal/foreign\t' + str(coverage.root / 'internal/foreign') + '\tforeign',
            'unbound-metadata',
            '',
        ]
        for row in rows:
            with self.subTest(row=row), mock.patch.object(coverage.subprocess, 'check_output', return_value=row):
                with self.assertRaises(RuntimeError):
                    coverage.first_party_packages({})

    def test_foreign_raw_counters_fail_without_deleting_or_filtering_them(self):
        profile = self.profile('foreign.cover', ['localwallet/internal/webui/window.go:1.1,2.1 1 1',
                                               'github.com/wailsapp/wails/v2/internal/app.go:1.1,2.1 1 1'])
        before = profile.read_bytes()
        with self.assertRaisesRegex(RuntimeError, 'dependency instrumentation'):
            coverage.merge_profiles([profile])
        self.assertEqual(profile.read_bytes(), before)

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
                     "cmd/wallet-core", "cmd/wallet-signer", "internal/walletcore", "internal/walletsigner", "internal/nativeui", "internal/nativewallet", "internal/webui", "internal/sessionwatch"):
            profile = self.profile("critical.cover", [f"localwallet/{name}/sample.go:1.1,2.1 94 1", f"localwallet/{name}/sample.go:3.1,4.1 6 0"])
            result = subprocess.run(["python3", str(source.with_name("coverage.py")), str(profile)], cwd=coverage.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            self.assertNotEqual(result.returncode, 0, name)
            self.assertIn("95%", result.stdout)
            self.assertEqual(binding.go_rows(profile)[0]["floor"], 95, name)
            self.assertEqual(binding.go_rows(profile)[0]["status"], "FAIL", name)

    def test_wails_runtime_campaign_targets_shipped_adapter_and_keeps_raw_profile(self):
        commands = []
        evidence = coverage.reports / 'wails-wallet-coverage.json'
        evidence.write_text('{"status":"PASS","instrumented":true,"sourceStable":true,"processIDs":[123],"privateProcessIDs":[456]}')
        def run(command, **kwargs):
            commands.append(command)
            if command[:2] == ['go', 'build']:
                binary = pathlib.Path(command[command.index('-o') + 1])
                binary.write_bytes(b'public instrumented executable fixture')
                (binary.parent / 'counters' / 'covmeta.fixture').write_bytes(b'public metadata fixture')
                (binary.parent / 'counters' / 'covcounters.fixture.123.1').write_bytes(b'public parent counter fixture')
                (binary.parent / 'counters' / 'covcounters.fixture.456.1').write_bytes(b'public private-role statement counter fixture')
            return mock.Mock(returncode=0)
        with mock.patch.object(coverage.subprocess, 'run', side_effect=run), mock.patch.object(coverage, 'selected_version', return_value='1.2.3'):
            self.assertEqual(coverage.portable_runtime_coverage({}), 0)
        build, driver, convert = commands
        self.assertIn('production,webkit2_41,portable,walletcoverage', build)
        self.assertIn('-coverpkg=./cmd/cryptowallet,./internal/nativewallet,./internal/webui,./internal/isolation', build)
        self.assertNotIn('./internal/nativeui', ' '.join(build))
        self.assertEqual(driver[1], 'scripts/wails-wallet-test.py')
        self.assertIn('wails-wallet-coverage', driver)
        self.assertIn('--instrumented', driver)
        self.assertTrue(convert[-1].endswith('go-native-runtime.cover'))
        self.assertEqual(coverage.report['nativeRuntime']['privateCounterProcesses'], [456])

    def test_failed_gui_prerequisite_preserves_driver_reason_without_claiming_measurement(self):
        evidence = coverage.reports / 'wails-wallet-coverage.json'
        evidence.write_text('{"status":"FAIL","instrumented":true,"reason":"Private network namespace preserving ordinary UID unavailable"}')
        commands = []
        def run(command, **kwargs):
            commands.append(command)
            if command[:2] == ['go', 'build']:
                pathlib.Path(command[command.index('-o') + 1]).write_bytes(b'public executable fixture')
                return mock.Mock(returncode=0)
            return mock.Mock(returncode=1)
        with mock.patch.object(coverage.subprocess, 'run', side_effect=run), mock.patch.object(coverage, 'selected_version', return_value='1.2.3'):
            self.assertEqual(coverage.portable_runtime_coverage({}), 1)
        self.assertEqual(len(commands), 2)
        self.assertEqual(coverage.report['nativeRuntime']['coverageStatus'], 'NOT_RUN')
        self.assertIn('namespace', coverage.report['nativeRuntime']['reason'])

    def test_parent_only_runtime_counters_never_pass_private_prompt_gate(self):
        evidence = coverage.reports / 'wails-wallet-coverage.json'
        evidence.write_text('{"status":"PASS","instrumented":true,"processIDs":[123],"privateProcessIDs":[456]}')
        def run(command, **kwargs):
            if command[:2] == ['go', 'build']:
                binary = pathlib.Path(command[command.index('-o') + 1])
                binary.write_bytes(b'public executable fixture')
                (binary.parent / 'counters' / 'covmeta.fixture').write_bytes(b'public fixture')
                (binary.parent / 'counters' / 'covcounters.fixture.123.1').write_bytes(b'public fixture')
                (binary.parent / 'counters' / 'covcounters.fixture.789.1').write_bytes(b'unrelated helper-process fixture')
            return mock.Mock(returncode=0)
        with mock.patch.object(coverage.subprocess, 'run', side_effect=run), mock.patch.object(coverage, 'selected_version', return_value='1.2.3'):
            with self.assertRaisesRegex(RuntimeError, 'every observed private prompt process'):
                coverage.portable_runtime_coverage({})

    def test_coverage_never_inherits_funded_public_staking_opt_in(self):
        env = {"WALLET_NEAR_STAKING_TESTNET": "1", "WALLET_NEAR_STAKING_LOCAL_RPC": "http://public.invalid",
               "WALLET_NEAR_LOCAL_RPC": "http://public.invalid", "WALLET_APTOS_LOCAL": "1",
               "WALLET_APTOS_ROTATION": "1", "LOCALWALLET_EVM_NODE": "http://public.invalid"}
        coverage.report["gates"] = []
        with mock.patch.object(coverage.shutil, 'which', return_value=None):
            coverage.local_gates(env)
        for key in ("WALLET_NEAR_STAKING_TESTNET", "WALLET_NEAR_STAKING_LOCAL_RPC", "WALLET_NEAR_LOCAL_RPC",
                    "WALLET_APTOS_LOCAL", "WALLET_APTOS_ROTATION", "LOCALWALLET_EVM_NODE"):
            self.assertNotIn(key, env)


if __name__ == "__main__":
    unittest.main()
