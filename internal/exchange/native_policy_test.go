package exchange

import (
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestNativeIOCOriginalSpendingCeilingUsesExactPriceTimesQuantity(t *testing.T) {
	order := bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Buy", OrderType: "Limit", Qty: "100", MarketUnit: "quoteCoin", SlippageToleranceType: "Percent", SlippageTolerance: "1", OrderLinkID: "exact-original-id", LimitPrice: "2500", BaseQty: "0.04"}
	if ValidateIOCOrder(order) != nil {
		t.Fatal("exact 100 USDT ceiling rejected")
	}
	order.BaseQty = "0.040000000000000000000000000000000001"
	if ValidateIOCOrder(order) == nil {
		t.Fatal("unsafe native order was prepared")
	}
	order.BaseQty, order.Qty = "0.04", "99.999999999999999999999999999999999999"
	if ValidateIOCOrder(order) == nil {
		t.Fatal("unsafe native order was prepared")
	}
}
