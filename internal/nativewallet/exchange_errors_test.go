package nativewallet

import (
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/ipc"
)

func TestExchangeReadDialogsShowExactStageCodeAndReasonWithoutSecretText(t *testing.T) {
	for _, tc := range []struct{ stage, cause, hint string }{{"BALANCE", "BYBIT_10005", "permission"}, {"DEPOSIT_ADDRESS", "BYBIT_10010", "IP"}, {"KEY", "BYBIT_10003", "region"}, {"COIN", "BYBIT_123456", "123456"}, {"FEE", "PROVIDER_INVALID_RESPONSE", "malformed"}, {"INSTRUMENT", "EXCHANGE_STATE_INVALID", "authorization"}, {"TICKER", "REQUEST_FAILED_STATE_UNKNOWN", "read failed"}} {
		a, _, _ := fixture(t)
		err := fmt.Errorf("PRIVATE_API_SECRET_CANARY: %w", ipc.NewExchangeReadError(tc.stage, tc.cause))
		a.reportError("Preparing deposit to Bybit", err)
		v := a.view()
		if v.ErrorNotice == nil || !strings.Contains(v.ErrorNotice.Message, tc.hint) || !strings.Contains(v.ErrorNotice.Message, "did not authorize") || strings.Contains(viewText(v), "CANARY") || v.Status != "" {
			t.Fatal("exchange diagnostic missing or exposed in status/secret text", tc.stage, v.ErrorNotice)
		}
	}
	for _, cause := range []string{"PAYMENT_QUOTE_INSUFFICIENT_FUNDS", "PAYMENT_QUOTE_UNAVAILABLE", "TON_RPC_RATE_LIMITED"} {
		a, _, _ := fixture(t)
		a.reportError("Preparing deposit to Bybit", ipc.NewExchangeReadError("WALLET_FEE", cause))
		v := a.view()
		if v.ErrorNotice == nil || !strings.Contains(v.ErrorNotice.Message, "native network fee") || !strings.Contains(v.ErrorNotice.Message, safeError(ipc.NewRequestError(cause))) || v.Status != "" {
			t.Fatal("fee failure collapsed into an unidentified error", cause)
		}
	}
}
