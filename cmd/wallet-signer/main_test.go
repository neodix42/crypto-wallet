package main

// These tests use only the published BIP39 zero-entropy example. They exercise
// the real signer, encrypted private files and EIP-1559 serializer; no adapter
// or signature is mocked and no transaction is submitted to any network.
import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
	"localwallet/internal/derivation"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/secureui"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const publicPassword = "PUBLIC-TEST-ONLY-storage-password-31954"
const publicNewPassword = "PUBLIC-TEST-ONLY-replacement-password-97124"
const publicPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

var signerTestDisplay string
var signerTestXvfb *exec.Cmd

func TestMain(m *testing.M) {
	code := m.Run()
	if signerTestXvfb != nil {
		_ = signerTestXvfb.Process.Kill()
		_ = signerTestXvfb.Wait()
	}
	os.Exit(code)
}

type dispatchUI struct {
	inputs                                            [][][]byte
	path                                              string
	approve                                           bool
	inputCalls, fileCalls, confirmCalls, receiveCalls int
	summaries                                         []string
	copied                                            string
}

func publicInput(s ...string) [][]byte {
	out := make([][]byte, len(s))
	for i := range s {
		out[i] = []byte(s[i])
	}
	return out
}
func (u *dispatchUI) Input(string, ...string) ([][]byte, error) {
	u.inputCalls++
	if len(u.inputs) == 0 {
		return nil, signer.ErrDenied
	}
	in := u.inputs[0]
	u.inputs = u.inputs[1:]
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = bytes.Clone(in[i])
	}
	return out, nil
}
func (u *dispatchUI) Confirm(_ string, summary string) bool {
	u.confirmCalls++
	u.summaries = append(u.summaries, summary)
	return u.approve
}
func (u *dispatchUI) File(string, bool) (string, error) {
	u.fileCalls++
	if u.path == "" {
		return "", signer.ErrDenied
	}
	return u.path, nil
}
func (u *dispatchUI) Receive(_ string, summary string, png []byte) bool {
	u.receiveCalls++
	u.summaries = append(u.summaries, summary)
	return u.approve && bytes.HasPrefix(png, []byte("\x89PNG\r\n\x1a\n"))
}
func (u *dispatchUI) CopyPublic(s string) { u.copied = s }

