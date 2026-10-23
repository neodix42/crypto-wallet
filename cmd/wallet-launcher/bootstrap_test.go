package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func launcherFixture(t *testing.T, brokenCore bool) string {
	t.Helper()
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(self)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "resources/bin")
	if e = os.MkdirAll(bin, 0700); e != nil {
		t.Fatal(e)
	}
	manifest := map[string]string{}
	for _, name := range []string{"wallet-core", "wallet-signer"} {
		data := binary
		if name == "wallet-core" && brokenCore {
			data = []byte("public intentionally invalid executable\n")
		}
		if e = os.WriteFile(filepath.Join(bin, name), data, 0700); e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data)
		manifest[name] = hex.EncodeToString(sum[:])
	}
	b, _ := json.Marshal(manifest)
	if e = os.WriteFile(filepath.Join(bin, "manifest.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	return dir
}

// Filtering is irreversible. Every call to the actual bootstrap below runs in
// an isolated test process, which returns normally to flush Go coverage data.
func TestTP041BootstrapWorker(t *testing.T) {
	kind := os.Getenv("LOCALWALLET_BOOTSTRAP_TEST_CASE")
	if kind == "" {
		t.Skip("subprocess helper")
	}
	base := os.Getenv("LOCALWALLET_BOOTSTRAP_TEST_BASE")
	switch kind {
	case "invalid-argument":
		for _, arg := range []string{"--profile=local", "--profile=mainnet\x00--no-sandbox", "--profile", "--no-sandbox", "--executable=/tmp/public", "--environment=mainnet", "--ozone-platform=auto", "--remote-debugging-port=9222", "--ozone-platform=x11\x00--no-sandbox"} {
			if l, status := prepareLaunch(base, []string{arg}); status != 2 || l != nil {
				t.Fatal("unsafe argument accepted", arg, status)
			}
		}
		if l, status := prepareLaunch(base, []string{"--profile=testnet", "--profile=mainnet"}); status != 2 || l != nil {
			t.Fatal("ambiguous profile selection accepted", status)
		}
		original := os.Args
		defer func() { os.Args = original }()
		os.Args = []string{os.Args[0], "--no-sandbox"}
		if status := run(); status != 2 {
			t.Fatal("entrypoint did not reject arguments", status)
		}
		os.Args = []string{os.Args[0]}
		if status := run(); status != 3 {
			t.Fatal("entrypoint accepted absent installation manifest", status)
		}
		return
	case "bad-manifest":
		if l, status := prepareLaunch(base, nil); status != 3 || l != nil {
			t.Fatal("invalid installation manifest accepted", status)
		}
		return
	case "bad-core":
		if l, status := prepareLaunch(base, nil); status != 4 || l != nil {
			t.Fatal("unexecutable core accepted", status)
		}
		return
	case "pipe-first", "pipe-second":
		var previous unix.Rlimit
		if e := unix.Getrlimit(unix.RLIMIT_NOFILE, &previous); e != nil {
			t.Fatal(e)
		}
		defer unix.Setrlimit(unix.RLIMIT_NOFILE, &previous)
		if e := unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 32, Max: previous.Max}); e != nil {
			t.Fatal(e)
		}
		var files []*os.File
		defer func() {
			for _, f := range files {
				_ = f.Close()
			}
		}()
		for {
			f, e := os.Open("/dev/null")
			if e != nil {
				break
			}
			files = append(files, f)
		}
		free := 1
		if kind == "pipe-second" {
			free = 3
		}
		for i := 0; i < free; i++ {
			_ = files[len(files)-1-i].Close()
		}
		if l, status := prepareLaunch(base, nil); status != 3 || l != nil {
			t.Fatal("pipe resource exhaustion accepted", status)
		}
		return
	case "closed-reader", "closed-writer":
		r, w, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		defer r.Close()
		defer w.Close()
		if kind == "closed-reader" {
			_ = r.Close()
		} else {
			_ = w.Close()
		}
		before, e := os.ReadDir("/proc/self/fd")
		if e != nil {
			t.Fatal(e)
		}
		if pinPipes(r, w) == nil {
			t.Fatal("closed pipe pinned")
		}
		after, e := os.ReadDir("/proc/self/fd")
		if e != nil || len(before) != len(after) {
			t.Fatal("failed pin leaked duplicate descriptor", e)
		}
		return
	case "isolation-failure":
		// A real kernel policy denies only the dumpability transition. Core
		// startup still succeeds, so this exercises fail-closed cleanup after
		// the child has started, without replacing production isolation code.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if e := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); e != nil {
			t.Fatal(e)
		}
		filters := []unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_PRCTL), Jf: 3},
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.PR_SET_DUMPABLE, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		}
		program := unix.SockFprog{Len: uint16(len(filters)), Filter: &filters[0]}
		if _, _, e := unix.Syscall6(unix.SYS_PRCTL, unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0, 0); e != 0 {
			t.Fatal(e)
		}
		if l, status := prepareLaunch(base, nil); status != 5 || l != nil {
			t.Fatal("isolation failure did not abort launch", status)
		}
		for _, fd := range []int{3, 4} {
			if _, e := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); e != unix.EBADF {
				t.Fatal("isolation failure retained private pipe", fd, e)
			}
		}
		return
	case "run-exec-failure":
		original := os.Args
		defer func() { os.Args = original }()
		os.Args = []string{os.Args[0]}
		if code := run(); code != 6 {
			t.Fatal("entrypoint did not return failed exec status", code)
		}
		return
	}
	for _, key := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "NODE_OPTIONS", "ELECTRON_RUN_AS_NODE", "LOCALWALLET_DEV", "LOCALWALLET_INHERITED_CORE", "GDK_BACKEND", "PATH", "GOTRACEBACK"} {
		if e := os.Setenv(key, "public-untrusted-inherited-value"); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.Setenv("LANG", "C.UTF-8"); e != nil {
		t.Fatal(e)
	}
	if kind == "minimal" {
		for _, key := range []string{"HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG", "LC_ALL"} {
			_ = os.Unsetenv(key)
		}
	}
	args := []string{"--ozone-platform=x11"}
	if kind == "wayland" {
		args = []string{"--ozone-platform=wayland"}
	}
	expectedArgs := append([]string(nil), args...)
	expectedEnvironment := "mainnet"
	if kind == "testnet" {
		args = append(args, "--profile=testnet")
		expectedEnvironment = "testnet"
	} else if kind == "mainnet" {
		args = append(args, "--profile=mainnet")
	}
	l, status := prepareLaunch(base, args)
	if status != 0 || l == nil {
		t.Fatal("bootstrap failed", status)
	}
	defer l.stop()
	if l.electron != filepath.Join(base, "LocalWallet") || !reflect.DeepEqual(l.args, append([]string{l.electron}, expectedArgs...)) {
		t.Fatal("fixed executable or approved arguments changed")
	}
	values := map[string]string{}
	for _, entry := range l.env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatal("malformed environment")
		}
		values[key] = value
	}
	for _, key := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "NODE_OPTIONS", "ELECTRON_RUN_AS_NODE", "LOCALWALLET_DEV", "LOCALWALLET_BOOTSTRAP_TEST_CASE", "LOCALWALLET_BOOTSTRAP_TEST_BASE"} {
		if _, ok := values[key]; ok {
			t.Fatal("unsafe environment inherited", key)
		}
	}
	if values["PATH"] != "/usr/bin:/bin" || values["GOTRACEBACK"] != "none" || values["LOCALWALLET_INHERITED_CORE"] != "1" {
		t.Fatal("fixed environment missing")
	}
	backend := "x11"
	if kind == "wayland" {
		backend = "wayland"
	}
	if values["GDK_BACKEND"] != backend {
		t.Fatal("display backend did not follow approved launcher flag")
	}
	if kind != "minimal" && values["LANG"] != "C.UTF-8" {
		t.Fatal("locale dropped")
	}
	for _, af := range []int{syscall.AF_INET, syscall.AF_INET6, syscall.AF_PACKET} {
		if fd, e := syscall.Socket(af, syscall.SOCK_DGRAM, 0); e == nil {
			syscall.Close(fd)
			t.Fatal("renderer network restriction absent")
		}
	}
	if fd, e := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0); e != nil {
		t.Fatal(e)
	} else {
		syscall.Close(fd)
	}
	if _, _, e := unix.Syscall(unix.SYS_IO_URING_SETUP, 1, 0, 0); e != unix.EPERM && e != unix.ENOSYS {
		t.Fatal("io_uring restriction absent", e)
	}
	for _, fd := range []int{3, 4} {
		flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if e != nil || flags&unix.FD_CLOEXEC != 0 {
			t.Fatal("private pipe will not survive exec", fd, e)
		}
	}
	var boot [12]byte
	if _, e := io.ReadFull(os.NewFile(3, "public-core-output"), boot[:]); e != nil || !bytes.Equal(boot[:], []byte("PUBLIC-BOOT"+strings.ToUpper(expectedEnvironment[:1]))) {
		t.Fatal("core output pipe unavailable", e)
	}
	if _, e := os.NewFile(4, "public-core-input").Write([]byte{1}); e != nil {
		t.Fatal(e)
	}
	if kind == "exec-failure" {
		if code := l.execute(); code != 6 || l.child.ProcessState == nil {
			t.Fatal("failed exec did not reap core", code)
		}
		for _, fd := range []int{3, 4} {
			if _, e := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); e != unix.EBADF {
				t.Fatal("failed exec retained private pipe", fd, e)
			}
		}
	}
}

