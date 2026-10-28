#!/usr/bin/env python3
"""Run Exchange authorization mutants through Go overlays, never live edits.

Only public fixture tests run. A compiler error, timeout, missing assertion or
changed source is INVALID; it is never counted as a killed security mutant.
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
REPORT = ROOT / "reports/exchange-signermutation.json"
SIGNER = "internal/signer/exchange.go"
MUTANTS = [
    {
        "id": "exchange-app-password-reauthentication",
        "policy": "A wrong app password cannot authorize a funds-moving Bybit operation; connecting an unlocked wallet is the explicit exception.",
        "path": SIGNER,
        "changes": [("!hmac.Equal(in[0], e.password)", "(!hmac.Equal(in[0], e.password) && false)")],
        "selector": "^TestExchangeTP043AuthorizationFailuresLockAndPersistence$/^wrong-password$",
        "failedTest": "TestExchangeTP043AuthorizationFailuresLockAndPersistence/wrong-password",
        "assertion": "rejected authorization released operation",
    },
    {
        "id": "exchange-one-use-authorization-capability",
        "policy": "A released POST capability cannot authorize the operation again.",
        "path": SIGNER,
        # Durable state and the ephemeral session map enforce this same policy
        # independently. This compound policy mutant explicitly bypasses both;
        # it does not claim that deleting either redundant check alone is killed.
        "changes": [("found.State != \"AUTHORIZED\"", "(found.State != \"AUTHORIZED\" && found.State != \"SUBMITTING\")"),
                    ("e.exchangeAuthorizations[found.ID] != found.Digest", "false")],
        "selector": "^TestExchangeTP040TP060CredentialsAndCrashSafeSwap$",
        "failedTest": "TestExchangeTP040TP060CredentialsAndCrashSafeSwap",
        "assertion": "POST authentication replayed",
    },
    {
        "id": "exchange-durable-submit-before-hmac-release",
        "policy": "A failed encrypted commit cannot release a POST HMAC capability.",
        "path": SIGNER,
        "changes": [("found.State = \"SUBMITTING\"\n\t\tif err = e.persist(); err != nil {\n\t\t\treturn bybit.Auth{}, err\n\t\t}",
                     "// MUTANT: deliberately omit the durable submission commit.")],
        "selector": "^TestExchangeTP060FailedDurableSubmissionReturnsNoSignature$",
        "failedTest": "TestExchangeTP060FailedDurableSubmissionReturnsNoSignature",
        "assertion": "undurable authentication escaped",
    },
    {
        "id": "exchange-current-owned-withdrawal-destination",
        "policy": "A changed owned account descriptor invalidates withdrawal authorization.",
        "path": SIGNER,
        "changes": [("if _, err = exchangeApproval(*found, account, asset); err != nil {",
                     "if _, err = exchangeApproval(*found, account, asset); false {")],
        "selector": "^TestExchangeTP042CurrentOwnedWithdrawalDescriptorMustStillMatch$",
        "failedTest": "TestExchangeTP042CurrentOwnedWithdrawalDescriptorMustStillMatch",
        "assertion": "withdrawal authenticated after owned address changed",
    },
    {
        "id": "exchange-duplicate-response-cannot-release-reservation",
        "policy": "Duplicate code 10014 cannot convert an ambiguous request into failure.",
        "path": "internal/vault/exchange.go",
        "changes": [("case 10001, 10003, 10004, 10005:", "case 10001, 10003, 10004, 10005, 10014:")],
        "selector": "^TestExchangeTP060DefinitiveRejectionReleasesOnlyDocumentedFirstPostFailures$",
        "failedTest": "TestExchangeTP060DefinitiveRejectionReleasesOnlyDocumentedFirstPostFailures",
        "assertion": "ambiguous/unsupported error released reservation",
    },
]


def sha(path: pathlib.Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sources() -> dict[str, str]:
    excluded = {".git", ".tools", "node_modules", "reports", "dist", "bin", "wallet-runtime", "__pycache__"}
    if shutil.which("rg"):
        command = ["rg", "--files", "--hidden", "--no-ignore", "-g", "*.go"]
        for directory in sorted(excluded):
            command += ["-g", "!**/" + directory + "/**"]
        names = subprocess.check_output(command, cwd=ROOT, text=True, timeout=30).splitlines()
    else:
        names = []
        for directory, children, files in os.walk(ROOT):
            children[:] = [name for name in children if name not in excluded]
            names.extend(str((pathlib.Path(directory) / name).relative_to(ROOT))
                         for name in files if name.endswith(".go"))
    names += ["go.mod", "go.sum", str(pathlib.Path(__file__).relative_to(ROOT))]
    return {name: sha(ROOT / name) for name in sorted(set(names))
            if not name.startswith((".tools/", "reports/", "dist/", "bin/", "wallet-runtime/"))}


def write_report(value: dict) -> None:
    REPORT.parent.mkdir(exist_ok=True)
    temporary = REPORT.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(REPORT)


def run(selector: str, env: dict[str, str], overlay: pathlib.Path | None = None) -> dict:
    command = ["go", "test", "-json", "-count=1", "-timeout=2m"]
    if overlay is not None:
        command += ["-overlay=" + str(overlay)]
    command += ["./internal/signer", "-run", selector]
    started = time.monotonic()
    process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=150)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.communicate()
        return {"exit": process.returncode, "invalidReason": "test deadline exceeded",
                "elapsedSeconds": round(time.monotonic() - started, 3)}
    # Inspect output only in memory. Never retain untrusted diagnostic bodies or
    # private inputs in the report; only fixed assertion markers are recorded.
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
    build_failed = any(event.get("Action") in ("build-fail",) or
                       "[build failed]" in event.get("Output", "") or
                       "undefined:" in event.get("Output", "") or
                       "declared and not used" in event.get("Output", "") or
                       "imported and not used" in event.get("Output", "")
                       for event in events)
    result = {"exit": process.returncode,
              "elapsedSeconds": round(time.monotonic() - started, 3),
              "passedTests": sorted({event["Test"] for event in events
                                      if event.get("Action") == "pass" and "Test" in event}),
              "failedTests": sorted({event["Test"] for event in events
                                      if event.get("Action") == "fail" and "Test" in event}),
              "skippedTests": sorted({event["Test"] for event in events
                                       if event.get("Action") == "skip" and "Test" in event}),
              "assertions": sorted({mutant["assertion"] for mutant in MUTANTS
                                    if any(mutant["assertion"] in event.get("Output", "")
                                           for event in events)})}
    if build_failed:
        result["invalidReason"] = "compiler or build failure"
    elif invalid:
        result["invalidReason"] = "unexpected non-JSON tool output"
    return result


def main() -> int:
    os.umask(0o077)
    started = time.monotonic()
    report = {"status": "RUNNING", "scope": "Five Exchange signer security policy mutants; public fixtures only",
              "execution": "Go overlay; original repository Go files never modified",
              "mutants": [], "sourceHashes": sources()}
    write_report(report)
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("WALLET_", "LOCALWALLET_")):
            env.pop(key)
    try:
        report["goVersion"] = subprocess.check_output(["go", "version"], cwd=ROOT, env=env,
                                                       text=True, timeout=30).strip()
        selector = "^(" + "|".join(sorted({m["selector"].split("$")[0].lstrip("^")
                                             for m in MUTANTS})) + ")$"
        baseline = run(selector, env)
        expected = {mutant["failedTest"].split("/")[0] for mutant in MUTANTS}
        baseline["status"] = "PASS" if (baseline["exit"] == 0 and not baseline.get("invalidReason")
                                             and not baseline["skippedTests"]
                                             and expected <= set(baseline["passedTests"])) else "FAIL"
        report["baseline"] = baseline
        write_report(report)
        print("Exchange mutation baseline:", baseline["status"], flush=True)
        if baseline["status"] != "PASS":
            raise RuntimeError("unmodified baseline did not pass")
        with tempfile.TemporaryDirectory(prefix="localwallet-exchange-mutants-") as directory:
            private = pathlib.Path(directory)
            env["WALLET_EVIDENCE_DIR"] = str(private / "evidence")
            for index, mutant in enumerate(MUTANTS):
                original = ROOT / mutant["path"]
                contents = original.read_text()
                row = {key: mutant[key] for key in ("id", "policy", "path", "selector", "failedTest")}
                row["changedGuards"] = len(mutant["changes"])
                if any(contents.count(old) != 1 for old, _ in mutant["changes"]):
                    row.update(status="INVALID", invalidReason="expected unique mutation source changed")
                else:
                    for old, new in mutant["changes"]:
                        contents = contents.replace(old, new, 1)
                    replacement = private / ("mutant-" + str(index) + ".go")
                    replacement.write_text(contents)
                    overlay = private / ("overlay-" + str(index) + ".json")
                    overlay.write_text(json.dumps({"Replace": {str(original): str(replacement)}}))
                    row["overlaySourceSHA256"] = sha(replacement)
                    execution = run(mutant["selector"], env, overlay)
                    row.update(execution)
                    if execution.get("invalidReason"):
                        row["status"] = "INVALID"
                    elif execution["exit"] == 0:
                        row["status"] = "SURVIVED"
                    elif (execution["exit"] == 1 and not execution["skippedTests"]
                          and mutant["failedTest"] in execution["failedTests"]
                          and mutant["assertion"] in execution["assertions"]):
                        row["status"] = "KILLED"
                        row["runtimeAssertion"] = mutant["assertion"]
                    else:
                        row["status"] = "INVALID"
                        row["invalidReason"] = "expected security assertion did not cause the failure"
                report["mutants"].append(row)
                write_report(report)
                print(mutant["id"], row["status"], flush=True)
        report["status"] = "PASS" if all(row["status"] == "KILLED" for row in report["mutants"]) else "FAIL"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        report["status"] = "FAIL"
        report["failureReason"] = "mutation harness or unmodified baseline could not complete"
    finally:
        report["sourceHashesAfter"] = sources()
        report["sourceStable"] = report["sourceHashesAfter"] == report["sourceHashes"]
        if not report["sourceStable"]:
            report["status"] = "FAIL"
            report["failureReason"] = "source changed during mutation campaign"
        report["elapsedSeconds"] = round(time.monotonic() - started, 3)
        write_report(report)
    return int(report["status"] != "PASS")


if __name__ == "__main__":
    raise SystemExit(main())
