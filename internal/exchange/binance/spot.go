package binance

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) SpotInstrument(ctx context.Context, symbol string) (bybit.Instrument, error) {
	var raw struct {
		Symbols []struct {
			Symbol         string            `json:"symbol"`
			Status         string            `json:"status"`
			Base           string            `json:"baseAsset"`
			Quote          string            `json:"quoteAsset"`
			QuotePrecision *uint8            `json:"quoteAssetPrecision"`
			Spot           *bool             `json:"isSpotTradingAllowed"`
			OrderTypes     []string          `json:"orderTypes"`
			Filters        []json.RawMessage `json:"filters"`
		} `json:"symbols"`
	}
	if e := c.get(ctx, "/api/v3/exchangeInfo", url.Values{"symbol": {symbol}}, false, &raw); e != nil {
		return bybit.Instrument{}, e
	}
	if raw.Symbols != nil && len(raw.Symbols) == 0 {
		return bybit.Instrument{}, bybit.ErrNotListed
	}
	if len(raw.Symbols) != 1 {
		return bybit.Instrument{}, ErrInvalidResponse
	}
	s := raw.Symbols[0]
	base, quote, ok := bybit.SpotPair(symbol)
	if !ok || s.Symbol != symbol || s.Base != base || s.Quote != quote || s.Status != "TRADING" || s.Spot == nil || !*s.Spot || s.QuotePrecision == nil || *s.QuotePrecision > 36 || len(s.Filters) == 0 || len(s.Filters) > 64 || len(s.OrderTypes) > 16 {
		return bybit.Instrument{}, ErrInvalidResponse
	}
	limit := false
	for _, kind := range s.OrderTypes {
		if kind == "LIMIT" {
			limit = true
		}
	}
	if !limit {
		return bybit.Instrument{}, ErrInvalidResponse
	}
	i := bybit.Instrument{Symbol: symbol, BaseCoin: base, QuoteCoin: quote, Status: "Trading"}
	i.LotSizeFilter.QuotePrecision = "1"
	if *s.QuotePrecision != 0 {
		i.LotSizeFilter.QuotePrecision = "0." + strings.Repeat("0", int(*s.QuotePrecision)-1) + "1"
	}
	seen := map[string]bool{}
	for _, filter := range s.Filters {
		var f struct {
			Kind        string `json:"filterType"`
			Step        string `json:"stepSize"`
			MinQty      string `json:"minQty"`
			MaxQty      string `json:"maxQty"`
			MinNotional string `json:"minNotional"`
			Tick        string `json:"tickSize"`
		}
		if json.Unmarshal(filter, &f) != nil || !identifier(f.Kind, 40) || seen[f.Kind] {
			return bybit.Instrument{}, ErrInvalidResponse
		}
		seen[f.Kind] = true
		switch f.Kind {
		case "LOT_SIZE":
			if !decimal(f.Step, true) || !decimal(f.MinQty, true) || !decimal(f.MaxQty, true) {
				return bybit.Instrument{}, ErrInvalidResponse
			}
			i.LotSizeFilter.BasePrecision = f.Step
			i.LotSizeFilter.MaxMarketOrderQty = f.MaxQty
		case "PRICE_FILTER":
			if !decimal(f.Tick, true) {
				return bybit.Instrument{}, ErrInvalidResponse
			}
			i.PriceFilter.TickSize = f.Tick
		case "NOTIONAL", "MIN_NOTIONAL":
			if i.LotSizeFilter.MinOrderAmt != "" || !decimal(f.MinNotional, true) {
				return bybit.Instrument{}, ErrInvalidResponse
			}
			i.LotSizeFilter.MinOrderAmt = f.MinNotional
		}
	}
	if i.LotSizeFilter.BasePrecision == "" || i.PriceFilter.TickSize == "" || i.LotSizeFilter.MinOrderAmt == "" {
		return bybit.Instrument{}, ErrInvalidResponse
	}
	return i, nil
}
func (c *Client) SpotTicker(ctx context.Context, symbol string) (bybit.Ticker, error) {
	var raw struct {
		Symbol string `json:"symbol"`
		Last   string `json:"lastPrice"`
		Bid    string `json:"bidPrice"`
		Ask    string `json:"askPrice"`
	}
	if e := c.get(ctx, "/api/v3/ticker/24hr", url.Values{"symbol": {symbol}}, false, &raw); e != nil {
		return bybit.Ticker{}, e
	}
	if raw.Symbol != symbol || !decimal(raw.Last, true) || !decimal(raw.Bid, true) || !decimal(raw.Ask, true) || rat(raw.Bid).Cmp(rat(raw.Ask)) > 0 {
		return bybit.Ticker{}, ErrInvalidResponse
	}
	return bybit.Ticker{Symbol: symbol, LastPrice: raw.Last, Bid1Price: raw.Bid, Ask1Price: raw.Ask}, nil
}

