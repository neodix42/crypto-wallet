package signer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func wizardSignerDraft(t *testing.T, e *Engine, a model.Account) exchange.WizardState {
	t.Helper()
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: a.ID, SourceAsset: a.Network + ":native", DepositAmount: "1"}
	if err := e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	p := evmPlan(a)
	p.Amount = "1000000000000000000"
	p.Payload = bytes.ReplaceAll(p.Payload, []byte(`"chainId":"11155111"`), []byte(`"chainId":"1"`))
	p.Payload = bytes.ReplaceAll(p.Payload, []byte(`"value":"123456789"`), []byte(`"value":"1000000000000000000"`))
	p.ID = ""
	p.ID = "evm:" + journal.Digest(p)
	w.DepositOperation = p.ID
	if err := e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	w.Step = 2
	if err := e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	return w
}

func settleWizardExchange(t *testing.T, e *Engine, u *testUI, o vault.ExchangeOperation) {
	t.Helper()
	o = authorizeExchange(t, e, u, o)
	if _, err := e.ExchangeAuth(exchangeRequest(o)); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateExchange(o.ID, "CONFIRMED", "remote-"+o.ID); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeWizardPersistsPhasesWithoutGrantingPaymentAuthority(t *testing.T) {
	e, u, a := exchangeEngine(t)
	w := wizardSignerDraft(t, e, a)
	transfer := ExchangeIntent{WizardID: w.ID, Kind: "transfer", AccountID: a.ID, AssetID: w.SourceAsset, Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}}
	o, err := e.PrepareExchange(transfer)
	if err != nil {
		t.Fatal(err)
	}
	status, _ := e.ExchangeStatus()
	if status.Wizard.TransferOperation != o.ID || status.Wizard.TransferAmount != "0.1" {
		t.Fatal("operation was not atomically associated before approval")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err == nil {
		t.Fatal("navigation granted POST authority")
	}
	w = *status.Wizard
	w.Step = 3
	if e.SaveExchangeWizard(w) == nil {
		t.Fatal("unsettled transfer skipped")
	}
	settleWizardExchange(t, e, u, o)
	if err = e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	swap := exchangeSwap(a)
	swap.WizardID = w.ID
	w.TradeAccount, w.TradeAsset, w.TradeSide, w.TradeAmount, w.QuoteCoin = a.ID, w.SourceAsset, "Sell", swap.Order.Qty, "USDT"
	if err = e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	o, err = e.PrepareExchange(swap)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = e.ExchangeStatus()
	w = *status.Wizard
	if w.SwapOperation != o.ID {
		t.Fatal("order association lost")
	}
	// A lost reply/restart retains the original ID but never replays its POST.
	settleWizardExchange(t, e, u, o)
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	status, err = e.ExchangeStatus()
	if err != nil || status.Wizard == nil || status.Wizard.SwapOperation != o.ID || status.Wizard.Step != 3 {
		t.Fatal("encrypted workflow did not resume", err)
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err == nil {
		t.Fatal("restart replayed order")
	}
	w = *status.Wizard
	w.Step = 4
	w.OutputCoin = "USDT"
	w.OutputAmount = "199.8"
	if err = e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	var usdt model.Asset
	adapter, _ := e.adapter(a.Network)
	for _, n := range adapter.Networks() {
		if n.ID == a.Network {
			for _, asset := range n.Assets {
				if asset.Symbol == "USDT" {
					usdt = asset
				}
			}
		}
	}
	w.WithdrawAccount, w.WithdrawAsset, w.WithdrawAmount = a.ID, usdt.ID, "199.8"
	if err = e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	withdraw := ExchangeIntent{WizardID: w.ID, Kind: "withdraw", AccountID: a.ID, AssetID: usdt.ID, ChainType: "Ethereum(ERC20)", ContractAddress: usdt.Contract, Fee: "1", FeeCoin: "USDT", Withdrawal: &bybit.WithdrawalRequest{Coin: "USDT", Chain: "ETH", Address: a.Address, Amount: w.WithdrawAmount, ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	o, err = e.PrepareExchange(withdraw)
	if err != nil {
		t.Fatal(err)
	}
	settleWizardExchange(t, e, u, o)
	status, _ = e.ExchangeStatus()
	w = *status.Wizard
	w.Step = 5
	if err = e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	raw, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{w.ID, exchangeTestKey, exchangeTestSecret, syntheticPhrase} {
		if bytes.Contains(raw, []byte(canary)) {
			t.Fatal("plaintext workflow/credential escaped vault")
		}
	}
	if len(e.data.Exchange.Operations) != 3 {
		t.Fatal("workflow created implicit economic operations")
	}
}

func TestExchangeWizardRejectsForeignStateAndRetainsUnknownOriginal(t *testing.T) {
	e, u, a := exchangeEngine(t)
	w := wizardSignerDraft(t, e, a)
	for _, mutate := range []func(*exchange.WizardState){
		func(w *exchange.WizardState) { w.Step = 4 }, func(w *exchange.WizardState) { w.SourceAccount = "foreign" }, func(w *exchange.WizardState) { w.DepositAmount = "2" },
		func(w *exchange.WizardState) { w.ID = strings.Repeat("b", 32) }, func(w *exchange.WizardState) { w.DepositOperation = "" }, func(w *exchange.WizardState) { w.Step = 1 },
	} {
		bad := w
		mutate(&bad)
		if e.SaveExchangeWizard(bad) == nil {
			t.Fatal("mutated navigation accepted", bad)
		}
	}
	in := ExchangeIntent{WizardID: w.ID, Kind: "transfer", AccountID: a.ID, AssetID: w.SourceAsset, Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}}
	for _, mutate := range []func(*ExchangeIntent){
		func(i *ExchangeIntent) { i.WizardID = "foreign" }, func(i *ExchangeIntent) { i.Transfer.Amount = "1.1" }, func(i *ExchangeIntent) { i.Transfer.Coin = "USDT" }, func(i *ExchangeIntent) { i.Transfer.FromAccountType = "UNIFIED"; i.Transfer.ToAccountType = "FUND" },
	} {
		bad := in
		copy := *in.Transfer
		bad.Transfer = &copy
		mutate(&bad)
		if _, err := e.PrepareExchange(bad); err == nil {
			t.Fatal("wizard transfer exceeded exact source/budget")
		}
	}
	o, err := e.PrepareExchange(in)
	if err != nil {
		t.Fatal(err)
	}
	u.approve = false
	if e.CancelExchangeWizardOperation(o.ID) == nil {
		t.Fatal("declined clear accepted")
	}
	u.approve = true
	if err = e.CancelExchangeWizardOperation(o.ID); err != nil {
		t.Fatal(err)
	}
	status, _ := e.ExchangeStatus()
	if status.Wizard.TransferOperation != "" || status.Operations[0].State != "CANCELLED" {
		t.Fatal("unsigned cancellation lost original record")
	}
	o, err = e.PrepareExchange(in)
	if err != nil {
		t.Fatal(err)
	}
	o = authorizeExchange(t, e, u, o)
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(e.data.Exchange)
	if e.CancelExchangeWizardOperation(o.ID) == nil {
		t.Fatal("unknown original cleared")
	}
	if _, err = e.PrepareExchange(in); err == nil {
		t.Fatal("unknown original replaced")
	}
	after, _ := json.Marshal(e.data.Exchange)
	if !bytes.Equal(before, after) {
		t.Fatal("unknown operation or wizard changed")
	}
}

func TestExchangeWizardCancelledAndMissingDepositDraftCanBeClearedOnlyExplicitly(t *testing.T) {
	e, u, a := exchangeEngine(t)
	w := exchange.WizardState{ID: strings.Repeat("b", 32), Step: 1, SourceAccount: a.ID, SourceAsset: a.Network + ":native", DepositAmount: "1", DepositOperation: "evm:planned-before-prepare"}
	if err := e.SaveExchangeWizard(w); err != nil {
		t.Fatal(err)
	}
	status, _ := e.ExchangeStatus()
	if status.WizardDepositState != "MISSING" {
		t.Fatal("missing draft not distinguished")
	}
	u.approve = false
	if e.CancelExchangeWizardOperation(w.DepositOperation) == nil {
		t.Fatal("declined missing-draft reset")
	}
	u.approve = true
	if err := e.CancelExchangeWizardOperation(w.DepositOperation); err != nil {
		t.Fatal(err)
	}
	status, _ = e.ExchangeStatus()
	if status.Wizard.DepositOperation != "" || len(e.data.Operations) != 0 {
		t.Fatal("draft reset invented transaction")
	}
	w = wizardSignerDraft(t, e, a)
	w.Step = 1
	e.data.Exchange.Wizard = &w // exercise unsigned original on its initial screen
	if err := e.CancelExchangeWizardOperation(w.DepositOperation); err != nil {
		t.Fatal(err)
	}
	if e.data.Operations[0].State != "CANCELLED" || e.data.Operations[0].Signed != nil || e.data.Exchange.Wizard.DepositOperation != "" {
		t.Fatal("unsigned original was not retained")
	}
	e.Lock()
	if e.SaveExchangeWizard(w) == nil || e.CancelExchangeWizardOperation(w.DepositOperation) == nil {
		t.Fatal("locked navigation mutated")
	}
}
