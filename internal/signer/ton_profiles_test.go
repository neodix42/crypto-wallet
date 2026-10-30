package signer

import (
	"bytes"
	"encoding/json"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func TestTONExplicitRecoveryProfilePreservesExistingAccountsAcrossRestart(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, engine, ui, "testnet")
	// Model an authenticated old installation with its original contract
	// descriptor. The upgraded default must never relabel this funded address.
	seed, err := derivation.Seed(engine.data.Entropy, engine.data.Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ton.New().DeriveProfile(seed, 0, "ton-testnet", ton.Profile)
	clear(seed)
	if err != nil {
		t.Fatal(err)
	}
	engine.data.Accounts = []model.Account{legacy}
	if err := engine.persist(); err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(legacy)
	if err := engine.AddTONAccount(legacy.Index, ton.ProfileSubwallet42); err != nil {
		t.Fatal("explicit profile recovery failed", err)
	}
	accounts := engine.Snapshot().Accounts
	if len(accounts) != 2 || accounts[1].Address == legacy.Address || accounts[1].Profile != ton.ProfileSubwallet42 || accounts[1].ID == legacy.ID {
		t.Fatal("explicit subwallet42 overwrote legacy identity")
	}
	after, _ := json.Marshal(accounts[0])
	if !bytes.Equal(original, after) {
		t.Fatal("original funded-account descriptor changed")
	}
	engine.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := engine.Unlock(); err != nil {
		t.Fatal("registered legacy/new identities did not survive restart", err)
	}
	for index, account := range engine.Snapshot().Accounts {
		expected, _ := json.Marshal(accounts[index])
		actual, _ := json.Marshal(account)
		if !bytes.Equal(expected, actual) {
			t.Fatal("restarted profile/address changed")
		}
	}
	if engine.AddTONAccount(legacy.Index, ton.Profile) == nil || engine.AddTONAccount(legacy.Index, ton.ProfileSubwallet42) == nil {
		t.Fatal("duplicate existing profile was re-added")
	}
}

func TestTONExplicitRecoveryProfileRequiresKnownProfileConsentAndConfirmedWallet(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	if engine.AddTONAccount(0, ton.Profile) == nil {
		t.Fatal("locked profile recovery permitted")
	}
	createImported(t, engine, ui, "mainnet")
	for _, profile := range []string{"", "42", "raw-wallet-id42", "arbitrary-contract"} {
		if engine.AddTONAccount(1, profile) == nil {
			t.Fatal("unregistered profile accepted", profile)
		}
	}
	ui.approve = false
	if engine.AddTONAccount(1, ton.ProfileSubwallet42) == nil || len(engine.Snapshot().Accounts) != 1 {
		t.Fatal("cancelled contract choice changed the account list")
	}
	engine.data.RecoveryConfirmed = false
	ui.approve = true
	if engine.AddTONAccount(1, ton.ProfileSubwallet42) == nil {
		t.Fatal("unconfirmed root offered new TON contract")
	}
	engine.data.RecoveryConfirmed = true
	if engine.AddTONAccount(1000001, ton.ProfileSubwallet42) == nil {
		t.Fatal("unbounded profile index accepted")
	}
	engine.Adapters = []model.Adapter{}
	if engine.AddTONAccount(1, ton.ProfileSubwallet42) == nil {
		t.Fatal("missing TON adapter accepted")
	}
}

func TestTONSubwallet42FreshPhraseRestoreWithDifferentStoragePassword(t *testing.T) {
	source, sourceUI, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, source, sourceUI, "mainnet")
	if err := source.AddTONAccount(17, ton.ProfileSubwallet42); err != nil {
		t.Fatal(err)
	}
	expected := source.Snapshot().Accounts[1]
	source.Lock()
	// No file, profile, machine keyring or local secret is copied. The explicit
	// contract profile/index accompanies the fixed public recovery phrase.
	fresh, freshUI, _ := newEngine(t, []model.Adapter{ton.New()})
	const otherPassword = "OTHER-public-synthetic-storage-password-726391"
	freshUI.inputs = append(freshUI.inputs, input(otherPassword, otherPassword, syntheticPhrase, ""))
	if err := fresh.Create("mainnet", 12, true); err != nil {
		t.Fatal(err)
	}
	if err := fresh.AddTONAccount(17, ton.ProfileSubwallet42); err != nil {
		t.Fatal(err)
	}
	actual := fresh.Snapshot().Accounts[1]
	expectedJSON, _ := json.Marshal(expected)
	actualJSON, _ := json.Marshal(actual)
	if !bytes.Equal(expectedJSON, actualJSON) {
		t.Fatal("fresh phrase/profile/index restore changed TON StateInit, wallet ID, address or key")
	}
}
