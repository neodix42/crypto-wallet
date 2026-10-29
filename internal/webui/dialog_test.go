package webui

import (
	"context"
	"encoding/json"
	"errors"
	"localwallet/internal/uiprompt"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitDialog(t *testing.T, b *DialogBridge) *DialogView {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p := b.Snapshot(); p != nil {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("current in-window dialog did not become available")
	return nil
}
func startDialog(t *testing.T, w *Window, p uiprompt.Prompt) (*DialogBridge, <-chan promptResult) {
	t.Helper()
	result := make(chan promptResult, 1)
	go func() { reply, err := w.Prompt(p); result <- promptResult{reply, err} }()
	bridge := &DialogBridge{window: w}
	waitDialog(t, bridge)
	return bridge, result
}
func dialogResult(t *testing.T, result <-chan promptResult) promptResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(time.Second):
		t.Fatal("dialog did not finish after reply/cancellation")
	}
	return promptResult{}
}
func newDialogWindow(t *testing.T) *Window {
	t.Helper()
	w, _ := New(nil)
	t.Cleanup(func() { w.mu.Lock(); w.ctx = nil; w.mu.Unlock(); w.Close() })
	return w
}

func TestDialogSnapshotIsRepeatableAndReplyIsValidatedAgainstCurrentRequest(t *testing.T) {
	w := newDialogWindow(t)
	p := promptFixture()
	bridge, result := startDialog(t, w, p)
	for range 3 {
		snapshot := bridge.Snapshot()
		if snapshot == nil || snapshot.ID != p.ID || snapshot.Labels[0] != "Password" {
			t.Fatal("persistent dialog snapshot missing or aliased")
		}
		snapshot.Labels[0] = "changed renderer copy"
	}
	for _, invalid := range []uiprompt.Reply{
		{PromptID: strings.Repeat("b", 32), Accepted: true, Values: []string{"synthetic"}},
		{PromptID: p.ID, Accepted: true, Values: []string{strings.Repeat("x", 4097)}},
		{PromptID: p.ID, Accepted: false, Values: []string{"synthetic"}},
	} {
		if bridge.Reply(invalid) {
			t.Fatal("invalid or stale dialog reply accepted")
		}
	}
	if !bridge.Reply(uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"public-test-password"}}) {
		t.Fatal("current dialog input rejected")
	}
	got := dialogResult(t, result)
	if got.err != nil || !got.reply.Accepted || len(got.reply.Values) != 1 || got.reply.Values[0] != "public-test-password" {
		t.Fatal("dialog input did not reach its waiting Go caller")
	}
	if bridge.Snapshot() != nil || bridge.Reply(uiprompt.Reply{PromptID: p.ID}) || bridge.Copy(strings.Repeat("a", 32)) || bridge.Activity(strings.Repeat("a", 32)) {
		t.Fatal("dismissed dialog retained state or acted on a stale request")
	}
}

func TestConsecutiveDialogsDoNotReusePreviousReplyOrPresentation(t *testing.T) {
	w := newDialogWindow(t)
	for index, id := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		w.mu.Lock()
		w.view.Appearance = Appearance{"dark", 24}
		w.mu.Unlock()
		p := promptFixture()
		p.ID = id
		bridge, result := startDialog(t, w, p)
		snapshot := bridge.Snapshot()
		if snapshot.Appearance != (Appearance{"dark", 24}) {
			t.Fatal("dialog did not use the current app appearance")
		}
		if index > 0 && bridge.Reply(uiprompt.Reply{PromptID: strings.Repeat("a", 32), Accepted: true, Values: []string{"stale"}}) {
			t.Fatal("previous dialog reply completed a new request")
		}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields["appearance"] == nil || fields["id"] == nil || fields["prompt"] != nil {
			t.Fatal("dialog binding lost the agreed flat Prompt shape")
		}
		clear(raw)
		if !bridge.Reply(uiprompt.Reply{PromptID: id}) {
			t.Fatal("current cancellation rejected")
		}
		if got := dialogResult(t, result); got.err != nil || got.reply.Accepted || len(got.reply.Values) != 0 {
			t.Fatal("clean UI cancellation returned input or approval")
		}
	}
}

