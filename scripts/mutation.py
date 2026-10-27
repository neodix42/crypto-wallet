#!/usr/bin/env python3
"""Controlled critical mutants run in an isolated source copy. Compile errors
are INVALID, never counted as killed. Surviving mutants fail the command."""
import argparse,hashlib,json,pathlib,shutil,subprocess,tempfile,time
from evidence_binding import go_sources
root=pathlib.Path(__file__).resolve().parents[1]
mutants=[
 ('prompt-identity-binding','internal/uiprompt/prompt.go','r.PromptID != p.ID','false','./internal/uiprompt'),
 ('electron-recorded-word-verification','internal/uiprompt/prompt.go','subtle.ConstantTimeCompare(value, words[positions[i]]) != 1','subtle.ConstantTimeCompare(value, words[positions[i]]) < 0','./internal/uiprompt'),
 ('aead-authentication','internal/vault/envelope.go','return nil, ErrUnlock','return []byte("synthetic authentication bypass"), nil','./internal/vault'),
 ('kdf-floor','internal/vault/envelope.go','p.Memory >= 64*1024','p.Memory >= 1024','./internal/vault'),
 ('chain-id','internal/chains/evm/adapter.go','if x.ChainID != n.ChainID {','if false {','./internal/chains/evm'),
 ('native-confirmation','internal/signer/engine.go','if !e.UI.Confirm("Authorize exact transfer','if false && !e.UI.Confirm("Authorize exact transfer','./internal/signer'),
 ('changed-recipient','internal/chains/evm/adapter.go','x.To != p.Recipient || x.Value != p.Amount','false || x.Value != p.Amount','./internal/chains/evm'),
 ('foreign-change','internal/chains/decred/decred.go','if e != nil || change != x.ChangeAddress {','if e != nil || len(change)==0 {','./internal/chains/decred'),
 ('locked-signing','internal/signer/engine.go','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif !e.canAuthorize() {','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif false {','./internal/signer'),
 ('recovery-authorization','internal/signer/engine.go','return e.data != nil && !e.persistFailed && e.data.RecoveryConfirmed','return e.data != nil && !e.persistFailed','./internal/signer'),
 ('recorded-phrase-confirmation','internal/signer/engine.go','if !ui.Recovery("Record recovery phrase — Go signer", phrase) {','if false && !ui.Recovery("Record recovery phrase — Go signer", phrase) {','./internal/signer'),
 ('nonce-reservation','internal/journal/journal.go','o.Plan.Account.ID == p.Account.ID && !Terminal(o.State)','o.Plan.Account.ID == p.Account.ID && false','./internal/journal ./internal/signer'),
 ('ambiguous-resign','internal/journal/journal.go','s == "CANCELLED" || s == "FAILED"','s == "UNKNOWN" || s == "CANCELLED" || s == "FAILED"','./internal/journal ./internal/signer'),
 ('password-reauthentication','internal/signer/engine.go','!hmac.Equal(in[0], e.password) || p.Expires','false || p.Expires','./internal/signer'),
 ('backup-ready-state-and-reentry','internal/signer/engine.go','if !e.canAuthorize() || e.backupInProgress {','if false {','./internal/signer'),
 ('backup-explicit-confirmation','internal/signer/engine.go','if !accepted {\n\t\treturn nil\n\t}\n\tin, err := e.UI.Input("Confirm app password to back up recovery phrase"','if false && !accepted {\n\t\treturn nil\n\t}\n\tin, err := e.UI.Input("Confirm app password to back up recovery phrase"','./internal/signer'),
 ('backup-app-password-reauthentication','internal/signer/engine.go','if len(in) != 1 || !hmac.Equal(in[0], e.password) {\n\t\treturn ErrDenied\n\t}\n\t// Authentication inputs are no longer needed while the words are displayed.','if len(in) != 1 {\n\t\treturn ErrDenied\n\t}\n\t// Authentication inputs are no longer needed while the words are displayed.','./internal/signer'),
 ('backup-current-generation-binding','internal/signer/engine.go','return e.data == d && e.canAuthorize() && d.Generation == generation','return e.data == d && e.canAuthorize() && (d.Generation == generation || true)','./internal/signer'),
]
# New secret-display guards require an unmodified passing control and their
# exact runtime security assertion. An unrelated failure or panic cannot kill
# these mutants. Existing default mutants and their criteria remain unchanged.
backup_checks={
 'backup-ready-state-and-reentry':('^TestTP033BackupPhraseRejectsUnavailableAndPendingVaultWithoutPrompt$/^(pending|reentry)$', 'TestTP033BackupPhraseRejectsUnavailableAndPendingVaultWithoutPrompt/pending', 'unavailable backup requested secret input or phrase'),
 'backup-explicit-confirmation':('^TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority$/^cancel-confirm$', 'TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority/cancel-confirm', 'backup cancellation or authentication did not fail safely'),
 'backup-app-password-reauthentication':('^TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority$/^wrong-password$', 'TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority/wrong-password', 'backup cancellation or authentication did not fail safely'),
 'backup-current-generation-binding':('^TestTP043BackupPhraseChecksSessionAtEveryPromptBoundary$/^input$/^generation$', 'TestTP043BackupPhraseChecksSessionAtEveryPromptBoundary/input/generation', 'backup accepted authorization across a changed session'),
}
parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--only',nargs='+',choices=[m[0] for m in mutants],help='Run only these exact mutant IDs; the default still runs every mutant.')
parser.add_argument('--report',help='Repository-relative reports/*.json output; selected runs default to mutation-selected.json.')
args=parser.parse_args()
selected=[m for m in mutants if args.only is None or m[0] in args.only]
report_name=args.report or ('reports/mutation-selected.json' if args.only else 'reports/mutation.json')
report_path=pathlib.PurePosixPath(report_name)
if report_path.is_absolute() or len(report_path.parts)!=2 or report_path.parts[0]!='reports' or report_path.suffix!='.json':
    parser.error('--report must be a direct repository-relative reports/*.json path')
