package nativewallet

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

func wizardNativeFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account) {
	t.Helper()
	a, ui, backend := fixture(t)
	account := ethAccount(t, a)
	a.fields["exchangeAccount"], a.fields["exchangeAsset"], a.fields["exchangeAmount"] = account.ID, account.Network+":native", "0.1"
	a.exchange = json.RawMessage(`{"provider":"bybit","connected":true,"operations":[]}`)
	return a, ui, backend, account
}
func TestExchangeWizardRefreshShowsBoundAddressMemoAndAvailableWalletBalance(t *testing.T) {
	a, ui, backend, account := wizardNativeFixture(t)
	preview := wizardDepositPreview{Provider: "bybit", AccountID: account.ID, AssetID: account.Network + ":native", Network: account.Network, Coin: "ETH", Address: "0x2222222222222222222222222222222222222222", Chain: "ETH", MinDeposit: "0.001", ObservedAt: time.Now().UTC().Format(time.RFC3339), Unified: true, CanTransfer: true, CanTrade: true, CanWithdraw: true}
	var saved *exchange.WizardState
	prepared := 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeDepositPreview":
			return answer(out, preview)
		case "balances":
			return answer(out, []model.Balance{{Asset: preview.AssetID, Atomic: "500000000000000000", State: "provider-observed", At: "public-fixture-block"}})
		case "exchangeWizardLoad":
			return answer(out, map[string]any{"wizard": saved, "operations": []any{}})
		case "exchangeWizardSave":
			w := *(body.(*exchange.WizardState))
			saved = &w
			return answer(out, map[string]any{"wizard": saved})
		case "exchangeDeposit":
			request := body.(map[string]string)
			if request["expectedAddress"] != preview.Address || request["expectedMemo"] != preview.Memo || request["accountId"] != account.ID || request["amount"] != "0.1" || request["wizardId"] != saved.ID {
				t.Error("displayed address not automatically bound to deposit", request)
			}
			prepared++
			saved.DepositOperation = "original-wallet-payment"
			return answer(out, wizardResult{ID: saved.DepositOperation, Kind: "deposit", State: "SUBMITTED"})
		default:
			t.Error("unexpected wizard method", method)
			return ipc.NewRequestError("INVALID_ARGUMENT")
		}
	}
	if n, _ := findNode(a.exchangePage(), "wizardDeposit"); !n.Disabled {
		t.Fatal("deposit enabled before address refresh")
	}
	a.wizardAction("wizardRefreshDeposit")
	ui.apply(t)
	page := a.exchangePage()
	text := viewText(page)
	if !strings.Contains(text, preview.Address) || !strings.Contains(text, "Wallet available: 0.5 ETH") || !strings.Contains(text, "no deposit memo") || !a.wizardPreviewCurrent() {
		t.Fatal("public verified destination/balance missing", text)
	}
	copy, ok := findNode(page, "copyReceive/exchange-deposit")
	if !ok || copy.Value != preview.Address {
		t.Fatal("copy not bound to shown address")
	}
	if n, _ := findNode(page, "wizardDeposit"); n.Disabled {
		t.Fatal("valid verified deposit stayed disabled")
	}
	a.wizardAction("wizardDeposit")
	ui.apply(t)
	if prepared != 1 || a.wizard.state.DepositOperation == "" || a.wizard.state.Step != 1 {
		t.Fatal("submitted deposit advanced without exchange credit")
	}
	if _, ok := findNode(a.exchangePage(), "wizardMove"); ok {
		t.Fatal("future step economic control exposed")
	}
}

