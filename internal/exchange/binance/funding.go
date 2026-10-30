package binance

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strconv"
	"time"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) Transfer(ctx context.Context, request bybit.TransferRequest) (bybit.TransferAcknowledgement, error) {
	q, e := transferQuery(request)
	if e != nil {
		return bybit.TransferAcknowledgement{}, e
	}
	var raw struct {
		ID *uint64 `json:"tranId"`
	}
	if e = c.do(ctx, "POST", "/sapi/v1/asset/transfer", q, true, &raw); e != nil {
		return bybit.TransferAcknowledgement{}, e
	}
	if raw.ID == nil || *raw.ID == 0 {
		return bybit.TransferAcknowledgement{}, ErrInvalidResponse
	}
	// This is the server identity, never the untransmitted local UUID. It must
	// be persisted before exact history reconciliation can establish success.
	return bybit.TransferAcknowledgement{TransferID: strconv.FormatUint(*raw.ID, 10), Status: "PENDING"}, nil
}
func (c *Client) TransferStatus(_ context.Context, originalID string) (*bybit.TransferRecord, error) {
	if !uuid(originalID) {
		return nil, ErrInvalidRequest
	}
	// The universal-transfer API has no client ID. An absent acknowledgement
	// cannot be reconstructed by matching amount, coin, time or a nearby row.
	return nil, nil
}
func (c *Client) TransferStatusFor(ctx context.Context, request bybit.TransferRequest, remoteID string) (*bybit.TransferRecord, error) {
	q, e := transferQuery(request)
	if e != nil {
		return nil, e
	}
	if remoteID == "" {
		return nil, nil
	}
	if !number(remoteID, true) {
		return nil, ErrInvalidRequest
	}
	for page := 1; page <= 32; page++ {
		var raw struct {
			Total *uint64 `json:"total"`
			Rows  []struct {
				Asset     string  `json:"asset"`
				Amount    string  `json:"amount"`
				Type      string  `json:"type"`
				Status    string  `json:"status"`
				ID        *uint64 `json:"tranId"`
				Timestamp *int64  `json:"timestamp"`
			} `json:"rows"`
		}
		if e = c.get(ctx, "/sapi/v1/asset/transfer", url.Values{"type": {q.Get("type")}, "current": {strconv.Itoa(page)}, "size": {"100"}}, true, &raw); e != nil {
			return nil, e
		}
		if raw.Total == nil || raw.Rows == nil || len(raw.Rows) > 100 || *raw.Total < uint64(len(raw.Rows)) {
			return nil, ErrInvalidResponse
		}
		var found *bybit.TransferRecord
		seen := map[uint64]bool{}
		for _, row := range raw.Rows {
			if row.ID == nil || *row.ID == 0 || seen[*row.ID] || !coin(row.Asset) || !decimal(row.Amount, true) || row.Type != q.Get("type") || row.Timestamp == nil || *row.Timestamp <= 0 {
				return nil, ErrInvalidResponse
			}
			seen[*row.ID] = true
			status := ""
			switch row.Status {
			case "CONFIRMED":
				status = "SUCCESS"
			case "FAILED":
				status = "FAILED"
			case "PENDING":
				status = "PENDING"
			default:
				return nil, ErrInvalidResponse
			}
			if strconv.FormatUint(*row.ID, 10) != remoteID {
				continue
			}
			if row.Asset != request.Coin || rat(row.Amount).Cmp(rat(request.Amount)) != 0 {
				return nil, ErrInvalidResponse
			}
			found = &bybit.TransferRecord{TransferID: request.TransferID, Coin: row.Asset, Amount: row.Amount, FromAccountType: request.FromAccountType, ToAccountType: request.ToAccountType, Timestamp: strconv.FormatInt(*row.Timestamp, 10), Status: status}
		}
		if found != nil {
			return found, nil
		}
		if uint64(page*100) >= *raw.Total {
			return nil, nil
		}
		if len(raw.Rows) != 100 {
			return nil, ErrInvalidResponse
		}
	}
	return nil, ErrInvalidResponse
}
func (c *Client) Withdraw(ctx context.Context, withdrawal bybit.WithdrawalRequest) (bybit.WithdrawalAcknowledgement, error) {
	q, e := withdrawalQuery(withdrawal)
	if e != nil {
		return bybit.WithdrawalAcknowledgement{}, e
	}
	if e = c.WithdrawalAllowed(ctx); e != nil {
		return bybit.WithdrawalAcknowledgement{}, e
	}
	var raw struct {
		ID string `json:"id"`
	}
	if e = c.do(ctx, "POST", "/sapi/v1/capital/withdraw/apply", q, true, &raw); e != nil {
		return bybit.WithdrawalAcknowledgement{}, e
	}
	if !identifier(raw.ID, 128) {
		return bybit.WithdrawalAcknowledgement{}, ErrInvalidResponse
	}
	return bybit.WithdrawalAcknowledgement{ID: raw.ID}, nil
}
func withdrawalStatus(status int) (string, bool) {
	switch status {
	case 0, 2, 4:
		return "Pending", true
	case 1:
		return "CancelByUser", true
	case 3:
		return "Reject", true
	case 5:
		return "Fail", true
	case 6:
		return "success", true
	}
	return "", false
}
func (c *Client) WithdrawalRecords(ctx context.Context, selected, id, cursor string) (bybit.WithdrawalPage, error) {
	offset, e := canonicalCursor(cursor)
	if e != nil {
		return bybit.WithdrawalPage{}, e
	}
	return c.withdrawalRecords(ctx, url.Values{"coin": {selected}, "idList": {id}, "limit": {"50"}, "offset": {strconv.FormatUint(offset, 10)}}, selected, id, "")
}

