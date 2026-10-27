package nativewallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/chains"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/nativeui"
	"localwallet/internal/uiprompt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testFrontend struct {
	queue           chan func()
	views           []nativeui.View
	prompt          func(uiprompt.Prompt) (uiprompt.Reply, error)
	cancels, closes atomic.Int32
	controls        atomic.Int32
}

func (f *testFrontend) Render(v nativeui.View)    { f.views = append(f.views, v) }
func (f *testFrontend) Dispatch(fn func())        { f.queue <- fn }
func (f *testFrontend) DispatchControl(fn func()) { f.controls.Add(1); f.queue <- fn }
func (f *testFrontend) Prompt(p uiprompt.Prompt) (uiprompt.Reply, error) {
	if f.prompt != nil {
		return f.prompt(p)
	}
	return uiprompt.Reply{}, errors.New("unexpected private prompt")
}
func (f *testFrontend) CancelPrompt() { f.cancels.Add(1) }
func (f *testFrontend) Close()        { f.closes.Add(1) }
func (f *testFrontend) apply(t *testing.T) {
	t.Helper()
	select {
	case fn := <-f.queue:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("backend callback did not become ready")
	}
}

type testBackend struct {
	generation atomic.Uint64
	handler    func(uint64, string, any, any) error
	replies    chan json.RawMessage
	killed     atomic.Int32
}

func (b *testBackend) Generation() uint64 { return b.generation.Load() }
func (b *testBackend) Call(m string, body, out any) error {
	return b.CallAt(b.Generation(), m, body, out)
}
func (b *testBackend) CallAt(g uint64, m string, body, out any) error {
	if b.handler == nil {
		return errors.New("unexpected backend call")
	}
	return b.handler(g, m, body, out)
}
func (b *testBackend) Kill() { b.killed.Add(1); b.generation.Add(1) }
func (b *testBackend) ReplyPrompt(raw json.RawMessage) error {
	b.replies <- append([]byte(nil), raw...)
	return nil
}
func (b *testBackend) SendActivity() error { return nil }
func answer(out, value any) error {
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func fixture(t *testing.T) (*App, *testFrontend, *testBackend) {
	t.Helper()
	ui := &testFrontend{queue: make(chan func(), 32)}
	core := &testBackend{replies: make(chan json.RawMessage, 8)}
	core.generation.Store(1)
	a := &App{ui: ui, core: core, coreGeneration: 1, environment: "mainnet", fields: map[string]string{"side": "Sell", "slippage": "0.5", "settings": "Wallet"}, balances: map[string][]model.Balance{}, balanceErrors: map[string]bool{}, sendForms: map[string]bool{}, active: "group/0", lastActivity: time.Now()}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if !network.Testnet {
				a.networks = append(a.networks, network)
			}
		}
	}
	for i, network := range a.networks {
		a.state.Accounts = append(a.state.Accounts, model.Account{ID: fmt.Sprintf("public-account-%d", i), Family: network.Family, Network: network.ID, Address: fmt.Sprintf("public-owned-address-%d", i)})
	}
	a.state.TotalAccounts = len(a.state.Accounts)
	a.state.RecoveryConfirmed = true
	a.state.VaultState = "present"
	a.ensureExchangeSelection()
	return a, ui, core
}
func viewText(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func findNode(nodes []nativeui.Node, id string) (nativeui.Node, bool) {
	for _, n := range nodes {
		if n.ID == id {
			return n, true
		}
		if found, ok := findNode(n.Children, id); ok {
			return found, true
		}
	}
	return nativeui.Node{}, false
}
func ethAccount(t *testing.T, a *App) model.Account {
	t.Helper()
	for _, account := range a.state.Accounts {
		if account.Network == "ethereum-mainnet" {
			return account
		}
	}
	t.Fatal("Ethereum mainnet missing from fixture")
	return model.Account{}
}

func TestNativeControllerAllNetworksPublicBalancesAndWholeAssetUnits(t *testing.T) {
	a, _, _ := fixture(t)
	if len(a.networks) != 9 || len(a.state.Accounts) != 9 {
		t.Fatal("native controller narrowed the network registry")
	}
	assetCount := 0
	for _, n := range a.networks {
		assetCount += len(n.Assets)
	}
	if assetCount != 12 {
		t.Fatal("native controller narrowed supported assets")
	}
	account := ethAccount(t, a)
	asset := a.asset(account)
	a.balances[account.ID] = []model.Balance{{Asset: asset.ID, Atomic: "1000000000000000000", State: "provider-observed", At: "public-block-7"}}
	value, observation := a.balance(account, asset)
	if value != "1" || !strings.Contains(observation, "public-block-7") {
		t.Fatal("native whole-asset amount formatting failed")
	}
	page := a.groupPage(0)
	row, ok := findNode(page.Nodes, "open/"+account.ID)
	if !ok {
		t.Fatal("wallet row missing")
	}
	if !strings.Contains(row.Children[1].Tooltip, "public-block-7") || strings.Contains(row.Children[1].Text, "observed") {
		t.Fatal("observation was not hover-only")
	}
	a.sendForms[account.ID] = true
	text := viewText(a.walletPage(account))
	if !strings.Contains(text, "Amount (1 means 1 ETH)") || !strings.Contains(text, account.Address) {
		t.Fatal("wallet form units or persistent address missing")
	}
	if strings.Contains(text, "Scan local QR") || strings.Contains(text, "Provider observed") {
		t.Fatal("removed send controls or balance metadata appeared")
	}
}

func TestNativeControllerSecretPromptsNeverEnterPublicPresentation(t *testing.T) {
	a, ui, core := fixture(t)
	const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	const input = "public-synthetic-password-canary-9182"
	const id = "0123456789abcdef0123456789abcdef"
	seen := make(chan struct{}, 1)
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		if p.Phrase != phrase || p.ID != id {
			t.Error("private prompt changed identity or fixture")
		}
		seen <- struct{}{}
		return uiprompt.Reply{PromptID: id, Accepted: true}, nil
	}
	raw, _ := json.Marshal(uiprompt.Prompt{ID: id, Kind: "recoveryBackup", Title: "Back up recovery phrase", Phrase: phrase})
	if err := a.privatePrompt(raw); err != nil {
		t.Fatal("private prompt rejected")
	}
	clear(raw)
	ui.apply(t)
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("private prompt not delivered")
	}
	select {
	case reply := <-core.replies:
		var r uiprompt.Reply
		if json.Unmarshal(reply, &r) != nil || r.PromptID != id || !r.Accepted {
			t.Fatal("private reply identity lost")
		}
		clear(reply)
	case <-time.After(time.Second):
		t.Fatal("private reply not delivered")
	}
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		return uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{input}}, nil
	}
	raw, _ = json.Marshal(uiprompt.Prompt{ID: id, Kind: "input", Title: "Synthetic password", Labels: []string{"Password"}})
	if a.privatePrompt(raw) != nil {
		t.Fatal("private input rejected")
	}
	clear(raw)
	ui.apply(t)
	select {
	case reply := <-core.replies:
		if !strings.Contains(string(reply), input) {
			t.Fatal("private input did not reach private reply")
		}
		clear(reply)
	case <-time.After(time.Second):
		t.Fatal("private reply not delivered")
	}
	a.render()
	public := viewText(a.view()) + viewText(a.fields) + viewText(ui.views)
	if strings.Contains(public, phrase) || strings.Contains(public, input) {
		t.Fatal("private values leaked into public presentation")
	}
}

