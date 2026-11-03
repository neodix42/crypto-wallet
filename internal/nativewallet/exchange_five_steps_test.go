package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"localwallet/internal/webui"
)

func TestFiveStepExchangeLegacyProgressMapsWithoutChangingDurableState(t *testing.T) {
	for _, test := range []struct{ saved, visible int }{{1, 1}, {2, 2}, {3, 3}, {4, 5}, {5, 1}} {
		a, _, _, _, saved := freeNativeFixture(t)
		a.wizard = nil
		saved.Step = test.saved
		saved.TransferOperation, saved.TransferAmount = "old-transfer", "6"
		saved.SwapOperation, saved.OutputCoin, saved.OutputAmount = "old-swap", "USDT", "5"
		saved.WithdrawOperation = "old-withdraw"
		raw, _ := json.Marshal(map[string]any{"provider": "bybit", "wizard": saved})
		a.adoptExchangeWizard(raw)
		if a.wizard.viewStep != test.visible || a.wizard.state.Step != test.saved || a.wizard.state.DepositOperation != saved.DepositOperation {
			t.Fatal("visible five-step mapping changed legacy chronology", test, a.wizard)
		}
	}
}

func TestFiveStepExchangeReturnUsesUnifiedOnlyAndSeparateOriginalIdentity(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/4"})
	u := a.freeData()
	u.balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "100"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "2.75"}}
	u.outputCoin, u.outputAmount, u.outputAccount = "ETH", "2.5", source.ID
	if button, _ := findNode(a.exchangePage(), "freeReturn"); button.Disabled || a.fields["freeReturnAmount"] != "2.5" {
		t.Fatal("verified net output did not default to an affordable Unified return")
	}
	a.fields["freeReturnAmount"] = "3"
	if button, _ := findNode(a.exchangePage(), "freeReturn"); !button.Disabled {
		t.Fatal("Funding balance incorrectly covered Unified insufficiency")
	}
	a.fields["freeReturnAmount"] = "2.5"
	moves, reads := 0, 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeUnifiedMove":
			fields := freeRequestFields(t, body)
			if fields["accountId"] != source.ID || fields["assetId"] != saved.SourceAsset || fields["amount"] != "2.5" || fields["wizardId"] != nil || fields["fromAccountType"] != nil || fields["toAccountType"] != nil {
				t.Error("reverse transfer authority changed", fields)
			}
			moves++
			return answer(out, wizardResult{ID: "separate-original-return", Kind: "transfer", State: "CONFIRMED"})
		case "exchangeFundingBalances":
			reads++
			return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "102.5"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0.25"}}})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("reverse transfer requested unexpected authority", method)
			return errors.New("unexpected reverse method")
		}
	}
	a.wizardAction("freeReturn")
	ui.apply(t)
	if moves != 1 || reads != 1 || len(u.operations) != 1 || u.operations[0].Stage != 4 || u.operations[0].ID != "separate-original-return" || a.fields["freeReturnAmount"] != "0.25" || a.wizard.state.DepositOperation != saved.DepositOperation || a.wizard.state.Step != saved.Step {
		t.Fatal("reverse transfer lost original identity, fresh pools or saved chronology", moves, reads, u.operations, a.fields)
	}
}

func TestFiveStepExchangeWithdrawalNeverCountsUnifiedOrRequiresItsAvailability(t *testing.T) {
	for _, unified := range []string{"", "0", "1000000"} {
		a, _, _, source, saved := freeNativeFixture(t)
		a.event(webui.Event{ID: "wizardStep/5"})
		a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "1.25"}, {AccountType: "UNIFIED", Coin: "ETH", Available: unified}}
		d := wizardDestination{AccountID: source.ID, AssetID: saved.SourceAsset, Coin: "ETH", Address: source.Address, Precision: "8", Minimum: "0.001", Fee: "0.0001", PercentageFee: "0"}
		a.freeData().destinations = []wizardDestination{d}
		a.fields["freeDestination"] = d.AccountID + "|" + d.AssetID
		a.freeWithdrawDefault()
		if a.freeWithdrawalAvailable("ETH") != "1.25" || a.fields["freeWithdrawAmount"] != "1.25" {
			t.Fatal("withdrawal used Unified funds or blocked known Funding", unified)
		}
		if button, _ := findNode(a.exchangePage(), "freeWithdraw"); button.Disabled {
			t.Fatal("Funded withdrawal unavailable", unified)
		}
		a.fields["freeWithdrawAmount"] = "1.26"
		if button, _ := findNode(a.exchangePage(), "freeWithdraw"); !button.Disabled {
			t.Fatal("withdrawal implicitly used Unified", unified)
		}
		if strings.Contains(viewText(a.exchangePage()), "Available Funding + Unified") {
			t.Fatal("combined withdrawal source still advertised")
		}
	}
}

