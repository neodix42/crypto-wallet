package nativewallet

// Public, deterministic descriptor/controller tests only. No wallet profile,
// network, clipboard, private prompt or funded Decred transaction is exercised.
import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/decred"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

func decredReceiveFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account, model.Account) {
	t.Helper()
	a, ui, core := fixture(t)
	a.state.Environment = a.environment
	a.state.WalletExists = true
	account, err := decred.New().Derive(bytes.Repeat([]byte{0x27}, 64), 0, "decred-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for index, old := range a.state.Accounts {
		if old.Family == "decred" {
			account.ID = old.ID
			a.state.Accounts[index] = account
			found = true
		}
	}
	if !found {
		t.Fatal("fixture has no Decred wallet")
	}
	next, err := decred.AdvanceReceive(account)
	if err != nil {
		t.Fatal(err)
	}
	return a, ui, core, account, next
}

func TestDecredReceiveAndQRRefreshOnlyTheDurableSelectedAddressEvenAfterCancellation(t *testing.T) {
	for _, method := range []string{"receive", "saveQR"} {
		for _, cancelled := range []bool{false, true} {
			name := method + map[bool]string{false: "-completed", true: "-cancelled"}[cancelled]
			t.Run(name, func(t *testing.T) {
				a, ui, core, before, after := decredReceiveFixture(t)
				plan := &model.Plan{ID: "public-original-plan", Account: before}
				a.plan, a.planRevision = plan, 19
				a.tabs, a.active = []string{before.ID}, "wallet/"+before.ID
				a.fields["recipient/"+before.ID] = "public-existing-recipient"
				a.balances[before.ID] = []model.Balance{{Asset: "dcr", Atomic: "7", State: "provider-observed", At: "public-block"}}
				a.balanceQueue = []string{"public-pending-balance"}
				a.bookOwner, a.bookEdit = ethAccount(t, a).ID, "public-contact"
				a.stakingReading, a.stakingRevision = "public-near-account", 29
				balances, fields := viewText(a.balances), viewText(a.fields)
				accounts := append([]model.Account(nil), a.state.Accounts...)
				fresh := a.state
				fresh.Accounts = append([]model.Account(nil), accounts...)
				for index := range fresh.Accounts {
					if fresh.Accounts[index].ID == before.ID {
						fresh.Accounts[index] = after
					}
				}
				var calls []string
				core.handler = func(_ uint64, called string, body, out any) error {
					calls = append(calls, called)
					switch called {
					case method:
						if viewText(body) != viewText(map[string]string{"accountId": before.ID}) {
							t.Error("Receive changed its selected owned account")
						}
						if cancelled {
							return ipc.NewRequestError("REQUEST_CANCELLED")
						}
						return answer(out, json.RawMessage(`null`))
					case "accounts":
						return answer(out, fresh)
					case "copyReceive":
						if viewText(body) != viewText(map[string]string{"accountId": after.ID}) {
							t.Error("Copy did not bind the synchronized owned account")
						}
						return answer(out, json.RawMessage(`null`))
					default:
						return errors.New("unrelated backend request")
					}
				}
				a.walletEvent(method + "/" + before.ID)
				ui.apply(t)
				current, ok := a.account(before.ID)
				if !ok || viewText(current) != viewText(after) || a.state.Locked || a.busy {
					t.Fatal("durable Receive address was not synchronized")
				}
				for _, account := range accounts {
					if account.ID != before.ID {
						current, _ := a.account(account.ID)
						if viewText(current) != viewText(account) {
							t.Fatal("Receive replaced a different wallet descriptor")
						}
					}
				}
				if a.plan != plan || a.planRevision != 19 || viewText(a.balances) != balances || viewText(a.fields) != fields || !reflect.DeepEqual(a.balanceQueue, []string{"public-pending-balance"}) || a.bookOwner == "" || a.bookEdit != "public-contact" || a.stakingRevision != 29 || a.active != "wallet/"+before.ID || !reflect.DeepEqual(a.tabs, []string{before.ID}) {
					t.Fatal("Receive refresh discarded unrelated prepared or UI state")
				}
				page := a.walletPage(current)
				if page.Nodes[1].Children[0].Value != after.Address {
					t.Fatal("wallet continued displaying the old receive address")
				}
				if cancelled && !strings.Contains(a.status, "cancelled") {
					t.Fatal("successful address synchronization hid the cancelled action")
				}
				a.walletEvent("copyReceive/" + before.ID)
				ui.apply(t)
				if a.status != "Address copied." || !reflect.DeepEqual(calls, []string{method, "accounts", "copyReceive"}) {
					t.Fatal("Copy did not use its acknowledged direct path after synchronization")
				}
			})
		}
	}
}

