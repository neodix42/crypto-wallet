package signer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

// Only public BIP39 fixture material is used. This adds a historical descriptor
// to disposable encrypted test data; the active adapter registry stays unchanged.
func legacyDecredAccount(t *testing.T, e *Engine, network string, index uint32) model.Account {
	t.Helper()
	seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	account, err := decred.New().Derive(seed, index, network)
	if err != nil {
		t.Fatal(err)
	}
	e.data.Accounts = append(e.data.Accounts, account)
	return account
}
func retiredDecredPlan(account model.Account) model.Plan {
	return model.Plan{ID: "public-retired-decred-operation", Account: account, Asset: account.Network + ":DCR", FeeAsset: account.Network + ":DCR", Recipient: account.Address, Amount: "1", MaxFee: "1", Nonce: "public-original-outpoint", Expires: time.Now().Unix() + 600}
}
func TestRetiredDecredLegacyVaultUpgradePreservesAllIndicesAndEncryptedRecords(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			e, ui, _ := newEngine(t, chains.All())
			createImported(t, e, ui, environment)
			for _, index := range []uint32{1, 127} {
				if err := e.AddAccount(index); err != nil {
					t.Fatal(err)
				}
			}
			arb := map[string]string{"mainnet": "arbitrum-mainnet", "testnet": "arbitrum-sepolia"}[environment]
			var originalActive []model.Account
			for _, account := range e.data.Accounts {
				if account.Network != arb {
					originalActive = append(originalActive, account)
				}
			}
			e.data.Accounts = append([]model.Account(nil), originalActive...)
			networks := []string{"decred-mainnet"}
			if environment == "testnet" {
				networks = []string{"decred-testnet3", "decred-simnet"}
			}
			var retired []model.Account
			for _, network := range networks {
				for _, index := range []uint32{0, 1, 127} {
					account := legacyDecredAccount(t, e, network, index)
					account, err := decred.AdvanceReceive(account)
					if err != nil {
						t.Fatal(err)
					}
					account, err = decred.AdvanceChange(account)
					if err != nil {
						t.Fatal(err)
					}
					account, err = decred.SetDiscovery(account, 123, 25, 500)
					if err != nil {
						t.Fatal(err)
					}
					account, err = decred.SetDiscoveryBudget(account, 12345)
					if err != nil {
						t.Fatal(err)
					}
					e.data.Accounts[len(e.data.Accounts)-1] = account
					retired = append(retired, account)
					e.data.AddressBook = append(e.data.AddressBook, model.AddressBookEntry{ID: fmt.Sprintf("%032x", len(retired)), AccountID: account.ID, Network: network, Name: fmt.Sprintf("Historical DCR contact %d", len(retired)), Address: account.Address})
				}
			}
			e.data.Endpoints = map[string]vault.Endpoint{}
			for _, network := range networks {
				e.data.Endpoints[network] = vault.Endpoint{URL: "https://retired.invalid/public-old-path", User: "public-retired-rpc-user", Password: "public-retired-rpc-password"}
			}
			plan := retiredDecredPlan(retired[0])
			signed := model.Signed{Identity: "public-historical-decred-tx", Bytes: []byte("public historical signed bytes")}
			e.data.Operations = []journal.Operation{{Plan: plan, Digest: journal.Digest(plan), Signed: &signed, State: "UNKNOWN", CreatedAt: 12345}}
			originalRaw, _ := json.Marshal(viewRetiredRecords(e.data))
			var oldRecords retiredRecords
			if err := json.Unmarshal(originalRaw, &oldRecords); err != nil {
				t.Fatal(err)
			}
			if err := e.persist(); err != nil {
				t.Fatal(err)
			}
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatalf("legacy retirement upgrade failed: %v", err)
			}
			if !reflect.DeepEqual(oldRecords, viewRetiredRecords(e.data)) {
				t.Fatalf("retirement modified historical data: accounts=%t endpoints=%t book=%t operations=%t oldstate=%s newstate=%s", reflect.DeepEqual(oldRecords.Accounts, viewRetiredRecords(e.data).Accounts), reflect.DeepEqual(oldRecords.Endpoints, viewRetiredRecords(e.data).Endpoints), reflect.DeepEqual(oldRecords.Book, viewRetiredRecords(e.data).Book), reflect.DeepEqual(oldRecords.Operations, viewRetiredRecords(e.data).Operations), oldRecords.Operations[0].State, e.data.Operations[0].State)
			}
			for _, old := range originalActive {
				found, err := e.PublicAccount(old.ID)
				if err != nil || !reflect.DeepEqual(old, found) {
					t.Fatalf("existing active descriptor changed %s", old.ID)
				}
			}
			arbs := map[uint32]model.Account{}
			for _, account := range e.Snapshot().Accounts {
				if chains.IsRetiredNetwork(account.Network) {
					t.Fatal("retired descriptor exposed")
				}
				if account.Network == arb {
					arbs[account.Index] = account
				}
			}
			if len(arbs) != 3 || len(e.Snapshot().Accounts) != len(originalActive)+3 {
				t.Fatal("upgrade omitted/duplicated account indices")
			}
			seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(seed)
			for _, index := range []uint32{0, 1, 127} {
				expected, err := evm.New().Derive(seed, index, arb)
				if err != nil || !reflect.DeepEqual(expected, arbs[index]) {
					t.Fatalf("Arbitrum migration used a different recovery identity at %d", index)
				}
			}
			previous, err := e.Files.Read("vault.previous.lwv")
			if err != nil || !bytes.Equal(previous, before) {
				t.Fatal("upgrade did not preserve previous encrypted recovery generation")
			}
			current, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			for _, canary := range []string{"public-retired-rpc-password", "Historical DCR contact", "public historical signed bytes"} {
				if bytes.Contains(current, []byte(canary)) {
					t.Fatal("retired data escaped encryption")
				}
			}
			if len(e.Settings()) != 0 {
				t.Fatal("retired RPC credentials exposed as active settings")
			}
			for _, account := range retired {
				if _, err = e.PublicAccount(account.ID); err == nil {
					t.Fatal("retired public account active")
				}
				if _, err = e.Receive(account.ID); err == nil {
					t.Fatal("retired receive active")
				}
				if e.CopyReceive(account.ID) == nil || e.Configure(account.Network) == nil || e.Prepare(retiredDecredPlan(account)) == nil || e.Discovery(account.ID, 0, 20, 100) == nil {
					t.Fatal("retired route reactivated")
				}
				if _, err = e.AddressBook(account.ID); err == nil {
					t.Fatal("retired contact scope active")
				}
			}
			generation := e.data.Generation
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil || e.data.Generation != generation {
				t.Fatal("idempotent migration rewrote vault or failed reopen", err)
			}
			if !reflect.DeepEqual(oldRecords, viewRetiredRecords(e.data)) {
				t.Fatal("repeated unlock changed retained history")
			}
		})
	}
}

