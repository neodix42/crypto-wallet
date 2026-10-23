package ton

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

func signedCell(c *cell.Cell) model.Signed {
	return model.Signed{Identity: hex.EncodeToString(c.Hash()), Bytes: c.ToBOC()}
}
func TestSignedAndArchivedMessageCodecsFailClosed(t *testing.T) {
	a, seed, p := testPlan(t)
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	_, ext, e := a.validateSigned(p, s)
	if e != nil {
		t.Fatal(e)
	}
	x, _ := payload(p)
	for _, q := range []model.Plan{{}, func() model.Plan { q := p; q.MaxFee = "0"; q.ID = planID(q); return q }()} {
		if _, e := a.Reconcile(context.Background(), q, s); e == nil {
			t.Fatal("invalid archived plan accepted")
		}
		if _, _, e := a.validateSigned(q, s); e == nil {
			t.Fatal("invalid signed plan accepted")
		}
	}
	malformed := signedCell(cell.BeginCell().EndCell())
	emptyBody, e := external(p, x, cell.BeginCell().EndCell())
	if e != nil {
		t.Fatal(e)
	}
	wrongDest := *ext
	wrongDest.DstAddr, _ = parseAddress(p.Account.Network, p.Recipient)
	wrong, e := tlb.ToCell(&wrongDest)
	if e != nil {
		t.Fatal(e)
	}
	bs, e := ext.Body.BeginParse()
	if e != nil {
		t.Fatal(e)
	}
	head, e := bs.LoadSlice(130)
	if e != nil {
		t.Fatal(e)
	}
	sig, e := bs.LoadSlice(512)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := bs.LoadRefCell()
	if e != nil {
		t.Fatal(e)
	}
	invalidSig := append([]byte(nil), sig...)
	invalidSig[0] ^= 1
	badBody := cell.BeginCell().MustStoreSlice(head, 130).MustStoreSlice(invalidSig, 512).MustStoreRef(ref).EndCell()
	badSig, e := external(p, x, badBody)
	if e != nil {
		t.Fatal(e)
	}
	noRefBody := cell.BeginCell().MustStoreSlice(head, 130).MustStoreSlice(sig, 512).EndCell()
	noRef, e := external(p, x, noRefBody)
	if e != nil {
		t.Fatal(e)
	}
	for name, artifact := range map[string]model.Signed{"malformed-external": malformed, "empty-body": signedCell(emptyBody), "wrong-outer-destination": signedCell(wrong), "invalid-signature": signedCell(badSig), "missing-action-ref": signedCell(noRef), "invalid-boc": {Identity: s.Identity, Bytes: []byte{1, 2, 3}}} {
		t.Run(name, func(t *testing.T) {
			if _, _, e := a.validateSigned(p, artifact); e == nil {
				t.Fatal("malformed signed message accepted")
			}
			if _, e := a.Reconcile(context.Background(), p, artifact); e == nil {
				t.Fatal("malformed archived message accepted")
			}
		})
	}
	q := p
	q.ID = "changed"
	if _, e := a.Submit(context.Background(), q, s); e == nil {
		t.Fatal("changed approval reached submission")
	}
	h, close := tonNode(t, a, p)
	defer close()
	h.failMethod = "getMasterchainInfo"
	if _, e := a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("unavailable network accepted by reconciliation")
	}
	h.failMethod = "getTransactions"
	if _, e := a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("unavailable source trace accepted")
	}
}
func TestTransactionsBoundedParsing(t *testing.T) {
	a, _, p := testPlan(t)
	h, close := tonNode(t, a, p)
	defer close()
	for name, rows := range map[string]any{"bad-base64": []map[string]string{{"data": "!"}}, "bad-boc": []map[string]string{{"data": "AQID"}}, "malformed-transaction": []map[string]string{{"data": txAsBadCell()}}, "excess-transactions": func() any {
		x := make([]map[string]string, 101)
		for i := range x {
			x[i] = map[string]string{"data": ""}
		}
		return x
	}()} {
		t.Run(name, func(t *testing.T) {
			h.transactions = rows
			if _, e := a.transactions(context.Background(), p.Account.Network, p.Account.Address); e == nil {
				t.Fatal("malformed/oversized transaction list accepted")
			}
		})
	}
	h.transactions = []any{}
	if _, e := a.transactions(context.Background(), p.Account.Network, "!"); e == nil {
		t.Fatal("unknown transaction account accepted")
	}
	bad := p
	bad.Account.Parameters = []byte(`{}`)
	if _, e := a.estimate(context.Background(), bad, Payload{Deploy: true}, cell.BeginCell().EndCell(), true); e == nil {
		t.Fatal("deployment estimate without authenticated descriptor accepted")
	}
}
func txAsBadCell() string { return cellBOC(cell.BeginCell().EndCell()) }
func cellBOC(c *cell.Cell) string {
	b, _ := json.Marshal(c.ToBOC())
	return strings.Trim(string(b), `"`)
}

func TestPinnedArtifactMismatchPreventsAddressChanges(t *testing.T) {
	original := codeBOC
	defer func() { codeBOC = original }()
	codeBOC = cell.BeginCell().EndCell().ToBOC()
	pub := make([]byte, 32)
	if _, _, e := StateInit(pub, "ton-testnet"); e == nil {
		t.Fatal("replaced valid BOC contract artifact accepted")
	}
	if _, e := account(pub, 0, "ton-testnet"); e == nil {
		t.Fatal("artifact mismatch silently changed account address")
	}
}
func TestRecipientTraceUnavailableIsPending(t *testing.T) {
	a, p, s, source, _ := receiptFixture(t)
	h, close := tonNode(t, a, p)
	defer close()
	h.transactions = []map[string]string{{"data": txBOC(t, source)}}
	r, e := a.Reconcile(context.Background(), p, s)
	if e == nil || r.State != "SUBMITTED" {
		t.Fatal("wrong recipient account BOC was accepted or finalized", e, r)
	}
}
