package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/isolation"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const stderrProcessCase = "LOCALWALLET_STDERR_PROCESS_CASE"

// Exercise changes to process descriptors and irreversible seccomp restrictions
// only in disposable public-fixture subprocesses. No wallet data is accessed.
func TestTP041StderrRelayProcessLifecycle(t *testing.T) {
	if mode := os.Getenv(stderrProcessCase); mode != "" {
		runStderrProcessCase(t, mode)
		return
	}
	if os.Getuid() == 0 {
		t.Fatal("BLOCKED: launcher must be tested as non-root")
	}
	for _, mode := range []string{
		"startup-stop", "stop-restore-failure", "missing-stderr", "pipe-failure",
		"exec-failure", "dup2-failure", "direct-eof", "direct-write-failure",
		"execute-failure", "execute-diagnostic-failure", "normal-parent-eof", "abrupt-parent-eof",
	} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := stderrProcessCommand(ctx, mode)
			var output, diagnostics bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &diagnostics
			cmd.Stdin = strings.NewReader(stderrProcessInput())
			if err := cmd.Run(); err != nil {
				t.Fatalf("public relay fixture %s: %v; stdout %q; stderr %q", mode, err, output.Bytes(), diagnostics.Bytes())
			}
			want := ""
			if mode == "startup-stop" || mode == "stop-restore-failure" || mode == "direct-eof" || strings.HasSuffix(mode, "parent-eof") {
				want = stderrProcessExpected()
			} else if mode == "execute-diagnostic-failure" {
				want = "Wallet startup failed: cannot initialize diagnostic output.\n"
			}
			if diagnostics.String() != want {
				t.Fatalf("%s changed genuine diagnostics: got %q; want %q", mode, diagnostics.String(), want)
			}
		})
	}
}

func stderrProcessCommand(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTP041StderrRelayProcessLifecycle$")
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			cmd.Args = append(cmd.Args, arg)
		}
	}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, stderrProcessCase+"=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, stderrProcessCase+"="+mode)
	cmd.WaitDelay = 3 * time.Second
	return cmd
}

func stderrProcessInput() string {
	var input strings.Builder
	for _, atom := range []string{"_NET_WM_STATE_MODAL", "application/vnd.portal.filetransfer", "application/vnd.portal.files"} {
		fmt.Fprintf(&input, "%s%s to kAtomsToCache\n", atomDiagnosticPrefix, atom)
	}
	return input.String() + stderrProcessExpected()
}

func stderrProcessExpected() string {
	return strings.Replace(atomDiagnosticPrefix, ":ERROR:", ":FATAL:", 1) + "_NET_WM_STATE_MODAL to kAtomsToCache\n" +
		atomDiagnosticPrefix + "_NET_WM_UNKNOWN to kAtomsToCache\n" +
		"PUBLIC genuine wallet operation failure\n" +
		strings.Repeat("public oversized diagnostic ", stderrLineLimit/8) + "\n" +
		"PUBLIC unterminated fatal tail"
}

