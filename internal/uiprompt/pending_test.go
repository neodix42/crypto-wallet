package uiprompt

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPendingChoicesAreBoundedPublicAndReplyCapabilitiesMatchCurrentDialog(t *testing.T) {
	p := Prompt{ID: strings.Repeat("a", 32), Kind: "pending", Title: "Pending operations", Summary: "Saved original operations", Choices: []PendingChoice{{ID: strings.Repeat("b", 32), Label: "Ethereum · UNKNOWN", Summary: "Original amount/address\n<script>inert memo</script>"}}}
	raw, _ := json.Marshal(p)
	if _, err := DecodePrompt(raw); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"empty", "too-many", "duplicate", "invalid-id", "long-label", "long-summary", "nul", "secret-field"} {
		q := p
		q.Choices = append([]PendingChoice(nil), p.Choices...)
		switch mode {
		case "empty":
			q.Choices = nil
		case "too-many":
			q.Choices = make([]PendingChoice, 19)
		case "duplicate":
			q.Choices = append(q.Choices, q.Choices[0])
		case "invalid-id":
			q.Choices[0].ID = "operation-id"
		case "long-label":
			q.Choices[0].Label = strings.Repeat("x", 513)
		case "long-summary":
			q.Choices[0].Summary = strings.Repeat("x", 16385)
		case "nul":
			q.Choices[0].Summary = "x\x00"
		case "secret-field":
			q.Phrase = "unexpected private field"
		}
		b, _ := json.Marshal(q)
		if _, err := DecodePrompt(b); err == nil {
			t.Fatal("invalid pending prompt accepted", mode)
		}
	}
	for _, reply := range []Reply{{PromptID: p.ID, Accepted: true, Values: []string{p.Choices[0].ID}}, {PromptID: p.ID, Accepted: false}} {
		b, _ := json.Marshal(reply)
		if _, err := DecodeReply(b, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, reply := range []Reply{{PromptID: p.ID, Accepted: true}, {PromptID: p.ID, Accepted: true, Values: []string{strings.Repeat("c", 32)}}, {PromptID: p.ID, Accepted: true, Values: []string{p.Choices[0].ID, p.Choices[0].ID}}, {PromptID: p.ID, Accepted: false, Values: []string{p.Choices[0].ID}}, {PromptID: strings.Repeat("f", 32), Accepted: true, Values: []string{p.Choices[0].ID}}} {
		b, _ := json.Marshal(reply)
		if _, err := DecodeReply(b, p); err == nil {
			t.Fatal("foreign/stale pending selection accepted")
		}
	}
}
