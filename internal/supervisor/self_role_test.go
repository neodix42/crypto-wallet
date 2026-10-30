package supervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/uiprompt"
)

// The public fixture is the test executable itself. No production dispatcher
// test hook, helper file, environment capability, or manifest is involved.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "--internal-signer" || os.Args[1] == "--internal-core") {
		data, environment := "", ""
		if os.Args[1] == "--internal-signer" {
			if len(os.Args) != 6 || os.Args[2] != "--data" || os.Args[4] != "--environment" {
				os.Exit(2)
			}
			data, environment = os.Args[3], os.Args[5]
		} else {
			if len(os.Args) != 4 || os.Args[2] != "--environment" {
				os.Exit(2)
			}
			environment = os.Args[3]
		}
		channel := ipc.Channel{R: os.Stdin, W: os.Stdout}
		promptCounter := 0
		historyRequests := map[string]bool{}
		for {
			message, err := channel.Read()
			if err != nil {
				return
			}
			if message.Method == "cancelHistory" {
				key := message.Session + "\x00" + message.ID
				if !historyRequests[key] || message.Error != "" || !bytes.Equal(message.Body, []byte(`{}`)) {
					return
				}
				delete(historyRequests, key)
				continue
			}
			if channel.Accept(message) != nil {
				return
			}
			if message.Method == "chainHistory" {
				historyRequests[message.Session+"\x00"+message.ID] = true
				var request struct {
					Block bool
					Ready string
				}
				if json.Unmarshal(message.Body, &request) != nil {
					return
				}
				if request.Ready != "" {
					if os.WriteFile(request.Ready, []byte("public history fixture"), 0600) != nil {
						return
					}
				}
				if request.Block {
					cancel, err := channel.Read()
					if err != nil || cancel.Method != "cancelHistory" || cancel.ID != message.ID || cancel.Session != message.Session || cancel.Error != "" || !bytes.Equal(cancel.Body, []byte(`{}`)) {
						return
					}
					delete(historyRequests, message.Session+"\x00"+message.ID)
					message.Body, message.Error = []byte(`null`), "REQUEST_CANCELLED"
					if channel.Write(message) != nil {
						return
					}
					continue
				}
			}
			if message.Method == "activity" {
				message.Body = []byte("null")
				if channel.Write(message) != nil {
					return
				}
				continue
			}
			if message.Method == "unlock" {
				promptCounter++
				prompt := uiprompt.Prompt{ID: fmt.Sprintf("%032x", promptCounter), Kind: "input", Title: "Public synthetic prompt", Labels: []string{"Public fixture value"}}
				raw, _ := json.Marshal(prompt)
				if channel.Write(ipc.Message{Version: 1, ID: message.ID, Session: message.Session, Method: "prompt", Body: raw}) != nil {
					return
				}
				for {
					reply, err := channel.Read()
					if err != nil {
						return
					}
					if reply.Method == "activity" {
						if channel.Accept(reply) != nil {
							return
						}
						reply.Body = []byte("null")
						if strings.Contains(string(message.Body), "invalidActivity") {
							reply.Body = []byte(`{"value":"PUBLIC-FORBIDDEN-CANARY"}`)
						}
						if channel.Write(reply) != nil {
							return
						}
						continue
					}
					if reply.ID != message.ID || reply.Session != message.Session || reply.Method != "promptReply" {
						return
					}
					if _, err := uiprompt.DecodeReply(reply.Body, prompt); err != nil {
						return
					}
					break
				}
			}
			message.Body, _ = json.Marshal(struct {
				OK          bool   `json:"ok"`
				Environment string `json:"environment"`
				Data        string `json:"data"`
				Config      string `json:"config"`
			}{true, environment, data, os.Getenv("XDG_CONFIG_HOME")})
			if channel.Write(message) != nil {
				return
			}
		}
	}
	os.Exit(m.Run())
}

func TestSelfRoleUsesCurrentExecutableWithoutExternalManifest(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", executable, "/proc/self/exe"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			child := &Child{Path: path, SelfRole: "--internal-signer", Data: t.TempDir(), Environment: "testnet"}
			t.Cleanup(child.Kill)
			var output struct {
				OK          bool   `json:"ok"`
				Environment string `json:"environment"`
				Data        string `json:"data"`
			}
			if err := child.Call("snapshot", struct{}{}, &output); err != nil || !output.OK || output.Environment != "testnet" || output.Data != child.Data {
				t.Fatal("self signer did not preserve private protocol and profile", output, err)
			}
			generation := child.Generation()
			child.Kill()
			if child.CallAt(generation, "snapshot", struct{}{}, nil) == nil {
				t.Fatal("old self-signer generation restarted after lock")
			}
			if err := child.Call("snapshot", struct{}{}, &output); err != nil {
				t.Fatal("fresh self-signer generation did not restart", err)
			}
		})
	}
}

