package nativewallet

import (
	"strings"
	"testing"
	"time"

	"localwallet/internal/marketdata"
	"localwallet/internal/model"
)

// Stablecoin balances on different ledgers remain separate units. In particular,
// Core USDC8 cannot be interpreted using the six-decimal HyperEVM token policy.
func TestPortfolioDefaultStablecoinsUseExactLedgerPrecisionAndObservedUSDCPrice(t *testing.T) {
	now := time.Now()
	a := &App{environment: "mainnet", state: snapshot{RecoveryConfirmed: true}, balances: map[string][]model.Balance{}, balanceErrors: map[string]bool{}, marketPrices: marketdata.Snapshot{FetchedAt: now.UnixMilli(), Quotes: map[string]marketdata.Quote{"USDCUSDT": {Pair: "USDCUSDT", Price: "0.98", ObservedAt: now.UnixMilli(), Source: "Bybit public spot"}}}}
	for _, r := range []struct {
		network, family, chain, symbol, contract, id string
		decimals                                     uint8
	}{
		{"ethereum-mainnet", "evm", "1", "USDC", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "", 6},
		{"arbitrum-mainnet", "evm", "42161", "USDT0", "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", "", 6},
		{"arbitrum-mainnet", "evm", "42161", "USDC", "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", "", 6},
		{"base-mainnet", "evm", "8453", "USDT", "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", "", 6},
		{"base-mainnet", "evm", "8453", "USDC", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", "", 6},
		{"hyperevm-mainnet", "evm", "999", "USDC", "0xb88339CB7199b77E23DB6E890353E22632Ba630f", "", 6},
		{"hypercore-mainnet", "hypercore", "HyperCore Mainnet", "USDC", "USDC:0x6d1e7cde53ba9467b783cb7c530ce054", "hypercore-mainnet:USDC:0x6d1e7cde53ba9467b783cb7c530ce054", 8},
		{"ton-mainnet", "ton", "-239", "USDT", "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe", "ton-mainnet:USDT", 6},
	} {
		asset := model.Asset{ID: r.id, Contract: r.contract, Symbol: r.symbol, Decimals: r.decimals}
		if asset.ID == "" {
			asset.ID = r.network + ":erc20:" + r.contract
		}
		found := -1
		for i, n := range a.networks {
			if n.ID == r.network {
				found = i
			}
		}
		if found < 0 {
			a.networks = append(a.networks, model.Network{ID: r.network, Family: r.family, ChainID: r.chain, Name: r.network})
			found = len(a.networks) - 1
			a.state.Accounts = append(a.state.Accounts, model.Account{ID: r.network, Network: r.network, Family: r.family, Address: "0x1111111111111111111111111111111111111111"})
		}
		a.networks[found].Assets = append(a.networks[found].Assets, asset)
		a.balances[r.network] = append(a.balances[r.network], model.Balance{Asset: asset.ID, Atomic: "1" + strings.Repeat("0", int(r.decimals)), State: "provider-observed", At: "verified-ledger-position"})
	}
	total := a.portfolioValuation(0, now)
	if total.State != "ready" || total.PricedAssets != 8 || total.UnpricedAssets != 0 || total.USDT != "7.90" {
		t.Fatal("stablecoins or Core precision misvalued", total)
	}
	// A missing USDC quote leaves a three-USDT subtotal, not an invented five-dollar peg.
	a.marketPrices.Quotes = map[string]marketdata.Quote{}
	total = a.portfolioValuation(0, now)
	if total.State != "partial" || total.PricedAssets != 3 || total.UnpricedAssets != 5 || total.USDT != "3.00" {
		t.Fatal("USDC peg or missing price invented", total)
	}
	for _, network := range []string{"ethereum-mainnet", "arbitrum-mainnet", "base-mainnet", "hyperevm-mainnet", "hypercore-mainnet"} {
		named := false
		for _, missing := range total.Missing {
			if missing.NetworkID == network && missing.Symbol == "USDC" {
				named = true
			}
		}
		if !named {
			t.Fatal("unavailable stablecoin quote not individually named", network, total)
		}
	}
}
