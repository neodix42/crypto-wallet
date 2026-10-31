package signer

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func historicalPromotedPolicy() model.ERC20Token {
	return model.ERC20Token{Network: "ethereum-mainnet", Asset: model.Asset{ID: evm.USDCAsset, Contract: evm.USDCContract, Symbol: "OLD_METADATA", Decimals: 18}, CodeHash: "0x" + strings.Repeat("a", 64)}
}
func currentUSDCFixturePlan(account model.Account, nonce string) model.Plan {
	plan := customTokenPlan(account, historicalPromotedPolicy())
	var payload map[string]any
	_ = json.Unmarshal(plan.Payload, &payload)
	payload["chainId"] = "1"
	plan.Payload, _ = json.Marshal(payload)
	plan.Nonce = nonce
	plan.ID = ""
	plan.ID = "evm:" + journal.Digest(plan)
	return plan
}
func TestStablecoinDefaultsUnlockPreservesCiphertextOldPolicyAndActualUnknownSignedBytes(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	account := findAccount(t, e, "ethereum-mainnet")
	oldPolicy := historicalPromotedPolicy()
	if evm.ValidateStoredTokenPolicy(oldPolicy) != nil || evm.ValidateTokenPolicy(oldPolicy) == nil {
		t.Fatal("stored promotion policy became a new import")
	}
	oldPlan := currentUSDCFixturePlan(account, "7")
	adapter, err := e.adapter(account.Network)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := adapter.Sign(seed, oldPlan)
	clear(seed)
	if err != nil {
		t.Fatal("public synthetic historical EVM signature fixture invalid", err)
	}
	original := journal.Operation{Plan: oldPlan, Digest: journal.Digest(oldPlan), State: "UNKNOWN", Signed: &signed}
	e.data.ERC20Tokens = []model.ERC20Token{oldPolicy}
	e.data.Operations = []journal.Operation{original}
	if err = e.persist(); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	accountsBefore := e.Snapshot().Accounts
	e.Lock()
	fresh := New(e.Files, ui, chains.All())
	fresh.parameters = e.parameters
	t.Cleanup(fresh.Lock)
	ui.inputs = append(ui.inputs, input("public-wrong-password"))
	if fresh.Unlock() == nil || !fresh.Snapshot().Locked {
		t.Fatal("promotion bypassed password authentication")
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(after, ciphertext) {
		t.Fatal("failed authentication rewrote historical ciphertext", err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = fresh.Unlock(); err != nil {
		t.Fatal("historical approved metadata prevented unlock", err)
	}
	after, err = e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(after, ciphertext) {
		t.Fatal("unchanged promotion unlock rewrote vault", err)
	}
	if !reflect.DeepEqual(fresh.TokenPolicies(), []model.ERC20Token{oldPolicy}) || !reflect.DeepEqual(fresh.Snapshot().Accounts, accountsBefore) || !reflect.DeepEqual(fresh.data.Operations, []journal.Operation{original}) {
		t.Fatal("promotion changed keys, legacy metadata, signed bytes, digest or UNKNOWN state")
	}
	currentAdapter, err := fresh.adapter(account.Network)
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, network := range currentAdapter.Networks() {
		if network.ID == account.Network {
			for _, asset := range network.Assets {
				if asset.ID == oldPolicy.Asset.ID {
					matches++
					if asset.Decimals != 6 || asset.Symbol != "USDC" {
						t.Fatal("legacy precision/ticker overrode canonical adapter", asset)
					}
				}
			}
		}
	}
	if matches != 1 {
		t.Fatal("promoted watch asset duplicated current default", matches)
	}
	summary := fresh.tokenApproval(oldPlan)
	for _, wanted := range []string{evm.USDCContract, "Token decimals: 6", "Whole-token amount: 123.456789 USDC", "native gas is separate"} {
		if !strings.Contains(summary, wanted) {
			t.Fatal("default approval missing current exact denomination", summary)
		}
	}
	if strings.Contains(summary, "OLD_METADATA") || strings.Contains(summary, oldPolicy.CodeHash) || strings.Contains(summary, "Token decimals: 18") {
		t.Fatal("old metadata granted new approval", summary)
	}
	if fresh.ImportToken(account.ID, oldPolicy) == nil {
		t.Fatal("historical default could be imported again")
	}
	if _, err = fresh.Sign(oldPlan.ID); err == nil || !reflect.DeepEqual(fresh.data.Operations[0], original) {
		t.Fatal("UNKNOWN signed operation reauthorized or changed")
	}
	if fresh.Prepare(currentUSDCFixturePlan(account, "8")) == nil {
		t.Fatal("promotion released UNKNOWN account reservation")
	}
	if err = fresh.AddAccount(1); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range fresh.Snapshot().Accounts {
		if candidate.Network == "ethereum-mainnet" && candidate.Index == 1 {
			account = candidate
		}
	}
	if account.Index != 1 {
		t.Fatal("independent synthetic account absent")
	}
	// An ordinary new transfer still needs its separate exact confirmation and
	// current app password; the stored historical import grants neither.
	for i, mode := range []string{"cancel", "wrong-password", "approved"} {
		plan := currentUSDCFixturePlan(account, strconv.Itoa(8+i))
		if err = fresh.Prepare(plan); err != nil {
			t.Fatal(mode, err)
		}
		ui.approve = mode != "cancel"
		if mode != "cancel" {
			password := syntheticPassword
			if mode == "wrong-password" {
				password = "public-wrong-password"
			}
			ui.inputs = append(ui.inputs, input(password))
		}
		result, err := fresh.Sign(plan.ID)
		if (err == nil) != (mode == "approved") {
			t.Fatal("default token authority bypassed confirmation or password", mode, err)
		}
		if mode == "approved" && (result.Identity == "" || len(result.Bytes) == 0) {
			t.Fatal("approved current default did not produce actual signed EVM bytes")
		}
		if !reflect.DeepEqual(fresh.data.Operations[0], original) {
			t.Fatal("new approval modified original UNKNOWN transaction")
		}
	}
}
