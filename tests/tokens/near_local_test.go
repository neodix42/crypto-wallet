package tokens

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bip39 "github.com/tyler-smith/go-bip39"
	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

const publicPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
const publicPassword = "PUBLIC-local-NEP141-fixture-password-5291"

// Only explicit user input/approval is scripted. The real Engine performs its
// production KDFs, authentication, policy installation, signing and persistence.
type tokenApprovals struct {
	imported       bool
	allow          bool
	wrongPassword  bool
	summaries      []string
	passwordChecks int
}

func (ui *tokenApprovals) Input(_ string, _ ...string) ([][]byte, error) {
	if !ui.imported {
		ui.imported = true
		return [][]byte{[]byte(publicPassword), []byte(publicPassword), []byte(publicPhrase), {}}, nil
	}
	ui.passwordChecks++
	password := publicPassword
	if ui.wrongPassword {
		password = "WRONG-PUBLIC-SYNTHETIC-PASSWORD"
	}
	return [][]byte{[]byte(password)}, nil
}
func (ui *tokenApprovals) Confirm(title, summary string) bool {
	if strings.Contains(title, "Authorize") {
		ui.summaries = append(ui.summaries, summary)
		return ui.allow
	}
	return true // Explicit import of this published disposable fixture only.
}
func (*tokenApprovals) File(string, bool) (string, error) {
	return "", errors.New("unused synthetic fixture UI")
}
func (*tokenApprovals) Receive(string, string, []byte) bool { return false }

type nearTokenRecord struct {
	Plan    model.Plan    `json:"plan"`
	Signed  model.Signed  `json:"signed"`
	Receipt model.Receipt `json:"receipt"`
}

