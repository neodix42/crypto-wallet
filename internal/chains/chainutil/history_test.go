package chainutil

import (
	"fmt"
	"strings"
	"testing"

	"github.com/decred/base58"
	"localwallet/internal/model"
)

func TestTransactionExplorerUsesOnlyFixedNetworkRoutesAndCanonicalIdentities(t *testing.T) {
	hex := strings.Repeat("a", 64)
	near := base58.Encode(make([]byte, 32))
	cases := map[string]string{
		"ethereum-mainnet":  "https://etherscan.io/tx/0x" + hex,
		"ethereum-sepolia":  "https://sepolia.etherscan.io/tx/0x" + hex,
		"base-mainnet":      "https://basescan.org/tx/0x" + hex,
		"base-sepolia":      "https://sepolia.basescan.org/tx/0x" + hex,
		"arbitrum-mainnet":  "https://arbiscan.io/tx/0x" + hex,
		"arbitrum-sepolia":  "https://sepolia.arbiscan.io/tx/0x" + hex,
		"hyperevm-mainnet":  "https://hyperevmscan.io/tx/0x" + hex,
		"hypercore-mainnet": "https://app.hyperliquid.xyz/explorer/tx/0x" + hex,
		"hypercore-testnet": "https://app.hyperliquid-testnet.xyz/explorer/tx/0x" + hex,
		"near-mainnet":      "https://nearblocks.io/txns/" + near,
		"near-testnet":      "https://testnet.nearblocks.io/txns/" + near,
		"aptos-mainnet":     "https://explorer.aptoslabs.com/txn/0x" + hex + "?network=mainnet",
		"aptos-testnet":     "https://explorer.aptoslabs.com/txn/0x" + hex + "?network=testnet",
		"decred-mainnet":    "https://dcrdata.decred.org/tx/" + hex,
		"decred-testnet3":   "https://testnet.dcrdata.org/tx/" + hex,
		"ton-mainnet":       "https://tonviewer.com/transaction/" + hex,
		"ton-testnet":       "https://testnet.tonviewer.com/transaction/" + hex,
	}
	for network, want := range cases {
		identity := "0x" + hex
		if strings.HasPrefix(network, "near-") {
			identity = near
		}
		if strings.HasPrefix(network, "ton-") || strings.HasPrefix(network, "decred-") {
			identity = hex
		}
		if got := TransactionExplorer(network, identity); got != want {
			t.Fatal(network, got, want)
		}
		for _, hostile := range []string{"", "https://evil.invalid", identity + "/../x", identity + "?host=evil.invalid", identity + "#fragment", identity + "\x00", strings.ToUpper(identity)} {
			if hostile == identity {
				continue
			}
			if got := TransactionExplorer(network, hostile); got != "" {
				t.Fatal("provider-controlled navigation", network, got)
			}
		}
	}
	for _, network := range []string{"near-localnet", "evm-local", "ton-localnet", "horizen-mainnet", "unknown"} {
		if got := TransactionExplorer(network, "0x"+hex); got != "" {
			t.Fatal("unreviewed explorer route", got)
		}
	}
}

func TestMergeHistoryRetainsEveryEffectAndRejectsConflictingObservations(t *testing.T) {
	row := func(id string) model.Receipt {
		return model.Receipt{Identity: "transaction", State: "OBSERVED", Block: "block", Timestamp: 1700000000, Transfers: []model.HistoryTransfer{{ID: id, Asset: model.Asset{ID: "asset", Decimals: 18}, Atomic: "123"}}}
	}
	merged, err := MergeHistory([]model.Receipt{row("native"), row("token/1"), row("token/2")})
	if err != nil || len(merged) != 1 || len(merged[0].Transfers) != 3 {
		t.Fatal(merged, err)
	}
	for name, change := range map[string]func(*model.Receipt){
		"state": func(r *model.Receipt) { r.State = "FAILED" }, "block": func(r *model.Receipt) { r.Block = "other" }, "time": func(r *model.Receipt) { r.Timestamp++ }, "explorer": func(r *model.Receipt) { r.Explorer = "https://evil.invalid" }, "duplicate-effect": func(r *model.Receipt) { r.Transfers[0].ID = "native" }, "negative-amount": func(r *model.Receipt) { r.Transfers[0].Atomic = "-1" }, "nul-comment": func(r *model.Receipt) { r.Transfers[0].Comment = "\x00" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := row("token")
			change(&bad)
			if _, err := MergeHistory([]model.Receipt{row("native"), bad}); err == nil {
				t.Fatal("conflicting effect accepted")
			}
		})
	}
	rows := make([]model.Receipt, 1001)
	for i := range rows {
		rows[i] = row(fmt.Sprint(i))
	}
	if _, err := MergeHistory(rows); err == nil {
		t.Fatal("merged effect bound bypassed")
	}
}
