package nativewallet

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"localwallet/internal/model"
)

type streamingBalanceBackend struct {
	*testBackend
	stream func(context.Context, uint64, []string, func(model.BalanceBatchResult)) error
}

func (b *streamingBalanceBackend) CallBalancesAt(ctx context.Context, g uint64, ids []string, emit func(model.BalanceBatchResult)) error {
	return b.stream(ctx, g, ids, emit)
}

func nativeBalanceResult(a *App, account model.Account, atomic string) model.BalanceBatchResult {
	return model.BalanceBatchResult{AccountID: account.ID, Network: account.Network, Values: []model.Balance{{Asset: a.network(account.Network).Assets[0].ID, Atomic: atomic, State: "provider-observed", At: "public-checkpoint"}}}
}

func finishBalanceRefresh(t *testing.T, a *App, ui *testFrontend) {
	t.Helper()
	for a.balanceRefresh != nil {
		ui.apply(t)
	}
}

func TestBalanceRefreshPaintsFastNetworkWhileAnotherProviderIsBlocked(t *testing.T) {
	a, ui, core := fixture(t)
	accounts := append([]model.Account(nil), a.state.Accounts...)
	fast := ethAccount(t, a)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	selected := make(chan []string, 1)
	a.core = &streamingBalanceBackend{testBackend: core, stream: func(ctx context.Context, generation uint64, ids []string, emit func(model.BalanceBatchResult)) error {
		selected <- append([]string(nil), ids...)
		value := nativeBalanceResult(a, fast, "1000000000000000000")
		emit(value)
		value.Values[0].Atomic = "999" // The queued UI result must own its slice.
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		for _, account := range accounts {
			if account.ID != fast.ID {
				emit(nativeBalanceResult(a, account, "1"))
			}
		}
		return nil
	}}
	for _, account := range accounts {
		a.balanceQueue = append(a.balanceQueue, account.ID)
	}
	a.balanceQueue = append(a.balanceQueue, fast.ID, "foreign-account")
	a.nextBalance()
	ui.apply(t)
	if got := <-selected; len(got) != 8 {
		t.Fatal("batch narrowed, duplicated or included an unowned account", got)
	}
	value, _ := a.balance(fast, a.asset(fast))
	if value != "1" || !a.background || a.busy || a.balanceRefresh == nil {
		t.Fatal("fast network waited for the slow provider or acquired confirmation", value)
	}
	close(release)
	finishBalanceRefresh(t, a, ui)
	if a.background || core.killed.Load() != 0 || len(a.balances) != 8 || len(a.balanceErrors) != 0 {
		t.Fatal("stream completion lost network coverage or ended the session")
	}
}

func TestBalanceRefreshForegroundCancellationKeepsCompletedRowsAndRequeuesOnlyUnfinished(t *testing.T) {
	a, ui, core := fixture(t)
	fast, slow := a.state.Accounts[0], a.state.Accounts[1]
	stopped := make(chan struct{})
	started := make(chan struct{})
	a.core = &streamingBalanceBackend{testBackend: core, stream: func(ctx context.Context, generation uint64, ids []string, emit func(model.BalanceBatchResult)) error {
		emit(nativeBalanceResult(a, fast, "1"))
		close(started)
		<-ctx.Done()
		// Even a late, otherwise valid observation cannot repaint after cancellation.
		emit(nativeBalanceResult(a, slow, "999"))
		close(stopped)
		return ctx.Err()
	}}
	a.balanceQueue = []string{fast.ID, slow.ID}
	a.nextBalance()
	<-started
	ui.apply(t)
	core.handler = func(uint64, string, any, any) error { return nil }
	a.work("Foreground fixture action", func(g uint64) (func(), error) {
		<-stopped
		return nil, core.CallAt(g, "fixtureAction", struct{}{}, nil)
	})
	for a.busy {
		ui.apply(t)
	}
	for len(ui.queue) != 0 {
		ui.apply(t)
	}
	if !reflect.DeepEqual(a.balanceQueue, []string{slow.ID}) || len(a.balances[fast.ID]) != 1 || len(a.balances[slow.ID]) != 0 || a.background || a.balanceRefresh != nil || core.killed.Load() != 0 {
		t.Fatal("foreground preemption lost a completed balance, accepted stale data or killed the signer", a.balanceQueue)
	}
}

