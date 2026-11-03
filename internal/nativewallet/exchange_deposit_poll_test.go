package nativewallet

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

func depositPollFixture(t *testing.T) (*App, *testFrontend, *testBackend, exchange.WizardState) {
	t.Helper()
	a, ui, core, account := wizardNativeFixture(t)
	a.active = "exchange"
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: account.ID, SourceAsset: account.Network + ":native", DepositAmount: "6", DepositOperation: "recorded-original-deposit"}
	a.adoptWizardState(&w)
	a.wizard.depositState = "SUBMITTED"
	return a, ui, core, w
}

func depositPollCallback(t *testing.T, ui *testFrontend) func() {
	t.Helper()
	select {
	case callback := <-ui.queue:
		return callback
	case <-time.After(3 * time.Second):
		t.Fatal("deposit check did not finish")
		return nil
	}
}

func TestWizardDepositPollOnlyReadsOriginalAndShowsBalancesBeforeEnablingTransfer(t *testing.T) {
	a, ui, core, w := depositPollFixture(t)
	loaded := w
	loaded.Step = 2
	check := wizardDepositCredit{ChainConfirmed: true, ChainState: "CONFIRMED", DepositedAmount: "6", TransactionID: "checked-recipient-transaction", Message: "Confirmed on-chain; awaiting Bybit credit.", Balances: []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "6", WalletBalance: "6"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "2", WalletBalance: "2.1"}}}
	var methods []string
	core.handler = func(g uint64, method string, body, out any) error {
		methods = append(methods, method)
		if g != 1 {
			t.Error("deposit poll used another session")
		}
		switch method {
		case "exchangeDepositCheck":
			if !reflect.DeepEqual(body, map[string]string{"wizardId": w.ID}) {
				t.Error("deposit poll substituted its original identity", body)
			}
			return answer(out, check)
		case "exchangeWizardLoad":
			return answer(out, map[string]any{"wizard": &loaded, "wizardDepositState": "CONFIRMED"})
		default:
			t.Error("automatic check requested payment authority", method)
			return errors.New("unexpected deposit mutation")
		}
	}
	activity := a.lastActivity
	if !a.nextWizardDepositCheck() || a.busy || a.background || a.nextWizardDepositCheck() {
		t.Fatal("automatic check blocked the page or started a duplicate read")
	}
	ui.apply(t)
	if got := strings.Join(methods, ","); got != "exchangeDepositCheck,exchangeWizardLoad" {
		t.Fatal("automatic deposit check mutated or signed an operation", got)
	}
	if a.wizard.state.Step != 2 || a.wizard.depositState != "CONFIRMED" || a.wizard.depositChecking || !a.lastActivity.Equal(activity) {
		t.Fatal("confirmed original did not advance, or polling extended the unlock session")
	}
	page := a.exchangePage()
	text := viewText(page)
	for _, expected := range []string{"6 ETH", "Funding", "Unified", "2.1", check.TransactionID} {
		if !strings.Contains(text, expected) {
			t.Fatal("deposited asset/account balance missing", expected, text)
		}
	}
	if move, ok := findNode(page, "wizardMove"); !ok || !move.Disabled {
		t.Fatal("on-chain receipt alone authorized Funding transfer")
	}
	if a.nextWizardDepositCheck() {
		t.Fatal("automatic read ignored its poll interval")
	}
	statusRevision := a.statusRevision
	check.Ready, check.ReportedAmount = true, "6"
	check.Message = "Original deposit credited by Bybit."
	a.wizard.depositCheckedAt = time.Now().Add(-6 * time.Second)
	if !a.nextWizardDepositCheck() {
		t.Fatal("awaiting exchange credit stopped automatic reads")
	}
	ui.apply(t)
	if a.statusRevision != statusRevision || a.nextWizardDepositCheck() {
		t.Fatal("credit check nagged again or continued polling after verified credit")
	}
	if move, ok := findNode(a.exchangePage(), "wizardMove"); !ok || move.Disabled {
		t.Fatal("verified credit and Funding balance did not enable explicit transfer")
	}
	if a.busy || a.background || len(a.errorQueue) != 0 || ui.cancels.Load() != 0 {
		t.Fatal("read-only confirmation opened or revoked a modal")
	}
}

