package nativewallet

// These are controller contracts with a synthetic backend, not network, signing
// or transaction-acceptance evidence. Actual Go recovery and local-node sends
// are exercised by the separate portable integration drivers.
import (
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/webui"
	"reflect"
	"strings"
	"testing"
	"time"
)

func routeFixture(t *testing.T, a *App, core *testBackend, method string, expected any, result any) {
	t.Helper()
	core.handler = func(g uint64, m string, body, out any) error {
		if g != core.Generation() {
			return errors.New("stale synthetic request")
		}
		switch m {
		case "accounts":
			return answer(out, a.state)
		case "networks":
			return answer(out, a.networks)
		}
		if m != method || viewText(body) != viewText(expected) {
			return fmt.Errorf("unexpected public controller route %s", m)
		}
		return answer(out, result)
	}
}

func TestWalletCreationRestorationAndAuthenticatedSettingsRouteOnlyTypedRequests(t *testing.T) {
	for _, row := range []struct {
		event, method string
		body          any
	}{
		{"create12", "create", map[string]int{"words": 12}}, {"create24", "create", map[string]int{"words": 24}},
		{"restore", "import", map[string]int{"words": 12}}, {"unlock", "unlock", struct{}{}},
		{"confirmRecovery", "confirmRecovery", struct{}{}}, {"backupPhrase", "backupPhrase", struct{}{}}, {"password", "password", struct{}{}},
	} {
		t.Run(row.event, func(t *testing.T) {
			a, ui, core := fixture(t)
			routeFixture(t, a, core, row.method, row.body, json.RawMessage(`{"ok":true}`))
			a.event(webui.Event{ID: row.event})
			ui.apply(t)
			if a.busy || !strings.Contains(a.status, "completed") {
				t.Fatal("typed mutation did not complete")
			}
			if row.event != "backupPhrase" && len(a.balanceQueue) != 9 {
				t.Fatal("state mutation did not refresh canonical wallets")
			}
			if row.event == "backupPhrase" && len(a.balanceQueue) != 0 {
				t.Fatal("read-only phrase display restarted balance synchronization")
			}
			if strings.Contains(viewText(a.view()), "secret-canary") {
				t.Fatal("secret field reached public view")
			}
		})
	}
}

func TestAdditionalAccountIndexBoundsRejectBeforeBackend(t *testing.T) {
	for _, row := range []struct {
		event  string
		fields map[string]string
		method string
		body   any
	}{
		{"addAccount", map[string]string{"index": "4294967295"}, "addAccount", map[string]uint32{"index": 4294967295}},
		{"discoverAccounts", map[string]string{"from": "4", "count": "3"}, "discoverAccounts", map[string]uint32{"start": 4, "end": 6}},
	} {
		a, ui, core := fixture(t)
		for k, v := range row.fields {
			a.fields[k] = v
		}
		routeFixture(t, a, core, row.method, row.body, json.RawMessage(`{}`))
		a.event(webui.Event{ID: row.event})
		ui.apply(t)
		if a.busy || !strings.Contains(a.status, "completed") {
			t.Fatal("valid bounded account index request failed")
		}
	}
	for _, fields := range []map[string]string{{"index": "-1"}, {"index": "4294967296"}, {"from": "4294967295", "count": "2"}, {"from": "0", "count": "0"}, {"from": "bad", "count": "1"}} {
		a, _, _ := fixture(t)
		for k, v := range fields {
			a.fields[k] = v
		}
		event := "discoverAccounts"
		if _, ok := fields["index"]; ok {
			event = "addAccount"
		}
		a.event(webui.Event{ID: event})
		if a.busy || !strings.Contains(a.status, "valid") {
			t.Fatal("invalid account index reached backend")
		}
	}
}