func dispatchEngine(t *testing.T, environment string, withVault bool) (*signer.Engine, *dispatchUI) {
	t.Helper()
	f, err := vault.NewFiles(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	u := &dispatchUI{approve: true}
	adapters := []model.Adapter{evm.New(), near.New(), decred.New()}
	// Use the production format's explicitly permitted 64-MiB/3-pass/4-lane
	// floor for test cost. Restore the public default before returning; no
	// production configuration or validation floor is changed.
	previous := vault.Default
	vault.Default = vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4}
	e := signer.New(f, u, adapters)
	vault.Default = previous
	e.Environment = environment
	t.Cleanup(e.Lock)
	if withVault {
		entropy, err := derivation.Import([]byte(publicPhrase))
		if err != nil {
			t.Fatal(err)
		}
		defer clear(entropy)
		seed, err := derivation.Seed(entropy, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer clear(seed)
		d := vault.Data{Version: 1, WalletID: strings.Repeat("a", 32), Generation: 1, Scheme: "bip39-english-v1", Entropy: entropy, Environment: environment, Birthday: time.Now().Unix(), Endpoints: map[string]vault.Endpoint{}}
		ids := []string{"ethereum-sepolia", "near-testnet", "decred-testnet3"}
		if environment == "mainnet" {
			ids = []string{"ethereum-mainnet", "near-mainnet", "decred-mainnet"}
		}
		for i, a := range adapters {
			ac, err := a.Derive(seed, 0, ids[i])
			if err != nil {
				t.Fatal(err)
			}
			d.Accounts = append(d.Accounts, ac)
		}
		plain, err := d.Encode()
		if err != nil {
			t.Fatal(err)
		}
		defer clear(plain)
		sealed, err := vault.Seal(plain, []byte(publicPassword), vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Write("vault.lwv", sealed, false); err != nil {
			t.Fatal(err)
		}
	}
	return e, u
}
func request(method string, body any) ipc.Message {
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return ipc.Message{Version: 1, ID: "public-request", Session: "public-test-session-39175", Method: method, Body: b}
}
func dispatch(t *testing.T, e *signer.Engine, method string, body any) json.RawMessage {
	t.Helper()
	out, err := handle(e, request(method, body), "testnet")
	if err != nil {
		t.Fatalf("%s denied: %v", method, err)
	}
	if !json.Valid(out) {
		t.Fatalf("%s returned invalid JSON", method)
	}
	return out
}
func denied(t *testing.T, e *signer.Engine, method string, body any) {
	t.Helper()
	out, err := handle(e, request(method, body), "testnet")
	if err == nil || out != nil {
		t.Fatalf("%s accepted unauthorized request", method)
	}
}
func unlockFixture(t *testing.T, e *signer.Engine, u *dispatchUI) {
	t.Helper()
	u.inputs = append(u.inputs, publicInput(publicPassword))
	dispatch(t, e, "unlock", struct{}{})
	if e.Snapshot().Locked {
		t.Fatal("unlock did not change state")
	}
}
func accountFor(t *testing.T, e *signer.Engine, family string) model.Account {
	t.Helper()
	for _, a := range e.Snapshot().Accounts {
		if a.Family == family && a.Index == 0 {
			return a
		}
	}
	t.Fatal("missing fixture account", family)
	return model.Account{}
}
func publicPlan(a model.Account) model.Plan {
	p := model.Plan{Account: a, Asset: a.Network + ":native", Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789", MaxFee: "50400000025200", FeeAsset: a.Network + ":native", Nonce: "7", Expires: time.Now().Add(time.Minute).Unix()}
	p.Payload, _ = json.Marshal(map[string]string{"chainId": "11155111", "to": p.Recipient, "value": p.Amount, "data": "0x", "gas": "25200", "feeCap": "2000000001", "tipCap": "1", "dataFeeReserve": "0", "operatorFeeReserve": "0", "stateBlock": "0x10", "stateHash": "0x" + strings.Repeat("a", 64), "stateTime": fmt.Sprintf("0x%x", time.Now().Unix())})
	p.ID = "evm:" + journal.Digest(p)
	return p
}

func TestDispatchTypedTransferPersistsRealSignatureAndRecoveryIdentity(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	if !bytes.Contains(dispatch(t, e, "snapshot", struct{}{}), []byte(`"locked":true`)) {
		t.Fatal("initial snapshot unlocked")
	}
	unlockFixture(t, e, u)
	a := accountFor(t, e, "evm")
	p := publicPlan(a)
	dispatch(t, e, "prepare", p)
	if len(e.Pending()) != 1 || e.Pending()[0].State != "PREPARED" {
		t.Fatal("prepare journal missing")
	}
	denied(t, e, "prepare", p)
	u.inputs = append(u.inputs, publicInput(publicPassword))
	raw := dispatch(t, e, "sign", map[string]string{"id": p.ID})
	var signed model.Signed
	if json.Unmarshal(raw, &signed) != nil || signed.Identity == "" || len(signed.Bytes) == 0 {
		t.Fatal("real signed transaction missing")
	}
	var tx types.Transaction
	if tx.UnmarshalBinary(signed.Bytes) != nil {
		t.Fatal("transaction is not EIP-2718 bytes")
	}
	owner, err := types.Sender(types.NewLondonSigner(tx.ChainId()), &tx)
	if err != nil || owner != common.HexToAddress(a.Address) || tx.ChainId().Uint64() != 11155111 || tx.Type() != types.DynamicFeeTxType || tx.Nonce() != 7 || tx.To() == nil || *tx.To() != common.HexToAddress(p.Recipient) || tx.Value().String() != p.Amount || len(tx.Data()) != 0 || tx.Hash().Hex() != signed.Identity {
		t.Fatal("signature or transaction effects differ from native-approved plan")
	}
	if len(u.summaries) == 0 || !strings.Contains(u.summaries[len(u.summaries)-1], "Atomic amount: "+p.Amount) || !strings.Contains(u.summaries[len(u.summaries)-1], "Destination: "+p.Recipient) {
		t.Fatal("native approval did not include exact chain effects")
	}
	encrypted, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(encrypted, []byte(publicPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	d, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Destroy()
	if len(d.Operations) != 1 || d.Operations[0].State != "SIGNED_NOT_SUBMITTED" || d.Operations[0].Signed == nil || !bytes.Equal(d.Operations[0].Signed.Bytes, signed.Bytes) {
		t.Fatal("sign returned before signed identity was durably encrypted")
	}
	for _, secret := range []string{publicPassword, publicPhrase, string(plain)} {
		if bytes.Contains(encrypted, []byte(secret)) {
			t.Fatal("plaintext appears in encrypted private file")
		}
	}
	snapshot := dispatch(t, e, "snapshot", struct{}{})
	for _, secret := range []string{publicPassword, publicPhrase, `"entropy"`, `"passphrase"`, `"bytes"`} {
		if bytes.Contains(snapshot, []byte(secret)) {
			t.Fatal("snapshot exposed private data or signed bytes")
		}
	}
	var pending []journal.Operation
	if json.Unmarshal(dispatch(t, e, "pending", struct{}{}), &pending) != nil || len(pending) != 1 || pending[0].Signed == nil || !bytes.Equal(pending[0].Signed.Bytes, signed.Bytes) {
		t.Fatal("private pending journal lost exact signed identity")
	}
	denied(t, e, "sign", map[string]string{"id": p.ID})
	denied(t, e, "update", map[string]any{"id": p.ID, "state": "CONFIRMED", "receipt": model.Receipt{Identity: "unrelated", State: "CONFIRMED"}})
	dispatch(t, e, "update", map[string]any{"id": p.ID, "state": "SUBMITTING", "receipt": nil})
	dispatch(t, e, "update", map[string]any{"id": p.ID, "state": "UNKNOWN", "receipt": model.Receipt{Identity: signed.Identity, State: "UNKNOWN", Detail: "No network submission performed by this dispatch test"}})
	if e.Pending()[0].State != "UNKNOWN" {
		t.Fatal("unknown broadcast identity not held")
	}
	dispatch(t, e, "lock", struct{}{})
	if !e.Snapshot().Locked {
		t.Fatal("lock failed")
	}
	unlockFixture(t, e, u)
	if len(e.Pending()) != 1 || e.Pending()[0].State != "UNKNOWN" || e.Pending()[0].Signed.Identity != signed.Identity {
		t.Fatal("recovery changed unresolved identity")
	}
}

func TestDispatchPublicReceiveQRConfigurationAssociationAndDiscovery(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, u)
	a := accountFor(t, e, "evm")
	n := accountFor(t, e, "near")
	d := accountFor(t, e, "decred")
	var result struct {
		PNG     []byte        `json:"png"`
		Account model.Account `json:"account"`
	}
	if json.Unmarshal(dispatch(t, e, "receive", map[string]string{"accountId": a.ID}), &result) != nil || result.Account.ID != a.ID || !bytes.HasPrefix(result.PNG, []byte("\x89PNG")) {
		t.Fatal("verified public QR missing")
	}
	if string(dispatch(t, e, "copyReceive", map[string]string{"accountId": a.ID})) != "null" || u.copied != a.Address {
		t.Fatal("copyReceive copied a different value")
	}
	u.path = filepath.Join(t.TempDir(), "public-address.png")
	dispatch(t, e, "saveQR", map[string]string{"accountId": a.ID})
	if stat, err := os.Stat(u.path); err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("public QR file missing or wrong mode")
	}
	var decoded string
	if json.Unmarshal(dispatch(t, e, "scanQR", struct{}{}), &decoded) != nil || decoded != a.Address {
		t.Fatal("QR scan did not recover public address")
	}
	denied(t, e, "saveQR", map[string]string{"accountId": a.ID})
	denied(t, e, "receive", map[string]string{"accountId": "foreign"})
	denied(t, e, "copyReceive", map[string]string{"accountId": "foreign"})
	u.inputs = append(u.inputs, publicInput("https://rpc.example.invalid", "public-test-user", "public-node-password", "public-node-api-key"))
	dispatch(t, e, "configure", map[string]string{"network": a.Network})
	var settings map[string]vault.Endpoint
	if json.Unmarshal(dispatch(t, e, "settings", struct{}{}), &settings) != nil || settings[a.Network].URL != "https://rpc.example.invalid" || settings[a.Network].APIKey != "public-node-api-key" {
		t.Fatal("private core settings lost configured endpoint")
	}
	// Association to the same canonical implicit descriptor exercises the
	// dispatch/native approval path without asserting any network authority.
	dispatch(t, e, "associate", n)
	denied(t, e, "associate", a)
	dispatch(t, e, "discovery", map[string]any{"accountId": d.ID, "birthday": 1, "gap": 10, "limit": 100})
	dispatch(t, e, "discovery", map[string]any{"accountId": d.ID, "birthday": 1, "gap": 10, "limit": 100, "blockBudget": 2000000})
	denied(t, e, "discovery", map[string]any{"accountId": d.ID, "birthday": 1, "gap": 10, "limit": 100, "blockBudget": 2000001})
	denied(t, e, "discovery", map[string]any{"accountId": a.ID, "birthday": 1, "gap": 10, "limit": 100})
	denied(t, e, "discovery", map[string]any{"accountId": d.ID, "birthday": 1, "gap": 0, "limit": 100})
	dispatch(t, e, "addAccount", map[string]uint32{"index": 1})
	if len(e.Snapshot().Accounts) <= 3 {
		t.Fatal("addAccount did not persist deterministic public accounts")
	}
	denied(t, e, "addAccount", map[string]uint32{"index": 1})
	denied(t, e, "addAccount", map[string]uint32{"index": 1 << 31})
	dispatch(t, e, "discoverAccounts", map[string]uint32{"start": 127, "end": 129})
	indices := map[uint32]bool{}
	for _, account := range e.Snapshot().Accounts { indices[account.Index] = true }
	for _, index := range []uint32{0, 1, 127, 128, 129} { if !indices[index] { t.Fatal("manual recovery range omitted a canonical index") } }
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 20, "end": 19})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 0, "end": 20})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 1000000, "end": 1000000})
	dispatch(t, e, "hideAccount", map[string]any{"accountId": a.ID, "hidden": true})
	hidden := accountFor(t, e, "evm")
	if !hidden.Hidden || hidden.Index != a.Index || hidden.ID != a.ID || hidden.Address != a.Address { t.Fatal("hiding changed the recovery identity") }
	dispatch(t, e, "hideAccount", map[string]any{"accountId": a.ID, "hidden": false})
	denied(t, e, "hideAccount", map[string]any{"accountId": "foreign", "hidden": true})
	u.approve = false
	denied(t, e, "receive", map[string]string{"accountId": a.ID})
	u.inputs = append(u.inputs, publicInput("https://rejected.example.invalid", "", "", ""))
	denied(t, e, "configure", map[string]string{"network": a.Network})
	if e.Settings()[a.Network].URL != "https://rpc.example.invalid" {
		t.Fatal("cancelled configuration changed durable endpoint")
	}
}

