// Package profilepath defines the on-disk product directory. Cryptographic,
// derivation and protocol identities are independent of its display name.
package profilepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const Name = "cryptowallet"
const legacyName = "localwallet"

var ErrStorage = errors.New("wallet storage unavailable")
var ErrConflict = fmt.Errorf("%w: both localwallet and cryptowallet exist; keep both intact and resolve their locations before restarting", ErrStorage)
var ErrBusy = fmt.Errorf("%w: close other wallet processes before migrating storage", ErrStorage)

func Profile(configuration, environment string) string {
	return filepath.Join(configuration, Name, environment)
}

// Prepare moves the entire legacy directory without reading/decrypting files.
// It never creates a fresh configuration/profile, merges trees or replaces a
// destination. Call before preferences or a signer can create the new tree.
func Prepare(configuration string) error {
	if !filepath.IsAbs(configuration) {
		return ErrStorage
	}
	parent, err := openDirectory(configuration)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return ErrStorage
	}
	defer unix.Close(parent)
	var st unix.Stat_t
	// Configuration parents may use Ubuntu's per-user group (0775), as the
	// existing vault traversal permits. Wallet roots/profiles below must remain
	// owner-only; never change permissions of a shared configuration directory.
	if unix.Fstat(parent, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&0002 != 0 {
		return ErrStorage
	}
	// Serialize new-version migrations without leaving a lock-file sidecar.
	if unix.Flock(parent, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return ErrBusy
	}
	defer unix.Flock(parent, unix.LOCK_UN)
	old, oldExists, err := directoryEntry(parent, legacyName)
	if err != nil {
		return err
	}
	_, newExists, err := directoryEntry(parent, Name)
	if err != nil {
		return err
	}
	if oldExists && newExists {
		return ErrConflict
	}
	if !oldExists {
		return nil
	}
	root, err := unix.Openat(parent, legacyName, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrStorage
	}
	defer unix.Close(root)
	if unix.Fstat(root, &st) != nil || st.Dev != old.Dev || st.Ino != old.Ino || st.Mode&0077 != 0 {
		return ErrStorage
	}
	if _, _, err := directoryEntry(root, "ui"); err != nil {
		return err
	}
	// Existing writers lock these same inodes. Hold both profiles until the
	// rename is durable; a busy/unsafe lock must never become a fresh wallet.
	var locks []int
	defer func() {
		for _, fd := range locks {
			unix.Close(fd)
		}
	}()
	for _, profile := range []string{"mainnet", "testnet"} {
		if _, exists, e := directoryEntry(root, profile); e != nil {
			return e
		} else if !exists {
			continue
		}
		dir, e := unix.Openat(root, profile, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return ErrStorage
		}
		fd, e := unix.Openat(dir, "writer.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
		unix.Close(dir)
		if e != nil {
			return ErrStorage
		}
		locks = append(locks, fd)
		if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Nlink != 1 {
			return ErrStorage
		}
		if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
			return ErrBusy
		}
	}
	return commitRename(parent, old)
}

func commitRename(parent int, old unix.Stat_t) error {
	var st unix.Stat_t
	// Pin traversal and recheck the source entry before a no-replace rename.
	if unix.Fstatat(parent, legacyName, &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Dev != old.Dev || st.Ino != old.Ino {
		return ErrStorage
	}
	if err := unix.Renameat2(parent, legacyName, parent, Name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return ErrConflict
		}
		return ErrStorage
	}
	if unix.Fsync(parent) != nil {
		return ErrStorage
	}
	return nil
}

func directoryEntry(parent int, name string) (unix.Stat_t, bool, error) {
	var st unix.Stat_t
	err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return st, false, nil
	}
	if err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uint32(os.Getuid()) || st.Mode&0077 != 0 {
		return st, false, ErrStorage
	}
	return st, true, nil
}

func openDirectory(path string) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
	}
	return fd, nil
}