func TestWalletPrivateActionsBindTheSelectedAccountAndTypedNetwork(t *testing.T) {
	for _, kind := range []string{"receive", "copyReceive", "saveQR", "configure", "hideAccount", "associate", "discovery"} {
		t.Run(kind, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.fields["associate/"+account.ID] = "public-associated-address"
			a.fields["birthday"], a.fields["gap"], a.fields["limit"], a.fields["blockBudget"] = "0", "20", "1000", "500"
			var expected any = map[string]string{"accountId": account.ID}
			switch kind {
			case "configure":
				expected = map[string]string{"network": account.Network}
			case "hideAccount":
				expected = map[string]any{"accountId": account.ID, "hidden": true}
			case "associate":
				expected = map[string]string{"accountId": account.ID, "address": "public-associated-address"}
			case "discovery":
				expected = map[string]any{"accountId": account.ID, "birthday": uint64(0), "gap": uint64(20), "limit": uint64(1000), "blockBudget": uint64(500)}
			}
			routeFixture(t, a, core, kind, expected, json.RawMessage(`{}`))
			a.walletEvent(kind + "/" + account.ID)
			ui.apply(t)
			if a.busy || !strings.Contains(a.status, "completed") {
				t.Fatal("selected wallet request contract changed")
			}
		})
	}
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.fields["birthday"] = "-1"
	a.walletEvent("discovery/" + account.ID)
	if a.busy || !strings.Contains(a.status, "valid discovery") {
		t.Fatal("invalid discovery reached backend")
	}
}

func TestTabsFormsAssetsAndPagingPreserveWalletContextWithoutImplicitSpends(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.walletEvent("open/" + account.ID)
	a.walletEvent("open/" + account.ID)
	if len(a.tabs) != 1 || a.active != "wallet/"+account.ID {
		t.Fatal("opening existing wallet duplicated/lost its tab")
	}
	a.walletEvent("sendForm/" + account.ID)
	if !a.sendForms[account.ID] {
		t.Fatal("send form did not open")
	}
	a.walletEvent("sendForm/" + account.ID)
	if a.sendForms[account.ID] {
		t.Fatal("send form did not close")
	}
	a.plan = &model.Plan{ID: "public-old-plan"}
	asset := a.network(account.Network).Assets[1]
	a.walletEvent("asset/" + account.ID + "/" + asset.ID)
	if a.plan != nil || a.asset(account).ID != asset.ID {
		t.Fatal("asset change retained old payment authority")
	}
	a.walletEvent("nextRows/rows/0")
	if a.fields["rows/0"] != "100" {
		t.Fatal("wallet page did not advance")
	}
	a.walletEvent("prevRows/rows/0")
	if a.fields["rows/0"] != "0" {
		t.Fatal("wallet page did not return")
	}
	a.event(webui.Event{ID: "closeTab", Value: "wallet/" + account.ID})
	if len(a.tabs) != 0 || a.active != "group/0" {
		t.Fatal("middle-close lost account group")
	}
	a.walletEvent("open/missing-account")
	a.walletEvent("bad-route")
	a.walletEvent("receive/missing-account")
	if a.busy {
		t.Fatal("unknown wallet action reached backend")
	}
	for i := 0; i < 64; i++ {
		a.tabs = append(a.tabs, fmt.Sprintf("public-tab-%d", i))
	}
	a.walletEvent("open/" + account.ID)
	if len(a.tabs) != 64 || !strings.Contains(a.status, "Close a wallet tab") {
		t.Fatal("tab budget was not enforced")
	}
}

func TestPrepareAndMaximumSpendUseWholeAmountsAndOriginalPlanIdentity(t *testing.T) {
	for _, kind := range []string{"prepare", "max"} {
		a, ui, core := fixture(t)
		account := ethAccount(t, a)
		asset := a.asset(account)
		a.active = "wallet/" + account.ID
		a.fields["recipient/"+account.ID] = "public-recipient"
		a.fields["amount/"+account.ID] = "1.5"
		a.fields["memo/"+account.ID] = "public memo"
		transfer := model.Transfer{AccountID: account.ID, Asset: asset.ID, Recipient: "public-recipient", Amount: "1.5", Memo: "public memo"}
		plan := model.Plan{ID: "original-public-plan", Account: account, Asset: asset.ID, Recipient: transfer.Recipient, Amount: "1500000000000000000", MaxFee: "21000000000000", FeeAsset: asset.ID, Expires: time.Now().Add(time.Minute).Unix()}
		routeFixture(t, a, core, kind, transfer, plan)
		a.walletEvent(kind + "/" + account.ID)
		ui.apply(t)
		if a.plan == nil || a.plan.ID != plan.ID {
			t.Fatal("prepare replaced the original plan")
		}
		if kind == "max" && a.fields["amount/"+account.ID] != "1.500000000000000000" {
			t.Fatal("maximum lost exact asset denomination")
		}
		a.sendForms[account.ID] = true
		text := viewText(a.walletPage(account))
		if !strings.Contains(text, "public-recipient") || !strings.Contains(text, "Maximum fee:") || !strings.Contains(text, "public memo") {
			t.Fatal("public prepared review omitted spend effects")
		}
		routeFixture(t, a, core, "send", map[string]string{"id": plan.ID}, json.RawMessage(`{"state":"UNKNOWN","identity":"public-original-transaction"}`))
		a.walletEvent("send/" + account.ID)
		ui.apply(t)
		if a.plan != nil || a.resultOwner != account.ID || !strings.Contains(a.result, "UNKNOWN") {
			t.Fatal("send changed owner/original operation state")
		}
	}
}

