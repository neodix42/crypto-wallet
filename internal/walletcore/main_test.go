package walletcore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/hypercore"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/supervisor"
)

// This public-only fake child tests coordinator IPC and scheduling. It is never
// built into the wallet; protocol/native-node acceptance has separate tests.
const publicChild = `#!/usr/bin/python3
import sys,struct,json,os,time
folder=sys.argv[sys.argv.index('--data')+1]
statefile=os.path.join(folder,'public-state.json')
while True:
 header=sys.stdin.buffer.read(4)
 if len(header)!=4:break
 size=struct.unpack('>I',header)[0]
 raw=sys.stdin.buffer.read(size)
 if len(raw)!=size:break
 request=json.loads(raw)
 with open(statefile) as f:state=json.load(f)
 method=request['method'];body=request['body'];error=''
 if method==state.get('activityMethod',''):
  activity={'version':1,'id':request['id'],'session':request['session'],'method':'activity','body':{}}
  encoded=json.dumps(activity).encode();sys.stdout.buffer.write(struct.pack('>I',len(encoded))+encoded);sys.stdout.buffer.flush()
 if method==state.get('promptMethod',''):
  prompt={'version':1,'id':request['id'],'session':request['session'],'method':'prompt','body':{'id':'0123456789abcdef0123456789abcdef','kind':'confirm','title':'Public fixture','summary':'Go signer supplied public fixture summary'}}
  encoded=json.dumps(prompt).encode();sys.stdout.buffer.write(struct.pack('>I',len(encoded))+encoded);sys.stdout.buffer.flush()
  header=sys.stdin.buffer.read(4)
  if len(header)!=4:sys.exit(0)
  reply=json.loads(sys.stdin.buffer.read(struct.unpack('>I',header)[0]))
  if reply['id']!=request['id'] or reply['session']!=request['session'] or reply['method']!='promptReply':sys.exit(4)
 if method==state.get('delayMethod',''):
  open(os.path.join(folder,'native-ready'),'w').close()
  while not os.path.exists(os.path.join(folder,'native-release')):time.sleep(0.001)
 if method=='snapshot' and state.get('delaySnapshot',False):
  open(os.path.join(folder,'snapshot-ready'),'w').close()
  while not os.path.exists(os.path.join(folder,'snapshot-release')):time.sleep(0.001)
 state.setdefault('calls',[]).append(method)
 output=None
 if method in state.get('reject',[]):error='SYNTHETIC_DENIED'
 elif method in ['snapshot','accounts','status','confirmRecovery','backupPhrase','accountPage','importToken','importNearToken']:
  if method=='importToken':state.setdefault('tokenPolicies',[]).append(body['token'])
  if method=='importNearToken':state.setdefault('nearTokenPolicies',[]).append(body['token'])
  allaccounts=state.get('accounts',[]);offset=int(body.get('cursor','') or '0');accounts=allaccounts[offset:offset+128]
  output={'locked':state.get('locked',False),'walletExists':state.get('walletExists',True),'vaultState':state.get('vaultState','present'),'environment':'testnet','accounts':accounts,'operations':None,'recoveryConfirmed':True,'revision':str(state.get('revision',1)),'totalAccounts':len(allaccounts),'totalOperations':len(state.get('ops',[])),'accountsCursor':str(offset+len(accounts)) if offset+len(accounts)<len(allaccounts) else ''}
 elif method=='pending':output=state.get('ops',[])
 elif method=='operation':output=next((op for op in state.get('ops',[]) if op['plan']['id']==body['id']),None)
 elif method=='account':output=next((ac for ac in state.get('accounts',[]) if ac['id']==body['accountId']),None)
 elif method=='journalPage':
  ops=[dict(op) for op in reversed(state.get('ops',[])) if not body['accountId'] or op['plan']['account']['id']==body['accountId'] and op['plan']['asset']==body['asset']]
  for op in ops:op.pop('signed',None)
  offset=int(body['cursor'] or '0');page=ops[offset:offset+128]
  output={'operations':page,'nextCursor':str(offset+len(page)) if offset+len(page)<len(ops) else '', 'complete':offset+len(page)==len(ops),'revision':str(state.get('revision',1)),'total':len(ops)}
  output.update(state.get('journalOverride',{}))
 elif method=='settings':output=state.get('settings',{})
 elif method=='tokenPolicies':output=state.get('tokenPolicies',[])
 elif method=='nearTokenPolicies':output=state.get('nearTokenPolicies',[])
 elif method=='prepare':
  state.setdefault('ops',[]).append({'plan':body,'state':'PREPARED','digest':'public-test'})
  output={}
 elif method=='sign':
  output={'identity':'synthetic-identity','bytes':'AQ=='}
  for operation in state.get('ops',[]):
   if operation['plan']['id']==body['id']:operation['signed']=output;operation['state']='SIGNED_NOT_SUBMITTED'
 elif method=='update':
  for operation in state.get('ops',[]):
   if operation['plan']['id']==body['id']:operation['state']=body['state'];operation['receipt']=body.get('receipt')
 else:output={'locked':False}
 with open(statefile,'w') as f:json.dump(state,f)
 response={'version':1,'id':request['id'],'session':request['session'],'method':method,'body':output}
 if error:response['error']=error
 encoded=json.dumps(response).encode()
 sys.stdout.buffer.write(struct.pack('>I',len(encoded))+encoded);sys.stdout.buffer.flush()
`

