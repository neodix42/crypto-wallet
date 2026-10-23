package hypercore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func syntheticSeed() []byte {
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}
func fixturePlan(t *testing.T, a *Adapter, net string) model.Plan {
	t.Helper()
	ac, e := a.Derive(syntheticSeed(), 0, net)
	if e != nil {
		t.Fatal(e)
	}
	n, _ := network(net)
	at := uint64(time.Now().UnixMilli())
	env := "Mainnet"
	if n.Testnet {
		env = "Testnet"
	}
	x := planPayload{Action: action{"spotSend", "0x66eee", env, "0x1111111111111111111111111111111111111111", n.Assets[0].Contract, "0.12345678", at}, SourceRole: "user", RecipientRole: "user", SourceAbstraction: "default", StateTime: at}
	raw, _ := json.Marshal(x)
	p := model.Plan{Account: ac, Asset: n.Assets[0].ID, Recipient: x.Action.Destination, Amount: "12345678", MaxFee: "0", FeeAsset: n.Assets[0].ID, Nonce: strconv.FormatUint(at, 10), Expires: time.Now().Add(time.Minute).Unix(), Payload: raw}
	p.ID = planID(p)
	return p
}
func TestTP002OfficialPythonSDKFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../tests/oracles/hypercore/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixtures struct {
		Fixtures []struct {
			Index     uint32    `json:"index"`
			Mainnet   bool      `json:"mainnet"`
			Address   string    `json:"address"`
			Action    action    `json:"action"`
			Hash      string    `json:"hash"`
			Signature signature `json:"signature"`
		} `json:"fixtures"`
	}
	if e = json.Unmarshal(raw, &fixtures); e != nil {
		t.Fatal(e)
	}
	for _, f := range fixtures.Fixtures {
		net := "hypercore-testnet"
		if f.Mainnet {
			net = "hypercore-mainnet"
		}
		ac, e := New().Derive(syntheticSeed(), f.Index, net)
		if e != nil || ac.Address != f.Address {
			t.Fatal("independent derivation mismatch", f.Index, e)
		}
		h, e := typedHash(f.Action)
		if e != nil || "0x"+hex.EncodeToString(h) != f.Hash {
			t.Fatal("official SDK typed-hash mismatch", e)
		}
		key, _ := deriveKey(syntheticSeed(), f.Index)
		sig, e := crypto.Sign(h, key)
		key.D.SetInt64(0)
		if e != nil {
			t.Fatal(e)
		}
		got := signature{"0x" + new(big.Int).SetBytes(sig[:32]).Text(16), "0x" + new(big.Int).SetBytes(sig[32:64]).Text(16), sig[64] + 27}
		wipe(sig)
		if got != f.Signature {
			t.Fatal("official SDK signature mismatch", f.Index)
		}
	}
}
func TestTP052CorePrecisionAndRegistry(t *testing.T) {
	a := New()
	if len(a.Networks()) != 2 {
		t.Fatal("Core coverage")
	}
	for _, v := range []string{"0", "0.0", "1", "0.00000001", "1.12345678", "1000000000.00000000"} {
		n, e := parseUnits(v)
		if e != nil {
			t.Fatal(v, e)
		}
		again, e := parseUnits(formatUnits(n))
		if e != nil || again.Cmp(n) != 0 {
			t.Fatal("precision changed")
		}
	}
	for _, v := range []string{"", "01", "1e2", "-1", "1.", ".1", "1.000000001", "0.123456789", "1.0.0", strings.Repeat("9", 90)} {
		if _, e := parseUnits(v); e == nil {
			t.Fatal("invalid precision accepted", v)
		}
	}
	for _, v := range []string{"", "-1", "01", "1e3", strings.Repeat("9", 79)} {
		if _, e := atomic(v); e == nil {
			t.Fatal("atomic validation", v)
		}
	}
	for _, v := range []string{"0x2222222222222222222222222222222222222222", "0x2000000000000000000000000000000000000001", "bad"} {
		if a.ValidateAddress("hypercore-testnet", v) == nil {
			t.Fatal("system recipient accepted")
		}
	}
	if a.ValidateAddress("unknown", "bad") == nil {
		t.Fatal("network accepted")
	}
	if _, e := a.Derive(nil, 0, "hypercore-testnet"); e == nil {
		t.Fatal("invalid seed")
	}
	if _, e := a.Derive(syntheticSeed(), 0, "unknown"); e == nil {
		t.Fatal("unknown net")
	}
	if _, e := NewWithEndpoints(map[string]string{"unknown": "https://example.org"}); e == nil {
		t.Fatal("invalid network endpoint")
	}
	if _, e := NewWithEndpoints(map[string]string{"hypercore-testnet": "http://example.org"}); e == nil {
		t.Fatal("HTTP endpoint")
	}
}
func TestTP042CorePlanSignatureMutations(t *testing.T) {
	a := New()
	p := fixturePlan(t, a, "hypercore-testnet")
	s, e := a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.verifySigned(p, s); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*model.Plan){func(p *model.Plan) { p.Recipient = "invalid" }, func(p *model.Plan) { p.Asset = "HYPE" }, func(p *model.Plan) { p.Amount = "0" }, func(p *model.Plan) { p.Nonce = "01" }, func(p *model.Plan) { p.MaxFee = "1" }, func(p *model.Plan) { p.Payload = json.RawMessage(`{"action":{},"action":{}}`) }, func(p *model.Plan) { p.Account.Profile = "other" }, func(p *model.Plan) { p.Account.PublicKey = "00" }} {
		q := p
		mutate(&q)
		q.ID = planID(q)
		if a.ValidatePlan(q) == nil {
			t.Fatal("mutated plan accepted")
		}
		if _, e = a.Sign(syntheticSeed(), q); e == nil {
			t.Fatal("mutated signature request accepted")
		}
	}
	for _, mutate := range []func(*planPayload){func(x *planPayload) { x.Action.Type = "order" }, func(x *planPayload) { x.Action.SignatureChainID = "0x3e6" }, func(x *planPayload) { x.Action.HyperliquidChain = "Mainnet" }, func(x *planPayload) { x.Action.Token = mainToken }, func(x *planPayload) { x.Action.Destination = "0x2222222222222222222222222222222222222223" }, func(x *planPayload) { x.Action.Amount = "0.123456780" }, func(x *planPayload) { x.Action.Time++ }, func(x *planPayload) { x.SourceRole = "agent" }, func(x *planPayload) { x.RecipientRole = "missing" }, func(x *planPayload) { x.SourceAbstraction = "unifiedAccount" }, func(x *planPayload) { x.StateTime = 0 }} {
		q := p
		var x planPayload
		_ = json.Unmarshal(q.Payload, &x)
		mutate(&x)
		q.Payload, _ = json.Marshal(x)
		q.ID = planID(q)
		if a.ValidatePlan(q) == nil {
			t.Fatal("action/domain modification accepted")
		}
	}
	q := p
	q.Expires = time.Now().Add(-time.Minute).Unix()
	q.ID = planID(q)
	if _, e = a.Sign(syntheticSeed(), q); e == nil {
		t.Fatal("expired approval")
	}
	foreign := syntheticSeed()
	foreign[0]++
	if _, e = a.Sign(foreign, p); e == nil {
		t.Fatal("foreign owner")
	}
	for _, mutate := range []func(*envelope){func(x *envelope) { x.Action.Amount = "2" }, func(x *envelope) { x.Nonce++ }, func(x *envelope) { x.Signature.V = 1 }, func(x *envelope) { x.Signature.R = "0x0" }, func(x *envelope) {
		x.Signature.S = "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	}, func(x *envelope) { x.Signature.R = "0x" + strings.Repeat("a", 64) }} {
		var x envelope
		_ = json.Unmarshal(s.Bytes, &x)
		mutate(&x)
		bad := s
		bad.Bytes, _ = json.Marshal(x)
		if _, e = a.verifySigned(p, bad); e == nil {
			t.Fatal("invalid signed envelope")
		}
	}

	var forged envelope
	_ = json.Unmarshal(s.Bytes, &forged)
	h, _ := typedHash(forged.Action)
	key, _ := deriveKey(foreign, p.Account.Index)
	forgedSig, _ := crypto.Sign(h, key)
	key.D.SetInt64(0)
	forged.Signature = signature{"0x" + new(big.Int).SetBytes(forgedSig[:32]).Text(16), "0x" + new(big.Int).SetBytes(forgedSig[32:64]).Text(16), forgedSig[64] + 27}
	wipe(forgedSig)
	wrongOwner := s
	wrongOwner.Bytes, _ = json.Marshal(forged)
	if _, e = a.verifySigned(p, wrongOwner); e == nil {
		t.Fatal("valid foreign-owner signature accepted")
	}
	bad := s
	bad.Identity = "0xdead"
	if _, e = a.verifySigned(p, bad); e == nil {
		t.Fatal("identity mismatch")
	}
	bad = s
	bad.Bytes = append([]byte(" "), s.Bytes...)
	if _, e = a.verifySigned(p, bad); e == nil {
		t.Fatal("noncanonical envelope")
	}
	bad = s
	bad.Auxiliary = json.RawMessage(`{}`)
	if _, e = a.verifySigned(p, bad); e == nil {
		t.Fatal("unexpected auxiliary")
	}
}

