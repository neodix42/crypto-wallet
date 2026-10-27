package signer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

// All secrets in these tests are disposable public BIP39 fixtures. The UI keeps
// borrowed buffers only so tests can verify that the engine clears them.
type backupUI struct {
	*testUI
	stages       []string
	confirm      func(string) bool
	hook         func(string)
	inputError   error
	borrowed     [][]byte
	phrase       []byte
	shown        []byte
	acceptPhrase bool
}

func (u *backupUI) Input(title string, labels ...string) ([][]byte, error) {
	u.stages = append(u.stages, "input")
	if title != "Confirm app password to back up recovery phrase" || len(labels) != 1 || labels[0] != "App password" {
		return nil, ErrDenied
	}
	u.borrowed, _ = u.testUI.Input(title, labels...)
	if u.hook != nil {
		u.hook("input")
	}
	return u.borrowed, u.inputError
}
func (u *backupUI) Confirm(title, summary string) bool {
	u.stages = append(u.stages, title)
	u.summaries = append(u.summaries, summary)
	if u.hook != nil {
		u.hook(title)
	}
	return u.confirm == nil || u.confirm(title)
}
func (u *backupUI) BackupRecovery(title string, phrase []byte) bool {
	u.stages = append(u.stages, "display")
	clear(u.shown)
	u.phrase, u.shown = phrase, bytes.Clone(phrase)
	if u.hook != nil {
		u.hook("display")
	}
	return u.acceptPhrase
}

func readyBackupEngine(t *testing.T) (*Engine, *backupUI, string) {
	t.Helper()
	e, base, dir := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, base, "mainnet")
	u := &backupUI{testUI: base, acceptPhrase: true}
	e.UI = u
	t.Cleanup(func() { clear(u.shown) })
	return e, u, dir
}

func assertBackupBuffersCleared(t *testing.T, u *backupUI) {
	t.Helper()
	for _, in := range u.borrowed {
		if !bytes.Equal(in, make([]byte, len(in))) {
			t.Fatal("backup password buffer retained after prompt")
		}
	}
	if !bytes.Equal(u.phrase, make([]byte, len(u.phrase))) {
		t.Fatal("backup phrase buffer retained after prompt")
	}
}

func TestTP033BackupPhraseRequiresFreshAuthenticationAndNeverPersists(t *testing.T) {
	e, u, dir := readyBackupEngine(t)
	before, err := e.data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(before)
	files := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = content
	}
	for i := 0; i < 2; i++ {
		u.inputs = append(u.inputs, input(syntheticPassword))
		if err := e.BackupPhrase(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(u.shown, []byte(syntheticPhrase)) {
			t.Fatal("backup did not display the existing root")
		}
		assertBackupBuffersCleared(t, u)
	}
	if strings.Join(u.stages, ",") != "Back up recovery phrase,input,display,Back up recovery phrase,input,display" {
		t.Fatal("repeat backup reused authentication or omitted confirmation")
	}
	after, err := e.data.Encode()
	defer clear(after)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("backup changed recovery state, generation or wallet metadata")
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != len(files) {
		t.Fatal("backup wrote a new file")
	}
	for name, original := range files {
		actual, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(original, actual) {
			t.Fatal("backup changed a profile file")
		}
	}
	public, _ := json.Marshal(e.Snapshot())
	if bytes.Contains(public, []byte(syntheticPhrase)) || bytes.Contains(public, []byte(syntheticPassword)) {
		t.Fatal("backup secret entered public snapshot")
	}
	// Authorization is never cached for the next invocation.
	u.inputs = append(u.inputs, input("public incorrect password"))
	u.phrase, u.shown = nil, nil
	if e.BackupPhrase() == nil || len(u.phrase) != 0 {
		t.Fatal("a prior successful backup authorized a later wrong password")
	}
	assertBackupBuffersCleared(t, u)
}

