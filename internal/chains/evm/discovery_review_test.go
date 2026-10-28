package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Controlled RPC fixtures verify the read-only boundary and revalidation;
// independent real EVM execution remains a separate acceptance gate.
func TestBaseDiscoveryUsesOnlyOneBlockReadCallsAndRechecksCandidateRuntimeAndPrecision(t *testing.T) {
	for _, mutation := range []string{"none", "runtime", "precision"} {
		t.Run(mutation, func(t *testing.T) {
			contract := importedTokenFixture().Asset.Contract
			f, account, requests := discoveryNode(t, discoveryBody(contract), 200, false)
			original := f.intercept
			codeReads, precisionReads, balanceReads := 0, 0, 0
			f.intercept = func(method string, arguments []json.RawMessage) (any, bool) {
				switch method {
				case "eth_chainId", "eth_getBlockByNumber":
				case "eth_getCode", "eth_call":
					if len(arguments) != 2 || string(arguments[1]) != `"0x10"` {
						t.Error("candidate observations mixed block identities", method)
					}
					if method == "eth_getCode" {
						codeReads++
						if codeReads == 2 && mutation == "runtime" {
							return "0x6002", true
						}
					} else {
						var call map[string]string
						_ = json.Unmarshal(arguments[0], &call)
						if call["to"] != contract || len(call) != 2 {
							t.Error("discovery called a foreign contract or supplied transaction authority")
						}
						switch call["data"] {
						case callData("symbol()"):
						case callData("decimals()"):
							precisionReads++
							if precisionReads == 2 && mutation == "precision" {
								return "0x" + common.Bytes2Hex(word(big.NewInt(7))), true
							}
						case callData("balanceOf(address)", common.BytesToHash(common.HexToAddress(account.Address).Bytes()).Bytes()):
							balanceReads++
						default:
							t.Error("discovery expanded its exact read-only contract method allowlist")
						}
					}
				default:
					t.Error("discovery attempted a mutating or unreviewed RPC", method)
				}
				return original(method, arguments)
			}
			result, err := f.a.DiscoverTokens(context.Background(), account)
			if err != nil || *requests != 1 || f.calls["eth_getBlockByNumber"] != 1 || codeReads != 2 {
				t.Fatal("candidate reads were not bounded to one chain-checked block", err, result)
			}
			if mutation == "none" {
				if len(result.Items) != 1 || result.Unavailable != 0 || precisionReads != 2 || balanceReads != 1 {
					t.Fatal("unchanged candidate runtime/precision did not produce a checked balance", result)
				}
			} else if len(result.Items) != 0 || result.Unavailable != 1 || balanceReads != 0 {
				t.Fatal("changed candidate runtime/precision became a balance or zero", result)
			}
			if len(f.a.tokens) != 0 || f.sent != "" {
				t.Fatal("candidate observation acquired spend policy or broadcast authority")
			}
		})
	}
}
