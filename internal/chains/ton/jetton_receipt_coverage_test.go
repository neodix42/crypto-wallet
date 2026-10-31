package ton

import (
	"context"
	"encoding/base64"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

// These synthetic hostile archival graphs use real BOC/TLB codecs and an
// independently encoded TEP-74 message chain. They test local evidence policy;
// the separate SDK/TVM integration establishes actual contract execution.
type jettonReceiptGraph struct {
	a                             *Adapter
	p                             model.Plan
	s                             model.Signed
	rpc                           *jettonRPC
	owner, source, recipient      *tlb.Transaction
	sourceWallet, recipientWallet string
}

func jettonReceiptFixture(t *testing.T) *jettonReceiptGraph {
	t.Helper()
	a, seed, tr, ac, rpc := jettonFixture(t, ProfileSubwallet42)
	p, e := a.Prepare(context.Background(), ac, tr)
	if e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	x, e := payload(p)
	if e != nil {
		t.Fatal(e)
	}
	_, ext, e := a.validateSigned(p, s)
	if e != nil {
		t.Fatal(e)
	}
	own, _ := parseAddress(ac.Network, ac.Address)
	dst, _ := parseAddress(ac.Network, tr.Recipient)
	sw, _ := parseAddress(ac.Network, x.Jetton.Wallet)
	dw, _ := parseAddress(ac.Network, x.Jetton.RecipientWallet)
	master, _ := address.ParseRawAddr(USDTMaster)
	data := cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(0).MustStoreAddr(dst).MustStoreAddr(master).EndCell()
	rpc.states[dw.StringRaw()] = accountState{State: "active", Balance: "50000000", Code: usdtWalletLibraryBOC, Data: base64.StdEncoding.EncodeToString(data.ToBOC())}
	query, e := strconv.ParseUint(x.Jetton.QueryID, 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	amount, ok := new(big.Int).SetString(p.Amount, 10)
	if !ok {
		t.Fatal("invalid public test amount")
	}
	comment := cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice([]byte(p.Memo), uint(8*len(p.Memo))).EndCell()
	request := cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(dst).MustStoreAddr(own).MustStoreBoolBit(false).MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(comment).EndCell()
	credit := cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(own).MustStoreAddr(own).MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(comment).EndCell()
	notice := cell.BeginCell().MustStoreUInt(0x7362d09c, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(own).MustStoreBoolBit(true).MustStoreRef(comment).EndCell()
	first := &tlb.InternalMessage{IHRDisabled: true, Bounce: true, SrcAddr: own, DstAddr: sw, Amount: tlb.FromNanoTONU(100000000), Body: request}
	forward := &tlb.InternalMessage{IHRDisabled: true, Bounce: true, SrcAddr: sw, DstAddr: dw, Amount: tlb.FromNanoTONU(50000000), Body: credit}
	notification := &tlb.InternalMessage{IHRDisabled: true, SrcAddr: dw, DstAddr: dst, Amount: tlb.FromNanoTONU(1), Body: notice}
	_, _, _, template, _ := receiptFixture(t)
	build := func(account *address.Address, in *tlb.Message, out *tlb.InternalMessage, lt uint64) *tlb.Transaction {
		tx := cloneHistory(t, template)
		tx.AccountAddr = account.Data()
		tx.LT, tx.PrevTxLT, tx.PrevTxHash = lt, 0, make([]byte, 32)
		tx.IO.In, tx.OutMsgCount = in, 1
		setOutgoing(t, tx, out)
		return tx
	}
	g := &jettonReceiptGraph{a: a, p: p, s: s, rpc: rpc, sourceWallet: sw.StringRaw(), recipientWallet: dw.StringRaw()}
	g.owner = build(own, &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: ext}, first, 100)
	g.source = build(sw, &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: first}, forward, 200)
	g.recipient = build(dw, &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: forward}, notification, 300)
	return g
}

func (g *jettonReceiptGraph) store(t *testing.T) {
	t.Helper()
	g.rpc.transactions[g.p.Account.Address] = []map[string]string{{"data": txBOC(t, g.owner)}}
	// RPC canonicalizes friendly/raw address representations before looking up.
	own, _ := parseAddress(g.p.Account.Network, g.p.Account.Address)
	g.rpc.transactions[own.StringRaw()] = g.rpc.transactions[g.p.Account.Address]
	g.rpc.transactions[g.sourceWallet] = []map[string]string{{"data": txBOC(t, g.source)}}
	g.rpc.transactions[g.recipientWallet] = []map[string]string{{"data": txBOC(t, g.recipient)}}
}

