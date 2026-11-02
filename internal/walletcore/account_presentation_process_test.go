package walletcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/supervisor"
	"localwallet/internal/uiprompt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Exercise actual prompt framing through both process roles; direct Engine or
// dispatch tests cannot prove that a new confirmation reaches the main UI.
func TestAccountPresentationThroughActualCoordinatorAndSigner(t *testing.T) {
	dir, original, operation := tonProfileEncryptedFixture(t)
	child := &supervisor.Child{Path: "/proc/self/exe", SelfRole: "--internal-signer", Data: dir, Environment: "mainnet"}
	c := &core{child: child, adapters: chains.All(), last: time.Now()}
	t.Cleanup(c.lock)
	var wire bytes.Buffer
	c.publicChannel = &ipc.Channel{W: &wire}
	accepted := true
	confirmationTitle := ""
	confirmations, inputs := 0, 0
	child.OnPrompt = func(body json.RawMessage) error {
		if err := c.relayPrompt(body); err != nil {
			return err
		}
		forwarded, err := (&ipc.Channel{R: &wire}).Read()
		if err != nil || forwarded.Method != "prompt" {
			return fmt.Errorf("confirmation lost framing: %v", err)
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
				return fmt.Errorf("metadata requested extra credential dialog")
			}
			reply.Values = []string{tonProfileTestPassword}
			reply.Accepted = true
		case "confirm":
			confirmations++
			if prompt.Title != confirmationTitle {
				return fmt.Errorf("wrong metadata confirmation title: %s", prompt.Title)
			}
			if prompt.Title == "Add deterministic account" && !strings.Contains(prompt.Summary, "account index 1 ") {
				return fmt.Errorf("next index was not confirmed exactly")
			}
		default:
			return fmt.Errorf("unexpected metadata dialog kind: %s", prompt.Kind)
		}
		raw, _ := json.Marshal(reply)
		return c.replyPrompt(ipc.Message{Version: 1, ID: forwarded.ID, Session: forwarded.Session, Method: "promptReply", Body: raw})
	}
	call := func(method string, body any) (snapshot, error) {
		request := message(method, body)
		request.Session = "actual-account-presentation-session"
		out, err := c.handle(request)
		if err != nil {
			return snapshot{}, err
		}
		state, ok := out.(snapshot)
		if !ok {
			return snapshot{}, fmt.Errorf("metadata response was not an account snapshot")
		}
		return state, nil
	}
	state, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(state.Accounts, original) {
		t.Fatal("fixture unlock failed", err)
	}
	confirmationTitle = "Add deterministic account"
	state, err = call("addNextAccount", struct{}{})
	if err != nil || len(state.Accounts) != 2*len(original) || state.NextAccountIndex == nil || *state.NextAccountIndex != 2 {
		t.Fatal("actual next-account confirmation failed", err)
	}
	target := operation.Plan.Account
	confirmationTitle = "Rename wallet"
	state, err = call("renameWallet", map[string]string{"accountId": target.ID, "name": "Main reserve"})
	if err != nil || state.Presentation.WalletLabels[target.ID] != "Main reserve" {
		t.Fatal("actual wallet rename failed", err)
	}
	confirmationTitle = "Rename account group"
	state, err = call("renameGroup", map[string]any{"index": 1, "name": "Savings"})
	if err != nil || state.Presentation.GroupLabels["1"] != "Savings" {
		t.Fatal("actual group rename failed", err)
	}
	confirmationTitle = "Delete wallet from visible account"
	accepted = false
	if _, err = call("deleteWallet", map[string]string{"accountId": target.ID}); ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
		t.Fatal("cancelled wallet archive lost its cancellation category", err)
	}
	accepted = true
	state, err = call("deleteWallet", map[string]string{"accountId": target.ID})
	if err != nil {
		t.Fatal("actual wallet archive failed", err)
	}
	found := false
	for _, a := range state.Accounts {
		if a.ID == target.ID {
			found = a.Hidden && a.Address == target.Address
		}
	}
	if !found {
		t.Fatal("wallet archive changed or lost its original identity")
	}
	confirmationTitle = "Delete account group from visible list"
	accepted = false
	if _, err = call("deleteGroup", map[string]uint32{"index": 1}); ipc.PublicErrorCode(err) != "REQUEST_CANCELLED" {
		t.Fatal("cancelled group archive lost its cancellation category", err)
	}
	accepted = true
	state, err = call("deleteGroup", map[string]uint32{"index": 1})
	if err != nil || !state.Presentation.DeletedGroups["1"] {
		t.Fatal("actual group archive failed", err)
	}
	for _, a := range state.Accounts {
		if a.Index == 1 && !a.Hidden {
			t.Fatal("group archive omitted a wallet")
		}
	}
	verifyOperation := func() {
		var actual journal.Operation
		if err := c.childCallAt(c.epoch, "operation", map[string]string{"id": operation.Plan.ID}, &actual); err != nil || !reflect.DeepEqual(actual, operation) {
			t.Fatal("presentation changed original signed UNKNOWN bytes or reservation", err)
		}
	}
	verifyOperation()
	c.lock()
	reopened, err := call("unlock", struct{}{})
	if err != nil || !reflect.DeepEqual(reopened.Accounts, state.Accounts) || !reflect.DeepEqual(reopened.Presentation, state.Presentation) {
		t.Fatal("metadata lost on real signer restart", err)
	}
	verifyOperation()
	if inputs != 2 || confirmations != 7 {
		t.Fatal("unexpected credential/confirmation count", inputs, confirmations)
	}
}
