package walletsigner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/signer"
)

func TestAccountRecoveryTypedFramesCarrySafeKnownReasonsOnly(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{signer.ErrAccountRecoveryInvalidRange, "ACCOUNT_RECOVERY_INVALID_RANGE"},
		{signer.ErrAccountRecoveryLimit, "ACCOUNT_RECOVERY_LIMIT"},
		{signer.ErrAccountRecoveryDerivation, "ACCOUNT_RECOVERY_DERIVATION"},
		{signer.ErrAccountRecoveryStorage, "ACCOUNT_RECOVERY_STORAGE"},
		{signer.ErrAccountRecoveryStale, "ACCOUNT_RECOVERY_STALE"},
		{signer.ErrAccountRecoveryCancelled, "REQUEST_CANCELLED"},
		{signer.ErrAccountRecoveryLocked, "WALLET_LOCKED"},
	} {
		t.Run(test.code, func(t *testing.T) {
			var wire bytes.Buffer
			wrapped := fmt.Errorf("CANARY-private-seed-password-path: %w", test.err)
			if requestErrorCode(wrapped) != test.code || requestErrorCode(errors.New(test.code)) == test.code {
				t.Fatal("recovery code lost typed provenance")
			}
			if err := respond(&ipc.Channel{W: &wire}, ipc.Message{Version: 1, ID: "recovery", Session: "public-fixture-session", Method: "discoverAccounts"}, func() (json.RawMessage, error) {
				return nil, wrapped
			}); err != nil {
				t.Fatal(err)
			}
			frame, err := (&ipc.Channel{R: bytes.NewReader(wire.Bytes())}).Read()
			if err != nil || frame.Error != test.code || string(frame.Body) != "null" || bytes.Contains(wire.Bytes(), []byte("CANARY")) {
				t.Fatal("recovery frame leaked private details or lost safe category", err)
			}
		})
	}
}

func TestAccountRecoveryDispatchRequiresExplicitBoundedRangeAndReusesExisting(t *testing.T) {
	e, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, ui)
	call := func(raw string) error {
		_, err := handle(e, ipc.Message{Method: "discoverAccounts", Body: json.RawMessage(raw)}, "testnet")
		return err
	}
	for _, raw := range []string{`null`, `[]`, `false`, `{}`, `{"start":0}`, `{"end":0}`, `{"start":null,"end":0}`, `{"start":0,"end":null}`, `{"start":-1,"end":0}`, `{"start":"0","end":0}`, `{"start":0,"end":0,"unknown":true}`, `{"start":0,"start":0,"end":0}`, `{"start":0,"end":20}`, `{"start":1,"end":0}`, `{"start":0,"end":1000000}`} {
		if code := requestErrorCode(call(raw)); code != "ACCOUNT_RECOVERY_INVALID_RANGE" {
			t.Fatal("invalid recovery range lost safe category", raw, code)
		}
	}
	initial := len(e.Snapshot().Accounts)
	inputCalls := ui.inputCalls
	if err := call(`{"start":0,"end":0}`); err != nil || len(e.Snapshot().Accounts) != initial || ui.inputCalls != inputCalls {
		t.Fatal("existing recovery failed, duplicated wallets or requested another password", err)
	}
	dispatch(t, e, "deleteGroup", map[string]uint32{"index": 0})
	if err := call(`{"start":0,"end":0}`); err != nil {
		t.Fatal("hidden recovery failed", err)
	}
	for _, account := range e.Snapshot().Accounts {
		if account.Hidden {
			t.Fatal("recovery did not restore all existing active descriptors")
		}
	}
	if e.Snapshot().Presentation.DeletedGroups["0"] {
		t.Fatal("recovery did not clear archived group")
	}
	ui.approve = false
	if code := requestErrorCode(call(`{"start":1,"end":1}`)); code != "REQUEST_CANCELLED" || len(e.Snapshot().Accounts) != initial {
		t.Fatal("recovery cancellation lost category or mutated accounts", code)
	}
	e.Lock()
	if code := requestErrorCode(call(`{"start":0,"end":0}`)); code != "WALLET_LOCKED" {
		t.Fatal("locked recovery lost category", code)
	}
}
