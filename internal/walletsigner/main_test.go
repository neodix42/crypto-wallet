package walletsigner

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
	"localwallet/internal/signer"
	"localwallet/internal/vault"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const publicPassword = "PUBLIC-TEST-ONLY-storage-password-31954"
const publicNewPassword = "PUBLIC-TEST-ONLY-replacement-password-97124"
const publicPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

type dispatchUI struct {
	inputs                                                           [][][]byte
	path                                                             string
	approve                                                          bool
	inputCalls, fileCalls, confirmCalls, receiveCalls, recoveryCalls int
	summaries                                                        []string
	copied                                                           string
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
func (u *dispatchUI) Recovery(_ string, phrase []byte) bool {
	u.recoveryCalls++
	entropy, err := derivation.Import(phrase)
	clear(entropy)
	return u.approve && err == nil
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
		d := vault.Data{Version: 1, WalletID: strings.Repeat("a", 32), Generation: 1, Scheme: "bip39-english-v1", Entropy: entropy, Environment: environment, RecoveryConfirmed: true, Birthday: time.Now().Unix(), Endpoints: map[string]vault.Endpoint{}}
		for _, adapter := range adapters {
			for _, network := range adapter.Networks() {
				if network.Testnet != (environment == "testnet") {
					continue
				}
				account, err := adapter.Derive(seed, 0, network.ID)
				if err != nil {
					t.Fatal(err)
				}
				d.Accounts = append(d.Accounts, account)
			}
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
	denied(t, e, "cancelPrepared", map[string]string{"id": p.ID})
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
	initialAccounts := len(e.Snapshot().Accounts)
	dispatch(t, e, "addAccount", map[string]uint32{"index": 1})
	if len(e.Snapshot().Accounts) != initialAccounts*2 {
		t.Fatal("addAccount did not persist deterministic public accounts")
	}
	denied(t, e, "addAccount", map[string]uint32{"index": 1})
	denied(t, e, "addAccount", map[string]uint32{"index": 1 << 31})
	dispatch(t, e, "discoverAccounts", map[string]uint32{"start": 127, "end": 129})
	indices := map[uint32]bool{}
	for _, account := range e.Snapshot().Accounts {
		indices[account.Index] = true
	}
	for _, index := range []uint32{0, 1, 127, 128, 129} {
		if !indices[index] {
			t.Fatal("manual recovery range omitted a canonical index")
		}
	}
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 20, "end": 19})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 0, "end": 20})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 1000000, "end": 1000000})
	dispatch(t, e, "hideAccount", map[string]any{"accountId": a.ID, "hidden": true})
	hidden := accountFor(t, e, "evm")
	if !hidden.Hidden || hidden.Index != a.Index || hidden.ID != a.ID || hidden.Address != a.Address {
		t.Fatal("hiding changed the recovery identity")
	}
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

func TestDispatchPhraseRestoreAndPasswordRotationPreserveWallets(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, u)
	oldCiphertext, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	r, v := dispatchEngine(t, "testnet", false)
	v.inputs = append(v.inputs, publicInput(publicPassword, publicPassword, publicPhrase, ""))
	dispatch(t, r, "import", map[string]int{"words": 12})
	x, _ := json.Marshal(e.Snapshot().Accounts)
	y, _ := json.Marshal(r.Snapshot().Accounts)
	if !bytes.Equal(x, y) || !r.Snapshot().RecoveryConfirmed {
		t.Fatal("phrase restore changed canonical public wallets or left known-phrase confirmation incomplete")
	}
	for _, method := range []string{"backup", "restore", "confirmRecovery"} {
		denied(t, r, method, struct{}{})
	}
	v.inputs = append(v.inputs, publicInput(publicPassword, publicNewPassword, publicNewPassword))
	dispatch(t, r, "password", struct{}{})
	if !r.Snapshot().RecoveryConfirmed {
		t.Fatal("password rotation revoked recorded phrase confirmation")
	}
	rotated, _ := json.Marshal(r.Snapshot().Accounts)
	if !bytes.Equal(y, rotated) {
		t.Fatal("storage password changed blockchain identities")
	}
	dispatch(t, r, "lock", struct{}{})
	v.inputs = append(v.inputs, publicInput(publicPassword))
	denied(t, r, "unlock", struct{}{})
	v.inputs = append(v.inputs, publicInput(publicNewPassword))
	dispatch(t, r, "unlock", struct{}{})
	old, err := vault.Open(oldCiphertext, []byte(publicPassword))
	if err != nil {
		t.Fatal("copied older local ciphertext lost its original password")
	}
	clear(old)
	if _, err = vault.Open(oldCiphertext, []byte(publicNewPassword)); err == nil {
		t.Fatal("new password decrypted copied older ciphertext")
	}
	if v.fileCalls != 0 {
		t.Fatal("phrase restoration opened a file chooser")
	}
}

