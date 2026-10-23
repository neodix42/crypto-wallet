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

type presencePromptUI struct {
	*testUI
	inputCalls  int
	fileCalls   int
	beforeInput func()
}

func (u *presencePromptUI) Input(title string, labels ...string) ([][]byte, error) {
	u.inputCalls++
	if u.beforeInput != nil {
		u.beforeInput()
		u.beforeInput = nil
	}
	return u.testUI.Input(title, labels...)
}

func (u *presencePromptUI) File(title string, save bool) (string, error) {
	u.fileCalls++
	return u.testUI.File(title, save)
}

func TestFirstLaunchPresenceSurvivesLockWithoutSecretAccess(t *testing.T) {
	e, ui, dir := newEngine(t, chains.All())
	before := e.Snapshot()
	if !before.Locked || before.WalletExists || before.VaultState != "missing" || len(ui.inputs) != 0 {
		t.Fatalf("new profile: %+v", before)
	}
	createImported(t, e, ui, "testnet")
	created := e.Snapshot()
	if created.Locked || !created.WalletExists || created.VaultState != "present" || len(created.Accounts) == 0 {
		t.Fatalf("created wallet: %+v", created)
	}
	for _, a := range created.Accounts {
		if a.Index != 0 {
			t.Fatal("main wallet did not use the shared index-zero root")
		}
	}
	e.Lock()
	locked := e.Snapshot()
	if !locked.Locked || !locked.WalletExists || locked.VaultState != "present" || len(locked.Accounts) != 0 || len(locked.Operations) != 0 {
		t.Fatalf("locked existing wallet: %+v", locked)
	}
	public, err := json.Marshal(locked)
	if err != nil || bytes.Contains(public, []byte(syntheticPassword)) || bytes.Contains(public, []byte(syntheticPhrase)) || bytes.Contains(public, []byte("entropy")) || bytes.Contains(public, []byte("seed")) {
		t.Fatalf("presence response exposed secret fields: %s %v", public, err)
	}
	// Snapshot does not decrypt or validate ciphertext. Corruption must still
	// choose unlock/recovery, never the first-launch creation path.
	if err := os.WriteFile(filepath.Join(dir, "vault.lwv"), []byte("corrupt ciphertext"), 0600); err != nil {
		t.Fatal(err)
	}
	if s := e.Snapshot(); !s.WalletExists || s.VaultState != "present" || !s.Locked {
		t.Fatalf("corrupt existing wallet misclassified: %+v", s)
	}
	if err := e.Create("testnet", 12, false); err == nil {
		t.Fatal("presence check allowed overwriting existing ciphertext")
	}
}