func TestNativeControllerRejectsOldPromptGeneration(t *testing.T) {
	a, ui, core := fixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		close(entered)
		<-release
		return uiprompt.Reply{PromptID: p.ID, Accepted: true}, nil
	}
	raw := json.RawMessage(`{"id":"0123456789abcdef0123456789abcdef","kind":"confirm","title":"Public fixture","summary":"Public approval"}`)
	if a.privatePrompt(raw) != nil {
		t.Fatal("private confirm rejected")
	}
	core.generation.Add(1)
	close(release)
	ui.apply(t)
	select {
	case <-entered:
		t.Fatal("queued stale prompt reached UI after backend replacement")
	default:
	}
	select {
	case <-core.replies:
		t.Fatal("old prompt replied to replacement backend")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestNativeControllerAccountsPaginationRejectsDuplicatesAndRevisionChanges(t *testing.T) {
	for _, mode := range []string{"complete", "duplicate", "revision", "cursor-loop", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			_, _, core := fixture(t)
			calls := 0
			core.handler = func(g uint64, m string, body, out any) error {
				calls++
				if m != "accounts" || g != 1 {
					t.Fatal("wrong pagination route")
				}
				p := snapshot{Revision: "r1", TotalAccounts: 2, Accounts: []model.Account{{ID: "one"}}, AccountsCursor: "next"}
				if calls > 1 {
					p.Accounts = []model.Account{{ID: "two"}}
					p.AccountsCursor = ""
					switch mode {
					case "duplicate":
						p.Accounts[0].ID = "one"
					case "revision":
						p.Revision = "r2"
					case "cursor-loop":
						p.Accounts = nil
						p.AccountsCursor = "next"
					case "truncated":
						p.Accounts = nil
					}
				}
				return answer(out, p)
			}
			state, err := readAccounts(core, 1)
			if mode == "complete" {
				if err != nil || len(state.Accounts) != 2 || state.AccountsCursor != "" {
					t.Fatal("valid account pages lost")
				}
			} else if err == nil {
				t.Fatal("invalid account continuation accepted")
			}
		})
	}
}

