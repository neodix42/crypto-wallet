package walletcore

import (
	"context"
	"sort"
	"strconv"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

type exchangeWithdrawalRow struct {
	Source        string `json:"source"`
	OperationID   string `json:"operationId,omitempty"`
	WithdrawalID  string `json:"withdrawalId,omitempty"`
	Coin          string `json:"coin"`
	Chain         string `json:"chain"`
	Address       string `json:"address"`
	Amount        string `json:"amount"`
	AmountBasis   string `json:"amountBasis"`
	Fee           string `json:"fee"`
	FeeBasis      string `json:"feeBasis"`
	Status        string `json:"status"`
	State         string `json:"state"`
	Reason        string `json:"reason"`
	Code          string `json:"code,omitempty"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	TransactionID string `json:"transactionId,omitempty"`
	createdMS     int64
}

type exchangeWithdrawalsSnapshot struct {
	Rows       []exchangeWithdrawalRow `json:"rows"`
	ObservedAt string                  `json:"observedAt"`
	More       bool                    `json:"more"`
	ReadError  string                  `json:"readError,omitempty"`
	Scope      string                  `json:"scope"`
}

type exchangeWithdrawalAddressStatus struct {
	Coin       string `json:"coin"`
	Chain      string `json:"chain"`
	Address    string `json:"address"`
	State      string `json:"state"`
	Verified   *bool  `json:"verified,omitempty"`
	ObservedAt string `json:"observedAt"`
	Reason     string `json:"reason"`
	ReadError  string `json:"readError,omitempty"`
}

func (c *core) exchangeWithdrawalViewAt(message ipc.Message, epoch uint64) (any, error) {
	var in struct {
		AccountID strictjson.Text `json:"accountId"`
		AssetID   strictjson.Text `json:"assetId"`
	}
	if message.Method == "exchangeWithdrawals" {
		if strictjson.Decode(message.Body, &struct{}{}) != nil {
			return nil, ipc.NewRequestError("INVALID_ARGUMENT")
		}
	} else if strictjson.Decode(message.Body, &in) != nil || in.AccountID == "" || in.AssetID == "" {
		return nil, ipc.NewRequestError("INVALID_ARGUMENT")
	}
	client, status, err := c.exchangeClientAt(epoch)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	provider, ok := exchange.ProviderForEndpoint(client.Endpoint())
	if !ok || provider.ID != "bybit" {
		return nil, ipc.NewRequestError("INVALID_ARGUMENT")
	}
	ctx, cancel := c.networkContextAt(90*time.Second, epoch)
	defer cancel()
	if message.Method == "exchangeWithdrawals" {
		return c.exchangeWithdrawalsAt(ctx, client, status), nil
	}
	return c.exchangeWithdrawalAddressStatusAt(epoch, ctx, client, string(in.AccountID), string(in.AssetID))
}

func (c *core) exchangeWithdrawalsAt(ctx context.Context, client exchangeAPI, status exchangePrivateStatus) exchangeWithdrawalsSnapshot {
	view := exchangeWithdrawalsSnapshot{Rows: []exchangeWithdrawalRow{}, ObservedAt: time.Now().UTC().Format(time.RFC3339), Scope: "Up to ten newest local attempts and Bybit on-chain records observed within a bounded scan of at most 200 records from Bybit's default recent 30-day window. Manual Bybit requests are included. Missing records do not prove a request failed. Provider success does not prove independent on-chain receipt."}
	locals := map[string]vault.ExchangeOperation{}
	localRows := map[string]exchangeWithdrawalRow{}
	for _, op := range status.Operations {
		if op.Kind != "withdraw" || op.CredentialID != status.CredentialID || op.Endpoint != status.Endpoint {
			continue
		}
		var request bybit.WithdrawalRequest
		if strictjson.Decode([]byte(op.Payload), &request) != nil || request.RequestID != op.ID {
			continue
		}
		state, reason := localWithdrawalReason(op)
		row := exchangeWithdrawalRow{Source: "local", OperationID: op.ID, WithdrawalID: op.ExternalID, Coin: request.Coin, Chain: request.Chain, Address: request.Address, Amount: request.Amount, AmountBasis: "plannedDebit", Fee: op.Fee, FeeBasis: "estimated", Status: op.State, State: state, Reason: reason, CreatedAt: time.Unix(op.CreatedAt, 0).UTC().Format(time.RFC3339), createdMS: op.CreatedAt * 1000}
		if op.RejectionCode != 0 && vault.ExchangeRejectionAllowed(op.RejectionCode) {
			row.Code = exchangeRejectionCode(op.RejectionCode)
		} else if state == "UNKNOWN" {
			row.Code = "EXCHANGE_WITHDRAWAL_UNKNOWN"
		}
		locals[op.ID], localRows[op.ID] = op, row
	}
	reader, available := client.(interface {
		RecentWithdrawalRecords(context.Context, string) (bybit.WithdrawalPage, error)
	})
	if !available {
		view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAWALS", bybit.ErrUnavailable))
	} else {
		cursor, seenIDs, seenCursors := "", map[string]bool{}, map[string]bool{}
		for pageNumber := 0; pageNumber < 4; pageNumber++ {
			page, err := reader.RecentWithdrawalRecords(ctx, cursor)
			if err == nil {
				err = bybit.ValidateRecentWithdrawalPage(page, cursor)
			}
			if err != nil {
				view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAWALS", err))
				view.More = true
				break
			}
			invalid := false
			for _, record := range page.Rows {
				if seenIDs[record.WithdrawID] {
					invalid = true
					break
				}
				seenIDs[record.WithdrawID] = true
				created, _ := strconv.ParseInt(record.CreateTime, 10, 64)
				updated, _ := strconv.ParseInt(record.UpdateTime, 10, 64)
				state, reason := providerWithdrawalReason(record.Status)
				row := exchangeWithdrawalRow{Source: "bybit", WithdrawalID: record.WithdrawID, Coin: record.Coin, Chain: record.Chain, Address: record.ToAddress, Amount: record.Amount, AmountBasis: "providerReported", Fee: record.WithdrawFee, FeeBasis: "providerReported", Status: record.Status, State: state, Reason: reason, TransactionID: record.TxID, CreatedAt: time.UnixMilli(created).UTC().Format(time.RFC3339Nano), UpdatedAt: time.UnixMilli(updated).UTC().Format(time.RFC3339Nano), createdMS: created}
				// Only the exact acknowledged provider ID and original signed
				// request identity can join observations. Amount/time cannot.
				matches := []string{}
				for id, op := range locals {
					var request bybit.WithdrawalRequest
					if op.ExternalID == record.WithdrawID && strictjson.Decode([]byte(op.Payload), &request) == nil && request.Coin == record.Coin && request.Chain == record.Chain && request.Address == record.ToAddress && request.Tag == record.Tag {
						matches = append(matches, id)
					}
				}
				if len(matches) == 1 {
					row.Source, row.OperationID = "bybit+local", matches[0]
					delete(localRows, matches[0])
				}
				view.Rows = append(view.Rows, row)
			}
			if invalid || page.NextPageCursor != "" && seenCursors[page.NextPageCursor] {
				view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAWALS", bybit.ErrInvalidResponse))
				view.More = true
				break
			}
			if page.NextPageCursor == "" {
				break
			}
			seenCursors[page.NextPageCursor], cursor, view.More = true, page.NextPageCursor, pageNumber == 3
		}
	}
	for _, row := range localRows {
		view.Rows = append(view.Rows, row)
	}
	sort.SliceStable(view.Rows, func(i, j int) bool {
		if view.Rows[i].createdMS != view.Rows[j].createdMS {
			return view.Rows[i].createdMS > view.Rows[j].createdMS
		}
		return view.Rows[i].WithdrawalID+view.Rows[i].OperationID < view.Rows[j].WithdrawalID+view.Rows[j].OperationID
	})
	if len(view.Rows) > 10 {
		view.Rows, view.More = view.Rows[:10], true
	}
	return view
}

func localWithdrawalReason(op vault.ExchangeOperation) (string, string) {
	if op.State == "FAILED" && vault.ExchangeRejectionAllowed(op.RejectionCode) {
		return "REJECTED", exchangeRejectionMessage(op.RejectionCode)
	}
	switch op.State {
	case "PREPARED", "AUTHORIZED":
		return "REQUESTED", "Prepared locally; this is not proof that Bybit accepted or sent a withdrawal."
	case "SUBMITTING", "UNKNOWN":
		return "UNKNOWN", "The original request outcome is unknown. Its identity is retained; no replacement or retry was sent."
	case "PENDING":
		return "PENDING", "Bybit acknowledged the original request; completion and receipt have not been established."
	case "CONFIRMED":
		return "SUCCESS", "Stored exchange-reported completion; independent on-chain receipt is not established by this local record."
	case "FAILED":
		return "FAILED", "The original operation was recorded failed; no additional private provider reason is available."
	case "CANCELLED":
		return "CANCELLED", "The original request was cancelled locally; this does not classify another exchange request."
	}
	return "UNKNOWN", "The original local state does not establish exchange completion or receipt."
}

func providerWithdrawalReason(status string) (string, string) {
	switch status {
	case "success":
		return "SUCCESS", "Bybit reports this withdrawal completed. This observation does not independently establish destination credit or finality."
	case "SecurityCheck":
		return "PENDING", "Bybit is performing a security check; no completed withdrawal is established."
	case "MoreInformationRequired", "HighValueReviewEDDSubmission":
		return "PENDING", "Bybit requires additional information. Review the original request in Bybit."
	case "BlockchainConfirmed":
		return "PENDING", "Bybit reports blockchain confirmation while its withdrawal workflow remains incomplete."
	case "Pending", "HighValueReviewPending":
		return "PENDING", "Bybit reports the original withdrawal is pending."
	case "CancelByUser":
		return "CANCELLED", "Bybit reports the original withdrawal was cancelled."
	case "Reject", "HighValueReviewRejected", "HighValueReviewRejectedRfunding", "HighValueReviewRejectedRefunded":
		return "REJECTED", "Bybit reports the original withdrawal was rejected; this API does not provide a more specific refusal reason."
	case "Fail":
		return "FAILED", "Bybit reports the original withdrawal failed; this API does not provide a more specific failure reason."
	}
	return "UNKNOWN", "Bybit has not classified the original withdrawal outcome."
}

func (c *core) exchangeWithdrawalAddressStatusAt(epoch uint64, ctx context.Context, client exchangeAPI, accountID, assetID string) (exchangeWithdrawalAddressStatus, error) {
	view := exchangeWithdrawalAddressStatus{State: "unknown", Reason: "The selected address-book entry has not been verified. This is not a global whitelist setting.", ObservedAt: time.Now().UTC().Format(time.RFC3339)}
	account, asset, _, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return view, err
	}
	view.Coin, view.Address = asset.Symbol, account.Address
	_, chain, _, err := c.exchangeWithdrawalQuoteAt(epoch, client, accountID, assetID)
	if err != nil {
		view.ReadError = publicRequestErrorCode(err)
		return view, nil
	}
	view.Chain = chain.Chain
	reader, available := client.(interface {
		WithdrawalAddresses(context.Context, string, string) (bybit.WithdrawalAddressPage, error)
	})
	if !available {
		view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAW_ADDRESS", bybit.ErrUnavailable))
		return view, nil
	}
	cursor, seen, matched, locked, verified := "", map[string]bool{}, false, false, true
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		page, err := reader.WithdrawalAddresses(ctx, chain.Chain, cursor)
		if err == nil {
			err = bybit.ValidateWithdrawalAddressPage(page, chain.Chain, cursor)
		}
		if err != nil || page.NextPageCursor != "" && seen[page.NextPageCursor] {
			if err == nil {
				err = bybit.ErrInvalidResponse
			}
			view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAW_ADDRESS", err))
			return view, nil
		}
		for _, row := range page.Rows {
			// Bybit documents exact, case-sensitive whitelist addresses. Do
			// not invent a match by rewriting case, memo or TON formatting.
			if (row.Coin == asset.Symbol || row.Coin == "baseCoin") && row.Address == account.Address && row.Tag == "" {
				matched, locked, verified = true, locked || row.Status == 1, verified && row.Verified == 1
			}
		}
		if page.NextPageCursor == "" {
			if !matched {
				view.State, view.Reason = "notListed", "The exact selected address and empty memo were not found in the fully read Bybit address book for this chain. Add this exact destination in Bybit before withdrawing."
				return view, nil
			}
			view.Verified = &verified
			if locked {
				view.State, view.Reason = "locked24h", "Bybit reports a new-address 24-hour withdrawal restriction. This endpoint does not provide a verified unlock time."
			} else {
				view.State, view.Reason = "listed", "The exact selected destination is present in Bybit's address book and reports normal address status. Account restrictions and final withdrawal acceptance remain separate."
			}
			return view, nil
		}
		seen[page.NextPageCursor], cursor = true, page.NextPageCursor
	}
	view.ReadError = publicRequestErrorCode(exchangeReadFailure("WITHDRAW_ADDRESS", bybit.ErrInvalidResponse))
	view.Reason = "The address-book scan reached its bounded limit. Address eligibility remains unknown."
	return view, nil
}