func TestDispatchEncryptedBackupRestoreAndPasswordRotation(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, u)
	u.path = filepath.Join(t.TempDir(), "encrypted-backup", "portable.lwv")
	dispatch(t, e, "backup", struct{}{})
	if !e.Snapshot().BackupVerified {
		t.Fatal("backup did not verify recovery")
	}
	r, v := dispatchEngine(t, "testnet", false)
	v.path = u.path
	v.inputs = append(v.inputs, publicInput(publicPassword))
	dispatch(t, r, "restore", struct{}{})
	x, _ := json.Marshal(e.Snapshot().Accounts)
	y, _ := json.Marshal(r.Snapshot().Accounts)
	if !bytes.Equal(x, y) || !r.Snapshot().BackupVerified {
		t.Fatal("portable restore changed public recovery descriptors")
	}
	denied(t, r, "restore", struct{}{})
	v.inputs = append(v.inputs, publicInput(publicPassword, publicNewPassword, publicNewPassword))
	dispatch(t, r, "password", struct{}{})
	if r.Snapshot().BackupVerified {
		t.Fatal("password rotation retained old backup approval")
	}
	dispatch(t, r, "lock", struct{}{})
	v.inputs = append(v.inputs, publicInput(publicPassword))
	denied(t, r, "unlock", struct{}{})
	v.inputs = append(v.inputs, publicInput(publicNewPassword))
	dispatch(t, r, "unlock", struct{}{})
	backup, err := os.ReadFile(u.path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := vault.Open(backup, []byte(publicPassword))
	if err != nil {
		t.Fatal("old portable backup lost old password")
	}
	clear(old)
	if _, err = vault.Open(backup, []byte(publicNewPassword)); err == nil {
		t.Fatal("new password unexpectedly decrypted old backup")
	}
}

