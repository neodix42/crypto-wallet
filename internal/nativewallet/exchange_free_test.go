package nativewallet

// These controller fixtures verify navigation, fresh read contracts and exact
// requested authority. They do not claim live Bybit credit or funded execution.
import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func freeNativeFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account, exchange.WizardState) {
	t.Helper()
	a, ui, backend, source := wizardNativeFixture(t)
	w := exchange.WizardState{ID: strings.Repeat("b", 32), Step: 2, SourceAccount: source.ID, SourceAsset: source.Network + ":native", DepositAmount: "6", DepositOperation: "retained-original-deposit"}
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "wizard": w, "wizardDepositState": "CONFIRMED", "operations": []any{}})
	a.adoptExchangeWizard(a.exchange)
	a.active = "exchange"
	a.fields["freeAccount"], a.fields["freeAsset"] = source.ID, w.SourceAsset
	return a, ui, backend, source, w
}

func freeRequestFields(t *testing.T, body any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func freeSavedAnswer(out any, saved exchange.WizardState, operations []any) error {
	return answer(out, map[string]any{"wizard": saved, "wizardDepositState": "CONFIRMED", "operations": operations})
}

func freePreview(source model.Account, asset string) wizardDepositPreview {
	return wizardDepositPreview{Provider: "bybit", AccountID: source.ID, AssetID: asset, Network: source.Network, Coin: "ETH", Chain: "ETH", Address: "0x2222222222222222222222222222222222222222", Memo: "", ObservedAt: time.Now().UTC().Format(time.RFC3339), Unified: true, CanTransfer: true, CanTrade: true, CanWithdraw: true}
}

func TestFreeExchangeNavigationIsIndependentOfDurablePaymentProgress(t *testing.T) {
	a, _, backend, _, saved := freeNativeFixture(t)
	before := viewText(a.wizard.state)
	backend.handler = func(_ uint64, method string, _, _ any) error {
		t.Error("navigation requested network or transaction authority", method)
		return errors.New("unexpected navigation request")
	}
	if a.wizard.viewStep != 2 {
		t.Fatal("runtime did not restore the visible saved step")
	}
	for _, step := range []int{3, 1, 4, 5, 2, 1} {
		a.event(webui.Event{ID: fmt.Sprintf("wizardStep/%d", step)})
		if a.wizard.viewStep != step || viewText(a.wizard.state) != before || a.wizard.state.DepositOperation != saved.DepositOperation {
			t.Fatal("navigation mutated payment chronology or lost its original", step)
		}
		n, ok := findNode(a.exchangePage(), "exchangeSteps")
		if !ok || n.Value != fmt.Sprint(step) || len(n.Children) != 5 {
			t.Fatal("free navigation not reflected in view", n)
		}
		for i, child := range n.Children {
			if child.Kind != "button" || child.ID != fmt.Sprintf("wizardStep/%d", i+1) || child.Disabled {
				t.Fatal("step cannot be selected freely", child)
			}
		}
	}
	for _, invalid := range []string{"wizardStep/0", "wizardStep/6", "wizardStep/invalid"} {
		a.event(webui.Event{ID: invalid})
		if a.wizard.viewStep != 1 {
			t.Fatal("unreviewed step became a view", invalid)
		}
	}
}

func TestFreeExchangeNewDepositUsesShownDestinationWithoutReplacingOriginal(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	before := viewText(a.wizard.state)
	a.event(webui.Event{ID: "wizardStep/1"})
	a.fields["freeDepositAmount"] = "0.1"
	preview := freePreview(source, saved.SourceAsset)
	deposits := 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeDepositPreview":
			return answer(out, preview)
		case "exchangeDeposit":
			fields := freeRequestFields(t, body)
			if fields["accountId"] != source.ID || fields["assetId"] != saved.SourceAsset || fields["amount"] != "0.1" || fields["expectedAddress"] != preview.Address || fields["expectedMemo"] != preview.Memo || fields["wizardId"] != nil {
				t.Error("new deposit reused a workflow identity or changed its verified destination", fields)
			}
			deposits++
			return answer(out, wizardResult{ID: "distinct-new-deposit", Kind: "deposit", State: "SUBMITTED", TransactionID: "new-signed-message"})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("new deposit requested unrelated or replacement authority", method)
			return errors.New("unexpected deposit method")
		}
	}
	if button, _ := findNode(a.exchangePage(), "freeDeposit"); !button.Disabled {
		t.Fatal("deposit enabled before its destination was read")
	}
	a.wizardAction("freeRefreshDeposit")
	ui.apply(t)
	if button, _ := findNode(a.exchangePage(), "freeDeposit"); button.Disabled {
		t.Fatal("confirmed older deposit prevented a separate explicit deposit")
	}
	a.wizardAction("freeDeposit")
	ui.apply(t)
	if deposits != 1 || viewText(a.wizard.state) != before || len(a.freeData().operations) != 1 || a.freeData().operations[0].ID != "distinct-new-deposit" {
		t.Fatal("new economic action replaced the prior record or lost its identity")
	}
	if button, _ := findNode(a.exchangePage(), "freeDeposit"); !button.Disabled {
		t.Fatal("submitted new deposit allowed an accidental repeat")
	}
	a.wizardAction("freeDeposit")
	if deposits != 1 {
		t.Fatal("forged repeat bypassed the known original operation")
	}
}

