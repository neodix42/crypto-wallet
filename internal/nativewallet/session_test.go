package nativewallet

// These tests exercise controller lifecycle ordering without connecting to any
// real user/system bus, locking the desktop or creating a GTK display.
import (
	"encoding/json"
	"errors"
	"localwallet/internal/uiprompt"
	"strings"
	"testing"
	"time"
)

type lifecycleFrontend struct {
	*testFrontend
	controls chan func()
}

func (f *lifecycleFrontend) DispatchControl(fn func()) { f.controls <- fn }

func (f *lifecycleFrontend) control(t *testing.T) {
	t.Helper()
	select {
	case fn := <-f.controls:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("priority lifecycle callback did not become ready")
	}
}

func TestSessionMonitorStartupFailureRemainsVisibleAfterFeedbackExpiryAndUnlock(t *testing.T) {
	a, ui, _ := fixture(t)
	a.state.Locked = true
	a.active = "settings"
	// A nil context is rejected synchronously by the monitor package before
	// making any D-Bus connection. Only its controller completion is queued.
	a.startSessionMonitor(nil)
	ui.apply(t)
	if a.monitorState != unavailableMonitoring || a.status != unavailableMonitoring || !a.state.Locked {
		t.Fatal("monitor startup failure was hidden or claimed active protection")
	}
	a.statusUntil = time.Now().Add(-time.Second)
	a.lastStatus = time.Now()
	a.tick()
	if a.status != "" || !strings.Contains(viewText(a.view()), unavailableMonitoring) {
		t.Fatal("seven-second feedback expiry removed persistent monitoring guidance")
	}
	a.state.Locked = false // Presentation check; no signer authorization is inferred.
	a.notice("Public unrelated operation completed.")
	if !strings.Contains(viewText(a.view()), unavailableMonitoring) ||
		strings.Contains(a.monitorState, "is active") {
		t.Fatal("unlocked Settings lost unsupported-monitoring guidance")
	}
}

func TestClosedControllerRejectsDelayedMonitorStartupCallback(t *testing.T) {
	a, ui, core := fixture(t)
	a.startSessionMonitor(nil)
	a.close()
	beforeStatus, beforeViews := a.status, len(ui.views)
	ui.apply(t)
	if !a.closed || core.killed.Load() != 1 || ui.closes.Load() != 1 || len(ui.views) != beforeViews || a.status != beforeStatus {
		t.Fatal("a delayed monitor callback modified or reopened a closed wallet")
	}
}

func TestPriorityBackendFailureRevokesQueuedPromptAndAllowsNextValidRequest(t *testing.T) {
	a, publicUI, core := fixture(t)
	ui := &lifecycleFrontend{testFrontend: publicUI, controls: make(chan func(), 8)}
	a.ui = ui
	const oldID = "0123456789abcdef0123456789abcdef"
	const nextID = "1123456789abcdef0123456789abcdef"
	old := json.RawMessage(`{"id":"` + oldID + `","kind":"input","title":"Old private prompt","labels":["Synthetic password"]}`)
	if err := a.privatePrompt(old); err != nil {
		t.Fatal(err)
	}
	a.work("Public failed operation", func(uint64) (func(), error) { return nil, errors.New("PUBLIC-PRIVATE-ERROR-CANARY") })
	ui.control(t) // Error cancellation takes priority over the queued modal.
	ui.prompt = func(prompt uiprompt.Prompt) (uiprompt.Reply, error) {
		if prompt.ID == oldID {
			t.Error("backend failure reopened a queued stale credential prompt")
		}
		return uiprompt.Reply{PromptID: prompt.ID, Accepted: true}, nil
	}
	ui.apply(t)
	if ui.cancels.Load() != 1 || core.killed.Load() != 0 || a.busy || strings.Contains(a.status, "CANARY") {
		t.Fatal("ordinary backend error did not safely revoke the private modal")
	}
	select {
	case <-core.replies:
		t.Fatal("revoked queued prompt replied into the surviving coordinator")
	case <-time.After(20 * time.Millisecond):
	}
	completed := false
	a.work("New explicit request", func(uint64) (func(), error) {
		next := json.RawMessage(`{"id":"` + nextID + `","kind":"confirm","title":"New private approval","summary":"Public fixture"}`)
		if err := a.privatePrompt(next); err != nil {
			return nil, err
		}
		select {
		case raw := <-core.replies:
			var reply uiprompt.Reply
			if json.Unmarshal(raw, &reply) != nil || reply.PromptID != nextID || !reply.Accepted {
				return nil, errors.New("new private reply identity changed")
			}
		case <-time.After(3 * time.Second):
			return nil, errors.New("new private reply unavailable")
		}
		return func() { completed = true }, nil
	})
	ui.apply(t) // Fresh modal remains available after error revocation.
	ui.apply(t) // Explicit operation completion.
	if !completed || a.busy {
		t.Fatal("modal revocation permanently disabled new authorized requests")
	}
}

func TestPriorityBackendFailureDiscardsLateAcceptedReplyWithLiveCoordinator(t *testing.T) {
	a, publicUI, core := fixture(t)
	ui := &lifecycleFrontend{testFrontend: publicUI, controls: make(chan func(), 8)}
	a.ui = ui
	ui.prompt = func(prompt uiprompt.Prompt) (uiprompt.Reply, error) {
		ui.control(t) // Models failure notification while a private loop is open.
		return uiprompt.Reply{PromptID: prompt.ID, Accepted: true, Values: []string{"PUBLIC-LATE-INPUT"}}, nil
	}
	raw := json.RawMessage(`{"id":"0123456789abcdef0123456789abcdef","kind":"input","title":"Public synthetic password","labels":["Password"]}`)
	if err := a.privatePrompt(raw); err != nil {
		t.Fatal(err)
	}
	a.work("Failed private operation", func(uint64) (func(), error) { return nil, errors.New("PUBLIC-PRIVATE-FAILURE") })
	ui.apply(t)
	if core.Generation() != 1 || ui.cancels.Load() != 1 || a.busy {
		t.Fatal("fixture did not preserve the coordinator during modal failure")
	}
	select {
	case <-core.replies:
		t.Fatal("accepted response escaped after its private display was revoked")
	case <-time.After(20 * time.Millisecond):
	}
}