func TestWizardDepositManualRefreshLoadsCurrentDraftWithoutSavingStaleState(t *testing.T) {
	a, ui, core, w := depositPollFixture(t)
	current := w
	current.Step, current.TransferAmount = 2, "5"
	var methods []string
	core.handler = func(_ uint64, method string, body, out any) error {
		methods = append(methods, method)
		switch method {
		case "exchangeDepositCheck":
			return answer(out, wizardDepositCredit{ChainConfirmed: true, Ready: true, ChainState: "CONFIRMED", ReportedAmount: "6"})
		case "exchangeWizardLoad":
			return answer(out, map[string]any{"wizard": current, "wizardDepositState": "CONFIRMED"})
		default:
			t.Error("manual confirmation refresh requested a write", method)
			return errors.New("unexpected deposit mutation")
		}
	}
	a.wizardAction("wizardCheckDeposit")
	ui.apply(t)
	if strings.Join(methods, ",") != "exchangeDepositCheck,exchangeWizardLoad" || a.wizard.state.Step != 2 || a.fields["wizardTransferAmount"] != "5" {
		t.Fatal("manual refresh overwrote the signer-owned current draft", methods, a.fields["wizardTransferAmount"])
	}
}

func TestWizardDepositPollLifecycleAndUnsignedStatesDoNotStartReads(t *testing.T) {
	cases := map[string]func(*App){
		"closed":             func(a *App) { a.closed = true },
		"locked":             func(a *App) { a.state.Locked = true },
		"recovery-pending":   func(a *App) { a.state.RecoveryConfirmed = false },
		"foreground-request": func(a *App) { a.busy = true },
		"other-background":   func(a *App) { a.background = true },
		"another-page":       func(a *App) { a.active = "group/0" },
		"existing-tools":     func(a *App) { a.fields["exchangeMode"] = "existing" },
		"another-provider":   func(a *App) { a.fields["exchangeProvider"] = "binance" },
		"no-workflow":        func(a *App) { a.wizard = nil },
		"no-deposit":         func(a *App) { a.wizard.state.DepositOperation = "" },
		"later-step":         func(a *App) { a.wizard.state.Step = 3 },
		"transfer-recorded":  func(a *App) { a.wizard.state.TransferOperation = "original-transfer" },
		"read-in-flight":     func(a *App) { a.wizard.depositChecking = true },
		"recent-read":        func(a *App) { a.wizard.depositCheckedAt = time.Now() },
		"already-credited":   func(a *App) { a.wizard.state.Step = 2; a.wizard.credit = &wizardDepositCredit{Ready: true} },
	}
	for _, state := range []string{"PREPARED", "AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "CANCELLED", "MISSING", "FAILED"} {
		state := state
		cases[state] = func(a *App) { a.wizard.depositState = state }
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a, _, core, _ := depositPollFixture(t)
			core.handler = func(_ uint64, method string, body, out any) error {
				t.Error("inactive or unsigned workflow requested a provider read", method)
				return errors.New("unexpected read")
			}
			change(a)
			if a.nextWizardDepositCheck() {
				t.Fatal("inactive or unsigned workflow started polling")
			}
		})
	}
}

func TestWizardDepositPollDiscardsStaleLifecycleAndManualProgress(t *testing.T) {
	cases := map[string]func(*App, *testBackend){
		"closed":                 func(a *App, _ *testBackend) { a.closed = true },
		"session-epoch":          func(a *App, _ *testBackend) { a.epoch++ },
		"provider-change":        func(a *App, _ *testBackend) { a.exchangeRevision++ },
		"replacement-workflow":   func(a *App, _ *testBackend) { replacement := *a.wizard; a.wizard = &replacement },
		"invalidated-read":       func(a *App, _ *testBackend) { a.wizard.depositPollSerial++ },
		"manual-next-step":       func(a *App, _ *testBackend) { a.wizard.state.Step = 2 },
		"manual-transfer":        func(a *App, _ *testBackend) { a.wizard.state.TransferOperation = "new-original-transfer" },
		"coordinator-generation": func(_ *App, core *testBackend) { core.generation.Add(1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a, ui, core, original := depositPollFixture(t)
			loaded := original
			loaded.Step = 2
			core.handler = func(_ uint64, method string, body, out any) error {
				switch method {
				case "exchangeDepositCheck":
					return answer(out, wizardDepositCredit{Ready: true, ChainConfirmed: true, ChainState: "CONFIRMED"})
				case "exchangeWizardLoad":
					return answer(out, map[string]any{"wizard": &loaded, "wizardDepositState": "CONFIRMED"})
				}
				t.Error("unexpected payment authority", method)
				return errors.New("unexpected mutation")
			}
			if !a.nextWizardDepositCheck() {
				t.Fatal("deposit read not started")
			}
			callback := depositPollCallback(t, ui)
			change(a, core)
			before := viewText(a.wizard.state)
			callback()
			if name == "coordinator-generation" {
				if !a.state.Locked || a.wizard != nil {
					t.Fatal("lost coordinator did not lock and clear deposit state")
				}
				return
			}
			if viewText(a.wizard.state) != before || a.wizard.credit != nil || a.status != "" {
				t.Fatal("stale deposit read overwrote manual progress or another lifecycle", before, viewText(a.wizard.state), a.status)
			}
		})
	}
}

