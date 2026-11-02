package exchange

import (
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

func TestWizardStateRejectsUnboundedMalformedAndSkippedSteps(t *testing.T) {
	w := WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: "owned", SourceAsset: "ton-mainnet:TON", DepositAmount: "1.25"}
	if !w.Valid() {
		t.Fatal("valid initial draft rejected")
	}
	for _, mutate := range []func(*WizardState){
		func(w *WizardState) { w.ID = strings.Repeat("A", 32) }, func(w *WizardState) { w.Step = 0 }, func(w *WizardState) { w.Step = 6 },
		func(w *WizardState) { w.SourceAccount = "" }, func(w *WizardState) { w.SourceAsset = "foreign\x00asset" },
		func(w *WizardState) { w.DepositAmount = "1e3" }, func(w *WizardState) { w.DepositAmount = "-1" }, func(w *WizardState) { w.DepositAmount = "0" },
		func(w *WizardState) { w.SourceAccount = strings.Repeat("x", 257) }, func(w *WizardState) { w.TradeSide = "Limit" }, func(w *WizardState) { w.QuoteCoin = "BTC" },
		func(w *WizardState) { w.OutputCoin = "<script>" }, func(w *WizardState) { w.Step = 2 }, func(w *WizardState) { w.Step = 3; w.DepositOperation = "p" },
		func(w *WizardState) {
			w.Step = 4
			w.DepositOperation = "p"
			w.TransferOperation = "t"
			w.TransferAmount = "1"
		},
	} {
		bad := w
		mutate(&bad)
		if bad.Valid() {
			t.Fatal("invalid draft accepted", bad)
		}
	}
	for step := 2; step <= 5; step++ {
		w.Step = step
		w.DepositOperation = "p"
		w.TransferOperation = "t"
		w.TransferAmount = "1.25"
		w.SwapOperation = "s"
		w.OutputCoin = "USDT"
		w.OutputAmount = "3.123456"
		w.WithdrawOperation = "w"
		if !w.Valid() {
			t.Fatal("valid correlated stage rejected", step)
		}
	}
}

func TestReviewedTONUSDTAndArbitrumUSDTLedgerRoutes(t *testing.T) {
	tonAccount := model.Account{Network: "ton-mainnet", Family: "ton"}
	jetton := model.Asset{ID: "ton-mainnet:USDT", Symbol: "USDT", Decimals: 6, Contract: ton.USDTMaster}
	parsed, err := address.ParseRawAddr(ton.USDTMaster)
	if err != nil {
		t.Fatal(err)
	}
	for _, full := range []string{ton.USDTMaster, parsed.StringRaw(), parsed.Bounce(false).String()} {
		if ValidateRoute(tonAccount, jetton, "TON", "TON", full) != nil {
			t.Fatal("canonical same-master route rejected", full)
		}
	}
	for _, mutate := range []func(*model.Asset){func(a *model.Asset) { a.ID = "ton-mainnet:OTHER" }, func(a *model.Asset) { a.Decimals = 9 }, func(a *model.Asset) { a.Contract = "EQARBITRARY" }} {
		bad := jetton
		mutate(&bad)
		if ValidateRoute(tonAccount, bad, "TON", "TON", ton.USDTMaster) == nil {
			t.Fatal("foreign jetton admitted")
		}
	}
	if ContractMatches("ton-mainnet", strings.ToLower(parsed.String()), ton.USDTMaster) {
		t.Fatal("case-folded friendly address admitted")
	}
	asset := model.Asset{ID: evm.ArbitrumUSDTAsset, Symbol: "USDT0", Decimals: 6, Contract: evm.ArbitrumUSDTContract}
	ledger := LedgerAsset(asset)
	if ledger.Symbol != "USDT" || ledger.ID != asset.ID || ledger.Contract != asset.Contract || ledger.Decimals != 6 || asset.Symbol != "USDT0" {
		t.Fatal("ledger mapping changed immutable identity")
	}
	account := model.Account{Network: "arbitrum-mainnet", Family: "evm"}
	if ValidateRoute(account, ledger, "ARBI", "Arbitrum One", asset.Contract) != nil {
		t.Fatal("reviewed Bybit Arbitrum route rejected")
	}
	for _, chain := range []string{"ETH", "BASE", "ARBITRUM"} {
		if ValidateRoute(account, ledger, chain, "Arbitrum One", asset.Contract) == nil {
			t.Fatal("foreign service chain accepted", chain)
		}
	}
	for _, provider := range []string{"binance", "kucoin"} {
		if ValidateProviderRoute(provider, account, ledger, "ARBI", "Arbitrum One", asset.Contract) == nil {
			t.Fatal("unreviewed provider route admitted")
		}
	}
	bad := asset
	bad.Contract = "0x0000000000000000000000000000000000000001"
	if LedgerAsset(bad).Symbol != "USDT0" {
		t.Fatal("ticker-only mapping admitted")
	}
}

func TestBybitHyperEVMDisplayAliasesKeepExactLedgerAndQuarantinedRoutesClosed(t *testing.T) {
	account, asset := nativeRoute("hyperevm-mainnet", "HYPE")
	for _, label := range []string{"HyperEVM", "HYPEREVM", "Hyperliquid", "HyperLiquid", "Hyperliquid (HYPE)", "HypeEVM"} {
		if ValidateRoute(account, asset, "HYPEREVM", label, "") != nil {
			t.Fatal("same exact HyperEVM service chain rejected", label)
		}
	}
	for _, chain := range []string{"HYPERCORE", "HYPE", "ETH"} {
		if ValidateRoute(account, asset, chain, "Hyperliquid", "") == nil {
			t.Fatal("generic label crossed ledger", chain)
		}
	}
	coin := bybit.Coin{Coin: "HYPE", Chains: []bybit.Chain{{Chain: "HYPEREVM", ChainType: "HyperEVM"}}, InvalidChains: map[string]string{"HYPEREVM": "PRECISION"}}
	if _, err := MatchRoute(account, asset, coin); err == nil {
		t.Fatal("quarantined selected route became usable")
	}
	coin.Chains = append(coin.Chains, coin.Chains[0])
	if _, err := MatchRoute(account, asset, coin); err == nil {
		t.Fatal("duplicate quarantined identity ignored")
	}
}
