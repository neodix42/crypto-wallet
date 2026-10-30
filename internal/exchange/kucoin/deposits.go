package kucoin

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) DepositRecords(ctx context.Context, currency, transaction, cursor string) (bybit.DepositPage, error) {
	if !coin(currency) || transaction == "" || len(transaction) > 256 {
		return bybit.DepositPage{}, ErrRequest
	}
	page := uint64(1)
	if cursor != "" {
		if !canonicalUint(cursor) {
			return bybit.DepositPage{}, ErrRequest
		}
		page, _ = strconv.ParseUint(cursor, 10, 64)
		if page < 1 || page > 200 {
			return bybit.DepositPage{}, ErrRequest
		}
	}
	var raw struct {
		Current *uint64           `json:"currentPage"`
		Size    *uint64           `json:"pageSize"`
		Total   *uint64           `json:"totalNum"`
		Pages   *uint64           `json:"totalPage"`
		Items   []json.RawMessage `json:"items"`
	}
	if e := c.get(ctx, "/api/v1/deposits", url.Values{"currency": {currency}, "currentPage": {strconv.FormatUint(page, 10)}, "pageSize": {"50"}}, true, &raw); e != nil {
		return bybit.DepositPage{}, e
	}
	if raw.Current == nil || *raw.Current != page || raw.Size == nil || *raw.Size != 50 || raw.Total == nil || raw.Pages == nil || raw.Items == nil || len(raw.Items) > 50 || *raw.Total > 10000 || *raw.Pages > 200 || *raw.Pages != (*raw.Total+49)/50 {
		return bybit.DepositPage{}, ErrResponse
	}
	expected := uint64(0)
	if (page-1)*50 < *raw.Total {
		expected = min(uint64(50), *raw.Total-(page-1)*50)
	}
	if uint64(len(raw.Items)) != expected || *raw.Pages > 0 && page > *raw.Pages {
		return bybit.DepositPage{}, ErrResponse
	}
	result := bybit.DepositPage{Rows: []bybit.DepositRecord{}}
	seen := map[string]bool{}
	for _, r := range raw.Items {
		var d struct {
			ID       string  `json:"id"`
			Currency string  `json:"currency"`
			Chain    string  `json:"chain"`
			Status   string  `json:"status"`
			Address  string  `json:"address"`
			Memo     string  `json:"memo"`
			Inner    *bool   `json:"isInner"`
			Amount   string  `json:"amount"`
			Fee      string  `json:"fee"`
			TxID     *string `json:"walletTxId"`
			Created  *int64  `json:"createdAt"`
			Updated  *int64  `json:"updatedAt"`
			Arrears  *bool   `json:"arrears"`
			NeedTax  *bool   `json:"needSubmitWht"`
		}
		if json.Unmarshal(r, &d) != nil || !identifier(d.ID, 128) || seen[d.ID] || d.Currency != currency || !identifier(d.Chain, 40) || d.Inner == nil || !decimal(d.Amount, true) || !decimal(d.Fee, false) || len(d.Address) == 0 || len(d.Address) > 256 || len(d.Memo) > 120 || d.Created == nil || d.Updated == nil || *d.Created <= 0 || *d.Updated < *d.Created || d.Arrears == nil {
			return bybit.DepositPage{}, ErrResponse
		}
		seen[d.ID] = true
		// Internal credits and nonmatching transactions cannot establish the
		// selected on-chain deposit, even with matching amount or address.
		if *d.Inner || d.TxID == nil || *d.TxID != transaction {
			continue
		}
		status := 0
		switch d.Status {
		case "SUCCESS":
			status = 3
		case "FAILURE":
			status = 2
		case "PROCESSING", "WALLET_PROCESSING":
			status = 0
		default:
			return bybit.DepositPage{}, ErrResponse
		}
		travel := 0
		if *d.Arrears || d.NeedTax != nil && *d.NeedTax {
			travel = 1
		}
		result.Rows = append(result.Rows, bybit.DepositRecord{ID: d.ID, Coin: currency, Chain: d.Chain, Amount: d.Amount, TxID: *d.TxID, Status: status, ToAddress: d.Address, Tag: d.Memo, SuccessAt: strconv.FormatInt(*d.Updated, 10), DepositFee: d.Fee, DepositType: "0", TravelRuleStatus: travel})
	}
	if page < *raw.Pages {
		if len(raw.Items) != 50 {
			return bybit.DepositPage{}, ErrResponse
		}
		result.NextPageCursor = strconv.FormatUint(page+1, 10)
	}
	return result, nil
}