func TestDispatchCreateImportAndFixedEnvironmentBoundaries(t *testing.T) {
	for _, method := range []string{"create", "import"} {
		t.Run(method, func(t *testing.T) {
			e, u := dispatchEngine(t, "testnet", false)
			in := publicInput(publicPassword, publicPassword)
			if method == "import" {
				in = publicInput(publicPassword, publicPassword, publicPhrase, "")
			}
			u.inputs = append(u.inputs, in)
			dispatch(t, e, method, map[string]int{"words": 12})
			if e.Snapshot().Locked || len(e.Snapshot().Accounts) == 0 || e.Snapshot().Environment != "testnet" {
				t.Fatal("creation/import did not produce encrypted usable wallet")
			}
			denied(t, e, method, map[string]int{"words": 12})
		})
	}
	e, u := dispatchEngine(t, "mainnet", true)
	e.Environment = ""
	u.inputs = append(u.inputs, publicInput(publicPassword))
	denied(t, e, "unlock", struct{}{})
	if !e.Snapshot().Locked {
		t.Fatal("mismatched environment remained unlocked")
	}
	// Verify the dispatch layer's second environment check independently of
	// Engine.Environment; production sets both to the same immutable profile.
	u.inputs = append(u.inputs, publicInput(publicPassword))
	if _, err := handle(e, request("unlock", struct{}{}), "mainnet"); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(t.TempDir(), "encrypted-backup", "mainnet-public-fixture.lwv")
	if _, err := handle(e, request("backup", struct{}{}), "mainnet"); err != nil {
		t.Fatal(err)
	}
	r, v := dispatchEngine(t, "testnet", false)
	r.Environment = ""
	v.path = u.path
	v.inputs = append(v.inputs, publicInput(publicPassword))
	denied(t, r, "restore", struct{}{})
	if !r.Snapshot().Locked {
		t.Fatal("wrong-profile restored wallet stayed unlocked")
	}
}

