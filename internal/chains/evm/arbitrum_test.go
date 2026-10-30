package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

func TestArbitrumUsesFullNitroGasEstimateWithoutOPOracleOrSecondPostingCharge(t *testing.T) {
	for _, network := range []string{"arbitrum-mainnet", "arbitrum-sepolia"} {
		t.Run(network, func(t *testing.T) {
			f := fakeNode(t, network)
			// This controlled node quote includes the L1 posting gas. It tests
			// interpretation, not actual Nitro consensus or posting economics.
			f.override["eth_estimateGas"] = "0x1d4c0" // 120000 gas including posting.
			oracleCalls := 0
			f.intercept = func(method string, args []json.RawMessage) (any, bool) {
				if method == "eth_call" {
					var msg map[string]string
					_ = json.Unmarshal(args[0], &msg)
					if strings.EqualFold(msg["to"], "0x420000000000000000000000000000000000000F") {
						oracleCalls++
						return nil, true
					}
				}
				return nil, false
			}
			p, err := f.prepare(t, network, false)
			if err != nil {
				t.Fatal(err)
			}
			var x payload
			if StrictJSON(p.Payload, &x) != nil || x.Gas != "144000" || x.DataFeeReserve != "0" || x.OperatorFeeReserve != "0" || oracleCalls != 0 {
				t.Fatal("Arbitrum posting fee omitted or charged again through OP oracle", x, oracleCalls)
			}
			feeCap, _ := decimal(x.FeeCap)
			if p.MaxFee != new(big.Int).Mul(big.NewInt(144000), feeCap).String() {
				t.Fatal("Arbitrum total signed gas envelope did not bound the complete estimate")
			}
			signed, err := f.a.Sign(syntheticSeed(), p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.a.Submit(context.Background(), p, signed); err != nil || oracleCalls != 0 {
				t.Fatal("Arbitrum submit incorrectly required an OP fee oracle", err)
			}
			for _, change := range []func(*payload){func(x *payload) { x.ChainID = "8453" }, func(x *payload) { x.DataFeeReserve = "1" }, func(x *payload) { x.OperatorFeeReserve = "1" }} {
				bad := p
				altered := x
				change(&altered)
				if altered.DataFeeReserve == "1" || altered.OperatorFeeReserve == "1" {
					fee, _ := decimal(p.MaxFee)
					bad.MaxFee = fee.Add(fee, big.NewInt(1)).String()
				}
				bad.Payload, _ = json.Marshal(altered)
				bad.ID = planID(bad)
				if f.a.ValidatePlan(bad) == nil {
					t.Fatal("wrong domain or duplicated fee accepted")
				}
			}
		})
	}
}

func TestCanonicalARBRejectsProxyUpgradeOrChangedImplementation(t *testing.T) {
	for _, fault := range []string{"healthy", "implementation-slot", "implementation-code", "missing-implementation", "precision"} {
		t.Run(fault, func(t *testing.T) {
			f := fakeNode(t, "arbitrum-mainnet")
			f.intercept = func(method string, args []json.RawMessage) (any, bool) {
				if method == "eth_getStorageAt" && fault == "implementation-slot" {
					return "0x" + strings.Repeat("0", 64), true
				}
				if method == "eth_getCode" {
					var address string
					_ = json.Unmarshal(args[0], &address)
					if address == arbImplementation {
						if fault == "implementation-code" {
							return "0x6000", true
						}
						if fault == "missing-implementation" {
							return "0x", true
						}
					}
				}
				if method == "eth_call" && fault == "precision" {
					var call map[string]string
					_ = json.Unmarshal(args[0], &call)
					if call["data"] == callData("decimals()") {
						return "0x" + hex.EncodeToString(word(big.NewInt(6))), true
					}
				}
				return nil, false
			}
			p, err := f.prepare(t, "arbitrum-mainnet", true)
			if fault == "healthy" {
				if err != nil || p.Asset != ARBAsset || p.FeeAsset != "arbitrum-mainnet:native" {
					t.Fatal("canonical ARB approval unavailable or gas is not ETH", err)
				}
			} else if err == nil {
				t.Fatal("changed canonical ARB runtime, implementation or precision accepted")
			}
		})
	}
}

func TestIndependentArbitrumTokenTransactionFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/oracles/evm/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Fixtures []struct {
			Index                                 uint32 `json:"index"`
			Network, Contract, Data, Signed, Hash string
		} `json:"arbitrumTokenFixtures"`
	}
	if json.Unmarshal(raw, &data) != nil || len(data.Fixtures) != 6 {
		t.Fatal("independent Arbitrum token fixtures unavailable")
	}
	for _, fixture := range data.Fixtures {
		adapter := New()
		assetID := ARBAsset
		if fixture.Network == "arbitrum-sepolia" {
			token := importedTokenFixture()
			token.Network = fixture.Network
			token.Asset.ID = fixture.Network + ":erc20:" + token.Asset.Contract
			adapter, err = adapter.WithTokens([]model.ERC20Token{token})
			assetID = token.Asset.ID
			if err != nil {
				t.Fatal(err)
			}
		}
		plan := fixturePlan(t, adapter, fixture.Network)
		plan.Account, err = adapter.Derive(syntheticSeed(), fixture.Index, fixture.Network)
		if err != nil {
			t.Fatal(err)
		}
		plan.Asset = assetID
		var x payload
		_ = json.Unmarshal(plan.Payload, &x)
		amount, _ := decimal(plan.Amount)
		x.To, x.Value, x.Data, x.Gas = fixture.Contract, "0", transferData(plan.Recipient, amount), "60000"
		plan.Payload, _ = json.Marshal(x)
		plan.MaxFee = "120000000060000"
		plan.ID = planID(plan)
		signed, err := adapter.Sign(syntheticSeed(), plan)
		if err != nil || x.Data != fixture.Data || "0x"+hex.EncodeToString(signed.Bytes) != fixture.Signed || signed.Identity != fixture.Hash {
			t.Fatal("independent Arbitrum ERC20 bytes/signature/domain differed", fixture.Network, fixture.Index, err)
		}
	}
}