type publicAdapter struct {
	mu                                                      sync.Mutex
	prepares, submits, reconciles                           int
	prepareError, balanceError, submitError, reconcileError bool
	receiptState                                            string
	lastTransfer                                            model.Transfer
	beforeReconcile                                         func()
	networks                                                []model.Network
}

func (a *publicAdapter) Networks() []model.Network {
	if a.networks != nil {
		return a.networks
	}
	return []model.Network{{ID: "unit-testnet", Family: "unit", Testnet: true, Assets: []model.Asset{{ID: "unit-testnet:native", Decimals: 2}}}, {ID: "unit-mainnet", Family: "unit", Testnet: false, Assets: []model.Asset{{ID: "unit-mainnet:native", Decimals: 2}}}}
}
func (a *publicAdapter) Derive([]byte, uint32, string) (model.Account, error) {
	return model.Account{}, errors.New("synthetic public adapter cannot derive")
}
func (a *publicAdapter) ValidateAddress(string, string) error { return nil }
func (a *publicAdapter) Balance(context.Context, model.Account) ([]model.Balance, error) {
	if a.balanceError {
		return nil, errors.New("synthetic unavailable")
	}
	return []model.Balance{{Asset: "unit-testnet:native", Atomic: "1000", State: "PUBLIC_TEST"}}, nil
}
func (a *publicAdapter) Prepare(_ context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prepares++
	a.lastTransfer = t
	if a.prepareError {
		return model.Plan{}, errors.New("synthetic preparation rejected")
	}
	return model.Plan{Account: ac, Asset: t.Asset, Recipient: t.Recipient, Amount: t.Amount, MaxFee: "1", Nonce: "42", Expires: time.Now().Unix() + 60}, nil
}
func (a *publicAdapter) ValidatePlan(model.Plan) error { return nil }
func (a *publicAdapter) Sign([]byte, model.Plan) (model.Signed, error) {
	return model.Signed{}, errors.New("synthetic public adapter has no signer")
}
func (a *publicAdapter) Submit(_ context.Context, _ model.Plan, s model.Signed) (model.Receipt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.submits++
	if a.submitError {
		return model.Receipt{}, errors.New("synthetic timeout after acceptance")
	}
	state := a.receiptState
	if state == "" {
		state = "SUBMITTED"
	}
	return model.Receipt{Identity: s.Identity, State: state}, nil
}
func (a *publicAdapter) Reconcile(_ context.Context, _ model.Plan, s model.Signed) (model.Receipt, error) {
	a.mu.Lock()
	a.reconciles++
	hook := a.beforeReconcile
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	if a.reconcileError {
		return model.Receipt{}, errors.New("synthetic outage")
	}
	state := a.receiptState
	if state == "" {
		state = "UNKNOWN"
	}
	return model.Receipt{Identity: s.Identity, State: state}, nil
}
func publicAccount() model.Account {
	return model.Account{ID: "public-account", Family: "unit", Profile: "public-test-only", Network: "unit-testnet", Address: "public-recipient", PublicKey: "public-key"}
}
func publicPlan() model.Plan {
	return model.Plan{ID: "public-operation", Account: publicAccount(), Asset: "unit-testnet:native", Recipient: "public-destination", Amount: "125", Nonce: "42", MaxFee: "1", Expires: time.Now().Unix() + 60}
}
func fakeCoordinator(t *testing.T, adapter *publicAdapter, op *journal.Operation) (*core, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wallet-signer")
	if err := os.WriteFile(path, []byte(publicChild), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(publicChild))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(sum[:])})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{"accounts": []model.Account{publicAccount()}, "ops": []journal.Operation{}}
	if op != nil {
		state["ops"] = []journal.Operation{*op}
	}
	statePath := filepath.Join(dir, "public-state.json")
	writePublicState(t, statePath, state)
	c := &core{child: &supervisor.Child{Path: path, Data: dir, Environment: "testnet"}, adapters: []model.Adapter{adapter}, last: time.Now(), active: true}
	t.Cleanup(c.lock)
	return c, statePath
}
func writePublicState(t *testing.T, path string, state map[string]any) {
	t.Helper()
	b, e := json.Marshal(state)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func readPublicState(t *testing.T, path string) map[string]any {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var state map[string]any
	if e = json.Unmarshal(b, &state); e != nil {
		t.Fatal(e)
	}
	return state
}
func message(method string, body any) ipc.Message {
	b, _ := json.Marshal(body)
	return ipc.Message{Version: 1, ID: "public-request", Session: "public-test", Method: method, Body: b}
}
func TestTP041PublicHandlerSchemas(t *testing.T) {
	a := &publicAdapter{}
	c, path := fakeCoordinator(t, a, nil)
	for _, method := range []string{"balances", "prepare", "send", "retry", "reconcile", "associate", "confirmRecovery", "backupPhrase"} {
		if _, e := c.handle(message(method, map[string]any{"password": "untrusted"})); e == nil {
			t.Fatalf("secret/unknown field accepted by %s", method)
		}
	}
	if _, e := c.handle(message("SignBytes", map[string]any{})); e == nil {
		t.Fatal("raw signing endpoint")
	}
	if _, e := c.adapter("unknown"); e == nil {
		t.Fatal("unknown adapter")
	}
	for _, method := range []string{"create", "import", "unlock", "confirmRecovery", "backupPhrase", "password", "addAccount", "receive", "copyReceive", "saveQR", "scanQR", "configure"} {
		if _, e := c.handle(message(method, map[string]any{})); e != nil {
			t.Fatalf("forward %s %v", method, e)
		}
	}
	for _, method := range []string{"accounts", "status", "history"} {
		if _, e := c.handle(message(method, map[string]any{})); e != nil {
			t.Fatal(e)
		}
	}
	n, e := c.handle(message("networks", struct{}{}))
	if e != nil || len(n.([]model.Network)) != 1 {
		t.Fatal("profile network filtering")
	}
	if _, e := c.account("unknown"); e == nil {
		t.Fatal("unknown account")
	}
	state := readPublicState(t, path)
	state["locked"] = true
	writePublicState(t, path, state)
	if _, e := c.account(publicAccount().ID); e == nil {
		t.Fatal("locked account")
	}
}
func TestTP052PrepareUsesAtomicPrecisionAndState(t *testing.T) {
	a := &publicAdapter{}
	c, path := fakeCoordinator(t, a, nil)
	request := model.Transfer{AccountID: publicAccount().ID, Asset: "unit-testnet:native", Recipient: "public-destination", Amount: "1.25"}
	out, e := c.handle(message("prepare", request))
	if e != nil {
		t.Fatal(e)
	}
	p := out.(model.Plan)
	if p.Amount != "125" || p.ID == "" || a.lastTransfer.Amount != "125" {
		t.Fatal("atomic precision or operation ID")
	}
	state := readPublicState(t, path)
	ops := state["ops"].([]any)
	if len(ops) != 1 {
		t.Fatal("plan not journaled before return")
	}
	for _, bad := range []model.Transfer{{AccountID: request.AccountID, Asset: request.Asset, Recipient: request.Recipient, Amount: "1.001"}, {AccountID: request.AccountID, Asset: "foreign", Recipient: request.Recipient, Amount: "1"}, {AccountID: "unknown", Asset: request.Asset, Recipient: request.Recipient, Amount: "1"}} {
		if _, e := c.handle(message("prepare", bad)); e == nil {
			t.Fatal("invalid transfer")
		}
	}
	a.prepareError = true
	if _, e := c.handle(message("prepare", request)); e == nil {
		t.Fatal("failed preparation")
	}
	a.prepareError = false
	state = readPublicState(t, path)
	state["reject"] = []string{"prepare"}
	writePublicState(t, path, state)
	if _, e := c.handle(message("prepare", request)); e == nil {
		t.Fatal("signer refused preparation")
	}
	if _, e := c.handle(message("balances", map[string]string{"accountId": request.AccountID})); e != nil {
		t.Fatal(e)
	}
	a.balanceError = true
	if _, e := c.handle(message("balances", map[string]string{"accountId": request.AccountID})); e == nil {
		t.Fatal("unavailable balance became zero")
	}
}
func TestTP060PaymentSubmissionUncertainty(t *testing.T) {
	for _, tc := range []struct {
		name, state, method, receipt string
		submitError, reconcileError  bool
		want                         string
		submits, reconciles          int
	}{{"new", "PREPARED", "send", "CONFIRMED", false, false, "CONFIRMED", 1, 0}, {"accepted-timeout", "PREPARED", "send", "", true, false, "UNKNOWN", 1, 0}, {"retry-resolved", "UNKNOWN", "retry", "CONFIRMED", false, false, "CONFIRMED", 0, 1}, {"retry-original", "UNKNOWN", "retry", "UNKNOWN", false, false, "UNKNOWN", 1, 1}, {"retry-query-outage", "UNKNOWN", "retry", "SUBMITTED", false, true, "SUBMITTED", 1, 1}, {"reconcile-query-outage", "SUBMITTED", "reconcile", "", false, true, "UNKNOWN", 0, 1}, {"reconcile-failed", "SUBMITTED", "reconcile", "FAILED", false, false, "FAILED", 0, 1}, {"unknown-status", "PREPARED", "send", "INVENTED", false, false, "UNKNOWN", 1, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			p := publicPlan()
			op := journal.Operation{Plan: p, State: tc.state}
			if tc.state != "PREPARED" {
				op.Signed = &model.Signed{Identity: "synthetic-identity", Bytes: []byte{1}}
			}
			a := &publicAdapter{receiptState: tc.receipt, submitError: tc.submitError, reconcileError: tc.reconcileError}
			c, path := fakeCoordinator(t, a, &op)
			out, e := c.payment(tc.method, p.ID)
			if e != nil {
				t.Fatal(e)
			}
			r := out.(model.Receipt)
			if r.State != tc.want || a.submits != tc.submits || a.reconciles != tc.reconciles {
				t.Fatalf("state %s submits %d reconciles %d", r.State, a.submits, a.reconciles)
			}
			calls := readPublicState(t, path)["calls"].([]any)
			signatures := 0
			for _, call := range calls {
				if call == "sign" {
					signatures++
				}
			}
			if tc.method != "send" && signatures != 0 {
				t.Fatal("recovery re-signed payment")
			}
			if tc.method == "send" && signatures != 1 {
				t.Fatal("send signature count")
			}
		})
	}
}
func TestTP060PaymentsRejectConflictsAndLostSigner(t *testing.T) {
	for _, tc := range []struct {
		state, method string
		signed        bool
	}{{"CONFIRMED", "send", true}, {"SUBMITTED", "send", true}, {"PREPARED", "retry", false}, {"SUBMITTED", "retry", true}} {
		p := publicPlan()
		op := journal.Operation{Plan: p, State: tc.state}
		if tc.signed {
			op.Signed = &model.Signed{Identity: "synthetic-identity", Bytes: []byte{1}}
		}
		c, _ := fakeCoordinator(t, &publicAdapter{}, &op)
		if _, e := c.payment(tc.method, p.ID); e == nil {
			t.Fatal("invalid payment lifecycle")
		}
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	if _, e := c.payment("send", "unknown"); e == nil {
		t.Fatal("unknown payment")
	}
	p := publicPlan()
	state := readPublicState(t, path)
	state["ops"] = []journal.Operation{{Plan: p, State: "PREPARED"}}
	state["reject"] = []string{"sign"}
	writePublicState(t, path, state)
	if _, e := c.payment("send", p.ID); e == nil {
		t.Fatal("signer denied signature")
	}
	state = readPublicState(t, path)
	state["reject"] = []string{"operation"}
	writePublicState(t, path, state)
	if _, e := c.payment("send", p.ID); e == nil {
		t.Fatal("missing pending state")
	}
}
func TestTP043LockInvalidatesRetryBeforeBroadcast(t *testing.T) {
	p := publicPlan()
	op := journal.Operation{Plan: p, State: "UNKNOWN", Signed: &model.Signed{Identity: "synthetic-identity", Bytes: []byte{1}}}
	a := &publicAdapter{}
	c, _ := fakeCoordinator(t, a, &op)
	a.beforeReconcile = c.lock
	if _, e := c.payment("retry", p.ID); e == nil {
		t.Fatal("locked retry allowed")
	}
	if a.submits != 0 {
		t.Fatal("broadcast after lock")
	}
}
func TestTP041PrivateNetworkSettingsAndAssociationBounds(t *testing.T) {
	a := &publicAdapter{}
	c, path := fakeCoordinator(t, a, nil)
	for _, req := range []map[string]string{{"accountId": "missing", "address": "public"}, {"accountId": publicAccount().ID, "address": "public"}} {
		if _, e := c.handle(message("associate", req)); e == nil {
			t.Fatal("unavailable association")
		}
	}
	state := readPublicState(t, path)
	state["settings"] = map[string]any{"unknown": map[string]string{"url": "https://example.org"}}
	writePublicState(t, path, state)
	if c.applySettings() == nil {
		t.Fatal("unknown network settings")
	}
	c.adapters = []model.Adapter{near.New(), aptos.New()}
	state = readPublicState(t, path)
	state["settings"] = map[string]any{"near-testnet": map[string]string{"url": "http://127.0.0.1:3030"}, "aptos-testnet": map[string]string{"url": "http://127.0.0.1:8080/v1"}}
	writePublicState(t, path, state)
	if e := c.applySettings(); e != nil {
		t.Fatal(e)
	}
	for _, ad := range c.adapters {
		for _, network := range ad.Networks() {
			if strings.HasSuffix(network.ID, "-testnet") && !strings.HasPrefix(network.RPC, "http://127.0.0.1:") {
				t.Fatal("approved local endpoint not applied")
			}
		}
	}
}

// Run the real inherited-pipe event loop with a public-only child. No native
// credential bypass exists in production: this executable is generated by tests.
func TestTP041CoreMainInheritedPipes(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binDir := filepath.Dir(executable)
	childPath := filepath.Join(binDir, "wallet-signer")
	manifestPath := filepath.Join(binDir, "manifest.json")
	if _, e = os.Stat(childPath); !os.IsNotExist(e) {
		t.Fatal("refusing to overwrite test executable directory")
	}
	if e = os.WriteFile(childPath, []byte(publicChild), 0700); e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256([]byte(publicChild))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(sum[:])})
	if e = os.WriteFile(manifestPath, manifest, 0600); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Remove(childPath); os.Remove(manifestPath) })
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	profile := filepath.Join(config, "cryptowallet", "testnet")
	if e = os.MkdirAll(profile, 0700); e != nil {
		t.Fatal(e)
	}
	writePublicState(t, filepath.Join(profile, "public-state.json"), map[string]any{"accounts": []model.Account{publicAccount()}, "ops": []journal.Operation{}})
	inputRead, inputWrite, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	outputRead, outputWrite, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	oldIn, oldOut, oldArgs, oldFlags := os.Stdin, os.Stdout, os.Args, flag.CommandLine
	os.Stdin = inputRead
	os.Stdout = outputWrite
	os.Args = []string{executable, "--environment", "testnet"}
	flag.CommandLine = flag.NewFlagSet("public-core-test", flag.ContinueOnError)
	defer func() {
		os.Stdin = oldIn
		os.Stdout = oldOut
		os.Args = oldArgs
		flag.CommandLine = oldFlags
		inputRead.Close()
		inputWrite.Close()
		outputRead.Close()
		outputWrite.Close()
	}()
	finished := make(chan struct{})
	go func() { main(); close(finished) }()
	ch := ipc.Channel{R: outputRead, W: inputWrite}
	for i, method := range []string{"networks", "accounts", "activity", "lock", "unsupported"} {
		request := message(method, map[string]any{})
		request.ID = fmt.Sprint(i)
		request.Session = strings.Repeat("a", 32)
		if e = ch.Write(request); e != nil {
			t.Fatal(e)
		}
		response, e := ch.Read()
		if e != nil {
			t.Fatal(e)
		}
		if response.ID != request.ID || response.Method != method {
			t.Fatal("response identity binding")
		}
		if method == "unsupported" && response.Error == "" {
			t.Fatal("unknown action accepted")
		}
	}
	// The worker is blocked in a signer prompt, while activity and its answer
	// travel through the reader fast paths without opening another operation.
	promptStatePath := filepath.Join(profile, "public-state.json")
	promptState := readPublicState(t, promptStatePath)
	promptState["promptMethod"] = "confirmRecovery"
	writePublicState(t, promptStatePath, promptState)
	promptRequest := message("confirmRecovery", struct{}{})
	promptRequest.ID = "prompt-original"
	promptRequest.Session = strings.Repeat("a", 32)
	if e = ch.Write(promptRequest); e != nil {
		t.Fatal(e)
	}
	promptEvent, e := ch.Read()
	if e != nil || promptEvent.Method != "prompt" || promptEvent.ID != promptRequest.ID || !bytes.Contains(promptEvent.Body, []byte("Go signer supplied public fixture summary")) {
		t.Fatal("signer prompt not relayed", e)
	}
	activityRequest := message("activity", struct{}{})
	activityRequest.ID = "prompt-activity"
	activityRequest.Session = promptRequest.Session
	if e = ch.Write(activityRequest); e != nil {
		t.Fatal(e)
	}
	if ack, e := ch.Read(); e != nil || ack.ID != activityRequest.ID || ack.Method != "activity" {
		t.Fatal("prompt blocked real activity", e)
	}
	answer := ipc.Message{Version: 1, ID: promptRequest.ID, Session: promptRequest.Session, Method: "promptReply", Body: []byte(`{"promptId":"0123456789abcdef0123456789abcdef","accepted":true}`)}
	if e = ch.Write(answer); e != nil {
		t.Fatal(e)
	}
	if final, e := ch.Read(); e != nil || final.ID != promptRequest.ID || final.Method != "confirmRecovery" || final.Error != "" {
		t.Fatal("prompt answer did not complete original operation", e)
	}
	promptState = readPublicState(t, promptStatePath)
	delete(promptState, "promptMethod")
	writePublicState(t, promptStatePath, promptState)
	for i, malformed := range []struct{ method, body string }{
		{"activity", "null"}, {"lock", "null"}, {"networks", "null"},
		{"activity", `{"input":"public forbidden field"}`}, {"lock", `{"unexpected":true}`},
	} {
		request := message(malformed.method, struct{}{})
		request.ID, request.Session, request.Body = fmt.Sprint(100+i), strings.Repeat("a", 32), []byte(malformed.body)
		if e = ch.Write(request); e != nil {
			t.Fatal(e)
		}
		response, err := ch.Read()
		if err != nil || response.ID != request.ID || response.Error != "INVALID_ARGUMENT" {
			t.Fatal("invalid fast-path body accepted", malformed.method, err, response)
		}
	}
	// Hold the worker in an earlier snapshot, queue an unlock, then exercise
	// the independent lock fast path. The queued old-session unlock must never
	// start a new child or reopen a native input prompt after the lock.
	statePath := filepath.Join(profile, "public-state.json")
	state := readPublicState(t, statePath)
	state["delaySnapshot"] = true
	writePublicState(t, statePath, state)
	request := message("accounts", struct{}{})
	request.ID, request.Session = "queued-working", strings.Repeat("a", 32)
	if e = ch.Write(request); e != nil {
		t.Fatal(e)
	}
	waitPublicFixture(t, filepath.Join(profile, "snapshot-ready"))
	for _, queued := range []struct{ id, method string }{{"queued-stale", "unlock"}, {"queued-lock", "lock"}} {
		request := message(queued.method, struct{}{})
		request.ID, request.Session = queued.id, strings.Repeat("a", 32)
		if e = ch.Write(request); e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	for range 3 {
		response, err := ch.Read()
		if err != nil || seen[response.ID] {
			t.Fatal("queued response framing", err, response)
		}
		seen[response.ID] = true
		if (response.ID == "queued-lock" && response.Error != "") || (response.ID != "queued-lock" && response.Error == "") {
			t.Fatal("stale queued operation survived lock", response)
		}
	}
	state = readPublicState(t, statePath)
	for _, call := range state["calls"].([]any) {
		if call == "unlock" {
			t.Fatal("queued old-session unlock reached the child")
		}
	}
	inputWrite.Close()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("EOF did not stop core")
	}
}
func TestTP041AllEndpointFamiliesFailClosed(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	real := func() []model.Adapter {
		return []model.Adapter{evm.New(), hypercore.New(), near.New(), aptos.New(), decred.New(), ton.New()}
	}
	c.adapters = real()
	ids := []string{"ethereum-sepolia", "hypercore-testnet", "near-testnet", "aptos-testnet", "decred-testnet3", "ton-testnet"}
	settings := map[string]any{}
	for _, id := range ids {
		settings[id] = map[string]string{"url": "https://example.org/rpc", "user": "public-test", "password": "public-test-only", "apiKey": "public-api-key"}
	}
	state := readPublicState(t, path)
	state["settings"] = settings
	writePublicState(t, path, state)
	if e := c.applySettings(); e != nil {
		t.Fatal(e)
	}
	for _, id := range ids {
		c.adapters = real()
		bad := map[string]any{id: map[string]string{"url": "http://remote.invalid", "user": "public-test", "password": "public-test-only"}}
		state = readPublicState(t, path)
		state["settings"] = bad
		writePublicState(t, path, state)
		if c.applySettings() == nil {
			t.Fatalf("unsafe endpoint accepted for %s", id)
		}
	}
	state = readPublicState(t, path)
	state["reject"] = []string{"settings"}
	writePublicState(t, path, state)
	if c.applySettings() == nil {
		t.Fatal("unavailable encrypted settings")
	}
}
func TestTP060SignerBoundaryAndUnsupportedState(t *testing.T) {
	p := publicPlan()
	op := journal.Operation{Plan: p, State: "PREPARED"}
	a := &publicAdapter{}
	c, path := fakeCoordinator(t, a, &op)
	if _, e := c.handle(message("send", map[string]string{"id": p.ID})); e != nil {
		t.Fatal(e)
	}
	state := readPublicState(t, path)
	state["reject"] = []string{"confirmRecovery"}
	writePublicState(t, path, state)
	if _, e := c.handle(message("confirmRecovery", map[string]any{})); e == nil {
		t.Fatal("forwarded rejection")
	}
	state = readPublicState(t, path)
	state["reject"] = []string{"settings"}
	writePublicState(t, path, state)
	if _, e := c.handle(message("unlock", map[string]any{})); e == nil {
		t.Fatal("settings restoration failure ignored")
	}
	state = readPublicState(t, path)
	state["reject"] = []string{}
	unknown := publicAccount()
	unknown.Network = "unsupported"
	state["accounts"] = []model.Account{unknown}
	writePublicState(t, path, state)
	for _, method := range []string{"balances", "prepare", "associate"} {
		body := map[string]string{"accountId": unknown.ID}
		if method == "prepare" {
			body["asset"] = "unit-testnet:native"
			body["recipient"] = "public"
			body["amount"] = "1"
		}
		if _, e := c.handle(message(method, body)); e == nil {
			t.Fatal("unsupported account network")
		}
	}
	state = readPublicState(t, path)
	p.Account = unknown
	state["ops"] = []journal.Operation{{Plan: p, State: "UNKNOWN", Signed: &model.Signed{Identity: "synthetic-identity", Bytes: []byte{1}}}}
	writePublicState(t, path, state)
	if _, e := c.payment("retry", p.ID); e == nil {
		t.Fatal("unsupported operation network")
	}
}
func TestTP060NoBroadcastWhenDurableSubmittingRejected(t *testing.T) {
	p := publicPlan()
	op := journal.Operation{Plan: p, State: "PREPARED"}
	a := &publicAdapter{}
	c, path := fakeCoordinator(t, a, &op)
	state := readPublicState(t, path)
	state["reject"] = []string{"update"}
	writePublicState(t, path, state)
	if _, e := c.payment("send", p.ID); e == nil {
		t.Fatal("durable submission marker rejection ignored")
	}
	if a.submits != 0 {
		t.Fatal("broadcast preceded durable SUBMITTING marker")
	}
}