func TestUSDTLinkedReceiptRejectsCoherentWrongEffects(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*testing.T, *jettonReceiptGraph)
	}{
		{"exact-control", func(*testing.T, *jettonReceiptGraph) {}},
		{"owner-amount", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.owner)
			m.Amount = tlb.FromNanoTONU(99999999)
			setOutgoing(t, g.owner, m)
			g.source.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"owner-source", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.owner)
			m.SrcAddr = m.DstAddr
			setOutgoing(t, g.owner, m)
			g.source.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"owner-destination", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.owner)
			m.DstAddr = m.SrcAddr
			setOutgoing(t, g.owner, m)
			g.source.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"owner-body", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.owner)
			m.Body = cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).EndCell()
			setOutgoing(t, g.owner, m)
			g.source.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"source-VM-failed", func(t *testing.T, g *jettonReceiptGraph) {
			ordinary(t, g.source, func(d *tlb.TransactionDescriptionOrdinary) { d.Aborted = true })
		}},
		{"source-unsupported-phase", func(_ *testing.T, g *jettonReceiptGraph) {
			g.source.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
		}},
		{"source-no-output", func(_ *testing.T, g *jettonReceiptGraph) { g.source.IO.Out = nil; g.source.OutMsgCount = 0 }},
		{"forward-source", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.source)
			m.SrcAddr = m.DstAddr
			setOutgoing(t, g.source, m)
			g.recipient.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"forward-destination", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.source)
			m.DstAddr = m.SrcAddr
			setOutgoing(t, g.source, m)
			g.recipient.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"forward-wrong-token-amount", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.source)
			x, _ := payload(g.p)
			q, _ := strconv.ParseUint(x.Jetton.QueryID, 10, 64)
			own, _ := parseAddress(g.p.Account.Network, g.p.Account.Address)
			m.Body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(q, 64).MustStoreCoins(1234566).MustStoreAddr(own).MustStoreAddr(own).MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice([]byte(g.p.Memo), uint(8*len(g.p.Memo))).EndCell()).EndCell()
			setOutgoing(t, g.source, m)
			g.recipient.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: m}
		}},
		{"recipient-VM-failed", func(t *testing.T, g *jettonReceiptGraph) {
			ordinary(t, g.recipient, func(d *tlb.TransactionDescriptionOrdinary) { d.Aborted = true })
		}},
		{"notification-source", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.recipient)
			m.SrcAddr = m.DstAddr
			setOutgoing(t, g.recipient, m)
		}},
		{"notification-destination", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.recipient)
			m.DstAddr = m.SrcAddr
			setOutgoing(t, g.recipient, m)
		}},
		{"notification-value", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.recipient)
			m.Amount = tlb.FromNanoTONU(2)
			setOutgoing(t, g.recipient, m)
		}},
		{"notification-body", func(t *testing.T, g *jettonReceiptGraph) {
			m := outgoing(t, g.recipient)
			m.Body = cell.BeginCell().MustStoreUInt(0x7362d09c, 32).EndCell()
			setOutgoing(t, g.recipient, m)
		}},
		{"recipient-no-notification", func(_ *testing.T, g *jettonReceiptGraph) { g.recipient.OutMsgCount = 0; g.recipient.IO.Out = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := jettonReceiptFixture(t)
			tc.edit(t, g)
			g.store(t)
			r, e := g.a.Reconcile(context.Background(), g.p, g.s)
			want := "UNKNOWN"
			if tc.name == "exact-control" {
				want = "CONFIRMED"
			}
			if e != nil || r.State != want || r.Identity != g.s.Identity || g.rpc.send != 0 {
				t.Fatalf("wanted %s without broadcast: %+v %v", want, r, e)
			}
		})
	}
}

func TestUSDTOutputDictionaryBoundsAndMessageTypes(t *testing.T) {
	g := jettonReceiptFixture(t)
	valid := outgoing(t, g.owner)
	encoded, e := tlb.ToCell(valid)
	if e != nil {
		t.Fatal(e)
	}
	external, e := g.owner.IO.In.ToCell()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		edit func(*tlb.Transaction)
		want string
	}{
		{"empty", func(tx *tlb.Transaction) { tx.IO.Out = nil; tx.OutMsgCount = 0 }, ""},
		{"nil-list-empty", func(tx *tlb.Transaction) { tx.IO.Out = &tlb.MessagesList{}; tx.OutMsgCount = 0 }, ""},
		{"count-bound", func(tx *tlb.Transaction) { tx.OutMsgCount = 257 }, "count exceeds"},
		{"missing-dictionary", func(tx *tlb.Transaction) { tx.IO.Out = nil }, "dictionary absent"},
		{"nil-list", func(tx *tlb.Transaction) { tx.IO.Out = &tlb.MessagesList{} }, "dictionary absent"},
		{"count-mismatch", func(tx *tlb.Transaction) { tx.OutMsgCount = 2 }, "count mismatch"},
		{"malformed-dictionary", func(tx *tlb.Transaction) { tx.IO.Out.List = cell.BeginCell().EndCell().AsDict(15) }, "count mismatch"},
		{"missing-reference", func(tx *tlb.Transaction) { setOutValue(t, tx, cell.BeginCell().EndCell()) }, "reference invalid"},
		{"trailing-bit", func(tx *tlb.Transaction) {
			setOutValue(t, tx, cell.BeginCell().MustStoreBoolBit(false).MustStoreRef(encoded).EndCell())
		}, "reference invalid"},
		{"trailing-reference", func(tx *tlb.Transaction) {
			setOutValue(t, tx, cell.BeginCell().MustStoreRef(encoded).MustStoreRef(encoded).EndCell())
		}, "reference invalid"},
		{"malformed-message", func(tx *tlb.Transaction) {
			setOutValue(t, tx, cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell())
		}, "message type unsupported"},
		{"external-message", func(tx *tlb.Transaction) { setOutValue(t, tx, cell.BeginCell().MustStoreRef(external).EndCell()) }, "message type unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := cloneHistory(t, g.owner)
			tc.edit(tx)
			rows, e := internalOutputs(tx)
			if tc.want == "" {
				if e != nil || len(rows) != 0 {
					t.Fatal("empty output set changed", rows, e)
				}
			} else if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatal("malformed output dictionary accepted", tc.want, rows, e)
			}
		})
	}
	if _, e := g.a.linkedMessageTransaction(context.Background(), g.p.Account.Network, g.sourceWallet, &tlb.Message{}); e == nil {
		t.Fatal("unencodable link message accepted")
	}
}

