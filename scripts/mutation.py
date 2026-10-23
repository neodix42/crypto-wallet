#!/usr/bin/env python3
"""Controlled critical mutants run in an isolated source copy. Compile errors
are INVALID, never counted as killed. Surviving mutants fail the command."""
import json,pathlib,shutil,subprocess,tempfile,time
root=pathlib.Path(__file__).resolve().parents[1]
mutants=[
 ('aead-authentication','internal/vault/envelope.go','return nil, ErrUnlock','return []byte("synthetic authentication bypass"), nil','./internal/vault'),
 ('kdf-floor','internal/vault/envelope.go','p.Memory >= 64*1024','p.Memory >= 1024','./internal/vault'),
 ('chain-id','internal/chains/evm/adapter.go','if x.ChainID != n.ChainID {','if false {','./internal/chains/evm'),
 ('native-confirmation','internal/signer/engine.go','if !e.UI.Confirm("Authorize exact transfer','if false && !e.UI.Confirm("Authorize exact transfer','./internal/signer'),
 ('changed-recipient','internal/chains/evm/adapter.go','x.To != p.Recipient || x.Value != p.Amount','false || x.Value != p.Amount','./internal/chains/evm'),
 ('foreign-change','internal/chains/decred/decred.go','if e != nil || change != x.ChangeAddress {','if e != nil || len(change)==0 {','./internal/chains/decred'),
 ('locked-signing','internal/signer/engine.go','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif e.data == nil || e.persistFailed {','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif false {','./internal/signer'),
 ('nonce-reservation','internal/journal/journal.go','o.Plan.Account.ID == p.Account.ID && !Terminal(o.State)','o.Plan.Account.ID == p.Account.ID && false','./internal/journal ./internal/signer'),
 ('ambiguous-resign','internal/journal/journal.go','s == "CANCELLED" || s == "FAILED"','s == "UNKNOWN" || s == "CANCELLED" || s == "FAILED"','./internal/journal ./internal/signer'),
 ('password-reauthentication','internal/signer/engine.go','!hmac.Equal(in[0], e.password) || p.Expires','false || p.Expires','./internal/signer'),
]
rows=[];start=time.monotonic()
with tempfile.TemporaryDirectory(prefix='localwallet-mutants-') as tmp:
    copy=pathlib.Path(tmp)/'source'
    shutil.copytree(root,copy,ignore=shutil.ignore_patterns('.git','node_modules','.tools','dist','bin','reports','.idea'))
    for name,filename,before,after,packages in mutants:
        target=copy/filename;original=target.read_text()
        if original.count(before)!=1:
            rows.append({'id':name,'status':'INVALID','reason':'expected unique source location changed'});continue
        target.write_text(original.replace(before,after,1))
        # Removing a funds check may make its local variable unused; keep that
        # public value referenced so this is a semantic mutant, not a syntax test.
        if name=='foreign-change':target.write_text(target.read_text().replace('change, e := AddressAt','change, e := AddressAt').replace('if e != nil {\n\t\treturn errors.New("foreign change destination")','if e != nil || len(change)==0 {\n\t\treturn errors.New("foreign change destination")'))
        try:
            p=subprocess.run(['go','test','-count=1',*packages.split()],cwd=copy,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
            status='INVALID' if '[build failed]' in p.stdout else 'KILLED' if p.returncode else 'SURVIVED'
            rows.append({'id':name,'status':status,'exit':p.returncode})
        except subprocess.TimeoutExpired:rows.append({'id':name,'status':'INVALID','reason':'test timed out'})
        finally:target.write_text(original)
        print(name,rows[-1]['status'],flush=True)
report={'status':'PASS' if all(r['status']=='KILLED' for r in rows) else 'FAIL','elapsedSeconds':round(time.monotonic()-start,3),'mutants':rows}
(root/'reports').mkdir(exist_ok=True);(root/'reports/mutation.json').write_text(json.dumps(report,indent=2)+'\n')
raise SystemExit(report['status']!='PASS')
