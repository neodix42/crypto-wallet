package evm

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"strings"
)

// Canonical issuer/network deployments checked read-only on 2026-10-31.
// Base USDT is the canonical standard-bridge representation, not issuer-native
// USDT or USDT0. HyperEVM uses Circle-native USDC, not USDC.e.
const (
	USDCContract         = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	USDCAsset            = "ethereum-mainnet:erc20:" + USDCContract
	ArbitrumUSDCContract = "0xaf88d065e77c8cC2239327C5EDb3A432268e5831"
	ArbitrumUSDCAsset    = "arbitrum-mainnet:erc20:" + ArbitrumUSDCContract
	ArbitrumUSDTContract = "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9"
	ArbitrumUSDTAsset    = "arbitrum-mainnet:erc20:" + ArbitrumUSDTContract
	BaseUSDCContract     = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	BaseUSDCAsset        = "base-mainnet:erc20:" + BaseUSDCContract
	BaseUSDTContract     = "0xfde4C96c8593536E31F229EA8f37b2ADa2699bb2"
	BaseUSDTAsset        = "base-mainnet:erc20:" + BaseUSDTContract
	HyperEVMUSDCContract = "0xb88339CB7199b77E23DB6E890353E22632Ba630f"
	HyperEVMUSDCAsset    = "hyperevm-mainnet:erc20:" + HyperEVMUSDCContract
)

const circleImplementationSlot = "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3"

type stablecoinPolicy struct {
	codeHash, implementationSlot, implementation, implementationHash, symbol string
	circle, bridged                                                          bool
}

// Proxy runtime and current implementation are both pinned. Issuer/admin power
// remains trusted; a change requires a reviewed policy update, not autoapproval.
var stablecoinPolicies = map[string]stablecoinPolicy{
	USDCAsset:         {codeHash: "0xd80d4b7c890cb9d6a4893e6b52bc34b56b25335cb13716e0d1d31383e6b41505", implementationSlot: "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3", implementation: "0x43506849d7c04f9138d1a2050bbf3a0c054402dd", implementationHash: "0xcdfb7d322961af3acae7a8f7ee8b69c205b36f576cc5b077f170c7eb8ecbe3ea", symbol: "USDC", circle: true},
	ArbitrumUSDCAsset: {codeHash: "0xad30d819dbc47814b7e6cb837fd7cc57fcb591479a38596ee93de4fc52e8c435", implementationSlot: "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3", implementation: "0x86e721b43d4ecfa71119dd38c0f938a75fdb57b3", implementationHash: "0xda0578bf7fe0d04e320e166ab8f98061328fda8ae0a299882aeb38f1543c6a9d", symbol: "USDC", circle: true},
	ArbitrumUSDTAsset: {codeHash: "0xfc1ea81db44e2de921b958dc92da921a18968ff3f3465bd475fb86dd1af03986", implementationSlot: "0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc", implementation: "0x3263cd783823d04a6b9819517e0e6840d37ca3f4", implementationHash: "0x1d4c20a1a19a0e182c0d2bffbe433a96bd70a1dbb383b7a2d293976df4042279", symbol: "USD₮0"},
	BaseUSDCAsset:     {codeHash: "0xa6705a10bb756b5dea144591118be77d7af0c3eee3bf2dfe2583dcb0364fefab", implementationSlot: "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3", implementation: "0x2ce6311ddae708829bc0784c967b7d77d19fd779", implementationHash: "0x11b75a237997ab8328f65b2d5a55c10f0346d0a175741ed42ddf4f2c66b9e873", symbol: "USDC", circle: true},
	BaseUSDTAsset:     {codeHash: "0x478746a8e4b17181c4efbeb2069d86f22ec395788099ed65c9442ca44df265bc", symbol: "USDT", bridged: true},
	HyperEVMUSDCAsset: {codeHash: "0xa119f674d44072e257f162aee49c7aba9b7a04fe2e8c84fcefddb366ab6d40cd", implementationSlot: "0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3", implementation: "0x003f73f58ca78880da3642c5cb71d7459b3fe4f8", implementationHash: "0x8cfef4ac48d43548c119ca5858411f006f968896d98d7ed43d4ccad2ae9d355e", symbol: "USDC", circle: true},
}

