//go:build automation

package secureui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRealX11NativeActivityBeyondFiveMinutes(t *testing.T) {
	if os.Getenv("LOCALWALLET_LONG_NATIVE_ACTIVITY") != "1" {
		t.Skip("NOT_RUN: explicit five-minute wall-clock native input campaign")
	}
	if !strings.HasPrefix(os.Getenv("DISPLAY"), ":18") {
		t.Fatal("BLOCKED: long native input requires an owned private :18x display")
	}
	if err := Init(); err != nil {
		t.Fatalf("BLOCKED native display: %v", err)
	}
	_, source, _, _ := runtime.Caller(0)
	module := filepath.Join(filepath.Dir(source), "..", "..", "tests", "e2e")
	const script = `import ctypes as C,os,sys,time
sys.path.insert(0,sys.argv[1])
from x11 import Desktop,X
d=Desktop(os.environ['DISPLAY'].encode());w=d.wait('TEST sustained public native activity')[0]
X.XSetInputFocus.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_ulong]
X.XSetInputFocus(d.d,w,2,0);X.XFlush(d.d)
for i in range(62):
 d.key('a')
 time.sleep(5)
d.key('Escape')
`
	ctx, cancel := context.WithTimeout(context.Background(), 340*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-c", script, module)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	start := time.Now()
	var count atomic.Int32
	var last atomic.Int64
	var expired atomic.Bool
	SetActivity(func() {
		last.Store(time.Since(start).Nanoseconds())
		count.Add(1)
	})
	defer SetActivity(nil)
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				if time.Since(start)-time.Duration(last.Load()) >= 5*time.Minute {
					expired.Store(true)
				}
			}
		}
	}()
	autoSchedule("", -6)
	autoDelay(330000)
	defer autoCancel()
	out, err := Input("TEST sustained public native activity", "Public synthetic activity fixture")
	for _, b := range out {
		clear(b)
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		t.Fatal("long X11 helper failed", waitErr, output.String())
	}
	if err == nil || out != nil || time.Since(start) < 5*time.Minute || count.Load() < 60 || expired.Load() {
		t.Fatal("sustained native input did not preserve inactivity semantics", err, time.Since(start), count.Load(), expired.Load())
	}
	t.Logf("PASS: %d actual key events over %s; no five-minute inactivity expiration", count.Load(), time.Since(start).Round(time.Millisecond))
}
