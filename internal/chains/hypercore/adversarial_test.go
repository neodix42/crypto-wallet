package hypercore

import (
	"context"
	"encoding/json"
	"errors"
	"localwallet/internal/model"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTP052MissingOrConflictingBalanceState(t *testing.T) {
	for _, entries := range []any{nil, []any{map[string]any{"coin": "OTHER", "token": 1105, "total": "1", "hold": "0"}}, []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "1e2", "hold": "0"}}, []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "1", "hold": "1.000000001"}}, []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "1", "hold": "0"}, map[string]any{"coin": "HYPE", "token": 1105, "total": "1", "hold": "0"}}} {
		f := fakeCore(t)
		f.override["spotClearinghouseState"] = map[string]any{"balances": entries}
		if _, e := f.prepare(t); e == nil {
			t.Fatal("unreliable available balance prepared")
		}
		ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
		if _, e := f.a.Balance(context.Background(), ac); e == nil {
			t.Fatal("unreliable balance displayed")
		}
	}
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	bad := ac
	bad.ID = "foreign"
	if _, e := f.a.Balance(context.Background(), bad); e == nil {
		t.Fatal("bad descriptor")
	}
	f.fail = "spotMeta"
	if _, e := f.a.Balance(context.Background(), ac); e == nil {
		t.Fatal("unverified balance")
	}
	if _, e := atomic(new(big.Int).Lsh(big.NewInt(1), 256).String()); e == nil {
		t.Fatal("amount overflow")
	}
	if sameAction(json.RawMessage(`{"notAllowed":1}`), action{}) {
		t.Fatal("unvalidated history action")
	}
}
func TestTP052CoreIntentAndRecipientPrerequisites(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	base := model.Transfer{AccountID: ac.ID, Asset: "hypercore-testnet:" + testToken, Recipient: "0x1111111111111111111111111111111111111111", Amount: "12345678"}
	for _, mutate := range []func(*model.Transfer){func(x *model.Transfer) { x.AccountID = "foreign" }, func(x *model.Transfer) { x.Asset = "HYPE" }, func(x *model.Transfer) { x.Memo = "other" }, func(x *model.Transfer) { x.Recipient = "invalid" }, func(x *model.Transfer) { x.Amount = "0" }, func(x *model.Transfer) { x.Amount = "-1" }} {
		bad := base
		mutate(&bad)
		if _, e := f.a.Prepare(context.Background(), ac, bad); e == nil {
			t.Fatal("invalid Core intent")
		}
	}
	bad := ac
	bad.ID = "foreign"
	if _, e := f.a.Prepare(context.Background(), bad, base); e == nil {
		t.Fatal("wrong Core account")
	}
	for _, role := range []string{"missing", "agent", "vault", "subAccount"} {
		f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
			if typ == "userRole" && string(req["user"]) == `"0x1111111111111111111111111111111111111111"` {
				return map[string]string{"role": role}, true
			}
			return nil, false
		}
		if _, e := f.prepare(t); e == nil {
			t.Fatal("unverified recipient activation/role", role)
		}
	}
	f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
		if typ == "userRole" && string(req["user"]) == `"0x1111111111111111111111111111111111111111"` {
			return map[string]string{"role": "invalid"}, true
		}
		return nil, false
	}
	if _, e := f.prepare(t); e == nil {
		t.Fatal("unknown recipient role")
	}
	f.intercept = nil
	f.a.lastNonce = uint64(time.Now().Add(time.Hour).UnixMilli())
	if _, e := f.prepare(t); e == nil {
		t.Fatal("impossible nonce schedule")
	}
}
func TestTP042CoreStaleNonceAndSignatureEncoding(t *testing.T) {
	a := New()
	p := fixturePlan(t, a, "hypercore-testnet")
	var x planPayload
	_ = json.Unmarshal(p.Payload, &x)
	x.Action.Time = uint64(time.Now().Add(-time.Hour).UnixMilli())
	x.StateTime = x.Action.Time
	p.Nonce = new(big.Int).SetUint64(x.Action.Time).String()
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if _, e := a.Sign(syntheticSeed(), p); e == nil {
		t.Fatal("stale nonce action")
	}
	p = fixturePlan(t, a, "hypercore-testnet")
	s, e := a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	bad := p
	bad.ID = "foreign"
	if _, e = a.verifySigned(bad, s); e == nil {
		t.Fatal("invalid plan signature verification")
	}
	for _, raw := range [][]byte{nil, []byte(`{`), []byte(`{"action":{},"action":{}}`)} {
		bad := s
		bad.Bytes = raw
		if _, e = a.verifySigned(p, bad); e == nil {
			t.Fatal("invalid signed schema")
		}
	}
	for _, change := range []func(*signature){func(s *signature) { s.R = "ffff" }, func(s *signature) { s.S = "0x0" }, func(s *signature) { s.S = "0xzz" }} {
		var env envelope
		_ = json.Unmarshal(s.Bytes, &env)
		change(&env.Signature)
		bad := s
		bad.Bytes, _ = json.Marshal(env)
		if _, e = a.verifySigned(p, bad); e == nil {
			t.Fatal("invalid signature scalar")
		}
	}
}
func TestTP060CoreSubmitStalenessAndErrors(t *testing.T) {
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	bad := s
	bad.Identity = "foreign"
	if _, e = f.a.Submit(context.Background(), p, bad); e == nil {
		t.Fatal("foreign signed identity")
	}
	if _, e = f.a.Reconcile(context.Background(), p, bad); e == nil {
		t.Fatal("foreign reconcile identity")
	}
	f.fail = "spotMeta"
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("unverified ledger submit")
	}
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("unverified ledger reconciliation")
	}
	f.fail = "userRole"
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("unknown recipient")
	}
	f.fail = ""
	f.role = "missing"
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("prerequisite changed")
	}
	f.role = "user"
	expired := p
	expired.Expires = time.Now().Add(-time.Minute).Unix()
	expired.ID = planID(expired)
	if _, e = f.a.Submit(context.Background(), expired, s); e == nil {
		t.Fatal("expired Core broadcast")
	}
}
func TestTP050CoreReconciliationDemandsExactActionAndTwoLedgers(t *testing.T) {
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	var env envelope
	_ = json.Unmarshal(s.Bytes, &env)
	f.action = env.Action
	f.owner = p.Account.Address
	f.ledger = true
	tx := func() map[string]any {
		return map[string]any{"time": f.action.Time, "user": f.owner, "action": f.action, "block": 123, "hash": f.hash, "error": nil}
	}
	f.override["userDetails"] = map[string]any{"type": "wrong", "txs": []any{}}
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("wrong explorer schema")
	}
	f.override["userDetails"] = map[string]any{"type": "userDetails", "txs": []any{tx(), tx()}}
	r, e := f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" {
		t.Fatal("nonunique action accepted", r, e)
	}
	delete(f.override, "userDetails")
	for _, mutate := range []func(map[string]any){func(x map[string]any) { x["hash"] = "invalid" }, func(x map[string]any) { x["block"] = 0 }, func(x map[string]any) { delete(x, "error") }} {
		bad := tx()
		mutate(bad)
		f.override["userDetails"] = map[string]any{"type": "userDetails", "txs": []any{bad}}
		if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
			t.Fatal("incomplete inclusion accepted")
		}
	}
	delete(f.override, "userDetails")
	f.fail = "txDetails"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("independent detail missing")
	}
	f.fail = ""
	wrong := tx()
	wrong["block"] = 124
	f.override["txDetails"] = map[string]any{"type": "txDetails", "tx": wrong}
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("conflicting explorer details")
	}
	delete(f.override, "txDetails")
	f.fail = "userNonFundingLedgerUpdates"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("missing ledger effects")
	}
	f.fail = ""
	f.override["userNonFundingLedgerUpdates"] = make([]any, 500)
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" {
		t.Fatal("saturated history accepted", r, e)
	}
	delete(f.override, "userNonFundingLedgerUpdates")
	for _, key := range []string{"amount", "fee", "nativeTokenFee"} {
		delta := map[string]any{"type": "spotTransfer", "token": testToken, "amount": "0.12345678", "user": f.owner, "destination": p.Recipient, "fee": "0", "nativeTokenFee": "0"}
		delta[key] = "invalid"
		f.override["userNonFundingLedgerUpdates"] = []any{map[string]any{"time": f.action.Time, "hash": f.hash, "delta": delta}}
		if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
			t.Fatal("malformed ledger amount", key)
		}
	}
	delete(f.override, "userNonFundingLedgerUpdates")
	// A ledger's human ticker is accepted only after the exact explorer action has
	// bound this hash to the pinned full HYPE token identity on the selected ledger.
	f.override["userNonFundingLedgerUpdates"] = []any{map[string]any{"time": f.action.Time, "hash": f.hash, "delta": map[string]any{"type": "spotTransfer", "token": "HYPE", "amount": json.Number("0.12345678"), "user": f.owner, "destination": p.Recipient, "fee": json.Number("0")}}}
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "CONFIRMED" {
		t.Fatal("official ledger numeric schema", r, e)
	}
	if _, e = decimalJSON(json.RawMessage(`"bad`)); e == nil {
		t.Fatal("malformed quantity")
	}
	if validHash("bad") {
		t.Fatal("bad hash")
	}
	if validHash("0x" + strings.Repeat("g", 64)) {
		t.Fatal("bad hash encoding")
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("synthetic short read") }
func (failingBody) Close() error             { return nil }