func TestDispatchRejectsMalformedUnknownSecretBearingAndLockedRequests(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", false)
	typed := []string{"create", "import", "addAccount", "hideAccount", "discoverAccounts", "receive", "copyReceive", "saveQR", "discovery", "associate", "configure", "prepare", "sign", "update"}
	empty := []string{"unlock", "lock", "snapshot", "settings", "scanQR", "backup", "restore", "password", "pending"}
	for _, method := range append(append([]string{}, typed...), empty...) {
		t.Run(method, func(t *testing.T) {
			before := u.inputCalls + u.confirmCalls + u.fileCalls + u.receiveCalls
			for _, body := range []string{`{"password":"PUBLIC-untrusted-credential"}`, `{"seed":"PUBLIC-untrusted-seed"}`, `{"mnemonic":"PUBLIC-untrusted-phrase"}`, `{"privateKey":"PUBLIC-untrusted-key"}`, `{"unknown":true}`, `{"index":1,"index":2}`, `{"index":1,"\u0069ndex":2}`, `{} {}`, `[]`, `null`, `{"words":`, strings.Repeat("[", 40) + strings.Repeat("]", 40)} {
				m := request(method, struct{}{})
				m.Body = []byte(body)
				if out, err := handle(e, m, "testnet"); err == nil || out != nil {
					t.Fatalf("accepted malformed/credential-bearing body for %s: %q", method, body)
				}
			}
			if after := u.inputCalls + u.confirmCalls + u.fileCalls + u.receiveCalls; before != after {
				t.Fatal("malformed request reached native UI")
			}
		})
	}
	for _, method := range []string{"unlock", "scanQR", "backup", "restore", "password"} {
		denied(t, e, method, struct{}{})
	}
	for _, method := range []string{"create", "import"} {
		denied(t, e, method, map[string]int{"words": 13})
	}
	denied(t, e, "addAccount", map[string]int{"index": 0})
	denied(t, e, "hideAccount", map[string]any{"accountId": "absent", "hidden": true})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 0, "end": 0})
	denied(t, e, "receive", map[string]string{"accountId": "absent"})
	denied(t, e, "copyReceive", map[string]string{"accountId": "absent"})
	denied(t, e, "saveQR", map[string]string{"accountId": "absent"})
	denied(t, e, "discovery", map[string]any{"accountId": "absent", "birthday": 0, "gap": 20, "limit": 100})
	denied(t, e, "associate", model.Account{})
	denied(t, e, "configure", map[string]string{"network": "unknown"})
	denied(t, e, "prepare", model.Plan{})
	denied(t, e, "sign", map[string]string{"id": "absent"})
	denied(t, e, "update", map[string]any{"id": "absent", "state": "CONFIRMED"})
	denied(t, e, "exportPrivateKey", struct{}{})
	if string(dispatch(t, e, "settings", struct{}{})) != "null" || string(dispatch(t, e, "pending", struct{}{})) != "null" {
		t.Fatal("locked handler retained private state")
	}
	dispatch(t, e, "lock", struct{}{})
}

type failedWriter struct{ err error }

