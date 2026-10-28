//go:build linux && amd64

package isolation

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This uses a test-only syscall tracer, not a production injection hook. The
// actual seccomp filter still rejects the IP socket. Replacing its denied return
// value with harmless fd0 forces the compiled startup guard to see a permissive
// kernel result and prove that it refuses to continue. No IP socket is opened.
func TestTP041StartupRejectsApparentlyPermissiveKernel(t *testing.T) {
	if fault := os.Getenv("LOCALWALLET_ISOLATION_PERMISSIVE_TEST"); fault != "" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		want := "signer network self-check failed"
		if fault == "prctl" {
			want = "signer isolation unavailable"
		}
		if err := Install(); err == nil || err.Error() != want {
			t.Fatal("apparently permissive network escaped startup self-check", err)
		}
		return
	}
	for _, fault := range []string{"socket", "prctl"} {
		t.Run(fault, func(t *testing.T) { traceIsolationFault(t, fault) })
	}
}

func traceIsolationFault(t *testing.T, fault string) {
	traceStartupFault(t, "TestTP041StartupRejectsApparentlyPermissiveKernel", "LOCALWALLET_ISOLATION_PERMISSIVE_TEST", fault)
}

func TestTP041MemoryProtectionFailsClosedOnKernelErrors(t *testing.T) {
	if fault := os.Getenv("LOCALWALLET_MEMORY_FAULT_TEST"); fault != "" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := ProtectMemory(); err == nil || err.Error() != "wallet memory protection unavailable" {
			t.Fatal("core dump protection failure accepted", err)
		}
		return
	}
	for _, fault := range []string{"dump-set", "dump-check"} {
		t.Run(fault, func(t *testing.T) {
			traceStartupFault(t, "TestTP041MemoryProtectionFailsClosedOnKernelErrors", "LOCALWALLET_MEMORY_FAULT_TEST", fault)
		})
	}
}

func traceStartupFault(t *testing.T, testName, environment, fault string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cmd, output := startStartupTrace(t, testName, environment, fault)
	pid := cmd.Process.Pid
	defer func() {
		if err := cleanupStartupTrace(cmd, 2*time.Second); err != nil {
			t.Error("test-only tracer cleanup", err)
		}
	}()
	timer := time.AfterFunc(15*time.Second, func() { _ = unix.Kill(-pid, unix.SIGKILL) })
	defer timer.Stop()
	var status unix.WaitStatus
	if _, err := unix.Wait4(cmd.Process.Pid, &status, 0, nil); err != nil || !status.Stopped() {
		t.Fatal("traced helper did not stop after exec", err)
	}
	options := unix.PTRACE_O_TRACESYSGOOD | unix.PTRACE_O_TRACECLONE | unix.PTRACE_O_EXITKILL
	if err := unix.PtraceSetOptions(cmd.Process.Pid, options); err != nil {
		t.Skipf("BLOCKED: test-only ptrace options unavailable: %v", err)
	}
	if err := unix.PtraceSyscall(cmd.Process.Pid, 0); err != nil {
		t.Fatal(err)
	}
	injected, exit := false, -1
	for {
		// Drain this helper's whole process group rather than relying on clone
		// notification order to maintain a userspace list of live threads.
		tid, err := unix.Wait4(-cmd.Process.Pid, &status, unix.WALL|unix.WNOTHREAD, nil)
		if err == unix.EINTR {
			continue
		}
		if err == unix.ECHILD {
			break
		}
		if err != nil {
			t.Fatal("traced helper wait", err)
		}
		if status.Exited() {
			if tid == cmd.Process.Pid {
				exit = status.ExitStatus()
			}
			continue
		}
		if status.Signaled() {
			t.Fatal("traced helper terminated", status.Signal())
		}
		if !status.Stopped() {
			t.Fatal("unexpected traced helper status", status)
		}
		signal := status.StopSignal()
		deliver := 0
		if signal == syscall.Signal(int(syscall.SIGTRAP)|0x80) {
			var registers unix.PtraceRegs
			if err = unix.PtraceGetRegs(tid, &registers); err == unix.ESRCH {
				// exit_group can terminate another thread after its syscall stop
				// was reported. Its final wait status must still be drained.
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			socketFault := fault == "socket" && registers.Orig_rax == unix.SYS_SOCKET && (registers.Rdi == unix.AF_INET || registers.Rdi == unix.AF_INET6) && int64(registers.Rax) == -int64(unix.EPERM)
			prctlFault := fault == "prctl" && registers.Orig_rax == unix.SYS_PRCTL && registers.Rdi == unix.PR_SET_NO_NEW_PRIVS && registers.Rax == 0
			dumpSetFault := fault == "dump-set" && registers.Orig_rax == unix.SYS_PRCTL && registers.Rdi == unix.PR_SET_DUMPABLE && registers.Rax == 0
			dumpCheckFault := fault == "dump-check" && registers.Orig_rax == unix.SYS_PRCTL && registers.Rdi == unix.PR_GET_DUMPABLE && registers.Rax == 0
			if !injected && (socketFault || prctlFault || dumpSetFault || dumpCheckFault) {
				if socketFault {
					registers.Rax = 0
				} else if dumpCheckFault {
					registers.Rax = 1
				} else {
					denied := -int64(unix.EPERM)
					registers.Rax = uint64(denied)
				}
				if err = unix.PtraceSetRegs(tid, &registers); err == unix.ESRCH {
					continue
				} else if err != nil {
					t.Fatal("test-only denied-result substitution", err)
				}
				injected = true
			}
		} else if signal != syscall.SIGTRAP && signal != syscall.SIGSTOP {
			deliver = int(signal)
		}
		if err = unix.PtraceSyscall(tid, deliver); err != nil && err != unix.ESRCH {
			t.Fatal(err)
		}
	}
	if !injected || exit != 0 {
		contents, _ := os.ReadFile(output.Name())
		t.Fatal("startup guard did not reject the simulated permissive result", injected, exit, string(contents))
	}
}

func startStartupTrace(t *testing.T, testName, environment, fault string) (*exec.Cmd, *os.File) {
	t.Helper()
	args := []string{"-test.run=^" + testName + "$"}
	// Go's raw test coverage directory aggregates the real helper execution.
	// No counters or expected outcomes are synthesized by the tracer.
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			args = append(args, arg)
		}
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), environment+"="+fault)
	cmd.SysProcAttr = &syscall.SysProcAttr{Ptrace: true, Setpgid: true}
	// A direct file descriptor avoids exec.Cmd output-copy goroutines: raw
	// wait4 owns all reaping, including cleanup after a test assertion fails.
	output, err := os.CreateTemp(t.TempDir(), "traced-helper-output-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Skipf("BLOCKED: test-only ptrace unavailable: %v", err)
	}
	return cmd, output
}

