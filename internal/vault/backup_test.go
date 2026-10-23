package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func backupDir(t *testing.T, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "selected-backup-folder")
	if err := os.Mkdir(dir, mode); err != nil {
		t.Fatal(err)
	}
	// Ignore the test runner's umask when asserting the ordinary 0755 case.
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTP032BackupInOrdinaryFolderIsPrivateAndHasNoProfileSideEffects(t *testing.T) {
	for _, mode := range []os.FileMode{0700, 0755} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := backupDir(t, mode)
			f, err := NewBackupFiles(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			ciphertext, err := Seal([]byte("public synthetic encrypted backup fixture"), testPassword, testParams)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Write("portable.lwv", ciphertext); err != nil {
				t.Fatal(err)
			}
			read, err := f.Read("portable.lwv")
			if err != nil || !bytes.Equal(read, ciphertext) {
				t.Fatalf("backup did not reopen identically: %v", err)
			}
			plain, err := Open(read, testPassword)
			defer clear(plain)
			if err != nil || !bytes.Equal(plain, []byte("public synthetic encrypted backup fixture")) {
				t.Fatalf("backup could not authenticate: %v", err)
			}
			info, err := os.Stat(filepath.Join(dir, "portable.lwv"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("backup mode was not 0600: %v", err)
			}
			info, err = os.Stat(dir)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("selected folder mode changed: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "portable.lwv" {
				t.Fatalf("backup polluted the folder: %v", err)
			}
			// A second operation needs no profile lock, while a live profile
			// still refuses an ordinary 0755 directory.
			second, err := NewBackupFiles(dir, false)
			if err != nil {
				t.Fatal(err)
			}
			second.Close()
			if mode == 0755 {
				if live, err := NewFiles(dir); err == nil {
					live.Close()
					t.Fatal("portable-backup change weakened live directory policy")
				}
			}
		})
	}
}