type testTransport struct {
	response  string
	readError bool
}

func (t testTransport) RoundTrip(*http.Request) (*http.Response, error) {
	var body httpBody = readString{strings.NewReader(t.response)}
	if t.readError {
		body = failingBody{}
	}
	return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
}

type httpBody interface {
	Read([]byte) (int, error)
	Close() error
}
type readString struct{ *strings.Reader }

func (readString) Close() error { return nil }
func TestTP041CoreResponseBoundsAndTransport(t *testing.T) {
	f := fakeCore(t)
	n, _ := network("hypercore-testnet")
	if len(f.a.Networks()) != 2 || f.a.Networks()[1].RPC != f.server.URL {
		t.Fatal("endpoint copy")
	}
	for _, raw := range []string{`{"x":1,"x":2}`, `{} {}`, strings.Repeat("x", 2<<20+1)} {
		f.a.HTTPClient = &http.Client{Transport: testTransport{response: raw}}
		var out any
		if f.a.request(context.Background(), n, "/info", nil, &out) == nil {
			t.Fatal("malformed Core response")
		}
	}
	f.a.HTTPClient = &http.Client{Transport: testTransport{response: `{}`}}
	var str string
	if f.a.request(context.Background(), n, "/info", nil, &str) == nil {
		t.Fatal("wrong result schema")
	}
	f.a.HTTPClient = &http.Client{Transport: testTransport{readError: true}}
	var out any
	if f.a.request(context.Background(), n, "/info", nil, &out) == nil {
		t.Fatal("short read")
	}
	f.a.HTTPClient = nil
	if f.a.request(context.Background(), n, "/info", nil, &out) == nil {
		t.Fatal("nil HTTP client")
	}
	f.a.endpoints[n.ID] = "http://example.org"
	if f.a.request(context.Background(), n, "/info", nil, &out) == nil {
		t.Fatal("unsafe endpoint")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.org")
		w.WriteHeader(302)
	}))
	defer server.Close()
	a, _ := NewWithEndpoints(map[string]string{n.ID: server.URL})
	if a.request(context.Background(), n, "/info", nil, &out) == nil {
		t.Fatal("redirect followed")
	}
}
