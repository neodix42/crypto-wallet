package ton

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/crc16"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

type hostileNode struct {
	failMethod, failAddress string
	source, destination     accountState
	fee                     any
	masterSeq               uint32
	broadcasts              int
	transactions            any
	apiKeySeen              bool
}

func tonNode(t *testing.T, a *Adapter, p model.Plan) (*hostileNode, func()) {
	t.Helper()
	h := &hostileNode{source: accountState{Balance: "10000000000", State: "uninitialized", SyncUTime: time.Now().Unix()}, destination: accountState{Balance: "0", State: "uninitialized", SyncUTime: time.Now().Unix()}, fee: 1000000, masterSeq: 123, transactions: []any{}}
	n, _ := netInfo(p.Account.Network)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		h.apiKeySeen = r.Header.Get("X-API-Key") == "public-test-key"
		var addr string
		_ = json.Unmarshal(q.Params["address"], &addr)
		if q.Method == h.failMethod && (h.failAddress == "" || h.failAddress == addr) {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "public synthetic node failure"})
			return
		}
		var result any
		switch q.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": h.masterSeq}}
		case "getAddressInformation":
			result = h.destination
			if addr == p.Account.Address {
				result = h.source
			}
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]any{"in_fwd_fee": 0, "storage_fee": 0, "fwd_fee": 0, "gas_fee": h.fee}}
		case "sendBoc":
			h.broadcasts++
			result = map[string]any{"@type": "ok"}
		case "getTransactions":
			result = h.transactions
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	if e := a.Configure(p.Account.Network, Endpoint{URL: server.URL, APIKey: "public-test-key"}); e != nil {
		t.Fatal(e)
	}
	return h, server.Close
}

func TestPreparationFailsClosedAtEachNetworkBoundary(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*hostileNode, *model.Account, *model.Transfer)
	}{
		{"wrong-account-id", func(h *hostileNode, a *model.Account, tr *model.Transfer) { tr.AccountID = "other" }},
		{"bad-account-descriptor", func(h *hostileNode, a *model.Account, tr *model.Transfer) { a.Parameters = []byte(`{}`) }},
		{"bad-recipient", func(h *hostileNode, a *model.Account, tr *model.Transfer) { tr.Recipient = "!" }},
		{"oversized-comment", func(h *hostileNode, a *model.Account, tr *model.Transfer) { tr.Memo = strings.Repeat("x", 121) }},
		{"master-unavailable", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.failMethod = "getMasterchainInfo" }},
		{"no-master-checkpoint", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.masterSeq = 0 }},
		{"source-unavailable", func(h *hostileNode, a *model.Account, tr *model.Transfer) {
			h.failMethod = "getAddressInformation"
			h.failAddress = a.Address
		}},
		{"destination-unavailable", func(h *hostileNode, a *model.Account, tr *model.Transfer) {
			h.failMethod = "getAddressInformation"
			h.failAddress = tr.Recipient
		}},
		{"frozen-destination", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.destination.State = "frozen" }},
		{"zero-amount", func(h *hostileNode, a *model.Account, tr *model.Transfer) { tr.Amount = "0" }},
		{"fee-unavailable", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.failMethod = "estimateFee" }},
		{"fee-zero", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.fee = 0 }},
		{"fee-negative", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.fee = -1 }},
		{"fee-unbounded", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.fee = 250000000 }},
		{"fee-fractional", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.fee = json.Number("1.5") }},
		{"insufficient-fee-reserve", func(h *hostileNode, a *model.Account, tr *model.Transfer) { h.source.Balance = tr.Amount }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _, p := testPlan(t)
			h, close := tonNode(t, a, p)
			defer close()
			ac := p.Account
			tr := model.Transfer{AccountID: ac.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: p.Amount}
			tc.mutate(h, &ac, &tr)
			if _, e := a.Prepare(context.Background(), ac, tr); e == nil {
				t.Fatal("unsafe or incomplete preparation accepted")
			}
			if h.broadcasts != 0 {
				t.Fatal("preparation sent a transaction")
			}
		})
	}
}
func TestSubmissionDoesNotBroadcastChangedAuthorityState(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*hostileNode, model.Plan)
	}{
		{"master-unavailable", func(h *hostileNode, p model.Plan) { h.failMethod = "getMasterchainInfo" }},
		{"source-unavailable", func(h *hostileNode, p model.Plan) {
			h.failMethod = "getAddressInformation"
			h.failAddress = p.Account.Address
		}},
		{"source-authority-disabled", func(h *hostileNode, p model.Plan) { h.source.State = "frozen" }},
		{"changed-source-activation", func(h *hostileNode, p model.Plan) {
			pub, _ := hex.DecodeString(p.Account.PublicKey)
			init, _, e := StateInit(pub, p.Account.Network)
			if e != nil {
				panic(e)
			}
			h.source.State = "active"
			h.source.Code = base64.StdEncoding.EncodeToString(init.Code.ToBOC())
			h.source.Data = base64.StdEncoding.EncodeToString(init.Data.ToBOC())
		}},
		{"destination-unavailable", func(h *hostileNode, p model.Plan) {
			h.failMethod = "getAddressInformation"
			h.failAddress = p.Recipient
		}},
		{"destination-now-active", func(h *hostileNode, p model.Plan) { h.destination.State = "active" }},
		{"destination-now-frozen", func(h *hostileNode, p model.Plan) { h.destination.State = "frozen" }},
		{"estimate-failure", func(h *hostileNode, p model.Plan) { h.failMethod = "estimateFee" }},
		{"fee-increased", func(h *hostileNode, p model.Plan) { h.fee = 4000000 }},
		{"balance-spent", func(h *hostileNode, p model.Plan) { h.source.Balance = "1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, seed, p := testPlan(t)
			signed, e := a.Sign(seed, p)
			if e != nil {
				t.Fatal(e)
			}
			h, close := tonNode(t, a, p)
			defer close()
			tc.mutate(h, p)
			if _, e := a.Submit(context.Background(), p, signed); e == nil {
				t.Fatal("changed state accepted")
			}
			if h.broadcasts != 0 {
				t.Fatal("changed plan reached sendBoc")
			}
		})
	}
	a, seed, p := testPlan(t)
	signed, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	h, close := tonNode(t, a, p)
	defer close()
	h.failMethod = "sendBoc"
	r, e := a.Submit(context.Background(), p, signed)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("submission failure became a final result", e, r)
	}
	if !h.apiKeySeen {
		t.Fatal("encrypted-settings API credential missing from provider request")
	}
}

