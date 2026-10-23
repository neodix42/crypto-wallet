package vault

import (
	"bytes"
	"errors"
	"localwallet/internal/model"
	"os"
	"path/filepath"
	"testing"
)

var testParams = Parameters{64 * 1024, 3, 4}
var testPassword = []byte("synthetic-only storage password")

func synthetic() *Data {
	return &Data{Version: 1, WalletID: "0123456789abcdef0123456789abcdef", Generation: 1, Scheme: "bip39-english-v1", Entropy: make([]byte, 16), Environment: "testnet", Accounts: []model.Account{{ID: "a", Family: "near", Profile: "near-bip39-slip10-v1", Network: "near-testnet", Address: "public", PublicKey: "public"}}}
}
func TestTP030AuthenticatedEnvelope(t *testing.T) {
	plain := []byte("synthetic-only private vault payload")
	b, e := Seal(plain, testPassword, testParams)
	if e != nil {
		t.Fatal(e)
	}
	b2, e := Seal(plain, testPassword, testParams)
	if e != nil || bytes.Equal(b, b2) {
		t.Fatal("fresh random salt/nonce")
	}
	out, e := Open(b, testPassword)
	if e != nil || !bytes.Equal(out, plain) {
		t.Fatal("round trip")
	}
	clear(out)
	if bytes.Contains(b, plain) || bytes.Contains(b, testPassword) {
		t.Fatal("plaintext leak")
	}
	if p, e := Open(b, []byte("wrong")); e != ErrUnlock || p != nil {
		t.Fatal("authentication fail closed")
	}
	for _, i := range []int{0, 8, 10, 11, 12, 16, 20, 21, 24, 40, 64, 72, len(b) - 1} {
		tampered := bytes.Clone(b)
		tampered[i] ^= 1
		if p, e := Open(tampered, testPassword); e == nil || p != nil {
			t.Fatalf("tamper byte %d", i)
		}
	}
	for _, n := range []int{0, 8, 24, 40, 64, 71, 72, len(b) - 1} {
		if _, e := Open(b[:n], testPassword); e == nil {
			t.Fatal("truncation")
		}
	}
	if _, e := Open(append(bytes.Clone(b), 0), testPassword); e == nil {
		t.Fatal("trailing data")
	}
	for _, p := range []Parameters{{1024, 3, 4}, {1, 3, 4}, {513 * 1024, 3, 4}, {64 * 1024, 2, 4}, {64 * 1024, 7, 4}, {64 * 1024, 3, 1}, {64*1024 + 1, 3, 4}} {
		if _, e := Seal(plain, testPassword, p); e == nil {
			t.Fatal("KDF bound")
		}
	}
	if _, e := Seal(nil, testPassword, testParams); e == nil {
		t.Fatal("empty")
	}
	if _, e := Seal(plain, []byte("short"), testParams); e == nil {
		t.Fatal("password policy")
	}
	if _, e := seal(bytes.NewReader([]byte{0}), plain, testPassword, testParams); e == nil {
		t.Fatal("entropy failure")
	}
	if _, e := Open(b, make([]byte, 4097)); e == nil {
		t.Fatal("password allocation bound")
	}
}
func privateDir(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "private")
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestTP031AtomicFaultsAndPermissions(t *testing.T) {
	dir := privateDir(t)
	f, e := NewFiles(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if _, e := NewFiles(dir); e == nil {
		t.Fatal("concurrent launch")
	}
	a, _ := Seal([]byte("old generation"), testPassword, testParams)
	b, _ := Seal([]byte("new generation"), testPassword, testParams)
	if e = f.Write("vault.lwv", a, false); e != nil {
		t.Fatal(e)
	}
	if e = f.Write("vault.lwv", b, false); e == nil {
		t.Fatal("collision")
	}
	for _, stage := range []string{"before-write", "after-write", "after-sync", "after-rename", "after-directory-sync"} {
		f.fault = nil
		if e = f.Write("vault.lwv", a, true); e != nil {
			t.Fatal(e)
		}
		f.fault = func(s string) error {
			if s == stage {
				return errors.New("injected crash")
			}
			return nil
		}
		if e = f.Write("vault.lwv", b, true); e == nil {
			t.Fatal("fault not injected")
		}
		out, e := f.Read("vault.lwv")
		if e != nil || (!bytes.Equal(out, a) && !bytes.Equal(out, b)) {
			t.Fatal("torn generation")
		}
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if len(entry.Name()) >= 11 && entry.Name()[:11] == ".encrypted-" {
				t.Fatal("temporary residue")
			}
		}
	}
	f.fault = nil
	for _, name := range []string{"", "..", "/tmp/out", "../escape", "x\x00y"} {
		if f.Write(name, a, true) == nil {
			t.Fatal("unsafe path")
		}
		if _, e := f.Read(name); e == nil {
			t.Fatal("unsafe read")
		}
	}
	if e = os.Symlink("vault.lwv", filepath.Join(dir, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Read("link"); e == nil {
		t.Fatal("symlink")
	}
	if f.Write("link", a, true) == nil {
		t.Fatal("symlink write")
	}
	if e = os.Link(filepath.Join(dir, "vault.lwv"), filepath.Join(dir, "hard")); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Read("hard"); e == nil {
		t.Fatal("hardlink")
	}
	if f.Write("short", []byte{1}, true) == nil {
		t.Fatal("size")
	}
	if _, e = f.Read("absent"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("not found")
	}
	unsafeDir := filepath.Join(t.TempDir(), "unsafe")
	os.Mkdir(unsafeDir, 0755)
	if _, e = NewFiles(unsafeDir); e == nil {
		t.Fatal("unsafe permissions")
	}
	if _, e = NewFiles("relative"); e == nil {
		t.Fatal("relative path")
	}
}
func TestTP032PortableRecoveryAndRotation(t *testing.T) {
	d := synthetic()
	plain, e := d.Encode()
	if e != nil {
		t.Fatal(e)
	}
	old, e := Seal(plain, testPassword, testParams)
	if e != nil {
		t.Fatal(e)
	}
	newPassword := []byte("synthetic replacement password")
	rotated, e := Seal(plain, newPassword, testParams)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Open(rotated, testPassword); e != ErrUnlock {
		t.Fatal("old password accepted")
	}
	oldPlain, e := Open(old, testPassword)
	if e != nil {
		t.Fatal("old backup lost")
	}
	restored, e := Decode(oldPlain)
	if e != nil || VerifyRecovery(d, restored) != nil {
		t.Fatal("recovery changed")
	}
	restored.Entropy[0] = 1
	if VerifyRecovery(d, restored) == nil {
		t.Fatal("root comparison")
	}
	restored.Entropy[0] = 0
	restored.Accounts[0].Address = "changed"
	if VerifyRecovery(d, restored) == nil {
		t.Fatal("descriptor comparison")
	}
	restored.Destroy()
	if restored.Entropy != nil || restored.Passphrase != nil {
		t.Fatal("destroy")
	}
	bad := synthetic()
	bad.Version = 2
	if _, e := bad.Encode(); e == nil {
		t.Fatal("unknown schema")
	}
	if _, e := Decode([]byte(`{"version":1,"version":1}`)); e == nil {
		t.Fatal("duplicate field")
	}
}
func FuzzTP012Envelope(f *testing.F) {
	f.Add([]byte("invalid vault"))
	f.Add(make([]byte, 72))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) >= 72 && string(b[:8]) == "LWVLT001" {
			return
		}
		_, _ = Open(b, []byte("public synthetic fuzz password"))
	})
}
