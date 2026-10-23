package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrExists identifies a no-overwrite conflict without disclosing a path.
var ErrExists = fmt.Errorf("%w: encrypted backup destination already exists", ErrFile)

// BackupFiles is an explicitly selected portable-backup directory. Unlike a
// live profile, it may be readable by other users, but its entries cannot be
// changed by them. The encrypted files themselves remain owner-only. It never
// creates a directory, changes permissions or leaves a profile lockfile behind.
type BackupFiles struct {
	files    *Files
	writable bool
}

func NewBackupFiles(path string, writable bool) (*BackupFiles, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrFile
	}
	fd, err := openExistingDirectory(path)
	if err != nil {
		return nil, ErrFile
	}
	f := &BackupFiles{files: &Files{dir: os.NewFile(uintptr(fd), path)}, writable: writable}
	if err := f.checkDirectory(); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (f *BackupFiles) Close() error { return f.files.Close() }

func (f *BackupFiles) checkDirectory() error {
	var s unix.Stat_t
	if unix.Fstat(int(f.files.dir.Fd()), &s) != nil || s.Uid != uint32(os.Getuid()) || s.Mode&unix.S_IFMT != unix.S_IFDIR || s.Mode&0022 != 0 || s.Mode&0500 != 0500 || s.Nlink == 0 || (f.writable && s.Mode&0200 == 0) {
		return ErrFile
	}
	return nil
}

func (f *BackupFiles) Read(name string) ([]byte, error) {
	if err := f.checkDirectory(); err != nil {
		return nil, err
	}
	return f.files.Read(name)
}

func (f *BackupFiles) Write(name string, data []byte) error {
	if !f.writable || !validName(name) || f.checkDirectory() != nil {
		return ErrFile
	}
	var s unix.Stat_t
	if err := unix.Fstatat(int(f.files.dir.Fd()), name, &s, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return ErrExists
	} else if err != unix.ENOENT {
		return ErrFile
	}
	return f.files.Write(name, data, false)
}

// Traverse existing directory components only, holding each verified FD until
// the next has opened. A file chooser result must not cause mkdir or follow a
// symlink anywhere in its parent path.
func openExistingDirectory(path string) (int, error) {
	fd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return 0, err
		}
		fd = next
	}
	return fd, nil
}
