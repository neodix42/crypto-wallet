package nativewallet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

type pendingPaymentResponse struct {
	Operation *journal.Operation `json:"operation"`
}

func (a *App) confirmPaymentDialog(title, summary string) bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return false
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false
	}
	prompt := uiprompt.Prompt{ID: hex.EncodeToString(nonce[:]), Kind: "confirm", Title: title, Summary: summary}
	epoch, generation, revision := a.epoch, a.core.Generation(), a.privateRevision.Load()
	a.busy = true
	a.render()
	reply, err := a.ui.Prompt(prompt)
	if a.closed || a.state.Locked || a.epoch != epoch || a.core.Generation() != generation || a.privateRevision.Load() != revision || !a.state.RecoveryConfirmed {
		if !a.closed && a.epoch == epoch {
			a.busy = false
			if a.core.Generation() != generation {
				a.lock()
			}
		}
		return false
	}
	a.busy = false
	if err != nil {
		return false
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return false
	}
	defer clear(raw)
	checked, err := uiprompt.DecodeReply(raw, prompt)
	return err == nil && checked.Accepted
}

func (a *App) pendingSummary(operation journal.Operation) string {
	plan := operation.Plan
	value := plan.Amount + " atomic units"
	for _, asset := range a.network(plan.Account.Network).Assets {
		if asset.ID == plan.Asset {
			if formatted, err := amount.Format(plan.Amount, asset.Decimals); err == nil {
				value = trimDecimal(formatted) + " " + asset.Symbol
			}
		}
	}
	text := "Original operation: " + plan.ID + "\nState: " + operation.State + "\nNetwork: " + plan.Account.Network + "\nFrom: " + plan.Account.Address + "\nTo: " + plan.Recipient + "\nAmount: " + value + "\nMemo: " + plan.Memo + "\n" + a.paymentFeeText(plan)
	if operation.Receipt != nil && operation.Receipt.Identity != "" {
		text += "\nOriginal transaction: " + operation.Receipt.Identity
	}
	if ton.IsDeploymentPlan(plan) {
		text += "\n" + ton.DeploymentEffects(plan)
	}
	if effects, ok := near.TokenEffects(plan); ok {
		text += "\n" + effects
	}
	if effects, ok := stakingDialogDescription(plan); ok {
		text += "\n" + effects
	}
	return text
}

func (a *App) hasPendingPayment(account model.Account) bool {
	for _, operation := range a.walletHistory[account.ID].Local.Operations {
		if !journal.Terminal(operation.State) && a.walletHistoryOperation(account, operation) {
			return true
		}
	}
	return false
}

func (a *App) resolvePendingPayment(account model.Account, draft *model.Transfer, revision uint64) {
	a.resolvePendingPaymentExpected(account, draft, revision, "")
}

func (a *App) resolvePendingPaymentExpected(account model.Account, draft *model.Transfer, revision uint64, expectedID string) {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.active != "wallet/"+account.ID {
		return
	}
	a.stopPaymentQuote()
	var operation *journal.Operation
	invalidOperation := false
	a.workWithFeedback("Checking this wallet’s pending payment…", func(g uint64) (func(), error) {
		var response pendingPaymentResponse
		err := a.callAt(g, "pendingPayment", map[string]string{"accountId": account.ID}, &response)
		return func() {
			if response.Operation != nil && (!a.walletHistoryOperation(account, *response.Operation) || journal.Terminal(response.Operation.State) || expectedID != "" && response.Operation.Plan.ID != expectedID) {
				invalidOperation = true
				return
			}
			operation = response.Operation
		}, err
	}, func(err error) {
		if invalidOperation {
			a.notice(safeError(ipc.NewRequestError("PAYMENT_PENDING_UNAVAILABLE")))
			return
		}
		if err != nil || a.state.Locked || a.active != "wallet/"+account.ID {
			return
		}
		if operation == nil {
			if expectedID != "" {
				a.notice("The selected original operation is no longer pending. No other operation was opened or sent.")
				return
			}
			a.notice("This wallet has no unresolved local payment.")
			if draft != nil {
				a.preparePaymentDraft(account, *draft, revision)
			}
			return
		}
		if operation.State == "PREPARED" {
			plan := operation.Plan
			if expectedID != "" {
				cancelChoice, e := newPendingChoice("Cancel unsigned preview", "Cancel only this original unsigned preview. Nothing is signed or broadcast; the saved record remains.")
				if e != nil {
					return
				}
				choices := []uiprompt.PendingChoice{}
				reviewID := ""
				if plan.Expires > time.Now().Unix() {
					review, e := newPendingChoice("Review original payment", "Open the exact original approval and current-password dialog. No replacement payment is prepared.")
					if e != nil {
						return
					}
					choices, reviewID = append(choices, review), review.ID
				}
				choices = append(choices, cancelChoice)
				selected, accepted, _ := a.pendingChoiceDialog("Original unsigned payment", a.pendingSummary(*operation), choices)
				if !accepted {
					a.notice("Unsigned preview left unchanged.")
					return
				}
				if selected == cancelChoice.ID {
					a.cancelPendingPayment(account, plan.ID, nil, revision)
					return
				}
				if selected != reviewID || reviewID == "" {
					return
				}
			}
			matches := draft == nil
			if draft != nil {
				matches = a.validPaymentQuote(plan, account, *draft, false)
			}
			if matches && plan.Expires > time.Now().Unix() {
				a.plan, a.planIsStaking = &plan, false
				a.paymentAction("send", plan.ID)
				return
			}
			if !a.confirmPaymentDialog("Cancel earlier unsigned payment?", a.pendingSummary(*operation)+"\n\nThis unsigned preview cannot be used for the current payment. Cancel it? Nothing is signed or sent by cancelling.") {
				a.notice("Pending payment left unchanged.")
				return
			}
			a.cancelPendingPayment(account, operation.Plan.ID, draft, revision)
			return
		}
		if operation.State == "SIGNED_NOT_SUBMITTED" {
			// A standalone reconciliation cannot move this durable state to
			// UNKNOWN. Retry already checks the original identity before marking
			// SUBMITTING, and requires no new signature or replacement plan.
			if !a.confirmPaymentDialog("Broadcast original signed payment?", a.pendingSummary(*operation)+"\n\nThis operation is already signed. Broadcast only its already-approved signed bytes? The original identity is checked before transmission. This does not create or sign the payment currently entered in the form.") {
				a.notice("Original signed payment left unchanged.")
				return
			}
			if a.active == "wallet/"+account.ID {
				a.paymentAction("retry", operation.Plan.ID)
			}
			return
		}
		if len(historyOperationKinds(operation.State)) == 0 {
			choice, e := newPendingChoice("Leave this original unchanged", "This saved approval has not reached a state that can be checked or broadcast here.")
			if e == nil {
				_, _, _ = a.pendingChoiceDialog("Original pending approval", a.pendingSummary(*operation), []uiprompt.PendingChoice{choice})
			}
			return
		}
		// An inbox selection already explicitly requested this original's
		// review. A direct Send conflict also needs an actionable dialog,
		// instead of silently checking and reporting only a status-bar message.
		if expectedID == "" && operation.State != "UNKNOWN" {
			choice, e := newPendingChoice("Check original transaction status", "Read the original transaction's status. This does not sign, broadcast, cancel or replace a payment.")
			if e != nil {
				return
			}
			selected, accepted, _ := a.pendingChoiceDialog("Payment is still pending", a.pendingSummary(*operation), []uiprompt.PendingChoice{choice})
			if !accepted || selected != choice.ID {
				a.notice("Original pending payment left unchanged.")
				return
			}
		}
		a.reconcilePendingPayment(account, *operation, draft, revision)
	})
}

