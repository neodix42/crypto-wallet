#!/usr/bin/env python3
"""Compile isolated USDT policy mutants; actual TVM receipt gates stay enabled."""
import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import time

root = pathlib.Path(__file__).resolve().parents[3]
mutants = [
    ("canonical-master-code", "internal/chains/ton/jetton.go", "hex.EncodeToString(code.Hash()) != USDTMasterCodeHash", "(false && hex.EncodeToString(code.Hash()) != USDTMasterCodeHash)"),
    ("canonical-wallet-code", "internal/chains/ton/jetton.go", "hex.EncodeToString(code.Hash()) != USDTWalletCodeHash", "(false && hex.EncodeToString(code.Hash()) != USDTWalletCodeHash)"),
    ("recorded-owner", "internal/chains/ton/jetton.go", "!recordedOwner.Equals(own)", "(false && !recordedOwner.Equals(own))"),
    ("recorded-master", "internal/chains/ton/jetton.go", "!master.Equals(want)", "(false && !master.Equals(want))"),
    ("exact-transfer-balance", "internal/chains/ton/jetton.go", "amount.Sign() == 0 || balance.Cmp(amount) < 0", "amount.Sign() == 0 || (false && balance.Cmp(amount) < 0)"),
    ("derived-source-wallet", "internal/chains/ton/jetton.go", "j.Wallet != source.StringRaw()", "(false && j.Wallet != source.StringRaw())"),
    ("recipient-successful-VM", "internal/chains/ton/jetton_receipt.go", "if ok, known := success(recipientTx); !known || !ok {", "if ok, known := success(recipientTx); (!known && false) || (!ok && false) {"),
    ("exact-credit-notification", "internal/chains/ton/jetton_receipt.go", "&& sameCell(im.Body, want)", "&& (sameCell(im.Body, want) || true)"),
]
paths = [root / "go.mod", root / "go.sum", pathlib.Path(__file__).resolve(), root / "tests/oracles/ton/jetton.cjs"]
paths += sorted(p for p in (root / "internal/chains/ton").rglob("*") if p.is_file())
hashes = {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths}
environment = os.environ.copy()
environment["WALLET_TON_EMULATOR"] = "1"
environment.pop("WALLET_TON_PUBLIC_PROBE", None)
started = time.monotonic()
rows = []
with tempfile.TemporaryDirectory(prefix="cryptowallet-ton-usdt-mutants-") as temporary:
    copy = pathlib.Path(temporary)
    for name in ("go.mod", "go.sum"):
        shutil.copy2(root / name, copy / name)
    shutil.copytree(root / "internal", copy / "internal")
    shutil.copytree(root / "tests/oracles/ton", copy / "tests/oracles/ton", ignore=shutil.ignore_patterns("node_modules"))
    (copy / "tests/oracles/ton/node_modules").symlink_to(root / "tests/oracles/ton/node_modules", target_is_directory=True)
    def test():
        return subprocess.run(["go", "test", "-count=1", "-run", "TestCanonicalUSDT|TestUSDTState|TestUSDTActual|TestUSDTContinuation", "./internal/chains/ton"], cwd=copy, env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    baseline = test()
    if baseline.returncode:
        raise RuntimeError("Unmutated USDT baseline failed: " + baseline.stdout[-4000:])
    for name, filename, before, after in mutants:
        target = copy / filename
        original = target.read_text()
        if original.count(before) != 1:
            rows.append({"id": name, "status": "INVALID", "reason": "source match is not unique"})
            continue
        target.write_text(original.replace(before, after, 1))
        try:
            result = test()
            status = "INVALID" if "[build failed]" in result.stdout else "KILLED" if result.returncode else "SURVIVED"
            rows.append({"id": name, "status": status, "exitCode": result.returncode, "failureExcerpt": result.stdout[-2500:] if result.returncode else ""})
        except subprocess.TimeoutExpired:
            rows.append({"id": name, "status": "INVALID", "reason": "test timed out"})
        finally:
            target.write_text(original)
        print(name, rows[-1]["status"], flush=True)
stable = all(hashlib.sha256((root / path).read_bytes()).hexdigest() == value for path, value in hashes.items())
report = {"status": "PASS" if stable and all(row["status"] == "KILLED" for row in rows) else "FAIL", "sourceStable": stable, "sources": hashes, "unmutatedBaseline": "PASS", "scope": "Compiled isolated canonical-USDT identity/amount/owner/derived-wallet guards and actual pinned-contract TVM recipient VM/notification gates; no network or mainnet spending", "elapsedSeconds": round(time.monotonic() - started, 3), "mutants": rows}
(root / "reports").mkdir(exist_ok=True)
(root / "reports/ton-usdt-mutations.json").write_text(json.dumps(report, indent=2) + "\n")
raise SystemExit(report["status"] != "PASS")
