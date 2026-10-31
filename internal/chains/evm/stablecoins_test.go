package evm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

type stablecoinRuntime struct {
	Network, Endpoint, Contract, ChainID, Block, BlockHash, CodeHash, Code string
	Metadata                                                               map[string]any
	Proxies                                                                []struct{ Slot, Value, Implementation, CodeHash, Code string }
}

func stablecoinRuntimes(t *testing.T) []stablecoinRuntime {
	t.Helper()
	raw, err := os.ReadFile("testdata/stablecoin-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Tokens []stablecoinRuntime }
	if json.Unmarshal(raw, &fixture) != nil || len(fixture.Tokens) != 6 {
		t.Fatal("invalid reviewed stablecoin runtime fixture")
	}
	return fixture.Tokens
}
func stablecoinFixtureRPC(records []stablecoinRuntime, network, method string, args []json.RawMessage) (any, bool) {
	if len(args) == 0 {
		return nil, false
	}
	var address string
	var call map[string]string
	if method == "eth_call" {
		_ = json.Unmarshal(args[0], &call)
		address = call["to"]
	} else {
		_ = json.Unmarshal(args[0], &address)
	}
	for _, record := range records {
		if record.Network != network {
			continue
		}
		if strings.EqualFold(address, record.Contract) {
			if method == "eth_getCode" {
				return record.Code, true
			}
			if method == "eth_getStorageAt" && len(args) > 1 {
				var slot string
				_ = json.Unmarshal(args[1], &slot)
				for _, proxy := range record.Proxies {
					if slot == proxy.Slot {
						return proxy.Value, true
					}
				}
			}
			if method == "eth_call" {
				for signature, value := range record.Metadata {
					if call["data"] == callData(signature) {
						if encoded, ok := value.(string); ok {
							return encoded, true
						}
					}
				}
			}
		}
		for _, proxy := range record.Proxies {
			if method == "eth_getCode" && strings.EqualFold(address, proxy.Implementation) {
				return proxy.Code, true
			}
		}
	}
	return nil, false
}
func TestDefaultStablecoinIssuerIdentitiesAndProxyRuntimeFixtures(t *testing.T) {
	records := stablecoinRuntimes(t)
	want := map[string]int{"ethereum-mainnet": 3, "arbitrum-mainnet": 4, "base-mainnet": 4, "hyperevm-mainnet": 2}
	a := New()
	for network, count := range want {
		n, _ := a.network(network)
		if len(n.Assets) != count {
			t.Fatal("default asset coverage changed", network, len(n.Assets))
		}
		seen := map[string]bool{}
		for _, asset := range n.Assets {
			if seen[asset.ID] {
				t.Fatal("duplicate default identity")
			}
			seen[asset.ID] = true
		}
	}
	for _, record := range records {
		tokenID := record.Network + ":erc20:" + record.Contract
		policy, ok := stablecoinPolicies[tokenID]
		code, err := hex.DecodeString(strings.TrimPrefix(record.Code, "0x"))
		if err != nil || !ok || crypto.Keccak256Hash(code).Hex() != policy.codeHash || record.CodeHash != policy.codeHash {
			t.Fatal("runtime pin diverged from recorded public bytes", tokenID)
		}
		n, _ := a.network(record.Network)
		asset, err := asset(n, tokenID)
		if err != nil || asset.Decimals != 6 || record.ChainID != n.ChainID {
			t.Fatal("canonical token network/precision mismatch", tokenID)
		}
		if asset.Contract == USDTContract || asset.Contract == "0xA7fEFfF82c29DBD4337ef72CeF8c3FeF9Db5d248" {
			t.Fatal("foreign native/bridged identity substituted")
		}
		for _, proxy := range record.Proxies {
			code, err := hex.DecodeString(strings.TrimPrefix(proxy.Code, "0x"))
			if err != nil || crypto.Keccak256Hash(code).Hex() != policy.implementationHash || proxy.Slot != policy.implementationSlot || proxy.Implementation != policy.implementation {
				t.Fatal("implementation pin differs from read-only observation")
			}
		}
	}
	if policy := stablecoinPolicies[BaseUSDTAsset]; !policy.bridged || policy.circle || policy.implementation != "" {
		t.Fatal("Base bridged USDT misidentified as issuer-native/USDT0")
	}
	if policy := stablecoinPolicies[HyperEVMUSDCAsset]; !policy.circle {
		t.Fatal("HyperEVM USDC not pinned to Circle native token")
	}
}
func TestDefaultStablecoinBalancesTransfersSignaturesAndReceiveAddresses(t *testing.T) {
	for _, record := range stablecoinRuntimes(t) {
		t.Run(record.Network+"/"+record.Contract, func(t *testing.T) {
			f := fakeNode(t, record.Network)
			ctx := context.Background()
			account, err := f.a.Derive(syntheticSeed(), 0, record.Network)
			if err != nil {
				t.Fatal(err)
			}
			tokenID := record.Network + ":erc20:" + record.Contract
			balances, err := f.a.Balance(ctx, account)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, balance := range balances {
				if balance.Asset == tokenID {
					found = balance.State == "provider-observed" && balance.Atomic == "1000000000000000000000000"
				}
			}
			if !found {
				t.Fatal("default token balance missing/unknown")
			}
			plan, err := f.a.Prepare(ctx, account, model.Transfer{AccountID: account.ID, Asset: tokenID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "1234567"})
			if err != nil {
				t.Fatal(err)
			}
			signed, err := f.a.Sign(syntheticSeed(), plan)
			if err != nil {
				t.Fatal(err)
			}
			var tx types.Transaction
			if tx.UnmarshalBinary(signed.Bytes) != nil || tx.ChainId().String() != record.ChainID || tx.To() == nil || tx.To().Hex() != record.Contract || tx.Value().Sign() != 0 || len(tx.Data()) != 68 || hex.EncodeToString(tx.Data()[:4]) != "a9059cbb" || new(big.Int).SetBytes(tx.Data()[36:]).String() != "1234567" || plan.FeeAsset != record.Network+":native" {
				t.Fatal("default transfer changed chain/contract/amount/native gas")
			}
			owner, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), &tx)
			if err != nil || owner.Hex() != account.Address || signed.Identity != tx.Hash().Hex() {
				t.Fatal("default token signature/same owned receive address mismatch")
			}
			if f.calls["eth_sendRawTransaction"] != 0 {
				t.Fatal("default observations/signing broadcast without consent")
			}
		})
	}
}
func TestDefaultStablecoinHostileStateIsUnknownAndCannotPrepare(t *testing.T) {
	for _, record := range stablecoinRuntimes(t) {
		for _, fault := range []string{"proxy-code", "precision", "symbol", "false-transfer", "empty-transfer", "implementation", "implementation-code", "paused", "bridge"} {
			t.Run(record.Network+"/"+record.Contract+"/"+fault, func(t *testing.T) {
				policy := stablecoinPolicies[record.Network+":erc20:"+record.Contract]
				if (fault == "implementation" || fault == "implementation-code") && policy.implementation == "" {
					return
				}
				if fault == "paused" && !policy.circle {
					return
				}
				if fault == "bridge" && !policy.bridged {
					return
				}
				f := fakeNode(t, record.Network)
				account, _ := f.a.Derive(syntheticSeed(), 0, record.Network)
				f.intercept = func(method string, args []json.RawMessage) (any, bool) {
					var address string
					var call map[string]string
					if method == "eth_call" {
						_ = json.Unmarshal(args[0], &call)
						address = call["to"]
					} else if len(args) > 0 {
						_ = json.Unmarshal(args[0], &address)
					}
					if method == "eth_getStorageAt" && address == record.Contract && fault == "implementation" {
						return "0x" + strings.Repeat("0", 64), true
					}
					if method == "eth_getCode" && (address == record.Contract && fault == "proxy-code" || strings.EqualFold(address, policy.implementation) && fault == "implementation-code") {
						return "0x6000", true
					}
					if method == "eth_call" && address == record.Contract {
						switch {
						case fault == "precision" && call["data"] == callData("decimals()"):
							return "0x" + hex.EncodeToString(word(big.NewInt(18))), true
						case fault == "symbol" && call["data"] == callData("symbol()"):
							return tokenSymbolABI("OTHER"), true
						case fault == "paused" && call["data"] == callData("paused()"):
							return "0x" + hex.EncodeToString(word(big.NewInt(1))), true
						case fault == "bridge" && call["data"] == callData("l1Token()"):
							return "0x" + strings.Repeat("0", 64), true
						case strings.HasPrefix(call["data"], "0xa9059cbb") && fault == "false-transfer":
							return "0x" + strings.Repeat("0", 64), true
						case strings.HasPrefix(call["data"], "0xa9059cbb") && fault == "empty-transfer":
							return "0x", true
						}
					}
					return nil, false
				}
				tokenID := record.Network + ":erc20:" + record.Contract
				balances, err := f.a.Balance(context.Background(), account)
				if err != nil {
					t.Fatal(err)
				}
				for _, balance := range balances {
					if balance.Asset == tokenID && fault != "false-transfer" && fault != "empty-transfer" && (balance.State != "unavailable" || balance.Atomic != "") {
						t.Fatal("hostile issuer/proxy became verified zero/value")
					}
					if balance.Asset == record.Network+":native" && balance.State != "provider-observed" {
						t.Fatal("one token failure hid native balance")
					}
				}
				if _, err = f.a.Prepare(context.Background(), account, model.Transfer{AccountID: account.ID, Asset: tokenID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "1"}); err == nil {
					t.Fatal("hostile token state prepared a spend")
				}
				if f.calls["eth_sendRawTransaction"] != 0 {
					t.Fatal("failed state broadcast")
				}
			})
		}
	}
}
func TestPromotedDefaultPoliciesArePreservedInertAndDeduplicated(t *testing.T) {
	for _, record := range stablecoinRuntimes(t) {
		original := model.ERC20Token{Network: record.Network, Asset: model.Asset{ID: record.Network + ":erc20:" + record.Contract, Symbol: "OLD", Decimals: 18, Contract: record.Contract}, CodeHash: crypto.Keccak256Hash([]byte{0x60, 0x01}).Hex()}
		if !IsPromotedDefault(original) || ValidateStoredTokenPolicy(original) != nil || ValidateTokenPolicy(original) == nil {
			t.Fatal("promotion lost historical bounded metadata or enabled duplicate import")
		}
		rebuilt, err := New().WithTokens([]model.ERC20Token{original})
		if err != nil {
			t.Fatal(err)
		}
		n, _ := rebuilt.network(record.Network)
		count := 0
		for _, asset := range n.Assets {
			if asset.ID == original.Asset.ID {
				count++
				if asset.Decimals != 6 || asset.Symbol == "OLD" {
					t.Fatal("historical metadata overrode canonical default")
				}
			}
		}
		if count != 1 || len(rebuilt.tokens) != 0 {
			t.Fatal("promoted policy duplicated or overrode runtime pin")
		}
		bad := original
		bad.Asset.Contract = common.HexToAddress("0x1111111111111111111111111111111111111111").Hex()
		if IsPromotedDefault(bad) || ValidateStoredTokenPolicy(bad) == nil {
			t.Fatal("promotion exception escaped exact contract")
		}
	}
}

