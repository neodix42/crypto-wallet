package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

// Every profile, network and canonical account is restored from the actual
// CSPRNG-generated phrase alone. Separate private directories represent clean
// installations: no vault, endpoint, journal, machine ID or keyring is copied.
func TestFreshMachinePhraseRecoveryAllNetworksAndStoragePasswords(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			original, ui, originalDir := newEngine(t, chains.All())
			words := 12
			if environment == "testnet" {
				words = 24
			}
			ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
			if err := original.Create(environment, words, false); err != nil {
				t.Fatal(err)
			}
			phrase := bytes.Clone(ui.recoveryPhrase)
			defer clear(phrase)
			if !original.Snapshot().RecoveryConfirmed || len(bytes.Fields(phrase)) != words {
				t.Fatal("generated phrase was not recorded")
			}
			for _, index := range []uint32{1, 127} {
				if err := original.AddAccount(index); err != nil {
					t.Fatal(err)
				}
			}
			expected, err := json.Marshal(original.Snapshot().Accounts)
			if err != nil {
				t.Fatal(err)
			}
			if environment == "mainnet" && len(original.Snapshot().Accounts) != 27 {
				t.Fatal("mainnet recovery omitted required domains")
			}
			original.Lock()
			for _, password := range []string{syntheticPassword, "public different installation password 59268"} {
				fresh, targetUI, targetDir := newEngine(t, chains.All())
				if targetDir == originalDir {
					t.Fatal("recovery reused original installation")
				}
				entries, err := os.ReadDir(targetDir)
				if err != nil || len(entries) != 1 || entries[0].Name() != "writer.lock" {
					t.Fatal("recovery did not start from a fresh profile")
				}
				targetUI.inputs = append(targetUI.inputs, [][]byte{[]byte(password), []byte(password), bytes.Clone(phrase), {}})
				if err = fresh.Create(environment, 12, true); err != nil {
					t.Fatal(err)
				}
				for _, index := range []uint32{1, 127} {
					if err = fresh.AddAccount(index); err != nil {
						t.Fatal(err)
					}
				}
				actual, err := json.Marshal(fresh.Snapshot().Accounts)
				if err != nil || !bytes.Equal(actual, expected) || !fresh.Snapshot().RecoveryConfirmed {
					t.Fatal("fresh installation did not reproduce canonical wallets")
				}
				fresh.Lock()
				targetUI.inputs = append(targetUI.inputs, input(password))
				if err = fresh.Unlock(); err != nil {
					t.Fatal("restored installation failed durable unlock", err)
				}
				unlocked, _ := json.Marshal(fresh.Snapshot().Accounts)
				if !bytes.Equal(unlocked, expected) || !fresh.Snapshot().RecoveryConfirmed {
					t.Fatal("restored installation changed after restart")
				}
				public, _ := json.Marshal(fresh.Snapshot())
				if bytes.Contains(public, phrase) || bytes.Contains(public, []byte(password)) || bytes.Contains(public, []byte(`"entropy"`)) {
					t.Fatal("recovery secret entered public IPC")
				}
			}
		})
	}
}

