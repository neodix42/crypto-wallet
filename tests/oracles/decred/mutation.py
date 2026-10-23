#!/usr/bin/env python3
"""DCR/TON funds-preservation mutants, each compiled in an isolated source copy.
Invalid source/compilation is INVALID, never counted as a killed mutant."""
import json,pathlib,shutil,subprocess,tempfile,time
root=pathlib.Path(__file__).resolve().parents[3]
mutants=[
 ('dcr-strict-bip32','internal/chains/decred/decred.go','next, e := k.Child(n)','next, e := k.ChildBIP32Std(n)','decred'),
 ('dcr-discard-tree','internal/chains/decred/decred.go','wire.NewOutPoint(h, in.Vout, in.Tree)','wire.NewOutPoint(h, in.Vout, wire.TxTreeRegular)','decred'),
 ('dcr-fake-input-value','internal/chains/decred/decred.go','o.Value != in.Value || o.Version != 0','false || o.Version != 0','decred'),
 ('dcr-foreign-change','internal/chains/decred/decred.go','if e != nil || change != x.ChangeAddress {','if e != nil || len(change)==0 {','decred'),
 ('ton-network-wallet-id','internal/chains/ton/ton.go','0x7fffff11','0x7fffff10','ton'),
 ('ton-active-code-authority','internal/chains/ton/network.go','if e != nil || hex.EncodeToString(code.Hash()) != CodeHash {','if e != nil || len(code.Hash())==0 {','ton'),
 ('ton-skip-signature-verification','internal/chains/ton/ton.go','!ed25519.Verify(pub, u.Hash(), sig)','(len(pub)==0 && len(sig)==0)','ton'),
]
rows=[];start=time.monotonic()
with tempfile.TemporaryDirectory(prefix='localwallet-dcr-ton-mutants-') as tmp:
 copy=pathlib.Path(tmp)
 for name in ('go.mod','go.sum'):shutil.copy2(root/name,copy/name)
 shutil.copytree(root/'internal',copy/'internal')
 for name in ('decred','ton'):shutil.copytree(root/'tests/oracles'/name,copy/'tests/oracles'/name,ignore=shutil.ignore_patterns('node_modules'))
 for name,file,before,after,pkg in mutants:
  target=copy/file;original=target.read_text()
  if original.count(before)!=1:rows.append({'id':name,'status':'INVALID','reason':'source match is not unique'});continue
  target.write_text(original.replace(before,after,1))
  try:
   p=subprocess.run(['go','test','-count=1','./internal/chains/'+pkg],cwd=copy,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
   status='INVALID' if '[build failed]' in p.stdout else 'KILLED' if p.returncode else 'SURVIVED'
   rows.append({'id':name,'status':status,'exitCode':p.returncode})
  except subprocess.TimeoutExpired:rows.append({'id':name,'status':'INVALID','reason':'test timed out'})
  finally:target.write_text(original)
  print(name,rows[-1]['status'],flush=True)
report={'status':'PASS' if all(r['status']=='KILLED' for r in rows) else 'FAIL','elapsedSeconds':round(time.monotonic()-start,3),'mutants':rows}
(root/'tests/oracles/decred/mutation-results.json').write_text(json.dumps(report,indent=2)+'\n')
raise SystemExit(report['status']!='PASS')
