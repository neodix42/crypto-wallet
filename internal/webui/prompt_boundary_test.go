package webui

import (
	"bytes"
	"image"
	"image/png"
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

func TestMainWindowKeepsOverviewHeightWithinMonitorBounds(t *testing.T) {
	opts := appOptions()
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

func TestConfiguredWebKitSignalIsNeverReassigned(t *testing.T) {
	t.Setenv("JSC_SIGNAL_FOR_GC", "40")
	if configureWebKitSignal() != 0 || os.Getenv("JSC_SIGNAL_FOR_GC") != "40" {
		t.Fatal("existing JavaScriptCore signal policy was overwritten")
	}
}
