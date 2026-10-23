package isolation

import (
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

func TestTP041ProductionNetworkIsolation(t *testing.T) {
	if os.Getenv("LOCALWALLET_ISOLATION_TEST") == "1" {
		if e := Install(); e != nil {
			t.Fatal(e)
		}
		_, _, ioErr := unix.Syscall(unix.SYS_IO_URING_SETUP, 1, 0, 0)
		if ioErr != unix.EPERM && ioErr != unix.ENOSYS {
			t.Fatal("io_uring bypass not blocked")
		}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, af := range []int{syscall.AF_INET, syscall.AF_INET6, syscall.AF_PACKET} {
					for _, typ := range []int{syscall.SOCK_STREAM, syscall.SOCK_DGRAM} {
						fd, e := syscall.Socket(af, typ, 0)
						if e == nil {
							syscall.Close(fd)
							t.Error("IP socket escaped seccomp")
						}
					}
				}
				fd, e := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
				if e != nil {
					t.Error("native local sockets blocked")
				}
				syscall.Close(fd)
			}()
		}
		wg.Wait()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTP041ProductionNetworkIsolation$")
	cmd.Env = append(os.Environ(), "LOCALWALLET_ISOLATION_TEST=1")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("isolation subprocess: %v %s", e, b)
	}
}
