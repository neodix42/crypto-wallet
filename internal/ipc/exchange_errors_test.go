package ipc

import (
	"errors"
	"testing"
)

func TestExchangeReadDiagnosticsRejectTextAndKeepCanonicalNumericCodes(t *testing.T) {
	for _, stage := range []string{"KEY", "COIN", "BALANCE", "INSTRUMENT", "TICKER", "FEE", "DEPOSIT_ADDRESS", "WALLET_FEE"} {
		for _, cause := range []string{"BYBIT_10005", "BYBIT_-1", "BYBIT_999999", "NETWORK_TIMEOUT", "PROVIDER_INVALID_RESPONSE", "EXCHANGE_STATE_INVALID"} {
			code := PublicErrorCode(NewExchangeReadError(stage, cause))
			s, c, ok := ExchangeReadErrorParts(code)
			if !ok || s != stage || c != cause || SafeErrorCode(code) != code {
				t.Fatal("lost safe read diagnostic", code)
			}
			if PublicErrorCode(errors.New(code)) != "REQUEST_FAILED_STATE_UNKNOWN" {
				t.Fatal("untyped error text gained diagnostic authority")
			}
		}
	}
	for _, cause := range []string{"BYBIT_0", "BYBIT_010005", "BYBIT_+10005", "BYBIT_1e5", "BYBIT_9223372036854775808", "BYBIT_10005 API_KEY_CANARY", "BYBIT_10005\n", "NETWORK_TIMEOUT/private-url", "EXCHANGE_READ_KEY__BYBIT_10005", "CANARY"} {
		if PublicErrorCode(NewExchangeReadError("KEY", cause)) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("unsafe diagnostic admitted", cause)
		}
	}
	if PublicErrorCode(NewExchangeReadError("POST_WITHDRAW_CANARY", "BYBIT_10005")) != "REQUEST_FAILED_STATE_UNKNOWN" {
		t.Fatal("an unreviewed write stage became a read diagnostic")
	}
	for _, cause := range []string{"TON_RPC_RATE_LIMITED", "PAYMENT_QUOTE_INSUFFICIENT_FUNDS", "PAYMENT_QUOTE_BALANCE_UNAVAILABLE", "PAYMENT_QUOTE_UNSTABLE", "PAYMENT_QUOTE_UNAVAILABLE"} {
		if _, _, ok := ExchangeReadErrorParts(PublicErrorCode(NewExchangeReadError("WALLET_FEE", cause))); !ok {
			t.Fatal("known fee failure lost")
		}
		if PublicErrorCode(NewExchangeReadError("KEY", cause)) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("fee reason admitted in the wrong read stage")
		}
	}
}
