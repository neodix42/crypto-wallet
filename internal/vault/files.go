package vault

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrFile = errors.New("unsafe, busy or unavailable vault storage")

type Files struct {
	dir   *os.File
	lock  *os.File
	fault func(string) error
}

func NewFiles(path string) (*Files, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrFile
	}
	fd, e := openDirectory(path)
	if e != nil {
		return nil, ErrFile
	}
	d := os.NewFile(uintptr(fd), path)
	var s unix.Stat_t
	if unix.Fstat(fd, &s) != nil || s.Uid != uint32(os.Getuid()) || s.Mode&0077 != 0 {
		d.Close()
		return nil, ErrFile
	}
	lfd, e := unix.Openat(fd, "writer.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		d.Close()
		return nil, ErrFile
	}
	l := os.NewFile(uintptr(lfd), "writer.lock")
	if unix.Fstat(lfd, &s) != nil || s.Uid != uint32(os.Getuid()) || s.Mode&0077 != 0 || s.Nlink != 1 || s.Mode&unix.S_IFMT != unix.S_IFREG || unix.Flock(lfd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		l.Close()
		d.Close()
		return nil, ErrFile
	}
	return &Files{dir: d, lock: l}, nil
}
func (f *Files) Close() error {
	if f.lock != nil {
		f.lock.Close()
	}
	return f.dir.Close()
}
func validName(s string) bool {
	return s != "" && s != "." && s != ".." && filepath.Base(s) == s && !strings.ContainsRune(s, 0) && len(s) < 200
}

// Exists checks an entry relative to the already verified directory without
// opening or reading its contents. Only an absent entry returns false, nil;
// unsafe entries and unavailable storage remain errors, never an empty vault.
func (f *Files) Exists(name string) (bool, error) {
	if f == nil || f.dir == nil || !validName(name) {
		return false, ErrFile
	}
	dir := int(f.dir.Fd())
	var s unix.Stat_t
	if unix.Fstat(dir, &s) != nil || s.Uid != uint32(os.Getuid()) || s.Mode&unix.S_IFMT != unix.S_IFDIR || s.Mode&0077 != 0 || s.Nlink == 0 {
		return false, ErrFile
	}
	if err := unix.Fstatat(dir, name, &s, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if err == unix.ENOENT {
			return false, nil
		}
		return false, ErrFile
	}
	if s.Uid != uint32(os.Getuid()) || s.Nlink != 1 || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Mode&0077 != 0 || s.Mode&0400 == 0 || s.Size > MaxSize+HeaderSize+16 || s.Size < 0 {
		return false, ErrFile
	}
	return true, nil
}

func (f *Files) Read(name string) ([]byte, error) {
	if !validName(name) {
		return nil, ErrFile
	}
	fd, e := unix.Openat(int(f.dir.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		if e == unix.ENOENT {
			return nil, os.ErrNotExist
		}
		return nil, ErrFile
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var s unix.Stat_t
	if unix.Fstat(fd, &s) != nil || s.Uid != uint32(os.Getuid()) || s.Nlink != 1 || s.Mode&unix.S_IFMT != unix.S_IFREG || s.Mode&0077 != 0 || s.Size > MaxSize+HeaderSize+16 || s.Size < 0 {
		return nil, ErrFile
	}
	b, e := io.ReadAll(io.LimitReader(file, MaxSize+HeaderSize+17))
	if e != nil || len(b) > MaxSize+HeaderSize+16 {
		return nil, ErrFile
	}
	return b, nil
}
func (f *Files) stage(s string) error {
	if f.fault != nil {
		return f.fault(s)
	}
	return nil
}
func (f *Files) Write(name string, data []byte, replace bool) error {
	if !validName(name) || len(data) > MaxSize+HeaderSize+16 || len(data) < HeaderSize+16 {
		return ErrFile
	}
	if _, e := f.Read(name); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	random := make([]byte, 16)
	if _, e := io.ReadFull(rand.Reader, random); e != nil {
		return ErrFile
	}
	temp := ".encrypted-" + hex.EncodeToString(random)
	dir := int(f.dir.Fd())
	fd, e := unix.Openat(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return ErrFile
	}
	file := os.NewFile(uintptr(fd), temp)
	defer func() { file.Close(); unix.Unlinkat(dir, temp, 0) }()
	if e = f.stage("before-write"); e != nil {
		return e
	}
	n, e := file.Write(data)
	if e != nil || n != len(data) {
		return ErrFile
	}
	if e = f.stage("after-write"); e != nil {
		return e
	}
	if e = file.Sync(); e != nil {
		return ErrFile
	}
	if e = f.stage("after-sync"); e != nil {
		return e
	}
	if e = file.Close(); e != nil {
		return ErrFile
	}
	flags := uint(0)
	if !replace {
		flags = unix.RENAME_NOREPLACE
	}
	if e = unix.Renameat2(dir, temp, dir, name, flags); e != nil {
		return ErrFile
	}
	if e = f.stage("after-rename"); e != nil {
		return e
	}
	if e = unix.Fsync(dir); e != nil {
		return ErrFile
	}
	return f.stage("after-directory-sync")
}

// Avoid an unused OS-specific import being mistaken for cross-platform support.
var _ = syscall.S_IRUSR

func openDirectory(path string) (int, error) {
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return 0, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == unix.ENOENT {
			if e := unix.Mkdirat(fd, part, 0700); e != nil && e != unix.EEXIST {
				unix.Close(fd)
				return 0, e
			}
			next, err = unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		unix.Close(fd)
		if err != nil {
			return 0, err
		}
		fd = next
	}
	return fd, nil
}
