//go:build automation

package nativeui

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"localwallet/internal/uiprompt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Only frozen public fixtures are used. This test never opens a wallet profile,
// imports a user's root, retains a screenshot or contacts any blockchain.
func TestNativeDialogsAndCancellationOnPrivateX11(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_NATIVE_TEST") != "1" || !strings.HasPrefix(os.Getenv("DISPLAY"), ":18") {
		t.Skip("NOT_RUN: requires explicitly owned private :18x Xvfb")
	}
	const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	const id = "0123456789abcdef0123456789abcdef"
	const canary = "public-synthetic-private-input-71032"
	var privateEvents atomic.Int32
	w, err := New(func(e Event) {
		if strings.Contains(e.Value, canary) || strings.Contains(e.Value, phrase) {
			privateEvents.Add(1)
		}
	})
	if err != nil {
		t.Fatal("private test display unavailable")
	}
	view := View{Title: "TEST CryptoWallet native", Active: "main", Pages: []Page{{ID: "main", Title: "Main account", Nodes: []Node{{Kind: "label", Text: "Public test view"}, {Kind: "entry", ID: "publicRecipient", Value: "public-recipient-value"}}}, {ID: "other", Title: "Other public wallet", Nodes: []Node{{Kind: "column", Children: []Node{{Kind: "heading", Text: "Public wallet"}, {Kind: "separator"}, {Kind: "row", ID: "publicRow", Children: []Node{{Kind: "address", Value: "public-owned-address"}, {Kind: "button", ID: "publicCopy", Text: "Copy public address", Tooltip: "Public tooltip"}}}, {Kind: "combo", ID: "publicAsset", Value: "native", Options: []Option{{ID: "native", Text: "Native asset"}}}, {Kind: "check", ID: "publicHidden", Text: "Public fixture", Value: "true", Disabled: true}}}}}}}
	w.Render(view)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer w.Close()
		focusCheck := make(chan bool, 1)
		w.Dispatch(func() {
			w.Render(View{Title: "bad\x00public title"})
			if w.autoStatus() != "Desktop configuration unavailable." {
				t.Error("invalid public model did not fail closed")
			}
			view.Status = "Repeated fixed public status"
			view.StatusRevision = 1
			w.Render(view)
			w.statusUntil = time.Now().Add(-time.Millisecond)
			w.Render(view)
			if w.autoStatus() != "" {
				t.Error("background render revived expired status")
			}
			view.StatusRevision++
			w.Render(view)
			deadline := w.statusUntil
			if w.autoStatus() != view.Status || time.Until(deadline) < 6*time.Second {
				t.Error("new equal status did not start seven-second interval")
			}
			w.Render(view)
			if !w.statusUntil.Equal(deadline) {
				t.Error("background render restarted status timer")
			}
			if state := w.autoVisibleTab(0); state != 6 {
				t.Errorf("notebook tab label was not visible: state %d", state)
			}
			view.Pages[1].Nodes = append(view.Pages[1].Nodes, Node{Kind: "address", Text: "Owned wallet receive address", Value: "public-owned-address"}, Node{Kind: "address", Text: "Token contract (not your receive address)", Value: "public-token-contract"})
			view.Pages[1].Nodes = append(view.Pages[1].Nodes, Node{Kind: "address", Text: "public-fallback-address"})
			view.Active = "other"
			w.Render(view)
			if !w.autoPlainLabel("Owned wallet receive address") || !w.autoPlainLabel("Token contract (not your receive address)") {
				t.Error("native address fields omitted their distinguishing labels")
			}
			if w.autoActive() != "other" {
				t.Error("requested wallet tab did not become active")
			}
			view.Active = "main"
			w.Render(view)
			ok := w.autoFocus("publicRecipient", 7, 2, 7)
			view.Status = "A public balance changed while editing"
			w.Render(view)
			id, position, start, end := w.autoFocusState()
			focusCheck <- ok && id == "publicRecipient" && position == 7 && start == 2 && end == 7
		})
		if !<-focusCheck {
			t.Error("background rerender lost public entry focus, cursor or selection")
		}
		var pngData bytes.Buffer
		_ = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 8, 8)))
		for _, kind := range []string{"input", "confirm", "notice", "receive", "recovery", "recoveryBackup", "file", "copy"} {
			p := uiprompt.Prompt{ID: id, Kind: kind, Title: "TEST private " + kind}
			switch kind {
			case "input":
				p.Labels = []string{"Synthetic password", "Synthetic private phrase"}
			case "confirm", "notice":
				p.Summary = "Public fixture approval / notice"
			case "receive":
				p.Address, p.PNG = "public-owned-address", pngData.Bytes()
			case "recovery", "recoveryBackup":
				p.Phrase = phrase
			case "copy":
				p.Address = "public-owned-address"
			case "file":
				save := true
				p.Save = &save
			}
			response := -3
			if kind == "notice" {
				response = -7
			}
			if kind == "file" {
				response = -6
			}
			if kind != "copy" {
				w.Dispatch(func() {
					value := ""
					if kind == "input" {
						value = canary
					}
					autoSchedule(value, response, 0)
				})
			}
			r, err := w.Prompt(p)
			if err != nil || r.Accepted != (kind != "file") || r.PromptID != id {
				t.Errorf("native %s prompt failed", kind)
			}
			if kind == "input" {
				if len(r.Values) != 2 || r.Values[0] != canary || r.Values[1] != canary {
					t.Error("masked input changed public fixture bytes")
				}
				masked := make(chan bool, 1)
				w.Dispatch(func() { masked <- autoMasked() })
				if !<-masked {
					t.Error("private input was visible or unbounded")
				}
			}
			for i := range r.Values {
				r.Values[i] = ""
			}
			if kind == "recovery" {
				clip := make(chan string, 1)
				w.Dispatch(func() { clip <- autoClipboard() })
				if <-clip == phrase {
					t.Error("initial recovery copied phrase without authorization")
				}
			}
		}
		fileFolder := t.TempDir()
		w.Dispatch(func() { autoScheduleFile(fileFolder, "public-qr-fixture.png") })
		save := true
		fileReply, fileErr := w.Prompt(uiprompt.Prompt{ID: id, Kind: "file", Title: "TEST public QR save selection", Save: &save})
		if fileErr != nil || !fileReply.Accepted || fileReply.Path != filepath.Join(fileFolder, "public-qr-fixture.png") {
			t.Error("native save chooser lost exact owned path")
		}
		if _, err := os.Stat(fileReply.Path); !os.IsNotExist(err) {
			t.Error("file chooser wrote a public file without backend handling")
		}
		_, imageErr := w.Prompt(uiprompt.Prompt{ID: id, Kind: "receive", Title: "TEST malformed receive image", Address: "public-address", PNG: []byte("invalid public PNG")})
		if !errors.Is(imageErr, uiprompt.ErrProtocol) {
			t.Error("invalid QR bytes rendered as a receive view")
		}
		w.Dispatch(func() { autoSchedule("", 1, -3) })
		_, imageErr = w.Prompt(uiprompt.Prompt{ID: id, Kind: "receive", Title: "TEST explicit public QR copy", Address: "public-exact-QR-address", PNG: pngData.Bytes()})
		if imageErr != nil {
			t.Error("explicit receive copy failed")
		}
		publicClip := make(chan string, 1)
		w.Dispatch(func() { publicClip <- autoClipboard() })
		if <-publicClip != "public-exact-QR-address" {
			t.Error("receive copy changed verified public address")
		}
		// Reproduce a lock after the backend queued a private prompt but before
		// GTK opened it. It must complete cancelled without ever showing a view.
		queuedResult := make(chan error, 1)
		w.Dispatch(func() {
			go func() {
				_, err := w.Prompt(uiprompt.Prompt{ID: id, Kind: "confirm", Title: "TEST cancelled before display", Summary: "Public fixture"})
				queuedResult <- err
			}()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				w.mu.Lock()
				pending := len(w.queue) > 0
				w.mu.Unlock()
				if pending {
					break
				}
				runtime.Gosched()
			}
			w.CancelPrompt()
		})
		if err := <-queuedResult; !errors.Is(err, uiprompt.ErrCancelled) {
			t.Error("queued private prompt survived cancellation epoch")
		}
		// Only the authenticated recoveryBackup prompt exposes Copy phrase.
		w.Dispatch(func() { autoSchedule("", 1, -3) })
		r, err := w.Prompt(uiprompt.Prompt{ID: id, Kind: "recoveryBackup", Title: "TEST authenticated backup copy", Phrase: phrase})
		if err != nil || !r.Accepted {
			t.Error("authenticated backup copy failed")
		}
		clip := make(chan string, 1)
		w.Dispatch(func() { clip <- autoClipboard() })
		if <-clip != phrase {
			t.Error("backup copy did not copy exact phrase")
		}
		// Cancellation clears masked entries and unblocks a pending client even
		// while ordinary backend updates are deferred by the private modal loop.
		var updated atomic.Bool
		var controlRan atomic.Bool
		go func() {
			time.Sleep(60 * time.Millisecond)
			w.Dispatch(func() { updated.Store(true) })
			w.DispatchControl(func() { controlRan.Store(true) })
			time.Sleep(60 * time.Millisecond)
			w.CancelPrompt()
		}()
		_, err = w.Prompt(uiprompt.Prompt{ID: id, Kind: "input", Title: "TEST cancel private input", Labels: []string{"Synthetic password"}})
		if !errors.Is(err, uiprompt.ErrCancelled) {
			t.Error("priority cancellation did not invalidate active prompt")
		}
		if updated.Load() {
			t.Error("public backend update ran inside private modal loop")
		}
		if !controlRan.Load() {
			t.Error("lifecycle control was blocked by private modal loop")
		}
		// Multibyte GTK character limits must not evade the byte limit.
		w.Dispatch(func() { autoSchedule(strings.Repeat("é", 2049), -3, 0) })
		_, err = w.Prompt(uiprompt.Prompt{ID: id, Kind: "input", Title: "TEST multibyte bound", Labels: []string{"Synthetic password"}})
		if !errors.Is(err, uiprompt.ErrInputTooLong) {
			t.Error("multibyte input evaded 4096-byte limit")
		}
		if privateEvents.Load() != 0 {
			t.Error("private prompt data escaped to public events")
		}
		// Closing the public window must also release an active private input
		// and its waiting backend goroutine, without touching destroyed widgets.
		go func() { time.Sleep(80 * time.Millisecond); w.Close() }()
		_, err = w.Prompt(uiprompt.Prompt{ID: id, Kind: "input", Title: "TEST close with private input", Labels: []string{"Synthetic password"}})
		if !errors.Is(err, uiprompt.ErrCancelled) {
			t.Error("window close left a private prompt alive")
		}
	}()
	w.Run()
	<-finished
	w.autoLateCallbacks() // GLib callbacks left after closure must be inert.
}
