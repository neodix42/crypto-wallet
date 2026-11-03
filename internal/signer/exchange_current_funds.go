package signer

import (
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

// Legacy linked operations retain their conservative global uncertainty gate.
// A separately approved current-funds request does not replace an older one.
// Only a live, unconsumed authorization capability remains serialized.
func (e *Engine) exchangePreparationBlocked(other vault.ExchangeOperation, currentFunds bool) bool {
	if !currentFunds {
		return exchangeUnresolved(other.State) || other.State == "AUTHORIZED"
	}
	return other.State == "AUTHORIZED" && other.ExpiresAt > time.Now().Unix() && other.Digest != "" && e.exchangeAuthorizations[other.ID] == other.Digest
}

// CurrentFunds is authenticated in the encrypted operation and immutable
// digest. Rechecking its exact scope before approval/authentication prevents
// this policy from becoming a generic uncertainty bypass for old requests.
func exchangeCurrentFundsPolicy(operation vault.ExchangeOperation, asset model.Asset) bool {
	provider, known := exchange.ProviderForEndpoint(operation.Endpoint)
	if !operation.CurrentFunds || !known || provider.ID != "bybit" {
		return false
	}
	switch operation.Kind {
	case "transfer":
		var request bybit.TransferRequest
		if strictjson.Decode([]byte(operation.Payload), &request) != nil || bybit.ValidateTransfer(request) != nil || request.TransferID != exchangeUUID(operation.ID) || request.Coin != asset.Symbol {
			return false
		}
		return request.FromAccountType == "FUND" && request.ToAccountType == "UNIFIED" || request.FromAccountType == "UNIFIED" && request.ToAccountType == "FUND"
	case "swap":
		var request bybit.SpotOrder
		if strictjson.Decode([]byte(operation.Payload), &request) != nil || bybit.ValidateSpotOrder(request) != nil || request.OrderLinkID != operation.ID {
			return false
		}
		base, _, pair := bybit.SpotPair(request.Symbol)
		return pair && base == asset.Symbol && request.Category == "spot" && request.OrderType == "Market" && request.IsLeverage == 0
	case "withdraw":
		var request bybit.WithdrawalRequest
		return strictjson.Decode([]byte(operation.Payload), &request) == nil && bybit.ValidateWithdrawal(request) == nil && request.RequestID == operation.ID && request.AccountType == "FUND" && request.ForceChain == 1 && request.FeeType == 1 && request.Coin == asset.Symbol
	}
	return false
}
