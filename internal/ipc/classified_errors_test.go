package ipc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestNewPublicReasonCodesAreClosedAndNeverTrustErrorText(t *testing.T) {
	codes := []string{"ACCOUNT_RECOVERY_INVALID_RANGE", "ACCOUNT_RECOVERY_LIMIT", "ACCOUNT_RECOVERY_DERIVATION", "ACCOUNT_RECOVERY_STORAGE", "ACCOUNT_RECOVERY_STALE", "REQUEST_CANCELLED", "REQUEST_DENIED", "INVALID_ARGUMENT", "WALLET_LOCKED", "AUTHENTICATION_FAILED", "ACCESS_DENIED", "STORAGE_PERMISSION_DENIED", "STORAGE_UNAVAILABLE", "STORAGE_CORRUPT", "NETWORK_UNAVAILABLE", "NETWORK_TIMEOUT", "NETWORK_TLS_FAILED", "PROVIDER_RATE_LIMITED", "PROVIDER_AUTHENTICATION_FAILED", "PROVIDER_ACCESS_DENIED", "PROVIDER_UNAVAILABLE", "PROVIDER_INVALID_RESPONSE", "PROVIDER_REQUEST_REJECTED", "PROVIDER_REDIRECT_DENIED", "PROVIDER_CLOCK_SKEW", "CHAIN_DATA_INVALID"}
	for _, code := range codes {
		wrapped := fmt.Errorf("CANARY-private-password-path-body: %w", NewRequestError(code))
		if SafeErrorCode(code) != code || PublicErrorCode(wrapped) != code || ClassifyErrorCode(wrapped) != code {
			t.Fatal("lost explicitly authored reason", code)
		}
		for _, raw := range []string{code, code + " CANARY-private-context", `{"code":"` + code + `","body":"CANARY"}`} {
			if PublicErrorCode(errors.New(raw)) != "REQUEST_FAILED_STATE_UNKNOWN" || ClassifyErrorCode(errors.New(raw)) != "REQUEST_FAILED_STATE_UNKNOWN" {
				t.Fatal("text spoofed a typed public category", raw)
			}
		}
	}
}

func TestTypedOperationalReasonsDropPathsAddressesAndPrivateContext(t *testing.T) {
	const canary = "CANARY-password-recovery-phrase-api-key-response-body"
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "REQUEST_CANCELLED"}, {context.DeadlineExceeded, "NETWORK_TIMEOUT"},
		{&os.PathError{Op: "open", Path: "/private/" + canary, Err: os.ErrPermission}, "STORAGE_PERMISSION_DENIED"},
		{&os.LinkError{Op: "rename", Old: canary, New: canary, Err: os.ErrPermission}, "STORAGE_PERMISSION_DENIED"},
		{&os.PathError{Op: "write", Path: canary, Err: errors.New(canary)}, "STORAGE_UNAVAILABLE"},
		{os.ErrPermission, "ACCESS_DENIED"},
		{&net.DNSError{Err: canary, Name: canary}, "NETWORK_UNAVAILABLE"},
		{&net.DNSError{Err: canary, Name: canary, IsTimeout: true}, "NETWORK_TIMEOUT"},
		{&url.Error{Op: "Get", URL: "https://provider.invalid/?key=" + canary, Err: &net.DNSError{Err: canary}}, "NETWORK_UNAVAILABLE"},
		{&tls.CertificateVerificationError{Err: errors.New(canary)}, "NETWORK_TLS_FAILED"},
		{x509.UnknownAuthorityError{}, "NETWORK_TLS_FAILED"},
		{x509.HostnameError{Host: canary}, "NETWORK_TLS_FAILED"},
		{x509.CertificateInvalidError{Detail: canary}, "NETWORK_TLS_FAILED"},
	} {
		code := ClassifyErrorCode(fmt.Errorf(canary+": %w", tc.err))
		raw, _ := json.Marshal(map[string]string{"error": code})
		if code != tc.code || strings.Contains(string(raw), canary) {
			t.Fatal("typed category lost or private context leaked", code, tc.code)
		}
	}
	if ClassifyErrorCode(nil) != "" {
		t.Fatal("nil error became a failure")
	}
	var missing *RequestError
	if ClassifyErrorCode(missing) != "REQUEST_FAILED_STATE_UNKNOWN" {
		t.Fatal("typed nil error accepted")
	}
	var missingFile *os.PathError
	if ClassifyErrorCode(missingFile) != "REQUEST_FAILED_STATE_UNKNOWN" {
		t.Fatal("typed nil file error accepted")
	}
}

func TestHTTPReasonCategoriesAreNumericOnlyAndPreserveUncertainSubmission(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{301, "PROVIDER_REDIRECT_DENIED"}, {400, "PROVIDER_REQUEST_REJECTED"}, {401, "PROVIDER_AUTHENTICATION_FAILED"}, {403, "PROVIDER_ACCESS_DENIED"}, {404, "PROVIDER_REQUEST_REJECTED"}, {408, "NETWORK_TIMEOUT"}, {429, "PROVIDER_RATE_LIMITED"}, {500, "PROVIDER_UNAVAILABLE"}, {503, "PROVIDER_UNAVAILABLE"}, {504, "NETWORK_TIMEOUT"}, {599, "PROVIDER_UNAVAILABLE"}, {-1, "REQUEST_FAILED_STATE_UNKNOWN"}, {0, "REQUEST_FAILED_STATE_UNKNOWN"}, {200, "REQUEST_FAILED_STATE_UNKNOWN"}, {600, "REQUEST_FAILED_STATE_UNKNOWN"}} {
		if code := HTTPStatusErrorCode(tc.status); code != tc.code || SafeErrorCode(code) != code {
			t.Fatal("wrong HTTP reason", tc.status, code)
		}
	}
}
