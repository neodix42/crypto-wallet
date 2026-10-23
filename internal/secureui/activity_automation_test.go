//go:build automation

package secureui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeProgrammaticFillAndDialogTimersDoNotCountAsInput(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	var count atomic.Int32
	SetActivity(func() { count.Add(1) })
	defer SetActivity(nil)
	autoSchedule("public synthetic programmatic fixture", -3)
	out, err := Input("TEST programmatic fill is not user input", "Synthetic password")
	for _, b := range out {
		clear(b)
	}
	if err != nil || count.Load() != 0 {
		t.Fatal("background fill/timer counted as user activity", err, count.Load())
	}
}

func TestRealX11NativeKeyAndButtonActivity(t *testing.T) {
	display := os.Getenv("DISPLAY")
	if !strings.HasPrefix(display, ":18") {
		t.Skip("NOT_RUN: real XTest input requires an isolated :18x X11 test display")
	}
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	_, source, _, _ := runtime.Caller(0)
	module := filepath.Join(filepath.Dir(source), "..", "..", "tests", "e2e")
	const script = `import ctypes as C,os,sys,time
sys.path.insert(0,sys.argv[1])
from x11 import Desktop,X
d=Desktop(os.environ['DISPLAY'].encode());w=d.wait('TEST input-only native activity')[0]
X.XSetInputFocus.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_ulong]
X.XSetInputFocus(d.d,w,2,0);X.XFlush(d.d)
# Redraws, focus and idle time must not count as keyboard/button activity.
time.sleep(1.2)
d.key('a');time.sleep(1.15)
d.key('b');time.sleep(1.15)
X.XTranslateCoordinates.argtypes=[C.c_void_p,C.c_ulong,C.c_ulong,C.c_int,C.c_int,C.POINTER(C.c_int),C.POINTER(C.c_int),C.POINTER(C.c_ulong)]
x=C.c_int();y=C.c_int();child=C.c_ulong()
X.XTranslateCoordinates(d.d,w,d.root,50,60,C.byref(x),C.byref(y),C.byref(child))
d.click(x.value,y.value)
d.key('Escape')
`
	cmd := exec.Command("/usr/bin/python3", "-c", script, module)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	var count atomic.Int32
	var first atomic.Int64
	start := time.Now()
	SetActivity(func() {
		first.CompareAndSwap(0, time.Since(start).Nanoseconds())
		count.Add(1)
	})
	defer SetActivity(nil)
	// Test-only GLib fallback bounds a failed XTest helper without adding any
	// timeout/polling activity to production credential dialogs.
	autoSchedule("", -6)
	// autoSchedule's normal 100ms response would hide the window before the
	// real input arrives; use the explicit automation-only delayed variant.
	autoDelay(8000)
	defer autoCancel()
	out, err := Input("TEST input-only native activity", "Synthetic password")
	for _, b := range out {
		clear(b)
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		t.Fatalf("real X11 helper failed: %v: %s", waitErr, output.String())
	}
	if err == nil || out != nil || count.Load() != 3 || time.Duration(first.Load()) < time.Second {
		t.Fatal("native key/button activity or idle exclusion failed", err, count.Load(), time.Duration(first.Load()))
	}
}
