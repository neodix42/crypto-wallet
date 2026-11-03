package nativewallet

// Public synthetic market/account fixtures reproduce the owner's form values.
// These are form-gate tests, not authenticated Bybit trading acceptance.
import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func gramSwapGateFixture(t *testing.T, side string) (*App, model.Account) {
	t.Helper()
	a, _, _, _, _ := freeNativeFixture(t)
	var account model.Account
	for _, owned := range a.state.Accounts {
		if owned.Network == "ton-mainnet" {
			account = owned
			break
		}
	}
	if account.ID == "" {
		t.Fatal("fixture lacks owned TON account")
	}
	a.fields["freeAccount"], a.fields["freeAsset"] = account.ID, "ton-mainnet:TON"
	a.fields["freeTradeAmount"], a.fields["slippage"] = "5", "0.5"
	a.wizard.viewStep = 3
	pair := wizardPair{ID: "gram-usdt-" + side, AccountID: account.ID, AssetID: "ton-mainnet:TON", Side: side, QuoteCoin: "USDT", Symbol: "GRAMUSDT", OutputCoin: "USDT"}
	if side == "Buy" {
		pair.OutputCoin = "GRAM"
	}
	a.freeData().pairs = []wizardPair{pair}
	a.fields["freePair"] = pair.ID
	// Use the production JSON field spellings, independent of the validation DTO.
	data := map[string]any{
		"provider": "bybit", "accountId": account.ID, "assetId": "ton-mainnet:TON",
		"coin": "GRAM", "quoteCoin": "USDT", "symbol": "GRAMUSDT", "side": side,
		"lastPrice": "1.176", "bidPrice": "1.175", "askPrice": "1.177",
		"basePrecision": "0.01", "quotePrecision": "0.00001", "pricePrecision": "0.001",
		"minOrderAmount": "5", "maxOrderQty": "100200", "feeRate": "0.001",
		"observedAt": nowToExchangeMilliseconds(time.Now()), "route": map[string]any{"supported": false},
		"balances": []wizardBalance{
			{AccountType: "FUND", Coin: "GRAM", Available: "0.99998566", WalletBalance: "0.99998566"},
			{AccountType: "UNIFIED", Coin: "GRAM", Available: "5", WalletBalance: "5"},
			{AccountType: "UNIFIED", Coin: "USDT", Available: "5", WalletBalance: "5"},
		},
	}
	var err error
	a.freeData().quote, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return a, account
}

func TestGRAMFiveUnitSwapEnablesWithFreshCurrentVenueRules(t *testing.T) {
	for _, side := range []string{"Sell", "Buy"} {
		t.Run(side, func(t *testing.T) {
			a, _ := gramSwapGateFixture(t, side)
			if result := a.freeTradeValidation(); !result.SwapValid {
				t.Fatal("5 whole units with valid current rules stayed disabled", result)
			}
			button, exists := findNode(a.exchangePage(), "freeSwap")
			if !exists || button.Disabled {
				t.Fatal("valid GRAMUSDT form stayed disabled", button)
			}
			if !strings.Contains(viewText(a.exchangePage()), "1.176 USDT") {
				t.Fatal("exact displayed last price lost")
			}
			// Ordinary edits may change quantity/slippage without requiring another
			// instrument lookup, while the existing quote is still fresh and bound.
			a.event(webui.Event{ID: "field/freeTradeAmount", Value: "4"})
			a.event(webui.Event{ID: "field/freeTradeAmount", Value: "5"})
			a.event(webui.Event{ID: "field/slippage", Value: "0.50"})
			if result := a.freeTradeValidation(); !result.SwapValid {
				t.Fatal("valid draft edits unnecessarily disabled Swap", result)
			}
		})
	}
}

func TestGRAMSwapPreservesExactLotMinimumFreshnessAndSpendingCoinBounds(t *testing.T) {
	for _, scenario := range []string{"lot", "minimum", "stale", "future", "base-balance", "quote-balance", "quote-identity", "side", "slippage"} {
		t.Run(scenario, func(t *testing.T) {
			side := "Sell"
			if scenario == "quote-balance" {
				side = "Buy"
			}
			a, _ := gramSwapGateFixture(t, side)
			fields := freeRequestFields(t, a.freeData().quote)
			switch scenario {
			case "lot":
				a.fields["freeTradeAmount"] = "5.001"
			case "minimum":
				fields["minOrderAmount"] = "6"
			case "stale":
				fields["observedAt"] = nowToExchangeMilliseconds(time.Now().Add(-3 * time.Minute))
			case "future":
				fields["observedAt"] = nowToExchangeMilliseconds(time.Now().Add(3 * time.Second))
			case "quote-identity":
				fields["assetId"] = "ethereum-mainnet:native"
			case "side":
				fields["side"] = "Buy"
			case "slippage":
				a.fields["slippage"] = "1.01"
			case "base-balance", "quote-balance":
				coin := "GRAM"
				if scenario == "quote-balance" {
					coin = "USDT"
				}
				fields["balances"] = []wizardBalance{{AccountType: "FUND", Coin: coin, Available: "1000"}, {AccountType: "UNIFIED", Coin: coin, Available: "4.99999"}}
			}
			var err error
			a.freeData().quote, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			result := a.freeTradeValidation()
			if result.SwapValid || result.SwapError == "" {
				t.Fatal("unsafe or unavailable trade rules did not provide a refusal", scenario, result)
			}
			if button, _ := findNode(a.exchangePage(), "freeSwap"); !button.Disabled {
				t.Fatal("unsafe trade enabled", scenario)
			}
			if (scenario == "stale" || scenario == "future" || scenario == "quote-identity" || scenario == "side") && !strings.Contains(result.SwapError, "Refresh market data") {
				t.Fatal("stale/foreign quote lost actionable refresh reason", result)
			}
		})
	}
}

func TestGRAMSwapPendingOriginalIsSeparateFromNumericEligibility(t *testing.T) {
	a, account := gramSwapGateFixture(t, "Sell")
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "operations": []map[string]string{{"id": "original-transfer", "kind": "transfer", "state": "PENDING", "accountId": account.ID, "assetId": "ton-mainnet:TON"}}})
	if result := a.freeTradeValidation(); !result.SwapValid {
		t.Fatal("pending original corrupted numeric market eligibility", result)
	}
	if button, _ := findNode(a.exchangePage(), "freeSwap"); !button.Disabled {
		t.Fatal("unresolved exchange write allowed another trade")
	}
}
