package nativewallet

import (
	"errors"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func TestFailedCandidateRefreshDiscardsOldNumbersWithoutChangingTrackedBalances(t *testing.T) {
	a, ui, core := fixture(t)
	account := baseAccount(t, a)
	a.tokenDiscovery = map[string]tokenDiscoveryState{account.ID: {Result: publicDiscovery(account)}}
	a.balances[account.ID] = []model.Balance{{Asset: a.network(account.Network).Assets[0].ID, Atomic: "1234500000000000000", State: "provider-observed", At: "public-block"}}
	before := viewText(a.balances)
	core.handler = func(_ uint64, method string, body, _ any) error {
		if method != "discoverTokens" || viewText(body) != viewText(map[string]string{"accountId": account.ID}) {
			t.Error("refresh crossed its public account boundary")
		}
		return errors.New("PRIVATE-provider-error-never-display")
	}
	a.discoverBaseTokens(account.ID)
	ui.apply(t)
	text := viewText(a.discoveredTokenRows(account))
	if !strings.Contains(text, "Inventory is unknown") || strings.Contains(text, "1.250001") || strings.Contains(text, "PRIVATE-provider") || len(a.tokenDiscovery[account.ID].Result.Items) != 0 {
		t.Fatal("failed refresh retained old candidate numbers or exposed provider text")
	}
	if viewText(a.balances) != before {
		t.Fatal("candidate failure changed separately tracked balances")
	}
}

func TestCandidateSelectorsCannotAuthorizeForeignOrRetiredSnapshotEntries(t *testing.T) {
	a, _, core := fixture(t)
	account := baseAccount(t, a)
	other := account
	other.ID = "another-owned-base-account"
	a.state.Accounts = append(a.state.Accounts, other)
	a.tokenDiscovery = map[string]tokenDiscoveryState{account.ID: {Result: publicDiscovery(account)}}
	core.handler = func(_ uint64, method string, _, _ any) error {
		t.Fatalf("stale or foreign candidate reached backend: %s", method)
		return nil
	}
	for _, selector := range []string{other.ID + "/" + publicUSDCContract, account.ID + "/0x1111111111111111111111111111111111111111", "missing/" + publicUSDCContract, account.ID + "/" + publicUSDCContract + "/extra"} {
		if !a.tokenEvent("importDiscoveredToken", selector) {
			t.Fatal("invalid selector was not consumed")
		}
	}
	if a.busy || a.fields["tokenContract/"+account.ID] != "" || a.fields["tokenContract/"+other.ID] != "" {
		t.Fatal("foreign candidate changed import state")
	}
	a.applySnapshot(a.state, a.networks)
	if a.tokenDiscovery != nil {
		t.Fatal("fresh wallet snapshot retained old discovery candidates")
	}
	a.tokenEvent("importDiscoveredToken", account.ID+"/"+publicUSDCContract)
	if a.busy || a.fields["tokenContract/"+account.ID] != "" {
		t.Fatal("stale candidate acquired import authority after snapshot replacement")
	}
}

func TestDiscoveryDoesNotRequestForeignClosedBusyOrLockedWallets(t *testing.T) {
	for _, mode := range []string{"foreign", "missing", "closed", "busy", "locked"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := baseAccount(t, a)
			core.handler = func(_ uint64, method string, _, _ any) error {
				t.Errorf("ineligible discovery reached backend: %s", method)
				return nil
			}
			switch mode {
			case "foreign":
				account = ethAccount(t, a)
			case "missing":
				account.ID = "missing"
			case "closed":
				a.closed = true
			case "busy":
				a.busy = true
			case "locked":
				a.state.Locked = true
			}
			initialBusy := a.busy
			a.discoverBaseTokens(account.ID)
			if len(ui.queue) != 0 || len(ui.views) != 0 || a.busy != initialBusy || a.tokenDiscovery != nil {
				t.Fatal("ineligible discovery queued a result")
			}
		})
	}
}

func TestRepeatedOpenKeepsCachedCandidatesButCoreRestartRevokesAnInFlightRefresh(t *testing.T) {
	a, ui, core := fixture(t)
	account := baseAccount(t, a)
	a.tokenDiscovery = map[string]tokenDiscoveryState{account.ID: {Result: publicDiscovery(account)}}
	requests := 0
	core.handler = func(_ uint64, method string, _, out any) error {
		requests++
		if method != "discoverTokens" {
			t.Error("refresh attempted private/mutating operation")
		}
		return answer(out, publicDiscovery(account))
	}
	a.walletEvent("open/" + account.ID)
	if a.busy || requests != 0 || a.active != "wallet/"+account.ID {
		t.Fatal("reopening cached wallet performed another indexer request")
	}
	a.discoverBaseTokens(account.ID)
	core.generation.Add(1)
	ui.apply(t)
	if !a.state.Locked || a.tokenDiscovery != nil || len(a.state.Accounts) != 0 {
		t.Fatal("old core generation restored discovery or wallet authority")
	}
}
