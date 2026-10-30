package walletcore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func configureUSDCFixture(api *fixtureExchangeAPI) {
	instrument := bybit.Instrument{Symbol: "ETHUSDC", BaseCoin: "ETH", QuoteCoin: "USDC", Status: "Trading"}
	instrument.LotSizeFilter.BasePrecision = "0.000001"
	instrument.LotSizeFilter.QuotePrecision = "0.01"
	instrument.LotSizeFilter.MinOrderAmt = "5"
	instrument.LotSizeFilter.MaxMarketOrderQty = "100"
	instrument.PriceFilter.TickSize = "0.01"
	api.instrument = &instrument
	api.executions = func(symbol, link, cursor string) (bybit.ExecutionPage, error) {
		fee, coin := "0.2", "USDC"
		if api.order.Side == "Buy" {
			fee, coin = "0.0001", "ETH"
		}
		return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{{OrderID: "remote-order-1", OrderLinkID: link, Symbol: symbol, Side: api.order.Side, OrderType: "Market", ExecID: "usdc-fill-1", ExecQty: "0.1", ExecValue: "200", ExecPrice: "2000", ExecFee: fee, FeeCurrency: coin, ExtraFees: json.RawMessage(`""`)}}}, nil
	}
}

func TestUSDCQuoteBuySellAndIndependentlyApprovedFunding(t *testing.T) {
	for _, side := range []string{"Buy", "Sell"} {
		for _, needsFunding := range []bool{false, true} {
			t.Run(side+map[bool]string{false: "/funded", true: "/transfer"}[needsFunding], func(t *testing.T) {
				c, api, _, path := fixtureExchangeCoordinator(t)
				configureUSDCFixture(api)
				spend, value, feeCoin, fee := "ETH", "0.1", "USDC", "0.2"
				if side == "Buy" {
					spend, value, feeCoin, fee = "USDC", "200", "ETH", "0.0001"
				}
				api.balances["FUND:"+spend] = "1000"
				api.balances["UNIFIED:"+spend] = "1000"
				if needsFunding {
					api.balances["UNIFIED:"+spend] = "0"
				}
				out, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": side, "quoteCoin": "USDC"}))
				if err != nil {
					t.Fatal(err)
				}
				q := out.(exchangeQuote)
				if q.Symbol != "ETHUSDC" || q.QuoteCoin != "USDC" || exchangeAvailable(q.Balances, "FUND", "USDC") == "" || exchangeAvailable(q.Balances, "FUND", "USDT") != "" {
					t.Fatal("quote did not retain selected quote and balances", q)
				}
				result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": side, "quoteCoin": "USDC", "amount": value, "slippage": "0.5"})
				if result.State != "CONFIRMED" || result.Fees[feeCoin] != fee || api.order.Symbol != "ETHUSDC" || api.order.Side != side || api.order.Qty != value {
					t.Fatal("USDC fill or exact intent differs", result, api.order)
				}
				state := readExchangeFixtureState(path)
				ops := state["exchangeOps"].([]any)
				want := 1
				if needsFunding {
					want = 2
					if api.transfer.Coin != spend || api.transfer.Amount != value || api.transfer.FromAccountType != "FUND" || api.transfer.ToAccountType != "UNIFIED" || ops[0].(map[string]any)["kind"] != "transfer" {
						t.Fatal("funding changed coin/amount or lacked separate durable operation", api.transfer)
					}
				}
				if len(ops) != want || api.posts != want {
					t.Fatal("funding/trade approval identities were collapsed")
				}
				approvals := 0
				for _, call := range state["calls"].([]any) {
					if call == "exchangeAuthorize" {
						approvals++
					}
				}
				if approvals != want {
					t.Fatal("USDC transfer authorized the spot trade implicitly")
				}
				trade := ops[len(ops)-1].(map[string]any)
				if trade["feeCoin"] != feeCoin || trade["fee"] != fee {
					t.Fatal("fee estimate uses wrong received coin", trade)
				}
			})
		}
	}
}