func TestNativeControllerRejectsStaleWorkAfterLockAndBackendReplacement(t *testing.T) {
	for _, change := range []string{"lock", "generation"} {
		t.Run(change, func(t *testing.T) {
			a, ui, core := fixture(t)
			var applied bool
			ready := make(chan struct{})
			a.work("Public operation", func(uint64) (func(), error) { close(ready); return func() { applied = true }, nil })
			<-ready
			if change == "lock" {
				a.lock()
			} else {
				core.generation.Add(1)
			}
			ui.apply(t)
			if applied {
				t.Fatal("stale backend result changed public wallet state")
			}
		})
	}
}

func TestNativeControllerPreparePassesWholeUnitsAndInvalidatesEditedPlan(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	a.fields["recipient/"+account.ID] = "public-recipient"
	a.fields["amount/"+account.ID] = "1"
	a.fields["memo/"+account.ID] = "public memo"
	entered, release := make(chan struct{}), make(chan struct{})
	core.handler = func(g uint64, m string, body, out any) error {
		if m != "prepare" {
			return errors.New("wrong method")
		}
		transfer, ok := body.(model.Transfer)
		if !ok || transfer.Amount != "1" || transfer.Recipient != "public-recipient" {
			t.Error("whole-unit UI request changed before Go amount parsing")
		}
		close(entered)
		<-release
		return answer(out, model.Plan{ID: "prepared-original", Account: account, Asset: transfer.Asset, Recipient: transfer.Recipient, Amount: "1000000000000000000"})
	}
	a.walletEvent("prepare/" + account.ID)
	<-entered
	a.event(nativeui.Event{ID: "field/recipient/" + account.ID, Value: "public-edited-recipient"})
	close(release)
	ui.apply(t)
	if a.plan != nil {
		t.Fatal("stale prepare restored a plan after recipient edit")
	}
	a.plan = &model.Plan{ID: "fixture-plan", Account: account}
	a.event(nativeui.Event{ID: "field/amount/" + account.ID, Value: "2"})
	if a.plan != nil {
		t.Fatal("editing amount retained review authority")
	}
}

func TestNativeControllerJournalPaginationRemainsBoundedAndNavigable(t *testing.T) {
	a, ui, core := fixture(t)
	a.fields["settings"] = "Journal"
	a.journalRevision = "revision-one"
	for i := 0; i < 128; i++ {
		a.journal = append(a.journal, journal.Operation{Plan: model.Plan{ID: fmt.Sprintf("old-operation-%03d", i)}, State: "CONFIRMED"})
	}
	a.journalCursor = "next-page"
	core.handler = func(g uint64, m string, body, out any) error {
		if m != "history" || viewText(body) != `{"cursor":"next-page"}` {
			t.Fatal("journal continuation lost its cursor")
		}
		return answer(out, journalPage{Revision: "revision-one", Operations: []journal.Operation{{Plan: model.Plan{ID: "new-operation-128"}, State: "CONFIRMED"}}})
	}
	a.loadJournal(true)
	ui.apply(t)
	if len(a.journal) != 129 || a.journalOffset != 128 {
		t.Fatal("new journal page not retained/accessed")
	}
	text := viewText(a.settingsPage())
	if !strings.Contains(text, "new-operation-128") || strings.Contains(text, "old-operation-000") {
		t.Fatal("journal visible window not bounded")
	}
	a.event(nativeui.Event{ID: "journalPrevious"})
	text = viewText(a.settingsPage())
	if !strings.Contains(text, "old-operation-000") || strings.Contains(text, "new-operation-128") {
		t.Fatal("prior loaded journal page inaccessible")
	}
	before := len(a.journal)
	a.journalCursor = "next-page"
	core.handler = func(g uint64, m string, body, out any) error {
		return answer(out, journalPage{Revision: "changed", Operations: []journal.Operation{{Plan: model.Plan{ID: "must-not-append"}}}})
	}
	a.loadJournal(true)
	ui.apply(t)
	if len(a.journal) != before {
		t.Fatal("mixed journal revisions appended")
	}
}

