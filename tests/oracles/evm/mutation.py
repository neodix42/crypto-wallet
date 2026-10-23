"""Controlled chain-policy mutants, isolated copies, synthetic tests only.
Each mutant must fail a named existing invariant test; compiler failure does not kill it.
"""
import json, shutil, subprocess, tempfile
from pathlib import Path

root=Path(__file__).resolve().parents[3]
mutants=[
 ('EVM_CHAIN_ID','internal/chains/evm/adapter.go','if x.ChainID != n.ChainID {','if false {','./internal/chains/evm','TestTP042PlanAndSignatureTampering'),
 ('EVM_NATIVE_EFFECTS','internal/chains/evm/adapter.go','if x.To != p.Recipient || x.Value != p.Amount || x.Data != "0x" {','if false {','./internal/chains/evm','TestTP042PlanAndSignatureTampering'),
 ('EVM_TOKEN_APPROVAL','internal/chains/evm/adapter.go','if x.To != s.Contract || x.Value != "0" || x.Data != transferData(p.Recipient, val) {','if false {','./internal/chains/evm','TestTP042ReconstructionRejectsAllChangedEffects'),
 ('EVM_FEE_CEILING','internal/chains/evm/adapter.go','if max.BitLen() > 256 || p.MaxFee != max.String() {','if false {','./internal/chains/evm','TestTP042PlanAndSignatureTampering'),
 ('EVM_ROLLUP_FEE_UNAVAILABLE','internal/chains/evm/adapter.go','if rollup(n) && dataFee.Sign() == 0 {','if false {','./internal/chains/evm','TestTP052MissingRollupDataFee'),
 ('CORE_ENVIRONMENT','internal/chains/hypercore/adapter.go','if x.Action != want || x.SourceRole != "user"','want.HyperliquidChain = x.Action.HyperliquidChain\n if x.Action != want || x.SourceRole != "user"','./internal/chains/hypercore','TestTP042CorePlanSignatureMutations'),
 ('CORE_NONCE_BINDING','internal/chains/hypercore/adapter.go','if env.Action != x.Action || env.Nonce != x.Action.Time {','if env.Action != x.Action {','./internal/chains/hypercore','TestTP042CorePlanSignatureMutations'),
 ('CORE_SIGNATURE_OWNER','internal/chains/hypercore/adapter.go','if e != nil || crypto.PubkeyToAddress(*pub).Hex() != p.Account.Address {','if e != nil || (pub == nil && false) {','./internal/chains/hypercore','TestTP042CorePlanSignatureMutations'),
]
report=[]
for name,file,before,after,pkg,test in mutants:
    with tempfile.TemporaryDirectory(prefix='localwallet-chain-mutant-') as tmp:
        target=Path(tmp)
        for module in ['go.mod','go.sum']:shutil.copy(root/module,target/module)
        for directory in ['internal/model','internal/chains/evm','internal/chains/hypercore']:
            shutil.copytree(root/directory,target/directory)
        for directory in ['tests/oracles/evm','tests/oracles/hypercore']:
            (target/directory).mkdir(parents=True)
            for fixture in (root/directory).glob('*.json'):
                shutil.copy(fixture,target/directory/fixture.name)
        src=(target/file).read_text()
        if src.count(before)!=1:raise RuntimeError('mutant source no longer matches: '+name)
        (target/file).write_text(src.replace(before,after,1))
        ran=subprocess.run(['go','test',pkg,'-run','^'+test+'$','-count=1'],cwd=target,capture_output=True,text=True)
        killed=ran.returncode!=0 and '--- FAIL:' in ran.stdout and '[build failed]' not in ran.stdout+ran.stderr
        report.append({'mutant':name,'test':test,'status':'PASS' if killed else 'FAIL','result':'KILLED' if killed else 'SURVIVED_OR_INVALID'})
        print(name,report[-1]['result'],flush=True)
Path(__file__).with_name('mutation-results.json').write_text(json.dumps(report,indent=2)+'\n')
if any(x['status']!='PASS' for x in report):raise SystemExit(1)