func (w failedWriter) Write([]byte) (int, error) { return 0, w.err }
func TestRespondFramesExactCorrelationAndSanitizesDenial(t *testing.T) {
	m := request("snapshot", struct{}{})
	for _, wantError := range []bool{false, true} {
		var wire bytes.Buffer
		c := ipc.Channel{W: &wire}
		if err := respond(&c, m, func() (json.RawMessage, error) {
			if wantError {
				return []byte(`{"password":"PUBLIC-denied-output"}`), errors.New("PUBLIC-private-error-canary")
			}
			return []byte(`{"locked":true}`), nil
		}); err != nil {
			t.Fatal(err)
		}
		in := ipc.Channel{R: bytes.NewReader(wire.Bytes())}
		got, err := in.Read()
		if err != nil || got.ID != m.ID || got.Session != m.Session || got.Method != m.Method || got.Version != 1 {
			t.Fatal("response escaped request correlation")
		}
		if wantError {
			if got.Error != "REQUEST_DENIED" || string(got.Body) != "null" || bytes.Contains(wire.Bytes(), []byte("PUBLIC-")) {
				t.Fatal("denial leaked private data")
			}
		} else if got.Error != "" || string(got.Body) != `{"locked":true}` {
			t.Fatal("success response changed public body")
		}
		if _, err = in.Read(); !errors.Is(err, io.EOF) {
			t.Fatal("unexpected extra activity/final response")
		}
	}
	sentinel := errors.New("PUBLIC-pipe-closed")
	if err := respond(&ipc.Channel{W: failedWriter{sentinel}}, m, func() (json.RawMessage, error) { return []byte(`{}`), nil }); !errors.Is(err, sentinel) {
		t.Fatal("write failure was swallowed")
	}
	if err := respond(&ipc.Channel{W: io.Discard}, m, func() (json.RawMessage, error) { return []byte(`{"invalid":`), nil }); !errors.Is(err, ipc.ErrFrame) {
		t.Fatal("invalid public JSON was framed")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("unexpected panic recovery")
			}
		}()
		_ = respond(&ipc.Channel{W: io.Discard}, m, func() (json.RawMessage, error) { panic("PUBLIC-test-panic") })
	}()
	// A later request remains usable after panic cleanup; native activity's
	// actual X11 key/button and callback lifecycle has separate secureui tests.
	if err := respond(&ipc.Channel{W: io.Discard}, m, func() (json.RawMessage, error) { return []byte(`{}`), nil }); err != nil {
		t.Fatal(err)
	}
}

// os.Exit error paths run in this test-only helper; production has no injectable
// startup hooks. Success-loop coverage is measured in-process below.
func TestSignerStartupExitHelper(t *testing.T) {
	mode := os.Getenv("LOCALWALLET_SIGNER_TEST_HELPER")
	if mode == "" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("wallet-signer", flag.ContinueOnError)
	os.Args = []string{"wallet-signer", "-data", os.Getenv("LOCALWALLET_SIGNER_TEST_DIR"), "-environment", "testnet"}
	if mode == "environment" {
		os.Args[4] = "unsupported"
	}
	main()
	os.Exit(0)
}
func TestSignerRejectsInvalidStartupEnvironmentAndDisplay(t *testing.T) {
	for _, tc := range []struct {
		mode string
		code int
	}{{"environment", 2}, {"display", 4}} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignerStartupExitHelper$")
			cmd.Env = append(os.Environ(), "LOCALWALLET_SIGNER_TEST_HELPER="+tc.mode, "LOCALWALLET_SIGNER_TEST_DIR="+filepath.Join(t.TempDir(), "profile"), "DISPLAY=", "WAYLAND_DISPLAY=", "GDK_BACKEND=x11")
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != tc.code {
				t.Fatalf("startup %s exit mismatch: %v (%q)", tc.mode, err, out)
			}
		})
	}
}

