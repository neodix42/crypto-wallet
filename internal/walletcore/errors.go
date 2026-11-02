package walletcore

import (
	"errors"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/binance"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/exchange/kucoin"
	"localwallet/internal/ipc"
	"localwallet/internal/vault"
)

// Only fixed categories cross public IPC, including failures discovered after
// approval or submission. A category describes a failure; it is not proof that
// an economic operation was never accepted and never authorizes an auto-retry.
func publicRequestErrorCode(err error) string {
	if code := ipc.ClassifyErrorCode(err); code != "REQUEST_FAILED_STATE_UNKNOWN" {
		return code
	}
	for _, entry := range []struct {
		err  error
		code string
	}{
		{ton.ErrRateLimited, "TON_RPC_RATE_LIMITED"},
		{vault.ErrUnlock, "AUTHENTICATION_FAILED"},
		{vault.ErrFormat, "STORAGE_CORRUPT"},
		{vault.ErrFile, "STORAGE_UNAVAILABLE"},
		{chainutil.ErrInvalid, "CHAIN_DATA_INVALID"},
		{bybit.ErrAuthorization, "PROVIDER_AUTHENTICATION_FAILED"},
		{binance.ErrAuthorization, "PROVIDER_AUTHENTICATION_FAILED"},
		{bybit.ErrUnavailable, "PROVIDER_UNAVAILABLE"},
		{binance.ErrUnavailable, "PROVIDER_UNAVAILABLE"},
		{kucoin.ErrUnavailable, "PROVIDER_UNAVAILABLE"},
		{bybit.ErrInvalidResponse, "PROVIDER_INVALID_RESPONSE"},
		{binance.ErrInvalidResponse, "PROVIDER_INVALID_RESPONSE"},
		{kucoin.ErrResponse, "PROVIDER_INVALID_RESPONSE"},
		{bybit.ErrInvalidRequest, "INVALID_ARGUMENT"},
		{binance.ErrInvalidRequest, "INVALID_ARGUMENT"},
		{kucoin.ErrRequest, "INVALID_ARGUMENT"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	var httpFailure *chainutil.HTTPError
	if errors.As(err, &httpFailure) && httpFailure != nil {
		return ipc.HTTPStatusErrorCode(httpFailure.Status)
	}
	var transportFailure *chainutil.TransportError
	if errors.As(err, &transportFailure) && transportFailure != nil {
		return "NETWORK_UNAVAILABLE"
	}
	var bybitFailure *bybit.APIError
	if errors.As(err, &bybitFailure) && bybitFailure != nil {
		if bybitFailure.HTTPStatus != 0 {
			return ipc.HTTPStatusErrorCode(bybitFailure.HTTPStatus)
		}
		// REST UTA codes, not the different WebSocket error-code namespace.
		// https://bybit-exchange.github.io/docs/v5/error
		switch bybitFailure.Code {
		case -2015, 10003, 10004, 10007:
			return "PROVIDER_AUTHENTICATION_FAILED"
		case 10005, 10008, 10009, 10010, 10024, 10027:
			return "PROVIDER_ACCESS_DENIED"
		case 10006, 429:
			return "PROVIDER_RATE_LIMITED"
		case -1, 10002:
			return "PROVIDER_CLOCK_SKEW"
		case 10000:
			return "NETWORK_TIMEOUT"
		case 10016:
			return "PROVIDER_UNAVAILABLE"
		default:
			if bybitFailure.Code != 0 {
				return "PROVIDER_REQUEST_REJECTED"
			}
		}
	}
	var binanceFailure *binance.APIError
	if errors.As(err, &binanceFailure) && binanceFailure != nil {
		if binanceFailure.HTTPStatus != 0 {
			return ipc.HTTPStatusErrorCode(binanceFailure.HTTPStatus)
		}
		// https://developers.binance.com/en/docs/products/spot/errors
		switch binanceFailure.Code {
		case -1022, -2014:
			return "PROVIDER_AUTHENTICATION_FAILED"
		case -1002, -2015:
			return "PROVIDER_ACCESS_DENIED"
		case -1003, -1015:
			return "PROVIDER_RATE_LIMITED"
		case -1021:
			return "PROVIDER_CLOCK_SKEW"
		case -1007:
			return "NETWORK_TIMEOUT"
		case -1001, -1006, -1008, -1016:
			return "PROVIDER_UNAVAILABLE"
		default:
			if binanceFailure.Code != 0 {
				return "PROVIDER_REQUEST_REJECTED"
			}
		}
	}
	return "REQUEST_FAILED_STATE_UNKNOWN"
}
