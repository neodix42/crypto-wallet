#!/usr/bin/env python3
"""Measure all shipped Go packages, including Wails/WebKit and retained GTK, without hiding skips.
No public-chain funds or user secrets are used. Local node gates are separate
from the required external testnet/release evidence.
"""
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import time
import tempfile

from version import selected_version
from evidence_binding import go_sources

root = pathlib.Path(__file__).resolve().parents[1]
reports = root / "reports"
report = {"status": "NOT_RUN", "measurement": "Go statement coverage, not branch coverage",
          "gates": [], "skippedTests": [], "exclusions": [], "profiles": []}


def source_hashes():
    # WebKit/GTK C and embedded React/fork inputs affect exercised behavior;
    # Go instrumentation does not measure C, JavaScript or TypeScript statements.
    return go_sources(root)


def local_gates(env):
    # These flags can only enable local emulator/simnet tests. External funded
    # testnet gates stay separate; do not inherit a user-selected live endpoint.
    for name in ("WALLET_NEAR_LOCAL_RPC", "WALLET_APTOS_LOCAL", "WALLET_APTOS_ROTATION", "LOCALWALLET_EVM_NODE"):
        env.pop(name, None)
    candidates = [os.environ.get("WALLET_DCRD", ""), str(root / ".tools/dcrd/dcrd"),
                  str(root / ".tools/dcrd"), "/tmp/localwallet-dcr-bin/dcrd"]
    env.pop("WALLET_DCRD", None)
    for candidate in candidates:
        path = pathlib.Path(candidate) if candidate else None
        if path is None or not path.is_file():
            continue
        build = subprocess.run(["go", "version", "-m", str(path)], cwd=root, text=True,
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15)
        expected = "mod\tgithub.com/decred/dcrd\tv1.10.9\th1:NnmBxAvXwzuzP6JGYDSNu1TBMIoLPp0YdVixfm9gLHs="
        if build.returncode != 0 or expected not in build.stdout:
            if candidate == os.environ.get("WALLET_DCRD"):
                raise RuntimeError("Explicit WALLET_DCRD does not have the pinned v1.10.9 Go module identity")
            continue
        env["WALLET_DCRD"] = str(path.resolve())
        report["gates"].append({"gate": "decred-simnet", "status": "READY", "source": "github.com/decred/dcrd v1.10.9",
                                "binarySHA256": hashlib.sha256(path.read_bytes()).hexdigest()})
        break
    else:
        report["gates"].append({"gate": "decred-simnet", "status": "BLOCKED",
                                "reason": "Pinned dcrd v1.10.9 executable unavailable; install under .tools/dcrd or set WALLET_DCRD"})
    sandbox = root / "tests/oracles/ton/node_modules/@ton/sandbox/package.json"
    env.pop("WALLET_TON_EMULATOR", None)
    if shutil.which("node") and sandbox.is_file() and json.loads(sandbox.read_text()).get("version") == "0.45.0":
        env["WALLET_TON_EMULATOR"] = "1"
        report["gates"].append({"gate": "ton-v5r1-emulator", "status": "READY", "source": "@ton/sandbox 0.45.0"})
    else:
        report["gates"].append({"gate": "ton-v5r1-emulator", "status": "BLOCKED",
                                "reason": "Run npm ci --ignore-scripts in tests/oracles/ton to install pinned emulator"})
    env.pop("WALLET_USDT_EVM", None)
    oracle = root / "tests/oracles/evm/node_modules"
    if shutil.which("node") and all((oracle / name / "package.json").is_file() and
            json.loads((oracle / name / "package.json").read_text()).get("version") == version
            for name, version in (("ethers", "6.17.0"), ("ganache", "7.9.2"))):
        env["WALLET_USDT_EVM"] = "1"
        report["gates"].append({"gate": "usdt-evm-execution", "status": "READY",
                                "source": "Pinned public USDT runtime; ethers 6.17.0 / Ganache 7.9.2"})
    else:
        report["gates"].append({"gate": "usdt-evm-execution", "status": "BLOCKED",
                                "reason": "Run npm ci --ignore-scripts in tests/oracles/evm"})
    env["WALLET_EVIDENCE_DIR"] = str(reports)


