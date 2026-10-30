package walletcore

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
)

func TestNativeExchangeIOCPriceRoundingAndWholeSpendingCeiling(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		for _, side := range []string{"Buy", "Sell"} {
			t.Run(provider+side, func(t *testing.T) {
				quote := exchangeQuote{Provider: provider, Coin: "ETH", QuoteCoin: "USDT", Side: side, BidPrice: "1999.987", AskPrice: "2001.013", BasePrecision: "0.00001", PricePrecision: "0.01", FeeRate: "0.001", MinOrderAmount: "5", MaxOrderQty: "100"}
				value, unit := "0.12345", "baseCoin"
				if side == "Buy" {
					value, unit = "100", "quoteCoin"
				}
				original := bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: side, OrderType: "Market", Qty: value, MarketUnit: unit, SlippageToleranceType: "Percent", SlippageTolerance: "0.5"}
				order, fee, feeCoin, err := exchangeIOCOrder(quote, original)
				if err != nil {
					t.Fatal(err)
				}
				if order.Qty != value || order.OrderType != "Limit" || order.Side != side {
					t.Fatal("IOC changed original budget")
				}
				order.OrderLinkID = "test-original-identity"
				if exchange.ValidateIOCOrder(order) != nil {
					t.Fatal("IOC violates signer policy")
				}
				price, _ := exchangeAtomic(order.LimitPrice)
				observedText := quote.BidPrice
				factor := int64(9950)
				if side == "Buy" {
					observedText = quote.AskPrice
					factor = 10050
				}
				observed, _ := exchangeAtomic(observedText)
				bound := new(big.Int).Mul(observed, big.NewInt(factor))
				actual := new(big.Int).Mul(price, big.NewInt(10000))
				if side == "Buy" && actual.Cmp(bound) > 0 || side == "Sell" && actual.Cmp(bound) < 0 {
					t.Fatal("tick rounding exceeded selected slippage")
				}
				if side == "Sell" && order.BaseQty != value {
					t.Fatal("sell quantity changed")
				}
				if side == "Buy" {
					base, _ := exchangeAtomic(order.BaseQty)
					spent := new(big.Int).Mul(base, price)
					spent.Div(spent, exchangeScale())
					budget, _ := exchangeAtomic(value)
					if provider == "kucoin" {
						feeN, _ := exchangeAtomic(fee)
						spent.Add(spent, feeN)
						if feeCoin != "USDT" {
							t.Fatal("KuCoin quote fee not reserved")
						}
					} else if feeCoin != "ETH" {
						t.Fatal("Binance normal received-asset fee changed")
					}
					if spent.Cmp(budget) > 0 {
						t.Fatal("buy exceeded original quote budget including input fee")
					}
				}
			})
		}
	}
}

func TestNativeExchangeIOCRejectsUnusableQuoteBeforeAnyProviderRequest(t *testing.T) {
	quote := exchangeQuote{Provider: "binance", Coin: "ETH", QuoteCoin: "USDT", Side: "Buy", BidPrice: "2000", AskPrice: "2001", BasePrecision: "0.00001", PricePrecision: "0.01", FeeRate: "0.001", MinOrderAmount: "5", MaxOrderQty: "100"}
	order := bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Buy", OrderType: "Market", Qty: "100", MarketUnit: "quoteCoin", SlippageToleranceType: "Percent", SlippageTolerance: "1"}
	for _, change := range []func(*exchangeQuote, *bybit.SpotOrder){func(q *exchangeQuote, o *bybit.SpotOrder) { q.PricePrecision = "" }, func(q *exchangeQuote, o *bybit.SpotOrder) { q.BasePrecision = "0" }, func(q *exchangeQuote, o *bybit.SpotOrder) { q.AskPrice = "unknown" }, func(q *exchangeQuote, o *bybit.SpotOrder) { q.FeeRate = "1" }, func(q *exchangeQuote, o *bybit.SpotOrder) { o.Qty = "0.0000000001" }, func(q *exchangeQuote, o *bybit.SpotOrder) { o.SlippageTolerance = "1.01" }, func(q *exchangeQuote, o *bybit.SpotOrder) { o.Side = "arbitrary" }} {
		q, o := quote, order
		change(&q, &o)
		if _, _, _, err := exchangeIOCOrder(q, o); err == nil {
			t.Fatal("unusable policy constructed")
		}
	}
}