type maxAdapter struct {
	*publicAdapter
	balances []model.Balance
	feeAsset string
	fee      func(int) string
	payload  json.RawMessage
}

func (a *maxAdapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	if a.balanceError {
		return nil, errors.New("synthetic unavailable balance")
	}
	return a.balances, nil
}
func (a *maxAdapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	p, e := a.publicAdapter.Prepare(ctx, ac, t)
	if e != nil {
		return p, e
	}
	p.FeeAsset = a.feeAsset
	if a.payload != nil {
		p.Payload = a.payload
	}
	if a.fee != nil {
		p.MaxFee = a.fee(a.prepares)
	}
	return p, nil
}
func TestTP052MaxExactIntegerAndFeeReserve(t *testing.T) {
	for _, tc := range []struct {
		name, balance, feeAsset    string
		fee                        func(int) string
		balanceError, prepareError bool
		want                       string
	}{{"native", "1000", "unit-testnet:native", func(int) string { return "1" }, false, false, "999"}, {"separate-gas", "1000", "gas-asset", func(int) string { return "999" }, false, false, "1000"}, {"tiny", "1", "unit-testnet:native", func(int) string { return "0" }, false, false, "1"}, {"malformed-balance", "00", "unit-testnet:native", nil, false, false, ""}, {"zero", "0", "unit-testnet:native", nil, false, false, ""}, {"unknown", "", "unit-testnet:native", nil, false, false, ""}, {"balance-outage", "1000", "unit-testnet:native", nil, true, false, ""}, {"prepare-outage", "1000", "unit-testnet:native", nil, false, true, ""}, {"invalid-fee", "1000", "unit-testnet:native", func(int) string { return "bad" }, false, false, ""}, {"insufficient", "1", "unit-testnet:native", func(int) string { return "2" }, false, false, ""}, {"unstable", "1000", "unit-testnet:native", func(i int) string { return fmt.Sprint(100 + i) }, false, false, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			base := &publicAdapter{balanceError: tc.balanceError, prepareError: tc.prepareError}
			a := &maxAdapter{publicAdapter: base, feeAsset: tc.feeAsset, fee: tc.fee}
			if tc.balance != "" {
				a.balances = []model.Balance{{Asset: "unit-testnet:native", Atomic: tc.balance}}
			}
			p, e := maxPlan(context.Background(), a, publicAccount(), model.Transfer{AccountID: publicAccount().ID, Asset: "unit-testnet:native", Recipient: "public"})
			if tc.want != "" {
				if e != nil || p.Amount != tc.want {
					t.Fatalf("amount %s error %v", p.Amount, e)
				}
			} else if e == nil {
				t.Fatal("unknown or unsafe maximum accepted")
			}
		})
	}
	base := &publicAdapter{}
	a := &maxAdapter{publicAdapter: base, balances: []model.Balance{{Asset: "unit-testnet:native", Atomic: "1000"}}, feeAsset: "unit-testnet:native", fee: func(int) string { return "1" }}
	c, _ := fakeCoordinator(t, base, nil)
	c.adapters = []model.Adapter{a}
	out, e := c.handle(message("max", model.Transfer{AccountID: publicAccount().ID, Asset: "unit-testnet:native", Recipient: "public"}))
	if e != nil || out.(model.Plan).Amount != "999" {
		t.Fatalf("public max %v %v", out, e)
	}
}

