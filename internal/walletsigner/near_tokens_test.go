package walletsigner

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
)

func publicDispatchNEP141Token(network string) model.NEP141Token {
	const contract = "token.testnet"
	return model.NEP141Token{Network: network, Asset: model.Asset{ID: network + ":nep141:" + contract,
		Symbol: "PUBLIC", Decimals: 0, Contract: contract}, CodeHash: base58.Encode(bytes.Repeat([]byte{0x42}, 32))}
}

func TestNEP141TokenPolicyHandlerIsStrictReadOnlyAndRequiresNoPrompt(t *testing.T) {
	engine, ui := dispatchEngine(t, "testnet", true)
	for _, unlocked := range []bool{false, true} {
		if unlocked {
			unlockFixture(t, engine, ui)
		}
		before, err := engine.Files.Read("vault.lwv")
		if err != nil {
			t.Fatal(err)
		}
		inputs, confirmations := ui.inputCalls, ui.confirmCalls
		if out := dispatch(t, engine, "nearTokenPolicies", struct{}{}); string(out) != "null" {
			t.Fatal("empty token policies returned unexpected authority or metadata")
		}
		for _, body := range []string{"", "null", "[]", `{"unexpected":true}`, `{"accountId":"arbitrary"}`, `{"seed":null}`} {
			message := request("nearTokenPolicies", struct{}{})
			message.Body = []byte(body)
			if out, err := handle(engine, message, "testnet"); err == nil || out != nil {
				t.Fatal("read-only policy endpoint accepted a malformed/nonempty body")
			}
		}
		after, err := engine.Files.Read("vault.lwv")
		if err != nil || !bytes.Equal(before, after) || ui.inputCalls != inputs || ui.confirmCalls != confirmations {
			t.Fatal("read-only policy metadata modified ciphertext or requested private authority")
		}
	}
}

func TestNEP141TokenImportHandlerRejectsMalformedBodiesBeforeUIOrPersistence(t *testing.T) {
	engine, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, engine, ui)
	account := accountFor(t, engine, "near")
	token := publicDispatchNEP141Token(account.Network)
	valid, err := json.Marshal(map[string]any{"accountId": account.ID, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if json.Unmarshal(valid, &base) != nil {
		t.Fatal("public policy fixture invalid")
	}
	malformed := []string{"", "null", "[]", "{}", `{"token":null}`, `{"accountId":null}`, `{"accountId":1,"token":null}`, string(valid) + "{}"}
	for _, tc := range []struct{ old, next string }{
		{`"accountId":"` + account.ID + `"`, `"accountId":null`},
		{`"accountId":"` + account.ID + `"`, `"accountId":{}`},
		{`"accountId":"` + account.ID + `"`, `"accountId":"foreign"`},
		{`"token":{`, `"unexpected":true,"token":{`},
		{`"network":`, `"network":null,"network":`},
		{`"asset":{`, `"asset":{"unexpected":true,`},
		{`"symbol":"PUBLIC"`, `"symbol":null`},
		{`"symbol":"PUBLIC"`, `"symbol":"PUBLIC","symbol":"OTHER"`},
		{`"decimals":0`, `"decimals":null`},
		{`"decimals":0,`, ``},
		{`"decimals":0`, `"decimals":"0"`},
		{`"decimals":0`, `"decimals":-1`},
		{`"decimals":0`, `"decimals":256`},
		{`"codeHash":`, `"unknown":true,"codeHash":`},
	} {
		changed := strings.Replace(string(valid), tc.old, tc.next, 1)
		if changed == string(valid) {
			t.Fatal("malformed child-body fixture did not change its intended field")
		}
		malformed = append(malformed, changed)
	}
	for _, field := range []string{"accountId", "token"} {
		copy := map[string]any{}
		for key, value := range base {
			if key != field {
				copy[key] = value
			}
		}
		raw, _ := json.Marshal(copy)
		malformed = append(malformed, string(raw))
	}
	before, err := engine.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	state := engine.Snapshot()
	inputs, confirmations := ui.inputCalls, ui.confirmCalls
	for _, body := range malformed {
		message := request("importNearToken", struct{}{})
		message.Body = []byte(body)
		if out, err := handle(engine, message, "testnet"); err == nil || out != nil {
			t.Fatal("malformed/null/missing child token metadata reached approval")
		}
	}
	after, err := engine.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) || ui.inputCalls != inputs || ui.confirmCalls != confirmations ||
		len(engine.NEP141TokenPolicies()) != 0 || !reflect.DeepEqual(engine.Snapshot(), state) {
		t.Fatal("invalid token request changed storage, account state or private UI authority")
	}
}

