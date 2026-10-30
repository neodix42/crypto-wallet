package evm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"localwallet/internal/model"
)

// Explicit read-only public acceptance uses only the address the owner supplied.
// No vault, recovery root, private key, account signing or transaction submission
// is involved. The public key of this receiving-only address is not available, so
// this exercises the same candidate/checked-receipt functions used after the
// production owned-account validator, without inventing a recovery descriptor.
func TestPublicIndexedHistoryReadOnly(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_PUBLIC_HISTORY") != "1" {
		t.Skip("explicit read-only public-network acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a := New()
	n, _ := a.network("ethereum-mainnet")
	account := model.Account{Network: n.ID, Address: "0x73410EA1FcbE1b8bCA883762dC56beF46403A469"}
	head, err := a.current(ctx, n)
	if err != nil {
		t.Fatal("chain-checked public RPC unavailable", err)
	}
	end, _ := hexInteger(head.Number)
	var finalized *block
	if err := a.rpc(ctx, n, "eth_getBlockByNumber", []any{"finalized", false}, &finalized); err != nil || finalized == nil {
		t.Fatal("public finality unavailable", err)
	}
	final, err := hexInteger(finalized.Number)
	if err != nil {
		t.Fatal(err)
	}
	policies := map[string]model.Asset{}
	for _, asset := range n.Assets {
		if asset.Contract == "" {
			continue
		}
		if err := a.tokenIdentity(ctx, n, asset, head.Number); err != nil {
			t.Fatal("public tracked token identity unavailable", err)
		}
		policies["0xdac17f958d2ee523a2206206994597c13d831ec7"] = asset
	}
	identities := map[string]bool{}
	for _, tokens := range []bool{false, true} {
		ids, _, err := a.historyCandidates(ctx, historyIndexEndpoints(n.ID)[0], account, head.Number, 1, tokens)
		if err != nil {
			t.Fatal("public candidate index unavailable", err)
		}
		for _, id := range ids {
			identities[id] = true
		}
		if !tokens {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(510 * time.Millisecond):
			}
		}
	}
	rows := []model.Receipt{}
	assets := map[string]bool{}
	for id := range identities {
		row, err := a.checkedIndexedReceipt(ctx, n, account, id, end, final, policies)
		if err != nil || row == nil {
			t.Fatal("public indexed transaction failed independent RPC binding", id, err)
		}
		rows = append(rows, *row)
		for _, transfer := range row.Transfers {
			assets[transfer.Asset.ID] = true
		}
	}
	if !assets[n.ID+":native"] || !assets[USDTAsset] {
		t.Fatal("owner public example native or USDT history missing", rows)
	}
	if target := os.Getenv("CRYPTOWALLET_PUBLIC_HISTORY_REPORT"); target != "" {
		raw, _ := json.MarshalIndent(map[string]any{"scope": "Read-only owner-supplied public Ethereum address; production candidate and chain-checked receipt functions, no recovery descriptor or UI acceptance", "address": account.Address, "head": head.Number, "entries": rows}, "", "  ")
		if err := os.WriteFile(target, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("PASS %d checked public transactions, native ETH and exact six-decimal USDT", len(rows))
}