func runStderrProcessCase(t *testing.T, mode string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if mode == "normal-parent-eof" || mode == "abrupt-parent-eof" {
		checkParentExitDrainsRelay(t, mode == "abrupt-parent-eof")
		return
	}
	if mode == "execute-failure" || mode == "execute-diagnostic-failure" {
		checkFailedElectronExecCleansChildren(t, mode == "execute-diagnostic-failure")
		return
	}
	if err := isolation.Install(); err != nil {
		t.Fatal(err)
	}
	if mode == "direct-eof" || mode == "direct-write-failure" {
		want := 0
		if mode == "direct-write-failure" {
			if err := unix.Close(2); err != nil {
				t.Fatal(err)
			}
			want = 1
		}
		if status := runStderrRelay(); status != want {
			t.Fatalf("relay returned %d; want %d", status, want)
		}
		for _, fd := range []int{3, 4} {
			var stat unix.Stat_t
			if err := unix.Fstat(fd, &stat); !errors.Is(err, unix.EBADF) {
				t.Fatalf("relay retained unnecessary coordinator descriptor %d: %v", fd, err)
			}
		}
		return
	}
	if mode == "missing-stderr" {
		if err := unix.Close(2); err != nil {
			t.Fatal(err)
		}
		relay, err := startStderrRelay()
		if relay != nil || !errors.Is(err, unix.EBADF) {
			t.Fatalf("closed stderr was replaced or accepted: %v %v", relay, err)
		}
		return
	}
	if strings.HasSuffix(mode, "-failure") && mode != "stop-restore-failure" {
		// Initialize the runtime's shared poll descriptors before comparing the
		// count; the relay's first os.Pipe may otherwise create those normally.
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_ = reader.Close()
		_ = writer.Close()
		call, errno := uint32(unix.SYS_PIPE2), unix.EMFILE
		switch mode {
		case "exec-failure":
			call, errno = uint32(unix.SYS_EXECVE), unix.EPERM
		case "dup2-failure":
			errno = unix.EPERM
		}
		if mode == "dup2-failure" {
			denyStderrPipeReplacement(t)
		} else {
			denyStderrTestSyscall(t, call, errno)
		}
		before := stderrOpenDescriptorCount(t)
		relay, err := startStderrRelay()
		if relay != nil || !errors.Is(err, errno) {
			t.Fatalf("real syscall failure was not returned: %v %v; want %v", relay, err, errno)
		}
		if after := stderrOpenDescriptorCount(t); after != before {
			t.Fatalf("failed relay startup leaked descriptors: before %d after %d", before, after)
		}
		assertNoStderrChild(t)
		return
	}
	// These private fixture pipes deliberately occupy the production fixed
	// coordinator slots. The exec helper's init hook verifies that its fd 3/4
	// instead refer to original stderr, IP is denied and the environment is clean.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := pinPipes(reader, writer); err != nil {
		t.Fatal(err)
	}
	var original [2]unix.Stat_t
	for index := range original {
		if err := unix.Fstat(3+index, &original[index]); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Setenv("LOCALWALLET_INHERITED_CORE", "public-private-pipe-fixture")
	relay, err := startStderrRelay()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stderr.WriteString(stderrProcessInput()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(mode, "worker-") {
		fmt.Println(relay.process.Process.Pid)
		if mode == "worker-abrupt" {
			for {
				time.Sleep(time.Hour)
			}
		}
		return // Process exit closes fd 2; the orphan relay must drain to EOF.
	}
	if mode == "stop-restore-failure" {
		denyStderrTestSyscall(t, uint32(unix.SYS_DUP3), unix.EPERM)
	}
	relay.stop()
	if relay.process.ProcessState == nil || !relay.process.ProcessState.Success() {
		t.Fatalf("relay failed its isolation/descriptor assertions: %v", relay.process.ProcessState)
	}
	for index, want := range original {
		var current unix.Stat_t
		if err := unix.Fstat(3+index, &current); err != nil || current.Dev != want.Dev || current.Ino != want.Ino || current.Mode != want.Mode {
			t.Fatalf("relay changed the parent's coordinator descriptor %d: %v", 3+index, err)
		}
	}
	assertNoStderrChild(t)
}

func stderrOpenDescriptorCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func assertNoStderrChild(t *testing.T) {
	t.Helper()
	var status unix.WaitStatus
	if pid, err := unix.Wait4(-1, &status, unix.WNOHANG, nil); pid > 0 || !errors.Is(err, unix.ECHILD) {
		t.Fatalf("relay startup/cleanup left a live or unreaped child: pid %d error %v", pid, err)
	}
}

func checkFailedElectronExecCleansChildren(t *testing.T, diagnosticFailure bool) {
	// The existing public coordinator fixture checks that it starts with IP
	// available. prepareLaunch then installs the real renderer restrictions.
	binary, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	bin := filepath.Join(base, "resources/bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wallet-core", "wallet-signer"} {
		if err := os.WriteFile(filepath.Join(bin, name), binary, 0700); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(binary)
	manifest, err := json.Marshal(map[string]string{"wallet-core": hex.EncodeToString(sum[:]), "wallet-signer": hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	launcher, status := prepareLaunch(base, nil)
	if status != 0 {
		t.Fatalf("public coordinator could not start: status %d", status)
	}
	want := 6 // The intentionally absent Electron executable makes exec fail.
	if diagnosticFailure {
		denyStderrTestSyscall(t, uint32(unix.SYS_PIPE2), unix.EMFILE)
		want = 7
	}
	if status := launcher.execute(); status != want {
		t.Fatalf("failed startup returned %d; want %d", status, want)
	}
	if launcher.child.ProcessState == nil || launcher.child.ProcessState.Success() {
		t.Fatalf("failed Electron startup did not stop its already-started coordinator: %v", launcher.child.ProcessState)
	}
	for _, fd := range []int{3, 4} {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); !errors.Is(err, unix.EBADF) {
			t.Fatalf("failed startup retained coordinator descriptor %d: %v", fd, err)
		}
	}
	assertNoStderrChild(t)
}

// Install real kernel fault injection in this owned process, avoiding mutable
// production test hooks. Only the named syscall is denied; all others pass.
func denyStderrTestSyscall(t *testing.T, number uint32, errno unix.Errno) {
	t.Helper()
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: number, Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(errno)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	installStderrTestFilter(t, filter)
}

// x/sys implements Dup2 with dup3. Let exec map its saved original stderr
// (duplicated at fd >= 10), then deny only the parent's pipe-to-fd2 replacement.
func denyStderrPipeReplacement(t *testing.T) {
	t.Helper()
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_DUP3), Jf: 5},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 24},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: 2, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, K: 10, Jt: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	installStderrTestFilter(t, filter)
}

