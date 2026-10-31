package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func historyChild(t *testing.T) *Child {
	t.Helper()
	child := &Child{SelfRole: "--internal-core", Environment: "testnet"}
	t.Cleanup(child.Kill)
	return child
}

func waitHistoryPeer(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("history peer did not start")
		case <-tick.C:
			if _, err := os.Stat(path); err == nil {
				return
			}
		}
	}
}

func historyResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("history cancellation blocked serialized child calls")
		return nil
	}
}

func TestHistoryContextPreemptsOriginalCallWithoutRestartOrCancellingNextRequest(t *testing.T) {
	child := historyChild(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "history-ready")
	result := make(chan error, 1)
	generation := child.Generation()
	go func() {
		result <- child.CallHistoryAt(ctx, generation, map[string]any{"Block": true, "Ready": ready}, nil)
	}()
	waitHistoryPeer(t, ready)
	cancel()
	if err := historyResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal("context cancellation not returned", err)
	}
	var next struct{ OK bool }
	if err := child.CallAt(generation, "snapshot", struct{}{}, &next); err != nil || !next.OK || child.Generation() != generation {
		t.Fatal("history preemption restarted/locked child or swallowed next response", next, err)
	}
	if child.CancelHistory() == nil {
		t.Fatal("completed history retained cancellation authority")
	}
}

func TestHistoryContextCancelledWhileWaitingForMutexStartsNoRequest(t *testing.T) {
	child := historyChild(t)
	ctx, cancel := context.WithCancel(context.Background())
	child.mu.Lock()
	result := make(chan error, 1)
	go func() { result <- child.CallHistoryAt(ctx, child.Generation(), struct{}{}, nil) }()
	cancel()
	child.mu.Unlock()
	if err := historyResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	child.life.Lock()
	started, counter := child.cmd != nil, child.counter
	child.life.Unlock()
	if started || counter != 0 {
		t.Fatal("preempted queued history started a process or wrote a request")
	}
}

