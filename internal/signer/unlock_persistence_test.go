package signer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func TestUnlockUnchangedVaultPreservesBothEncryptedGenerations(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	createImported(t, e, ui, "mainnet")
	if err := e.AddAccount(1); err != nil {
		t.Fatal(err)
	}
	generation, accountCount := e.data.Generation, len(e.data.Accounts)
	before := make(map[string][]byte)
	for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
		var err error
		before[name], err = e.Files.Read(name)
		if err != nil {
			t.Fatal(err)
		}
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input("incorrect-synthetic-password"))
	if err := e.Unlock(); err == nil || !e.Snapshot().Locked {
		t.Fatal("unchanged-vault path bypassed password authentication")
	}
	for range 3 {
		ui.inputs = append(ui.inputs, input(syntheticPassword))
		if err := e.Unlock(); err != nil {
			t.Fatal(err)
		}
		if e.data.Generation != generation || len(e.data.Accounts) != accountCount || !e.data.RecoveryConfirmed {
			t.Fatal("read-only unlock changed authenticated wallet state")
		}
		for name, original := range before {
			after, err := e.Files.Read(name)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("unchanged unlock rewrote an encrypted generation", name)
			}
		}
		e.Lock()
	}
}

func TestUnlockRecoveryTransitionsRemainDurable(t *testing.T) {
	for _, mode := range []string{"PREPARED", "AWAITING_CONFIRMATION", "SUBMITTING", "legacy", "legacy+PREPARED"} {
		t.Run(mode, func(t *testing.T) {
			e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
			createImported(t, e, ui, "testnet")
			account := findAccount(t, e, "ethereum-sepolia")
			var signed *model.Signed
			if mode != "legacy" {
				plan := evmPlan(account)
				if err := e.Prepare(plan); err != nil {
					t.Fatal(err)
				}
				if mode == "AWAITING_CONFIRMATION" {
					e.data.Operations[0].State = mode
					if err := e.persist(); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "SUBMITTING" {
					ui.inputs = append(ui.inputs, input(syntheticPassword))
					result, err := e.Sign(plan.ID)
					if err != nil {
						t.Fatal(err)
					}
					signed = &result
					if err = e.Update(plan.ID, "SUBMITTING", nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "legacy" || mode == "legacy+PREPARED" {
				e.data.DeprecatedBackupVerified = true
				if err := e.persist(); err != nil {
					t.Fatal(err)
				}
			}
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			generation := e.data.Generation
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			after, err := e.Files.Read("vault.lwv")
			previous, previousErr := e.Files.Read("vault.previous.lwv")
			if err != nil || previousErr != nil || bytes.Equal(before, after) || !bytes.Equal(previous, before) || e.data.Generation != generation+1 {
				t.Fatal("recovery transitions were not committed as one new encrypted generation")
			}
			plain, err := vault.Open(after, []byte(syntheticPassword))
			if err != nil {
				t.Fatal(err)
			}
			defer clear(plain)
			disk, err := vault.Decode(plain)
			if err != nil {
				t.Fatal(err)
			}
			defer disk.Destroy()
			if disk.DeprecatedBackupVerified {
				t.Fatal("retired backup authority was retained")
			}
			if mode != "legacy" {
				want := "CANCELLED"
				if signed != nil {
					want = "UNKNOWN"
				}
				if len(disk.Operations) != 1 || disk.Operations[0].State != want {
					t.Fatal("recovery state was not authenticated on disk")
				}
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			repeated, err := e.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(after, repeated) || e.data.Generation != generation+1 {
				t.Fatal("completed recovery was rewritten on the next unlock")
			}
			if signed != nil {
				op := e.data.Operations[0]
				if op.Signed == nil || op.Signed.Identity != signed.Identity || !bytes.Equal(op.Signed.Bytes, signed.Bytes) {
					t.Fatal("unchanged unlock lost the original ambiguous signed transaction")
				}
				replacement := evmPlan(account)
				replacement.Expires++
				replacement.ID = ""
				replacement.ID = "evm:" + journal.Digest(replacement)
				if err = evm.New().ValidatePlan(replacement); err != nil {
					t.Fatal("replacement fixture invalid", err)
				}
				if err = e.Prepare(replacement); err == nil {
					t.Fatal("unchanged unlock released an unresolved reservation")
				}
			}
		})
	}
}

func TestUnlockRecoveryWriteFailureStillLocksAndPreservesCiphertext(t *testing.T) {
	e, ui, directory := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	if err := e.Prepare(evmPlan(findAccount(t, e, "ethereum-sepolia"))); err != nil {
		t.Fatal(err)
	}
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	e.Lock()
	e.UI = &presencePromptUI{testUI: ui, beforeInput: func() {
		if err := os.Chmod(directory, 0500); err != nil {
			t.Fatal(err)
		}
	}}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = e.Unlock(); err == nil || !e.Snapshot().Locked || !e.persistFailed || e.Pending() != nil || e.password != nil {
		t.Fatal("failed recovery write released an unlocked session")
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed recovery write damaged the original ciphertext")
	}
}

func TestUnlockUnsignedExchangeCancellationIsDurableBeforeReadOnlyReopen(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		name := "PREPARED"
		if authorized {
			name = "AUTHORIZED"
		}
		t.Run(name, func(t *testing.T) {
			e, ui, account := exchangeEngine(t)
			op, err := e.PrepareExchange(exchangeSwap(account))
			if err != nil {
				t.Fatal(err)
			}
			if authorized {
				op = authorizeExchange(t, e, ui, op)
			}
			generation := e.data.Generation
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			sealed, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			plain, err := vault.Open(sealed, []byte(syntheticPassword))
			if err != nil {
				t.Fatal(err)
			}
			defer clear(plain)
			disk, err := vault.Decode(plain)
			if err != nil {
				t.Fatal(err)
			}
			defer disk.Destroy()
			if disk.Generation != generation+1 || disk.Exchange == nil || len(disk.Exchange.Operations) != 1 {
				t.Fatal("unsigned exchange recovery did not commit a generation")
			}
			saved := disk.Exchange.Operations[0]
			if saved.ID != op.ID || saved.Payload != op.Payload || saved.State != "CANCELLED" {
				t.Fatal("unsigned exchange cancellation was not durable")
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			reopened, err := e.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(sealed, reopened) {
				t.Fatal("settled exchange recovery rewrote the vault")
			}
			if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
				t.Fatal("read-only unlock revived a previous exchange authorization")
			}
		})
	}
}

func TestUnlockAfterFailedMutationRequiresFreshSigner(t *testing.T) {
	e, ui, directory := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "ethereum-sepolia")
	generation := e.data.Generation
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0700) })
	if err = os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SaveAddress(account.ID, "", "Synthetic recipient", account.Address, ""); err == nil || !e.persistFailed || !e.Snapshot().Locked || e.password != nil {
		t.Fatal("failed mutation did not lock and poison the session")
	}
	if err = os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = e.Unlock(); err == nil || !e.Snapshot().Locked || e.data != nil || e.password != nil {
		t.Fatal("read-only unlock revived a failed-persistence session")
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed session changed the last durable ciphertext")
	}
	if err = e.Files.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := vault.NewFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = files.Close() })
	freshUI := &testUI{approve: true, inputs: [][][]byte{input(syntheticPassword)}}
	fresh := New(files, freshUI, []model.Adapter{evm.New()})
	t.Cleanup(fresh.Lock)
	if err = fresh.Unlock(); err != nil {
		t.Fatal(err)
	}
	entries, err := fresh.AddressBook(account.ID)
	if err != nil || len(entries) != 0 || fresh.data.Generation != generation {
		t.Fatal("fresh signer failed to recover the last durable state")
	}
	reopened, err := files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, reopened) {
		t.Fatal("fresh read-only recovery rewrote the last durable ciphertext")
	}
}

// This opt-in benchmark uses the actual production KDF and every active
// mainnet descriptor. Setup is outside the measured loop; no RPC is used.
func BenchmarkUnlockUnchangedVault(b *testing.B) {
	files, err := vault.NewFiles(filepath.Join(b.TempDir(), "profile"))
	if err != nil {
		b.Fatal(err)
	}
	ui := &testUI{approve: true, inputs: [][][]byte{input(syntheticPassword, syntheticPassword, syntheticPhrase, "")}}
	e := New(files, ui, chains.All())
	b.Cleanup(func() { e.Lock(); _ = files.Close() })
	if err = e.Create("mainnet", 12, true); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		e.Lock()
		ui.inputs = append(ui.inputs, input(syntheticPassword))
		if err = e.Unlock(); err != nil {
			b.Fatal(err)
		}
	}
}