func installStderrTestFilter(t *testing.T, filter []unix.SockFilter) {
	t.Helper()
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, err := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if err != 0 {
		t.Fatal("could not install public syscall-failure fixture", err)
	}
}

func checkParentExitDrainsRelay(t *testing.T, abrupt bool) {
	// Keep orphan adoption scoped to this disposable orchestrator, then reap the
	// actual relay explicitly. Tests must not leave diagnostics helpers behind.
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	mode := "worker-normal"
	if abrupt {
		mode = "worker-abrupt"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := stderrProcessCommand(ctx, mode)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	control, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(control).ReadString('\n')
	if err != nil {
		t.Fatal("public parent did not report its relay", err)
	}
	relayPID, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || relayPID <= 1 {
		t.Fatalf("invalid owned relay PID %q: %v", line, err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = unix.Kill(relayPID, unix.SIGKILL)
			var status unix.WaitStatus
			_, _ = unix.Wait4(relayPID, &status, 0, nil)
		}
	}()
	if abrupt {
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
	err = cmd.Wait()
	if abrupt {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("public fixture parent was not killed as requested: %v", err)
		}
	} else if err != nil {
		t.Fatal("normal public fixture parent failed", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(relayPID, &status, unix.WNOHANG, nil)
		if err != nil {
			t.Fatal("orphan relay could not be reaped", err)
		}
		if pid == relayPID {
			reaped = true
			if !status.Exited() || status.ExitStatus() != 0 {
				t.Fatalf("relay lost queued diagnostics after parent exit: %v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("relay remained live after its input reached EOF")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if diagnostics.String() != stderrProcessExpected() {
		t.Fatalf("parent exit lost queued genuine diagnostics: got %q; want %q", diagnostics.String(), stderrProcessExpected())
	}
	if _, err := os.Stderr.Write(diagnostics.Bytes()); err != nil {
		t.Fatal(err)
	}
	assertNoStderrChild(t)
}