func TestTP041ProductionBootstrapRejectionsAndBoundary(t *testing.T) {
	if os.Getuid() == 0 {
		t.Fatal("BLOCKED: launcher must be tested as non-root")
	}
	for _, kind := range []string{"invalid-argument", "bad-manifest", "bad-core", "pipe-first", "pipe-second", "closed-reader", "closed-writer", "isolation-failure", "normal", "mainnet", "testnet", "wayland", "minimal", "exec-failure", "run-exec-failure"} {
		t.Run(kind, func(t *testing.T) {
			base := launcherFixture(t, kind == "bad-core")
			if kind == "bad-manifest" {
				if e := os.WriteFile(filepath.Join(base, "resources/bin/manifest.json"), []byte("{}"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestTP041BootstrapWorker$")
			if kind == "run-exec-failure" {
				b, e := os.ReadFile(os.Args[0])
				if e != nil {
					t.Fatal(e)
				}
				path := filepath.Join(base, "launcher-returning-worker")
				if e = os.WriteFile(path, b, 0700); e != nil {
					t.Fatal(e)
				}
				cmd = exec.Command(path, "-test.run=^TestTP041BootstrapWorker$")
			}
			if coverage := flag.Lookup("test.gocoverdir"); coverage != nil && coverage.Value.String() != "" {
				cmd.Args = append(cmd.Args, "-test.gocoverdir="+coverage.Value.String())
			}
			cmd.Env = append(os.Environ(), "LOCALWALLET_BOOTSTRAP_TEST_CASE="+kind, "LOCALWALLET_BOOTSTRAP_TEST_BASE="+base)
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("production bootstrap subprocess: %v %s", e, b)
			}
		})
	}
}
