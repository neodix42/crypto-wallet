#!/usr/bin/env python3
"""Compile isolated TON deployment mutants; timeouts/build failures are INVALID."""
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
    ("deployment-zero-amount", "internal/chains/ton/ton.go", ' || p.Amount != "0"', ""),
    ("deployment-owned-recipient", "internal/chains/ton/ton.go", 'p.Recipient != p.Account.Address || ', ""),
    ("deployment-only-uninitialized", "internal/chains/ton/deployment.go", 'if !auth.Deploy || auth.Seqno != 0 {', 'if false {'),
    ("deployment-no-outgoing", "internal/chains/ton/deployment.go", 'if tx.OutMsgCount != 0 || (tx.IO.Out != nil && tx.IO.Out.List != nil && !tx.IO.Out.List.IsEmpty()) {', 'if false {'),
    ("deployment-active-authority", "internal/chains/ton/deployment.go", 'if err != nil || auth.Deploy || auth.Seqno == 0 {', 'if err != nil || (auth.Deploy && false) || (auth.Seqno == 0 && false) {'),
]
paths = [root / "go.mod", root / "go.sum", pathlib.Path(__file__).resolve()]
paths += sorted((root / "internal/chains/ton").glob("*"))
paths += [root / "tests/oracles/ton/deployment.cjs"]
hashes = {str(path.relative_to(root)): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths if path.is_file()}
environment = os.environ.copy()
environment.pop("WALLET_TON_EMULATOR", None)
started = time.monotonic()
rows = []
with tempfile.TemporaryDirectory(prefix="cryptowallet-ton-deployment-mutants-") as temporary:
    copy = pathlib.Path(temporary)
    for name in ("go.mod", "go.sum"):
        shutil.copy2(root / name, copy / name)
    shutil.copytree(root / "internal", copy / "internal")
    shutil.copytree(root / "tests/oracles/ton", copy / "tests/oracles/ton", ignore=shutil.ignore_patterns("node_modules"))
    def test():
        return subprocess.run(["go", "test", "-count=1", "./internal/chains/ton"], cwd=copy, env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    baseline = test()
    if baseline.returncode:
        raise RuntimeError("Unmutated TON baseline failed: " + baseline.stdout[-2000:])
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
            rows.append({"id": name, "status": status, "exitCode": result.returncode})
        except subprocess.TimeoutExpired:
            rows.append({"id": name, "status": "INVALID", "reason": "test timed out"})
        finally:
            target.write_text(original)
        print(name, rows[-1]["status"], flush=True)
stable = all(hashlib.sha256((root / path).read_bytes()).hexdigest() == value for path, value in hashes.items())
report = {"status": "PASS" if stable and all(row["status"] == "KILLED" for row in rows) else "FAIL", "sourceStable": stable, "sources": hashes, "unmutatedBaseline": "PASS", "scope": "Offline unit/adversarial TON deployment guards; actual emulator execution is a separate test", "elapsedSeconds": round(time.monotonic() - started, 3), "mutants": rows}
(root / "reports").mkdir(exist_ok=True)
(root / "reports/ton-deployment-mutations.json").write_text(json.dumps(report, indent=2) + "\n")
raise SystemExit(report["status"] != "PASS")
