package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountPresentationOwnsOnlySafeLabelsAndCanonicalGroups(t *testing.T) {
	accounts := []Account{{ID: "first", Index: 0}, {ID: "second", Index: 12}}
	good := AccountPresentation{WalletLabels: map[string]string{"first": "Savings"}, GroupLabels: map[string]string{"0": "Основной счёт"}, DeletedGroups: map[string]bool{"12": true}}
	if !good.ValidFor(accounts) {
		t.Fatal("valid local labels rejected")
	}
	for _, bad := range []AccountPresentation{
		{WalletLabels: map[string]string{"foreign": "name"}}, {WalletLabels: map[string]string{"first": " name"}}, {WalletLabels: map[string]string{"first": "a\nNetwork: false"}}, {WalletLabels: map[string]string{"first": "a\u202eb"}}, {WalletLabels: map[string]string{"first": strings.Repeat("x", 81)}},
		{GroupLabels: map[string]string{"00": "name"}}, {GroupLabels: map[string]string{"-1": "name"}}, {GroupLabels: map[string]string{"4294967296": "name"}}, {GroupLabels: map[string]string{"1": "foreign"}}, {DeletedGroups: map[string]bool{"12": false}}, {DeletedGroups: map[string]bool{"13": true}},
	} {
		if bad.ValidFor(accounts) {
			t.Fatal("unsafe presentation accepted", bad)
		}
	}
	scoped := good.ForAccounts(accounts[:1])
	if len(scoped.WalletLabels) != 1 || len(scoped.GroupLabels) != 1 || len(scoped.DeletedGroups) != 0 {
		t.Fatal("page leaked foreign metadata", scoped)
	}
	scoped.WalletLabels["first"] = "changed"
	if good.WalletLabels["first"] != "Savings" {
		t.Fatal("public metadata aliases vault maps")
	}
	raw, err := json.Marshal(struct {
		Presentation AccountPresentation `json:"presentation,omitzero"`
	}{})
	if err != nil || string(raw) != "{}" {
		t.Fatal("empty optional metadata must preserve old vault encoding", string(raw), err)
	}
}
