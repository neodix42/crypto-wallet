package decred

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/wire"
	"localwallet/internal/chains/chainutil"
)

func TestDecredHistoryInterpretsAuthenticatedIncomingBlocksWithoutGuessingExternalSources(t *testing.T) {
	h, stop := dcrNode(t)
	defer stop()
	stamp := time.Unix(1720000000, 0)
	h.blocks[1].Header.Timestamp = stamp
	h.rehash()
	entries, err := h.a.History(context.Background(), h.account)
	if err != nil || len(entries) != 1 {
		t.Fatal("bounded history failed", err, len(entries))
	}
	entry := entries[0]
	if entry.Timestamp != stamp.Unix() || entry.Identity != h.blocks[1].Transactions[0].TxHash().String() || entry.State != "CONFIRMED" || len(entry.Transfers) != 1 {
		t.Fatal("history lost original block/transaction/time/effect", entry)
	}
	effect := entry.Transfers[0]
	if effect.Source != "" || effect.Destination != h.account.Address || effect.Atomic != "100000000" || effect.Asset.ID != h.account.Network+":DCR" || effect.Asset.Decimals != 8 || effect.ID != "output/0/0" {
		t.Fatal("incoming transfer guessed a peer or changed exact atoms", effect)
	}
	if entry.Explorer != chainutil.TransactionExplorer(h.account.Network, entry.Identity) || !strings.Contains(h.a.HistoryScope(), "unknown") {
		t.Fatal("explorer or incomplete peer scope changed")
	}
	h.fail["getblock"] = -1
	if rows, err := h.a.History(context.Background(), h.account); err == nil || rows != nil {
		t.Fatal("unavailable trusted node became empty successful history")
	}
}

func TestDecredHistoryOutgoingEffectsPreserveChangeAndBoundAllocation(t *testing.T) {
	a, _, plan := testPlan(t)
	_, script, err := paymentScript(plan.Account.Network, plan.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	_, ours, err := paymentScript(plan.Account.Network, plan.Account.Address)
	if err != nil {
		t.Fatal(err)
	}
	tx := wire.NewMsgTx()
	tx.AddTxOut(&wire.TxOut{Value: 12345678, PkScript: script})
	tx.AddTxOut(&wire.TxOut{Value: 222, PkScript: ours})
	scripts := map[string]owned{hex.EncodeToString(ours): {}}
	rows, limited := historyOutputs(plan.Account.Network, tx, wire.TxTreeRegular, scripts, true, []string{plan.Account.Address, plan.Account.Address}, true, 16384)
	if limited || len(rows) != 2 || rows[0].Source != plan.Account.Address || rows[0].Destination != plan.Recipient || rows[0].Atomic != "12345678" || rows[1].Destination != plan.Account.Address || rows[1].Atomic != "222" {
		t.Fatal("outgoing or change effects changed", rows, limited)
	}
	for _, row := range rows {
		if a.ValidateAddress(plan.Account.Network, row.Destination) != nil {
			t.Fatal("wrong-network destination")
		}
	}
	unknown, _ := historyOutputs(plan.Account.Network, tx, wire.TxTreeRegular, scripts, true, []string{plan.Account.Address}, false, 16384)
	if unknown[0].Source != "" {
		t.Fatal("mixed/unknown inputs attributed to own address")
	}
	incoming, _ := historyOutputs(plan.Account.Network, tx, wire.TxTreeRegular, scripts, false, nil, false, 16384)
	if len(incoming) != 1 || !reflect.DeepEqual(incoming[0], unknown[1]) {
		t.Fatal("incoming observation included unrelated external output")
	}
	bounded, limited := historyOutputs(plan.Account.Network, tx, wire.TxTreeRegular, scripts, true, nil, false, 1)
	if !limited || len(bounded) != 1 {
		t.Fatal("shared output budget was ignored")
	}
	for len(tx.TxOut) < 257 {
		tx.AddTxOut(&wire.TxOut{Value: 1, PkScript: script})
	}
	bounded, limited = historyOutputs(plan.Account.Network, tx, wire.TxTreeRegular, scripts, true, nil, false, 16384)
	if !limited || len(bounded) != 256 {
		t.Fatal("transaction output bound was ignored")
	}
	disapproved := HistoryReceipts(plan.Account.Network, SyncReport{History: []HistoryItem{{Identity: tx.TxHash().String(), State: "disapproved", Timestamp: 1720000000}}})
	if len(disapproved) != 1 || len(disapproved[0].Transfers) != 0 || disapproved[0].State != "DISAPPROVED" {
		t.Fatal("vote-disapproved effects were fabricated")
	}
}
