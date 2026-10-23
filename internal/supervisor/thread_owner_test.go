package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A locked goroutine ending without UnlockOSThread retires its worker thread.
// Keep the initial process thread parked if selected: Go may retain that one.
func retiringWorker(t *testing.T, run func() error) (int, error) {
	t.Helper()
	type outcome struct {
		tid int
		err error
	}
	result := make(chan outcome, 1)
	leader := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	start := func() {
		go func() {
			runtime.LockOSThread()
			tid := unix.Gettid()
			if tid == os.Getpid() {
				leader <- struct{}{}
				<-release
				runtime.UnlockOSThread()
				return
			}
			result <- outcome{tid, run()}
			// Deliberately retire this caller's OS thread.
		}()
	}
	start()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-leader:
			start()
		case r := <-result:
			return r.tid, r.err
		case <-deadline:
			t.Fatal("retiring worker did not complete")
		}
	}
}

func waitRetiredThread(t *testing.T, tid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(fmt.Sprintf("/proc/self/task/%d", tid)); os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("caller worker thread %d remained alive", tid)
}

func creatorThread(t *testing.T, pid int) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		children, err := os.ReadFile(filepath.Join("/proc/self/task", entry.Name(), "children"))
		if err != nil {
			continue
		}
		for _, child := range strings.Fields(string(children)) {
			if child == strconv.Itoa(pid) {
				tid, err := strconv.Atoi(entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				return tid
			}
		}
	}
	t.Fatal("child had no live creator thread")
	return 0
}

func TestKernelPdeathsigControlKillsChildWhenCreatorThreadRetires(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	tid, err := retiringWorker(t, cmd.Start)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	waitRetiredThread(t, tid)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err := <-waited:
		status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if err == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("kernel control did not observe creator-thread SIGKILL: %v %v", err, status)
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatal("creator thread ended but raw child survived")
	}
}

func TestOwnedChildSurvivesCallerThreadRetirementAndReapsOnKill(t *testing.T) {
	c := syntheticChild(t)
	tid, err := retiringWorker(t, func() error {
		return c.Call("snapshot", map[string]string{"mode": "none"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd, owner := c.cmd, c.owner
	ownerTID := creatorThread(t, cmd.Process.Pid)
	if ownerTID == tid {
		t.Fatal("signer inherited caller worker's parent-death lifetime")
	}
	waitRetiredThread(t, tid)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("signer died with caller thread", err)
	}
	if err := c.Call("snapshot", map[string]string{"mode": "none"}, nil); err != nil {
		t.Fatal("signer unusable after caller thread exit", err)
	}
	c.Kill()
	select {
	case <-owner.done:
	default:
		t.Fatal("Kill returned before owner released and reaped child")
	}
	if cmd.ProcessState == nil || c.cmd != nil || c.owner != nil {
		t.Fatal("Kill retained an unreaped process or owner")
	}
}

func TestRestartedChildrenReleaseEveryLifetimeOwner(t *testing.T) {
	c := syntheticChild(t)
	for i := 0; i < 8; i++ {
		if err := c.Call("snapshot", map[string]string{"mode": "none"}, nil); err != nil {
			t.Fatal(err)
		}
		cmd, owner := c.cmd, c.owner
		c.Kill()
		select {
		case <-owner.done:
		default:
			t.Fatalf("restart %d leaked its creator owner", i)
		}
		if cmd.ProcessState == nil || c.owner != nil {
			t.Fatalf("restart %d left an unreaped process", i)
		}
	}
}
