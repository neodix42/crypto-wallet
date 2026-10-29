// Package walletclipboard acknowledges a dispatched native clipboard write.
package walletclipboard

import (
	"context"
	"errors"
	"sync"
)

var ErrUnavailable = errors.New("Wallet clipboard unavailable")

// Execute dispatches a single synchronous commit to the native event loop.
// authorize must synchronously validate owner state and invoke commit while
// holding the same mutex used to revoke that state. Neither a queue acknowledgment
// nor a stale/canceled callback is reported as a successful clipboard write.
func Execute(ctx context.Context, authorize func(func()) bool, dispatch func(func()), write func() bool) error {
	if ctx == nil || authorize == nil || dispatch == nil || write == nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	result := make(chan error, 1)
	var gate sync.Mutex
	revoked, active, written, attempted := false, true, false, false
	dispatch(func() {
		outcome := ErrUnavailable
		defer func() {
			// Even a failed/misbehaving authorizer cannot retain a future commit.
			gate.Lock()
			active = false
			gate.Unlock()
			if recover() != nil {
				outcome = ErrUnavailable
			}
			result <- outcome
		}()
		if ctx.Err() != nil {
			return
		}
		accepted := authorize(func() {
			gate.Lock()
			defer gate.Unlock()
			if revoked || !active || attempted || ctx.Err() != nil {
				return
			}
			attempted = true
			written = write()
		})
		gate.Lock()
		active = false
		committed := written
		gate.Unlock()
		if accepted && committed {
			outcome = nil
		}
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		// Serialize revocation with an already-running native write. Therefore this
		// method cannot return cancellation and then commit from its queued closure.
		gate.Lock()
		revoked = true
		active = false
		gate.Unlock()
		return ErrUnavailable
	}
}
