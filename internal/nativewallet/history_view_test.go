package nativewallet

// These are explicit controller peers, not blockchain acceptance fixtures.
import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func historyOriginal(account model.Account, asset, state string) journal.Operation {
	return journal.Operation{Plan: model.Plan{ID: "original-" + state, Account: account, Asset: asset, Amount: "1000000000000000000", Recipient: "0x" + strings.Repeat("1", 40), Expires: time.Now().Add(time.Minute).Unix()}, State: state}
}

func TestWalletHistoryExposesOnlyOriginalOperationControlsForItsExactState(t *testing.T) {
	for _, test := range []struct {
		state string
		kinds []string
	}{
		{"PREPARED", []string{"sendOperation", "cancelPrepared"}},
		{"SIGNED_NOT_SUBMITTED", []string{"retry", "reconcile"}},
		{"UNKNOWN", []string{"retry", "reconcile"}},
		{"SUBMITTING", []string{"reconcile"}},
		{"SUBMITTED", []string{"reconcile"}},
		{"REORGED", []string{"reconcile"}},
		{"AWAITING_CONFIRMATION", nil}, {"CONFIRMED", nil}, {"FAILED", nil}, {"CANCELLED", nil}, {"REPLACED", nil}, {"OBSERVED", nil},
	} {
		t.Run(test.state, func(t *testing.T) {
			a, _, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			op := historyOriginal(account, a.network(account.Network).Assets[0].ID, test.state)
			a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Local: journalPage{Operations: []journal.Operation{op}}}}
			calls := 0
			core.handler = func(uint64, string, any, any) error { calls++; return errors.New("render must not call a payment API") }
			nodes := a.walletHistoryNodes(account)
			for _, kind := range []string{"sendOperation", "cancelPrepared", "retry", "reconcile"} {
				button, found := findNode(nodes, kind+"/"+op.Plan.ID)
				want := false
				for _, allowed := range test.kinds {
					want = want || allowed == kind
				}
				if found != want || found && (button.Kind != "button" || button.Disabled) {
					t.Fatal("wrong original-operation action set", kind, button, want)
				}
			}
			if calls != 0 || op.State != test.state || op.Signed != nil {
				t.Fatal("render changed authority or started payment work")
			}
		})
	}
}

func TestWalletHistoryActionsBindOriginalLocalOperationAndIgnoreObservedIdentities(t *testing.T) {
	for _, test := range []struct{ state, kind, method string }{
		{"PREPARED", "sendOperation", "send"}, {"PREPARED", "cancelPrepared", "cancelPrepared"},
		{"SIGNED_NOT_SUBMITTED", "retry", "retry"}, {"UNKNOWN", "retry", "retry"}, {"UNKNOWN", "reconcile", "reconcile"},
		{"SUBMITTED", "reconcile", "reconcile"}, {"SUBMITTING", "reconcile", "reconcile"}, {"REORGED", "reconcile", "reconcile"},
	} {
		t.Run(test.state+"/"+test.kind, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			op := historyOriginal(account, a.network(account.Network).Assets[0].ID, test.state)
			a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Local: journalPage{Operations: []journal.Operation{op}}, Chain: model.HistoryPage{Entries: []model.Receipt{{Identity: "observed-only", State: "UNKNOWN"}}}}}
			calls := 0
			core.handler = func(_ uint64, method string, body, out any) error {
				calls++
				request, ok := body.(map[string]string)
				if method != test.method || !ok || len(request) != 1 || request["id"] != op.Plan.ID {
					t.Error("operation replaced or reconstructed by the UI", method, body)
				}
				return answer(out, map[string]string{"state": test.state})
			}
			if a.historyPaymentAction(test.kind, "observed-only") || calls != 0 {
				t.Fatal("chain observation granted payment authority")
			}
			if !a.historyPaymentAction(test.kind, op.Plan.ID) {
				t.Fatal("original operation control refused")
			}
			ui.apply(t)
			if calls != 1 || a.resultOwner != account.ID || a.walletHistory[account.ID].Request != "localRefresh" || op.State != test.state {
				t.Fatal("action changed identity, repeated work or missed authoritative refresh", calls)
			}
		})
	}
}

