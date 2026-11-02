package ipc

import (
	"strconv"
	"strings"
)

// ExchangeReadErrorParts admits only reviewed read stages and fixed categories
// or a canonical numeric Bybit retCode. Provider messages never enter IPC.
func ExchangeReadErrorParts(code string) (stage, cause string, ok bool) {
	if len(code) > 80 || !strings.HasPrefix(code, "EXCHANGE_READ_") {
		return "", "", false
	}
	stage, cause, ok = strings.Cut(strings.TrimPrefix(code, "EXCHANGE_READ_"), "__")
	if !ok {
		return "", "", false
	}
	switch stage {
	case "KEY", "COIN", "BALANCE", "INSTRUMENT", "TICKER", "FEE", "EXECUTIONS", "DEPOSIT_ADDRESS", "DEPOSIT_CREDIT", "WALLET_FEE":
	default:
		return "", "", false
	}
	switch cause {
	case "METADATA_IDENTITY", "METADATA_DUPLICATE_CHAIN", "METADATA_FLAGS", "METADATA_DEPOSIT_LIMIT", "METADATA_WITHDRAWAL_LIMIT", "METADATA_PRECISION", "METADATA_CONFIRMATIONS":
		return stage, cause, stage == "COIN"
	case "REQUEST_FAILED_STATE_UNKNOWN", "EXCHANGE_STATE_INVALID", "REQUEST_CANCELLED", "REQUEST_DENIED", "WALLET_LOCKED", "INVALID_ARGUMENT",
		"PROVIDER_AUTHENTICATION_FAILED", "PROVIDER_ACCESS_DENIED", "PROVIDER_RATE_LIMITED", "PROVIDER_CLOCK_SKEW",
		"PROVIDER_UNAVAILABLE", "PROVIDER_INVALID_RESPONSE", "PROVIDER_REQUEST_REJECTED", "PROVIDER_REDIRECT_DENIED",
		"NETWORK_UNAVAILABLE", "NETWORK_TIMEOUT", "NETWORK_TLS_FAILED":
		return stage, cause, true
	case "TON_RPC_RATE_LIMITED", "PAYMENT_QUOTE_INSUFFICIENT_FUNDS", "PAYMENT_QUOTE_BALANCE_UNAVAILABLE", "PAYMENT_QUOTE_UNSTABLE", "PAYMENT_QUOTE_UNAVAILABLE":
		return stage, cause, stage == "WALLET_FEE"
	}
	if strings.HasPrefix(cause, "BYBIT_") {
		value := strings.TrimPrefix(cause, "BYBIT_")
		n, err := strconv.ParseInt(value, 10, 64)
		if err == nil && n != 0 && strconv.FormatInt(n, 10) == value {
			return stage, cause, true
		}
	}
	return "", "", false
}

func NewExchangeReadError(stage, cause string) error {
	return NewRequestError("EXCHANGE_READ_" + stage + "__" + cause)
}
