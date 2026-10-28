package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func TestAddressBookAllNetworksEncryptedReloadAndOwnedAccountIsolation(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			e, u, dir := newEngine(t, chains.All())
			createImported(t, e, u, environment)
			accounts := e.Snapshot().Accounts
			if environment == "mainnet" && len(accounts) != 8 || environment == "testnet" && len(accounts) < 8 {
				t.Fatal("required network omitted", len(accounts))
			}
			if err := e.AddAccount(1); err != nil {
				t.Fatal(err)
			}
			book := map[string][]model.AddressBookEntry{}
			for _, account := range accounts {
				memo := ""
				if account.Family == "ton" {
					memo = "Deposit memo 7182"
				}
				entry, err := e.SaveAddress(account.ID, "", "Saved recipient "+account.Network, account.Address, memo)
				if err != nil || entry.Network != account.Network || entry.AccountID != account.ID {
					t.Fatal("network was not assigned from owned account", err)
				}
				book[account.ID] = []model.AddressBookEntry{entry}
				list, err := e.AddressBook(account.ID)
				if err != nil || !reflect.DeepEqual(list, book[account.ID]) {
					t.Fatal("contact list leaked another network")
				}
				list[0].Name = "changed public copy"
				again, _ := e.AddressBook(account.ID)
				if again[0].Name != entry.Name {
					t.Fatal("caller could mutate signer metadata")
				}
			}
			for _, account := range e.Snapshot().Accounts {
				if account.Index == 1 {
					list, err := e.AddressBook(account.ID)
					if err != nil || len(list) != 0 {
						t.Fatal("contacts crossed account index")
					}
				}
			}
			ciphertext, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(ciphertext, []byte("Saved recipient")) {
				t.Fatal("address book persisted in plaintext")
			}
			public, _ := json.Marshal(e.Snapshot())
			if bytes.Contains(public, []byte("Saved recipient")) {
				t.Fatal("contacts escaped scoped public request")
			}
			e.Lock()
			if entries, err := e.AddressBook(accounts[0].ID); err == nil || entries != nil {
				t.Fatal("locked address book exposed")
			}
			u.inputs = append(u.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal(err)
			}
			for id, expected := range book {
				entries, err := e.AddressBook(id)
				if err != nil || !reflect.DeepEqual(entries, expected) {
					t.Fatal("contacts lost across encrypted reload", err)
				}
			}
			first := book[accounts[0].ID][0]
			first, err = e.SaveAddress(first.AccountID, first.ID, "Renamed contact", first.Address, first.Memo)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.RemoveAddress(accounts[1].ID, first.ID); err == nil {
				t.Fatal("foreign network removed contact")
			}
			if err := e.RemoveAddress(first.AccountID, first.ID); err != nil {
				t.Fatal(err)
			}
			entries, _ := e.AddressBook(first.AccountID)
			if len(entries) != 0 {
				t.Fatal("removed contact still listed")
			}
			if err := e.RemoveAddress(first.AccountID, first.ID); err == nil {
				t.Fatal("stale deletion accepted")
			}
		})
	}
}

