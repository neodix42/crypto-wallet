package uiprompt

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestPublicInputUsesBoundedDistinctKindAndCurrentReply(t *testing.T) {
	ui := UI{Exchange: func(p Prompt) (Reply, error) {
		if p.Kind != "publicInput" || p.Title != "Track new token" || len(p.Labels) != 1 || !ValidID(p.ID) {
			t.Fatal("public selector entered a password or unbound prompt", p.Kind)
		}
		if _, err := DecodePrompt(encoded(t, p)); err != nil {
			t.Fatal(err)
		}
		for _, reply := range []Reply{{PromptID: "stale", Accepted: true, Values: []string{"contract.near"}}, {PromptID: p.ID, Accepted: true}, {PromptID: p.ID, Accepted: true, Values: []string{"one", "two"}}, {PromptID: p.ID, Accepted: true, Values: []string{strings.Repeat("x", 4097)}}, {PromptID: p.ID, Accepted: true, Values: []string{"bad\x00value"}}, {PromptID: p.ID, Accepted: false, Values: []string{"unaccepted"}}} {
			if _, err := DecodeReply(encoded(t, reply), p); err == nil {
				t.Fatal("unbound, extra or malformed public input accepted")
			}
		}
		return Reply{PromptID: p.ID, Accepted: true, Values: []string{"contract.near"}}, nil
	}}
	values, err := ui.PublicInput("Track new token", "Token contract account on NEAR")
	if err != nil || len(values) != 1 || values[0] != "contract.near" {
		t.Fatal("exact public contract input was not returned", err)
	}
	ui.Exchange = func(p Prompt) (Reply, error) { return Reply{PromptID: p.ID}, nil }
	if _, err := ui.PublicInput("Track new token", "Contract"); !errors.Is(err, ErrCancelled) {
		t.Fatal("public dialog cancellation was not preserved", err)
	}
}

func TestPublicInputCannotCarrySecretOrOtherPromptFields(t *testing.T) {
	p := Prompt{ID: fixtureID, Kind: "publicInput", Title: "Track new token", Labels: []string{"Contract"}}
	for _, field := range []string{"phrase", "address", "summary", "save", "choices", "password", "masked"} {
		var fields map[string]any
		_ = json.Unmarshal(encoded(t, p), &fields)
		fields[field] = "extra"
		if _, err := DecodePrompt(encoded(t, fields)); err == nil {
			t.Fatal("public input accepted cross-kind data", field)
		}
	}
	for _, labels := range [][]string{nil, {}, {""}, {"bad\x00label"}, {strings.Repeat("x", 257)}, make([]string, 9)} {
		p.Labels = labels
		if _, err := DecodePrompt(encoded(t, p)); err == nil {
			t.Fatal("unbounded public input labels accepted")
		}
	}
}
