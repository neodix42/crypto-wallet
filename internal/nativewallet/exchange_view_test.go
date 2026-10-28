package nativewallet

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/webui"
)

func TestSpotDirectionsSendExactUSDTOrUSDCAndInvalidateChangedQuotes(t *testing.T) {
	for _, direction := range []string{"Sell", "Buy", "SellUSDC", "BuyUSDC"} {
		for _, method := range []string{"exchangeQuote", "exchangeSwap"} {
			t.Run(direction+"/"+method, func(t *testing.T) {
				a, ui, core := fixture(t)
				side, quote, ok := spotDirection(direction)
				if !ok {
					t.Fatal("supported direction rejected")
				}
				a.quote = json.RawMessage(`{"lastPrice":"1"}`)
				a.event(webui.Event{ID: "field/side", Value: direction})
				if a.quote != nil {
					t.Fatal("changing quote currency retained stale market")
				}
				a.fields["exchangeAmount"] = "1.25"
				body := map[string]string{"accountId": a.fields["exchangeAccount"], "assetId": a.fields["exchangeAsset"], "side": side, "quoteCoin": quote}
				if method == "exchangeSwap" {
					body["amount"], body["slippage"] = "1.25", "0.5"
				}
				routeFixture(t, a, core, method, body, json.RawMessage(`{"state":"UNKNOWN"}`))
				a.event(webui.Event{ID: method})
				ui.apply(t)
				if !strings.Contains(a.status, "completed") {
					t.Fatal("direction/quote changed on public request path")
				}
			})
		}
	}
	a, _, _ := fixture(t)
	a.fields["side"] = "BuyUSDC-arbitrary"
	a.event(webui.Event{ID: "exchangeSwap"})
	if a.busy {
		t.Fatal("arbitrary quote reached backend")
	}
}

func TestExchangeHumanReadableStatusQuoteAndOriginalReconcileWithoutJSONDump(t *testing.T) {
	a, _, _ := fixture(t)
	a.exchange = json.RawMessage(`{"connected":true,"endpoint":"https://api.bybit.com","message":"Public connected","operations":[{"id":"original-id","kind":"spot","state":"UNKNOWN"}],"debugDump":"must-not-render"}`)
	a.quote = json.RawMessage(`{"symbol":"ETHUSDC","coin":"ETH","quoteCoin":"USDC","side":"Buy","lastPrice":"2500.123456","bidPrice":"2500","askPrice":"2501","feeRate":"0.001","observedAt":"public-time","balances":[{"accountType":"UNIFIED","coin":"USDC","available":"50.25"}],"route":{"supported":true,"chain":"ETH","minWithdraw":"0.001","withdrawFee":"0.0001","withdrawPercentageFee":"0"}}`)
	page := a.exchangePage()
	text := viewText(page)
	if strings.Contains(text, "must-not-render") || strings.Contains(text, `\"connected\"`) || strings.Contains(text, `\"lastPrice\"`) {
		t.Fatal("raw JSON leaked into exchange presentation")
	}
	for _, required := range []string{"ETHUSDC", "2500.123456 USDC", "50.25 USDC", "0.0001 ETH", "original-id"} {
		if !strings.Contains(text, required) {
			t.Fatal("formatted exchange view lost exact price/currency/fee or original operation")
		}
	}
	if _, ok := findNode(page, "exchangeReconcile/original-id"); !ok {
		t.Fatal("unknown original operation cannot reconcile")
	}
	result := exchangeResultSummary(json.RawMessage(`{"id":"op-id","kind":"spot","state":"PARTIALLY_FILLED","filledBase":"0.123456","filledQuote":"45.123456","fees":{"USDC":"0.045123"},"message":"Reconcile before retry"}`))
	for _, required := range []string{"op-id", "PARTIALLY_FILLED", "0.123456", "45.123456", "0.045123 USDC"} {
		if !strings.Contains(result, required) {
			t.Fatal("formatted result hid reconciliation/precision")
		}
	}
	if strings.Contains(result, "{") {
		t.Fatal("result is raw JSON")
	}
}
