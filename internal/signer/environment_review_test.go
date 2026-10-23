package signer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

func TestWrongProfileRecoveryDoesNotWriteVaultOrRetainPlaintext(t *testing.T) {
	source, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, source, u, "testnet")
	backupDirectory := filepath.Join(t.TempDir(), "private-backups")
	if err := os.Mkdir(backupDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(backupDirectory, "public-synthetic-testnet-backup.lwv")
	if err := source.Backup(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(u.path)
	if err != nil {
		t.Fatal(err)
	}
	destination, v, _ := newEngine(t, []model.Adapter{evm.New()})
	destination.Environment = "mainnet"
	v.path = u.path
	v.inputs = append(v.inputs, input(syntheticPassword))
	if err := destination.Restore(); err == nil || !destination.Snapshot().Locked || destination.data != nil || destination.password != nil {
		t.Fatal("wrong environment backup became an unlocked profile", err)
	}
	if _, err := destination.Files.Read("vault.lwv"); !os.IsNotExist(err) {
		t.Fatal("wrong environment restore wrote destination vault", err)
	}
	if after, err := os.ReadFile(u.path); err != nil || !bytes.Equal(backup, after) {
		t.Fatal("rejected restore modified portable backup", err)
	}
	// Matching-environment recovery still uses the same authenticated backup.
	destination.Environment = "testnet"
	v.inputs = append(v.inputs, input(syntheticPassword))
	if err := destination.Restore(); err != nil || destination.Snapshot().Locked {
		t.Fatal("matching environment recovery failed", err)
	}
	destination.Lock()
	destination.Environment = "mainnet"
	v.inputs = append(v.inputs, input(syntheticPassword))
	if err := destination.Unlock(); err == nil || !destination.Snapshot().Locked {
		t.Fatal("wrong profile unlock accepted", err)
	}
}

func TestWrongProfileCreationNeverWritesVault(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	e.Environment = "mainnet"
	u.inputs = append(u.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
	if err := e.Create("testnet", 12, true); err == nil || !e.Snapshot().Locked {
		t.Fatal("wrong environment import accepted", err)
	}
	if _, err := e.Files.Read("vault.lwv"); !os.IsNotExist(err) {
		t.Fatal("wrong environment import wrote a vault", err)
	}
}
