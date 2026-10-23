package ton

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

func TestActiveAuthorityAndHostileSnapshots(t *testing.T) {
	a, _, p := testPlan(t)
	n, _ := netInfo(p.Account.Network)
	pub, _ := hex.DecodeString(p.Account.PublicKey)
	mode := "valid"
	data := func() *cell.Cell {
		allowed := mode != "disabled"
		wallet := uint64(n.wallet)
		if mode == "walletid" {
			wallet++
		}
		key := append([]byte(nil), pub...)
		if mode == "pubkey" {
			key[0] ^= 1
		}
		b := cell.BeginCell().MustStoreBoolBit(allowed).MustStoreUInt(7, 32).MustStoreUInt(wallet, 32).MustStoreSlice(key, 256)
		if mode == "extensions" {
			b.MustStoreBoolBit(true).MustStoreRef(cell.BeginCell().EndCell())
		} else {
			b.MustStoreBoolBit(false)
		}
		if mode == "trailing" {
			b.MustStoreUInt(0, 1)
		}
		return b.EndCell()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		var result any
		switch q.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation":
			st := map[string]any{"balance": "10000000000", "state": "active", "code": base64.StdEncoding.EncodeToString(codeBOC), "data": base64.StdEncoding.EncodeToString(data().ToBOC()), "sync_utime": time.Now().Unix()}
			switch mode {
			case "code":
				st["code"] = base64.StdEncoding.EncodeToString(cell.BeginCell().EndCell().ToBOC())
			case "data":
				st["data"] = "!"
			case "shortdata":
				st["data"] = base64.StdEncoding.EncodeToString(cell.BeginCell().EndCell().ToBOC())
			case "short-seqno":
				st["data"] = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreBoolBit(true).EndCell().ToBOC())
			case "short-pubkey":
				st["data"] = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreBoolBit(true).MustStoreUInt(0, 32).MustStoreUInt(uint64(n.wallet), 32).MustStoreUInt(0, 8).EndCell().ToBOC())
			case "frozen":
				st["state"] = "frozen"
			case "state":
				st["state"] = "UNKNOWN"
			case "balance":
				st["balance"] = "01"
			case "stale":
				st["sync_utime"] = time.Now().Add(-time.Hour).Unix()
			case "future":
				st["sync_utime"] = time.Now().Add(time.Hour).Unix()
			case "suspended":
				st["suspended"] = true
			case "missing-freshness":
				delete(st, "sync_utime")
			}
			result = st
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]int{"in_fwd_fee": 100000, "storage_fee": 100000, "gas_fee": 700000, "fwd_fee": 100000}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}))
	defer server.Close()
	if e := a.Configure(p.Account.Network, Endpoint{URL: server.URL}); e != nil {
		t.Fatal(e)
	}
	auth, e := a.authority(context.Background(), p.Account)
	if e != nil || auth.Seqno != 7 || auth.Deploy || auth.DataHash == "" {
		t.Fatal("valid active V5R1 authority failed", e, auth)
	}
	plan, e := a.Prepare(context.Background(), p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: p.Amount})
	if e != nil {
		t.Fatal(e)
	}
	x, _ := payload(plan)
	if x.Deploy || !x.Bounce || x.Seqno != 7 {
		t.Fatal("active contract plan mismatch")
	}
	for _, bad := range []string{"disabled", "walletid", "pubkey", "extensions", "trailing", "code", "data", "shortdata", "short-seqno", "short-pubkey", "frozen", "state", "balance", "stale", "future", "suspended", "missing-freshness"} {
		t.Run(bad, func(t *testing.T) {
			mode = bad
			if _, e := a.authority(context.Background(), p.Account); e == nil {
				t.Fatal("unsafe authority snapshot accepted")
			}
		})
	}
	mode = "valid"
	ac := p.Account
	ac.Parameters = []byte(`{}`)
	if _, e := a.authority(context.Background(), ac); e == nil {
		t.Fatal("invalid descriptor accepted")
	}
}
func TestBoundedRPCAndRedirects(t *testing.T) {
	a, _, p := testPlan(t)
	if len(a.Networks()) != 2 {
		t.Fatal("network matrix incomplete")
	}
	for _, e := range []Endpoint{{URL: "http://untrusted.example"}, {URL: "https://user:pass@example.com"}, {URL: "https://example.com/?query=1"}} {
		if er := a.Configure("ton-testnet", e); er == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if e := a.Configure("other", Endpoint{URL: "https://example.com"}); e == nil {
		t.Fatal("unknown network accepted")
	}
	mode := "http"
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]int{}})
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "http":
			w.WriteHeader(503)
		case "json":
			_, _ = io.WriteString(w, "!")
		case "duplicate":
			_, _ = io.WriteString(w, `{"ok":true,"ok":false,"result":{}}`)
		case "id":
			_, _ = io.WriteString(w, `{"ok":true,"id":2,"result":{}}`)
		case "error":
			_, _ = io.WriteString(w, `{"ok":false,"error":"synthetic failure"}`)
		case "large":
			_, _ = io.WriteString(w, strings.Repeat("x", (2<<20)+1))
		case "redirect":
			http.Redirect(w, r, target.URL, 302)
		case "shape":
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
		}
	}))
	defer server.Close()
	if e := a.Configure("ton-testnet", Endpoint{URL: server.URL, Client: &http.Client{}}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"http", "json", "duplicate", "id", "error", "large", "redirect", "shape"} {
		mode = bad
		var out map[string]any
		if e := a.rpc(context.Background(), p.Account.Network, "getMasterchainInfo", map[string]any{}, &out); e == nil {
			t.Fatal("hostile RPC accepted", mode)
		}
	}
	if forwarded {
		t.Fatal("RPC followed redirect")
	}
	server.Close()
	var out any
	if e := a.rpc(context.Background(), p.Account.Network, "getMasterchainInfo", map[string]any{}, &out); e == nil {
		t.Fatal("closed RPC accepted")
	}
	if _, e := decodeBOC(strings.Repeat("x", 350001)); e == nil {
		t.Fatal("oversized BOC accepted")
	}
	if _, _, e := StateInit(make([]byte, 31), "ton-testnet"); e == nil {
		t.Fatal("bad public key accepted")
	}
	if _, _, e := StateInit(make([]byte, 32), "other"); e == nil {
		t.Fatal("unknown network accepted")
	}
}
func TestTONPolicyAdverseCases(t *testing.T) {
	a, _, p := testPlan(t)
	for name, mutate := range map[string]func(*Payload){"fee": func(x *Payload) { x.EstimatedFee = "0" }, "reserve": func(x *Payload) { x.StorageReserve = "0" }, "balance": func(x *Payload) { x.SourceBalance = "1" }, "deployment-seq": func(x *Payload) { x.Seqno = 1 }, "authority-hash": func(x *Payload) { x.Deploy = false; x.SourceDataHash = "!" }} {
		t.Run(name, func(t *testing.T) {
			q := p
			x, _ := payload(q)
			mutate(&x)
			q.Payload, _ = json.Marshal(x)
			q.ID = planID(q)
			if e := a.ValidatePlan(q); e == nil {
				t.Fatal("invalid TON policy accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*model.Plan){"asset": func(q *model.Plan) { q.Asset = "ton-mainnet:TON" }, "feeasset": func(q *model.Plan) { q.FeeAsset = "other" }, "fee-limit": func(q *model.Plan) { q.MaxFee = "500000001" }, "fee-zero": func(q *model.Plan) { q.MaxFee = "0" }, "invalid-memo": func(q *model.Plan) { q.Memo = string([]byte{0xff}) }, "long-memo": func(q *model.Plan) { q.Memo = strings.Repeat("a", 121) }, "recipient": func(q *model.Plan) { q.Recipient = "!" }, "publickey": func(q *model.Plan) { q.Account.PublicKey = "00" }, "descriptor": func(q *model.Plan) { q.Account.Parameters = []byte(`{}`) }} {
		t.Run(name, func(t *testing.T) {
			q := p
			mutate(&q)
			q.ID = planID(q)
			if e := a.ValidatePlan(q); e == nil {
				t.Fatal("invalid TON plan accepted")
			}
		})
	}
}
