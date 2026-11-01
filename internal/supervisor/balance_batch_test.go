package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/model"
)

func TestBalanceBatchProgressIsBoundToSelectedAccountsAndFinalCompleteness(t *testing.T) {
	child := historyChild(t)
	var results []model.BalanceBatchResult
	if err := child.CallBalancesAt(context.Background(), child.Generation(), []string{"first", "second"}, func(result model.BalanceBatchResult) { results = append(results, result) }); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].AccountID != "first" || results[1].AccountID != "second" || results[0].Values[0].Atomic != "1000000000000000001" {
		t.Fatal("incremental observations were lost or precision changed", results)
	}
	if child.CancelHistory() == nil {
		t.Fatal("completed batch retained cancellation authority")
	}
}

func TestBalanceBatchRejectsUntrustedFramesWithoutOpeningPrompts(t *testing.T) {
	for _, mode := range []string{"foreign-account", "wrong-session", "wrong-id", "provider-secret", "private-field", "prompt", "missing", "duplicate", "wrong-count", "private-final"} {
		t.Run(mode, func(t *testing.T) {
			child := historyChild(t)
			var prompts atomic.Int32
			child.OnPrompt = func(json.RawMessage) error { prompts.Add(1); return nil }
			generation := child.Generation()
			if err := child.CallBalancesAt(context.Background(), generation, []string{mode}, func(model.BalanceBatchResult) {}); err == nil {
				t.Fatal("malformed streaming response accepted", mode)
			}
			if prompts.Load() != 0 || child.Generation() == generation {
				t.Fatal("malformed read opened approval or retained its transport", prompts.Load())
			}
		})
	}
}

func TestBalanceBatchCancellationPreservesSessionAndCannotInterruptFollowingWrite(t *testing.T) {
	child := historyChild(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	generation := child.Generation()
	go func() {
		done <- child.CallBalancesAt(ctx, generation, []string{"completed", "blocked"}, func(model.BalanceBatchResult) { ready <- struct{}{} })
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("completed balance was hidden by a later blocked provider")
	}
	cancel()
	if err := historyResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("batch cancellation failed", err)
	}
	var next struct{ OK bool }
	if err := child.CallAt(generation, "snapshot", struct{}{}, &next); err != nil || !next.OK || child.Generation() != generation {
		t.Fatal("read cancellation locked or restarted the signer", err)
	}
	dir := t.TempDir()
	sendReady, release := filepath.Join(dir, "write-ready"), filepath.Join(dir, "write-release")
	go func() {
		done <- child.CallAt(generation, "send", map[string]string{"Ready": sendReady, "Release": release}, nil)
	}()
	waitHistoryPeer(t, sendReady)
	if child.CancelHistory() == nil {
		t.Fatal("later write acquired completed balance cancellation")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatal("old read cancellation interrupted a write", err)
	case <-time.After(20 * time.Millisecond):
	}
	// This fixture has no financial authority; releasing it tests serialized
	// transport, not a production payment or broadcast.
	if err := os.WriteFile(release, []byte("public release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := historyResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestBalanceBatchInvalidArgumentsAndPrequeuedCancellationStartNothing(t *testing.T) {
	child := historyChild(t)
	callback := func(model.BalanceBatchResult) {}
	for _, ids := range [][]string{nil, {""}, {"same", "same"}, make([]string, model.BalanceBatchLimit+1)} {
		if child.CallBalancesAt(context.Background(), child.Generation(), ids, callback) == nil {
			t.Fatal("unbounded/ambiguous account batch accepted", ids)
		}
	}
	if child.CallBalancesAt(nil, 0, []string{"owned"}, callback) == nil || child.CallBalancesAt(context.Background(), 0, []string{"owned"}, nil) == nil {
		t.Fatal("missing context/callback accepted")
	}
	foreign := &Child{SelfRole: "--internal-signer"}
	if foreign.CallBalancesAt(context.Background(), 0, []string{"owned"}, callback) == nil {
		t.Fatal("signer role acquired public provider streaming")
	}
	ctx, cancel := context.WithCancel(context.Background())
	child.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- child.CallBalancesAt(ctx, child.Generation(), []string{"owned"}, callback) }()
	cancel()
	child.mu.Unlock()
	if err := historyResult(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	child.life.Lock()
	defer child.life.Unlock()
	if child.cmd != nil || child.counter != 0 {
		t.Fatal("preempted or invalid batch started a child")
	}
}
