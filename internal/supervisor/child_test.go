package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Public, synthetic peer only. This implements the private framing protocol;
// real GTK input forwarding is tested separately on a private X11 display.
const activityPeer = `#!/usr/bin/python3
import json,struct,sys,time
def send(m):
 b=json.dumps(m,separators=(',',':')).encode();sys.stdout.buffer.write(struct.pack('>I',len(b))+b);sys.stdout.buffer.flush()
while True:
 h=sys.stdin.buffer.read(4)
 if len(h)!=4:break
 m=json.loads(sys.stdin.buffer.read(struct.unpack('>I',h)[0]));mode=m['body']['mode']
 a=dict(m);a['method']='activity';a['body']={}
 if mode=='foreign-id':a['id']='foreign-public-id'
 if mode=='foreign-session':a['session']='foreign-public-session'
 if mode=='body':a['body']={'input':'public synthetic forbidden canary'}
 if mode=='null':a['body']=None
 if mode=='error':a['error']='public synthetic forbidden canary'
 if mode=='version':a['version']=2
 if mode=='method':a['method']='unexpected'
 if mode=='eof':sys.exit(0)
 if mode=='frame':sys.stdout.buffer.write(struct.pack('>I',1048577));sys.stdout.buffer.flush();sys.exit(0)
 if mode!='none':
  for i in range(65 if mode=='flood' else 3):send(a)
 if mode=='delayed':time.sleep(1.05);send(a)
 if mode=='wait':time.sleep(30)
 m['body']={'ok':True}
 if mode=='denied':m['error']='REQUEST_DENIED'
 send(m)
 if mode=='stale':send(a)
`

func syntheticChild(t *testing.T) *Child {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic-peer")
	if err := os.WriteFile(path, []byte(activityPeer), 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(activityPeer))
	manifest, _ := json.Marshal(map[string]string{filepath.Base(path): hex.EncodeToString(hash[:])})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	c := &Child{Path: path, Data: t.TempDir(), Environment: "testnet"}
	t.Cleanup(c.Kill)
	timer := time.AfterFunc(10*time.Second, c.Kill)
	t.Cleanup(func() { timer.Stop() })
	return c
}

func TestNativeActivityFramesArePrivateBoundedAndRequestScoped(t *testing.T) {
	c := syntheticChild(t)
	var count atomic.Int32
	c.OnActivity = func() { count.Add(1) }
	var out struct{ OK bool }
	if err := c.Call("unlock", map[string]string{"mode": "valid"}, &out); err != nil || !out.OK || count.Load() != 1 {
		t.Fatal("valid activity did not precede real response", err, out, count.Load())
	}
	if err := c.Call("snapshot", map[string]string{"mode": "none"}, &out); err != nil || count.Load() != 1 {
		t.Fatal("ordinary response counted as activity", err)
	}
	if err := c.Call("password", map[string]string{"mode": "delayed"}, &out); err != nil || count.Load() != 3 {
		t.Fatal("continued native input did not refresh inactivity", err, count.Load())
	}
	c.OnActivity = nil
	if err := c.Call("unlock", map[string]string{"mode": "valid"}, &out); err != nil {
		t.Fatal("activity callback is optional", err)
	}
}

func TestMalformedNativeActivityKillsPeerWithoutInputDisclosure(t *testing.T) {
	for _, mode := range []string{"foreign-id", "foreign-session", "body", "null", "error", "version", "method", "flood", "eof", "frame"} {
		t.Run(mode, func(t *testing.T) {
			c := syntheticChild(t)
			var count atomic.Int32
			c.OnActivity = func() { count.Add(1) }
			var out struct{ OK bool }
			err := c.Call("unlock", map[string]string{"mode": mode}, &out)
			if err == nil || out.OK || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), "input") {
				t.Fatal("invalid peer frame was accepted or disclosed its body", err)
			}
			if c.cmd != nil || c.channel != nil || c.session != "" {
				t.Fatal("invalid peer remained active")
			}
			want := int32(0)
			if mode == "flood" {
				want = 1
			}
			if count.Load() != want {
				t.Fatal("unverified activity reached coordinator", count.Load())
			}
		})
	}
}

