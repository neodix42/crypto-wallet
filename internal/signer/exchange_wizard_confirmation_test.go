package signer

import (
	"bytes"
	"encoding/json"
	"testing"

	"localwallet/internal/model"
)

func TestExchangeWizardDepositConfirmationRequiresOriginalSignedConfirmedReceipt(t *testing.T) {
	e, u, a := exchangeEngine(t)
	w := wizardSignerDraft(t, e, a)
	w.Step = 1
	e.data.Exchange.Wizard = &w // The original draft before a background check.
	u.inputs = append(u.inputs, input(syntheticPassword))
	signed, err := e.Sign(w.DepositOperation)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Update(w.DepositOperation, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	if err = e.Update(w.DepositOperation, "CONFIRMED", &model.Receipt{Identity: signed.Identity, State: "CONFIRMED"}); err != nil {
		t.Fatal(err)
	}
	confirmed := e.data.Operations[0]
	for _, scenario := range []string{"wrong-wizard", "wrong-operation", "unsigned", "unconfirmed", "missing-receipt", "wrong-receipt-state", "wrong-receipt-identity", "wrong-account", "wrong-asset", "wrong-amount"} {
		t.Run(scenario, func(t *testing.T) {
			candidate := confirmed
			r := *confirmed.Receipt
			candidate.Receipt = &r
			id, operation := w.ID, w.DepositOperation
			switch scenario {
			case "wrong-wizard":
				id = "another-workflow"
			case "wrong-operation":
				operation = "another-deposit"
			case "unsigned":
				candidate.Signed = nil
			case "unconfirmed":
				candidate.State = "SUBMITTED"
			case "missing-receipt":
				candidate.Receipt = nil
			case "wrong-receipt-state":
				candidate.Receipt.State = "SUBMITTED"
			case "wrong-receipt-identity":
				candidate.Receipt.Identity = "another-payment"
			case "wrong-account":
				candidate.Plan.Account.ID = "foreign-account"
			case "wrong-asset":
				candidate.Plan.Asset = "ethereum-mainnet:usdt"
			case "wrong-amount":
				candidate.Plan.Amount = "1"
			}
			e.data.Operations[0] = candidate
			if e.ConfirmExchangeWizardDeposit(id, operation) == nil || e.data.Exchange.Wizard.Step != 1 || len(e.data.Exchange.Operations) != 0 {
				t.Fatal("navigation accepted unrelated or unconfirmed credit, or granted exchange authority")
			}
		})
	}
	e.data.Operations[0] = confirmed
	confirmations := len(u.summaries)
	if err = e.ConfirmExchangeWizardDeposit(w.ID, w.DepositOperation); err != nil || e.data.Exchange.Wizard.Step != 2 {
		t.Fatal("verified original did not advance navigation", err)
	}
	if len(u.summaries) != confirmations || len(u.inputs) != 0 || len(e.data.Operations) != 1 || len(e.data.Exchange.Operations) != 0 {
		t.Fatal("read-only navigation prompted, signed or created a funds-moving operation")
	}
	// A foreground edit after the first poll survives every delayed duplicate
	// observation. Only the signer's current step may be changed, once.
	draft := *e.data.Exchange.Wizard
	draft.TransferAmount = "0.75"
	if err = e.SaveExchangeWizard(draft); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(e.data.Exchange.Wizard)
	if err = e.ConfirmExchangeWizardDeposit(w.ID, w.DepositOperation); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(e.data.Exchange.Wizard)
	if !bytes.Equal(before, after) {
		t.Fatal("delayed confirmation overwrote the user's current draft")
	}
	e.Lock()
	if e.ConfirmExchangeWizardDeposit(w.ID, w.DepositOperation) == nil {
		t.Fatal("locked confirmation changed encrypted navigation")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	if e.data.Exchange.Wizard.Step != 2 || e.data.Exchange.Wizard.TransferAmount != "0.75" || len(e.data.Operations) != 1 || e.data.Operations[0].State != "CONFIRMED" || e.data.Operations[0].Signed.Identity != signed.Identity {
		t.Fatal("encrypted restart lost navigation or original signed operation")
	}
}