func privateSignerDisplay(t *testing.T) {
	t.Helper()
	if strings.HasPrefix(os.Getenv("DISPLAY"), ":18") {
		return
	}
	if signerTestDisplay != "" {
		t.Setenv("DISPLAY", signerTestDisplay)
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("GDK_BACKEND", "x11")
		return
	}
	server, _ := exec.LookPath("Xvfb")
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(source), "..", "..")
	local := filepath.Join(root, ".tools", "xvfb", "root", "usr", "bin", "Xvfb")
	if server == "" {
		if st, err := os.Stat(local); err == nil && st.Mode().IsRegular() {
			server = local
		}
	}
	if server == "" {
		t.Skip("NOT_RUN: isolated signer startup requires test-only Xvfb")
	}
	for n := 189; n >= 180; n-- {
		socket := fmt.Sprintf("/tmp/.X11-unix/X%d", n)
		if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := os.Lstat(fmt.Sprintf("/tmp/.X%d-lock", n)); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		display := fmt.Sprintf(":%d", n)
		cmd := exec.Command(server, display, "-screen", "0", "1280x1024x24", "-nolisten", "tcp")
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Join(root, ".tools", "xvfb", "root", "usr", "lib", "x86_64-linux-gnu")+":"+os.Getenv("LD_LIBRARY_PATH"))
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatalf("BLOCKED: private Xvfb startup: %v", err)
		}
		signerTestXvfb = cmd
		for start := time.Now(); time.Since(start) < 5*time.Second; {
			if _, err := os.Stat(socket); err == nil {
				signerTestDisplay = display
				t.Setenv("DISPLAY", display)
				t.Setenv("WAYLAND_DISPLAY", "")
				t.Setenv("GDK_BACKEND", "x11")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("BLOCKED: private Xvfb did not become ready")
	}
	t.Fatal("BLOCKED: no unused private :18x Xvfb display")
}

type countedFailedWriter struct {
	calls int
	err   error
}

func (w *countedFailedWriter) Write([]byte) (int, error) { w.calls++; return 0, w.err }

func TestRespondNativeActivityIsCorrelatedValueFreeAndCleared(t *testing.T) {
	privateSignerDisplay(t)
	if err := secureui.Init(); err != nil {
		runtime.UnlockOSThread()
		t.Fatalf("BLOCKED: native GTK initialization: %v", err)
	}
	defer runtime.UnlockOSThread()
	_, source, _, _ := runtime.Caller(0)
	module := filepath.Join(filepath.Dir(source), "..", "..", "tests", "e2e")
	const script = `import ctypes as C,sys,time
sys.path.insert(0,sys.argv[1])
from x11 import Desktop,X
d=Desktop(sys.argv[2].encode());w=d.wait(sys.argv[3])[0]
X.XSetInputFocus.argtypes=[C.c_void_p,C.c_ulong,C.c_int,C.c_ulong]
X.XSetInputFocus(d.d,w,2,0);X.XFlush(d.d)
d.key('a');time.sleep(1.1);d.key('b');d.key('Escape')
`
	drive := func(title string) func() {
		cmd := exec.Command("/usr/bin/python3", "-c", script, module, os.Getenv("DISPLAY"), title)
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return func() {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("BLOCKED: actual private X11 input failed: %v: %s", err, output.String())
			}
		}
	}
	m := request("sign", map[string]string{"id": "public-activity-operation"})
	var wire bytes.Buffer
	wait := drive("TEST signer response public activity")
	if err := respond(&ipc.Channel{W: &wire}, m, func() (json.RawMessage, error) {
		if secureui.Confirm("TEST signer response public activity", "Public fixture: no credentials or transaction submission") {
			t.Error("X11 cancellation unexpectedly approved")
		}
		return []byte(`{"password":"PUBLIC-redacted-output"}`), errors.New("PUBLIC-redacted-error")
	}); err != nil {
		t.Fatal(err)
	}
	wait()
	r := ipc.Channel{R: bytes.NewReader(wire.Bytes())}
	activities := 0
	for {
		frame, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if frame.ID != m.ID || frame.Session != m.Session {
			t.Fatal("native activity escaped request correlation")
		}
		if frame.Method == "activity" {
			activities++
			if string(frame.Body) != "{}" || frame.Error != "" {
				t.Fatal("native input values appeared in activity frame")
			}
		} else if frame.Method != m.Method || frame.Error != "REQUEST_DENIED" || string(frame.Body) != "null" {
			t.Fatal("final native response was not sanitized")
		}
	}
	if activities < 2 || bytes.Contains(wire.Bytes(), []byte("PUBLIC-redacted")) {
		t.Fatal("real key activity missing or private denial leaked")
	}
	before := wire.Len()
	wait = drive("TEST signer response after cleanup")
	_ = secureui.Confirm("TEST signer response after cleanup", "No pending IPC request")
	wait()
	if wire.Len() != before {
		t.Fatal("native activity callback survived final response")
	}
	sentinel := errors.New("PUBLIC-activity-pipe-failure")
	writer := &countedFailedWriter{err: sentinel}
	wait = drive("TEST signer response broken private pipe")
	err := respond(&ipc.Channel{W: writer}, m, func() (json.RawMessage, error) {
		_ = secureui.Confirm("TEST signer response broken private pipe", "Public activity pipe failure fixture")
		return []byte(`{}`), nil
	})
	wait()
	if !errors.Is(err, sentinel) || writer.calls != 1 {
		t.Fatal("activity write failure retried or followed by final response")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("missing public test panic")
			}
		}()
		_ = respond(&ipc.Channel{W: &wire}, m, func() (json.RawMessage, error) { panic("PUBLIC-test-panic") })
	}()
	before = wire.Len()
	wait = drive("TEST signer response after panic")
	_ = secureui.Confirm("TEST signer response after panic", "No pending IPC request")
	wait()
	if wire.Len() != before || writer.calls != 1 {
		t.Fatal("native activity callback survived panic/error cleanup")
	}
}

