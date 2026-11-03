package nativewallet

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/webui"
)

func TestGRAMReturnDefaultFollowsUntouchedZeroToAvailableFive(t *testing.T) {
	a, _ := gramSwapGateFixture(t, "Sell")
	a.wizard.viewStep = 4
	a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "5.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "0"}}
	a.exchangePage()
	if a.fields["freeReturnAmount"] != "0" {
		t.Fatal("initial observed zero suggestion changed")
	}
	a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "5"}}
	page := a.exchangePage()
	if a.fields["freeReturnAmount"] != "5" {
		t.Fatal("old zero suggestion did not follow fresh Unified funds", a.fields)
	}
	if button, _ := findNode(page, "freeReturn"); button.Disabled {
		t.Fatal("fresh five-GRAM return stayed disabled", viewText(page))
	}
}

func TestGRAMReturnBalanceRefreshPreservesUserAmountOrClearedDraft(t *testing.T) {
	for _, value := range []string{"3", "", "6"} {
		t.Run(value, func(t *testing.T) {
			a, _ := gramSwapGateFixture(t, "Sell")
			a.wizard.viewStep = 4
			a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "1"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "5"}}
			a.exchangePage()
			a.event(webui.Event{ID: "field/freeReturnAmount", Value: value})
			a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "1"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "4"}}
			page := a.exchangePage()
			if a.fields["freeReturnAmount"] != value {
				t.Fatal("refresh overwrote the entered or cleared draft", value, a.fields)
			}
			button, _ := findNode(page, "freeReturn")
			if button.Disabled != (value != "3") {
				t.Fatal("draft was not checked against actual Unified balance", value, button)
			}
		})
	}
}

func TestExchangeExpiredQuoteExplainsDisabledSwapAndHasNoManualStatusButtons(t *testing.T) {
	a, account := gramSwapGateFixture(t, "Sell")
	var quote map[string]any
	if json.Unmarshal(a.freeData().quote, &quote) != nil {
		t.Fatal("fixture quote")
	}
	quote["observedAt"] = nowToExchangeMilliseconds(time.Now().Add(-3 * time.Minute))
	a.freeData().quote, _ = json.Marshal(quote)
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "operations": []map[string]string{{"provider": "bybit", "id": "restored-transfer", "kind": "transfer", "state": "PENDING", "accountId": account.ID, "assetId": "ton-mainnet:TON"}}})
	page := a.exchangePage()
	field, _ := findNode(page, "field/freeTradeAmount")
	if !strings.Contains(field.Error, "Refresh market data") {
		t.Fatal("expired quote silently disabled Swap", field)
	}
	if strings.Contains(viewText(page), "checking automatically") || strings.Contains(viewText(page), "Original deposit status") {
		t.Fatal("hidden bookkeeping was rendered as a workflow gate")
	}
	for _, id := range []string{"exchangeReconcile/restored-transfer", "freeCheck/restored-transfer"} {
		if _, exists := findNode(page, id); exists {
			t.Fatal("manual original-transfer check still shown", id)
		}
	}
}

func TestFreshTransferBalanceUpdatesQuoteFundsWithoutRenewingMarketRules(t *testing.T) {
	a, _ := gramSwapGateFixture(t, "Sell")
	var quote map[string]any
	if json.Unmarshal(a.freeData().quote, &quote) != nil {
		t.Fatal("quote")
	}
	observed := quote["observedAt"]
	quote["balances"] = []wizardBalance{{AccountType: "UNIFIED", Coin: "GRAM", Available: "0"}, {AccountType: "UNIFIED", Coin: "USDT", Available: "2"}}
	a.freeData().quote, _ = json.Marshal(quote)
	if a.freeTradeValidation().SwapValid {
		t.Fatal("unfunded trade started enabled")
	}
	a.setFreeBalances([]wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "5"}})
	if !a.freeTradeValidation().SwapValid {
		t.Fatal("fresh five-GRAM observation did not update available funds", a.freeTradeValidation())
	}
	if json.Unmarshal(a.freeData().quote, &quote) != nil || quote["observedAt"] != observed {
		t.Fatal("balance observation renewed market freshness")
	}
	quote["observedAt"] = nowToExchangeMilliseconds(time.Now().Add(-3 * time.Minute))
	a.freeData().quote, _ = json.Marshal(quote)
	a.setFreeBalances([]wizardBalance{{AccountType: "UNIFIED", Coin: "GRAM", Available: "5"}})
	if a.freeTradeValidation().SwapValid {
		t.Fatal("balance refresh revived expired market rules")
	}
}

