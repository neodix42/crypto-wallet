// Package supervisor starts only manifest-checked executables over private pipes.
package supervisor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
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
	// SelfRole is a closed internal entry point in the current executable. It
	// never authorizes an arbitrary executable, argument, or manifest bypass.
	// The legacy sibling-executable path leaves this empty and remains checked
	// against its release manifest.
	SelfRole string
	// OnActivity receives native key/button activity during an active request.
	// Set it before the first Call. It receives no credential or event values.
	OnActivity func()
	// OnPrompt relays a validated, signer-generated transient prompt. It must
	// return promptly; the out-of-band ReplyPrompt method supplies the answer.
	OnPrompt                     func(json.RawMessage) error
	prompt                       *uiprompt.Prompt
	promptRequest, promptSession string
	promptGeneration             uint64
	seenPrompts                  map[string]bool
	activityIDs                  map[string]bool
	activitySent                 time.Time
	historyID, historySession    string
	historyGeneration            uint64
	historyCancelled             bool
	historyReady                 bool
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
func (c *Child) command() (*exec.Cmd, error) {
	if c.SelfRole == "" {
		if err := Verify(c.Path); err != nil {
			return nil, err
		}
		return exec.Command(c.Path, "--data", c.Data, "--environment", c.Environment), nil
	}
	if c.SelfRole != "--internal-signer" && c.SelfRole != "--internal-core" {
		return nil, errors.New("unsupported internal wallet role")
	}
	// Execute the already running image through procfs, rather than resolving a
	// mutable pathname after checking it. This also works when an application
	// upgrade replaces the downloaded file while the old process remains open.
	const self = "/proc/self/exe"
	current, err := os.Stat(self)
	if err != nil || !current.Mode().IsRegular() {
		return nil, errors.New("current executable unavailable")
	}
	if c.Path != "" {
		requested, err := os.Stat(c.Path)
		if err != nil || !os.SameFile(current, requested) {
			return nil, errors.New("internal wallet must use current executable")
		}
	}
	if c.SelfRole == "--internal-core" {
		return exec.Command(self, c.SelfRole, "--environment", c.Environment), nil
	}
	return exec.Command(self, c.SelfRole, "--data", c.Data, "--environment", c.Environment), nil
}

func (c *Child) start() error {
	cmd, e := c.command()
	if e != nil {
		return e
	}
	for _, k := range []string{"HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG", "LC_ALL", "GDK_BACKEND"} {
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
	c.seenPrompts = map[string]bool{}
	c.activityIDs = map[string]bool{}
	c.activitySent = time.Time{}
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

// CallHistoryAt can be preempted without ending the unlocked signer session.
// Its cancellation capability is scoped to this original read-only request.
func (c *Child) CallHistoryAt(ctx context.Context, generation uint64, body, out any) error {
	if ctx == nil {
		return errors.New("wallet history requires a context")
	}
	if c.SelfRole != "--internal-core" {
		return errors.New("wallet history cancellation requires the core role")
	}
	return c.callContext(&generation, "chainHistory", body, out, ctx)
}

func (c *Child) call(expected *uint64, method string, body any, out any) error {
	return c.callContext(expected, method, body, out, nil)
}

func (c *Child) callContext(expected *uint64, method string, body any, out any, historyContext context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if historyContext != nil && historyContext.Err() != nil {
		return historyContext.Err()
	}
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
	if method == "chainHistory" && c.SelfRole == "--internal-core" {
		c.historyID, c.historySession, c.historyGeneration, c.historyCancelled = id, session, generation, false
		c.historyReady = false
	}
	c.life.Unlock()
	historyDone := make(chan struct{})
	defer func() {
		c.life.Lock()
		if c.historyID == id && c.historySession == session && c.historyGeneration == generation {
			c.historyID, c.historySession = "", ""
			c.historyCancelled = false
			c.historyReady = false
		}
		c.life.Unlock()
		close(historyDone)
	}()
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	if e = ch.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: method, Body: b}); e != nil {
		c.Kill()
		return errors.New("secure child unavailable")
	}
	if historyContext != nil {
		c.life.Lock()
		if c.historyID == id && c.historySession == session && c.historyGeneration == generation {
			c.historyReady = true
		}
		c.life.Unlock()
		go func() {
			select {
			case <-historyDone:
			case <-historyContext.Done():
				_ = c.cancelHistoryAt(generation, id, session)
			}
		}()
	} else if method == "chainHistory" {
		c.life.Lock()
		if c.historyID == id && c.historySession == session && c.historyGeneration == generation {
			c.historyReady = true
		}
		c.life.Unlock()
	}
	var r ipc.Message
	var activityWindow, lastActivity time.Time
	activityCount := 0
	promptIDs := map[string]bool{}
	for {
		r, e = ch.Read()
		c.life.Lock()
		changed := c.generation != generation
		activityReply := !changed && c.activityIDs[r.ID]
		if activityReply {
			delete(c.activityIDs, r.ID)
		}
		c.life.Unlock()
		if e == nil && !changed && activityReply {
			if r.Session != session || r.Method != "activity" || r.Error != "" || !bytes.Equal(r.Body, []byte("null")) {
				c.Kill()
				return errors.New("invalid wallet activity reply")
			}
			continue
		}
		if e != nil || changed || r.ID != id || r.Session != session {
			c.Kill()
			return errors.New("secure child session ended")
		}
		if r.Method == "prompt" {
			p, promptError := uiprompt.DecodePrompt(r.Body)
			if promptError != nil || r.Error != "" || promptIDs[p.ID] || len(promptIDs) >= 64 || c.OnPrompt == nil {
				c.Kill()
				return errors.New("invalid secure child prompt")
			}
			promptIDs[p.ID] = true
			c.life.Lock()
			if c.generation != generation || c.prompt != nil || c.seenPrompts[p.ID] || len(c.seenPrompts) >= 100000 {
				c.life.Unlock()
				c.Kill()
				return errors.New("secure child session ended")
			}
			c.prompt, c.promptRequest, c.promptSession, c.promptGeneration = &p, id, session, generation
			c.seenPrompts[p.ID] = true
			c.life.Unlock()
			if err := c.OnPrompt(r.Body); err != nil {
				c.Kill()
				return errors.New("secure prompt unavailable")
			}
			clear(r.Body)
			continue
		}
		if r.Method != "activity" || method == "activity" {
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
	c.life.Lock()
	unanswered := c.prompt != nil
	c.life.Unlock()
	if unanswered {
		c.Kill()
		return errors.New("secure prompt was not answered")
	}
	if historyContext != nil && historyContext.Err() != nil {
		return historyContext.Err()
	}
	if r.Error != "" {
		return ipc.NewRequestError(r.Error)
	}
	if out != nil {
		return json.Unmarshal(r.Body, out)
	}
	return nil
}

func (c *Child) CancelHistory() error {
	c.life.Lock()
	generation, id, session := c.historyGeneration, c.historyID, c.historySession
	c.life.Unlock()
	return c.cancelHistoryAt(generation, id, session)
}

func (c *Child) cancelHistoryAt(generation uint64, id, session string) error {
	c.life.Lock()
	if c.SelfRole != "--internal-core" || c.cmd == nil || c.channel == nil || id == "" || c.generation != generation || c.historyID != id || c.historySession != session || c.historyGeneration != generation || c.historyCancelled || !c.historyReady {
		c.life.Unlock()
		return errors.New("no active wallet history")
	}
	c.historyCancelled = true
	channel := c.channel
	c.life.Unlock()
	if err := channel.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "cancelHistory", Body: []byte(`{}`)}); err != nil {
		c.life.Lock()
		if c.generation == generation {
			c.killLocked()
		}
		c.life.Unlock()
		return errors.New("wallet history cancellation unavailable")
	}
	return nil
}

