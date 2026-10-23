package decred

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/decred/dcrd/wire"
)

func TestExpiryOutputMaturityAtConsensusBoundaryBothTrees(t *testing.T) {
	for _, tree := range []int8{wire.TxTreeRegular, wire.TxTreeStake} {
		for _, expiry := range []uint32{0, 100} {
			for _, confirmations := range []uint32{15, 16} {
				t.Run(fmt.Sprintf("tree%d-expiry%d-confirmations%d", tree, expiry, confirmations), func(t *testing.T) {
					h, closeNode := dcrNode(t)
					defer closeNode()
					tx := h.funding(t, 0, 0, 100000000, 99)
					tx.Expiry = expiry
					h.blocks[1].Transactions = nil
					if tree == wire.TxTreeRegular {
						h.blocks[1].Transactions = []*wire.MsgTx{tx}
					} else {
						h.blocks[1].STransactions = []*wire.MsgTx{tx}
					}
					h.tipHeight = confirmations
					h.rehash()
					r, e := h.a.Sync(context.Background(), h.account)
					if e != nil {
						t.Fatal(e)
					}
					immature := expiry != 0 && confirmations < 16
					if immature {
						if len(r.Spendable) != 0 || r.ImmatureAtoms != 100000000 || len(r.ImmatureOutputs) != 1 || r.ImmatureOutputs[0].RemainingBlocks != 1 || r.ImmatureOutputs[0].SpendableFromBlock != 17 || r.ImmatureOutputs[0].Reason != "transaction-expiry" {
							t.Fatal("expiry maturity snapshot", r)
						}
					} else if len(r.Spendable) != 1 || r.ImmatureAtoms != 0 {
						t.Fatal("mature/no-expiry output excluded", r)
					}
					a, _, p := testPlan(t)
					x, _ := decodePlan(p)
					prev, e := previous(x.Inputs[0])
					if e != nil {
						t.Fatal(e)
					}
					prev.Expiry = expiry
					data, e := prev.Bytes()
					if e != nil {
						t.Fatal(e)
					}
					x.Inputs[0].Previous = hex.EncodeToString(data)
					x.Inputs[0].TxID = prev.TxHash().String()
					x.Inputs[0].Tree = tree
					x.Inputs[0].Height = x.TipHeight - confirmations + 1
					p.Payload, _ = json.Marshal(x)
					p.ID = planID(p)
					if e = a.ValidatePlan(p); (e != nil) != immature {
						t.Fatal("secure maturity validator boundary", e, immature)
					}
				})
			}
		}
	}
}
func TestImmatureBalanceMonetaryBound(t *testing.T) {
	h, closeNode := dcrNode(t)
	defer closeNode()
	first := h.funding(t, 0, 0, MaxAtoms, 1)
	second := h.funding(t, 0, 0, MaxAtoms, 2)
	first.Expiry = 100
	second.Expiry = 100
	h.blocks[1].Transactions = []*wire.MsgTx{first, second}
	h.rehash()
	if _, e := h.a.Sync(context.Background(), h.account); e == nil {
		t.Fatal("immature monetary overflow accepted")
	}
}