func TestLocalNEP141TokenCycle(t *testing.T) {
	endpoint := os.Getenv("WALLET_NEAR_TOKEN_LOCAL_RPC")
	if endpoint == "" {
		t.Skip("NOT_RUN: explicit pinned local FT WASM harness required")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("owned loopback sandbox only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	seed := bip39.NewSeed(publicPhrase, "")
	defer clear(seed)
	base := near.New()
	if err := base.SetEndpoint("near-localnet", endpoint); err != nil {
		t.Fatal(err)
	}
	accounts := make([]model.Account, 2)
	for i := range accounts {
		var err error
		accounts[i], err = base.Derive(seed, uint32(i), "near-localnet")
		if err != nil {
			t.Fatal(err)
		}
	}
	policies := make([]model.NEP141Token, 2)
	for i, contract := range []string{"first.sandbox", "second.sandbox"} {
		var err error
		policies[i], err = base.InspectToken(ctx, accounts[0], contract)
		if err != nil {
			t.Fatal("inspect actual WASM", err)
		}
		if policies[i].Asset.Decimals != []uint8{6, 18}[i] {
			t.Fatal("fixture precision")
		}
	}
	profile := filepath.Join(t.TempDir(), "encrypted-profile")
	files, err := vault.NewFiles(profile)
	if err != nil {
		t.Fatal(err)
	}
	ui := &tokenApprovals{allow: true}
	engine := signer.New(files, ui, []model.Adapter{base}) // Concrete adapter retains WithTokens dispatch.
	engine.Environment = "testnet"
	t.Cleanup(func() { engine.Lock(); _ = files.Close() })
	if err = engine.Create("testnet", 12, true); err != nil {
		t.Fatal("real phrase import", err)
	}
	if err = engine.AddAccount(1); err != nil {
		t.Fatal("derive second account group", err)
	}
	for _, account := range accounts {
		found := false
		for _, restored := range engine.Snapshot().Accounts {
			if restored.ID == account.ID {
				found = reflect.DeepEqual(restored, account)
			}
		}
		if !found {
			t.Fatal("real signer phrase derivation differs from public SDK fixture")
		}
	}
	for _, policy := range policies {
		if err = engine.ImportNEP141Token(accounts[0].ID, policy); err != nil {
			t.Fatal("authenticated exact token import", err)
		}
	}
	if !reflect.DeepEqual(engine.NEP141TokenPolicies(), policies) {
		t.Fatal("real signer did not install exact imported policies")
	}
	adapter, err := base.WithTokens(policies)
	if err != nil {
		t.Fatal(err)
	}
	records := []nearTokenRecord{}
	signPlan := func(p model.Plan) (model.Plan, model.Signed) {
		t.Helper()
		p.ID = signer.ID()
		if e := engine.Prepare(p); e != nil {
			t.Fatal("durable preparation", e)
		}
		beforePassword := ui.passwordChecks
		signed, e := engine.Sign(p.ID)
		if e != nil {
			t.Fatal(e)
		}
		if ui.passwordChecks != beforePassword+1 || len(ui.summaries) == 0 {
			t.Fatal("real transaction lacked password/approval")
		}
		var details near.Payload
		if json.Unmarshal(p.Payload, &details) != nil || details.Token == nil {
			t.Fatal("invalid exact token plan")
		}
		for _, exact := range []string{details.Token.Policy.Asset.Contract, details.Token.Policy.CodeHash, details.Token.Method, details.Token.Args, near.TokenGas, details.Token.Deposit, p.Account.Address, p.Recipient} {
			if !strings.Contains(ui.summaries[len(ui.summaries)-1], exact) {
				t.Fatal("Go approval omitted exact token authority", exact)
			}
		}
		if _, e = engine.Sign(p.ID); e == nil {
			t.Fatal("one-use approval signed twice")
		}
		return p, signed
	}
	execute := func(p model.Plan) model.Receipt {
		t.Helper()
		p, signed := signPlan(p)
		if e := engine.Update(p.ID, "SUBMITTING", nil); e != nil {
			t.Fatal(e)
		}
		receipt, e := adapter.Submit(ctx, p, signed)
		if e != nil || receipt.State != "CONFIRMED" {
			t.Fatalf("real call %s returned %s: %v; detail=%s", p.Asset, receipt.State, e, receipt.Detail)
		}
		reconciled, e := adapter.Reconcile(ctx, p, signed)
		if e != nil || reconciled.State != "CONFIRMED" || reconciled.Identity != signed.Identity {
			t.Fatal("original identity reconciliation", reconciled, e)
		}
		if e = engine.Update(p.ID, receipt.State, &receipt); e != nil {
			t.Fatal("durable receipt", e)
		}
		records = append(records, nearTokenRecord{Plan: p, Signed: signed, Receipt: receipt})
		return receipt
	}
	balance := func(account model.Account, asset string) *big.Int {
		t.Helper()
		balances, e := adapter.Balance(ctx, account)
		if e != nil {
			t.Fatal(e)
		}
		for _, b := range balances {
			if b.Asset == asset {
				if b.State != "FINAL" {
					t.Fatal("token not verified", b)
				}
				n, ok := new(big.Int).SetString(b.Atomic, 10)
				if !ok {
					t.Fatal("bad balance")
				}
				return n
			}
		}
		t.Fatal("token balance omitted")
		return nil
	}
	// Both refusal and a wrong current password must cancel real durable plans
	// without obtaining a signature or submitting anything to the local chain.
	for _, refusal := range []string{"cancel", "password"} {
		draft, e := adapter.PrepareTokenRegistration(ctx, accounts[1], policies[0].Asset.ID)
		if e != nil {
			t.Fatal(e)
		}
		draft.ID = signer.ID()
		if e = engine.Prepare(draft); e != nil {
			t.Fatal(e)
		}
		ui.allow = refusal != "cancel"
		ui.wrongPassword = refusal == "password"
		if result, e := engine.Sign(draft.ID); e == nil || len(result.Bytes) != 0 {
			t.Fatal("cancelled/wrong-password plan obtained signature")
		}
		ui.allow = true
		ui.wrongPassword = false
		found := false
		for _, op := range engine.Pending() {
			if op.Plan.ID == draft.ID {
				found = op.State == "CANCELLED" && op.Signed == nil
			}
		}
		if !found {
			t.Fatal("denied approval did not durably release unsigned reservation")
		}
	}
	restart := func() {
		t.Helper()
		engine.Lock()
		if engine.NEP141TokenPolicies() != nil {
			t.Fatal("locked signer retained token policy cache")
		}
		if e := files.Close(); e != nil {
			t.Fatal(e)
		}
		var e error
		files, e = vault.NewFiles(profile)
		if e != nil {
			t.Fatal(e)
		}
		engine = signer.New(files, ui, []model.Adapter{base})
		engine.Environment = "testnet"
		if e = engine.Unlock(); e != nil {
			t.Fatal("fresh signer unlock", e)
		}
		if !reflect.DeepEqual(engine.NEP141TokenPolicies(), policies) {
			t.Fatal("encrypted token policies did not survive fresh signer")
		}
	}
	expected := map[string][]string{}
	for _, policy := range policies {
		initial := balance(accounts[0], policy.Asset.ID)
		storage, e := adapter.TokenStorage(ctx, accounts[1], policy.Asset.ID)
		if e != nil || storage.Registered {
			t.Fatal("fresh receiver registration", storage, e)
		}
		out, _ := amount.Parse("12.345678", policy.Asset.Decimals)
		back, _ := amount.Parse("1.234567", policy.Asset.Decimals)
		transfer := model.Transfer{AccountID: accounts[0].ID, Asset: policy.Asset.ID, Recipient: accounts[1].Address, Amount: out}
		if _, e = adapter.Prepare(ctx, accounts[0], transfer); e == nil || !strings.Contains(e.Error(), "not registered") {
			t.Fatal("unregistered recipient accepted", e)
		}
		register, e := adapter.PrepareTokenRegistration(ctx, accounts[1], policy.Asset.ID)
		if e != nil || register.Amount != storage.MinDeposit {
			t.Fatal("exact storage deposit", e)
		}
		execute(register)
		storage, e = adapter.TokenStorage(ctx, accounts[1], policy.Asset.ID)
		if e != nil || !storage.Registered {
			t.Fatal("registration did not execute", storage, e)
		}
		p, e := adapter.Prepare(ctx, accounts[0], transfer)
		if e != nil {
			t.Fatal(e)
		}
		execute(p)
		outN, _ := new(big.Int).SetString(out, 10)
		if balance(accounts[1], policy.Asset.ID).Cmp(outN) != 0 {
			t.Fatal("outgoing units differ")
		}
		transfer = model.Transfer{AccountID: accounts[1].ID, Asset: policy.Asset.ID, Recipient: accounts[0].Address, Amount: back}
		p, e = adapter.Prepare(ctx, accounts[1], transfer)
		if e != nil {
			t.Fatal(e)
		}
		execute(p)
		backN, _ := new(big.Int).SetString(back, 10)
		receiver := new(big.Int).Sub(outN, backN)
		owner := new(big.Int).Sub(initial, receiver)
		if balance(accounts[0], policy.Asset.ID).Cmp(owner) != 0 || balance(accounts[1], policy.Asset.ID).Cmp(receiver) != 0 {
			t.Fatal("two-way token accounting differs")
		}
		expected[policy.Asset.Contract] = []string{owner.String(), receiver.String()}
	}
	// The node accepts the exact original token call; its HTTP acknowledgment is
	// then discarded by an owned proxy. Reconcile, never re-sign or resend.
	forwarded := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var request struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &request)
		req, _ := http.NewRequestWithContext(r.Context(), "POST", endpoint, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			http.Error(w, "local proxy unavailable", 502)
			return
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if request.Method == "send_tx" {
			forwarded++
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"wallet","error":{"code":-32000,"message":"synthetic lost acknowledgment"}}`))
			return
		}
		_, _ = w.Write(raw)
	}))
	defer proxy.Close()
	policy := policies[0]
	transfer := model.Transfer{AccountID: accounts[0].ID, Asset: policy.Asset.ID, Recipient: accounts[1].Address, Amount: "1"}
	p, e := adapter.Prepare(ctx, accounts[0], transfer)
	if e != nil {
		t.Fatal(e)
	}
	p, signed := signPlan(p)
	if e = engine.Update(p.ID, "SUBMITTING", nil); e != nil {
		t.Fatal(e)
	}
	if e = adapter.SetEndpoint("near-localnet", proxy.URL); e != nil {
		t.Fatal(e)
	}
	unknown, e := adapter.Submit(ctx, p, signed)
	if e == nil || unknown.State != "UNKNOWN" || forwarded != 1 {
		t.Fatal("lost acknowledgment was treated as safe to retry", unknown, e)
	}
	if e = engine.Update(p.ID, "UNKNOWN", &unknown); e != nil {
		t.Fatal("persist uncertain original", e)
	}
	restart()
	foundOriginal := false
	for _, op := range engine.Pending() {
		if op.Plan.ID == p.ID {
			foundOriginal = op.State == "UNKNOWN" && op.Signed != nil && op.Signed.Identity == signed.Identity && bytes.Equal(op.Signed.Bytes, signed.Bytes)
		}
	}
	if !foundOriginal {
		t.Fatal("fresh encrypted signer lost original UNKNOWN identity/bytes")
	}
	if _, e = engine.Sign(p.ID); e == nil {
		t.Fatal("fresh signer re-signed ambiguous operation")
	}
	if e = adapter.SetEndpoint("near-localnet", endpoint); e != nil {
		t.Fatal(e)
	}
	conflict, e := adapter.Prepare(ctx, accounts[0], transfer)
	if e != nil {
		t.Fatal(e)
	}
	conflict.ID = signer.ID()
	if e = engine.Prepare(conflict); e == nil {
		t.Fatal("UNKNOWN operation failed to reserve whole account after restart")
	}
	confirmed, e := adapter.Reconcile(ctx, p, signed)
	if e != nil || confirmed.State != "CONFIRMED" || confirmed.Identity != signed.Identity || forwarded != 1 {
		t.Fatal("lost ack original reconciliation", confirmed, e)
	}
	if e = engine.Update(p.ID, confirmed.State, &confirmed); e != nil {
		t.Fatal("persist reconciled receipt", e)
	}
	// Read the already mined final token call through the production bounded
	// history adapter. This creates no transaction and does not use an indexer.
	history, e := adapter.HistoryPage(ctx, accounts[0], "")
	if e != nil || history.Complete || history.NextCursor != "" {
		t.Fatal("actual RPC token history unavailable or overstated", history, e)
	}
	observedHistory := []model.Receipt{}
	for _, entry := range history.Entries {
		if entry.Identity != signed.Identity {
			continue
		}
		if entry.State != "OBSERVED" || entry.Timestamp <= 0 || entry.Timestamp > time.Now().Unix()+300 || entry.Explorer != "" {
			t.Fatal("actual local token history identity/time/scope changed", entry)
		}
		found := false
		for _, effect := range entry.Transfers {
			if effect.Asset == policy.Asset && effect.Source == p.Account.Address && effect.Destination == p.Recipient && effect.Atomic == p.Amount {
				found = true
			}
		}
		if !found {
			t.Fatal("actual RPC history omitted exact last token movement", entry)
		}
		observedHistory = append(observedHistory, entry)
	}
	if len(observedHistory) != 1 {
		t.Fatal("last confirmed real token transaction missing/duplicated in bounded history", signed.Identity, history)
	}
	if e = engine.Prepare(conflict); e != nil {
		t.Fatal("confirmed original failed to release reservation", e)
	}
	if e = engine.CancelPrepared(conflict.ID); e != nil {
		t.Fatal("cancel unused control plan", e)
	}
	records = append(records, nearTokenRecord{Plan: p, Signed: signed, Receipt: confirmed})
	owner, _ := new(big.Int).SetString(expected[policy.Asset.Contract][0], 10)
	receiver, _ := new(big.Int).SetString(expected[policy.Asset.Contract][1], 10)
	owner.Sub(owner, big.NewInt(1))
	receiver.Add(receiver, big.NewInt(1))
	expected[policy.Asset.Contract] = []string{owner.String(), receiver.String()}
	if balance(accounts[0], policy.Asset.ID).Cmp(owner) != 0 || balance(accounts[1], policy.Asset.ID).Cmp(receiver) != 0 {
		t.Fatal("lost acknowledgment duplicated token movement")
	}
	restart()
	confirmedCount := 0
	for _, op := range engine.Pending() {
		if op.State == "CONFIRMED" {
			confirmedCount++
			if op.Signed == nil || op.Receipt == nil || op.Receipt.Identity != op.Signed.Identity {
				t.Fatal("durable confirmed identity missing")
			}
		}
	}
	if confirmedCount != 7 {
		t.Fatal("fresh signer did not restore seven confirmed real calls", confirmedCount)
	}
	ciphertext, e := files.Read("vault.lwv")
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{publicPhrase, publicPassword, policies[0].Asset.Contract, policies[1].Asset.Contract} {
		if bytes.Contains(ciphertext, []byte(secret)) {
			t.Fatal("secret/policy was stored in plaintext")
		}
	}
	clear(ciphertext)
	report := struct {
		Status                string              `json:"status"`
		Accounts              []model.Account     `json:"accounts"`
		Policies              []model.NEP141Token `json:"policies"`
		Transactions          []nearTokenRecord   `json:"transactions"`
		Expected              map[string][]string `json:"expected"`
		RealSigner            bool                `json:"realSigner"`
		PasswordApprovedCalls int                 `json:"passwordApprovedCalls"`
		DurableConfirmed      int                 `json:"durableConfirmed"`
		RestartedUnknown      bool                `json:"restartedUnknown"`
		ObservedHistory       []model.Receipt     `json:"observedHistory"`
	}{"PASS", accounts, policies, records, expected, true, len(records), confirmedCount, foundOriginal, observedHistory}
	raw, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join("..", "..", "reports", "near-token-transactions.json")
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, append(raw, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	if len(records) != 7 {
		t.Fatal(errors.New("required real registrations/transfers were not all executed"))
	}
	t.Log("PASS: two official NEP-141 contracts (6/18 decimals), two self registrations, four out/back transfers and one accepted/lost-ack original reconciliation; all seven calls password-approved and encrypted-journaled by real signer, fresh-signer UNKNOWN/reservation/recovery, exact receipt/balance checks and read-only history of the final token movement verified")
}