func TestWalletPresenceUncertaintyNeverMeansFirstRun(t *testing.T) {
	for _, mode := range []string{"symlink", "directory", "unreadable", "closed", "nil", "previous symlink", "previous empty"} {
		t.Run(mode, func(t *testing.T) {
			e, ui, dir := newEngine(t, nil)
			prompts := &presencePromptUI{testUI: ui}
			e.UI = prompts
			path := filepath.Join(dir, "vault.lwv")
			var err error
			switch mode {
			case "symlink":
				err = os.Symlink("absent-target", path)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "unreadable":
				err = os.WriteFile(path, nil, 0200)
			case "closed":
				err = e.Files.Close()
			case "nil":
				e.Files = nil
			case "previous symlink":
				err = os.Symlink("absent-target", filepath.Join(dir, "vault.previous.lwv"))
			case "previous empty":
				err = os.WriteFile(filepath.Join(dir, "vault.previous.lwv"), nil, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := e.Snapshot(); !s.Locked || !s.WalletExists || s.VaultState != "unavailable" {
				t.Fatalf("storage uncertainty authorized first run: %+v", s)
			}
			if err := e.Create("testnet", 12, false); err == nil {
				t.Fatal("creation bypassed unavailable storage")
			}
			if err := e.Restore(); err == nil {
				t.Fatal("restore bypassed unavailable storage")
			}
			if prompts.inputCalls != 0 || prompts.fileCalls != 0 {
				t.Fatal("unavailable storage opened native input or file prompt")
			}
		})
	}
}

func TestOrphanedEncryptedPreviousGenerationBlocksNewRootAndRestore(t *testing.T) {
	e, ui, dir := newEngine(t, chains.All())
	createImported(t, e, ui, "testnet")
	walletID := e.data.WalletID
	// A second authenticated persistence creates the actual prior generation.
	if err := e.persist(); err != nil {
		t.Fatal(err)
	}
	previousPath := filepath.Join(dir, "vault.previous.lwv")
	previous, err := os.ReadFile(previousPath)
	if err != nil || len(previous) == 0 {
		t.Fatalf("encrypted previous generation unavailable: %v", err)
	}
	e.Lock()
	if err := os.Remove(filepath.Join(dir, "vault.lwv")); err != nil {
		t.Fatal(err)
	}
	prompts := &presencePromptUI{testUI: ui}
	e.UI = prompts
	ui.path = previousPath
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword), input(syntheticPassword))
	if s := e.Snapshot(); !s.Locked || !s.WalletExists || s.VaultState != "unavailable" || len(s.Accounts) != 0 {
		t.Fatalf("orphan previous generation classified as empty: %+v", s)
	}
	for _, importing := range []bool{false, true} {
		if err := e.Create("testnet", 12, importing); err == nil {
			t.Fatal("created another root over recoverable previous generation")
		}
	}
	if err := e.Restore(); err == nil {
		t.Fatal("restored over recoverable previous generation")
	}
	if prompts.inputCalls != 0 || prompts.fileCalls != 0 || len(ui.inputs) != 2 {
		t.Fatal("collision reached native password/file input")
	}
	if _, err := os.Stat(filepath.Join(dir, "vault.lwv")); !os.IsNotExist(err) {
		t.Fatalf("guard copied or replaced the missing primary: %v", err)
	}
	if after, err := os.ReadFile(previousPath); err != nil || !bytes.Equal(previous, after) {
		t.Fatalf("guard modified the recoverable generation: %v", err)
	}
	if e.data != nil || len(e.password) != 0 {
		t.Fatal("presence guard loaded previous-generation secrets")
	}
	// Independently authenticate the untouched fixture to show the protected
	// bytes still recover the original wallet identity.
	plain, err := vault.Open(previous, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	recovered, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Destroy()
	if recovered.WalletID != walletID || len(recovered.Accounts) == 0 {
		t.Fatal("protected previous generation lost the original wallet")
	}
}

func TestUnlockedMissingVaultIsUnavailable(t *testing.T) {
	e, ui, dir := newEngine(t, chains.All())
	createImported(t, e, ui, "testnet")
	if err := os.Remove(filepath.Join(dir, "vault.lwv")); err != nil {
		t.Fatal(err)
	}
	if s := e.Snapshot(); s.Locked || !s.WalletExists || s.VaultState != "unavailable" {
		t.Fatalf("live wallet lost storage: %+v", s)
	}
}

func TestInitializationRechecksFilesThatAppearDuringNativeInput(t *testing.T) {
	source, sourceUI, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, source, sourceUI, "testnet")
	original, err := source.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "portable")
	if err := os.Mkdir(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupDir, "source.lwv")
	if err := os.WriteFile(backup, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"create", "import", "restore"} {
		for _, collision := range []string{"primary", "previous", "both"} {
			t.Run(operation+"/"+collision, func(t *testing.T) {
				e, ui, dir := newEngine(t, []model.Adapter{evm.New()})
				ui.path = backup
				switch operation {
				case "create":
					ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
				case "import":
					ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
				case "restore":
					ui.inputs = append(ui.inputs, input(syntheticPassword))
				}
				prompts := &presencePromptUI{testUI: ui}
				prompts.beforeInput = func() {
					for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
						if (name == "vault.lwv" && collision == "previous") || (name == "vault.previous.lwv" && collision == "primary") {
							continue
						}
						if err := os.WriteFile(filepath.Join(dir, name), original, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				e.UI = prompts
				if operation == "restore" {
					err = e.Restore()
				} else {
					err = e.Create("testnet", 12, operation == "import")
				}
				if err == nil || !e.Snapshot().Locked || e.data != nil || len(e.password) != 0 || !e.persistFailed || e.initializing {
					t.Fatalf("initialization collision did not stop/clear the session: %v", err)
				}
				if prompts.inputCalls != 1 {
					t.Fatal("collision was not introduced after the pre-prompt presence check")
				}
				for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
					after, readErr := os.ReadFile(filepath.Join(dir, name))
					absent := (name == "vault.lwv" && collision == "previous") || (name == "vault.previous.lwv" && collision == "primary")
					if absent {
						if !os.IsNotExist(readErr) {
							t.Fatalf("initialization copied/created %s: %v", name, readErr)
						}
					} else if readErr != nil || !bytes.Equal(after, original) {
						t.Fatalf("initialization replaced original %s: %v", name, readErr)
					}
				}
			})
		}
	}
}
