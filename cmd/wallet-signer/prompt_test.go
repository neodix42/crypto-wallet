package main

import (
	"bytes"
	"encoding/json"
	"io"
	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func electronSigner(t *testing.T) *ipc.Channel {
	t.Helper()
	files, err := vault.NewFiles(filepath.Join(t.TempDir(), "fresh-profile"))
	if err != nil {
		t.Fatal(err)
	}
	previous := vault.Default
	vault.Default = vault.Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4}
	engine := signer.New(files, &uiprompt.UI{}, chains.All())
	vault.Default = previous
	engine.Environment = "mainnet"
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer outW.Close()
		defer inR.Close()
		defer engine.Lock()
		serve(&ipc.Channel{R: inR, W: outW}, engine, "mainnet")
	}()
	t.Cleanup(func() {
		inW.Close()
		outR.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("prompt signer failed to close")
		}
		files.Close()
	})
	return &ipc.Channel{R: outR, W: inW}
}

func electronCall(t *testing.T, c *ipc.Channel, id, method string, body any, reply func(uiprompt.Prompt) uiprompt.Reply) ipc.Message {
	t.Helper()
	raw, _ := json.Marshal(body)
	m := ipc.Message{Version: 1, ID: id, Session: "public-electron-fixture-session", Method: method, Body: raw}
	if e := c.Write(m); e != nil {
		t.Fatal("request framing failed")
	}
	seen := map[string]bool{}
	for {
		r, e := c.Read()
		if e != nil {
			t.Fatal("signer pipe closed unexpectedly")
		}
		if r.ID != id || r.Session != m.Session {
			t.Fatal("prompt escaped original operation")
		}
		if r.Method == method {
			return r
		}
		if r.Method != "prompt" || r.Error != "" || reply == nil {
			t.Fatal("unsolicited signer event")
		}
		p, e := uiprompt.DecodePrompt(r.Body)
		clear(r.Body)
		if e != nil || seen[p.ID] {
			t.Fatal("invalid or repeated prompt")
		}
		seen[p.ID] = true
		answer := reply(p)
		answer.PromptID = p.ID
		raw, e = json.Marshal(answer)
		if e != nil {
			t.Fatal("answer encoding failed")
		}
		if e = c.Write(ipc.Message{Version: 1, ID: id, Session: m.Session, Method: "promptReply", Body: raw}); e != nil {
			t.Fatal("answer framing failed")
		}
		clear(raw)
	}
}

func decodeSnapshot(t *testing.T, m ipc.Message) signer.Snapshot {
	t.Helper()
	var s signer.Snapshot
	if m.Error != "" || json.Unmarshal(m.Body, &s) != nil {
		t.Fatal("operation failed")
	}
	return s
}