type publicHistoryAdapter struct {
	*publicAdapter
	failed bool
}

func (a *publicHistoryAdapter) History(context.Context, model.Account) ([]model.Receipt, error) {
	if a.failed {
		return nil, errors.New("synthetic incomplete history")
	}
	return []model.Receipt{{Identity: "public-incoming", State: "CONFIRMED"}}, nil
}
func (a *publicHistoryAdapter) HistoryScope() string { return "PUBLIC SYNTHETIC bounded history only" }
func TestTP051HistoryScopeAndFailures(t *testing.T) {
	base := &publicAdapter{}
	c, path := fakeCoordinator(t, base, nil)
	for _, method := range []string{"networks", "accounts", "status", "history", "chainHistory"} {
		if _, e := c.handle(message(method, map[string]any{"password": "forbidden"})); e == nil {
			t.Fatal("unknown Go IPC fields accepted")
		}
	}
	out, e := c.handle(message("chainHistory", map[string]string{"accountId": publicAccount().ID}))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(out)
	if !bytes.Contains(b, []byte("not implemented")) {
		t.Fatal("incomplete incoming history hidden")
	}
	history := &publicHistoryAdapter{publicAdapter: base}
	c.adapters = []model.Adapter{history}
	out, e = c.handle(message("chainHistory", map[string]string{"accountId": publicAccount().ID}))
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(out)
	if !bytes.Contains(b, []byte("public-incoming")) || !bytes.Contains(b, []byte(history.HistoryScope())) {
		t.Fatal("history omitted scope")
	}
	history.failed = true
	if _, e = c.handle(message("chainHistory", map[string]string{"accountId": publicAccount().ID})); e == nil {
		t.Fatal("unavailable history became empty success")
	}
	state := readPublicState(t, path)
	dcrAccount := publicAccount()
	dcrAccount.Family = "decred"
	dcrAccount.Network = "decred-simnet"
	state["accounts"] = []model.Account{dcrAccount}
	writePublicState(t, path, state)
	c.adapters = []model.Adapter{decred.New()}
	if _, e = c.handle(message("chainHistory", map[string]string{"accountId": dcrAccount.ID})); e == nil {
		t.Fatal("invalid Decred discovery descriptor")
	}
	if _, e = c.handle(message("chainHistory", map[string]string{"accountId": "missing"})); e == nil {
		t.Fatal("unknown history account")
	}
	dcrAccount.Network = "unsupported"
	state = readPublicState(t, path)
	state["accounts"] = []model.Account{dcrAccount}
	writePublicState(t, path, state)
	if _, e = c.handle(message("chainHistory", map[string]string{"accountId": dcrAccount.ID})); e == nil {
		t.Fatal("unsupported history network")
	}
}

