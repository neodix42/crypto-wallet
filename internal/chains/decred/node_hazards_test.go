package decred

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/model"
)

// This node is an adversarial transport/state machine. Only TestDcrdSimnet is
// evidence of consensus acceptance; these blocks deliberately need no PoW.
type hostileDCR struct {
	a             *Adapter
	account       model.Account
	seed          []byte
	blocks        map[uint32]*wire.MsgBlock
	tipHeight     uint32
	tipSequence   []uint32
	tipCalls      int
	calls         map[string]int
	fail          map[string]int
	relay, quote  string
	quoteErrors   []string
	quoteBlocks   int64
	signed        model.Signed
	receiptHeight uint32
	confirmations int64
	override      func(string, []json.RawMessage, any) (any, bool)
	broadcasts    int
}

func dcrNode(t *testing.T) (*hostileDCR, func()) {
	t.Helper()
	a, seed, p := testPlan(t)
	ac, e := SetDiscovery(p.Account, 1, 5, 40)
	if e != nil {
		t.Fatal(e)
	}
	h := &hostileDCR{a: a, account: ac, seed: seed, blocks: map[uint32]*wire.MsgBlock{}, tipHeight: 3, calls: map[string]int{}, fail: map[string]int{}, relay: "0.0001", quote: "0.0001", quoteBlocks: 2, receiptHeight: 2, confirmations: 7}
	for height := uint32(1); height <= 160; height++ {
		h.blocks[height] = &wire.MsgBlock{Header: wire.BlockHeader{Height: height, VoteBits: 1}}
	}
	h.blocks[1].Transactions = []*wire.MsgTx{h.funding(t, 0, 0, 100000000, 1)}
	h.rehash()
	cp, _ := params(ac.Network)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		h.calls[q.Method]++
		if target := h.fail[q.Method]; target == -1 || target == h.calls[q.Method] {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "error": map[string]int{"code": -1}})
			return
		}
		var result any
		switch q.Method {
		case "getblockhash":
			var height uint32
			_ = json.Unmarshal(q.Params[0], &height)
			if height == 0 {
				result = cp.GenesisHash.String()
			} else if b := h.blocks[height]; b != nil {
				result = b.BlockHash().String()
			}
		case "getblockchaininfo":
			if len(h.tipSequence) > 0 {
				idx := h.tipCalls
				if idx >= len(h.tipSequence) {
					idx = len(h.tipSequence) - 1
				}
				h.tipHeight = h.tipSequence[idx]
			}
			h.tipCalls++
			result = map[string]any{"blocks": h.tipHeight, "syncheight": h.tipHeight, "bestblockhash": h.blocks[h.tipHeight].BlockHash().String(), "initialblockdownload": false}
		case "getblock":
			var hash string
			var verbose bool
			_ = json.Unmarshal(q.Params[0], &hash)
			if len(q.Params) > 1 {
				_ = json.Unmarshal(q.Params[1], &verbose)
			}
			for _, b := range h.blocks {
				if b.BlockHash().String() != hash {
					continue
				}
				if verbose {
					result = map[string]any{"votebits": b.Header.VoteBits}
				} else {
					var raw bytes.Buffer
					_ = b.Serialize(&raw)
					result = hex.EncodeToString(raw.Bytes())
				}
				break
			}
		case "gettxout":
			var id string
			var vout uint32
			var tree int8
			_ = json.Unmarshal(q.Params[0], &id)
			_ = json.Unmarshal(q.Params[1], &vout)
			_ = json.Unmarshal(q.Params[2], &tree)
			for height, b := range h.blocks {
				txs := b.Transactions
				if tree == 1 {
					txs = b.STransactions
				}
				for _, tx := range txs {
					if tx.TxHash().String() != id || int(vout) >= len(tx.TxOut) {
						continue
					}
					o := tx.TxOut[vout]
					result = map[string]any{"bestblock": h.blocks[h.tipHeight].BlockHash().String(), "confirmations": h.tipHeight - height + 1, "value": json.Number(fmt.Sprintf("%d.%08d", o.Value/100000000, o.Value%100000000)), "coinbase": coinbase(tx), "scriptPubKey": map[string]any{"hex": hex.EncodeToString(o.PkScript), "version": o.Version}}
				}
			}
		case "getnetworkinfo":
			result = map[string]any{"relayfee": json.Number(h.relay)}
		case "estimatesmartfee":
			result = map[string]any{"feerate": json.Number(h.quote), "blocks": h.quoteBlocks, "errors": h.quoteErrors}
		case "sendrawtransaction":
			h.broadcasts++
			result = h.signed.Identity
		case "getrawtransaction":
			result = map[string]any{"txid": h.signed.Identity, "hex": hex.EncodeToString(h.signed.Bytes), "blockheight": h.receiptHeight, "blockhash": h.blocks[h.receiptHeight].BlockHash().String(), "confirmations": h.confirmations}
		default:
			t.Error("unexpected method", q.Method)
		}
		if h.override != nil {
			if altered, ok := h.override(q.Method, q.Params, result); ok {
				result = altered
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "error": nil, "result": result})
	}))
	if e = a.Configure(ac.Network, Endpoint{URL: server.URL, User: "public-fixture", Password: "public-fixture"}); e != nil {
		t.Fatal(e)
	}
	return h, server.Close
}
func (h *hostileDCR) rehash() {
	cp, _ := params(h.account.Network)
	previous := cp.GenesisHash
	for height := uint32(1); height <= 160; height++ {
		b := h.blocks[height]
		b.Header.PrevBlock = previous
		previous = b.BlockHash()
	}
}
func (h *hostileDCR) funding(t *testing.T, branch, index uint32, value int64, salt uint32) *wire.MsgTx {
	t.Helper()
	addr, e := AddressAt(h.account, branch, index)
	if e != nil {
		t.Fatal(e)
	}
	_, s, e := paymentScript(h.account.Network, addr)
	if e != nil {
		t.Fatal(e)
	}
	tx := wire.NewMsgTx()
	hash := chainhash.Hash{byte(salt), byte(salt >> 8), byte(salt >> 16), 1}
	tx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&hash, salt, 0), value, nil))
	tx.AddTxOut(&wire.TxOut{Value: value, PkScript: s})
	return tx
}
func (h *hostileDCR) transfer(amount string) model.Transfer {
	return model.Transfer{AccountID: h.account.ID, Asset: h.account.Network + ":DCR", Recipient: func() string { a, _ := h.a.Derive(h.seed, 1, h.account.Network); return a.Address }(), Amount: amount}
}
func (h *hostileDCR) prepared(t *testing.T) model.Plan {
	t.Helper()
	p, e := h.a.Prepare(context.Background(), h.account, h.transfer("10000000"))
	if e != nil {
		t.Fatal(e)
	}
	h.signed, e = h.a.Sign(h.seed, p)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestScanCatchupAndEveryCheckpointFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setup     func(*hostileDCR)
		wantError bool
	}{
		{"catchup", func(h *hostileDCR) { h.tipSequence = []uint32{2, 3, 3, 3} }, false},
		{"catchup-budget", func(h *hostileDCR) { h.tipSequence = []uint32{2, 3}; _ = h.a.SetScanBudget(1) }, true},
		{"reorg-during-catchup", func(h *hostileDCR) { h.tipSequence = []uint32{3, 2} }, true},
		{"final-tip-changed", func(h *hostileDCR) { h.tipSequence = []uint32{3, 3, 4} }, true},
		{"missing-checkpoint", func(h *hostileDCR) {
			h.override = func(m string, p []json.RawMessage, r any) (any, bool) {
				if m == "getblockchaininfo" {
					r.(map[string]any)["bestblockhash"] = ""
					return r, true
				}
				return nil, false
			}
		}, true},
		{"different-checkpoint", func(h *hostileDCR) {
			h.override = func(m string, p []json.RawMessage, r any) (any, bool) {
				if m == "getblockchaininfo" {
					r.(map[string]any)["bestblockhash"] = chainhash.Hash{99}.String()
					return r, true
				}
				return nil, false
			}
		}, true},
		{"malformed-block-hex", func(h *hostileDCR) {
			h.override = func(m string, p []json.RawMessage, r any) (any, bool) { return "!", m == "getblock" }
		}, true},
		{"block-hash-rpc", func(h *hostileDCR) { h.fail["getblockhash"] = 2 }, true},
		{"block-rpc", func(h *hostileDCR) { h.fail["getblock"] = -1 }, true},
		{"initial-info-rpc", func(h *hostileDCR) { h.fail["getblockchaininfo"] = 1 }, true},
		{"catchup-info-rpc", func(h *hostileDCR) { h.fail["getblockchaininfo"] = 2 }, true},
		{"final-info-rpc", func(h *hostileDCR) { h.fail["getblockchaininfo"] = 3 }, true},
		{"utxo-rpc", func(h *hostileDCR) { h.fail["gettxout"] = -1 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			tc.setup(h)
			r, e := h.a.Sync(context.Background(), h.account)
			if tc.wantError {
				if e == nil {
					t.Fatal("incomplete/adversarial checkpoint accepted", r)
				}
				return
			}
			if e != nil || r.Height != 3 || r.ScannedThrough != 3 {
				t.Fatal("advancing chain failed to catch up", e, r)
			}
		})
	}
	h, close := dcrNode(t)
	defer close()
	ctx, cancel := context.WithCancel(context.Background())
	_, e := h.a.SyncWithProgress(ctx, h.account, func(SyncProgress) { cancel() })
	if !errors.Is(e, context.Canceled) {
		t.Fatal("in-progress cancellation ignored", e)
	}
	if _, e = h.a.Sync(context.Background(), model.Account{}); e == nil {
		t.Fatal("unknown scan account accepted")
	}
	if _, _, e = h.a.ChainTip(context.Background(), "other"); e == nil {
		t.Fatal("unknown node network accepted")
	}
}