func TestElectronPromptsRestoreFreshWalletsWithoutNativeWindowOrCopiedFiles(t *testing.T) {
	first := electronSigner(t)
	var phrase []byte
	defer func() { clear(phrase) }()
	confirm := false
	answer := func(p uiprompt.Prompt) uiprompt.Reply {
		r := uiprompt.Reply{Accepted: true}
		switch p.Kind {
		case "input":
			if strings.HasPrefix(p.Labels[0], "Recorded word") {
				words := bytes.Fields(phrase)
				r.Values = []string{string(words[1]), string(words[5]), string(words[len(words)-1])}
				if !confirm {
					r.Values[0] = "incorrect"
				}
			} else {
				r.Values = make([]string, len(p.Labels))
				for i := range r.Values {
					r.Values[i] = publicPassword
				}
			}
		case "recovery":
			if phrase == nil {
				phrase = []byte(p.Phrase)
			} else if !bytes.Equal(phrase, []byte(p.Phrase)) {
				t.Fatal("cancelled setup replaced recovery root")
			}
		case "notice":
			if p.Title != "Recovery phrase not confirmed" {
				t.Fatal("unexpected error notice")
			}
		default:
			t.Fatal("unexpected onboarding prompt")
		}
		return r
	}
	pending := decodeSnapshot(t, electronCall(t, first, "create", "create", map[string]int{"words": 12}, answer))
	if pending.Locked || pending.RecoveryConfirmed || !pending.WalletExists || len(pending.Accounts) != 9 {
		t.Fatal("incorrect-word challenge enabled wallet or lost durable root")
	}
	confirm = true
	ready := decodeSnapshot(t, electronCall(t, first, "confirm", "confirmRecovery", struct{}{}, answer))
	if !ready.RecoveryConfirmed || !reflect.DeepEqual(ready.Accounts, pending.Accounts) {
		t.Fatal("confirmed phrase changed accounts")
	}
	for _, i := range []uint32{1, 127} {
		ready = decodeSnapshot(t, electronCall(t, first, "a-index-"+string(rune(i+65)), "addAccount", map[string]uint32{"index": i}, nil))
	}
	if len(ready.Accounts) != 27 {
		t.Fatal("all network accounts not derived")
	}
	second := electronSigner(t)
	restored := decodeSnapshot(t, electronCall(t, second, "import", "import", map[string]int{"words": 12}, func(p uiprompt.Prompt) uiprompt.Reply {
		r := uiprompt.Reply{Accepted: true}
		switch p.Kind {
		case "input":
			if len(p.Labels) != 4 {
				t.Fatal("import inputs missing")
			}
			r.Values = []string{publicNewPassword, publicNewPassword, string(phrase), ""}
		case "confirm":
			if p.Title != "Imported account recovery limits" {
				t.Fatal("import limits missing")
			}
		default:
			t.Fatal("unexpected fresh import prompt")
		}
		return r
	}))
	for _, i := range []uint32{1, 127} {
		restored = decodeSnapshot(t, electronCall(t, second, "b-index-"+string(rune(i+65)), "addAccount", map[string]uint32{"index": i}, nil))
	}
	if !restored.RecoveryConfirmed || !reflect.DeepEqual(ready.Accounts, restored.Accounts) {
		t.Fatal("fresh profile and different local password did not restore all 27 public descriptors")
	}
	var eth model.Account
	for _, a := range restored.Accounts {
		if a.Network == "ethereum-mainnet" && a.Index == 0 {
			eth = a
		}
	}
	if eth.ID == "" {
		t.Fatal("Ethereum mainnet missing")
	}
	received := electronCall(t, second, "receive", "receive", map[string]string{"accountId": eth.ID}, func(p uiprompt.Prompt) uiprompt.Reply {
		if p.Kind != "receive" || p.Address != eth.Address || len(p.PNG) == 0 {
			t.Fatal("receive did not use signer-derived address and QR")
		}
		return uiprompt.Reply{Accepted: true}
	})
	if received.Error != "" {
		t.Fatal("restored receive unavailable")
	}
	if bytes.Contains(received.Body, phrase) || bytes.Contains(received.Body, []byte(publicNewPassword)) {
		t.Fatal("ordinary result exposed prompt secret")
	}
}