func TestModalActivityAndPriorityControlsRemainResponsive(t *testing.T) {
	for _, mode := range []string{"activity", "lock", "close"} {
		t.Run(mode, func(t *testing.T) {
			w := newDialogWindow(t)
			activity := make(chan struct{}, 1)
			w.onEvent = func(e Event) {
				if e.ID != "activity" || e.Value != "" {
					t.Error("dialog activity carried input")
				}
				activity <- struct{}{}
				w.CancelPrompt()
			}
			bridge, result := startDialog(t, w, promptFixture())
			if mode == "activity" {
				if !bridge.Activity(strings.Repeat("a", 32)) {
					t.Fatal("visible dialog could not report activity")
				}
			} else if mode == "lock" {
				w.DispatchControl(w.CancelPrompt)
			} else {
				w.DispatchControl(w.Close)
			}
			got := dialogResult(t, result)
			if !errors.Is(got.err, uiprompt.ErrCancelled) || got.reply.Accepted || len(got.reply.Values) != 0 {
				t.Fatal("lock/close/activity cancellation released approval or input")
			}
			if bridge.Snapshot() != nil {
				t.Fatal("cancelled dialog remained visible through its binding")
			}
			if mode == "activity" {
				select {
				case <-activity:
				default:
					t.Fatal("activity not delivered to lifecycle controller")
				}
			}
			w.mu.Lock()
			retired := !w.modal && w.activeCancel == nil && w.dialog == nil
			w.mu.Unlock()
			if !retired {
				t.Fatal("completed dialog retained modal lifecycle state")
			}
		})
	}
}

func TestQueuedLockWinsOverDialogApprovalAndOtherUIActionsStayQueued(t *testing.T) {
	w := newDialogWindow(t)
	// The dialog starts on the controller queue. Its own synchronous loop must
	// service lifecycle controls without running unrelated queued wallet actions.
	ranOrdinary := false
	w.Dispatch(func() { ranOrdinary = true })
	bridge, result := startDialog(t, w, promptFixture())
	w.mu.Lock()
	state := w.dialog
	state.result <- promptResult{reply: uiprompt.Reply{PromptID: state.prompt.ID, Accepted: true, Values: []string{"synthetic"}}}
	w.controls <- w.CancelPrompt
	w.mu.Unlock()
	got := dialogResult(t, result)
	if !errors.Is(got.err, uiprompt.ErrCancelled) || got.reply.Accepted || len(got.reply.Values) != 0 || ranOrdinary {
		t.Fatal("queued approval bypassed lock or ran unrelated modal actions")
	}
	if bridge.Snapshot() != nil {
		t.Fatal("lock retained current dialog")
	}
}

type normalClipboardRecorder struct {
	mu      sync.Mutex
	values  []string
	failure error
	entered chan struct{}
	release chan struct{}
}

