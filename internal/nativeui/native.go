// Package nativeui provides a native GTK desktop and private signer dialogs.
// Views and events contain public data only. Recovery phrases and credentials
// enter only Prompt and are never delivered to the public event callback.
package nativeui

/*
#cgo LDFLAGS: -l:libgtk-3.so.0 -l:libgobject-2.0.so.0 -l:libglib-2.0.so.0 -l:libgdk_pixbuf-2.0.so.0 -l:libgdk-3.so.0
#include <stdlib.h>
#include <string.h>
#include "native.h"
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"errors"
	"localwallet/internal/uiprompt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

type Option struct{ ID, Text string }
type Node struct {
	Kind, ID, Text, Value, Tooltip string
	Disabled                       bool
	Options                        []Option
	Children                       []Node
}
type Page struct {
	ID, Title string
	Nodes     []Node
}
type View struct {
	Title, Status, Active string
	StatusRevision        uint64
	Pages                 []Page
}
type Event struct{ ID, Value string }

var errDisplay = errors.New("native desktop unavailable")
var nextWindow atomic.Uint64
var windows sync.Map

// Window owns the GTK thread. Create it on the program's main goroutine, then
// call Run there. Render, Dispatch, Prompt, CancelPrompt and Close are safe to
// call from backend goroutines; their GTK work is marshalled to that thread.
type Window struct {
	id                 uint64
	thread             int
	widget             unsafe.Pointer
	book               unsafe.Pointer
	onEvent            func(Event)
	mu                 sync.Mutex
	queue              []func()
	controls           []func()
	cancelEpoch        uint64
	closed             bool
	rendering          bool
	modal              bool
	dialog             unsafe.Pointer
	lastStatus         string
	lastStatusRevision uint64
	statusUntil        time.Time
	lastInput          time.Time
	promptBusy         atomic.Bool
}

func New(onEvent func(Event)) (*Window, error) {
	runtime.LockOSThread()
	if C.wallet_ui_init() == 0 {
		runtime.UnlockOSThread()
		return nil, errDisplay
	}
	w := &Window{id: nextWindow.Add(1), thread: syscall.Gettid(), onEvent: onEvent}
	windows.Store(w.id, w)
	w.widget = C.wallet_ui_window(C.uintptr_t(w.id))
	if w.widget == nil {
		windows.Delete(w.id)
		runtime.UnlockOSThread()
		return nil, errDisplay
	}
	w.book = C.wallet_ui_book(w.widget)
	return w, nil
}

func (w *Window) Run() {
	if syscall.Gettid() != w.thread {
		return
	}
	C.wallet_ui_show(w.widget)
	C.wallet_ui_run()
	windows.Delete(w.id)
	runtime.UnlockOSThread()
}

func (w *Window) Dispatch(f func()) {
	if f == nil {
		return
	}
	w.mu.Lock()
	if !w.closed {
		w.queue = append(w.queue, f)
	}
	w.mu.Unlock()
}

func (w *Window) control(f func()) {
	w.mu.Lock()
	if !w.closed {
		w.controls = append(w.controls, f)
	}
	w.mu.Unlock()
}

// DispatchControl is reserved for value-free lifecycle checks (idle lock,
// cancellation and shutdown). It runs inside a private modal loop. Public
// backend results must use Dispatch, which stays deferred until the dialog ends.
func (w *Window) DispatchControl(f func()) {
	if f != nil {
		w.control(f)
	}
}

func (w *Window) Close() {
	w.control(func() {
		w.cancelOnThread()
		C.wallet_ui_close(w.widget)
	})
}

// CancelPrompt is a priority operation: GTK services it even while a private
// modal dialog is running. It invalidates queued prompts as well as the active
// dialog, preventing late backend work from displaying a stale phrase.
func (w *Window) CancelPrompt() {
	w.mu.Lock()
	w.cancelEpoch++
	w.mu.Unlock()
	w.control(w.cancelOnThread)
}

func (w *Window) cancelOnThread() {
	if w.dialog != nil {
		C.wallet_ui_cancel(w.dialog)
	}
}

func (w *Window) Render(v View) {
	// Copy the mutable tree before handing it to the GTK thread.
	v = cloneView(v)
	if syscall.Gettid() == w.thread && !w.modal {
		w.render(v)
		return
	}
	w.Dispatch(func() { w.render(v) })
}

func cloneView(v View) View {
	pages := make([]Page, len(v.Pages))
	copy(pages, v.Pages)
	for i := range pages {
		pages[i].Nodes = cloneNodes(pages[i].Nodes)
	}
	v.Pages = pages
	return v
}
func cloneNodes(nodes []Node) []Node {
	out := make([]Node, len(nodes))
	copy(out, nodes)
	for i := range out {
		out[i].Options = append([]Option(nil), out[i].Options...)
		out[i].Children = cloneNodes(out[i].Children)
	}
	return out
}

func plain(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// validateView bounds GTK allocation and prevents accidentally introducing a
// password/mnemonic entry into the public event channel. Secrets belong only
// to the separately typed Prompt API, never a public declarative form.
func validateView(v View) bool {
	if !plain(v.Title, 256) || !plain(v.Status, 65536) || !plain(v.Active, 256) || len(v.Pages) > 1000 {
		return false
	}
	remaining := 20000
	var nodes func([]Node, int) bool
	nodes = func(list []Node, depth int) bool {
		if depth > 32 {
			return false
		}
		for _, n := range list {
			remaining--
			if remaining < 0 || !plain(n.ID, 256) || !plain(n.Text, 65536) || !plain(n.Tooltip, 65536) || !plain(n.Value, 4096) || len(n.Options) > 10000 {
				return false
			}
			switch n.Kind {
			case "label", "heading", "separator", "address", "button", "combo", "check", "row", "column":
			case "entry":
				id := strings.ToLower(n.ID)
				for _, secret := range []string{"password", "phrase", "mnemonic", "secret", "apikey", "api_key", "credential", "passphrase"} {
					if strings.Contains(id, secret) {
						return false
					}
				}
			default:
				return false
			}
			for _, option := range n.Options {
				if !plain(option.ID, 4096) || !plain(option.Text, 4096) {
					return false
				}
			}
			if !nodes(n.Children, depth+1) {
				return false
			}
		}
		return true
	}
	ids := map[string]bool{}
	for _, page := range v.Pages {
		if page.ID == "" || ids[page.ID] || !plain(page.ID, 256) || !plain(page.Title, 256) || !nodes(page.Nodes, 0) {
			return false
		}
		ids[page.ID] = true
	}
	return true
}

func ctext(s string, f func(*C.char)) {
	p := C.CString(s)
	defer C.free(unsafe.Pointer(p))
	f(p)
}

func (w *Window) render(v View) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	if !validateView(v) {
		ctext("Desktop configuration unavailable.", func(s *C.char) { C.wallet_ui_status(w.widget, s) })
		return
	}
	w.rendering = true
	defer func() { w.rendering = false }()
	// Background balance/status updates rebuild only public widgets. Preserve
	// the user's current public edit, cursor and selection on the same page.
	var position, start, end, entry C.int
	focus := C.wallet_ui_focus(w.widget, &position, &start, &end, &entry)
	if focus != nil {
		defer C.free(unsafe.Pointer(focus))
	}
	page := ""
	if active := C.wallet_ui_active(w.book); active != nil {
		page = C.GoString(active)
	}
	ctext(v.Title, func(s *C.char) { C.wallet_ui_title(w.widget, s) })
	C.wallet_ui_clear(w.book)
	for _, page := range v.Pages {
		var box unsafe.Pointer
		ctext(page.ID, func(id *C.char) {
			ctext(page.Title, func(title *C.char) { box = C.wallet_ui_page(w.book, C.uintptr_t(w.id), id, title) })
		})
		w.nodes(box, page.Nodes)
	}
	// GTK refuses to select a hidden notebook page. Show the new public tree
	// before restoring the requested selection; changed events stay suppressed.
	C.wallet_ui_show(w.widget)
	ctext(v.Active, func(s *C.char) { C.wallet_ui_activate(w.book, s) })
	if v.Status != w.lastStatus || v.StatusRevision != w.lastStatusRevision {
		w.lastStatus = v.Status
		w.lastStatusRevision = v.StatusRevision
		w.statusUntil = time.Now().Add(7 * time.Second)
	}
	status := v.Status
	if !time.Now().Before(w.statusUntil) {
		status = ""
	}
	ctext(status, func(s *C.char) { C.wallet_ui_status(w.widget, s) })
	if focus != nil && page == v.Active {
		ctext(page, func(p *C.char) { C.wallet_ui_restore_focus(w.book, p, focus, position, start, end, entry) })
	}
}

func (w *Window) nodes(parent unsafe.Pointer, list []Node) {
	for _, n := range list {
		var widget unsafe.Pointer
		ctext(n.Text, func(text *C.char) {
			ctext(n.ID, func(id *C.char) {
				ctext(n.Value, func(value *C.char) {
					switch n.Kind {
					case "row", "column":
						if n.Kind == "row" && n.ID != "" {
							widget = C.wallet_ui_row(parent, C.uintptr_t(w.id), id)
						} else {
							widget = C.wallet_ui_box(parent, boolInt(n.Kind == "row"))
						}
						w.nodes(widget, n.Children)
					case "label", "heading":
						widget = C.wallet_ui_label(parent, text, boolInt(n.Kind == "heading"))
					case "separator":
						C.wallet_ui_separator(parent)
					case "address":
						if n.Value != "" {
							if n.Text != "" {
								C.wallet_ui_label(parent, text, 0)
							}
							widget = C.wallet_ui_address(parent, value)
						} else {
							widget = C.wallet_ui_address(parent, text)
						}
					case "button":
						widget = C.wallet_ui_button(parent, C.uintptr_t(w.id), id, text)
					case "entry":
						if n.Text != "" {
							C.wallet_ui_label(parent, text, 0)
						}
						widget = C.wallet_ui_entry(parent, C.uintptr_t(w.id), id, value)
					case "combo":
						if n.Text != "" {
							C.wallet_ui_label(parent, text, 0)
						}
						widget = C.wallet_ui_combo(parent, C.uintptr_t(w.id), id)
						for _, option := range n.Options {
							ctext(option.ID, func(oid *C.char) { ctext(option.Text, func(t *C.char) { C.wallet_ui_combo_option(widget, oid, t) }) })
						}
						C.wallet_ui_combo_value(widget, value)
					case "check":
						widget = C.wallet_ui_check(parent, C.uintptr_t(w.id), id, text, boolInt(n.Value == "true"))
					}
				})
			})
		})
		if widget != nil {
			C.wallet_ui_sensitive(widget, boolInt(!n.Disabled))
			if n.Tooltip != "" {
				ctext(n.Tooltip, func(s *C.char) { C.wallet_ui_tooltip(widget, s) })
			}
		}
	}
}

func boolInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

type promptResult struct {
	reply uiprompt.Reply
	err   error
}

func (w *Window) Prompt(p uiprompt.Prompt) (uiprompt.Reply, error) {
	if !w.promptBusy.CompareAndSwap(false, true) {
		return uiprompt.Reply{}, uiprompt.ErrProtocol
	}
	defer w.promptBusy.Store(false)
	raw, err := json.Marshal(p)
	if err != nil {
		return uiprompt.Reply{}, uiprompt.ErrProtocol
	}
	defer clear(raw)
	p, err = uiprompt.DecodePrompt(raw)
	if err != nil {
		return uiprompt.Reply{}, err
	}
	w.mu.Lock()
	epoch, closed := w.cancelEpoch, w.closed
	w.mu.Unlock()
	if closed {
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
	run := func() promptResult {
		w.mu.Lock()
		cancelled := w.cancelEpoch != epoch || w.closed
		w.mu.Unlock()
		if cancelled {
			return promptResult{err: uiprompt.ErrCancelled}
		}
		reply, err := w.prompt(p, epoch)
		w.mu.Lock()
		cancelled = w.cancelEpoch != epoch || w.closed
		w.mu.Unlock()
		if cancelled {
			for i := range reply.Values {
				reply.Values[i] = ""
			}
			return promptResult{err: uiprompt.ErrCancelled}
		}
		if err == nil {
			encoded, encodeErr := json.Marshal(reply)
			if encodeErr != nil {
				return promptResult{err: uiprompt.ErrProtocol}
			}
			reply, err = uiprompt.DecodeReply(encoded, p)
			clear(encoded)
		}
		p.Phrase, p.Summary = "", ""
		p.Labels = nil
		return promptResult{reply, err}
	}
	if syscall.Gettid() == w.thread {
		r := run()
		return r.reply, r.err
	}
	result := make(chan promptResult, 1)
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
	w.queue = append(w.queue, func() { result <- run() })
	w.mu.Unlock()
	r := <-result
	return r.reply, r.err
}

func (w *Window) prompt(p uiprompt.Prompt, epoch uint64) (reply uiprompt.Reply, err error) {
	reply.PromptID = p.ID
	w.modal = true
	defer func() { w.modal = false; w.dialog = nil }()
	if p.Kind == "copy" {
		w.mu.Lock()
		if w.closed || w.cancelEpoch != epoch {
			w.mu.Unlock()
			return reply, uiprompt.ErrCancelled
		}
		copyClipboard(p.Address)
		w.mu.Unlock()
		reply.Accepted = true
		return reply, nil
	}
	var dialog unsafe.Pointer
	ctext(p.Title, func(title *C.char) {
		if p.Kind == "file" {
			dialog = C.wallet_ui_file(w.widget, title, boolInt(*p.Save))
		} else {
			kind := 0
			switch p.Kind {
			case "notice":
				kind = 1
			case "receive":
				kind = 2
			case "recovery":
				kind = 3
			case "recoveryBackup":
				kind = 4
			}
			dialog = C.wallet_ui_dialog(w.widget, title, C.int(kind))
		}
	})
	w.dialog = dialog
	defer C.wallet_ui_destroy(dialog)
	if p.Kind == "file" {
		if C.wallet_ui_response(dialog) == -3 {
			path := C.wallet_ui_filename(dialog)
			if path != nil {
				reply.Path = C.GoString(path)
				C.wallet_ui_free(unsafe.Pointer(path))
				reply.Accepted = true
			}
		}
		return reply, nil
	}
	body := C.wallet_ui_body(dialog)
	var entries []unsafe.Pointer
	var phraseLabel unsafe.Pointer
	if p.Kind == "input" {
		C.wallet_ui_input_height(dialog, C.int(len(p.Labels)))
		for _, label := range p.Labels {
			ctext(label, func(s *C.char) { entries = append(entries, C.wallet_ui_secret(body, s)) })
		}
		defer func() {
			for _, entry := range entries {
				C.wallet_ui_secret_clear(entry)
			}
		}()
	} else if p.Kind == "recovery" || p.Kind == "recoveryBackup" {
		guidance := "Write these words in order and keep them private and offline. Anyone with this phrase can spend your funds. The app password only encrypts this device. Keep any original optional BIP39 passphrase separately."
		if p.Kind == "recoveryBackup" {
			guidance += " Copy phrase puts the words on the system clipboard. Other applications and clipboard history may retain them; clear your clipboard after use."
		}
		ctext(guidance, func(s *C.char) { C.wallet_ui_label(body, s, 0) })
		numbered := numberPhrase([]byte(p.Phrase))
		defer clear(numbered)
		terminated := make([]byte, len(numbered)+1)
		copy(terminated, numbered)
		secret := C.CBytes(terminated)
		clear(terminated)
		defer C.wallet_ui_secret_free(secret, C.size_t(len(numbered)+1))
		phraseLabel = C.wallet_ui_phrase(body, (*C.char)(secret))
		defer C.wallet_ui_phrase_clear(phraseLabel)
	} else {
		ctext(p.Summary, func(s *C.char) { C.wallet_ui_label(body, s, 0) })
		if p.Kind == "receive" {
			ctext(p.Address, func(s *C.char) { C.wallet_ui_address(body, s) })
			if C.wallet_ui_png(body, (*C.uchar)(unsafe.Pointer(&p.PNG[0])), C.size_t(len(p.PNG))) == 0 {
				return reply, uiprompt.ErrProtocol
			}
		}
	}
	response := C.wallet_ui_response(dialog)
	for response == 1 && (p.Kind == "receive" || p.Kind == "recoveryBackup") {
		// Serialize the explicit click with cancellation. A queued lock must
		// invalidate copying before its GTK close control is dispatched.
		w.mu.Lock()
		if w.closed || w.cancelEpoch != epoch {
			w.mu.Unlock()
			response = -6
			break
		}
		if p.Kind == "receive" {
			copyClipboard(p.Address)
		} else {
			copyClipboard(p.Phrase)
		}
		w.mu.Unlock()
		response = C.wallet_ui_response(dialog)
	}
	reply.Accepted = response == -3 || p.Kind == "notice" && response == -7
	if reply.Accepted && p.Kind == "input" {
		for _, entry := range entries {
			text := C.wallet_ui_secret_text(entry)
			length := C.strlen(text)
			if length > 4096 {
				reply.Values = nil
				reply.Accepted = false
				return reply, uiprompt.ErrInputTooLong
			}
			value := C.GoBytes(unsafe.Pointer(text), C.int(length))
			if !utf8.Valid(value) {
				clear(value)
				return uiprompt.Reply{PromptID: p.ID}, uiprompt.ErrProtocol
			}
			reply.Values = append(reply.Values, string(value))
			clear(value)
		}
	}
	return reply, nil
}

func numberPhrase(phrase []byte) []byte {
	defer clear(phrase)
	words := bytes.Fields(phrase)
	out := make([]byte, 0, len(phrase)+len(words)*5)
	for i, word := range words {
		out = strconv.AppendInt(out, int64(i+1), 10)
		out = append(out, '.', ' ')
		out = append(out, word...)
		out = append(out, '\n')
	}
	return out
}

func copyClipboard(value string) {
	p := C.CString(value)
	defer C.wallet_ui_secret_free(unsafe.Pointer(p), C.size_t(len(value)+1))
	C.wallet_ui_clipboard(p, C.int(len(value)))
}

func lookup(id C.uintptr_t) *Window {
	value, ok := windows.Load(uint64(id))
	if !ok {
		return nil
	}
	return value.(*Window)
}

//export nativeUIEvent
func nativeUIEvent(owner C.uintptr_t, id, value *C.char) {
	w := lookup(owner)
	if w == nil || w.rendering || w.modal || w.onEvent == nil {
		return
	}
	w.onEvent(Event{ID: C.GoString(id), Value: C.GoString(value)})
}

//export nativeUITick
func nativeUITick(owner C.uintptr_t) {
	w := lookup(owner)
	if w == nil {
		return
	}
	w.mu.Lock()
	controls := w.controls
	w.controls = nil
	w.mu.Unlock()
	for _, f := range controls {
		f()
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if w.modal || closed {
		return
	}
	w.mu.Lock()
	queue := w.queue
	w.queue = nil
	w.mu.Unlock()
	for _, f := range queue {
		f()
	}
	w.mu.Lock()
	closed = w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	if !w.statusUntil.IsZero() && !time.Now().Before(w.statusUntil) {
		ctext("", func(s *C.char) { C.wallet_ui_status(w.widget, s) })
		w.statusUntil = time.Time{}
	}
}

//export nativeUIClosed
func nativeUIClosed(owner C.uintptr_t) {
	w := lookup(owner)
	if w == nil {
		return
	}
	w.mu.Lock()
	w.closed = true
	w.cancelEpoch++
	// Drain queued prompt completions after closure so their callers cannot
	// remain blocked waiting for a GTK loop that has stopped.
	queue := w.queue
	w.queue = nil
	w.mu.Unlock()
	w.cancelOnThread()
	for _, f := range queue {
		f()
	}
	if w.onEvent != nil {
		w.onEvent(Event{ID: "close"})
	}
}

//export nativeUIActivity
func nativeUIActivity(owner C.uintptr_t) {
	w := lookup(owner)
	if w == nil || w.onEvent == nil || time.Since(w.lastInput) < time.Second {
		return
	}
	w.lastInput = time.Now()
	w.onEvent(Event{ID: "activity"})
}