if args.only and report_path==pathlib.PurePosixPath('reports/mutation.json'):
    parser.error('Selected runs must not overwrite the full mutation report')

def sources():
    result=go_sources(root)
    for name in ('scripts/mutation.py','scripts/evidence_binding.py'):
        result[name]=hashlib.sha256((root/name).read_bytes()).hexdigest()
    return result

def backup_execution(copy,packages,selector,expected,assertion):
    started=time.monotonic()
    try:
        p=subprocess.run(['go','test','-json','-count=1','-timeout=2m',*packages.split(),'-run',selector],cwd=copy,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
    except subprocess.TimeoutExpired:
        return {'status':'INVALID','reason':'test timed out'}
    try:
        events=[json.loads(line) for line in p.stdout.splitlines()]
        if not events or any(not isinstance(event,dict) for event in events):
            raise ValueError('invalid test events')
    except ValueError:
        return {'status':'INVALID','reason':'invalid Go test event stream'}
    passed=[e['Test'] for e in events if e.get('Action')=='pass' and 'Test' in e]
    failed=[e['Test'] for e in events if e.get('Action')=='fail' and 'Test' in e]
    skipped=[e['Test'] for e in events if e.get('Action')=='skip' and 'Test' in e]
    invalid=any(e.get('Action')=='build-fail' or '[build failed]' in e.get('Output','') or 'panic:' in e.get('Output','') for e in events)
    found=any(assertion in e.get('Output','') and e.get('Test')==expected for e in events)
    status='INVALID' if invalid or skipped else 'PASS' if p.returncode==0 and expected in passed else 'KILLED' if p.returncode==1 and expected in failed and found else 'SURVIVED' if p.returncode==0 else 'INVALID'
    return {'status':status,'exit':p.returncode,'elapsedSeconds':round(time.monotonic()-started,3),'expectedTest':expected,'expectedAssertionObserved':found,'passedTests':passed,'failedTests':failed,'skippedTests':skipped}

rows=[];start=time.monotonic();source_before=sources()
with tempfile.TemporaryDirectory(prefix='localwallet-mutants-') as tmp:
    copy=pathlib.Path(tmp)/'source'
    shutil.copytree(root,copy,ignore=shutil.ignore_patterns('.git','node_modules','.tools','dist','bin','reports','wallet-runtime','wallet','.idea','__pycache__'))
    for name,filename,before,after,packages in selected:
        target=copy/filename;original=target.read_text()
        if original.count(before)!=1:
            rows.append({'id':name,'status':'INVALID','reason':'expected unique source location changed'});continue
        if name in backup_checks:
            selector,expected,assertion=backup_checks[name]
            baseline=backup_execution(copy,packages,selector,expected,assertion)
            if baseline['status']!='PASS':
                rows.append({'id':name,'status':'INVALID','reason':'unmodified security control did not pass','baseline':baseline})
                print(name,rows[-1]['status'],flush=True)
                continue
        target.write_text(original.replace(before,after,1))
        # Removing a funds check may make its local variable unused; keep that
        # public value referenced so this is a semantic mutant, not a syntax test.
        if name=='foreign-change':target.write_text(target.read_text().replace('change, e := AddressAt','change, e := AddressAt').replace('if e != nil {\n\t\treturn errors.New("foreign change destination")','if e != nil || len(change)==0 {\n\t\treturn errors.New("foreign change destination")'))
        try:
            if name in backup_checks:
                execution=backup_execution(copy,packages,selector,expected,assertion)
                if execution['status']=='PASS':execution['status']='SURVIVED'
                rows.append({'id':name,'baseline':baseline,'selector':selector,'source':filename,'mutantSourceSHA256':hashlib.sha256(target.read_bytes()).hexdigest(),**execution})
            else:
                p=subprocess.run(['go','test','-count=1',*packages.split()],cwd=copy,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=120)
                status='INVALID' if '[build failed]' in p.stdout else 'KILLED' if p.returncode else 'SURVIVED'
                rows.append({'id':name,'status':status,'exit':p.returncode})
        except subprocess.TimeoutExpired:rows.append({'id':name,'status':'INVALID','reason':'test timed out'})
        finally:target.write_text(original)
        print(name,rows[-1]['status'],flush=True)
source_after=sources()
report={'status':'PASS' if rows and all(r['status']=='KILLED' for r in rows) and source_before==source_after else 'FAIL','scope':'selected' if args.only else 'all','selectedIDs':[m[0] for m in selected],'sourceSHA256':source_before,'sourceSHA256After':source_after,'sourceStable':source_before==source_after,'elapsedSeconds':round(time.monotonic()-start,3),'mutants':rows}
(root/'reports').mkdir(exist_ok=True);(root/report_path).write_text(json.dumps(report,indent=2)+'\n')
raise SystemExit(report['status']!='PASS')
