package walletcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/binance"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/exchange/kucoin"
	"localwallet/internal/ipc"
	"localwallet/internal/vault"
)

func TestPublicRequestReasonsRetainOnlyFixedTypedCategories(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{ipc.NewRequestError("ACCOUNT_RECOVERY_INVALID_RANGE"), "ACCOUNT_RECOVERY_INVALID_RANGE"},
		{ipc.NewRequestError("WALLET_LOCKED"), "WALLET_LOCKED"},
		{ton.ErrRateLimited, "TON_RPC_RATE_LIMITED"}, {vault.ErrUnlock, "AUTHENTICATION_FAILED"}, {vault.ErrFormat, "STORAGE_CORRUPT"}, {vault.ErrFile, "STORAGE_UNAVAILABLE"},
		{context.Canceled, "REQUEST_CANCELLED"}, {context.DeadlineExceeded, "NETWORK_TIMEOUT"},
		{&os.PathError{Op: "read", Path: "CANARY-private-absolute-path", Err: os.ErrPermission}, "STORAGE_PERMISSION_DENIED"},
		{&net.DNSError{Err: "CANARY-private-RPC-host"}, "NETWORK_UNAVAILABLE"},
		{chainutil.ErrInvalid, "CHAIN_DATA_INVALID"},
		{bybit.ErrAuthorization, "PROVIDER_AUTHENTICATION_FAILED"}, {binance.ErrAuthorization, "PROVIDER_AUTHENTICATION_FAILED"},
		{bybit.ErrUnavailable, "PROVIDER_UNAVAILABLE"}, {binance.ErrUnavailable, "PROVIDER_UNAVAILABLE"}, {kucoin.ErrUnavailable, "PROVIDER_UNAVAILABLE"},
		{bybit.ErrInvalidRequest, "INVALID_ARGUMENT"}, {binance.ErrInvalidRequest, "INVALID_ARGUMENT"}, {kucoin.ErrRequest, "INVALID_ARGUMENT"},
		{bybit.ErrInvalidResponse, "PROVIDER_INVALID_RESPONSE"}, {binance.ErrInvalidResponse, "PROVIDER_INVALID_RESPONSE"}, {kucoin.ErrResponse, "PROVIDER_INVALID_RESPONSE"},
	} {
		err := fmt.Errorf("CANARY-private-password-api-key-provider-body: %w", tc.err)
		code := publicRequestErrorCode(err)
		if code != tc.code || ipc.SafeErrorCode(code) != code {
			t.Fatal("safe reason lost", code, tc.code)
		}
		var wire bytes.Buffer
		if e := (&ipc.Channel{W: &wire}).Write(ipc.Message{Version: 1, ID: "public-test", Session: "public-fixture-session", Method: "public-method", Body: []byte("null"), Error: code}); e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(wire.Bytes(), []byte("CANARY")) {
			t.Fatal("private context entered public frame")
		}
	}
	for _, raw := range []string{"ACCOUNT_RECOVERY_INVALID_RANGE", "WALLET_LOCKED", "NETWORK_TIMEOUT", "TON_RPC_RATE_LIMITED private-provider-token", `{"retCode":10003,"msg":"CANARY-password-api-key"}`} {
		if publicRequestErrorCode(errors.New(raw)) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("raw provider/error text spoofed a public category", raw)
		}
	}
}

func TestPublicExchangeAPIReasonNamespacesRemainTypedAndNeverUseBodies(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&bybit.APIError{Code: 10003}, "PROVIDER_AUTHENTICATION_FAILED"}, {&bybit.APIError{Code: 10004}, "PROVIDER_AUTHENTICATION_FAILED"}, {&bybit.APIError{Code: -2015}, "PROVIDER_AUTHENTICATION_FAILED"},
		{&bybit.APIError{Code: 10005}, "PROVIDER_ACCESS_DENIED"}, {&bybit.APIError{Code: 10009}, "PROVIDER_ACCESS_DENIED"}, {&bybit.APIError{Code: 10010}, "PROVIDER_ACCESS_DENIED"},
		{&bybit.APIError{Code: 10006}, "PROVIDER_RATE_LIMITED"}, {&bybit.APIError{Code: 10002}, "PROVIDER_CLOCK_SKEW"}, {&bybit.APIError{Code: 10000}, "NETWORK_TIMEOUT"}, {&bybit.APIError{Code: 10016}, "PROVIDER_UNAVAILABLE"}, {&bybit.APIError{Code: 10001}, "PROVIDER_REQUEST_REJECTED"},
		{&bybit.APIError{HTTPStatus: 403, Code: 10003}, "PROVIDER_ACCESS_DENIED"},
		{&binance.APIError{Code: -1022}, "PROVIDER_AUTHENTICATION_FAILED"}, {&binance.APIError{Code: -2014}, "PROVIDER_AUTHENTICATION_FAILED"}, {&binance.APIError{Code: -2015}, "PROVIDER_ACCESS_DENIED"}, {&binance.APIError{Code: -1003}, "PROVIDER_RATE_LIMITED"}, {&binance.APIError{Code: -1021}, "PROVIDER_CLOCK_SKEW"}, {&binance.APIError{Code: -1007}, "NETWORK_TIMEOUT"}, {&binance.APIError{Code: -1006}, "PROVIDER_UNAVAILABLE"}, {&binance.APIError{Code: -2010}, "PROVIDER_REQUEST_REJECTED"},
		{&binance.APIError{HTTPStatus: 401, Code: -1003}, "PROVIDER_AUTHENTICATION_FAILED"},
	} {
		if got := publicRequestErrorCode(fmt.Errorf("CANARY-private-exchange-body: %w", tc.err)); got != tc.code || strings.Contains(got, "CANARY") {
			t.Fatal("typed exchange reason misclassified", got, tc.code)
		}
	}
}

func TestActualRejectedRPCBodiesCannotReachPublicReason(t *testing.T) {
	const canary = "CANARY-private-request-path-provider-response-body"
	for _, tc := range []struct {
		status int
		code   string
	}{{301, "PROVIDER_REDIRECT_DENIED"}, {401, "PROVIDER_AUTHENTICATION_FAILED"}, {403, "PROVIDER_ACCESS_DENIED"}, {429, "PROVIDER_RATE_LIMITED"}, {500, "PROVIDER_UNAVAILABLE"}, {504, "NETWORK_TIMEOUT"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://invalid.example/?secret="+canary)
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(canary))
		}))
		err := chainutil.NewClient().Do(context.Background(), "GET", server.URL+"/"+canary, "", nil, nil)
		server.Close()
		if err == nil || publicRequestErrorCode(err) != tc.code || strings.Contains(err.Error(), canary) || strings.Contains(publicRequestErrorCode(err), canary) {
			t.Fatal("actual RPC body/path leaked or category lost", tc.status, err)
		}
	}
}