func TestWalletHistoryRejectsForeignUnavailableStaleAndUnsupportedActions(t *testing.T) {
	for _, mode := range []string{"locked", "closed", "busy", "pendingRecovery", "inactive", "otherWallet", "notLoaded", "localFailed", "loading", "refresh", "foreignAddress", "foreignKey", "foreignProfile", "foreignNetwork", "foreignParameters", "unknownAsset", "signedArtifact", "duplicate", "terminal", "wrongAction", "missing"} {
		t.Run(mode, func(t *testing.T) {
			a, _, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			op := historyOriginal(account, a.network(account.Network).Assets[0].ID, "PREPARED")
			state := walletHistoryState{Loaded: true, Local: journalPage{Operations: []journal.Operation{op}}}
			kind, id := "sendOperation", op.Plan.ID
			switch mode {
			case "locked":
				a.state.Locked = true
			case "closed":
				a.closed = true
			case "busy":
				a.busy = true
			case "pendingRecovery":
				a.state.RecoveryConfirmed = false
			case "inactive":
				a.active = "settings"
			case "otherWallet":
				for _, other := range a.state.Accounts {
					if other.ID != account.ID {
						a.active = "wallet/" + other.ID
						break
					}
				}
			case "notLoaded":
				state.Loaded = false
			case "localFailed":
				state.LocalFailed = true
			case "loading":
				state.Loading = true
			case "refresh":
				state.Request = "localRefresh"
			case "foreignAddress":
				state.Local.Operations[0].Plan.Account.Address += "different"
			case "foreignKey":
				state.Local.Operations[0].Plan.Account.PublicKey = "different"
			case "foreignProfile":
				state.Local.Operations[0].Plan.Account.Profile = "different"
			case "foreignNetwork":
				state.Local.Operations[0].Plan.Account.Network = "base-mainnet"
			case "foreignParameters":
				state.Local.Operations[0].Plan.Account.Parameters = []byte(`{"different":true}`)
			case "unknownAsset":
				state.Local.Operations[0].Plan.Asset += ":untracked"
			case "signedArtifact":
				state.Local.Operations[0].Signed = &model.Signed{Bytes: []byte("never accepted in public history")}
			case "duplicate":
				state.Local.Operations = append(state.Local.Operations, op)
			case "terminal":
				state.Local.Operations[0].State = "CONFIRMED"
			case "wrongAction":
				kind = "retry"
			case "missing":
				id = "not-an-original-operation"
			}
			a.walletHistory = map[string]walletHistoryState{account.ID: state}
			calls := 0
			core.handler = func(uint64, string, any, any) error { calls++; return nil }
			if a.historyPaymentAction(kind, id) || calls != 0 {
				t.Fatal("unavailable or foreign action reached backend", mode)
			}
		})
	}
}

func TestWalletHistoryObservedAndRetiredRowsNeverOfferPaymentControls(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	op := historyOriginal(account, a.network(account.Network).Assets[0].ID, "UNKNOWN")
	state := walletHistoryState{Loaded: true, Chain: model.HistoryPage{Entries: []model.Receipt{{Identity: "arbitrary-observed", State: "UNKNOWN"}}}}
	if rows := a.walletHistoryRows(account, state); len(rows) != 1 || rows[0].operation != nil {
		t.Fatal("observed-only transaction acquired original authority", rows)
	}
	for _, mutate := range []func(*journal.Operation){
		func(o *journal.Operation) { o.Plan.Account.Address += "foreign" },
		func(o *journal.Operation) { o.Plan.Asset = "untracked" },
		func(o *journal.Operation) { o.Plan.Account.Network, o.Plan.Account.Family = "decred-mainnet", "decred" },
	} {
		invalid := op
		mutate(&invalid)
		state.Local.Operations = []journal.Operation{invalid}
		a.walletHistory = map[string]walletHistoryState{account.ID: state}
		for _, kind := range []string{"sendOperation", "cancelPrepared", "retry", "reconcile"} {
			if _, found := findNode(a.walletHistoryNodes(account), kind+"/"+op.Plan.ID); found {
				t.Fatal("unsupported local row exposed a payment control", kind)
			}
		}
	}
}