func TestDispatchPendingRecoveryRequiresReauthenticationAndReturnsOnlyPublicState(t *testing.T) {
	for _, environment := range []string{"testnet", "mainnet"} {
		e, u := dispatchEngine(t, environment, false)
		u.approve = false
		u.inputs = append(u.inputs, publicInput(publicPassword, publicPassword, publicPhrase, ""))
		if _, err := handle(e, request("import", map[string]int{"words": 12}), environment); err != nil {
			t.Fatal(err)
		}
		if e.Snapshot().Locked || e.Snapshot().RecoveryConfirmed {
			t.Fatal("cancelled native recovery acknowledgement did not leave the encrypted root pending")
		}
		account := accountFor(t, e, "evm")
		if _, err := handle(e, request("receive", map[string]string{"accountId": account.ID}), environment); err == nil {
			t.Fatal("pending recovery authorized receive")
		}
		u.inputs = append(u.inputs, publicInput("PUBLIC-wrong-app-password"))
		if _, err := handle(e, request("confirmRecovery", struct{}{}), environment); err == nil {
			t.Fatal("wrong app password opened phrase recording")
		}
		if u.recoveryCalls != 0 {
			t.Fatal("phrase recording occurred before reauthentication")
		}
		u.inputs = append(u.inputs, publicInput(publicPassword))
		raw, err := handle(e, request("confirmRecovery", struct{}{}), environment)
		if err != nil || !bytes.Contains(raw, []byte(`"recoveryConfirmed":false`)) {
			t.Fatal("cancelled phrase confirmation did not return pending snapshot", err)
		}
		u.approve = true
		u.inputs = append(u.inputs, publicInput(publicPassword))
		raw, err = handle(e, request("confirmRecovery", struct{}{}), environment)
		if err != nil || !bytes.Contains(raw, []byte(`"recoveryConfirmed":true`)) {
			t.Fatal("recorded phrase did not return confirmed snapshot", err)
		}
		for _, secret := range []string{publicPassword, publicPhrase, `"entropy"`, `"passphrase"`, `"backupVerified"`, `"mnemonic"`} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("confirmation leaked secret or obsolete authority field")
			}
		}
		before := u.inputCalls + u.recoveryCalls
		if _, err = handle(e, request("confirmRecovery", struct{}{}), environment); err == nil {
			t.Fatal("confirmed root could be revealed again")
		}
		if before != u.inputCalls+u.recoveryCalls || u.fileCalls != 0 {
			t.Fatal("confirmed root opened another native secret/file surface")
		}
	}
}

