package aptos

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aptos-labs/aptos-go-sdk/bcs"
	bip39 "github.com/tyler-smith/go-bip39"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func seed() []byte { return bip39.NewSeed(phrase, "") }
func fixturePlan(t *testing.T, a *Adapter) model.Plan {
	t.Helper()
	ac, e := a.Derive(seed(), 0, "aptos-testnet")
	if e != nil {
		t.Fatal(e)
	}
	p := model.Plan{Account: ac, Asset: asset(ac.Network), FeeAsset: asset(ac.Network), Amount: "123456789", Recipient: "0x" + strings.Repeat("0", 60) + "cafe", Nonce: "42", MaxFee: "200000", Expires: time.Now().Unix() + 120}
	d := Payload{Version: 1, GasUnits: "2000", GasPrice: "100", Ledger: "123", Prepared: p.Expires - 120}
	r, e := raw(p, d)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := bcs.Serialize(r)
	d.Raw = hex.EncodeToString(b)
	p.Payload, _ = json.Marshal(d)
	return p
}
func TestTP002IndependentOracle(t *testing.T) {
	b, e := os.ReadFile("../../../tests/oracles/aptos/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Vectors []struct {
			Index                                                                                                 uint32
			Address, PublicKey, Recipient, Amount, Nonce, MaxGas, GasPrice, Raw, Message, Signature, Signed, Hash string
			Expires                                                                                               int64
		}
	}
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	a := New()
	for _, v := range f.Vectors {
		ac, e := a.Derive(seed(), v.Index, "aptos-testnet")
		if e != nil || ac.Address != v.Address || ac.PublicKey != v.PublicKey {
			t.Fatalf("oracle derivation index %d: %v", v.Index, e)
		}
		p := fixturePlan(t, a)
		p.Account = ac
		p.Expires = v.Expires
		p.Amount = v.Amount
		p.Recipient = v.Recipient
		p.Nonce = v.Nonce
		d := Payload{GasUnits: v.MaxGas, GasPrice: v.GasPrice}
		r, e := raw(p, d)
		if e != nil {
			t.Fatal(e)
		}
		b, e := bcs.Serialize(r)
		if e != nil || hex.EncodeToString(b) != v.Raw {
			t.Fatalf("BCS oracle index %d", v.Index)
		}
		message, e := r.SigningMessage()
		if e != nil || hex.EncodeToString(message) != v.Message {
			t.Fatal("signing message")
		}
		k, e := derivation.Ed25519(seed(), []uint32{44, 637, v.Index, 0, 0})
		if e != nil {
			t.Fatal(e)
		}
		sig := ed25519.Sign(k, message)
		clear(k)
		if hex.EncodeToString(sig) != v.Signature {
			t.Fatal("oracle signature")
		}
		pub, _ := hex.DecodeString(v.PublicKey)
		tx, e := signed(r, pub, sig)
		if e != nil || tx.Verify() != nil {
			t.Fatal("signature validation")
		}
		signedBytes, e := bcs.Serialize(tx)
		if e != nil || hex.EncodeToString(signedBytes) != v.Signed || identity(signedBytes) != v.Hash {
			t.Fatal("signed BCS / transaction identity")
		}
	}
}
func TestTP042PlanTamperingAndSign(t *testing.T) {
	a := New()
	p := fixturePlan(t, a)
	s, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.verify(p, s); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*model.Plan){func(p *model.Plan) { p.Amount = "0001" }, func(p *model.Plan) { p.MaxFee = "0" }, func(p *model.Plan) { p.Asset = "aptos-mainnet:native" }, func(p *model.Plan) { p.Account.Network = "aptos-mainnet" }, func(p *model.Plan) { p.Memo = "send" }, func(p *model.Plan) { p.Nonce = "-1" }, func(p *model.Plan) { p.Expires-- }, func(p *model.Plan) { p.Payload = []byte(`{"version":1,"version":1}`) }, func(p *model.Plan) { p.Account.PublicKey = "111" }, func(p *model.Plan) { p.Account.Profile = "single-key" }, func(p *model.Plan) { p.Recipient = "0xcafe" }} {
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
func TestTP052AddressProfile(t *testing.T) {
	a := New()
	for _, s := range []string{"", "0x", "0X1", "hello", "0x+1", "0x" + strings.Repeat("1", 65)} {
		if a.ValidateAddress("aptos-testnet", s) == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"0x1", "0xf", "0x" + strings.Repeat("0", 63) + "a"} {
		if a.ValidateAddress("aptos-testnet", s) != nil {
			t.Fatal(s)
		}
	}
	if len(a.Networks()) != 3 {
		t.Fatal("networks")
	}
	if _, e := a.Derive(seed(), 1<<31, "aptos-testnet"); e == nil {
		t.Fatal("index bound")
	}
	if _, e := a.Derive(seed(), 0, "unknown"); e == nil {
		t.Fatal("network")
	}
	if e := a.SetEndpoint("aptos-testnet", "http://remote"); e == nil {
		t.Fatal("TLS")
	}
	ac, _ := a.Derive(seed(), 0, "aptos-testnet")
	ac.Address = "0x" + strings.Repeat("0", 64)
	if a.account(ac) == nil {
		t.Fatal("unverified association")
	}
}
func aptosServer(t *testing.T, h func(*http.Request) any) (*Adapter, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { out := h(r); _ = json.NewEncoder(w).Encode(out) }))
	a := New()
	if e := a.SetEndpoint("aptos-testnet", srv.URL); e != nil {
		t.Fatal(e)
	}
	return a, srv
}
func TestTP051AuthoritySimulationAndCommittedFailure(t *testing.T) {
	p := fixturePlan(t, New())
	wrong := false
	rotated := false
	simOK := true
	pending := true
	success := true
	s, _ := New().Sign(seed(), p)
	a, srv := aptosServer(t, func(r *http.Request) any {
		switch {
		case r.URL.Path == "":
			return nil
		case r.URL.Path == "/":
			id := 2
			if wrong {
				id = 1
			}
			return map[string]any{"chain_id": id, "ledger_version": "123", "ledger_timestamp": fmt.Sprint(time.Now().UnixMicro())}
		case strings.HasPrefix(r.URL.Path, "/accounts/"):
			key := p.Account.Address
			if rotated {
				key = "0x1"
			}
			return map[string]any{"sequence_number": "42", "authentication_key": key}
		case r.URL.Path == "/view":
			var q map[string]any
			_ = json.NewDecoder(r.Body).Decode(&q)
			if q["function"] == "0x1::account::originating_address" {
				return []any{map[string]any{"vec": []string{p.Account.Address}}}
			}
			return []string{"999999999999"}
		case r.URL.Path == "/estimate_gas_price":
			return map[string]any{"gas_estimate": 100}
		case r.URL.Path == "/transactions/simulate":
			return []any{map[string]any{"success": simOK, "gas_used": "1000"}}
		case strings.HasPrefix(r.URL.Path, "/transactions"):
			if pending {
				return map[string]any{"type": "pending_transaction", "hash": s.Identity}
			}
			return map[string]any{"type": "user_transaction", "hash": s.Identity, "sender": p.Account.Address, "sequence_number": "42", "success": success, "version": "124"}
		}
		return map[string]any{}
	})
	defer srv.Close()
	ctx := context.Background()
	b, e := a.Balance(ctx, p.Account)
	if e != nil || b[0].Atomic != "999999999999" {
		t.Fatal(e)
	}
	rotated = true
	if _, e = a.Balance(ctx, p.Account); e == nil {
		t.Fatal("removed authority")
	}
	rotated = false
	wrong = true
	if _, e = a.Balance(ctx, p.Account); e == nil {
		t.Fatal("wrong ledger")
	}
	wrong = false
	q, e := a.Prepare(ctx, p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "1"})
	if e != nil || q.MaxFee != "160000" {
		t.Fatalf("prepare %v %v", q, e)
	}
	simOK = false
	if _, e = a.Prepare(ctx, p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "1"}); e == nil {
		t.Fatal("failed simulation accepted")
	}
	for _, tc := range []struct {
		pending, success bool
		want             string
	}{{true, true, "SUBMITTED"}, {false, true, "CONFIRMED"}, {false, false, "FAILED"}} {
		pending = tc.pending
		success = tc.success
		r, e := a.Reconcile(ctx, p, s)
		if e != nil || r.State != tc.want {
			t.Fatalf("%+v %v", r, e)
		}
	}
	if _, e = a.Discover(ctx, p.Account); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Associate(ctx, p.Account, p.Account.Address); e != nil {
		t.Fatal(e)
	}
	pending = true
	if r, e := a.Submit(ctx, p, s); e != nil || r.State != "SUBMITTED" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestLocalNode(t *testing.T) {
	if os.Getenv("WALLET_APTOS_LOCAL") == "" {
		t.Skip("NOT_RUN: pinned local validator required")
	}
	a := New()
	ac, e := a.Derive(seed(), 0, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	recipient, e := a.Derive(seed(), 1, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, addr := range []string{ac.Address, recipient.Address} {
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8081/mint?address="+addr+"&amount=1000000000", nil)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		var hashes []string
		if e = json.NewDecoder(resp.Body).Decode(&hashes); e != nil {
			resp.Body.Close()
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("local faucet HTTP %d", resp.StatusCode)
		}
		for _, hash := range hashes {
			ready := false
			for j := 0; j < 100; j++ {
				response, er := http.Get("http://127.0.0.1:8080/v1/transactions/by_hash/" + hash)
				if er != nil {
					t.Fatal(er)
				}
				var committed struct {
					Type    string `json:"type"`
					Success bool   `json:"success"`
				}
				er = json.NewDecoder(response.Body).Decode(&committed)
				response.Body.Close()
				if er == nil && committed.Type == "user_transaction" && committed.Success {
					ready = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if !ready {
				t.Fatal("local faucet transaction not committed")
			}
		}
	}
	before, e := independentBalance(ctx, recipient.Address)
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Prepare(ctx, ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: recipient.Address, Amount: "123456789"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := a.Submit(ctx, p, s)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 50 && r.State != "CONFIRMED"; i++ {
		time.Sleep(100 * time.Millisecond)
		r, e = a.Reconcile(ctx, p, s)
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.State != "CONFIRMED" {
		t.Fatalf("%+v", r)
	}
	after, e := independentBalance(ctx, recipient.Address)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := new(big.Int).SetString(before, 10)
	afterN, _ := new(big.Int).SetString(after, 10)
	afterN.Sub(afterN, b)
	if afterN.String() != "123456789" {
		t.Fatalf("independent recipient delta %s", afterN)
	}
	for _, account := range []model.Account{ac, recipient} {
		entries, err := a.History(ctx, account)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if entry.Identity == s.Identity && entry.State == "CONFIRMED" {
				found = true
			}
		}
		if !found {
			t.Fatal("bounded native incoming/outgoing history omitted accepted transfer")
		}
	}
	t.Log("PASS: validator committed native BCS APT transfer; independently queried exact recipient delta and bounded incoming/outgoing history")
}
func independentBalance(ctx context.Context, addr string) (string, error) {
	b, _ := json.Marshal(map[string]any{"function": "0x1::coin::balance", "type_arguments": []string{"0x1::aptos_coin::AptosCoin"}, "arguments": []string{addr}})
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8080/v1/view", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	var out []string
	e = json.NewDecoder(resp.Body).Decode(&out)
	if e != nil || len(out) != 1 {
		return "", fmt.Errorf("independent balance query failed")
	}
	return out[0], nil
}
func FuzzAddress(f *testing.F) {
	for _, s := range []string{"0x1", "0xcafe", "", strings.Repeat("f", 64)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _ = New().ValidateAddress("aptos-testnet", s) })
}
func TestLocalNodeRotation(t *testing.T) {
	if os.Getenv("WALLET_APTOS_ROTATION") == "" {
		t.Skip("NOT_RUN: independent TS SDK rotation setup required")
	}
	a := New()
	old, e := a.Derive(seed(), 10, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	current, e := a.Derive(seed(), 3, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, e = a.Balance(ctx, old); e == nil {
		t.Fatal("rotated-out key retained authority")
	}
	associated, e := a.Discover(ctx, current)
	if e != nil {
		t.Fatal(e)
	}
	if associated.Address != old.Address || associated.Address == current.Address {
		t.Fatalf("rotation origin wrong: %s", associated.Address)
	}
	before, e := independentBalance(ctx, old.Address)
	if e != nil {
		t.Fatal(e)
	}
	recipient, e := a.Derive(seed(), 1, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	p, e := a.Prepare(ctx, associated, model.Transfer{AccountID: associated.ID, Asset: asset(associated.Network), Recipient: recipient.Address, Amount: "1000"})
	if e != nil {
		t.Fatal(e)
	}
	signed, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := a.Submit(ctx, p, signed)
	if e != nil {
		t.Fatal(e)
	}
	for j := 0; j < 50 && r.State != "CONFIRMED"; j++ {
		time.Sleep(50 * time.Millisecond)
		r, e = a.Reconcile(ctx, p, signed)
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.State != "CONFIRMED" {
		t.Fatalf("rotated send %+v", r)
	}
	after, e := independentBalance(ctx, old.Address)
	if e != nil {
		t.Fatal(e)
	}
	oldN, _ := new(big.Int).SetString(before, 10)
	newN, _ := new(big.Int).SetString(after, 10)
	if oldN.Cmp(newN) <= 0 {
		t.Fatal("original account wasn't debited")
	}
	t.Log("PASS: proven rotation lookup preserves original address; old key refused; restored new key signed successful native transfer")
}

// A synthetic direct fixture deliberately bypasses the public preparation refusal
// to prove that accepted-but-failed ledger executions consume gas/sequence and
// are reconciled as FAILED, never payment success.
func TestLocalNodeCommittedFailure(t *testing.T) {
	if os.Getenv("WALLET_APTOS_LOCAL") == "" {
		t.Skip("NOT_RUN: local validator required")
	}
	a := New()
	ac, e := a.Derive(seed(), 0, "aptos-localnet")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		t.Fatal(e)
	}
	state, e := a.authority(ctx, ac, l.Version)
	if e != nil {
		t.Fatal(e)
	}
	p := fixturePlan(t, a)
	p.Account = ac
	p.Asset = asset(ac.Network)
	p.FeeAsset = p.Asset
	p.Nonce = state.Sequence
	p.Amount = "18446744073709551615"
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	d.Ledger = l.Version
	rawTx, e := raw(p, d)
	if e != nil {
		t.Fatal(e)
	}
	b, e := bcs.Serialize(rawTx)
	if e != nil {
		t.Fatal(e)
	}
	d.Raw = hex.EncodeToString(b)
	p.Payload, _ = json.Marshal(d)
	s, e := a.Sign(seed(), p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := a.Submit(ctx, p, s)
	if e != nil {
		t.Fatal(e)
	}
	for j := 0; j < 50 && r.State == "SUBMITTED"; j++ {
		time.Sleep(50 * time.Millisecond)
		r, e = a.Reconcile(ctx, p, s)
		if e != nil {
			t.Fatal(e)
		}
	}
	if r.State != "FAILED" {
		t.Fatalf("committed failure %+v", r)
	}
	l, e = a.identity(ctx, ac.Network)
	if e != nil {
		t.Fatal(e)
	}
	after, e := a.authority(ctx, ac, l.Version)
	if e != nil {
		t.Fatal(e)
	}
	beforeSeq, _ := new(big.Int).SetString(state.Sequence, 10)
	afterSeq, _ := new(big.Int).SetString(after.Sequence, 10)
	if afterSeq.Sub(afterSeq, beforeSeq).String() != "1" {
		t.Fatal("committed failure did not consume sequence")
	}
	t.Log("PASS: node committed failing native transfer, reconciliation reports FAILED, independent account sequence consumed exactly once")
}