func TestExchangeWizardViewsGateEveryPhaseAndShowOwnedWithdrawalRemainder(t *testing.T) {
	a, _, _, account := wizardNativeFixture(t)
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: account.ID, SourceAsset: account.Network + ":native", DepositAmount: "1"}
	for step := 1; step <= 5; step++ {
		w.Step = step
		if step >= 2 {
			w.DepositOperation = "deposit"
		}
		if step >= 3 {
			w.TransferOperation = "transfer"
			w.TransferAmount = "1"
		}
		if step >= 4 {
			w.SwapOperation = "swap"
			w.OutputCoin = "USDT"
			w.OutputAmount = "100.123456789"
		}
		if step == 5 {
			w.WithdrawOperation = "withdraw"
		}
		a.adoptWizardState(&w)
		page := a.exchangePage()
		stepper, ok := findNode(page, "exchangeSteps")
		if !ok || len(stepper.Children) != 4 {
			t.Fatal("four-step navigation lost")
		}
		for action, phase := range map[string]int{"wizardDeposit": 1, "wizardMove": 2, "wizardSwap": 3, "wizardWithdraw": 4} {
			if _, exists := findNode(page, action); exists && phase != step {
				t.Fatal("wrong stage action visible", step, action)
			}
		}
	}
	w.Step = 4
	w.WithdrawOperation = ""
	a.adoptWizardState(&w)
	a.wizard.destinations = []wizardDestination{{AccountID: account.ID, AssetID: "ethereum-mainnet:usdt", Address: account.Address, Coin: "USDT", NetworkName: "Ethereum", Precision: "6", Minimum: "1", Fee: "1", PercentageFee: "0"}}
	a.fields["wizardDestination"] = account.ID + "|ethereum-mainnet:usdt"
	a.wizardSetWithdrawalDefault()
	if a.fields["wizardWithdrawAmount"] != "100.123456" {
		t.Fatal("precision default wrong")
	}
	text := viewText(a.exchangePage())
	if !strings.Contains(text, account.Address) || !strings.Contains(text, "0.000000789 USDT") || strings.Contains(text, `"Text":"Recipient address"`) {
		t.Fatal("owned address or explicit remainder missing", text)
	}
	a.fields["wizardWithdrawAmount"] = "101"
	if n, _ := findNode(a.exchangePage(), "wizardWithdraw"); !n.Disabled {
		t.Fatal("withdrawal exceeded actual proceeds")
	}
}

func TestExchangeWizardStalePreviewChangedSourceAndSubmittedOperationsRemainBlocked(t *testing.T) {
	a, _, _, account := wizardNativeFixture(t)
	p := &wizardDepositPreview{Provider: "bybit", AccountID: account.ID, AssetID: account.Network + ":native", Address: "public", ObservedAt: time.Now().Add(-3 * time.Minute).Format(time.RFC3339)}
	a.exchangeWizardData().preview = p
	if a.wizardPreviewCurrent() {
		t.Fatal("expired deposit preview accepted")
	}
	p.ObservedAt = time.Now().Format(time.RFC3339)
	p.AssetID = "foreign"
	if a.wizardPreviewCurrent() {
		t.Fatal("foreign asset preview accepted")
	}
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 2, SourceAccount: account.ID, SourceAsset: account.Network + ":native", DepositAmount: "1", DepositOperation: "deposit", TransferOperation: "original-transfer", TransferAmount: "1"}
	a.adoptWizardState(&w)
	for _, state := range []string{"SUBMITTING", "PENDING", "UNKNOWN", "CONFIRMED"} {
		a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "operations": []map[string]string{{"id": "original-transfer", "state": state}}})
		if _, ok := findNode(a.exchangePage(), "wizardCancelUnsigned"); ok {
			t.Fatal("submitted/confirmed operation offered reset", state)
		}
	}
	for _, state := range []string{"PREPARED", "AUTHORIZED", "FAILED", "CANCELLED"} {
		a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "operations": []map[string]string{{"id": "original-transfer", "state": state}}})
		if _, ok := findNode(a.exchangePage(), "wizardCancelUnsigned"); !ok {
			t.Fatal("safe explicit reset unavailable", state)
		}
	}
}

