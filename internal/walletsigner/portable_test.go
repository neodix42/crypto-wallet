package walletsigner

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
	"localwallet/internal/ipc"
)

func TestPortableSignerMemoryProtectionWithoutSyscallSandbox(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_PORTABLE_SIGNER_TEST") == "1" {
		flag.CommandLine = flag.NewFlagSet("cryptowallet", flag.ContinueOnError)
		os.Args = []string{"cryptowallet", "--data", os.Getenv("CRYPTOWALLET_PORTABLE_SIGNER_DATA"), "--environment", "testnet"}
		MainPortable()
		var limit unix.Rlimit
		dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
		if err != nil || dumpable != 0 || unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
			t.Fatal("portable signer did not protect secret-bearing memory")
		}
		// Create and immediately close a socket; no connection, packets or funds.
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err != nil {
			t.Fatal("portable signer installed an IP socket restriction", err)
		}
		syscall.Close(fd)
		return
	}
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inputReader.Close()
	defer inputWriter.Close()
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputReader.Close()
	defer outputWriter.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestPortableSignerMemoryProtectionWithoutSyscallSandbox$")
	for _, argument := range os.Args[1:] {
		if strings.HasPrefix(argument, "-test.gocoverdir=") {
			child.Args = append(child.Args, argument)
		}
	}
	child.Env = append(os.Environ(), "CRYPTOWALLET_PORTABLE_SIGNER_TEST=1", "CRYPTOWALLET_PORTABLE_SIGNER_DATA="+filepath.Join(t.TempDir(), "profile"))
	child.Stdin, child.Stdout, child.Stderr = inputReader, outputWriter, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	_ = outputWriter.Close()
	if err := (&ipc.Channel{W: inputWriter}).Write(request("snapshot", struct{}{})); err != nil {
		t.Fatal(err)
	}
	_ = inputWriter.Close()
	reply, err := (&ipc.Channel{R: outputReader}).Read()
	if err != nil || reply.Error != "" || !strings.Contains(string(reply.Body), `"locked":true`) || !strings.Contains(string(reply.Body), `"vaultState":"missing"`) {
		t.Fatal("portable signer did not use the real locked engine", reply, err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal("portable signer process failed", err)
	}
	if _, err := io.ReadAll(outputReader); err != nil {
		t.Fatal(err)
	}
}
