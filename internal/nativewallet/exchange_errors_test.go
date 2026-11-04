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

func TestDepositCreditErrorsShowSpecificFixedReasonWithoutPrivateValues(t *testing.T) {
	for cause, hint := range map[string]string{"DEPOSIT_RECORD_ID": "unique record ID", "DEPOSIT_RECORD_TRANSACTION": "transaction identity", "DEPOSIT_RECORD_REVIEW": "Travel Rule review", "CREDIT_ADDRESS": "receiving address", "CREDIT_SOURCE": "sender", "CREDIT_MEMO": "memo", "CREDIT_FEE": "fee"} {
		a, _, _ := fixture(t)
		a.reportError("Move to Unified", fmt.Errorf("PRIVATE_PROVIDER_BODY_CANARY: %w", ipc.NewExchangeReadError("DEPOSIT_CREDIT", cause)))
		view := a.view()
		if view.ErrorNotice == nil || !strings.Contains(view.ErrorNotice.Message, hint) || strings.Contains(viewText(view), "CANARY") || view.Status != "" {
			t.Fatal("deposit reason was generic, leaked private data or entered status bar", cause, view.ErrorNotice)
		}
	}
}

func TestBybitFollowupReadDiagnosticsKeepTheirExactPublicStage(t *testing.T) {
	for stage, heading := range map[string]string{"ORDER_STATUS": "Reading the original spot order's status", "TRANSFER_STATUS": "Reading the original internal transfer's status"} {
		reason, ok := exchangeReadReason("EXCHANGE_READ_" + stage + "__PROVIDER_INVALID_RESPONSE")
		if !ok || !strings.HasPrefix(reason, heading+" failed.") || !strings.Contains(reason, "did not authorize") {
			t.Fatal("original-operation read lost its public diagnostic", stage, reason)
		}
	}
}