func TestWizardDepositPollTransientFailureRetriesWithoutDialogOrAuthority(t *testing.T) {
	a, ui, core, _ := depositPollFixture(t)
	calls := 0
	core.handler = func(_ uint64, method string, body, out any) error {
		calls++
		if method != "exchangeDepositCheck" {
			t.Error("failed read requested authority or assumed a state", method)
		}
		return errors.New("PRIVATE_PROVIDER_RESPONSE_CANARY")
	}
	a.nextWizardDepositCheck()
	ui.apply(t)
	if calls != 1 || a.wizard.depositChecking || a.wizard.state.Step != 1 || a.busy || a.background || len(a.errorQueue) != 0 || a.statusIsError || ui.cancels.Load() != 0 {
		t.Fatal("transient read failure interrupted or advanced the workflow")
	}
	if strings.Contains(viewText(a.exchangePage()), "PRIVATE_PROVIDER_RESPONSE_CANARY") {
		t.Fatal("private provider text escaped to the page")
	}
	if a.nextWizardDepositCheck() {
		t.Fatal("failed read ignored retry interval")
	}
	a.wizard.depositCheckedAt = time.Now().Add(-6 * time.Second)
	if !a.nextWizardDepositCheck() {
		t.Fatal("transient failure permanently stopped original reconciliation")
	}
	ui.apply(t)
	if calls != 2 {
		t.Fatal("original check was not retried")
	}
}

func TestWizardConfirmedCreditDefaultsTransferToExactNetCreditAndPreservesUserChoice(t *testing.T) {
	for _, value := range []string{"", "6", "5.999999999999999999", "5"} {
		t.Run("amount-"+value, func(t *testing.T) {
			a, _, _, _ := depositPollFixture(t)
			a.wizard.state.Step = 2
			a.fields["wizardTransferAmount"] = value
			check := &wizardDepositCredit{Ready: true, ChainConfirmed: true, ChainState: "CONFIRMED", DepositedAmount: "6", ReportedAmount: "6", DepositFee: "0.000000000000000002", TransferBudget: "5.999999999999999998"}
			a.notifyWizardDepositConfirmed(a.wizard, check)
			want := check.TransferBudget
			if value == "5" {
				want = value
			}
			if a.fields["wizardTransferAmount"] != want {
				t.Fatal("transfer default rounded credit, exceeded fee-adjusted deposit, or replaced valid user choice", a.fields["wizardTransferAmount"], want)
			}
			a.wizard.credit = check
			a.wizard.balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "10", WalletBalance: "10"}}
			a.fields["wizardTransferAmount"] = "6"
			if move, ok := findNode(a.exchangePage(), "wizardMove"); !ok || !move.Disabled {
				t.Fatal("unrelated Funding holdings enabled a debit exceeding exact credited amount")
			}
		})
	}
	a, _, _, _ := depositPollFixture(t)
	a.fields["wizardTransferAmount"] = "5"
	a.notifyWizardDepositConfirmed(a.wizard, &wizardDepositCredit{ChainConfirmed: true, ReportedAmount: "4"})
	if a.fields["wizardTransferAmount"] != "5" {
		t.Fatal("uncredited exchange record changed planned transfer")
	}
	a.wizard.state.TransferOperation = "already-recorded-transfer"
	a.notifyWizardDepositConfirmed(a.wizard, &wizardDepositCredit{Ready: true, ChainConfirmed: true, ReportedAmount: "4"})
	if a.fields["wizardTransferAmount"] != "5" {
		t.Fatal("credit refresh replaced a recorded transfer's amount")
	}
}

