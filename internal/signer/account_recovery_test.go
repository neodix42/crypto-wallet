package signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func recoveryJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAccountRecoveryReusesVisibleRangeWithoutDerivationOrVaultRewrite(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	before := recoveryJSON(t, e.data)
	ciphertext, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := e.Files.Read("vault.previous.lwv")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for index, adapter := range e.Adapters {
		e.Adapters[index] = &recoveryDerivationAdapter{Adapter: adapter, derive: func([]byte, uint32, string) (model.Account, error) {
			calls++
			return model.Account{}, errors.New("CANARY-adapter-detail-must-not-be-requested")
		}}
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := e.DiscoverAccounts(0, 0); err != nil {
			t.Fatal("already available range failed", err)
		}
	}
	if calls != 0 || !bytes.Equal(before, recoveryJSON(t, e.data)) {
		t.Fatal("idempotent recovery derived again or changed state")
	}
	after, _ := e.Files.Read("vault.lwv")
	old, _ := e.Files.Read("vault.previous.lwv")
	if !bytes.Equal(ciphertext, after) || !bytes.Equal(previous, old) {
		t.Fatal("idempotent recovery replaced encrypted generations")
	}
	if e.AddAccount(0) == nil {
		t.Fatal("legacy addAccount duplicate denial changed")
	}
}

func TestAccountRecoveryRestoresAllProfilesAndAssociationsPreservingActualUnknown(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "testnet")
	// These offline public fixtures model already authenticated association
	// metadata; recovery must not replace them with a newly derived address.
	for _, network := range []string{"near-testnet", "aptos-testnet"} {
		account := findAccount(t, e, network)
		if network == "near-testnet" {
			var parameters near.Parameters
			if json.Unmarshal(account.Parameters, &parameters) != nil {
				t.Fatal("NEAR fixture parameters")
			}
			parameters.Named = true
			account.Address = "verified.testnet"
			account.Parameters = recoveryJSON(t, parameters)
		} else {
			var parameters aptos.Parameters
			if json.Unmarshal(account.Parameters, &parameters) != nil {
				t.Fatal("Aptos fixture parameters")
			}
			parameters.Associated = true
			account.Address = "0x000000000000000000000000000000000000000000000000000000000000000a"
			account.Parameters = recoveryJSON(t, parameters)
		}
		if err := e.Associate(account); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.AddTONAccount(0, ton.Profile); err != nil {
		t.Fatal(err)
	}
	account := findAccount(t, e, "ethereum-sepolia")
	plan := evmPlan(account)
	if err := e.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	signed, err := e.Sign(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Update(plan.ID, "SUBMITTING", nil) != nil || e.Update(plan.ID, "UNKNOWN", &model.Receipt{Identity: signed.Identity, State: "UNKNOWN"}) != nil {
		t.Fatal("actual signed UNKNOWN fixture")
	}
	if e.RenameWallet(account.ID, "Saved ETH name") != nil || e.RenameGroup(0, "Saved group name") != nil || e.DeleteGroup(0) != nil {
		t.Fatal("local label/archive fixture")
	}
	root := bytes.Clone(e.data.Entropy)
	defer clear(root)
	original := append([]model.Account(nil), e.data.Accounts...)
	operations := recoveryJSON(t, e.data.Operations)
	generation := e.data.Generation
	if err := e.DiscoverAccounts(0, 1); err != nil {
		t.Fatal(err)
	}
	if e.data.Generation != generation+1 || e.data.AccountPresentation.DeletedGroups["0"] || e.data.AccountPresentation.GroupLabels["0"] != "Saved group name" || e.data.AccountPresentation.WalletLabels[account.ID] != "Saved ETH name" {
		t.Fatal("recovery lost local names or did not clear archive marker once")
	}
	networks := map[string]bool{}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if network.Testnet && !chains.IsRetiredNetwork(network.ID) {
				networks[network.ID] = true
			}
		}
	}
	if len(e.data.Accounts) != len(original)+len(networks) {
		t.Fatal("missing canonical wallets or duplicate alternate profiles")
	}
	for index, expected := range original {
		expected.Hidden = false
		if !bytes.Equal(recoveryJSON(t, expected), recoveryJSON(t, e.data.Accounts[index])) {
			t.Fatal("recovery changed existing keys, profiles, association or address")
		}
	}
	assertOriginal := func() {
		t.Helper()
		if !bytes.Equal(root, e.data.Entropy) || !bytes.Equal(operations, recoveryJSON(t, e.data.Operations)) || !journal.Conflicts(e.data.Operations, plan) {
			t.Fatal("recovery changed root, original signed UNKNOWN bytes or reservations")
		}
		if e.data.Operations[0].Signed.Identity != signed.Identity || !bytes.Equal(e.data.Operations[0].Signed.Bytes, signed.Bytes) || e.data.Operations[0].Digest != journal.Digest(plan) {
			t.Fatal("signed transaction identity changed")
		}
	}
	assertOriginal()
	expectedAccounts := recoveryJSON(t, e.data.Accounts)
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	assertOriginal()
	if !bytes.Equal(expectedAccounts, recoveryJSON(t, e.data.Accounts)) || e.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("recovery visibility/profile changes were not durable")
	}
}

type recoveryDerivationAdapter struct {
	model.Adapter
	derive func([]byte, uint32, string) (model.Account, error)
}

