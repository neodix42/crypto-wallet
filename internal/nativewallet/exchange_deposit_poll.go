package nativewallet

import (
	"strings"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/journal"
)

// A submitted wizard deposit is tracked in its own page rather than repeatedly
// interrupting that workflow with the automatic pending-operations inbox.
// The explicit original-payment recovery and every other payment remain intact.
func (a *App) wizardManagesPendingDeposit(operation journal.Operation) bool {
	if a.wizard == nil {
		return false
	}
	return wizardOwnsPendingDeposit(a.wizard.state, operation)
}

func wizardOwnsPendingDeposit(w *exchange.WizardState, operation journal.Operation) bool {
	if w == nil || !w.Valid() {
		return false
	}
	if operation.Plan.ID != w.DepositOperation || operation.Plan.Account.ID != w.SourceAccount || operation.Plan.Asset != w.SourceAsset {
		return false
	}
	switch operation.State {
	case "SUBMITTING", "SUBMITTED", "UNKNOWN", "REORGED":
		return true
	}
	return false
}

func (a *App) notifyWizardDepositConfirmed(u *exchangeWizardUI, check *wizardDepositCredit) {
	if check == nil || !check.ChainConfirmed && !check.Ready {
		return
	}
	if u.state != nil && u.state.TransferOperation == "" && check.Ready && check.ReportedAmount != "" {
		value, limit := a.fields["wizardTransferAmount"], wizardTransferLimit(check)
		if value == "" || value == u.state.DepositAmount || !wizardAtMost(value, limit) {
			a.fields["wizardTransferAmount"] = limit
		}
	}
	a.result = "Deposit confirmed on-chain."
	if check.Message != "" {
		a.result += "\n" + check.Message
	}
	if check.TransactionID != "" {
		a.result += "\nTransaction: " + check.TransactionID
	}
	a.resultOwner = "exchange"
	if !u.depositNotified {
		u.depositNotified = true
		a.notice("Deposit confirmed on-chain. Step 2 shows the deposited asset and Funding / Unified balances.")
	}
}

// Background polling reconciles only the recorded original deposit. It never
// prepares, signs, retries, sends, or transfers a payment.
func (a *App) nextWizardDepositCheck() bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.background || a.active != "exchange" || a.fields["exchangeMode"] == "existing" || a.selectedExchangeProvider().ID != "bybit" {
		return false
	}
	u := a.wizard
	if u == nil || u.state == nil || u.state.DepositOperation == "" || u.state.Step > 2 || u.state.TransferOperation != "" || u.depositChecking || time.Since(u.depositCheckedAt) < 5*time.Second {
		return false
	}
	if u.state.Step == 2 && u.credit != nil && u.credit.Ready {
		return false
	}
	switch u.depositState {
	case "PREPARED", "AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "CANCELLED", "MISSING", "FAILED":
		return false
	}
	u.depositChecking, u.depositCheckedAt = true, time.Now()
	u.depositPollSerial++
	serial := u.depositPollSerial
	original := *u.state
	epoch, generation, revision := a.epoch, a.core.Generation(), a.exchangeRevision
	go func() {
		var check wizardDepositCredit
		err := a.callAt(generation, "exchangeDepositCheck", map[string]string{"wizardId": original.ID}, &check)
		var result struct {
			Wizard             *exchange.WizardState
			WizardDepositState string
		}
		if err == nil {
			err = a.callAt(generation, "exchangeWizardLoad", struct{}{}, &result)
		}
		a.ui.Dispatch(func() {
			if u.depositPollSerial != serial {
				return
			}
			u.depositChecking = false
			if a.closed || a.epoch != epoch {
				return
			}
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			if a.busy || a.wizard != u || u.state == nil || u.state.ID != original.ID || u.state.DepositOperation != original.DepositOperation || u.state.Step != original.Step || u.state.TransferOperation != original.TransferOperation || u.state.SwapOperation != original.SwapOperation || u.state.WithdrawOperation != original.WithdrawOperation || a.exchangeRevision != revision || a.selectedExchangeProvider().ID != "bybit" {
				return
			}
			if err != nil {
				// Keep the page usable and retry the read on the next interval.
				// Transient provider reads do not open a confirmation/error modal.
				if u.credit == nil {
					u.credit = &wizardDepositCredit{}
				}
				u.credit.ReadError = safeError(err)
				u.credit.Message = "Deposit confirmation could not be refreshed. Checking again automatically. " + u.credit.ReadError
				a.render()
				return
			}
			if result.Wizard == nil || !result.Wizard.Valid() || result.Wizard.ID != original.ID || result.Wizard.DepositOperation != original.DepositOperation {
				return
			}
			a.adoptWizardState(result.Wizard)
			u.depositState = result.WizardDepositState
			u.credit, u.balances = &check, check.Balances
			a.notifyWizardDepositConfirmed(u, &check)
			a.render()
		})
	}()
	return true
}

func wizardCreditReady(check *wizardDepositCredit) bool {
	return check != nil && check.Ready && !strings.EqualFold(check.ChainState, "FAILED")
}

func wizardTransferLimit(check *wizardDepositCredit) string {
	if check == nil {
		return ""
	}
	if check.TransferBudget != "" {
		return check.TransferBudget
	}
	return check.ReportedAmount
}
