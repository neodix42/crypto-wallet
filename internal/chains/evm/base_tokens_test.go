package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"localwallet/internal/model"
)

func TestBaseERC20ImportInspectsBalancesAndKeepsExactNetworkPolicy(t *testing.T) {
	for _, network := range []string{"base-mainnet", "base-sepolia"} {
		t.Run(network, func(t *testing.T) {
			f := fakeNode(t, network)
			account, _ := f.a.Derive(syntheticSeed(), 0, network)
			token := importedTokenFixture()
			token.Network, token.Asset.ID = network, network+":erc20:"+token.Asset.Contract
			f.intercept = func(method string, args []json.RawMessage) (any, bool) {
				if method == "eth_getCode" {
					var contract string
					_ = json.Unmarshal(args[0], &contract)
					if contract == token.Asset.Contract {
						return "0x6001", true
					}
				}
				if method == "eth_call" {
					var call map[string]string
					_ = json.Unmarshal(args[0], &call)
					if call["to"] == token.Asset.Contract {
						switch call["data"] {
						case callData("symbol()"):
							return tokenSymbolABI("TEST"), true
						case callData("decimals()"):
							return "0x" + common.Bytes2Hex(word(big.NewInt(6))), true
						case callData("balanceOf(address)", common.BytesToHash(common.HexToAddress(account.Address).Bytes()).Bytes()):
							return "0x" + common.Bytes2Hex(word(big.NewInt(1234567))), true
						}
					}
				}
				return nil, false
			}
			got, err := f.a.InspectToken(context.Background(), account, token.Asset.Contract)
			if err != nil || got != token {
				t.Fatal("Base token metadata not checked", err)
			}
			with, err := f.a.WithTokens([]model.ERC20Token{got})
			if err != nil {
				t.Fatal(err)
			}
			balances, err := with.Balance(context.Background(), account)
			if err != nil || len(balances) != 3 || balances[2].Asset != token.Asset.ID || balances[2].Atomic != "1234567" {
				t.Fatal("Base token balance missing or decimal precision guessed", err)
			}
			for _, n := range with.Networks() {
				if n.ID != network {
					for _, asset := range n.Assets {
						if asset.ID == token.Asset.ID {
							t.Fatal("Base token appeared on a foreign network")
						}
					}
				}
			}
		})
	}
	for _, network := range []string{"ethereum-mainnet", "ethereum-sepolia", "base-mainnet", "base-sepolia"} {
		if !SupportsTokenImport(network) {
			t.Fatal("supported token network missing")
		}
	}
	for _, network := range []string{"", "hyperevm-mainnet", "horizen-mainnet", "base-unknown"} {
		if SupportsTokenImport(network) {
			t.Fatal("foreign network acquired token authority")
		}
	}
}