func TestExchangeProviderIntentCannotFollowAnUnacknowledgedSelection(t *testing.T) {
	for _, selected := range []string{"bybit", "binance", "kucoin"} {
		for _, requested := range []string{"", "bybit", "binance", "kucoin", "unknown"} {
			want := requested == selected || selected == "bybit" && requested == ""
			if exchangeProviderMatches(requested, selected) != want {
				t.Fatal("stale provider intent accepted", requested, selected)
			}
		}
	}
}

func TestNativeExchangeFillPricesAndInputFeesRetainOriginalSpendingBudget(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		api := newFixtureExchangeAPI()
		api.endpoint = "https://api." + provider + ".com"
		request := bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Buy", OrderType: "Limit", Qty: "100", BaseQty: "0.04", LimitPrice: "2500", MarketUnit: "quoteCoin", OrderLinkID: "native-exact-original"}
		feeCoin, fee := "ETH", "0.00004"
		quoteValue := "100"
		if provider == "kucoin" {
			feeCoin, fee, quoteValue = "USDT", "0.1", "99.9"
		}
		fillPrice := "2500"
		if provider == "kucoin" {
			fillPrice = "2497.5"
		}
		fill := bybit.Execution{OrderID: "original-remote", OrderLinkID: request.OrderLinkID, Symbol: request.Symbol, Side: "Buy", OrderType: "Limit", OrderQty: "0.04", ExecID: "exact-fill", ExecQty: "0.04", ExecValue: quoteValue, ExecPrice: fillPrice, ExecFee: fee, FeeCurrency: feeCoin, ExtraFees: json.RawMessage(`[]`)}
		api.executions = func(string, string, string) (bybit.ExecutionPage, error) {
			return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{fill}}, nil
		}
		remote := bybit.Order{OrderID: "original-remote", OrderStatus: "Filled", CumExecQty: "0.04", CumExecValue: quoteValue}
		if _, _, _, err := verifiedExchangeExecutions(context.Background(), api, request, remote); err != nil {
			t.Fatal("valid native exact records rejected", provider, err)
		}
		fill.ExecPrice = "2500.01"
		if _, _, _, err := verifiedExchangeExecutions(context.Background(), api, request, remote); err == nil {
			t.Fatal("fill beyond approved limit accepted")
		}
		fill.ExecPrice = fillPrice
		fill.FeeCurrency = "BNB"
		if _, _, _, err := verifiedExchangeExecutions(context.Background(), api, request, remote); err == nil {
			t.Fatal("unapproved third-asset fee accepted")
		}
		fill.FeeCurrency = feeCoin
		if provider == "kucoin" {
			fill.ExecFee = "0.11"
			if _, _, _, err := verifiedExchangeExecutions(context.Background(), api, request, remote); err == nil {
				t.Fatal("input fee exceeded exact spending budget")
			}
		}
	}
}

func TestNativeExchangeWithdrawalAdditionalFeeEstimateKeepsAPIAmountExact(t *testing.T) {
	fee, err := exchangeNativeWithdrawalFee("1", "0.01", "0.005", 4)
	if err != nil || fee != "0.015" {
		t.Fatal("native additional fee estimate", fee, err)
	}
	for _, values := range [][3]string{{"0", "0.01", "0"}, {"1", "unknown", "0"}, {"1", "0", "1"}} {
		if _, err := exchangeNativeWithdrawalFee(values[0], values[1], values[2], 18); err == nil {
			t.Fatal("unavailable fee accepted")
		}
	}
}
