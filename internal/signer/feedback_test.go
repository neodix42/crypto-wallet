package signer

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

type retainedInputUI struct {
	testUI
	values [][]byte
}

func (u *retainedInputUI) Input(string, ...string) ([][]byte, error) { return u.values, nil }

func TestTP020FailedSetupReturnsSpecificPublicFeedbackWithoutInputOrVault(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    [][]byte
		importing bool
		want      error
	}{
		{"short-password-123", input("123", "123"), false, ErrPasswordPolicy},
		{"long-password", input(strings.Repeat("a", 4097), strings.Repeat("a", 4097)), false, ErrPasswordPolicy},
		{"invalid-utf8-password", [][]byte{bytes.Repeat([]byte{0xff}, 12), bytes.Repeat([]byte{0xff}, 12)}, false, ErrPasswordPolicy},
		{"mismatched-password", input(syntheticPassword, "different public synthetic password"), false, ErrPasswordMismatch},
		{"phrase-123", input(syntheticPassword, syntheticPassword, "123", ""), true, ErrInvalidPhrase},
		{"checksum-invalid", input(syntheticPassword, syntheticPassword, strings.TrimSuffix(syntheticPhrase, "about")+"abandon", ""), true, ErrInvalidPhrase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, dir := newEngine(t, []model.Adapter{evm.New()})
			u := &retainedInputUI{values: tc.values}
			e.UI = u
			if err := e.Create("testnet", 12, tc.importing); !errors.Is(err, tc.want) || err.Error() != tc.want.Error() {
				t.Fatalf("missing safe validation feedback: %v", err)
			}
			for _, value := range u.values {
				if !bytes.Equal(value, make([]byte, len(value))) {
					t.Fatal("rejected native input remained in mutable buffer")
				}
			}
			if !e.Snapshot().Locked || e.data != nil || e.password != nil {
				t.Fatal("invalid setup left secret state active")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "writer.lock" {
				t.Fatalf("invalid setup wrote a wallet: %v", err)
			}
		})
	}
}

func TestTP032BackupOrdinaryDirectoryErrorsAreSpecificAndNonDestructive(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	dir := filepath.Join(t.TempDir(), "Documents")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "wallet-backup.lwv")
	if err := e.Backup(); err != nil || !e.Snapshot().RecoveryConfirmed {
		t.Fatalf("ordinary Documents directory failed: %v", err)
	}
	saved, err := os.ReadFile(u.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Backup(); !errors.Is(err, ErrBackupExists) {
		t.Fatalf("collision did not explain new filename requirement: %v", err)
	}
	if got, err := os.ReadFile(u.path); err != nil || !bytes.Equal(got, saved) {
		t.Fatalf("failed retry altered existing backup: %v", err)
	}
	if err := os.Symlink(u.path, filepath.Join(dir, "link.lwv")); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "link.lwv")
	if err := e.Backup(); !errors.Is(err, ErrBackupExists) {
		t.Fatalf("symlink destination did not fail closed: %v", err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "not-writable.lwv")
	if err := e.Backup(); !errors.Is(err, ErrBackupDestination) {
		t.Fatalf("unwritable folder did not explain destination requirement: %v", err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, strings.Repeat("x", 200))
	if err := e.Backup(); !errors.Is(err, ErrBackupWrite) {
		t.Fatalf("write failure did not explain retry: %v", err)
	}
	if e.Snapshot().Locked {
		t.Fatal("failed portable destination locked the intact live wallet")
	}
	if _, err := os.Stat(filepath.Join(dir, "writer.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("backup operation left a profile lock in Documents")
	}
}

func TestTP032RestoreFromReadOnlyOrdinaryDirectoryDoesNotWriteSource(t *testing.T) {
	source, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, source, u, "testnet")
	dir := filepath.Join(t.TempDir(), "Documents")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "portable.lwv")
	if err := source.Backup(); err != nil {
		t.Fatal(err)
	}
	expected := source.Snapshot().Accounts
	before, err := os.ReadFile(u.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	destination, v, _ := newEngine(t, []model.Adapter{evm.New()})
	v.path = u.path
	v.inputs = append(v.inputs, input(syntheticPassword))
	if err := destination.Restore(); err != nil || destination.Snapshot().Locked || !destination.Snapshot().RecoveryConfirmed {
		t.Fatalf("read-only backup restore failed: %v", err)
	}
	actual := destination.Snapshot().Accounts
	if len(actual) != len(expected) {
		t.Fatal("restore omitted accounts")
	}
	for i, account := range expected {
		if actual[i].ID != account.ID || actual[i].Address != account.Address || actual[i].Profile != account.Profile {
			t.Fatal("restore changed recovery identity")
		}
	}
	if got, err := os.ReadFile(u.path); err != nil || !bytes.Equal(got, before) {
		t.Fatalf("restore altered source backup: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0500 {
		t.Fatalf("restore altered source folder permissions: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "portable.lwv" {
		t.Fatalf("restore polluted source folder: %v", err)
	}
	other, otherUI, _ := newEngine(t, []model.Adapter{evm.New()})
	otherUI.path = filepath.Join(dir, "missing.lwv")
	if err := other.Restore(); !errors.Is(err, ErrRestoreSource) {
		t.Fatalf("invalid source lacked safe feedback: %v", err)
	}
}

func TestTP032BackupDerivationFailureDoesNotClaimVerifiedRecovery(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	e.data.RecoveryConfirmed = false
	e.data.Accounts[0].Address = "0x1111111111111111111111111111111111111111"
	dir := filepath.Join(t.TempDir(), "Documents")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "must-not-mark-verified.lwv")
	if err := e.Backup(); !errors.Is(err, ErrBackupVerification) || e.Snapshot().RecoveryConfirmed {
		t.Fatalf("unverified descriptors were marked as recovered: %v", err)
	}
}
