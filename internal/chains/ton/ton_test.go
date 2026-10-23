package ton

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

type fixture struct {
	Seed     string `json:"seed"`
	Accounts []struct {
		Network      string `json:"network"`
		Index        uint32 `json:"index"`
		PublicKey    string `json:"publicKey"`
		Address      string `json:"address"`
		StateHash    string `json:"stateHash"`
		StateInit    string `json:"stateInit"`
		BodyHash     string `json:"bodyHash"`
		ExternalHash string `json:"externalHash"`
		Recipient    string `json:"recipient"`
	} `json:"accounts"`
}

func fixtures(t *testing.T) (fixture, []byte) {
	t.Helper()
	b, e := os.ReadFile("../../../tests/oracles/ton/fixtures.json")
	if e != nil {
		t.Fatal(e)
	}
	var f fixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	seed, e := hex.DecodeString(f.Seed)
	if e != nil {
		t.Fatal(e)
	}
	return f, seed
}
func testPlan(t *testing.T) (*Adapter, []byte, model.Plan) {
	t.Helper()
	_, seed := fixtures(t)
	a := New()
	ac, e := a.Derive(seed, 0, "ton-testnet")
	if e != nil {
		t.Fatal(e)
	}
	dest, e := a.Derive(seed, 1, "ton-testnet")
	if e != nil {
		t.Fatal(e)
	}
	x := Payload{Deploy: true, SourceBalance: "10000000000", EstimatedFee: "1000000", StorageReserve: "10000000"}
	p := model.Plan{Account: ac, Asset: "ton-testnet:TON", Recipient: dest.Address, Amount: "123456789", FeeAsset: "ton-testnet:TON", MaxFee: "3000000", Nonce: "0", Expires: time.Now().Add(5 * time.Minute).Unix(), Memo: "fixture"}
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	return a, seed, p
}
func TestIndependentProfilesStateInitAndSignature(t *testing.T) {
	f, seed := fixtures(t)
	a := New()
	for _, row := range f.Accounts {
		t.Run(row.Network+strconv.Itoa(int(row.Index)), func(t *testing.T) {
			ac, e := a.Derive(seed, row.Index, row.Network)
			if e != nil {
				t.Fatal(e)
			}
			if ac.Address != row.Address || ac.PublicKey != row.PublicKey {
				t.Fatal("independent public descriptor mismatch")
			}
			pub, _ := hex.DecodeString(ac.PublicKey)
			_, state, e := StateInit(pub, row.Network)
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(state.Hash()) != row.StateHash {
				t.Fatal("independent StateInit hash mismatch")
			}
			ref, e := decodeBOC(row.StateInit)
			if e != nil || hex.EncodeToString(ref.Hash()) != hex.EncodeToString(state.Hash()) {
				t.Fatal("independent serialized StateInit mismatch")
			}
			p := model.Plan{Account: ac, Recipient: row.Recipient, Amount: "123456789", Memo: "fixture", Expires: 2000000000}
			x := Payload{Seqno: 7, Bounce: true}
			u, _, e := message(p, x)
			if e != nil {
				t.Fatal(e)
			}
			k, e := derivation.Ed25519(seed, []uint32{44, 607, row.Index})
			if e != nil {
				t.Fatal(e)
			}
			sig := ed25519.Sign(k, u.Hash())
			clear(k)
			b := cell.BeginCell().MustStoreBuilder(u.ToBuilder()).MustStoreSlice(sig, 512).EndCell()
			if hex.EncodeToString(b.Hash()) != row.BodyHash {
				t.Fatal("independent wallet transfer signature/BOC mismatch")
			}
			ext, e := external(p, x, b)
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(ext.Hash()) != row.ExternalHash {
				t.Fatal("independent external-message hash mismatch")
			}
		})
	}
}
func TestSignVerifyTamperingAndRecovery(t *testing.T) {
	a, seed, p := testPlan(t)
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.validateSigned(p, s); e != nil {
		t.Fatal(e)
	}
	_, ext, e := a.validateSigned(p, s)
	if e != nil {
		t.Fatal(e)
	}
	sl, e := ext.Body.BeginParse()
	if e != nil {
		t.Fatal(e)
	}
	head, e := sl.LoadSlice(130)
	if e != nil {
		t.Fatal(e)
	}
	signature, e := sl.LoadSlice(512)
	if e != nil {
		t.Fatal(e)
	}
	signature[0] ^= 1
	ref, e := sl.LoadRefCell()
	if e != nil {
		t.Fatal(e)
	}
	ext.Body = cell.BeginCell().MustStoreSlice(head, 130).MustStoreSlice(signature, 512).MustStoreRef(ref).EndCell()
	corrupt, e := tlb.ToCell(ext)
	if e != nil {
		t.Fatal(e)
	}
	forged := model.Signed{Identity: hex.EncodeToString(corrupt.Hash()), Bytes: corrupt.ToBOC()}
	if _, _, e = a.validateSigned(p, forged); e == nil {
		t.Fatal("invalid Ed25519 signature accepted in otherwise valid BOC")
	}
	restored, e := a.Derive(seed, 0, "ton-testnet")
	if e != nil || restored.Address != p.Account.Address {
		t.Fatal("recovery descriptor differs")
	}
	for _, mutate := range []func(*model.Plan){func(q *model.Plan) { q.Recipient = p.Account.Address }, func(q *model.Plan) { q.Amount = "123456790" }, func(q *model.Plan) { q.Memo = "changed" }, func(q *model.Plan) { q.Expires-- }, func(q *model.Plan) {
		var x Payload
		_ = json.Unmarshal(q.Payload, &x)
		x.Bounce = true
		q.Payload, _ = json.Marshal(x)
	}} {
		q := p
		mutate(&q)
		q.ID = planID(q)
		if _, _, e = a.validateSigned(q, s); e == nil {
			t.Fatal("old signature accepted changed plan")
		}
	}
	bad := s
	bad.Bytes = append([]byte(nil), s.Bytes...)
	bad.Bytes[len(bad.Bytes)/2] ^= 1
	if _, _, e = a.validateSigned(p, bad); e == nil {
		t.Fatal("tampered BOC accepted")
	}
	other := append([]byte(nil), seed...)
	other[0] ^= 1
	if _, e = a.Sign(other, p); e == nil {
		t.Fatal("foreign recovery root accepted")
	}
}
func TestAddressesAndMalformedPlans(t *testing.T) {
	a, _, p := testPlan(t)
	if e := a.ValidateAddress("ton-mainnet", p.Recipient); e == nil {
		t.Fatal("testnet address accepted mainnet")
	}
	for _, s := range []string{"", p.Recipient[:47] + "!", "1:" + strings.Repeat("00", 32), "0:" + strings.Repeat("00", 31), strings.Repeat("a", 500)} {
		if e := a.ValidateAddress("ton-testnet", s); e == nil {
			t.Fatalf("bad address accepted %q", s)
		}
	}
	for _, amount := range []string{"0", "-1", "1.2", "01", "1e9", strings.Repeat("9", 100)} {
		q := p
		q.Amount = amount
		q.ID = planID(q)
		if e := a.ValidatePlan(q); e == nil {
			t.Fatal("invalid amount accepted")
		}
	}
	q := p
	q.Payload = []byte(`{"seqno":0,"seqno":1}`)
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("duplicate JSON accepted")
	}
	q = p
	q.Expires = time.Now().Add(-time.Second).Unix()
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("expired plan accepted")
	}
	if _, e := a.Derive(make([]byte, 32), 0, "ton-testnet"); e == nil {
		t.Fatal("truncated seed accepted")
	}
	if _, e := a.Derive(make([]byte, 64), derivation.MaxIndex, "ton-testnet"); e == nil {
		t.Fatal("out-of-bounds account accepted")
	}
}
func TestTrustedRPCPreparationSubmission(t *testing.T) {
	a, seed, p := testPlan(t)
	n, _ := netInfo("ton-testnet")
	submitted := false
	wrongNetwork := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			t.Error(e)
			return
		}
		var result any
		switch v.Method {
		case "getMasterchainInfo":
			root := n.root
			if wrongNetwork {
				root = "wrong"
			}
			result = map[string]any{"init": map[string]any{"root_hash": root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation":
			result = map[string]any{"balance": "10000000000", "state": "uninitialized", "sync_utime": time.Now().Unix()}
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]int{"in_fwd_fee": 100000, "storage_fee": 100000, "gas_fee": 700000, "fwd_fee": 100000}}
		case "sendBoc":
			submitted = true
			result = map[string]any{"@type": "ok"}
		case "getTransactions":
			result = []any{}
		default:
			t.Errorf("unexpected RPC %s", v.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
	}))
	defer server.Close()
	if e := a.Configure("ton-testnet", Endpoint{URL: server.URL}); e != nil {
		t.Fatal(e)
	}
	balances, e := a.Balance(context.Background(), p.Account)
	if e != nil || balances[0].Atomic != "10000000000" {
		t.Fatal(e)
	}
	prepared, e := a.Prepare(context.Background(), p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: p.Amount, Memo: p.Memo})
	if e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed, prepared)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := a.Submit(context.Background(), prepared, s)
	if e != nil || receipt.State != "SUBMITTED" || !submitted {
		t.Fatal(e, receipt)
	}
	receipt, e = a.Reconcile(context.Background(), prepared, s)
	if e != nil || receipt.State != "UNKNOWN" {
		t.Fatal(e, receipt)
	}
	wrongNetwork = true
	if _, e = a.Balance(context.Background(), p.Account); e == nil {
		t.Fatal("wrong zerostate accepted")
	}
}
func TestWalletV5R1Emulator(t *testing.T) {
	if os.Getenv("WALLET_TON_EMULATOR") != "1" {
		t.Skip("NOT_RUN: set WALLET_TON_EMULATOR=1 after npm ci --ignore-scripts in tests/oracles/ton")
	}
	a, seed, p := testPlan(t)
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	insufficient := p
	insufficient.Amount = "20000000000"
	x, _ := payload(insufficient)
	x.SourceBalance = "100000000000"
	insufficient.Payload, _ = json.Marshal(x)
	insufficient.ID = planID(insufficient)
	failedSigned, e := a.Sign(seed, insufficient)
	if e != nil {
		t.Fatal(e)
	}
	req := map[string]any{"address": p.Account.Address, "recipient": p.Recipient, "amount": p.Amount, "expires": p.Expires, "boc": base64.StdEncoding.EncodeToString(s.Bytes), "insufficientBoc": base64.StdEncoding.EncodeToString(failedSigned.Bytes)}
	raw, _ := json.Marshal(req)
	request := filepath.Join(dir, "request.json")
	report := filepath.Join(dir, "report.json")
	if e = os.WriteFile(request, raw, 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("node", "../../../tests/oracles/ton/emulator.cjs", request, report)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("emulator: %v\n%s", e, out)
	}
	t.Log(strings.TrimSpace(string(out)))
	b, e := os.ReadFile(report)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Status       string `json:"status"`
		Transactions []struct {
			Address string `json:"address"`
			Data    string `json:"data"`
		} `json:"transactions"`
		FailedTransactions []struct {
			Address string `json:"address"`
			Data    string `json:"data"`
		} `json:"failedTransactions"`
	}
	if e = json.Unmarshal(b, &v); e != nil || v.Status != "PASS" {
		t.Fatal("emulator report failed", e)
	}
	n, _ := netInfo("ton-testnet")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
		}
		var raw map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_ = json.Unmarshal(raw["method"], &q.Method)
		if q.Method == "getTransactions" {
			var par struct {
				Address string `json:"address"`
			}
			_ = json.Unmarshal(raw["params"], &par)
			addr, _ := parseAddress("ton-testnet", par.Address)
			var rows []map[string]string
			for _, tx := range v.Transactions {
				if tx.Address == addr.StringRaw() {
					rows = append(rows, map[string]string{"data": tx.Data})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "ok": true, "result": rows})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "ok": true, "result": map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}})
	}))
	defer server.Close()
	if e = a.Configure("ton-testnet", Endpoint{URL: server.URL}); e != nil {
		t.Fatal(e)
	}
	receipt, e := a.Reconcile(context.Background(), p, s)
	if e != nil || receipt.State != "CONFIRMED" {
		t.Fatalf("emulator BOC reconciliation: %v %+v", e, receipt)
	}
	v.Transactions = v.FailedTransactions
	receipt, e = a.Reconcile(context.Background(), insufficient, failedSigned)
	if e != nil || receipt.State != "FAILED" {
		t.Fatalf("seqno advanced without payment was not rejected: %v %+v", e, receipt)
	}
	if os.Getenv("WALLET_EVIDENCE_DIR") != "" {
		if e = os.WriteFile(filepath.Join(os.Getenv("WALLET_EVIDENCE_DIR"), "ton-emulator.json"), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func FuzzAddressAndBOC(f *testing.F) {
	f.Add([]byte("0:" + strings.Repeat("00", 32)))
	f.Add(codeBOC)
	f.Add([]byte{0xb5, 0xee, 0x9c, 0x72, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 256<<10 {
			t.Skip()
		}
		_, _ = parseBOC(b)
		_ = New().ValidateAddress("ton-testnet", string(b))
	})
}
