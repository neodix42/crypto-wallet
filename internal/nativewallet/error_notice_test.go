package nativewallet

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/webui"
)

func TestDeterministicRecoveryRangeIsCanonicalAndBounded(t *testing.T) {
	for _, test := range []struct {
		from, count string
		start, end  uint32
		valid       bool
	}{
		{"0", "1", 0, 0, true}, {"0", "20", 0, 19, true},
		{"999980", "20", 999980, 999999, true}, {"999999", "1", 999999, 999999, true},
		{"", "1", 0, 0, false}, {"0", "0", 0, 0, false}, {"0", "21", 0, 0, false},
		{"1000000", "1", 0, 0, false}, {"999999", "2", 0, 0, false},
		{"00", "1", 0, 0, false}, {"0", "01", 0, 0, false}, {"+1", "1", 0, 0, false},
		{"-1", "1", 0, 0, false}, {"1.0", "1", 0, 0, false}, {"1e1", "1", 0, 0, false},
		{" 1", "1", 0, 0, false}, {"0", "4294967295", 0, 0, false},
	} {
		start, end, valid := recoveryRange(test.from, test.count)
		if valid != test.valid || valid && (start != test.start || end != test.end) {
			t.Fatalf("wrong recovery bound for %q/%q", test.from, test.count)
		}
	}
}

func TestInvalidAccountRecoveryShowsPersistentReasonWithoutStartingBackend(t *testing.T) {
	a, _, core := fixture(t)
	var calls atomic.Int32
	core.handler = func(uint64, string, any, any) error { calls.Add(1); return nil }
	a.fields["from"], a.fields["count"] = "0", "21"
	a.event(webui.Event{ID: "discoverAccounts"})
	v := a.view()
	if calls.Load() != 0 || a.busy || v.ErrorNotice == nil || v.ErrorNotice.Code != "ACCOUNT_RECOVERY_INVALID_RANGE" || v.ErrorNotice.Operation != "Recover deterministic accounts" || !strings.Contains(v.ErrorNotice.Message, "1 through 20") || v.Status != "" {
		t.Fatal("invalid recovery did not stop locally with a complete error dialog")
	}
	id := v.ErrorNotice.ID
	a.statusUntil = time.Now().Add(-time.Second)
	a.lastStatus = time.Now()
	a.tick()
	if a.view().ErrorNotice == nil || a.view().ErrorNotice.ID != id || calls.Load() != 0 {
		t.Fatal("error expired or started background work before acknowledgement")
	}
	a.event(webui.Event{ID: "ackError", Value: "stale"})
	if a.view().ErrorNotice == nil {
		t.Fatal("stale acknowledgement dismissed an error")
	}
	a.event(webui.Event{ID: "ackError", Value: id})
	if a.view().ErrorNotice != nil {
		t.Fatal("exact error acknowledgement was not applied")
	}
}

func TestOperationalFailuresExposeTypedReasonsWithoutPrivateErrorText(t *testing.T) {
	const canary = "PRIVATE-password-seed-path-and-api-key-CANARY"
	for _, test := range []struct {
		err  error
		code string
	}{
		{ipc.NewRequestError("ACCOUNT_RECOVERY_DERIVATION"), "ACCOUNT_RECOVERY_DERIVATION"},
		{ipc.NewRequestError("ACCOUNT_RECOVERY_STORAGE"), "ACCOUNT_RECOVERY_STORAGE"},
		{ipc.NewRequestError("PROVIDER_RATE_LIMITED"), "PROVIDER_RATE_LIMITED"},
		{&os.PathError{Op: "write", Path: canary, Err: os.ErrPermission}, "STORAGE_PERMISSION_DENIED"},
		{&url.Error{Op: "Post", URL: "https://" + canary, Err: &net.DNSError{Err: canary, IsTimeout: true}}, "NETWORK_TIMEOUT"},
		{errors.New(canary), "REQUEST_FAILED_STATE_UNKNOWN"},
	} {
		a, _, _ := fixture(t)
		a.reportError("Recover deterministic accounts", fmt.Errorf("%s: %w", canary, test.err))
		v := a.view()
		if v.ErrorNotice == nil || v.ErrorNotice.Code != test.code || len(v.ErrorNotice.Message) < 40 || v.Status != "" || strings.Contains(viewText(v), canary) {
			t.Fatal("failure lost its safe reason or leaked private diagnostic text", test.code)
		}
	}
}

