package main

import (
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

const stderrRelayArgument = "--internal-stderr-relay"

type stderrRelay struct {
	process  *exec.Cmd
	original *os.File
}

// runStderrRelay accepts no wallet/profile/IPC capability. Its parent has
// already installed IP restrictions and zero core-size limits, inherited across
// exec. As with Electron, exec does not preserve PR_SET_DUMPABLE protection.
func runStderrRelay() int {
	if os.Getuid() == 0 {
		return 2
	}
	// startStderrRelay replaces the fixed coordinator slots with copies of the
	// original stderr. Never carry those unnecessary descriptors into the loop.
	_ = unix.Close(3)
	_ = unix.Close(4)
	if relayStderr(os.Stdin, os.Stderr) != nil {
		return 1
	}
	return 0
}

func startStderrRelay() (_ *stderrRelay, err error) {
	// Save stderr before allocating pipes so a missing fd 2 cannot accidentally
	// be replaced by a newly allocated pipe and mistaken for the output target.
	fd, err := unix.FcntlInt(2, unix.F_DUPFD_CLOEXEC, 10)
	if err != nil {
		return nil, err
	}
	original := os.NewFile(uintptr(fd), "original-stderr")
	defer func() {
		if err != nil {
			_ = original.Close()
		}
	}()
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	defer writer.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	child := exec.Command(executable, stderrRelayArgument)
	child.Env = []string{"GOTRACEBACK=none"}
	child.Stdin, child.Stdout, child.Stderr = reader, original, original
	// The coordinator pipes deliberately survive the subsequent Electron exec.
	// Replace fd 3/4 in this child instead: relying on CLOEXEC here would leak
	// the private protocol into a process whose sole job is public diagnostics.
	child.ExtraFiles = []*os.File{original, original}
	if err = child.Start(); err != nil {
		return nil, err
	}
	if err = unix.Dup2(int(writer.Fd()), 2); err != nil {
		_ = writer.Close()
		_ = child.Wait()
		return nil, err
	}
	// Only fd 2 retains the write end. On normal Electron shutdown (or a crash),
	// its descendants close that end and the relay drains through EOF, then exits.
	// A parent-death SIGKILL would discard final genuine crash diagnostics.
	return &stderrRelay{process: child, original: original}, nil
}

// stop is used only when Electron exec failed. Restore output before waiting so
// the relay observes EOF; reap it rather than leaving a bootstrap-failure child.
func (r *stderrRelay) stop() {
	if unix.Dup2(int(r.original.Fd()), 2) != nil {
		// Restoration failure must still close the relay writer; otherwise
		// waiting for its EOF would deadlock bootstrap-failure cleanup.
		_ = unix.Close(2)
	}
	_ = r.original.Close()
	_ = r.process.Wait()
}