func TestFreeExchangeFundingUsesActualAvailabilityWithoutCertifyingOldCredit(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.wizard.credit = &wizardDepositCredit{Ready: false, ChainConfirmed: true, ReadError: "EXCHANGE_READ_DEPOSIT_CREDIT__DEPOSIT_RECORD_REVIEW", Message: "Original deposit review is unresolved."}
	balances := []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5.99998566", WalletBalance: "5.99998566"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0", WalletBalance: "0"}}
	moves, creditReads, balanceReads := 0, 0, 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeFundingBalances":
			balanceReads++
			return answer(out, map[string]any{"balances": balances})
		case "exchangeFundingMove":
			fields := freeRequestFields(t, body)
			if fields["accountId"] != source.ID || fields["assetId"] != saved.SourceAsset || fields["amount"] != "5.99998566" || fields["wizardId"] != nil {
				t.Error("current custody transfer borrowed old deposit authority", fields)
			}
			moves++
			balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0", WalletBalance: "0"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "5.99998566", WalletBalance: "5.99998566"}}
			return answer(out, wizardResult{ID: "funding-current-funds", Kind: "transfer", State: "CONFIRMED"})
		case "exchangeDepositCheck":
			creditReads++
			return errors.New("independent custody action must not require deposit attribution")
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("unexpected Funding method", method)
			return errors.New("unexpected Funding method")
		}
	}
	a.wizardAction("freeRefreshFunding")
	ui.apply(t)
	if a.fields["freeMoveAmount"] != "5.99998566" {
		t.Fatal("Funding default is not affordable", a.fields["freeMoveAmount"])
	}
	if button, _ := findNode(a.exchangePage(), "freeMove"); button.Disabled {
		t.Fatal("an unrelated old credit read blocked explicitly selected custody funds")
	}
	if strings.Contains(viewText(a.exchangePage()), "does not certify the credit") || strings.Contains(viewText(a.exchangePage()), "Original deposit review is unresolved") || strings.Contains(viewText(a.exchangePage()), "Original deposit status") {
		t.Fatal("historical deposit attribution reappeared on the current-funds transfer form")
	}
	a.wizardAction("freeMove")
	ui.apply(t)
	if moves != 1 || creditReads != 0 || a.wizard.credit.Ready || a.wizard.state.TransferOperation != "" || a.wizard.state.DepositOperation != saved.DepositOperation {
		t.Fatal("custody transfer changed the old workflow or falsely approved credit")
	}
	if balanceReads != 2 || wizardAvailable(a.freeData().balances, "FUND", "ETH") != "0" || wizardAvailable(a.freeData().balances, "UNIFIED", "ETH") != "5.99998566" {
		t.Fatal("confirmed transfer did not refresh both custody pools")
	}
}

