package signer

import (
	"reflect"
	"testing"
)

func TestBybitDefinitiveWithdrawalRefusalsCannotResolveOrdersTransfersOrEarlierUnknown(t *testing.T) {
	for _, code := range []int64{131093, 131098, 131089} {
		e, ui, account := currentFundsGRAMEngine(t)
		for _, kind := range []string{"transfer", "swap"} {
			op, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, kind))
			if err != nil {
				t.Fatal(err)
			}
			op = authorizeExchange(t, e, ui, op)
			if _, err := e.ExchangeAuth(exchangeRequest(op)); err != nil {
				t.Fatal(err)
			}
			before, _ := e.ExchangeOperation(op.ID)
			if e.RejectExchange(op.ID, code) == nil {
				t.Fatal("withdrawal-only refusal released an unrelated operation", code, kind)
			}
			after, _ := e.ExchangeOperation(op.ID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected scope check changed original operation")
			}
			if err := e.UpdateExchange(op.ID, "UNKNOWN", ""); err != nil {
				t.Fatal(err)
			}
		}
		op, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "withdraw"))
		if err != nil {
			t.Fatal(err)
		}
		if e.RejectExchange(op.ID, code) == nil {
			t.Fatal("unsubmitted withdrawal was marked remotely rejected")
		}
		op = authorizeExchange(t, e, ui, op)
		if _, err := e.ExchangeAuth(exchangeRequest(op)); err != nil {
			t.Fatal(err)
		}
		for _, unknown := range []int64{131085, 131090, 131094, 131097, 131099, 131214, 10000, 10002, 10006, 10014, 429, 403} {
			if e.RejectExchange(op.ID, unknown) == nil {
				t.Fatal("ambiguous or unsupported response released withdrawal", unknown)
			}
		}
		if err := e.RejectExchange(op.ID, code); err != nil {
			t.Fatal("documented first-POST withdrawal refusal was not recorded", code, err)
		}
		stored, err := e.ExchangeOperation(op.ID)
		if err != nil || stored.State != "FAILED" || stored.RejectionCode != code || stored.ExternalID != "" || stored.Digest != op.Digest {
			t.Fatal("definitive refusal lost its original immutable identity", code)
		}
		if e.RejectExchange(op.ID, code) == nil {
			t.Fatal("terminal withdrawal refusal replayed")
		}
		pending, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "withdraw"))
		if err != nil {
			t.Fatal(err)
		}
		pending = authorizeExchange(t, e, ui, pending)
		if _, err := e.ExchangeAuth(exchangeRequest(pending)); err != nil {
			t.Fatal(err)
		}
		if err := e.UpdateExchange(pending.ID, "UNKNOWN", ""); err != nil {
			t.Fatal(err)
		}
		if e.RejectExchange(pending.ID, code) == nil {
			t.Fatal("new refusal falsely resolved an earlier lost acknowledgement")
		}
	}
}