func (a *App) cancelPendingPayment(account model.Account, id string, draft *model.Transfer, revision uint64) {
	a.workWithFeedback("Cancelling original unsigned payment…", func(g uint64) (func(), error) {
		err := a.callAt(g, "cancelPrepared", map[string]string{"id": id}, nil)
		return func() {
			a.plan = nil
			delete(a.tonPending, account.ID)
			delete(a.stakingPending, account.ID)
			delete(a.nearTokenPending, account.ID)
		}, err
	}, func(err error) {
		a.refreshWalletLocalHistory(account.ID)
		if err == nil && draft != nil {
			a.preparePaymentDraft(account, *draft, revision)
		}
	})
}

func (a *App) reconcilePendingPayment(account model.Account, original journal.Operation, draft *model.Transfer, revision uint64) {
	var receipt model.Receipt
	a.workWithFeedback("Checking original transaction status…", func(g uint64) (func(), error) {
		err := a.callAt(g, "reconcile", map[string]string{"id": original.Plan.ID}, &receipt)
		return nil, err
	}, func(err error) {
		a.refreshWalletLocalHistory(account.ID)
		if err != nil || a.state.Locked || a.active != "wallet/"+account.ID {
			return
		}
		switch receipt.State {
		case "CONFIRMED", "FAILED", "REPLACED", "SUBMITTED", "UNKNOWN", "REORGED":
		default:
			a.notice("The original transaction status could not be verified. No broadcast was requested.")
			return
		}
		if receipt.Identity == "" || original.Receipt != nil && original.Receipt.Identity != "" && receipt.Identity != original.Receipt.Identity {
			a.notice("The original transaction identity could not be verified. No broadcast was requested.")
			return
		}
		if journal.Terminal(receipt.State) {
			a.notice("Original payment status: " + receipt.State + ". No new payment was prepared. Choose Send again only if you intend another payment.")
			return
		}
		if original.State != "UNKNOWN" && original.State != "SIGNED_NOT_SUBMITTED" && receipt.State != "UNKNOWN" {
			a.notice("Original payment status: " + receipt.State + ". Its reservation remains until resolved.")
			return
		}
		original.Receipt = &receipt
		if !a.confirmPaymentDialog("Broadcast original signed payment?", a.pendingSummary(original)+"\n\nThe original outcome is unresolved. Broadcast only its already-approved signed bytes again? This does not create or sign the payment currently entered in the form.") {
			a.notice("Original signed payment left unchanged.")
			return
		}
		if a.active == "wallet/"+account.ID {
			a.paymentAction("retry", original.Plan.ID)
		}
	})
}

func (a *App) pendingPaymentEvent(kind, id string) bool {
	if kind != "pendingPayment" {
		return false
	}
	account, ok := a.account(id)
	if ok && !a.state.Locked && a.state.RecoveryConfirmed && !a.busy && a.active == "wallet/"+id {
		a.forcePendingPayments(account.ID)
		a.nextPendingPayments()
	}
	return true
}

// History is presentation only. Its former action IDs cannot request authority.
func removedHistoryAction(kind string) bool {
	return strings.Contains(" sendOperation retry reconcile ", " "+kind+" ")
}
