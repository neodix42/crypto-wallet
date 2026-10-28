package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test executable is a controlled hostile protocol peer only. The shipped
// executable has no such dispatch, environment switch or renderer capability.
// Tests still exercise real anonymous pipes, self-executable identity, process
// groups, cancellation and synchronous child reaping in runPromptProcess.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--internal-prompt" {
		os.Exit(promptProcessPeer())
	}
	os.Exit(m.Run())
}

func promptProcessPeer() int {
	read, write, image := os.NewFile(3, "test-input"), os.NewFile(4, "test-output"), os.NewFile(5, "test-image")
	if checkPrivatePipes(read, write, image) != nil {
		return 70
	}
	channel := &ipc.Channel{R: read, W: write}
	m, err := channel.Read()
	if err != nil {
		return 71
	}
	p, err := uiprompt.DecodePrompt(m.Body)
	clear(m.Body)
	if err != nil {
		return 72
	}
	response := ipc.Message{Version: 1, ID: p.ID, Session: m.Session, Method: "reply"}
	mode := p.Title
	if mode == "wait-for-cancel" {
		_, _ = channel.Read()
		return 0
	}
	if mode == "activity-wait-for-cancel" {
		_ = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: m.Session, Method: "activity", Body: []byte(`{}`)})
		_, _ = channel.Read()
		return 0
	}
	if mode == "eof" {
		return 0
	}
	if mode == "foreign-id" {
		response.ID = strings.Repeat("f", 32)
	}
	if mode == "foreign-session" {
		response.Session = strings.Repeat("f", 32)
	}
	if mode == "private-error" {
		response.Error = "SYNTHETIC-PRIVATE-ERROR"
	}
	if mode == "wrong-method" {
		response.Method = "unapproved"
	}
	if mode == "activity" {
		for range 3 {
			_ = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: m.Session, Method: "activity", Body: []byte(`{}`)})
		}
	}
	if mode == "delayed-reply" {
		time.Sleep(100 * time.Millisecond)
	}
	if strings.HasPrefix(mode, "copy") {
		request := []byte(`{"sequence":1}`)
		if mode == "copy-reader-closed" {
			// A real peer can lose its return pipe after issuing an intent.
			// The parent must report uncertainty rather than invent a reply.
			read.Close()
			_ = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: m.Session, Method: "copy", Body: request})
			time.Sleep(5 * time.Second)
			return 0
		}
		switch mode {
		case "copy-invalid":
			request = []byte(`{"sequence":1,"words":"SYNTHETIC-NOT-A-PHRASE"}`)
		case "copy-replay":
			request = []byte(`{"sequence":2}`)
		}
		_ = channel.Write(ipc.Message{Version: 1, ID: p.ID, Session: m.Session, Method: "copy", Body: request})
		if mode == "copy-valid" {
			ack, err := channel.Read()
			if err != nil || ack.Method != "copyResult" || string(ack.Body) != `{"sequence":1,"copied":true}` {
				return 73
			}
			clear(ack.Body)
		} else {
			_, _ = channel.Read()
			return 0
		}
	}
	reply := uiprompt.Reply{PromptID: p.ID, Accepted: true}
	if p.Kind == "input" {
		reply.Values = []string{strconv.Itoa(os.Getpid())}
	}
	response.Body, _ = json.Marshal(reply)
	if mode == "bad-reply" {
		response.Body = []byte(`{"promptId":"` + p.ID + `","accepted":false,"values":["SYNTHETIC-PRIVATE"]}`)
	}
	if channel.Write(response) != nil {
		return 74
	}
	clear(response.Body)
	// A valid reply does not entitle the peer to outlive its private request.
	_, _ = channel.Read()
	return 0
}

func TestPrivateProcessBindsFramesAndRetiresTheActualSelfRole(t *testing.T) {
	for _, mode := range []string{"valid", "activity", "foreign-id", "foreign-session", "private-error", "wrong-method", "bad-reply", "eof"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			prompt := promptFixture()
			prompt.Title = mode
			activity := make(chan struct{}, 1)
			reply, err := runPromptProcess(ctx, prompt, activity, nil)
			if mode == "valid" || mode == "activity" {
				if err != nil || !reply.Accepted || reply.PromptID != prompt.ID || len(reply.Values) != 1 {
					t.Fatal("exact live private reply rejected")
				}
				pid, e := strconv.Atoi(reply.Values[0])
				if e != nil || pid <= 1 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					t.Fatal("private peer was not synchronously reaped")
				}
				if mode == "activity" && len(activity) != 1 {
					t.Fatal("value-free activity was not bounded")
				}
			} else if err == nil || reply.Accepted || len(reply.Values) != 0 {
				t.Fatal("unbound/ambiguous/closed private peer returned approval")
			}
		})
	}
}

