package exchange

import (
	"errors"
	"strings"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

const publicUSDTContract = "0xdAC17F958D2ee523a2206206994597C13D831ec7"

func nativeRoute(network, symbol string) (model.Account, model.Asset) {
	id := network + ":native"
	if network == "ton-mainnet" {
		id = network + ":TON"
	}
	asset := model.Asset{ID: id, Symbol: symbol}
	if network == "ton-mainnet" {
		asset.Decimals = 9
	}
	return model.Account{ID: network + "-fixture-account", Network: network}, asset
}

func TestExchangeRoutesUseActualWalletRegistryIdentities(t *testing.T) {
	labels := map[string]string{"ethereum-mainnet": "Ethereum", "base-mainnet": "Base", "hyperevm-mainnet": "HyperEVM", "near-mainnet": "NEAR", "aptos-mainnet": "APTOS", "ton-mainnet": "TON"}
	seen := map[string]bool{}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			label, ok := labels[network.ID]
			if !ok {
				continue
			}
			seen[network.ID] = true
			for _, asset := range network.Assets {
				// Wallet support alone cannot authorize a new exchange route. TON
				// USDT uses a jetton master, unlike the reviewed native TON route.
				if network.ID == "ton-mainnet" && asset.Contract != "" {
					for _, provider := range []string{"bybit", "binance", "kucoin"} {
						if !errors.Is(ValidateProviderRoute(provider, model.Account{Network: network.ID}, asset, "TON", label, asset.Contract), ErrRoute) {
							t.Fatal("wallet jetton addition authorized an unreviewed exchange route", provider, asset.ID)
						}
					}
					continue
				}
				if err := ValidateRoute(model.Account{Network: network.ID}, asset, "ETH", label, asset.Contract); err != nil {
					t.Fatalf("actual registered wallet asset rejected: %s %v", asset.ID, err)
				}
			}
		}
	}
	if len(seen) != len(labels) {
		t.Fatal("registry coverage lost", seen)
	}
	account, asset := nativeRoute("ton-mainnet", "GRAM")
	asset.ID = "ton-mainnet:native"
	if ValidateRoute(account, asset, "TON", "TON", "") == nil {
		t.Fatal("unregistered TON asset admitted")
	}
}

func TestExchangeRouteMatchesActualLedgerAndFullTokenIdentity(t *testing.T) {
	for _, row := range []struct{ network, symbol, chain, chainType string }{
		{"ethereum-mainnet", "ETH", "ETH", "Ethereum"},
		{"ethereum-mainnet", "ETH", "ETH", "Ethereum (ERC20)"},
		{"ethereum-mainnet", "ETH", "ETH", "Ethereum(ERC20)"},
		{"ethereum-mainnet", "ETH", "ETH", "ERC20"},
		{"ethereum-mainnet", "ETH", "ETH", "ETH"},
		{"base-mainnet", "ETH", "service-base", "Base"},
		{"base-mainnet", "ETH", "service-base", "BASE"},
		{"hyperevm-mainnet", "HYPE", "service-hyper", "HyperEVM"},
		{"hyperevm-mainnet", "HYPE", "service-hyper", "Hyperliquid HyperEVM"},
		{"near-mainnet", "NEAR", "service-near", "NEAR"},
		{"near-mainnet", "NEAR", "service-near", "NEAR Protocol"},
		{"near-mainnet", "NEAR", "service-near", "Near Protocol"},
		{"aptos-mainnet", "APT", "service-aptos", "Aptos"},
		{"aptos-mainnet", "APT", "service-aptos", "APTOS"},
		{"ton-mainnet", "GRAM", "service-ton", "TON"},
		{"ton-mainnet", "GRAM", "service-ton", "Toncoin"},
		{"ton-mainnet", "GRAM", "service-ton", "TON (The Open Network)"},
	} {
		t.Run(row.network+"/"+row.chainType, func(t *testing.T) {
			account, asset := nativeRoute(row.network, row.symbol)
			if e := ValidateRoute(account, asset, row.chain, row.chainType, ""); e != nil {
				t.Fatal(e)
			}
			// A shared address/ticker never makes another ledger a valid route.
			if e := ValidateRoute(account, asset, row.chain, "Arbitrum One", ""); !errors.Is(e, ErrRoute) {
				t.Fatal("foreign ledger accepted", e)
			}
			if e := ValidateRoute(account, asset, row.chain, row.chainType, publicUSDTContract); !errors.Is(e, ErrRoute) {
				t.Fatal("native/token confusion", e)
			}
		})
	}
	account, _ := nativeRoute("ethereum-mainnet", "ETH")
	token := model.Asset{ID: "ethereum-mainnet:usdt", Symbol: "USDT", Decimals: 6, Contract: publicUSDTContract}
	for _, contract := range []string{publicUSDTContract, strings.ToLower(publicUSDTContract), strings.ToUpper(publicUSDTContract)} {
		if e := ValidateRoute(account, token, "ETH", "Ethereum", contract); e != nil {
			t.Fatal("full contract rejected", e)
		}
	}
	for _, contract := range []string{"", publicUSDTContract[len(publicUSDTContract)-6:], "0x1111111111111111111111111111111111111111"} {
		if e := ValidateRoute(account, token, "ETH", "Ethereum", contract); !errors.Is(e, ErrRoute) {
			t.Fatal("wrong/truncated contract accepted", e)
		}
	}
	base, _ := nativeRoute("base-mainnet", "ETH")
	if ValidateRoute(base, token, "ETH", "Ethereum", publicUSDTContract) == nil {
		t.Fatal("Ethereum token selected on Base")
	}
}

