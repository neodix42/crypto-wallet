package walletcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/supervisor"
	"localwallet/internal/uiprompt"
)

func TestAccountRecoveryThroughActualCoordinatorAndSigner(t *testing.T) {
	dir, original, operation := tonProfileEncryptedFixture(t)
	child := &supervisor.Child{Path: "/proc/self/exe", SelfRole: "--internal-signer", Data: dir, Environment: "mainnet"}
	c := &core{child: child, adapters: chains.All(), last: time.Now()}
	t.Cleanup(c.lock)
	var wire bytes.Buffer
	c.publicChannel = &ipc.Channel{W: &wire}
	accepted, expectedTitle := true, ""
	confirmations, inputs := 0, 0
	child.OnPrompt = func(body json.RawMessage) error {
		if err := c.relayPrompt(body); err != nil {
			return err
		}
		forwarded, err := (&ipc.Channel{R: &wire}).Read()
		if err != nil || forwarded.Method != "prompt" {
			return fmt.Errorf("recovery prompt did not reach the main UI")
		}
		prompt, err := uiprompt.DecodePrompt(forwarded.Body)
		if err != nil {
			return err
		}
		reply := uiprompt.Reply{PromptID: prompt.ID, Accepted: accepted}
		switch prompt.Kind {
		case "input":
			inputs++
			if prompt.Title != "Unlock local wallet" {
				return fmt.Errorf("local recovery requested another credential")
			}
			reply.Values, reply.Accepted = []string{tonProfileTestPassword}, true
		case "confirm":
			confirmations++
			if prompt.Title != expectedTitle {
				return fmt.Errorf("unexpected recovery confirmation")
			}
			if strings.Contains(prompt.Title, "Recover bounded") && !strings.Contains(prompt.Summary, "Existing keys, profiles, names and original pending operations are preserved") {
				return fmt.Errorf("recovery did not explain its exact bounded effect")
			}
		default:
			return fmt.Errorf("recovery unexpectedly opened another dialog")
		}
		raw, _ := json.Marshal(reply)
		return c.replyPrompt(ipc.Message{Version: 1, ID: forwarded.ID, Session: forwarded.Session, Method: "promptReply", Body: raw})
	}
	call := func(method string, body any) (snapshot, error) {
		request := message(method, body)
		request.Session = "actual-account-recovery-session"
		out, err := c.handle(request)
		if err != nil {
			return snapshot{}, err
		}
		state, ok := out.(snapshot)
		if !ok {
			return snapshot{}, fmt.Errorf("recovery did not return an account snapshot")
		}
		return state, nil
	}
	state, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(state.Accounts, original) {
		t.Fatal("public fixture unlock failed", err)
	}
	expectedTitle = "Recover bounded account index range"
	before, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil {
		t.Fatal(err)
	}
	state, err = call("discoverAccounts", map[string]uint32{"start": 0, "end": 0})
	after, readErr := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil || readErr != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(state.Accounts, original) {
		t.Fatal("already present recovery failed or rewrote the encrypted vault", err, readErr)
	}
	for _, body := range []any{map[string]uint32{}, map[string]uint32{"start": 0}, map[string]uint32{"start": 0, "end": 20}, map[string]uint32{"start": 1000000, "end": 1000000}} {
		if _, err = call("discoverAccounts", body); ipc.PublicErrorCode(err) != "ACCOUNT_RECOVERY_INVALID_RANGE" {
			t.Fatal("invalid range lost its safe reason through the actual process roles", err)
		}
	}
	expectedTitle = "Rename wallet"
	if _, err = call("renameWallet", map[string]string{"accountId": original[0].ID, "name": "Recovered reserve"}); err != nil {
		t.Fatal(err)
	}
	expectedTitle = "Delete account group from visible list"
	if _, err = call("deleteGroup", map[string]uint32{"index": 0}); err != nil {
		t.Fatal(err)
	}
	expectedTitle = "Recover bounded account index range"
	state, err = call("discoverAccounts", map[string]uint32{"start": 0, "end": 1})
	if err != nil || len(state.Accounts) != 2*len(original) || state.Presentation.DeletedGroups["0"] || state.Presentation.WalletLabels[original[0].ID] != "Recovered reserve" {
		t.Fatal("recovery did not restore archived identities and derive only missing slots", err)
	}
	for i, account := range state.Accounts[:len(original)] {
		if account.Hidden || !reflect.DeepEqual(account, original[i]) {
			t.Fatal("recovery replaced an existing wallet identity or retained hidden state")
		}
	}
	accepted = false
	if _, err = call("discoverAccounts", map[string]uint32{"start": 2, "end": 2}); ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
		t.Fatal("recovery cancellation lost its category", err)
	}
	verifyOperation := func() {
		var actual journal.Operation
		if err := c.childCallAt(c.epoch, "operation", map[string]string{"id": operation.Plan.ID}, &actual); err != nil || !reflect.DeepEqual(actual, operation) {
			t.Fatal("recovery changed original signed UNKNOWN bytes or reservation", err)
		}
	}
	verifyOperation()
	c.lock()
	reopened, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(reopened.Accounts, state.Accounts) || !reflect.DeepEqual(reopened.Presentation, state.Presentation) {
		t.Fatal("recovered state changed after real signer restart", err)
	}
	verifyOperation()
	if confirmations != 5 || inputs != 2 {
		t.Fatal("invalid range requested approval or local recovery requested extra authentication", confirmations, inputs)
	}
}
