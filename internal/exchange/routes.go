// Package exchange binds exchange ledger routes to registered wallet identities.
package exchange

import (
	"errors"
	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"strings"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

var ErrRoute = errors.New("Bybit does not provide an unambiguous supported wallet network")

// USDT0 is the reviewed Arbitrum representation of the exchange's USDT
// ledger. The immutable wallet asset ID/contract and on-chain precision stay.
func LedgerAsset(asset model.Asset) model.Asset {
	if asset.ID == evm.ArbitrumUSDTAsset && asset.Contract == evm.ArbitrumUSDTContract && asset.Symbol == "USDT0" {
		asset.Symbol = "USDT"
	}
	return asset
}

func ContractMatches(network, a, b string) bool {
	if network == "ton-mainnet" {
		parse := func(s string) *address.Address {
			var a *address.Address
			var err error
			if strings.Contains(s, ":") {
				a, err = address.ParseRawAddr(s)
			} else {
				a, err = address.ParseAddr(s)
			}
			if err != nil {
				return nil
			}
			return a
		}
		x, y := parse(a), parse(b)
		return x != nil && y != nil && x.Equals(y)
	}
	return strings.EqualFold(a, b)
}

// ValidateRoute treats chain identifiers as Bybit service identifiers, never as
// EVM chain IDs. Explicit ledger labels and exact token contracts must match;
// the core must obtain all fields from the authenticated selected service.
func ValidateRoute(account model.Account, asset model.Asset, chain, chainType, contract string) error {
	return ValidateProviderRoute("bybit", account, asset, chain, chainType, contract)
}

func ValidateProviderRoute(provider string, account model.Account, asset model.Asset, chain, chainType, contract string) error {
	if _, ok := ProviderByID(provider); !ok {
		return ErrRoute
	}
	if chain == "" || len(chain) > 40 || asset.ID == "" || !strings.HasPrefix(asset.ID, account.Network+":") {
		return ErrRoute
	}
	// TON is the ledger, GRAM is its current native currency. The durable
	// :TON identifier remains unchanged; the ticker cannot admit an unrelated
	// jetton or revive an obsolete TON spot/deposit route for a new operation.
	if account.Network == "ton-mainnet" {
		switch asset.ID {
		case "ton-mainnet:TON":
			if asset.Symbol != "GRAM" || asset.Decimals != 9 || asset.Contract != "" || contract != "" {
				return ErrRoute
			}
		case "ton-mainnet:USDT":
			if provider != "bybit" || asset.Symbol != "USDT" || asset.Decimals != 6 || asset.Contract != ton.USDTMaster || !ContractMatches(account.Network, contract, ton.USDTMaster) {
				return ErrRoute
			}
		default:
			return ErrRoute
		}
	}
	if asset.Contract == "" {
		nativeID := account.Network + ":native"
		if account.Network == "ton-mainnet" {
			nativeID = "ton-mainnet:TON"
		}
		if contract != "" || asset.ID != nativeID {
			return ErrRoute
		}
	} else if !ContractMatches(account.Network, asset.Contract, contract) {
		return ErrRoute
	}
	names := map[string][]string{
		"ethereum-mainnet": {"Ethereum", "Ethereum(ERC20)", "Ethereum (ERC20)", "ERC20", "ETH"},
		"base-mainnet":     {"Base", "BASE"},
		"arbitrum-mainnet": {"Arbitrum One", "Arbitrum", "ARBITRUM"},
		"hyperevm-mainnet": {"HyperEVM", "Hyperliquid HyperEVM", "HYPEREVM"},
		"near-mainnet":     {"NEAR", "NEAR Protocol", "Near Protocol"},
		"aptos-mainnet":    {"APTOS", "Aptos"},
		"ton-mainnet":      {"TON", "Toncoin", "TON (The Open Network)"},
	}
	if account.Network == "arbitrum-mainnet" && (provider != "bybit" || chain != "ARBI") {
		return ErrRoute
	}
	if account.Network == "ethereum-mainnet" {
		expected := "ETH"
		if provider == "kucoin" {
			expected = "eth"
		}
		if chain != expected {
			return ErrRoute
		}
	}
	// Bybit's exact HyperEVM chain code disambiguates the older generic
	// Hyperliquid display name from its separate HyperCore ledger.
	if provider == "bybit" && account.Network == "hyperevm-mainnet" && chain == "HYPEREVM" {
		for _, label := range []string{"Hyperliquid", "HyperLiquid", "Hyperliquid (HYPE)", "HypeEVM"} {
			if chainType == label {
				return nil
			}
		}
	}
	for _, name := range names[account.Network] {
		if chainType == name {
			return nil
		}
	}
	return ErrRoute
}

// MatchRoute rejects ambiguous multiple matches instead of selecting the first
// ticker-compatible chain. Maintenance flags are retained for the caller.
func MatchRoute(account model.Account, asset model.Asset, coin bybit.Coin) (bybit.Chain, error) {
	return MatchProviderRoute("bybit", account, asset, coin)
}

func MatchProviderRoute(provider string, account model.Account, asset model.Asset, coin bybit.Coin) (bybit.Chain, error) {
	if coin.Coin != asset.Symbol {
		return bybit.Chain{}, ErrRoute
	}
	var match *bybit.Chain
	for _, chain := range coin.Chains {
		if ValidateProviderRoute(provider, account, asset, chain.Chain, chain.ChainType, chain.ContractAddress) != nil {
			continue
		}
		if match != nil {
			return bybit.Chain{}, ErrRoute
		}
		copy := chain
		match = &copy
	}
	if match == nil {
		return bybit.Chain{}, ErrRoute
	}
	if coin.InvalidChains[match.Chain] != "" {
		return bybit.Chain{}, &bybit.CoinMetadataError{Field: coin.InvalidChains[match.Chain]}
	}
	return *match, nil
}
