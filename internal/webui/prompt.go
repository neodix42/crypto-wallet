package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"localwallet/internal/strictjson"
	"localwallet/internal/uiprompt"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type promptResult struct {
	reply uiprompt.Reply
	err   error
}

// DialogView is the current dialog in the application's single trusted WebView.
// It is intentionally available through ordinary Wails bindings and events.
type DialogView struct {
	uiprompt.Prompt
	Appearance Appearance `json:"appearance"`
}

func (view DialogView) MarshalJSON() ([]byte, error) {
	// Prompt's kind-specific marshaler would otherwise be promoted and drop
	// the sibling appearance field from the ordinary dialog binding/event.
	raw, err := json.Marshal(view.Prompt)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, uiprompt.ErrProtocol
	}
	defer func() {
		for _, value := range fields {
			clear(value)
		}
	}()
	fields["appearance"], _ = json.Marshal(view.Appearance.Normalized())
	return json.Marshal(fields)
}

type dialogState struct {
	prompt       uiprompt.Prompt
	appearance   Appearance
	epoch        uint64
	result       chan promptResult
	activity     chan struct{}
	lastActivity time.Time
}

func checkedPrompt(p uiprompt.Prompt) (uiprompt.Prompt, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return uiprompt.Prompt{}, uiprompt.ErrProtocol
	}
	defer clear(raw)
	p, err = uiprompt.DecodePrompt(raw)
	if err == nil && p.Kind == "receive" {
		config, e := png.DecodeConfig(bytes.NewReader(p.PNG))
		if e != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
			return uiprompt.Prompt{}, uiprompt.ErrProtocol
		}
	}
	return p, err
}

func cloneDialog(state *dialogState) *DialogView {
	if state == nil {
		return nil
	}
	p := state.prompt
	p.Labels = append([]string(nil), p.Labels...)
	p.PNG = append([]byte(nil), p.PNG...)
	if p.Save != nil {
		save := *p.Save
		p.Save = &save
	}
	return &DialogView{Prompt: p, Appearance: state.appearance}
}

func clearPrompt(p *uiprompt.Prompt) {
	p.Phrase = ""
	p.Summary = ""
	p.Labels = nil
	clear(p.PNG)
	p.PNG = nil
}

// notifyDialogLocked serializes dialog events with state transitions. Callers
// hold the same window mutex, so a dismissed dialog cannot publish after the
// next dialog. Snapshots remain available if the frontend subscribes late.
func (w *Window) notifyDialogLocked() {
	if w.ctx != nil {
		// This is the ordinary Wails event emitter from its lifecycle context.
		// Only Emit is needed by this controller, alongside Snapshot for clients
		// whose subscription starts after the event was sent.
		if events, ok := w.ctx.Value("events").(interface{ Emit(string, ...interface{}) }); ok {
			events.Emit("wallet:dialog", cloneDialog(w.dialog))
		}
	}
}

func (w *Window) clearDialogLocked() {
	if w.dialog != nil {
		clearPrompt(&w.dialog.prompt)
		w.dialog = nil
	}
}