func TestWalletHistoryMergedOriginalRetainsControlsWithoutMakingOtherEffectsActionable(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	assets := a.network(account.Network).Assets
	op := historyOriginal(account, assets[0].ID, "UNKNOWN")
	tx := "0x" + strings.Repeat("b", 64)
	op.Receipt = &model.Receipt{Identity: tx}
	state := walletHistoryState{Loaded: true, Local: journalPage{Operations: []journal.Operation{op}}, Chain: model.HistoryPage{Entries: []model.Receipt{{Identity: tx, State: "OBSERVED", Transfers: []model.HistoryTransfer{
		{ID: "owned-outgoing", Source: account.Address, Destination: op.Plan.Recipient, Asset: assets[0], Atomic: op.Plan.Amount},
		{ID: "other-token", Source: op.Plan.Recipient, Destination: account.Address, Asset: assets[1], Atomic: "1000000"},
	}}}}}
	rows := a.walletHistoryRows(account, state)
	if len(rows) != 2 {
		t.Fatal("local payment duplicated", rows)
	}
	withActions := 0
	for _, row := range rows {
		if row.operation != nil {
			withActions++
			if row.operation.Plan.ID != op.Plan.ID || row.state != "UNKNOWN · provider: OBSERVED" {
				t.Fatal("merged original identity changed", row)
			}
		}
	}
	if withActions != 1 {
		t.Fatal("observed token effect gained payment authority", withActions)
	}
}

func TestCancelPreparedPreviewUsesExactOwnedPlanBeforeHistoryAndAfterExpiry(t *testing.T) {
	for _, source := range []string{"send", "ton", "staking", "nearToken", "history"} {
		t.Run(source, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			op := historyOriginal(account, a.network(account.Network).Assets[0].ID, "PREPARED")
			op.Plan.Expires = time.Now().Add(-time.Hour).Unix()
			a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, LocalFailed: source != "history", Local: journalPage{Operations: []journal.Operation{op}}}}
			switch source {
			case "send":
				a.plan = &op.Plan
			case "ton":
				a.tonPending = map[string]model.Plan{account.ID: op.Plan}
			case "staking":
				a.stakingPending = map[string]model.Plan{account.ID: op.Plan}
			case "nearToken":
				a.nearTokenPending = map[string]model.Plan{account.ID: op.Plan}
			}
			calls := 0
			core.handler = func(_ uint64, method string, body, out any) error {
				calls++
				if method != "cancelPrepared" || body.(map[string]string)["id"] != op.Plan.ID {
					t.Error("cancel changed original identity", method, body)
				}
				return answer(out, map[string]string{"state": "CANCELLED"})
			}
			if !a.cancelPreparedAction(op.Plan.ID) {
				t.Fatal("expired unsigned original could not be cancelled", source)
			}
			ui.apply(t)
			if calls != 1 {
				t.Fatal("cancellation repeated", calls)
			}
		})
	}
}

func TestCancelPreparedPreviewCannotCrossWalletDescriptorOrPendingOwner(t *testing.T) {
	for _, mode := range []string{"locked", "busy", "inactive", "changedAddress", "mapOwner", "conflictingPointers", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			a, _, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			op := historyOriginal(account, a.network(account.Network).Assets[0].ID, "PREPARED")
			a.plan = &op.Plan
			switch mode {
			case "locked":
				a.state.Locked = true
			case "busy":
				a.busy = true
			case "inactive":
				a.active = "settings"
			case "changedAddress":
				a.plan.Account.Address += "foreign"
			case "mapOwner":
				a.plan = nil
				a.tonPending = map[string]model.Plan{"foreign-owner": op.Plan}
			case "conflictingPointers":
				other := op.Plan
				other.Amount = "2"
				a.tonPending = map[string]model.Plan{account.ID: other}
			case "unknown":
				a.plan = nil
			}
			calls := 0
			core.handler = func(uint64, string, any, any) error { calls++; return nil }
			if a.cancelPreparedAction(op.Plan.ID) || calls != 0 {
				t.Fatal("foreign preview cancellation reached backend", mode)
			}
		})
	}
}

func TestEveryWalletAndStakingPageHasBottomHistoryAndSettingsIsRightmost(t *testing.T) {
	a, _, _ := fixture(t)
	for _, account := range a.state.Accounts {
		a.active = "wallet/" + account.ID
		for _, section := range []string{"Wallet", "Staking"} {
			a.fields["walletSection/"+account.ID] = section
			page := a.walletPage(account)
			if _, ok := findNode(page.Nodes, "walletHistoryTable/"+account.ID); !ok {
				t.Fatal("wallet lacks transaction table", account.Network, section)
			}
			if page.Nodes[len(page.Nodes)-1].Kind != "transactionTable" {
				t.Fatal("history not at bottom", account.Network, section)
			}
		}
	}
	for _, locked := range []bool{false, true} {
		a.state.Locked = locked
		pages := a.view().Pages
		if pages[len(pages)-1].ID != "settings" || pages[len(pages)-2].ID != "addressbook" {
			t.Fatal("Settings is not rightmost", locked)
		}
	}
}