func TestJournalActionsReuseOriginalIdentityAndNeverReplaceAmbiguousPayment(t *testing.T) {
	for _, row := range []struct{ event, method string }{{"sendOperation", "send"}, {"retry", "retry"}, {"reconcile", "reconcile"}, {"cancelPrepared", "cancelPrepared"}} {
		a, ui, core := fixture(t)
		account := ethAccount(t, a)
		a.active = "settings"
		a.journal = []journal.Operation{{Plan: model.Plan{ID: "original-public-id", Account: account}, State: "UNKNOWN"}}
		routeFixture(t, a, core, row.method, map[string]string{"id": "original-public-id"}, json.RawMessage(`{"state":"UNKNOWN"}`))
		a.walletEvent(row.event + "/original-public-id")
		ui.apply(t)
		if a.resultOwner != account.ID || !strings.Contains(a.result, "UNKNOWN") {
			t.Fatal("journal action lost original identity/state")
		}
	}
}

func TestControllerBackgroundStatusAndBalancesRespectEpochAndFailures(t *testing.T) {
	for _, phase := range []string{"locked", "still-locked", "error", "epoch", "generation"} {
		a, ui, core := fixture(t)
		if phase == "still-locked" {
			a.state.Locked = true
		}
		core.handler = func(g uint64, m string, body, out any) error {
			if m != "status" {
				return errors.New("unexpected")
			}
			if phase == "error" {
				return errors.New("private-upstream-canary")
			}
			return answer(out, snapshot{Locked: true, VaultState: "present"})
		}
		a.pollStatus()
		if phase == "epoch" {
			a.epoch++
		}
		if phase == "generation" {
			core.generation.Add(1)
		}
		ui.apply(t)
		if phase == "locked" || phase == "generation" {
			if !a.state.Locked || core.killed.Load() != 1 {
				t.Fatal("status lifecycle failed to lock")
			}
		} else if phase == "error" && strings.Contains(a.status, "canary") {
			t.Fatal("background failure disclosed details")
		}
	}
	for _, phase := range []string{"epoch", "generation"} {
		a, ui, core := fixture(t)
		account := ethAccount(t, a)
		core.handler = func(uint64, string, any, any) error { return nil }
		a.balanceQueue = []string{account.ID}
		a.nextBalance()
		if phase == "epoch" {
			a.epoch++
		} else {
			core.generation.Add(1)
		}
		ui.apply(t)
		if len(a.balances) != 0 {
			t.Fatal("old balance response survived lifecycle change")
		}
	}
}

func TestControllerTickExpiresFeedbackLocksIdleAndQueuesReadOnlyWork(t *testing.T) {
	a, _, _ := fixture(t)
	a.closed = true
	a.tick()
	a.closed = false
	a.busy = true
	a.notice("public old notice")
	a.statusUntil = time.Now().Add(-time.Second)
	a.tick()
	if a.status != "" {
		t.Fatal("feedback expiry waited for mutation completion")
	}
	a.busy = false
	a.background = true
	a.tick()
	if !a.background {
		t.Fatal("tick disturbed active observation")
	}
	a.background = false
	a.lastActivity = time.Now().Add(-6 * time.Minute)
	a.tick()
	if !a.state.Locked {
		t.Fatal("idle timeout did not lock")
	}
	a, ui, core := fixture(t)
	core.handler = func(g uint64, m string, body, out any) error {
		if m != "balances" {
			return errors.New("unexpected")
		}
		return answer(out, []model.Balance{})
	}
	a.balanceQueue = []string{a.state.Accounts[0].ID}
	a.tick()
	ui.apply(t)
	if a.background || len(a.balanceQueue) != 0 {
		t.Fatal("balance queue did not progress")
	}
	a, ui, core = fixture(t)
	core.handler = func(g uint64, m string, body, out any) error { return answer(out, snapshot{Locked: false}) }
	a.lastStatus = time.Now().Add(-6 * time.Second)
	a.tick()
	ui.apply(t)
	if a.background {
		t.Fatal("read-only status poll remained busy")
	}
	a.close()
	a.close()
	if !a.closed || ui.closes.Load() != 1 || core.killed.Load() != 1 {
		t.Fatal("close did not cancel exactly once")
	}
}

