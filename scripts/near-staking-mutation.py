#!/usr/bin/env python3
"""Exercise NEAR staking security mutants in an isolated source snapshot.

Only public synthetic fixtures run. The live checkout is never modified.
Compiler errors, timeouts, skipped assertions and unexpected failures are INVALID,
and never count as killed mutants. No RPC endpoint or real wallet is used.
"""
from __future__ import annotations

import hashlib
import json
import os
import pathlib
import shutil
import signal
import subprocess
import tempfile
import time


ROOT = pathlib.Path(__file__).resolve().parents[1]
REPORT = ROOT / "reports/near-staking-mutation.json"
NEAR = "internal/chains/near/staking.go"
SIGNER = "internal/signer/engine.go"
MUTANTS = [
    {
        "id": "staking-method-allowlist",
        "policy": "The signer cannot authorize arbitrary contract methods.",
        "path": NEAR,
        "changes": [("default:\n\t\treturn errInvalid\n\t}\n\treturn nil\n}\n\nfunc IsStakingPlan",
                     "default:\n\t\treturn nil\n\t}\n\treturn nil\n}\n\nfunc IsStakingPlan")],
        "package": "./internal/signer",
        "selector": "^TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay$/^arbitrary_method$",
        "failedTest": "TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay/arbitrary_method",
        "assertion": "unsafe staking intent entered durable authorization",
    },
    {
        "id": "staking-unstake-zero-attached-deposit",
        "policy": "An unstake call cannot attach native NEAR.",
        "path": NEAR,
        "changes": [('if deposit.Sign() != 0 || call.Args != `{"amount":"`+p.Amount+`"}` {',
                     'if call.Args != `{"amount":"`+p.Amount+`"}` {')],
        "package": "./internal/signer",
        "selector": "^TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay$/^unstake_attached_value$",
        "failedTest": "TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay/unstake_attached_value",
        "assertion": "unsafe staking intent entered durable authorization",
    },
    {
        "id": "staking-network-factory-namespace",
        "policy": "A mainnet pool cannot be silently reused on testnet.",
        "path": NEAR,
        "changes": [('suffixes = []string{".pool.f863973.m0"}',
                     'suffixes = []string{".pool.f863973.m0", ".poolv1.near"}')],
        "package": "./internal/chains/near",
        "selector": "^TestStakingPoolNetworkScope$",
        "failedTest": "TestStakingPoolNetworkScope",
        "assertion": "pool scope near-testnet/astro-stakers.poolv1.near",
    },
    {
        "id": "staking-current-app-password",
        "policy": "An unlocked vault still requires its current app password for staking.",
        "path": SIGNER,
        "changes": [('if len(in) != 1 || !hmac.Equal(in[0], e.password) || p.Expires',
                     'if len(in) != 1 || (!hmac.Equal(in[0], e.password) && false) || p.Expires')],
        "package": "./internal/signer",
        "selector": "^TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay$",
        "failedTest": "TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay",
        "assertion": "unlocked wallet signed without current password",
    },
    {
        "id": "staking-approved-plan-immutability",
        "policy": "Changing the durable plan while its private confirmation is open must be rejected before cryptographic signing; encrypted persistence independently checks its digest.",
        "path": SIGNER,
        # Both the captured plan and current journal record guard this boundary.
        # This one policy mutant deliberately removes both complementary checks.
        "changes": [(' || journal.Digest(p) != o.Digest || journal.Digest(o.Plan) != o.Digest', '')],
        "package": "./internal/signer",
        "selector": "^TestNEARStakingSignerRejectsPlanChangeAcrossConfirmation$",
        "failedTest": "TestNEARStakingSignerRejectsPlanChangeAcrossConfirmation",
        "assertion": "changed staking plan reached private signing",
    },
    {
        "id": "staking-pool-withdrawal-unlock",
        "policy": "withdraw_all is unavailable while the selected pool reports can_withdraw=false.",
        "path": NEAR,
        "changes": [('if request.Action == "withdraw_all" && !state.CanWithdraw {',
                     'if request.Action == "withdraw_all" && !state.CanWithdraw && false {')],
        "package": "./internal/chains/near",
        "selector": "^TestStakingReadAndPrepareAgainstHostileRPC$/^locked$",
        "failedTest": "TestStakingReadAndPrepareAgainstHostileRPC/locked",
        "assertion": "hostilelocked accepted actionwithdraw_all",
    },
    {
        "id": "staking-complete-downstream-receipts",
        "policy": "A successful pool entry point cannot confirm staking or withdrawal when its downstream receipt failed.",
        "path": NEAR,
        "changes": [('if err != nil || z.State != "CONFIRMED" {',
                     'if err != nil || z.State != "CONFIRMED" || p.Recipient != "" {')],
        "package": "./internal/chains/near",
        "selector": "^TestStakingRequiresCompleteSuccessfulDownstreamReceipts$/^deposit_and_stake$/^downstream-failure$",
        "failedTest": "TestStakingRequiresCompleteSuccessfulDownstreamReceipts/deposit_and_stake/downstream-failure",
        "assertion": "wantUNKNOWN",
    },
]


