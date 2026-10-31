package ipc

import (
	"errors"
	"fmt"
	"testing"
)

func TestPaymentQuoteOnlyTypedValueFreeCategoriesCrossIPC(t *testing.T) {
	for _, code := range []string{"PAYMENT_QUOTE_INVALID_INPUT", "PAYMENT_QUOTE_BALANCE_UNAVAILABLE", "PAYMENT_QUOTE_INSUFFICIENT_FUNDS", "PAYMENT_QUOTE_UNAVAILABLE", "PAYMENT_QUOTE_INVALID_PLAN", "PAYMENT_QUOTE_UNSTABLE", "PAYMENT_PENDING", "PAYMENT_PENDING_UNAVAILABLE"} {
		if SafeErrorCode(code) != code || PublicErrorCode(fmt.Errorf("private provider body: %w", NewRequestError(code))) != code {
			t.Fatal("safe quote category lost", code)
		}
		if PublicErrorCode(errors.New(code)) != "REQUEST_FAILED_STATE_UNKNOWN" || SafeErrorCode(code+" private body") != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("untrusted provider string gained a quote category", code)
		}
	}
}
