package signer

import (
	"bytes"
	"encoding/json"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestIndependentCustodyActionsRetainOriginalWizardAndRequireSeparateApprovals(t *testing.T) {
	e, ui, account := exchangeEngine(t)
	w := wizardSignerDraft(t, e, account)
	before, err := json.Marshal(e.data.Exchange.Wizard)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := e.PrepareExchange(ExchangeIntent{Kind: "transfer", AccountID: account.ID, AssetID: w.SourceAsset, Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.5", FromAccountType: "FUND", ToAccountType: "UNIFIED"}, Fee: "0", FeeCoin: "ETH"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ExchangeAuth(exchangeRequest(transfer)); err == nil {
		t.Fatal("an independent transfer was authorized by navigation or a pending deposit")
	}
	order := exchangeSwap(account)
	transfer = authorizeExchange(t, e, ui, transfer)
	if _, err = e.ExchangeAuth(exchangeRequest(transfer)); err != nil {
		t.Fatal(err)
	}
	if _, err = e.PrepareExchange(order); err == nil {
		t.Fatal("a submitted independent transfer was silently replaced by a new order")
	}
	if err = e.UpdateExchange(transfer.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = e.PrepareExchange(order); err == nil {
		t.Fatal("an ambiguous independent transfer allowed a replacement debit")
	}
	if err = e.UpdateExchange(transfer.ID, "CONFIRMED", "remote-"+transfer.ID); err != nil {
		t.Fatal(err)
	}
	trade, err := e.PrepareExchange(order)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ExchangeAuth(exchangeRequest(trade)); err == nil {
		t.Fatal("Funding-transfer approval authorized a separate spot order")
	}
	settleWizardExchange(t, e, ui, trade)
	after, _ := json.Marshal(e.data.Exchange.Wizard)
	if !bytes.Equal(before, after) || len(e.data.Operations) != 1 || e.data.Operations[0].Plan.ID != w.DepositOperation || e.data.Operations[0].Signed != nil || len(e.data.Exchange.Operations) != 2 || transfer.ID == trade.ID {
		t.Fatal("independent actions changed original metadata, signed/replaced its deposit, or collapsed identities")
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	restored, _ := json.Marshal(e.data.Exchange.Wizard)
	if !bytes.Equal(before, restored) || len(e.data.Exchange.Operations) != 2 || len(e.data.Operations) != 1 {
		t.Fatal("encrypted restart lost the original workflow or independent operation identities")
	}
}

type independentCancelUI struct {
	UI
	onConfirm func()
	approve   bool
}

func (u independentCancelUI) Confirm(_, _ string) bool {
	if u.onConfirm != nil {
		u.onConfirm()
	}
	return u.approve
}

func TestIndependentUnsignedCancellationRequiresExactConfirmationAndLiveState(t *testing.T) {
	for _, scenario := range []string{"decline", "prepared", "authorized", "submitting", "unknown", "foreign-key", "lock-during-confirm"} {
		t.Run(scenario, func(t *testing.T) {
			e, ui, account := exchangeEngine(t)
			op, err := e.PrepareExchange(exchangeSwap(account))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "authorized" || scenario == "submitting" || scenario == "unknown" {
				op = authorizeExchange(t, e, ui, op)
			}
			if scenario == "submitting" || scenario == "unknown" {
				if _, err = e.ExchangeAuth(exchangeRequest(op)); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unknown" {
				if err = e.UpdateExchange(op.ID, "UNKNOWN", ""); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "foreign-key" {
				e.data.Exchange.Operations[0].CredentialID = "not-current"
			}
			calls := 0
			e.UI = independentCancelUI{UI: ui, approve: scenario != "decline", onConfirm: func() {
				calls++
				if scenario == "lock-during-confirm" {
					e.Lock()
				}
			}}
			err = e.CancelExchangeConfirmed(op.ID)
			accepted := scenario == "prepared" || scenario == "authorized"
			if (err == nil) != accepted {
				t.Fatal("unsigned cancellation ignored consent, credentials or original state", err)
			}
			if scenario == "submitting" || scenario == "unknown" || scenario == "foreign-key" {
				if calls != 0 {
					t.Fatal("ineligible cancellation displayed a misleading approval")
				}
			}
			if scenario == "lock-during-confirm" {
				ui.inputs = append(ui.inputs, input(syntheticPassword))
				e.UI = ui
				if err = e.Unlock(); err != nil {
					t.Fatal(err)
				}
			}
			stored, err := e.ExchangeOperation(op.ID)
			// Fresh unlock independently cancels unsigned stale requests under
			// the pre-existing recovery policy; the stale confirmation above
			// still had to fail while the original signer was locked.
			cancelled := accepted || scenario == "lock-during-confirm"
			if err != nil || cancelled && stored.State != "CANCELLED" || !cancelled && stored.State == "CANCELLED" || stored.Payload != op.Payload || stored.Digest != op.Digest {
				t.Fatal("cancel rewrote immutable economic fields or cancelled a stale request", stored, err)
			}
			if _, err = e.ExchangeAuth(exchangeRequest(op)); accepted && err == nil {
				t.Fatal("cancelled unsigned operation retained POST signing authority")
			}
		})
	}
}