func defaultStablecoins(network string) []model.Asset {
	switch network {
	case "ethereum-mainnet":
		return []model.Asset{{ID: USDCAsset, Symbol: "USDC", Decimals: 6, Contract: USDCContract}}
	case "arbitrum-mainnet":
		return []model.Asset{{ID: ArbitrumUSDTAsset, Symbol: "USDT0", Decimals: 6, Contract: ArbitrumUSDTContract}, {ID: ArbitrumUSDCAsset, Symbol: "USDC", Decimals: 6, Contract: ArbitrumUSDCContract}}
	case "base-mainnet":
		return []model.Asset{{ID: BaseUSDTAsset, Symbol: "USDT", Decimals: 6, Contract: BaseUSDTContract}, {ID: BaseUSDCAsset, Symbol: "USDC", Decimals: 6, Contract: BaseUSDCContract}}
	case "hyperevm-mainnet":
		return []model.Asset{{ID: HyperEVMUSDCAsset, Symbol: "USDC", Decimals: 6, Contract: HyperEVMUSDCContract}}
	}
	return nil
}

// IsPromotedDefault identifies only newly pinned exact network/contract IDs.
// Previously approved bounded metadata stays encrypted for old journals, but
// its symbol, precision and hash never override the current pinned policy.
func IsPromotedDefault(token model.ERC20Token) bool {
	for _, asset := range defaultStablecoins(token.Network) {
		if asset.ID == token.Asset.ID && asset.Contract == token.Asset.Contract {
			return true
		}
	}
	return false
}

func (a *Adapter) stablecoinIdentity(ctx context.Context, n model.Network, asset model.Asset, at string, policy stablecoinPolicy) error {
	if policy.implementationSlot != "" {
		var implementation, code string
		if err := a.rpc(ctx, n, "eth_getStorageAt", []any{asset.Contract, policy.implementationSlot, at}, &implementation); err != nil {
			return err
		}
		if implementation != "0x"+strings.Repeat("0", 24)+policy.implementation[2:] {
			return errors.New("stablecoin implementation requires a reviewed policy update")
		}
		if err := a.rpc(ctx, n, "eth_getCode", []any{policy.implementation, at}, &code); err != nil {
			return err
		}
		if len(code) < 4 || len(code) > 2<<20 || !cleanHex(code) {
			return errors.New("stablecoin implementation code unavailable")
		}
		decoded, err := hex.DecodeString(code[2:])
		if err != nil || crypto.Keccak256Hash(decoded).Hex() != policy.implementationHash {
			return errors.New("stablecoin implementation code requires a reviewed policy update")
		}
	}
	var symbol string
	if err := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": asset.Contract, "data": callData("symbol()")}, at}, &symbol); err != nil {
		return err
	}
	// These exact pinned symbols include the issuer's UTF-8 USD₮0. Do not
	// broaden arbitrary imported-token symbol or nonstandard transfer policy.
	encoded := make([]byte, 96)
	encoded[31] = 32
	encoded[63] = byte(len(policy.symbol))
	copy(encoded[64:], policy.symbol)
	if symbol != "0x"+hex.EncodeToString(encoded) {
		return errors.New("stablecoin symbol differs from its reviewed identity")
	}
	if policy.circle {
		var raw string
		if err := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": asset.Contract, "data": callData("paused()")}, at}, &raw); err != nil {
			return err
		}
		paused, err := abiInteger(raw)
		if err != nil || paused.Sign() != 0 {
			return errors.New("USDC issuer state requires a reviewed policy update")
		}
	}
	if policy.bridged {
		for signature, address := range map[string]string{"l1Token()": USDTContract, "l2Bridge()": "0x4200000000000000000000000000000000000010"} {
			var raw string
			if err := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": asset.Contract, "data": callData(signature)}, at}, &raw); err != nil {
				return err
			}
			if raw != "0x"+strings.Repeat("0", 24)+strings.ToLower(address[2:]) {
				return errors.New("Base USDT bridge identity mismatch")
			}
		}
	}
	return nil
}