func TestDispatchCancelReleasesOnlyAnUnsignedPreparationDurably(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, u)
	p := publicPlan(accountFor(t, e, "evm"))
	dispatch(t, e, "prepare", p)
	dispatch(t, e, "cancelPrepared", map[string]string{"id": p.ID})
	if len(e.Pending()) != 1 || e.Pending()[0].State != "CANCELLED" || e.Pending()[0].Signed != nil || e.Pending()[0].Digest != journal.Digest(p) {
		t.Fatal("cancellation changed the original unsigned identity")
	}
	denied(t, e, "cancelPrepared", map[string]string{"id": p.ID})
	denied(t, e, "cancelPrepared", map[string]string{"id": "unknown"})
	dispatch(t, e, "lock", struct{}{})
	unlockFixture(t, e, u)
	if e.Pending()[0].State != "CANCELLED" {
		t.Fatal("unsigned cancellation was not persisted")
	}
	revised := p
	revised.ID = ""
	revised.Nonce = "8"
	revised.ID = "evm:" + journal.Digest(revised)
	dispatch(t, e, "prepare", revised)
	if len(e.Pending()) != 2 || e.Pending()[1].State != "PREPARED" {
		t.Fatal("explicit cancellation did not release the account reservation")
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
			if e.Snapshot().Locked || len(e.Snapshot().Accounts) == 0 || e.Snapshot().Environment != "testnet" || !e.Snapshot().RecoveryConfirmed {
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
	r, v := dispatchEngine(t, "mainnet", false)
	v.inputs = append(v.inputs, publicInput(publicPassword, publicPassword, publicPhrase, ""))
	denied(t, r, "import", map[string]int{"words": 12})
	if !r.Snapshot().Locked || v.inputCalls != 0 || r.Snapshot().WalletExists {
		t.Fatal("wrong-profile phrase restoration created or prompted for a different root")
	}
	// Phrase import selects its destination profile explicitly and never treats
	// an encrypted mainnet file as a testnet import source.
	for _, method := range []string{"backup", "restore"} {
		denied(t, e, method, struct{}{})
	}

}

func TestDispatchRejectsMalformedUnknownSecretBearingAndLockedRequests(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", false)
	typed := []string{"create", "import", "addAccount", "hideAccount", "discoverAccounts", "cancelPrepared", "receive", "copyReceive", "saveQR", "discovery", "associate", "configure", "prepare", "sign", "update"}
	empty := []string{"unlock", "lock", "snapshot", "settings", "scanQR", "confirmRecovery", "backupPhrase", "password", "pending"}
	for _, method := range append(append([]string{}, typed...), empty...) {
		t.Run(method, func(t *testing.T) {
			before := u.inputCalls + u.confirmCalls + u.fileCalls + u.receiveCalls + u.recoveryCalls
			for _, body := range []string{`{"password":"PUBLIC-untrusted-credential"}`, `{"seed":"PUBLIC-untrusted-seed"}`, `{"mnemonic":"PUBLIC-untrusted-phrase"}`, `{"privateKey":"PUBLIC-untrusted-key"}`, `{"unknown":true}`, `{"index":1,"index":2}`, `{"index":1,"\u0069ndex":2}`, `{} {}`, `[]`, `null`, `{"words":`, strings.Repeat("[", 40) + strings.Repeat("]", 40)} {
				m := request(method, struct{}{})
				m.Body = []byte(body)
				if out, err := handle(e, m, "testnet"); err == nil || out != nil {
					t.Fatalf("accepted malformed/credential-bearing body for %s: %q", method, body)
				}
			}
			if after := u.inputCalls + u.confirmCalls + u.fileCalls + u.receiveCalls + u.recoveryCalls; before != after {
				t.Fatal("malformed request reached native UI")
			}
		})
	}
	for _, method := range []string{"unlock", "scanQR", "confirmRecovery", "backupPhrase", "password"} {
		denied(t, e, method, struct{}{})
	}
	for _, method := range []string{"create", "import"} {
		denied(t, e, method, map[string]int{"words": 13})
	}
	denied(t, e, "addAccount", map[string]int{"index": 0})
	denied(t, e, "hideAccount", map[string]any{"accountId": "absent", "hidden": true})
	denied(t, e, "discoverAccounts", map[string]uint32{"start": 0, "end": 0})
	denied(t, e, "cancelPrepared", map[string]string{"id": "absent"})
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
func TestSignerRejectsInvalidEnvironmentAndStartsWithoutDisplay(t *testing.T) {
	for _, tc := range []struct {
		mode string
		code int
	}{{"environment", 2}, {"display", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSignerStartupExitHelper$")
			cmd.Env = append(os.Environ(), "LOCALWALLET_SIGNER_TEST_HELPER="+tc.mode, "LOCALWALLET_SIGNER_TEST_DIR="+filepath.Join(t.TempDir(), "profile"), "DISPLAY=", "WAYLAND_DISPLAY=", "GDK_BACKEND=x11")
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if (tc.code == 0 && err != nil) || (tc.code != 0 && (!errors.As(err, &exit) || exit.ExitCode() != tc.code)) {
				t.Fatalf("startup %s exit mismatch: %v (%q)", tc.mode, err, out)
			}
		})
	}
}

func TestServeHandlesIdleEOFAndFramedRepliesWithoutDisplay(t *testing.T) {
	e, _ := dispatchEngine(t, "testnet", false)
	var input, output bytes.Buffer
	enc := ipc.Channel{W: &input}
	if err := enc.Write(request("snapshot", struct{}{})); err != nil {
		t.Fatal(err)
	}
	serve(&ipc.Channel{R: &input, W: &output}, e, "testnet")
	dec := ipc.Channel{R: &output}
	got, err := dec.Read()
	if err != nil || got.Method != "snapshot" || !bytes.Contains(got.Body, []byte(`"locked":true`)) {
		t.Fatal("headless signer failed bounded request", err)
	}
	if _, err = dec.Read(); !errors.Is(err, io.EOF) {
		t.Fatal("unexpected extra prompt/response")
	}
}

func TestZZSignerMainUsesRealIsolationAndBoundedIPCWithoutDisplay(t *testing.T) {
	// Process-wide filters survive exec and cannot be removed. Keep this legacy
	// test in its own child so it cannot sandbox the later portable-role tests.
	if os.Getenv("CRYPTOWALLET_LEGACY_SIGNER_TEST") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestZZSignerMainUsesRealIsolationAndBoundedIPCWithoutDisplay$")
		for _, argument := range os.Args[1:] {
			if strings.HasPrefix(argument, "-test.gocoverdir=") {
				command.Args = append(command.Args, argument)
			}
		}
		command.Env = append(os.Environ(), "CRYPTOWALLET_LEGACY_SIGNER_TEST=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("legacy signer process failed: %v %s", err, output)
		}
		return
	}
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
			// The signer needs no native display or GUI event loop.
			main()
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
}