func TestPrivateProcessAllowsOnlyExactCopyIntentForCurrentCopyablePrompt(t *testing.T) {
	for _, mode := range []string{"copy-valid", "copy-invalid", "copy-replay", "copy-wrong-kind", "copy-no-authority"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			prompt := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "recoveryBackup", Title: mode,
				Phrase: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"}
			if mode == "copy-wrong-kind" {
				prompt = promptFixture()
				prompt.Title = mode
			}
			copies := 0
			authorize := func() bool { copies++; return true }
			if mode == "copy-no-authority" {
				authorize = nil
			}
			reply, err := runPromptProcess(ctx, prompt, make(chan struct{}, 1), authorize)
			if mode == "copy-valid" {
				if err != nil || !reply.Accepted || copies != 1 {
					t.Fatal("exact authorized copy/reply did not complete once")
				}
			} else if err == nil || reply.Accepted || copies != 0 {
				t.Fatal("unbound private copy authorized a clipboard commit")
			}
		})
	}
}

func TestPrivateProcessCancellationTerminatesAndReapsBeforeReturning(t *testing.T) {
	for _, alreadyCancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(alreadyCancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			prompt := promptFixture()
			prompt.Title = "wait-for-cancel"
			if alreadyCancelled {
				cancel()
			} else {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			started := time.Now()
			reply, err := runPromptProcess(ctx, prompt, make(chan struct{}, 1), nil)
			if !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted || time.Since(started) > time.Second {
				t.Fatal("cancelled private role did not retire within the deadline")
			}
		})
	}
}

func TestWindowPromptKeepsMissingDesktopExportsDisabledAndRejectsConcurrentRequests(t *testing.T) {
	w, _ := New(nil)
	defer w.Close()
	if _, err := w.Prompt(uiprompt.Prompt{}); !errors.Is(err, uiprompt.ErrProtocol) {
		t.Fatal("invalid prompt reached a desktop capability")
	}
	for _, kind := range []string{"copy", "file"} {
		p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: kind, Title: "Public export"}
		if kind == "copy" {
			p.Address = "0x1111111111111111111111111111111111111111"
		} else {
			save := false
			p.Save = &save
		}
		r, err := w.Prompt(p)
		if !errors.Is(err, errDesktop) || r.Accepted || r.Path != "" {
			t.Fatal("missing desktop authorized an export")
		}
	}
	w.mu.Lock()
	w.modal = true
	w.mu.Unlock()
	if _, err := w.Prompt(promptFixture()); !errors.Is(err, uiprompt.ErrCancelled) {
		t.Fatal("a second prompt crossed the active modal boundary")
	}
	w.mu.Lock()
	w.modal = false
	w.mu.Unlock()
}

func TestWindowPromptRechecksEpochAndHandlesPriorityCancellation(t *testing.T) {
	for _, mode := range []string{"valid", "activity", "stale", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			activity := 0
			w, _ := New(func(e Event) {
				if e.ID != "activity" || e.Value != "" {
					t.Error("private activity emitted values or unrelated authority")
				}
				activity++
			})
			defer w.Close()
			p := promptFixture()
			p.Title = mode
			if mode == "stale" {
				p.Title = "delayed-reply"
				w.DispatchControl(func() { w.mu.Lock(); w.cancelEpoch++; w.mu.Unlock() })
			}
			if mode == "cancel" {
				p.Title = "wait-for-cancel"
				w.DispatchControl(w.CancelPrompt)
			}
			reply, err := w.Prompt(p)
			if mode == "stale" || mode == "cancel" {
				if !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted || len(reply.Values) != 0 {
					t.Fatal("retired prompt returned a late accepted credential")
				}
			} else if err != nil || !reply.Accepted {
				t.Fatal("current private prompt failed to complete")
			}
			if mode == "activity" && activity != 1 {
				t.Fatal("valid private activity did not reach the lifecycle controller")
			}
			w.mu.Lock()
			retired := !w.modal && w.activeCancel == nil && w.activeDone == nil
			w.mu.Unlock()
			if !retired {
				t.Fatal("retired prompt retained cancellation or child lifecycle state")
			}
		})
	}
}
