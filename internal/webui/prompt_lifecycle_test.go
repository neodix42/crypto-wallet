package webui

import (
	"context"
	"errors"
	"flag"
	"io"
	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This is the existing Wails clipboard capability, not a renderer/test hook.
// The controlled dependency records whether the real Go lifecycle guard grants
// its synchronous commit. No system/user clipboard is read or changed.
type guardedClipboardRecorder struct {
	before  func()
	commits int
	text    string
}

func (c *guardedClipboardRecorder) ClipboardSetTextAuthorized(_ context.Context, text string, authorize func(func()) bool) error {
	if c.before != nil {
		c.before()
	}
	if !authorize(func() { c.commits++; c.text = text }) {
		return errors.New("clipboard lifecycle was revoked")
	}
	return nil
}

func TestPublicCopyCommitsOnlyUnderItsCurrentWindowEpoch(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(strconv.FormatBool(stale), func(t *testing.T) {
			w, _ := New(nil)
			clipboard := &guardedClipboardRecorder{}
			if stale {
				clipboard.before = func() { w.mu.Lock(); w.cancelEpoch++; w.mu.Unlock() }
			}
			w.ctx = context.WithValue(context.Background(), "frontend", clipboard)
			p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "copy", Title: "Copy verified address", Address: "public-owned-address"}
			reply, err := w.Prompt(p)
			w.ctx = nil
			w.Close()
			if stale {
				if !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted || clipboard.commits != 0 || clipboard.text != "" {
					t.Fatal("late public address copy crossed its window epoch")
				}
			} else if err != nil || !reply.Accepted || clipboard.commits != 1 || clipboard.text != p.Address {
				t.Fatal("live public address copy did not commit exactly the Go-authored value")
			}
		})
	}
}

func TestPrivateReceiveCopyUsesOwnedAddressAndRejectsMissingOrRetiredDesktop(t *testing.T) {
	for _, mode := range []string{"live", "missing", "stale"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := New(nil)
			clipboard := &guardedClipboardRecorder{}
			if mode != "missing" {
				w.ctx = context.WithValue(context.Background(), "frontend", clipboard)
			}
			if mode == "stale" {
				clipboard.before = func() { w.mu.Lock(); w.cancelEpoch++; w.mu.Unlock() }
			}
			p := receiveFixture(t, 1)
			p.Title = "copy-valid"
			reply, err := w.Prompt(p)
			w.ctx = nil
			w.Close()
			if mode == "live" {
				if err != nil || !reply.Accepted || clipboard.commits != 1 || clipboard.text != p.Address {
					t.Fatal("private receive copy did not use the exact owned address")
				}
			} else if err == nil || reply.Accepted || clipboard.commits != 0 {
				t.Fatal("missing or revoked desktop granted a private copy")
			}
		})
	}
}

func TestDeliveredInputNeverAcquiresClipboardAuthority(t *testing.T) {
	read, write := io.Pipe()
	defer read.Close()
	defer write.Close()
	b := &PrivateBridge{prompt: promptFixture(), delivered: true, ctx: context.Background(), session: strings.Repeat("b", 32), channel: &ipc.Channel{W: write}}
	if b.Copy() || b.copySequence != 0 || b.copyPending != nil {
		t.Fatal("a delivered password/input prompt acquired a copy capability")
	}
}

func TestPrivateCopyLosingReturnPipeDoesNotInventAcknowledgmentOrRetry(t *testing.T) {
	p := receiveFixture(t, 1)
	p.Title = "copy-reader-closed"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	commits := 0
	reply, err := runPromptProcess(ctx, p, make(chan struct{}, 1), func() bool { commits++; return true })
	if !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted || commits != 1 {
		t.Fatal("lost copy acknowledgment was fabricated, retried or turned into acceptance")
	}
}

func TestPromptCancellationWhileOwnedPeerWaitsRetiresAllAuthority(t *testing.T) {
	// Exercise repeated request generations: a late result/cancellation must
	// never become authority for the next prompt on the same window.
	w, _ := New(nil)
	defer w.Close()
	w.onEvent = func(event Event) {
		if event.ID != "activity" || event.Value != "" {
			t.Error("private lifecycle signal carried input")
		}
		w.mu.Lock()
		cancel := w.activeCancel
		w.cancelEpoch++
		w.mu.Unlock()
		cancel()
	}
	for range 8 {
		p := promptFixture()
		p.Title = "activity-wait-for-cancel"
		reply, err := w.Prompt(p)
		if !errors.Is(err, uiprompt.ErrCancelled) || reply.Accepted || len(reply.Values) != 0 || reply.Path != "" {
			t.Fatal("cancellation released private input or approval")
		}
		w.mu.Lock()
		retired := !w.modal && w.activeCancel == nil && w.activeDone == nil
		w.mu.Unlock()
		if !retired {
			t.Fatal("cancelled peer retained live private lifecycle state")
		}
	}
}

func TestPrivatePromptResourceExhaustionFailsClosedAndReleasesCreatedPipes(t *testing.T) {
	const marker = "CRYPTOWALLET_TEST_PRIVATE_FD_BUDGET"
	if value := os.Getenv(marker); value != "" {
		spare, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal("invalid controlled descriptor budget")
		}
		// A separate test process alone receives a reduced soft descriptor
		// limit. Existing production code encounters real kernel EMFILE errors;
		// no production syscall/entropy hook or host-wide limit is changed.
		var original syscall.Rlimit
		if err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &original); err != nil {
			t.Fatal(err)
		}
		limit := original
		limit.Cur = 128
		if err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		defer syscall.Setrlimit(syscall.RLIMIT_NOFILE, &original)
		var held []*os.File
		defer func() {
			for _, f := range held {
				f.Close()
			}
		}()
		for {
			f, openErr := os.Open("/dev/null")
			if openErr != nil {
				if !errors.Is(openErr, syscall.EMFILE) {
					t.Fatal("descriptor exhaustion did not reach the expected kernel refusal")
				}
				break
			}
			held = append(held, f)
		}
		for range spare {
			held[len(held)-1].Close()
			held = held[:len(held)-1]
		}
		reply, promptErr := runPromptProcess(context.Background(), promptFixture(), make(chan struct{}, 1), nil)
		if !errors.Is(promptErr, errDesktop) || reply.Accepted || len(reply.Values) != 0 {
			t.Fatal("kernel resource refusal started a prompt or returned private authority")
		}
		// All original spare descriptors must remain available after failure.
		var recovered []*os.File
		for range spare {
			f, openErr := os.Open("/dev/null")
			if openErr != nil {
				t.Fatal("failed prompt leaked partially created inherited descriptors")
			}
			recovered = append(recovered, f)
		}
		for _, f := range recovered {
			f.Close()
		}
		return
	}
	for _, spare := range []int{0, 2, 4, 5} {
		t.Run(strconv.Itoa(spare), func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestPrivatePromptResourceExhaustionFailsClosedAndReleasesCreatedPipes$", "-test.count=1")
			// Retain normal Go test instrumentation in this owned worker.
			// Go itself writes/merges its counters; the test never edits them.
			if directory := flag.Lookup("test.gocoverdir"); directory != nil && directory.Value.String() != "" {
				command.Args = append(command.Args, "-test.gocoverdir="+directory.Value.String())
			}
			command.Env = append(os.Environ(), marker+"="+strconv.Itoa(spare))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("controlled descriptor-pressure worker failed: %s", output)
			}
		})
	}
}