func TestUSDTLinkedReceiptIncompleteAndMalformedTraces(t *testing.T) {
	for _, name := range []string{"master-unavailable", "source-state-unavailable", "recipient-state-unavailable", "source-uninitialized", "recipient-uninitialized", "owner-zero-outputs", "owner-dictionary-malformed", "owner-body-invalid-plan", "source-archive-unavailable", "source-link-absent", "source-incoming-external", "source-dictionary-malformed", "recipient-link-absent", "recipient-incoming-external", "recipient-dictionary-malformed", "recipient-archive-malformed"} {
		t.Run(name, func(t *testing.T) {
			g := jettonReceiptFixture(t)
			g.store(t)
			wantError := false
			switch name {
			case "master-unavailable":
				g.rpc.fail, wantError = "getAddressInformation", true
			case "source-state-unavailable":
				st := g.rpc.states[g.sourceWallet]
				st.State = "frozen"
				g.rpc.states[g.sourceWallet] = st
				wantError = true
			case "recipient-state-unavailable":
				st := g.rpc.states[g.recipientWallet]
				st.State = "frozen"
				g.rpc.states[g.recipientWallet] = st
				wantError = true
			case "source-uninitialized":
				delete(g.rpc.states, g.sourceWallet)
			case "recipient-uninitialized":
				delete(g.rpc.states, g.recipientWallet)
			case "owner-zero-outputs":
				g.owner.OutMsgCount = 0
				g.owner.IO.Out = nil
			case "owner-dictionary-malformed":
				g.owner.IO.Out = nil
				wantError = true
			case "owner-body-invalid-plan":
				g.p.Amount = "not-decimal"
				wantError = true
			case "source-archive-unavailable":
				g.rpc.fail, wantError = "getTransactions", true
			case "source-link-absent":
				delete(g.rpc.transactions, g.sourceWallet)
			case "source-incoming-external":
				g.source.IO.In = g.owner.IO.In
				g.rpc.transactions[g.sourceWallet] = []map[string]string{{"data": txBOC(t, g.source)}}
			case "source-dictionary-malformed":
				g.source.IO.Out = nil
				g.rpc.transactions[g.sourceWallet] = []map[string]string{{"data": txBOC(t, g.source)}}
				wantError = true
			case "recipient-link-absent":
				delete(g.rpc.transactions, g.recipientWallet)
			case "recipient-incoming-external":
				g.recipient.IO.In = g.owner.IO.In
				g.rpc.transactions[g.recipientWallet] = []map[string]string{{"data": txBOC(t, g.recipient)}}
			case "recipient-dictionary-malformed":
				g.recipient.IO.Out = nil
				g.rpc.transactions[g.recipientWallet] = []map[string]string{{"data": txBOC(t, g.recipient)}}
				wantError = true
			case "recipient-archive-malformed":
				g.rpc.transactions[g.recipientWallet] = []map[string]string{{"data": "!"}}
				wantError = true
			}
			// Invoke the receipt-policy stage with an authenticated owner transaction,
			// including malformed owner dictionaries that cannot be reserialized.
			r, e := g.a.reconcileJetton(context.Background(), g.p, g.s, g.owner)
			if (e != nil) != wantError || r.State != "UNKNOWN" || r.Identity != g.s.Identity || g.rpc.send != 0 {
				t.Fatalf("incomplete/malformed chain falsely confirmed or broadcast: %+v %v (wantError=%t)", r, e, wantError)
			}
		})
	}
}
