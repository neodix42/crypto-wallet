package signer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const syntheticPassword = "CANARY-public-synthetic-password-79326"
const syntheticPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type testUI struct {
	inputs          [][][]byte
	path            string
	approve         bool
	summaries       []string
	copied          string
	recoveryPhrase  []byte
	recoveryApprove *bool
	recoveryHook    func()
}

func (u *testUI) Recovery(_ string, phrase []byte) bool {
	clear(u.recoveryPhrase)
	u.recoveryPhrase = bytes.Clone(phrase)
	if u.recoveryHook != nil {
		u.recoveryHook()
	}
	if u.recoveryApprove != nil {
		return *u.recoveryApprove
	}
	return u.approve
}

func (u *testUI) Input(_ string, _ ...string) ([][]byte, error) {
	if len(u.inputs) == 0 {
		return nil, ErrDenied
	}
	in := u.inputs[0]
	u.inputs = u.inputs[1:]
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = bytes.Clone(in[i])
	}
	return out, nil
}
func (u *testUI) Confirm(_ string, s string) bool {
	u.summaries = append(u.summaries, s)
	return u.approve
}
func (u *testUI) File(string, bool) (string, error) {
	if u.path == "" {
		return "", ErrDenied
	}
	return u.path, nil
}
func (u *testUI) Receive(_ string, s string, _ []byte) bool {
	u.summaries = append(u.summaries, s)
	return u.approve
}
func (u *testUI) CopyPublic(s string) { u.copied = s }
func input(s ...string) [][]byte {
	out := make([][]byte, len(s))
	for i := range s {
		out[i] = []byte(s[i])
	}
	return out
}
func newEngine(t *testing.T, adapters []model.Adapter) (*Engine, *testUI, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "profile")
	f, e := vault.NewFiles(dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close() })
	ui := &testUI{approve: true}
	engine := New(f, ui, adapters)
	engine.parameters = vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4}
	t.Cleanup(engine.Lock)
	return engine, ui, dir
}
func createImported(t *testing.T, e *Engine, u *testUI, environment string) {
	t.Helper()
	u.inputs = append(u.inputs, input(syntheticPassword, syntheticPassword, syntheticPhrase, ""))
	if err := e.Create(environment, 12, true); err != nil {
		t.Fatal(err)
	}
}
func TestTP032AllProfilesCleanPortableRecovery(t *testing.T) {
	e, u, dir := newEngine(t, chains.All())
	if !e.Snapshot().Locked {
		t.Fatal("starts unlocked")
	}
	createImported(t, e, u, "testnet")
	initial := e.Snapshot()
	families := map[string]bool{}
	for _, a := range initial.Accounts {
		families[a.Family] = true
	}
	if len(families) != 6 {
		t.Fatal("required family omitted")
	}
	if err := e.AddAccount(1); err != nil {
		t.Fatal(err)
	}
	if e.AddAccount(1) == nil || e.AddAccount(1000001) == nil {
		t.Fatal("index policy")
	}
	for _, a := range e.Snapshot().Accounts {
		if a.Family == "decred" {
			if _, err := e.Receive(a.ID); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	backupDir := filepath.Join(t.TempDir(), "backup")
	if err := os.Mkdir(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(backupDir, "portable.lwv")
	if err := e.Backup(); err != nil {
		t.Fatal(err)
	}
	expected := e.Snapshot()
	if !expected.RecoveryConfirmed {
		t.Fatal("backup not verified")
	}
	f, e2 := vault.NewFiles(filepath.Join(t.TempDir(), "fresh-machine"))
	if e2 != nil {
		t.Fatal(e2)
	}
	defer f.Close()
	restoredUI := &testUI{approve: true, path: u.path, inputs: [][][]byte{input(syntheticPassword)}}
	restored := New(f, restoredUI, chains.All())
	restored.parameters = e.parameters
	defer restored.Lock()
	if err := restored.Restore(); err != nil {
		t.Fatal(err)
	}
	got := restored.Snapshot()
	b1, _ := json.Marshal(expected.Accounts)
	b2, _ := json.Marshal(got.Accounts)
	if !bytes.Equal(b1, b2) {
		t.Fatal("clean recovery descriptor mismatch")
	}
	if restored.Restore() == nil {
		t.Fatal("destination collision")
	}
	u.inputs = append(u.inputs, input(syntheticPassword, "CANARY-public-new-password-16482", "CANARY-public-new-password-16482"))
	if err := e.ChangePassword(); err != nil {
		t.Fatal(err)
	}
	if !e.Snapshot().RecoveryConfirmed {
		t.Fatal("storage password rotation revoked recorded phrase")
	}
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.Unlock() == nil {
		t.Fatal("old current password")
	}
	u.inputs = append(u.inputs, input("CANARY-public-new-password-16482"))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(e.Snapshot().Accounts)
	if !bytes.Equal(actual, b1) {
		t.Fatal("rotation changed addresses")
	}
	// Canary scans use public synthetic secrets; never print matching data.
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range []string{syntheticPassword, syntheticPhrase, "CANARY-public-new-password-16482"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("secret canary leaked to file")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(e.Snapshot())
	if bytes.Contains(public, []byte(syntheticPassword)) || bytes.Contains(public, []byte(syntheticPhrase)) || bytes.Contains(public, []byte(`"entropy"`)) {
		t.Fatal("public IPC leakage")
	}
}
func evmPlan(a model.Account) model.Plan {
	p := model.Plan{Account: a, Asset: a.Network + ":native", Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789", MaxFee: "50400000025200", FeeAsset: a.Network + ":native", Nonce: "7", Expires: time.Now().Add(time.Minute).Unix()}
	p.Payload = []byte(fmt.Sprintf(`{"chainId":"11155111","to":"%s","value":"123456789","data":"0x","gas":"25200","feeCap":"2000000001","tipCap":"1","dataFeeReserve":"0","operatorFeeReserve":"0","stateBlock":"0x10","stateHash":"0x%s","stateTime":"0x%x"}`, p.Recipient, strings.Repeat("a", 64), time.Now().Unix()))
	p.ID = "evm:" + journal.Digest(p)
	return p
}
func TestTP042TP060AuthorizationAndDurableSignedArtifact(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	var a model.Account
	for _, ac := range e.Snapshot().Accounts {
		if ac.Network == "ethereum-sepolia" {
			a = ac
		}
	}
	p := evmPlan(a)
	changed := p
	changed.Recipient = "0x2222222222222222222222222222222222222222"
	if e.Prepare(changed) == nil {
		t.Fatal("changed recipient accepted")
	}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	if e.Prepare(p) == nil {
		t.Fatal("conflicting nonce accepted")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	signed, err := e.Sign(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed.Bytes) == 0 || signed.Identity == "" {
		t.Fatal("no artifact")
	}
	encrypted, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(encrypted, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	disk, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Destroy()
	if len(disk.Operations) != 1 || disk.Operations[0].State != "SIGNED_NOT_SUBMITTED" || disk.Operations[0].Signed.Identity != signed.Identity {
		t.Fatal("signed artifact returned before journal durability")
	}
	if _, err = e.Sign(p.ID); err == nil {
		t.Fatal("one-use approval replay")
	}
	public, _ := json.Marshal(e.Snapshot())
	if bytes.Contains(public, []byte(`"bytes"`)) {
		t.Fatal("raw signing artifact exposed in public snapshot")
	}
	if err = e.Update(p.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	if err = e.Update(p.ID, "UNKNOWN", &model.Receipt{Identity: signed.Identity, State: "UNKNOWN"}); err != nil {
		t.Fatal(err)
	}
	e.Lock()
	if _, err = e.Sign(p.ID); err == nil {
		t.Fatal("signed while locked")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	if e.Pending()[0].State != "UNKNOWN" || e.Prepare(p) == nil {
		t.Fatal("ambiguous payment released reservation")
	}
}
func TestTP042CancelledAndExpiredAuthorization(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	if e.Unlock() == nil || e.AddAccount(0) == nil || e.Backup() == nil || e.ChangePassword() == nil || e.Prepare(model.Plan{}) == nil {
		t.Fatal("locked operation")
	}
	createImported(t, e, u, "testnet")
	var a model.Account
	for _, ac := range e.Snapshot().Accounts {
		if ac.Network == "ethereum-sepolia" {
			a = ac
		}
	}
	p := evmPlan(a)
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	u.approve = false
	u.inputs = append(u.inputs, input(syntheticPassword))
	if _, err := e.Sign(p.ID); err == nil {
		t.Fatal("cancelled confirmation signed")
	}
	if e.Pending()[0].State != "CANCELLED" {
		t.Fatal("cancel journal")
	}
	if len(u.inputs) != 1 {
		t.Fatal("cancelled confirmation requested credentials")
	}
	u.inputs = nil
	u.approve = true
	p.Nonce = "8"
	p.ID = ""
	p.ID = "evm:" + journal.Digest(p)
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input("wrong synthetic password"))
	if _, err := e.Sign(p.ID); err == nil {
		t.Fatal("wrong reauthentication signed")
	}
	p.Expires = time.Now().Unix() - 1
	p.ID = ""
	p.ID = "evm:" + journal.Digest(p)
	if e.Prepare(p) == nil {
		t.Fatal("expired plan")
	}
	if e.Update("missing", "CONFIRMED", nil) == nil {
		t.Fatal("unknown operation")
	}
	if e.Create("testnet", 12, false) == nil {
		t.Fatal("overwrite existing root")
	}
}
func TestTP033PendingRecoveryGateAndNoRepeatedDisclosure(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "mainnet")
	e.data.RecoveryConfirmed = false
	a := e.Snapshot().Accounts[0]
	if _, err := e.Receive(a.ID); err == nil {
		t.Fatal("mainnet deposit before backup")
	}
	if _, err := e.Receive("missing"); err == nil {
		t.Fatal("unknown account")
	}
	if e.CopyReceive(a.ID) == nil {
		t.Fatal("copy before backup")
	}
	if e.Configure("unknown") == nil {
		t.Fatal("unknown network")
	}
	if e.Associate(a) == nil {
		t.Fatal("unsupported association")
	}
	e.Lock()
	if e.Settings() != nil || e.Pending() != nil {
		t.Fatal("locked metadata")
	}
	if e.Restore() == nil {
		t.Fatal("restore collision")
	}
}
