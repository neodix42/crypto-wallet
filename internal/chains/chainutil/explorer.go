package chainutil

import (
	"encoding/hex"
	"strings"

	"github.com/decred/base58"
)

// TransactionExplorer constructs a known network transaction link. No provider
// URL, host, credential, path or query can select browser navigation authority.
// Local chains and unreviewed explorer routes deliberately have no public link.
func TransactionExplorer(network, identity string) string {
	evm := false
	base := ""
	switch network {
	case "ethereum-mainnet":
		base = "https://etherscan.io/tx/"
		evm = true
	case "ethereum-sepolia":
		base = "https://sepolia.etherscan.io/tx/"
		evm = true
	case "base-mainnet":
		base = "https://basescan.org/tx/"
		evm = true
	case "base-sepolia":
		base = "https://sepolia.basescan.org/tx/"
		evm = true
	case "arbitrum-mainnet":
		base = "https://arbiscan.io/tx/"
		evm = true
	case "arbitrum-sepolia":
		base = "https://sepolia.arbiscan.io/tx/"
		evm = true
	case "hyperevm-mainnet":
		base = "https://hyperevmscan.io/tx/"
		evm = true
	case "hypercore-mainnet":
		base = "https://app.hyperliquid.xyz/explorer/tx/"
		evm = true
	case "hypercore-testnet":
		base = "https://app.hyperliquid-testnet.xyz/explorer/tx/"
		evm = true
	case "near-mainnet", "near-testnet":
		if !validBase58Hash(identity) {
			return ""
		}
		if network == "near-mainnet" {
			return "https://nearblocks.io/txns/" + identity
		}
		return "https://testnet.nearblocks.io/txns/" + identity
	case "aptos-mainnet", "aptos-testnet":
		if !validHexHash(identity, true) {
			return ""
		}
		suffix := "mainnet"
		if network == "aptos-testnet" {
			suffix = "testnet"
		}
		return "https://explorer.aptoslabs.com/txn/" + identity + "?network=" + suffix
	case "decred-mainnet":
		base = "https://dcrdata.decred.org/tx/"
	case "decred-testnet3":
		base = "https://testnet.dcrdata.org/tx/"
	case "ton-mainnet":
		base = "https://tonviewer.com/transaction/"
	case "ton-testnet":
		base = "https://testnet.tonviewer.com/transaction/"
	default:
		return ""
	}
	if !validHexHash(identity, evm) {
		return ""
	}
	return base + identity
}
func validBase58Hash(s string) bool {
	return len(s) <= 44 && len(base58.Decode(s)) == 32 && base58.Encode(base58.Decode(s)) == s
}
func validHexHash(s string, prefix bool) bool {
	if prefix {
		if !strings.HasPrefix(s, "0x") {
			return false
		}
		s = s[2:]
	}
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