func TestFreeExchangeSwapUsesUnifiedFundsRatherThanOldTransferBudget(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	saved.Step, saved.TransferOperation, saved.TransferAmount = 3, "old-internal-transfer", "0.1"
	a.adoptWizardState(&saved)
	a.event(webui.Event{ID: "wizardStep/3"})
	_, asset, ok := a.freeSource()
	if !ok {
		t.Fatal("free source missing")
	}
	pair := wizardPair{ID: "eth-usdt", AccountID: source.ID, AssetID: asset.ID, Side: "Sell", QuoteCoin: "USDT", OutputCoin: "USDT", Symbol: "ETHUSDT"}
	a.freeData().pairs = []wizardPair{pair}
	a.fields["freePair"], a.fields["freeTradeAmount"] = pair.ID, "1"
	quote := exchangeValidationQuote(a, source, asset, "Sell", "USDT", time.Now())
	fields := freeRequestFields(t, quote)
	fields["balances"] = []wizardBalance{{AccountType: "UNIFIED", Coin: "ETH", Available: "2", WalletBalance: "2"}}
	a.freeData().quote, _ = json.Marshal(fields)
	swaps := 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeSwap":
			request := freeRequestFields(t, body)
			if request["unifiedOnly"] != true || request["amount"] != "1" || request["wizardId"] != nil || request["accountId"] != source.ID || request["assetId"] != asset.ID {
				t.Error("swap lost its explicitly selected Unified-only funding policy", request)
			}
			swaps++
			return answer(out, wizardResult{ID: "new-unified-swap", Kind: "swap", State: "CONFIRMED", FilledBase: "1", FilledQuote: "100.123456789", Fees: map[string]string{"USDT": "0.1"}})
		case "exchangeWithdrawOptions":
			request := freeRequestFields(t, body)
			if request["existingFunds"] != true || request["coin"] != "USDT" || request["sourceAccountId"] != source.ID {
				t.Error("withdrawal discovery did not follow the received asset/owned group", request)
			}
			_, output, ok := a.freeSelectedAsset("freeWithdrawAsset")
			if !ok {
				t.Fatal("owned output descriptor missing")
			}
			return answer(out, []wizardDestination{{AccountID: source.ID, AssetID: output.ID, Address: source.Address, Coin: "USDT", NetworkName: "Ethereum", Precision: "6", Minimum: "1", Fee: "1", PercentageFee: "0", Index: source.Index}})
		case "exchangeFundingBalances":
			return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "USDT", Available: "1000"}, {AccountType: "UNIFIED", Coin: "USDT", Available: "500"}}})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("visiting Swap moved Funding or replayed another operation", method)
			return errors.New("unexpected Swap method")
		}
	}
	if button, _ := findNode(a.exchangePage(), "freeSwap"); button.Disabled {
		t.Fatal("old transfer ceiling blocked independently available Unified funds", a.freeTradeValidation())
	}
	a.wizardAction("freeSwap")
	ui.apply(t)
	if swaps != 1 || a.wizard.state.TransferAmount != "0.1" || a.wizard.state.SwapOperation != "" {
		t.Fatal("new swap rewrote or reused old operation chronology")
	}
	if a.freeData().outputCoin != "USDT" || a.freeData().outputAmount != "100.023456789" || a.freeData().outputAccount != source.ID {
		t.Fatal("confirmed swap did not retain exact received-asset fees/net output")
	}
	a.event(webui.Event{ID: "wizardStep/5"})
	_, receivedAsset, exists := a.freeSelectedAsset("freeWithdrawAsset")
	if !exists || a.fields["freeAccount"] != source.ID || a.fields["freeAsset"] != saved.SourceAsset || receivedAsset.Symbol != "USDT" {
		t.Fatal("Withdraw did not select the owned received asset", a.fields["freeAccount"], receivedAsset)
	}
	a.wizardAction("freeRefreshWithdraw")
	ui.apply(t)
	if a.fields["freeWithdrawAmount"] != "100.023456" || !strings.Contains(viewText(a.exchangePage()), source.Address) {
		t.Fatal("withdrawal default ignored exact net receipts or network precision", a.fields["freeWithdrawAmount"])
	}
}

func TestFreeExchangeInvalidMoveAmountsRequestNoAuthority(t *testing.T) {
	for _, value := range []string{"0", "-1", "1e3", "NaN", "6"} {
		t.Run(value, func(t *testing.T) {
			a, _, backend, _, _ := freeNativeFixture(t)
			a.fields["freeMoveAmount"] = value
			a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5.99998566"}}
			backend.handler = func(_ uint64, method string, _, _ any) error {
				t.Error("invalid amount requested transaction authority", method)
				return errors.New("unexpected invalid-amount request")
			}
			a.wizardAction("freeMove")
			if a.busy || a.view().ErrorNotice == nil {
				t.Fatal("invalid amount did not stop before backend work", value)
			}
		})
	}
}

