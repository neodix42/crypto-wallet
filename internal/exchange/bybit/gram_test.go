package bybit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Current GRAM orders use the new service ticker while retained historical TON
// read/authentication fixtures continue to exercise original-ID reconciliation.
func TestGramUSDTAndUSDCMetadataAndExactSpotOrders(t *testing.T) {
	for _, quote := range []string{"USDT", "USDC"} {
		symbol := "GRAM" + quote
		if base, gotQuote, ok := SpotPair(symbol); !ok || base != "GRAM" || gotQuote != quote {
			t.Fatal("current native TON currency did not select its exact GRAM spot pair")
		}
		metadata := `{"category":"spot","list":[{"symbol":"GRAMQUOTE","baseCoin":"GRAM","quoteCoin":"QUOTE","status":"Trading","lotSizeFilter":{"basePrecision":"0.000001","quotePrecision":"0.01","minOrderAmt":"5","maxMarketOrderQty":"100"},"priceFilter":{"tickSize":"0.01"}}]}`
		metadata = strings.ReplaceAll(metadata, "QUOTE", quote)
		client := testClient(t, metadata)
		if instrument, err := client.SpotInstrument(context.Background(), symbol); err != nil || instrument.BaseCoin != "GRAM" || instrument.QuoteCoin != quote {
			t.Fatal("GRAM instrument identity unavailable", err)
		}
		for _, bad := range []string{strings.Replace(metadata, `"baseCoin":"GRAM"`, `"baseCoin":"TON"`, 1), strings.Replace(metadata, `"symbol":"GRAM`+quote+`"`, `"symbol":"TON`+quote+`"`, 1)} {
			if _, err := testClient(t, bad).SpotInstrument(context.Background(), symbol); err == nil {
				t.Fatal("old TON instrument was silently substituted for current GRAM")
			}
		}
		for _, side := range []string{"Buy", "Sell"} {
			order := fixtureOrder()
			order.Symbol, order.Side = symbol, side
			if side == "Buy" {
				order.Qty, order.MarketUnit = "25.01", "quoteCoin"
			}
			wire, _ := json.Marshal(order)
			client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil || request.Method != "POST" || request.URL.Path != "/v5/order/create" || string(body) != string(wire) || request.Header.Get("X-BAPI-API-KEY") != fixtureKey {
					t.Fatal("GRAM intent changed before its exact authenticated test transport")
				}
				return resultResponse(`{"orderId":"public-gram-order","orderLinkId":"fixtureOrder1"}`), nil
			})
			if _, err := client.CreateOrder(context.Background(), order); err != nil {
				t.Fatal("exact current GRAM order rejected", err)
			}
		}
	}
}