// Opt-in, low-volume, strictly read-only live capability checks. No vault,
// transaction signing/submission, user address or API credentials are involved.
func TestDefaultEVMProviderReadOnlyAcceptance(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_PUBLIC_PROVIDERS") != "1" {
		t.Skip("NOT_RUN: explicit read-only public provider check")
	}
	for _, id := range []string{"base-mainnet", "base-sepolia", "arbitrum-mainnet", "arbitrum-sepolia"} {
		t.Run(id, func(t *testing.T) {
			adapter := New()
			network, _ := adapter.network(id)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			head, err := adapter.current(ctx, network)
			if err != nil {
				t.Fatal("current checked default provider unavailable", err)
			}
			var estimate string
			err = adapter.rpc(ctx, network, "eth_estimateGas", []any{map[string]string{"to": "0x1111111111111111111111111111111111111111", "value": "0x0", "data": "0x"}}, &estimate)
			gas, parseErr := hexInteger(estimate)
			if err != nil || parseErr != nil || !gas.IsUint64() || gas.Uint64() < 21000 {
				t.Fatal("default provider gas estimation unavailable", err, parseErr)
			}
			if strings.HasPrefix(id, "base-") {
				l1, operator, err := adapter.rollupFees(ctx, network, 200, gas.Uint64(), head.Number)
				if err != nil || l1.Sign() <= 0 || operator.Sign() < 0 {
					t.Fatal("default provider Base data/operator fee methods unavailable", err)
				}
				t.Logf("Base fee oracle: L1 quote %s wei, operator quote %s wei", l1, operator)
			}
			if id == "arbitrum-mainnet" {
				if err := adapter.tokenIdentity(ctx, network, network.Assets[1], head.Number); err != nil {
					t.Fatal("canonical ARB identity no longer matches reviewed public observation", err)
				}
			}
			if strings.HasPrefix(id, "arbitrum-") {
				var raw string
				data := callData("gasEstimateComponents(address,bool,bytes)",
					append(make([]byte, 12), strings.Repeat("\x11", 20)...), word(big.NewInt(0)), word(big.NewInt(96)), word(big.NewInt(0)))
				if err := adapter.rpc(ctx, network, "eth_call", []any{map[string]string{"to": "0x00000000000000000000000000000000000000c8", "data": data}, head.Number}, &raw); err != nil {
					t.Fatal("documented Arbitrum NodeInterface gas components unavailable", err)
				}
				decoded, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
				if err != nil || !strings.HasPrefix(raw, "0x") || len(decoded) != 128 {
					t.Fatal("malformed Arbitrum NodeInterface gas components")
				}
				total, posting, price := new(big.Int).SetBytes(decoded[:32]), new(big.Int).SetBytes(decoded[32:64]), new(big.Int).SetBytes(decoded[64:96])
				if !total.IsUint64() || !posting.IsUint64() || total.Uint64() < 21000 || posting.Cmp(total) > 0 || price.Sign() <= 0 {
					t.Fatal("invalid Arbitrum NodeInterface gas components")
				}
				t.Logf("NodeInterface gas includes posting: total %s, posting component %s, L2 price %s wei", total, posting, price)
			}
			t.Logf("PASS read-only chain %s checked head %s; gas estimate %s; no consensus or funded transfer claim", network.ChainID, head.Number, gas.String())
		})
	}
}