func (a *recoveryDerivationAdapter) Derive(seed []byte, index uint32, network string) (model.Account, error) {
	return a.derive(seed, index, network)
}

func TestAccountRecoveryTypedRangeCancellationStaleAndDerivationFailuresAreAtomic(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	if !errors.Is(e.DiscoverAccounts(0, 0), ErrAccountRecoveryLocked) {
		t.Fatal("locked recovery category")
	}
	createImported(t, e, ui, "testnet")
	if e.DeleteGroup(0) != nil {
		t.Fatal("hidden group fixture")
	}
	before := recoveryJSON(t, e.data)
	for _, bounds := range [][2]uint32{{2, 1}, {0, 20}, {derivation.MaxIndex - 1, derivation.MaxIndex}, {0, ^uint32(0)}} {
		if !errors.Is(e.DiscoverAccounts(bounds[0], bounds[1]), ErrAccountRecoveryInvalidRange) {
			t.Fatal("range not rejected", bounds)
		}
	}
	ui.approve = false
	if !errors.Is(e.DiscoverAccounts(0, 1), ErrAccountRecoveryCancelled) || !bytes.Equal(before, recoveryJSON(t, e.data)) {
		t.Fatal("cancelled recovery mutated state")
	}
	ui.approve = true
	e.UI = &presentationHookUI{testUI: ui, hook: func() { e.data.Generation++ }}
	accounts := recoveryJSON(t, e.data.Accounts)
	if !errors.Is(e.DiscoverAccounts(0, 1), ErrAccountRecoveryStale) || !bytes.Equal(accounts, recoveryJSON(t, e.data.Accounts)) || !e.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("stale approval restored visibility or added wallets")
	}
	e.UI = ui
	before = recoveryJSON(t, e.data)
	adapter := evm.New()
	calls := 0
	e.Adapters = []model.Adapter{&recoveryDerivationAdapter{Adapter: adapter, derive: func(seed []byte, index uint32, network string) (model.Account, error) {
		calls++
		if calls > 1 {
			return model.Account{}, errors.New("CANARY-provider-or-secret-detail")
		}
		return adapter.Derive(seed, index, network)
	}}}
	if err := e.DiscoverAccounts(0, 1); !errors.Is(err, ErrAccountRecoveryDerivation) || calls != 2 || !bytes.Equal(before, recoveryJSON(t, e.data)) {
		t.Fatal("partially derived recovery mutated state or leaked error", err)
	}
	e.Adapters = nil
	if !errors.Is(e.DiscoverAccounts(0, 1), ErrAccountRecoveryDerivation) || !bytes.Equal(before, recoveryJSON(t, e.data)) {
		t.Fatal("missing recovery adapter silently reported success")
	}
	e.Adapters = []model.Adapter{adapter}
	e.UI = &presentationHookUI{testUI: ui, hook: e.Lock}
	if !errors.Is(e.DiscoverAccounts(0, 1), ErrAccountRecoveryStale) || !e.Snapshot().Locked {
		t.Fatal("lock in approval accepted stale recovery")
	}
}

func TestAccountRecoveryEnforcesTwentyIndicesExclusiveBoundAndStoredBudget(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	if err := e.DiscoverAccounts(derivation.MaxIndex-20, derivation.MaxIndex-1); err != nil {
		t.Fatal("last twenty valid indices rejected", err)
	}
	last := map[uint32]bool{}
	for _, account := range e.data.Accounts {
		last[account.Index] = true
	}
	if !last[derivation.MaxIndex-20] || !last[derivation.MaxIndex-1] || last[derivation.MaxIndex] {
		t.Fatal("inclusive range omitted edge or exceeded exclusive bound")
	}
	// A bounded in-memory account-count fixture exercises the preflight limit;
	// these synthetic records are never saved or claimed as derived accounts.
	for len(e.data.Accounts) < 10000 {
		account := e.data.Accounts[0]
		account.ID = fmt.Sprintf("budget-fixture:%d", len(e.data.Accounts))
		e.data.Accounts = append(e.data.Accounts, account)
	}
	before := recoveryJSON(t, e.data)
	if !errors.Is(e.DiscoverAccounts(1, 1), ErrAccountRecoveryLimit) || !bytes.Equal(before, recoveryJSON(t, e.data)) {
		t.Fatal("stored account budget not enforced before mutation")
	}
}

func TestAccountRecoveryFailedPersistenceLocksAndRetainsOriginalEncryptedWallet(t *testing.T) {
	e, ui, directory := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	if e.DeleteGroup(0) != nil {
		t.Fatal("hidden group fixture")
	}
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	e.Files.Close()
	if !errors.Is(e.DiscoverAccounts(0, 1), ErrAccountRecoveryStorage) || !e.Snapshot().Locked {
		t.Fatal("failed recovery save did not return typed failure and lock")
	}
	after, err := os.ReadFile(filepath.Join(directory, "vault.lwv"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed recovery save replaced original ciphertext", err)
	}
	files, err := vault.NewFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	reopened := New(files, ui, []model.Adapter{evm.New()})
	defer reopened.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := reopened.Unlock(); err != nil {
		t.Fatal(err)
	}
	if !reopened.data.AccountPresentation.DeletedGroups["0"] {
		t.Fatal("failed recovery destroyed original archive marker")
	}
	for _, account := range reopened.data.Accounts {
		if account.Index != 0 || !account.Hidden {
			t.Fatal("failed recovery persisted partial visibility or new accounts")
		}
	}
}