func TestWizardGRAMTransferDefaultUsesActualSafeFundingAndKeepsCreditGate(t *testing.T) {
	for _, typed := range []string{"", "6", "5.999985661", "5"} {
		t.Run("typed-"+typed, func(t *testing.T) {
			a, _, core, _ := depositPollFixture(t)
			var source model.Account
			for _, account := range a.state.Accounts {
				if account.Network == "ton-mainnet" {
					source = account
				}
			}
			if source.ID == "" {
				t.Fatal("TON fixture missing")
			}
			a.wizard.state.SourceAccount, a.wizard.state.SourceAsset, a.wizard.state.Step = source.ID, "ton-mainnet:TON", 2
			a.fields["wizardTransferAmount"] = typed
			core.handler = func(_ uint64, method string, _, _ any) error {
				t.Error("suggesting a Funding amount requested authority", method)
				return errors.New("unexpected backend call")
			}
			check := &wizardDepositCredit{Ready: true, ChainConfirmed: true, ChainState: "CONFIRMED", Coin: "GRAM", DepositedAmount: "6", ReportedAmount: "6", TransferBudget: "6", Balances: []wizardBalance{{AccountType: "FUND", Coin: "GRAM", Available: "5.99998566", WalletBalance: "5.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "0", WalletBalance: "0"}}}
			a.wizard.credit, a.wizard.balances = check, check.Balances
			a.notifyWizardDepositConfirmed(a.wizard, check)
			want := "5.99998566"
			if typed == "5" {
				want = typed
			}
			if a.fields["wizardTransferAmount"] != want {
				t.Fatal("default exceeded actual safe Funding balance or changed a valid user amount", a.fields["wizardTransferAmount"], want)
			}
			if move, ok := findNode(a.exchangePage(), "wizardMove"); !ok || move.Disabled {
				t.Fatal("verified GRAM credit and affordable amount stayed disabled")
			}
			check.Ready = false
			if move, ok := findNode(a.exchangePage(), "wizardMove"); !ok || !move.Disabled {
				t.Fatal("Funding balance alone authorized an unverified deposit")
			}
			check.Ready = true
			check.Balances[0].Available = "5.999985660999"
			if got := a.wizardSuggestedTransfer(check); got != "5.99998566" {
				t.Fatal("extra exchange precision rounded a GRAM debit upward", got)
			}
		})
	}
}

func TestWizardPendingDepositFilterRetainsUnsignedAndUnrelatedOperations(t *testing.T) {
	a, _, _, w := depositPollFixture(t)
	operation := journal.Operation{Plan: model.Plan{ID: w.DepositOperation, Account: model.Account{ID: w.SourceAccount}, Asset: w.SourceAsset}}
	for _, state := range []string{"SUBMITTING", "SUBMITTED", "UNKNOWN", "REORGED"} {
		operation.State = state
		if !a.wizardManagesPendingDeposit(operation) {
			t.Fatal("automatic inbox would interrupt managed submitted deposit", state)
		}
	}
	for _, state := range []string{"PREPARED", "AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "FAILED", "CANCELLED", "CONFIRMED"} {
		operation.State = state
		if a.wizardManagesPendingDeposit(operation) {
			t.Fatal("deposit filtering hid an unsigned approval/recovery record", state)
		}
	}
	operation.State = "SUBMITTED"
	operation.Plan.ID = "another-payment"
	if a.wizardManagesPendingDeposit(operation) {
		t.Fatal("deposit filtering hid an unrelated payment")
	}
	operation.Plan.ID = w.DepositOperation
	operation.Plan.Account.ID = "foreign-source"
	if a.wizardManagesPendingDeposit(operation) {
		t.Fatal("deposit filtering accepted a foreign source")
	}
	operation.Plan.Account.ID = w.SourceAccount
	operation.Plan.Asset = "foreign-token"
	if a.wizardManagesPendingDeposit(operation) {
		t.Fatal("deposit filtering accepted a foreign asset")
	}
}

func TestWizardSavedSubmittedDepositDoesNotInterruptUnlockButExplicitRecoveryStillWorks(t *testing.T) {
	a, ui, core, w := depositPollFixture(t)
	a.active, a.wizard = "group/0", nil // Exchange has not been opened since unlock.
	op := pendingFixture(t, a, "SUBMITTED")
	op.Plan.ID, op.Plan.Account.ID, op.Plan.Asset = w.DepositOperation, w.SourceAccount, w.SourceAsset
	before := viewText(op)
	var methods []string
	core.handler = func(_ uint64, method string, body, out any) error {
		methods = append(methods, method)
		switch method {
		case "history":
			return answer(out, journalPage{Operations: []journal.Operation{op}, Total: 1, Revision: "original", Complete: true})
		case "exchangeWizardLoad":
			return answer(out, map[string]any{"wizard": &w})
		default:
			t.Error("startup/recovery display requested payment authority", method)
			return errors.New("unexpected mutation")
		}
	}
	prompts := 0
	ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
		prompts++
		if p.Kind != "pending" || len(p.Choices) != 1 || !strings.Contains(p.Choices[0].Summary, w.DepositOperation) {
			t.Error("explicit recovery lost the original deposit")
		}
		return uiprompt.Reply{PromptID: p.ID, Accepted: false}, nil
	}
	a.queuePendingPayments("")
	if !a.nextPendingPayments() {
		t.Fatal("startup scan not queued")
	}
	ui.apply(t)
	if prompts != 0 || a.wizard != nil || !a.pendingInbox.GlobalChecked || len(a.pendingInbox.Seen) != 0 {
		t.Fatal("saved deposit opened a pending modal or pretended to display it")
	}
	a.forcePendingPayments(w.SourceAccount)
	if !a.nextPendingPayments() {
		t.Fatal("explicit original recovery not queued")
	}
	ui.apply(t)
	if prompts != 1 || strings.Join(methods, ",") != "history,exchangeWizardLoad,history" || viewText(op) != before {
		t.Fatal("explicit recovery hidden, original changed, or a network action requested", methods, prompts)
	}
}

func TestWizardStartupFilterCannotHideForeignUnsignedOrUnrelatedPayments(t *testing.T) {
	for _, mode := range []string{"foreign-operation", "foreign-account", "foreign-asset", "unsigned", "malformed-wizard", "unavailable-wizard", "another-provider"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core, w := depositPollFixture(t)
			a.active, a.wizard = "group/0", nil
			op := pendingFixture(t, a, "SUBMITTED")
			op.Plan.ID, op.Plan.Account.ID, op.Plan.Asset = w.DepositOperation, w.SourceAccount, w.SourceAsset
			switch mode {
			case "foreign-operation":
				w.DepositOperation = "another-deposit"
			case "foreign-account":
				w.SourceAccount = "another-source"
			case "foreign-asset":
				w.SourceAsset = "foreign-token"
			case "unsigned":
				op.State = "PREPARED"
				op.Signed, op.Receipt = nil, nil
			case "malformed-wizard":
				w.ID = "invalid-workflow"
			case "another-provider":
				a.fields["exchangeProvider"] = "binance"
			}
			core.handler = func(_ uint64, method string, body, out any) error {
				switch method {
				case "history":
					return answer(out, journalPage{Operations: []journal.Operation{op}, Total: 1, Revision: "original", Complete: true})
				case "exchangeWizardLoad":
					if mode == "another-provider" {
						t.Error("non-Bybit startup read a Bybit workflow")
					}
					if mode == "unavailable-wizard" {
						return errors.New("local metadata unavailable")
					}
					return answer(out, map[string]any{"wizard": &w})
				default:
					t.Error("startup filter requested authority", method)
					return errors.New("unexpected mutation")
				}
			}
			prompts := 0
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				prompts++
				if p.Kind != "pending" || len(p.Choices) != 1 || !strings.Contains(p.Choices[0].Summary, op.Plan.ID) {
					t.Error("original operation missing from recovery")
				}
				return uiprompt.Reply{PromptID: p.ID, Accepted: false}, nil
			}
			a.queuePendingPayments("")
			a.nextPendingPayments()
			ui.apply(t)
			if prompts != 1 {
				t.Fatal("startup filter hid an unsigned or unrelated operation")
			}
		})
	}
}