func TestTP031BackupDestinationNeverOverwritesOrFollowsLinks(t *testing.T) {
	dir := backupDir(t, 0755)
	f, err := NewBackupFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ciphertext, err := Seal([]byte("public synthetic destination protection"), testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.lwv")
	if err := os.WriteFile(outside, ciphertext, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exists.lwv"), []byte("unrelated existing file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "symlink.lwv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(dir, "hardlink.lwv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.lwv"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(dir, "fifo.lwv"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"exists.lwv", "symlink.lwv", "hardlink.lwv", "directory.lwv", "fifo.lwv"} {
		if err := f.Write(name, ciphertext); !errors.Is(err, ErrExists) {
			t.Fatalf("existing destination was not preserved: %s %v", name, err)
		}
		if _, err := f.Read(name); err == nil {
			t.Fatalf("unsafe restore source accepted: %s", name)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, "exists.lwv")); err != nil || string(got) != "unrelated existing file" {
		t.Fatalf("existing file altered: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || !bytes.Equal(got, ciphertext) {
		t.Fatalf("link target altered: %v", err)
	}
	for _, name := range []string{"", "..", "../outside.lwv", "/absolute.lwv", "nul\x00file"} {
		if err := f.Write(name, ciphertext); err == nil {
			t.Fatal("invalid destination name accepted")
		}
	}
	if err := f.Write("short.lwv", nil); err == nil {
		t.Fatal("unencrypted short backup accepted")
	}
	// An unrelated writer can appear between the initial absence check and
	// publication. RENAME_NOREPLACE must preserve its complete file.
	f.files.fault = func(stage string) error {
		if stage == "after-sync" {
			return os.WriteFile(filepath.Join(dir, "late.lwv"), ciphertext, 0600)
		}
		return nil
	}
	if err := f.Write("late.lwv", ciphertext); !errors.Is(err, ErrExists) {
		t.Fatalf("late destination replaced: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "late.lwv")); err != nil || !bytes.Equal(got, ciphertext) {
		t.Fatalf("late destination altered: %v", err)
	}
}

func TestTP031BackupFolderValidationAndPermissionRevocation(t *testing.T) {
	dir := backupDir(t, 0755)
	if os.Getuid() != 0 {
		if f, err := NewBackupFiles("/", true); err == nil {
			f.Close()
			t.Fatal("foreign-owned backup directory accepted")
		}
	}
	for _, path := range []string{"relative", filepath.Join(dir, "missing"), filepath.Join(dir, "bad\x00name")} {
		if f, err := NewBackupFiles(path, true); err == nil {
			f.Close()
			t.Fatal("unavailable backup folder accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file chooser result created a folder")
	}
	link := filepath.Join(t.TempDir(), "folder-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if f, err := NewBackupFiles(link, true); err == nil {
		f.Close()
		t.Fatal("symlink folder accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("public"), 0600); err != nil {
		t.Fatal(err)
	}
	if f, err := NewBackupFiles(filepath.Join(dir, "file"), false); err == nil {
		f.Close()
		t.Fatal("non-directory parent accepted")
	}
	for _, mode := range []os.FileMode{0777, 0775, 0757, 0600, 0500, 0300} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if f, err := NewBackupFiles(dir, true); err == nil {
			f.Close()
			t.Fatalf("unsafe/unwritable backup folder accepted: %o", mode)
		}
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	readOnly, err := NewBackupFiles(dir, false)
	if err != nil {
		t.Fatal("safe read-only restore folder rejected", err)
	}
	if err := readOnly.Write("refused.lwv", make([]byte, HeaderSize+16)); err == nil {
		t.Fatal("read-only backup handle permitted writing")
	}
	readOnly.Close()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := NewBackupFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Read("file"); err == nil {
		t.Fatal("folder permission revocation did not prevent reading")
	}
	if err := f.Write("refused.lwv", make([]byte, HeaderSize+16)); err == nil {
		t.Fatal("folder permission revocation did not prevent writing")
	}
	f.Close()
	if _, err := f.Read("file"); err == nil {
		t.Fatal("closed backup handle permitted reading")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

func TestTP031BackupDescriptorExhaustionFailsWithoutCreatingFiles(t *testing.T) {
	dir := backupDir(t, 0755)
	f, err := NewBackupFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ciphertext, err := Seal([]byte("public descriptor-exhaustion fixture"), testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	var previous unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &previous); err != nil {
		t.Fatal(err)
	}
	// No other test in this package runs in parallel. Restore the descriptor
	// limit before reporting errors or touching the test runner's output.
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &unix.Rlimit{Cur: 0, Max: previous.Max}); err != nil {
		t.Fatal(err)
	}
	defer unix.Setrlimit(unix.RLIMIT_NOFILE, &previous)
	backup, backupErr := NewBackupFiles(dir, true)
	live, liveErr := NewFiles(dir)
	writeErr := f.Write("not-published.lwv", ciphertext)
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &previous); err != nil {
		t.Fatal(err)
	}
	if backup != nil || !errors.Is(backupErr, ErrFile) || live != nil || !errors.Is(liveErr, ErrFile) || !errors.Is(writeErr, ErrFile) {
		t.Fatal("descriptor exhaustion did not fail closed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("descriptor exhaustion created profile or backup files: %v", err)
	}
}

func TestTP031BackupUsesAnchoredDirectoryAfterPathReplacement(t *testing.T) {
	dir := backupDir(t, 0755)
	f, err := NewBackupFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	moved := dir + "-moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	outside := backupDir(t, 0755)
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := Seal([]byte("public anchored backup"), testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Write("portable.lwv", ciphertext); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(moved, "portable.lwv")); err != nil || !bytes.Equal(got, ciphertext) {
		t.Fatal("backup lost original directory binding", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "portable.lwv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("backup followed replacement symlink")
	}
	if _, err := f.Read("portable.lwv"); err != nil {
		t.Fatal("anchored reopen failed", err)
	}
}
