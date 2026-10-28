package webui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/strictjson"
	"localwallet/internal/uiprompt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type promptResult struct {
	reply uiprompt.Reply
	err   error
}

func checkedPrompt(p uiprompt.Prompt) (uiprompt.Prompt, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return uiprompt.Prompt{}, uiprompt.ErrProtocol
	}
	defer clear(raw)
	p, err = uiprompt.DecodePrompt(raw)
	if err == nil && p.Kind == "receive" {
		config, e := png.DecodeConfig(bytes.NewReader(p.PNG))
		if e != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
			return uiprompt.Prompt{}, uiprompt.ErrProtocol
		}
	}
	return p, err
}

func (w *Window) Prompt(p uiprompt.Prompt) (uiprompt.Reply, error) {
	p, err := checkedPrompt(p)
	if err != nil {
		return uiprompt.Reply{}, err
	}
	w.mu.Lock()
	if w.closed || w.modal {
		w.mu.Unlock()
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
	epoch := w.cancelEpoch
	w.modal = true
	ctx, cancel := context.WithCancel(context.Background())
	w.activeCancel = cancel
	privateProcess := p.Kind != "copy" && p.Kind != "file"
	var reaped chan struct{}
	if privateProcess {
		reaped = make(chan struct{})
		w.activeDone = reaped
	}
	uiCtx := w.ctx
	w.mu.Unlock()
	defer func() {
		cancel()
		w.mu.Lock()
		w.modal = false
		w.activeCancel = nil
		w.activeDone = nil
		w.mu.Unlock()
		p.Phrase = ""
		p.Summary = ""
		clear(p.PNG)
	}()
	result := make(chan promptResult, 1)
	activity := make(chan struct{}, 1)
	if !privateProcess {
		go func() {
			r := uiprompt.Reply{PromptID: p.ID}
			var e error
			if uiCtx == nil {
				e = errDesktop
			} else if p.Kind == "copy" {
				e = wruntime.ClipboardSetTextAuthorized(uiCtx, p.Address, func(commit func()) bool {
					w.mu.Lock()
					defer w.mu.Unlock()
					if w.closed || w.cancelEpoch != epoch {
						return false
					}
					commit()
					return true
				})
				r.Accepted = e == nil
			} else {
				if *p.Save {
					r.Path, e = wruntime.SaveFileDialog(uiCtx, wruntime.SaveDialogOptions{Title: p.Title, DefaultFilename: "wallet-address.png"})
				} else {
					r.Path, e = wruntime.OpenFileDialog(uiCtx, wruntime.OpenDialogOptions{Title: p.Title})
				}
				r.Accepted = e == nil && r.Path != ""
			}
			result <- promptResult{r, e}
		}()
	} else {
		go func() {
			defer close(reaped)
			r, e := runPromptProcess(ctx, p, activity, func() bool {
				var value string
				switch p.Kind {
				case "recoveryBackup":
					value = p.Phrase
				case "receive":
					value = p.Address
				default:
					return false
				}
				if uiCtx == nil {
					return false
				}
				return wruntime.ClipboardSetTextAuthorized(uiCtx, value, func(commit func()) bool {
					w.mu.Lock()
					defer w.mu.Unlock()
					if w.closed || w.cancelEpoch != epoch || ctx.Err() != nil {
						return false
					}
					commit()
					return true
				}) == nil
			})
			result <- promptResult{r, e}
		}()
	}
	for {
		select {
		case f := <-w.controls:
			f()
		case <-activity:
			if w.onEvent != nil {
				w.onEvent(Event{ID: "activity"})
			}
		case <-ctx.Done():
			if privateProcess {
				r := <-result
				for i := range r.reply.Values {
					r.reply.Values[i] = ""
				}
				r.reply.Path = ""
			} // Reap before clearing captured private data.
			return uiprompt.Reply{PromptID: p.ID}, uiprompt.ErrCancelled
		case r := <-result:
			w.mu.Lock()
			stale := w.closed || w.cancelEpoch != epoch
			w.mu.Unlock()
			if stale {
				for i := range r.reply.Values {
					r.reply.Values[i] = ""
				}
				return uiprompt.Reply{PromptID: p.ID}, uiprompt.ErrCancelled
			}
			return r.reply, r.err
		}
	}
}

func promptEnvironment() []string {
	env := []string{"PATH=/usr/bin:/bin", "GOTRACEBACK=none"}
	for _, key := range []string{"HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY", "XDG_CONFIG_HOME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG", "LC_ALL", "GDK_BACKEND"} {
		if value, ok := os.LookupEnv(key); ok {
			if key == "GDK_BACKEND" && value != "x11" && value != "wayland" {
				continue
			}
			env = append(env, key+"="+value)
		}
	}
	return env
}

// Only the current executable can host a private prompt. The words/passwords
// are framed over anonymous inherited pipes, never argv, environment, files or
// the main window's Wails event/binding channel. A random session and prompt ID
// bind the sole reply; cancellation kills and reaps the child and its engine.
func runPromptProcess(ctx context.Context, p uiprompt.Prompt, activity chan<- struct{}, copyCurrent func() bool) (uiprompt.Reply, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	defer inputRead.Close()
	defer inputWrite.Close()
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	defer outputRead.Close()
	defer outputWrite.Close()
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	session := hex.EncodeToString(nonce[:])
	clear(nonce[:])
	image, err := os.Open("/proc/self/exe")
	if err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	defer image.Close()
	command := exec.CommandContext(ctx, "/proc/self/exe", "--internal-prompt")
	command.ExtraFiles = []*os.File{inputRead, outputWrite, image}
	command.Env = promptEnvironment()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
	command.Cancel = func() error {
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			return command.Process.Kill()
		}
		return nil
	}
	if ctx.Err() != nil {
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
	if err = command.Start(); err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	defer func() {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	inputRead.Close()
	outputWrite.Close()
	channel := &ipc.Channel{R: outputRead, W: inputWrite}
	body, err := json.Marshal(p)
	if err != nil {
		return uiprompt.Reply{}, uiprompt.ErrProtocol
	}
	defer clear(body)
	if err = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: session, Method: "prompt", Body: body}); err != nil {
		return uiprompt.Reply{}, errDesktop
	}
	responses := make(chan promptResult, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		lastActivity := time.Time{}
		var copySequence uint32
		for {
			m, e := channel.Read()
			if e != nil {
				responses <- promptResult{err: uiprompt.ErrCancelled}
				return
			}
			if m.ID != p.ID || m.Session != session || m.Error != "" {
				clear(m.Body)
				responses <- promptResult{err: uiprompt.ErrProtocol}
				return
			}
			if m.Method == "activity" && bytes.Equal(m.Body, []byte(`{}`)) {
				clear(m.Body)
				if time.Since(lastActivity) >= time.Second {
					select {
					case activity <- struct{}{}:
					default:
					}
					lastActivity = time.Now()
				}
				continue
			}
			if m.Method == "copy" {
				var request struct {
					Sequence uint32 `json:"sequence"`
				}
				e := strictjson.Decode(m.Body, &request)
				clear(m.Body)
				if e != nil || request.Sequence != copySequence+1 || request.Sequence > 10000 || (p.Kind != "recoveryBackup" && p.Kind != "receive") || copyCurrent == nil {
					responses <- promptResult{err: uiprompt.ErrProtocol}
					return
				}
				copySequence = request.Sequence
				copied := ctx.Err() == nil && copyCurrent()
				body, _ := json.Marshal(struct {
					Sequence uint32 `json:"sequence"`
					Copied   bool   `json:"copied"`
				}{copySequence, copied})
				e = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: session, Method: "copyResult", Body: body})
				clear(body)
				if e != nil {
					responses <- promptResult{err: uiprompt.ErrCancelled}
					return
				}
				continue
			}
			if m.Method != "reply" {
				clear(m.Body)
				responses <- promptResult{err: uiprompt.ErrProtocol}
				return
			}
			r, e := decodePrivate(m.Body, p)
			clear(m.Body)
			responses <- promptResult{r, e}
			return
		}
	}()
	select {
	case r := <-responses:
		return r.reply, r.err
	case <-ctx.Done():
		_ = outputRead.Close()
		_ = inputWrite.Close()
		<-readerDone
		select {
		case stale := <-responses:
			for i := range stale.reply.Values {
				stale.reply.Values[i] = ""
			}
			stale.reply.Path = ""
		default:
		}
		return uiprompt.Reply{}, uiprompt.ErrCancelled
	}
}