func TestRefreshMutationsAndFailuresDoNotInventWalletState(t *testing.T) {
	for _, phase := range []string{"success", "accounts-failure", "networks-failure", "mutation-failure"} {
		a, ui, core := fixture(t)
		core.handler = func(g uint64, m string, body, out any) error {
			if phase == m+"-failure" || phase == "mutation-failure" && m == "unlock" {
				return errors.New("private-error-canary")
			}
			switch m {
			case "accounts":
				return answer(out, a.state)
			case "networks":
				return answer(out, a.networks)
			default:
				return answer(out, json.RawMessage(`{}`))
			}
		}
		if phase == "mutation-failure" {
			a.mutation("unlock", struct{}{})
		} else {
			a.refresh()
		}
		ui.apply(t)
		if a.busy {
			t.Fatal("completed/failing refresh stayed busy")
		}
		if strings.Contains(a.status, "canary") {
			t.Fatal("private failure entered public feedback")
		}
		if phase == "success" && len(a.balanceQueue) != 9 {
			t.Fatal("refresh narrowed canonical wallets")
		}
		if phase != "success" && !strings.Contains(a.status, "failed") {
			t.Fatal("failed refresh invented a successful snapshot")
		}
	}
	a, _, _ := fixture(t)
	a.applySnapshot(snapshot{Locked: true}, nil)
	if len(a.tabs) != 0 || a.plan != nil || a.active != "group/0" {
		t.Fatal("locked snapshot retained authority")
	}
}

func TestSettingsExchangeAndJournalNavigationRouteCurrentSelection(t *testing.T) {
	for _, row := range []struct {
		event, method    string
		expected, result any
	}{{"settingsExchange", "exchangeStatus", struct{}{}, json.RawMessage(`{"connected":false}`)}, {"settingsJournal", "history", map[string]string{"cursor": ""}, journalPage{Operations: []journal.Operation{}, Complete: true}}, {"exchangeConnect", "exchangeConnect", map[string]string{"endpoint": "https://api.bybit.com"}, json.RawMessage(`{"connected":true}`)}, {"exchangeDisconnect", "exchangeDisconnect", struct{}{}, json.RawMessage(`{"connected":false}`)}} {
		a, ui, core := fixture(t)
		a.fields["region"] = "https://api.bybit.com"
		routeFixture(t, a, core, row.method, row.expected, row.result)
		a.event(webui.Event{ID: row.event})
		ui.apply(t)
		if a.busy || !strings.Contains(a.status, "completed") {
			t.Fatal("settings operation contract changed")
		}
	}
	a, _, _ := fixture(t)
	a.event(webui.Event{ID: "settingsWallet"})
	if a.fields["settings"] != "Wallet" {
		t.Fatal("wallet settings navigation failed")
	}
	a.event(webui.Event{ID: "field/group", Value: "7"})
	if a.active != "group/7" {
		t.Fatal("deterministic group selector ignored")
	}
	a.event(webui.Event{ID: "field/showHidden", Value: "true"})
	a.event(webui.Event{ID: "showHidden", Value: "true"})
	a.event(webui.Event{ID: "field/tooLong", Value: strings.Repeat("x", 4097)})
	if _, ok := a.fields["tooLong"]; ok {
		t.Fatal("public field exceeded limit")
	}
}

