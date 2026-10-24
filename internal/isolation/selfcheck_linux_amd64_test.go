//go:build linux && amd64

package isolation

import (
	"bytes"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Ptrace: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Skipf("BLOCKED: test-only ptrace unavailable: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	timer := time.AfterFunc(15*time.Second, func() { _ = cmd.Process.Kill() })
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
	threads := map[int]bool{cmd.Process.Pid: true}
	for len(threads) > 0 {
		tid, err := unix.Wait4(-1, &status, unix.WALL, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatal("traced helper wait", err)
		}
		if status.Exited() {
			delete(threads, tid)
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
			if err = unix.PtraceGetRegs(tid, &registers); err != nil {
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
				if err = unix.PtraceSetRegs(tid, &registers); err != nil {
					t.Fatal("test-only denied-result substitution", err)
				}
				injected = true
			}
		} else if signal == syscall.SIGTRAP && status.TrapCause() == unix.PTRACE_EVENT_CLONE {
			thread, eventErr := unix.PtraceGetEventMsg(tid)
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			threads[int(thread)] = true
		} else if signal != syscall.SIGTRAP && signal != syscall.SIGSTOP {
			deliver = int(signal)
		}
		if err = unix.PtraceSyscall(tid, deliver); err != nil && err != unix.ESRCH {
			t.Fatal(err)
		}
	}
	// Wait drains the helper's output-copy goroutines; Wait4 already reaped it.
	_ = cmd.Wait()
	if !injected || exit != 0 {
		t.Fatal("startup guard did not reject the simulated permissive result", injected, exit, output.String())
	}
}