type coreNode struct {
	a              *Adapter
	server         *httptest.Server
	override       map[string]any
	fail           string
	role           string
	abstraction    string
	action         action
	owner          string
	hash           string
	ledger         bool
	executionError json.RawMessage
	ambiguous      bool
	submitCount    int
	mu             sync.Mutex
	intercept      func(string, map[string]json.RawMessage) (any, bool)
}

func fakeCore(t *testing.T) *coreNode {
	t.Helper()
	f := &coreNode{override: map[string]any{}, role: "user", abstraction: "default", hash: "0x" + strings.Repeat("b", 64), executionError: json.RawMessage(`null`)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&req)
		var typ string
		_ = json.Unmarshal(req["type"], &typ)
		if r.URL.Path == "/exchange" {
			typ = "exchange"
		}
		if typ == f.fail {
			w.WriteHeader(503)
			return
		}
		if f.intercept != nil {
			if out, ok := f.intercept(typ, req); ok {
				_ = json.NewEncoder(w).Encode(out)
				return
			}
		}
		if v, ok := f.override[typ]; ok {
			_ = json.NewEncoder(w).Encode(v)
			return
		}
		var out any
		switch typ {
		case "spotMeta":
			out = map[string]any{"tokens": []any{map[string]any{"name": "HYPE", "weiDecimals": 8, "index": 1105, "tokenId": strings.TrimPrefix(testToken, "HYPE:")}}}
		case "exchangeStatus":
			out = map[string]any{"time": time.Now().UnixMilli()}
		case "userRole":
			out = map[string]string{"role": f.role}
		case "userAbstraction":
			out = f.abstraction
		case "spotClearinghouseState":
			out = map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "10.00000000", "hold": "1.0"}}}
		case "exchange":
			f.mu.Lock()
			f.submitCount++
			_ = json.Unmarshal(req["action"], &f.action)
			ambiguous := f.ambiguous
			f.mu.Unlock()
			if ambiguous {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			out = map[string]any{"status": "ok", "response": map[string]string{"type": "default"}}
		case "userDetails":
			txs := []any{}
			if f.owner != "" {
				txs = append(txs, map[string]any{"time": f.action.Time, "user": f.owner, "action": f.action, "block": 123, "hash": f.hash, "error": f.executionError})
			}
			out = map[string]any{"type": "userDetails", "txs": txs}
		case "txDetails":
			out = map[string]any{"type": "txDetails", "tx": map[string]any{"time": f.action.Time, "user": f.owner, "action": f.action, "block": 123, "hash": f.hash, "error": f.executionError}}
		case "userNonFundingLedgerUpdates":
			out = []any{}
			if f.ledger {
				out = []any{map[string]any{"time": f.action.Time, "hash": f.hash, "delta": map[string]any{"type": "spotTransfer", "token": testToken, "amount": f.action.Amount, "user": f.owner, "destination": f.action.Destination, "fee": "0", "nativeTokenFee": "0"}}}
			}
		default:
			t.Errorf("unexpected %s %s", r.URL.Path, typ)
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.server.Close)
	f.a, _ = NewWithEndpoints(map[string]string{"hypercore-testnet": f.server.URL, "hypercore-testnet:explorer": f.server.URL})
	return f
}
func (f *coreNode) prepare(t *testing.T) (model.Plan, error) {
	t.Helper()
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	n, _ := network(ac.Network)
	return f.a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: n.Assets[0].ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "12345678"})
}
func TestTP050CorePrepareSubmitEvidence(t *testing.T) {
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	balances, e := f.a.Balance(context.Background(), p.Account)
	if e != nil || balances[0].Atomic != "1000000000" {
		t.Fatal(balances, e)
	}
	s, e := f.a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.a.Submit(context.Background(), p, s)
	if e != nil || r.State != "SUBMITTED" {
		t.Fatal(r, e)
	}
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" {
		t.Fatal(r, e)
	}
	f.owner = p.Account.Address
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" {
		t.Fatal("absence of ledger must remain unknown", r, e)
	}
	f.ledger = true
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "CONFIRMED" {
		t.Fatal(r, e)
	}
	f.executionError = json.RawMessage(`"Insufficient balance"`)
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" {
		t.Fatal("execution error needs ledger inspection", r, e)
	}
}
func TestTP060CoreFailureAmbiguityAndNonceConcurrency(t *testing.T) {
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	f.ambiguous = true
	r, e := f.a.Submit(context.Background(), p, s)
	f.mu.Lock()
	count := f.submitCount
	f.ambiguous = false
	f.mu.Unlock()
	if e == nil || r.State != "UNKNOWN" || count != 1 {
		t.Fatal("ambiguous acceptance triggered duplicate/rejection", r, e)
	}
	for _, method := range []string{"spotMeta", "exchangeStatus", "userRole", "userAbstraction", "spotClearinghouseState"} {
		f.fail = method
		if _, e := f.prepare(t); e == nil {
			t.Fatal("state failure accepted", method)
		}
	}
	f.fail = ""
	for _, role := range []string{"missing", "agent", "vault", "subAccount", "unknown"} {
		f.role = role
		if _, e := f.prepare(t); e == nil {
			t.Fatal("role accepted", role)
		}
	}
	f.role = "user"
	f.abstraction = "unifiedAccount"
	if _, e := f.prepare(t); e == nil {
		t.Fatal("unsupported collateral spendability accepted")
	}
	f.abstraction = "default"
	var wg sync.WaitGroup
	nonces := make(chan string, 100)
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Go(func() {
			p, e := f.prepare(t)
			if e != nil {
				errs <- e
			} else {
				nonces <- p.Nonce
			}
		})
	}
	wg.Wait()
	close(nonces)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for n := range nonces {
		if seen[n] {
			t.Fatal("duplicate Core nonce")
		}
		seen[n] = true
	}
}
func TestTP052CoreHostileMetadataAndEffects(t *testing.T) {
	for _, tc := range []struct {
		typ string
		out any
	}{{"spotMeta", map[string]any{"tokens": []any{}}}, {"spotMeta", map[string]any{"tokens": []any{map[string]any{"name": "HYPE", "weiDecimals": 18, "index": 1105, "tokenId": strings.TrimPrefix(testToken, "HYPE:")}}}}, {"exchangeStatus", map[string]any{"time": 1}}, {"spotClearinghouseState", map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "0.1", "hold": "0"}}}}, {"spotClearinghouseState", map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 1105, "total": "1", "hold": "2"}}}}} {
		f := fakeCore(t)
		f.override[tc.typ] = tc.out
		if _, e := f.prepare(t); e == nil {
			t.Fatal("untrustworthy state accepted", tc.typ)
		}
	}
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	f.override["exchange"] = map[string]any{"status": "err", "response": "secret-like attacker message"}
	r, e := f.a.Submit(context.Background(), p, s)
	if e == nil || r.State != "UNKNOWN" || strings.Contains(e.Error(), "attacker") {
		t.Fatal(r, e)
	}
	f.override["exchange"] = map[string]any{"status": "ok", "response": map[string]any{"type": "order"}}
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("wrong acknowledgement")
	}
	f.fail = "userDetails"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("missing explorer")
	}
}
func TestTP022CoreAccountDescriptorAdversaries(t *testing.T) {
	a := New()
	p := fixturePlan(t, a, "hypercore-testnet")
	for _, mutate := range []func(*model.Account){func(ac *model.Account) { ac.Family = "evm" }, func(ac *model.Account) { ac.ID = "other" }, func(ac *model.Account) { ac.Parameters = json.RawMessage(`{}`) }, func(ac *model.Account) { ac.PublicKey = "0x" + strings.Repeat("00", 65) }, func(ac *model.Account) { ac.Address = "bad" }, func(ac *model.Account) { ac.Network = "unknown" }} {
		ac := p.Account
		mutate(&ac)
		if _, e := a.validateAccount(ac); e == nil {
			t.Fatal("descriptor accepted")
		}
	}
}
func FuzzTP012CoreAction(f *testing.F) {
	f.Add([]byte(`{"action":{"type":"spotSend"}}`), "0.00000001")
	f.Fuzz(func(t *testing.T, raw []byte, v string) {
		if len(raw) > 8192 || len(v) > 1024 {
			return
		}
		_, _ = parseUnits(v)
		var p model.Plan
		if json.Unmarshal(raw, &p) == nil {
			_ = New().ValidatePlan(p)
		}
	})
}
