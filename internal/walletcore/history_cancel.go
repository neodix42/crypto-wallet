package walletcore

import (
	"context"
	"errors"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/strictjson"
)

const historyControlLimit = 32

type historyControl struct {
	epoch           uint64
	cancel          context.CancelFunc
	cancelled, done bool
	finished        time.Time
}

func historyControlKey(message ipc.Message) string { return message.Session + "\x00" + message.ID }

// Register at the reader before queuing, so cancellation can race worker start.
// Direct Go callers also register at handleAt's entry without replacing a live
// record. Completed identities are retained briefly for the final-response race.
func (c *core) registerHistoryControl(message ipc.Message, epoch uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if message.Method != "chainHistory" || c.epoch != epoch {
		return errors.New("wallet history session changed")
	}
	key := historyControlKey(message)
	if existing := c.historyControls[key]; existing != nil && !existing.done && existing.epoch == epoch {
		return nil
	}
	for key, control := range c.historyControls {
		if control.done && time.Since(control.finished) > time.Minute {
			delete(c.historyControls, key)
		}
	}
	if len(c.historyControls) >= historyControlLimit {
		for key, control := range c.historyControls {
			if control.done {
				delete(c.historyControls, key)
				break
			}
		}
	}
	if len(c.historyControls) >= historyControlLimit {
		return errors.New("too many pending history requests")
	}
	if c.historyControls == nil {
		c.historyControls = map[string]*historyControl{}
	}
	c.historyControls[key] = &historyControl{epoch: epoch}
	return nil
}

func (c *core) finishHistoryControl(message ipc.Message, epoch uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	control := c.historyControls[historyControlKey(message)]
	if control == nil || control.epoch != epoch {
		return false
	}
	if control.cancel != nil {
		control.cancel()
		control.cancel = nil
	}
	control.done, control.finished = true, time.Now()
	return control.cancelled
}

func (c *core) cancelHistory(message ipc.Message) error {
	if message.Version != 1 || message.Method != "cancelHistory" || message.Error != "" || !objectBody(message.Body) || strictjson.Decode(message.Body, &struct{}{}) != nil {
		return errors.New("invalid wallet history cancellation")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	control := c.historyControls[historyControlKey(message)]
	if control == nil || control.epoch != c.epoch || control.cancelled || control.done && time.Since(control.finished) > time.Minute {
		return errors.New("no matching wallet history request")
	}
	control.cancelled = true
	if control.cancel != nil {
		control.cancel()
	}
	return nil
}

func (c *core) historyContextAt(duration time.Duration, epoch uint64, session string) (context.Context, context.CancelFunc) {
	ctx, cancel := c.networkContextAt(duration, epoch)
	c.mu.Lock()
	defer c.mu.Unlock()
	message := ipc.Message{ID: c.requestID, Session: session}
	if control := c.historyControls[historyControlKey(message)]; control != nil && control.epoch == epoch && !control.done && c.requestEpoch == epoch && c.requestSession == session {
		control.cancel = cancel
		if control.cancelled {
			cancel()
		}
	}
	return ctx, cancel
}