// WithdrawalRecordsFor can recover a lost acknowledgement only through the
// exact client ID sent in the original write. Its bounded historical interval
// remains anchored to the immutable request timestamp, including after restart.
func (c *Client) WithdrawalRecordsFor(ctx context.Context, request bybit.WithdrawalRequest, remoteID, cursor string) (bybit.WithdrawalPage, error) {
	if _, err := withdrawalQuery(request); err != nil {
		return bybit.WithdrawalPage{}, err
	}
	position, err := canonicalCursor(cursor)
	if err != nil {
		return bybit.WithdrawalPage{}, err
	}
	q := url.Values{"coin": {request.Coin}, "limit": {"50"}, "offset": {strconv.FormatUint(position, 10)}}
	if remoteID != "" {
		q.Set("idList", remoteID)
	} else {
		const day = int64(24 * 60 * 60 * 1000)
		if request.Timestamp > math.MaxInt64-day {
			return bybit.WithdrawalPage{}, ErrInvalidRequest
		}
		start := request.Timestamp - day
		if start < 1 {
			start = 1
		}
		q.Set("withdrawOrderId", request.RequestID)
		q.Set("startTime", strconv.FormatInt(start, 10))
		q.Set("endTime", strconv.FormatInt(request.Timestamp+day, 10))
	}
	page, err := c.withdrawalRecords(ctx, q, request.Coin, remoteID, request.RequestID)
	if err != nil {
		return bybit.WithdrawalPage{}, err
	}
	for _, row := range page.Rows {
		if row.Chain != request.Chain || row.ToAddress != request.Address || row.Tag != request.Tag || rat(row.Amount).Cmp(rat(request.Amount)) != 0 {
			return bybit.WithdrawalPage{}, ErrInvalidResponse
		}
	}
	return page, nil
}

