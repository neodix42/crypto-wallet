package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func findAccount(t *testing.T, e *Engine, network string) model.Account {
	t.Helper()
	for _, a := range e.Snapshot().Accounts {
		if a.Network == network {
			return a
		}
	}
	t.Fatal("synthetic account missing")
	return model.Account{}
}

// All backup/envelope helpers contain public BIP39 vector material only.
func sealedFixture(t *testing.T, e *Engine, plaintext []byte) []byte {
	t.Helper()
	b, err := vault.Seal(plaintext, []byte(syntheticPassword), e.parameters)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func writeCiphertext(t *testing.T, e *Engine, b []byte) {
	t.Helper()
	if err := e.Files.Write("vault.lwv", b, true); err != nil {
		t.Fatal(err)
	}
}
func TestTP033ReceiveQRFileAndCopyBoundaries(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	if _, err := e.ScanQR(); err == nil {
		t.Fatal("locked QR scan")
	}
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "ethereum-sepolia")
	if err := e.CopyReceive(a.ID); err != nil || u.copied != a.Address {
		t.Fatal("verified public copy", err)
	}
	u.path = filepath.Join(t.TempDir(), "verified.png")
	if err := e.SaveQR(a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.ScanQR()
	if err != nil || got != a.Address {
		t.Fatal("independent saved QR decode", err)
	}
	if st, err := os.Stat(u.path); err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("QR file permissions")
	}
	if e.SaveQR(a.ID) == nil {
		t.Fatal("QR overwrite allowed")
	}
	u.approve = false
	if _, err := e.Receive(a.ID); err == nil || e.SaveQR(a.ID) == nil {
		t.Fatal("cancelled verification accepted")
	}
	if err := e.CopyReceive(a.ID); err != nil || u.copied != a.Address {
		t.Fatal("public copy unnecessarily required another dialog")
	}
	u.approve = true
	e.UI = struct{ UI }{u}
	if e.CopyReceive(a.ID) == nil {
		t.Fatal("missing clipboard capability accepted")
	}
	e.UI = u
	u.path = ""
	if e.SaveQR(a.ID) == nil {
		t.Fatal("cancelled file choice")
	}
	if _, err = e.ScanQR(); err == nil {
		t.Fatal("cancelled scan choice")
	}
	u.path = filepath.Join(t.TempDir(), "missing")
	if _, err = e.ScanQR(); err == nil {
		t.Fatal("missing scan file")
	}
	u.path = t.TempDir()
	if _, err = e.ScanQR(); err == nil {
		t.Fatal("directory scan")
	}
	u.path = filepath.Join(t.TempDir(), "large-image")
	if err = os.WriteFile(u.path, make([]byte, 5<<20+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ScanQR(); err == nil {
		t.Fatal("oversized image")
	}
	if err = os.WriteFile(u.path, []byte("invalid image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ScanQR(); err == nil {
		t.Fatal("malformed image")
	}
	u.approve = false
	if _, err = e.Receive(a.ID); err == nil {
		t.Fatal("receive cancellation")
	}
	u.approve = true
	original := e.Adapters
	e.Adapters = nil
	if _, err = e.Receive(a.ID); err == nil {
		t.Fatal("unavailable adapter")
	}
	e.Adapters = original
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == a.ID {
			e.data.Accounts[i].Address = "invalid"
		}
	}
	if _, err = e.Receive(a.ID); err == nil {
		t.Fatal("invalid receive address")
	}
}
func TestTP041EncryptedSettingsAndNativeCancellation(t *testing.T) {
	e, u, dir := newEngine(t, []model.Adapter{evm.New()})
	if e.Configure("ethereum-sepolia") == nil {
		t.Fatal("locked configuration")
	}
	createImported(t, e, u, "testnet")
	if e.Configure("ethereum-sepolia") == nil {
		t.Fatal("cancelled input")
	}
	for _, in := range [][][]byte{input("https://example.org"), input("http://remote.invalid", "", "", ""), input("https://user:pass@example.org", "", "", "")} {
		u.inputs = append(u.inputs, in)
		if e.Configure("ethereum-sepolia") == nil {
			t.Fatal("invalid RPC configuration")
		}
	}
	u.approve = false
	u.inputs = append(u.inputs, input("https://example.org", "public-user", "PUBLIC-SYNTHETIC-NODE-CREDENTIAL", ""))
	if e.Configure("ethereum-sepolia") == nil {
		t.Fatal("unapproved provider")
	}
	u.approve = true
	e.data.Endpoints = nil
	u.inputs = append(u.inputs, input("https://example.org", "public-user", "PUBLIC-SYNTHETIC-NODE-CREDENTIAL", "PUBLIC-API-CREDENTIAL"))
	if err := e.Configure("ethereum-sepolia"); err != nil {
		t.Fatal(err)
	}
	if e.Settings()["ethereum-sepolia"].Password != "PUBLIC-SYNTHETIC-NODE-CREDENTIAL" {
		t.Fatal("encrypted setting absent")
	}
	b, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("PUBLIC-SYNTHETIC-NODE-CREDENTIAL")) {
		t.Fatal("plaintext provider credential")
	}
	public, _ := json.Marshal(e.Snapshot())
	if bytes.Contains(public, []byte("PUBLIC-API-CREDENTIAL")) {
		t.Fatal("provider credential in public snapshot")
	}
	var parsed struct{}
	if DecodeBody([]byte(`{"password":"forbidden"}`), &parsed) == nil {
		t.Fatal("secret-bearing field")
	}
	if DecodeBody([]byte(`{}`), &parsed) != nil {
		t.Fatal("empty public request")
	}
}
func TestTP032NamedAndRotatedMetadataAssociation(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{near.New(), aptos.New()})
	createImported(t, e, u, "testnet")
	for _, network := range []string{"near-testnet", "aptos-testnet"} {
		original := findAccount(t, e, network)
		updated := original
		if network == "near-testnet" {
			var p near.Parameters
			_ = json.Unmarshal(updated.Parameters, &p)
			p.Named = true
			updated.Address = "verified.testnet"
			updated.Parameters, _ = json.Marshal(p)
		} else {
			var p aptos.Parameters
			_ = json.Unmarshal(updated.Parameters, &p)
			p.Associated = true
			updated.Address = "0x" + strings.Repeat("0", 63) + "a"
			updated.Parameters, _ = json.Marshal(p)
		}
		u.approve = false
		if e.Associate(updated) == nil {
			t.Fatal("association without native approval")
		}
		u.approve = true
		if err := e.Associate(updated); err != nil {
			t.Fatal(err)
		}
		actual := findAccount(t, e, network)
		if actual.ID != original.ID || actual.Address != updated.Address || !e.data.RecoveryConfirmed {
			t.Fatal("association identity or backup state")
		}
		bad := updated
		bad.ID = "missing"
		if e.Associate(bad) == nil {
			t.Fatal("unknown association")
		}
		bad = updated
		bad.Profile = "other"
		if e.Associate(bad) == nil {
			t.Fatal("association profile swap")
		}
		bad = updated
		bad.Address = "bad!!"
		if e.Associate(bad) == nil {
			t.Fatal("association address syntax")
		}
	}
	before := e.data.Accounts[0]
	e.data.Accounts[0].Network = "unsupported"
	bad := e.data.Accounts[0]
	if e.Associate(bad) == nil {
		t.Fatal("unavailable association adapter")
	}
	e.data.Accounts[0] = before
}
func TestTP020CreateFailuresDoNotReplaceRoot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    [][]byte
		words     int
		importing bool
	}{{"cancel", nil, 12, false}, {"short-password", input("short", "short"), 12, false}, {"mismatch", input(syntheticPassword, "other-password"), 12, false}, {"bad-count", input(syntheticPassword), 12, false}, {"invalid-phrase", input(syntheticPassword, syntheticPassword, "public invalid phrase", ""), 12, true}, {"unsupported-length", input(syntheticPassword, syntheticPassword), 13, false}} {
		t.Run(tc.name, func(t *testing.T) {
			e, u, _ := newEngine(t, []model.Adapter{evm.New()})
			if tc.values != nil {
				u.inputs = append(u.inputs, tc.values)
			}
			if e.Create("testnet", tc.words, tc.importing) == nil {
				t.Fatal("invalid creation accepted")
			}
			if !e.Snapshot().Locked {
				t.Fatal("failed creation left unlocked state")
			}
			if _, err := e.Files.Read("vault.lwv"); !os.IsNotExist(err) {
				t.Fatal("failed creation wrote root")
			}
		})
	}
	e, u, _ := newEngine(t, nil)
	u.inputs = append(u.inputs, input(syntheticPassword, syntheticPassword))
	if e.Create("testnet", 12, false) == nil || !e.Snapshot().Locked {
		t.Fatal("creation without supported accounts")
	}
	if e.Create("invalid", 12, false) == nil {
		t.Fatal("invalid environment")
	}
	e, u, _ = newEngine(t, []model.Adapter{evm.New()})
	writeCiphertext(t, e, sealedFixture(t, e, []byte(`{}`)))
	u.inputs = append(u.inputs, input(syntheticPassword, syntheticPassword))
	if e.Create("testnet", 12, false) == nil {
		t.Fatal("unreadable existing vault replaced")
	}
}
func TestTP030UnlockAuthenticationAndDescriptorFailures(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New(), near.New(), aptos.New()})
	createImported(t, e, u, "testnet")
	if e.Unlock() == nil {
		t.Fatal("unlock while active")
	}
	saved, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(saved, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	e.Lock()
	if e.Unlock() == nil {
		t.Fatal("cancelled unlock")
	}
	u.inputs = append(u.inputs, input(syntheticPassword, "extra"))
	if e.Unlock() == nil {
		t.Fatal("extra credential field")
	}
	for _, bad := range [][]byte{[]byte(`{}`), []byte(`{"version":1,"unknown":1}`)} {
		writeCiphertext(t, e, sealedFixture(t, e, bad))
		u.inputs = append(u.inputs, input(syntheticPassword))
		if e.Unlock() == nil || !e.Snapshot().Locked {
			t.Fatal("malformed authenticated plaintext")
		}
	}
	var d vault.Data
	if err = json.Unmarshal(plain, &d); err != nil {
		t.Fatal(err)
	}
	defer d.Destroy()
	d.Accounts[0].Profile = "unsupported-profile"
	bad, _ := json.Marshal(d)
	writeCiphertext(t, e, sealedFixture(t, e, bad))
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.Unlock() == nil || !e.Snapshot().Locked {
		t.Fatal("unsupported encrypted descriptor")
	}
	writeCiphertext(t, e, saved)
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
}
func TestTP032RestoreRejectsMalformedEncryptedInputs(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	if e.Restore() == nil {
		t.Fatal("cancelled restore")
	}
	u.path = "relative.lwv"
	if e.Restore() == nil {
		t.Fatal("relative backup path")
	}
	u.path = filepath.Join(t.TempDir(), "missing.lwv")
	if e.Restore() == nil {
		t.Fatal("missing backup")
	}
	dir := filepath.Join(t.TempDir(), "private-backups")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(dir, "malformed.lwv")
	if err := os.WriteFile(u.path, []byte("invalid envelope"), 0600); err != nil {
		t.Fatal(err)
	}
	if e.Restore() == nil {
		t.Fatal("cancelled backup password")
	}
	u.inputs = append(u.inputs, input(syntheticPassword, "extra"))
	if e.Restore() == nil {
		t.Fatal("bad password arity")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.Restore() == nil {
		t.Fatal("malformed backup accepted")
	}
	if err := os.WriteFile(u.path, sealedFixture(t, e, []byte(`{}`)), 0600); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.Restore() == nil {
		t.Fatal("invalid authenticated recovery data")
	}
	source, sourceUI, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, source, sourceUI, "testnet")
	plain, _ := json.Marshal(source.data)
	var d vault.Data
	_ = json.Unmarshal(plain, &d)
	defer d.Destroy()
	d.Accounts[0].Profile = "invalid"
	bad, _ := json.Marshal(d)
	if err := os.WriteFile(u.path, sealedFixture(t, e, bad), 0600); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.Restore() == nil {
		t.Fatal("unrecoverable descriptor accepted")
	}
	if _, err := e.Files.Read("vault.lwv"); !os.IsNotExist(err) {
		t.Fatal("failed restore overwrote empty destination")
	}
}
func TestTP032PasswordAndBackupFailureBoundaries(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	if e.ChangePassword() == nil {
		t.Fatal("cancelled password change")
	}
	for _, in := range [][][]byte{input(syntheticPassword), input("wrong", syntheticPassword, syntheticPassword), input(syntheticPassword, "short", "short"), input(syntheticPassword, syntheticPassword, "mismatch")} {
		u.inputs = append(u.inputs, in)
		if e.ChangePassword() == nil {
			t.Fatal("bad password change")
		}
	}
	if e.Backup() == nil {
		t.Fatal("cancelled backup path")
	}
	u.path = "relative.lwv"
	if e.Backup() == nil {
		t.Fatal("relative backup")
	}
	u.path = filepath.Join(t.TempDir(), "public-folder", "backup.lwv")
	if err := os.Mkdir(filepath.Dir(u.path), 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(u.path), 0777); err != nil {
		t.Fatal(err)
	}
	if e.Backup() == nil {
		t.Fatal("unsafe backup directory")
	}
	backup := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(backup, "backup.lwv")
	e.parameters = vault.Parameters{}
	if e.Backup() == nil {
		t.Fatal("invalid encryption policy")
	}
	u.inputs = append(u.inputs, input(syntheticPassword, "NEW-SYNTHETIC-PASSWORD", "NEW-SYNTHETIC-PASSWORD"))
	if e.ChangePassword() == nil || !e.Snapshot().Locked {
		t.Fatal("failed password persistence did not lock")
	}
	disk, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(disk, []byte(syntheticPassword))
	clear(plain)
	if err != nil {
		t.Fatal("failed password rotation destroyed prior generation")
	}
}
func TestTP011ImmutableRecoveryValidation(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New(), near.New(), aptos.New(), decred.New()})
	createImported(t, e, u, "testnet")
	clone := func() *vault.Data {
		b, _ := json.Marshal(e.data)
		var d vault.Data
		_ = json.Unmarshal(b, &d)
		t.Cleanup(d.Destroy)
		return &d
	}
	for _, mode := range []string{"seed", "network", "index", "profile", "evm-address", "near-params", "aptos-params", "decred-params", "address-syntax"} {
		d := clone()
		switch mode {
		case "seed":
			d.Entropy = []byte{1}
		case "network":
			d.Accounts[0].Network = "unknown"
		case "index":
			d.Accounts[0].Index = derivation.MaxIndex + 1
		case "profile":
			d.Accounts[0].Profile = "other"
		case "evm-address":
			d.Accounts[0].Address = "0x2222222222222222222222222222222222222222"
		case "near-params", "aptos-params", "decred-params":
			family := strings.TrimSuffix(mode, "-params")
			for i := range d.Accounts {
				if d.Accounts[i].Family == family {
					d.Accounts[i].Parameters = []byte(`{}`)
					break
				}
			}
		case "address-syntax":
			for i := range d.Accounts {
				if d.Accounts[i].Family == "near" {
					var p near.Parameters
					_ = json.Unmarshal(d.Accounts[i].Parameters, &p)
					p.Named = true
					d.Accounts[i].Parameters, _ = json.Marshal(p)
					d.Accounts[i].Address = "bad!!"
					break
				}
			}
		}
		if e.validateAccounts(d) == nil {
			t.Fatal("invalid recovery descriptor accepted: " + mode)
		}
	}
	p := model.Plan{Account: model.Account{Family: "decred"}, Recipient: "public-receiver", Amount: "123"}
	p.Payload = []byte(`{`)
	if !strings.Contains(nativeEffects(p), "Invalid effects") {
		t.Fatal("invalid effects")
	}
	x := decred.Payload{ChangeAddress: "public-change", Inputs: []decred.Input{{TxID: strings.Repeat("0", 64), Vout: 1, Tree: 0, Value: 1000, Branch: 0, Index: 3, Height: 12}}}
	p.Payload, _ = json.Marshal(x)
	effects := nativeEffects(p)
	if !strings.Contains(effects, "public-receiver") || !strings.Contains(effects, "public-change") || !strings.Contains(effects, "tree 0") {
		t.Fatal("DCR native output/input effects incomplete")
	}
}
func TestTP031PersistenceQuarantinesAllFailureStages(t *testing.T) {
	for _, stage := range []string{"invalid-data", "read-failure", "prior-generation-write", "current-generation-write"} {
		t.Run(stage, func(t *testing.T) {
			e, u, dir := newEngine(t, []model.Adapter{evm.New()})
			createImported(t, e, u, "testnet")
			switch stage {
			case "invalid-data":
				e.data.Version = 2
			case "read-failure":
				if err := os.Chmod(filepath.Join(dir, "vault.lwv"), 0644); err != nil {
					t.Fatal(err)
				}
			case "prior-generation-write":
				if err := os.Remove(filepath.Join(dir, "vault.previous.lwv")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("vault.lwv", filepath.Join(dir, "vault.previous.lwv")); err != nil {
					t.Fatal(err)
				}
			case "current-generation-write":
				if err := os.Remove(filepath.Join(dir, "vault.lwv")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "vault.lwv"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if e.persist() == nil || !e.Snapshot().Locked || !e.persistFailed {
				t.Fatal("persistence failure left usable secrets")
			}
			if e.Pending() != nil {
				t.Fatal("quarantined pending artifacts")
			}
		})
	}
	e, _, _ := newEngine(t, nil)
	if e.persist() == nil {
		t.Fatal("locked persistence")
	}
	if e.Update("x", "UNKNOWN", nil) == nil {
		t.Fatal("locked update")
	}
}
func TestTP042PrepareAndSignAdversarialState(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "ethereum-sepolia")
	p := evmPlan(a)
	for _, mode := range []string{"missing-account", "descriptor", "bad-json", "too-future", "missing-id"} {
		q := p
		switch mode {
		case "missing-account":
			q.Account.ID = "missing"
		case "descriptor":
			q.Account.Address = "0x2222222222222222222222222222222222222222"
		case "bad-json":
			q.Payload = []byte(`{`)
		case "too-future":
			q.Expires = time.Now().Add(time.Hour).Unix()
		case "missing-id":
			q.ID = ""
		}
		if e.Prepare(q) == nil {
			t.Fatal("invalid preparation " + mode)
		}
	}
	if _, err := e.Sign("missing"); err == nil {
		t.Fatal("unknown sign ID")
	}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	e.data.Operations[0].Digest = "changed"
	if _, err := e.Sign(p.ID); err == nil {
		t.Fatal("mutable intent")
	}
	e.data.Operations[0].Digest = journal.Digest(p)
	if _, err := e.Sign(p.ID); err == nil {
		t.Fatal("cancelled native credentials")
	}
	if e.Pending()[0].State != "CANCELLED" {
		t.Fatal("credential cancellation not journaled")
	}
}

// Synthetic adapter faults isolate the Engine authorization contract. Actual
// signatures/codecs/authority are independently exercised in chain packages.
type signerFaultAdapter struct {
	model.Adapter
	invalid     bool
	mode        string
	deriveError bool
	emptyID     bool
}

func (a *signerFaultAdapter) ValidatePlan(p model.Plan) error {
	if a.invalid {
		return ErrDenied
	}
	return a.Adapter.ValidatePlan(p)
}
func (a *signerFaultAdapter) Derive(seed []byte, index uint32, network string) (model.Account, error) {
	if a.deriveError {
		return model.Account{}, ErrDenied
	}
	ac, err := a.Adapter.Derive(seed, index, network)
	if a.emptyID {
		ac.ID = ""
	}
	return ac, err
}
func (a *signerFaultAdapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	switch a.mode {
	case "error":
		return model.Signed{}, ErrDenied
	case "empty":
		return model.Signed{}, nil
	case "public-only":
		return model.Signed{Identity: "public-engine-fixture", Bytes: []byte{1}}, nil
	}
	return a.Adapter.Sign(seed, p)
}

type adversarialUI struct {
	*testUI
	confirmHook func()
	inputHook   func(string)
}

func (u *adversarialUI) Confirm(title, summary string) bool {
	if u.confirmHook != nil {
		u.confirmHook()
	}
	return u.testUI.Confirm(title, summary)
}
func (u *adversarialUI) Input(title string, labels ...string) ([][]byte, error) {
	result, err := u.testUI.Input(title, labels...)
	if u.inputHook != nil {
		u.inputHook(title)
	}
	return result, err
}
func TestTP042SigningBoundaryFailures(t *testing.T) {
	for _, mode := range []string{"adapter-unavailable", "validation-failure", "seed-failure", "sign-failure", "empty-artifact", "pre-confirmation-persistence", "cancel-persistence"} {
		t.Run(mode, func(t *testing.T) {
			adapter := &signerFaultAdapter{Adapter: evm.New()}
			e, u, _ := newEngine(t, []model.Adapter{adapter})
			createImported(t, e, u, "testnet")
			a := findAccount(t, e, "ethereum-sepolia")
			p := evmPlan(a)
			if err := e.Prepare(p); err != nil {
				t.Fatal(err)
			}
			u.inputs = append(u.inputs, input(syntheticPassword))
			switch mode {
			case "pre-confirmation-persistence":
				e.parameters = vault.Parameters{}
			case "cancel-persistence":
				u.approve = false
				e.UI = &adversarialUI{testUI: u, confirmHook: func() { e.parameters = vault.Parameters{} }}
			default:
				e.UI = &adversarialUI{testUI: u, inputHook: func(title string) {
					if !strings.HasPrefix(title, "Reauthenticate") {
						return
					}
					switch mode {
					case "adapter-unavailable":
						e.Adapters = nil
					case "validation-failure":
						adapter.invalid = true
					case "seed-failure":
						e.data.Entropy = []byte{1}
					case "sign-failure":
						adapter.mode = "error"
					case "empty-artifact":
						adapter.mode = "empty"
					}
				}}
			}
			if signed, err := e.Sign(p.ID); err == nil || len(signed.Bytes) != 0 {
				t.Fatal("failed authorization produced artifact")
			}
			if mode == "pre-confirmation-persistence" || mode == "cancel-persistence" {
				if !e.Snapshot().Locked || !e.persistFailed {
					t.Fatal("failed journal did not lock")
				}
			}
		})
	}
}
func TestTP032DCRChangeAdvanceAfterDurableSign(t *testing.T) {
	real := decred.New()
	adapter := &signerFaultAdapter{Adapter: real, mode: "public-only"}
	e, u, _ := newEngine(t, []model.Adapter{adapter})
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "decred-simnet")
	p := model.Plan{ID: "public-dcr-engine-test", Account: a, Asset: "decred-simnet:DCR", FeeAsset: "decred-simnet:DCR", Recipient: a.Address, Amount: "1", MaxFee: "1", Nonce: "1", Expires: time.Now().Unix() + 60, Payload: json.RawMessage(`{}`)} // Engine plan validation is isolated below, never used in production.
	adapterForEngine := &engineOnlyPlanAdapter{signerFaultAdapter: adapter}
	e.Adapters = []model.Adapter{adapterForEngine}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	before := findAccount(t, e, a.Network)
	u.inputs = append(u.inputs, input(syntheticPassword))
	if _, err := e.Sign(p.ID); err != nil {
		t.Fatal(err)
	}
	after := findAccount(t, e, a.Network)
	if bytes.Equal(before.Parameters, after.Parameters) {
		t.Fatal("DCR change index not advanced")
	}
	if after.ID != before.ID || after.PublicKey != before.PublicKey {
		t.Fatal("DCR account root changed")
	}
}

type engineOnlyPlanAdapter struct{ *signerFaultAdapter }

func (a *engineOnlyPlanAdapter) ValidatePlan(model.Plan) error { return nil }
func TestTP031AdditionalPersistenceAndReadFaults(t *testing.T) {
	e, u, dir := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	if err := os.Remove(filepath.Join(dir, "vault.lwv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "vault.lwv"), 0700); err != nil {
		t.Fatal(err)
	} // Safe read refuses this before the final write, preserving quarantine.
	if e.persist() == nil || !e.Snapshot().Locked {
		t.Fatal("directory substitution")
	}
	e2, u2, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e2, u2, "testnet")
	backupDir := filepath.Join(t.TempDir(), "private-backup")
	if err := os.Mkdir(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	u2.path = filepath.Join(backupDir, "copy.lwv")
	if err := e2.Backup(); err != nil {
		t.Fatal(err)
	}
	if e2.Backup() == nil {
		t.Fatal("backup destination collision")
	}
	e2.data.Version = 2
	u2.path = filepath.Join(backupDir, "invalid.lwv")
	if e2.Backup() == nil {
		t.Fatal("invalid state exported")
	}
	fresh, freshUI, _ := newEngine(t, []model.Adapter{evm.New()})
	freshUI.path = filepath.Join(backupDir, "missing.lwv")
	if fresh.Restore() == nil {
		t.Fatal("unavailable private backup")
	}
	fresh.Lock()
	if _, err := fresh.account("missing"); err == nil {
		t.Fatal("locked account lookup")
	}
}
func TestTP022DerivationAndDescriptorFailureBoundaries(t *testing.T) {
	adapter := &signerFaultAdapter{Adapter: evm.New()}
	e, u, _ := newEngine(t, []model.Adapter{adapter})
	createImported(t, e, u, "testnet")
	e.data.Entropy = []byte{1}
	if e.AddAccount(1) == nil {
		t.Fatal("malformed root derived")
	}
	e.data.Entropy = make([]byte, 16)
	adapter.deriveError = true
	if e.AddAccount(1) == nil {
		t.Fatal("derivation failure ignored")
	}
	if e.validateAccounts(e.data) == nil {
		t.Fatal("failed descriptor derivation")
	}
	adapter.deriveError = false
	adapter.emptyID = true
	if e.AddAccount(1) == nil {
		t.Fatal("account without durable identity accepted")
	}
	adapter.emptyID = false
	if err := e.AddAccount(1); err != nil {
		t.Fatal(err)
	}
	p := evmPlan(findAccount(t, e, "ethereum-sepolia"))
	p.Account.Index = 1 // no matching descriptor; ordinary immutable comparison rejects.
	if e.Prepare(p) == nil {
		t.Fatal("foreign account index")
	}
	e.data.Environment = "mainnet"
	e.data.RecoveryConfirmed = false
	p = evmPlan(e.data.Accounts[0])
	if e.Prepare(p) == nil {
		t.Fatal("mainnet send before verified backup")
	}
	e.data.Environment = "testnet"
	e.data.RecoveryConfirmed = true
	p = evmPlan(e.data.Accounts[0])
	e.data.Operations = []journal.Operation{{Plan: p, Digest: journal.Digest(p), State: "CANCELLED"}}
	if e.Prepare(p) == nil {
		t.Fatal("reused operation ID")
	}
	if e.Update(p.ID, "SUBMITTING", nil) == nil {
		t.Fatal("invalid journal transition")
	}
}
func TestTP042FeePolicyNativeLabels(t *testing.T) {
	for _, tc := range []struct {
		adapter               model.Adapter
		network, asset, label string
	}{{ton.New(), "ton-testnet", "ton-testnet:TON", "TON protocol has no signed fee cap"}, {evm.New(), "base-sepolia", "base-sepolia:native", "rollup charges are protocol-assessed"}, {near.New(), "near-testnet", "near-testnet:native", "NEAR protocol has no signed fee cap"}} {
		t.Run(tc.network, func(t *testing.T) {
			fake := &engineOnlyPlanAdapter{signerFaultAdapter: &signerFaultAdapter{Adapter: tc.adapter, mode: "public-only"}}
			e, u, _ := newEngine(t, []model.Adapter{fake})
			createImported(t, e, u, "testnet")
			a := findAccount(t, e, tc.network)
			p := model.Plan{ID: "native-fee-policy-public-test", Account: a, Asset: tc.asset, FeeAsset: tc.asset, Recipient: a.Address, Amount: "1", MaxFee: "1", Nonce: "1", Expires: time.Now().Unix() + 60, Payload: json.RawMessage(`{}`)}
			if err := e.Prepare(p); err != nil {
				t.Fatal(err)
			}
			u.inputs = append(u.inputs, input(syntheticPassword))
			if _, err := e.Sign(p.ID); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, summary := range u.summaries {
				if strings.Contains(summary, tc.label) {
					found = true
				}
			}
			if !found {
				t.Fatal("native fee reserve was presented as an enforceable cap")
			}
		})
	}
}
func TestTP031SaveQRShortWriteFailsClosed(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "ethereum-sepolia")
	u.path = filepath.Join(t.TempDir(), "short.png")
	var limits syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limits); err != nil {
		t.Fatal(err)
	}
	constrained := limits
	constrained.Cur = 128
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &constrained); err != nil {
		t.Fatal(err)
	}
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limits)
	if e.SaveQR(a.ID) == nil {
		t.Fatal("short QR write accepted")
	}
}
func TestTP032ReceiveFailurePreservesDCRSequence(t *testing.T) {
	for _, mode := range []string{"invalid-descriptor", "persistence"} {
		e, u, _ := newEngine(t, []model.Adapter{decred.New()})
		createImported(t, e, u, "testnet")
		a := findAccount(t, e, "decred-simnet")
		if mode == "invalid-descriptor" {
			for i := range e.data.Accounts {
				if e.data.Accounts[i].ID == a.ID {
					e.data.Accounts[i].Parameters = []byte(`{}`)
				}
			}
		} else {
			e.parameters = vault.Parameters{}
		}
		if _, err := e.Receive(a.ID); err == nil {
			t.Fatal("invalid receive sequence released")
		}
		if mode == "persistence" && !e.Snapshot().Locked {
			t.Fatal("undurable receive index left unlocked")
		}
	}
}
func TestTP022BoundedDiscoveryRequiresNativeApproval(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{decred.New(), evm.New()})
	if e.Discovery("missing", 0, 20, 100) == nil {
		t.Fatal("locked discovery")
	}
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "decred-simnet")
	evmAccount := findAccount(t, e, "ethereum-sepolia")
	if e.Discovery(evmAccount.ID, 0, 20, 100) == nil {
		t.Fatal("non-Decred discovery")
	}
	if e.Discovery(a.ID, 0, 0, 100) == nil {
		t.Fatal("zero gap")
	}
	if e.Discovery(a.ID, 0, 20, 10001) == nil {
		t.Fatal("unbounded scan")
	}
	u.approve = false
	if e.Discovery(a.ID, 0, 20, 100) == nil {
		t.Fatal("unapproved scan mutation")
	}
	u.approve = true
	if err := e.Discovery(a.ID, 123, 25, 200); err != nil {
		t.Fatal(err)
	}
	updated := findAccount(t, e, a.Network)
	var p decred.Parameters
	if err := json.Unmarshal(updated.Parameters, &p); err != nil {
		t.Fatal(err)
	}
	if p.Birthday != 123 || p.GapLimit != 25 || p.ScanLimit != 200 || updated.ID != a.ID || updated.Address != a.Address || !e.data.RecoveryConfirmed {
		t.Fatal("discovery changed root or lost explicit bounds")
	}
}
func TestReviewTP060DCRExhaustionCannotExposeUndurableArtifact(t *testing.T) {
	adapter := &engineOnlyPlanAdapter{signerFaultAdapter: &signerFaultAdapter{Adapter: decred.New(), mode: "public-only"}}
	e, u, _ := newEngine(t, []model.Adapter{adapter})
	createImported(t, e, u, "testnet")
	a := findAccount(t, e, "decred-simnet")
	var metadata decred.Parameters
	_ = json.Unmarshal(a.Parameters, &metadata)
	metadata.ChangeIndex = 999999
	a.Parameters, _ = json.Marshal(metadata)
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == a.ID {
			e.data.Accounts[i] = a
		}
	}
	p := model.Plan{ID: "public-dcr-exhausted-engine-test", Account: a, Asset: "decred-simnet:DCR", FeeAsset: "decred-simnet:DCR", Recipient: a.Address, Amount: "1", MaxFee: "1", Nonce: "1", Expires: time.Now().Unix() + 60, Payload: json.RawMessage(`{}`)}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if result, err := e.Sign(p.ID); err == nil || len(result.Bytes) != 0 {
		t.Fatal("exhausted change sequence exposed artifact")
	}
	if e.Pending() != nil || !e.Snapshot().Locked {
		t.Fatal("undurable DCR artifact reachable after change exhaustion")
	}
}
func TestTP031FinalVaultShortWritePreservesPriorGeneration(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	prior, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	} // A longer generation makes the new ciphertext larger than the prior one.
	e.data.Generation = 99
	var limits syscall.Rlimit
	if err = syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limits); err != nil {
		t.Fatal(err)
	}
	constrained := limits
	constrained.Cur = uint64(len(prior))
	if err = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &constrained); err != nil {
		t.Fatal(err)
	}
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limits)
	if e.persist() == nil || !e.Snapshot().Locked {
		t.Fatal("short final vault write accepted")
	}
	actual, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(prior, actual) {
		t.Fatal("short write replaced the recoverable prior generation")
	}
}