def sha(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_report(report: dict) -> None:
    REPORT.parent.mkdir(exist_ok=True)
    temporary = REPORT.with_suffix(".tmp")
    temporary.write_text(json.dumps(report, indent=2) + "\n")
    temporary.replace(REPORT)


def source_paths(env: dict[str, str], packages: list[str] | None = None) -> list[str]:
    """Bind all first-party sources/tests loaded by the two tested packages."""
    raw = subprocess.check_output(
        ["go", "list", "-deps", "-test", "-json"] + (packages or ["./internal/signer", "./internal/chains/near"]),
        cwd=ROOT, env=env, text=True, timeout=60,
    )
    decoder = json.JSONDecoder()
    names = {"go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum", str(pathlib.Path(__file__).relative_to(ROOT))}
    while raw.strip():
        item, offset = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[offset:]
        directory = pathlib.Path(item.get("Dir", "/"))
        for key in ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles", "EmbedFiles"):
            for name in item.get(key, []):
                path = directory / name
                try:
                    relative = path.relative_to(ROOT)
                except ValueError:
                    continue
                if path.is_file():
                    names.add(str(relative))
    names.update(str(path.relative_to(ROOT)) for path in (ROOT / "tests/oracles/near").glob("*.json"))
    return sorted(names)


def run(snapshot: pathlib.Path, package: str, selector: str, env: dict[str, str], overlay: pathlib.Path | None = None) -> dict:
    command = ["go", "test", "-json", "-count=1", "-timeout=2m"]
    if overlay is not None:
        command += ["-overlay=" + str(overlay)]
    command += [package, "-run", selector]
    started = time.monotonic()
    process = subprocess.Popen(command, cwd=snapshot, env=env, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=150)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.communicate()
        return {"exit": process.returncode, "invalidReason": "test deadline exceeded"}
    events = []
    invalid = False
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except (ValueError, UnicodeError):
            invalid = True
            continue
        if not isinstance(event, dict):
            invalid = True
            continue
        events.append(event)
    result = {
        "exit": process.returncode,
        "elapsedSeconds": round(time.monotonic() - started, 3),
        "passedTests": sorted({event["Test"] for event in events if event.get("Action") == "pass" and "Test" in event}),
        "failedTests": sorted({event["Test"] for event in events if event.get("Action") == "fail" and "Test" in event}),
        "skippedTests": sorted({event["Test"] for event in events if event.get("Action") == "skip" and "Test" in event}),
        "assertions": sorted({mutant["assertion"] for mutant in MUTANTS if any(mutant["assertion"] in event.get("Output", "") for event in events)}),
    }
    if any(event.get("Action") == "build-fail" or "[build failed]" in event.get("Output", "") for event in events):
        result["invalidReason"] = "compiler or build failure"
    elif invalid:
        result["invalidReason"] = "unexpected non-JSON tool output"
    return result


def main() -> int:
    os.umask(0o077)
    started = time.monotonic()
    report = {"status": "RUNNING", "scope": "Seven NEAR staking semantic security policy mutants; public fixture tests only",
              "execution": "Isolated temporary source snapshot plus private Go overlays; live files never modified", "mutants": []}
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("WALLET_", "LOCALWALLET_")):
            env.pop(key)
    env["GOWORK"] = "off"
    try:
        report["goVersion"] = subprocess.check_output(["go", "version"], cwd=ROOT, env=env, text=True, timeout=30).strip()
        paths = source_paths(env)
        report["sourceHashes"] = {name: sha(ROOT / name) for name in paths}
        write_report(report)
        with tempfile.TemporaryDirectory(prefix="cryptowallet-near-staking-mutants-") as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / "source"
            snapshot.mkdir()
            ignore = shutil.ignore_patterns("node_modules", "dist", "__pycache__")
            for name in ("internal", "tests/oracles"):
                shutil.copytree(ROOT / name, snapshot / name, ignore=ignore)
            for name in ("go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum", str(pathlib.Path(__file__).relative_to(ROOT))):
                target = snapshot / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            if any(sha(snapshot / name) != expected for name, expected in report["sourceHashes"].items()):
                raise RuntimeError("source changed while preparing the isolated snapshot")
            env["WALLET_EVIDENCE_DIR"] = str(temporary / "evidence")
            baseline = []
            for package in sorted({mutant["package"] for mutant in MUTANTS}):
                tests = sorted({mutant["selector"].split("$")[0].lstrip("^") for mutant in MUTANTS if mutant["package"] == package})
                row = run(snapshot, package, "^(" + "|".join(tests) + ")$", env)
                expected = {mutant["failedTest"].split("/")[0] for mutant in MUTANTS if mutant["package"] == package}
                row["package"] = package
                row["status"] = "PASS" if row["exit"] == 0 and not row.get("invalidReason") and not row["skippedTests"] and expected <= set(row["passedTests"]) else "FAIL"
                baseline.append(row)
            report["baseline"] = baseline
            write_report(report)
            if any(row["status"] != "PASS" for row in baseline):
                raise RuntimeError("unmodified baseline did not pass")
            print("NEAR staking mutation baselines: PASS", flush=True)
            for index, mutant in enumerate(MUTANTS):
                row = {key: mutant[key] for key in ("id", "policy", "path", "package", "selector", "failedTest")}
                original = snapshot / mutant["path"]
                contents = original.read_text()
                if any(contents.count(old) != 1 for old, _ in mutant["changes"]):
                    row.update(status="INVALID", invalidReason="expected unique mutation source changed")
                else:
                    for old, new in mutant["changes"]:
                        contents = contents.replace(old, new, 1)
                    replacement = temporary / (str(index) + ".go")
                    replacement.write_text(contents)
                    overlay = temporary / (str(index) + ".json")
                    overlay.write_text(json.dumps({"Replace": {str(original): str(replacement)}}))
                    row["mutantSHA256"] = sha(replacement)
                    execution = run(snapshot, mutant["package"], mutant["selector"], env, overlay)
                    row.update(execution)
                    if execution.get("invalidReason"):
                        row["status"] = "INVALID"
                    elif execution["exit"] == 0:
                        row["status"] = "SURVIVED"
                    elif execution["exit"] == 1 and not execution["skippedTests"] and mutant["failedTest"] in execution["failedTests"] and mutant["assertion"] in execution["assertions"]:
                        row["status"] = "KILLED"
                    else:
                        row.update(status="INVALID", invalidReason="expected security assertion did not cause the failure")
                report["mutants"].append(row)
                write_report(report)
                print(mutant["id"], row["status"], flush=True)
        report["sourceHashesAfter"] = {name: sha(ROOT / name) for name in paths}
        report["sourceStable"] = report["sourceHashesAfter"] == report["sourceHashes"]
        report["status"] = "PASS" if report["sourceStable"] and len(report["mutants"]) == len(MUTANTS) and all(row["status"] == "KILLED" for row in report["mutants"]) else "FAIL"
        if not report["sourceStable"]:
            report["failureReason"] = "source changed during mutation campaign"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        report["status"] = "FAIL"
        report["failureReason"] = "mutation harness or unmodified baseline could not complete"
    report["elapsedSeconds"] = round(time.monotonic() - started, 3)
    write_report(report)
    return int(report["status"] != "PASS")


if __name__ == "__main__":
    raise SystemExit(main())
