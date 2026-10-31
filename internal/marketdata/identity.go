package marketdata

import (
	"localwallet/internal/model"
	"strings"
)

// IdentityPair identifies only reviewed mainnet assets, never an arbitrary
// token's display ticker. It grants no token import, exchange route or signing
// authority. USDT is the reporting unit itself only for the pinned issuer token.
// Primary contract references:
// https://tether.to/en/supported-protocols/
// https://developers.circle.com/stablecoins/usdc-contract-addresses
// https://docs.arbitrum.foundation/deployment-addresses
// https://docs.usdt0.to/technical-documentation/deployments
// https://docs.base.org/base-chain/network-information/base-contracts
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/spot
// TON/GRAM 1:1 market rename, without changing the on-chain identity:
// https://ton.org/media
// https://announcements.bybit.com/en/article/bybit-toncoin-ton-gram-gram--blt617f4bf48950ab77/
// Base ZEN and HyperCore HYPE are the existing independently pinned registry
// identities (BLOCKCHAIN_COMPATIBILITY.md), not inferred wrapped equivalents.
func IdentityPair(network model.Network, asset model.Asset) (string, bool) {
	if network.Testnet {
		return "", false
	}
	native := func(family, chain, pair string, decimals uint8) (string, bool) {
		return pair, network.Family == family && network.ChainID == chain && asset.ID == network.ID+":native" && asset.Contract == "" && asset.Decimals == decimals
	}
	switch network.ID {
	case "ethereum-mainnet":
		if asset.Contract == "" {
			return native("evm", "1", "ETHUSDT", 18)
		}
		if network.Family != "evm" || network.ChainID != "1" {
			return "", false
		}
		switch strings.ToLower(asset.Contract) {
		case "0xdac17f958d2ee523a2206206994597c13d831ec7":
			return tokenPair(network, asset, "erc20", "USDT", 6)
		case "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48":
			return tokenPair(network, asset, "erc20", "USDCUSDT", 6)
		case "0xb50721bcf8d664c30412cfbc6cf7a15145234ad1":
			return tokenPair(network, asset, "erc20", "ARBUSDT", 18)
		}
	case "base-mainnet":
		if asset.Contract == "" {
			return native("evm", "8453", "ETHUSDT", 18)
		}
		if network.Family != "evm" || network.ChainID != "8453" {
			return "", false
		}
		switch strings.ToLower(asset.Contract) {
		case "0xf43eb8de897fbc7f2502483b2bef7bb9ea179229":
			return tokenPair(network, asset, "erc20", "ZENUSDT", 18)
		case "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913":
			return tokenPair(network, asset, "erc20", "USDCUSDT", 6)
		case "0xfde4c96c8593536e31f229ea8f37b2ada2699bb2":
			// Canonical Base bridge USDT, distinct from native USDC/USDT0.
			return tokenPair(network, asset, "erc20", "USDT", 6)
		}
	case "arbitrum-mainnet":
		if asset.Contract == "" {
			return native("evm", "42161", "ETHUSDT", 18)
		}
		if network.Family != "evm" || network.ChainID != "42161" {
			return "", false
		}
		switch strings.ToLower(asset.Contract) {
		case "0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9":
			return tokenPair(network, asset, "erc20", "USDT", 6)
		case "0xaf88d065e77c8cc2239327c5edb3a432268e5831":
			return tokenPair(network, asset, "erc20", "USDCUSDT", 6)
		case "0x912ce59144191c1204e64559fe8253a0e49e6548":
			return tokenPair(network, asset, "erc20", "ARBUSDT", 18)
		}
	case "hyperevm-mainnet":
		if asset.Contract == "" {
			return native("evm", "999", "HYPEUSDT", 18)
		}
		if network.Family == "evm" && network.ChainID == "999" && strings.EqualFold(asset.Contract, "0xb88339CB7199b77E23DB6E890353E22632Ba630f") {
			return tokenPair(network, asset, "erc20", "USDCUSDT", 6)
		}
	case "hypercore-mainnet":
		if network.Family != "hypercore" || network.ChainID != "HyperCore Mainnet" || asset.ID != network.ID+":"+asset.Contract || asset.Decimals != 8 {
			return "", false
		}
		switch asset.Contract {
		case "HYPE:0x0d01dc56dcaaca66ad901c959b4011ec":
			return "HYPEUSDT", true
		case "USDC:0x6d1e7cde53ba9467b783cb7c530ce054":
			return "USDCUSDT", true
		}
	case "near-mainnet":
		if asset.Contract == "" {
			return native("near", "mainnet", "NEARUSDT", 24)
		}
		if network.Family == "near" && network.ChainID == "mainnet" && asset.Contract == "17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1" {
			return tokenPair(network, asset, "nep141", "USDCUSDT", 6)
		}
	case "aptos-mainnet":
		return native("aptos", "1", "APTUSDT", 8)
	case "ton-mainnet":
		if network.Family == "ton" && network.ChainID == "-239" && asset.ID == "ton-mainnet:USDT" && asset.Contract == "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe" && asset.Decimals == 6 {
			return "USDT", true
		}
		return "GRAMUSDT", network.Family == "ton" && network.ChainID == "-239" && asset.ID == network.ID+":TON" && asset.Contract == "" && asset.Decimals == 9
	}
	return "", false
}
func tokenPair(network model.Network, asset model.Asset, kind, pair string, decimals uint8) (string, bool) {
	return pair, asset.ID == network.ID+":"+kind+":"+asset.Contract && asset.Decimals == decimals
}

// ActiveMainnet excludes retained retired descriptors and test/local profiles.
func ActiveMainnet(network model.Network) bool {
	if network.Testnet {
		return false
	}
	switch network.ID {
	case "ethereum-mainnet", "base-mainnet", "arbitrum-mainnet", "hyperevm-mainnet", "hypercore-mainnet", "near-mainnet", "aptos-mainnet", "ton-mainnet":
		return true
	}
	return false
}