func (c *Client) withdrawalRecords(ctx context.Context, query url.Values, selected, id, originalID string) (bybit.WithdrawalPage, error) {
	var raw []json.RawMessage
	if e := c.get(ctx, "/sapi/v1/capital/withdraw/history", query, true, &raw); e != nil {
		return bybit.WithdrawalPage{}, e
	}
	if raw == nil || len(raw) > 1 {
		return bybit.WithdrawalPage{}, ErrInvalidResponse
	}
	result := bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{}}
	for _, r := range raw {
		var w struct {
			ID        string `json:"id"`
			RequestID string `json:"withdrawOrderId"`
			Coin      string `json:"coin"`
			Network   string `json:"network"`
			Amount    string `json:"amount"`
			Fee       string `json:"transactionFee"`
			Status    *int   `json:"status"`
			Address   string `json:"address"`
			Tag       string `json:"addressTag"`
			TxID      string `json:"txId"`
			Applied   string `json:"applyTime"`
			Completed string `json:"completeTime"`
			Transfer  *int   `json:"transferType"`
		}
		if !required(r, "id", "coin", "network", "amount", "transactionFee", "status", "address", "txId", "applyTime", "transferType") || json.Unmarshal(r, &w) != nil || !identifier(w.ID, 128) || id != "" && w.ID != id || id == "" && w.RequestID != originalID || originalID != "" && w.RequestID != "" && w.RequestID != originalID || w.RequestID != "" && !identifier(w.RequestID, 32) || w.Coin != selected || !identifier(w.Network, 40) || !decimal(w.Amount, true) || !decimal(w.Fee, false) || !text(w.Address, 256, false) || !text(w.Tag, 120, true) || !text(w.TxID, 256, true) || w.Status == nil || w.Transfer == nil || *w.Transfer != 0 {
			return bybit.WithdrawalPage{}, ErrInvalidResponse
		}
		status, ok := withdrawalStatus(*w.Status)
		if !ok {
			return bybit.WithdrawalPage{}, ErrInvalidResponse
		}
		created, e := time.Parse("2006-01-02 15:04:05", w.Applied)
		if e != nil || created.UnixMilli() <= 0 {
			return bybit.WithdrawalPage{}, ErrInvalidResponse
		}
		updated := ""
		if w.Completed != "" {
			t, e := time.Parse("2006-01-02 15:04:05", w.Completed)
			if e != nil || t.Before(created) {
				return bybit.WithdrawalPage{}, ErrInvalidResponse
			}
			updated = strconv.FormatInt(t.UnixMilli(), 10)
		}
		result.Rows = append(result.Rows, bybit.WithdrawalRecord{WithdrawID: w.ID, RequestID: w.RequestID, Coin: w.Coin, Chain: w.Network, Amount: w.Amount, WithdrawFee: w.Fee, Status: status, ToAddress: w.Address, Tag: w.Tag, TxID: w.TxID, CreateTime: strconv.FormatInt(created.UnixMilli(), 10), UpdateTime: updated, WithdrawType: 0})
	}
	return result, nil
}
func (c *Client) DepositRecords(ctx context.Context, selected, transaction, cursor string) (bybit.DepositPage, error) {
	offset, e := canonicalCursor(cursor)
	if e != nil {
		return bybit.DepositPage{}, e
	}
	var raw []json.RawMessage
	if e = c.get(ctx, "/sapi/v1/capital/deposit/hisrec", url.Values{"coin": {selected}, "txId": {transaction}, "limit": {"50"}, "offset": {strconv.FormatUint(offset, 10)}}, true, &raw); e != nil {
		return bybit.DepositPage{}, e
	}
	if raw == nil || len(raw) > 50 {
		return bybit.DepositPage{}, ErrInvalidResponse
	}
	result := bybit.DepositPage{Rows: []bybit.DepositRecord{}}
	seen := map[string]bool{}
	for _, r := range raw {
		var d struct {
			ID            string `json:"id"`
			Coin          string `json:"coin"`
			Network       string `json:"network"`
			Amount        string `json:"amount"`
			Status        *int   `json:"status"`
			Address       string `json:"address"`
			Tag           string `json:"addressTag"`
			TxID          string `json:"txId"`
			Inserted      *int64 `json:"insertTime"`
			Completed     *int64 `json:"completeTime"`
			Transfer      *int   `json:"transferType"`
			Confirmations string `json:"confirmTimes"`
			Travel        *int   `json:"travelRuleStatus"`
		}
		if !required(r, "id", "coin", "network", "amount", "status", "address", "addressTag", "txId", "insertTime", "transferType", "confirmTimes", "travelRuleStatus") || json.Unmarshal(r, &d) != nil || !identifier(d.ID, 128) || seen[d.ID] || d.Coin != selected || d.TxID != transaction || !identifier(d.Network, 40) || !decimal(d.Amount, true) || !text(d.Address, 256, false) || !text(d.Tag, 120, true) || d.Status == nil || d.Inserted == nil || *d.Inserted <= 0 || d.Transfer == nil || *d.Transfer != 0 || !text(d.Confirmations, 64, false) || d.Travel == nil || *d.Travel < 0 || *d.Travel > 1 {
			return bybit.DepositPage{}, ErrInvalidResponse
		}
		status := -1
		switch *d.Status {
		case 0, 6, 8:
			status = 0
		case 1:
			status = 3
		case 2, 7:
			status = 2
		}
		if status < 0 {
			return bybit.DepositPage{}, ErrInvalidResponse
		}
		completed := ""
		if d.Completed != nil && *d.Completed > 0 {
			if *d.Completed < *d.Inserted {
				return bybit.DepositPage{}, ErrInvalidResponse
			}
			completed = strconv.FormatInt(*d.Completed, 10)
		}
		result.Rows = append(result.Rows, bybit.DepositRecord{ID: d.ID, Coin: d.Coin, Chain: d.Network, Amount: d.Amount, Status: status, ToAddress: d.Address, Tag: d.Tag, TxID: d.TxID, SuccessAt: completed, Confirmations: d.Confirmations, DepositType: "0", TravelRuleStatus: *d.Travel})
		seen[d.ID] = true
	}
	if len(raw) == 50 {
		if offset >= 10000 {
			return bybit.DepositPage{}, ErrInvalidResponse
		}
		result.NextPageCursor = strconv.FormatUint(offset+50, 10)
	}
	return result, nil
}
