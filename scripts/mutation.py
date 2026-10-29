#!/usr/bin/env python3
"""Controlled critical mutants run in an isolated source copy. Compile errors
are INVALID, never counted as killed. Surviving mutants fail the command."""
import argparse,hashlib,json,os,pathlib,shutil,signal,subprocess,tempfile,time
from evidence_binding import go_sources
root=pathlib.Path(__file__).resolve().parents[1]
mutants=[
 ('prompt-identity-binding','internal/uiprompt/prompt.go','r.PromptID != p.ID','false','./internal/uiprompt'),
 ('electron-recorded-word-verification','internal/uiprompt/prompt.go','subtle.ConstantTimeCompare(value, words[positions[i]]) != 1','subtle.ConstantTimeCompare(value, words[positions[i]]) < 0','./internal/uiprompt'),
 ('aead-authentication','internal/vault/envelope.go','return nil, ErrUnlock','return []byte("synthetic authentication bypass"), nil','./internal/vault'),
 ('kdf-floor','internal/vault/envelope.go','p.Memory >= 64*1024','p.Memory >= 1024','./internal/vault'),
 ('chain-id','internal/chains/evm/adapter.go','if x.ChainID != n.ChainID {','if false {','./internal/chains/evm'),
 ('native-confirmation','internal/signer/engine.go','if !e.UI.Confirm(approvalTitle, summary) {','if false && !e.UI.Confirm(approvalTitle, summary) {','./internal/signer'),
 ('changed-recipient','internal/chains/evm/adapter.go','x.To != p.Recipient || x.Value != p.Amount','false || x.Value != p.Amount','./internal/chains/evm'),
 ('foreign-change','internal/chains/decred/decred.go','if e != nil || change != x.ChangeAddress {','if e != nil || len(change)==0 {','./internal/chains/decred'),
 ('locked-signing','internal/signer/engine.go','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif !e.canAuthorize() {','func (e *Engine) Sign(id string) (model.Signed, error) {\n\tif false {','./internal/signer'),
 ('recovery-authorization','internal/signer/engine.go','return e.data != nil && !e.persistFailed && e.data.RecoveryConfirmed','return e.data != nil && !e.persistFailed','./internal/signer'),
 ('recorded-phrase-confirmation','internal/signer/engine.go','if !ui.Recovery("Record recovery phrase — Go signer", phrase) {','if false && !ui.Recovery("Record recovery phrase — Go signer", phrase) {','./internal/signer'),
 ('unlock-recovery-durability','internal/signer/engine.go','if changed {\n\t\t// Recovery transitions must be durable','if false && changed {\n\t\t// Recovery transitions must be durable','./internal/signer'),
 ('nonce-reservation','internal/journal/journal.go','o.Plan.Account.ID == p.Account.ID && !Terminal(o.State)','o.Plan.Account.ID == p.Account.ID && false','./internal/journal ./internal/signer'),
 ('ambiguous-resign','internal/journal/journal.go','s == "CANCELLED" || s == "FAILED"','s == "UNKNOWN" || s == "CANCELLED" || s == "FAILED"','./internal/journal ./internal/signer'),
 ('password-reauthentication','internal/signer/engine.go','!hmac.Equal(in[0], e.password) || p.Expires','false || p.Expires','./internal/signer'),
 ('backup-ready-state-and-reentry','internal/signer/engine.go','if !e.canAuthorize() || e.backupInProgress {','if false {','./internal/signer'),
 ('backup-explicit-confirmation','internal/signer/engine.go','if !accepted {\n\t\treturn nil\n\t}\n\tin, err := e.UI.Input("Confirm app password to back up recovery phrase"','if false && !accepted {\n\t\treturn nil\n\t}\n\tin, err := e.UI.Input("Confirm app password to back up recovery phrase"','./internal/signer'),
 ('backup-app-password-reauthentication','internal/signer/engine.go','if len(in) != 1 || !hmac.Equal(in[0], e.password) {\n\t\treturn ErrDenied\n\t}\n\t// Authentication inputs are no longer needed while the words are displayed.','if len(in) != 1 {\n\t\treturn ErrDenied\n\t}\n\t// Authentication inputs are no longer needed while the words are displayed.','./internal/signer'),
 ('backup-current-generation-binding','internal/signer/engine.go','return e.data == d && e.canAuthorize() && d.Generation == generation','return e.data == d && e.canAuthorize() && (d.Generation == generation || true)','./internal/signer'),
 ('erc20-import-confirmation','internal/signer/tokens.go','if !e.UI.Confirm("Import ERC-20 token", summary) ||','if (false && !e.UI.Confirm("Import ERC-20 token", summary)) ||','./internal/signer'),
 ('erc20-import-password','internal/signer/tokens.go','!hmac.Equal(in[0], e.password)','(false && !hmac.Equal(in[0], e.password))','./internal/signer'),
 ('erc20-runtime-pinning','internal/chains/evm/adapter.go','if crypto.Keccak256Hash(codeBytes).Hex() != expected {','if false && crypto.Keccak256Hash(codeBytes).Hex() != expected {','./internal/chains/evm'),
 ('erc20-false-transfer-result','internal/chains/evm/adapter.go','if s.Contract != "" && !(s.ID == USDTAsset && simulated == "0x") {','if false && s.Contract != "" && !(s.ID == USDTAsset && simulated == "0x") {','./internal/chains/evm'),
 ('exchange-connect-custody-confirmation','internal/signer/exchange.go','if !e.UI.Confirm("Approve Bybit custody and API access",','if false && !e.UI.Confirm("Approve Bybit custody and API access",','./internal/signer'),
 ('exchange-connect-current-generation','internal/signer/exchange.go','data.Generation == generation && data.Exchange == x','(data.Generation == generation || true) && data.Exchange == x','./internal/signer'),
]
# Approval and secret-display guards require an unmodified passing control and their
# exact runtime security assertion. An unrelated failure or panic cannot kill
# these mutants. Other historical mutants keep their existing failure criteria.
backup_checks={
 'password-reauthentication':('^TestTP042CancelledAndExpiredAuthorization$', 'TestTP042CancelledAndExpiredAuthorization', 'wrong reauthentication signed'),
 'native-confirmation':('^TestTP042CancelledAndExpiredAuthorization$', 'TestTP042CancelledAndExpiredAuthorization', 'cancelled confirmation signed'),
 'backup-ready-state-and-reentry':('^TestTP033BackupPhraseRejectsUnavailableAndPendingVaultWithoutPrompt$/^(pending|reentry)$', 'TestTP033BackupPhraseRejectsUnavailableAndPendingVaultWithoutPrompt/pending', 'unavailable backup requested secret input or phrase'),
 'backup-explicit-confirmation':('^TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority$/^cancel-confirm$', 'TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority/cancel-confirm', 'backup cancellation or authentication did not fail safely'),
 'backup-app-password-reauthentication':('^TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority$/^wrong-password$', 'TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority/wrong-password', 'backup cancellation or authentication did not fail safely'),
 'backup-current-generation-binding':('^TestTP043BackupPhraseChecksSessionAtEveryPromptBoundary$/^input$/^generation$', 'TestTP043BackupPhraseChecksSessionAtEveryPromptBoundary/input/generation', 'backup accepted authorization across a changed session'),
 'erc20-import-confirmation':('^TestTokenImportRejectsCancelledWrongPasswordStaleAndLockedRequestsAtomically$/^cancel$', 'TestTokenImportRejectsCancelledWrongPasswordStaleAndLockedRequestsAtomically/cancel', 'cancelled/stale/failed authorization persisted an imported token'),
 'erc20-import-password':('^TestTokenImportRejectsCancelledWrongPasswordStaleAndLockedRequestsAtomically$/^password$', 'TestTokenImportRejectsCancelledWrongPasswordStaleAndLockedRequestsAtomically/password', 'cancelled/stale/failed authorization persisted an imported token'),
 'erc20-runtime-pinning':('^TestTP052USDTRuntimeDecimalsIssuerStateAndNoReturnPolicy$', 'TestTP052USDTRuntimeDecimalsIssuerStateAndNoReturnPolicy', 'wrong USDT runtime accepted'),
 'erc20-false-transfer-result':('^TestTP052USDTRuntimeDecimalsIssuerStateAndNoReturnPolicy$', 'TestTP052USDTRuntimeDecimalsIssuerStateAndNoReturnPolicy', 'false/malformed USDT simulation accepted'),
 'exchange-connect-custody-confirmation':('^TestExchangeConnectRejectsCancelledChangedAndUnavailableSessions$/^cancel-confirm$', 'TestExchangeConnectRejectsCancelledChangedAndUnavailableSessions/cancel-confirm', 'cancelled Bybit custody approval granted connection'),
 'exchange-connect-current-generation':('^TestExchangeConnectRejectsCancelledChangedAndUnavailableSessions$/^generation-input$', 'TestExchangeConnectRejectsCancelledChangedAndUnavailableSessions/generation-input', 'Bybit connection accepted a changed wallet session'),
}
# These guards previously reran all signer tests for every mutant. That suite
# includes many deliberately expensive Argon2/recovery fixtures and exhausted
# the hosted runner's 120-second budget without preserving its completed tests.
# Keep that budget and require the exact invariant instead. Reservation mutants
# must be killed both by the journal rule and by real signing, encrypted journal
# persistence, restart and rejection of a distinct replacement transaction.
runtime_checks={
 'unlock-recovery-durability':[
  ('./internal/signer', '^TestUnlockRecoveryTransitionsRemainDurable$/^SUBMITTING$', 'TestUnlockRecoveryTransitionsRemainDurable/SUBMITTING', 'recovery transitions were not committed as one new encrypted generation'),
  ('./internal/signer', '^TestUnlockRecoveryWriteFailureStillLocksAndPreservesCiphertext$', 'TestUnlockRecoveryWriteFailureStillLocksAndPreservesCiphertext', 'failed recovery write released an unlocked session'),
 ],
 'recovery-authorization':[
  ('./internal/signer', '^TestPreparedMainnetTransferRequiresRecordedPhraseAtEveryAuthorizationBoundary$', 'TestPreparedMainnetTransferRequiresRecordedPhraseAtEveryAuthorizationBoundary/before-sign', 'mainnet signature escaped after verified backup was revoked'),
 ],
 'recorded-phrase-confirmation':[
  ('./internal/signer', '^TestCancelledRecoveryResumesSameDurableRootAndSetupCannotBeRepeated$', 'TestCancelledRecoveryResumesSameDurableRootAndSetupCannotBeRepeated', 'cancelled recovery did not leave pending encrypted root'),
 ],
 'nonce-reservation':[
  ('./internal/journal', '^TestTP060StateAndReservation$', 'TestTP060StateAndReservation', 'unreserved'),
  ('./internal/signer', '^TestNEARStakingSignerExplicitApprovalAndDurableOriginalIdentity$', 'TestNEARStakingSignerExplicitApprovalAndDurableOriginalIdentity', 'ambiguous staking operation lost its nonce/account reservation'),
 ],
 'ambiguous-resign':[
  ('./internal/journal', '^TestTP060AmbiguousIdentityRemainsReserved$', 'TestTP060AmbiguousIdentityRemainsReserved', 'unknown payment allowed new signature'),
  ('./internal/signer', '^TestNEARStakingSignerExplicitApprovalAndDurableOriginalIdentity$', 'TestNEARStakingSignerExplicitApprovalAndDurableOriginalIdentity', 'ambiguous staking operation lost its nonce/account reservation'),
 ],
}