func TestDefaultStablecoinIndependentEthersBytesAtThreeAccountIndices(t *testing.T) {
	raw, err := os.ReadFile("testdata/stablecoin-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Oracle, Profile, ProducerSHA256, LockSHA256 string
		Fixtures                                    []struct {
			Network, ChainID, Contract, Address, Recipient, Amount, Signed string
			Index                                                          uint32
			Decimals                                                       uint8
		}
	}
	if json.Unmarshal(raw, &vectors) != nil || vectors.Oracle != "ethers 6.17.0" || vectors.Profile != Profile || len(vectors.Fixtures) != 18 {
		t.Fatal("independent oracle scope changed")
	}
	for name, expected := range map[string]string{"../../../tests/oracles/evm/stablecoin-generate.mjs": vectors.ProducerSHA256, "../../../tests/oracles/evm/package-lock.json": vectors.LockSHA256} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != expected {
			t.Fatal("independent stablecoin generator/lock provenance changed", name)
		}
	}
	seen := map[string]bool{}
	for _, vector := range vectors.Fixtures {
		key := fmt.Sprintf("%s/%s/%d", vector.Network, vector.Contract, vector.Index)
		if seen[key] || (vector.Index != 0 && vector.Index != 1 && vector.Index != 127) {
			t.Fatal("independent token/index coverage duplicated or changed")
		}
		seen[key] = true
		if _, ok := stablecoinPolicies[vector.Network+":erc20:"+vector.Contract]; !ok {
			t.Fatal("independent vector contract not a reviewed default")
		}
		n, _ := network(vector.Network)
		if vector.ChainID != n.ChainID {
			t.Fatal("independent vector signing domain mismatch")
		}

		a := New()
		plan := fixturePlan(t, a, vector.Network)
		account, err := a.Derive(syntheticSeed(), vector.Index, vector.Network)
		if err != nil || account.Address != vector.Address {
			t.Fatal("independent restored owner mismatch")
		}
		plan.Account = account
		plan.Asset = vector.Network + ":erc20:" + vector.Contract
		plan.Recipient = vector.Recipient
		plan.Amount = vector.Amount
		plan.MaxFee = "200000000100000"
		var body payload
		_ = json.Unmarshal(plan.Payload, &body)
		body.To = vector.Contract
		body.Value = "0"
		body.Data = callData("transfer(address,uint256)", common.BytesToHash(common.HexToAddress(vector.Recipient).Bytes()).Bytes(), word(big.NewInt(1234567)))
		body.Gas = "100000"
		if vector.Network == "base-mainnet" {
			body.DataFeeReserve = "1"
			body.OperatorFeeReserve = "1"
			plan.MaxFee = "200000000100002"
		}
		plan.Payload, _ = json.Marshal(body)
		plan.ID = planID(plan)
		signed, err := a.Sign(syntheticSeed(), plan)
		if err != nil || "0x"+hex.EncodeToString(signed.Bytes) != vector.Signed || vector.Decimals != 6 {
			t.Fatal("Go token bytes differ from independent chain-bound ethers vector", vector.Network, vector.Index, err)
		}
	}
}