func TestWalletEmptyHistorySeparatesUnavailableFilteredAndCompletedObservations(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	for _, test := range []struct {
		name, expected string
		state          walletHistoryState
	}{
		{"unavailable", "Transaction history could not be loaded", walletHistoryState{Loaded: true, ChainFailed: true}},
		{"paused", "Transaction history loading is paused", walletHistoryState{Loaded: true, ChainPaused: true}},
		{"filtered", "More history is available", walletHistoryState{Loaded: true, Chain: model.HistoryPage{NextCursor: strings.Repeat("a", 32)}}},
		{"incomplete", "Historical activity has not been fully checked", walletHistoryState{Loaded: true}},
		{"complete", "completed provider query", walletHistoryState{Loaded: true, Chain: model.HistoryPage{Complete: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a.walletHistory = map[string]walletHistoryState{account.ID: test.state}
			text := viewText(a.walletHistoryNodes(account))
			if !strings.Contains(text, test.expected) || strings.Contains(text, "No transactions in the loaded scope") {
				t.Fatal("unavailable or filtered history presented as empty", text)
			}
			if test.name != "complete" && strings.Contains(text, "completed provider query") {
				t.Fatal("partial history became completed empty result")
			}
		})
	}
}

type historyContextBackend struct {
	*testBackend
	started chan struct{}
}

func (b *historyContextBackend) CallHistoryAt(ctx context.Context, _ uint64, _, _ any) error {
	close(b.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestWalletWorkPreemptsAutomaticHistoryWithoutEndingTheSession(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	peer := &historyContextBackend{testBackend: core, started: make(chan struct{})}
	a.core = peer
	core.handler = func(_ uint64, method string, _, out any) error {
		if method != "history" {
			return errors.New("unexpected protocol request")
		}
		return answer(out, journalPage{Complete: true, Revision: "1"})
	}
	a.queueWalletHistory(account.ID, "initial")
	a.nextWalletHistory()
	<-peer.started
	generation := core.Generation()
	a.work("Reading metadata…", func(uint64) (func(), error) { return nil, nil })
	ui.apply(t)
	ui.apply(t)
	state := a.walletHistory[account.ID]
	if a.state.Locked || a.busy || a.background || core.Generation() != generation || !state.ChainPaused || state.ChainFailed || state.Loading || !strings.Contains(viewText(a.walletHistoryNodes(account)), "paused for your action") {
		t.Fatal("history held up work or ended the unlocked session", state)
	}
}

func TestWalletWorkBeforeTheChainReadPreventsStartingAnUnneededScan(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	started, release := make(chan struct{}), make(chan struct{})
	scans := 0
	core.handler = func(_ uint64, method string, _, out any) error {
		if method == "history" {
			close(started)
			<-release
			return answer(out, journalPage{Complete: true, Revision: "1"})
		}
		scans++
		return errors.New("history started after preemption")
	}
	a.queueWalletHistory(account.ID, "initial")
	a.nextWalletHistory()
	<-started
	a.work("Reading metadata…", func(uint64) (func(), error) { return nil, nil })
	close(release)
	ui.apply(t)
	ui.apply(t)
	if scans != 0 || !a.walletHistory[account.ID].ChainPaused || a.state.Locked {
		t.Fatal("cancel-before-start race launched a chain scan", scans)
	}
}

func TestWalletHistoryTONPeerFormatsRepresentTheSamePayment(t *testing.T) {
	raw := "0:" + strings.Repeat("1", 64)
	peer := address.MustParseRawAddr(raw)
	if !historyPeerEqual("ton", peer.String(), raw) || !historyPeerEqual("ton", raw, peer.Bounce(false).String()) {
		t.Fatal("friendly and raw TON addresses duplicated a payment")
	}
	for _, other := range []string{"0:" + strings.Repeat("2", 64), "-1:" + strings.Repeat("1", 64), "invalid"} {
		if historyPeerEqual("ton", peer.String(), other) {
			t.Fatal("different or invalid TON peer merged", other)
		}
	}
}

func TestWalletJournalPaginationRejectsTruncatedOrChangingTotals(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	operation := func(id string) journal.Operation {
		return journal.Operation{Plan: model.Plan{ID: id, Account: account}}
	}
	previous := journalPage{Revision: "1", Total: 3, Operations: []journal.Operation{operation("first")}, NextCursor: "cursor"}
	for _, page := range []journalPage{
		{Revision: "1", Total: 3, Complete: true},
		{Revision: "1", Total: 2, Complete: true, Operations: []journal.Operation{operation("second")}},
		{Revision: "1", Total: 3, Complete: false, NextCursor: "next", Operations: []journal.Operation{operation("second"), operation("third")}},
	} {
		if validateWalletJournal(account, previous, page, true) == nil {
			t.Fatal("truncated or changing journal total accepted", page)
		}
	}
	last := journalPage{Revision: "1", Total: 3, Complete: true, Operations: []journal.Operation{operation("second"), operation("third")}}
	if err := validateWalletJournal(account, previous, last, true); err != nil {
		t.Fatal("complete exact journal rejected", err)
	}
}

func TestWalletJournalRefreshSurvivesAnInflightReadAndDoesNotRescanTheChain(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	started, release := make(chan struct{}), make(chan struct{})
	reads, scans := 0, 0
	core.handler = func(_ uint64, method string, _, out any) error {
		if method == "history" {
			reads++
			if reads == 1 {
				close(started)
				<-release
			}
			page := journalPage{Complete: true, Revision: "1"}
			if reads > 1 {
				page.Revision, page.Total = "2", 1
				page.Operations = []journal.Operation{{Plan: model.Plan{ID: "new-payment", Account: account}, State: "UNKNOWN"}}
			}
			return answer(out, page)
		}
		if method == "chainHistory" {
			scans++
			return answer(out, model.HistoryPage{Scope: "bounded chain"})
		}
		return errors.New("unexpected operation")
	}
	a.queueWalletHistory(account.ID, "initial")
	a.nextWalletHistory()
	<-started
	a.refreshWalletLocalHistory(account.ID)
	close(release)
	ui.apply(t)
	if a.walletHistory[account.ID].Request != "localRefresh" || !a.nextWalletHistory() {
		t.Fatal("payment refresh lost behind in-flight observation")
	}
	ui.apply(t)
	state := a.walletHistory[account.ID]
	if reads != 2 || scans != 1 || state.LocalFailed || len(state.Local.Operations) != 1 || state.Local.Operations[0].State != "UNKNOWN" {
		t.Fatal("local payment state stale or chain rescanned", reads, scans, state)
	}
}

func TestPaymentAcknowledgementFailureStillRefreshesWalletJournal(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true}}
	core.handler = func(uint64, string, any, any) error { return errors.New("lost acknowledgement") }
	a.paymentAction("send", "original-operation")
	ui.apply(t)
	if a.walletHistory[account.ID].Request != "localRefresh" {
		t.Fatal("potential UNKNOWN payment was absent from history refresh")
	}
}

func TestWalletHistoryMergesNativeTokensWithoutInventingFinalityOrDuplicatePayments(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	assets := a.network(account.Network).Assets
	tx := "0x" + strings.Repeat("a", 64)
	dest := "0x" + strings.Repeat("1", 40)
	entry := model.Receipt{Identity: tx, Timestamp: 1720000000, State: "OBSERVED", Explorer: "https://evil.invalid/collect", Transfers: []model.HistoryTransfer{
		{ID: "native", Source: account.Address, Destination: dest, Asset: assets[0], Atomic: "1000000000000000000"},
		{ID: "log/7", Source: dest, Destination: account.Address, Asset: assets[1], Atomic: "1234567", Comment: "<script>plain comment</script>"},
	}}
	local := journal.Operation{Plan: model.Plan{ID: "original-operation", Account: account, Asset: assets[0].ID, Recipient: dest, Amount: "1000000000000000000", Memo: "local comment"}, State: "UNKNOWN", CreatedAt: 1719990000, Receipt: &model.Receipt{Identity: tx}}
	legacy := journal.Operation{Plan: model.Plan{ID: "legacy-pending", Account: account, Asset: assets[1].ID, Recipient: dest, Amount: "2000000"}, State: "PREPARED"}
	state := walletHistoryState{Loaded: true, Chain: model.HistoryPage{Entries: []model.Receipt{entry}, Scope: "bounded fixture"}, Local: journalPage{Operations: []journal.Operation{local, legacy}}}
	rows := a.walletHistoryRows(account, state)
	if len(rows) != 3 {
		t.Fatal("native/token effects or local dedup lost", rows)
	}
	var native, token walletHistoryRow
	for _, entry := range rows {
		if entry.amount == "1 ETH" {
			native = entry
		}
		if entry.amount == "1.234567 USDT" {
			token = entry
		}
	}
	if native.state != "UNKNOWN · provider: OBSERVED" || native.comment != "local comment" || native.explorer != chainutil.TransactionExplorer(account.Network, tx) {
		t.Fatal("pending identity or native units changed", native)
	}
	if token.comment != "<script>plain comment</script>" || token.source != dest || token.destination != account.Address {
		t.Fatal("token precision/peers/comment changed", token)
	}
	if rows[2].timestamp != 0 || rows[2].transaction != "" || rows[2].explorer != "" || rows[2].state != "PREPARED" {
		t.Fatal("unsigned legacy operation invented a date or transaction", rows[2])
	}
	a.walletHistory = map[string]walletHistoryState{account.ID: state}
	table, ok := findNode(a.walletHistoryNodes(account), "walletHistoryTable/"+account.ID)
	if !ok || len(table.Children) != 3 || len(table.Children[0].Children) != 7 || table.Children[2].Children[0].Text != "—" || table.Children[0].Children[4].Value != rows[0].explorer {
		t.Fatal("history table schema changed", table)
	}
	if local.State != "UNKNOWN" || local.Plan.ID != "original-operation" {
		t.Fatal("read-only history changed durable payment")
	}
}

func TestAutomaticWalletHistoryIsBackgroundAccountWideAndRetainsItsOwnPages(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	tx := "0x" + strings.Repeat("a", 64)
	started, release := make(chan struct{}), make(chan struct{})
	core.handler = func(_ uint64, method string, body, out any) error {
		request := body.(map[string]string)
		if request["accountId"] != account.ID || request["asset"] != "" {
			t.Error("history not wallet-wide", body)
			return errors.New("wrong scope")
		}
		switch method {
		case "history":
			close(started)
			<-release
			return answer(out, journalPage{Complete: true, Revision: "1", Operations: []journal.Operation{}})
		case "chainHistory":
			return answer(out, model.HistoryPage{Scope: "bounded chain", Entries: []model.Receipt{{Identity: tx, State: "OBSERVED"}}, NextCursor: strings.Repeat("b", 32)})
		default:
			return errors.New("unexpected signing/mutation request")
		}
	}
	a.walletEvent("open/" + account.ID)
	if a.busy || !a.nextWalletHistory() {
		t.Fatal("history blocked wallet UX or was not queued")
	}
	<-started
	if a.busy || !a.background || strings.Contains(a.status, "confirmation") {
		t.Fatal("read-only history uses signing UX")
	}
	close(release)
	ui.apply(t)
	state := a.walletHistory[account.ID]
	if !state.Loaded || state.Loading || state.ChainFailed || state.LocalFailed || state.Chain.NextCursor == "" || len(state.Chain.Entries) != 1 {
		t.Fatal("initial history unavailable", state)
	}
	core.handler = func(_ uint64, method string, body, out any) error {
		request := body.(map[string]string)
		if method != "chainHistory" || request["accountId"] != account.ID || request["cursor"] != state.Chain.NextCursor {
			t.Error("continuation changed account/query", method, body)
		}
		return answer(out, model.HistoryPage{Scope: "bounded chain", Entries: []model.Receipt{{Identity: "0x" + strings.Repeat("c", 64), State: "OBSERVED"}}})
	}
	a.queueWalletHistory(account.ID, "chainMore")
	a.nextWalletHistory()
	ui.apply(t)
	if len(a.walletHistory[account.ID].Chain.Entries) != 2 || a.walletHistory[account.ID].Chain.NextCursor != "" {
		t.Fatal("older page replaced earlier transactions")
	}
}

func TestWalletHistoryFailuresNeverReuseConsumedCursorsOrInventEmptySuccess(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Chain: model.HistoryPage{Scope: "bounded", Entries: []model.Receipt{{Identity: "original"}}, NextCursor: strings.Repeat("a", 32)}}}
	core.handler = func(uint64, string, any, any) error { return errors.New("private provider error canary") }
	a.queueWalletHistory(account.ID, "chainMore")
	a.nextWalletHistory()
	ui.apply(t)
	state := a.walletHistory[account.ID]
	if !state.ChainFailed || len(state.Chain.Entries) != 1 || state.Chain.NextCursor != "" || strings.Contains(viewText(a.view()), "canary") {
		t.Fatal("failed continuation lost evidence/reused authority/exposed error", state)
	}
	a.queueWalletHistory(account.ID, "refresh")
	a.nextWalletHistory()
	ui.apply(t)
	state = a.walletHistory[account.ID]
	if !state.ChainFailed || !state.LocalFailed || len(state.Chain.Entries) != 0 || !strings.Contains(viewText(a.walletHistoryNodes(account)), "unavailable") {
		t.Fatal("refresh failure became empty successful history")
	}
}

