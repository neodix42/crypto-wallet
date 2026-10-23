package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"io"
	"localwallet/internal/model"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func syntheticSeed() []byte {
	b := make([]byte, 64)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
func fixturePlan(t *testing.T, a *Adapter, net string) model.Plan {
	t.Helper()
	ac, e := a.Derive(syntheticSeed(), 0, net)
	if e != nil {
		t.Fatal(e)
	}
	n, _ := network(net)
	p := model.Plan{Account: ac, Asset: n.Assets[0].ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789", MaxFee: "50400000025200", FeeAsset: n.Assets[0].ID, Nonce: "7", Expires: time.Now().Add(time.Minute).Unix()}
	x := payload{ChainID: n.ChainID, To: p.Recipient, Value: p.Amount, Data: "0x", Gas: "25200", FeeCap: "2000000001", TipCap: "1", DataFeeReserve: "0", OperatorFeeReserve: "0", StateBlock: "0x10", StateHash: "0x" + strings.Repeat("a", 64), StateTime: fmt.Sprintf("0x%x", time.Now().Unix())}
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	return p
}
func TestTP002IndependentEthersFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../tests/oracles/evm/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Seed     string `json:"seed"`
		Fixtures []struct {
			Index     uint32 `json:"index"`
			Address   string `json:"address"`
			PublicKey string `json:"publicKey"`
			Unsigned  string `json:"unsigned"`
			Signed    string `json:"signed"`
			Hash      string `json:"hash"`
		} `json:"fixtures"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	seed, _ := hex.DecodeString(strings.TrimPrefix(f.Seed, "0x"))
	a := New()
	for _, v := range f.Fixtures {
		ac, e := a.Derive(seed, v.Index, "ethereum-sepolia")
		if e != nil || ac.Address != v.Address || ac.PublicKey != v.PublicKey {
			t.Fatalf("index %d public derivation mismatch", v.Index)
		}
		p := fixturePlan(t, a, "ethereum-sepolia")
		p.Account = ac
		p.ID = planID(p)
		s, e := a.Sign(seed, p)
		if e != nil {
			t.Fatal(e)
		}
		if "0x"+hex.EncodeToString(s.Bytes) != v.Signed || s.Identity != v.Hash {
			t.Fatalf("index %d independent transaction fixture mismatch", v.Index)
		}
	}
}
func TestTP022RegistryAndDescriptors(t *testing.T) {
	a := New()
	if len(a.Networks()) != 8 {
		t.Fatal("coverage narrowed")
	}
	for _, n := range a.Networks() {
		for _, idx := range []uint32{0, 1, 127} {
			ac, e := a.Derive(syntheticSeed(), idx, n.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = a.validateAccount(ac); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, s := range [][]byte{nil, make([]byte, 32), make([]byte, 65)} {
		if _, e := a.Derive(s, 0, "ethereum-sepolia"); e == nil {
			t.Fatal("seed width accepted")
		}
	}
	if _, e := a.Derive(syntheticSeed(), 1<<31, "ethereum-sepolia"); e == nil {
		t.Fatal("hardened account index accepted")
	}
	if _, e := a.Derive(syntheticSeed(), 0, "other"); e == nil {
		t.Fatal("unknown network")
	}
	if _, e := NewWithEndpoints(map[string]string{"other": "https://localhost"}); e == nil {
		t.Fatal("unknown endpoint")
	}
	if _, e := NewWithEndpoints(map[string]string{"ethereum-sepolia": "http://example.org"}); e == nil {
		t.Fatal("plaintext endpoint")
	}
	ac, _ := a.Derive(syntheticSeed(), 0, "ethereum-sepolia")
	for _, mutate := range []func(*model.Account){func(x *model.Account) { x.Profile = "other" }, func(x *model.Account) { x.Family = "core" }, func(x *model.Account) { x.ID = "other" }, func(x *model.Account) { x.Parameters = json.RawMessage(`{}`) }, func(x *model.Account) { x.PublicKey = "0x00" }, func(x *model.Account) { x.PublicKey = "0x" + strings.Repeat("00", 65) }, func(x *model.Account) { x.Address = "invalid" }} {
		bad := ac
		mutate(&bad)
		if _, e := a.validateAccount(bad); e == nil {
			t.Fatal("invalid descriptor accepted")
		}
	}
}

func TestTP002AllEVMNetworkDomainFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../tests/oracles/evm/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		NetworkFixtures []struct {
			Index   uint32 `json:"index"`
			Network string `json:"network"`
			Signed  string `json:"signed"`
			Hash    string `json:"hash"`
		} `json:"networkFixtures"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if len(f.NetworkFixtures) != 24 {
		t.Fatal("network fixture coverage narrowed")
	}
	a := New()
	for _, v := range f.NetworkFixtures {
		p := fixturePlan(t, a, v.Network)
		p.Account, e = a.Derive(syntheticSeed(), v.Index, v.Network)
		if e != nil {
			t.Fatal(e)
		}
		n, _ := network(v.Network)
		if rollup(n) {
			var x payload
			_ = json.Unmarshal(p.Payload, &x)
			x.DataFeeReserve = "1"
			p.Payload, _ = json.Marshal(x)
			max, _ := decimal(p.MaxFee)
			p.MaxFee = max.Add(max, big.NewInt(1)).String()
		}
		p.ID = planID(p)
		s, e := a.Sign(syntheticSeed(), p)
		if e != nil {
			t.Fatal(v.Network, e)
		}
		if "0x"+hex.EncodeToString(s.Bytes) != v.Signed || s.Identity != v.Hash {
			t.Fatal("independent chain domain bytes mismatch", v.Network, v.Index)
		}
	}
}

func TestTP002IndependentZENTokenTransactionFixtures(t *testing.T) {
	raw, e := os.ReadFile("../../../tests/oracles/evm/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		TokenFixtures []struct {
			Index    uint32 `json:"index"`
			Network  string `json:"network"`
			Contract string `json:"contract"`
			Data     string `json:"data"`
			Signed   string `json:"signed"`
			Hash     string `json:"hash"`
		} `json:"tokenFixtures"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	if len(f.TokenFixtures) != 12 {
		t.Fatal("ZEN fixture coverage narrowed")
	}
	a := New()
	for _, v := range f.TokenFixtures {
		p := fixturePlan(t, a, v.Network)
		p.Account, e = a.Derive(syntheticSeed(), v.Index, v.Network)
		if e != nil {
			t.Fatal(e)
		}
		n, _ := network(v.Network)
		p.Asset = n.Assets[1].ID
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		value, _ := decimal(p.Amount)
		x.To = n.Assets[1].Contract
		x.Value = "0"
		x.Data = transferData(p.Recipient, value)
		x.Gas = "60000"
		x.DataFeeReserve = "1"
		if x.Data != v.Data || x.To != v.Contract {
			t.Fatal("independent ERC-20 construction mismatch")
		}
		p.Payload, _ = json.Marshal(x)
		p.MaxFee = "120000000060001"
		p.ID = planID(p)
		s, e := a.Sign(syntheticSeed(), p)
		if e != nil {
			t.Fatal(e)
		}
		if "0x"+hex.EncodeToString(s.Bytes) != v.Signed || s.Identity != v.Hash {
			t.Fatal("ZEN transaction bytes/signature mismatch", v.Network, v.Index)
		}
	}
}
func TestTP052AddressAndNumbers(t *testing.T) {
	a := New()
	for _, s := range []string{"0x52908400098527886E0F7030069857D2E4169EE7", "0xde709f2102306220921060314715629080e2fb77", "0x5AEDA56215b167893e80B4fE645BA6d5Bab767DE"} {
		if e := a.ValidateAddress("ethereum-mainnet", s); e != nil {
			t.Fatalf("valid address %s: %v", s, e)
		}
	}
	for _, s := range []string{"", "0x0000000000000000000000000000000000000000", "0x5Aeda56215b167893e80B4fE645BA6d5Bab767De", "0Xde709f2102306220921060314715629080e2fb77", strings.Repeat("f", 42)} {
		if a.ValidateAddress("ethereum-mainnet", s) == nil {
			t.Fatal("invalid address accepted")
		}
	}
	for _, s := range []string{"", "01", "-1", "+1", "1.0", "1e2", strings.Repeat("9", 79)} {
		if _, e := decimal(s); e == nil {
			t.Fatalf("bad integer %s", s)
		}
	}
	for _, s := range []string{"0x", "0X1", "0x01", "0xG1", "0xAA", strings.Repeat("f", 80)} {
		if _, e := hexInteger(s); e == nil {
			t.Fatal("bad RPC integer")
		}
	}
	if _, e := decimal("0"); e != nil {
		t.Fatal(e)
	}
	if _, e := uint64Decimal("18446744073709551616"); e == nil {
		t.Fatal("nonce overflow")
	}
	if _, e := abiInteger("0x01"); e == nil {
		t.Fatal("short ABI")
	}
	if _, e := abiInteger("0x" + strings.Repeat("z", 64)); e == nil {
		t.Fatal("nonhex ABI")
	}
	for _, ep := range []string{"https://example.org/rpc", "http://127.0.0.1:1234", "http://[::1]:1234", "http://localhost:1234"} {
		if ValidateEndpoint(ep) != nil {
			t.Fatal(ep)
		}
	}
	for _, ep := range []string{"", "https://user:pass@example.org", "https://example.org/#x", "file:///tmp/rpc", "http://example.org", "http://127.0.0.2.example.org"} {
		if ValidateEndpoint(ep) == nil {
			t.Fatal(ep)
		}
	}
}
func TestTP042PlanAndSignatureTampering(t *testing.T) {
	a := New()
	p := fixturePlan(t, a, "ethereum-sepolia")
	s, e := a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.verifySigned(p, s); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*model.Plan){func(x *model.Plan) { x.Recipient = "0x2222222222222222222222222222222222222223" }, func(x *model.Plan) { x.Amount = "2" }, func(x *model.Plan) { x.Nonce = "9" }, func(x *model.Plan) { x.MaxFee = "0" }, func(x *model.Plan) { x.Memo = "x" }, func(x *model.Plan) { x.Account.Network = "ethereum-mainnet" }} {
		q := p
		mutate(&q)
		if a.ValidatePlan(q) == nil {
			t.Fatal("altered plan accepted")
		}
		q.ID = planID(q)
		if _, e = a.verifySigned(q, s); e == nil {
			t.Fatal("signature accepted changed effects")
		}
	}
	for _, mutate := range []func(*payload){func(x *payload) { x.ChainID = "1" }, func(x *payload) { x.Data = "0x1234" }, func(x *payload) { x.To = "0x2222222222222222222222222222222222222223" }, func(x *payload) { x.Value = "0" }, func(x *payload) { x.Gas = "1" }, func(x *payload) { x.Gas = "30000001" }, func(x *payload) { x.FeeCap = "0" }, func(x *payload) { x.TipCap = "999999999999" }, func(x *payload) { x.DataFeeReserve = "1" }, func(x *payload) { x.OperatorFeeReserve = "1" }, func(x *payload) { x.StateHash = "bad" }, func(x *payload) { x.StateBlock = "0x00" }, func(x *payload) { x.StateTime = "0xz" }} {
		var x payload
		_ = json.Unmarshal(p.Payload, &x)
		mutate(&x)
		q := p
		q.Payload, _ = json.Marshal(x)
		q.ID = planID(q)
		if a.ValidatePlan(q) == nil {
			t.Fatalf("mutated payload accepted: %s", q.Payload)
		}
	}
	q := p
	q.Expires = time.Now().Add(-time.Hour).Unix()
	q.ID = planID(q)
	if _, e = a.Sign(syntheticSeed(), q); e == nil {
		t.Fatal("expired approval")
	}
	q = p
	q.Expires = time.Now().Add(time.Hour).Unix()
	q.ID = planID(q)
	if _, e = a.Sign(syntheticSeed(), q); e == nil {
		t.Fatal("far future approval")
	}
	other := syntheticSeed()
	other[0]++
	if _, e = a.Sign(other, p); e == nil {
		t.Fatal("foreign seed")
	}
	for _, mutate := range []func(*model.Signed){func(x *model.Signed) { x.Identity = "0xdead" }, func(x *model.Signed) { x.Bytes = []byte{2, 3} }, func(x *model.Signed) { x.Bytes = nil }, func(x *model.Signed) { x.Auxiliary = json.RawMessage(`{}`) }} {
		q := s
		mutate(&q)
		if _, e = a.verifySigned(p, q); e == nil {
			t.Fatal("invalid signed envelope")
		}
	}
}

type nodeFixture struct {
	a         *Adapter
	server    *httptest.Server
	override  map[string]any
	rpcError  string
	calls     map[string]int
	receipt   any
	known     any
	sent      string
	status    int
	intercept func(string, []json.RawMessage) (any, bool)
}

func deployedCode(t *testing.T, network string) string {
	t.Helper()
	raw, e := os.ReadFile("../../../tests/oracles/evm/deployed-tokens.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Networks []struct {
			Network string `json:"network"`
			Code    string `json:"code"`
		} `json:"networks"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	for _, n := range f.Networks {
		if n.Network == network {
			return n.Code
		}
	}
	return "0x6000"
}

func fakeNode(t *testing.T, net string) *nodeFixture {
	t.Helper()
	f := &nodeFixture{override: map[string]any{}, calls: map[string]int{}}
	n, _ := network(net)
	tokenCode := deployedCode(t, net)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.calls[req.Method]++
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if req.Method == f.rpcError {
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"hostile data never echoed"}}`)
			return
		}
		var result any
		var handled bool
		if f.intercept != nil {
			result, handled = f.intercept(req.Method, req.Params)
		}
		if handled {
		} else if v, ok := f.override[req.Method]; ok {
			result = v
		} else {
			switch req.Method {
			case "eth_chainId":
				chain, _ := decimal(n.ChainID)
				result = quantity(chain)
			case "eth_getBlockByNumber":
				result = map[string]string{"number": "0x10", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x3b9aca00"}
			case "eth_getCode":
				var addr string
				_ = json.Unmarshal(req.Params[0], &addr)
				if strings.EqualFold(addr, n.Assets[0].Contract) {
					result = "0x6000"
				} else if len(n.Assets) > 1 && strings.EqualFold(addr, n.Assets[1].Contract) {
					result = tokenCode
				} else {
					result = "0x"
				}
			case "eth_getTransactionCount":
				result = "0x7"
			case "eth_maxPriorityFeePerGas":
				result = "0x1"
			case "eth_getBalance":
				result = "0x3635c9adc5dea00000"
			case "eth_estimateGas":
				result = "0x5208"
			case "eth_call":
				var msg map[string]string
				_ = json.Unmarshal(req.Params[0], &msg)
				switch {
				case strings.HasPrefix(msg["data"], callData("decimals()")):
					result = "0x" + hex.EncodeToString(word(big.NewInt(18)))
				case strings.HasPrefix(msg["data"], callData("balanceOf(address)")[:10]):
					result = "0x" + hex.EncodeToString(word(new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)))
				case strings.HasPrefix(msg["data"], "0xa9059cbb"):
					result = "0x" + hex.EncodeToString(word(big.NewInt(1)))
				case strings.EqualFold(msg["to"], "0x420000000000000000000000000000000000000F"):
					result = "0x" + hex.EncodeToString(word(big.NewInt(1000)))
				default:
					result = "0x"
				}
			case "eth_sendRawTransaction":
				var raw string
				_ = json.Unmarshal(req.Params[0], &raw)
				b, _ := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
				var tx types.Transaction
				_ = tx.UnmarshalBinary(b)
				f.sent = tx.Hash().Hex()
				result = f.sent
			case "eth_getTransactionReceipt":
				result = f.receipt
			case "eth_getTransactionByHash":
				result = f.known
			default:
				t.Errorf("unexpected RPC %s", req.Method)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(f.server.Close)
	f.a, _ = NewWithEndpoints(map[string]string{net: f.server.URL})
	return f
}
func (f *nodeFixture) prepare(t *testing.T, net string, token bool) (model.Plan, error) {
	t.Helper()
	ac, _ := f.a.Derive(syntheticSeed(), 0, net)
	n, _ := network(net)
	idx := 0
	if token {
		idx = 1
	}
	return f.a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: n.Assets[idx].ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789"})
}
func TestTP050RPCPrepareSubmitReconcile(t *testing.T) {
	for _, net := range []string{"ethereum-sepolia", "hyperevm-testnet", "base-sepolia", "horizen-testnet"} {
		t.Run(net, func(t *testing.T) {
			f := fakeNode(t, net)
			for _, token := range []bool{false, true} {
				if token && len(f.a.Networks()[0].Assets) == 0 {
					t.Fatal("unreachable")
				}
				n, _ := network(net)
				if token && len(n.Assets) < 2 {
					continue
				}
				p, e := f.prepare(t, net, token)
				if e != nil {
					t.Fatal(e)
				}
				b, e := f.a.Balance(context.Background(), p.Account)
				if e != nil || len(b) != len(n.Assets) {
					t.Fatal(e)
				}
				s, e := f.a.Sign(syntheticSeed(), p)
				if e != nil {
					t.Fatal(e)
				}
				r, e := f.a.Submit(context.Background(), p, s)
				if e != nil || r.State != "SUBMITTED" || f.sent != s.Identity {
					t.Fatalf("submit: %+v %v", r, e)
				}
				r, e = f.a.Reconcile(context.Background(), p, s)
				if e != nil || r.State != "UNKNOWN" {
					t.Fatal(r, e)
				}
				f.known = map[string]string{"hash": s.Identity}
				r, e = f.a.Reconcile(context.Background(), p, s)
				if e != nil || r.State != "SUBMITTED" {
					t.Fatal(r, e)
				}
				f.receipt = map[string]any{"transactionHash": s.Identity, "blockHash": "0x" + strings.Repeat("a", 64), "blockNumber": "0x10", "status": "0x1", "logs": []any{}}
				if token {
					f.receipt.(map[string]any)["logs"] = []any{map[string]any{"address": n.Assets[1].Contract, "topics": []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), common.BytesToHash(common.HexToAddress(p.Account.Address).Bytes()).Hex(), common.BytesToHash(common.HexToAddress(p.Recipient).Bytes()).Hex()}, "data": "0x" + hex.EncodeToString(word(big.NewInt(123456789)))}}
				}
				r, e = f.a.Reconcile(context.Background(), p, s)
				if e != nil || r.State != "CONFIRMED" {
					t.Fatalf("reconcile %+v %v", r, e)
				}
				f.receipt.(map[string]any)["status"] = "0x0"
				r, e = f.a.Reconcile(context.Background(), p, s)
				if e != nil || r.State != "FAILED" {
					t.Fatal(r, e)
				}
				f.receipt.(map[string]any)["blockHash"] = "0x" + strings.Repeat("b", 64)
				r, e = f.a.Reconcile(context.Background(), p, s)
				if e != nil || r.State != "REORGED" {
					t.Fatal(r, e)
				}
				f.receipt = nil
				f.known = nil
			}
		})
	}
}
func TestTP052RPCAdversarial(t *testing.T) {
	for _, method := range []string{"eth_chainId", "eth_getBlockByNumber", "eth_getCode", "eth_getTransactionCount", "eth_maxPriorityFeePerGas", "eth_call", "eth_estimateGas", "eth_getBalance"} {
		t.Run(method, func(t *testing.T) {
			f := fakeNode(t, "ethereum-sepolia")
			f.rpcError = method
			if _, e := f.prepare(t, "ethereum-sepolia", false); e == nil {
				t.Fatal("failed dependency accepted")
			}
		})
	}
	for _, tc := range []struct {
		method string
		value  any
	}{{"eth_chainId", "0x1"}, {"eth_getTransactionCount", "0x10000000000000000"}, {"eth_maxPriorityFeePerGas", "0xz"}, {"eth_estimateGas", "0x1"}, {"eth_getBalance", "0x0"}, {"eth_getCode", "0x6000"}, {"eth_getBlockByNumber", map[string]string{"number": "0x1", "hash": "0x" + strings.Repeat("a", 64), "timestamp": "0x1", "baseFeePerGas": "0x1"}}} {
		f := fakeNode(t, "ethereum-sepolia")
		f.override[tc.method] = tc.value
		if _, e := f.prepare(t, "ethereum-sepolia", false); e == nil {
			t.Fatal("malicious state accepted", tc.method)
		}
	}
	f := fakeNode(t, "base-sepolia")
	f.override["eth_call"] = "0x" + strings.Repeat("0", 64)
	if _, e := f.prepare(t, "base-sepolia", true); e == nil {
		t.Fatal("false token/decimals result")
	}
	f = fakeNode(t, "base-sepolia")
	f.rpcError = "eth_call"
	if _, e := f.prepare(t, "base-sepolia", false); e == nil {
		t.Fatal("missing rollup estimate")
	}
	f = fakeNode(t, "ethereum-sepolia")
	p, e := f.prepare(t, "ethereum-sepolia", false)
	if e != nil {
		t.Fatal(e)
	}
	s, _ := f.a.Sign(syntheticSeed(), p)
	f.rpcError = "eth_sendRawTransaction"
	r, e := f.a.Submit(context.Background(), p, s)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("timeout was treated as rejection", r, e)
	}
	if f.calls["eth_sendRawTransaction"] != 1 {
		t.Fatal("automatic duplicate attempt")
	}
}
func TestTP012StrictJSON(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":1} {}`, `{"a":`, strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34), `{1:2}`, `[}`, strings.Repeat(" ", 2<<20) + "0"} {
		var out any
		if StrictJSON([]byte(raw), &out) == nil {
			t.Fatal("malformed JSON accepted")
		}
	}
	var x struct {
		A int `json:"a"`
	}
	if StrictJSON([]byte(`{"b":1}`), &x) == nil {
		t.Fatal("unknown field accepted")
	}
	if StrictJSON([]byte(`{"a":1}`), &x) != nil || x.A != 1 {
		t.Fatal("valid schema rejected")
	}
}