type retiredRecords struct {
	Accounts   []model.Account
	Endpoints  map[string]vault.Endpoint
	Book       []model.AddressBookEntry
	Operations []journal.Operation
}

func viewRetiredRecords(data *vault.Data) retiredRecords {
	records := retiredRecords{Endpoints: map[string]vault.Endpoint{}, Book: append([]model.AddressBookEntry(nil), data.AddressBook...), Operations: append([]journal.Operation(nil), data.Operations...)}
	for _, account := range data.Accounts {
		if chains.IsRetiredNetwork(account.Network) {
			records.Accounts = append(records.Accounts, account)
		}
	}
	for network, endpoint := range data.Endpoints {
		records.Endpoints[network] = endpoint
	}
	return records
}
func TestRetiredDecredDescriptorMustStillAuthenticateAgainstRecoveryRoot(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	original := legacyDecredAccount(t, e, "decred-mainnet", 0)
	for _, mutate := range []func(*model.Account){func(a *model.Account) { a.Family = "evm" }, func(a *model.Account) { a.PublicKey = "invalid" }, func(a *model.Account) { a.Address = "invalid" }, func(a *model.Account) { a.Index++ }, func(a *model.Account) { a.Profile = "foreign" }, func(a *model.Account) { a.Parameters = []byte(`{}`) }} {
		changed := original
		mutate(&changed)
		e.data.Accounts[len(e.data.Accounts)-1] = changed
		if e.validateAccounts(e.data) == nil {
			t.Fatal("retired malformed/foreign-root descriptor accepted")
		}
	}
	e.data.Accounts[len(e.data.Accounts)-1] = original
	if err := e.validateAccounts(e.data); err != nil {
		t.Fatal(err)
	}
}
func TestArbitrumUpgradeNeverReplacesExistingDescriptorAndRejectsAccountCapacity(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	before := append([]model.Account(nil), e.data.Accounts...)
	if changed, err := e.addSupportedNetworks(e.data); err != nil || changed || !reflect.DeepEqual(before, e.data.Accounts) {
		t.Fatal("existing Arbitrum descriptor replaced")
	}
	var missing []model.Account
	for _, account := range e.data.Accounts {
		if account.Network != "arbitrum-mainnet" {
			missing = append(missing, account)
		}
	}
	for len(missing) < 10000 {
		account := missing[0]
		account.ID = fmt.Sprintf("bounded-%d", len(missing))
		missing = append(missing, account)
	}
	e.data.Accounts = missing
	if changed, err := e.addSupportedNetworks(e.data); err == nil || changed || len(e.data.Accounts) != 10000 {
		t.Fatal("migration exceeded account budget")
	}
}
func TestNoNewWalletOrAddedAccountReactivatesRetiredNetworks(t *testing.T) {
	e, ui, _ := newEngine(t, append(chains.All(), decred.New()))
	createImported(t, e, ui, "testnet")
	if err := e.AddAccount(1); err != nil {
		t.Fatal(err)
	}
	for _, account := range e.data.Accounts {
		if strings.HasPrefix(account.Network, "decred-") {
			t.Fatal("retained adapter reactivated retired network")
		}
	}
	for _, network := range []string{"decred-mainnet", "decred-testnet3", "decred-simnet"} {
		if _, err := e.adapter(network); err == nil {
			t.Fatal("retained adapter became active")
		}
	}
}

func TestArbitrumUpgradePreservesCompletelyHiddenAccountGroups(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	for _, allHidden := range []bool{true, false} {
		var old []model.Account
		for _, account := range e.data.Accounts {
			if account.Network != "arbitrum-mainnet" {
				account.Hidden = allHidden
				old = append(old, account)
			}
		}
		if !allHidden {
			old[0].Hidden = true
		}
		e.data.Accounts = old
		if changed, err := e.addSupportedNetworks(e.data); err != nil || !changed {
			t.Fatal(err)
		}
		added := e.data.Accounts[len(e.data.Accounts)-1]
		if added.Network != "arbitrum-mainnet" || added.Hidden != allHidden {
			t.Fatal("upgrade changed account group visibility")
		}
		for i := range old {
			if !reflect.DeepEqual(old[i], e.data.Accounts[i]) {
				t.Fatal("upgrade changed an existing visibility/descriptor")
			}
		}
	}
}
