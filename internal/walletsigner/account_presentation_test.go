package walletsigner

import (
	"bytes"
	"encoding/json"
	"localwallet/internal/ipc"
	"localwallet/internal/signer"
	"testing"
)

func TestTypedLocalPresentationRoutesRetainIdentityAndValidateBodies(t *testing.T) {
	e, u := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, u)
	a := accountFor(t, e, "evm")
	inputs := u.inputCalls
	dispatch(t, e, "renameWallet", map[string]string{"accountId": a.ID, "name": "Savings"})
	dispatch(t, e, "renameGroup", map[string]any{"index": 0, "name": "Long term"})
	dispatch(t, e, "deleteWallet", map[string]string{"accountId": a.ID})
	dispatch(t, e, "deleteGroup", map[string]uint32{"index": 0})
	raw := dispatch(t, e, "snapshot", map[string]any{})
	var state signer.SnapshotPage
	if json.Unmarshal(raw, &state) != nil || state.Presentation.WalletLabels[a.ID] != "Savings" || state.Presentation.GroupLabels["0"] != "Long term" || !state.Presentation.DeletedGroups["0"] {
		t.Fatal("typed metadata response lost scope")
	}
	if u.inputCalls != inputs {
		t.Fatal("local-only display metadata unexpectedly requested password")
	}
	dispatch(t, e, "addNextAccount", map[string]any{})
	if state.NextAccountIndex == nil || *state.NextAccountIndex != 1 {
		t.Fatal("missing authoritative next index")
	}
	for method, body := range map[string]any{
		"renameWallet":   map[string]any{"accountId": a.ID, "name": nil},
		"renameGroup":    map[string]any{"name": "missing index"},
		"deleteWallet":   map[string]any{"accountId": "foreign"},
		"deleteGroup":    map[string]any{"index": nil},
		"addNextAccount": map[string]any{"index": 0},
	} {
		denied(t, e, method, body)
	}
}

func TestPresentationDeclinesUseCancellationFramesWithoutChangingStorage(t *testing.T) {
	e, ui := dispatchEngine(t, "testnet", true)
	unlockFixture(t, e, ui)
	account := accountFor(t, e, "evm")
	requests := []struct {
		method string
		body   any
	}{
		{"renameWallet", map[string]string{"accountId": account.ID, "name": "Cancelled name"}},
		{"renameGroup", map[string]any{"index": 0, "name": "Cancelled group"}},
		{"deleteWallet", map[string]string{"accountId": account.ID}},
		{"deleteGroup", map[string]uint32{"index": 0}},
		{"addNextAccount", struct{}{}},
	}
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, _ := json.Marshal(e.Snapshot())
	inputCalls := ui.inputCalls
	ui.approve = false
	frameFor := func(method string, body any) ipc.Message {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := ipc.Message{Version: 1, ID: "declined-metadata", Session: "public-fixture-session", Method: method, Body: raw}
		var wire bytes.Buffer
		if err := respond(&ipc.Channel{W: &wire}, request, func() (json.RawMessage, error) {
			return handle(e, request, "testnet")
		}); err != nil {
			t.Fatal(err)
		}
		frame, err := (&ipc.Channel{R: bytes.NewReader(wire.Bytes())}).Read()
		if err != nil {
			t.Fatal(err)
		}
		return frame
	}
	for _, request := range requests {
		frame := frameFor(request.method, request.body)
		if frame.Error != "REQUEST_CANCELLED" || string(frame.Body) != "null" {
			t.Fatal("actual metadata decline lost cancellation category", request.method, frame.Error)
		}
	}
	after, err := e.Files.Read("vault.lwv")
	stateAfter, _ := json.Marshal(e.Snapshot())
	if err != nil || !bytes.Equal(before, after) || !bytes.Equal(stateBefore, stateAfter) || ui.inputCalls != inputCalls {
		t.Fatal("declined presentation request changed wallet, storage or password flow", err)
	}
	e.Lock()
	for _, request := range requests {
		frame := frameFor(request.method, request.body)
		if frame.Error != "REQUEST_DENIED" {
			t.Fatal("locked metadata request was misreported as cancellation", request.method, frame.Error)
		}
	}
}