type commission struct {
	Maker  string `json:"maker"`
	Taker  string `json:"taker"`
	Buyer  string `json:"buyer"`
	Seller string `json:"seller"`
}

func commissionRate(parts ...commission) (string, string, error) {
	maker, taker := new(big.Rat), new(big.Rat)
	for _, p := range parts {
		for _, v := range []string{p.Maker, p.Taker, p.Buyer, p.Seller} {
			if !decimal(v, false) {
				return "", "", ErrInvalidResponse
			}
		}
		side := rat(p.Buyer)
		if side.Cmp(rat(p.Seller)) < 0 {
			side = rat(p.Seller)
		}
		maker.Add(maker, new(big.Rat).Add(rat(p.Maker), side))
		taker.Add(taker, new(big.Rat).Add(rat(p.Taker), side))
	}
	if maker.Cmp(big.NewRat(1, 1)) > 0 || taker.Cmp(big.NewRat(1, 1)) > 0 {
		return "", "", ErrInvalidResponse
	}
	return maker.FloatString(36), taker.FloatString(36), nil
}
func (c *Client) SpotFeeRate(ctx context.Context, symbol string) (bybit.FeeRate, error) {
	var raw struct {
		Symbol   string     `json:"symbol"`
		Standard commission `json:"standardCommission"`
		Special  commission `json:"specialCommission"`
		Tax      commission `json:"taxCommission"`
		Discount struct {
			Account *bool `json:"enabledForAccount"`
			Symbol  *bool `json:"enabledForSymbol"`
		} `json:"discount"`
	}
	if e := c.get(ctx, "/api/v3/account/commission", url.Values{"symbol": {symbol}}, true, &raw); e != nil {
		return bybit.FeeRate{}, e
	}
	if raw.Symbol != symbol || raw.Discount.Account == nil || raw.Discount.Symbol == nil {
		return bybit.FeeRate{}, ErrInvalidResponse
	}
	if *raw.Discount.Account && *raw.Discount.Symbol {
		return bybit.FeeRate{}, ErrFeeCurrency
	}
	maker, taker, e := commissionRate(raw.Standard, raw.Special, raw.Tax)
	if e != nil {
		return bybit.FeeRate{}, e
	}
	return bybit.FeeRate{Symbol: symbol, MakerFeeRate: maker, TakerFeeRate: taker}, nil
}
func (c *Client) CreateOrder(ctx context.Context, order bybit.SpotOrder) (bybit.OrderAcknowledgement, error) {
	q, e := orderQuery(order)
	if e != nil {
		return bybit.OrderAcknowledgement{}, e
	}
	if _, e = c.SpotFeeRate(ctx, order.Symbol); e != nil {
		return bybit.OrderAcknowledgement{}, e
	}
	var raw struct {
		Symbol   string  `json:"symbol"`
		OrderID  *uint64 `json:"orderId"`
		ClientID string  `json:"clientOrderId"`
	}
	if e = c.do(ctx, "POST", "/api/v3/order", q, true, &raw); e != nil {
		return bybit.OrderAcknowledgement{}, e
	}
	if raw.Symbol != order.Symbol || raw.OrderID == nil || *raw.OrderID == 0 || raw.ClientID != order.OrderLinkID {
		return bybit.OrderAcknowledgement{}, ErrInvalidResponse
	}
	return bybit.OrderAcknowledgement{OrderID: strconv.FormatUint(*raw.OrderID, 10), OrderLinkID: raw.ClientID}, nil
}
func orderStatus(status string, executed string) (string, bool) {
	switch status {
	case "NEW":
		return "New", true
	case "PARTIALLY_FILLED":
		return "PartiallyFilled", true
	case "FILLED":
		return "Filled", true
	case "CANCELED", "EXPIRED", "EXPIRED_IN_MATCH":
		if rat(executed).Sign() > 0 {
			return "PartiallyFilledCanceled", true
		}
		return "Cancelled", true
	case "PENDING_CANCEL":
		return "PendingCancel", true
	case "REJECTED":
		return "Rejected", true
	}
	return "", false
}
func (c *Client) FindOrder(ctx context.Context, symbol, linkID string) (*bybit.Order, error) {
	var raw struct {
		Symbol   string  `json:"symbol"`
		ID       *uint64 `json:"orderId"`
		ClientID string  `json:"clientOrderId"`
		Price    string  `json:"price"`
		Qty      string  `json:"origQty"`
		Executed string  `json:"executedQty"`
		Quote    string  `json:"cummulativeQuoteQty"`
		Status   string  `json:"status"`
		TIF      string  `json:"timeInForce"`
		Type     string  `json:"type"`
		Side     string  `json:"side"`
		Time     *int64  `json:"time"`
		Updated  *int64  `json:"updateTime"`
	}
	if e := c.get(ctx, "/api/v3/order", url.Values{"symbol": {symbol}, "origClientOrderId": {linkID}}, true, &raw); e != nil {
		return nil, e
	}
	if raw.Symbol != symbol || raw.ID == nil || *raw.ID == 0 || raw.ClientID != linkID || raw.Type != "LIMIT" || raw.TIF != "IOC" || (raw.Side != "BUY" && raw.Side != "SELL") || !decimal(raw.Price, true) || !decimal(raw.Qty, true) || !decimal(raw.Executed, false) || !decimal(raw.Quote, false) || rat(raw.Executed).Cmp(rat(raw.Qty)) > 0 || raw.Time == nil || raw.Updated == nil || *raw.Time <= 0 || *raw.Updated < *raw.Time {
		return nil, ErrInvalidResponse
	}
	status, ok := orderStatus(raw.Status, raw.Executed)
	if !ok {
		return nil, ErrInvalidResponse
	}
	side := "Sell"
	if raw.Side == "BUY" {
		side = "Buy"
	}
	return &bybit.Order{OrderID: strconv.FormatUint(*raw.ID, 10), OrderLinkID: linkID, Symbol: symbol, Side: side, OrderType: "Limit", Qty: raw.Qty, BaseQty: raw.Qty, LimitPrice: raw.Price, TimeInForce: "IOC", MarketUnit: "baseCoin", IsLeverage: "0", OrderStatus: status, CumExecQty: raw.Executed, CumExecValue: raw.Quote, CreatedTime: strconv.FormatInt(*raw.Time, 10), UpdatedTime: strconv.FormatInt(*raw.Updated, 10)}, nil
}
func (c *Client) Executions(ctx context.Context, symbol, linkID, cursor string) (bybit.ExecutionPage, error) {
	if cursor != "" && !number(cursor, false) {
		return bybit.ExecutionPage{}, ErrInvalidRequest
	}
	order, e := c.FindOrder(ctx, symbol, linkID)
	if e != nil {
		return bybit.ExecutionPage{}, e
	}
	if order == nil {
		return bybit.ExecutionPage{}, ErrInvalidResponse
	}
	q := url.Values{"symbol": {symbol}, "orderId": {order.OrderID}, "limit": {"100"}}
	if cursor != "" {
		q.Set("fromId", cursor)
	}
	var raw []struct {
		Symbol     string  `json:"symbol"`
		ID         *uint64 `json:"id"`
		OrderID    *uint64 `json:"orderId"`
		Price      string  `json:"price"`
		Qty        string  `json:"qty"`
		Quote      string  `json:"quoteQty"`
		Commission string  `json:"commission"`
		Asset      string  `json:"commissionAsset"`
		Time       *int64  `json:"time"`
		Buyer      *bool   `json:"isBuyer"`
	}
	if e = c.get(ctx, "/api/v3/myTrades", q, true, &raw); e != nil {
		return bybit.ExecutionPage{}, e
	}
	if raw == nil || len(raw) > 100 {
		return bybit.ExecutionPage{}, ErrInvalidResponse
	}
	result := bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{}}
	var previous uint64
	for i, x := range raw {
		if x.Symbol != symbol || x.ID == nil || x.OrderID == nil || strconv.FormatUint(*x.OrderID, 10) != order.OrderID || i > 0 && *x.ID <= previous || cursor != "" && lessID(*x.ID, cursor) || !decimal(x.Price, true) || !decimal(x.Qty, true) || !decimal(x.Quote, true) || !decimal(x.Commission, false) || !coin(x.Asset) || x.Time == nil || *x.Time <= 0 || x.Buyer == nil || (*x.Buyer) != (order.Side == "Buy") {
			return bybit.ExecutionPage{}, ErrInvalidResponse
		}
		previous = *x.ID
		result.List = append(result.List, bybit.Execution{Symbol: symbol, OrderID: order.OrderID, OrderLinkID: linkID, Side: order.Side, OrderQty: order.BaseQty, OrderType: "Limit", ExecID: strconv.FormatUint(*x.ID, 10), ExecPrice: x.Price, ExecQty: x.Qty, ExecValue: x.Quote, ExecFee: x.Commission, FeeCurrency: x.Asset, ExecTime: strconv.FormatInt(*x.Time, 10), ExtraFees: json.RawMessage(`[]`)})
	}
	if len(raw) == 100 {
		if previous == ^uint64(0) {
			return bybit.ExecutionPage{}, ErrInvalidResponse
		}
		result.NextPageCursor = strconv.FormatUint(previous+1, 10)
	}
	return result, nil
}
func lessID(id uint64, lower string) bool { v, _ := strconv.ParseUint(lower, 10, 64); return id < v }
