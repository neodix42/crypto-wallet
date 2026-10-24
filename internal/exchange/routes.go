// Package exchange binds exchange ledger routes to registered wallet identities.
package exchange

import (
	"errors"
	"strings"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

var ErrRoute = errors.New("Bybit does not provide an unambiguous supported wallet network")

// ValidateRoute treats chain identifiers as Bybit service identifiers, never as
// EVM chain IDs. Explicit ledger labels and exact token contracts must match;
// the core must obtain all fields from the authenticated selected service.
func ValidateRoute(account model.Account, asset model.Asset, chain, chainType, contract string) error {
	if chain == "" || len(chain) > 40 || asset.ID == "" || !strings.HasPrefix(asset.ID, account.Network+":") {
		return ErrRoute
	}
	if asset.Contract == "" {
		nativeID := account.Network + ":native"
		if account.Network == "ton-mainnet" {
			nativeID = "ton-mainnet:TON"
		}
		if contract != "" || asset.ID != nativeID {
			return ErrRoute
		}
	} else if !strings.EqualFold(asset.Contract, contract) {
		return ErrRoute
	}
	names := map[string][]string{
		"ethereum-mainnet": {"Ethereum", "Ethereum(ERC20)", "Ethereum (ERC20)", "ERC20", "ETH"},
		"base-mainnet":     {"Base", "BASE"},
		"horizen-mainnet":  {"Horizen", "Horizen Chain"},
		"hyperevm-mainnet": {"HyperEVM", "Hyperliquid HyperEVM"},
		"near-mainnet":     {"NEAR", "NEAR Protocol", "Near Protocol"},
		"aptos-mainnet":    {"APTOS", "Aptos"},
		"ton-mainnet":      {"TON", "Toncoin", "TON (The Open Network)"},
	}
	if account.Network == "ethereum-mainnet" && chain != "ETH" {
		return ErrRoute
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
	if coin.Coin != asset.Symbol {
		return bybit.Chain{}, ErrRoute
	}
	var match *bybit.Chain
	for _, chain := range coin.Chains {
		if ValidateRoute(account, asset, chain.Chain, chain.ChainType, chain.ContractAddress) != nil {
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
	return *match, nil
}