func (w *Window) Prompt(p uiprompt.Prompt) (uiprompt.Reply, error) {
	p, err := checkedPrompt(p)
	if err != nil {
		return uiprompt.Reply{}, err
	}
	w.mu.Lock()
	if w.closed || w.modal {
		w.mu.Unlock()
		clearPrompt(&p)
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
	epoch := w.cancelEpoch
	ctx, cancel := context.WithCancel(context.Background())
	w.modal = true
	w.activeCancel = cancel
	state := &dialogState{prompt: p, appearance: w.view.Appearance.Normalized(), epoch: epoch,
		result: make(chan promptResult, 1), activity: make(chan struct{}, 1)}
	native := p.Kind == "copy" || p.Kind == "file"
	uiCtx := w.ctx
	if !native {
		w.dialog = state
		w.notifyDialogLocked()
	}
	w.mu.Unlock()
	defer func() {
		cancel()
		w.mu.Lock()
		if w.dialog == state {
			w.clearDialogLocked()
			w.notifyDialogLocked()
		}
		w.modal = false
		w.activeCancel = nil
		w.mu.Unlock()
		clearPrompt(&p)
	}()
	if native {
		go func() {
			reply := uiprompt.Reply{PromptID: p.ID}
			var operationErr error
			if uiCtx == nil {
				operationErr = errDesktop
			} else if p.Kind == "copy" {
				w.mu.Lock()
				stale := w.closed || w.cancelEpoch != epoch || ctx.Err() != nil
				w.mu.Unlock()
				if stale {
					operationErr = uiprompt.ErrCancelled
				} else {
					operationErr = setClipboard(uiCtx, p.Address)
				}
				reply.Accepted = operationErr == nil
			} else {
				if *p.Save {
					reply.Path, operationErr = wruntime.SaveFileDialog(uiCtx, wruntime.SaveDialogOptions{Title: p.Title, DefaultFilename: "wallet-address.png"})
				} else {
					reply.Path, operationErr = wruntime.OpenFileDialog(uiCtx, wruntime.OpenDialogOptions{Title: p.Title})
				}
				reply.Accepted = operationErr == nil && reply.Path != ""
			}
			state.result <- promptResult{reply, operationErr}
		}()
	}
	for {
		select {
		case control := <-w.controls:
			control()
			continue
		default:
		}
		select {
		case control := <-w.controls:
			control()
		case <-state.activity:
			if w.onEvent != nil {
				w.onEvent(Event{ID: "activity"})
			}
		case <-ctx.Done():
			select {
			case result := <-state.result:
				clearReply(&result.reply)
			default:
			}
			return uiprompt.Reply{PromptID: p.ID}, uiprompt.ErrCancelled
		case result := <-state.result:
			// A queued lock/close wins over an already queued dialog reply.
		drainControls:
			for {
				select {
				case control := <-w.controls:
					control()
				default:
					break drainControls
				}
			}
			w.mu.Lock()
			stale := w.closed || w.cancelEpoch != epoch || ctx.Err() != nil
			w.mu.Unlock()
			if stale {
				clearReply(&result.reply)
				return uiprompt.Reply{PromptID: p.ID}, uiprompt.ErrCancelled
			}
			return result.reply, result.err
		}
	}
}

func clearReply(reply *uiprompt.Reply) {
	for index := range reply.Values {
		reply.Values[index] = ""
	}
	reply.Values = nil
	reply.Path = ""
}

// setClipboard uses the ordinary Wails frontend clipboard operation. The
// narrow Go interface avoids coupling the dialog controller to unrelated
// window operations; this is not a restricted browser transport.
func setClipboard(ctx context.Context, text string) error {
	if ctx == nil {
		return errDesktop
	}
	clipboard, ok := ctx.Value("frontend").(interface{ ClipboardSetText(string) error })
	if !ok {
		return errDesktop
	}
	return clipboard.ClipboardSetText(text)
}

// DialogBridge is persistent and bound beside PublicBridge in the main window.
// The request ID only correlates UI replies with the current dialog; there is
// no private subprocess, inherited pipe, one-use delivery or session bridge.
type DialogBridge struct{ window *Window }

func (b *DialogBridge) Snapshot() *DialogView {
	w := b.window
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	return cloneDialog(w.dialog)
}

func (b *DialogBridge) Reply(reply uiprompt.Reply) bool {
	w := b.window
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.dialog
	if w.closed || state == nil || state.epoch != w.cancelEpoch {
		return false
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return false
	}
	checked, err := decodeDialogReply(raw, state.prompt)
	clear(raw)
	if err != nil {
		return false
	}
	select {
	case state.result <- promptResult{reply: checked}:
		w.clearDialogLocked()
		w.notifyDialogLocked()
		return true
	default:
		clearReply(&checked)
		return false
	}
}

func (b *DialogBridge) Copy(promptId string) bool {
	w := b.window
	w.mu.Lock()
	state := w.dialog
	if w.closed || state == nil || state.epoch != w.cancelEpoch || state.prompt.ID != promptId {
		w.mu.Unlock()
		return false
	}
	var text string
	switch state.prompt.Kind {
	case "receive":
		text = state.prompt.Address
	case "recovery", "recoveryBackup":
		text = state.prompt.Phrase
	default:
		w.mu.Unlock()
		return false
	}
	ctx := w.ctx
	w.mu.Unlock()
	// This is an ordinary clipboard request. Do not hold the window mutex
	// while GTK schedules it; close/lock must remain responsive. A request
	// already issued to the OS may complete while its dialog is dismissed.
	return setClipboard(ctx, text) == nil
}

func (b *DialogBridge) Activity(promptId string) bool {
	w := b.window
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.dialog
	if w.closed || state == nil || state.epoch != w.cancelEpoch || state.prompt.ID != promptId || time.Since(state.lastActivity) < time.Second {
		return false
	}
	select {
	case state.activity <- struct{}{}:
		state.lastActivity = time.Now()
		return true
	default:
		return false
	}
}

func decodeDialogReply(raw []byte, p uiprompt.Prompt) (uiprompt.Reply, error) {
	var fields map[string]json.RawMessage
	if strictjson.Decode(raw, &fields) != nil {
		return uiprompt.Reply{}, uiprompt.ErrProtocol
	}
	return uiprompt.DecodeReply(raw, p)
}
