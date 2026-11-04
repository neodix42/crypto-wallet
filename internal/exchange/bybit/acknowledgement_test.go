package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSpotBuyReadPreservesDocumentedQuoteUnitAndCurrentFeeDetailSchema(t *testing.T) {
	// The current API defines qty by marketUnit; cumulative executed base and
	// quote amounts are separate. Deprecated cumExecFee may be absent.
	row := map[string]any{"orderId": "public-gram-order", "orderLinkId": "public-link", "symbol": "GRAMUSDT", "side": "Buy", "orderType": "Market", "qty": "5", "marketUnit": "quoteCoin", "isLeverage": "0", "orderStatus": "Filled", "cumExecQty": "4.25", "cumExecValue": "5", "avgPrice": "1.176470588", "cumFeeDetail": map[string]string{"GRAM": "0.00425"}}
	for _, missing := range []string{"", "marketUnit", "isLeverage", "qty", "orderLinkId"} {
		t.Run(missing, func(t *testing.T) {
			copy := map[string]any{}
			for key, value := range row {
				copy[key] = value
			}
			delete(copy, missing)
			raw, _ := json.Marshal(map[string]any{"category": "spot", "list": []any{copy}, "nextPageCursor": ""})
			order, err := testClient(t, string(raw)).FindOrder(context.Background(), "GRAMUSDT", "public-link")
			if missing != "" {
				if !errors.Is(err, ErrInvalidResponse) || order != nil {
					t.Fatal("missing critical spot identity/unit was silently inferred", missing, order, err)
				}
				return
			}
			if err != nil || order.Qty != "5" || order.MarketUnit != "quoteCoin" || order.CumExecQty != "4.25" || order.CumExecValue != "5" || order.CumExecFee != "" {
				t.Fatal("documented quote-unit or current fee schema was changed", order, err)
			}
		})
	}
}

func TestTransferStatusReadKeepsExactUUIDDirectionAndAllowsUnrelatedMetadata(t *testing.T) {
	row := map[string]any{"transferId": fixtureUUID, "coin": "USDT", "amount": "5", "fromAccountType": "FUND", "toAccountType": "UNIFIED", "timestamp": "1793788800000", "status": "SUCCESS", "fromMemberId": 123, "toMemberId": "123"}
	for _, missing := range []string{"", "transferId", "coin", "amount", "fromAccountType", "toAccountType", "status"} {
		t.Run(missing, func(t *testing.T) {
			copy := map[string]any{}
			for key, value := range row {
				copy[key] = value
			}
			delete(copy, missing)
			raw, _ := json.Marshal(map[string]any{"list": []any{copy}, "nextPageCursor": ""})
			result, err := testClient(t, string(raw)).TransferStatus(context.Background(), fixtureUUID)
			if missing != "" {
				if !errors.Is(err, ErrInvalidResponse) || result != nil {
					t.Fatal("missing transfer identity/status became success", missing, result, err)
				}
				return
			}
			if err != nil || result.TransferID != fixtureUUID || result.Coin != "USDT" || result.Amount != "5" || result.FromAccountType != "FUND" || result.ToAccountType != "UNIFIED" || result.Status != "SUCCESS" {
				t.Fatal("exact transfer response schema rejected or reinterpreted", result, err)
			}
		})
	}
}

func TestOrderReadChecksHistoricalExactLinkAfterRealtimeRecordIsNotVisible(t *testing.T) {
	client := testClient(t, "{}")
	paths := []string{}
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Path)
		if request.Method != "GET" || request.URL.Query().Get("category") != "spot" || request.URL.Query().Get("symbol") != "ETHUSDT" || request.URL.Query().Get("orderLinkId") != "fixtureOrder1" {
			t.Fatal("fallback changed exact spot request identity or attempted a write")
		}
		if request.URL.Path == "/v5/order/realtime" {
			return resultResponse(`{"category":"spot","list":[],"nextPageCursor":""}`), nil
		}
		return resultResponse(methodFixtures()[8].result), nil
	})
	order, err := client.FindOrder(context.Background(), "ETHUSDT", "fixtureOrder1")
	if err != nil || order == nil || strings.Join(paths, ",") != "/v5/order/realtime,/v5/order/history" {
		t.Fatal("eventually visible exact history lookup failed", order, err, paths)
	}
}
