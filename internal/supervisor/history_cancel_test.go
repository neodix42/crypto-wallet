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
