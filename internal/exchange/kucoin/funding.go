package kucoin

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) Transfer(ctx context.Context, transfer bybit.TransferRequest) (bybit.TransferAcknowledgement, error) {
	request, err := OperationRequest(c.endpoint, "transfer", marshal(transfer), c.now().UnixMilli())
	if err != nil {
		return bybit.TransferAcknowledgement{}, err
	}
	if err = c.classic(ctx); err != nil {
		return bybit.TransferAcknowledgement{}, err
	}
	var ack struct {
		OrderID string `json:"orderId"`
	}
	if err = c.do(ctx, request.Method, request.Path, request.Payload, true, &ack); err != nil {
		return bybit.TransferAcknowledgement{}, err
	}
	if !identifier(ack.OrderID, 120) {
		return bybit.TransferAcknowledgement{}, ErrResponse
	}
	// An acknowledgement is not an independent status observation. The Classic
	// API publishes no reviewed exact-client-ID transfer lookup here. New
	// automatic transfers are gated by the coordinator before authorization.
	return bybit.TransferAcknowledgement{TransferID: ack.OrderID, Status: "PENDING"}, nil
}

func (c *Client) TransferStatus(_ context.Context, original string) (*bybit.TransferRecord, error) {
	if bybit.ValidateTransfer(bybit.TransferRequest{TransferID: original, Coin: "USDT", Amount: "1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}) != nil {
		return nil, ErrRequest
	}
	return nil, nil
}

func (c *Client) TransferStatusFor(ctx context.Context, original bybit.TransferRequest, acknowledgedID string) (*bybit.TransferRecord, error) {
	if bybit.ValidateTransfer(original) != nil || acknowledgedID != "" && !identifier(acknowledgedID, 120) {
		return nil, ErrRequest
	}
	// Neither a ledger row with a similar amount nor the current balance binds
	// a transfer to its original clientOid. Preserve an unresolved operation.
	return c.TransferStatus(ctx, original.TransferID)
}

func (c *Client) Withdraw(ctx context.Context, withdrawal bybit.WithdrawalRequest) (bybit.WithdrawalAcknowledgement, error) {
	request, err := OperationRequest(c.endpoint, "withdraw", marshal(withdrawal), c.now().UnixMilli())
	if err != nil {
		return bybit.WithdrawalAcknowledgement{}, err
	}
	if err = c.WithdrawalAllowed(ctx); err != nil {
		return bybit.WithdrawalAcknowledgement{}, err
	}
	var ack struct {
		WithdrawalID string `json:"withdrawalId"`
	}
	if err = c.do(ctx, request.Method, request.Path, request.Payload, true, &ack); err != nil {
		return bybit.WithdrawalAcknowledgement{}, err
	}
	if !identifier(ack.WithdrawalID, 120) {
		return bybit.WithdrawalAcknowledgement{}, ErrResponse
	}
	return bybit.WithdrawalAcknowledgement{ID: ack.WithdrawalID}, nil
}

func fundingText(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

// WithdrawalRecords queries only an acknowledged server ID. Remark is not a
// client-ID lookup mechanism; a lost acknowledgement remains UNKNOWN.
func (c *Client) WithdrawalRecords(ctx context.Context, currency, acknowledgedID, cursor string) (bybit.WithdrawalPage, error) {
	if !coin(currency) || !identifier(acknowledgedID, 120) || cursor != "" {
		return bybit.WithdrawalPage{}, ErrRequest
	}
	var raw json.RawMessage
	if err := c.get(ctx, "/api/v1/withdrawals/"+acknowledgedID, nil, true, &raw); err != nil {
		return bybit.WithdrawalPage{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bybit.WithdrawalPage{}, ErrResponse
	}
	for _, key := range []string{"id", "currency", "chainId", "status", "address", "memo", "isInner", "amount", "fee", "createdAt", "returnStatus"} {
		if _, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return bybit.WithdrawalPage{}, ErrResponse
		}
	}
	var row struct {
		ID           string          `json:"id"`
		Currency     string          `json:"currency"`
		Chain        string          `json:"chainId"`
		Status       string          `json:"status"`
		Address      string          `json:"address"`
		Memo         string          `json:"memo"`
		Inner        *bool           `json:"isInner"`
		Amount       string          `json:"amount"`
		Fee          string          `json:"fee"`
		WalletTxID   *string         `json:"walletTxId"`
		TxID         *string         `json:"txId"`
		Remark       string          `json:"remark"`
		Created      *int64          `json:"createdAt"`
		ReturnStatus string          `json:"returnStatus"`
		Taxes        json.RawMessage `json:"taxes"`
	}
	if json.Unmarshal(raw, &row) != nil || row.ID != acknowledgedID || row.Currency != currency || !identifier(row.Chain, 40) || !fundingText(row.Address, 256, false) || !fundingText(row.Memo, 120, true) || row.Inner == nil || *row.Inner || !decimal(row.Amount, true) || !decimal(row.Fee, false) || row.Created == nil || *row.Created <= 0 || *row.Created > c.now().UnixMilli()+1000 || row.ReturnStatus != "NOT_RETURN" || row.Remark != "" && !identifier(row.Remark, 32) {
		return bybit.WithdrawalPage{}, ErrResponse
	}
	if taxes := bytes.TrimSpace(row.Taxes); len(taxes) != 0 && !bytes.Equal(taxes, []byte("null")) && !bytes.Equal(taxes, []byte("[]")) {
		return bybit.WithdrawalPage{}, ErrResponse
	}
	txID := ""
	for _, value := range []*string{row.WalletTxID, row.TxID} {
		if value == nil || *value == "" {
			continue
		}
		if !fundingText(*value, 256, false) || txID != "" && txID != *value {
			return bybit.WithdrawalPage{}, ErrResponse
		}
		txID = *value
	}
	status := ""
	switch row.Status {
	case "PROCESSING":
		status = "Pending"
	case "SUCCESS":
		if txID == "" {
			return bybit.WithdrawalPage{}, ErrResponse
		}
		status = "success"
	case "FAILURE":
		status = "Fail"
	default:
		return bybit.WithdrawalPage{}, ErrResponse
	}
	return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{RequestID: row.Remark, WithdrawID: row.ID, Coin: row.Currency, Chain: row.Chain, Amount: row.Amount, WithdrawFee: row.Fee, Status: status, ToAddress: row.Address, Tag: row.Memo, TxID: txID, CreateTime: strconv.FormatInt(*row.Created, 10), WithdrawType: 0}}}, nil
}