func TestFailedPostTransferRefreshCannotRetainQuoteSpendability(t *testing.T) {
	a, _ := gramSwapGateFixture(t, "Sell")
	if !a.freeTradeValidation().SwapValid {
		t.Fatal("fixture")
	}
	a.setFreeBalances([]wizardBalance{})
	if a.freeTradeValidation().SwapValid {
		t.Fatal("failed balance observation preserved available quote funds")
	}
}

func TestPartialTransferObservationCannotRetainUnobservedUnifiedFunds(t *testing.T) {
	a, _ := gramSwapGateFixture(t, "Sell")
	a.setFreeBalances([]wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "1"}})
	if a.freeTradeValidation().SwapValid {
		t.Fatal("partial refresh retained unobserved Unified funds")
	}
}

func TestRestoredGRAMTransferAutomaticallyEnablesSwapAndReturnWithoutManualCheck(t *testing.T) {
	a, account := gramSwapGateFixture(t, "Sell")
	ui := a.ui.(*testFrontend)
	backend := a.core.(*testBackend)
	original := strings.Repeat("c", 32)
	a.active = "exchange"
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "endpoint": "https://api.bybit.com", "keySuffix": "test", "operations": []map[string]string{{"provider": "bybit", "id": original, "kind": "transfer", "state": "PENDING", "accountId": account.ID, "assetId": "ton-mainnet:TON"}}})
	a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "5.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "0"}}
	a.wizard.viewStep = 4
	a.exchangePage()
	if a.fields["freeReturnAmount"] != "0" {
		t.Fatal("initial empty Unified suggestion")
	}
	a.wizard.viewStep = 3
	if button, _ := findNode(a.exchangePage(), "freeSwap"); button.Disabled {
		t.Fatal("older unresolved original blocked independently available funds")
	}
	var calls []string
	backend.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "exchangeReconcile":
			if freeRequestFields(t, body)["id"] != original {
				t.Error("reconciliation replaced original transfer ID")
			}
			return answer(out, wizardResult{ID: original, Kind: "transfer", State: "CONFIRMED"})
		case "exchangeFundingBalances":
			return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "5"}}})
		default:
			t.Error("automatic tracker requested financial authority", method)
			return nil
		}
	}
	if !a.nextFreeExchangeCheck() {
		t.Fatal("persisted transfer was not resumed automatically")
	}
	ui.apply(t)
	if strings.Join(calls, ",") != "exchangeReconcile,exchangeFundingBalances" {
		t.Fatal("automatic tracker did not use original readonly lookup", calls)
	}
	if button, _ := findNode(a.exchangePage(), "freeSwap"); button.Disabled {
		t.Fatal("valid five-GRAM Swap did not unblock after verified original confirmation", viewText(a.exchangePage()))
	}
	a.wizard.viewStep = 4
	page := a.exchangePage()
	if a.fields["freeReturnAmount"] != "5" {
		t.Fatal("confirmed transfer retained zero return suggestion", a.fields)
	}
	if button, _ := findNode(page, "freeReturn"); button.Disabled {
		t.Fatal("available five-GRAM return remained disabled", viewText(page))
	}
	if strings.Contains(viewText(page), "Check original") || strings.Contains(viewText(page), "Refresh original operation") {
		t.Fatal("manual original-status check still rendered")
	}
}