func TestNativeControllerChainContinuationNeverMixesWallets(t *testing.T) {
	a, _, core := fixture(t)
	a.chainOwner = "one-wallet"
	a.chain = model.HistoryPage{Entries: []model.Receipt{{Identity: "old-wallet-receipt"}}, NextCursor: "one-wallet-cursor"}
	var called atomic.Bool
	core.handler = func(g uint64, m string, body, out any) error { called.Store(true); return nil }
	a.loadChain("another-wallet", true)
	if called.Load() || a.busy {
		t.Fatal("cross-wallet history continuation reached backend")
	}
}

func TestNativeControllerLockClearsTransientPublicStateAndCancelsSecrets(t *testing.T) {
	a, ui, core := fixture(t)
	a.chain = model.HistoryPage{NextCursor: "private-session-capability", Entries: []model.Receipt{{Identity: "public-old-receipt"}}}
	a.chainOwner = "old-owner"
	a.result = "old-result"
	a.resultOwner = "old-owner"
	a.journalCursor = "old-journal"
	a.journalRevision = "old-revision"
	a.sendForms["old-owner"] = true
	a.fields["recipient/old-owner"] = "old-recipient"
	a.plan = &model.Plan{ID: "old-plan"}
	a.exchange = json.RawMessage(`{"connected":true}`)
	a.quote = json.RawMessage(`{"lastPrice":"3"}`)
	a.lock()
	if !a.state.Locked || a.plan != nil || len(a.state.Accounts) != 0 || len(a.chain.Entries) != 0 || a.chainOwner != "" || a.resultOwner != "" || a.journalCursor != "" || a.journalRevision != "" || len(a.sendForms) != 0 || a.exchange != nil || a.quote != nil {
		t.Fatal("lock retained session state or authority")
	}
	if ui.cancels.Load() != 1 || core.killed.Load() != 1 {
		t.Fatal("lock failed to cancel secret prompt and backend")
	}
	if strings.Contains(viewText(a.view()), "old-recipient") {
		t.Fatal("locked presentation retained form content")
	}
}

func TestNativeControllerExchangeAndWalletContextsStaySeparate(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.fields["exchangeAccount"] = account.ID
	a.fields["exchangeAsset"] = "usdt"
	network := a.network(account.Network)
	for _, asset := range network.Assets {
		if asset.Symbol == "USDT" {
			a.fields["exchangeAsset"] = asset.ID
		}
	}
	text := viewText(a.exchangePage())
	if !strings.Contains(text, account.Address) || !strings.Contains(text, "Token contract (not your receive address)") {
		t.Fatal("token identity confused with owned address")
	}
	for _, key := range []string{"exchangeAsset", "side", "slippage"} {
		a.quote = json.RawMessage(`{"lastPrice":"123"}`)
		a.event(nativeui.Event{ID: "field/" + key, Value: "changed-public-value"})
		if a.quote != nil {
			t.Fatal("changed exchange terms retained stale quote")
		}
	}
	a.result = "public-exchange-result"
	a.resultOwner = "exchange"
	if strings.Contains(viewText(a.walletPage(account)), a.result) {
		t.Fatal("exchange result appeared in unrelated wallet tab")
	}
	for _, id := range []string{"exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
		n, ok := findNode(a.exchangePage(), id)
		if !ok || !n.Disabled {
			t.Fatal("disconnected exchange mutation enabled")
		}
	}
}

func TestNativeControllerFixedErrorsNeverEchoUpstreamDetails(t *testing.T) {
	const canary = "private-upstream-canary-must-not-appear"
	if strings.Contains(safeError(errors.New(canary)), canary) {
		t.Fatal("upstream error detail escaped")
	}
	if len(publicText(json.RawMessage(strings.Repeat("x", 131073)))) > 256 {
		t.Fatal("unbounded result display")
	}
}