func TestFreeExchangeWithdrawalUsesExistingBalancesAndOwnedDestination(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/5"})
	destination := wizardDestination{AccountID: source.ID, AssetID: saved.SourceAsset, Coin: "ETH", NetworkName: "Ethereum", Address: source.Address, Precision: "8", Minimum: "0.001", Fee: "0.0001", PercentageFee: "0", Index: source.Index}
	withdrawals := 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeWithdrawOptions":
			fields := freeRequestFields(t, body)
			if fields["coin"] != "ETH" || fields["existingFunds"] != true || fields["sourceAccountId"] != source.ID {
				t.Error("withdrawal route discovery lost owned source/current-funds scope", fields)
			}
			return answer(out, []wizardDestination{destination})
		case "exchangeFundingBalances":
			return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5.123456789"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "2"}}})
		case "exchangeWithdraw":
			fields := freeRequestFields(t, body)
			if fields["existingFunds"] != true || fields["fundingOnly"] != true || fields["accountId"] != destination.AccountID || fields["assetId"] != destination.AssetID || fields["amount"] != "5.12345678" || fields["wizardId"] != nil || fields["address"] != nil {
				t.Error("withdrawal reused an old proceeds budget or accepted a caller address", fields)
			}
			withdrawals++
			return answer(out, wizardResult{ID: "current-funds-withdrawal", Kind: "withdraw", State: "SUBMITTED"})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("unexpected withdrawal authority", method)
			return errors.New("unexpected withdrawal method")
		}
	}
	a.wizardAction("freeRefreshWithdraw")
	ui.apply(t)
	if a.fields["freeWithdrawAmount"] != "5.12345678" || !strings.Contains(viewText(a.exchangePage()), source.Address) {
		t.Fatal("withdrawal did not default to current balances and selected network precision")
	}
	if button, _ := findNode(a.exchangePage(), "freeWithdraw"); button.Disabled {
		t.Fatal("missing historical swap prevented an independent affordable withdrawal")
	}
	a.fields["freeWithdrawAmount"] = "6"
	if button, _ := findNode(a.exchangePage(), "freeWithdraw"); !button.Disabled {
		t.Fatal("withdrawal greater than verified Funding funds stayed enabled")
	}
	a.fields["freeWithdrawAmount"] = "5.12345678"
	a.wizardAction("freeWithdraw")
	ui.apply(t)
	if withdrawals != 1 || a.wizard.state.WithdrawOperation != "" || a.wizard.state.SwapOperation != "" || a.wizard.state.DepositOperation != saved.DepositOperation {
		t.Fatal("independent withdrawal fabricated old swap/payment chronology")
	}
}

func TestFreeExchangeSubmittedDepositChecksOnlyItsOriginalSignedIdentity(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/1"})
	a.freeData().operations = []freeExchangeOperation{{Stage: 1, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "current-original-deposit", Kind: "deposit", State: "SUBMITTED"}}}
	var calls []string
	backend.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		switch method {
		case "reconcile":
			if freeRequestFields(t, body)["id"] != "current-original-deposit" {
				t.Error("deposit status query changed its original identity", body)
			}
			return answer(out, model.Receipt{Identity: "checked-chain-identity", State: "CONFIRMED", Detail: "Checked original delivery."})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{})
		default:
			t.Error("deposit check requested a signature, replacement or submit", method)
			return errors.New("unexpected deposit-check method")
		}
	}
	a.wizardAction("freeCheck/current-original-deposit")
	ui.apply(t)
	if strings.Join(calls, ",") != "reconcile,exchangeWizardLoad" || len(a.freeData().operations) != 1 || a.freeData().operations[0].ID != "current-original-deposit" || a.freeData().operations[0].State != "CONFIRMED" || a.wizard.state.DepositOperation != saved.DepositOperation {
		t.Fatal("original deposit check lost or replaced a recorded identity", calls)
	}
}

func TestFreeExchangeReadCannotOverwriteNewViewOrSource(t *testing.T) {
	for _, change := range []string{"view", "source"} {
		t.Run(change, func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			a.event(webui.Event{ID: "wizardStep/1"})
			backend.handler = func(_ uint64, method string, _, out any) error {
				switch method {
				case "exchangeDepositPreview":
					return answer(out, freePreview(source, saved.SourceAsset))
				case "exchangeWizardLoad":
					return freeSavedAnswer(out, saved, []any{})
				default:
					t.Error("read requested economic authority", method)
					return errors.New("unexpected read")
				}
			}
			a.wizardAction("freeRefreshDeposit")
			callback := depositPollCallback(t, ui)
			if change == "view" {
				if button, _ := findNode(a.exchangePage(), "wizardStep/3"); button.Disabled {
					t.Fatal("read blocked independent step navigation")
				}
				a.event(webui.Event{ID: "wizardStep/3"})
			} else {
				for _, account := range a.state.Accounts {
					if account.Network == "arbitrum-mainnet" {
						a.event(webui.Event{ID: "field/freeAccount", Value: account.ID})
					}
				}
			}
			callback()
			if a.freeData().preview != nil || a.busy || change == "view" && a.wizard.viewStep != 3 || change == "source" && a.fields["freeAccount"] == source.ID {
				t.Fatal("stale response replaced a newer navigation/source choice")
			}
		})
	}
}

