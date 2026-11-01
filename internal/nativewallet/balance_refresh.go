package nativewallet

import (
	"context"
	"reflect"

	"localwallet/internal/model"
)

type balanceBatchBackend interface {
	CallBalancesAt(context.Context, uint64, []string, func(model.BalanceBatchResult)) error
}

type balanceRefreshState struct {
	serial   uint64
	ids      []string
	accounts map[string]model.Account
	seen     map[string]bool
	cancel   context.CancelFunc
	invalid  bool
}

// Foreground work preempts this read-only request without killing the signer.
// Only unfinished observations return to the queue; completed wallet rows stay
// available while an unrelated provider is slow or unavailable.
func (a *App) stopBalanceRefresh(requeue bool) {
	run := a.balanceRefresh
	if run == nil {
		return
	}
	a.balanceRefresh = nil
	a.balanceSerial++
	run.cancel()
	a.background = false
	if !requeue {
		return
	}
	queued := make(map[string]bool)
	ids := make([]string, 0, len(run.ids)+len(a.balanceQueue))
	add := func(id string) {
		if queued[id] {
			return
		}
		if _, owned := a.account(id); owned {
			queued[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range run.ids {
		if !run.seen[id] {
			add(id)
		}
	}
	for _, id := range a.balanceQueue {
		add(id)
	}
	a.balanceQueue = ids
}

func (a *App) nextBalance() {
	if a.closed || a.state.Locked || a.busy || a.background || len(a.balanceQueue) == 0 {
		return
	}
	if core, ok := a.core.(balanceBatchBackend); ok {
		a.startBalanceRefresh(core)
		return
	}
	// Compatibility for explicitly retained old backends and controller fixtures.
	// The shipped supervisor always implements the streaming batch interface.
	a.nextLegacyBalance()
}

func (a *App) startBalanceRefresh(core balanceBatchBackend) {
	accounts := make(map[string]model.Account)
	ids := make([]string, 0, model.BalanceBatchLimit)
	for len(a.balanceQueue) > 0 && len(ids) < model.BalanceBatchLimit {
		id := a.balanceQueue[0]
		a.balanceQueue = a.balanceQueue[1:]
		account, owned := a.account(id)
		if _, duplicate := accounts[id]; owned && !duplicate {
			account.Parameters = append([]byte(nil), account.Parameters...)
			accounts[id] = account
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.balanceSerial++
	run := &balanceRefreshState{serial: a.balanceSerial, ids: ids, accounts: accounts, seen: map[string]bool{}, cancel: cancel}
	a.balanceRefresh, a.background = run, true
	epoch, generation := a.epoch, a.core.Generation()
	go func() {
		defer cancel()
		_ = core.CallBalancesAt(ctx, generation, append([]string(nil), ids...), func(result model.BalanceBatchResult) {
			result.Values = append([]model.Balance(nil), result.Values...)
			a.ui.Dispatch(func() {
				if !a.currentBalanceRefresh(run, epoch, generation) || run.invalid {
					return
				}
				account, selected := run.accounts[result.AccountID]
				current, owned := a.account(result.AccountID)
				valid := selected && owned && reflect.DeepEqual(account, current) && result.Network == account.Network && !run.seen[result.AccountID] && model.ValidBalanceBatchResult(result)
				if valid {
					assets := map[string]bool{}
					for _, asset := range a.network(account.Network).Assets {
						assets[asset.ID] = true
					}
					for _, value := range result.Values {
						valid = valid && assets[value.Asset]
					}
				}
				if !valid {
					run.invalid = true
					run.cancel()
					return
				}
				run.seen[result.AccountID] = true
				if result.Error != "" {
					a.balanceErrors[result.AccountID] = true
					delete(a.balances, result.AccountID)
				} else {
					delete(a.balanceErrors, result.AccountID)
					a.balances[result.AccountID] = result.Values
				}
				a.render()
			})
		})
		a.ui.Dispatch(func() {
			if !a.currentBalanceRefresh(run, epoch, generation) {
				return
			}
			a.balanceRefresh, a.background = nil, false
			for _, id := range run.ids {
				if !run.seen[id] || run.invalid {
					a.balanceErrors[id] = true
					delete(a.balances, id)
				}
			}
			// Successfully completed observations survive an unrelated RPC or
			// batch failure. Missing observations remain unknown, never zero.
			a.render()
		})
	}()
}

func (a *App) currentBalanceRefresh(run *balanceRefreshState, epoch, generation uint64) bool {
	if a.closed || a.state.Locked || a.epoch != epoch || a.balanceRefresh != run || a.balanceSerial != run.serial {
		return false
	}
	if a.core.Generation() != generation {
		a.lock()
		return false
	}
	return true
}
