package evm

import (
	"context"
	"encoding/hex"
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

func TestTP042ReconstructionRejectsAllChangedEffects(t *testing.T) {
	a := New()
	p := fixturePlan(t, a, "ethereum-sepolia")
	for _, mutate := range []func(*model.Plan){func(q *model.Plan) { q.Asset = "ETH" }, func(q *model.Plan) { q.Recipient = "invalid" }, func(q *model.Plan) { q.Recipient = "0x2222222222222222222222222222222222222222" }, func(q *model.Plan) { q.Recipient = "0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229" }, func(q *model.Plan) { q.Recipient = strings.ToLower("0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229") }, func(q *model.Plan) { q.Amount = "0" }, func(q *model.Plan) { q.Nonce = "-1" }, func(q *model.Plan) { q.Payload = json.RawMessage(`{"chainId":"1","chainId":"2"}`) }, func(q *model.Plan) { q.Payload = json.RawMessage(`{"notAllowed":1}`) }} {
		q := p
		mutate(&q)
		q.ID = planID(q)
		if a.ValidatePlan(q) == nil {
			t.Fatal("changed effect accepted")
		}
		if _, e := a.Sign(syntheticSeed(), q); e == nil {
			t.Fatal("invalid signing plan accepted")
		}
	}
	for _, mutate := range []func(*payload){func(x *payload) { x.DataFeeReserve = "-1" }, func(x *payload) { x.OperatorFeeReserve = "-1" }, func(x *payload) { x.StateHash = "0x" + strings.Repeat("g", 64) }, func(x *payload) { x.FeeCap = "-1" }} {
		q := p
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		mutate(&x)
		q.Payload, _ = json.Marshal(x)
		q.ID = planID(q)
		if a.ValidatePlan(q) == nil {
			t.Fatal("invalid payload quantity")
		}
	}
	f := fakeNode(t, "base-sepolia")
	q, e := f.prepare(t, "base-sepolia", true)
	if e != nil {
		t.Fatal(e)
	}
	var x payload
	_ = json.Unmarshal(q.Payload, &x)
	x.Data = "0x095ea7b3"
	q.Payload, _ = json.Marshal(x)
	q.ID = planID(q)
	if f.a.ValidatePlan(q) == nil {
		t.Fatal("ERC-20 approval accepted as transfer")
	}
	if _, e := decimal(new(big.Int).Lsh(big.NewInt(1), 256).String()); e == nil {
		t.Fatal("256-bit overflow accepted")
	}
	if a.ValidateAddress("other", "0x1111111111111111111111111111111111111111") == nil {
		t.Fatal("unknown chain")
	}
}
func TestTP052PreparationRejectsInvalidIntent(t *testing.T) {
	f := fakeNode(t, "ethereum-sepolia")
	ac, _ := f.a.Derive(syntheticSeed(), 0, "ethereum-sepolia")
	base := model.Transfer{AccountID: ac.ID, Asset: "ethereum-sepolia:native", Recipient: "0x1111111111111111111111111111111111111111", Amount: "1"}
	for _, mutate := range []func(*model.Transfer){func(t *model.Transfer) { t.AccountID = "foreign" }, func(t *model.Transfer) { t.Asset = "ETH" }, func(t *model.Transfer) { t.Memo = "memo" }, func(t *model.Transfer) { t.Recipient = "invalid" }, func(t *model.Transfer) { t.Recipient = "0x2222222222222222222222222222222222222222" }, func(t *model.Transfer) { t.Recipient = "0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229" }, func(t *model.Transfer) { t.Amount = "0" }, func(t *model.Transfer) { t.Amount = "-1" }} {
		bad := base
		mutate(&bad)
		if _, e := f.a.Prepare(context.Background(), ac, bad); e == nil {
			t.Fatal("unsafe intent prepared")
		}
	}
	badAc := ac
	badAc.Profile = "other"
	if _, e := f.a.Prepare(context.Background(), badAc, base); e == nil {
		t.Fatal("unsafe account prepared")
	}
	if _, e := f.a.Balance(context.Background(), badAc); e == nil {
		t.Fatal("invalid account balance")
	}
	f.rpcError = "eth_chainId"
	if _, e := f.a.Balance(context.Background(), ac); e == nil {
		t.Fatal("wrong chain balance")
	}
	f.rpcError = "eth_getBalance"
	if _, e := f.a.Balance(context.Background(), ac); e == nil {
		t.Fatal("failed balance shown as zero")
	}
}
func TestTP052TokenAndGasStateFailures(t *testing.T) {
	for _, code := range []string{"0x", "0x0", "0xzz", "0x6000"} {
		f := fakeNode(t, "base-sepolia")
		n, _ := network("base-sepolia")
		f.override["eth_getCode"] = code
		if f.a.tokenIdentity(context.Background(), n, n.Assets[1], "latest") == nil {
			t.Fatal("missing/malformed token code")
		}
	}
	for _, method := range []string{"eth_getCode", "eth_call"} {
		f := fakeNode(t, "base-sepolia")
		n, _ := network("base-sepolia")
		f.rpcError = method
		if f.a.tokenIdentity(context.Background(), n, n.Assets[1], "latest") == nil {
			t.Fatal("missing token identity")
		}
		ac, _ := f.a.Derive(syntheticSeed(), 0, n.ID)
		if _, e := f.a.balance(context.Background(), n, ac, n.Assets[1], "latest"); e == nil {
			t.Fatal("failed token balance accepted")
		}
	}
	for _, tc := range []struct {
		selector string
		result   any
	}{{"0xa9059cbb", "0x" + strings.Repeat("0", 64)}, {"0x70a08231", "0x" + strings.Repeat("0", 64)}, {"0xf1c7a58b", "0x01"}} {
		f := fakeNode(t, "base-sepolia")
		f.intercept = func(method string, args []json.RawMessage) (any, bool) {
			if method == "eth_call" {
				var msg map[string]string
				_ = json.Unmarshal(args[0], &msg)
				if strings.HasPrefix(msg["data"], tc.selector) {
					return tc.result, true
				}
			}
			return nil, false
		}
		if _, e := f.prepare(t, "base-sepolia", true); e == nil {
			t.Fatal("unsafe token/rollup state prepared", tc.selector)
		}
	}
	f := fakeNode(t, "base-sepolia")
	n, _ := network("base-sepolia")
	ac, _ := f.a.Derive(syntheticSeed(), 0, n.ID)
	f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		if method == "eth_call" {
			var msg map[string]string
			_ = json.Unmarshal(args[0], &msg)
			if strings.HasPrefix(msg["data"], "0x70a08231") {
				return map[string]int{}, true
			}
		}
		return nil, false
	}
	if _, e := f.a.Balance(context.Background(), ac); e == nil {
		t.Fatal("malformed token balance")
	}
	for _, b := range []map[string]string{{"number": "0xz", "hash": "x", "timestamp": "0x1", "baseFeePerGas": "0x1"}, {"number": "0x1", "hash": "0x" + strings.Repeat("a", 64), "timestamp": quantity(big.NewInt(time.Now().Unix())), "baseFeePerGas": "0xz"}} {
		f := fakeNode(t, "ethereum-sepolia")
		f.override["eth_getBlockByNumber"] = b
		if _, e := f.prepare(t, "ethereum-sepolia", false); e == nil {
			t.Fatal("malformed block accepted")
		}
	}
	f = fakeNode(t, "ethereum-sepolia")
	f.override["eth_maxPriorityFeePerGas"] = "0x" + strings.Repeat("f", 64)
	if _, e := f.prepare(t, "ethereum-sepolia", false); e == nil {
		t.Fatal("fee overflow")
	}
}