func TestNEP141TokenImportHandlerAuthenticatesExplicitZeroPrecisionAndReturnsPublicSnapshot(t *testing.T) {
	engine, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, engine, ui)
	account := accountFor(t, engine, "near")
	token := publicDispatchNEP141Token(account.Network)
	before := engine.Snapshot()
	ui.inputs = append(ui.inputs, publicInput(publicPassword))
	inputs, confirmations := ui.inputCalls, ui.confirmCalls
	raw := dispatch(t, engine, "importNearToken", map[string]any{"accountId": account.ID, "token": token})
	var result signer.SnapshotPage
	if json.Unmarshal(raw, &result) != nil || result.Locked || !result.RecoveryConfirmed || result.TotalOperations != 0 ||
		!reflect.DeepEqual(result.Accounts, before.Accounts) || ui.inputCalls != inputs+1 || ui.confirmCalls != confirmations+1 {
		t.Fatal("valid token handler did not authenticate exactly once or changed canonical wallets")
	}
	for _, secret := range []string{publicPassword, publicPhrase, `"entropy"`, `"passphrase"`, `"bytes"`} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("public import result disclosed private input or key state")
		}
	}
	for _, expected := range []string{token.Asset.Contract, token.CodeHash, "Decimals: 0", account.Network, account.Address} {
		if !strings.Contains(ui.summaries[len(ui.summaries)-1], expected) {
			t.Fatal("private import approval omitted exact identity or zero precision")
		}
	}
	var policies []model.NEP141Token
	if json.Unmarshal(dispatch(t, engine, "nearTokenPolicies", struct{}{}), &policies) != nil || len(policies) != 1 || policies[0] != token {
		t.Fatal("read-only child policy result differs from the approved metadata")
	}
	policies[0].Asset.Decimals = 18
	if engine.NEP141TokenPolicies()[0] != token {
		t.Fatal("returned policy metadata aliased signer-owned state")
	}
	ciphertext, err := engine.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := vault.Open(ciphertext, []byte(publicPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	stored, err := vault.Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Destroy()
	if len(stored.NEP141Tokens) != 1 || stored.NEP141Tokens[0] != token || !reflect.DeepEqual(stored.Accounts, before.Accounts) || len(stored.Operations) != 0 {
		t.Fatal("handler returned before durably encrypting the exact policy")
	}
	inputs, confirmations = ui.inputCalls, ui.confirmCalls
	denied(t, engine, "importNearToken", map[string]any{"accountId": account.ID, "token": token})
	if ui.inputCalls != inputs || ui.confirmCalls != confirmations {
		t.Fatal("duplicate import reached a private approval boundary")
	}
}

func TestNEP141TokenImportPrivatePromptRejectsForeignFrameSessionMethodAndPromptID(t *testing.T) {
	for _, mode := range []string{"request-id", "session", "method", "prompt-id"} {
		t.Run(mode, func(t *testing.T) {
			engine, ui := dispatchEngine(t, "testnet", true)
			unlockFixture(t, engine, ui)
			account := accountFor(t, engine, "near")
			before, err := engine.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			engine.UI = &uiprompt.UI{}
			inR, inW := io.Pipe()
			outR, outW := io.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer inR.Close()
				defer outW.Close()
				serve(&ipc.Channel{R: inR, W: outW}, engine, "testnet")
			}()
			t.Cleanup(func() {
				inW.Close()
				outR.Close()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("hostile token prompt left the signer dispatch running")
				}
			})
			channel := &ipc.Channel{R: outR, W: inW}
			message := request("importNearToken", map[string]any{"accountId": account.ID, "token": publicDispatchNEP141Token(account.Network)})
			if channel.Write(message) != nil {
				t.Fatal("token request framing failed")
			}
			frame, err := channel.Read()
			if err != nil || frame.Method != "prompt" || frame.ID != message.ID || frame.Session != message.Session {
				t.Fatal("token import did not bind its exact pending private prompt")
			}
			prompt, err := uiprompt.DecodePrompt(frame.Body)
			clear(frame.Body)
			if err != nil || prompt.Kind != "confirm" {
				t.Fatal("token import skipped private contract confirmation")
			}
			answer := uiprompt.Reply{PromptID: prompt.ID, Accepted: true}
			if mode == "prompt-id" {
				answer.PromptID = strings.Repeat("f", 32)
			}
			body, _ := json.Marshal(answer)
			reply := ipc.Message{Version: 1, ID: message.ID, Session: message.Session, Method: "promptReply", Body: body}
			switch mode {
			case "request-id":
				reply.ID = "foreign-token-request"
			case "session":
				reply.Session = "foreign-token-session"
			case "method":
				reply.Method = "importNearToken"
			}
			if channel.Write(reply) != nil {
				t.Fatal("hostile prompt reply framing failed")
			}
			clear(body)
			result, err := channel.Read()
			if err != nil || result.Method != "importNearToken" || result.ID != message.ID || result.Session != message.Session || result.Error == "" || string(result.Body) != "null" {
				t.Fatal("foreign prompt reply acquired import authority")
			}
			if _, err := channel.Read(); err == nil {
				t.Fatal("signer continued after a private prompt protocol violation")
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("rejected private token prompt did not retire")
			}
			after, err := engine.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(before, after) || len(engine.NEP141TokenPolicies()) != 0 {
				t.Fatal("rejected prompt persisted imported token metadata")
			}
		})
	}
}
