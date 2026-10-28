package signer

import (
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestUSDCExactPrivateApprovalTamperAndRestart(t *testing.T) {
	for _, side := range []string{"Sell", "Buy"} {
		t.Run(side, func(t *testing.T) {
			e, ui, account := exchangeEngine(t)
			intent := exchangeSwap(account)
			intent.Order.Symbol, intent.Order.Side = "ETHUSDC", side
			spend := "0.012345678912345678 ETH"
			intent.FeeCoin = "USDC"
			if side == "Buy" {
				intent.Order.MarketUnit, intent.Order.Qty = "quoteCoin", "123.45"
				intent.FeeCoin = "ETH"
				spend = "123.45 USDC"
			}
			op, err := e.PrepareExchange(intent)
			if err != nil {
				t.Fatal("exact USDC operation denied", err)
			}
			op = authorizeExchange(t, e, ui, op)
			summary := ui.summaries[len(ui.summaries)-1]
			for _, want := range []string{side + " ETHUSDC", spend, "2500.1234 USDC per ETH", "Estimated trading fee: 0.001 " + intent.FeeCoin, "Margin or borrowing: disabled", account.Address} {
				if !strings.Contains(summary, want) {
					t.Fatal("private approval omitted exact quote/spend/fee identity", want)
				}
			}
			for _, mutate := range []func(*bybit.AuthRequest){
				func(r *bybit.AuthRequest) { r.Payload = strings.Replace(r.Payload, "ETHUSDC", "ETHUSDT", 1) },
				func(r *bybit.AuthRequest) { r.Payload = strings.Replace(r.Payload, "ETHUSDC", "TONUSDC", 1) },
				func(r *bybit.AuthRequest) {
					r.Payload = strings.Replace(r.Payload, `"qty":"`+intent.Order.Qty+`"`, `"qty":"999"`, 1)
				},
			} {
				r := exchangeRequest(op)
				mutate(&r)
				if _, err := e.ExchangeAuth(r); err == nil {
					t.Fatal("changed quote/base/amount reused USDC approval")
				}
			}
			r := exchangeRequest(op)
			auth, err := e.SignExchangeRequest(r)
			if err != nil {
				t.Fatal(err)
			}
			assertExchangeHMAC(t, r, auth)
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal(err)
			}
			restored, err := e.ExchangeOperation(op.ID)
			if err != nil || restored.State != "SUBMITTING" || restored.Payload != op.Payload || restored.Digest != op.Digest || restored.FeeCoin != intent.FeeCoin {
				t.Fatal("encrypted restart changed USDC operation or fee identity")
			}
			if _, err := e.ExchangeAuth(r); err == nil {
				t.Fatal("restart replayed USDC write")
			}
			if _, err := e.PrepareExchange(intent); err == nil {
				t.Fatal("ambiguous USDC request allowed replacement")
			}
		})
	}
}

func TestUSDCPrepareRestrictsBaseQuoteFeesAndFunding(t *testing.T) {
	e, ui, account := exchangeEngine(t)
	for _, fault := range []string{"foreign-base", "foreign-quote", "same-coin", "wrong-received-fee", "buy-input-fee"} {
		intent := exchangeSwap(account)
		intent.Order.Symbol, intent.FeeCoin = "ETHUSDC", "USDC"
		switch fault {
		case "foreign-base":
			intent.Order.Symbol = "TONUSDC"
		case "foreign-quote":
			intent.Order.Symbol = "ETHDAI"
		case "same-coin":
			intent.Order.Symbol = "USDCUSDC"
		case "wrong-received-fee":
			intent.FeeCoin = "USDT"
		case "buy-input-fee":
			intent.Order.Side, intent.Order.MarketUnit = "Buy", "quoteCoin"
		}
		if _, err := e.PrepareExchange(intent); err == nil {
			t.Fatal("invalid USDC authority accepted", fault)
		}
	}
	intent := ExchangeIntent{Kind: "transfer", AccountID: account.ID, AssetID: account.Network + ":native", Fee: "0", FeeCoin: "USDC", Transfer: &bybit.TransferRequest{Coin: "USDC", Amount: "123.45", FromAccountType: "FUND", ToAccountType: "UNIFIED"}}
	op, err := e.PrepareExchange(intent)
	if err != nil {
		t.Fatal("USDC funding denied", err)
	}
	op = authorizeExchange(t, e, ui, op)
	if !strings.Contains(ui.summaries[len(ui.summaries)-1], "123.45 USDC") {
		t.Fatal("funding approval hid exact USDC quantity")
	}
	if _, err := e.SignExchangeRequest(exchangeRequest(op)); err != nil {
		t.Fatal(err)
	}
}
