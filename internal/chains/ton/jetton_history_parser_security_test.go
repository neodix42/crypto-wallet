package ton

import (
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestUSDTHistoryCreditRejectsTruncationAndAmbiguousPayload(t *testing.T) {
	own := address.NewAddress(0, 0, make([]byte, 32))
	header := func() *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreCoins(1234567)
	}
	owners := func() *cell.Builder { return header().MustStoreAddr(own).MustStoreAddr(own) }
	payload := func(raw []byte) *cell.Cell {
		return cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice(raw, uint(8*len(raw))).EndCell()
	}
	for _, tc := range []struct {
		name string
		body *cell.Cell
	}{
		{"query-truncated", cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(1, 63).EndCell()},
		{"amount-truncated", cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreUInt(15, 4).EndCell()},
		{"zero-amount", cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreCoins(0).EndCell()},
		{"source-truncated", header().EndCell()},
		{"source-external", header().MustStoreAddr(address.NewAddressExt(0, 8, []byte{1})).EndCell()},
		{"response-truncated", header().MustStoreAddr(own).EndCell()},
		{"forward-coins-truncated", owners().MustStoreUInt(15, 4).EndCell()},
		{"forward-either-missing", owners().MustStoreCoins(1).EndCell()},
		{"forward-reference-missing", owners().MustStoreCoins(1).MustStoreBoolBit(true).EndCell()},
		{"forward-trailing-bits", owners().MustStoreCoins(1).MustStoreBoolBit(true).MustStoreBoolBit(false).MustStoreRef(payload([]byte("ok"))).EndCell()},
		{"forward-trailing-reference", owners().MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(payload([]byte("ok"))).MustStoreRef(payload(nil)).EndCell()},
		{"invalid-UTF8-comment", owners().MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(payload([]byte{0xff})).EndCell()},
		{"NUL-comment", owners().MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(payload([]byte{'a', 0, 'b'})).EndCell()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, e := parseJettonCredit(tc.body); e == nil {
				t.Fatal("malformed recognized credit body accepted")
			}
		})
	}
	for _, body := range []*cell.Cell{nil, cell.BeginCell().EndCell(), cell.BeginCell().MustStoreUInt(0x7362d09c, 32).EndCell()} {
		if _, recognized, e := parseJettonCredit(body); e != nil || recognized {
			t.Fatal("unrelated payload became token credit", recognized, e)
		}
	}
	for _, referenced := range []bool{false, true} {
		b := owners().MustStoreCoins(1).MustStoreBoolBit(referenced)
		if referenced {
			b.MustStoreRef(payload([]byte("public inert comment")))
		} else {
			b.MustStoreBuilder(payload([]byte("public inert comment")).ToBuilder())
		}
		credit, recognized, e := parseJettonCredit(b.EndCell())
		if e != nil || !recognized || credit.Query != 7 || credit.Amount != "1234567" || credit.Forward != "1" || credit.Comment != "public inert comment" || !credit.From.Equals(own) || !credit.Response.Equals(own) {
			t.Fatal("exact inline/reference credit changed", credit, recognized, e)
		}
	}
}

func TestUSDTHistoryTransferCommandRejectsIncompleteDestination(t *testing.T) {
	header := func() *cell.Builder {
		return cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(7, 64).MustStoreCoins(1234567)
	}
	for _, body := range []*cell.Cell{
		nil,
		cell.BeginCell().EndCell(),
		cell.BeginCell().MustStoreUInt(0x178d4519, 32).EndCell(),
		cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(1, 63).EndCell(),
		cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(7, 64).MustStoreUInt(15, 4).EndCell(),
		header().EndCell(),
		header().MustStoreAddr(address.NewAddressNone()).EndCell(),
		header().MustStoreAddr(address.NewAddressExt(0, 8, []byte{1})).EndCell(),
		header().MustStoreAddr(address.NewAddress(0, 255, make([]byte, 32))).EndCell(),
	} {
		if _, e := transferDestination(body); e == nil {
			t.Fatal("invalid/unrelated command supplied a recipient")
		}
	}
	own := address.NewAddress(0, 0, make([]byte, 32))
	if dest, e := transferDestination(header().MustStoreAddr(own).EndCell()); e != nil || !dest.Equals(own) {
		t.Fatal("valid owner address lost", dest, e)
	}
}