func TestBackupPhraseProtocolAuthenticatesEveryViewAndKeepsPublicRepliesSecretFree(t *testing.T) {
	c := electronSigner(t)
	initial := decodeSnapshot(t, electronCall(t, c, "import-backup", "import", map[string]int{"words": 12}, func(p uiprompt.Prompt) uiprompt.Reply {
		r := uiprompt.Reply{Accepted: true}
		if p.Kind == "input" {
			r.Values = []string{publicPassword, publicPassword, publicPhrase, ""}
		} else if p.Kind != "confirm" {
			t.Fatal("unexpected fixture import prompt")
		}
		return r
	}))
	expected, _ := json.Marshal(initial.Accounts)
	for i, mode := range []string{"cancel-confirm", "wrong-password", "cancel-password", "accepted", "cancel-display", "accepted"} {
		stages := []string{}
		result := electronCall(t, c, "backup-"+string(rune('a'+i)), "backupPhrase", struct{}{}, func(p uiprompt.Prompt) uiprompt.Reply {
			stages = append(stages, p.Kind)
			r := uiprompt.Reply{Accepted: true}
			switch p.Kind {
			case "confirm":
				if p.Title != "Back up recovery phrase" || strings.Contains(p.Summary, publicPhrase) {
					t.Fatal("backup confirmation contained unexpected secret or title")
				}
				r.Accepted = mode != "cancel-confirm"
			case "input":
				if len(p.Labels) != 1 || p.Labels[0] != "App password" {
					t.Fatal("backup did not request app-password reauthentication")
				}
				r.Values = []string{publicPassword}
				if mode == "wrong-password" {
					r.Values[0] = "PUBLIC-incorrect-password"
				}
				if mode == "cancel-password" {
					r.Accepted, r.Values = false, nil
				}
			case "recoveryBackup":
				if mode == "wrong-password" || mode == "cancel-password" || mode == "cancel-confirm" || p.Phrase != publicPhrase || p.Title != "Back up recovery phrase" {
					t.Fatal("backup disclosed an unauthorized or changed recovery phrase")
				}
				r.Accepted = mode != "cancel-display"
			default:
				t.Fatal("backup issued setup proof or unrelated prompt")
			}
			return r
		})
		for _, secret := range []string{publicPhrase, publicPassword, "PUBLIC-incorrect-password", `"entropy"`, `"passphrase"`, `"seed"`} {
			if bytes.Contains(result.Body, []byte(secret)) || strings.Contains(result.Error, secret) {
				t.Fatal("backup secret escaped its dedicated prompt channel")
			}
		}
		wantStages := "confirm,input,recoveryBackup"
		if mode == "wrong-password" || mode == "cancel-password" {
			wantStages = "confirm,input"
			if result.Error == "" {
				t.Fatal("backup accepted cancelled or wrong password")
			}
		} else {
			if mode == "cancel-confirm" {
				wantStages = "confirm"
			}
			s := decodeSnapshot(t, result)
			actual, _ := json.Marshal(s.Accounts)
			if !s.RecoveryConfirmed || s.Locked || !bytes.Equal(expected, actual) {
				t.Fatal("backup changed wallet recovery authority or accounts")
			}
		}
		if strings.Join(stages, ",") != wantStages {
			t.Fatal("backup reused prior authorization or displayed words after rejection")
		}
	}
}

func TestSignerRejectsWrongPromptCorrelationAndObsoleteGoWindowAPI(t *testing.T) {
	for _, mode := range []string{"request", "session", "method", "prompt", "unknown", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			c := electronSigner(t)
			m := request("create", map[string]int{"words": 12})
			if e := c.Write(m); e != nil {
				t.Fatal(e)
			}
			p, e := c.Read()
			if e != nil || p.Method != "prompt" {
				t.Fatal("input prompt missing")
			}
			var prompt uiprompt.Prompt
			if json.Unmarshal(p.Body, &prompt) != nil {
				t.Fatal("invalid prompt")
			}
			body, _ := json.Marshal(uiprompt.Reply{PromptID: prompt.ID, Accepted: true, Values: []string{publicPassword}})
			r := ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: "promptReply", Body: body}
			switch mode {
			case "request":
				r.ID = "foreign"
			case "session":
				r.Session = "foreign-public-session"
			case "method":
				r.Method = "unlock"
			case "prompt":
				r.Body = []byte(`{"promptId":"ffffffffffffffffffffffffffffffff","accepted":false}`)
			case "unknown":
				r.Body = []byte(`{"promptId":"` + prompt.ID + `","accepted":false,"extra":"PUBLIC-secret-canary"}`)
			case "duplicate":
				r.Body = []byte(`{"promptId":"` + prompt.ID + `","accepted":false,"accepted":true}`)
			}
			if e = c.Write(r); e != nil {
				t.Fatal("malformed control frame failed to reach peer")
			}
			final, e := c.Read()
			if e != nil || final.Error != "REQUEST_DENIED" || string(final.Body) != "null" {
				t.Fatal("malformed prompt reply not denied")
			}
			if _, e = c.Read(); e != io.EOF {
				t.Fatal("malformed prompt peer not closed")
			}
		})
	}
	c := electronSigner(t)
	m := electronCall(t, c, "obsolete", "backup", struct{}{}, nil)
	if m.Error != "REQUEST_DENIED" {
		t.Fatal("obsolete backup API accepted")
	}
}