func TestFreeExchangeWriteRetainsOriginalOutcomeAfterSourceEdit(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.fields["freeMoveAmount"] = "1"
	a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5"}}
	started, release := make(chan struct{}), make(chan struct{})
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeFundingMove":
			fields := freeRequestFields(t, body)
			if fields["accountId"] != source.ID || fields["assetId"] != saved.SourceAsset || fields["amount"] != "1" {
				t.Error("in-flight immutable write changed", fields)
			}
			close(started)
			<-release
			return answer(out, wizardResult{ID: "original-uncertain-transfer", Kind: "transfer", State: "UNKNOWN"})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, []any{map[string]string{"provider": "bybit", "id": "original-uncertain-transfer", "kind": "transfer", "state": "UNKNOWN", "accountId": source.ID, "assetId": saved.SourceAsset}})
		default:
			return errors.New("unexpected write method")
		}
	}
	a.wizardAction("freeMove")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("expected explicit Funding move not requested")
	}
	for _, account := range a.state.Accounts {
		if account.Network == "arbitrum-mainnet" {
			a.event(webui.Event{ID: "field/freeAccount", Value: account.ID})
		}
	}
	close(release)
	ui.apply(t)
	retained := false
	for _, operation := range a.freeData().operations {
		if operation.ID == "original-uncertain-transfer" && operation.AccountID == source.ID && operation.AssetID == saved.SourceAsset && operation.State == "UNKNOWN" {
			retained = true
		}
	}
	if !retained || !strings.Contains(string(a.exchange), "original-uncertain-transfer") || a.wizard.state.DepositOperation != saved.DepositOperation {
		t.Fatal("a field edit hid the economic result or its original journal identity")
	}
}

func TestFreeExchangeRestoredUnknownMoveDoesNotGateFreshFundingAction(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	oldID := "restored-original-transfer"
	operations := []any{map[string]string{"provider": "bybit", "id": oldID, "kind": "transfer", "state": "UNKNOWN", "accountId": source.ID, "assetId": saved.SourceAsset}}
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "wizard": saved, "wizardDepositState": "CONFIRMED", "operations": operations})
	a.fields["freeMoveAmount"] = "1"
	a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0"}}
	moves := 0
	backend.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "exchangeFundingMove":
			f := freeRequestFields(t, body)
			if f["accountId"] != source.ID || f["assetId"] != saved.SourceAsset || f["amount"] != "1" || f["id"] != nil || f["wizardId"] != nil {
				t.Error("new action reused old identity or different source", f)
			}
			moves++
			return answer(out, wizardResult{ID: "separately-approved-current-funds-move", Kind: "transfer", State: "PENDING"})
		case "exchangeWizardLoad":
			return freeSavedAnswer(out, saved, operations)
		default:
			t.Error("current funds action requested original check or unexpected authority", method)
			return errors.New("unexpected current-funds request")
		}
	}
	page := a.exchangePage()
	if button, _ := findNode(page, "freeMove"); button.Disabled {
		t.Fatal("old unknown transfer gated an affordable current Funding action")
	}
	for _, id := range []string{"freeCheck/" + oldID, "exchangeReconcile/" + oldID} {
		if _, exists := findNode(page, id); exists {
			t.Fatal("manual original check remained visible", id)
		}
	}
	a.wizardAction("freeMove")
	ui.apply(t)
	if moves != 1 || a.wizard.state.DepositOperation != saved.DepositOperation || !strings.Contains(string(a.exchange), oldID) || !strings.Contains(string(a.exchange), "UNKNOWN") || a.view().ErrorNotice != nil {
		t.Fatal("current funds action waited for or discarded its older original", moves, a.exchange)
	}
}

func freeTrackerFixture(t *testing.T) (*App, *testFrontend, *testBackend, *exchangeFreeUI, freeExchangeOperation) {
	t.Helper()
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.event(webui.Event{ID: "wizardStep/1"})
	op := freeExchangeOperation{Stage: 1, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "new-original-tracked-deposit", Kind: "deposit", State: "SUBMITTED"}}
	u := a.freeData()
	u.operations = []freeExchangeOperation{op}
	return a, ui, backend, u, op
}