def all_packages(env):
    command = ["go", "test", "-tags", "production,webkit2_41", "-timeout=10m", "-count=1", "-covermode=atomic", "-coverpkg=./...",
               "-coverprofile=" + str(reports / "go-base.cover"), "-json", "./..."]
    packages = {}
    failures = []
    passed_tests = set()
    skip_notes = {}
    with (reports / "go-test.jsonl").open("w") as log:
        process = subprocess.Popen(command, cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        try:
            for line in process.stdout:
                log.write(line)
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    print(line, end="", flush=True)
                    continue
                action, package, test = event.get("Action"), event.get("Package"), event.get("Test")
                if action == "output" and test:
                    note = event.get("Output", "")
                    if "BLOCKED" in note or "NOT_RUN" in note:
                        key = (package, test)
                        skip_notes[key] = (skip_notes.get(key, "") + note)[:4096]
                if action == "skip" and test:
                    reason = skip_notes.get((package, test), "Explicit test skip; inspect go-test.jsonl for prerequisites")
                    report["skippedTests"].append({"package": package, "test": test, "status": "BLOCKED" if "BLOCKED" in reason else "NOT_RUN", "reason": reason.strip()})
                if action == "fail":
                    failures.append({"package": package, "test": test})
                if action == "pass" and test:
                    passed_tests.add((package, test))
                if action in ("pass", "fail", "skip") and not test:
                    packages[package] = action.upper()
                    print(action.upper(), package, flush=True)
                if action == "output" and ("--- FAIL:" in event.get("Output", "") or "coverage:" in event.get("Output", "")):
                    print(event["Output"], end="", flush=True)
            code = process.wait(timeout=30)
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
    report["goTests"] = {"status": "PASS" if code == 0 else "FAIL", "exit": code, "packages": packages, "failures": failures}
    for gate, test in (("decred-simnet", "TestDcrdSimnet"), ("ton-v5r1-emulator", "TestWalletV5R1Emulator"),
                       ("usdt-evm-execution", "TestTP050USDTGoSignedTransactionsExecuteOnIndependentGanache")):
        for row in report["gates"]:
            if row["gate"] == gate and row["status"] == "READY":
                executed = any(name == test for _, name in passed_tests)
                row["status"] = "FAIL" if any(x["test"] == test for x in failures) else ("PASS" if executed else "NOT_RUN")
    return code


PORTABLE_MEMORY_TESTS = {"TestStaticBuildDoesNotPretendLegacySandboxIsInstalled",
                         "TestTP041MemoryProtectionPreservesCoordinatorNetworking",
                         "TestTP041MemoryProtectionFailsClosedOnKernelErrors"}


def portable_memory_coverage(env):
    """Exercise the shipped memory-only implementation and real kernel faults.

    The full base suite still tests legacy seccomp separately. These targeted
    portable tests prove memory protection, loopback preservation and explicit
    refusal to pretend a syscall sandbox was installed; no production hook.
    """
    profile = reports / "go-portable-memory.cover"
    profile.unlink(missing_ok=True)
    command = ["go", "test", "-tags", "portable", "-covermode=atomic",
               "-coverprofile=" + str(profile), "-run=^(TestTP041MemoryProtection|TestStaticBuild)",
               "-count=1", "-json", "./internal/isolation"]
    result = subprocess.run(command, cwd=root, env=env, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=90)
    log = reports / "portable-memory-test.jsonl"
    log.write_text(result.stdout)
    passed, skipped, failed = set(), [], []
    for line in result.stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get("Test"):
            if event.get("Action") == "pass":
                passed.add(event["Test"])
            elif event.get("Action") == "skip":
                skipped.append(event["Test"])
            elif event.get("Action") == "fail":
                failed.append(event["Test"])
    status = "PASS" if result.returncode == 0 and PORTABLE_MEMORY_TESTS <= passed and not skipped and not failed else "BLOCKED" if skipped else "FAIL"
    report["portableMemory"] = {"status": status, "command": command,
                               "passedTests": sorted(passed), "skippedTests": skipped,
                               "failedTests": failed, "rawLogSHA256": hashlib.sha256(log.read_bytes()).hexdigest(),
                               "scope": "Actual memory-only portable code and test-only syscall tracing; no legacy network-denial claim"}
    print("Portable memory protection coverage:", status, flush=True)
    return 0 if status == "PASS" else 1


def portable_runtime_coverage(env):
    """Instrumented GUI acceptance stays separate from the shipped candidate."""
    profile = reports / "go-native-runtime.cover"
    profile.unlink(missing_ok=True)
    with tempfile.TemporaryDirectory(prefix="cryptowallet-coverage-") as temporary:
        folder = pathlib.Path(temporary)
        binary = folder / "cryptowallet-instrumented"
        counters = folder / "counters"
        counters.mkdir(mode=0o700)
        subprocess.run(["go", "build", "-tags", "production,webkit2_41,portable", "-cover", "-covermode=atomic",
                        "-coverpkg=./cmd/cryptowallet,./internal/nativewallet,./internal/webui,./internal/isolation",
                        "-trimpath", "-ldflags=-X localwallet/internal/buildinfo.Version=" + selected_version(root),
                        "-o", str(binary), "./cmd/cryptowallet"],
                       cwd=root, env=env, check=True, timeout=180)
        runtime_env = {**env, "GOCOVERDIR": str(counters)}
        result = subprocess.run(["python3", "scripts/wails-wallet-test.py", "--binary", str(binary),
                                 "--report-name", "wails-wallet-coverage", "--instrumented"],
                                cwd=root, env=runtime_env, timeout=900)
        evidence = reports / "wails-wallet-coverage.json"
        report["nativeRuntime"] = json.loads(evidence.read_text()) if evidence.is_file() else {"status": "BLOCKED", "reason": "Instrumented Wails runtime report missing"}
        report["nativeRuntime"]["instrumentedBinarySHA256"] = hashlib.sha256(binary.read_bytes()).hexdigest()
        if not any(counters.glob("covmeta.*")):
            raise RuntimeError("Instrumented native runtime produced no Go coverage metadata")
        subprocess.run(["go", "tool", "covdata", "textfmt", "-i=" + str(counters), "-o=" + str(profile)],
                       cwd=root, env=env, check=True, timeout=60)
        if result.returncode or report["nativeRuntime"].get("status") != "PASS" or report["nativeRuntime"].get("instrumented") is not True:
            return 1
        return 0


def merge_profiles(paths):
    blocks = {}
    sizes = {}
    excluded = {}
    for path in paths:
        lines = path.read_text().splitlines()
        if not lines or lines[0] != "mode: atomic":
            raise RuntimeError("Coverage union requires atomic profiles: " + str(path))
        report["profiles"].append({"file": str(path.relative_to(root)), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
        for line in lines[1:]:
            location, statements, count = line.rsplit(" ", 2)
            source = location.rsplit(":", 1)[0]
            if source in ("localwallet/internal/secureui/automation.go",
                          "localwallet/internal/secureui/recovery_automation.go",
                          "localwallet/internal/nativeui/automation.go"):
                excluded[source] = "automation build tag: test-only GTK fixture injector, absent from production"
                continue
            if source.startswith("localwallet/tests/oracles/"):
                excluded[source] = "independent non-shipped public reference generator"
                continue
            statements, count = int(statements), int(count)
            if statements < 0 or count < 0:
                raise RuntimeError("Negative coverage counters are invalid")
            if location in sizes and sizes[location] != statements:
                raise RuntimeError("Mismatched block source sizes across profiles: " + location)
            sizes[location] = statements
            key = (location, statements)
            # Union executions without multiplying the statement denominator.
            blocks[key] = max(blocks.get(key, 0), count)
    report["exclusions"] = [{"source": source, "reason": reason} for source, reason in sorted(excluded.items())]
    merged = reports / "go.cover"
    if not blocks:
        raise RuntimeError("No shipped production statement blocks were measured")
    merged.write_text("mode: atomic\n" + "".join(f"{location} {statements} {count}\n"
                                              for (location, statements), count in sorted(blocks.items())))
    return merged


def uncovered_blocks(profile):
    rows = []
    for line in profile.read_text().splitlines()[1:]:
        location, statements, count = line.rsplit(" ", 2)
        if int(count) or not int(statements):
            continue
        source, span = location.rsplit(":", 1)
        source = source.removeprefix("localwallet/")
        first, last = span.split(",")
        first_line, last_line = int(first.split(".")[0]), int(last.split(".")[0])
        lines = (root / source).read_text().splitlines()
        rows.append({"source": source, "start": first, "end": last, "statements": int(statements),
                     "excerpt": "\n".join(lines[first_line - 1:last_line])})
    (reports / "go-uncovered.json").write_text(json.dumps({"status": "MEASURED",
        "meaning": "Unexecuted production statement blocks; review is in docs/COVERAGE.md, not an audit approval",
        "blocks": rows}, indent=2) + "\n")
    functions = subprocess.check_output(["go", "tool", "cover", "-func=" + str(profile)], cwd=root, text=True)
    (reports / "go-functions.txt").write_text(functions)


def main():
    start = time.monotonic()
    reports.mkdir(exist_ok=True)
    try:
        before = source_hashes()
        env = os.environ.copy()
        local_gates(env)
        go_exit = all_packages(env)
        native = subprocess.run(["python3", "scripts/native-input.py"], cwd=root, timeout=120)
        native_report = reports / "native-input.json"
        report["nativeInput"] = json.loads(native_report.read_text()) if native_report.is_file() else {"status": "BLOCKED", "reason": "Native report missing"}
        native_ui = subprocess.run(["python3", "internal/nativeui/test-x11.py"], cwd=root, timeout=240)
        native_ui_report = reports / "native-ui.json"
        report["nativeDesktopInput"] = json.loads(native_ui_report.read_text()) if native_ui_report.is_file() else {"status": "BLOCKED", "reason": "Native desktop input report missing"}
        memory_exit = portable_memory_coverage(env)
        runtime_exit = portable_runtime_coverage(env)
        after = source_hashes()
        if after != before:
            report["changedSources"] = [name for name in sorted(before.keys() | after.keys()) if before.get(name) != after.get(name)]
            raise RuntimeError("Go sources/module pins changed during measurement; rerun on a stable working tree")
        report["sourceSHA256"] = before
        report["sourceCommit"] = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
        if go_exit or native.returncode or native_ui.returncode or memory_exit or runtime_exit:
            raise RuntimeError("A Go/native suite failed; do not reuse old coverage as evidence")
        # Retain the exact input to this measurement: a later standalone native
        # security run rewrites native-input.cover with different counters.
        native_profile = reports / "go-native.cover"
        shutil.copyfile(reports / "native-input.cover", native_profile)
        # Freeze this exact native-application run separately from legacy GTK
        # regression counters; both must remain available to evidence verification.
        native_ui_profile = reports / "go-nativeui.cover"
        shutil.copyfile(reports / "native-ui.cover", native_ui_profile)
        profile = merge_profiles([reports / "go-base.cover", native_profile, native_ui_profile, reports / "go-native-runtime.cover", reports / "go-portable-memory.cover"])
        uncovered_blocks(profile)
        subprocess.run(["go", "tool", "cover", "-html=" + str(profile), "-o", str(reports / "go.html")], cwd=root, check=True)
        floor = subprocess.run(["python3", "scripts/coverage.py", str(profile)], cwd=root)
        report["floors"] = "PASS" if floor.returncode == 0 else "FAIL"
        report["status"] = "PASS" if floor.returncode == 0 and all(row["status"] == "PASS" for row in report["gates"]) else "FAIL"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as error:
        report.update(status="FAIL", reason=str(error))
    finally:
        report["seconds"] = round(time.monotonic() - start, 3)
        (reports / "go-coverage.json").write_text(json.dumps(report, indent=2) + "\n")
    print("Go/native coverage:", report["status"], report.get("reason", ""), flush=True)
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
