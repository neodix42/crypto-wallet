package main

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/vault"
)

func TestExchangeDecimalBoundsCannotRepresentOverflowAsZero(t *testing.T) {
	for _, bad := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		if text, err := exchangeDecimal(bad); err == nil || text != "" {
			t.Fatal("invalid or overflowing aggregate produced a monetary value")
		}
	}
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	text, err := exchangeDecimal(maximum)
	if err != nil {
		t.Fatal("valid uint256 maximum rejected")
	}
	atomic, err := amount.Parse(text, 36)
	if err != nil || atomic != maximum.String() {
		t.Fatal("maximum exact decimal changed during formatting")
	}
	for _, row := range []struct {
		atomic int64
		want   string
	}{{0, "0"}, {1, "0.000000000000000000000000000000000001"}, {1000, "0.000000000000000000000000000000001"}} {
		text, err := exchangeDecimal(big.NewInt(row.atomic))
		if err != nil || text != row.want {
			t.Fatal("small valid amount changed")
		}
	}
}

func TestExchangeFeeEstimateRejectsScaledOverflowAndPreservesWideProducts(t *testing.T) {
	huge := "1" + strings.Repeat("0", 40)
	quote := exchangeQuote{Coin: "ETH", LastPrice: huge, FeeRate: "0.5"}
	if fee, coin, err := exchangeFeeEstimate(quote, huge, "Sell"); err == nil || fee != "" || coin != "" {
		t.Fatal("scaled fee overflow was displayed as zero")
	}
	quote.LastPrice = "0.000000000000000000000000000000000001"
	if fee, coin, err := exchangeFeeEstimate(quote, huge, "Buy"); err == nil || fee != "" || coin != "" {
		t.Fatal("buy fee overflow was displayed as zero")
	}
	quote.LastPrice = "2000"
	for _, side := range []string{"", "Margin", "sell"} {
		if _, _, err := exchangeFeeEstimate(quote, "1", side); err == nil {
			t.Fatal("unknown direction was interpreted as a buy")
		}
	}
	if _, _, err := exchangeFeeEstimate(quote, "0", "Sell"); err == nil {
		t.Fatal("zero spending accepted for fee estimate")
	}
	// The exact intermediate product exceeds uint256, but its scaled monetary
	// result is representable. It must not be rejected merely for using big.Int.
	quote.LastPrice = "10000000000"
	quote.FeeRate = "0.00000001"
	fee, coin, err := exchangeFeeEstimate(quote, "1"+strings.Repeat("0", 30), "Sell")
	if err != nil || coin != "USDT" || fee != "1"+strings.Repeat("0", 32) {
		t.Fatal("representable scaled fee rejected after wide intermediate multiplication")
	}
	quote.LastPrice = huge
	quote.FeeRate = "0.5"
	fee, coin, err = exchangeFeeEstimate(quote, huge, "Buy")
	if err != nil || coin != "ETH" || fee != "0.5" {
		t.Fatal("representable buy fee lost exact precision")
	}
}

func TestExchangeOverflowingExecutionAggregatesStayUnknownAndRetainOriginalOperation(t *testing.T) {
	huge := "6" + strings.Repeat("0", 40)
	for _, scenario := range []string{"base", "quote", "fee"} {
		t.Run(scenario, func(t *testing.T) {
			request := fixtureRemoteOrder()
			order := fixtureOrderRecord(request)
			first := fixtureExecution(request)
			first.ExecFee = "0"
			order.OrderStatus = "Cancelled"
			switch scenario {
			case "base":
				request.Side = "Buy"
				request.MarketUnit = "quoteCoin"
				request.Qty = "120000"
				first.Side = "Buy"
				first.FeeCurrency = "ETH"
				first.ExecQty = huge
				first.ExecValue = "60000"
				first.ExecPrice = "0.000000000000000000000000000000000001"
				order.Side = "Buy"
				order.MarketUnit = "quoteCoin"
				order.Qty = request.Qty
				order.CumExecQty = "0"
				order.CumExecValue = "120000"
			case "quote":
				request.Qty = "2"
				order.Qty = "2"
				order.CumExecQty = "2"
				order.CumExecValue = "0"
				first.ExecQty = "1"
				first.ExecValue = huge
				first.ExecPrice = huge
			case "fee":
				request.Qty = "2"
				order.Qty = "2"
				order.CumExecQty = "2"
				order.CumExecValue = "8" + strings.Repeat("0", 40)
				first.ExecQty = "1"
				first.ExecValue = "4" + strings.Repeat("0", 40)
				first.ExecPrice = first.ExecValue
				first.ExecFee = huge
			}
			second := first
			second.ExecID = "fill-2"
			fills := []bybit.Execution{first, second}
			c, api, _, path := fixtureExchangeCoordinator(t)
			api.orderResult = &order
			api.executions = func(string, string, string) (bybit.ExecutionPage, error) {
				return bybit.ExecutionPage{Category: "spot", List: fills}, nil
			}
			if base, quote, fees, err := verifiedExchangeExecutions(context.Background(), api, request, order); err == nil || base != "" || quote != "" || fees != nil {
				t.Fatal("overflowing execution aggregate was accepted or represented as zero")
			}
			payload, _ := json.Marshal(request)
			op := vault.ExchangeOperation{ID: request.OrderLinkID, Kind: "swap", Payload: string(payload), State: "PENDING", ExternalID: order.OrderID, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix()}
			storeFixtureOperation(t, path, op)
			result, err := c.reconcileExchangeAt(0, api, op)
			if err != nil || result.State != "UNKNOWN" || result.ID != op.ID || result.FilledBase != "" || result.FilledQuote != "" || result.Fees != nil {
				t.Fatal("overflow released the operation or reported a false execution")
			}
			state := readExchangeFixtureState(path)
			operations := state["exchangeOps"].([]any)
			if len(operations) != 1 || operations[0].(map[string]any)["state"] != "UNKNOWN" || operations[0].(map[string]any)["id"] != op.ID || api.posts != 0 {
				t.Fatal("overflow lost the original uncertain request or created another payment")
			}
		})
	}
}
