package nativewallet

import (
	"context"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

func (a *App) maximumPayment(account model.Account) {
	draft := a.paymentDraft(account)
	if !paymentRecipientValid(account, draft.Recipient) {
		a.errorText("Prepare wallet payment", "Enter a valid recipient first so Max can estimate the transaction fee.")
		a.render()
		return
	}
	a.stopPaymentQuote()
	revision := a.planRevision
	invalidPlan := false
	a.workWithFeedback("Estimating maximum amount and transaction fee…", func(g uint64) (func(), error) {
		var plan model.Plan
		err := a.callPaymentQuote(context.Background(), g, draft, true, &plan)
		return func() {
			if a.planRevision != revision || a.active != "wallet/"+account.ID || !a.sendForms[account.ID] {
				return
			}
			if !a.validPaymentQuote(plan, account, draft, true) {
				invalidPlan = true
				return
			}
			value, err := amount.Format(plan.Amount, a.asset(account).Decimals)
			if err != nil {
				return
			}
			a.fields["amount/"+account.ID] = value
			a.plan, a.planIsStaking = nil, false
			a.planRevision++
			a.paymentQuote = paymentQuoteState{Draft: a.paymentDraft(account), Plan: &plan, Revision: a.planRevision, Complete: true}
		}, err
	}, func(err error) {
		if invalidPlan {
			a.reportError("Prepare wallet payment", ipc.NewRequestError("PAYMENT_QUOTE_INVALID_PLAN"))
			return
		}
		if err == nil {
			a.notice("Maximum amount and estimated fee updated. Send opens confirmation; no payment has been prepared or sent.")
		}
	})
}

// A single Send click prepares once, then opens the existing exact signer
// confirmation/password dialog. A quote is never treated as an authorization.
func (a *App) preparePayment(account model.Account) {
	a.preparePaymentDraft(account, a.paymentDraft(account), a.planRevision)
}

func (a *App) preparePaymentDraft(account model.Account, draft model.Transfer, revision uint64) {
	if a.state.Locked || !a.state.RecoveryConfirmed || a.planRevision != revision || a.active != "wallet/"+account.ID || a.paymentDraft(account) != draft {
		return
	}
	validation := a.paymentValidation(account, draft)
	if !validation.RecipientValid || !validation.AmountValid {
		message := validation.RecipientError
		if validation.RecipientValid {
			message = validation.AmountError
		}
		if message == "" {
			message = "Enter a valid recipient and a positive amount using the selected asset’s decimal precision."
		}
		a.errorText("Prepare wallet payment", message)
		a.render()
		return
	}
	a.stopPaymentQuote()
	ready := false
	invalidPlan := false
	a.workWithFeedback("Preparing payment and fresh transaction fee…", func(g uint64) (func(), error) {
		var plan model.Plan
		err := a.callAt(g, "prepare", draft, &plan)
		return func() {
			if a.planRevision != revision || a.active != "wallet/"+account.ID || !a.sendForms[account.ID] || a.paymentDraft(account) != draft {
				return
			}
			if !a.validPaymentQuote(plan, account, draft, false) {
				invalidPlan = true
				return
			}
			a.plan, a.planIsStaking = &plan, false
			a.paymentQuote = paymentQuoteState{Draft: draft, Plan: &plan, Revision: revision, Complete: true}
			a.refreshWalletLocalHistory(account.ID)
			ready = true
		}, err
	}, func(err error) {
		// Preparation may have been durably recorded even when its reply is
		// lost or the edited form no longer accepts the returned preview.
		defer a.queuePendingPayments(account.ID)
		if invalidPlan {
			a.reportError("Prepare wallet payment", ipc.NewRequestError("PAYMENT_QUOTE_INVALID_PLAN"))
			return
		}
		if ipc.PublicErrorCode(err) == "PAYMENT_PENDING" {
			a.resolvePendingPayment(account, &draft, revision)
			return
		}
		if err == nil && ready && a.plan != nil && a.plan.Expires > time.Now().Unix() {
			a.paymentAction("send", a.plan.ID)
		}
	})
}