func TestOldHistoryContextCannotCancelLaterHistoryOrCrossGeneration(t *testing.T) {
	child := historyChild(t)
	old, stopOld := context.WithCancel(context.Background())
	defer stopOld()
	if err := child.CallHistoryAt(old, child.Generation(), struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := filepath.Join(t.TempDir(), "later-history")
	result := make(chan error, 1)
	go func() {
		result <- child.CallHistoryAt(ctx, child.Generation(), map[string]any{"Block": true, "Ready": ready}, nil)
	}()
	waitHistoryPeer(t, ready)
	stopOld()
	select {
	case err := <-result:
		t.Fatal("old history context cancelled a later request", err)
	case <-time.After(20 * time.Millisecond):
	}
	child.life.Lock()
	generation, id, session := child.historyGeneration, child.historyID, child.historySession
	child.life.Unlock()
	child.Kill()
	if err := historyResult(t, result); err == nil {
		t.Fatal("killed history returned success")
	}
	if err := child.Call("snapshot", struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	if child.cancelHistoryAt(generation, id, session) == nil {
		t.Fatal("stale history identity crossed process generation")
	}
}

func TestHistoryManualControlIsSingleUseAndNeverTargetsSigning(t *testing.T) {
	child := historyChild(t)
	if child.CancelHistory() == nil {
		t.Fatal("no-request history cancellation accepted")
	}
	ready := filepath.Join(t.TempDir(), "manual-history")
	result := make(chan error, 1)
	go func() { result <- child.Call("chainHistory", map[string]any{"Block": true, "Ready": ready}, nil) }()
	waitHistoryPeer(t, ready)
	if err := child.CancelHistory(); err != nil {
		t.Fatal(err)
	}
	if child.CancelHistory() == nil {
		t.Fatal("history cancellation replay was written")
	}
	if err := historyResult(t, result); err == nil {
		t.Fatal("cancelled history returned success")
	}
	if err := child.Call("snapshot", struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	if child.CancelHistory() == nil {
		t.Fatal("non-history operation retained cancellation authority")
	}
	foreign := &Child{SelfRole: "--internal-signer", Environment: "testnet", historyID: "original", historySession: "public", historyReady: true}
	if foreign.CancelHistory() == nil || foreign.CallHistoryAt(context.Background(), 0, struct{}{}, nil) == nil {
		t.Fatal("signer role acquired history control")
	}
}

func TestPaymentQuoteContextPreemptsOnlyOriginalReadWithoutRestart(t *testing.T) {
	for _, method := range []string{"quotePayment", "max"} {
		t.Run(method, func(t *testing.T) {
			child := historyChild(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := filepath.Join(t.TempDir(), "quote-ready")
			result := make(chan error, 1)
			generation := child.Generation()
			go func() {
				body := map[string]any{"Block": true, "Ready": ready}
				if method == "quotePayment" {
					result <- child.CallPaymentQuoteAt(ctx, generation, body, nil)
				} else {
					result <- child.callContext(&generation, method, body, nil, ctx)
				}
			}()
			waitHistoryPeer(t, ready)
			child.life.Lock()
			id, session := child.historyID, child.historySession
			child.life.Unlock()
			cancel()
			if err := historyResult(t, result); !errors.Is(err, context.Canceled) {
				t.Fatal("quote context cancellation was lost", err)
			}
			var next struct{ OK bool }
			if err := child.CallAt(generation, "prepare", struct{}{}, &next); err != nil || !next.OK || child.Generation() != generation {
				t.Fatal("quote cancellation killed the child or affected later preparation", err)
			}
			if child.cancelHistoryAt(generation, id, session) == nil || child.CancelHistory() == nil {
				t.Fatal("finished quote retained cancellation authority")
			}
		})
	}
}

func TestPaymentQuotePrequeuedCancellationAndForbiddenMethodsStartNothing(t *testing.T) {
	child := historyChild(t)
	ctx, cancel := context.WithCancel(context.Background())
	child.mu.Lock()
	result := make(chan error, 1)
	go func() { result <- child.CallPaymentQuoteAt(ctx, child.Generation(), struct{}{}, nil) }()
	cancel()
	child.mu.Unlock()
	if err := historyResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal("prequeued quote cancellation was lost", err)
	}
	for _, method := range []string{"prepare", "send", "retry", "sign", "pendingPayment"} {
		if child.callContext(nil, method, struct{}{}, nil, context.Background()) == nil {
			t.Fatal("write or unrelated method acquired read-only cancellation", method)
		}
	}
	if child.CallPaymentQuoteAt(nil, child.Generation(), struct{}{}, nil) == nil {
		t.Fatal("quote without context accepted")
	}
	foreign := &Child{SelfRole: "--internal-signer", Environment: "testnet"}
	if foreign.CallPaymentQuoteAt(context.Background(), 0, struct{}{}, nil) == nil {
		t.Fatal("signer acquired payment quote cancellation")
	}
	child.life.Lock()
	defer child.life.Unlock()
	if child.cmd != nil || child.counter != 0 {
		t.Fatal("cancelled/forbidden quote started a child or sent a request")
	}
}

func TestLatePaymentQuoteCancellationCannotInterruptSubsequentSend(t *testing.T) {
	child := historyChild(t)
	old, stopOld := context.WithCancel(context.Background())
	defer stopOld()
	generation := child.Generation()
	if err := child.CallPaymentQuoteAt(old, generation, struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ready, release := filepath.Join(dir, "send-ready"), filepath.Join(dir, "send-release")
	result := make(chan error, 1)
	go func() {
		result <- child.CallAt(generation, "send", map[string]string{"Ready": ready, "Release": release}, nil)
	}()
	waitHistoryPeer(t, ready)
	stopOld()
	if child.CancelHistory() == nil {
		t.Fatal("pending send acquired stale quote cancellation authority")
	}
	select {
	case err := <-result:
		t.Fatal("late quote cancellation interrupted a subsequent send", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := os.WriteFile(release, []byte("public explicit fixture release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := historyResult(t, result); err != nil || child.Generation() != generation {
		t.Fatal("subsequent send lost its response or child lifecycle", err)
	}
}
