package signer

import (
	"localwallet/internal/exchange/binance"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/exchange/kucoin"
	"localwallet/internal/vault"
)

func exchangeOperationRequest(operation vault.ExchangeOperation, timestamp int64) (bybit.AuthRequest, error) {
	switch operation.Endpoint {
	case "https://api.binance.com":
		return binance.OperationRequest(operation.Endpoint, operation.Kind, operation.Payload, timestamp)
	case "https://api.kucoin.com":
		return kucoin.OperationRequest(operation.Endpoint, operation.Kind, operation.Payload, timestamp)
	default:
		request := bybit.AuthRequest{Endpoint: operation.Endpoint, Method: "POST", Path: exchangePath(operation.Kind), Payload: operation.Payload, TimestampMS: timestamp, ReceiveWindowMS: 5000}
		return request, bybit.ValidateAuthRequest(request)
	}
}

func exchangeRequestPolicy(provider string, request bybit.AuthRequest) (bool, error) {
	switch provider {
	case "binance":
		return binance.IsReadOnly(request), binance.ValidateAuthRequest(request)
	case "kucoin":
		return kucoin.IsReadOnly(request), kucoin.ValidateAuthRequest(request)
	default:
		return request.Method == "GET", bybit.ValidateAuthRequest(request)
	}
}

func exchangeRequestAuth(provider string, credential *vault.ExchangeCredentials, request bybit.AuthRequest) (bybit.Auth, error) {
	switch provider {
	case "binance":
		return binance.HMAC(string(credential.APIKey), credential.Secret, request)
	case "kucoin":
		return kucoin.HMAC(string(credential.APIKey), credential.Secret, credential.Passphrase, credential.KeyVersion, request)
	default:
		return bybit.HMAC(string(credential.APIKey), credential.Secret, request)
	}
}