func TestExchangeRequestContractsAndResponsesRemainSeparateFromWalletResults(t *testing.T) {
	for _, method := range []string{"exchangeStatus", "exchangeQuote", "exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
		a, ui, core := fixture(t)
		a.fields["exchangeAmount"] = "1.25"
		account := a.fields["exchangeAccount"]
		asset := a.fields["exchangeAsset"]
		var expected any = map[string]string{"accountId": account, "assetId": asset}
		switch method {
		case "exchangeStatus":
			expected = struct{}{}
		case "exchangeQuote":
			expected = map[string]string{"accountId": account, "assetId": asset, "side": "Sell"}
		case "exchangeSwap":
			expected = map[string]string{"accountId": account, "assetId": asset, "side": "Sell", "amount": "1.25", "slippage": "0.5"}
		default:
			expected = map[string]string{"accountId": account, "assetId": asset, "amount": "1.25"}
		}
		routeFixture(t, a, core, method, expected, json.RawMessage(`{"publicResult":"route-fixture"}`))
		a.event(webui.Event{ID: method})
		ui.apply(t)
		if !strings.Contains(a.status, "completed") {
			t.Fatal("exchange request body changed")
		}
		if method == "exchangeQuote" && len(a.quote) == 0 {
			t.Fatal("market response not retained")
		}
		if method == "exchangeStatus" && len(a.exchange) == 0 {
			t.Fatal("connection response not retained")
		}
		if method != "exchangeQuote" && method != "exchangeStatus" && a.resultOwner != "exchange" {
			t.Fatal("exchange mutation crossed wallet context")
		}
	}
	a, ui, core := fixture(t)
	routeFixture(t, a, core, "exchangeReconcile", map[string]string{"id": "original-exchange-id"}, json.RawMessage(`{"state":"UNKNOWN"}`))
	a.walletEvent("exchangeReconcile/original-exchange-id")
	ui.apply(t)
	if a.resultOwner != "exchange" || !strings.Contains(a.result, "UNKNOWN") {
		t.Fatal("ambiguous exchange operation was replaced")
	}
}

func TestChainHistoryPagingBindsWalletAndRetainsBoundedReceipts(t *testing.T) {
	a, ui, core := fixture(t)
	id := a.state.Accounts[0].ID
	page := model.HistoryPage{Entries: []model.Receipt{{Identity: "public-receipt-one", State: "FINAL"}}, NextCursor: "public-next", Scope: "public-provider"}
	routeFixture(t, a, core, "chainHistory", map[string]string{"accountId": id, "cursor": ""}, page)
	a.walletEvent("chainHistory/" + id)
	ui.apply(t)
	if a.chainOwner != id || len(a.chain.Entries) != 1 {
		t.Fatal("wallet history lost its owner")
	}
	page.Entries = []model.Receipt{{Identity: "public-receipt-two", State: "FINAL"}}
	page.NextCursor = ""
	routeFixture(t, a, core, "chainHistory", map[string]string{"accountId": id, "cursor": "public-next"}, page)
	a.walletEvent("chainMore/" + id)
	ui.apply(t)
	if len(a.chain.Entries) != 2 {
		t.Fatal("history continuation lost earlier receipts")
	}
	if !strings.Contains(viewText(a.walletPage(a.state.Accounts[0])), "public-receipt-two") {
		t.Fatal("selected wallet receipt hidden")
	}
	a.chain.Entries = make([]model.Receipt, 2048)
	routeFixture(t, a, core, "chainHistory", map[string]string{"accountId": id, "cursor": ""}, page)
	a.loadChain(id, true)
	ui.apply(t)
	if len(a.chain.Entries) != 2048 || !strings.Contains(a.status, "failed") {
		t.Fatal("history budget overflow was accepted")
	}
}