func TestDefaultStablecoinRecentHistoryUsesExactTrackedContractAndPrecision(t *testing.T) {
	for _, record := range stablecoinRuntimes(t) {
		t.Run(record.Network+"/"+record.Contract, func(t *testing.T) {
			h := newHistory(t, record.Network)
			n, _ := h.f.a.network(record.Network)
			token, err := asset(n, record.Network+":erc20:"+record.Contract)
			if err != nil {
				t.Fatal(err)
			}
			owner := common.BytesToHash(common.HexToAddress(h.ac.Address).Bytes()).Hex()
			peer := "0x1111111111111111111111111111111111111111"
			id := "0x" + strings.Repeat("b", 64)
			logs := []any{map[string]any{"address": token.Contract, "topics": []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), owner, common.BytesToHash(common.HexToAddress(peer).Bytes()).Hex()}, "data": "0x" + hex.EncodeToString(word(big.NewInt(1234567))), "transactionHash": id, "blockHash": h.hash, "blockNumber": "0x1", "logIndex": "0x2", "removed": false}}
			original := h.f.intercept
			h.f.intercept = func(method string, args []json.RawMessage) (any, bool) {
				if method == "eth_getLogs" {
					var filter map[string]any
					_ = json.Unmarshal(args[0], &filter)
					if filter["address"] == token.Contract {
						return logs, true
					}
					return []any{}, true
				}
				return original(method, args)
			}
			entries, err := h.run()
			if err != nil || len(entries) != 1 {
				t.Fatal("default token history missing", err)
			}
			found := false
			for _, effect := range entries[0].Transfers {
				if effect.Asset.ID == token.ID {
					found = effect.Asset == token && effect.Atomic == "1234567" && effect.Source == h.ac.Address && effect.Destination == peer
				}
			}
			if !found {
				t.Fatal("history collapsed or mislabelled default token effect")
			}
		})
	}
}

func TestDefaultStablecoinLiveProductionBalancesReadOnly(t *testing.T) {
	if os.Getenv("WALLET_STABLECOIN_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit read-only public-provider acceptance; no user storage, signing or mainnet funds")
	}
	a := New()
	for _, network := range []string{"ethereum-mainnet", "arbitrum-mainnet", "base-mainnet", "hyperevm-mainnet"} {
		t.Run(network, func(t *testing.T) {
			account, err := a.Derive(syntheticSeed(), 127, network)
			if err != nil {
				t.Fatal(err)
			}
			balances, err := a.Balance(context.Background(), account)
			if err != nil {
				t.Fatal("read-only checked production balance unavailable", err)
			}
			byID := map[string]model.Balance{}
			for _, balance := range balances {
				byID[balance.Asset] = balance
			}
			for _, asset := range defaultStablecoins(network) {
				balance := byID[asset.ID]
				if balance.State != "provider-observed" || balance.Atomic == "" || balance.At == "" {
					t.Fatal("canonical read-only token not verified", asset.ID, balance.State)
				}
				t.Log("PASS checked read-only", asset.ID, "precision", asset.Decimals)
			}
		})
	}
}
