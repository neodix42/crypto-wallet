#!/usr/bin/env python3
"""Native exchange policy sensitivity; isolated snapshots and Go overlays only.

Only the designated assertion's runtime failure can kill a mutant. Build errors,
panics, skips, timeouts and unrelated failures are INVALID, not security proof.
"""
from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE_PATH = ROOT / "scripts/near-token-mutation.py"
_spec = importlib.util.spec_from_file_location("exchange_mutation_runtime", BASE_PATH)
runtime = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(runtime)
REPORT = ROOT / "reports/exchange-native-mutations.json"
HELPER = "internal/exchange/test-native-mutations-test.py"

MUTANTS = [
    {"id": "native-exact-approved-wire", "path": "internal/signer/exchange.go",
     "changes": [("err == nil && expected == req", "err == nil && expected.Endpoint == req.Endpoint && expected.Method == req.Method && expected.Path == req.Path")],
     "package": "./internal/signer", "selector": "^TestNativeExchangeApprovedWirePayloadMustMatchExactly$/^binance$",
     "failedTest": "TestNativeExchangeApprovedWirePayloadMustMatchExactly/binance", "assertion": "altered native wire payload authenticated",
     "policy": "A native write must match the exact previously approved payload, not merely its endpoint and method."},
    {"id": "native-write-requires-approval", "path": "internal/signer/exchange.go",
     "changes": [("if !readOnly {", "if !readOnly && false {")],
     "package": "./internal/signer", "selector": "^TestNativeExchangeExactIOCApprovalDurableAuthorizationReplayAndRestart$/^binance$",
     "failedTest": "TestNativeExchangeExactIOCApprovalDurableAuthorizationReplayAndRestart/binance", "assertion": "unapproved native operation authenticated",
     "policy": "Read-only requests cannot grant a funds-moving authorization without exact consent and password."},
    {"id": "native-ioc-original-spending-ceiling", "path": "internal/exchange/native_policy.go",
     "changes": [("new(big.Int).Mul(baseN, priceN).Cmp(new(big.Int).Mul(budgetN, scale)) > 0", "new(big.Int).Mul(baseN, priceN).Cmp(new(big.Int).Mul(budgetN, scale)) > 0 && false")],
     "package": "./internal/exchange", "selector": "^TestNativeIOCOriginalSpendingCeilingUsesExactPriceTimesQuantity$",
     "failedTest": "TestNativeIOCOriginalSpendingCeilingUsesExactPriceTimesQuantity", "assertion": "unsafe native order was prepared",
     "policy": "The exact native base quantity multiplied by its limit price cannot exceed the original quote spending ceiling."},
    {"id": "kucoin-input-fee-inside-budget", "path": "internal/walletcore/exchange.go",
     "changes": [('if provider.ID == "kucoin" && fees[feeCoin] != nil {', 'if provider.ID == "kucoin" && fees[feeCoin] != nil && false {')],
     "package": "./internal/walletcore", "selector": "^TestNativeExchangeFillPricesAndInputFeesRetainOriginalSpendingBudget$",
     "failedTest": "TestNativeExchangeFillPricesAndInputFeesRetainOriginalSpendingBudget", "assertion": "input fee exceeded exact spending budget",
     "policy": "A quote-currency buy fee belongs inside the approved total spending ceiling."},
    {"id": "provider-api-passphrase-erasure", "path": "internal/vault/exchange.go",
     "changes": [("clear(credential.Passphrase)", "// MUTANT: omit explicit API passphrase buffer clearing")],
     "package": "./internal/vault", "selector": "^TestExchangeProvidersRoundtripEncryptedIdentityAndDestruction$",
     "failedTest": "TestExchangeProvidersRoundtripEncryptedIdentityAndDestruction", "assertion": "new provider private buffer survived destruction",
     "policy": "Destroy explicitly clears retained API key, secret and API passphrase buffers; this is not a general memory-erasure guarantee."},
    {"id": "native-execution-price-bound", "path": "internal/walletcore/exchange.go",
     "changes": [('request.Side == "Buy" && price.Cmp(limit) > 0', 'request.Side == "Buy" && price.Cmp(limit) > 0 && false')],
     "package": "./internal/walletcore", "selector": "^TestNativeExchangeFillPricesAndInputFeesRetainOriginalSpendingBudget$",
     "failedTest": "TestNativeExchangeFillPricesAndInputFeesRetainOriginalSpendingBudget", "assertion": "fill beyond approved limit accepted",
     "policy": "A reported fill beyond the exact approved native limit cannot resolve an operation as complete."},
]


