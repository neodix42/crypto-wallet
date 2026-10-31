package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

func baseImportedTokensNode(t *testing.T, network string) (*nodeFixture, model.Account, []model.ERC20Token) {
	t.Helper()
	f := fakeNode(t, network)
	account, err := f.a.Derive(syntheticSeed(), 0, network)
	if err != nil {
		t.Fatal(err)
	}
	contracts := []string{"0x1234567890123456789012345678901234567890", "0x2345678901234567890123456789012345678901"}
	decimals := []uint8{6, 18}
	tokens := make([]model.ERC20Token, len(contracts))
	for i, contract := range contracts {
		contract = common.HexToAddress(contract).Hex()
		tokens[i] = model.ERC20Token{Network: network, Asset: model.Asset{
			ID: network + ":erc20:" + contract, Symbol: "SAME", Decimals: decimals[i], Contract: contract,
		}, CodeHash: crypto.Keccak256Hash([]byte{0x60, byte(i + 1)}).Hex()}
	}
	f.intercept = func(method string, arguments []json.RawMessage) (any, bool) {
		var contract string
		var call map[string]string
		if method == "eth_getCode" {
			_ = json.Unmarshal(arguments[0], &contract)
		} else if method == "eth_call" {
			_ = json.Unmarshal(arguments[0], &call)
			contract = call["to"]
		}
		for i, token := range tokens {
			if contract != token.Asset.Contract {
				continue
			}
			if method == "eth_getCode" {
				return "0x" + common.Bytes2Hex([]byte{0x60, byte(i + 1)}), true
			}
			switch call["data"] {
			case callData("symbol()"):
				return tokenSymbolABI(token.Asset.Symbol), true
			case callData("decimals()"):
				return "0x" + common.Bytes2Hex(word(big.NewInt(int64(token.Asset.Decimals)))), true
			case callData("balanceOf(address)", common.BytesToHash(common.HexToAddress(account.Address).Bytes()).Bytes()):
				value, _ := new(big.Int).SetString([]string{"1234567", "9000000000000000001"}[i], 10)
				return "0x" + common.Bytes2Hex(word(value)), true
			}
		}
		return nil, false
	}
	return f, account, tokens
}

func TestBaseMultipleTokenContractsKeepIndependentBalancesAndSignedNetworkDomains(t *testing.T) {
	for _, networkID := range []string{"base-mainnet", "base-sepolia"} {
		t.Run(networkID, func(t *testing.T) {
			ctx := context.Background()
			f, account, tokens := baseImportedTokensNode(t, networkID)
			for _, token := range tokens {
				observed, err := f.a.InspectToken(ctx, account, token.Asset.Contract)
				if err != nil || observed != token {
					t.Fatal("Base metadata did not preserve exact network, contract, precision and runtime", err)
				}
				if _, err := f.a.Prepare(ctx, account, model.Transfer{AccountID: account.ID, Asset: token.Asset.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "1"}); err == nil {
					t.Fatal("inspected metadata alone installed a spend policy")
				}
			}
			tracked, err := f.a.WithTokens(tokens)
			if err != nil {
				t.Fatal(err)
			}
			balances, err := tracked.Balance(ctx, account)
			if err != nil || len(balances) != len(defaultStablecoins(networkID))+4 {
				t.Fatal("Base ETH, pinned ZEN and both tracked tokens were not observed independently", err)
			}
			byAsset := map[string]model.Balance{}
			for _, balance := range balances {
				if _, duplicate := byAsset[balance.Asset]; duplicate || balance.State != "provider-observed" || balance.At == "" {
					t.Fatal("Base balance identity was duplicated or lost its observed block")
				}
				byAsset[balance.Asset] = balance
			}
			if byAsset[tokens[0].Asset.ID].Atomic != "1234567" || byAsset[tokens[1].Asset.ID].Atomic != "9000000000000000001" {
				t.Fatal("same-symbol contracts replaced one another or used guessed precision")
			}
			network, _ := tracked.network(networkID)
			chainID, _ := new(big.Int).SetString(network.ChainID, 10)
			for tokenIndex, token := range tokens {
				plan, err := tracked.Prepare(ctx, account, model.Transfer{AccountID: account.ID, Asset: token.Asset.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "1234567"})
				if err != nil || plan.FeeAsset != networkID+":native" || plan.Asset != token.Asset.ID {
					t.Fatal("Base token preparation changed its asset or ETH gas domain", err)
				}
				signed, err := tracked.Sign(syntheticSeed(), plan)
				if err != nil {
					t.Fatal(err)
				}
				var tx types.Transaction
				if tx.UnmarshalBinary(signed.Bytes) != nil || tx.ChainId().Cmp(chainID) != 0 || tx.To() == nil || tx.To().Hex() != token.Asset.Contract || tx.Value().Sign() != 0 || len(tx.Data()) != 68 {
					t.Fatal("Base token signature changed its chain, contract, zero native value or transfer shape")
				}
				from, err := types.Sender(types.LatestSignerForChainID(chainID), &tx)
				if err != nil || from.Hex() != account.Address || signed.Identity != tx.Hash().Hex() {
					t.Fatal("signed Base token bytes no longer belong to the selected account")
				}
				for name, alter := range map[string]func(*model.Plan, *payload){
					"foreign chain":    func(_ *model.Plan, body *payload) { body.ChainID = "1" },
					"foreign contract": func(_ *model.Plan, body *payload) { body.To = tokens[1-tokenIndex].Asset.Contract },
					"token gas":        func(plan *model.Plan, _ *payload) { plan.FeeAsset = token.Asset.ID },
					"native value":     func(_ *model.Plan, body *payload) { body.Value = "1" },
				} {
					t.Run(token.Asset.Contract+"/"+name, func(t *testing.T) {
						bad := plan
						var body payload
						if json.Unmarshal(plan.Payload, &body) != nil {
							t.Fatal("invalid canonical plan payload")
						}
						alter(&bad, &body)
						bad.Payload, _ = json.Marshal(body)
						bad.ID = planID(bad)
						if _, err := tracked.Sign(syntheticSeed(), bad); err == nil {
							t.Fatal("inconsistent Base token signing policy accepted", name)
						}
					})
				}
			}
			if f.calls["eth_sendRawTransaction"] != 0 {
				t.Fatal("metadata, observations or offline signing unexpectedly broadcast")
			}
		})
	}
}