func TestLateActivityCannotBelongToNextRequest(t *testing.T) {
	c := syntheticChild(t)
	var count atomic.Int32
	c.OnActivity = func() { count.Add(1) }
	if err := c.Call("unlock", map[string]string{"mode": "stale"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("snapshot", map[string]string{"mode": "none"}, nil); err == nil || count.Load() != 1 {
		t.Fatal("previous request activity accepted", err, count.Load())
	}
}

func TestLockInterruptsNativeDialogActivity(t *testing.T) {
	c := syntheticChild(t)
	active := make(chan struct{}, 1)
	c.OnActivity = func() { active <- struct{}{} }
	done := make(chan error, 1)
	go func() { done <- c.Call("password", map[string]string{"mode": "wait"}, nil) }()
	select {
	case <-active:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic native peer did not start")
	}
	c.Kill()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("lock accepted a late native response")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lock did not interrupt pending native request")
	}
}

func TestIdentityAndFailurePaths(t *testing.T) {
	c := syntheticChild(t)
	if err := c.Call("unlock", make(chan int), nil); err == nil {
		t.Fatal("unencodable request accepted")
	}
	if err := c.Call("unlock", map[string]string{"mode": "denied"}, nil); err == nil || err.Error() != "REQUEST_DENIED" {
		t.Fatal("native denial lost", err)
	}
	var invalidOutput int
	if c.Call("snapshot", map[string]string{"mode": "none"}, &invalidOutput) == nil {
		t.Fatal("mismatched response output accepted")
	}
	c.Kill()
	if err := os.WriteFile(c.Path, []byte("modified executable"), 0700); err != nil {
		t.Fatal(err)
	}
	if c.Call("snapshot", nil, nil) == nil {
		t.Fatal("modified peer executable started")
	}
	if err := Verify(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing manifest accepted")
	}
	manifest := filepath.Join(filepath.Dir(c.Path), "manifest.json")
	for _, data := range []string{"{", "{}", `{"synthetic-peer":"x"}`} {
		if err := os.WriteFile(manifest, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if Verify(c.Path) == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
}

type brokenPrivatePipe struct{}

func (brokenPrivatePipe) Write([]byte) (int, error) {
	return 0, errors.New("synthetic broken private pipe")
}

func TestPrivateWriteAndExecutableOpenFailuresFailClosed(t *testing.T) {
	c := syntheticChild(t)
	if err := c.start(); err != nil {
		t.Fatal(err)
	}
	c.channel.W = brokenPrivatePipe{}
	if err := c.Call("unlock", map[string]string{"mode": "none"}, nil); err == nil || c.cmd != nil {
		t.Fatal("failed private write retained child", err)
	}
	if err := os.Chmod(c.Path, 0600); err != nil {
		t.Fatal(err)
	}
	if c.Call("unlock", nil, nil) == nil {
		t.Fatal("non-executable peer started")
	}
	if err := os.Remove(c.Path); err != nil {
		t.Fatal(err)
	}
	if Verify(c.Path) == nil {
		t.Fatal("missing executable accepted")
	}
	if err := os.Mkdir(c.Path, 0700); err != nil {
		t.Fatal(err)
	}
	if Verify(c.Path) == nil {
		t.Fatal("executable directory accepted")
	}
}

func TestGenerationBoundCallCannotOpenOrKillAnotherSession(t *testing.T) {
	c := syntheticChild(t)
	old := c.Generation()
	if err := c.CallAt(old, "snapshot", map[string]string{"mode": "none"}, nil); err != nil {
		t.Fatal("initial bound call", err)
	}
	c.Kill()
	fresh := c.Generation()
	if fresh == old {
		t.Fatal("lock did not change child lifecycle")
	}
	if err := c.CallAt(old, "unlock", map[string]string{"mode": "none"}, nil); err == nil || c.cmd != nil {
		t.Fatal("stale call reopened native process", err)
	}
	if err := c.CallAt(fresh, "snapshot", map[string]string{"mode": "none"}, nil); err != nil {
		t.Fatal("new session cannot start", err)
	}
	live := c.cmd
	if err := c.CallAt(old, "unlock", map[string]string{"mode": "none"}, nil); err == nil || c.cmd != live {
		t.Fatal("stale call killed or reused a fresh session", err)
	}
}

func TestGenerationBoundCallQueuedBehindNativeDialogEndsOnLock(t *testing.T) {
	c := syntheticChild(t)
	old := c.Generation()
	active := make(chan struct{}, 1)
	c.OnActivity = func() { active <- struct{}{} }
	running := make(chan error, 1)
	go func() { running <- c.CallAt(old, "password", map[string]string{"mode": "wait"}, nil) }()
	select {
	case <-active:
	case <-time.After(3 * time.Second):
		t.Fatal("native fixture did not start")
	}
	queued := make(chan error, 1)
	go func() { queued <- c.CallAt(old, "unlock", map[string]string{"mode": "none"}, nil) }()
	c.Kill()
	for _, done := range []chan error{running, queued} {
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("old native session completed after lock")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("native call survived lock")
		}
	}
	if c.cmd != nil {
		t.Fatal("queued stale request started fresh native process")
	}
}
