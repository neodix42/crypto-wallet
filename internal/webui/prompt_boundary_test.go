package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
	"os"
	"strings"
	"testing"
)

func receiveFixture(t *testing.T, width int) uiprompt.Prompt {
	t.Helper()
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewNRGBA(image.Rect(0, 0, width, 1))); err != nil {
		t.Fatal(err)
	}
	return uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "receive", Title: "Receive", Address: "public-owned-address", PNG: pixels.Bytes()}
}

func TestPrivateEnvelopeRejectsForeignAuthorityBeforeWindowCreation(t *testing.T) {
	for _, mode := range []string{"valid-input", "valid-receive", "bad-session", "foreign-id", "error", "method", "missing-prompt", "copy", "file", "bad-image", "large-image", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			prompt := promptFixture()
			if mode == "valid-receive" || mode == "bad-image" || mode == "large-image" {
				prompt = receiveFixture(t, 1)
			}
			if mode == "bad-image" {
				prompt.PNG = []byte("not a PNG")
			}
			if mode == "large-image" {
				prompt = receiveFixture(t, 2049)
			}
			if mode == "copy" {
				prompt = uiprompt.Prompt{ID: prompt.ID, Kind: "copy", Title: "Copy", Address: "public-address"}
			}
			if mode == "file" {
				save := false
				prompt = uiprompt.Prompt{ID: prompt.ID, Kind: "file", Title: "File", Save: &save}
			}
			body, _ := json.Marshal(prompt)
			message := ipc.Message{Version: 1, ID: prompt.ID, Session: strings.Repeat("b", 32), Method: "prompt", Body: body}
			switch mode {
			case "bad-session":
				message.Session = strings.Repeat("z", 32)
			case "foreign-id":
				message.ID = strings.Repeat("c", 32)
			case "error":
				message.Error = "SYNTHETIC-PRIVATE-FAILURE"
			case "method":
				message.Method = "private-action"
			case "missing-prompt":
				message.Body = []byte(`{}`)
			}
			var wire bytes.Buffer
			if mode == "truncated" {
				wire.Write([]byte{0, 0, 0, 20, '{'})
			} else if err := (&ipc.Channel{W: &wire}).Write(message); err != nil {
				t.Fatal(err)
			}
			got, session, err := readPrivatePrompt(&ipc.Channel{R: &wire})
			if strings.HasPrefix(mode, "valid-") {
				if err != nil || got.ID != prompt.ID || session != message.Session || got.Kind != prompt.Kind {
					t.Fatal("exact bound private envelope rejected")
				}
			} else if err == nil || session != "" || got.ID != "" || got.Phrase != "" {
				t.Fatal("invalid private envelope reached a browser role")
			}
		})
	}
}

func TestPrivateInputGeometryIsBoundedWithoutChangingRoleAuthority(t *testing.T) {
	for _, count := range []int{1, 4, 8} {
		p := promptFixture()
		p.Labels = make([]string, count)
		for i := range p.Labels {
			p.Labels[i] = "Private value"
		}
		options := privateAppOptions(p)
		if options.Height < 260 || options.Height > 850 || options.Width != 680 || options.Title != p.Title || len(options.Bind) != 0 {
			t.Fatal("private input sizing changed authority or exceeded bounded workarea")
		}
	}
	for _, kind := range []string{"receive", "confirm", "notice"} {
		p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: kind, Title: "Private view"}
		options := privateAppOptions(p)
		if options.Height != 440 && options.Height != 660 {
			t.Fatal("private presentation has unbounded height")
		}
	}
}

