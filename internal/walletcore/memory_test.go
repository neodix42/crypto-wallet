package walletcore

import (
	"errors"
	"flag"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// This subprocess helper exercises actual core startup. Its denial filter is
// test-only and cannot be selected through the shipped application's inputs.
func TestCoreMemoryStartupHelper(t *testing.T) {
	mode := os.Getenv("LOCALWALLET_CORE_MEMORY_TEST")
	if mode == "" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if mode == "denied" {
		filter := []unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_PRCTL, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		}
		program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
		if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
			t.Fatal("test filter unavailable", err)
		}
		if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&program)), 0, 0); err != nil {
			t.Fatal("test filter unavailable", err)
		}
	}
	flag.CommandLine = flag.NewFlagSet("wallet-core", flag.ContinueOnError)
	os.Args = []string{"wallet-core", "--environment=mainnet"}
	main()
	if mode == "denied" {
		t.Fatal("core accepted unavailable memory protection")
	}
	var limit unix.Rlimit
	dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil || dumpable != 0 || unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
		t.Fatal("core started without dump protection")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("core startup installed signer network restrictions", err)
	}
	listener.Close()
}

func TestCoreStartupProtectsTransientPromptSecretsAndFailsClosed(t *testing.T) {
	for _, mode := range []string{"protected", "denied"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCoreMemoryStartupHelper$")
			for _, arg := range os.Args[1:] {
				if strings.HasPrefix(arg, "-test.gocoverdir=") {
					cmd.Args = append(cmd.Args, arg)
				}
			}
			cmd.Env = append(os.Environ(), "LOCALWALLET_CORE_MEMORY_TEST="+mode, "XDG_CONFIG_HOME="+t.TempDir())
			output, err := cmd.CombinedOutput()
			if mode == "protected" {
				if err != nil {
					t.Fatalf("protected core startup failed: %v %s", err, output)
				}
				return
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 4 {
				t.Fatalf("core did not stop before prompt service: %v %s", err, output)
			}
		})
	}
}
