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

func TestDepositCreditDiagnosticsOnlyAdmitFixedFieldsAtCreditStage(t *testing.T) {
	causes := []string{"DEPOSIT_RECORD_RESPONSE", "DEPOSIT_RECORD_PAGE", "DEPOSIT_RECORD_CURSOR", "DEPOSIT_RECORD_ID", "DEPOSIT_RECORD_COIN", "DEPOSIT_RECORD_TRANSACTION", "DEPOSIT_RECORD_CHAIN", "DEPOSIT_RECORD_AMOUNT", "DEPOSIT_RECORD_ADDRESS", "DEPOSIT_RECORD_SOURCE", "DEPOSIT_RECORD_MEMO", "DEPOSIT_RECORD_STATUS", "DEPOSIT_RECORD_FEE", "DEPOSIT_RECORD_TIME", "DEPOSIT_RECORD_CONFIRMATIONS", "DEPOSIT_RECORD_TYPE", "DEPOSIT_RECORD_REVIEW", "CREDIT_TRANSACTION", "CREDIT_COIN", "CREDIT_CHAIN", "CREDIT_ADDRESS", "CREDIT_SOURCE", "CREDIT_MEMO", "CREDIT_DUPLICATE", "CREDIT_PAGINATION", "CREDIT_AMOUNT", "CREDIT_FEE"}
	for _, cause := range causes {
		code := PublicErrorCode(NewExchangeReadError("DEPOSIT_CREDIT", cause))
		stage, field, ok := ExchangeReadErrorParts(code)
		if !ok || stage != "DEPOSIT_CREDIT" || field != cause || SafeErrorCode(code) != code {
			t.Fatal("fixed deposit diagnostic was lost", code)
		}
		if PublicErrorCode(NewExchangeReadError("KEY", cause)) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("deposit diagnostic admitted for unrelated endpoint", cause)
		}
	}
	for _, shape := range []string{"NULL", "EMPTY", "TYPE", "VALUE", "SHADOW"} {
		cause := "DEPOSIT_RECORD_REVIEW_" + shape
		if code := PublicErrorCode(NewExchangeReadError("DEPOSIT_CREDIT", cause)); code != "EXCHANGE_READ_DEPOSIT_CREDIT__"+cause {
			t.Fatal("safe review shape lost", code)
		}
		if code := PublicErrorCode(NewExchangeReadError("KEY", cause)); code != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("review shape escaped credit stage", code)
		}
	}
	for _, cause := range []string{"DEPOSIT_RECORD_API_KEY_CANARY", "CREDIT_PRIVATE_RESPONSE_CANARY", "DEPOSIT_RECORD_MEMO/secret", "CREDIT_FEE\nsecret"} {
		if PublicErrorCode(NewExchangeReadError("DEPOSIT_CREDIT", cause)) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("provider text entered deposit diagnostic", cause)
		}
	}
}