func TestRecoveryWindowsFitWordCountAndFontWithoutChangingPromptAuthority(t *testing.T) {
	for _, font := range []int{14, 16, 18, 20, 22, 24} {
		for _, words := range []int{12, 15, 18, 21, 24} {
			p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "recoveryBackup", Title: "Back up recovery phrase", Phrase: strings.TrimSpace(strings.Repeat("abandon ", words))}
			backup := privateAppOptions(p, Appearance{"light", font})
			p.Kind = "recovery"
			setup := privateAppOptions(p, Appearance{"dark", font})
			if backup.Width < 840 || backup.Width > 1040 || backup.Height < 750 || backup.Height > 1100 || backup.Height <= setup.Height || backup.Width != setup.Width {
				t.Fatal("recovery content lacks bounded dedicated geometry", font, words, backup.Width, backup.Height)
			}
			if backup.Title != p.Title || backup.MinHeight != 260 || len(backup.Bind) != 0 || len(setup.Bind) != 0 {
				t.Fatal("recovery sizing changed private authority")
			}
			width, height := windowSizeForMonitor(backup.Width, backup.Height, 640, 480)
			if width != 608 || height != 416 {
				t.Fatal("recovery view escaped a small monitor")
			}
		}
	}
	for _, test := range []struct{ words, height int }{{12, 830}, {24, 980}} {
		p := uiprompt.Prompt{Kind: "recoveryBackup", Phrase: strings.TrimSpace(strings.Repeat("abandon ", test.words))}
		if got := privateAppOptions(p); got.Width != 920 || got.Height != test.height {
			t.Fatal("default phrase geometry regressed", test.words, got.Width, got.Height)
		}
	}
}

func TestMainWindowKeepsOverviewHeightWithinMonitorBounds(t *testing.T) {
	opts := appOptions(false)
	if opts.Width != 1440 || opts.Height != 1120 || opts.MinHeight != 640 || opts.MinWidth != 1000 {
		t.Fatal("main overview geometry changed unexpectedly")
	}
	for _, monitor := range []struct{ width, height, expectedWidth, expectedHeight int }{
		{1920, 1200, 1440, 1120}, {1920, 1080, 1440, 1016}, {1366, 768, 1334, 704}, {640, 480, 608, 416}, {0, 0, 1440, 1120},
	} {
		width, height := windowSizeForMonitor(opts.Width, opts.Height, monitor.width, monitor.height)
		if width != monitor.expectedWidth || height != monitor.expectedHeight {
			t.Fatal("main window exceeded logical monitor", monitor, width, height)
		}
	}
}

func TestParentLossAndUnexpectedFramesRevokePrivateSecretsAndCopyAuthority(t *testing.T) {
	for _, mode := range []string{"eof", "matching-ack-then-eof", "unexpected-method", "foreign-session", "duplicate-fields"} {
		t.Run(mode, func(t *testing.T) {
			var wire bytes.Buffer
			p := promptFixture()
			b := &PrivateBridge{prompt: p, delivered: true, session: strings.Repeat("b", 32),
				copySequence: 1, copyPending: make(chan bool, 1), done: make(chan struct{})}
			b.prompt.Phrase = "SYNTHETIC-PRIVATE-CANARY"
			if mode != "eof" {
				message := ipc.Message{Version: 1, ID: p.ID, Session: b.session, Method: "copyResult", Body: []byte(`{"sequence":1,"copied":true}`)}
				if mode == "unexpected-method" {
					message.Method = "reply"
				}
				if mode == "foreign-session" {
					message.Session = strings.Repeat("c", 32)
				}
				if mode == "duplicate-fields" {
					message.Body = []byte(`{"sequence":1,"copied":true,"copied":false}`)
				}
				if err := (&ipc.Channel{W: &wire}).Write(message); err != nil {
					t.Fatal(err)
				}
			}
			b.channel = &ipc.Channel{R: &wire}
			done := b.done
			b.parentMessages()
			if !b.finished || b.prompt.Phrase != "" || b.prompt.Labels != nil || b.done != nil {
				t.Fatal("lost/invalid parent retained private data or request authority")
			}
			select {
			case <-done:
			default:
				t.Fatal("lost parent did not revoke the pending copy lifecycle")
			}
			if got := len(b.copyPending); got != 0 && mode != "matching-ack-then-eof" {
				t.Fatal("unbound parent frame acknowledged a copy")
			}
		})
	}
}

