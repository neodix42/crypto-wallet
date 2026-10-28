package signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
			// Begin from a previously installed schema-1 vault that has no
			// optional addressBook member, rather than relying on create-time RAM.
			e.Lock()
			u.inputs = append(u.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal("existing vault without address book could not reopen", err)
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

func TestAddressBookCreateRetryReturnsDurableIdentityWithoutRewritingVaultOrPayment(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := e.Snapshot().Accounts[0]
	entry, err := e.SaveAddress(account.ID, "", "Exact retry recipient", account.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	plan := evmPlan(account)
	if err := e.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	signed, err := e.Sign(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(plan.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Update(plan.ID, "UNKNOWN", &model.Receipt{Identity: signed.Identity, State: "UNKNOWN", Detail: "Synthetic ambiguous submission; no network request"}); err != nil {
		t.Fatal(err)
	}
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	generation := e.data.Generation
	operation := e.data.Operations[0]
	// Simulate losing the initial acknowledgement: the client knows its exact
	// input but not the ID. The retry must observe the first committed record.
	for attempt := 0; attempt < 3; attempt++ {
		retried, err := e.SaveAddress(account.ID, "", entry.Name, entry.Address, entry.Memo)
		if err != nil || retried != entry || len(e.data.AddressBook) != 1 || e.data.Generation != generation {
			t.Fatal("exact create retry changed durable contact identity", err)
		}
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(operation, e.data.Operations[0]) || e.data.Operations[0].State != "UNKNOWN" || e.data.Operations[0].Signed == nil || !bytes.Equal(e.data.Operations[0].Signed.Bytes, signed.Bytes) {
		t.Fatal("metadata retry rewrote ciphertext or changed payment authority", err)
	}
	if _, err := e.SaveAddress(account.ID, "", strings.ToUpper(entry.Name), entry.Address, ""); !errors.Is(err, ErrAddressBookDuplicateName) {
		t.Fatal("non-exact name retry silently replaced the original", err)
	}
	if _, err := e.SaveAddress(account.ID, "", "Different label", entry.Address, ""); !errors.Is(err, ErrAddressBookDuplicateRecipient) {
		t.Fatal("changed-name duplicate silently created another recipient", err)
	}
	if _, err := e.SaveAddress(account.ID, "", entry.Name, "0x1111111111111111111111111111111111111111", ""); !errors.Is(err, ErrAddressBookDuplicateName) {
		t.Fatal("changed-destination duplicate silently replaced the original", err)
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	before, err = e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	generation = e.data.Generation
	retried, err := e.SaveAddress(account.ID, "", entry.Name, entry.Address, entry.Memo)
	after, readErr := e.Files.Read("vault.lwv")
	if err != nil || readErr != nil || retried != entry || len(e.data.AddressBook) != 1 || e.data.Generation != generation || !bytes.Equal(before, after) {
		t.Fatal("exact create retry after restart was not idempotent", err, readErr)
	}
	if !reflect.DeepEqual(operation, e.data.Operations[0]) || e.Prepare(plan) == nil {
		t.Fatal("contact acknowledgement retry changed unresolved payment or released its reservation")
	}
}

func TestAddressBookFailuresHaveFixedActionableCategoriesWithoutInputOrStorageDetails(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := e.Snapshot().Accounts[0]
	entry, err := e.SaveAddress(account.ID, "", "Original recipient", account.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                              string
		account, id, label, address, memo string
		want                              error
	}{
		{"unsupported address", account.ID, "", "Other", "CANARY-private-invalid-address", "", ErrAddressBookInvalidAddress},
		{"bad input", account.ID, "", "CANARY-private\nlabel", account.Address, "", ErrAddressBookInvalidInput},
		{"unsupported memo", account.ID, entry.ID, "Other", account.Address, "CANARY-private-comment", ErrAddressBookInvalidInput},
		{"same folded name", account.ID, "", strings.ToUpper(entry.Name), "0x1111111111111111111111111111111111111111", "", ErrAddressBookDuplicateName},
		{"same destination", account.ID, "", "Other", entry.Address, "", ErrAddressBookDuplicateRecipient},
		{"stale edit", account.ID, strings.Repeat("b", 32), "Other", "0x1111111111111111111111111111111111111111", "", ErrAddressBookStale},
		{"foreign account", "CANARY-private-foreign-account", "", "Other", account.Address, "", ErrAddressBookUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := e.SaveAddress(test.account, test.id, test.label, test.address, test.memo)
			if !errors.Is(err, test.want) || result.ID != "" || strings.Contains(err.Error(), "CANARY") {
				t.Fatal("wrong or unredacted failure category", err)
			}
		})
	}
	if err := e.RemoveAddress(account.ID, strings.Repeat("b", 32)); !errors.Is(err, ErrAddressBookStale) {
		t.Fatal("stale removal lost category", err)
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected input rewrote encrypted vault", err)
	}
	e.Files.Close()
	if _, err := e.SaveAddress(account.ID, entry.ID, "Changed", account.Address, ""); !errors.Is(err, ErrAddressBookWriteFailed) || !e.Snapshot().Locked {
		t.Fatal("failed write lost category or did not lock", err)
	}
	if _, err := e.AddressBook(account.ID); !errors.Is(err, ErrAddressBookUnavailable) {
		t.Fatal("failed storage retained contact read authority", err)
	}
}

func TestAddressBookFullOwnedBookRejectsNewRecordButAllowsExactRetryAndIDBoundEdit(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := e.Snapshot().Accounts[0]
	for i := 0; i < model.MaxAccountAddressBookEntries; i++ {
		e.data.AddressBook = append(e.data.AddressBook, model.AddressBookEntry{ID: fmt.Sprintf("%032x", i+1), AccountID: account.ID, Network: account.Network, Name: fmt.Sprintf("Contact %d", i), Address: fmt.Sprintf("0x%040x", i+1)})
	}
	if err := e.persist(); err != nil {
		t.Fatal("bounded valid address book did not persist", err)
	}
	entry := e.data.AddressBook[0]
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveAddress(account.ID, "", "Extra", "0x1111111111111111111111111111111111111111", ""); !errors.Is(err, ErrAddressBookLimit) {
		t.Fatal("full address book did not report limit", err)
	}
	retried, err := e.SaveAddress(account.ID, "", entry.Name, entry.Address, entry.Memo)
	if err != nil || retried != entry {
		t.Fatal("full address book rejected exact committed retry", err)
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("limit/retry rewrote full address book", err)
	}
	updated, err := e.SaveAddress(account.ID, entry.ID, "Renamed existing", entry.Address, entry.Memo)
	if err != nil || updated.ID != entry.ID || len(e.data.AddressBook) != model.MaxAccountAddressBookEntries {
		t.Fatal("bounded full book could not update exact record", err)
	}
}
