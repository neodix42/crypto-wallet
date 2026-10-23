package ipc

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPublicFailureCodesNeverForwardInputOrUntrustedMessages(t *testing.T) {
	for _, code := range []string{"PASSWORD_POLICY", "INPUT_TOO_LONG", "PASSWORD_MISMATCH", "INVALID_MNEMONIC", "BACKUP_DESTINATION_UNSAFE", "BACKUP_EXISTS", "BACKUP_WRITE_FAILED", "BACKUP_VERIFY_FAILED", "RESTORE_SOURCE_UNSAFE", "REQUEST_CANCELLED", "REQUEST_FAILED_STATE_UNKNOWN"} {
		if got := PublicErrorCode(fmt.Errorf("private internal context: %w", NewRequestError(code))); got != code {
			t.Fatalf("lost safe failure category %s: %s", code, got)
		}
	}
	for _, raw := range []string{"", "CANARY-secret-value-or-path", "PASSWORD_POLICY\nCANARY-private-data", strings.Repeat("X", 10000)} {
		err := NewRequestError(raw)
		if err.Error() != "REQUEST_FAILED_STATE_UNKNOWN" || PublicErrorCode(err) != "REQUEST_FAILED_STATE_UNKNOWN" {
			t.Fatal("untrusted failure was forwarded")
		}
	}
	// Matching an ordinary error's text is insufficient to authorize a code.
	if PublicErrorCode(errors.New("PASSWORD_POLICY")) != "REQUEST_FAILED_STATE_UNKNOWN" || PublicErrorCode(nil) != "" {
		t.Fatal("public failure classification accepted arbitrary errors")
	}
	var missing *RequestError
	if PublicErrorCode(missing) != "REQUEST_FAILED_STATE_UNKNOWN" {
		t.Fatal("typed nil failure accepted")
	}
}
