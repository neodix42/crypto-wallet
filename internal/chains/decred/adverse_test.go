package decred

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/model"
)

// This fake node exercises hostile transport, tree identity, discovery and
// finality responses. Actual consensus acceptance is a separate dcrd test.
func TestSynchronizationDisapprovalGapsAndReceipts(t *testing.T) {
	a, seed, base := testPlan(t)
	ac := base.Account
	meta, cp, e := descriptor(ac)
	if e != nil {
		t.Fatal(e)
	}
	meta.Birthday = 143
	meta.ScanLimit = 40
	meta.GapLimit = 5
	ac.Parameters, _ = json.Marshal(meta)
	makeTxAt := func(branch, index uint32, value int64) *wire.MsgTx {
		addr, e := AddressAt(ac, branch, index)
		if e != nil {
			t.Fatal(e)
		}
		_, s, e := paymentScript(ac.Network, addr)
		if e != nil {
			t.Fatal(e)
		}
		tx := wire.NewMsgTx()
		h := chainhash.Hash{byte(index + 1), byte(branch + 1)}
		tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&h, 0, 0), value+1, nil))
		tx.AddTxOut(&wire.TxOut{Value: value, PkScript: s})
		return tx
	}
	disapproved := makeTxAt(0, 0, 100000000)
	approved := makeTxAt(0, 25, 110000000)
	stake := makeTxAt(1, 3, 20000000)
	blocks := map[uint32]*wire.MsgBlock{}
	prev := chainhash.Hash{42}
	for h := uint32(143); h <= 145; h++ {
		b := &wire.MsgBlock{Header: wire.BlockHeader{Height: h, PrevBlock: prev, VoteBits: 1}}
		if h == 143 {
			b.Transactions = []*wire.MsgTx{disapproved}
			b.STransactions = []*wire.MsgTx{stake}
		}
		if h == 144 {
			b.Header.VoteBits = 0
			b.Transactions = []*wire.MsgTx{approved}
		}
		prev = b.BlockHash()
		blocks[h] = b
	}
	tip := blocks[145].BlockHash().String()
	mode := "valid"
	var signed model.Signed
	broadcasts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
			t.Error(e)
			return
		}
		result := any(nil)
		switch q.Method {
		case "getblockhash":
			var h uint32
			_ = json.Unmarshal(q.Params[0], &h)
			if h == 0 {
				result = cp.GenesisHash.String()
				if mode == "genesis" {
					result = chainhash.Hash{1}.String()
				}
			} else if b, ok := blocks[h]; ok {
				result = b.BlockHash().String()
				if mode == "reorg" && h == 144 {
					result = chainhash.Hash{88}.String()
				}
			}
		case "getblockchaininfo":
			currentHeight := 145
			if mode == "expired-disapproval" {
				currentHeight = 160
			}
			result = map[string]any{"blocks": currentHeight, "bestblockhash": tip, "syncheight": currentHeight, "initialblockdownload": mode == "syncing"}
		case "getblock":
			var hash string
			_ = json.Unmarshal(q.Params[0], &hash)
			verbose := false
			if len(q.Params) > 1 {
				_ = json.Unmarshal(q.Params[1], &verbose)
			}
			for _, b := range blocks {
				if b.BlockHash().String() == hash {
					if verbose {
						vote := b.Header.VoteBits
						if mode == "disapproved" || mode == "expired-disapproval" {
							vote = 0
						}
						result = map[string]any{"votebits": vote}
						break
					}
					var buf bytes.Buffer
					_ = b.Serialize(&buf)
					result = hex.EncodeToString(buf.Bytes())
					if mode == "corrupt-block" {
						result = "00"
					}
					break
				}
			}
		case "gettxout":
			var id string
			_ = json.Unmarshal(q.Params[0], &id)
			var tree int8
			_ = json.Unmarshal(q.Params[2], &tree)
			tx := approved
			wantTree := int8(0)
			h := uint32(144)
			if id == stake.TxHash().String() {
				tx = stake
				wantTree = 1
				h = 143
			}
			if tree != wantTree {
				t.Error("transaction tree was discarded")
			}
			value := tx.TxOut[0].Value
			if mode == "fake-value" {
				value++
			}
			script := hex.EncodeToString(tx.TxOut[0].PkScript)
			if mode == "fake-script" {
				script = "51"
			}
			best := tip
			if mode == "stale-utxo" {
				best = chainhash.Hash{77}.String()
			}
			result = map[string]any{"bestblock": best, "confirmations": 146 - h, "value": json.Number(fmt.Sprintf("%d.%08d", value/1e8, value%1e8)), "scriptPubKey": map[string]any{"hex": script, "version": 0}}
			if mode == "spent" {
				result = nil
			}
		case "getnetworkinfo":
			rate := "0.0001"
			if mode == "high-fee" {
				rate = "1"
			}
			result = map[string]any{"relayfee": json.Number(rate)}
		case "estimatesmartfee":
			result = map[string]any{"feerate": json.Number("0.00015"), "blocks": 2}
		case "sendrawtransaction":
			broadcasts++
			result = signed.Identity
			if mode == "wrong-submit-id" {
				result = chainhash.Hash{55}.String()
			}
			if mode == "timeout-after-acceptance" {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "error": map[string]any{"code": -32603}})
				return
			}
		case "getrawtransaction":
			confirmations := 7
			blockhash := blocks[144].BlockHash().String()
			if mode == "SUBMITTED" {
				blockhash = ""
				confirmations = 0
			}
			if mode == "negative-confirmations" {
				confirmations = -1
			}
			if mode == "SUBMITTED" {
				confirmations = 2
			}
			raw := hex.EncodeToString(signed.Bytes)
			if mode == "wrong-raw" {
				raw = "00"
			}
			result = map[string]any{"txid": signed.Identity, "hex": raw, "blockhash": blockhash, "blockheight": 144, "confirmations": confirmations}
		default:
			t.Error("unexpected RPC", q.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "error": nil, "result": result})
	}))
	defer server.Close()
	if e := a.Configure(ac.Network, Endpoint{URL: server.URL, User: "public-fixture", Password: "public-fixture-only"}); e != nil {
		t.Fatal(e)
	}
	report, e := a.Sync(context.Background(), ac)
	if e != nil {
		t.Fatal(e)
	}
	if report.HighestReceive != 25 || report.HighestChange != 3 || len(report.Spendable) != 2 {
		t.Fatal("discovery or tree scan mismatch", report)
	}
	for _, in := range report.Spendable {
		if in.TxID == disapproved.TxHash().String() {
			t.Fatal("disapproved regular-tree output spendable")
		}
	}
	seenDisapproval := false
	for _, h := range report.History {
		seenDisapproval = seenDisapproval || h.State == "disapproved"
	}
	if !seenDisapproval {
		t.Fatal("disapproval hidden")
	}
	var progress []SyncProgress
	if _, e := a.SyncWithProgress(context.Background(), ac, func(p SyncProgress) { progress = append(progress, p) }); e != nil || len(progress) != 3 || progress[2].Scanned != 145 {
		t.Fatal("scan progress missing", e, progress)
	}
	if h, hash, e := a.ChainTip(context.Background(), ac.Network); e != nil || h != 145 || hash != tip {
		t.Fatal("verified tip failed", e, h, hash)
	}
	prepared, e := a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: base.Asset, Recipient: base.Recipient, Amount: base.Amount})
	if e != nil {
		t.Fatal(e)
	}
	signed, e = a.Sign(seed, prepared)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := a.Submit(context.Background(), prepared, signed); e != nil || r.State != "SUBMITTED" || broadcasts != 1 {
		t.Fatal(e, r, broadcasts)
	}
	for _, bad := range []string{"genesis", "syncing", "corrupt-block", "fake-value", "fake-script", "stale-utxo", "spent"} {
		t.Run(bad, func(t *testing.T) {
			mode = bad
			if _, e := a.Sync(context.Background(), ac); e == nil {
				t.Fatal("unsafe synchronized state accepted")
			}
		})
	}
	mode = "valid"
	limited := ac
	m, _, _ := descriptor(limited)
	m.ScanLimit = 28
	limited.Parameters, _ = json.Marshal(m)
	if _, e := a.Sync(context.Background(), limited); e == nil {
		t.Fatal("discovery range exhaustion accepted as zero")
	}
	a.maxBlocks = 1
	if _, e := a.Sync(context.Background(), ac); e == nil {
		t.Fatal("incomplete scan accepted")
	}
	a.maxBlocks = 10000
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.Sync(cancelled, ac); e == nil {
		t.Fatal("cancelled scan completed")
	}
	for _, item := range []struct{ mode, state string }{{"valid", "CONFIRMED"}, {"SUBMITTED", "SUBMITTED"}, {"negative-confirmations", "REORGED"}, {"SUBMITTED", "SUBMITTED"}, {"reorg", "REORGED"}, {"disapproved", "REORGED"}, {"expired-disapproval", "FAILED"}} {
		mode = item.mode
		r, e := a.Reconcile(context.Background(), prepared, signed)
		if e != nil || r.State != item.state {
			t.Fatalf("%s: %v %+v", mode, e, r)
		}
	}
	mode = "wrong-raw"
	if _, e := a.Reconcile(context.Background(), prepared, signed); e == nil {
		t.Fatal("provider returned different signed bytes")
	}
	mode = "timeout-after-acceptance"
	r, e := a.Submit(context.Background(), prepared, signed)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("ambiguous submission not unknown", e, r)
	}
	if broadcasts != 2 {
		t.Fatal("unexpected duplicate broadcast count", broadcasts)
	}
	mode = "wrong-submit-id"
	r, e = a.Submit(context.Background(), prepared, signed)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("wrong submission identity accepted", e, r)
	}
	mode = "high-fee"
	if _, e := a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: base.Asset, Recipient: base.Recipient, Amount: base.Amount}); e == nil {
		t.Fatal("fee policy exceeded")
	}
}
func TestDCRPublicMetadataAndCanonicalAmounts(t *testing.T) {
	a, _, p := testPlan(t)
	if len(a.Networks()) != 3 {
		t.Fatal("missing network")
	}
	advanced, e := AdvanceReceive(p.Account)
	if e != nil {
		t.Fatal(e)
	}
	advanced, e = AdvanceChange(advanced)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateRecovery(p.Account, advanced); e != nil {
		t.Fatal("valid recovered counters rejected", e)
	}
	configured, e := SetDiscovery(advanced, 100, 10, 100)
	if e != nil {
		t.Fatal(e)
	}
	m, _, e := descriptor(configured)
	if e != nil || m.Birthday != 100 || m.GapLimit != 10 || m.ScanLimit != 100 {
		t.Fatal("discovery metadata not preserved", e)
	}
	if _, e := SetDiscovery(advanced, 100, 0, 100); e == nil {
		t.Fatal("zero gap accepted")
	}
	if _, e := SetDiscovery(advanced, 100, 10, 9); e == nil {
		t.Fatal("range below gap accepted")
	}
	if e := a.SetScanBudget(0); e == nil {
		t.Fatal("zero block budget accepted")
	}
	if e := a.SetScanBudget(2_000_001); e == nil {
		t.Fatal("unbounded block budget accepted")
	}
	if e := a.SetScanBudget(20000); e != nil {
		t.Fatal(e)
	}
	bad := advanced
	bad.PublicKey = p.Account.PublicKey
	if e = ValidateRecovery(p.Account, bad); e == nil {
		t.Fatal("restored address/public key mismatch accepted")
	}
	bad = advanced
	bad.Index++
	if e = ValidateRecovery(p.Account, bad); e == nil {
		t.Fatal("foreign restored index accepted")
	}
	for _, s := range []string{"", "00", "-1", "1.1", "1e9", "2100000000000001", strings.Repeat("9", 100)} {
		if _, e := atomic(s, true); e == nil {
			t.Fatal("invalid atoms accepted", s)
		}
	}
	for _, s := range []string{"1.000000001", "1.2.3", "1e8", "-1", "1.x", "21000001"} {
		if _, e := decimalAtoms(s); e == nil {
			t.Fatal("invalid decimal accepted", s)
		}
	}
	if v, e := decimalAtoms("0.12345678"); e != nil || v != 12345678 {
		t.Fatal(v, e)
	}
	bad = p.Account
	bad.Parameters = []byte(`{}`)
	if _, e := AdvanceChange(bad); e == nil {
		t.Fatal("invalid metadata accepted")
	}
	if _, e := AdvanceReceive(bad); e == nil {
		t.Fatal("invalid metadata accepted")
	}
	if _, e := AddressAt(p.Account, 2, 0); e == nil {
		t.Fatal("foreign derivation branch accepted")
	}
	if _, e := a.Derive(make([]byte, 64), 0, "other"); e == nil {
		t.Fatal("wrong network accepted")
	}
	if _, _, e := paymentScript("other", p.Recipient); e == nil {
		t.Fatal("wrong script network accepted")
	}
	if _, e := previous(Input{Previous: strings.Repeat("0", (1<<20)+1)}); e == nil {
		t.Fatal("unbounded previous transaction accepted")
	}
}
func TestDCRHostileRPCEnvelopes(t *testing.T) {
	a := New()
	var out any
	if e := a.rpc(context.Background(), "decred-simnet", "getblockcount", []any{}, &out); e == nil {
		t.Fatal("unconfigured node accepted")
	}
	mode := "http"
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "public-test" || p != "public-test" {
			t.Error("missing configured authentication")
		}
		switch mode {
		case "http":
			w.WriteHeader(503)
		case "json":
			_, _ = io.WriteString(w, "!")
		case "duplicate":
			_, _ = io.WriteString(w, `{"id":1,"id":2,"result":1}`)
		case "id":
			_, _ = io.WriteString(w, `{"id":2,"result":1}`)
		case "error":
			_, _ = io.WriteString(w, `{"id":1,"error":{"code":-1}}`)
		case "large":
			_, _ = io.WriteString(w, strings.Repeat("x", (2<<20)+1))
		case "redirect":
			http.Redirect(w, r, target.URL, 302)
		}
	}))
	defer server.Close()
	if e := a.Configure("decred-simnet", Endpoint{URL: server.URL, User: "public-test", Password: "public-test", Client: &http.Client{}}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"http", "json", "duplicate", "id", "error", "large", "redirect"} {
		mode = bad
		if e := a.rpc(context.Background(), "decred-simnet", "getblockcount", []any{}, &out); e == nil {
			t.Fatal("hostile RPC accepted", mode)
		}
	}
	if forwarded {
		t.Fatal("RPC followed redirect")
	}
	for _, e := range []Endpoint{{URL: server.URL}, {URL: "https://u:p@example.com", User: "p", Password: "p"}, {URL: "https://example.com/?q=1", User: "p", Password: "p"}} {
		if err := a.Configure("decred-simnet", e); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if e := a.Configure("other", Endpoint{}); e == nil {
		t.Fatal("unknown network accepted")
	}
}
