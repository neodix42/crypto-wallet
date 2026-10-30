package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

)

func changedFixture(t *testing.T, body, from, to string) string {
	t.Helper()
	if !strings.Contains(body, from) {
		t.Fatal("invalid test mutation", from)
	}
	return strings.Replace(body, from, to, 1)
}
func TestHostileObservationFieldsDoNotBecomeVerifiedBalancesRoutesPricesOrApprovals(t *testing.T) {
	ctx := context.Background()
	checks := []struct {
		name, path string
		call       func(*Client) error
		changes    [][2]string
	}{
		{"key", "/sapi/v1/account/apiRestrictions", func(c *Client) error { _, e := c.APIKeyInfo(ctx); return e }, [][2]string{{`"enableReading":true`, `"enableReading":false`}, {`"permitsUniversalTransfer":true`, `"enableInternalTransfer":true`}, {`"enableWithdrawals":true`, `"enableWithdrawals":null`}}},
		{"coin", "/sapi/v1/capital/config/getall", func(c *Client) error { _, e := c.CoinInfo(ctx, "ETH"); return e }, [][2]string{{`"coin":"ETH"`, `"coin":"BTC"`}, {`"name":"Ethereum"`, `"name":""`}, {`"network":"ETH"`, `"network":"bad/network"`}, {`"coin":"ETH","name":"Ethereum (ERC20)"`, `"coin":"BTC","name":"Ethereum (ERC20)"`}, {`"depositEnable":true`, `"depositEnable":null`}, {`"withdrawEnable":true`, `"withdrawEnable":null`}, {`"minConfirm":12`, `"minConfirm":null`}, {`"unLockConfirm":64`, `"unLockConfirm":null`}, {`"withdrawFee":"0.0002"`, `"withdrawFee":"-1"`}, {`"withdrawMin":"0.001"`, `"withdrawMin":"1e-3"`}, {`"withdrawMax":"100"`, `"withdrawMax":""`}, {`"withdrawIntegerMultiple":"0.00000001"`, `"withdrawIntegerMultiple":"0.000125"`}, {`"depositDust":"0.00001"`, `"depositDust":""`}, {`"denomination":1`, `"denomination":1000`}}},
		{"deposit", "/sapi/v1/capital/deposit/address", func(c *Client) error { _, e := c.DepositAddress(ctx, "ETH", "ETH"); return e }, [][2]string{{`"coin":"ETH"`, `"coin":"TON"`}, {`"address":"0x1111111111111111111111111111111111111111"`, `"address":""`}, {`"tag":""`, `"tag":null`}}},
		{"fund", "/sapi/v1/asset/get-funding-asset", func(c *Client) error { _, e := c.AccountBalance(ctx, "FUND", "ETH"); return e }, [][2]string{{`"free":"1.123456789123456789"`, `"free":"-1"`}, {`"locked":"2"`, `"locked":null`}, {`"freeze":"3"`, `"freeze":""`}, {`"withdrawing":"4"`, `"withdrawing":"1e2"`}, {`"asset":"ETH"`, `"asset":"bad/coin"`}}},
		{"spot", "/api/v3/account", func(c *Client) error { _, e := c.AccountBalance(ctx, "UNIFIED", "ETH"); return e }, [][2]string{{`"accountType":"SPOT"`, `"accountType":"MARGIN"`}, {`"balances":[`, `"balances":null,"ignored":[`}, {`"free":"1.123456789123456789"`, `"free":null`}}},
		{"instrument", "/api/v3/exchangeInfo", func(c *Client) error { _, e := c.SpotInstrument(ctx, "ETHUSDT"); return e }, [][2]string{{`"symbol":"ETHUSDT"`, `"symbol":"BTCUSDT"`}, {`"status":"TRADING"`, `"status":"BREAK"`}, {`"baseAsset":"ETH"`, `"baseAsset":"BTC"`}, {`"isSpotTradingAllowed":true`, `"isSpotTradingAllowed":null`}, {`"quoteAssetPrecision":8`, `"quoteAssetPrecision":37`}, {`"orderTypes":["LIMIT","MARKET"]`, `"orderTypes":["MARKET"]`}, {`"tickSize":"0.01"`, `"tickSize":"0"`}, {`"minQty":"0.0001"`, `"minQty":"0"`}, {`"maxQty":"100"`, `"maxQty":"-1"`}, {`"stepSize":"0.0001"`, `"stepSize":"1e-4"`}, {`"minNotional":"5"`, `"minNotional":"0"`}, {`"filterType":"PRICE_FILTER"`, `"filterType":"NOT_PRICE"`}, {`"filterType":"LOT_SIZE"`, `"filterType":"NOT_LOT"`}, {`"filterType":"NOTIONAL"`, `"filterType":"NOT_NOTIONAL"`}}},
		{"ticker", "/api/v3/ticker/24hr", func(c *Client) error { _, e := c.SpotTicker(ctx, "ETHUSDT"); return e }, [][2]string{{`"symbol":"ETHUSDT"`, `"symbol":"ETHUSDC"`}, {`"lastPrice":"2000.123456789123456789"`, `"lastPrice":"0"`}, {`"bidPrice":"2000.01"`, `"bidPrice":"2001"`}, {`"askPrice":"2000.02"`, `"askPrice":"1e3"`}}},
		{"fees", "/api/v3/account/commission", func(c *Client) error { _, e := c.SpotFeeRate(ctx, "ETHUSDT"); return e }, [][2]string{{`"symbol":"ETHUSDT"`, `"symbol":"ETHUSDC"`}, {`"enabledForAccount":false`, `"enabledForAccount":null`}, {`"enabledForSymbol":true`, `"enabledForSymbol":null`}, {`"maker":"0.001"`, `"maker":"-1"`}, {`"seller":"0.0002"`, `"seller":"1.01"`}, {`"taker":"0.002"`, `"taker":""`}}},
		{"order", "/api/v3/order", func(c *Client) error { _, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1"); return e }, [][2]string{{`"symbol":"ETHUSDT"`, `"symbol":"BTCUSDT"`}, {`"orderId":123`, `"orderId":null`}, {`"clientOrderId":"fixtureOrder1"`, `"clientOrderId":"different"`}, {`"type":"LIMIT"`, `"type":"MARKET"`}, {`"timeInForce":"IOC"`, `"timeInForce":"GTC"`}, {`"side":"SELL"`, `"side":"BORROW"`}, {`"price":"2000"`, `"price":"0"`}, {`"origQty":"0.1"`, `"origQty":"0"`}, {`"executedQty":"0.1"`, `"executedQty":"0.2"`}, {`"cummulativeQuoteQty":"200"`, `"cummulativeQuoteQty":"-1"`}, {`"time":1700000000000`, `"time":null`}, {`"updateTime":1700000001000`, `"updateTime":1699999999999`}, {`"status":"FILLED"`, `"status":"UNRECOGNIZED"`}}},
		{"fills", "/api/v3/myTrades", func(c *Client) error { _, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", ""); return e }, [][2]string{{`"symbol":"ETHUSDT"`, `"symbol":"BTCUSDT"`}, {`"id":1`, `"id":null`}, {`"orderId":123`, `"orderId":456`}, {`"price":"2000"`, `"price":"0"`}, {`"qty":"0.1"`, `"qty":"0"`}, {`"quoteQty":"200"`, `"quoteQty":"0"`}, {`"commission":"0.2"`, `"commission":"-1"`}, {`"commissionAsset":"USDT"`, `"commissionAsset":"bad/token"`}, {`"time":1700000000000`, `"time":null`}, {`"isBuyer":false`, `"isBuyer":true`}}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			for i, change := range check.changes {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					c := testClient(t, func(r *http.Request) string {
						body := wireFixture(r)
						if r.URL.Path == check.path {
							return changedFixture(t, body, change[0], change[1])
						}
						return body
					})
					if e := check.call(c); !errors.Is(e, ErrInvalidResponse) {
						t.Fatal("hostile observation accepted", e)
					}
				})
			}
			for _, body := range []string{`null`, `{}`, `[]`} {
				c := testClient(t, func(r *http.Request) string {
					if r.URL.Path == check.path {
						return body
					}
					return wireFixture(r)
				})
				e := check.call(c)
			if e == nil && check.name != "fund" && check.name != "fills" {
					t.Fatal("missing shape accepted", body)
				}
			}
			c := testClient(t, wireFixture)
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET_CANARY") })
			if e := check.call(c); e != ErrUnavailable {
				t.Fatal("unredacted/missing provider failure", e)
			}
		})
	}
}
func TestExplicitEmptyBalancesDuplicateRowsAndMetadataBinding(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"FUND", "UNIFIED"} {
		for _, body := range []string{`[]`, `[{"asset":"BTC","free":"9","locked":"0","freeze":"0","withdrawing":"0"}]`} {
			c := testClient(t, func(*http.Request) string {
				if kind == "UNIFIED" {
					return `{"accountType":"SPOT","balances":` + body + `}`
				}
				return body
			})
			v, e := c.AccountBalance(ctx, kind, "ETH")
			if e != nil || v.Balance.TransferSafeAmount != "0" {
				t.Fatal("complete absent coin not verified zero", v, e)
			}
		}
		row := `{"asset":"ETH","free":"1","locked":"0","freeze":"0","withdrawing":"0"}`
		c := testClient(t, func(*http.Request) string {
			body := `[` + row + `,` + row + `]`
			if kind == "UNIFIED" {
				body = `{"accountType":"SPOT","balances":` + body + `}`
			}
			return body
		})
		if _, e := c.AccountBalance(ctx, kind, "ETH"); e != ErrInvalidResponse {
			t.Fatal("duplicate spendable balance accepted")
		}
	}
	for _, body := range []string{`[` + fixtureCoin[1:len(fixtureCoin)-1] + `,` + fixtureCoin[1:len(fixtureCoin)-1] + `]`, strings.Replace(fixtureCoin, `"networkList":[`, `"networkList":null,"ignored":[`, 1)} {
		c := testClient(t, func(*http.Request) string { return body })
		if _, e := c.CoinInfo(ctx, "ETH"); e != ErrInvalidResponse {
			t.Fatal("duplicate/missing metadata accepted", e)
		}
	}
	for _, network := range []string{"ETH", "TON"} {
		c := testClient(t, func(r *http.Request) string {
			if r.URL.Path == "/sapi/v1/capital/config/getall" {
				return strings.Replace(fixtureCoin, `"depositEnable":true`, `"depositEnable":false`, 1)
			}
			t.Fatal("unavailable network reached deposit address")
			return `{}`
		})
		if _, e := c.DepositAddress(ctx, "ETH", network); e != ErrInvalidResponse {
			t.Fatal(e)
		}
	}
	for _, step := range []struct{ value, want string }{{"1", "0"}, {"0.1", "1"}, {"0.00010000", "4"}, {"10", ""}, {"0.000125", ""}, {"-1", ""}} {
		v, e := stepPrecision(step.value)
		if step.want == "" {
			if e == nil {
				t.Fatal("unsupported precision fabricated", step)
			}
		} else if e != nil || v != step.want {
			t.Fatal(step, v, e)
		}
	}
}
func TestFeesAndRegionalRequirementsPreventAnyNativeWrite(t *testing.T) {
	ctx := context.Background()
	for _, country := range []string{"AE", "AU", "BR", "TR", "nil", ""} {
		c := testClient(t, func(r *http.Request) string {
			if r.URL.Path != "/sapi/v1/localentity/questionnaire-requirements" {
				t.Fatal("regional withdrawal reached write")
			}
			b, _ := json.Marshal(map[string]string{"questionnaireCountryCode": country})
			return string(b)
		})
		if _, e := c.Withdraw(ctx, withdrawal()); e == nil {
			t.Fatal("unimplemented questionnaire bypassed", country)
		}
	}
	c := testClient(t, func(*http.Request) string { return `{}` })
	if e := c.WithdrawalAllowed(ctx); e != ErrInvalidResponse {
		t.Fatal(e)
	}
	c = testClient(t, func(r *http.Request) string {
		if r.URL.Path != "/api/v3/account/commission" {
			t.Fatal("BNB fee gate reached order")
		}
		return strings.Replace(fixtureCommission, `"enabledForAccount":false`, `"enabledForAccount":true`, 1)
	})
	if _, e := c.CreateOrder(ctx, spotOrder()); e != ErrFeeCurrency {
		t.Fatal("unapproved BNB fee asset admitted", e)
	}
}
func TestOrderStatusMappingAndExecutionContinuationAreExact(t *testing.T) {
	ctx := context.Background()
	for _, status := range []string{"NEW", "PARTIALLY_FILLED", "FILLED", "CANCELED", "EXPIRED", "EXPIRED_IN_MATCH", "PENDING_CANCEL", "REJECTED"} {
		for _, qty := range []string{"0", "0.1"} {
			c := testClient(t, func(*http.Request) string {
				return strings.Replace(strings.Replace(fixtureOrder, `"status":"FILLED"`, `"status":"`+status+`"`, 1), `"executedQty":"0.1"`, `"executedQty":"`+qty+`"`, 1)
			})
			if _, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1"); e != nil {
				t.Fatal(status, qty, e)
			}
		}
	}
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Path != "/api/v3/myTrades" {
			return fixtureOrder
		}
		return `[]`
	})
	page, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "")
	if e != nil || page.List == nil || len(page.List) != 0 {
		t.Fatal(page, e)
	}
	for _, cursor := range []string{"001", "-1", "nonnumeric"} {
		if _, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", cursor); e != ErrInvalidRequest {
			t.Fatal(e)
		}
	}
	rows := []string{}
	for i := 1; i <= 100; i++ {
		rows = append(rows, strings.Replace(fixtureTrades[1:len(fixtureTrades)-1], `"id":1`, `"id":`+fmt.Sprint(i), 1))
	}
	c = testClient(t, func(r *http.Request) string {
		if r.URL.Path != "/api/v3/myTrades" {
			return fixtureOrder
		}
		return `[` + strings.Join(rows, ",") + `]`
	})
	page, e = c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "1")
	if e != nil || page.NextPageCursor != "101" || len(page.List) != 100 {
		t.Fatal("fills continuation lost", page, e)
	}
	for _, body := range []string{`[` + rows[0] + `,` + rows[0] + `]`, `[` + rows[1] + `,` + rows[0] + `]`} {
		c = testClient(t, func(r *http.Request) string {
			if r.URL.Path != "/api/v3/myTrades" {
				return fixtureOrder
			}
			return body
		})
		if _, e = c.Executions(ctx, "ETHUSDT", "fixtureOrder1", ""); e != ErrInvalidResponse {
			t.Fatal("duplicate/reordered fill accepted")
		}
	}
	c = testClient(t, wireFixture)
	if _, e = c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "2"); e != ErrInvalidResponse {
		t.Fatal("earlier fill escaped requested continuation")
	}
	o := spotOrder()
	o.Side = "Buy"
	o.MarketUnit = "quoteCoin"
	o.Qty = "200"
	if !validOrder(o) {
		t.Fatal("valid bounded buy")
	}
}