func TestBalanceRefreshRejectsHostileOrMissingProgressWithoutZeroBalances(t *testing.T) {
	for _, mode := range []string{"foreign-account", "foreign-network", "foreign-token", "duplicate", "missing", "null-values", "private-error", "changed-descriptor", "rpc-error", "token-partial"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			for i := range a.state.Accounts {
				if a.state.Accounts[i].ID == account.ID {
					a.state.Accounts[i].Parameters = []byte("{}")
				}
			}
			a.core = &streamingBalanceBackend{testBackend: core, stream: func(ctx context.Context, generation uint64, ids []string, emit func(model.BalanceBatchResult)) error {
				value := nativeBalanceResult(a, account, "1000000000000000000")
				switch mode {
				case "foreign-account":
					value.AccountID = "foreign"
				case "foreign-network":
					value.Network = "ton-mainnet"
				case "foreign-token":
					value.Values[0].Asset = account.Network + ":untracked"
				case "null-values":
					value.Values = nil
				case "private-error":
					value.Error, value.Values = "private-provider-message", nil
				case "rpc-error":
					value.Error, value.Values = "BALANCE_UNAVAILABLE", nil
				case "token-partial":
					value.Values = append(value.Values, model.Balance{Asset: a.network(account.Network).Assets[1].ID, State: "unknown", At: "public-checkpoint"})
				}
				if mode != "missing" {
					emit(value)
				}
				if mode == "duplicate" {
					emit(value)
				}
				return errors.New("batch fixture completion")
			}}
			a.balanceQueue = []string{account.ID}
			a.nextBalance()
			if mode == "changed-descriptor" {
				for i := range a.state.Accounts {
					if a.state.Accounts[i].ID == account.ID {
						a.state.Accounts[i].Parameters[0] = '['
					}
				}
			}
			finishBalanceRefresh(t, a, ui)
			value, _ := a.balance(account, a.asset(account))
			if mode == "token-partial" {
				if value != "1" || a.balanceErrors[account.ID] {
					t.Fatal("unknown token hid a checked native balance")
				}
			} else if value != "Unknown" || !a.balanceErrors[account.ID] || len(a.balances[account.ID]) != 0 {
				t.Fatal("invalid/missing stream became a known or zero balance", mode, value)
			}
			if core.killed.Load() != 0 {
				t.Fatal("fixture stream cancelled by killing the signer")
			}
		})
	}
}

func TestBalanceRefreshLockCloseAndSnapshotIgnoreLateCallbacks(t *testing.T) {
	for _, mode := range []string{"lock", "close", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			late := nativeBalanceResult(a, account, "999")
			started, stopped := make(chan struct{}), make(chan struct{})
			a.core = &streamingBalanceBackend{testBackend: core, stream: func(ctx context.Context, generation uint64, ids []string, emit func(model.BalanceBatchResult)) error {
				close(started)
				<-ctx.Done()
				emit(late)
				close(stopped)
				return ctx.Err()
			}}
			a.balanceQueue = []string{account.ID}
			a.nextBalance()
			<-started
			switch mode {
			case "lock":
				a.lock()
			case "close":
				a.close()
			case "snapshot":
				a.applySnapshot(a.state, a.networks)
			}
			<-stopped
			for len(ui.queue) != 0 {
				ui.apply(t)
			}
			if len(a.balances[account.ID]) != 0 || a.balanceRefresh != nil || a.background {
				t.Fatal("late provider progress crossed lifecycle or snapshot boundary")
			}
		})
	}
}
