package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestVaultPresenceMetadataFailsClosed(t *testing.T) {
	dir := privateDir(t)
	f, err := NewFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if exists, err := f.Exists("vault.lwv"); err != nil || exists {
		t.Fatalf("new profile: %v %v", exists, err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if exists, err := f.Exists("vault.lwv"); exists || !errors.Is(err, ErrFile) {
		t.Fatalf("directory became unsafe: %v %v", exists, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if err := os.Chmod(dir, 0600); err != nil {
			t.Fatal(err)
		}
		if exists, err := f.Exists("vault.lwv"); exists || !errors.Is(err, ErrFile) {
			t.Fatalf("directory search permission revoked: %v %v", exists, err)
		}
		if err := os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"", ".", "..", "../vault.lwv", "/tmp/vault.lwv", "nul\x00file"} {
		if _, err := f.Exists(name); !errors.Is(err, ErrFile) {
			t.Fatalf("unsafe name %q: %v", name, err)
		}
	}
	// Empty/corrupt ciphertext still identifies an existing wallet. Presence
	// neither parses the envelope nor invokes its password/KDF path.
	for _, content := range [][]byte{nil, []byte("not an encrypted envelope")} {
		if err := os.WriteFile(filepath.Join(dir, "vault.lwv"), content, 0600); err != nil {
			t.Fatal(err)
		}
		if exists, err := f.Exists("vault.lwv"); err != nil || !exists {
			t.Fatalf("existing ciphertext: %v %v", exists, err)
		}
	}
	if err := os.Remove(filepath.Join(dir, "vault.lwv")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		make func(string) error
	}{
		{"symlink", func(path string) error { return os.Symlink("absent-target", path) }},
		{"directory", func(path string) error { return os.Mkdir(path, 0700) }},
		{"FIFO", func(path string) error { return unix.Mkfifo(path, 0600) }},
		{"shared permissions", func(path string) error { return os.WriteFile(path, nil, 0644) }},
		{"unreadable", func(path string) error { return os.WriteFile(path, nil, 0200) }},
		{"hardlink", func(path string) error {
			target := filepath.Join(dir, "hardlink-target")
			if err := os.WriteFile(target, nil, 0600); err != nil {
				return err
			}
			return os.Link(target, path)
		}},
		{"oversized", func(path string) error {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			defer file.Close()
			return file.Truncate(MaxSize + HeaderSize + 17)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "vault.lwv")
			if err := tc.make(path); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(path)
			if exists, err := f.Exists("vault.lwv"); exists || !errors.Is(err, ErrFile) {
				t.Fatalf("unsafe entry: %v %v", exists, err)
			}
		})
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if exists, err := f.Exists("vault.lwv"); exists || !errors.Is(err, ErrFile) {
		t.Fatalf("closed descriptor: %v %v", exists, err)
	}
	for _, missing := range []*Files{nil, {}} {
		if exists, err := missing.Exists("vault.lwv"); exists || !errors.Is(err, ErrFile) {
			t.Fatalf("missing descriptor: %v %v", exists, err)
		}
	}
}

func TestVaultPresenceUsesVerifiedDirectoryDescriptor(t *testing.T) {
	dir := privateDir(t)
	f, err := NewFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	moved := dir + "-moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault.lwv"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if exists, err := f.Exists("vault.lwv"); exists || err != nil {
		t.Fatalf("followed replacement path: %v %v", exists, err)
	}
	if err := os.WriteFile(filepath.Join(moved, "vault.lwv"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if exists, err := f.Exists("vault.lwv"); !exists || err != nil {
		t.Fatalf("lost anchored entry: %v %v", exists, err)
	}
}

func TestInitialVaultWriteAtomicallyPreservesLatePrimary(t *testing.T) {
	dir := privateDir(t)
	f, err := NewFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	original, err := Seal([]byte("public synthetic original root"), testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Seal([]byte("public synthetic unrelated new root"), testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	introduced := false
	f.fault = func(stage string) error {
		if stage == "after-sync" {
			introduced = true
			// This is after Write's presence read and staging fsync, directly
			// before the final RENAME_NOREPLACE publication.
			return os.WriteFile(filepath.Join(dir, "vault.lwv"), original, 0600)
		}
		return nil
	}
	if err := f.Write("vault.lwv", candidate, false); !errors.Is(err, ErrFile) || !introduced {
		t.Fatalf("late primary collision was not refused: %v", err)
	}
	f.fault = nil
	if after, err := f.Read("vault.lwv"); err != nil || !bytes.Equal(after, original) {
		t.Fatalf("atomic initial commit replaced the original root: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "vault.lwv" && entry.Name() != "writer.lock" {
			t.Fatalf("collision retained a temporary candidate: %s", entry.Name())
		}
	}
}