func TestExchangeRouteRejectsWrongEnvironmentUnsupportedAndMalformedSelections(t *testing.T) {
	account, asset := nativeRoute("ethereum-mainnet", "ETH")
	for _, row := range []struct {
		account                    model.Account
		asset                      model.Asset
		chain, chainType, contract string
	}{
		{account, asset, "", "Ethereum", ""},
		{account, asset, strings.Repeat("A", 41), "Ethereum", ""},
		{account, asset, "BASE", "Ethereum", ""},
		{account, asset, "ETH", "ethereum", ""},
		{account, model.Asset{ID: "", Symbol: "ETH"}, "ETH", "Ethereum", ""},
		{account, model.Asset{ID: "base-mainnet:native", Symbol: "ETH"}, "ETH", "Ethereum", ""},
		{account, model.Asset{ID: "ethereum-mainnet:unregistered", Symbol: "ETH"}, "ETH", "Ethereum", ""},
	} {
		if e := ValidateRoute(row.account, row.asset, row.chain, row.chainType, row.contract); !errors.Is(e, ErrRoute) {
			t.Fatalf("unsafe selection accepted: %+v %v", row, e)
		}
	}
	for _, network := range []string{"ethereum-sepolia", "arbitrum-mainnet", "arbitrum-sepolia", "ton-testnet", "near-testnet", "aptos-testnet", "decred-mainnet", "decred-testnet", "hypercore-mainnet", "hypercore-testnet", "unknown"} {
		account, asset := nativeRoute(network, "HYPE")
		for _, label := range []string{"Ethereum", "Arbitrum One", "TON", "HyperEVM", "NEAR", "APTOS", "Decred", "HyperCore"} {
			if ValidateRoute(account, asset, "ETH", label, "") == nil {
				t.Fatal("unsupported/environment substitution", network, label)
			}
		}
	}
}

func TestExchangeMatchRouteRejectsAmbiguityAndPreservesProviderMaintenance(t *testing.T) {
	account, asset := nativeRoute("ton-mainnet", "GRAM")
	route := bybit.Chain{Chain: "service-ton", ChainType: "TON", ChainDeposit: "0", ChainWithdraw: "1", DepositMin: "1", WithdrawFee: "0.1", WithdrawPercentageFee: "0", MinAccuracy: "9", ContractAddress: ""}
	coin := bybit.Coin{Coin: "GRAM", Name: "Gram", Chains: []bybit.Chain{
		{Chain: "ETH", ChainType: "Ethereum", ContractAddress: publicUSDTContract}, route,
	}}
	got, e := MatchRoute(account, asset, coin)
	if e != nil || got != route {
		t.Fatal("route/maintenance fields changed", got, e)
	}
	got.Chain = "mutated"
	if coin.Chains[1].Chain != "service-ton" {
		t.Fatal("result aliased provider state")
	}
	other := route
	other.Chain = "alternate-ton"
	other.ChainType = "Toncoin"
	ambiguous := coin
	ambiguous.Chains = append(append([]bybit.Chain(nil), coin.Chains...), other)
	if _, e := MatchRoute(account, asset, ambiguous); !errors.Is(e, ErrRoute) {
		t.Fatal("ambiguous route selected", e)
	}
	foreign := coin
	foreign.Coin = "USDT"
	if _, e := MatchRoute(account, asset, foreign); !errors.Is(e, ErrRoute) {
		t.Fatal("wrong coin selected", e)
	}
	missing := coin
	missing.Chains = coin.Chains[:1]
	if _, e := MatchRoute(account, asset, missing); !errors.Is(e, ErrRoute) {
		t.Fatal("missing network invented", e)
	}
	if _, e := MatchRoute(account, asset, bybit.Coin{Coin: "GRAM"}); !errors.Is(e, ErrRoute) {
		t.Fatal("empty route accepted", e)
	}
}

func TestGramNativeRoutesRejectOldTickerAndTokenImpostorsForNewOperations(t *testing.T) {
	account, canonical := nativeRoute("ton-mainnet", "GRAM")
	for _, provider := range []string{"bybit", "binance", "kucoin"} {
		coin := bybit.Coin{Coin: "GRAM", Name: "Gram", Chains: []bybit.Chain{{Chain: "TON", ChainType: "TON"}}}
		if _, err := MatchProviderRoute(provider, account, canonical, coin); err != nil {
			t.Fatal("canonical GRAM route unavailable", provider, err)
		}
		for _, change := range []func(*model.Asset){
			func(a *model.Asset) { a.Symbol = "TON" },
			func(a *model.Asset) { a.Decimals = 18 },
			func(a *model.Asset) { a.ID = "ton-mainnet:GRAM" },
			func(a *model.Asset) { a.ID = "ton-mainnet:jetton:public-impostor"; a.Contract = "public-impostor" },
			func(a *model.Asset) { a.Contract = "public-impostor" },
		} {
			bad := canonical
			change(&bad)
			if ValidateProviderRoute(provider, account, bad, "TON", "TON", bad.Contract) == nil {
				t.Fatal("ticker/precision/token substitution authorized", provider, bad)
			}
		}
		coin.Coin = "TON"
		if _, err := MatchProviderRoute(provider, account, canonical, coin); err == nil {
			t.Fatal("old TON service asset reused for new GRAM operation", provider)
		}
	}
}