type brokenTransport struct{}

func (brokenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic transport failure")
}
func TestTP041RPCEnvelope(t *testing.T) {
	for _, raw := range []string{`{"jsonrpc":"1.0","id":1,"result":"0x1"}`, `{"jsonrpc":"2.0","id":2,"result":"0x1"}`, `{"jsonrpc":"2.0","id":1}`, `{"jsonrpc":"2.0","id":1,"result":1,"result":2}`, `{"jsonrpc":"2.0","id":1,"result":{}}`, strings.Repeat("x", (2<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, raw) }))
		a, _ := NewWithEndpoints(map[string]string{"ethereum-sepolia": server.URL})
		n, _ := network("ethereum-sepolia")
		var s string
		if a.rpc(context.Background(), n, "eth_chainId", []any{}, &s) == nil {
			t.Fatal("hostile RPC envelope accepted")
		}
		server.Close()
	}
	f := fakeNode(t, "ethereum-sepolia")
	n, _ := network("ethereum-sepolia")
	var s string
	f.status = 503
	if f.a.rpc(context.Background(), n, "test", nil, &s) == nil {
		t.Fatal("HTTP error")
	}
	f.a.HTTPClient = &http.Client{Transport: brokenTransport{}}
	if f.a.rpc(context.Background(), n, "test", nil, &s) == nil {
		t.Fatal("transport error")
	}
	f.a.HTTPClient = nil
	if f.a.rpc(context.Background(), n, "test", nil, &s) == nil {
		t.Fatal("nil client")
	}
}
func FuzzTP012AddressAndPlan(f *testing.F) {
	f.Add("0x1111111111111111111111111111111111111111", []byte(`{}`))
	f.Fuzz(func(t *testing.T, address string, raw []byte) {
		if len(address) > 1000 || len(raw) > 10000 {
			return
		}
		a := New()
		_ = a.ValidateAddress("ethereum-mainnet", address)
		var p model.Plan
		if StrictJSON(raw, &p) == nil {
			_ = a.ValidatePlan(p)
		}
	})
}
