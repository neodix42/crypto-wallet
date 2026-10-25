package near

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	bip39 "github.com/tyler-smith/go-bip39"
	"localwallet/internal/model"
)

const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func seed() []byte { return bip39.NewSeed(phrase, "") }
func fixturePlan(t *testing.T, a *Adapter) model.Plan {
	t.Helper()
	ac, e := a.Derive(seed(), 0, "near-testnet")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Unix()
	p := Payload{Version: 1, BlockHash: base58.Encode(make([]byte, 32)), BlockHeight: "123", Protocol: "88", Gas: "446365125000", GasPrice: "1000000000", StorageReserve: "1000000000000000000000", RecipientExists: true, Prepared: now, FeePolicy: "protocol-assessed fee; estimate reserve is not a signed fee cap"}
	b, _ := json.Marshal(p)
	return model.Plan{Account: ac, Asset: asset(ac.Network), FeeAsset: asset(ac.Network), Amount: "12345", Recipient: "receiver.testnet", Nonce: "42", MaxFee: "892730250000000000000", Expires: now + 60, Payload: b}
}
func TestTP002IndependentOracle(t *testing.T) {
	b, e := os.ReadFile("../../../tests/oracles/near/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Vectors []struct {
			Index                                                                                 uint32
			Address, PublicKey, Recipient, Amount, Nonce, BlockHash, Raw, Hash, Signature, Signed string
		}
	}
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	a := New()
	for _, v := range f.Vectors {
		ac, e := a.Derive(seed(), v.Index, "near-testnet")
		if e != nil || ac.Address != v.Address || ac.PublicKey != v.PublicKey {
			t.Fatalf("oracle derivation index %d", v.Index)
		}
		p := fixturePlan(t, a)
		p.Account = ac
		p.Recipient = v.Recipient
		p.Amount = v.Amount
		p.Nonce = v.Nonce
		var payload Payload
		_ = json.Unmarshal(p.Payload, &payload)
		payload.BlockHash = v.BlockHash
		p.Payload, _ = json.Marshal(payload)
		raw, e := transaction(p)
		if e != nil || hex.EncodeToString(raw) != v.Raw {
			t.Fatalf("Borsh oracle index %d", v.Index)
		}
		signed, e := a.Sign(seed(), p)
		if e != nil || hex.EncodeToString(signed.Bytes) != v.Signed || signed.Identity != v.Hash {
			t.Fatalf("signature oracle index %d: %v", v.Index, e)
		}
		hash := sha256.Sum256(raw)
		pub, _ := public(ac.PublicKey)
		sig, _ := hex.DecodeString(v.Signature)
		if !ed25519.Verify(pub, hash[:], sig) {
			t.Fatal("oracle signature")
		}
	}
}
func TestTP042RejectChangedPlanAndArtifact(t *testing.T) {
	a := New()
	p := fixturePlan(t, a)
	s, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*model.Plan){func(p *model.Plan) { p.Amount = "0001" }, func(p *model.Plan) { p.MaxFee = "0" }, func(p *model.Plan) { p.Asset = "near-mainnet:native" }, func(p *model.Plan) { p.Account.Network = "near-mainnet" }, func(p *model.Plan) { p.Memo = "send" }, func(p *model.Plan) { p.Nonce = "-1" }, func(p *model.Plan) { p.Expires-- }, func(p *model.Plan) { p.Payload = []byte(`{"version":1,"version":1}`) }, func(p *model.Plan) { p.Account.PublicKey = "ed25519:111" }, func(p *model.Plan) { p.Account.Profile = "ledger" }, func(p *model.Plan) { p.Recipient = "Bad.NEAR" }} {
		q := p
		change(&q)
		if a.ValidatePlan(q) == nil && a.verify(q, s) == nil {
			t.Fatal("changed intent accepted")
		}
	}
	s.Bytes = bytes.Clone(s.Bytes)
	s.Bytes[len(s.Bytes)-1] ^= 1
	if a.verify(p, s) == nil {
		t.Fatal("tampered signature")
	}
	if _, e = a.Sign(make([]byte, 64), p); e == nil {
		t.Fatal("foreign seed")
	}
	p.Expires = time.Now().Unix() - 1
	if _, e = a.Sign(seed(), p); e == nil {
		t.Fatal("expired")
	}
}
func TestTP052AddressesAndDescriptor(t *testing.T) {
	a := New()
	for _, s := range []string{"", "x", "UPPER.near", "a..near", "-a.near", "a_.near", strings.Repeat("x", 65), "test.near/../../"} {
		if a.ValidateAddress("near-testnet", s) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"aa", "alice.near", strings.Repeat("0", 64), "one-two_three.testnet"} {
		if a.ValidateAddress("near-testnet", s) != nil {
			t.Fatal(s)
		}
	}
	if _, e := a.Derive(seed(), 1<<31, "near-testnet"); e == nil {
		t.Fatal("unbounded index")
	}
	if e := a.SetEndpoint("near-testnet", "http://example.org"); e == nil {
		t.Fatal("cleartext remote")
	}
	if len(a.Networks()) != 3 {
		t.Fatal("network coverage")
	}
	ac, _ := a.Derive(seed(), 0, "near-testnet")
	ac.Address = "alice.near"
	if a.account(ac) == nil {
		t.Fatal("unverified association")
	}
}
func nearServer(t *testing.T, handler func(string, map[string]json.RawMessage) (any, *rpcError)) (*Adapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
			t.Error(e)
		}
		var params map[string]json.RawMessage
		_ = json.Unmarshal(q.Params, &params)
		result, rpcErr := handler(q.Method, params)
		out := map[string]any{"jsonrpc": "2.0", "id": "wallet"}
		if rpcErr != nil {
			out["error"] = rpcErr
		} else {
			out["result"] = result
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	a := New()
	if e := a.SetEndpoint("near-testnet", srv.URL); e != nil {
		t.Fatal(e)
	}
	return a, srv
}
func TestTP051AuthorityAndFinality(t *testing.T) {
	permission := "FullAccess"
	wrong := false
	final := "FINAL"
	failed := false
	p := fixturePlan(t, New())
	s, _ := New().Sign(seed(), p)
	a, srv := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
		switch method {
		case "status":
			id := "testnet"
			if wrong {
				id = "mainnet"
			}
			return map[string]any{"chain_id": id, "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
		case "query":
			var req string
			_ = json.Unmarshal(params["request_type"], &req)
			if req == "view_access_key" {
				return map[string]any{"nonce": 41, "permission": permission, "block_height": 123, "block_hash": base58.Encode(make([]byte, 32))}, nil
			}
			return map[string]any{"amount": "1000000000000000000000000000", "locked": "0", "storage_usage": 182, "block_height": 123, "block_hash": base58.Encode(make([]byte, 32))}, nil
		case "tx", "send_tx":
			status := map[string]any{"SuccessValue": ""}
			if failed {
				status = map[string]any{"Failure": map[string]any{"ActionError": "synthetic"}}
			}
			return map[string]any{"status": status, "final_execution_status": final, "transaction": map[string]any{"hash": s.Identity, "signer_id": p.Account.Address, "receiver_id": p.Recipient}, "transaction_outcome": map[string]any{"block_hash": "final"}}, nil
		case "gas_price":
			return map[string]any{"gas_price": "1000000000"}, nil
		}
		return nil, &rpcError{Code: -1}
	})
	defer srv.Close()
	b, e := a.Balance(context.Background(), p.Account)
	if e != nil || b[0].Atomic != "1000000000000000000000000000" {
		t.Fatal(e)
	}
	permission = "FunctionCall"
	if _, e = a.Balance(context.Background(), p.Account); e == nil {
		t.Fatal("restricted key")
	}
	permission = "FullAccess"
	wrong = true
	if _, e = a.Balance(context.Background(), p.Account); e == nil {
		t.Fatal("wrong ledger")
	}
	wrong = false
	for _, test := range []struct {
		final  string
		failed bool
		want   string
	}{{"EXECUTED", false, "SUBMITTED"}, {"FINAL", false, "CONFIRMED"}, {"FINAL", true, "FAILED"}} {
		final = test.final
		failed = test.failed
		r, e := a.Reconcile(context.Background(), p, s)
		if e != nil || r.State != test.want {
			t.Fatalf("%+v %v", r, e)
		}
	}
	_, e = a.Associate(context.Background(), p.Account, "alice.testnet")
	if e != nil {
		t.Fatal(e)
	}
	if r, e := a.Submit(context.Background(), p, s); e != nil || r.State != "FAILED" {
		t.Fatalf("submit %v %v", r, e)
	}
}
func TestLocalNode(t *testing.T) {
	endpoint := os.Getenv("WALLET_NEAR_LOCAL_RPC")
	if endpoint == "" {
		t.Skip("NOT_RUN: pinned sandbox harness required")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("local node only")
	}
	a := New()
	if e := a.SetEndpoint("near-localnet", endpoint); e != nil {
		t.Fatal(e)
	}
	ac, e := a.Derive(seed(), 0, "near-localnet")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	recipient := "receiver.testnet"
	before, e := directBalance(ctx, endpoint, recipient)
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Prepare(ctx, ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: recipient, Amount: "1000000000000000000000000"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := a.Submit(ctx, p, s)
	if e != nil || r.State != "CONFIRMED" {
		t.Fatalf("%+v %v", r, e)
	}
	after, e := directBalance(ctx, endpoint, recipient)
	if e != nil || after != "11000000000000000000000000" || before != "10000000000000000000000000" {
		t.Fatalf("independent state: before %s after %s %v", before, after, e)
	}
	if r, e = a.Reconcile(ctx, p, s); e != nil || r.State != "CONFIRMED" {
		t.Fatalf("reconcile %+v %v", r, e)
	}
	associated, e := a.Associate(ctx, ac, "receiver.testnet")
	if e != nil {
		t.Fatal(e)
	}
	back, e := a.Prepare(ctx, associated, model.Transfer{AccountID: associated.ID, Asset: asset(associated.Network), Recipient: ac.Address, Amount: "100000000000000000000000"})
	if e != nil {
		t.Fatal(e)
	}
	backSigned, e := a.Sign(seed(), back)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := a.Submit(ctx, back, backSigned); e != nil || got.State != "CONFIRMED" {
		t.Fatalf("named-account send %+v %v", got, e)
	}
	fresh, e := a.Derive(seed(), 2, "near-localnet")
	if e != nil {
		t.Fatal(e)
	}
	if balances, err := a.Balance(ctx, fresh); err != nil || len(balances) != 1 || balances[0].Atomic != "0" || balances[0].State != missingBalanceState || len(base58.Decode(balances[0].At)) != 32 {
		t.Fatalf("fresh implicit account before creation: %+v %v", balances, err)
	}
	if _, err := a.Prepare(ctx, fresh, model.Transfer{AccountID: fresh.ID, Asset: asset(fresh.Network), Recipient: ac.Address, Amount: "1"}); err == nil {
		t.Fatal("uninitialized sender obtained a transaction plan")
	}
	fund, e := a.Prepare(ctx, ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: fresh.Address, Amount: "1000000000000000000000000"})
	if e != nil {
		t.Fatal(e)
	}
	fundSigned, e := a.Sign(seed(), fund)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := a.Submit(ctx, fund, fundSigned); e != nil || got.State != "CONFIRMED" {
		t.Fatalf("implicit creation %+v %v", got, e)
	}
	if got, e := directBalance(ctx, endpoint, fresh.Address); e != nil || got != "1000000000000000000000000" {
		t.Fatalf("fresh implicit balance %s %v", got, e)
	}
	if balances, err := a.Balance(ctx, fresh); err != nil || len(balances) != 1 || balances[0].Atomic != "1000000000000000000000000" || balances[0].State != "FINAL" {
		t.Fatalf("created implicit account retained missing-account state: %+v %v", balances, err)
	}
	for _, account := range []model.Account{ac, fresh} {
		entries, err := a.History(ctx, account)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if entry.Identity == fundSigned.Identity && entry.State == "CONFIRMED" {
				found = true
			}
		}
		if !found {
			t.Fatal("bounded native incoming/outgoing history omitted accepted transfer")
		}
	}
	if _, e = a.Prepare(ctx, ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: "missing.testnet", Amount: "1"}); e == nil {
		t.Fatal("missing named recipient")
	}
	for _, test := range []struct {
		index uint32
		name  string
	}{{0, "revoked.testnet"}, {1, "restricted.testnet"}} {
		derived, e := a.Derive(seed(), test.index, "near-localnet")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = a.Associate(ctx, derived, test.name); e == nil {
			t.Fatal("removed or function-only key accepted")
		}
	}
	// A public-RPC lie about recipient existence can still produce an accepted
	// transaction whose receipt fails. The adapter must report that execution.
	failure, e := a.Prepare(ctx, ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: "receiver.testnet", Amount: "1"})
	if e != nil {
		t.Fatal(e)
	}
	failure.Recipient = "missing.testnet"
	failureSigned, e := a.Sign(seed(), failure)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := a.Submit(ctx, failure, failureSigned); e != nil || got.State != "FAILED" {
		t.Fatalf("accepted but failed NEAR execution %+v %v", got, e)
	}
	if got, e := a.Reconcile(ctx, failure, failureSigned); e != nil || got.State != "FAILED" {
		t.Fatalf("failed NEAR reconciliation %+v %v", got, e)
	}
	t.Log("PASS: sandbox accepted native implicit/named transfers and fresh implicit creation; independent balances match; removed/restricted keys refused; accepted receipt failure remains FAILED")
}
func directBalance(ctx context.Context, endpoint, account string) (string, error) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "independent", "method": "query", "params": map[string]string{"request_type": "view_account", "finality": "final", "account_id": account}})
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	var out struct {
		Result struct {
			Amount string `json:"amount"`
		} `json:"result"`
	}
	e = json.NewDecoder(resp.Body).Decode(&out)
	return out.Result.Amount, e
}
func FuzzAddress(f *testing.F) {
	for _, s := range []string{"alice.near", "a..b", "", strings.Repeat("0", 64)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _ = New().ValidateAddress("near-testnet", s) })
}