func (c *normalClipboardRecorder) ClipboardSetText(value string) error {
	if c.entered != nil {
		close(c.entered)
		<-c.release
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failure != nil {
		return c.failure
	}
	c.values = append(c.values, value)
	return nil
}
func TestDialogCopyUsesNormalClipboardAndReportsFailureWithoutDismissing(t *testing.T) {
	for _, kind := range []string{"receive", "recovery", "recoveryBackup", "input"} {
		t.Run(kind, func(t *testing.T) {
			w := newDialogWindow(t)
			recorder := &normalClipboardRecorder{}
			w.ctx = context.WithValue(context.Background(), "frontend", recorder)
			p := promptFixture()
			expected := ""
			if kind == "receive" {
				p = receiveFixture(t, 1)
				expected = p.Address
			} else if kind == "recovery" || kind == "recoveryBackup" {
				p.Kind = kind
				p.Labels = nil
				p.Phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
				expected = p.Phrase
			}
			bridge, result := startDialog(t, w, p)
			if kind == "input" {
				if bridge.Copy(strings.Repeat("a", 32)) {
					t.Fatal("input dialog's Copy action exported a credential")
				}
			} else {
				recorder.failure = errors.New("public synthetic clipboard failure")
				if bridge.Copy(strings.Repeat("a", 32)) || bridge.Snapshot() == nil {
					t.Fatal("failed clipboard operation succeeded or dismissed the dialog")
				}
				recorder.failure = nil
				if !bridge.Copy(strings.Repeat("a", 32)) || !bridge.Copy(strings.Repeat("a", 32)) {
					t.Fatal("ordinary repeated clipboard operation failed")
				}
				if len(recorder.values) != 2 || recorder.values[0] != expected || recorder.values[1] != expected {
					t.Fatal("Copy did not use the exact current Go-authored value")
				}
			}
			if !bridge.Reply(uiprompt.Reply{PromptID: p.ID}) {
				t.Fatal("copy dialog could not finish")
			}
			dialogResult(t, result)
		})
	}
}

func TestCancellationRemainsResponsiveDuringAnAlreadyIssuedNativeCopy(t *testing.T) {
	w := newDialogWindow(t)
	recorder := &normalClipboardRecorder{entered: make(chan struct{}), release: make(chan struct{})}
	w.ctx = context.WithValue(context.Background(), "frontend", recorder)
	bridge, result := startDialog(t, w, receiveFixture(t, 1))
	copied := make(chan bool, 1)
	go func() { copied <- bridge.Copy(strings.Repeat("a", 32)) }()
	<-recorder.entered
	cancelled := make(chan struct{})
	go func() { w.CancelPrompt(); close(cancelled) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("normal clipboard request blocked dialog cancellation")
	}
	got := dialogResult(t, result)
	if !errors.Is(got.err, uiprompt.ErrCancelled) || bridge.Copy(strings.Repeat("a", 32)) || bridge.Snapshot() != nil {
		t.Fatal("cancelled dialog allowed another clipboard action")
	}
	// Ordinary OS clipboard requests already issued may finish after dismissal.
	// The detached UI result cannot complete another prompt or restore its state.
	close(recorder.release)
	if !<-copied {
		t.Fatal("already issued native clipboard request lost its result")
	}
	if bridge.Snapshot() != nil {
		t.Fatal("late clipboard result restored a dialog")
	}
}

func TestMissingNativeDesktopAndConcurrentDialogsFailWithoutApproval(t *testing.T) {
	w := newDialogWindow(t)
	for _, kind := range []string{"copy", "file"} {
		p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: kind, Title: "Public export"}
		if kind == "copy" {
			p.Address = "public-address"
		} else {
			save := false
			p.Save = &save
		}
		reply, err := w.Prompt(p)
		if !errors.Is(err, errDesktop) || reply.Accepted || reply.Path != "" {
			t.Fatal("missing desktop approved a native export")
		}
	}
	bridge, result := startDialog(t, w, promptFixture())
	if reply, err := w.Prompt(promptFixture()); !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted {
		t.Fatal("concurrent dialog displaced the current request")
	}
	w.CancelPrompt()
	dialogResult(t, result)
	if bridge.Snapshot() != nil {
		t.Fatal("cancelled dialog survived")
	}
}

func TestDialogRejectsInvalidReceiveImagesBeforePresentation(t *testing.T) {
	w := newDialogWindow(t)
	for _, p := range []uiprompt.Prompt{{}, receiveFixture(t, 2049), {ID: strings.Repeat("a", 32), Kind: "receive", Title: "Receive", Address: "public-address", PNG: []byte("not PNG")}} {
		if reply, err := w.Prompt(p); !errors.Is(err, uiprompt.ErrProtocol) || reply.Accepted {
			t.Fatal("invalid prompt reached the shared UI")
		}
		if (&DialogBridge{window: w}).Snapshot() != nil {
			t.Fatal("invalid prompt installed dialog state")
		}
	}
}

