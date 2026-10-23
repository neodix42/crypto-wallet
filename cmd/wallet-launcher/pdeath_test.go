package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// This test-only helper uses actual Linux Pdeathsig and exec, with /bin/cat and
// a public byte challenge. No test hook, credential or debug flag is shipped.
func init() {
	if filepath.Base(os.Args[0]) == "launcher-thread-lifecycle-fixture" {
		os.Exit(threadLifecycleFixture())
	}
}

func threadLifecycleFixture() int {
	if len(os.Args) == 3 && os.Args[1] == "after" {
		pid, err := strconv.Atoi(os.Args[2])
		if err != nil {
			return 90
		}
		defer func() { _ = syscall.Kill(pid, syscall.SIGKILL); _, _ = syscall.Wait4(pid, nil, 0, nil) }()
		time.Sleep(20 * time.Millisecond)
		input, output := os.NewFile(4, "public-child-input"), os.NewFile(3, "public-child-output")
		defer input.Close()
		defer output.Close()
		if _, err = input.Write([]byte("PUBLIC-PROBE")); err != nil {
			fmt.Println("child died at exec")
			return 42
		}
		done := make(chan error, 1)
		go func() {
			var reply [12]byte
			_, err := io.ReadFull(output, reply[:])
			if err == nil && string(reply[:]) != "PUBLIC-PROBE" {
				err = fmt.Errorf("different public challenge")
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				fmt.Println("child died at exec")
				return 42
			}
			fmt.Println("child survived exec")
			return 0
		case <-time.After(3 * time.Second):
			return 91
		}
	}
	runtime.LockOSThread()
	cmd := exec.Command("/bin/cat")
	reader, err := cmd.StdoutPipe()
	if err != nil {
		return 92
	}
	writer, err := cmd.StdinPipe()
	if err != nil {
		return 93
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	started := make(chan int, 1)
	if len(os.Args) == 2 && os.Args[1] == "same" {
		if err := cmd.Start(); err != nil {
			return 94
		}
		started <- syscall.Gettid()
	} else {
		go func() {
			runtime.LockOSThread()
			if err := cmd.Start(); err != nil {
				started <- 0
				return
			}
			started <- syscall.Gettid()
			select {}
		}()
	}
	creator := <-started
	if creator == 0 {
		return 95
	}
	fmt.Printf("creatorTID=%d execTID=%d\n", creator, syscall.Gettid())
	// Preserve both sources before installing the fixed inherited descriptors.
	a, err := syscall.Dup(int(reader.(*os.File).Fd()))
	if err != nil {
		return 96
	}
	b, err := syscall.Dup(int(writer.(*os.File).Fd()))
	if err != nil {
		return 97
	}
	if syscall.Dup2(a, 3) != nil || syscall.Dup2(b, 4) != nil {
		return 98
	}
	_ = syscall.Close(a)
	_ = syscall.Close(b)
	executable, err := os.Executable()
	if err != nil {
		return 99
	}
	_ = syscall.Exec(executable, []string{executable, "after", strconv.Itoa(cmd.Process.Pid)}, os.Environ())
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return 100
}

func TestTP041LinuxCreatorThreadLifetimeAcrossExec(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "launcher-thread-lifecycle-fixture")
	if err := os.WriteFile(helper, content, 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"other", "same"} {
		t.Run(mode, func(t *testing.T) {
			for attempt := 0; attempt < 3; attempt++ {
				command := exec.Command(helper, mode)
				output, err := command.CombinedOutput()
				if mode == "same" {
					if err != nil || !bytes.Contains(output, []byte("child survived exec")) {
						t.Fatalf("same creating/exec thread lost child: %v %s", err, output)
					}
				} else {
					var exit *exec.ExitError
					if err == nil {
						t.Fatalf("different creating thread unexpectedly survived: %s", output)
					}
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 42 || !bytes.Contains(output, []byte("child died at exec")) {
						t.Fatalf("different thread did not reproduce kernel signal: %v %s", err, output)
					}
				}
			}
		})
	}
}