func TestFreeExchangeTrackerOnlyReconcilesOriginalDepositAndNotifiesOnce(t *testing.T) {
	a, ui, backend, u, original := freeTrackerFixture(t)
	activity, statusRevision := a.lastActivity, a.statusRevision
	var calls []string
	backend.handler = func(_ uint64, method string, body, out any) error {
		calls = append(calls, method)
		if method != "reconcile" || freeRequestFields(t, body)["id"] != original.ID {
			t.Error("tracker requested preparation, signature, replacement or wrong identity", method, body)
			return errors.New("unexpected tracker authority")
		}
		return answer(out, model.Receipt{Identity: "checked-original-identity", State: "CONFIRMED", Detail: "Original recipient delivery checked."})
	}
	if !a.nextFreeExchangeCheck() || a.busy || a.background || a.nextFreeExchangeCheck() {
		t.Fatal("tracker blocked the page or started duplicate reconciliation")
	}
	ui.apply(t)
	if strings.Join(calls, ",") != "reconcile" || len(u.operations) != 1 || u.operations[0].ID != original.ID || u.operations[0].State != "CONFIRMED" || a.wizard.viewStep != 2 {
		t.Fatal("original delivery was not retained or visible Deposit did not advance", calls)
	}
	if a.statusRevision != statusRevision+1 || !strings.Contains(a.status, "CONFIRMED") || !a.lastActivity.Equal(activity) || ui.cancels.Load() != 0 || len(a.errorQueue) != 0 || a.view().ErrorNotice != nil {
		t.Fatal("readonly reconciliation prompted, extended unlock or failed to notify")
	}
	u.checkedAt = time.Now().Add(-6 * time.Second)
	if a.nextFreeExchangeCheck() || a.statusRevision != statusRevision+1 {
		t.Fatal("confirmed operation kept polling/notifying")
	}
}

func TestFreeExchangeTrackerDropsStaleLifecycleIdentityAndStateSnapshots(t *testing.T) {
	for _, change := range []string{"lock", "provider", "pointer", "newer-state", "wrong-returned-id"} {
		t.Run(change, func(t *testing.T) {
			a, ui, backend, u, original := freeTrackerFixture(t)
			if change == "wrong-returned-id" {
				u.operations[0].Stage, u.operations[0].Kind = 2, "transfer"
			}
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method == "exchangeReconcile" && change == "wrong-returned-id" {
					return answer(out, wizardResult{ID: "different-operation", Kind: "transfer", State: "CONFIRMED"})
				}
				if method != "reconcile" {
					t.Error("tracker acquired authority", method)
				}
				state := "CONFIRMED"
				if change == "newer-state" {
					state = "SUBMITTED" // A valid old reply delayed after a manual check.
				}
				return answer(out, model.Receipt{Identity: "checked-identity", State: state})
			}
			if !a.nextFreeExchangeCheck() {
				t.Fatal("tracker did not start")
			}
			callback := depositPollCallback(t, ui)
			switch change {
			case "lock":
				a.lock()
			case "provider":
				a.fields["exchangeProvider"] = "binance"
			case "pointer":
				a.wizard.free = &exchangeFreeUI{operations: []freeExchangeOperation{original}}
			case "newer-state":
				u.operations[0].State = "CONFIRMED"
			}
			beforeStatus, revision := a.status, a.statusRevision
			callback()
			if a.status != beforeStatus || a.statusRevision != revision {
				t.Fatal("stale snapshot emitted a completion notice")
			}
			if change == "lock" {
				if !a.state.Locked || a.wizard != nil {
					t.Fatal("stale result restored a locked flow")
				}
				return
			}
			want := "SUBMITTED"
			if change == "newer-state" {
				want = "CONFIRMED"
			}
			if a.freeData().operations[0].State != want || a.freeData().operations[0].ID != original.ID || a.wizard.viewStep != 1 {
				t.Fatal("stale receipt changed an original or current navigation", change)
			}
		})
	}
}

