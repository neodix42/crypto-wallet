"""Arbitrum fee/implementation policy mutants; copied sources, public fixtures only.

Only a compiling, named invariant-test failure kills a mutant. No public RPC,
user vault, transaction submission or credentials are used.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[3]
MUTANTS = [
    ("ARB_OP_ORACLE_SUBSTITUTION", "registry.go",
     'return n.ChainID == "8453" || n.ChainID == "84532"',
     'return n.ChainID == "8453" || n.ChainID == "84532" || n.ChainID == "42161" || n.ChainID == "421614"',
     "TestArbitrumUsesFullNitroGasEstimateWithoutOPOracleOrSecondPostingCharge"),
    ("ARB_SEPARATE_POSTING_FEE_ALLOWED", "adapter.go",
     'if !rollup(n) && (dataFee.Sign() != 0 || operatorFee.Sign() != 0) {',
     'if false && !rollup(n) && (dataFee.Sign() != 0 || operatorFee.Sign() != 0) {',
     "TestArbitrumUsesFullNitroGasEstimateWithoutOPOracleOrSecondPostingCharge"),
    ("ARB_IMPLEMENTATION_SLOT_IGNORED", "adapter.go",
     'if implementation != "0x"+strings.Repeat("0", 24)+arbImplementation[2:] {',
     'if false && implementation != "0x"+strings.Repeat("0", 24)+arbImplementation[2:] {',
     "TestCanonicalARBRejectsProxyUpgradeOrChangedImplementation"),
    ("ARB_IMPLEMENTATION_CODE_IGNORED", "adapter.go",
     'if e != nil || !strings.HasPrefix(implementationCode, "0x") || len(bytes) == 0 || crypto.Keccak256Hash(bytes).Hex() != arbImplementationHash {',
     'if false && (e != nil || !strings.HasPrefix(implementationCode, "0x") || len(bytes) == 0 || crypto.Keccak256Hash(bytes).Hex() != arbImplementationHash) {',
     "TestCanonicalARBRejectsProxyUpgradeOrChangedImplementation"),
]


def sources():
    result = {}
    for folder in ["internal/chains/evm", "internal/chains/chainutil", "internal/model", "internal/strictjson", "internal/amount"]:
        for path in sorted((ROOT / folder).glob("*.go")):
            result[str(path.relative_to(ROOT))] = hashlib.sha256(path.read_bytes()).hexdigest()
    for name in ["go.mod", "go.sum", "tests/oracles/evm/arbitrum-runtime.json", "tests/oracles/evm/fixtures.json"]:
        result[name] = hashlib.sha256((ROOT / name).read_bytes()).hexdigest()
    return result


before = sources()
results = []
for name, filename, original, changed, test in MUTANTS:
    with tempfile.TemporaryDirectory(prefix="cryptowallet-arbitrum-mutant-") as temporary:
        target = Path(temporary)
        for filename_root in ["go.mod", "go.sum"]:
            shutil.copyfile(ROOT / filename_root, target / filename_root)
        for folder in ["internal/chains/evm", "internal/chains/chainutil", "internal/model", "internal/strictjson", "internal/amount"]:
            shutil.copytree(ROOT / folder, target / folder)
        (target / "third_party/wails").mkdir(parents=True)
        shutil.copyfile(ROOT / "third_party/wails/go.mod", target / "third_party/wails/go.mod")
        fixture_folder = target / "tests/oracles/evm"
        fixture_folder.mkdir(parents=True)
        for path in (ROOT / "tests/oracles/evm").glob("*.json"):
            shutil.copyfile(path, fixture_folder / path.name)
        destination = target / "internal/chains/evm" / filename
        source = destination.read_text()
        if source.count(original) != 1:
            raise RuntimeError("Mutant source no longer matches: " + name)
        destination.write_text(source.replace(original, changed, 1))
        env = dict(os.environ)
        env.pop("CRYPTOWALLET_PUBLIC_PROVIDERS", None)
        run = subprocess.run(["go", "test", "./internal/chains/evm", "-run", "^" + test + "$", "-count=1"],
                             cwd=target, capture_output=True, text=True, timeout=120, env=env)
        output = run.stdout + run.stderr
        killed = run.returncode != 0 and "--- FAIL: " + test in output and "[build failed]" not in output
        results.append({"mutant": name, "test": test, "status": "PASS" if killed else "FAIL", "result": "KILLED" if killed else "SURVIVED_OR_INVALID"})
        print(name, results[-1]["result"], flush=True)
        if not killed:
            print(output[-4000:], flush=True)
stable = before == sources()
report = {"status": "PASS" if stable and all(item["status"] == "PASS" for item in results) else "FAIL",
          "sourceStable": stable, "scope": "Four compiling Arbitrum fee/proxy policy mutants on copied production sources and public fixtures; no node consensus or real funds claim",
          "sourceSHA256": before, "mutants": results}
(ROOT / "reports").mkdir(exist_ok=True)
(ROOT / "reports/arbitrum-mutation.json").write_text(json.dumps(report, indent=2) + "\n")
if report["status"] != "PASS":
    raise SystemExit(1)