def sources():
    result=go_sources(root)
    for name in ('scripts/mutation.py','scripts/evidence_binding.py'):
        result[name]=hashlib.sha256((root/name).read_bytes()).hexdigest()
    return result

def execute_command(command,copy,timeout=120):
    """Own the complete tool/test process group, including on Python deadlines."""
    started=time.monotonic()
    process=subprocess.Popen(command,cwd=copy,text=True,stdout=subprocess.PIPE,
                             stderr=subprocess.STDOUT,start_new_session=True)
    try:
        output,_=process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        # Killing only `go test` leaves its test binary (and expensive KDF work)
        # alive. It can contend with later mutants and keep the stdout pipe open.
        try:
            os.killpg(process.pid,signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        return {'exit':process.returncode,'elapsedSeconds':round(time.monotonic()-started,3),
                'timedOut':True}, ''
    return {'exit':process.returncode,'elapsedSeconds':round(time.monotonic()-started,3),
            'timedOut':False}, output

def backup_execution(copy,packages,selector,expected,assertion):
    # Leave ten seconds for Go's own deadline to emit diagnostics and terminate
    # children before the unchanged outer 120-second process-group deadline.
    result,output=execute_command(['go','test','-json','-count=1','-timeout=110s',*packages.split(),'-run',selector],copy)
    if result['timedOut']:
        return {**result,'status':'INVALID','reason':'test timed out'}
    try:
        events=[json.loads(line) for line in output.splitlines()]
        if not events or any(not isinstance(event,dict) for event in events):
            raise ValueError('invalid test events')
    except ValueError:
        return {**result,'status':'INVALID','reason':'invalid Go test event stream'}
    passed=[e['Test'] for e in events if e.get('Action')=='pass' and 'Test' in e]
    failed=[e['Test'] for e in events if e.get('Action')=='fail' and 'Test' in e]
    skipped=[e['Test'] for e in events if e.get('Action')=='skip' and 'Test' in e]
    invalid=any(e.get('Action')=='build-fail' or '[build failed]' in e.get('Output','') or 'panic:' in e.get('Output','') for e in events)
    found=any(assertion in e.get('Output','') and e.get('Test')==expected for e in events)
    status='INVALID' if invalid or skipped else 'PASS' if result['exit']==0 and expected in passed else 'KILLED' if result['exit']==1 and expected in failed and found else 'INVALID'
    return {**result,'status':status,'expectedTest':expected,'expectedAssertionObserved':found,'passedTests':passed,'failedTests':failed,'skippedTests':skipped}

def historical_execution(copy,packages):
    result,output=execute_command(['go','test','-count=1','-timeout=110s',*packages.split()],copy)
    if result['timedOut'] or 'panic: test timed out after' in output:
        return {**result,'status':'INVALID','reason':'test timed out'}
    if '[build failed]' in output or '[setup failed]' in output or result['exit']<0:
        return {**result,'status':'INVALID','reason':'compiler, setup or process failure'}
    return {**result,'status':'KILLED' if result['exit'] else 'SURVIVED'}

def run_mutant(copy,mutant):
    name,filename,before,after,packages=mutant
    target=copy/filename;original=target.read_text()
    if original.count(before)!=1:
        return {'id':name,'status':'INVALID','reason':'expected unique source location changed'}
    checks=runtime_checks.get(name,[])
    if name in backup_checks:
        checks=[(packages,*backup_checks[name])]
    controls=[]
    for check_packages,selector,expected,assertion in checks:
        baseline=backup_execution(copy,check_packages,selector,expected,assertion)
        control={'package':check_packages,'selector':selector,'baseline':baseline}
        controls.append(control)
        if baseline['status']!='PASS':
            return {'id':name,'status':'INVALID','reason':'unmodified security control did not pass','controls':controls}
    try:
        target.write_text(original.replace(before,after,1))
        # Removing a funds check may make its local variable unused; keep that
        # public value referenced so this is a semantic mutant, not a syntax test.
        if name=='foreign-change':
            target.write_text(target.read_text().replace('if e != nil {\n\t\treturn errors.New("foreign change destination")','if e != nil || len(change)==0 {\n\t\treturn errors.New("foreign change destination")'))
        if checks:
            for control,(check_packages,selector,expected,assertion) in zip(controls,checks):
                execution=backup_execution(copy,check_packages,selector,expected,assertion)
                if execution['status']=='PASS':
                    execution['status']='SURVIVED'
                control['execution']=execution
            row={'id':name,'source':filename,'mutantSourceSHA256':hashlib.sha256(target.read_bytes()).hexdigest()}
            if name in backup_checks:
                # Preserve the existing report fields for established guards.
                return {**row,'baseline':controls[0]['baseline'],'selector':checks[0][1],**controls[0]['execution']}
            return {**row,'status':'KILLED' if all(c['execution']['status']=='KILLED' for c in controls) else 'INVALID' if any(c['execution']['status']=='INVALID' for c in controls) else 'SURVIVED','controls':controls}
        return {'id':name,**historical_execution(copy,packages)}
    finally:
        target.write_text(original)

def main():
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
    rows=[];start=time.monotonic();source_before=sources()
    with tempfile.TemporaryDirectory(prefix='localwallet-mutants-') as tmp:
        copy=pathlib.Path(tmp)/'source'
        shutil.copytree(root,copy,ignore=shutil.ignore_patterns('.git','node_modules','.tools','dist','bin','reports','wallet-runtime','wallet','.idea','__pycache__'))
        for mutant in selected:
            rows.append(run_mutant(copy,mutant))
            print(mutant[0],rows[-1]['status'],flush=True)
    source_after=sources()
    report={'status':'PASS' if rows and all(r['status']=='KILLED' for r in rows) and source_before==source_after else 'FAIL','scope':'selected' if args.only else 'all','selectedIDs':[m[0] for m in selected],'sourceSHA256':source_before,'sourceSHA256After':source_after,'sourceStable':source_before==source_after,'elapsedSeconds':round(time.monotonic()-start,3),'mutants':rows}
    (root/'reports').mkdir(exist_ok=True);(root/report_path).write_text(json.dumps(report,indent=2)+'\n')
    return report['status']!='PASS'

if __name__=='__main__':
    raise SystemExit(main())