func TestAddressBookRejectsMalformedStaleLockedAndPendingWithoutWrites(t *testing.T) {
	e, u, _ := newEngine(t, chains.All())
	createImported(t, e, u, "testnet")
	var account model.Account
	for _, a := range e.Snapshot().Accounts {
		if a.Network == "ethereum-sepolia" {
			account = a
		}
	}
	entry, err := e.SaveAddress(account.ID, "", "Recipient", account.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := e.Files.Read("vault.lwv")
	for _, tc := range []struct{ account, id, name, address, memo string }{
		{"foreign", "", "Other", account.Address, ""},
		{account.ID, strings.Repeat("b", 32), "Other", account.Address, ""},
		{account.ID, "", "recipient", "0x1111111111111111111111111111111111111111", ""},
		{account.ID, "", "Another", account.Address, ""},
		{account.ID, "", "Other", "invalid", ""},
		{account.ID, "", "Other", "0x0000000000000000000000000000000000000000", ""},
		{account.ID, "", "Other\u202e", account.Address, ""},
		{account.ID, entry.ID, "Other", account.Address, "unsupported memo"},
	} {
		if result, err := e.SaveAddress(tc.account, tc.id, tc.name, tc.address, tc.memo); err == nil || result.ID != "" {
			t.Fatal("invalid/stale contact accepted")
		}
	}
	after, _ := e.Files.Read("vault.lwv")
	if !bytes.Equal(before, after) {
		t.Fatal("rejected contact rewrote vault")
	}
	for _, mode := range []string{"pending", "persistence failed", "locked"} {
		switch mode {
		case "pending":
			e.data.RecoveryConfirmed = false
		case "persistence failed":
			e.data.RecoveryConfirmed = true
			e.persistFailed = true
		case "locked":
			e.Lock()
		}
		if list, err := e.AddressBook(account.ID); err == nil || list != nil {
			t.Fatal("unavailable book returned", mode)
		}
		if saved, err := e.SaveAddress(account.ID, entry.ID, "Changed", account.Address, ""); err == nil || saved.ID != "" {
			t.Fatal("unavailable book edited", mode)
		}
		if err := e.RemoveAddress(account.ID, entry.ID); err == nil {
			t.Fatal("unavailable book removed", mode)
		}
	}
}

func TestAddressBookFailedSaveAndRemovePersistLockWithoutReturningSuccess(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "remove"}[remove], func(t *testing.T) {
			e, u, dir := newEngine(t, []model.Adapter{evm.New()})
			createImported(t, e, u, "testnet")
			account := e.Snapshot().Accounts[0]
			entry, err := e.SaveAddress(account.ID, "", "Durable original", account.Address, "")
			if err != nil {
				t.Fatal(err)
			}
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			e.Files.Close()
			if remove {
				if e.RemoveAddress(account.ID, entry.ID) == nil {
					t.Fatal("undurable removal succeeded")
				}
			} else {
				if result, err := e.SaveAddress(account.ID, entry.ID, "Undurable change", account.Address, ""); err == nil || result.ID != "" {
					t.Fatal("undurable edit returned contact")
				}
			}
			if !e.Snapshot().Locked {
				t.Fatal("persistence failure did not lock signer")
			}
			after, _ := os.ReadFile(filepath.Join(dir, "vault.lwv"))
			if !bytes.Equal(before, after) {
				t.Fatal("failed metadata persistence damaged previous ciphertext")
			}
			files, err := vault.NewFiles(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			reopened := New(files, u, []model.Adapter{evm.New()})
			defer reopened.Lock()
			u.inputs = append(u.inputs, input(syntheticPassword))
			if err := reopened.Unlock(); err != nil {
				t.Fatal(err)
			}
			entries, err := reopened.AddressBook(account.ID)
			if err != nil || len(entries) != 1 || entries[0] != entry {
				t.Fatal("restart lost prior durable contact", err)
			}
		})
	}
}

func TestAddressBookEditsNeverRewritePreparedPaymentOrGrantApproval(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	account := e.Snapshot().Accounts[0]
	entry, err := e.SaveAddress(account.ID, "", "Payment recipient", "0x1111111111111111111111111111111111111111", "")
	if err != nil {
		t.Fatal(err)
	}
	plan := evmPlan(account)
	if err := e.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	original := e.data.Operations[0]
	if _, err := e.SaveAddress(account.ID, entry.ID, "Changed recipient", "0x2222222222222222222222222222222222222222", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveAddress(account.ID, entry.ID); err != nil {
		t.Fatal(err)
	}
	current := e.data.Operations[0]
	if !reflect.DeepEqual(original, current) || current.State != "PREPARED" || current.Digest != journal.Digest(plan) || current.Signed != nil {
		t.Fatal("editing contacts changed or approved payment")
	}
}

func TestAddressBookFinalDeletionPreservesEncryptedRecoveryVerification(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := e.Snapshot().Accounts[0]
	entry, err := e.SaveAddress(account.ID, "", "Temporary recipient", account.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveAddress(account.ID, entry.ID); err != nil {
		t.Fatal(err)
	}
	if e.data.AddressBook != nil {
		t.Fatal("final deletion retained a noncanonical empty slice")
	}
	ciphertext, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(ciphertext, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	restored, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if err := vault.VerifyRecovery(e.data, restored); err != nil {
		t.Fatal("final contact deletion changed recovery verification after encrypted reload", err)
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	entries, err := e.AddressBook(account.ID)
	if err != nil || len(entries) != 0 || e.data.AddressBook != nil {
		t.Fatal("final deletion did not survive real signer unlock", err)
	}
}