func TestSynchronizationSpendAccountingAndBounds(t *testing.T) {
	t.Run("spent-output-and-stable-outpoint-order", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		fund := h.blocks[1].Transactions[0]
		spent := h.funding(t, 0, 1, 50000000, 2)
		hash := fund.TxHash()
		spent.TxIn = []*wire.TxIn{wire.NewTxIn(wire.NewOutPoint(&hash, 0, 0), 100000000, nil)}
		h.blocks[2].Transactions = []*wire.MsgTx{spent}
		h.rehash()
		r, e := h.a.Sync(context.Background(), h.account)
		if e != nil || len(r.Spendable) != 1 || r.Spendable[0].TxID != spent.TxHash().String() {
			t.Fatal("spent input was retained", e, r)
		}
		sawSpent := false
		for _, row := range r.History {
			sawSpent = sawSpent || row.Spent == 100000000
		}
		if !sawSpent {
			t.Fatal("spend history missing")
		}
		fund.TxOut = append(fund.TxOut, &wire.TxOut{Value: 10000, PkScript: fund.TxOut[0].PkScript})
		h.blocks[2].Transactions = nil
		h.blocks[1].STransactions = []*wire.MsgTx{fund}
		h.blocks[1].Transactions = append(h.blocks[1].Transactions, h.funding(t, 0, 2, 20000, 3))
		h.rehash()
		r, e = h.a.Sync(context.Background(), h.account)
		if e != nil || len(r.Spendable) != 5 {
			t.Fatal("tree-aware ordering case", e, r)
		}
		for i := 1; i < len(r.Spendable); i++ {
			l, q := r.Spendable[i-1], r.Spendable[i]
			if l.Height > q.Height || l.Height == q.Height && (l.TxID > q.TxID || l.TxID == q.TxID && (l.Tree > q.Tree || l.Tree == q.Tree && l.Vout > q.Vout)) {
				t.Fatal("noncanonical outpoint order")
			}
		}
	})
	t.Run("owned-output-total-overflow", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		fund := h.funding(t, 0, 0, MaxAtoms, 1)
		fund.AddTxOut(&wire.TxOut{Value: MaxAtoms, PkScript: fund.TxOut[0].PkScript})
		h.blocks[1].Transactions = []*wire.MsgTx{fund}
		h.rehash()
		if _, e := h.a.Sync(context.Background(), h.account); e == nil {
			t.Fatal("output sum beyond monetary supply accepted")
		}
	})
	t.Run("owned-input-total-overflow", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		one, two := h.funding(t, 0, 0, MaxAtoms, 1), h.funding(t, 0, 1, MaxAtoms, 2)
		h.blocks[1].Transactions = []*wire.MsgTx{one, two}
		spend := h.funding(t, 0, 2, 1, 3)
		first, second := one.TxHash(), two.TxHash()
		spend.TxIn = []*wire.TxIn{wire.NewTxIn(wire.NewOutPoint(&first, 0, 0), MaxAtoms, nil), wire.NewTxIn(wire.NewOutPoint(&second, 0, 0), MaxAtoms, nil)}
		h.blocks[2].Transactions = []*wire.MsgTx{spend}
		h.rehash()
		if _, e := h.a.Sync(context.Background(), h.account); e == nil {
			t.Fatal("input sum beyond monetary supply accepted")
		}
	})
	for _, height := range []uint32{1, 3} {
		t.Run(fmt.Sprintf("utxo-cap-at-height-%d", height), func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			h.blocks[1].Transactions = nil
			fund := h.funding(t, 0, 0, 1, 1)
			for i := 0; i < 10000; i++ {
				fund.AddTxOut(&wire.TxOut{Value: 1, PkScript: fund.TxOut[0].PkScript})
			}
			h.blocks[height].Transactions = []*wire.MsgTx{fund}
			h.blocks[height].STransactions = []*wire.MsgTx{h.funding(t, 0, 1, 1, 2)}
			h.rehash()
			if _, e := h.a.Sync(context.Background(), h.account); e == nil {
				t.Fatal("oversized owned-output set accepted")
			}
		})
	}
	t.Run("bounded-growing-history", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		h.blocks[1].Transactions = nil
		base := h.funding(t, 0, 0, 1, 1)
		previous := base.TxIn[0].PreviousOutPoint.Hash
		for i := 0; i < 10002; i++ {
			tx := base.Copy()
			tx.TxIn[0].PreviousOutPoint.Hash = previous
			previous = tx.TxHash()
			height := uint32(1)
			if i >= 5001 {
				height = 2
			}
			h.blocks[height].Transactions = append(h.blocks[height].Transactions, tx)
		}
		h.rehash()
		if _, e := h.a.Sync(context.Background(), h.account); e == nil {
			t.Fatal("unbounded account history accepted")
		}
	})
	t.Run("balance-and-coin-selection-overflow", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		h.blocks[1].Transactions = []*wire.MsgTx{h.funding(t, 0, 0, MaxAtoms, 1)}
		h.blocks[2].Transactions = []*wire.MsgTx{h.funding(t, 0, 1, MaxAtoms, 2)}
		h.rehash()
		if _, e := h.a.Balance(context.Background(), h.account); e == nil {
			t.Fatal("balance overflow shown as valid amount")
		}
		if _, e := h.a.Prepare(context.Background(), h.account, h.transfer(fmt.Sprint(MaxAtoms))); e == nil {
			t.Fatal("overflowing coin selection accepted")
		}
	})
}