func TestFreeExchangeTrackerRetriesTransientReadsWithoutModalOrAnotherSend(t *testing.T) {
	a, ui, backend, u, original := freeTrackerFixture(t)
	calls := 0
	backend.handler = func(_ uint64, method string, _, _ any) error {
		calls++
		if method != "reconcile" {
			t.Error("transient check requested economic authority", method)
		}
		return errors.New("PRIVATE_PROVIDER_ERROR_CANARY")
	}
	if !a.nextFreeExchangeCheck() {
		t.Fatal("pending original was not checked")
	}
	ui.apply(t)
	if u.checking || a.busy || a.background || len(a.errorQueue) != 0 || a.view().ErrorNotice != nil || a.status != "" || ui.cancels.Load() != 0 || u.operations[0].ID != original.ID || u.operations[0].State != original.State {
		t.Fatal("read failure interrupted the UI or changed a pending identity")
	}
	if strings.Contains(viewText(a.exchangePage()), "PRIVATE_PROVIDER_ERROR_CANARY") || a.nextFreeExchangeCheck() {
		t.Fatal("private error escaped or retry interval was ignored")
	}
	u.checkedAt = time.Now().Add(-6 * time.Second)
	if !a.nextFreeExchangeCheck() {
		t.Fatal("transient failure permanently stopped reconciliation")
	}
	ui.apply(t)
	if calls != 2 || u.operations[0].State != original.State {
		t.Fatal("retry re-created or resolved the original operation")
	}
}

func TestFreeExchangeAsyncSwapRemembersOriginalPairAndNetWithdrawalAsset(t *testing.T) {
	for _, editedPair := range []bool{false, true} {
		t.Run(fmt.Sprintf("edited-pair-%t", editedPair), func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			a.event(webui.Event{ID: "wizardStep/3"})
			_, asset, ok := a.freeSource()
			if !ok {
				t.Fatal("source missing")
			}
			pair := wizardPair{ID: "original-async-pair", AccountID: source.ID, AssetID: asset.ID, Side: "Sell", QuoteCoin: "USDT", OutputCoin: "USDT", Symbol: "ETHUSDT"}
			a.freeData().pairs = []wizardPair{pair}
			a.fields["freePair"], a.fields["freeTradeAmount"] = pair.ID, "1"
			quote := freeRequestFields(t, exchangeValidationQuote(a, source, asset, "Sell", "USDT", time.Now()))
			quote["balances"] = []wizardBalance{{AccountType: "UNIFIED", Coin: "ETH", Available: "2"}}
			a.freeData().quote, _ = json.Marshal(quote)
			writes, checks := 0, 0
			backend.handler = func(_ uint64, method string, body, out any) error {
				switch method {
				case "exchangeSwap":
					writes++
					if freeRequestFields(t, body)["unifiedOnly"] != true {
						t.Error("async order lost Unified-only intent")
					}
					return answer(out, wizardResult{ID: "async-original-swap", Kind: "swap", State: "PENDING"})
				case "exchangeReconcile":
					checks++
					if freeRequestFields(t, body)["id"] != "async-original-swap" {
						t.Error("async lookup replaced the order identity")
					}
					return answer(out, wizardResult{ID: "async-original-swap", Kind: "swap", State: "CONFIRMED", FilledBase: "1", FilledQuote: "100.123456789", Fees: map[string]string{"USDT": "0.1"}})
				case "exchangeWizardLoad":
					return freeSavedAnswer(out, saved, []any{})
				default:
					t.Error("async status lookup requested unrelated authority", method)
					return errors.New("unexpected async order method")
				}
			}
			a.wizardAction("freeSwap")
			ui.apply(t)
			if len(a.freeData().operations) != 1 || a.freeData().operations[0].pair.ID != pair.ID || a.freeData().outputCoin != "" {
				t.Fatal("pending order fabricated proceeds or lost original pair metadata")
			}
			if editedPair {
				a.event(webui.Event{ID: "field/freePair", Value: "different-user-selection"})
			}
			if !a.nextFreeExchangeCheck() {
				t.Fatal("pending original order was not tracked")
			}
			ui.apply(t)
			if writes != 1 || checks != 1 || a.freeData().operations[0].ID != "async-original-swap" || a.freeData().operations[0].State != "CONFIRMED" {
				t.Fatal("async check replayed an order or failed to retain confirmation")
			}
			if editedPair {
				if a.freeData().outputCoin != "" {
					t.Fatal("late fill changed an edited asset selection")
				}
				return
			}
			if a.freeData().outputCoin != "USDT" || a.freeData().outputAmount != "100.023456789" || a.freeData().outputAccount != source.ID {
				t.Fatal("async fill lost exact original output coin/net fees")
			}
			a.event(webui.Event{ID: "wizardStep/5"})
			_, output, exists := a.freeSelectedAsset("freeWithdrawAsset")
			if !exists || output.Symbol != "USDT" || a.fields["freeAccount"] != source.ID || a.fields["freeAsset"] != saved.SourceAsset {
				t.Fatal("async completed swap did not select its owned withdrawal asset")
			}
		})
	}
}