func TestTP060PendingSpendability(t *testing.T) {
	for _, token := range []bool{false, true} {
		f := fakeNode(t, "base-sepolia")
		f.intercept = func(method string, args []json.RawMessage) (any, bool) {
			if len(args) > 1 && string(args[1]) == `"pending"` {
				if !token && method == "eth_getBalance" {
					return "0x0", true
				}
				if token && method == "eth_call" {
					var call map[string]string
					_ = json.Unmarshal(args[0], &call)
					if strings.HasPrefix(call["data"], "0x70a08231") {
						return "0x" + strings.Repeat("0", 64), true
					}
				}
			}
			return nil, false
		}
		if _, e := f.prepare(t, "base-sepolia", token); e == nil {
			t.Fatal("pending outflow ignored")
		}
	}
}

func TestTP052MissingRollupDataFee(t *testing.T) {
	f := fakeNode(t, "base-sepolia")
	p, e := f.prepare(t, "base-sepolia", false)
	if e != nil {
		t.Fatal(e)
	}
	var x payload
	_ = json.Unmarshal(p.Payload, &x)
	fee, _ := decimal(p.MaxFee)
	data, _ := decimal(x.DataFeeReserve)
	fee.Sub(fee, data)
	p.MaxFee = fee.String()
	x.DataFeeReserve = "0"
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if f.a.ValidatePlan(p) == nil {
		t.Fatal("unknown/zero rollup data fee accepted")
	}
}
func receiptFor(s model.Signed) map[string]any {
	return map[string]any{"transactionHash": s.Identity, "blockHash": "0x" + strings.Repeat("a", 64), "blockNumber": "0x10", "status": "0x1", "logs": []any{}}
}
func TestTP060SubmissionAndReconciliationFailures(t *testing.T) {
	f := fakeNode(t, "ethereum-sepolia")
	p, e := f.prepare(t, "ethereum-sepolia", false)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	bad := s
	bad.Identity = "bad"
	if _, e = f.a.Submit(context.Background(), p, bad); e == nil {
		t.Fatal("invalid submission")
	}
	if _, e = f.a.Reconcile(context.Background(), p, bad); e == nil {
		t.Fatal("invalid reconcile")
	}
	f.rpcError = "eth_chainId"
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("wrong submission chain")
	}
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("wrong reconcile chain")
	}
	f.rpcError = ""
	q := p
	q.Expires = time.Now().Add(-time.Hour).Unix()
	q.ID = planID(q)
	if _, e = f.a.Submit(context.Background(), q, s); e == nil {
		t.Fatal("expired broadcast")
	}
	f.override["eth_sendRawTransaction"] = "0x" + strings.Repeat("b", 64)
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("foreign transaction response")
	}
	delete(f.override, "eth_sendRawTransaction")
	for _, method := range []string{"eth_getTransactionReceipt", "eth_getTransactionByHash", "eth_getTransactionCount"} {
		f.rpcError = method
		if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
			t.Fatal("missing reconciliation dependency", method)
		}
	}
	f.rpcError = ""
	f.override["eth_getTransactionCount"] = "0xz"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("invalid consumed nonce")
	}
	f.override["eth_getTransactionCount"] = "0x8"
	r, e := f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "UNKNOWN" || !strings.Contains(r.Detail, "nonce consumed") {
		t.Fatal(r, e)
	}
	delete(f.override, "eth_getTransactionCount")
	for _, known := range []any{false, map[string]string{"hash": "0x" + strings.Repeat("c", 64)}} {
		f.known = known
		if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
			t.Fatal("unbound known transaction response")
		}
	}
	f.known = nil
	f.override["eth_getTransactionCount"] = "0x10000000000000000"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("consumed nonce overflow")
	}
	delete(f.override, "eth_getTransactionCount")
	for key, value := range map[string]any{"transactionHash": "bad", "blockNumber": "0xz", "status": "0x2", "blockHash": "bad"} {
		f.receipt = receiptFor(s)
		f.receipt.(map[string]any)[key] = value
		if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
			t.Fatal("malformed receipt accepted", key)
		}
	}
	f.receipt = receiptFor(s)
	f.rpcError = "eth_getBlockByNumber"
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("canonical block unavailable")
	}
	f.rpcError = ""
	for _, final := range []any{nil, map[string]string{"number": "0xz"}, map[string]string{"number": "0x1"}} {
		f.intercept = func(method string, params []json.RawMessage) (any, bool) {
			if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
				return final, true
			}
			return nil, false
		}
		r, e = f.a.Reconcile(context.Background(), p, s)
		if final != nil && final.(map[string]string)["number"] == "0xz" {
			if e == nil {
				t.Fatal("malformed finality")
			}
		} else if e != nil || r.State != "SUBMITTED" {
			t.Fatal(r, e)
		}
	}

	f.intercept = func(method string, params []json.RawMessage) (any, bool) {
		if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
			return map[string]string{"number": "0x1"}, true
		}
		return nil, false
	}
	f.receipt.(map[string]any)["status"] = "0x0"
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "SUBMITTED" || !strings.Contains(r.Detail, "reverted") {
		t.Fatal("unfinalized failure released reservation", r, e)
	}
	f.intercept = func(method string, params []json.RawMessage) (any, bool) {
		if method == "eth_getBlockByNumber" && string(params[0]) == `"finalized"` {
			return "malformed", true
		}
		return nil, false
	}
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "SUBMITTED" || !strings.Contains(r.Detail, "finality unavailable") {
		t.Fatal("unavailable finality misclassified", r, e)
	}
	f = fakeNode(t, "base-sepolia")
	p, e = f.prepare(t, "base-sepolia", true)
	if e != nil {
		t.Fatal(e)
	}
	s, _ = f.a.Sign(syntheticSeed(), p)
	f.receipt = receiptFor(s)
	if _, e = f.a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("missing token transfer event")
	}
	f.rpcError = "eth_call"
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("missing current rollup fee state")
	}
	f.rpcError = ""
	f.override["eth_call"] = "0x" + hex.EncodeToString(word(big.NewInt(100_000)))
	if _, e = f.a.Submit(context.Background(), p, s); e == nil {
		t.Fatal("rollup fee reserve exceeded")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("synthetic read failure") }
func (errorReader) Close() error             { return nil }

type readErrorTransport struct{}

func (readErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errorReader{}, Header: http.Header{}}, nil
}
func TestTP041RPCReadAndRedirect(t *testing.T) {
	a := New()
	n, _ := network("ethereum-sepolia")
	a.HTTPClient = &http.Client{Transport: readErrorTransport{}}
	var raw string
	if a.rpc(context.Background(), n, "test", nil, &raw) == nil {
		t.Fatal("short read accepted")
	}
	a.endpoints[n.ID] = "http://example.org"
	if a.rpc(context.Background(), n, "test", nil, &raw) == nil {
		t.Fatal("invalid endpoint accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.org")
		w.WriteHeader(302)
	}))
	defer server.Close()
	a, _ = NewWithEndpoints(map[string]string{n.ID: server.URL})
	if a.rpc(context.Background(), n, "test", nil, &raw) == nil {
		t.Fatal("redirect followed")
	}
}
