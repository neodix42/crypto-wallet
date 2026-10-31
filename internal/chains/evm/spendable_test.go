package evm

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func TestSpendableNativeUsesLowerConfirmedAndPendingOnEveryEVMNetwork(t *testing.T) {
	for _, network := range New().Networks() {
		for _, pending := range []int64{0, 50, 100, 200} {
			t.Run(network.ID+"/"+big.NewInt(pending).String(), func(t *testing.T) {
				fixture := fakeNode(t, network.ID)
				fixture.intercept = func(method string, args []json.RawMessage) (any, bool) {
					if method != "eth_getBalance" {
						return nil, false
					}
					var at string
					_ = json.Unmarshal(args[1], &at)
					value := int64(100)
					if at == "pending" {
						value = pending
					} else if at != "0x10" {
						t.Errorf("confirmed balance did not bind checked block: %s", at)
					}
					return quantity(big.NewInt(value)), true
				}
				account, err := fixture.a.Derive(syntheticSeed(), 0, network.ID)
				if err != nil {
					t.Fatal(err)
				}
				value, err := fixture.a.SpendableBalance(context.Background(), account, network.Assets[0].ID)
				want := pending
				if want > 100 {
					want = 100
				}
				if err != nil || value != big.NewInt(want).String() || fixture.calls["eth_getBalance"] != 2 || fixture.sent != "" {
					t.Fatal("pending incoming credit or debit changed spendable policy", value, err)
				}
			})
		}
	}
}

func TestSpendableTokenKeepsExactPendingIdentityAndNativeGasSeparate(t *testing.T) {
	for _, network := range New().Networks() {
		if network.Testnet {
			continue
		}
		for _, token := range network.Assets {
			if token.Contract == "" {
				continue
			}
			t.Run(network.ID+"/"+token.Symbol, func(t *testing.T) {
				fixture := fakeNode(t, network.ID)
				pendingGasEmpty := false
				fixture.intercept = func(method string, args []json.RawMessage) (any, bool) {
					if method == "eth_getBalance" {
						var at string
						_ = json.Unmarshal(args[1], &at)
						if pendingGasEmpty && at == "pending" {
							return "0x0", true
						}
					}
					if method == "eth_call" {
						var call map[string]string
						var at string
						_ = json.Unmarshal(args[0], &call)
						_ = json.Unmarshal(args[1], &at)
						if call["to"] == token.Contract && strings.HasPrefix(call["data"], callData("balanceOf(address)")[:10]) {
							value := int64(2000000)
							if at == "pending" {
								value = 1234567
							}
							return "0x" + strings.Repeat("0", 64-len(big.NewInt(value).Text(16))) + big.NewInt(value).Text(16), true
						}
					}
					return nil, false
				}
				account, err := fixture.a.Derive(syntheticSeed(), 0, network.ID)
				if err != nil {
					t.Fatal(err)
				}
				value, err := fixture.a.SpendableBalance(context.Background(), account, token.ID)
				if err != nil || value != "1234567" {
					t.Fatal("pending token debit was not excluded", value, err)
				}
				transfer := model.Transfer{AccountID: account.ID, Asset: token.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: value}
				plan, err := fixture.a.Prepare(context.Background(), account, transfer)
				if err != nil || plan.Amount != value || plan.FeeAsset != network.Assets[0].ID || plan.FeeAsset == token.ID {
					t.Fatal("native gas was subtracted from token units", plan, err)
				}
				pendingGasEmpty = true
				if plan, err := fixture.a.Prepare(context.Background(), account, transfer); !errors.Is(err, ErrInsufficientPendingFunds) || plan.ID != "" || fixture.sent != "" {
					t.Fatal("token units replaced missing pending native gas", plan, err)
				}
			})
		}
	}
}

func TestSpendablePendingFailuresNeverBecomeZeroOrAConfirmedFallback(t *testing.T) {
	for _, kind := range []string{"account", "asset", "wrong-chain", "confirmed-malformed", "pending-malformed", "pending-null", "provider-error", "token-pending-code", "token-pending-implementation", "token-pending-precision", "token-pending-balance"} {
		t.Run(kind, func(t *testing.T) {
			fixture := fakeNode(t, "ethereum-mainnet")
			account, _ := fixture.a.Derive(syntheticSeed(), 0, "ethereum-mainnet")
			assetID := "ethereum-mainnet:native"
			if strings.HasPrefix(kind, "token-") {
				assetID = USDCAsset
			}
			if kind == "account" {
				account.Profile = "foreign"
			}
			if kind == "asset" {
				assetID = "foreign:asset"
			}
			if kind == "wrong-chain" {
				fixture.override["eth_chainId"] = "0x2105"
			}
			if kind == "provider-error" {
				fixture.rpcError = "eth_getBalance"
			}
			fixture.intercept = func(method string, args []json.RawMessage) (any, bool) {
				var at string
				if len(args) > 1 {
					_ = json.Unmarshal(args[len(args)-1], &at)
				}
				if method == "eth_getBalance" {
					if kind == "confirmed-malformed" && at == "0x10" || kind == "pending-malformed" && at == "pending" {
						return "0x00", true
					}
					if kind == "pending-null" && at == "pending" {
						return nil, true
					}
				}
				if at != "pending" {
					return nil, false
				}
				if kind == "token-pending-code" && method == "eth_getCode" {
					return "0x6000", true
				}
				if kind == "token-pending-implementation" && method == "eth_getStorageAt" {
					return "0x" + strings.Repeat("0", 64), true
				}
				if method == "eth_call" {
					var call map[string]string
					_ = json.Unmarshal(args[0], &call)
					if kind == "token-pending-precision" && call["data"] == callData("decimals()") {
						return "0x" + strings.Repeat("0", 63) + "9", true
					}
					if kind == "token-pending-balance" && strings.HasPrefix(call["data"], callData("balanceOf(address)")[:10]) {
						return "0x1", true
					}
				}
				return nil, false
			}
			if value, err := fixture.a.SpendableBalance(context.Background(), account, assetID); err == nil || value != "" || fixture.sent != "" {
				t.Fatal("unverified pending state became spendable funds", value, err)
			}
		})
	}
}
