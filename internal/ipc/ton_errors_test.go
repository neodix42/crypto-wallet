package ipc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTONDeploymentSafeFailureCodesRejectProviderText(t *testing.T) {
	const raw = "CANARY-public-provider-body API-key-path /private/wallet"
	for _, code := range []string{"TON_RPC_RATE_LIMITED", "TON_DEPLOYMENT_UNAVAILABLE", "TON_DEPLOYMENT_RECORD_FAILED", "TON_DEPLOYMENT_INVALID_PLAN"} {
		if SafeErrorCode(code) != code || NewRequestError(code).Error() != code || PublicErrorCode(fmt.Errorf(raw+": %w", NewRequestError(code))) != code {
			t.Fatal("safe TON classification lost", code)
		}
		for _, untrusted := range []string{code + " " + raw, code + "\n" + raw, strings.ToLower(code)} {
			if SafeErrorCode(untrusted) != "REQUEST_FAILED_STATE_UNKNOWN" || PublicErrorCode(errors.New(untrusted)) != "REQUEST_FAILED_STATE_UNKNOWN" {
				t.Fatal("untrusted TON message became an allowed code")
			}
		}
	}
}
