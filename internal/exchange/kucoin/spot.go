package kucoin

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"strconv"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) CreateOrder(context.Context, bybit.SpotOrder) (bybit.OrderAcknowledgement, error) {
	// There is no reviewed Classic read-only witness for the global KCS fee
	// payment setting. Do not place an order with unapproved third-asset costs.
	return bybit.OrderAcknowledgement{}, ErrFeeCurrency
}
func exactNumber(value string) (*big.Rat, bool) {
	if !decimal(value, false) {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(value)
	return v, ok
}
func (c *Client) FindOrder(ctx context.Context, symbol, clientID string) (*bybit.Order, error) {
	native, ok := nativeSymbol(symbol)
	if !ok || !identifier(clientID, 36) {
		return nil, ErrRequest
	}
	var raw struct {
		ID          string `json:"id"`
		Symbol      string `json:"symbol"`
		ClientID    string `json:"clientOid"`
		Type        string `json:"type"`
		Side        string `json:"side"`
		TradeType   string `json:"tradeType"`
		TIF         string `json:"timeInForce"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		Filled      string `json:"dealSize"`
		Funds       string `json:"dealFunds"`
		Fee         string `json:"fee"`
		FeeCurrency string `json:"feeCurrency"`
		Tax         string `json:"tax"`
		Active      *bool  `json:"active"`
		Cancelled   *bool  `json:"cancelExist"`
		Created     *int64 `json:"createdAt"`
		Updated     *int64 `json:"lastUpdatedAt"`
	}
	if e := c.get(ctx, "/api/v1/hf/orders/client-order/"+clientID, url.Values{"symbol": {native}}, true, &raw); e != nil {
		return nil, e
	}
	if !identifier(raw.ID, 128) || raw.Symbol != native || raw.ClientID != clientID || raw.Type != "limit" || raw.TIF != "IOC" || raw.TradeType != "TRADE" || normalizedSide(raw.Side) == "" || !decimal(raw.Price, true) || !decimal(raw.Size, true) || !decimal(raw.Filled, false) || !decimal(raw.Funds, false) || !decimal(raw.Fee, false) || !coin(raw.FeeCurrency) || !decimal(raw.Tax, false) || raw.Active == nil || raw.Cancelled == nil || raw.Created == nil || raw.Updated == nil || *raw.Created <= 0 || *raw.Updated < *raw.Created {
		return nil, ErrResponse
	}
	filled, _ := exactNumber(raw.Filled)
	size, _ := exactNumber(raw.Size)
	if filled.Cmp(size) > 0 {
		return nil, ErrResponse
	}
	status := "New"
	if *raw.Active {
		if filled.Sign() > 0 {
			status = "PartiallyFilled"
		}
	} else if *raw.Cancelled {
		status = "Cancelled"
		if filled.Sign() > 0 {
			status = "PartiallyFilledCanceled"
		}
	} else if filled.Cmp(size) == 0 {
		status = "Filled"
	} else {
		return nil, ErrResponse
	}
	return &bybit.Order{OrderID: raw.ID, OrderLinkID: clientID, Symbol: symbol, Side: normalizedSide(raw.Side), OrderType: "Limit", Qty: raw.Size, BaseQty: raw.Size, LimitPrice: raw.Price, TimeInForce: "IOC", MarketUnit: "baseCoin", IsLeverage: "0", OrderStatus: status, CumExecQty: raw.Filled, CumExecValue: raw.Funds, CumExecFee: raw.Fee, CreatedTime: strconv.FormatInt(*raw.Created, 10), UpdatedTime: strconv.FormatInt(*raw.Updated, 10)}, nil
}
func (c *Client) Executions(ctx context.Context, symbol, clientID, cursor string) (bybit.ExecutionPage, error) {
	if cursor != "" && !canonicalUint(cursor) {
		return bybit.ExecutionPage{}, ErrRequest
	}
	order, e := c.FindOrder(ctx, symbol, clientID)
	if e != nil {
		return bybit.ExecutionPage{}, e
	}
	if order == nil {
		return bybit.ExecutionPage{}, ErrResponse
	}
	native, _ := nativeSymbol(symbol)
	q := url.Values{"symbol": {native}, "orderId": {order.OrderID}, "limit": {"100"}}
	if cursor != "" {
		q.Set("lastId", cursor)
	}
	var raw struct {
		LastID *uint64           `json:"lastId"`
		Items  []json.RawMessage `json:"items"`
	}
	if e = c.get(ctx, "/api/v1/hf/fills", q, true, &raw); e != nil {
		return bybit.ExecutionPage{}, e
	}
	if raw.Items == nil || len(raw.Items) > 100 || raw.LastID == nil {
		return bybit.ExecutionPage{}, ErrResponse
	}
	result := bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{}}
	seen := map[uint64]bool{}
	var previous uint64
	for index, r := range raw.Items {
		var fill struct {
			ID          *uint64 `json:"id"`
			OrderID     string  `json:"orderId"`
			Symbol      string  `json:"symbol"`
			Side        string  `json:"side"`
			Type        string  `json:"type"`
			TradeType   string  `json:"tradeType"`
			Price       string  `json:"price"`
			Size        string  `json:"size"`
			Funds       string  `json:"funds"`
			Fee         string  `json:"fee"`
			FeeCurrency string  `json:"feeCurrency"`
			Tax         string  `json:"tax"`
			TaxRate     string  `json:"taxRate"`
			Created     *int64  `json:"createdAt"`
		}
		if json.Unmarshal(r, &fill) != nil || fill.ID == nil || *fill.ID == 0 || seen[*fill.ID] || index > 0 && *fill.ID >= previous || fill.OrderID != order.OrderID || fill.Symbol != native || normalizedSide(fill.Side) != order.Side || fill.Type != "limit" || fill.TradeType != "TRADE" || !decimal(fill.Price, true) || !decimal(fill.Size, true) || !decimal(fill.Funds, true) || !decimal(fill.Fee, false) || !coin(fill.FeeCurrency) || !decimal(fill.Tax, false) || !decimal(fill.TaxRate, false) || fill.Created == nil || *fill.Created <= 0 {
			return bybit.ExecutionPage{}, ErrResponse
		}
		if cursor != "" {
			v, _ := strconv.ParseUint(cursor, 10, 64)
			if *fill.ID >= v {
				return bybit.ExecutionPage{}, ErrResponse
			}
		}
		seen[*fill.ID] = true
		previous = *fill.ID
		// A native tax is not a Bybit extraFees type. Until separately modeled,
		// keep the original result unresolved instead of hiding or relabeling it.
		extra := json.RawMessage(`[]`)
		tax, _ := exactNumber(fill.Tax)
		if tax.Sign() > 0 {
			return bybit.ExecutionPage{}, ErrResponse
		}
		result.List = append(result.List, bybit.Execution{Symbol: symbol, OrderID: order.OrderID, OrderLinkID: clientID, Side: order.Side, OrderQty: order.BaseQty, OrderType: "Limit", ExecID: strconv.FormatUint(*fill.ID, 10), ExecPrice: fill.Price, ExecQty: fill.Size, ExecValue: fill.Funds, ExecFee: fill.Fee, FeeCurrency: fill.FeeCurrency, ExecTime: strconv.FormatInt(*fill.Created, 10), ExtraFees: extra})
	}
	if len(raw.Items) > 0 && *raw.LastID != previous {
		return bybit.ExecutionPage{}, ErrResponse
	}
	if len(raw.Items) == 100 {
		result.NextPageCursor = strconv.FormatUint(*raw.LastID, 10)
		if result.NextPageCursor == cursor {
			return bybit.ExecutionPage{}, ErrResponse
		}
	}
	return result, nil
}
func canonicalUint(value string) bool {
	v, e := strconv.ParseUint(value, 10, 64)
	return e == nil && strconv.FormatUint(v, 10) == value
}