type cancellableAdapter struct {
	*publicAdapter
	started chan struct{}
}

func (a *cancellableAdapter) Balance(ctx context.Context, _ model.Account) ([]model.Balance, error) {
	close(a.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestTP043LockCancelsPublicQueries(t *testing.T) {
	base := &publicAdapter{}
	c, _ := fakeCoordinator(t, base, nil)
	a := &cancellableAdapter{publicAdapter: base, started: make(chan struct{})}
	c.adapters = []model.Adapter{a}
	done := make(chan error, 1)
	go func() {
		_, e := c.handle(message("balances", map[string]string{"accountId": publicAccount().ID}))
		done <- e
	}()
	select {
	case <-a.started:
	case <-time.After(5 * time.Second):
		t.Fatal("query did not start")
	}
	c.lock()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled query returned balance")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock did not cancel network context")
	}
}

func TestTP043PostLockContextCannotRestartQueries(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	ctx, cancel := c.networkContext(time.Minute)
	defer cancel()
	c.lock()
	if ctx.Err() != context.Canceled {
		t.Fatal("prior session context survived lock")
	}
	late, stop := c.networkContext(time.Minute)
	defer stop()
	if late.Err() != context.Canceled {
		t.Fatal("stale handler created a fresh post-lock network context")
	}
}
func TestTP043StaleSnapshotCannotReactivateSession(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	state["delaySnapshot"] = true
	writePublicState(t, path, state)
	done := make(chan error, 1)
	go func() {
		s, e := c.current()
		if e == nil && !s.Locked {
			done <- errors.New("stale unlocked snapshot accepted")
			return
		}
		done <- nil
	}()
	dir := filepath.Dir(path)
	ready := false
	for i := 0; i < 500; i++ {
		if _, e := os.Stat(filepath.Join(dir, "snapshot-ready")); e == nil {
			ready = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !ready {
		t.Fatal("snapshot fixture did not reach response boundary")
	} // Reproduce the lock epoch changing after child.Call began and before its result is accepted.
	c.mu.Lock()
	c.epoch++
	c.active = false
	if c.sessionCancel != nil {
		c.sessionCancel()
	}
	if e := os.WriteFile(filepath.Join(dir, "snapshot-release"), nil, 0600); e != nil {
		c.mu.Unlock()
		t.Fatal(e)
	}
	c.mu.Unlock()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale snapshot did not resolve")
	}
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	if active {
		t.Fatal("stale snapshot reactivated the locked session")
	}
}
func TestTP052NativeMaxIncludesStorageReserve(t *testing.T) {
	for _, family := range []string{"near", "ton"} {
		for _, mode := range []string{"valid", "malformed", "invalid-reserve", "insufficient"} {
			t.Run(family+"-"+mode, func(t *testing.T) {
				ac := publicAccount()
				ac.Family = family
				reserve := "20"
				if mode == "invalid-reserve" {
					reserve = "-1"
				}
				if mode == "insufficient" {
					reserve = "1000"
				}
				payload, _ := json.Marshal(map[string]string{"storageReserve": reserve})
				if mode == "malformed" {
					payload = []byte(`{`)
				}
				a := &maxAdapter{publicAdapter: &publicAdapter{}, balances: []model.Balance{{Asset: "unit-testnet:native", Atomic: "1000"}}, feeAsset: "unit-testnet:native", fee: func(int) string { return "10" }, payload: payload}
				p, e := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: "unit-testnet:native", Recipient: "public"})
				if mode == "valid" {
					if e != nil || p.Amount != "970" {
						t.Fatalf("maximum %s error %v", p.Amount, e)
					}
				} else if e == nil {
					t.Fatal("unsafe native storage reserve")
				}
			})
		}
	}
}
func TestTP040PublicJSONPreservesAtomicStringsAndRedactsRPCCredentials(t *testing.T) {
	value := map[string]any{"operations": []any{map[string]any{"nonce": "18446744073709551615", "amount": "340282366920938463463374607431768211455", "payload": json.RawMessage(`{"nonce":18446744073709551615,"amount":"123"}`)}}, "rpc": "https://public-user:PUBLIC-NODE-PASSWORD@example.org/v1/PUBLIC-PATH-API-KEY?apiKey=PUBLIC-API-KEY#credential", "nested": map[string]any{"rpc": 42}}
	b, e := publicJSON(value)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte("PUBLIC-NODE-PASSWORD")) || bytes.Contains(b, []byte("PUBLIC-API-KEY")) || bytes.Contains(b, []byte("public-user")) || bytes.Contains(b, []byte("PUBLIC-PATH-API-KEY")) {
		t.Fatal("node credential in public IPC")
	}
	var decoded struct {
		Operations []struct {
			Nonce   string `json:"nonce"`
			Amount  string `json:"amount"`
			Payload string `json:"payload"`
		} `json:"operations"`
		RPC string `json:"rpc"`
	}
	if e = json.Unmarshal(b, &decoded); e != nil {
		t.Fatal(e)
	}
	if decoded.RPC != "https://example.org" || decoded.Operations[0].Nonce != "18446744073709551615" || !strings.Contains(decoded.Operations[0].Payload, "18446744073709551615") {
		t.Fatal("public quantity precision lost")
	}
	if _, e = publicJSON(make(chan int)); e == nil {
		t.Fatal("unencodable public response")
	}
}

