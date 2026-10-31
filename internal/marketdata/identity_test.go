package marketdata

import (
	"localwallet/internal/model"
	"testing"
)

func nativeNetwork(id, family, chain string, decimals uint8) model.Network {
	return model.Network{ID: id, Family: family, ChainID: chain, Assets: []model.Asset{{ID: id + ":native", Decimals: decimals}}}
}
func TestReviewedIdentityMappingDoesNotTrustSymbols(t *testing.T) {
	cases := []struct {
		network model.Network
		asset   model.Asset
		pair    string
	}{
		{nativeNetwork("ethereum-mainnet", "evm", "1", 18), model.Asset{ID: "ethereum-mainnet:native", Symbol: "BTC", Decimals: 18}, "ETHUSDT"},
		{nativeNetwork("base-mainnet", "evm", "8453", 18), model.Asset{ID: "base-mainnet:native", Decimals: 18}, "ETHUSDT"},
		{nativeNetwork("arbitrum-mainnet", "evm", "42161", 18), model.Asset{ID: "arbitrum-mainnet:native", Decimals: 18}, "ETHUSDT"},
		{nativeNetwork("hyperevm-mainnet", "evm", "999", 18), model.Asset{ID: "hyperevm-mainnet:native", Decimals: 18}, "HYPEUSDT"},
		{nativeNetwork("near-mainnet", "near", "mainnet", 24), model.Asset{ID: "near-mainnet:native", Decimals: 24}, "NEARUSDT"},
		{nativeNetwork("aptos-mainnet", "aptos", "1", 8), model.Asset{ID: "aptos-mainnet:native", Decimals: 8}, "APTUSDT"},
		{nativeNetwork("ton-mainnet", "ton", "-239", 9), model.Asset{ID: "ton-mainnet:TON", Decimals: 9}, "GRAMUSDT"},
		{nativeNetwork("hypercore-mainnet", "hypercore", "HyperCore Mainnet", 8), model.Asset{ID: "hypercore-mainnet:HYPE:0x0d01dc56dcaaca66ad901c959b4011ec", Contract: "HYPE:0x0d01dc56dcaaca66ad901c959b4011ec", Decimals: 8}, "HYPEUSDT"},
	}
	for _, row := range []struct {
		network, family, chain, contract, pair, kind string
		decimals                                     uint8
	}{
		{"ethereum-mainnet", "evm", "1", "0xdAC17F958D2ee523a2206206994597C13D831ec7", "USDT", "erc20", 6},
		{"ethereum-mainnet", "evm", "1", "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "USDCUSDT", "erc20", 6},
		{"ethereum-mainnet", "evm", "1", "0xB50721BCf8d664c30412Cfbc6cf7a15145234ad1", "ARBUSDT", "erc20", 18},
		{"base-mainnet", "evm", "8453", "0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229", "ZENUSDT", "erc20", 18},
		{"base-mainnet", "evm", "8453", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", "USDCUSDT", "erc20", 6},
		{"base-mainnet", "evm", "8453", "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2", "USDT", "erc20", 6},
		{"hyperevm-mainnet", "evm", "999", "0xb88339CB7199b77E23DB6E890353E22632Ba630f", "USDCUSDT", "erc20", 6},
		{"arbitrum-mainnet", "evm", "42161", "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", "USDT", "erc20", 6},
		{"arbitrum-mainnet", "evm", "42161", "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", "USDCUSDT", "erc20", 6},
		{"arbitrum-mainnet", "evm", "42161", "0x912CE59144191C1204E64559FE8253a0e49E6548", "ARBUSDT", "erc20", 18},
		{"near-mainnet", "near", "mainnet", "17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1", "USDCUSDT", "nep141", 6},
	} {
		cases = append(cases, struct {
			network model.Network
			asset   model.Asset
			pair    string
		}{model.Network{ID: row.network, Family: row.family, ChainID: row.chain}, model.Asset{ID: row.network + ":" + row.kind + ":" + row.contract, Contract: row.contract, Decimals: row.decimals, Symbol: "untrusted-label"}, row.pair})
	}
	cases = append(cases, struct {
		network model.Network
		asset   model.Asset
		pair    string
	}{
		model.Network{ID: "hypercore-mainnet", Family: "hypercore", ChainID: "HyperCore Mainnet"},
		model.Asset{ID: "hypercore-mainnet:USDC:0x6d1e7cde53ba9467b783cb7c530ce054", Contract: "USDC:0x6d1e7cde53ba9467b783cb7c530ce054", Decimals: 8, Symbol: "untrusted-label"}, "USDCUSDT",
	}, struct {
		network model.Network
		asset   model.Asset
		pair    string
	}{
		model.Network{ID: "ton-mainnet", Family: "ton", ChainID: "-239"},
		model.Asset{ID: "ton-mainnet:USDT", Contract: "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe", Decimals: 6, Symbol: "untrusted-label"}, "USDT",
	})
	for _, row := range cases {
		t.Run(row.asset.ID, func(t *testing.T) {
			pair, ok := IdentityPair(row.network, row.asset)
			if !ok || pair != row.pair {
				t.Fatalf("reviewed identity rejected %s %t", pair, ok)
			}
			for _, mutate := range []func(*model.Network, *model.Asset){func(n *model.Network, a *model.Asset) { n.Testnet = true }, func(n *model.Network, a *model.Asset) { n.ChainID += "-wrong" }, func(n *model.Network, a *model.Asset) { a.Decimals++ }, func(n *model.Network, a *model.Asset) { a.ID += "-wrong" }, func(n *model.Network, a *model.Asset) { n.Family = "foreign" }} {
				network, asset := row.network, row.asset
				mutate(&network, &asset)
				if _, ok := IdentityPair(network, asset); ok {
					t.Fatal("mutated identity was priced")
				}
			}
		})
	}
}
func TestUnknownAndRetiredIdentitiesCannotBecomeFiatValues(t *testing.T) {
	ethereum := nativeNetwork("ethereum-mainnet", "evm", "1", 18)
	for _, symbol := range []string{"ETH", "BTC", "USDT", "USDC", "ZEN"} {
		asset := model.Asset{ID: "ethereum-mainnet:erc20:0x1111111111111111111111111111111111111111", Contract: "0x1111111111111111111111111111111111111111", Symbol: symbol, Decimals: 6}
		if _, ok := IdentityPair(ethereum, asset); ok {
			t.Fatalf("symbol %s granted pricing identity", symbol)
		}
	}
	for _, id := range []string{"decred-mainnet", "horizen-mainnet", "near-testnet", "arbitrum-sepolia", "other-mainnet"} {
		if ActiveMainnet(model.Network{ID: id}) {
			t.Fatal("unknown/test/retired network active")
		}
	}
	pairs := SupportedPairs()
	pairs[0] = "spoof"
	if SupportedPairs()[0] == "spoof" {
		t.Fatal("mutable pair allowlist")
	}
}

func TestStablecoinLedgerPrecisionAndIdentityCannotBeSubstituted(t *testing.T) {
	core := model.Network{ID: "hypercore-mainnet", Family: "hypercore", ChainID: "HyperCore Mainnet"}
	evm := model.Network{ID: "hyperevm-mainnet", Family: "evm", ChainID: "999"}
	for _, row := range []struct {
		n model.Network
		a model.Asset
	}{
		{core, model.Asset{ID: "hypercore-mainnet:USDC:0x6d1e7cde53ba9467b783cb7c530ce054", Contract: "USDC:0x6d1e7cde53ba9467b783cb7c530ce054", Decimals: 6, Symbol: "USDC"}},
		{core, model.Asset{ID: "hypercore-mainnet:USDC:0xeb62eee3685fc4c43992febcd9e75443", Contract: "USDC:0xeb62eee3685fc4c43992febcd9e75443", Decimals: 8, Symbol: "USDC"}},
		{evm, model.Asset{ID: "hyperevm-mainnet:erc20:0x6b9e773128f453f5c2c60935ee2de2cbc5390a24", Contract: "0x6b9e773128f453f5c2c60935ee2de2cbc5390a24", Decimals: 6, Symbol: "USDC"}},
		{model.Network{ID: "base-mainnet", Family: "evm", ChainID: "8453"}, model.Asset{ID: "base-mainnet:erc20:0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", Contract: "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", Decimals: 6, Symbol: "USDT0"}},
	} {
		if _, ok := IdentityPair(row.n, row.a); ok {
			t.Fatal("foreign stablecoin identity or precision valued", row.a)
		}
	}
}
