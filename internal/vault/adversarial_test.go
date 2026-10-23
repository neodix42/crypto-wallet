package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func metadataOperation(d *Data, state string) journal.Operation {
	p := model.Plan{ID: "public-fixture-operation", Account: d.Accounts[0], Asset: "fixture-native", Recipient: "public-destination", Amount: "1", MaxFee: "1", FeeAsset: "fixture-native", Nonce: "0", Expires: 2_000_000_000, Payload: json.RawMessage(`{}`)}
	o := journal.Operation{Plan: p, Digest: journal.Digest(p), State: state}
	switch state {
	case "PREPARED", "AWAITING_CONFIRMATION", "CANCELLED":
	default:
		o.Signed = &model.Signed{Identity: "public-fixture-transaction", Bytes: []byte{1, 2, 3}}
	}
	return o
}
func TestTP030AdversarialRecoveryMetadata(t *testing.T) {
	mutants := map[string]func(*Data){
		"generation": func(d *Data) { d.Generation = 0 }, "wallet-id": func(d *Data) { d.WalletID = "short" }, "scheme": func(d *Data) { d.Scheme = "native-ton" }, "environment": func(d *Data) { d.Environment = "unknown" }, "passphrase-bound": func(d *Data) { d.Passphrase = make([]byte, 4097) }, "entropy-length": func(d *Data) { d.Entropy = make([]byte, 17) }, "accounts-bound": func(d *Data) { d.Accounts = make([]model.Account, 10001) }, "operations-bound": func(d *Data) { d.Operations = make([]journal.Operation, 10001) },
		"empty-account-id": func(d *Data) { d.Accounts[0].ID = "" }, "duplicate-account-id": func(d *Data) { d.Accounts = append(d.Accounts, d.Accounts[0]) }, "exclusive-account-bound": func(d *Data) { d.Accounts[0].Index = derivation.MaxIndex }, "profile": func(d *Data) { d.Accounts[0].Profile = "" }, "network": func(d *Data) { d.Accounts[0].Network = "" }, "address": func(d *Data) { d.Accounts[0].Address = "" },
		"empty-operation-id": func(d *Data) {
			o := metadataOperation(d, "PREPARED")
			o.Plan.ID = ""
			o.Digest = journal.Digest(o.Plan)
			d.Operations = []journal.Operation{o}
		}, "duplicate-operation": func(d *Data) { o := metadataOperation(d, "PREPARED"); d.Operations = []journal.Operation{o, o} }, "foreign-operation-account": func(d *Data) {
			o := metadataOperation(d, "PREPARED")
			o.Plan.Account.ID = "unknown"
			o.Digest = journal.Digest(o.Plan)
			d.Operations = []journal.Operation{o}
		}, "changed-plan-digest": func(d *Data) {
			o := metadataOperation(d, "PREPARED")
			o.Plan.Amount = "2"
			d.Operations = []journal.Operation{o}
		}, "unknown-lifecycle": func(d *Data) { d.Operations = []journal.Operation{metadataOperation(d, "invented")} },
		"empty-signed-identity": func(d *Data) {
			o := metadataOperation(d, "SUBMITTED")
			o.Signed.Identity = ""
			d.Operations = []journal.Operation{o}
		}, "empty-signed-bytes": func(d *Data) {
			o := metadataOperation(d, "SUBMITTED")
			o.Signed.Bytes = nil
			d.Operations = []journal.Operation{o}
		}, "oversized-signed-bytes": func(d *Data) {
			o := metadataOperation(d, "SUBMITTED")
			o.Signed.Bytes = make([]byte, (1<<20)+1)
			d.Operations = []journal.Operation{o}
		}, "unsigned-submitted-state": func(d *Data) {
			o := metadataOperation(d, "SUBMITTED")
			o.Signed = nil
			d.Operations = []journal.Operation{o}
		}, "preapproved-signature": func(d *Data) {
			o := metadataOperation(d, "SUBMITTED")
			o.State = "PREPARED"
			d.Operations = []journal.Operation{o}
		},
	}
	for name, mutate := range mutants {
		t.Run(name, func(t *testing.T) {
			d := synthetic()
			mutate(d)
			if _, e := d.Encode(); !errors.Is(e, ErrFormat) {
				t.Fatal("unsafe metadata encoded", e)
			}
			raw, e := json.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			decoded, e := Decode(raw)
			if e == nil || decoded != nil {
				t.Fatal("unsafe metadata released partial decoded vault", e)
			}
		})
	}
	for _, n := range []int{16, 20, 24, 28, 32} {
		d := synthetic()
		d.Entropy = make([]byte, n)
		d.Passphrase = []byte("public synthetic BIP39 passphrase")
		raw, e := d.Encode()
		if e != nil {
			t.Fatal(e)
		}
		restored, e := Decode(raw)
		if e != nil || VerifyRecovery(d, restored) != nil {
			t.Fatal("supported entropy length lost during recovery", n, e)
		}
		restored.Destroy()
	}
	for _, state := range []string{"PREPARED", "AWAITING_CONFIRMATION", "CANCELLED", "SIGNED_NOT_SUBMITTED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "CONFIRMED", "FAILED", "REPLACED", "REORGED"} {
		d := synthetic()
		d.Operations = []journal.Operation{metadataOperation(d, state)}
		raw, e := d.Encode()
		if e != nil {
			t.Fatal("valid journal lifecycle rejected", state, e)
		}
		restored, e := Decode(raw)
		if e != nil || VerifyRecovery(d, restored) != nil {
			t.Fatal("journal lost during recovery", state, e)
		}
	}
	for _, raw := range []string{`{"version":1,"unexpected":true}`, `{"version":1} trailing`, `{"version":1,"accounts":[{"id":"a","id":"b"}]}`, `{"entropy":"not-base64"}`} {
		if d, e := Decode([]byte(raw)); e == nil || d != nil {
			t.Fatal("malformed recovery parser released state")
		}
	}
}
func TestTP032EveryRecoveryIdentityIsCompared(t *testing.T) {
	for name, mutate := range map[string]func(*Data){"birthday": func(d *Data) { d.Birthday++ }, "passphrase": func(d *Data) { d.Passphrase = []byte("public synthetic imported passphrase") }, "environment": func(d *Data) { d.Environment = "mainnet" }, "wallet-id": func(d *Data) { d.WalletID = strings.Repeat("a", 32) }, "profile": func(d *Data) { d.Accounts[0].Profile = "another-version" }, "account-index": func(d *Data) { d.Accounts[0].Index++ }, "discovery-metadata": func(d *Data) { d.Accounts[0].Parameters = json.RawMessage(`{"birthdayHeight":1}`) }, "journal": func(d *Data) { d.Operations = []journal.Operation{metadataOperation(d, "UNKNOWN")} }, "endpoint-credential": func(d *Data) {
		d.Endpoints = map[string]Endpoint{"decred-simnet": {URL: "http://127.0.0.1:1", Password: "public synthetic fixture only"}}
	}} {
		t.Run(name, func(t *testing.T) {
			live, backup := synthetic(), synthetic()
			mutate(backup)
			if VerifyRecovery(live, backup) == nil {
				t.Fatal("backup verification ignored changed identity field")
			}
		})
	}
}
func TestTP030PasswordsAreNotNormalizedAndSizesBounded(t *testing.T) {
	composed := []byte("public synthetic caf\u00e9 password")
	decomposed := []byte("public synthetic cafe\u0301 password")
	b, e := Seal([]byte("public fixture plaintext"), composed, testParams)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := Open(b, decomposed); !errors.Is(e, ErrUnlock) || out != nil {
		t.Fatal("storage password unexpectedly normalized", e)
	}
	out, e := Open(b, composed)
	if e != nil || string(out) != "public fixture plaintext" {
		t.Fatal("exact UTF-8 password rejected", e)
	}
	clear(out)
	if _, e := Seal(make([]byte, MaxSize+1), testPassword, testParams); !errors.Is(e, ErrFormat) {
		t.Fatal("plaintext allocation limit ignored")
	}
	if _, e := Open(make([]byte, MaxSize+HeaderSize+17), testPassword); !errors.Is(e, ErrFormat) {
		t.Fatal("ciphertext allocation limit ignored")
	}
	for _, n := range []int{0, 11, 4097} {
		if PasswordValid(make([]byte, n)) {
			t.Fatal("password boundary accepted", n)
		}
	}
	for _, n := range []int{12, 4096} {
		if !PasswordValid(make([]byte, n)) {
			t.Fatal("valid password boundary rejected", n)
		}
	}
}
func TestTP031NoSymlinkDirectoryHierarchy(t *testing.T) {
	if f, e := NewFiles("/"); e == nil {
		f.Close()
		t.Fatal("filesystem root accepted as private wallet directory")
	}
	base := privateDir(t)
	target := filepath.Join(base, "target")
	if e := os.Mkdir(target, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(base, "parent-link")
	if e := os.Symlink(target, link); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{link, filepath.Join(link, "new-child")} {
		f, e := NewFiles(path)
		if e == nil {
			f.Close()
			t.Fatal("symlink directory hierarchy accepted")
		}
	}
	if _, e := os.Stat(filepath.Join(target, "new-child")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("rejected hierarchy created an outside directory", e)
	}
	file := filepath.Join(base, "regular-parent")
	if e := os.WriteFile(file, []byte("public fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if f, e := NewFiles(filepath.Join(file, "child")); e == nil {
		f.Close()
		t.Fatal("regular parent treated as directory")
	}
	if f, e := NewFiles(filepath.Join(base, "missing", "nested")); e != nil {
		t.Fatal("safe absent hierarchy could not be created", e)
	} else {
		f.Close()
	}
}
func TestTP031LockObjectAndFileTypeRejections(t *testing.T) {
	for _, kind := range []string{"world-readable-lock", "hardlinked-lock", "symlink-lock", "fifo-lock"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateDir(t)
			lock := filepath.Join(dir, "writer.lock")
			switch kind {
			case "world-readable-lock":
				if e := os.WriteFile(lock, nil, 0644); e != nil {
					t.Fatal(e)
				}
			case "hardlinked-lock":
				if e := os.WriteFile(lock, nil, 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Link(lock, filepath.Join(dir, "other")); e != nil {
					t.Fatal(e)
				}
			case "symlink-lock":
				if e := os.WriteFile(filepath.Join(dir, "other"), nil, 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink("other", lock); e != nil {
					t.Fatal(e)
				}
			case "fifo-lock":
				if e := unix.Mkfifo(lock, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if f, e := NewFiles(dir); e == nil {
				f.Close()
				t.Fatal("unsafe writer lock accepted")
			}
		})
	}
	dir := privateDir(t)
	f, e := NewFiles(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e := os.Mkdir(filepath.Join(dir, "directory-object"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Read("directory-object"); !errors.Is(e, ErrFile) {
		t.Fatal("directory read as vault", e)
	}
	path := filepath.Join(dir, "public-readable")
	if e := os.WriteFile(path, []byte("public fixture"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Read("public-readable"); !errors.Is(e, ErrFile) {
		t.Fatal("unsafe file permissions accepted", e)
	}
	big, e := os.OpenFile(filepath.Join(dir, "oversized"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = big.Truncate(MaxSize + HeaderSize + 17); e != nil {
		t.Fatal(e)
	}
	big.Close()
	if _, e := f.Read("oversized"); !errors.Is(e, ErrFile) {
		t.Fatal("oversized sparse file allocated/read", e)
	}
	if e = f.Write(strings.Repeat("x", 200), make([]byte, HeaderSize+16), false); !errors.Is(e, ErrFile) {
		t.Fatal("oversized filename accepted", e)
	}
}
func TestTP031ReadFIFOIsBounded(t *testing.T) {
	if os.Getenv("LOCALWALLET_FIFO_TEST") == "1" {
		dir := privateDir(t)
		f, e := NewFiles(dir)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		if e = unix.Mkfifo(filepath.Join(dir, "vault.lwv"), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = f.Read("vault.lwv"); !errors.Is(e, ErrFile) {
			t.Fatal("FIFO vault was accepted", e)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTP031ReadFIFOIsBounded$")
	cmd.Env = append(os.Environ(), "LOCALWALLET_FIFO_TEST=1")
	b, e := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("FIFO input blocked before regular-file validation")
	}
	if e != nil {
		t.Fatalf("FIFO rejection subprocess: %v %s", e, b)
	}
}
func TestTP031DirectoryFDResistsPathReplacement(t *testing.T) {
	base := privateDir(t)
	original := filepath.Join(base, "original")
	f, e := NewFiles(original)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	moved := filepath.Join(base, "moved")
	if e = os.Rename(original, moved); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(base, "replacement")
	if e = os.Mkdir(outside, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, original); e != nil {
		t.Fatal(e)
	}
	encrypted, e := Seal([]byte("public fixture in authenticated vault"), testPassword, testParams)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Write("vault.lwv", encrypted, false); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(moved, "vault.lwv"))
	if e != nil || !bytes.Equal(got, encrypted) {
		t.Fatal("held directory descriptor lost atomic destination", e)
	}
	if _, e = os.Stat(filepath.Join(outside, "vault.lwv")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("path replacement redirected write", e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Read("vault.lwv"); !errors.Is(e, ErrFile) {
		t.Fatal("closed directory remained readable", e)
	}
	if e = f.Write("vault.lwv", encrypted, true); !errors.Is(e, ErrFile) {
		t.Fatal("closed directory remained writable", e)
	}
}

func TestTP031PermissionRevocationDeniesTemporaryWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("NOT_APPLICABLE: root bypasses discretionary write permission checks")
	}
	dir := privateDir(t)
	f, e := NewFiles(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	b, e := Seal([]byte("public synthetic ciphertext-only write"), testPassword, testParams)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0500); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(dir, 0700)
	if created, e := NewFiles(filepath.Join(dir, "unavailable-child")); e == nil {
		created.Close()
		t.Fatal("new profile created under a directory with revoked write permission")
	}
	if e = f.Write("vault.lwv", b, false); !errors.Is(e, ErrFile) {
		t.Fatal("revoked directory permissions did not fail closed", e)
	}
	if _, e = os.Stat(filepath.Join(dir, "vault.lwv")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("permission failure created target", e)
	}
}
func TestTP031ActualShortWritePreservesOldGeneration(t *testing.T) {
	if os.Getenv("LOCALWALLET_SHORT_WRITE_TEST") == "1" {
		dir := privateDir(t)
		f, e := NewFiles(dir)
		if e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		old, e := Seal([]byte("public synthetic old generation"), testPassword, testParams)
		if e != nil {
			t.Fatal(e)
		}
		next, e := Seal([]byte("public synthetic next generation"), testPassword, testParams)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Write("vault.lwv", old, false); e != nil {
			t.Fatal(e)
		}
		var previous unix.Rlimit
		if e = unix.Getrlimit(unix.RLIMIT_FSIZE, &previous); e != nil {
			t.Fatal(e)
		}
		defer unix.Setrlimit(unix.RLIMIT_FSIZE, &previous)
		if e = unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 32, Max: previous.Max}); e != nil {
			t.Fatal(e)
		}
		if e = f.Write("vault.lwv", next, true); !errors.Is(e, ErrFile) {
			t.Fatal("actual truncated filesystem write was accepted", e)
		}
		got, e := f.Read("vault.lwv")
		if e != nil || !bytes.Equal(got, old) {
			t.Fatal("short write damaged previous authenticated generation", e)
		}
		entries, e := os.ReadDir(dir)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".encrypted-") {
				t.Fatal("short write left transaction temporary file")
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTP031ActualShortWritePreservesOldGeneration$")
	cmd.Env = append(os.Environ(), "LOCALWALLET_SHORT_WRITE_TEST=1")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("short-write subprocess: %v %s", e, b)
	}
}
