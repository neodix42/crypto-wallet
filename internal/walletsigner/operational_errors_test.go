package walletsigner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/vault"
)

func TestSignerOperationalReasonsStayTypedThroughActualResponseFrames(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{ipc.NewRequestError("ACCOUNT_RECOVERY_STORAGE"), "ACCOUNT_RECOVERY_STORAGE"},
		{ipc.NewRequestError("INVALID_ARGUMENT"), "INVALID_ARGUMENT"},
		{vault.ErrUnlock, "AUTHENTICATION_FAILED"}, {vault.ErrFormat, "STORAGE_CORRUPT"}, {vault.ErrFile, "STORAGE_UNAVAILABLE"},
		{context.Canceled, "REQUEST_CANCELLED"}, {context.DeadlineExceeded, "NETWORK_TIMEOUT"},
		{&os.PathError{Op: "open", Path: "CANARY-private-wallet-path", Err: os.ErrPermission}, "STORAGE_PERMISSION_DENIED"},
		{&net.DNSError{Err: "CANARY-private-provider-body", IsTimeout: true}, "NETWORK_TIMEOUT"},
		{errors.New("AUTHENTICATION_FAILED CANARY-api-secret"), "REQUEST_DENIED"},
	} {
		var wire bytes.Buffer
		err := fmt.Errorf("CANARY-password-key-path-provider-body: %w", tc.err)
		if requestErrorCode(err) != tc.code {
			t.Fatal("signer reason lost", requestErrorCode(err), tc.code)
		}
		if e := respond(&ipc.Channel{W: &wire}, ipc.Message{Version: 1, ID: "public-failure", Session: "public-fixture-session", Method: "unlock"}, func() (json.RawMessage, error) { return []byte(`{"mustNotPublish":"CANARY"}`), err }); e != nil {
			t.Fatal(e)
		}
		frame, e := (&ipc.Channel{R: bytes.NewReader(wire.Bytes())}).Read()
		if e != nil || frame.Error != tc.code || string(frame.Body) != "null" || strings.Contains(wire.String(), "CANARY") || ipc.PublicErrorCode(ipc.NewRequestError(frame.Error)) != tc.code {
			t.Fatal("signer public frame lost reason or leaked context", e)
		}
	}
}
