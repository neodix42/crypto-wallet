package isolation

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
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
		// These exercise the actual standard networking entry points after
		// isolation. A denied socket is required; an unreachable host or a
		// DNS failure alone would not prove that no packet could be sent.
		for _, destination := range []string{"127.0.0.1:9", "[::1]:9", "192.0.2.1:9"} {
			for _, network := range []string{"tcp", "udp"} {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				connection, err := (&net.Dialer{}).DialContext(ctx, network, destination)
				cancel()
				if connection != nil {
					connection.Close()
				}
				if !errors.Is(err, syscall.EPERM) {
					t.Fatal("network entry point did not fail at the isolation boundary", network, destination, err)
				}
			}
		}
		var dnsDenied atomic.Int32
		var dnsUnexpected atomic.Bool
		resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := (&net.Dialer{}).DialContext(ctx, "udp", "8.8.8.8:53")
			if errors.Is(err, syscall.EPERM) {
				dnsDenied.Add(1)
			} else {
				dnsUnexpected.Store(true)
			}
			return connection, err
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, dnsErr := resolver.LookupHost(ctx, "fixture.invalid")
		cancel()
		if dnsErr == nil || dnsDenied.Load() == 0 || dnsUnexpected.Load() {
			t.Fatal("DNS did not fail before opening its socket", dnsErr)
		}
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
		response, httpErr := client.Get("http://127.0.0.1:9/public-synthetic-isolation-check")
		if response != nil {
			response.Body.Close()
		}
		if !errors.Is(httpErr, syscall.EPERM) {
			t.Fatal("HTTP did not fail before opening its socket", httpErr)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTP041ProductionNetworkIsolation$")
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			cmd.Args = append(cmd.Args, arg)
		}
	}
	cmd.Env = append(os.Environ(), "LOCALWALLET_ISOLATION_TEST=1")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("isolation subprocess: %v %s", e, b)
	}
}
