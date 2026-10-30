package walletcore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

type cancellableHistoryAdapter struct {
	*publicAdapter
	started chan context.Context
}

func (a *cancellableHistoryAdapter) HistoryPage(ctx context.Context, _ model.Account, _ string) (model.HistoryPage, error) {
	a.started <- ctx
	<-ctx.Done()
	return model.HistoryPage{}, ctx.Err()
}

func historyControlMessage(request ipc.Message) ipc.Message {
	return ipc.Message{Version: 1, ID: request.ID, Session: request.Session, Method: "cancelHistory", Body: json.RawMessage(`{}`)}
}

func TestHistoryCancellationStopsOnlyExactReadContextAndPreservesUnlockedSession(t *testing.T) {
	adapter := &cancellableHistoryAdapter{publicAdapter: &publicAdapter{}, started: make(chan context.Context, 1)}
	c, _ := fakeCoordinator(t, adapter.publicAdapter, nil)
	c.adapters = []model.Adapter{adapter}
	other, stop := c.networkContext(time.Minute)
	defer stop()
	request := message("chainHistory", map[string]string{"accountId": publicAccount().ID})
	result := make(chan error, 1)
	go func() { _, err := c.handle(request); result <- err }()
	var active context.Context
	select {
	case active = <-adapter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("history did not start")
	}
	generation, last := c.child.Generation(), c.last
	for _, change := range []func(*ipc.Message){
		func(m *ipc.Message) { m.ID = "other-request" }, func(m *ipc.Message) { m.Session = "other-session" }, func(m *ipc.Message) { m.Method = "sign" }, func(m *ipc.Message) { m.Version = 2 }, func(m *ipc.Message) { m.Error = "unexpected" }, func(m *ipc.Message) { m.Body = []byte(`{"accountId":"a"}`) }, func(m *ipc.Message) { m.Body = []byte(`null`) },
	} {
		bad := historyControlMessage(request)
		change(&bad)
		if c.cancelHistory(bad) == nil || active.Err() != nil {
			t.Fatal("unbound control cancelled history")
		}
	}
	if err := c.cancelHistory(historyControlMessage(request)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
			t.Fatal("original request did not return cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("history context was not cancelled")
	}
	if other.Err() != nil || !c.active || c.last != last || c.child.Generation() != generation {
		t.Fatal("history cancellation changed signer/session/activity or other network authority")
	}
	if c.cancelHistory(historyControlMessage(request)) == nil {
		t.Fatal("history control replay accepted")
	}
	if _, err := c.handle(message("accounts", struct{}{})); err != nil {
		t.Fatal("unlocked signer unavailable after cancellation", err)
	}
}

func TestHistoryCancellationQueuedBeforeWorkerContextAndLateAfterCompletion(t *testing.T) {
	for _, when := range []string{"queued", "completed"} {
		t.Run(when, func(t *testing.T) {
			c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
			request := message("chainHistory", map[string]string{"accountId": publicAccount().ID})
			if err := c.registerHistoryControl(request, c.epoch); err != nil {
				t.Fatal(err)
			}
			other, stop := c.networkContext(time.Minute)
			defer stop()
			if when == "completed" {
				c.finishHistoryControl(request, c.epoch)
			}
			if err := c.cancelHistory(historyControlMessage(request)); err != nil {
				t.Fatal("legitimate queue/final-response race rejected", err)
			}
			if when == "queued" {
				adapter := &cancellableHistoryAdapter{publicAdapter: &publicAdapter{}, started: make(chan context.Context, 1)}
				c.adapters = []model.Adapter{adapter}
				_, err := c.handle(request)
				if ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
					t.Fatal("pre-context cancellation was lost", err)
				}
				if ctx := <-adapter.started; ctx.Err() != context.Canceled {
					t.Fatal("cancelled request began live network work")
				}
			}
			if other.Err() != nil {
				t.Fatal("late/queued cancellation affected another context")
			}
			if c.cancelHistory(historyControlMessage(request)) == nil {
				t.Fatal("replayed race control accepted")
			}
		})
	}
}

func TestHistoryCancellationRecordsAreBoundedExpireAndCannotCrossEpoch(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	request := message("chainHistory", struct{}{})
	for i := 0; i < historyControlLimit; i++ {
		request.ID = fmt.Sprint(i)
		if err := c.registerHistoryControl(request, c.epoch); err != nil {
			t.Fatal(err)
		}
	}
	request.ID = "over-bound"
	if c.registerHistoryControl(request, c.epoch) == nil {
		t.Fatal("unbounded queued history controls")
	}
	first := request
	first.ID = "0"
	c.finishHistoryControl(first, c.epoch)
	if err := c.registerHistoryControl(request, c.epoch); err != nil || len(c.historyControls) > historyControlLimit {
		t.Fatal("completed tombstone did not permit bounded progress", err)
	}
	c.finishHistoryControl(request, c.epoch)
	c.historyControls[historyControlKey(request)].finished = time.Now().Add(-2 * time.Minute)
	if c.cancelHistory(historyControlMessage(request)) == nil {
		t.Fatal("expired history control accepted")
	}
	old := request
	old.ID = "1"
	c.lock()
	if len(c.historyControls) != 0 || c.cancelHistory(historyControlMessage(old)) == nil {
		t.Fatal("history cancellation authority survived lock/epoch change")
	}
}
