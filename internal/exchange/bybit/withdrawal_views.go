package bybit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// RecentWithdrawalRecords is an observation of the provider's recent window.
// Its results must never be used to guess an unacknowledged client request ID.
func (c *Client) RecentWithdrawalRecords(ctx context.Context, cursor string) (WithdrawalPage, error) {
	var raw map[string]json.RawMessage
	q := cursorQuery(url.Values{"withdrawType": {"0"}, "limit": {"50"}}, cursor)
	if err := c.get(ctx, "/v5/asset/withdraw/query-record", q, true, &raw); err != nil {
		return WithdrawalPage{}, err
	}
	rows, next, err := withdrawalViewRows(raw, cursor)
	if err != nil {
		return WithdrawalPage{}, err
	}
	page := WithdrawalPage{Rows: []WithdrawalRecord{}, NextPageCursor: next}
	for _, row := range rows {
		var fields map[string]json.RawMessage
		var record WithdrawalRecord
		if json.Unmarshal(row, &fields) != nil || fields == nil {
			return WithdrawalPage{}, ErrInvalidResponse
		}
		for _, field := range []struct {
			name string
			out  any
		}{
			{"withdrawId", &record.WithdrawID}, {"coin", &record.Coin}, {"chain", &record.Chain},
			{"amount", &record.Amount}, {"withdrawFee", &record.WithdrawFee}, {"status", &record.Status},
			{"toAddress", &record.ToAddress}, {"tag", &record.Tag}, {"txID", &record.TxID},
			{"createTime", &record.CreateTime}, {"updateTime", &record.UpdateTime}, {"withdrawType", &record.WithdrawType},
		} {
			if withdrawalViewField(fields, field.name, field.out) != nil {
				return WithdrawalPage{}, ErrInvalidResponse
			}
		}
		page.Rows = append(page.Rows, record)
	}
	if err := ValidateRecentWithdrawalPage(page, cursor); err != nil {
		return WithdrawalPage{}, err
	}
	return page, nil
}

func WithdrawalStatusKnown(status string) bool {
	switch status {
	case "SecurityCheck", "Pending", "success", "CancelByUser", "Reject", "Fail", "BlockchainConfirmed", "MoreInformationRequired", "Unknown",
		"HighValueReviewPending", "HighValueReviewEDDSubmission", "HighValueReviewRejected", "HighValueReviewRejectedRfunding", "HighValueReviewRejectedRefunded":
		return true
	}
	return false
}

func withdrawalTime(value string) (int64, bool) {
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0 && n <= 253402300799999 && strconv.FormatInt(n, 10) == value
}

func ValidateRecentWithdrawalPage(page WithdrawalPage, cursor string) error {
	if page.Rows == nil || len(page.Rows) > 50 || !validCursor(page.NextPageCursor) || page.NextPageCursor != "" && page.NextPageCursor == cursor {
		return ErrInvalidResponse
	}
	seen := map[string]bool{}
	for _, row := range page.Rows {
		created, c := withdrawalTime(row.CreateTime)
		updated, u := withdrawalTime(row.UpdateTime)
		if !identifier(row.WithdrawID, 128) || seen[row.WithdrawID] || !coinName(row.Coin) || !identifier(row.Chain, 40) || !boundedText(row.ToAddress, 256, false) || !boundedText(row.Tag, 120, true) || !decimal(row.Amount, true) || !decimal(row.WithdrawFee, false) || !WithdrawalStatusKnown(row.Status) || row.WithdrawType != 0 || !boundedText(row.TxID, 256, true) || !c || !u || updated < created {
			return ErrInvalidResponse
		}
		seen[row.WithdrawID] = true
	}
	return nil
}

func (c *Client) WithdrawalAddresses(ctx context.Context, chain, cursor string) (WithdrawalAddressPage, error) {
	var raw map[string]json.RawMessage
	q := cursorQuery(url.Values{"chain": {chain}, "addressType": {"0"}, "limit": {"50"}}, cursor)
	if err := c.get(ctx, "/v5/asset/withdraw/query-address", q, true, &raw); err != nil {
		return WithdrawalAddressPage{}, err
	}
	rows, next, err := withdrawalViewRows(raw, cursor)
	if err != nil {
		return WithdrawalAddressPage{}, err
	}
	page := WithdrawalAddressPage{Rows: []WithdrawalAddress{}, NextPageCursor: next}
	for _, row := range rows {
		var fields map[string]json.RawMessage
		var address WithdrawalAddress
		if json.Unmarshal(row, &fields) != nil || fields == nil {
			return WithdrawalAddressPage{}, ErrInvalidResponse
		}
		for _, field := range []struct {
			name string
			out  any
		}{{"coin", &address.Coin}, {"chain", &address.Chain}, {"address", &address.Address}, {"tag", &address.Tag}, {"status", &address.Status}, {"addressType", &address.AddressType}, {"verified", &address.Verified}} {
			if withdrawalViewField(fields, field.name, field.out) != nil {
				return WithdrawalAddressPage{}, ErrInvalidResponse
			}
		}
		page.Rows = append(page.Rows, address)
	}
	if err := ValidateWithdrawalAddressPage(page, chain, cursor); err != nil {
		return WithdrawalAddressPage{}, err
	}
	return page, nil
}

func ValidateWithdrawalAddressPage(page WithdrawalAddressPage, chain, cursor string) error {
	if page.Rows == nil || len(page.Rows) > 50 || !validCursor(page.NextPageCursor) || page.NextPageCursor != "" && page.NextPageCursor == cursor {
		return ErrInvalidResponse
	}
	for _, row := range page.Rows {
		if row.Chain != chain || row.Coin != "baseCoin" && !coinName(row.Coin) || !boundedText(row.Address, 256, false) || !boundedText(row.Tag, 120, true) || row.Status < 0 || row.Status > 1 || row.Verified < 0 || row.Verified > 1 || row.AddressType != 0 && row.AddressType != 2 {
			return ErrInvalidResponse
		}
	}
	return nil
}

func withdrawalViewRows(raw map[string]json.RawMessage, cursor string) ([]json.RawMessage, string, error) {
	var rows []json.RawMessage
	var next string
	if withdrawalViewField(raw, "rows", &rows) != nil || rows == nil || len(rows) > 50 || withdrawalViewField(raw, "nextPageCursor", &next) != nil || !validCursor(next) || next != "" && next == cursor {
		return nil, "", ErrInvalidResponse
	}
	return rows, next, nil
}

func withdrawalViewField(fields map[string]json.RawMessage, name string, out any) error {
	for key := range fields {
		if key != name && strings.EqualFold(key, name) {
			return ErrInvalidResponse
		}
	}
	raw, exists := fields[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}