func TestZZSignerMainUsesRealIsolationNativeInitAndBoundedIPC(t *testing.T) {
	// This test intentionally installs process-wide isolation. Keep it last and
	// never use a desktop user's display. CI provides a dedicated :18x Xvfb.
	privateSignerDisplay(t)
	previousArgs, previousFlags, previousIn, previousOut := os.Args, flag.CommandLine, os.Stdin, os.Stdout
	defer func() {
		os.Args = previousArgs
		flag.CommandLine = previousFlags
		os.Stdin = previousIn
		os.Stdout = previousOut
	}()
	for _, tc := range []struct {
		name         string
		messages     []ipc.Message
		tail         []byte
		responses    int
		closedOutput bool
	}{
		{"snapshot-lock-denial", []ipc.Message{request("snapshot", struct{}{}), func() ipc.Message { m := request("lock", struct{}{}); m.ID = "public-lock"; return m }(), func() ipc.Message { m := request("unknown", struct{}{}); m.ID = "public-denial"; return m }()}, nil, 3, false},
		{"replay-closes-pipe", []ipc.Message{request("snapshot", struct{}{}), request("snapshot", struct{}{})}, nil, 1, false},
		{"foreign-session-closes-pipe", []ipc.Message{request("snapshot", struct{}{}), func() ipc.Message {
			m := request("snapshot", struct{}{})
			m.ID = "different"
			m.Session = "foreign-test-session-1234"
			return m
		}()}, nil, 1, false},
		{"oversized-frame", nil, []byte{0, 16, 0, 1}, 0, false},
		{"truncated-frame", nil, []byte{0, 0, 0, 4, '{'}, 0, false},
		{"closed-response-pipe", []ipc.Message{request("snapshot", struct{}{})}, nil, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wire bytes.Buffer
			w := ipc.Channel{W: &wire}
			for _, m := range tc.messages {
				if err := w.Write(m); err != nil {
					t.Fatal(err)
				}
			}
			wire.Write(tc.tail)
			in, err := os.CreateTemp(t.TempDir(), "public-input")
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err = in.Write(wire.Bytes()); err != nil {
				t.Fatal(err)
			}
			if _, err = in.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			out, err := os.CreateTemp(t.TempDir(), "public-output")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			if tc.closedOutput {
				_ = out.Close()
			}
			os.Stdin, os.Stdout = in, out
			flag.CommandLine = flag.NewFlagSet("wallet-signer", flag.ContinueOnError)
			os.Args = []string{"wallet-signer", "-data", filepath.Join(t.TempDir(), "profile"), "-environment", "testnet"}
			// main and secureui.Init each lock the current OS thread.
			main()
			runtime.UnlockOSThread()
			runtime.UnlockOSThread()
			if tc.closedOutput {
				return
			}
			if _, err = out.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			r := ipc.Channel{R: out}
			for i := 0; i < tc.responses; i++ {
				got, err := r.Read()
				if err != nil {
					t.Fatal(err)
				}
				if got.ID != tc.messages[i].ID || got.Session != tc.messages[i].Session || got.Method != tc.messages[i].Method {
					t.Fatal("main response correlation mismatch")
				}
				if got.Method == "unknown" {
					if got.Error != "REQUEST_DENIED" || string(got.Body) != "null" {
						t.Fatal("main leaked unknown-command error")
					}
				} else if !bytes.Contains(got.Body, []byte(`"locked":true`)) {
					t.Fatal("main did not use real locked engine")
				}
			}
			if _, err = r.Read(); !errors.Is(err, io.EOF) {
				t.Fatal("main accepted replay, foreign session or invalid framing")
			}
		})
	}
	secureui.SetActivity(nil)
}
