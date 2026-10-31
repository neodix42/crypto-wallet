package walletsigner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/signer"
	"localwallet/internal/uiprompt"
	"strings"
	"testing"
)

type failureNoticeUI struct {
	dispatchUI
	title, message string
}

func (u *failureNoticeUI) Notice(title, message string) { u.title, u.message = title, message }

func TestNativeFailureGuidanceAndPublicFramesContainNoInputOrPath(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{signer.ErrPasswordPolicy, "PASSWORD_POLICY"}, {uiprompt.ErrInputTooLong, "INPUT_TOO_LONG"},
		{ton.ErrRateLimited, "TON_RPC_RATE_LIMITED"}, {errors.New("TON_RPC_RATE_LIMITED"), "REQUEST_DENIED"},
		{signer.ErrPasswordMismatch, "PASSWORD_MISMATCH"}, {signer.ErrInvalidPhrase, "INVALID_MNEMONIC"},
		{uiprompt.ErrCancelled, "REQUEST_CANCELLED"}, {signer.ErrDenied, "REQUEST_DENIED"}, {errors.New("PASSWORD_POLICY"), "REQUEST_DENIED"},
		{signer.ErrAddressBookInvalidInput, "ADDRESS_BOOK_INVALID_INPUT"},
		{signer.ErrAddressBookInvalidAddress, "ADDRESS_BOOK_INVALID_ADDRESS"},
		{signer.ErrAddressBookDuplicateName, "ADDRESS_BOOK_DUPLICATE_NAME"},
		{signer.ErrAddressBookDuplicateRecipient, "ADDRESS_BOOK_DUPLICATE_RECIPIENT"},
		{signer.ErrAddressBookLimit, "ADDRESS_BOOK_LIMIT"},
		{signer.ErrAddressBookStale, "ADDRESS_BOOK_STALE"},
		{signer.ErrAddressBookUnavailable, "ADDRESS_BOOK_UNAVAILABLE"},
		{signer.ErrAddressBookWriteFailed, "ADDRESS_BOOK_WRITE_FAILED"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := fmt.Errorf("CANARY-private-password-or-path: %w", tc.err)
			if code := requestErrorCode(err); code != tc.code {
				t.Fatalf("wrong public error code: %s", code)
			}
			ui := &failureNoticeUI{}
			notifyFailure(ui, err)
			if strings.Contains(ui.message, "CANARY") || ui.message != failureGuidance[tc.code] {
				t.Fatal("native notice included private context or incorrect guidance")
			}
			if ui.message != "" && ui.title != "Wallet request failed" {
				t.Fatal("native notice missing clear title")
			}
			out := &bytes.Buffer{}
			channel := &ipc.Channel{W: out}
			if e := respond(channel, ipc.Message{Version: 1, ID: "failure", Session: "public-fixture-session", Method: "create"}, func() (json.RawMessage, error) { return nil, err }); e != nil {
				t.Fatal(e)
			}
			reader := &ipc.Channel{R: bytes.NewReader(out.Bytes())}
			frame, e := reader.Read()
			if e != nil || frame.Error != tc.code || string(frame.Body) != "null" || strings.Contains(out.String(), "CANARY") {
				t.Fatal("response lost safe category or leaked context", e)
			}
		})
	}
	// Unit interfaces without a native notice do not introduce another input API.
	notifyFailure(&dispatchUI{}, signer.ErrPasswordPolicy)
}
