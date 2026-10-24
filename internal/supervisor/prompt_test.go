package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"localwallet/internal/uiprompt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const promptPeer = `#!/usr/bin/python3
import json,struct,sys
def read():
 h=sys.stdin.buffer.read(4)
 if len(h)!=4:sys.exit(0)
 return json.loads(sys.stdin.buffer.read(struct.unpack('>I',h)[0]))
def send(m):
 b=json.dumps(m,separators=(',',':')).encode();sys.stdout.buffer.write(struct.pack('>I',len(b))+b);sys.stdout.buffer.flush()
count=0
while True:
 m=read();mode=m['body'].get('mode','valid');count+=1
 p=dict(m);p['method']='prompt';p['body']={'id':('%032x'%(1 if mode=='reuse' else count)),'kind':'input','title':'Public fixture','labels':['Password']}
 if mode=='unknown':p['body']['extra']='PUBLIC-canary'
 if mode=='kind':p['body']['kind']='eval'
 if mode=='id':p['body']['id']='INVALID'
 if mode=='error':p['error']='PUBLIC-canary'
 if mode=='session':p['session']='foreign-public-session'
 if mode=='request':p['id']='foreign-public-request'
 send(p)
 if mode=='double':send(p)
 if mode!='unanswered':
  r=read()
  if r['id']!=m['id'] or r['session']!=m['session'] or r['method']!='promptReply':sys.exit(4)
 m['body']={'ok':True};send(m)
`

func promptChild(t *testing.T) *Child {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt-peer")
	if e := os.WriteFile(path, []byte(promptPeer), 0700); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte(promptPeer))
	manifest, _ := json.Marshal(map[string]string{filepath.Base(path): hex.EncodeToString(h[:])})
	if e := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0600); e != nil {
		t.Fatal(e)
	}
	c := &Child{Path: path, Data: t.TempDir(), Environment: "testnet"}
	t.Cleanup(c.Kill)
	timer := time.AfterFunc(10*time.Second, c.Kill)
	t.Cleanup(func() { timer.Stop() })
	return c
}

func TestPromptRepliesDoNotDeadlockSerializedCallAndAreSingleUse(t *testing.T) {
	c := promptChild(t)
	count := 0
	c.OnPrompt = func(raw json.RawMessage) error {
		p, e := uiprompt.DecodePrompt(raw)
		if e != nil {
			return e
		}
		count++
		wrong, _ := json.Marshal(uiprompt.Reply{PromptID: strings.Repeat("f", 32), Accepted: true, Values: []string{"PUBLIC-password"}})
		if c.ReplyPrompt(wrong) == nil {
			t.Fatal("foreign prompt answer accepted")
		}
		reply, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"PUBLIC-password"}})
		if e = c.ReplyPrompt(reply); e != nil {
			return e
		}
		if c.ReplyPrompt(reply) == nil {
			t.Fatal("prompt answer replay accepted")
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		var out struct{ OK bool }
		if e := c.Call("unlock", map[string]string{"mode": "valid"}, &out); e != nil || !out.OK {
			t.Fatal("prompt relay failed", e)
		}
	}
	if count != 2 {
		t.Fatal("prompt dropped")
	}
}

func TestUntrustedChildCannotInventMalformedOverlappingOrReplayedPrompts(t *testing.T) {
	for _, mode := range []string{"unknown", "kind", "id", "error", "session", "request", "double", "unanswered", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			c := promptChild(t)
			c.OnPrompt = func(raw json.RawMessage) error {
				if mode == "double" || mode == "unanswered" {
					return nil
				}
				p, e := uiprompt.DecodePrompt(raw)
				if e != nil {
					return e
				}
				reply, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID})
				return c.ReplyPrompt(reply)
			}
			if mode == "reuse" {
				if e := c.Call("unlock", map[string]string{"mode": mode}, nil); e != nil {
					t.Fatal("initial prompt failed", e)
				}
			}
			if e := c.Call("unlock", map[string]string{"mode": mode}, nil); e == nil || strings.Contains(e.Error(), "PUBLIC") {
				t.Fatal("invalid prompt not rejected safely", e)
			}
			if c.cmd != nil || c.prompt != nil {
				t.Fatal("invalid child retained prompt authority")
			}
		})
	}
	c := promptChild(t)
	if c.Call("unlock", map[string]string{"mode": "valid"}, nil) == nil {
		t.Fatal("missing UI accepted prompt")
	}
}

func TestLockKillsPendingPromptAndRefusesLateReply(t *testing.T) {
	c := promptChild(t)
	ready := make(chan []byte, 1)
	done := make(chan error, 1)
	c.OnPrompt = func(raw json.RawMessage) error {
		p, e := uiprompt.DecodePrompt(raw)
		if e != nil {
			return e
		}
		reply, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"PUBLIC-password"}})
		ready <- reply
		return nil
	}
	go func() { done <- c.Call("unlock", map[string]string{"mode": "valid"}, nil) }()
	var reply []byte
	select {
	case reply = <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt not delivered")
	}
	c.Kill()
	if c.ReplyPrompt(reply) == nil {
		t.Fatal("late prompt answer accepted after lock")
	}
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("lock released successful operation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lock left serialized Call blocked")
	}
}

type failedPromptWriter struct{}

func (failedPromptWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPromptDisplayAndReplyPipeFailuresKillChildWithoutErrorDisclosure(t *testing.T) {
	for _, mode := range []string{"display", "pipe"} {
		t.Run(mode, func(t *testing.T) {
			c := promptChild(t)
			c.OnPrompt = func(raw json.RawMessage) error {
				if mode == "display" {
					return errors.New("PUBLIC-sensitive-error-canary")
				}
				p, e := uiprompt.DecodePrompt(raw)
				if e != nil {
					return e
				}
				reply, _ := json.Marshal(uiprompt.Reply{PromptID: p.ID})
				c.channel.W = failedPromptWriter{}
				if e = c.ReplyPrompt(reply); e == nil {
					t.Fatal("broken prompt reply pipe accepted")
				}
				return e
			}
			if e := c.Call("unlock", map[string]string{"mode": "valid"}, nil); e == nil || strings.Contains(e.Error(), "PUBLIC") {
				t.Fatal("prompt transport failure not sanitized")
			}
			if c.cmd != nil || c.prompt != nil {
				t.Fatal("failed prompt child still available")
			}
		})
	}
}