func TestTONPublicBoundaryRejections(t *testing.T) {
	a, seed, p := testPlan(t)
	bad := p.Account
	bad.Family = "other"
	if _, e := a.Balance(context.Background(), bad); e == nil {
		t.Fatal("unknown account accepted")
	}
	h, close := tonNode(t, a, p)
	defer close()
	h.failMethod = "getAddressInformation"
	if _, e := a.Balance(context.Background(), p.Account); e == nil {
		t.Fatal("missing balance reported as zero")
	}
	if _, e := a.Sign(seed[:32], p); e == nil {
		t.Fatal("truncated seed signed")
	}
	if _, e := a.Sign(seed, model.Plan{}); e == nil {
		t.Fatal("empty plan signed")
	}
	q := p
	x, _ := payload(q)
	x.Seqno = 1
	q.Nonce = "1"
	q.Payload, _ = json.Marshal(x)
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("deployment with nonzero seqno accepted")
	}
	q = p
	x, _ = payload(q)
	x.SourceBalance = "01"
	q.Payload, _ = json.Marshal(x)
	q.ID = planID(q)
	if e := a.ValidatePlan(q); e == nil {
		t.Fatal("noncanonical balance accepted")
	}
	if _, _, e := message(model.Plan{Account: p.Account, Recipient: "!", Amount: "1"}, Payload{}); e == nil {
		t.Fatal("codec accepted unknown destination")
	}
	if _, _, e := message(model.Plan{Account: bad, Recipient: p.Recipient, Amount: "1"}, Payload{}); e == nil {
		t.Fatal("codec accepted wrong descriptor")
	}
	if _, e := external(model.Plan{Account: bad}, Payload{}, cell.BeginCell().EndCell()); e == nil {
		t.Fatal("external codec accepted wrong descriptor")
	}
	if _, _, e := a.validateSigned(p, model.Signed{}); e == nil {
		t.Fatal("missing signed artifact accepted")
	}
	if _, e := a.verifyNetwork(context.Background(), "other"); e == nil {
		t.Fatal("unknown network accepted")
	}
	var out any
	if e := a.rpc(context.Background(), "other", "getMasterchainInfo", nil, &out); e == nil {
		t.Fatal("missing endpoint accepted")
	}
	if e := a.rpc(context.Background(), p.Account.Network, "getMasterchainInfo", make(chan int), &out); e == nil {
		t.Fatal("unencodable request accepted")
	}
	if _, e := parseBOC(nil); e == nil {
		t.Fatal("empty BOC accepted")
	}
	if _, e := parseBOC(make([]byte, (256<<10)+1)); e == nil {
		t.Fatal("oversized BOC accepted")
	}
	for _, n := range []uint32{1000000, 0xffffffff} {
		if _, e := account(make([]byte, 32), n, p.Account.Network); e == nil {
			t.Fatal("out-of-range account accepted")
		}
	}
	ad, e := parseAddress(p.Account.Network, p.Recipient)
	if e != nil {
		t.Fatal(e)
	}
	standardAddress := address.NewAddress(0, 0, bytes.Repeat([]byte{255}, 32)).Bounce(false).Testnet(true)
	standard := base64.RawStdEncoding.EncodeToString(mustFriendly(t, standardAddress))
	if e = a.ValidateAddress(p.Account.Network, standard); e != nil {
		t.Fatal("standard base64 friendly address rejected", e)
	}
	if e := a.ValidateAddress("other", standard); e == nil {
		t.Fatal("unknown address network accepted")
	}
	invalid := mustFriendly(t, ad)
	invalid[0] = 0x31
	sum := crc16.ChecksumXMODEM(invalid[:34])
	invalid[34] = byte(sum >> 8)
	invalid[35] = byte(sum)
	if e = a.ValidateAddress(p.Account.Network, base64.RawURLEncoding.EncodeToString(invalid)); e == nil {
		t.Fatal("CRC-valid unsupported flag accepted")
	}
}
func mustFriendly(t *testing.T, a *address.Address) []byte {
	t.Helper()
	b, e := base64.RawURLEncoding.DecodeString(a.String())
	if e != nil {
		t.Fatal(e)
	}
	return b
}
