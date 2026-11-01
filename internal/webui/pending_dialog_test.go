package webui

import (
	"strings"
	"testing"

	"localwallet/internal/uiprompt"
)

func TestPendingDialogSnapshotsCannotRewriteChoiceAuthority(t *testing.T) {
	p := uiprompt.Prompt{ID: strings.Repeat("a", 32), Kind: "pending", Title: "Pending operations", Summary: "Original public operations", Choices: []uiprompt.PendingChoice{{ID: strings.Repeat("b", 32), Label: "UNKNOWN", Summary: "Original transaction"}}}
	if _, err := checkedPrompt(p); err != nil {
		t.Fatal(err)
	}
	state := &dialogState{prompt: p}
	view := cloneDialog(state)
	view.Choices[0].ID = strings.Repeat("c", 32)
	if state.prompt.Choices[0].ID != strings.Repeat("b", 32) {
		t.Fatal("dialog snapshot rewrote original choice")
	}
	clearPrompt(&state.prompt)
	if state.prompt.Choices != nil {
		t.Fatal("dismissed dialog retained choice list")
	}
}
