package vault

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func promotedHistoricalPolicies() []model.ERC20Token {
	rows := []struct{ network, contract string }{
		{"ethereum-mainnet", evm.USDCContract}, {"arbitrum-mainnet", evm.ArbitrumUSDTContract}, {"arbitrum-mainnet", evm.ArbitrumUSDCContract},
		{"base-mainnet", evm.BaseUSDTContract}, {"base-mainnet", evm.BaseUSDCContract}, {"hyperevm-mainnet", evm.HyperEVMUSDCContract},
	}
	out := []model.ERC20Token{}
	for i, row := range rows {
		out = append(out, model.ERC20Token{Network: row.network, Asset: model.Asset{ID: row.network + ":erc20:" + row.contract, Contract: row.contract, Symbol: "OLD_METADATA", Decimals: uint8(9 + i)}, CodeHash: "0x" + strings.Repeat("a", 64)})
	}
	return out
}
func TestStablecoinDefaultsKeepHistoricalEncryptedWatchPoliciesAndUnknownJournal(t *testing.T) {
	data := synthetic()
	defer data.Destroy()
	data.Environment = "mainnet"
	data.Accounts = nil
	data.ERC20Tokens = promotedHistoricalPolicies()
	seed, err := derivation.Seed(data.Entropy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	for _, network := range []string{"ethereum-mainnet", "arbitrum-mainnet", "base-mainnet", "hyperevm-mainnet"} {
		account, err := evm.New().Derive(seed, 0, network)
		if err != nil {
			t.Fatal(err)
		}
		data.Accounts = append(data.Accounts, account)
	}
	// Vault format preserves an opaque original signed artifact. This deliberately
	// makes no claim that these public fixture bytes are validator-accepted EVM.
	plan := model.Plan{ID: "public-original-promoted-token", Account: data.Accounts[0], Asset: data.ERC20Tokens[0].Asset.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789", FeeAsset: data.Accounts[0].Network + ":native", MaxFee: "1", Nonce: "7", Expires: 1, Payload: json.RawMessage(`{"historicalPublicFixture":"unchanged"}`)}
	data.Operations = []journal.Operation{{Plan: plan, Digest: journal.Digest(plan), State: "UNKNOWN", Signed: &model.Signed{Identity: "original-public-test-only-transaction", Bytes: []byte{1, 2, 3, 4}}}}
	for _, policy := range data.ERC20Tokens {
		if evm.ValidateStoredTokenPolicy(policy) != nil || evm.ValidateTokenPolicy(policy) == nil {
			t.Fatal("promoted metadata gained new-import authority", policy)
		}
	}
	raw, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	encrypted, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range data.ERC20Tokens {
		if bytes.Contains(encrypted, []byte(policy.Asset.Contract)) || bytes.Contains(encrypted, []byte(policy.CodeHash)) {
			t.Fatal("historical token policy leaked outside encryption")
		}
	}
	files, err := NewFiles(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	if err = files.Write("vault.lwv", encrypted, false); err != nil {
		t.Fatal(err)
	}
	stored, err := files.Read("vault.lwv")
	if err != nil || !bytes.Equal(stored, encrypted) {
		t.Fatal("encrypted bytes changed", err)
	}
	opened, err := Open(stored, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if !reflect.DeepEqual(restored.ERC20Tokens, data.ERC20Tokens) || !reflect.DeepEqual(restored.Operations, data.Operations) || VerifyRecovery(data, restored) != nil {
		t.Fatal("promotion changed original metadata, signed operation or recovery identity")
	}
	adapter, err := evm.New().WithTokens(restored.ERC20Tokens)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range restored.ERC20Tokens {
		count := 0
		for _, network := range adapter.Networks() {
			if network.ID == policy.Network {
				for _, asset := range network.Assets {
					if asset.ID == policy.Asset.ID {
						count++
						if asset.Decimals != 6 || asset.Symbol == policy.Asset.Symbol {
							t.Fatal("inert historical metadata replaced current registry authority", asset)
						}
					}
				}
			}
		}
		if count != 1 {
			t.Fatal("default duplicated or lost", policy.Asset.ID, count)
		}
	}
	data.Environment = "testnet"
	if _, err = data.Encode(); err == nil {
		t.Fatal("historical mainnet approval crossed encrypted testnet profile")
	}
}