func TestTP033BackupPhraseCancellationAndInvalidInputNeverGrantAuthority(t *testing.T) {
	for _, mode := range []string{"cancel-confirm", "wrong-password", "empty-password", "extra-password", "input-cancel", "input-error-with-buffer", "cancel-warning", "cancel-display", "invalid-entropy", "no-prompt-interface"} {
		t.Run(mode, func(t *testing.T) {
			e, u, _ := readyBackupEngine(t)
			generation := e.data.Generation
			u.inputs = append(u.inputs, input(syntheticPassword))
			wantDenied, wantDisplay := false, false
			switch mode {
			case "cancel-confirm":
				u.confirm = func(string) bool { return false }
			case "wrong-password":
				u.inputs = [][][]byte{input("public incorrect password")}
				wantDenied = true
			case "empty-password":
				u.inputs = [][][]byte{{}}
				wantDenied = true
			case "extra-password":
				u.inputs = [][][]byte{input(syntheticPassword, syntheticPassword)}
				wantDenied = true
			case "input-cancel", "input-error-with-buffer":
				u.inputError = errors.New("public fixture input cancelled")
				if mode == "input-cancel" {
					u.inputs = nil
				}
				wantDenied = true
			case "cancel-warning":
				e.data.Passphrase = []byte("public separate BIP39 passphrase")
				u.confirm = func(title string) bool { return title != "Keep your separate BIP39 passphrase" }
			case "cancel-display":
				u.acceptPhrase = false
				wantDisplay = true
			case "invalid-entropy":
				clear(e.data.Entropy)
				e.data.Entropy = []byte{1}
				wantDenied = true
			case "no-prompt-interface":
				e.UI = noRecoveryUI{UI: u}
				wantDenied = true
			}
			err := e.BackupPhrase()
			if (err != nil) != wantDenied || (len(u.phrase) != 0) != wantDisplay {
				t.Fatal("backup cancellation or authentication did not fail safely")
			}
			assertBackupBuffersCleared(t, u)
			if e.backupInProgress || !e.canAuthorize() || e.data.Generation != generation {
				t.Fatal("backup changed authority or left its reentry guard active")
			}
		})
	}
}

func TestTP033BackupPhraseRejectsUnavailableAndPendingVaultWithoutPrompt(t *testing.T) {
	for _, state := range []string{"locked", "pending", "failed-persistence", "reentry"} {
		t.Run(state, func(t *testing.T) {
			e, u, _ := readyBackupEngine(t)
			switch state {
			case "locked":
				e.Lock()
			case "pending":
				e.data.RecoveryConfirmed = false
			case "failed-persistence":
				e.persistFailed = true
			case "reentry":
				e.backupInProgress = true
			}
			if e.BackupPhrase() == nil || len(u.stages) != 0 {
				t.Fatal("unavailable backup requested secret input or phrase")
			}
		})
	}
}

func TestTP043BackupPhraseChecksSessionAtEveryPromptBoundary(t *testing.T) {
	for _, stage := range []string{"Back up recovery phrase", "input", "Keep your separate BIP39 passphrase", "display"} {
		for _, change := range []string{"lock", "replace-root", "generation", "pending", "failed-persistence"} {
			t.Run(stage+"/"+change, func(t *testing.T) {
				e, u, _ := readyBackupEngine(t)
				u.inputs = append(u.inputs, input(syntheticPassword))
				if stage == "Keep your separate BIP39 passphrase" {
					e.data.Passphrase = []byte("public separate BIP39 passphrase")
				}
				u.hook = func(at string) {
					if at != stage {
						return
					}
					switch change {
					case "lock":
						e.Lock()
					case "replace-root":
						replacement := *e.data
						e.data = &replacement
					case "generation":
						e.data.Generation++
					case "pending":
						e.data.RecoveryConfirmed = false
					case "failed-persistence":
						e.persistFailed = true
					}
				}
				if e.BackupPhrase() == nil {
					t.Fatal("backup accepted authorization across a changed session")
				}
				if stage != "display" && len(u.phrase) != 0 {
					t.Fatal("phrase displayed after session invalidation")
				}
				assertBackupBuffersCleared(t, u)
			})
		}
	}
}

func TestTP033BackupPhraseRejectsReentryAndWarnsAboutSeparatePassphrase(t *testing.T) {
	e, u, _ := readyBackupEngine(t)
	const separate = "public synthetic separate BIP39 secret 27193"
	e.data.Passphrase = []byte(separate)
	u.inputs = append(u.inputs, input(syntheticPassword))
	u.hook = func(string) {
		if e.BackupPhrase() == nil {
			t.Fatal("reentrant phrase view bypassed fresh authorization")
		}
	}
	if err := e.BackupPhrase(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(u.stages, ",") != "Back up recovery phrase,input,Keep your separate BIP39 passphrase,display" {
		t.Fatal("separate passphrase reminder was omitted or preceded authentication")
	}
	for _, summary := range u.summaries {
		if strings.Contains(summary, separate) || strings.Contains(summary, syntheticPassword) || strings.Contains(summary, syntheticPhrase) {
			t.Fatal("secret entered ordinary confirmation summary")
		}
	}
	assertBackupBuffersCleared(t, u)
}