type brokenPrivateWriter struct{}

func (brokenPrivateWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPrivateReplyWriteFailureAndCancellationRetireOneUsePrompt(t *testing.T) {
	p := promptFixture()
	b := &PrivateBridge{prompt: p, delivered: true, session: strings.Repeat("b", 32), channel: &ipc.Channel{W: brokenPrivateWriter{}}, done: make(chan struct{})}
	if b.Reply(uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"SYNTHETIC-PRIVATE-INPUT"}}) || !b.finished || b.prompt.Labels != nil || b.done != nil {
		t.Fatal("failed private pipe retained/replayed accepted credentials")
	}
	var output bytes.Buffer
	b = &PrivateBridge{prompt: p, delivered: true, session: strings.Repeat("b", 32), channel: &ipc.Channel{W: &output}, done: make(chan struct{})}
	b.cancel()
	b.cancel()
	m, err := (&ipc.Channel{R: &output}).Read()
	if err != nil || m.Method != "reply" {
		t.Fatal("window-manager cancellation did not notify the exact parent request")
	}
	r, err := uiprompt.DecodeReply(m.Body, p)
	if err != nil || r.Accepted || len(r.Values) != 0 || output.Len() != 0 || !b.finished || b.prompt.Labels != nil {
		t.Fatal("private cancellation returned data or replayed a reply")
	}
	b = &PrivateBridge{prompt: uiprompt.Prompt{ID: p.ID, Kind: "receive"}, delivered: true, ctx: context.Background(), session: strings.Repeat("b", 32), channel: &ipc.Channel{W: brokenPrivateWriter{}}}
	if b.Copy() || b.copyPending != nil {
		t.Fatal("failed copy intent retained pending authority")
	}
}

func TestPrivateAcknowledgmentCannotOverwritePendingResult(t *testing.T) {
	p := promptFixture()
	b := &PrivateBridge{prompt: p, delivered: true, session: strings.Repeat("b", 32), copySequence: 1, copyPending: make(chan bool, 1)}
	b.copyPending <- false
	ack := ipc.Message{ID: p.ID, Session: b.session, Method: "copyResult", Body: []byte(`{"sequence":1,"copied":true}`)}
	if b.parentMessage(ack) || b.copyAcknowledged != 0 || <-b.copyPending {
		t.Fatal("late acknowledgment replaced an existing copy result")
	}
}

func TestPrivateRoleRequiresOwnedInheritedPipesAndRejectsEnvelopeBeforeBrowser(t *testing.T) {
	if runPrivate(nil, nil, nil) == nil {
		t.Fatal("private role opened without its inherited authority")
	}
	for _, foreignImage := range []bool{false, true} {
		input, inputWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer inputWrite.Close()
		outputRead, output, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer outputRead.Close()
		image, err := os.Open("/proc/self/exe")
		if foreignImage {
			if err == nil {
				image.Close()
			}
			image, err = os.CreateTemp(t.TempDir(), "foreign-image")
		}
		if err != nil {
			t.Fatal(err)
		}
		// The malformed parent frame must be rejected without starting WebKit.
		if err := (&ipc.Channel{W: inputWrite}).Write(ipc.Message{Version: 1, ID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Method: "foreign", Body: []byte(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if runPrivate(input, output, image) == nil {
			t.Fatal("private role accepted a foreign image or initial envelope")
		}
		for _, handle := range []*os.File{input, output, image} {
			if _, err := handle.Stat(); err == nil {
				t.Fatal("rejected private role leaked inherited handles")
			}
		}
	}
}

func TestConfiguredWebKitSignalIsNeverReassigned(t *testing.T) {
	t.Setenv("JSC_SIGNAL_FOR_GC", "40")
	if configureWebKitSignal() != 0 || os.Getenv("JSC_SIGNAL_FOR_GC") != "40" {
		t.Fatal("existing JavaScriptCore signal policy was overwritten")
	}
}