func TestWalletHistoryStaleCallbacksCannotResurrectAcrossLifecycle(t *testing.T) {
	for _, mode := range []string{"lock", "snapshot", "generation", "close"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			started, release := make(chan struct{}), make(chan struct{})
			core.handler = func(_ uint64, method string, _, out any) error {
				if method == "history" {
					close(started)
					<-release
					return answer(out, journalPage{Complete: true, Revision: "1"})
				}
				return answer(out, model.HistoryPage{Scope: "bounded", Entries: []model.Receipt{{Identity: "stale-transaction"}}})
			}
			a.queueWalletHistory(account.ID, "initial")
			a.nextWalletHistory()
			<-started
			switch mode {
			case "lock":
				a.lock()
			case "snapshot":
				a.applySnapshot(a.state, a.networks)
			case "generation":
				core.generation.Add(1)
			case "close":
				a.close()
			}
			close(release)
			ui.apply(t)
			if strings.Contains(viewText(a.view()), "stale-transaction") {
				t.Fatal("stale response resurfaced across lifecycle")
			}
		})
	}
}

func TestWalletHistoryRejectsMalformedOrMixedPagesAndBoundsVisibleTransfers(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	previous := model.HistoryPage{Entries: []model.Receipt{{Identity: "original"}}}
	for _, page := range []model.HistoryPage{
		{Scope: ""}, {Scope: "bounded", Entries: []model.Receipt{{Identity: ""}}}, {Scope: "bounded", Entries: []model.Receipt{{Identity: "original"}}},
		{Scope: "bounded", Entries: []model.Receipt{{Identity: "new", Timestamp: time.Now().Add(time.Hour).Unix()}}},
		{Scope: "bounded", Entries: []model.Receipt{{Identity: "new", Transfers: []model.HistoryTransfer{{ID: "x", Atomic: "-1"}}}}},
		{Scope: "bounded", Entries: []model.Receipt{{Identity: "new", Transfers: []model.HistoryTransfer{{ID: "x", Atomic: "1"}, {ID: "x", Atomic: "1"}}}}},
	} {
		if validateWalletChain(previous, page, true) == nil {
			t.Fatal("malformed/duplicate history accepted", page)
		}
	}
	bad := journalPage{Complete: true, Revision: "1", Total: 1, Operations: []journal.Operation{{Plan: model.Plan{ID: "x", Account: model.Account{ID: "foreign", Network: account.Network}}}}}
	if validateWalletJournal(account, journalPage{}, bad, false) == nil {
		t.Fatal("foreign wallet journal accepted")
	}
	bad.Operations[0].Plan.Account = account
	bad.Operations[0].Signed = &model.Signed{Bytes: []byte("private artifact")}
	if validateWalletJournal(account, journalPage{}, bad, false) == nil {
		t.Fatal("signed artifact entered public history")
	}
	state := walletHistoryState{Loaded: true}
	for i := 0; i < 130; i++ {
		state.Chain.Entries = append(state.Chain.Entries, model.Receipt{Identity: strings.Repeat("a", 64) + string(rune(1000+i)), State: "OBSERVED"})
	}
	a.walletHistory = map[string]walletHistoryState{account.ID: state}
	table, _ := findNode(a.walletHistoryNodes(account), "walletHistoryTable/"+account.ID)
	if len(table.Children) != 128 {
		t.Fatal("table did not bound visible rows")
	}
	a.fields["historyRows/"+account.ID] = "128"
	table, _ = findNode(a.walletHistoryNodes(account), "walletHistoryTable/"+account.ID)
	if len(table.Children) != 2 {
		t.Fatal("later loaded transactions inaccessible")
	}
}
