package nativewallet

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/webui"
)

type paymentQuoteRequest struct {
	AccountID string `json:"accountId"`
	Asset     string `json:"asset"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Memo      string `json:"memo"`
	Maximum   bool   `json:"maximum"`
}

type paymentQuoteState struct {
	Draft             model.Transfer
	Plan              *model.Plan
	Due               time.Time
	Revision          uint64
	Loading, Complete bool
	Failure           string
}

func (a *App) paymentDraft(account model.Account) model.Transfer {
	return model.Transfer{AccountID: account.ID, Asset: a.asset(account).ID, Recipient: a.fields["recipient/"+account.ID], Amount: a.fields["amount/"+account.ID], Memo: a.fields["memo/"+account.ID]}
}

func (a *App) stopPaymentQuote() {
	if a.paymentQuoteCancel != nil {
		a.paymentQuoteCancel()
		a.paymentQuoteCancel = nil
	}
	if a.paymentQuote.Loading {
		a.paymentQuote.Loading = false
		a.paymentQuote.Due = time.Now().Add(time.Second)
	}
	a.paymentQuoteSerial++
}

func (a *App) invalidatePaymentQuote() {
	a.stopPaymentQuote()
	if a.historyCancel != nil {
		a.historyCancel()
	}
	a.paymentQuote = paymentQuoteState{Due: time.Now().Add(time.Second), Revision: a.planRevision}
}

func paymentRecipientValid(account model.Account, recipient string) bool {
	if recipient == "" || strings.TrimSpace(recipient) != recipient {
		return false
	}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if network.ID == account.Network {
				return adapter.ValidateAddress(account.Network, recipient) == nil
			}
		}
	}
	return false
}

func quoteRecipientEqual(account model.Account, requested, planned string) bool {
	if !paymentRecipientValid(account, requested) || !paymentRecipientValid(account, planned) {
		return false
	}
	if account.Family == "aptos" {
		left, ok := new(big.Int).SetString(strings.TrimPrefix(requested, "0x"), 16)
		right, valid := new(big.Int).SetString(strings.TrimPrefix(planned, "0x"), 16)
		return ok && valid && left.Cmp(right) == 0
	}
	return historyPeerEqual(account.Family, requested, planned)
}

func (a *App) validPaymentQuote(plan model.Plan, account model.Account, draft model.Transfer, maximum bool) bool {
	if !reflect.DeepEqual(plan.Account, account) || plan.Asset != draft.Asset || plan.Memo != draft.Memo || !quoteRecipientEqual(account, draft.Recipient, plan.Recipient) || plan.Expires <= time.Now().Unix() || plan.Expires > time.Now().Add(30*time.Minute).Unix() {
		return false
	}
	value, err := amount.Atomic(plan.Amount)
	fee, invalid := amount.Atomic(plan.MaxFee)
	if err != nil || invalid != nil || value.Sign() <= 0 || value.BitLen() > 256 || fee.BitLen() > 256 {
		return false
	}
	found := false
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID == plan.FeeAsset {
			found = true
		}
		if !maximum && asset.ID == draft.Asset {
			atomic, e := amount.Parse(draft.Amount, asset.Decimals)
			if e != nil || atomic != plan.Amount {
				return false
			}
		}
	}
	return found
}

func (a *App) callPaymentQuote(ctx context.Context, generation uint64, draft model.Transfer, maximum bool, out *model.Plan) error {
	body := paymentQuoteRequest{draft.AccountID, draft.Asset, draft.Recipient, draft.Amount, draft.Memo, maximum}
	if core, ok := a.core.(interface {
		CallPaymentQuoteAt(context.Context, uint64, any, any) error
	}); ok {
		if err := core.CallPaymentQuoteAt(ctx, generation, body, out); err != nil {
			return err
		}
		if err := normalizePublicPlans(out); err != nil {
			return ipc.NewRequestError("PAYMENT_QUOTE_INVALID_PLAN")
		}
		return nil
	}
	return a.callAt(generation, "quotePayment", body, out)
}

// Debounced estimates never reserve, sign or submit anything. They are cancelled
// when a draft changes or an explicit action needs the coordinator.
func (a *App) nextPaymentQuote() bool {
	if a.busy || a.background || a.state.Locked || !a.state.RecoveryConfirmed {
		return false
	}
	account, ok := a.account(strings.TrimPrefix(a.active, "wallet/"))
	if !ok || a.active != "wallet/"+account.ID || !a.sendForms[account.ID] {
		return false
	}
	draft := a.paymentDraft(account)
	if !paymentRecipientValid(account, draft.Recipient) {
		return false
	}
	atomic, err := amount.Parse(draft.Amount, a.asset(account).Decimals)
	if err != nil || atomic == "0" {
		return false
	}
	q := a.paymentQuote
	if q.Draft != draft || q.Revision != a.planRevision {
		a.paymentQuote = paymentQuoteState{Draft: draft, Revision: a.planRevision, Due: time.Now().Add(time.Second)}
		a.render()
		return false
	}
	if q.Loading || q.Complete || time.Now().Before(q.Due) {
		return false
	}
	if a.historyCancel != nil {
		a.historyCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.paymentQuoteCancel = cancel
	a.paymentQuoteSerial++
	serial, epoch, generation, revision := a.paymentQuoteSerial, a.epoch, a.core.Generation(), a.planRevision
	a.paymentQuote.Loading, a.background = true, true
	a.render()
	go func() {
		defer cancel()
		var plan model.Plan
		err := a.callPaymentQuote(ctx, generation, draft, false, &plan)
		a.ui.Dispatch(func() {
			if a.closed || a.epoch != epoch {
				return
			}
			a.background = false
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			current, owned := a.account(account.ID)
			if !owned || !reflect.DeepEqual(current, account) {
				return
			}
			if a.paymentQuoteSerial != serial || a.planRevision != revision || a.paymentDraft(account) != draft || a.active != "wallet/"+account.ID || !a.sendForms[account.ID] {
				return
			}
			a.paymentQuoteCancel = nil
			a.paymentQuote.Loading, a.paymentQuote.Complete = false, true
			if err == nil && !a.validPaymentQuote(plan, account, draft, false) {
				err = ipc.NewRequestError("PAYMENT_QUOTE_INVALID_PLAN")
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.paymentQuote.Failure = safeError(err)
				}
			} else {
				a.paymentQuote.Plan = &plan
			}
			a.render()
		})
	}()
	return true
}

func (a *App) paymentFeeNodes(account model.Account) []webui.Node {
	text := "Enter a valid recipient and amount to estimate the transaction fee."
	q := a.paymentQuote
	if q.Draft == a.paymentDraft(account) && q.Revision == a.planRevision {
		switch {
		case q.Loading:
			text = "Estimating transaction fee…"
		case q.Failure != "":
			text = "Fee estimate unavailable. " + q.Failure
		case q.Plan != nil:
			text = a.paymentFeeText(*q.Plan)
			if q.Plan.Expires <= time.Now().Unix() {
				text += "\nEstimate expired; Send obtains a fresh estimate before confirmation."
			}
		case !q.Complete:
			text = "Transaction fee estimate pending…"
		}
	}
	n := label(text)
	n.ID = "paymentFee/" + account.ID
	n.Tooltip = "Read-only estimate. Sending obtains a fresh plan and requires confirmation. Network fee changes and refunds can leave a remainder."
	return []webui.Node{n}
}

func (a *App) paymentFeeText(plan model.Plan) string {
	format := func(atomic, assetID string) string {
		for _, asset := range a.network(plan.Account.Network).Assets {
			if asset.ID == assetID {
				if value, err := amount.Format(atomic, asset.Decimals); err == nil {
					return trimDecimal(value) + " " + asset.Symbol
				}
			}
		}
		return "Unknown"
	}
	text := "Estimated transaction fee budget: " + format(plan.MaxFee, plan.FeeAsset) + " (rechecked before confirmation)"
	if plan.Account.Family == "ton" {
		var payload ton.Payload
		if strictjson.Decode(plan.Payload, &payload) == nil {
			text = "Estimated network fee: " + format(payload.EstimatedFee, plan.FeeAsset) + "\nReserved native budget: " + format(plan.MaxFee, plan.FeeAsset)
			if ton.IsJettonPlan(plan) {
				text += " (includes token-call attachment; unused attachment can return)"
			}
			text += "\nRequired wallet storage reserve: " + format(payload.StorageReserve, plan.FeeAsset)
		}
	}
	return text
}
