package bybit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUSDCSpotPairScopeAndCanonicalAuthorization(t *testing.T) {
	for _, quote := range []string{"USDT", "USDC"} {
		base, got, ok := SpotPair("ETH" + quote)
		if !ok || base != "ETH" || got != quote {
			t.Fatal("supported spot pair did not retain its exact quote")
		}
		for _, side := range []string{"Buy", "Sell"} {
			o := fixtureOrder()
			o.Symbol, o.Side = "ETH"+quote, side
			if side == "Buy" {
				o.Qty, o.MarketUnit = "250.01", "quoteCoin"
			}
			if ValidateSpotOrder(o) != nil || ValidateAuthRequest(postRequest("/v5/order/create", o)) != nil {
				t.Fatal("exact supported quote order denied", quote, side)
			}
			for _, path := range []string{"/v5/market/instruments-info", "/v5/market/tickers", "/v5/account/fee-rate"} {
				if ValidateAuthRequest(readRequest(path, "category=spot&symbol="+o.Symbol)) != nil {
					t.Fatal("supported quote read denied", path)
				}
			}
		}
	}
	for _, symbol := range []string{"ETHBTC", "ETHDAI", "ETHusdc", "USDCUSDC", "USDTUSDT", "USDC", "ETHUSDCUSDT!", "ETH.USDC", "USDCETH"} {
		if _, _, ok := SpotPair(symbol); ok {
			t.Fatal("pair outside explicit quote policy accepted", symbol)
		}
	}
}

func TestUSDCInstrumentIdentityAndExactOrderWire(t *testing.T) {
	ctx := context.Background()
	metadata := `{"category":"spot","list":[{"symbol":"ETHUSDC","baseCoin":"ETH","quoteCoin":"USDC","status":"Trading","lotSizeFilter":{"basePrecision":"0.000001","quotePrecision":"0.01","minOrderAmt":"5","maxMarketOrderQty":"100"},"priceFilter":{"tickSize":"0.01"}}]}`
	c := testClient(t, metadata)
	if instrument, err := c.SpotInstrument(ctx, "ETHUSDC"); err != nil || instrument.QuoteCoin != "USDC" {
		t.Fatal("USDC instrument unavailable", err)
	}
	for _, invalid := range []string{
		strings.Replace(metadata, `"quoteCoin":"USDC"`, `"quoteCoin":"USDT"`, 1),
		strings.Replace(metadata, `"symbol":"ETHUSDC"`, `"symbol":"ETHUSDT"`, 1),
		strings.Replace(metadata, `"baseCoin":"ETH"`, `"baseCoin":"TON"`, 1),
		strings.Replace(metadata, `"status":"Trading"`, `"status":"Settling"`, 1),
	} {
		if _, err := testClient(t, invalid).SpotInstrument(ctx, "ETHUSDC"); err == nil {
			t.Fatal("foreign quote/base/status metadata accepted")
		}
	}
	for _, side := range []string{"Buy", "Sell"} {
		o := fixtureOrder()
		o.Symbol, o.Side = "ETHUSDC", side
		if side == "Buy" {
			o.Qty, o.MarketUnit = "123.45", "quoteCoin"
		}
		wire, _ := json.Marshal(o)
		c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "POST" || r.URL.Path != "/v5/order/create" || r.Header.Get("X-BAPI-API-KEY") != fixtureKey {
				t.Fatal("USDC request escaped its fixed authenticated API")
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != string(wire) {
				t.Fatal("wire changed exact USDC intent")
			}
			return resultResponse(`{"orderId":"remote-USDC-1","orderLinkId":"fixtureOrder1"}`), nil
		})
		if _, err := c.CreateOrder(ctx, o); err != nil {
			t.Fatal("USDC order transport failed", err)
		}
	}
}
