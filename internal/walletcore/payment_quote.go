package walletcore

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/amount"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

type paymentQuoteRequest struct {
	AccountID strictjson.Text `json:"accountId"`
	Asset     strictjson.Text `json:"asset"`
	Recipient strictjson.Text `json:"recipient"`
	Amount    strictjson.Text `json:"amount"`
	Memo      strictjson.Text `json:"memo"`
	Maximum   *bool           `json:"maximum"`
}

func (r paymentQuoteRequest) transfer() model.Transfer {
	return model.Transfer{AccountID: string(r.AccountID), Asset: string(r.Asset), Recipient: string(r.Recipient), Amount: string(r.Amount), Memo: string(r.Memo)}
}

var (
	errQuoteBalance      = errors.New("payment balance is unavailable")
	errQuoteInsufficient = errors.New("payment exceeds spendable balance and required reserves")
	errQuoteUnstable     = errors.New("maximum fee estimate did not stabilize")
)

func validPaymentQuoteInput(t model.Transfer, maximum bool) bool {
	return t.AccountID != "" && len(t.AccountID) <= 512 && t.Asset != "" && len(t.Asset) <= 512 && t.Recipient != "" && len(t.Recipient) <= 512 && t.Recipient == strings.TrimSpace(t.Recipient) && len(t.Amount) <= 4096 && (maximum || t.Amount != "") && len(t.Memo) <= 4096 && utf8.ValidString(t.Memo) && !strings.ContainsRune(t.Memo, 0)
}

func validPaymentQuotePlan(adapter model.Adapter, account model.Account, transfer model.Transfer, plan model.Plan, maximum bool) bool {
	want, e1 := json.Marshal(account)
	got, e2 := json.Marshal(plan.Account)
	if e1 != nil || e2 != nil || !bytes.Equal(want, got) || plan.ID == "" || len(plan.ID) > 512 || plan.Asset != transfer.Asset || !maximumRecipientEqual(adapter, account, transfer.Recipient, plan.Recipient) || plan.Memo != transfer.Memo || plan.Expires <= time.Now().Unix() || plan.Expires > time.Now().Add(30*time.Minute).Unix() || !maximum && plan.Amount != transfer.Amount {
		return false
	}
	value, err := amount.Atomic(plan.Amount)
	if err != nil || value.Sign() <= 0 {
		return false
	}
	if _, err := amount.Atomic(plan.MaxFee); err != nil || adapter.ValidatePlan(plan) != nil {
		return false
	}
	selected, fee := 0, 0
	for _, network := range adapter.Networks() {
		if network.ID == account.Network {
			for _, asset := range network.Assets {
				if asset.ID == plan.Asset {
					selected++
				}
				if asset.ID == plan.FeeAsset {
					fee++
				}
			}
		}
	}
	return selected == 1 && fee == 1
}

func paymentQuoteError(err error) error {
	var evmFee *evm.InsufficientFeeBalanceError
	var tonFee *ton.InsufficientNativeFeeBalanceError
	switch {
	case errors.Is(err, ton.ErrRateLimited):
		return ipc.NewRequestError("TON_RPC_RATE_LIMITED")
	case errors.Is(err, errQuoteBalance):
		return ipc.NewRequestError("PAYMENT_QUOTE_BALANCE_UNAVAILABLE")
	case errors.Is(err, errQuoteInsufficient), errors.Is(err, evm.ErrInsufficientPendingFunds), errors.As(err, &evmFee), errors.As(err, &tonFee):
		return ipc.NewRequestError("PAYMENT_QUOTE_INSUFFICIENT_FUNDS")
	case errors.Is(err, errQuoteUnstable):
		return ipc.NewRequestError("PAYMENT_QUOTE_UNSTABLE")
	default:
		return ipc.NewRequestError("PAYMENT_QUOTE_UNAVAILABLE")
	}
}

func (c *core) pendingPaymentAt(message ipc.Message, epoch uint64) (any, error) {
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" || len(request.AccountID) > 512 {
		return nil, ipc.NewRequestError("PAYMENT_PENDING_UNAVAILABLE")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, ipc.NewRequestError("PAYMENT_PENDING_UNAVAILABLE")
	}
	operation, err := c.accountPaymentReservationAt(epoch, account)
	if err != nil {
		return nil, ipc.NewRequestError("PAYMENT_PENDING_UNAVAILABLE")
	}
	if operation != nil {
		public := *operation
		public.Signed = nil
		operation = &public
	}
	return struct {
		Operation *journal.Operation `json:"operation"`
	}{operation}, nil
}

// Scan bounded signer pages, then retrieve and bind the unique original record.
// This is read-only; even expired or unsigned reservations are returned intact.
func (c *core) accountPaymentReservationAt(epoch uint64, account model.Account) (*journal.Operation, error) {
	const unavailable = "payment reservation unavailable"
	cursor, revision := "", ""
	seen := map[string]bool{}
	var reservation *journal.Operation
	for pages := 0; ; pages++ {
		if pages >= 10000 {
			return nil, errors.New(unavailable)
		}
		var page journalPage
		if err := c.childCallAt(epoch, "journalPage", struct {
			AccountID string `json:"accountId"`
			Asset     string `json:"asset"`
			Cursor    string `json:"cursor"`
		}{account.ID, "", cursor}, &page); err != nil {
			return nil, errors.New(unavailable)
		}
		if len(page.Operations) > 128 || page.Total < 0 || page.Total > 10000 || page.Total < len(page.Operations) || page.Revision == "" || revision != "" && revision != page.Revision || page.Complete != (page.NextCursor == "") || !page.Complete && (len(page.Operations) == 0 || page.NextCursor == cursor) {
			return nil, errors.New(unavailable)
		}
		revision = page.Revision
		for _, operation := range page.Operations {
			if operation.Plan.ID == "" || seen[operation.Plan.ID] || operation.Plan.Account.ID != account.ID || operation.Signed != nil || len(seen) >= 10000 {
				return nil, errors.New(unavailable)
			}
			seen[operation.Plan.ID] = true
			if !journal.Terminal(operation.State) {
				if reservation != nil {
					return nil, errors.New(unavailable)
				}
				original := operation
				reservation = &original
			}
		}
		if page.Complete {
			if len(seen) != page.Total {
				return nil, errors.New(unavailable)
			}
			break
		}
		cursor = page.NextCursor
	}
	if reservation == nil {
		return nil, nil
	}
	var original journal.Operation
	if err := c.childCallAt(epoch, "operation", struct {
		ID string `json:"id"`
	}{reservation.Plan.ID}, &original); err != nil {
		return nil, errors.New(unavailable)
	}
	public := original
	public.Signed = nil
	if !reflect.DeepEqual(public, *reservation) || !reflect.DeepEqual(original.Plan.Account, account) || original.Digest != journal.Digest(original.Plan) {
		return nil, errors.New(unavailable)
	}
	return &original, nil
}