func TestCancelledRecoveryResumesSameDurableRootAndCannotBeRevealedAfterwards(t *testing.T) {
	e, ui, dir := newEngine(t, chains.All())
	pending := false
	ui.recoveryApprove = &pending
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
	if err := e.Create("mainnet", 12, false); err != nil {
		t.Fatal(err)
	}
	originalPhrase := bytes.Clone(ui.recoveryPhrase)
	defer clear(originalPhrase)
	descriptors, _ := json.Marshal(e.Snapshot().Accounts)
	if e.Snapshot().RecoveryConfirmed || e.Snapshot().Locked {
		t.Fatal("cancelled recovery did not leave pending encrypted root")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.lwv")); err != nil {
		t.Fatal("phrase displayed before root was persisted", err)
	}
	account := e.Snapshot().Accounts[0]
	if _, err := e.Receive(account.ID); err == nil || e.CopyReceive(account.ID) == nil {
		t.Fatal("pending root permitted receiving")
	}
	if e.Create("mainnet", 12, false) == nil {
		t.Fatal("pending root was replaced")
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input("wrong synthetic password"))
	if e.ConfirmRecovery() == nil {
		t.Fatal("wrong password disclosed phrase")
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	pending = true
	if err := e.ConfirmRecovery(); err != nil || !e.Snapshot().RecoveryConfirmed {
		t.Fatal("recovery confirmation could not resume", err)
	}
	actual, _ := json.Marshal(e.Snapshot().Accounts)
	if !bytes.Equal(descriptors, actual) || !bytes.Equal(originalPhrase, ui.recoveryPhrase) {
		t.Fatal("resume changed root or canonical wallets")
	}
	if e.ConfirmRecovery() == nil {
		t.Fatal("confirmed root exposed repeated recovery reveal")
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil || !e.Snapshot().RecoveryConfirmed {
		t.Fatal("recovery acknowledgement was not durable", err)
	}
}

func TestLegacyFileVerificationDoesNotAuthorizePhraseRecovery(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
		createImported(t, e, ui, "mainnet")
		expected, _ := json.Marshal(e.Snapshot().Accounts)
		e.data.DeprecatedBackupVerified = legacy
		e.data.RecoveryConfirmed = false
		if err := e.persist(); err != nil {
			t.Fatal(err)
		}
		e.Lock()
		ui.inputs = append(ui.inputs, input(syntheticPassword))
		if err := e.Unlock(); err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(e.Snapshot().Accounts)
		if !bytes.Equal(expected, actual) || e.Snapshot().RecoveryConfirmed || e.data.DeprecatedBackupVerified {
			t.Fatal("legacy file flag changed root or granted recovery authority")
		}
		ui.inputs = append(ui.inputs, input(syntheticPassword))
		if err := e.ConfirmRecovery(); err != nil || !e.Snapshot().RecoveryConfirmed {
			t.Fatal("legacy root could not record phrase", err)
		}
	}
}

type recoveryConfirmHookUI struct {
	*testUI
	hook func()
}

func (u *recoveryConfirmHookUI) Confirm(title, summary string) bool {
	if u.hook != nil {
		u.hook()
	}
	return true
}

type noRecoveryUI struct{ UI }

func TestRecoveryNativeCallbacksCannotAcknowledgeLockedOrReplacedRoot(t *testing.T) {
	for _, point := range []string{"import-confirm", "phrase-display", "password-input", "passphrase-warning"} {
		t.Run(point, func(t *testing.T) {
			e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
			switch point {
			case "import-confirm":
				e.UI = &recoveryConfirmHookUI{testUI: ui, hook: e.Lock}
				ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
				if e.Create("mainnet", 12, true) == nil {
					t.Fatal("locked native import acquired authority")
				}
			case "phrase-display":
				ui.recoveryHook = e.Lock
				ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
				if e.Create("mainnet", 12, false) == nil {
					t.Fatal("locked native phrase acquired authority")
				}
			default:
				createImported(t, e, ui, "mainnet")
				e.data.RecoveryConfirmed = false
				ui.inputs = append(ui.inputs, input(syntheticPassword))
				if point == "password-input" {
					e.UI = &reviewInputUI{testUI: ui, hook: func(string) { e.Lock() }}
				} else {
					e.data.Passphrase = []byte("public synthetic separate BIP39 passphrase")
					e.UI = &recoveryConfirmHookUI{testUI: ui, hook: e.Lock}
				}
				if e.ConfirmRecovery() == nil {
					t.Fatal("locked native setup acquired authority")
				}
			}
			if !e.Snapshot().Locked {
				t.Fatal("native callback restored a locked session")
			}
		})
	}
}

func TestRecoveryAcknowledgementFailurePreservesPriorEncryptedRoot(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
	ui.recoveryHook = func() { e.parameters = vault.Parameters{} }
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
	if e.Create("testnet", 12, false) == nil || !e.Snapshot().Locked {
		t.Fatal("failed acknowledgement persistence remained authorized")
	}
	encrypted, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(encrypted, []byte(syntheticPassword))
	if err != nil {
		t.Fatal("failed acknowledgement destroyed pending root", err)
	}
	defer clear(plain)
	data, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Destroy()
	if data.RecoveryConfirmed {
		t.Fatal("failed acknowledgement claimed durable recovery")
	}
}

func TestPendingRecoveryFailsClosedOnAllProfilesAndNativeFailures(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
		ui.approve = false
		ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
		if err := e.Create(environment, 12, true); err != nil {
			t.Fatal(err)
		}
		if e.Snapshot().RecoveryConfirmed {
			t.Fatal("cancelled imported recovery limits granted authority")
		}
		if e.ConfirmRecovery() == nil {
			t.Fatal("cancelled password entry proceeded")
		}
		if _, err := e.Receive(e.Snapshot().Accounts[0].ID); err == nil {
			t.Fatal("pending root permitted receiving")
		}
		e.UI = noRecoveryUI{UI: ui}
		if e.recordRecovery() == nil {
			t.Fatal("missing native phrase surface was accepted")
		}
		e.UI = ui
		e.data.Entropy = []byte{1}
		if e.recordRecovery() == nil {
			t.Fatal("invalid recovery entropy displayed")
		}
		e.Lock()
		if e.recordRecovery() == nil || e.ConfirmRecovery() == nil {
			t.Fatal("locked setup proceeded")
		}
	}
}