func TestNativeControllerPriorityTickTearsDownPromptAfterCrashOrIdle(t *testing.T) {
	for _, mode := range []string{"crash", "idle"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			a.busy = true
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if mode == "crash" {
					core.generation.Add(1)
				} else {
					a.lastActivity = time.Now().Add(-6 * time.Minute)
				}
				a.tick()
				return uiprompt.Reply{}, uiprompt.ErrCancelled
			}
			raw := json.RawMessage(`{"id":"0123456789abcdef0123456789abcdef","kind":"input","title":"Public synthetic password","labels":["Password"]}`)
			if a.privatePrompt(raw) != nil {
				t.Fatal("private prompt setup failed")
			}
			ui.apply(t)
			if !a.state.Locked || ui.cancels.Load() != 1 || core.killed.Load() != 1 {
				t.Fatal("priority lifecycle tick left private prompt/session alive")
			}
			select {
			case <-core.replies:
				t.Fatal("cancelled generation received a prompt reply")
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestNativeControllerBalanceSuccessFailureRecoveryAndIncompleteObservations(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	asset := a.asset(account)
	phase := 0
	core.handler = func(g uint64, m string, body, out any) error {
		if m != "balances" {
			return errors.New("unexpected request")
		}
		if phase == 1 {
			return errors.New("public provider failure")
		}
		return answer(out, []model.Balance{{Asset: asset.ID, Atomic: "1000000000000000000", State: "provider-observed", At: "public-block-1"}})
	}
	for phase = 0; phase < 3; phase++ {
		a.balanceQueue = []string{account.ID}
		a.nextBalance()
		ui.apply(t)
		value, _ := a.balance(account, asset)
		if phase == 1 {
			if value != "Unknown" || len(a.balances[account.ID]) != 0 {
				t.Fatal("provider failure retained old known balance")
			}
		} else if value != "1" {
			t.Fatal("successful balance observation did not recover")
		}
	}
	for _, b := range []model.Balance{{Asset: asset.ID, Atomic: "0", State: "unknown", At: "block"}, {Asset: asset.ID, Atomic: "0", State: "pending", At: "block"}, {Asset: asset.ID, Atomic: "0", State: "provider-observed"}, {Asset: asset.ID, Atomic: "00", State: "provider-observed", At: "block"}, {Asset: asset.ID, Atomic: "-1", State: "provider-observed", At: "block"}} {
		a.balances[account.ID] = []model.Balance{b}
		value, _ := a.balance(account, asset)
		if value != "Unknown" {
			t.Fatal("incomplete/noncanonical provider data became a verified balance")
		}
	}
	b := model.Balance{Asset: asset.ID, Atomic: "0", State: "provider-observed", At: "block"}
	a.balances[account.ID] = []model.Balance{b, b}
	if value, _ := a.balance(account, asset); value != "Unknown" {
		t.Fatal("duplicate asset observations became a known balance")
	}
}

func TestNativeControllerInflightExchangeSelectionInvalidatesQuote(t *testing.T) {
	a, ui, core := fixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	core.handler = func(g uint64, m string, body, out any) error {
		close(entered)
		<-release
		return answer(out, json.RawMessage(`{"lastPrice":"123"}`))
	}
	a.exchangeAction("exchangeQuote", map[string]string{})
	<-entered
	a.event(nativeui.Event{ID: "field/side", Value: "Buy"})
	close(release)
	ui.apply(t)
	if a.quote != nil {
		t.Fatal("old in-flight market quote restored after direction edit")
	}
}

func TestNativeControllerBackendFailureCancelsPromptThroughPriorityQueue(t *testing.T) {
	a, ui, core := fixture(t)
	a.work("Public synthetic operation", func(uint64) (func(), error) { return nil, errors.New("private provider canary 72931") })
	ui.apply(t)
	if ui.controls.Load() != 1 || ui.cancels.Load() != 1 || a.busy || core.killed.Load() != 0 {
		t.Fatal("surviving coordinator/signing failure did not revoke modal through priority queue")
	}
	if strings.Contains(a.status, "private provider canary") {
		t.Fatal("provider error content reached status")
	}
}

func TestNativeControllerPaymentJournalActionsKeepOriginalIdentity(t *testing.T) {
	for _, method := range []string{"send", "retry", "reconcile", "cancelPrepared"} {
		t.Run(method, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "settings"
			a.journal = []journal.Operation{{Plan: model.Plan{ID: "original-payment-id", Account: account}}}
			called := false
			core.handler = func(g uint64, m string, body, out any) error {
				called = true
				if m != method || viewText(body) != `{"id":"original-payment-id"}` {
					t.Error("journal action replaced signed/prepared identity")
				}
				return answer(out, json.RawMessage(`{"identity":"original-transaction-hash","state":"UNKNOWN"}`))
			}
			a.paymentAction(method, "original-payment-id")
			ui.apply(t)
			if !called || a.resultOwner != account.ID || !strings.Contains(a.result, "UNKNOWN") {
				t.Fatal("journal result lost wallet scope or ambiguity")
			}
		})
	}
}

func TestNativeControllerPrivateAndMetadataActionsPreserveBackendContracts(t *testing.T) {
	cases := []struct {
		event        string
		setup        func(*App)
		method, body string
	}{
		{"create12", nil, "create", `{"words":12}`}, {"create24", nil, "create", `{"words":24}`}, {"restore", nil, "import", `{"words":12}`}, {"unlock", nil, "unlock", `{}`}, {"backupPhrase", nil, "backupPhrase", `{}`}, {"password", nil, "password", `{}`}, {"confirmRecovery", nil, "confirmRecovery", `{}`},
		{"addAccount", func(a *App) { a.fields["index"] = "127" }, "addAccount", `{"index":127}`}, {"discoverAccounts", func(a *App) { a.fields["from"] = "127"; a.fields["count"] = "3" }, "discoverAccounts", `{"end":129,"start":127}`},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			a, ui, core := fixture(t)
			originalPlan := &model.Plan{ID: "public-existing-reservation"}
			a.plan = originalPlan
			a.balances["public-existing-wallet"] = []model.Balance{{Asset: "public-asset", Atomic: "7", State: "provider-observed", At: "public-block"}}
			if tc.setup != nil {
				tc.setup(a)
			}
			calls := 0
			state := a.state
			networks := a.networks
			core.handler = func(g uint64, m string, body, out any) error {
				calls++
				switch m {
				case "accounts":
					return answer(out, state)
				case "networks":
					return answer(out, networks)
				default:
					if m != tc.method || viewText(body) != tc.body {
						t.Errorf("public action routed wrong request: %s", m)
					}
					return answer(out, json.RawMessage(`{"public":"result"}`))
				}
			}
			a.event(nativeui.Event{ID: tc.event})
			ui.apply(t)
			expectedCalls := 3
			if tc.event == "backupPhrase" {
				expectedCalls = 1
				if a.plan != originalPlan || len(a.balances["public-existing-wallet"]) != 1 {
					t.Fatal("read-only phrase backup discarded prepared payment or observed balances")
				}
			}
			if calls != expectedCalls || a.busy {
				t.Fatal("metadata action failed to refresh public accounts/networks")
			}
		})
	}
}