// PrivateBridge is bound only in the private self-role. The public Wails
// process has no instance of this type and no route to its inherited pipes.
type PrivateBridge struct {
	mu                  sync.Mutex
	prompt              uiprompt.Prompt
	ctx                 context.Context
	channel             *ipc.Channel
	session             string
	delivered, finished bool
	lastActivity        time.Time
	copySequence        uint32
	copyAcknowledged    uint32
	copyPending         chan bool
	done                chan struct{}
}

func (b *PrivateBridge) Prompt() (uiprompt.Prompt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished || b.delivered {
		return uiprompt.Prompt{}, uiprompt.ErrCancelled
	}
	b.delivered = true
	p := b.prompt
	p.Labels = append([]string(nil), p.Labels...)
	p.PNG = append([]byte(nil), p.PNG...)
	return p, nil
}
func (b *PrivateBridge) Reply(reply uiprompt.Reply) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished || !b.delivered {
		return false
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return false
	}
	defer clear(raw)
	if _, err = uiprompt.DecodeReply(raw, b.prompt); err != nil {
		return false
	}
	b.finished = true
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
	err = b.channel.Write(ipc.Message{Version: 1, ID: b.prompt.ID, Session: b.session, Method: "reply", Body: raw})
	b.clear()
	if b.ctx != nil {
		wruntime.Quit(b.ctx)
	}
	return err == nil
}

