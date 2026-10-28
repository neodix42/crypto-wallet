package webui

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var bundle embed.FS

var errDesktop = errors.New("wallet desktop unavailable")

// A role sees only its own entry document. There is no HTTP listener, fallback
// file server, directory listing, user-supplied path or external asset handler.
type roleAssets struct{ private bool }

func (r roleAssets) Open(name string) (fs.File, error) {
	if name != path.Clean(name) || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return nil, fs.ErrNotExist
	}
	if name == "." {
		return bundle.Open("frontend/dist")
	}
	if name == "index.html" {
		if r.private {
			name = "secret/index.html"
		} else {
			name = "index.html"
		}
	} else if !strings.HasPrefix(name, "assets/") || strings.Contains(name, "..") {
		return nil, fs.ErrNotExist
	}
	return bundle.Open("frontend/dist/" + name)
}

// Wails' default logger can print binding arguments or panic details. No
// framework diagnostic is allowed to serialize a secret-bearing invocation.
type silentLogger struct{}

func (silentLogger) Print(string)   {}
func (silentLogger) Trace(string)   {}
func (silentLogger) Debug(string)   {}
func (silentLogger) Info(string)    {}
func (silentLogger) Warning(string) {}
func (silentLogger) Error(string)   {}
func (silentLogger) Fatal(string)   {}

func appOptions(private bool) *options.App {
	return &options.App{Title: "CryptoWallet", Width: 1440, Height: 900, MinWidth: 1000, MinHeight: 640,
		BackgroundColour: options.NewRGB(13, 18, 27), AssetServer: &assetserver.Options{Assets: roleAssets{private}},
		Logger: silentLogger{}, LogLevel: logger.ERROR, LogLevelProduction: logger.ERROR,
		ErrorFormatter:           func(error) any { return "Wallet request unavailable." },
		EnableDefaultContextMenu: false, EnableFraudulentWebsiteDetection: false,
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		Linux:       &linux.Options{ProgramName: "cryptowallet", WebviewGpuPolicy: linux.WebviewGpuPolicyNever}}
}

// Window serializes controller transitions independently of WebKit's GTK
// thread. While a private child is open only lifecycle controls may run.
// Render/Snapshot/Event are strictly public; private material uses pipes only.
type Window struct {
	mu              sync.Mutex
	view            View
	ctx             context.Context
	closed, modal   bool
	queue, controls chan func()
	done            chan struct{}
	onEvent         func(Event)
	cancelEpoch     uint64
	activeCancel    context.CancelFunc
	activeDone      <-chan struct{}
	lastActivity    time.Time
}