func TestDelayedPreviousDialogCopyAndActivityCannotAffectTheNextDialog(t *testing.T) {
	w := newDialogWindow(t)
	recorder := &normalClipboardRecorder{}
	w.ctx = context.WithValue(context.Background(), "frontend", recorder)
	receive := receiveFixture(t, 1)
	bridge, result := startDialog(t, w, receive)
	if !bridge.Reply(uiprompt.Reply{PromptID: receive.ID}) {
		t.Fatal("receive dialog did not close")
	}
	dialogResult(t, result)
	recovery := uiprompt.Prompt{ID: strings.Repeat("b", 32), Kind: "recovery", Title: "Record recovery phrase", Phrase: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"}
	bridge, result = startDialog(t, w, recovery)
	if bridge.Copy(receive.ID) || bridge.Activity(receive.ID) || len(recorder.values) != 0 {
		t.Fatal("delayed receive UI intent acted on the next recovery dialog")
	}
	if !bridge.Copy(recovery.ID) || len(recorder.values) != 1 || recorder.values[0] != recovery.Phrase {
		t.Fatal("current dialog copy did not use its own explicitly requested phrase")
	}
	if !bridge.Reply(uiprompt.Reply{PromptID: recovery.ID}) {
		t.Fatal("current recovery dialog could not close")
	}
	dialogResult(t, result)
}

type dialogEventRecorder struct {
	mu     sync.Mutex
	names  []string
	states []*DialogView
}

func (events *dialogEventRecorder) Emit(name string, data ...interface{}) {
	events.mu.Lock()
	defer events.mu.Unlock()
	events.names = append(events.names, name)
	if len(data) == 1 {
		if state, ok := data[0].(*DialogView); ok {
			events.states = append(events.states, state)
			return
		}
	}
	events.states = append(events.states, &DialogView{})
}
func TestStartupPublishesDialogCreatedAfterAnInitialEmptySnapshot(t *testing.T) {
	w := newDialogWindow(t)
	bridge := &DialogBridge{window: w}
	if bridge.Snapshot() != nil {
		t.Fatal("fresh application already has a dialog")
	}
	// Frontend has observed null; the backend requests input before OnStartup
	// attaches its event context. The later attachment must publish that request.
	p := promptFixture()
	bridge, result := startDialog(t, w, p)
	events := &dialogEventRecorder{}
	ctx := context.WithValue(context.Background(), "events", events)
	if w.attachContext(ctx) {
		t.Fatal("open application was marked closed at startup")
	}
	if len(events.states) != 1 || events.names[0] != "wallet:dialog" || events.states[0] == nil || events.states[0].ID != p.ID {
		t.Fatal("startup lost the pending dialog after the frontend's empty snapshot")
	}
	if !bridge.Reply(uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"public-startup-password"}}) {
		t.Fatal("published startup dialog could not reply")
	}
	got := dialogResult(t, result)
	if got.err != nil || !got.reply.Accepted || len(got.reply.Values) != 1 {
		t.Fatal("startup dialog did not complete normally")
	}
	if len(events.states) != 2 || events.states[1] != nil {
		t.Fatal("completed startup dialog was not dismissed")
	}
}
func TestStartupAfterEmptyCancelledOrClosedStateDoesNotRestoreAnOldDialog(t *testing.T) {
	for _, mode := range []string{"empty", "cancelled", "closed"} {
		t.Run(mode, func(t *testing.T) {
			w := newDialogWindow(t)
			if mode != "empty" {
				_, result := startDialog(t, w, promptFixture())
				if mode == "closed" {
					w.Close()
				} else {
					w.CancelPrompt()
				}
				if got := dialogResult(t, result); !errors.Is(got.err, uiprompt.ErrCancelled) {
					t.Fatal("pre-startup request was not cancelled")
				}
			}
			events := &dialogEventRecorder{}
			closed := w.attachContext(context.WithValue(context.Background(), "events", events))
			if closed != (mode == "closed") || len(events.states) != 1 || events.states[0] != nil || (&DialogBridge{window: w}).Snapshot() != nil {
				t.Fatal("late startup restored obsolete dialog state")
			}
		})
	}
}