func TestErrorQueueKeepsDistinctReasonsAndInformationSeparate(t *testing.T) {
	a, _, _ := fixture(t)
	a.reportError("Recovery", ipc.NewRequestError("ACCOUNT_RECOVERY_STORAGE"))
	id := a.view().ErrorNotice.ID
	a.reportError("Recovery", ipc.NewRequestError("ACCOUNT_RECOVERY_STORAGE"))
	if len(a.errorQueue) != 1 || a.view().ErrorNotice.ID != id {
		t.Fatal("duplicate error lost its identity or produced another dialog")
	}
	a.reportError("Refresh history", ipc.NewRequestError("PROVIDER_RATE_LIMITED"))
	a.notice("Wallet state refreshed.")
	if a.view().Status != "Wallet state refreshed." || a.view().ErrorNotice.ID != id {
		t.Fatal("informational feedback replaced or exposed an error in the status bar")
	}
	v := a.view()
	v.ErrorNotice.Message = "mutated caller copy"
	if a.view().ErrorNotice.Message == v.ErrorNotice.Message {
		t.Fatal("view caller mutated a retained error")
	}
	a.event(webui.Event{ID: "ackError", Value: id})
	if a.view().ErrorNotice == nil || a.view().ErrorNotice.Code != "PROVIDER_RATE_LIMITED" {
		t.Fatal("acknowledging one error lost the following reason")
	}
	a.reportError("Cancelled recovery", ipc.NewRequestError("REQUEST_CANCELLED"))
	if len(a.errorQueue) != 1 || a.view().Status != "Request cancelled." {
		t.Fatal("user cancellation became a failure dialog")
	}
	for i := 0; i < 80; i++ {
		a.errorText(fmt.Sprintf("Operation %d", i), "Reviewed operation failure.")
	}
	if len(a.errorQueue) != 32 || !strings.Contains(a.errorQueue[31].Message, "Additional failures") {
		t.Fatal("error queue became unbounded or silently discarded overflow")
	}
	a.lock()
	if a.view().ErrorNotice != nil || a.view().Status != "Wallet locked." {
		t.Fatal("lock retained previous-session diagnostics")
	}
}

func TestCustomFailureFeedbackAndApplyFailureDoNotProduceGenericOrSuccessNotices(t *testing.T) {
	a, ui, _ := fixture(t)
	failure := ipc.NewRequestError("ACCOUNT_RECOVERY_STORAGE")
	a.reportError("Custom recovery", failure)
	a.workWithFeedback("Generic operation", func(uint64) (func(), error) { return nil, failure }, func(error) {
		a.reportError("Custom recovery", failure)
	})
	ui.apply(t)
	if len(a.errorQueue) != 1 || a.errorQueue[0].Operation != "Custom recovery" || a.view().Status != "" {
		t.Fatal("deduplicated custom feedback also queued the generic fallback")
	}
	a.event(webui.Event{ID: "ackError", Value: a.errorQueue[0].ID})
	a.work("Validate returned state", func(uint64) (func(), error) {
		return func() { a.reportError("Validate returned state", ipc.NewRequestError("CHAIN_DATA_INVALID")) }, nil
	})
	ui.apply(t)
	if a.view().ErrorNotice == nil || a.view().ErrorNotice.Code != "CHAIN_DATA_INVALID" || a.view().Status != "" {
		t.Fatal("apply failure was followed by misleading successful status")
	}
}
