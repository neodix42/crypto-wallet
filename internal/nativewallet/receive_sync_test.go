package nativewallet

// Decred descriptors remain authenticated encrypted recovery records. Current
// Wails actions must never reactivate their historical receive/copy/QR paths.
import (
	"bytes"
	"testing"

	"localwallet/internal/chains/decred"
	"localwallet/internal/model"
)

func TestRetiredDecredHasNoWalletPageOrReceiveAction(t *testing.T) {
	a, _, core := fixture(t)
	for _, network := range a.networks {
		if network.Family == "decred" {
			t.Fatal("retired network remains in active public registry")
		}
	}
	for _, account := range a.state.Accounts {
		if account.Family == "decred" {
			t.Fatal("retired account remains on overview")
		}
	}
	// A stale/hostile public descriptor must not bypass the retired-family guard.
	account, err := decred.New().Derive(bytes.Repeat([]byte{0x27}, 64), 0, "decred-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	a.state.Accounts = append(a.state.Accounts, account)
	calls := 0
	core.handler = func(uint64, string, any, any) error {
		calls++
		t.Error("retired UI action reached coordinator")
		return nil
	}
	if _, ok := a.account(account.ID); ok {
		t.Fatal("retired descriptor accepted by active account lookup")
	}
	for _, kind := range []string{"open", "receive", "saveQR", "copyReceive", "send", "prepare"} {
		a.walletEvent(kind + "/" + account.ID)
	}
	if calls != 0 || len(a.tabs) != 0 || a.busy {
		t.Fatal("retired wallet opened or started a public/private operation")
	}
}
func TestRetiredDecredSnapshotCannotQueueBalanceHistoryOrSettingsRoutes(t *testing.T) {
	a, _, _ := fixture(t)
	account, err := decred.New().Derive(bytes.Repeat([]byte{0x27}, 64), 0, "decred-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	state := a.state
	state.Accounts = append(append([]model.Account(nil), state.Accounts...), account)
	networks := append(append([]model.Network(nil), a.networks...), decred.New().Networks()...)
	a.applySnapshot(state, networks)
	for _, current := range a.state.Accounts {
		if current.Family == "decred" {
			t.Fatal("retired snapshot account retained in public UI")
		}
	}
	for _, network := range a.networks {
		if network.Family == "decred" {
			t.Fatal("retired snapshot RPC retained in Settings")
		}
	}
	for _, id := range a.balanceQueue {
		if id == account.ID {
			t.Fatal("retired account queued for balance RPC")
		}
	}
	a.queueWalletHistory(account.ID, "initial")
	if _, exists := a.walletHistory[account.ID]; exists {
		t.Fatal("retired account queued for history RPC")
	}
	if text := viewText(a.view()); bytes.Contains([]byte(text), []byte("Decred")) || bytes.Contains([]byte(text), []byte(account.Address)) {
		t.Fatal("retired address/connection guidance shown in current application")
	}
}