func TestLargeGroupsHiddenWalletsAndJournalStatesStayAccessibleAndBounded(t *testing.T) {
	a, _, _ := fixture(t)
	base := a.state.Accounts[0]
	for i := 1; i < 112; i++ {
		account := base
		account.ID = fmt.Sprintf("public-extra-%03d", i)
		a.state.Accounts = append(a.state.Accounts, account)
	}
	a.fields["rows/0"] = "100"
	text := viewText(a.groupPage(0))
	if !strings.Contains(text, "Previous wallets") || !strings.Contains(text, "public-extra-111") {
		t.Fatal("large wallet table inaccessible")
	}
	for i := uint32(1); i < 20; i++ {
		account := base
		account.Index = i
		account.ID = fmt.Sprintf("public-group-%d", i)
		a.state.Accounts = append(a.state.Accounts, account)
	}
	a.active = "group/17"
	view := a.view()
	if len(view.Pages) > 10 || !strings.Contains(viewText(view), "Account 18") {
		t.Fatal("deterministic group page window incorrect")
	}
	a.state.Accounts[0].Hidden = true
	a.fields["showHidden"] = "false"
	a.tabs = []string{base.ID}
	if strings.Contains(viewText(a.view()), "wallet/"+base.ID) {
		t.Fatal("hidden wallet tab visible")
	}
	a.fields["showHidden"] = "true"
	if !strings.Contains(viewText(a.view()), "wallet/"+base.ID) {
		t.Fatal("hidden wallet could not be recovered")
	}
	a.fields["settings"] = "Journal"
	for _, state := range []string{"PREPARED", "SIGNED_NOT_SUBMITTED", "UNKNOWN", "SUBMITTED", "SUBMITTING", "REORGED", "CONFIRMED"} {
		a.journal = append(a.journal, journal.Operation{Plan: model.Plan{ID: "public-" + state, Account: base, Amount: "1", Asset: a.network(base.Network).Assets[0].ID}, State: state, Receipt: &model.Receipt{Identity: "public-receipt", State: state}})
	}
	text = viewText(a.settingsPage())
	for _, required := range []string{"Review original payment", "Cancel prepared payment", "Broadcast original signed payment", "Reconcile original payment", "public-receipt"} {
		if !strings.Contains(text, required) {
			t.Fatal("journal recovery action missing")
		}
	}
	a.resultOwner, a.result = "public-wallet", "public-wallet-result"
	if !strings.Contains(viewText(a.settingsPage()), a.result) {
		t.Fatal("journal result omitted")
	}
}

func TestPublicErrorCodesAreFixedAndBusyClosedViewsDoNotAuthorizeWork(t *testing.T) {
	for _, code := range []string{"PASSWORD_POLICY", "PASSWORD_MISMATCH", "INVALID_MNEMONIC", "INPUT_TOO_LONG", "REQUEST_CANCELLED"} {
		message := safeError(ipc.NewRequestError(code))
		if strings.Contains(message, "Request failed") {
			t.Fatal("known actionable error hidden")
		}
	}
	if publicText(json.RawMessage(`not-json`)) != "Result unavailable." || trimDecimal("") != "0" || trimDecimal("1.2300") != "1.23" {
		t.Fatal("public display normalization failed")
	}
	a, _, _ := fixture(t)
	a.busy = true
	a.event(webui.Event{ID: "unlock"})
	a.work("unreachable", func(uint64) (func(), error) { t.Fatal("busy duplicate work accepted"); return nil, nil })
	a.closed = true
	a.event(webui.Event{ID: "refresh"})
	a.render()
	a.tick()
	if !a.closed {
		t.Fatal("closed state changed")
	}
	for _, state := range []string{"missing", "present", "checking", "unavailable"} {
		a, _, _ := fixture(t)
		a.state.Locked = true
		a.state.VaultState = state
		a.active = "settings"
		v := a.view()
		if v.Active != "settings" || len(v.Pages) != 2 {
			t.Fatal("locked UI navigation changed")
		}
	}
	a, _, _ = fixture(t)
	before := append([]model.Account(nil), a.state.Accounts...)
	a.applySnapshot(a.state, a.networks)
	if !reflect.DeepEqual(before, a.state.Accounts) {
		t.Fatal("public snapshot changed wallet identities")
	}
}

func TestRunRejectsForeignProfileAndMissingDisplayBeforeOpeningStorage(t *testing.T) {
	if Run("unrecognized-profile") == nil {
		t.Fatal("unrecognized profile entered native startup")
	}
	private := t.TempDir()
	t.Setenv("HOME", private)
	t.Setenv("XDG_CONFIG_HOME", private+"/config")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("GDK_BACKEND", "x11")
	if Run("mainnet") == nil {
		t.Fatal("missing native display did not fail safely")
	}
}

