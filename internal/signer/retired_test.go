package signer

import (
	"fmt"
	"reflect"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/vault"
)

func TestRetiredHorizenVaultUnlockPreservesKeysJournalAndEncryptedMetadata(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			e, ui, _ := newEngine(t, chains.All())
			createImported(t, e, ui, environment)
			before := e.Snapshot().Accounts
			base := findAccount(t, e, "base-"+map[string]string{"mainnet": "mainnet", "testnet": "sepolia"}[environment])
			old := base
			old.Network = "horizen-" + environment
			old.ID = fmt.Sprintf("evm:%d:%s", old.Index, old.Network)
			e.data.Accounts = append(e.data.Accounts, old)
			e.data.Endpoints = map[string]vault.Endpoint{old.Network: {URL: "https://retired.invalid", Password: "PUBLIC-retired-node-password"}}
			plan := evmPlan(old)
			plan.ID = "historical-cancelled-operation"
			e.data.Operations = []journal.Operation{{Plan: plan, Digest: journal.Digest(plan), State: "CANCELLED"}}
			historical := e.data.Operations[0]
			if err := e.persist(); err != nil {
				t.Fatal(err)
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal("existing vault cannot unlock after retirement", err)
			}
			if !reflect.DeepEqual(e.Snapshot().Accounts, before) || len(e.data.Accounts) != len(before)+1 || !reflect.DeepEqual(e.data.Operations[0], historical) || !reflect.DeepEqual(e.data.Accounts[len(before)], old) {
				t.Fatal("retirement changed an active key or destroyed historical data")
			}
			if len(e.Settings()) != 0 || e.data.Endpoints[old.Network].Password != "PUBLIC-retired-node-password" {
				t.Fatal("retired endpoint activated or destroyed")
			}
			page, err := e.SnapshotPage("")
			if err != nil || page.TotalAccounts != len(before) || !reflect.DeepEqual(page.Accounts, before) {
				t.Fatal("public paging exposed retired accounts")
			}
			if _, err = e.PublicAccount(old.ID); err == nil {
				t.Fatal("retired public account active")
			}
			if _, err = e.Receive(old.ID); err == nil {
				t.Fatal("retired receive active")
			}
			if err = e.Configure(old.Network); err == nil {
				t.Fatal("retired RPC configurable")
			}
			if err = e.Prepare(plan); err == nil {
				t.Fatal("retired payment prepared")
			}
			journalPage, err := e.JournalPage("", "", "")
			if err != nil || journalPage.Total != 1 || !reflect.DeepEqual(journalPage.Operations[0], historical) {
				t.Fatal("historical journal unavailable")
			}
			// Same root and index still control the Base receive address.
			seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(seed)
			if old.Address != base.Address {
				t.Fatal("historical EVM recovery path changed")
			}
			bad := old
			bad.PublicKey = "0x00"
			e.data.Accounts[len(before)] = bad
			if e.validateAccounts(e.data) == nil {
				t.Fatal("retirement bypassed root authentication")
			}
		})
	}
}

func TestRetiredActiveAccountsLocked(t *testing.T) {
	if got := (&Engine{}).activeAccounts(); got != nil {
		t.Fatal("locked engine exposed accounts")
	}
}
