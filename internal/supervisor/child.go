// Package supervisor starts only manifest-checked executables over private pipes.
package supervisor

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"localwallet/internal/ipc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type Child struct {
	mu                      sync.Mutex
	life                    sync.Mutex
	cmd                     *exec.Cmd
	owner                   *processOwner
	channel                 *ipc.Channel
	session                 string
	counter                 uint64
	generation              uint64
	Path, Data, Environment string
	// OnActivity receives native key/button activity during an active request.
	// Set it before the first Call. It receives no credential or event values.
	OnActivity func()
}

type processOwner struct {
	stop chan struct{}
	done chan struct{}
}

// Linux Pdeathsig belongs to the creating OS thread. Keep that thread alive
// until the child has been killed and reaped, even if the calling Go worker
// migrates or ends. A short LockOSThread around Start would not provide this.
func startOwned(cmd *exec.Cmd) (*processOwner, error) {
	owner := &processOwner{stop: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(owner.done)
		defer runtime.UnlockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		<-owner.stop
		_ = cmd.Wait()
	}()
	if err := <-started; err != nil {
		<-owner.done
		return nil, err
	}
	return owner, nil
}

func (owner *processOwner) kill(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
	close(owner.stop)
	<-owner.done
}

func Verify(path string) error {
	b, e := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json"))
	if e != nil {
		return e
	}
	var manifest map[string]string
	if json.Unmarshal(b, &manifest) != nil {
		return errors.New("invalid manifest")
	}
	want := manifest[filepath.Base(path)]
	if len(want) != 64 {
		return errors.New("missing executable identity")
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return errors.New("executable identity mismatch")
	}
	return nil
}
func (c *Child) start() error {
	if e := Verify(c.Path); e != nil {
		return e
	}
	cmd := exec.Command(c.Path, "--data", c.Data, "--environment", c.Environment)
	for _, k := range []string{"HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG", "LC_ALL", "GDK_BACKEND"} {
		if v, ok := os.LookupEnv(k); ok {
			if k == "GDK_BACKEND" && v != "x11" && v != "wayland" {
				continue
			}
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Env = append(cmd.Env, "PATH=/usr/bin:/bin", "GOTRACEBACK=none")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
	r, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	w, e := cmd.StdinPipe()
	if e != nil {
		r.Close()
		return e
	}
	cmd.Stderr = io.Discard
	owner, e := startOwned(cmd)
	if e != nil {
		r.Close()
		w.Close()
		return e
	}
	var b [16]byte
	if _, e = rand.Read(b[:]); e != nil {
		owner.kill(cmd)
		return e
	}
	c.cmd = cmd
	c.owner = owner
	c.channel = &ipc.Channel{R: r, W: w}
	c.session = hex.EncodeToString(b[:])
	return nil
}
func (c *Child) Call(method string, body any, out any) error {
	return c.call(nil, method, body, out)
}

// Generation identifies the current child lifecycle, including an as-yet
// unstarted child. A killed generation must never open another native prompt.
func (c *Child) Generation() uint64 {
	c.life.Lock()
	defer c.life.Unlock()
	return c.generation
}

// CallAt refuses work accepted before a lock, even if the call was waiting for
// another request and would otherwise start a fresh child after that lock.
func (c *Child) CallAt(generation uint64, method string, body any, out any) error {
	return c.call(&generation, method, body, out)
}

func (c *Child) call(expected *uint64, method string, body any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.life.Lock()
	if expected != nil && *expected != c.generation {
		c.life.Unlock()
		return errors.New("secure child session ended")
	}
	if c.cmd == nil {
		if e := c.start(); e != nil {
			c.life.Unlock()
			return e
		}
	}
	ch := c.channel
	session := c.session
	c.counter++
	id := hex.EncodeToString([]byte{byte(c.counter >> 56), byte(c.counter >> 48), byte(c.counter >> 40), byte(c.counter >> 32), byte(c.counter >> 24), byte(c.counter >> 16), byte(c.counter >> 8), byte(c.counter)})
	generation := c.generation
	c.life.Unlock()
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	if e = ch.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: method, Body: b}); e != nil {
		c.Kill()
		return errors.New("secure child unavailable")
	}
	var r ipc.Message
	var activityWindow, lastActivity time.Time
	activityCount := 0
	for {
		r, e = ch.Read()
		c.life.Lock()
		changed := c.generation != generation
		c.life.Unlock()
		if e != nil || changed || r.ID != id || r.Session != session {
			c.Kill()
			return errors.New("secure child session ended")
		}
		if r.Method != "activity" {
			if r.Method != method {
				c.Kill()
				return errors.New("secure child session ended")
			}
			break
		}
		// Activity has one exact, value-free representation. Bound a compromised
		// child's event flood as well as the callbacks into the coordinator.
		now := time.Now()
		if activityWindow.IsZero() || now.Sub(activityWindow) >= time.Second {
			activityWindow, activityCount = now, 0
		}
		activityCount++
		if r.Error != "" || !bytes.Equal(r.Body, []byte("{}")) || activityCount > 64 {
			c.Kill()
			return errors.New("invalid secure child activity")
		}
		if c.OnActivity != nil && (lastActivity.IsZero() || now.Sub(lastActivity) >= time.Second) {
			lastActivity = now
			c.OnActivity()
		}
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	if out != nil {
		return json.Unmarshal(r.Body, out)
	}
	return nil
}
func (c *Child) Kill() {
	c.life.Lock()
	defer c.life.Unlock()
	c.generation++
	if c.cmd != nil {
		cmd := c.cmd
		c.owner.kill(cmd)
		c.cmd = nil
		c.owner = nil
		c.channel = nil
		c.session = ""
	}
}