func TestValueFreeActivityAndTabNavigationDoNotCarryFormValues(t *testing.T) {
	a, ui, core := fixture(t)
	activity := make(chan string, 1)
	core.handler = func(g uint64, method string, body, out any) error {
		if method == "activity" && viewText(body) == `{}` {
			activity <- method
			return nil
		}
		return errors.New("unexpected public activity route")
	}
	a.lastActivity = time.Now().Add(-time.Minute)
	a.event(webui.Event{ID: "activity", Value: "ignored-public-canary"})
	select {
	case <-activity:
	case <-time.After(time.Second):
		t.Fatal("value-free activity did not reach current backend")
	}
	if time.Since(a.lastActivity) > time.Second {
		t.Fatal("native activity failed to extend user session")
	}
	a.event(webui.Event{ID: "tab", Value: "group/0"})
	if a.active != "group/0" {
		t.Fatal("ordinary tab did not activate")
	}
	routeFixture(t, a, core, "exchangeStatus", map[string]string{}, json.RawMessage(`{"connected":false}`))
	a.event(webui.Event{ID: "tab", Value: "exchange"})
	ui.apply(t)
	if a.active != "exchange" || len(a.exchange) == 0 {
		t.Fatal("exchange tab did not read current connection")
	}
	a.event(webui.Event{ID: "field/exchangeAccount", Value: a.state.Accounts[0].ID})
	if a.fields["exchangeAsset"] == "" {
		t.Fatal("wallet selection did not choose its registered asset")
	}
	a.event(webui.Event{ID: "lock"})
	if !a.state.Locked {
		t.Fatal("explicit lock event failed")
	}
	a, ui, core = fixture(t)
	a.event(webui.Event{ID: "close"})
	if !a.closed || ui.closes.Load() != 1 || core.killed.Load() != 1 {
		t.Fatal("explicit close event retained backend")
	}
}

func TestAccountPaginationRejectsMissingIdsDuplicatesInitialPageAndReadErrors(t *testing.T) {
	for _, mode := range []string{"first-error", "empty-id", "duplicate-initial", "next-error", "total-limit"} {
		_, _, core := fixture(t)
		count := 0
		core.handler = func(g uint64, m string, body, out any) error {
			count++
			if mode == "first-error" || mode == "next-error" && count > 1 {
				return errors.New("public read error")
			}
			value := snapshot{Revision: "public-r", TotalAccounts: 1, Accounts: []model.Account{{ID: "public-one"}}}
			switch mode {
			case "empty-id":
				value.Accounts[0].ID = ""
			case "duplicate-initial":
				value.Accounts = append(value.Accounts, value.Accounts[0])
				value.TotalAccounts = 2
			case "next-error":
				value.AccountsCursor = "public-next"
			case "total-limit":
				value.Accounts = make([]model.Account, 10001)
				for i := range value.Accounts {
					value.Accounts[i].ID = fmt.Sprintf("public-%d", i)
				}
				value.TotalAccounts = 10001
			}
			return answer(out, value)
		}
		if _, err := readAccounts(core, 1); err == nil {
			t.Fatal("invalid or unreadable account pagination accepted:", mode)
		}
	}
}

func TestJournalAndHistoryFailuresCannotReplacePreviousResults(t *testing.T) {
	for _, mode := range []string{"provider-error", "oversized-page", "revision-change"} {
		a, ui, core := fixture(t)
		a.journal = []journal.Operation{{Plan: model.Plan{ID: "original-public-payment"}}}
		a.journalRevision = "old"
		a.journalCursor = "next"
		core.handler = func(g uint64, m string, body, out any) error {
			if mode == "provider-error" {
				return errors.New("private-canary")
			}
			page := journalPage{Revision: "changed"}
			if mode == "oversized-page" {
				page.Operations = make([]journal.Operation, 129)
			}
			return answer(out, page)
		}
		a.loadJournal(true)
		ui.apply(t)
		if len(a.journal) != 1 || a.journal[0].Plan.ID != "original-public-payment" || strings.Contains(a.status, "canary") {
			t.Fatal("failed journal append changed original payment")
		}
	}
	a, ui, core := fixture(t)
	id := a.state.Accounts[0].ID
	a.chain = model.HistoryPage{Entries: []model.Receipt{{Identity: "public-old-receipt"}}}
	a.chainOwner = id
	core.handler = func(uint64, string, any, any) error { return errors.New("public history failure") }
	a.loadChain(id, false)
	ui.apply(t)
	if len(a.chain.Entries) != 1 || a.chain.Entries[0].Identity != "public-old-receipt" {
		t.Fatal("failed history refresh replaced earlier evidence")
	}
	a, ui, core = fixture(t)
	routeFixture(t, a, core, "history", map[string]string{"cursor": ""}, journalPage{Operations: []journal.Operation{}})
	a.event(webui.Event{ID: "journalRefresh"})
	ui.apply(t)
	a.event(webui.Event{ID: "journalPrevious"})
	a.event(webui.Event{ID: "journalNext"})
	if a.journalOffset != 0 {
		t.Fatal("empty journal paging exceeded bounds")
	}
}