func TestTP033RecoveryConfirmationHasOnlyPublicSnapshotAndRemovedMethodsCannotReachSigner(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	out, err := c.handle(message("confirmRecovery", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil || !bytes.Contains(raw, []byte(`"recoveryConfirmed":true`)) {
		t.Fatal("confirmation did not return fresh public state", err)
	}
	for _, name := range []string{`"backupVerified"`, `"entropy"`, `"mnemonic"`, `"passphrase"`, `"password"`, `"seed"`} {
		if bytes.Contains(raw, []byte(name)) {
			t.Fatal("confirmation exposed an obsolete authority field or secret")
		}
	}
	c.child.Kill()
	c.child.Path = filepath.Join(t.TempDir(), "unavailable-peer")
	for _, method := range []string{"backup", "restore", "getMnemonic", "revealPhrase", "exportPhrase"} {
		if out, err := c.handle(message(method, struct{}{})); err == nil || out != nil {
			t.Fatal("removed or secret method accepted", method)
		}
	}
	for _, body := range []string{`{"password":"PUBLIC-FORBIDDEN"}`, `{"mnemonic":"PUBLIC-FORBIDDEN"}`, `{"phrase":"PUBLIC-FORBIDDEN"}`, `{"words":["PUBLIC-FORBIDDEN"]}`, `{"x":1,"x":2}`} {
		out, err := c.handle(ipc.Message{ID: "recovery-public-schema", Session: "fixture", Method: "confirmRecovery", Body: []byte(body)})
		if err == nil || out != nil || err.Error() != "invalid fields" {
			t.Fatal("credential or malformed confirmation reached signer", body, err)
		}
	}
}

func TestTP033BackupPhraseOnlyAcceptsEmptyPublicRequestAndReturnsPublicState(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	out, err := c.handle(message("backupPhrase", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil || !bytes.Contains(raw, []byte(`"recoveryConfirmed":true`)) {
		t.Fatal("backup did not return public state")
	}
	for _, secret := range []string{`"phrase"`, `"mnemonic"`, `"entropy"`, `"password"`, `"seed"`, `"privateKey"`, `"passphrase"`} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("backup public reply exposed a secret field")
		}
	}
	c.child.Kill()
	c.child.Path = filepath.Join(t.TempDir(), "unavailable-peer")
	for _, body := range []string{`null`, `[]`, `{"password":"PUBLIC-FORBIDDEN"}`, `{"phrase":"PUBLIC-FORBIDDEN"}`, `{"confirmed":true}`, `{"words":["PUBLIC-FORBIDDEN"]}`, `{"x":1,"x":2}`} {
		if out, err := c.handle(ipc.Message{ID: "backup-public-schema", Session: "fixture", Method: "backupPhrase", Body: []byte(body)}); err == nil || out != nil {
			t.Fatal("credential or malformed backup request reached signer")
		}
	}
}
