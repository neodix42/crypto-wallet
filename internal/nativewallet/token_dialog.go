package nativewallet

import (
	"errors"
	"reflect"

	"localwallet/internal/uiprompt"
)

// Contract selectors are public text. Their metadata and subsequent import
// approval still come from the existing coordinator/signer policy path.
func (a *App) trackTokenDialog(id string) {
	account, owned := a.account(id)
	if !owned || a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.active != "wallet/"+id || !ethereumTokenNetwork(account) && !nearTokenNetwork(account) {
		return
	}
	a.pausePendingPayments()
	a.stopBalanceRefresh(true)
	a.stopPaymentQuote()
	if a.historyCancel != nil {
		a.historyCancel()
	}
	label := "Token contract address on " + a.network(account.Network).Name
	if nearTokenNetwork(account) {
		label = "Token contract account on " + a.network(account.Network).Name
	}
	epoch, generation, privateRevision := a.epoch, a.core.Generation(), a.privateRevision.Load()
	a.busy = true
	a.render()
	ui := uiprompt.UI{Exchange: a.ui.Prompt}
	values, err := ui.PublicInput("Track new token", label)
	defer func() {
		for i := range values {
			values[i] = ""
		}
	}()
	if a.closed || a.epoch != epoch {
		return
	}
	a.busy = false
	if a.core.Generation() != generation {
		a.lock()
		return
	}
	current, stillOwned := a.account(id)
	if a.state.Locked || !a.state.RecoveryConfirmed || a.privateRevision.Load() != privateRevision || !stillOwned || !reflect.DeepEqual(current, account) || a.active != "wallet/"+id {
		a.render()
		return
	}
	if err != nil || len(values) != 1 {
		if errors.Is(err, uiprompt.ErrCancelled) {
			a.notice("Token tracking cancelled.")
		} else {
			a.notice("The token contract could not be read. Open Track new token to try again.")
		}
		a.render()
		return
	}
	a.fields["tokenContract/"+id] = values[0]
	a.tokenEvent("importToken", id)
	delete(a.fields, "tokenContract/"+id)
}
