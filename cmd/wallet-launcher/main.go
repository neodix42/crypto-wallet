// wallet-launcher gives the public Go coordinator its private pipes before
// installing process-wide IP restrictions and execing Electron. Every Electron
// descendant inherits seccomp; only the already-started Go coordinator can use IP.
package main

import (
	"fmt"
	"io"
	"localwallet/internal/buildinfo"
	"localwallet/internal/isolation"
	"localwallet/internal/supervisor"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// launch is public process configuration only. Paths come from this executable's
// installation directory; command-line arguments cannot select an executable.
type launch struct {
	electron string
	args     []string
	env      []string
	child    *exec.Cmd
}

func main() { os.Exit(run()) }

func run() int {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return 0
	}
	exe, e := os.Executable()
	if e != nil {
		return 2
	}
	l, status := prepareLaunch(filepath.Dir(exe), os.Args[1:])
	if status != 0 {
		return status
	}
	return l.execute()
}

func (l *launch) stop() {
	_ = l.child.Process.Kill()
	_ = l.child.Wait()
	_ = unix.Close(3)
	_ = unix.Close(4)
}

func (l *launch) execute() int {
	// Successful exec never returns. Failure must reap the already-started core.
	_ = syscall.Exec(l.electron, l.args, l.env)
	l.stop()
	return 6
}

// pinPipes preserves both source descriptors before replacing fixed descriptors
// 3 and 4. A failure closes every duplicate and already-installed destination.
func pinPipes(reader, writer *os.File) (e error) {
	var copies []int
	var installed []int
	defer func() {
		for _, fd := range copies {
			_ = unix.Close(fd)
		}
		if e != nil {
			for _, fd := range installed {
				_ = unix.Close(fd)
			}
		}
	}()
	for _, f := range []*os.File{reader, writer} {
		var fd int
		fd, e = unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 10)
		if e != nil {
			return e
		}
		copies = append(copies, fd)
	}
	_ = reader.Close()
	_ = writer.Close()
	for i, fd := range copies {
		if e = unix.Dup2(fd, 3+i); e != nil {
			return e
		}
		installed = append(installed, 3+i)
	}
	return nil
}

// prepareLaunch establishes the same production process boundary before either
// exec or a caller's cleanup. No secret input is accepted in this process.
func prepareLaunch(base string, args []string) (_ *launch, status int) {
	if os.Getuid() == 0 {
		return nil, 2
	}
	l := &launch{electron: filepath.Join(base, "LocalWallet")}
	l.args = []string{l.electron}
	backend := ""
	for _, arg := range args {
		if arg != "--ozone-platform=x11" && arg != "--ozone-platform=wayland" {
			return nil, 2
		}
		l.args = append(l.args, arg)
		if arg == "--ozone-platform=x11" {
			backend = "x11"
		} else {
			backend = "wayland"
		}
	}
	for _, name := range []string{"wallet-core", "wallet-signer"} {
		if supervisor.Verify(filepath.Join(base, "resources/bin", name)) != nil {
			return nil, 3
		}
	}
	var files []*os.File
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
		if status != 0 && l.child != nil && l.child.Process != nil {
			_ = l.child.Process.Kill()
			_ = l.child.Wait()
		}
	}()
	for i := 0; i < 2; i++ {
		r, w, e := os.Pipe()
		if e != nil {
			return nil, 3
		}
		files = append(files, r, w)
	}
	for _, k := range []string{"HOME", "USER", "DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME", "DBUS_SESSION_BUS_ADDRESS", "XAUTHORITY", "LANG", "LC_ALL"} {
		if v, ok := os.LookupEnv(k); ok {
			l.env = append(l.env, k+"="+v)
		}
	}
	l.env = append(l.env, "PATH=/usr/bin:/bin", "GOTRACEBACK=none")
	if backend != "" {
		l.env = append(l.env, "GDK_BACKEND="+backend)
	}
	l.child = exec.Command(filepath.Join(base, "resources/bin/wallet-core"), "--environment", "testnet")
	l.child.Env = l.env
	l.child.Stdin, l.child.Stdout, l.child.Stderr = files[2], files[1], io.Discard
	l.child.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if l.child.Start() != nil {
		return nil, 4
	}
	_ = files[2].Close()
	_ = files[1].Close()
	if pinPipes(files[0], files[3]) != nil {
		return nil, 4
	}
	if isolation.Install() != nil {
		_ = unix.Close(3)
		_ = unix.Close(4)
		return nil, 5
	}
	l.env = append(l.env, "LOCALWALLET_INHERITED_CORE=1")
	return l, 0
}