func New(onEvent func(Event)) (*Window, error) {
	return &Window{onEvent: onEvent, queue: make(chan func(), 256), controls: make(chan func(), 64), done: make(chan struct{})}, nil
}
func (w *Window) Dispatch(f func()) {
	if f != nil {
		select {
		case w.queue <- f:
		case <-w.done:
		}
	}
}
func (w *Window) DispatchControl(f func()) {
	if f != nil {
		select {
		case w.controls <- f:
		case <-w.done:
		}
	}
}
func (w *Window) loop() {
	for {
		// Lifecycle work wins over a queued stream of public actions.
		select {
		case f := <-w.controls:
			f()
			continue
		case <-w.done:
			return
		default:
		}
		select {
		case f := <-w.controls:
			f()
		case f := <-w.queue:
			f()
		case <-w.done:
			return
		}
	}
}
func (w *Window) Render(v View) {
	if !validateView(v) {
		return
	}
	v = cloneView(v)
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.view = v
	ctx := w.ctx
	w.mu.Unlock()
	if ctx != nil {
		wruntime.WindowSetTitle(ctx, v.Title)
		wruntime.EventsEmit(ctx, "wallet:view", v)
	}
}
func (w *Window) Close() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.cancelEpoch++
	cancel, finished := w.activeCancel, w.activeDone
	ctx := w.ctx
	close(w.done)
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if finished != nil {
		<-finished
	}
	if ctx != nil {
		wruntime.Quit(ctx)
	}
}
func (w *Window) CancelPrompt() {
	w.mu.Lock()
	w.cancelEpoch++
	cancel, finished := w.activeCancel, w.activeDone
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if finished != nil {
		<-finished
	}
}
func (w *Window) Run() error {
	prepareWebKitRuntime()
	configureWebKitSignal()
	a := appOptions(false)
	a.Bind = []interface{}{&PublicBridge{window: w}}
	a.OnStartup = func(ctx context.Context) {
		w.mu.Lock()
		w.ctx = ctx
		closed := w.closed
		title := w.view.Title
		w.mu.Unlock()
		if title != "" {
			wruntime.WindowSetTitle(ctx, title)
		}
		if closed {
			wruntime.Quit(ctx)
		}
	}
	a.OnBeforeClose = func(context.Context) bool {
		w.mu.Lock()
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return false
		}
		w.DispatchControl(func() {
			if w.onEvent != nil {
				w.onEvent(Event{ID: "close"})
			}
			w.Close()
		})
		return true
	}
	a.OnShutdown = func(context.Context) { w.Close() }
	go w.loop()
	err := wails.Run(a)
	w.Close()
	if err != nil {
		return errDesktop
	}
	return nil
}

// PublicBridge contains no secret/prompt method or generic backend transport.
// Events must reference a live enabled widget. No arbitrary method, field name,
// executable, address-to-clipboard or prompt ID can be supplied by a renderer.
type PublicBridge struct{ window *Window }

func (b *PublicBridge) Snapshot() View {
	b.window.mu.Lock()
	defer b.window.mu.Unlock()
	return cloneView(b.window.view)
}
func validEvent(v View, e Event, modal bool) bool {
	if !plain(e.ID, 256) || !plain(e.Value, 4096) {
		return false
	}
	if e.ID == "activity" || e.ID == "lock" || e.ID == "close" {
		return e.Value == ""
	}
	if modal {
		return false
	}
	if e.ID == "tab" || e.ID == "closeTab" {
		for _, p := range v.Pages {
			if p.ID == e.Value {
				return e.ID == "tab" || strings.HasPrefix(p.ID, "wallet/")
			}
		}
		return false
	}
	var match func([]Node) bool
	match = func(nodes []Node) bool {
		for _, n := range nodes {
			if n.Disabled {
				continue
			}
			if n.ID == e.ID {
				switch n.Kind {
				case "button", "row":
					return e.Value == ""
				case "entry":
					return true
				case "check":
					return e.Value == "true" || e.Value == "false"
				case "combo":
					for _, o := range n.Options {
						if o.ID == e.Value {
							return true
						}
					}
				}
			}
			if match(n.Children) {
				return true
			}
		}
		return false
	}
	for _, p := range v.Pages {
		if p.ID == v.Active {
			return match(p.Nodes)
		}
	}
	return false
}
func (b *PublicBridge) Event(e Event) bool {
	w := b.window
	w.mu.Lock()
	valid := !w.closed && validEvent(w.view, e, w.modal)
	if e.ID == "activity" {
		if time.Since(w.lastActivity) < time.Second {
			valid = false
		} else if valid {
			w.lastActivity = time.Now()
		}
	}
	w.mu.Unlock()
	if !valid {
		return false
	}
	f := func() {
		w.mu.Lock()
		valid := !w.closed && validEvent(w.view, e, w.modal)
		w.mu.Unlock()
		if valid && w.onEvent != nil {
			w.onEvent(e)
		}
	}
	if e.ID == "lock" || e.ID == "close" {
		select {
		case w.controls <- f:
			return true
		case <-w.done:
			return false
		default:
			return false
		}
	}
	select {
	case w.queue <- f:
		return true
	case <-w.done:
		return false
	default:
		return false
	}
}
