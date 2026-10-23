//go:build automation

package secureui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeRejectsMultibyteSecretOverByteLimit(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	// GTK's limit is in characters; the signer additionally limits UTF-8 bytes.
	autoSchedule(strings.Repeat("é", 4096), -3)
	if out, err := (Native{}).Input("TEST public synthetic oversized UTF8", "Synthetic password"); !errors.Is(err, ErrInputTooLong) || out != nil {
		t.Fatal("native input exposed over-limit UTF8")
	}
}

func TestNativeFixedFailureNotice(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	autoSchedule("", -7)
	(Native{}).Notice("TEST public validation guidance", "Choose a new encrypted backup filename.")
}

func TestNativePublicReceiveAndScrollableApproval(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	autoSchedule("", -3)
	if !(Native{}).Receive("TEST public QR image", "Public address", encoded.Bytes()) {
		t.Fatal("valid public receive image rejected")
	}
	if (Native{}).Receive("TEST invalid public image", "Public address", []byte("not a PNG")) {
		t.Fatal("invalid receive image approved")
	}
	autoSchedule("", -3)
	if !(Native{}).Confirm("TEST scroll exact effects", "Public summary\nExact chain effects:\n"+strings.Repeat("public synthetic effect\n", 200)) {
		t.Fatal("long native approval unavailable")
	}
	(Native{}).CopyPublic("public synthetic receive address")
}

func TestNativeReceiveCopyRequiresExplicitButtonAndExactAddress(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	const address = "public synthetic receive address 79326"
	const oldClipboard = "public synthetic prior clipboard 45182"
	const summary = "Public fixture summary with a different address-shaped string: 0x0000000000000000000000000000000000000001"
	ui := Native{}
	ui.CopyPublic(oldClipboard)
	autoSchedule("", -3)
	if !ui.ReceiveAddress("TEST public receive without copy", summary, address, encoded.Bytes()) || autoPublicClipboard() != oldClipboard {
		t.Fatal("accepting verified receive implicitly copied an address")
	}
	autoScheduleReceiveCopy(-3)
	if !ui.ReceiveAddress("TEST public receive explicit copy", summary, address, encoded.Bytes()) || autoPublicClipboard() != address {
		t.Fatal("native Copy address did not copy the exact separately supplied public address")
	}
	ui.CopyPublic(oldClipboard)
	autoScheduleReceiveCopy(-6)
	if ui.ReceiveAddress("TEST public copy then cancel", summary, address, encoded.Bytes()) || autoPublicClipboard() != address {
		t.Fatal("cancellation changed the deliberate public copy or approved the view")
	}
	autoSchedule("", -6)
	if ui.ReceiveAddress("TEST public receive cancel", summary, address, encoded.Bytes()) {
		t.Fatal("cancel approved verified receive")
	}
	for _, invalid := range []struct {
		address string
		image   []byte
	}{
		{"", encoded.Bytes()},
		{address + "\x00substitution", encoded.Bytes()},
		{address, nil},
		{address, []byte("not a PNG")},
	} {
		if ui.ReceiveAddress("TEST invalid public receive", summary, invalid.address, invalid.image) {
			t.Fatal("invalid native receive accepted")
		}
	}
}

func TestNativeReceiveLongGuidanceKeepsCopyAndDoneVisible(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	// Deliberately much longer than current funding guidance: the public body
	// must scroll while its explicit copy and completion controls remain visible.
	autoScheduleReceiveLayout()
	if !(Native{}).ReceiveAddress("TEST bounded public receive", strings.Repeat("Public network prerequisite guidance\n", 100), "public synthetic receive address 79326", encoded.Bytes()) {
		t.Fatal("bounded receive view unavailable")
	}
	fits, width, height, screenWidth, screenHeight := autoReceiveLayout()
	t.Logf("receive=%dx%d display=%dx%d mapped Copy address and Done inside window=%v", width, height, screenWidth, screenHeight, fits)
	if !fits || width > 650 || height > 680 {
		t.Fatal("receive window or its action buttons overflowed the bounded display")
	}
}

func TestNativeFileChooserCancelAndVerifiedSelection(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	for _, save := range []bool{true, false} {
		autoSchedule("", -6)
		if path, err := (Native{}).File("TEST cancel public file selection", save); err == nil || path != "" {
			t.Fatal("cancelled file selection returned a path")
		}
	}
	path := filepath.Join(t.TempDir(), "public-synthetic.png")
	if err := os.WriteFile(path, []byte("public synthetic file"), 0600); err != nil {
		t.Fatal(err)
	}
	autoScheduleFile(path)
	defer autoCancel()
	if selected, err := (Native{}).File("TEST select public synthetic file", false); err != nil || selected != path {
		t.Fatal("native chooser changed selected path", selected, err)
	}
}

func TestNativeInitUnavailableDisplayFailsClosed(t *testing.T) {
	if os.Getenv("LOCALWALLET_TEST_INVALID_DISPLAY") == "1" {
		if Init() == nil {
			t.Fatal("native initialization accepted unavailable display")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeInitUnavailableDisplayFailsClosed$")
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "DISPLAY=") && !strings.HasPrefix(value, "WAYLAND_DISPLAY=") && !strings.HasPrefix(value, "GDK_BACKEND=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "DISPLAY=:65530", "GDK_BACKEND=x11", "LOCALWALLET_TEST_INVALID_DISPLAY=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native missing-display subprocess: %v: %s", err, output)
	}
}

func TestNativePublicClipboardIsServedWhileIdleWithoutActivity(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	const publicAddress = "public synthetic receive address 79326"
	(Native{}).CopyPublic(publicAddress)
	const reader = `import ctypes as C
g=C.CDLL('libgtk-3.so.0');d=C.CDLL('libgdk-3.so.0');glib=C.CDLL('libglib-2.0.so.0')
g.gtk_init_check.argtypes=[C.c_void_p,C.c_void_p];g.gtk_init_check.restype=C.c_int
if not g.gtk_init_check(None,None):raise SystemExit('reader display unavailable')
d.gdk_atom_intern_static_string.argtypes=[C.c_char_p];d.gdk_atom_intern_static_string.restype=C.c_void_p
g.gtk_clipboard_get.argtypes=[C.c_void_p];g.gtk_clipboard_get.restype=C.c_void_p
g.gtk_clipboard_wait_for_text.argtypes=[C.c_void_p];g.gtk_clipboard_wait_for_text.restype=C.c_void_p
glib.g_free.argtypes=[C.c_void_p]
p=g.gtk_clipboard_wait_for_text(g.gtk_clipboard_get(d.gdk_atom_intern_static_string(b'CLIPBOARD')))
if not p:raise SystemExit('native owner did not serve clipboard')
value=C.string_at(p);glib.g_free(p)
if value!=b'public synthetic receive address 79326':raise SystemExit('clipboard address mismatch')
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-c", reader)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var activity atomic.Int32
	SetActivity(func() { activity.Add(1) })
	defer SetActivity(nil)
	for {
		select {
		case err := <-done:
			if err != nil || activity.Load() != 0 {
				t.Fatal("idle public clipboard request failed or counted as input", err, output.String(), activity.Load())
			}
			return
		case <-tick.C:
			Pump()
		}
	}
}