func TestDecredReceiveRefreshFailureLocksInsteadOfEnablingTheOldAddress(t *testing.T) {
	for _, bad := range []string{"unavailable", "locked", "unconfirmed", "wrong-environment", "missing-account", "foreign-root", "wrong-network", "skipped-address"} {
		t.Run(bad, func(t *testing.T) {
			a, ui, core, before, after := decredReceiveFixture(t)
			fresh := a.state
			fresh.Accounts = append([]model.Account(nil), fresh.Accounts...)
			for index := range fresh.Accounts {
				if fresh.Accounts[index].ID == before.ID {
					fresh.Accounts[index] = after
					if bad == "foreign-root" {
						other, err := decred.New().Derive(bytes.Repeat([]byte{0x38}, 64), 0, "decred-mainnet")
						if err != nil {
							t.Fatal(err)
						}
						other.ID = before.ID
						fresh.Accounts[index] = other
					}
					if bad == "wrong-network" {
						fresh.Accounts[index].Network = "decred-testnet3"
					}
					if bad == "skipped-address" {
						next, err := decred.AdvanceReceive(after)
						if err != nil {
							t.Fatal(err)
						}
						fresh.Accounts[index] = next
					}
				}
			}
			if bad == "locked" {
				fresh.Locked = true
			}
			if bad == "unconfirmed" {
				fresh.RecoveryConfirmed = false
			}
			if bad == "wrong-environment" {
				fresh.Environment = "testnet"
			}
			if bad == "missing-account" {
				fresh.Accounts = nil
				fresh.TotalAccounts = 0
			}
			core.handler = func(_ uint64, method string, _, out any) error {
				if method == "receive" {
					return ipc.NewRequestError("REQUEST_CANCELLED")
				}
				if method != "accounts" || bad == "unavailable" {
					return errors.New("synthetic-read-failure")
				}
				return answer(out, fresh)
			}
			a.walletEvent("receive/" + before.ID)
			ui.apply(t)
			if !a.state.Locked || len(a.state.Accounts) != 0 || a.busy || core.killed.Load() != 1 || !strings.Contains(a.status, "receive address could not be refreshed") || strings.Contains(a.status, "synthetic") {
				t.Fatal("uncertain Receive left the previous address enabled or disclosed raw failure")
			}
		})
	}
}

func TestDecredReceiveRefreshCannotRestoreAccountsAfterLock(t *testing.T) {
	a, ui, core, before, after := decredReceiveFixture(t)
	fresh := a.state
	fresh.Accounts = []model.Account{after}
	fresh.TotalAccounts = 1
	reading, release := make(chan struct{}), make(chan struct{})
	core.handler = func(_ uint64, method string, _, out any) error {
		if method == "receive" {
			return answer(out, json.RawMessage(`null`))
		}
		if method != "accounts" {
			return errors.New("unexpected request")
		}
		close(reading)
		<-release
		return answer(out, fresh)
	}
	a.walletEvent("receive/" + before.ID)
	select {
	case <-reading:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("Receive never requested its current descriptor")
	}
	a.lock()
	close(release)
	ui.apply(t)
	if !a.state.Locked || len(a.state.Accounts) != 0 || a.plan != nil {
		t.Fatal("late Receive synchronization repopulated a locked wallet")
	}
}

func TestDecredReceiveCancellationBeforeAdvancePreservesItsValidatedDescriptor(t *testing.T) {
	a, ui, core, before, _ := decredReceiveFixture(t)
	fresh := a.state
	core.handler = func(_ uint64, method string, _, out any) error {
		if method == "receive" {
			return ipc.NewRequestError("REQUEST_CANCELLED")
		}
		if method != "accounts" {
			return errors.New("unexpected request")
		}
		return answer(out, fresh)
	}
	a.walletEvent("receive/" + before.ID)
	ui.apply(t)
	current, ok := a.account(before.ID)
	if !ok || viewText(current) != viewText(before) || a.state.Locked || !strings.Contains(a.status, "cancelled") {
		t.Fatal("cancellation before advance changed or disabled the verified current address")
	}
}
