package signer

import (
	"encoding/json"
	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/journal"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
)

// SaveExchangeWizard persists a navigation draft, not a debit capability.
// Every payment/transfer/order/withdrawal retains its separate authorization.
func (e *Engine) SaveExchangeWizard(w exchange.WizardState) error {
	x, err := e.exchangeData()
	if err != nil || x.Selected() != "bybit" || !w.Valid() {
		return ErrDenied
	}
	credential := x.Credential("bybit")
	if credential == nil {
		return ErrDenied
	}
	if old := x.Wizard; old != nil && x.WizardCredentialID == vault.ExchangeCredentialID(credential) {
		if w.ID != old.ID {
			if old.Step != 5 && (old.DepositOperation != "" || old.TransferOperation != "" || old.SwapOperation != "" || old.WithdrawOperation != "") {
				return ErrDenied
			}
		} else {
			if w.Step < old.Step || w.Step > old.Step+1 || old.DepositOperation != "" && (w.DepositOperation != old.DepositOperation || w.SourceAccount != old.SourceAccount || w.SourceAsset != old.SourceAsset || w.DepositAmount != old.DepositAmount) {
				return ErrDenied
			}
			for _, refs := range [][2]string{{old.TransferOperation, w.TransferOperation}, {old.SwapOperation, w.SwapOperation}, {old.WithdrawOperation, w.WithdrawOperation}} {
				if refs[0] == "" || refs[0] == refs[1] {
					continue
				}
				if refs[1] != "" {
					return ErrDenied
				}
				found := false
				for _, o := range x.Operations {
					if o.ID == refs[0] && (o.State == "FAILED" || o.State == "CANCELLED") {
						found = true
					}
				}
				if !found {
					return ErrDenied
				}
			}
		}
	}
	if _, _, err := e.exchangeAsset(w.SourceAccount, w.SourceAsset); err != nil {
		return ErrDenied
	}
	if w.TradeAccount != "" {
		if _, _, err := e.exchangeAsset(w.TradeAccount, w.TradeAsset); err != nil {
			return ErrDenied
		}
	}
	if w.WithdrawAccount != "" {
		if _, _, err := e.exchangeAsset(w.WithdrawAccount, w.WithdrawAsset); err != nil {
			return ErrDenied
		}
	}
	if w.DepositOperation != "" {
		found := false
		for _, o := range e.data.Operations {
			if o.Plan.ID == w.DepositOperation && o.Plan.Account.ID == w.SourceAccount && o.Plan.Asset == w.SourceAsset {
				_, asset, lookupErr := e.exchangeAsset(w.SourceAccount, w.SourceAsset)
				atomic, parse := amount.Parse(w.DepositAmount, asset.Decimals)
				if lookupErr != nil || parse != nil || atomic != o.Plan.Amount {
					return ErrDenied
				}
				found = true
			}
		}
		if !found && w.Step >= 2 {
			return ErrDenied
		}
	}
	for _, entry := range []struct{ id, kind, account, asset string }{{w.TransferOperation, "transfer", w.SourceAccount, w.SourceAsset}, {w.SwapOperation, "swap", w.TradeAccount, w.TradeAsset}, {w.WithdrawOperation, "withdraw", w.WithdrawAccount, w.WithdrawAsset}} {
		if entry.id == "" {
			continue
		}
		found := false
		for _, o := range x.Operations {
			if o.ID == entry.id && o.Kind == entry.kind && o.AccountID == entry.account && o.AssetID == entry.asset && o.CredentialID == vault.ExchangeCredentialID(credential) {
				if entry.kind == "transfer" && w.Step >= 3 && o.State != "CONFIRMED" || entry.kind == "swap" && w.Step >= 4 && o.State != "CONFIRMED" || entry.kind == "withdraw" && w.Step == 5 && o.State != "CONFIRMED" {
					return ErrDenied
				}
				if entry.kind == "transfer" {
					var r bybit.TransferRequest
					if json.Unmarshal([]byte(o.Payload), &r) != nil || w.TransferAmount != r.Amount {
						return ErrDenied
					}
				}
				if entry.kind == "swap" {
					var r bybit.SpotOrder
					if json.Unmarshal([]byte(o.Payload), &r) != nil || w.TradeAmount != r.Qty || w.TradeSide != r.Side {
						return ErrDenied
					}
					base, quote, ok := bybit.SpotPair(r.Symbol)
					if !ok || w.QuoteCoin != quote {
						return ErrDenied
					}
					if w.OutputCoin != "" && (r.Side == "Buy" && w.OutputCoin != base || r.Side == "Sell" && w.OutputCoin != quote) {
						return ErrDenied
					}
				}
				if entry.kind == "withdraw" {
					var r bybit.WithdrawalRequest
					if json.Unmarshal([]byte(o.Payload), &r) != nil || w.WithdrawAmount != r.Amount {
						return ErrDenied
					}
				}
				found = true
			}
		}
		if !found {
			return ErrDenied
		}
	}
	copy := w
	x.Wizard = &copy
	x.WizardCredentialID = vault.ExchangeCredentialID(credential)
	return e.persist()
}