// Copy sends only an argument-free intent to the owning parent. The parent
// holds the Go-authored text and commits under its current cancellation epoch.
// It owns CLIPBOARD, so closing this temporary view does not discard the copy.
func (b *PrivateBridge) Copy() bool {
	b.mu.Lock()
	if b.finished || !b.delivered || b.ctx == nil || b.copyPending != nil || b.copySequence >= 10000 {
		b.mu.Unlock()
		return false
	}
	if b.prompt.Kind != "recoveryBackup" && b.prompt.Kind != "receive" {
		b.mu.Unlock()
		return false
	}
	if b.done == nil {
		b.done = make(chan struct{})
	}
	b.copySequence++
	sequence := b.copySequence
	result := make(chan bool, 1)
	b.copyPending = result
	done := b.done
	id, session := b.prompt.ID, b.session
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.copyPending == result {
			b.copyPending = nil
		}
		b.mu.Unlock()
	}()
	body, _ := json.Marshal(struct {
		Sequence uint32 `json:"sequence"`
	}{sequence})
	err := b.channel.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "copy", Body: body})
	clear(body)
	if err != nil {
		return false
	}
	select {
	case copied := <-result:
		return copied
	case <-done:
		return false
	}
}
func (b *PrivateBridge) parentMessage(m ipc.Message) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished || !b.delivered || b.copyPending == nil || m.ID != b.prompt.ID || m.Session != b.session || m.Method != "copyResult" || m.Error != "" {
		return false
	}
	var ack struct {
		Sequence uint32 `json:"sequence"`
		Copied   *bool  `json:"copied"`
	}
	if strictjson.Decode(m.Body, &ack) != nil || ack.Sequence != b.copySequence || ack.Sequence <= b.copyAcknowledged || ack.Copied == nil {
		return false
	}
	select {
	case b.copyPending <- *ack.Copied:
		b.copyAcknowledged = ack.Sequence
		return true
	default:
		return false
	}
}
func (b *PrivateBridge) revokeLocked() {
	b.finished = true
	b.clear()
	if b.done != nil {
		close(b.done)
		b.done = nil
	}
}
func (b *PrivateBridge) Activity() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finished || !b.delivered || time.Since(b.lastActivity) < time.Second {
		return false
	}
	b.lastActivity = time.Now()
	return b.channel.Write(ipc.Message{Version: 1, ID: b.prompt.ID, Session: b.session, Method: "activity", Body: []byte(`{}`)}) == nil
}
func (b *PrivateBridge) clear() {
	b.prompt.Phrase = ""
	b.prompt.Summary = ""
	b.prompt.Labels = nil
	clear(b.prompt.PNG)
	b.prompt.PNG = nil
}