func cleanupStartupTrace(cmd *exec.Cmd, timeout time.Duration) error {
	// Never use cmd.Wait here. An abandoned ptrace stop can keep its pidfd
	// wait blocked forever even after SIGKILL; the tracer must drain stops.
	pid := cmd.Process.Pid
	if pid <= 0 { // An earlier cleanup already released the process handle.
		return nil
	}
	defer cmd.Process.Release()
	deadline := time.Now().Add(timeout)
	killed := false
	for {
		var status unix.WaitStatus
		tid, err := unix.Wait4(-pid, &status, unix.WALL|unix.WNOTHREAD|unix.WNOHANG, nil)
		if err == unix.EINTR {
			continue
		}
		if err == unix.ECHILD {
			return nil
		}
		if err != nil {
			return fmt.Errorf("drain helper wait: %w", err)
		}
		if !killed {
			if err := unix.Kill(-pid, unix.SIGKILL); err != nil && err != unix.ESRCH {
				return fmt.Errorf("kill helper process group: %w", err)
			}
			killed = true
		}
		if tid != 0 && status.Stopped() {
			if err := unix.PtraceCont(tid, int(unix.SIGKILL)); err != nil && err != unix.ESRCH {
				return fmt.Errorf("continue killed helper thread: %w", err)
			}
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("helper process group did not drain within %s", timeout)
		}
		if tid == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}

func TestTP041MemoryProtectionTracerCleanupDrainsStoppedThreads(t *testing.T) {
	if os.Getenv("LOCALWALLET_TRACER_CLEANUP_TEST") != "" {
		time.Sleep(time.Minute)
		return
	}
	for _, stage := range []string{"exec-stop", "clone-stop"} {
		t.Run(stage, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			cmd, _ := startStartupTrace(t, "TestTP041MemoryProtectionTracerCleanupDrainsStoppedThreads", "LOCALWALLET_TRACER_CLEANUP_TEST", stage)
			pid := cmd.Process.Pid
			defer func() {
				if err := cleanupStartupTrace(cmd, 2*time.Second); err != nil {
					t.Error(err)
				}
			}()
			var status unix.WaitStatus
			if _, err := unix.Wait4(cmd.Process.Pid, &status, 0, nil); err != nil || !status.Stopped() {
				t.Fatal("cleanup helper did not stop after exec", err)
			}
			if stage == "clone-stop" {
				if err := unix.PtraceSetOptions(cmd.Process.Pid, unix.PTRACE_O_TRACECLONE|unix.PTRACE_O_EXITKILL); err != nil {
					t.Skipf("BLOCKED: test-only ptrace options unavailable: %v", err)
				}
				if err := unix.PtraceCont(cmd.Process.Pid, 0); err != nil {
					t.Fatal(err)
				}
				timer := time.AfterFunc(5*time.Second, func() { _ = unix.Kill(-pid, unix.SIGKILL) })
				defer timer.Stop()
				for {
					tid, err := unix.Wait4(-cmd.Process.Pid, &status, unix.WALL|unix.WNOTHREAD, nil)
					if err == unix.EINTR {
						continue
					}
					if err != nil || !status.Stopped() {
						t.Fatal("cleanup helper did not create a traced thread", err, status)
					}
					if status.StopSignal() == syscall.SIGTRAP && status.TrapCause() == unix.PTRACE_EVENT_CLONE {
						break // Deliberately abandon real stopped parent/child tracees.
					}
					if err := unix.PtraceCont(tid, 0); err != nil && err != unix.ESRCH {
						t.Fatal(err)
					}
				}
			}
			started := time.Now()
			if err := cleanupStartupTrace(cmd, 2*time.Second); err != nil {
				t.Fatal(err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("cleanup exceeded its bounded deadline")
			}
			if _, err := unix.Wait4(-pid, &status, unix.WALL|unix.WNOTHREAD|unix.WNOHANG, nil); err != unix.ECHILD {
				t.Fatal("cleanup left helper wait statuses or live traced threads", err)
			}
			if err := unix.Kill(pid, 0); err != unix.ESRCH {
				t.Fatal("cleanup left helper leader alive", err)
			}
		})
	}
}