func (e *Engine) CancelExchangeWizardOperation(id string) error {
	x, err := e.exchangeData()
	if err != nil || x.Selected() != "bybit" || x.Wizard == nil || x.WizardCredentialID != vault.ExchangeCredentialID(x.Credential("bybit")) {
		return ErrDenied
	}
	w := *x.Wizard
	if id != "" && id == w.DepositOperation && w.Step == 1 {
		var operation *journal.Operation
		for i := range e.data.Operations {
			if e.data.Operations[i].Plan.ID == id {
				operation = &e.data.Operations[i]
			}
		}
		if operation != nil && (operation.Signed != nil || operation.State != "PREPARED" && operation.State != "CANCELLED") {
			return ErrDenied
		}
		data, generation := e.data, e.data.Generation
		accepted := e.UI.Confirm("Clear original unsigned Bybit deposit", "Original deposit: "+id+"\n\nThis deposit has no signed transaction. Clear this unsigned draft? No payment is sent; its local record is retained. A later deposit requires a new review and password confirmation.")
		if !accepted {
			return uiprompt.ErrCancelled
		}
		if !e.canAuthorize() || e.data != data || data.Generation != generation {
			return ErrDenied
		}
		if operation != nil {
			operation.State = "CANCELLED"
		}
		w.DepositOperation = ""
		x.Wizard = &w
		return e.persist()
	}
	if id == "" || id != w.TransferOperation && id != w.SwapOperation && id != w.WithdrawOperation {
		return ErrDenied
	}
	o, err := e.exchangeOperation(id)
	if err != nil || o.State != "PREPARED" && o.State != "AUTHORIZED" && o.State != "FAILED" && o.State != "CANCELLED" {
		return ErrDenied
	}
	a, asset, err := e.exchangeAsset(o.AccountID, o.AssetID)
	if err != nil {
		return err
	}
	summary, err := exchangeApproval(*o, a, asset)
	if err != nil {
		return err
	}
	data, generation := e.data, e.data.Generation
	accepted := e.UI.Confirm("Clear original Bybit operation", summary+"\n\nClear this unsigned or definitively failed/cancelled operation from the current step? Its original record is retained. No replacement is created and no network request is sent.")
	if !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	if !accepted {
		return uiprompt.ErrCancelled
	}
	if id == w.TransferOperation {
		w.TransferOperation = ""
	}
	if id == w.SwapOperation {
		w.SwapOperation = ""
	}
	if id == w.WithdrawOperation {
		w.WithdrawOperation = ""
	}
	if o.State == "PREPARED" || o.State == "AUTHORIZED" {
		o.State = "CANCELLED"
	}
	delete(e.exchangeAuthorizations, id)
	x.Wizard = &w
	return e.persist()
}

func exchangeWithinBudget(value, budget string) bool {
	v, e := amount.Parse(value, 36)
	b, f := amount.Parse(budget, 36)
	if e != nil || f != nil || v == "0" || b == "0" {
		return false
	}
	n, _ := amount.Atomic(v)
	cap, _ := amount.Atomic(b)
	return n.Cmp(cap) <= 0
}