func TestFreeExchangeAsyncTransferRefreshesOnlyItsSelectedOriginalPools(t *testing.T) {
	for _, changedSource := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed-source-%t", changedSource), func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			u := a.freeData()
			u.balances = []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "5"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0"}}
			u.operations = []freeExchangeOperation{{Stage: 2, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "async-original-transfer", Kind: "transfer", State: "PENDING"}}}
			a.fields["freeMoveAmount"] = "5"
			u.transferDefaults = map[string]string{"freeMoveAmount": "5"} // Untouched amount consumed by its original explicit submission.
			var calls []string
			backend.handler = func(_ uint64, method string, body, out any) error {
				calls = append(calls, method)
				switch method {
				case "exchangeReconcile":
					if freeRequestFields(t, body)["id"] != "async-original-transfer" {
						t.Error("transfer check changed its original UUID")
					}
					return answer(out, wizardResult{ID: "async-original-transfer", Kind: "transfer", State: "CONFIRMED"})
				case "exchangeFundingBalances":
					fields := freeRequestFields(t, body)
					if fields["accountId"] != source.ID || fields["assetId"] != saved.SourceAsset {
						t.Error("transfer refresh read another source", fields)
					}
					return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0.25"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "4.75"}}})
				default:
					t.Error("confirmed transfer tracking requested another debit", method)
					return errors.New("unexpected transfer tracker method")
				}
			}
			if !a.nextFreeExchangeCheck() {
				t.Fatal("pending transfer was not checked")
			}
			callback := depositPollCallback(t, ui)
			if changedSource {
				for _, account := range a.state.Accounts {
					if account.Network == "arbitrum-mainnet" {
						a.event(webui.Event{ID: "field/freeAccount", Value: account.ID})
					}
				}
			}
			callback()
			if strings.Join(calls, ",") != "exchangeReconcile,exchangeFundingBalances" || u.operations[0].State != "CONFIRMED" {
				t.Fatal("tracker did not reread both pools after original transfer confirmation", calls)
			}
			if changedSource {
				if u.balances != nil || a.fields["freeMoveAmount"] != "" {
					t.Fatal("old source's pool values overwrote the selected source")
				}
			} else if wizardAvailable(u.balances, "FUND", "ETH") != "0.25" || wizardAvailable(u.balances, "UNIFIED", "ETH") != "4.75" || a.fields["freeMoveAmount"] != "0.25" {
				t.Fatal("async confirmation retained stale pools or old transfer default")
			}
		})
	}
}

func TestFreeExchangeUnsignedCancelRequiresExplicitActionAndKeepsOriginalID(t *testing.T) {
	for _, state := range []string{"PREPARED", "AUTHORIZED", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			operation := map[string]string{"provider": "bybit", "id": "original-cancel-candidate", "kind": "transfer", "state": state, "accountId": source.ID, "assetId": saved.SourceAsset}
			a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "operations": []any{operation}})
			cancelled := 0
			backend.handler = func(_ uint64, method string, body, out any) error {
				switch method {
				case "exchangeCancel":
					cancelled++
					if freeRequestFields(t, body)["id"] != "original-cancel-candidate" {
						t.Error("unsigned cancellation replaced its original identity")
					}
					operation["state"] = "CANCELLED"
					return nil
				case "exchangeWizardLoad":
					return freeSavedAnswer(out, saved, []any{operation})
				default:
					t.Error("cancellation prepared or submitted another operation", method)
					return errors.New("unexpected cancellation method")
				}
			}
			_ = a.exchangePage()
			a.event(webui.Event{ID: "wizardStep/3"})
			if cancelled != 0 {
				t.Fatal("view rendering/navigation cancelled economic state")
			}
			a.wizardAction("freeCancel/original-cancel-candidate")
			if state == "UNKNOWN" {
				if cancelled != 0 || a.busy || operation["state"] != "UNKNOWN" {
					t.Fatal("possibly submitted operation was cancelled")
				}
				return
			}
			ui.apply(t)
			if cancelled != 1 || operation["state"] != "CANCELLED" || a.wizard.state.DepositOperation != saved.DepositOperation {
				t.Fatal("explicit unsigned cancellation lost an original record")
			}
		})
	}
}