func checkPrivatePipes(read, write, image *os.File) error {
	if os.Getppid() <= 1 {
		return errDesktop
	}
	self, err := os.Stat("/proc/self/exe")
	if err != nil {
		return errDesktop
	}
	// PR_SET_DUMPABLE=0 deliberately prevents opening the parent's /proc/exe
	// link, including from the same UID. Compare the inherited executable
	// handle instead; this is an identity guard, not a same-user malware claim.
	origin, err := image.Stat()
	if err != nil || !origin.Mode().IsRegular() || !os.SameFile(self, origin) {
		return errDesktop
	}
	for _, f := range []*os.File{read, write} {
		var st syscall.Stat_t
		if syscall.Fstat(int(f.Fd()), &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO || int(st.Uid) != os.Getuid() {
			return errDesktop
		}
	}
	return nil
}
func RunPrivate() error {
	read, write, image := os.NewFile(3, "wallet-private-input"), os.NewFile(4, "wallet-private-output"), os.NewFile(5, "wallet-private-image")
	if read == nil || write == nil || image == nil {
		return errDesktop
	}
	defer read.Close()
	defer write.Close()
	defer image.Close()
	if checkPrivatePipes(read, write, image) != nil {
		return errDesktop
	}
	channel := &ipc.Channel{R: read, W: write}
	m, err := channel.Read()
	if err != nil || m.Method != "prompt" || m.Error != "" || !uiprompt.ValidID(m.Session) {
		return errDesktop
	}
	p, err := uiprompt.DecodePrompt(m.Body)
	clear(m.Body)
	if err != nil || m.ID != p.ID || p.Kind == "file" || p.Kind == "copy" {
		return errDesktop
	}
	if p, err = checkedPrompt(p); err != nil {
		return errDesktop
	}
	b := &PrivateBridge{prompt: p, channel: channel, session: m.Session}
	defer b.clear()
	prepareWebKitRuntime()
	configureWebKitSignal()
	a := appOptions(true)
	a.Title = p.Title
	a.Width = 680
	a.Height = 440
	a.MinWidth = 560
	a.MinHeight = 260
	if p.Kind == "input" {
		a.Height = 205 + len(p.Labels)*82
		if a.Height > 850 {
			a.Height = 850
		}
	}
	if p.Kind == "recovery" || p.Kind == "recoveryBackup" || p.Kind == "receive" {
		a.Height = 660
	}
	a.Bind = []interface{}{b}
	a.OnStartup = func(ctx context.Context) {
		b.mu.Lock()
		b.ctx = ctx
		b.mu.Unlock()
		go func() {
			// Only a matching clipboard acknowledgment may follow the prompt.
			for {
				m, e := channel.Read()
				if e != nil {
					break
				}
				valid := b.parentMessage(m)
				clear(m.Body)
				if !valid {
					break
				}
			}
			b.mu.Lock()
			b.revokeLocked()
			b.mu.Unlock()
			wruntime.Quit(ctx)
		}()
	}
	a.OnBeforeClose = func(context.Context) bool {
		b.mu.Lock()
		if !b.finished {
			raw, _ := json.Marshal(uiprompt.Reply{PromptID: b.prompt.ID})
			_ = b.channel.Write(ipc.Message{Version: 1, ID: b.prompt.ID, Session: b.session, Method: "reply", Body: raw})
			clear(raw)
			b.revokeLocked()
		}
		b.mu.Unlock()
		return false
	}
	a.OnShutdown = func(context.Context) { b.mu.Lock(); b.revokeLocked(); b.mu.Unlock() }
	if wails.Run(a) != nil {
		return errors.New("private wallet desktop unavailable")
	}
	return nil
}

// Decode private replies here as well as in the signer. Keeping an explicit
// strict decoder available prevents future adapter changes from accepting a
// second untyped reply format or allowing duplicate fields.
func decodePrivate(raw []byte, p uiprompt.Prompt) (uiprompt.Reply, error) {
	var fields map[string]json.RawMessage
	if strictjson.Decode(raw, &fields) != nil {
		return uiprompt.Reply{}, uiprompt.ErrProtocol
	}
	return uiprompt.DecodeReply(raw, p)
}