// SendActivity forwards a value-free native key/button event during an active
// core prompt without waiting behind Call's operation lock. It cannot start or
// unlock a child. The exact acknowledgment is consumed by the active/next Call;
// neither the acknowledgment nor polling generates another activity event.
func (c *Child) SendActivity() error {
	c.life.Lock()
	if c.SelfRole != "--internal-core" || c.cmd == nil || c.channel == nil || c.prompt == nil || c.promptGeneration != c.generation {
		c.life.Unlock()
		return errors.New("no active wallet prompt")
	}
	now := time.Now()
	if !c.activitySent.IsZero() && now.Sub(c.activitySent) < time.Second {
		c.life.Unlock()
		return nil
	}
	if len(c.activityIDs) >= 8 {
		c.life.Unlock()
		return errors.New("wallet activity acknowledgment unavailable")
	}
	c.counter++
	id := hex.EncodeToString([]byte{byte(c.counter >> 56), byte(c.counter >> 48), byte(c.counter >> 40), byte(c.counter >> 32), byte(c.counter >> 24), byte(c.counter >> 16), byte(c.counter >> 8), byte(c.counter)})
	c.activityIDs[id] = true
	c.activitySent = now
	channel, session, generation := c.channel, c.session, c.generation
	c.life.Unlock()
	if err := channel.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "activity", Body: []byte("{}")}); err != nil {
		c.life.Lock()
		if c.generation == generation {
			c.killLocked()
		}
		c.life.Unlock()
		return errors.New("wallet activity unavailable")
	}
	return nil
}

// ReplyPrompt deliberately does not take mu: Call is waiting for this answer.
// Consume the capability before writing so replay and competing replies fail.
// Never hold life across a pipe write; Kill must remain able to cancel it.
func (c *Child) ReplyPrompt(raw json.RawMessage) error {
	c.life.Lock()
	if c.prompt == nil || c.promptGeneration != c.generation || c.channel == nil {
		c.life.Unlock()
		return errors.New("no active secure prompt")
	}
	if _, err := uiprompt.DecodeReply(raw, *c.prompt); err != nil {
		c.life.Unlock()
		return err
	}
	ch, id, session := c.channel, c.promptRequest, c.promptSession
	c.prompt = nil
	c.promptRequest, c.promptSession = "", ""
	c.life.Unlock()
	if err := ch.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "promptReply", Body: raw}); err != nil {
		c.Kill()
		return errors.New("secure prompt unavailable")
	}
	return nil
}
func (c *Child) Kill() {
	c.life.Lock()
	defer c.life.Unlock()
	c.killLocked()
}

func (c *Child) killLocked() {
	c.generation++
	c.prompt = nil
	c.promptRequest, c.promptSession = "", ""
	c.seenPrompts = nil
	c.activityIDs = nil
	c.activitySent = time.Time{}
	c.historyID, c.historySession = "", ""
	c.historyCancelled = false
	c.historyReady = false
	if c.cmd != nil {
		cmd := c.cmd
		c.owner.kill(cmd)
		c.cmd = nil
		c.owner = nil
		c.channel = nil
		c.session = ""
	}
}