func TestSelfRoleCannotSelectAnotherExecutableOrArgument(t *testing.T) {
	foreign := syntheticChild(t)
	for _, role := range []string{"--internal-ui", "--internal-signer --data /tmp/foreign", "signer", "--internal-signer\x00"} {
		child := &Child{SelfRole: role, Data: t.TempDir(), Environment: "testnet"}
		if command, err := child.command(); err == nil || command != nil {
			t.Fatal("unsupported self role created a command", role)
		}
	}
	for _, path := range []string{foreign.Path, filepath.Dir(foreign.Path), filepath.Join(t.TempDir(), "missing")} {
		for _, role := range []string{"--internal-signer", "--internal-core"} {
			child := &Child{Path: path, SelfRole: role, Data: t.TempDir(), Environment: "testnet"}
			if err := child.Call("snapshot", struct{}{}, nil); err == nil || child.cmd != nil {
				t.Fatal("self role bypassed executable identity for foreign path", path)
			}
		}
	}
	// Omitting the explicit internal role must still require the legacy manifest.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Child{Path: executable, Data: t.TempDir(), Environment: "testnet"}
	if command, err := legacy.command(); err == nil || command != nil {
		t.Fatal("legacy executable verification was weakened")
	}
}

func TestCoreSelfRoleOmitsPrivateDataArgumentAndRelaysPromptActivity(t *testing.T) {
	configuration := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configuration)
	child := &Child{SelfRole: "--internal-core", Data: "UNUSED-PUBLIC-DATA-CANARY", Environment: "testnet"}
	t.Cleanup(child.Kill)
	if child.SendActivity() == nil {
		t.Fatal("activity started a child without an active native prompt")
	}
	command, err := child.command()
	if err != nil || strings.Join(command.Args, " ") != "/proc/self/exe --internal-core --environment testnet" {
		t.Fatal("core role accepted signer-only data options", command, err)
	}
	var output struct {
		OK          bool   `json:"ok"`
		Environment string `json:"environment"`
		Data        string `json:"data"`
		Config      string `json:"config"`
	}
	if err := child.Call("activity", struct{}{}, nil); err != nil {
		t.Fatal("ordinary idle activity request failed", err)
	}
	child.OnPrompt = func(raw json.RawMessage) error {
		prompt, err := uiprompt.DecodePrompt(raw)
		if err != nil {
			return err
		}
		if err := child.SendActivity(); err != nil {
			return err
		}
		if err := child.SendActivity(); err != nil {
			return err
		}
		reply, _ := json.Marshal(uiprompt.Reply{PromptID: prompt.ID, Accepted: true, Values: []string{"PUBLIC-FIXTURE-VALUE"}})
		return child.ReplyPrompt(reply)
	}
	if err := child.Call("unlock", struct{}{}, &output); err != nil || !output.OK || output.Environment != "testnet" || output.Data != "" || output.Config != configuration {
		t.Fatal("core private prompt/activity exchange failed", output, err)
	}
	if child.SendActivity() == nil {
		t.Fatal("a finished prompt retained activity authority")
	}
}

func TestCoreSelfRoleRejectsMalformedActivityAcknowledgment(t *testing.T) {
	child := &Child{SelfRole: "--internal-core", Environment: "testnet"}
	t.Cleanup(child.Kill)
	child.OnPrompt = func(raw json.RawMessage) error {
		prompt, err := uiprompt.DecodePrompt(raw)
		if err != nil {
			return err
		}
		if err := child.SendActivity(); err != nil {
			return err
		}
		reply, _ := json.Marshal(uiprompt.Reply{PromptID: prompt.ID, Accepted: true, Values: []string{"PUBLIC-FIXTURE-VALUE"}})
		return child.ReplyPrompt(reply)
	}
	if err := child.Call("unlock", map[string]bool{"invalidActivity": true}, nil); err == nil || strings.Contains(err.Error(), "CANARY") || child.cmd != nil {
		t.Fatal("malformed activity result accepted, exposed, or retained its child", err)
	}
}