func TestFeeDustAndBoundedCoinSelection(t *testing.T) {
	t.Run("dust-remainder-is-fee-not-foreign-change", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		h.blocks[1].Transactions = []*wire.MsgTx{h.funding(t, 0, 0, 15000, 1)}
		h.rehash()
		p, e := h.a.Prepare(context.Background(), h.account, h.transfer("10000"))
		if e != nil {
			t.Fatal(e)
		}
		x, _ := decodePlan(p)
		if x.Change != 0 || x.Fee != 5000 {
			t.Fatal("incorrect dust remainder accounting", x)
		}
		if _, e := h.a.Sign(h.seed, p); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("input-budget", func(t *testing.T) {
		h, close := dcrNode(t)
		defer close()
		h.blocks[1].Transactions = nil
		for i := uint32(0); i < 130; i++ {
			h.blocks[1].Transactions = append(h.blocks[1].Transactions, h.funding(t, 0, 0, 5000, i+1))
		}
		h.rehash()
		if _, e := h.a.Prepare(context.Background(), h.account, h.transfer("1000000")); e == nil {
			t.Fatal("coin selection exceeded input resource budget")
		}
	})
	for _, tc := range []struct {
		name                 string
		relay, quote, amount string
		bad                  bool
	}{{"relay-floor", "0", "0.00001", "10000000", false}, {"bad-relay", "-1", "0.0001", "10000000", true}, {"smart-precision", "0.0001", "0.000100001", "10000000", true}, {"smart-ceiling", "0.0001", "1", "10000000", true}, {"dynamic-dust", "0.001", "0.001", "10000", true}} {
		t.Run(tc.name, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			h.relay = tc.relay
			h.quote = tc.quote
			p, e := h.a.Prepare(context.Background(), h.account, h.transfer(tc.amount))
			if tc.bad {
				if e == nil {
					t.Fatal("unsafe fee estimate accepted", p)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			x, _ := decodePlan(p)
			if x.FeeRate != 10000 {
				t.Fatal("minimum relay policy not retained")
			}
		})
	}
}

func TestPrepareRejectsUnavailableOrAlteredInputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*hostileDCR, *model.Account, *model.Transfer)
	}{
		{"unknown-account", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { a.Parameters = []byte(`{}`) }},
		{"memo", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { tr.Memo = "unsupported" }},
		{"recipient", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { tr.Recipient = "!" }},
		{"dust", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { tr.Amount = "1" }},
		{"scan-unavailable", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { h.fail["getblock"] = -1 }},
		{"fee-provider-unavailable", func(h *hostileDCR, a *model.Account, tr *model.Transfer) { h.fail["getnetworkinfo"] = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			ac := h.account
			tr := h.transfer("10000000")
			tc.setup(h, &ac, &tr)
			if _, e := h.a.Prepare(context.Background(), ac, tr); e == nil {
				t.Fatal("unsafe preparation accepted")
			}
			if h.broadcasts != 0 {
				t.Fatal("preparation broadcast")
			}
		})
	}
	h, close := dcrNode(t)
	defer close()
	if _, e := h.a.Balance(context.Background(), model.Account{}); e == nil {
		t.Fatal("unknown balance became zero")
	}
	var integer int
	if e := h.a.rpc(context.Background(), h.account.Network, "getblockhash", []any{0}, &integer); e == nil {
		t.Fatal("wrong RPC result schema accepted")
	}
	if e := h.a.rpc(context.Background(), h.account.Network, "getblockhash", []any{make(chan int)}, &integer); e == nil {
		t.Fatal("unencodable RPC input accepted")
	}
}
func TestSubmissionRechecksEveryReservedResource(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*hostileDCR, *model.Plan)
	}{
		{"plan-digest", func(h *hostileDCR, p *model.Plan) { p.ID = "changed" }},
		{"tip-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["getblockchaininfo"] = -1 }},
		{"wire-expired", func(h *hostileDCR, p *model.Plan) { h.tipHeight = 20 }},
		{"checkpoint-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["getblockhash"] = h.calls["getblockhash"] + 2 }},
		{"checkpoint-reorged", func(h *hostileDCR, p *model.Plan) {
			x, _ := decodePlan(*p)
			h.override = func(m string, args []json.RawMessage, r any) (any, bool) {
				var height uint32
				if len(args) > 0 {
					_ = json.Unmarshal(args[0], &height)
				}
				return chainhash.Hash{99}.String(), m == "getblockhash" && height == x.TipHeight
			}
		}},
		{"utxo-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["gettxout"] = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			p := h.prepared(t)
			tc.setup(h, &p)
			if _, e := h.a.Submit(context.Background(), p, h.signed); e == nil {
				t.Fatal("stale/unbound transaction submitted")
			}
			if h.broadcasts != 0 {
				t.Fatal("failed preflight reached broadcast")
			}
		})
	}
}
func TestReceiptFailureBoundariesRemainUnresolved(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*hostileDCR, *model.Plan)
	}{
		{"invalid-signature-artifact", func(h *hostileDCR, p *model.Plan) { h.signed.Bytes = []byte{1, 2, 3} }},
		{"tip-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["getblockchaininfo"] = -1 }},
		{"transaction-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["getrawtransaction"] = -1 }},
		{"canonical-block-unavailable", func(h *hostileDCR, p *model.Plan) { h.fail["getblockhash"] = h.calls["getblockhash"] + 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			p := h.prepared(t)
			tc.setup(h, &p)
			r, e := h.a.Reconcile(context.Background(), p, h.signed)
			if e == nil {
				t.Fatal("unavailable/invalid proof became a final receipt", r)
			}
			if r.State == "CONFIRMED" || r.State == "FAILED" {
				t.Fatal("uncertainty freed payment reservation", r)
			}
		})
	}
	for _, kind := range []string{"pending-stake-approval", "next-block-hash-unavailable", "next-block-unavailable"} {
		t.Run(kind, func(t *testing.T) {
			h, close := dcrNode(t)
			defer close()
			h.tipHeight = 145
			p := h.prepared(t)
			h.receiptHeight = 146
			h.tipHeight = 146
			h.confirmations = 1
			if kind == "next-block-hash-unavailable" {
				h.tipHeight = 147
				h.fail["getblockhash"] = h.calls["getblockhash"] + 3
			}
			if kind == "next-block-unavailable" {
				h.tipHeight = 147
				h.fail["getblock"] = -1
			}
			r, e := h.a.Reconcile(context.Background(), p, h.signed)
			if kind == "pending-stake-approval" {
				if e != nil || r.State != "SUBMITTED" {
					t.Fatal("unapproved regular-tree payment finalized", e, r)
				}
			} else if e == nil {
				t.Fatal("missing stake-approval proof finalized", r)
			}
		})
	}
}

func TestCoinSelectionAddsInputInsteadOfOverpayingDust(t *testing.T) {
	h, close := dcrNode(t)
	defer close()
	_, _, example := testPlan(t)
	x, _ := decodePlan(example)
	x.Change = 0
	tx, e := makeTx(example, x)
	if e != nil {
		t.Fatal(e)
	}
	minimum := feeFor(signedSize(tx), 10000)
	funding := 10000 + minimum + dustAtoms + 1
	h.blocks[1].Transactions = []*wire.MsgTx{h.funding(t, 0, 0, funding, 1)}
	h.rehash()
	if _, e := h.a.Prepare(context.Background(), h.account, h.transfer("10000")); e == nil {
		t.Fatal("dust ceiling was bypassed to consume isolated output")
	}
	h.blocks[2].Transactions = []*wire.MsgTx{h.funding(t, 0, 1, 10000, 2)}
	h.rehash()
	p, e := h.a.Prepare(context.Background(), h.account, h.transfer("10000"))
	if e != nil {
		t.Fatal(e)
	}
	selected, _ := decodePlan(p)
	if len(selected.Inputs) != 2 || selected.Change < dustAtoms {
		t.Fatal("did not select additional input for standard change", selected)
	}
}
