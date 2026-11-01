package walletcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/supervisor"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
	"localwallet/internal/walletsigner"
)

// Only the test image handles this role here. It runs the actual portable
// signer entry point, storage and framed prompt service, without a fake child
// implementation or a production-only test capability.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--internal-signer" {
		if len(os.Args) != 6 || os.Args[2] != "--data" || os.Args[4] != "--environment" {
			os.Exit(2)
		}
		os.Args = append(os.Args[:1], os.Args[2:]...)
		flag.CommandLine = flag.NewFlagSet("actual-test-signer", flag.ContinueOnError)
		walletsigner.MainPortable()
		return
	}
	os.Exit(m.Run())
}

const tonProfileTestPassword = "PUBLIC-TEST-only-TON-profile-password-79531"

func tonProfileEncryptedFixture(t *testing.T) (string, []model.Account, journal.Operation) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "disposable-profile")
	files, err := vault.NewFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	entropy, err := derivation.Import([]byte("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(entropy)
	seed, err := derivation.Seed(entropy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(seed)
	data := vault.Data{Version: 1, WalletID: strings.Repeat("b", 32), Generation: 1, Scheme: "bip39-english-v1", Entropy: entropy, Environment: "mainnet", RecoveryConfirmed: true, Birthday: time.Now().Unix(), Endpoints: map[string]vault.Endpoint{}}
	var source model.Account
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if network.Testnet {
				continue
			}
			account, err := adapter.Derive(seed, 0, network.ID)
			if err != nil {
				t.Fatal(err)
			}
			data.Accounts = append(data.Accounts, account)
			if network.ID == "ton-mainnet" {
				source = account
			}
		}
	}
	if source.Profile != ton.ProfileSubwallet42 {
		t.Fatal("fixture did not create the current canonical TON profile")
	}
	payload, _ := json.Marshal(ton.Payload{Kind: "deployment", Deploy: true, SourceBalance: "1000000000", EstimatedFee: "1000000", StorageReserve: "10000000", MasterSeqno: 123})
	plan := model.Plan{Account: source, Asset: "ton-mainnet:TON", Recipient: source.Address, Amount: "0", MaxFee: "3000000", FeeAsset: "ton-mainnet:TON", Nonce: "0", Expires: time.Now().Add(time.Minute).Unix(), Payload: payload}
	raw, _ := json.Marshal(plan)
	digest := sha256.Sum256(raw)
	plan.ID = hex.EncodeToString(digest[:])
	signed, err := ton.New().Sign(seed, plan)
	if err != nil {
		t.Fatal("offline public-fixture signature", err)
	}
	operation := journal.Operation{Plan: plan, Digest: journal.Digest(plan), Signed: &signed, State: "UNKNOWN", CreatedAt: time.Now().Unix()}
	data.Operations = []journal.Operation{operation}
	plain, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	ciphertext, err := vault.Seal(plain, []byte(tonProfileTestPassword), vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err = files.Write("vault.lwv", ciphertext, false); err != nil {
		t.Fatal(err)
	}
	return dir, append([]model.Account(nil), data.Accounts...), operation
}

func TestTONProfileRecoveryThroughActualCoordinatorAndSignerProcess(t *testing.T) {
	dir, originalAccounts, originalOperation := tonProfileEncryptedFixture(t)
	child := &supervisor.Child{Path: "/proc/self/exe", SelfRole: "--internal-signer", Data: dir, Environment: "mainnet"}
	c := &core{child: child, adapters: chains.All(), last: time.Now()}
	t.Cleanup(c.lock)
	var wire bytes.Buffer
	c.publicChannel = &ipc.Channel{W: &wire}
	confirm := true
	confirmations := 0
	child.OnPrompt = func(body json.RawMessage) error {
		if err := c.relayPrompt(body); err != nil {
			return err
		}
		forwarded, err := (&ipc.Channel{R: &wire}).Read()
		if err != nil || forwarded.Method != "prompt" {
			t.Error("actual signer prompt lost public framing", err)
			return err
		}
		prompt, err := uiprompt.DecodePrompt(forwarded.Body)
		if err != nil {
			return err
		}
		answer := uiprompt.Reply{PromptID: prompt.ID, Accepted: true}
		switch prompt.Kind {
		case "input":
			if prompt.Title != "Unlock local wallet" || len(prompt.Labels) != 1 {
				t.Error("unexpected secret request during public profile recovery")
			}
			answer.Values = []string{tonProfileTestPassword}
		case "confirm":
			confirmations++
			if (prompt.Title != "Add exact TON recovery profile" && prompt.Title != "Recover existing TON profile") || !strings.Contains(prompt.Summary, "Network: ton-mainnet\nIndex: 0\nRecovery profile: "+ton.Profile+"\nReceiving address:") {
				t.Error("actual signer did not confirm exact selected contract")
			}
			answer.Accepted = confirm
		default:
			t.Error("unexpected actual profile prompt", prompt.Kind)
		}
		raw, _ := json.Marshal(answer)
		return c.replyPrompt(ipc.Message{Version: 1, ID: forwarded.ID, Session: forwarded.Session, Method: "promptReply", Body: raw})
	}
	call := func(method string, body any) (snapshot, error) {
		t.Helper()
		request := message(method, body)
		request.Session = "actual-ton-profile-recovery-session"
		out, err := c.handle(request)
		if err != nil {
			return snapshot{}, err
		}
		state, ok := out.(snapshot)
		if !ok {
			t.Fatal("coordinator did not return a public snapshot")
		}
		return state, nil
	}
	initial, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(initial.Accounts, originalAccounts) {
		t.Fatal("actual signer could not open exact encrypted fixture", err)
	}
	confirm = false
	if _, err = call("addTonAccount", map[string]any{"index": 0, "profile": ton.Profile}); err == nil {
		t.Fatal("cancelled profile creation succeeded")
	}
	confirm = true
	recovered, err := call("addTonAccount", map[string]any{"index": 0, "profile": ton.Profile})
	if err != nil || len(recovered.Accounts) != len(originalAccounts)+1 || !reflect.DeepEqual(recovered.Accounts[:len(originalAccounts)], originalAccounts) {
		t.Fatal("actual core/signer 42-to-0 recovery changed existing identities", err)
	}
	legacy := recovered.Accounts[len(recovered.Accounts)-1]
	if legacy.Profile != ton.Profile || legacy.Index != 0 || confirmations != 2 {
		t.Fatal("actual profile selection/confirmation was lost")
	}
	before, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := call("addTonAccount", map[string]any{"index": 0, "profile": ton.Profile})
	after, readErr := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil || readErr != nil || !reflect.DeepEqual(duplicate.Accounts, recovered.Accounts) || !bytes.Equal(before, after) || confirmations != 3 {
		t.Fatal("existing visible profile was duplicated or rewritten", err, readErr)
	}
	hidden, err := call("hideAccount", map[string]any{"accountId": legacy.ID, "hidden": true})
	if err != nil || !hidden.Accounts[len(hidden.Accounts)-1].Hidden {
		t.Fatal("fixture could not hide original legacy profile", err)
	}
	confirm = false
	if _, err = call("addTonAccount", map[string]any{"index": 0, "profile": ton.Profile}); err == nil {
		t.Fatal("cancelled existing-profile recovery changed visibility")
	}
	var stillHidden model.Account
	if err := c.childCallAt(c.epoch, "account", map[string]string{"accountId": legacy.ID}, &stillHidden); err != nil || !stillHidden.Hidden {
		t.Fatal("cancelled hidden profile was exposed", err)
	}
	confirm = true
	visible, err := call("addTonAccount", map[string]any{"index": 0, "profile": ton.Profile})
	if err != nil || !reflect.DeepEqual(visible.Accounts, recovered.Accounts) || confirmations != 5 {
		t.Fatal("recovering a hidden profile changed more than visibility", err)
	}
	var operation journal.Operation
	if err := c.childCallAt(c.epoch, "operation", map[string]string{"id": originalOperation.Plan.ID}, &operation); err != nil || !reflect.DeepEqual(operation, originalOperation) {
		t.Fatal("profile addition changed original signed UNKNOWN operation", err)
	}
	c.lock()
	reopened, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(reopened.Accounts, recovered.Accounts) {
		t.Fatal("new signer process did not retain both exact profiles", err)
	}
	if err := c.childCallAt(c.epoch, "operation", map[string]string{"id": originalOperation.Plan.ID}, &operation); err != nil || !reflect.DeepEqual(operation, originalOperation) {
		t.Fatal("restart changed original pending identity or signed bytes", err)
	}
}