func TestExchangeWizardNetReceiptsUseExactArithmeticAndCannotSpendPastTransfer(t *testing.T) {
	for _, side := range []string{"Buy", "Sell"} {
		result := wizardResult{FilledBase: "2.000000000000000001", FilledQuote: "2.000000000000000001", Fees: map[string]string{"USDT": "0.000000000000000001"}}
		value, err := wizardNetOutput(result, wizardPair{Side: side, OutputCoin: "USDT"})
		if err != nil || value != "2" {
			t.Fatal("net output lost precision", value, err)
		}
		result.Fees["USDT"] = "3"
		if _, err = wizardNetOutput(result, wizardPair{Side: side, OutputCoin: "USDT"}); err == nil {
			t.Fatal("fee exceeded receipts")
		}
	}
	a, _, _, account := wizardNativeFixture(t)
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 3, SourceAccount: account.ID, SourceAsset: account.Network + ":native", DepositAmount: "1", DepositOperation: "p", TransferOperation: "t", TransferAmount: "1"}
	a.adoptWizardState(&w)
	p := wizardPair{ID: "0", AccountID: account.ID, AssetID: w.SourceAsset, Side: "Sell", QuoteCoin: "USDT", OutputCoin: "USDT", Symbol: "ETHUSDT"}
	a.wizard.pairs = []wizardPair{p}
	a.fields["wizardPair"], a.fields["wizardTradeAmount"] = "0", "1.1"
	q := exchangeValidationQuote(a, account, a.asset(account), "Sell", "USDT", time.Now())
	setExchangeValidationQuote(a, q)
	if a.wizardTradeValidation().SwapValid {
		t.Fatal("swap spent existing unrelated holdings beyond workflow budget")
	}
}

func TestExchangeCoinMetadataReasonsNameOnlyReviewedFields(t *testing.T) {
	for _, field := range []string{"IDENTITY", "DUPLICATE_CHAIN", "FLAGS", "DEPOSIT_LIMIT", "WITHDRAWAL_LIMIT", "PRECISION", "CONFIRMATIONS"} {
		reason, ok := exchangeReadReason("EXCHANGE_READ_COIN__METADATA_" + field)
		if !ok || !strings.Contains(reason, "metadata has missing or invalid") || strings.Contains(reason, "PRIVATE_CANARY") {
			t.Fatal("field reason missing or unsafe", field, reason)
		}
	}
	if _, ok := exchangeReadReason("EXCHANGE_READ_COIN__METADATA_PRIVATE_CANARY"); ok {
		t.Fatal("unreviewed field became public reason")
	}
}

func TestExchangeWizardConnectionChangeClearsPreviousDepositPreview(t *testing.T) {
	for _, method := range []string{"exchangeConnect", "exchangeDisconnect"} {
		a, ui, backend, _ := wizardNativeFixture(t)
		a.exchangeWizardData().preview = &wizardDepositPreview{Address: "old-exchange-account-address"}
		backend.handler = func(_ uint64, got string, body, out any) error {
			if got != method {
				t.Error("unexpected request", got)
			}
			return answer(out, map[string]any{"provider": "bybit", "connected": method == "exchangeConnect", "operations": []any{}})
		}
		a.exchangeAction(method, struct{}{})
		ui.apply(t)
		if a.wizard != nil && a.wizard.preview != nil {
			t.Fatal("old account's deposit destination survived connection change", method)
		}
	}
}

func TestExchangeWizardHiddenOriginalSourceRemainsVisibleAsLockedSelection(t *testing.T) {
	a, _, _, account := wizardNativeFixture(t)
	for i := range a.state.Accounts {
		if a.state.Accounts[i].ID == account.ID {
			a.state.Accounts[i].Hidden = true
		}
	}
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: account.ID, SourceAsset: account.Network + ":native", DepositAmount: "1", DepositOperation: "original"}
	a.adoptWizardState(&w)
	selector, ok := findNode(a.exchangePage(), "field/exchangeAccount")
	found := false
	for _, option := range selector.Options {
		if option.ID == account.ID {
			found = true
		}
	}
	if !ok || !selector.Disabled || selector.Value != account.ID || !found {
		t.Fatal("hidden original source visually changed to another account")
	}
}