func TestBaseTokenFailuresKeepOtherAssetsVisibleAndRequireNativeETH(t *testing.T) {
	for _, networkID := range []string{"base-mainnet", "base-sepolia"} {
		for _, fault := range []string{"runtime", "precision", "false-transfer", "missing-ETH", "wrong-chain"} {
			t.Run(networkID+"/"+fault, func(t *testing.T) {
				ctx := context.Background()
				f, account, tokens := baseImportedTokensNode(t, networkID)
				tracked, err := f.a.WithTokens(tokens)
				if err != nil {
					t.Fatal(err)
				}
				original := f.intercept
				f.intercept = func(method string, arguments []json.RawMessage) (any, bool) {
					if method == "eth_getCode" && fault == "runtime" {
						var contract string
						_ = json.Unmarshal(arguments[0], &contract)
						if contract == tokens[0].Asset.Contract {
							return "0x6009", true
						}
					}
					if method == "eth_call" {
						var call map[string]string
						_ = json.Unmarshal(arguments[0], &call)
						if call["to"] == tokens[0].Asset.Contract {
							if fault == "precision" && call["data"] == callData("decimals()") {
								return "0x" + common.Bytes2Hex(word(big.NewInt(7))), true
							}
							if fault == "false-transfer" && len(call["data"]) == 138 && call["data"][:10] == "0xa9059cbb" {
								return "0x" + common.Bytes2Hex(word(big.NewInt(0))), true
							}
						}
					}
					return original(method, arguments)
				}
				if fault == "missing-ETH" {
					f.override["eth_getBalance"] = "0x0"
				}
				if fault == "wrong-chain" {
					f.override["eth_chainId"] = "0x1"
				}
				balances, err := tracked.Balance(ctx, account)
				if fault == "wrong-chain" {
					if err == nil || len(balances) != 0 {
						t.Fatal("foreign RPC chain became a Base balance observation")
					}
				} else {
					if err != nil || len(balances) != len(defaultStablecoins(networkID))+4 {
						t.Fatal("one failed token discarded Base assets", err)
					}
					byAsset := map[string]model.Balance{}
					for _, balance := range balances {
						byAsset[balance.Asset] = balance
					}
					if balance := byAsset[tokens[1].Asset.ID]; balance.Atomic != "9000000000000000001" || balance.State != "provider-observed" {
						t.Fatal("one failing token hid the independently healthy token balance")
					}
					first := byAsset[tokens[0].Asset.ID]
					if fault == "runtime" || fault == "precision" {
						if first.State != "unavailable" || first.Atomic != "" || first.At == "" {
							t.Fatal("changed runtime or precision became a verified token balance or zero")
						}
					} else if first.State != "provider-observed" || first.Atomic != "1234567" {
						t.Fatal("transfer failure or missing ETH erased an otherwise verified token balance")
					}
					if fault == "missing-ETH" && byAsset[networkID+":native"].Atomic != "0" {
						t.Fatal("missing Base ETH was replaced by token value")
					}
				}
				transfer := model.Transfer{AccountID: account.ID, Asset: tokens[0].Asset.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "1"}
				if _, err := tracked.Prepare(ctx, account, transfer); err == nil {
					t.Fatal("invalid token, missing native gas or foreign chain prepared a Base payment")
				}
				transfer.Asset = tokens[1].Asset.ID
				_, err = tracked.Prepare(ctx, account, transfer)
				if (fault == "missing-ETH" || fault == "wrong-chain") != (err != nil) {
					t.Fatal("independent token spendability did not preserve exact native-gas and chain guards", err)
				}
				if f.calls["eth_sendRawTransaction"] != 0 {
					t.Fatal("a failed Base token plan broadcast a transaction")
				}
			})
		}
	}
}

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
			if err != nil || len(balances) != len(defaultStablecoins(network))+3 || balances[len(balances)-1].Asset != token.Asset.ID || balances[len(balances)-1].Atomic != "1234567" {
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
