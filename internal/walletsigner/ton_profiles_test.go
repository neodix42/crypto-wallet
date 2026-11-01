package walletsigner

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

func TestTONProfileRecoveryThroughRealEncryptedSignerPromptProtocol(t *testing.T) {
	c := electronSigner(t)
	initial := decodeSnapshot(t, electronCall(t, c, "ton-profile-import", "import", map[string]int{"words": 12}, func(p uiprompt.Prompt) uiprompt.Reply {
		r := uiprompt.Reply{Accepted: true}
		if p.Kind == "input" {
			r.Values = []string{publicPassword, publicPassword, publicPhrase, ""}
		} else if p.Kind != "confirm" || p.Title != "Imported account recovery limits" {
			t.Fatal("unexpected import prompt")
		}
		return r
	}))
	var original model.Account
	for _, account := range initial.Accounts {
		if account.Network == "ton-mainnet" && account.Index == 0 {
			original = account
		}
	}
	if original.Profile != ton.ProfileSubwallet42 {
		t.Fatal("fresh wallet does not contain canonical subwallet42")
	}
	prompts := 0
	answer := func(p uiprompt.Prompt) uiprompt.Reply {
		prompts++
		if p.Kind != "confirm" || (p.Title != "Add exact TON recovery profile" && p.Title != "Recover existing TON profile") || !strings.Contains(p.Summary, "Network: ton-mainnet\nIndex: 0\nRecovery profile: "+ton.Profile+"\nReceiving address:") {
			t.Fatal("exact selected recovery profile was not confirmed")
		}
		return uiprompt.Reply{Accepted: true}
	}
	recovered := decodeSnapshot(t, electronCall(t, c, "ton-profile-add-zero", "addTonAccount", map[string]any{"index": 0, "profile": ton.Profile}, answer))
	if prompts != 1 || len(recovered.Accounts) != len(initial.Accounts)+1 || !reflect.DeepEqual(recovered.Accounts[:len(initial.Accounts)], initial.Accounts) {
		t.Fatal("profile recovery did not preserve all original descriptors")
	}
	legacy := recovered.Accounts[len(recovered.Accounts)-1]
	if legacy.Profile != ton.Profile || legacy.Index != 0 || legacy.PublicKey != original.PublicKey || legacy.Address == original.Address || legacy.ID == original.ID {
		t.Fatal("legacy recovery failed to derive its separate same-key identity")
	}
	decodeSnapshot(t, electronCall(t, c, "ton-profile-lock", "lock", struct{}{}, nil))
	reopened := decodeSnapshot(t, electronCall(t, c, "ton-profile-unlock", "unlock", struct{}{}, func(p uiprompt.Prompt) uiprompt.Reply {
		if p.Kind != "input" || len(p.Labels) != 1 {
			t.Fatal("unexpected unlock prompt")
		}
		return uiprompt.Reply{Accepted: true, Values: []string{publicPassword}}
	}))
	if !reflect.DeepEqual(recovered.Accounts, reopened.Accounts) {
		t.Fatal("encrypted reload changed recovered profiles")
	}
	response := electronCall(t, c, "ton-profile-duplicate-zero", "addTonAccount", map[string]any{"index": 0, "profile": ton.Profile}, answer)
	if response.Error != "" || prompts != 2 || !reflect.DeepEqual(decodeSnapshot(t, response).Accounts, recovered.Accounts) {
		t.Fatal("confirmed existing profile did not recover idempotently", response.Error)
	}
	var after struct {
		Accounts []model.Account `json:"accounts"`
	}
	status := electronCall(t, c, "ton-profile-after-duplicate", "snapshot", struct{}{}, nil)
	if status.Error != "" || json.Unmarshal(status.Body, &after) != nil || !reflect.DeepEqual(after.Accounts, recovered.Accounts) {
		t.Fatal("duplicate denial changed existing identities")
	}
	for i, body := range []string{
		`{"profile":"` + ton.Profile + `"}`,
		`{"index":null,"profile":"` + ton.Profile + `"}`,
		`{"index":"0","profile":"` + ton.Profile + `"}`,
		`{"index":0.5,"profile":"` + ton.Profile + `"}`,
		`{"index":-1,"profile":"` + ton.Profile + `"}`,
		`{"index":4294967296,"profile":"` + ton.Profile + `"}`,
		`{"index":0,"profile":null}`,
		`{"index":0,"profile":"raw-wallet-id42"}`,
		`{"index":0,"profile":"` + ton.Profile + `","walletId":42}`,
	} {
		denied := electronCall(t, c, "ton-profile-invalid-"+strconv.Itoa(i), "addTonAccount", json.RawMessage(body), func(p uiprompt.Prompt) uiprompt.Reply {
			t.Fatal("invalid recovery intent reached a prompt")
			return uiprompt.Reply{}
		})
		if denied.Error != "REQUEST_DENIED" || string(denied.Body) != "null" {
			t.Fatal("invalid recovery intent accepted", i)
		}
	}
}