func TestUSDCInvalidQuoteOrMismatchedInstrumentNeverPosts(t *testing.T) {
	for _, selected := range []any{"DAI", "BTC", "usdc", "USDC ", nil, 1, []string{"USDC"}} {
		c, api, _, _ := fixtureExchangeCoordinator(t)
		configureUSDCFixture(api)
		for _, method := range []string{"exchangeQuote", "exchangeSwap"} {
			body := map[string]any{"side": "Buy", "quoteCoin": selected}
			if method == "exchangeSwap" {
				body["amount"], body["slippage"] = "200", "0.5"
			}
			if _, err := c.handle(exchangeMessage(method, body)); err == nil || api.posts != 0 {
				t.Fatal("invalid selected quote accepted", selected, method)
			}
		}
	}
	c, api, _, _ := fixtureExchangeCoordinator(t)
	configureUSDCFixture(api)
	api.instrument.QuoteCoin = "USDT"
	if _, err := c.handle(exchangeMessage("exchangeSwap", map[string]any{"side": "Buy", "quoteCoin": "USDC", "amount": "200", "slippage": "0.5"})); err == nil || api.posts != 0 {
		t.Fatal("mismatched provider quote authorized")
	}
	api.instrument.QuoteCoin = "USDC"
	api.fail["instrument"] = bybit.ErrNotListed
	value, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Buy", "quoteCoin": "USDC"}))
	if err != nil || value.(exchangeQuote).Symbol != "" || api.posts != 0 {
		t.Fatal("unlisted USDC pair invented or performed a write")
	}
}

func TestUSDCWrongFeeCurrencyAndAmbiguousRestartRemainUnresolved(t *testing.T) {
	for _, fault := range []string{"wrong-fee", "wrong-tax", "lost-ack"} {
		t.Run(fault, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			configureUSDCFixture(api)
			if fault == "lost-ack" {
				api.fail["/v5/order/create"] = errors.New("synthetic lost acknowledgment")
			} else {
				valid := api.executions
				api.executions = func(symbol, link, cursor string) (bybit.ExecutionPage, error) {
					page, err := valid(symbol, link, cursor)
					if fault == "wrong-fee" {
						page.List[0].FeeCurrency = "USDT"
					} else {
						page.List[0].ExtraFees = json.RawMessage(`[{"feeCoin":"USDT","feeType":"VAT","subFeeType":"ARE_VAT","feeRate":"0.05","fee":"0.01"}]`)
					}
					return page, err
				}
			}
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "quoteCoin": "USDC", "amount": "0.1", "slippage": "0.5"})
			if result.State != "UNKNOWN" || api.posts != 1 {
				t.Fatal("incomplete USDC outcome was called complete", result)
			}
			c.child.Kill()
			value, err := c.handle(message("exchangeReconcile", map[string]any{"id": result.ID}))
			if err != nil {
				t.Fatal("original USDC operation was not retained on child restart", err)
			}
			if fault != "lost-ack" && value.(exchangeResult).State != "UNKNOWN" {
				t.Fatal("restart discarded incomplete fee evidence")
			}
			ops := readExchangeFixtureState(path)["exchangeOps"].([]any)
			if len(ops) != 1 || !strings.Contains(ops[0].(map[string]any)["payload"].(string), `ETHUSDC`) || api.posts != 1 {
				t.Fatal("restart replaced original USDC request")
			}
		})
	}
}

func TestUSDCReceivedTaxAccounting(t *testing.T) {
	api := newFixtureExchangeAPI()
	configureUSDCFixture(api)
	api.order = bybit.SpotOrder{Symbol: "ETHUSDC", Side: "Sell", OrderType: "Market", Qty: "0.1", OrderLinkID: "usdc-original"}
	valid := api.executions
	api.executions = func(symbol, link, cursor string) (bybit.ExecutionPage, error) {
		page, err := valid(symbol, link, cursor)
		page.List[0].ExtraFees = json.RawMessage(`[{"feeCoin":"USDC","feeType":"VAT","subFeeType":"ARE_VAT","feeRate":"0.05","fee":"0.01"}]`)
		return page, err
	}
	order := bybit.Order{OrderID: "remote-order-1", CumExecQty: "0.1", CumExecValue: "200", OrderStatus: "Filled"}
	base, quote, fees, err := verifiedExchangeExecutions(context.Background(), api, api.order, order)
	if err != nil || base != "0.1" || quote != "200" || fees["USDC"] != "0.21" || fees["USDT"] != "" {
		t.Fatal("USDC execution tax was omitted or charged in wrong currency", fees, err)
	}
}
