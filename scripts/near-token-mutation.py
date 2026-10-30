#!/usr/bin/env python3
"""NEAR fungible-token security mutants in a disposable source snapshot.

Only public fixture tests run. Go overlays never rewrite the live checkout.
Every exact unmodified control must pass before mutation. Only its designated
runtime assertion can kill a mutant; build errors, skips, panics, deadlines and
unrelated failures are INVALID. No public RPC, user profile or funds are used.
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
REPORT = ROOT / "reports/near-token-mutation.json"
OPERATIONS = "internal/chains/near/token_operations.go"
TOKENS = "internal/chains/near/tokens.go"
IMPORT = "internal/signer/near_tokens.go"


def mutant(name, policy, path, changes, package, parent, case, assertion):
    expected = parent + ("/" + case if case else "")
    return {"id": name, "policy": policy, "path": path, "changes": changes,
            "package": package, "selector": "^" + parent + "$" + ("/^" + case + "$" if case else ""),
            "failedTest": expected, "assertion": assertion}


REJECT = "TestNEP141SignerRefusesEveryUnapprovedMethodOrSignedField"
PREFLIGHT = "TestNEP141TransferAndRegistrationPreflightFailuresNeverBroadcast"
RECEIPTS = "TestNEP141ReceiptsNeedExactEventAndCompleteSuccessfulChildren"
IMPORT_DENIAL = "TestNEP141ImportDeniesCancelledWrongPasswordStaleAndFailedPersistence"
MUTANTS = [
    mutant("token-import-confirmation", "Import needs explicit exact-policy consent.", IMPORT,
           [('if !e.UI.Confirm("Import NEP-141 token", summary) ||',
             'if (false && !e.UI.Confirm("Import NEP-141 token", summary)) ||')],
           "./internal/signer", IMPORT_DENIAL, "cancel", "failed authorization or storage installed a NEAR token policy"),
    mutant("token-import-current-password", "An unlocked wallet still reauthenticates token import.", IMPORT,
           [('!hmac.Equal(in[0], e.password)', '(false && !hmac.Equal(in[0], e.password))')],
           "./internal/signer", IMPORT_DENIAL, "password", "failed authorization or storage installed a NEAR token policy"),
    mutant("token-transfer-one-yocto", "ft_transfer attaches exactly one yoctoNEAR.", OPERATIONS,
           [(' || call.Deposit != "1" ||', ' || false ||')],
           "./internal/chains/near", REJECT, "deposit", "unapproved plan signed"),
    mutant("token-fixed-prepaid-gas", "The exact token call has the fixed approved 30 Tgas.", OPERATIONS,
           [('call.Gas != TokenGas', 'false')],
           "./internal/chains/near", REJECT, "gas", "unapproved plan signed"),
    mutant("token-method-allowlist", "No arbitrary method or receiver callback can be signed.", OPERATIONS,
           [('default:\n\t\treturn errInvalid\n\t}\n\treturn nil\n}\nfunc IsTokenPlan',
             'default:\n\t\treturn nil\n\t}\n\treturn nil\n}\nfunc IsTokenPlan')],
           "./internal/chains/near", REJECT, "method", "unapproved plan signed"),
    mutant("token-current-runtime-pinning", "Changed contract runtime cannot prepare a payment.", TOKENS,
           [('if got != t {', 'if got.Asset != t.Asset || got.Network != t.Network {')],
           "./internal/chains/near", PREFLIGHT, "changed-code", "hostile preparation accepted"),
    mutant("token-current-precision-pinning", "Current precision must equal the approved token policy.", TOKENS,
           [('if got != t {', 'if got.Network != t.Network || got.Asset.ID != t.Asset.ID || got.Asset.Contract != t.Asset.Contract || got.Asset.Symbol != t.Asset.Symbol || got.CodeHash != t.CodeHash {')],
           "./internal/chains/near", PREFLIGHT, "changed-decimals", "hostile preparation accepted"),
    # Go assigns #01 to the registration iteration of these same-named tests.
    # The exact expected test must pass in the control; renaming cannot silently
    # turn a missing registration assertion into a killed or passing mutant.
    mutant("token-self-registration-arguments", "Registration only targets the owned account and cannot authorize withdrawals.", OPERATIONS,
           [(' || call.Args != `{"account_id":"`+p.Account.Address+`","registration_only":true}`', '')],
           "./internal/chains/near", REJECT, "args#01", "unapproved plan signed"),
    mutant("token-registration-approved-deposit", "Signed storage deposit equals the exact approved native amount.", OPERATIONS,
           [(' || p.Amount != call.Deposit ||', ' || false ||')],
           "./internal/chains/near", REJECT, "deposit#01", "unapproved plan signed"),
    mutant("token-standard-transfer-event", "A successful call without its exact transfer event stays UNKNOWN.", OPERATIONS,
           [('if !tokenTransferEvent(p, d.Token.Policy.Asset.Contract, r.Receipts) {',
             'if false && !tokenTransferEvent(p, d.Token.Policy.Asset.Contract, r.Receipts) {')],
           "./internal/chains/near", RECEIPTS, "missing", "unverified transfer confirmed"),
    mutant("token-exact-event-amount", "Receipt event amount must match the approved atomic transfer.", OPERATIONS,
           [(' || item.Amount != p.Amount ||', ' || false ||')],
           "./internal/chains/near", RECEIPTS, "wrong-amount", "unverified transfer confirmed"),
    mutant("token-complete-successful-receipts", "A failed downstream receipt cannot be promoted to CONFIRMED.", OPERATIONS,
           [('observed, err := stakingReceipt(actual, id, r)',
             'observed, err := stakingReceipt(actual, id, r)\n\tif err == nil && observed.State == "UNKNOWN" { observed.State = "CONFIRMED" }')],
           "./internal/chains/near", RECEIPTS, "failed-child", "unverified transfer confirmed"),
    mutant("token-import-no-bybit-authority", "A watch policy never enables an unreviewed exchange route.", "internal/signer/exchange.go",
           [('for _, token := range e.NEP141TokenPolicies() {', 'for _, token := range []model.NEP141Token{} {')],
           "./internal/signer", "TestNEP141MainnetWatchPolicyCannotGainBybitAuthority", "", "mainnet imported NEAR token gained an unreviewed Bybit route"),
]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_report(report):
    REPORT.parent.mkdir(exist_ok=True)
    temporary = REPORT.with_suffix(".tmp")
    temporary.write_text(json.dumps(report, indent=2) + "\n")
    temporary.replace(REPORT)


def source_paths(env):
    """Bind first-party inputs actually loaded by the selected Go packages."""
    raw = subprocess.check_output(["go", "list", "-deps", "-test", "-json", "./internal/chains/near", "./internal/signer"],
                                  cwd=ROOT, env=env, text=True, timeout=60)
    decoder = json.JSONDecoder()
    names = {"go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum",
             "scripts/near-token-mutation.py", "scripts/near-token-mutation-test.py"}
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
    names.update(str(path.relative_to(ROOT)) for path in (ROOT / "tests/oracles/near").rglob("*")
                 if path.is_file() and "node_modules" not in path.parts and "__pycache__" not in path.parts)
    return sorted(names)


def classify_execution(exit_code, output, expected, assertion, timed_out=False, control=False):
    result = {"exit": exit_code, "expectedTest": expected}
    if timed_out:
        return {**result, "status": "INVALID", "invalidReason": "test deadline exceeded"}
    try:
        events = [json.loads(line) for line in output.splitlines()]
        if not events or any(not isinstance(event, dict) for event in events):
            raise ValueError("invalid event stream")
    except (ValueError, UnicodeError):
        return {**result, "status": "INVALID", "invalidReason": "invalid Go test event stream"}
    passed = sorted({event["Test"] for event in events if event.get("Action") == "pass" and isinstance(event.get("Test"), str)})
    failed = sorted({event["Test"] for event in events if event.get("Action") == "fail" and isinstance(event.get("Test"), str)})
    skipped = sorted({event["Test"] for event in events if event.get("Action") == "skip" and isinstance(event.get("Test"), str)})
    invalid_output = any(event.get("Action") == "build-fail" or any(marker in str(event.get("Output", ""))
                         for marker in ("[build failed]", "[setup failed]", "panic:", "fatal error:", "WARNING: DATA RACE")) for event in events)
    found = any(event.get("Test") == expected and assertion in str(event.get("Output", "")) for event in events)
    # Parent failures are the ordinary aggregate of the exact failed subtest.
    # A sibling or unrelated test failure cannot kill this security mutant.
    unexpected = [name for name in failed if name != expected and not expected.startswith(name + "/")]
    result.update(passedTests=passed, failedTests=failed, skippedTests=skipped,
                  expectedAssertionObserved=found)
    if invalid_output or skipped or unexpected or exit_code not in (0, 1):
        return {**result, "status": "INVALID", "invalidReason": "build, skip, panic, process or unrelated test failure"}
    if exit_code == 0 and not failed and expected in passed:
        return {**result, "status": "PASS" if control else "SURVIVED"}
    if not control and exit_code == 1 and expected in failed and found:
        return {**result, "status": "KILLED"}
    return {**result, "status": "INVALID", "invalidReason": "designated runtime assertion did not determine the result"}


def run(snapshot, specification, env, overlay=None, control=False):
    command = ["go", "test", "-json", "-count=1", "-timeout=110s"]
    if overlay is not None:
        command += ["-overlay=" + str(overlay)]
    command += [specification["package"], "-run", specification["selector"]]
    started = time.monotonic()
    process = subprocess.Popen(command, cwd=snapshot, env=env, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, text=True, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=120)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        result = classify_execution(process.returncode, "", specification["failedTest"], specification["assertion"], timed_out=True, control=control)
    else:
        result = classify_execution(process.returncode, output, specification["failedTest"], specification["assertion"], control=control)
    result["elapsedSeconds"] = round(time.monotonic() - started, 3)
    return result


def overlay_mutant(snapshot, temporary, index, specification):
    original = snapshot / specification["path"]
    contents = original.read_text()
    for old, new in specification["changes"]:
        if contents.count(old) != 1:
            raise ValueError("expected unique mutation source changed")
        contents = contents.replace(old, new, 1)
    replacement = temporary / (str(index) + ".go")
    replacement.write_text(contents)
    overlay = temporary / (str(index) + ".json")
    overlay.write_text(json.dumps({"Replace": {str(original): str(replacement)}}))
    return overlay, sha(replacement)


def main():
    os.umask(0o077)
    started = time.monotonic()
    report = {"status": "RUNNING", "scope": "Thirteen exact NEAR fungible-token security guards; public fixture tests only",
              "execution": "Private source snapshot and Go overlays; live sources never rewritten", "controls": [], "mutants": []}
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
        with tempfile.TemporaryDirectory(prefix="cryptowallet-near-token-mutants-") as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / "source"
            snapshot.mkdir()
            ignore = shutil.ignore_patterns("node_modules", "dist", "__pycache__")
            for name in ("internal", "tests/oracles"):
                shutil.copytree(ROOT / name, snapshot / name, ignore=ignore)
            for name in ("go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum", "scripts/near-token-mutation.py", "scripts/near-token-mutation-test.py"):
                destination = snapshot / name
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, destination)
            if any(sha(snapshot / name) != expected for name, expected in report["sourceHashes"].items()):
                raise RuntimeError("source changed while preparing the isolated snapshot")
            env["WALLET_EVIDENCE_DIR"] = str(temporary / "evidence")
            # Every designated case runs unchanged before any overlay is made.
            for specification in MUTANTS:
                row = {"id": specification["id"], **run(snapshot, specification, env, control=True)}
                report["controls"].append(row)
                write_report(report)
                if row["status"] != "PASS":
                    raise RuntimeError("unmodified designated security control did not pass")
            print("NEAR token mutation controls: PASS", flush=True)
            for index, specification in enumerate(MUTANTS):
                row = {key: specification[key] for key in ("id", "policy", "path", "package", "selector", "failedTest")}
                try:
                    overlay, digest = overlay_mutant(snapshot, temporary, index, specification)
                except ValueError:
                    row.update(status="INVALID", invalidReason="expected unique mutation source changed")
                else:
                    row["mutantSHA256"] = digest
                    row.update(run(snapshot, specification, env, overlay))
                report["mutants"].append(row)
                write_report(report)
                print(specification["id"], row["status"], flush=True)
        # Re-enumerate as well as rehash: a newly added Go test/source input
        # must invalidate evidence instead of escaping the original path list.
        report["sourceHashesAfter"] = {name: sha(ROOT / name) for name in source_paths(env)}
        report["sourceStable"] = report["sourceHashesAfter"] == report["sourceHashes"]
        report["status"] = "PASS" if report["sourceStable"] and len(report["mutants"]) == len(MUTANTS) and all(row["status"] == "KILLED" for row in report["mutants"]) else "FAIL"
        if not report["sourceStable"]:
            report["failureReason"] = "source changed during mutation campaign"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        report["status"] = "FAIL"
        report["failureReason"] = "mutation harness or unmodified designated controls did not complete"
    report["elapsedSeconds"] = round(time.monotonic() - started, 3)
    write_report(report)
    return int(report["status"] != "PASS")


if __name__ == "__main__":
    raise SystemExit(main())