func TestNativeControllerWalletActionsNeverSendSecretsInPublicRequests(t *testing.T) {
	for _, method := range []string{"receive", "copyReceive", "saveQR", "configure", "hideAccount", "associate", "discovery"} {
		t.Run(method, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			originalPlan := &model.Plan{ID: "public-existing-reservation", Account: account}
			a.plan = originalPlan
			a.balances[account.ID] = []model.Balance{{Asset: a.asset(account).ID, Atomic: "7", State: "provider-observed", At: "public-block"}}
			a.fields["associate/"+account.ID] = "public-associated-account"
			for k, v := range map[string]string{"birthday": "1", "gap": "20", "limit": "100", "blockBudget": "1000"} {
				a.fields[k] = v
			}
			state, networks := a.state, a.networks
			called := false
			calls := 0
			core.handler = func(g uint64, m string, body, out any) error {
				calls++
				switch m {
				case "accounts":
					return answer(out, state)
				case "networks":
					return answer(out, networks)
				default:
					called = true
					if m != method {
						t.Error("wrong wallet backend route")
					}
					raw := viewText(body)
					if strings.Contains(raw, "password") || strings.Contains(raw, "phrase") || strings.Contains(raw, "secret") {
						t.Error("secret-bearing field entered public request")
					}
					if method == "configure" && !strings.Contains(raw, account.Network) {
						t.Error("RPC configuration lost selected network")
					}
					return answer(out, json.RawMessage(`{"public":"result"}`))
				}
			}
			a.walletEvent(method + "/" + account.ID)
			ui.apply(t)
			if !called {
				t.Fatal("wallet action did not use backend")
			}
			if method == "receive" || method == "copyReceive" || method == "saveQR" {
				if calls != 1 || a.plan != originalPlan || len(a.balances[account.ID]) != 1 {
					t.Fatal("read-only address view discarded reservation, balances or requested metadata refresh")
				}
			}
		})
	}
}