func TestAllWalletFamiliesAndExchangeRecoveryStatesStayVisible(t *testing.T) {
	a, _, _ := fixture(t)
	for _, account := range a.state.Accounts {
		page := a.walletPage(account)
		if page.ID != "wallet/"+account.ID || !strings.Contains(viewText(page), account.Address) {
			t.Fatal("wallet family omitted its persistent receive address")
		}
		if account.Family == "near" || account.Family == "aptos" {
			if !strings.Contains(viewText(page), "Verify and associate on-chain account") {
				t.Fatal("account association control omitted")
			}
		}
		if account.Family == "decred" && !strings.Contains(viewText(page), "Configure Decred discovery") {
			t.Fatal("Decred recovery controls omitted")
		}
	}
	a.state.RecoveryConfirmed = false
	if !strings.Contains(viewText(a.groupPage(0)), "Record and confirm") || !strings.Contains(viewText(a.walletPage(a.state.Accounts[0])), "recorded-word confirmation") {
		t.Fatal("pending recovery state was hidden")
	}
	a.fields["settings"] = "Exchange"
	if !strings.Contains(viewText(a.settingsPage()), "Official API region") {
		t.Fatal("Bybit settings unavailable")
	}
	a.exchange = json.RawMessage(`{"connected":true,"operations":[{"id":"original-one","state":"UNKNOWN","kind":"withdrawal"},{"id":"original-two","state":"PARTIALLY_FILLED","kind":"spot"},{"id":"original-three","state":"PENDING","kind":"transfer"}]}`)
	a.result, a.resultOwner = "public-exchange-result", "exchange"
	text := viewText(a.exchangePage())
	for _, value := range []string{"original-one", "original-two", "original-three", a.result} {
		if !strings.Contains(text, value) {
			t.Fatal("exchange unresolved identity disappeared")
		}
	}
	a.fields["asset/"+a.state.Accounts[0].ID] = "missing-asset"
	if a.asset(a.state.Accounts[0]).ID == "missing-asset" {
		t.Fatal("unregistered asset bypassed pinned selection")
	}
	unknown := model.Account{ID: "missing", Network: "missing-network"}
	if a.asset(unknown).Symbol != "?" {
		t.Fatal("unknown network invented an asset")
	}
}

func TestReadOnlyPrivateDisplaysPreservePreparedPlanBalancesAndMetadata(t *testing.T) {
	for _, method := range []string{"receive", "copyReceive", "saveQR", "backupPhrase"} {
		t.Run(method, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			plan := &model.Plan{ID: "public-still-prepared", Account: account}
			a.plan = plan
			a.balances[account.ID] = []model.Balance{{Asset: a.asset(account).ID, Atomic: "1000000000000000000", State: "provider-observed", At: "public-block"}}
			a.balanceQueue = []string{"public-pending-balance"}
			beforeAccounts, beforeBalances := viewText(a.state), viewText(a.balances)
			var expected any = map[string]string{"accountId": account.ID}
			if method == "backupPhrase" {
				expected = struct{}{}
			}
			calls := 0
			core.handler = func(g uint64, m string, body, out any) error {
				calls++
				if m != method || viewText(body) != viewText(expected) {
					return errors.New("read-only action issued an unrelated metadata request")
				}
				return answer(out, json.RawMessage(`{"ok":true}`))
			}
			if method == "backupPhrase" {
				a.event(webui.Event{ID: method})
			} else {
				a.walletEvent(method + "/" + account.ID)
			}
			ui.apply(t)
			if a.busy || !strings.Contains(a.status, "completed") || calls != 1 || a.plan != plan || viewText(a.state) != beforeAccounts || viewText(a.balances) != beforeBalances || !reflect.DeepEqual(a.balanceQueue, []string{"public-pending-balance"}) {
				t.Fatal("read-only authenticated display changed payment/balance/account state")
			}
		})
	}
}
