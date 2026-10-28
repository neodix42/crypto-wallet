package walletclipboard

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRejectsUnavailableCapabilities(t *testing.T) {
	commit := func(f func()) bool { f(); return true }
	dispatch := func(f func()) { f() }
	write := func() bool { return true }
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		ctx       context.Context
		authorize func(func()) bool
		dispatch  func(func())
		write     func() bool
	}{
		{nil, commit, dispatch, write}, {context.Background(), nil, dispatch, write}, {context.Background(), commit, nil, write}, {context.Background(), commit, dispatch, nil}, {canceled, commit, dispatch, write},
	} {
		if Execute(test.ctx, test.authorize, test.dispatch, test.write) == nil {
			t.Fatal("unavailable capability accepted")
		}
	}
}
func TestQueuedCopyWaitsForNativeCommitUnderOwnerLock(t *testing.T) {
	queued := make(chan func(), 1)
	result := make(chan error, 1)
	var owner sync.Mutex
	writes := 0
	go func() {
		result <- Execute(context.Background(), func(commit func()) bool { owner.Lock(); defer owner.Unlock(); commit(); return true }, func(f func()) { queued <- f }, func() bool {
			if owner.TryLock() {
				owner.Unlock()
				t.Error("clipboard commit escaped lifecycle lock")
			}
			writes++
			return true
		})
	}()
	callback := <-queued
	select {
	case <-result:
		t.Fatal("queue acknowledgment incorrectly reported copied")
	default:
	}
	callback()
	if err := <-result; err != nil || writes != 1 {
		t.Fatal("native clipboard commit not acknowledged")
	}
}
func TestCanceledQueuedCopyCanNeverCommitLater(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queued := make(chan func(), 1)
	result := make(chan error, 1)
	writes := 0
	go func() {
		result <- Execute(ctx, func(commit func()) bool { commit(); return true }, func(f func()) { queued <- f }, func() bool { writes++; return true })
	}()
	callback := <-queued
	cancel()
	if <-result == nil {
		t.Fatal("canceled copy passed")
	}
	callback()
	if writes != 0 {
		t.Fatal("clipboard changed after canceled operation returned")
	}
}
func TestCancelWaitsForAlreadyRunningCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- Execute(ctx, func(commit func()) bool { commit(); return true }, func(f func()) { go f() }, func() bool { close(entered); <-release; return true })
	}()
	<-entered
	cancel()
	select {
	case <-result:
		t.Fatal("cancel returned while native clipboard write was still running")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	<-result
}
func TestStaleOwnerFailedWriteAndRetainedCommitAreDenied(t *testing.T) {
	writes := 0
	write := func() bool { writes++; return false }
	dispatch := func(f func()) { f() }
	if Execute(context.Background(), func(func()) bool { return false }, dispatch, write) == nil || writes != 0 {
		t.Fatal("stale owner wrote clipboard")
	}
	if Execute(context.Background(), func(commit func()) bool { commit(); commit(); return true }, dispatch, write) == nil || writes != 1 {
		t.Fatal("failed native write was retried or passed")
	}
	var retained func()
	if Execute(context.Background(), func(commit func()) bool { retained = commit; return true }, dispatch, write) == nil {
		t.Fatal("missing native commit passed")
	}
	retained()
	if writes != 1 {
		t.Fatal("retained callback wrote after operation completed")
	}
}
func TestAuthorizerPanicRevokesRetainedCommitWithoutLeakingPanic(t *testing.T) {
	writes := 0
	var retained func()
	if Execute(context.Background(), func(commit func()) bool { retained = commit; panic("synthetic private canary") }, func(f func()) { f() }, func() bool { writes++; return true }) == nil {
		t.Fatal("panicking authorizer passed")
	}
	retained()
	if writes != 0 {
		t.Fatal("panic left a future clipboard commit authorized")
	}
}