func TestFiveStepExchangeSpotReadsDeclareIndependentSpotPolicy(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/3"})
	a.fields["freeTradeAmount"] = "NaN" // A draft cannot block read-only pair discovery.
	pair := wizardPair{ID: "ETHUSDT-Buy", AccountID: source.ID, AssetID: saved.SourceAsset, Side: "Buy", QuoteCoin: "USDT", OutputCoin: "ETH", Symbol: "ETHUSDT"}
	calls := []string{}
	backend.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "exchangeSwapOptions":
			if freeRequestFields(t, body)["spotOnly"] != true {
				t.Error("spot discovery still requires transfer routes")
			}
			return answer(out, []wizardPair{pair})
		case "exchangeQuote":
			f := freeRequestFields(t, body)
			if f["unifiedOnly"] != true || f["side"] != "Buy" || f["quoteCoin"] != "USDT" || f["accountId"] != source.ID || f["assetId"] != saved.SourceAsset {
				t.Error("quote lost selected Buy side/current Unified policy", f)
			}
			return answer(out, map[string]any{"symbol": "ETHUSDT"})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("spot reads requested economic authority", method)
			return errors.New("unexpected spot read")
		}
	}
	a.wizardAction("freeRefreshPairs")
	ui.apply(t)
	a.wizardAction("freeRefreshMarket")
	ui.apply(t)
	if strings.Join(calls, ",") != "exchangeSwapOptions,exchangeWizardLoad,exchangeQuote,exchangeWizardLoad" || len(a.freeData().operations) != 0 {
		t.Fatal("read-only spot refresh created an operation", calls)
	}
}

func TestFiveStepExchangePendingReturnReconcilesOriginalAndRefreshesFunding(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/4"})
	u := a.freeData()
	u.operations = []freeExchangeOperation{{Stage: 4, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "pending-return", Kind: "transfer", State: "PENDING"}}}
	a.fields["freeReturnAmount"], a.fields["freeWithdrawAmount"] = "5", "1"
	calls := []string{}
	backend.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "exchangeReconcile":
			if freeRequestFields(t, body)["id"] != "pending-return" {
				t.Error("return reconcile replaced identity")
			}
			return answer(out, wizardResult{ID: "pending-return", Kind: "transfer", State: "CONFIRMED"})
		case "exchangeFundingBalances":
			return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0"}}})
		default:
			t.Error("return tracker issued an economic request", method)
			return errors.New("unexpected tracker method")
		}
	}
	if !a.nextFreeExchangeCheck() {
		t.Fatal("pending return was not tracked")
	}
	ui.apply(t)
	if strings.Join(calls, ",") != "exchangeReconcile,exchangeFundingBalances" || u.operations[0].State != "CONFIRMED" || a.fields["freeReturnAmount"] != "0" || a.fields["freeWithdrawAmount"] != "" || a.freeWithdrawalAvailable("ETH") != "5" {
		t.Fatal("return completion failed to refresh source pools", calls, a.fields)
	}
}

func TestFiveStepExchangeHasNoLegacyBybitTradingBypass(t *testing.T) {
	a, ui, backend, _, _ := freeNativeFixture(t)
	backend.handler = func(_ uint64, method string, _, _ any) error {
		t.Error("stale legacy Bybit event reached backend", method)
		return errors.New("unexpected legacy event")
	}
	a.fields["exchangeMode"] = "existing"
	a.event(webui.Event{ID: "wizardAdvanced"})
	page := a.exchangePage()
	if a.wizard.viewStep != 3 {
		t.Fatal("old navigation did not select the safe swap view")
	}
	for _, id := range []string{"wizardAdvanced", "exchangeQuote", "exchangeSwap", "exchangeWithdraw"} {
		if _, exists := findNode(page, id); exists {
			t.Fatal("legacy Bybit controls still reachable", id)
		}
	}
	for _, id := range []string{"exchangeQuote", "exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
		a.event(webui.Event{ID: id})
		if a.busy || len(ui.queue) != 0 {
			t.Fatal("stale legacy Bybit event acquired authority", id)
		}
	}
	for _, value := range []string{"-1", "1e3", "0", "NaN"} {
		a.fields["freeTradeAmount"] = value
		field, _ := findNode(a.exchangePage(), "field/freeTradeAmount")
		if !strings.Contains(field.Error, "positive decimal amount") {
			t.Fatal("disconnected swap lost numeric feedback", value, field)
		}
	}
	a.fields["freeTradeAmount"], a.fields["slippage"] = "1", "2"
	field, _ := findNode(a.exchangePage(), "field/slippage")
	if !strings.Contains(field.Error, "0.01% to 1%") {
		t.Fatal("disconnected swap lost slippage bounds", field)
	}
}