func TestImportedBIP39PassphraseReminderAndFreshRecovery(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	const passphrase = "public synthetic separate BIP39 passphrase 89175"
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, passphrase))
	if err := e.Create("mainnet", 12, true); err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(e.Snapshot().Accounts)
	e.data.RecoveryConfirmed = false
	ui.approve = false
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ConfirmRecovery(); err != nil || e.Snapshot().RecoveryConfirmed || len(ui.recoveryPhrase) != 0 {
		t.Fatal("cancelled passphrase reminder revealed words or authorized root", err)
	}
	ui.approve = true
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := e.ConfirmRecovery(); err != nil || !e.Snapshot().RecoveryConfirmed {
		t.Fatal(err)
	}
	if len(ui.summaries) < 3 {
		t.Fatal("separate passphrase reminder was not shown")
	}
	fresh, targetUI, _ := newEngine(t, chains.All())
	targetUI.inputs = append(targetUI.inputs, input("public fresh app password 72894", "public fresh app password 72894", syntheticPhrase, passphrase))
	if err := fresh.Create("mainnet", 12, true); err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(fresh.Snapshot().Accounts)
	if !bytes.Equal(expected, actual) {
		t.Fatal("known phrase and separate passphrase failed fresh-machine recovery")
	}
	wrong, wrongUI, _ := newEngine(t, chains.All())
	wrongUI.inputs = append(wrongUI.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
	if err := wrong.Create("mainnet", 12, true); err != nil {
		t.Fatal(err)
	}
	different, _ := json.Marshal(wrong.Snapshot().Accounts)
	if bytes.Equal(expected, different) {
		t.Fatal("omitting separate passphrase silently reproduced original wallet")
	}
}

type borrowedRecoveryUI struct {
	*testUI
	borrowed []byte
}

func (u *borrowedRecoveryUI) Recovery(_ string, phrase []byte) bool {
	u.borrowed = phrase
	return false
}

func TestNativeRecoveryPhraseBufferIsWipedAfterCancellation(t *testing.T) {
	e, base, _ := newEngine(t, []model.Adapter{evm.New()})
	ui := &borrowedRecoveryUI{testUI: base}
	e.UI = ui
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
	if err := e.Create("testnet", 12, false); err != nil {
		t.Fatal(err)
	}
	if len(ui.borrowed) == 0 || !bytes.Equal(ui.borrowed, make([]byte, len(ui.borrowed))) {
		t.Fatal("native phrase buffer was retained after dismissal")
	}
}
