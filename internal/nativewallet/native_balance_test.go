package nativewallet

import (
	"localwallet/internal/model"
	"math/big"
	"testing"
)

func TestOverviewDisplaysTheNativeBalanceOfEverySupportedNetwork(t *testing.T) {
	a, _, _ := fixture(t)
	expected := map[string]string{
		"ethereum-mainnet": "ETH", "base-mainnet": "ETH", "horizen-mainnet": "ETH",
		"hyperevm-mainnet": "HYPE", "hypercore-mainnet": "HYPE", "near-mainnet": "NEAR",
		"aptos-mainnet": "APT", "decred-mainnet": "DCR", "ton-mainnet": "TON",
	}
	for _, account := range a.state.Accounts {
		symbol, ok := expected[account.Network]
		if !ok {
			t.Fatalf("unreviewed native asset on %s", account.Network)
		}
		found := false
		for _, asset := range a.network(account.Network).Assets {
			atomic := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(asset.Decimals)), nil).String()
			a.balances[account.ID] = append(a.balances[account.ID], model.Balance{
				Asset: asset.ID, Atomic: atomic, State: "provider-observed", At: "public-block-reference",
			})
			found = found || asset.Symbol == symbol
		}
		if !found {
			t.Fatalf("registered native asset missing on %s", account.Network)
		}
	}
	page := a.groupPage(0)
	for _, account := range a.state.Accounts {
		row, ok := findNode(page.Nodes, "open/"+account.ID)
		if !ok {
			t.Fatalf("wallet row missing on %s", account.Network)
		}
		found := false
		for _, cell := range row.Children {
			found = found || cell.Kind == "label" && cell.Text == "1 "+expected[account.Network]
		}
		if !found {
			t.Fatalf("native balance missing or wrong asset selected on %s", account.Network)
		}
	}
}