def source_paths(env):
    raw = subprocess.check_output(["go", "list", "-deps", "-test", "-json", "./internal/exchange/...", "./internal/signer", "./internal/vault", "./internal/walletcore"],
                                  cwd=ROOT, env=env, text=True, timeout=60)
    names = {"go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum",
             "scripts/near-token-mutation.py", str(pathlib.Path(__file__).relative_to(ROOT)), HELPER}
    decoder = json.JSONDecoder()
    while raw.strip():
        item, offset = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[offset:]
        directory = pathlib.Path(item.get("Dir", "/"))
        for key in ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles", "EmbedFiles"):
            for filename in item.get(key, []):
                path = directory / filename
                try:
                    relative = path.relative_to(ROOT)
                except ValueError:
                    continue
                if path.is_file():
                    names.add(str(relative))
    names.update(str(path.relative_to(ROOT)) for path in (ROOT / "tests/oracles").rglob("*")
                 if path.is_file() and "node_modules" not in path.parts and "__pycache__" not in path.parts)
    return sorted(names)


def write_report(report):
    REPORT.parent.mkdir(exist_ok=True)
    REPORT.write_text(json.dumps(report, indent=2) + "\n")


def main():
    os.umask(0o077)
    started = time.monotonic()
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("WALLET_", "LOCALWALLET_")):
            env.pop(key)
    env["GOWORK"] = "off"
    report = {"status": "RUNNING", "scope": "Six native exchange security guards; public synthetic credentials and disposable profiles only",
              "execution": "Private source snapshot and Go overlays; production source files never rewritten",
              "controls": [], "mutants": [], "notRun": ["Private live exchange accounts", "Funded exchange operations", "Independent security review"]}
    try:
        report["goVersion"] = subprocess.check_output(["go", "version"], cwd=ROOT, env=env, text=True, timeout=30).strip()
        names = source_paths(env)
        report["sourceHashes"] = {name: runtime.sha(ROOT / name) for name in names}
        write_report(report)
        with tempfile.TemporaryDirectory(prefix="cryptowallet-exchange-native-mutants-") as directory:
            temporary = pathlib.Path(directory)
            snapshot = temporary / "source"
            snapshot.mkdir()
            ignored = shutil.ignore_patterns("node_modules", "dist", "__pycache__")
            for name in ("internal", "tests/oracles"):
                shutil.copytree(ROOT / name, snapshot / name, ignore=ignored)
            for name in ("go.mod", "go.sum", "third_party/wails/go.mod", "third_party/wails/go.sum", "scripts/near-token-mutation.py"):
                target = snapshot / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, target)
            if any(runtime.sha(snapshot / name) != expected for name, expected in report["sourceHashes"].items()):
                raise RuntimeError("source changed while copying isolated mutation inputs")
            env["WALLET_EVIDENCE_DIR"] = str(temporary / "evidence")
            for specification in MUTANTS:
                row = {"id": specification["id"], **runtime.run(snapshot, specification, env, control=True)}
                report["controls"].append(row)
                write_report(report)
                if row["status"] != "PASS":
                    raise RuntimeError("unmodified designated native security control did not pass")
            print("Native exchange mutation controls: PASS", flush=True)
            for index, specification in enumerate(MUTANTS):
                overlay, changed_hash = runtime.overlay_mutant(snapshot, temporary, index, specification)
                row = {"id": specification["id"], "policy": specification["policy"], "mutatedSourceSHA256": changed_hash,
                       **runtime.run(snapshot, specification, env, overlay=overlay)}
                report["mutants"].append(row)
                write_report(report)
                print(specification["id"] + ": " + row["status"], flush=True)
        report["sourceStable"] = source_paths(env) == names and all(runtime.sha(ROOT / name) == expected for name, expected in report["sourceHashes"].items())
        report["status"] = "PASS" if report["sourceStable"] and len(report["mutants"]) == len(MUTANTS) and all(row["status"] == "KILLED" for row in report["mutants"]) else "FAIL"
    except Exception as error:
        report["status"] = "FAIL"
        report["reason"] = type(error).__name__ + ": " + str(error)
    report["elapsedSeconds"] = round(time.monotonic() - started, 3)
    write_report(report)
    if report["status"] != "PASS":
        raise SystemExit(1)


if __name__ == "__main__":
    main()
