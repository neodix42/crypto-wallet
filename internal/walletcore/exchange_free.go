package walletcore

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"localwallet/internal/amount"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
)

// A present mode flag must be an actual JSON boolean. A null/string/number
// must not silently select another funding policy through a zero value.
type exchangeActionFlag bool

func (flag *exchangeActionFlag) UnmarshalJSON(raw []byte) error {
	switch string(bytes.TrimSpace(raw)) {
	case "true":
		*flag = true
	case "false":
		*flag = false
	default:
		return errExchange
	}
	return nil
}

func exactExchangeModeField(raw []byte, field string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for key := range fields {
		if key != field && strings.EqualFold(key, field) {
			return false
		}
	}
	return true
}

// This explicitly selected action moves currently available custody funds.
// It neither identifies a prior deposit nor mutates its workflow/journal.
func (c *core) exchangeFundingMoveAt(epoch uint64, ctx context.Context, client exchangeAPI, accountID, assetID, value string) (exchangeResult, error) {
	return c.exchangeCustodyMoveAt(epoch, ctx, client, accountID, assetID, value, "FUND", "UNIFIED")
}

// This is a separate economic action. Withdrawal must never infer approval to
// perform the reverse transfer from approval of an earlier deposit or trade.
func (c *core) exchangeUnifiedMoveAt(epoch uint64, ctx context.Context, client exchangeAPI, accountID, assetID, value string) (exchangeResult, error) {
	return c.exchangeCustodyMoveAt(epoch, ctx, client, accountID, assetID, value, "UNIFIED", "FUND")
}

func (c *core) exchangeCustodyMoveAt(epoch uint64, ctx context.Context, client exchangeAPI, accountID, assetID, value, from, to string) (exchangeResult, error) {
	if from != "FUND" && from != "UNIFIED" || to != "FUND" && to != "UNIFIED" || from == to {
		return exchangeResult{}, ipc.NewRequestError("INVALID_ARGUMENT")
	}
	_, asset, _, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return exchangeResult{}, err
	}
	atomic, err := amount.Parse(value, asset.Decimals)
	if err != nil || atomic == "0" {
		return exchangeResult{}, ipc.NewRequestError("INVALID_ARGUMENT")
	}
	info, err := client.APIKeyInfo(ctx)
	if err != nil {
		return exchangeResult{}, exchangeReadFailure("KEY", err)
	}
	if info.UTA != 1 {
		return exchangeResult{}, ipc.NewRequestError("EXCHANGE_UNIFIED_REQUIRED")
	}
	if !exchangePermission(info, "AccountTransfer") {
		return exchangeResult{}, ipc.NewRequestError("EXCHANGE_TRANSFER_PERMISSION")
	}
	balances, err := exchangeBalances(ctx, client, asset.Symbol)
	if err != nil {
		return exchangeResult{}, exchangeReadFailure("BALANCE", err)
	}
	if !exchangeAtLeast(exchangeAvailable(balances, from, asset.Symbol), value) {
		code := "EXCHANGE_FUNDING_INSUFFICIENT"
		if from == "UNIFIED" {
			code = "EXCHANGE_UNIFIED_INSUFFICIENT"
		}
		return exchangeResult{}, ipc.NewRequestError(code)
	}
	request := bybit.TransferRequest{Coin: asset.Symbol, Amount: value, FromAccountType: from, ToAccountType: to}
	return c.executeExchangeAt(epoch, client, exchangeIntent{Kind: "transfer", AccountID: accountID, AssetID: assetID, Transfer: &request, Fee: "0", FeeCoin: asset.Symbol})
}
