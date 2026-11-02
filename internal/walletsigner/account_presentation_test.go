package walletsigner

import (
	"encoding/json"
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