func TestUSDTHistoryRejectsSpoofedPeersAndUnrelatedExecution(t *testing.T) {
	for _, name := range []string{
		"outgoing-control", "incoming-control", "wrong-account", "no-input", "external-input", "wrong-input-destination", "bounced-input", "failed-VM", "unsupported-phase", "malformed-input-credit", "spoofed-source-wallet", "spoofed-mint-peer", "malformed-output-dictionary", "malformed-output-credit", "unrelated-output", "wrong-output-owner", "wrong-output-source", "wrong-output-recipient-wallet", "malformed-command", "wrong-command-authority",
	} {
		t.Run(name, func(t *testing.T) {
			g := jettonReceiptFixture(t)
			ac, tx := g.p.Account, g.source
			wantRows, wantError := 0, false
			switch name {
			case "outgoing-control":
				wantRows = 1
			case "incoming-control":
				ac.Address, tx, wantRows = g.p.Recipient, g.recipient, 1
			case "wrong-account":
				tx.AccountAddr = make([]byte, 32)
				wantError = true
			case "no-input":
				tx.IO.In = nil
			case "external-input":
				tx.IO.In = g.owner.IO.In
			case "wrong-input-destination":
				tx.IO.In.AsInternal().DstAddr = tx.IO.In.AsInternal().SrcAddr
			case "bounced-input":
				tx.IO.In.AsInternal().Bounced = true
			case "failed-VM":
				ordinary(t, tx, func(d *tlb.TransactionDescriptionOrdinary) { d.Aborted = true })
			case "unsupported-phase":
				tx.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
			case "malformed-input-credit":
				ac.Address, tx = g.p.Recipient, g.recipient
				tx.IO.In.AsInternal().Body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).EndCell()
				wantError = true
			case "spoofed-source-wallet":
				ac.Address, tx = g.p.Recipient, g.recipient
				tx.IO.In.AsInternal().SrcAddr = tx.IO.In.AsInternal().DstAddr
				wantError = true
			case "spoofed-mint-peer":
				ac.Address, tx = g.p.Recipient, g.recipient
				owner, _ := parseAddress(ac.Network, ac.Address)
				tx.IO.In.AsInternal().Body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreCoins(1).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(owner).MustStoreCoins(0).MustStoreBoolBit(false).EndCell()
				wantError = true
			case "malformed-output-dictionary":
				tx.IO.Out = nil
				wantError = true
			case "malformed-output-credit":
				m := outgoing(t, tx)
				m.Body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).EndCell()
				setOutgoing(t, tx, m)
				wantError = true
			case "unrelated-output":
				m := outgoing(t, tx)
				m.Body = cell.BeginCell().MustStoreUInt(0, 32).EndCell()
				setOutgoing(t, tx, m)
			case "wrong-output-owner":
				m := outgoing(t, tx)
				dst, _ := parseAddress(ac.Network, g.p.Recipient)
				m.Body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreCoins(1).MustStoreAddr(dst).MustStoreAddr(dst).MustStoreCoins(0).MustStoreBoolBit(false).EndCell()
				setOutgoing(t, tx, m)
				wantError = true
			case "wrong-output-source":
				m := outgoing(t, tx)
				m.SrcAddr = m.DstAddr
				setOutgoing(t, tx, m)
				wantError = true
			case "wrong-output-recipient-wallet":
				m := outgoing(t, tx)
				m.DstAddr = m.SrcAddr
				setOutgoing(t, tx, m)
				wantError = true
			case "malformed-command":
				tx.IO.In.AsInternal().Body = cell.BeginCell().MustStoreUInt(0, 32).EndCell()
				wantError = true
			case "wrong-command-authority":
				tx.IO.In.AsInternal().SrcAddr = tx.IO.In.AsInternal().DstAddr
				wantError = true
			}
			rows, e := jettonHistoryReceipts(ac, []*tlb.Transaction{tx})
			if (e != nil) != wantError || len(rows) != wantRows {
				t.Fatal("spoofed/unrelated history accepted or exact control lost", rows, e, wantError)
			}
			if len(rows) == 1 {
				if rows[0].State != "OBSERVED" || len(rows[0].Transfers) != 1 || rows[0].Transfers[0].Atomic != g.p.Amount || rows[0].Transfers[0].Source == rows[0].Transfers[0].Destination || rows[0].Transfers[0].Comment != g.p.Memo {
					t.Fatal("exact observed token effect changed", rows)
				}
			}
		})
	}
}
