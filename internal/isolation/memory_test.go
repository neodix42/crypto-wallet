package isolation

import (
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Run in a disposable subprocess: reducing RLIMIT_CORE's hard limit cannot be
// undone by an ordinary process and must never change the invoking user's app.
func TestTP041MemoryProtectionPreservesCoordinatorNetworking(t *testing.T) {
	if os.Getenv("LOCALWALLET_MEMORY_PROTECTION_TEST") == "1" {
		if err := unix.Prctl(unix.PR_SET_DUMPABLE, 1, 0, 0, 0); err != nil {
			t.Fatal("could not establish public fixture baseline", err)
		}
		if err := ProtectMemory(); err != nil {
			t.Fatal(err)
		}
		var limit unix.Rlimit
		dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
		if err != nil || dumpable != 0 || unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
			t.Fatal("secret-bearing coordinator still dumpable")
		}
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal("memory protection blocked loopback IP", err)
		}
		defer listener.Close()
		done := make(chan error, 1)
		go func() {
			connection, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
			_, err = io.CopyN(connection, connection, 1)
			done <- err
		}()
		connection, err := net.DialTimeout("tcp4", listener.Addr().String(), 3*time.Second)
		if err != nil {
			t.Fatal("coordinator RPC connection denied", err)
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err = connection.Write([]byte{'P'}); err != nil {
			t.Fatal(err)
		}
		var echo [1]byte
		if _, err = io.ReadFull(connection, echo[:]); err != nil || echo[0] != 'P' {
			t.Fatal("loopback RPC exchange failed", err)
		}
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		if err = ProtectMemory(); err != nil {
			t.Fatal("memory protection not idempotent", err)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTP041MemoryProtectionPreservesCoordinatorNetworking$")
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			cmd.Args = append(cmd.Args, arg)
		}
	}
	cmd.Env = append(os.Environ(), "LOCALWALLET_MEMORY_PROTECTION_TEST=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("memory-protection subprocess: %v %s", err, output)
	}
}
