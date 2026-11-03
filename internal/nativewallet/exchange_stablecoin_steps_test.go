package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func stablecoinFreeFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account) {
	t.Helper()
	a, ui, backend, _, _ := freeNativeFixture(t)
	for _, account := range a.state.Accounts {
		if account.Network == "ton-mainnet" {
			a.fields["freeAccount"], a.fields["freeAsset"] = account.ID, "ton-mainnet:TON"
			a.freeData().balances = []wizardBalance{
				{AccountType: "FUND", Coin: "GRAM", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "GRAM", Available: "5"},
				{AccountType: "FUND", Coin: "USDT", Available: "0"}, {AccountType: "UNIFIED", Coin: "USDT", Available: "12.5"},
				{AccountType: "FUND", Coin: "USDC", Available: "8.125"}, {AccountType: "UNIFIED", Coin: "USDC", Available: "9.5"},
			}
			return a, ui, backend, account
		}
	}
	t.Fatal("public fixture lacks TON")
	return nil, nil, nil, model.Account{}
}

func freeChoiceForCoin(t *testing.T, a *App, coin string) freeOwnedAsset {
	t.Helper()
	for _, choice := range a.freeOwnedAssets() {
		if choice.asset.Symbol == coin {
			return choice
		}
	}
	t.Fatal("owned reviewed coin choice missing", coin)
	return freeOwnedAsset{}
}

func TestFreeStablecoinsRemainVisibleOnAllFiveIndependentPages(t *testing.T) {
	a, _, backend, source := stablecoinFreeFixture(t)
	backend.handler = func(_ uint64, method string, _, _ any) error {
		t.Error("navigation acquired backend authority", method)
		return errors.New("unexpected navigation call")
	}
	for _, step := range []string{"1", "2", "3", "4", "5"} {
		a.event(webui.Event{ID: "wizardStep/" + step})
		page := a.exchangePage()
		summary, ok := findNode(page, "freeStablecoinBalances")
		if !ok || summary.Kind != "column" || len(summary.Children) != 3 {
			t.Fatal("compact stablecoin overview missing", step, summary)
		}
		for _, expected := range []string{"Funding available", "Unified available", "USDT", "USDC", "12.5", "8.125", "9.5"} {
			if !strings.Contains(viewText(summary), expected) {
				t.Fatal("verified stablecoin balance missing", step, expected)
			}
		}
		if summary.Children[1].Children[1].Text != "0" {
			t.Fatal("observed zero was converted to unknown", summary)
		}
		if _, ok := findNode(page, "freeRefreshBalances"); !ok {
			t.Fatal("current pools cannot be refreshed on this page", step)
		}
		if a.fields["freeAccount"] != source.ID || a.fields["freeAsset"] != "ton-mainnet:TON" {
			t.Fatal("visiting a custody page changed the deposit descriptor")
		}
	}
	a.freeData().balances = []wizardBalance{}
	if summary := a.freeStablecoinBalances(); strings.Count(viewText(summary), "Not yet verified") != 4 {
		t.Fatal("unknown stablecoins became zero", summary)
	}
}

func TestFreeMoveSelectorsBindOwnedStablecoinIndependentlyOfGRAMDeposit(t *testing.T) {
	for _, stage := range []int{2, 4} {
		for _, coin := range []string{"USDT", "USDC"} {
			t.Run(string(rune('0'+stage))+"-"+coin, func(t *testing.T) {
				a, ui, backend, source := stablecoinFreeFixture(t)
				a.wizard.viewStep = stage
				key, amountField, action, method := "freeMoveAsset", "freeMoveAmount", "freeMove", "exchangeFundingMove"
				if stage == 4 {
					key, amountField, action, method = "freeReturnAsset", "freeReturnAmount", "freeReturn", "exchangeUnifiedMove"
				}
				choice := freeChoiceForCoin(t, a, coin)
				if coin == "USDC" && choice.account.Network == source.Network {
					t.Fatal("USDC fabricated on TON")
				}
				a.event(webui.Event{ID: "field/" + key, Value: choice.id()})
				a.freeData().balances = []wizardBalance{{AccountType: "FUND", Coin: coin, Available: "8.125"}, {AccountType: "UNIFIED", Coin: coin, Available: "9.5"}}
				a.event(webui.Event{ID: "field/" + amountField, Value: "1.25"})
				a.freeData().operations = []freeExchangeOperation{{Stage: stage, wizardResult: wizardResult{ID: "old-uncertain-transfer", Kind: "transfer", State: "UNKNOWN"}}}
				a.freeData().balanceScope = choice.id() + "|" + string(rune('0'+stage))
				a.freeData().balanceCheckedAt = time.Now()
				writes := 0
				backend.handler = func(_ uint64, got string, body, out any) error {
					switch got {
					case method:
						if a.freeData().balanceScope != "" {
							t.Error("a new action retained the previous custody refresh throttle")
						}
						f := freeRequestFields(t, body)
						if f["accountId"] != choice.account.ID || f["assetId"] != choice.asset.ID || f["amount"] != "1.25" || f["wizardId"] != nil || f["coin"] != nil {
							t.Error("transfer did not bind the selected owned asset", f)
						}
						writes++
						return answer(out, wizardResult{ID: "new-distinct-" + coin, Kind: "transfer", State: "CONFIRMED"})
					case "exchangeFundingBalances":
						f := freeRequestFields(t, body)
						if f["accountId"] != choice.account.ID || f["assetId"] != choice.asset.ID {
							t.Error("post-transfer read selected the deposit asset", f)
						}
						return answer(out, map[string]any{"balances": a.freeData().balances})
					case "exchangeWizardLoad":
						return answer(out, map[string]any{"operations": []any{}})
					default:
						t.Error("independent transfer requested unrelated authority", got)
						return errors.New("unexpected method")
					}
				}
				if button, _ := findNode(a.exchangePage(), action); button.Disabled {
					t.Fatal("selected affordable stablecoin stayed disabled")
				}
				a.freeExchangeAction(action)
				ui.apply(t)
				if writes != 1 || a.fields["freeAccount"] != source.ID || a.fields["freeAsset"] != "ton-mainnet:TON" || a.freeData().operations[0].State != "UNKNOWN" {
					t.Fatal("transfer changed the deposit asset or older request")
				}
			})
		}
	}
}

func TestFreeStablecoinSelectionPreservesIndependentDraftsAndRejectsForeignDescriptor(t *testing.T) {
	a, _, backend, _ := stablecoinFreeFixture(t)
	a.wizard.viewStep = 4
	usdc := freeChoiceForCoin(t, a, "USDC")
	a.event(webui.Event{ID: "field/freeReturnAsset", Value: usdc.id()})
	a.event(webui.Event{ID: "field/freeReturnAmount", Value: "3"})
	a.setFreeBalances([]wizardBalance{{AccountType: "UNIFIED", Coin: "USDC", Available: "5"}})
	a.freeRefreshTransferDrafts()
	if a.fields["freeReturnAmount"] != "3" {
		t.Fatal("fresh stablecoin balance replaced the entered amount")
	}
	a.freeData().outputCoin, a.freeData().outputAccount = "USDT", usdc.account.ID
	a.freeSelectOutput()
	if a.fields["freeReturnAsset"] != usdc.id() || a.fields["freeReturnAmount"] != "3" {
		t.Fatal("late swap output replaced an independently selected return asset")
	}
	backend.handler = func(_ uint64, method string, _, _ any) error {
		t.Error("forged descriptor requested backend authority", method)
		return errors.New("unexpected authority")
	}
	a.event(webui.Event{ID: "field/freeReturnAsset", Value: "not-owned|USDC"})
	a.fields["freeReturnAmount"] = "1"
	if button, _ := findNode(a.exchangePage(), "freeReturn"); !button.Disabled {
		t.Fatal("forged descriptor enabled a transfer")
	}
	a.freeExchangeAction("freeReturn")
	if a.busy || a.view().ErrorNotice == nil {
		t.Fatal("forged descriptor did not fail before preparation")
	}
}

func TestFreeWithdrawalSelectsUSDCAndUSDTWithExactOwnedNetworkRoutes(t *testing.T) {
	for _, coin := range []string{"USDT", "USDC"} {
		t.Run(coin, func(t *testing.T) {
			a, ui, backend, source := stablecoinFreeFixture(t)
			a.wizard.viewStep = 5
			choice := freeChoiceForCoin(t, a, coin)
			a.event(webui.Event{ID: "field/freeWithdrawAsset", Value: choice.id()})
			d := wizardDestination{AccountID: choice.account.ID, AssetID: choice.asset.ID, Coin: coin, Address: choice.account.Address, NetworkName: a.network(choice.account.Network).Name, Index: choice.account.Index, Precision: "6", Minimum: "1", Fee: "0.1", PercentageFee: "0"}
			wrong := d
			wrong.Coin = "GRAM"
			wrongAddress := d
			wrongAddress.Address = "foreign-receiving-address"
			withdrawals := 0
			backend.handler = func(_ uint64, method string, body, out any) error {
				switch method {
				case "exchangeWithdrawOptions":
					f := freeRequestFields(t, body)
					if f["coin"] != coin || f["sourceAccountId"] != source.ID || f["existingFunds"] != true {
						t.Error("withdrawal route did not use the independent stablecoin within its original group", f)
					}
					return answer(out, []wizardDestination{wrong, wrongAddress, d})
				case "exchangeFundingBalances":
					return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: coin, Available: "8.125"}, {AccountType: "UNIFIED", Coin: coin, Available: "100"}}})
				case "exchangeWithdraw":
					f := freeRequestFields(t, body)
					if f["accountId"] != d.AccountID || f["assetId"] != d.AssetID || f["amount"] != "5.125" || f["fundingOnly"] != true || f["existingFunds"] != true || f["address"] != nil {
						t.Error("withdrawal changed the owned asset/address or funding-only policy", f)
					}
					withdrawals++
					return answer(out, wizardResult{ID: "new-stable-withdrawal", Kind: "withdraw", State: "SUBMITTED"})
				case "exchangeWizardLoad":
					return answer(out, map[string]any{"operations": []any{}})
				default:
					t.Error("withdrawal made an implicit transfer or unrelated call", method)
					return errors.New("unexpected withdrawal method")
				}
			}
			a.freeExchangeAction("freeRefreshWithdraw")
			ui.apply(t)
			if len(a.freeData().destinations) != 1 || a.fields["freeWithdrawAmount"] != "8.125" {
				t.Fatal("wrong coin/foreign address retained or Funding default lost", a.freeData().destinations, a.fields)
			}
			a.event(webui.Event{ID: "field/freeWithdrawAmount", Value: "5.125"})
			if button, _ := findNode(a.exchangePage(), "freeWithdraw"); button.Disabled {
				t.Fatal("stablecoin withdrawal stayed disabled")
			}
			a.freeExchangeAction("freeWithdraw")
			ui.apply(t)
			if withdrawals != 1 || a.fields["freeAsset"] != "ton-mainnet:TON" {
				t.Fatal("stablecoin withdrawal replaced the GRAM deposit selection")
			}
		})
	}
}

func TestFreeStablecoinAutomaticRefreshIsReadOnlyAndDiscardsStaleObservations(t *testing.T) {
	for _, change := range []string{"none", "view", "provider", "generation", "newer-funds", "error"} {
		t.Run(change, func(t *testing.T) {
			a, ui, backend, _ := stablecoinFreeFixture(t)
			a.wizard.viewStep = 4
			u := a.freeData()
			lastActivity := a.lastActivity
			backend.handler = func(_ uint64, method string, body, out any) error {
				if method != "exchangeFundingBalances" {
					t.Error("automatic overview requested economic authority", method)
				}
				if change == "error" {
					return errors.New("PRIVATE_PROVIDER_CANARY")
				}
				return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "USDC", Available: "0"}, {AccountType: "UNIFIED", Coin: "USDC", Available: "7"}}})
			}
			if !a.nextFreeBalanceRefresh() || a.busy || a.background || a.nextFreeBalanceRefresh() || a.lastActivity != lastActivity {
				t.Fatal("overview read blocked navigation, extended activity or duplicated its request")
			}
			switch change {
			case "view":
				a.event(webui.Event{ID: "wizardStep/2"})
			case "provider":
				a.fields["exchangeProvider"] = "binance"
			case "generation":
				backend.generation.Add(1)
			case "newer-funds":
				a.setFreeBalances([]wizardBalance{{AccountType: "UNIFIED", Coin: "USDC", Available: "3"}})
			}
			ui.apply(t)
			if u.balanceChecking || a.busy || a.background || a.view().ErrorNotice != nil || len(a.errorQueue) != 0 || strings.Contains(viewText(a.exchangePage()), "PRIVATE_PROVIDER_CANARY") {
				t.Fatal("read-only refresh retained work or exposed an error dialog")
			}
			if change == "none" && (wizardAvailable(u.balances, "FUND", "USDC") != "0" || wizardAvailable(u.balances, "UNIFIED", "USDC") != "7" || a.nextFreeBalanceRefresh()) {
				t.Fatal("fresh stablecoin pools missing or polling throttle lost")
			}
			if change == "newer-funds" && wizardAvailable(u.balances, "UNIFIED", "USDC") != "3" {
				t.Fatal("old background observation replaced newer funds")
			}
			if change == "view" && wizardAvailable(u.balances, "UNIFIED", "USDC") != "9.5" {
				t.Fatal("old page read changed the new page")
			}
			if change == "generation" && !a.state.Locked {
				t.Fatal("generation change did not invalidate the session")
			}
			if change == "error" && (len(u.balances) != 0 || !strings.Contains(viewText(a.freeStablecoinBalances()), "Not yet verified")) {
				t.Fatal("failed overview retained spendable or zero balance")
			}
		})
	}
}

func TestFreeStablecoinAutomaticRefreshKeepsTypedTransferAndQuoteAge(t *testing.T) {
	a, ui, backend, _ := stablecoinFreeFixture(t)
	a.wizard.viewStep = 4
	usdc := freeChoiceForCoin(t, a, "USDC")
	a.event(webui.Event{ID: "field/freeReturnAsset", Value: usdc.id()})
	a.event(webui.Event{ID: "field/freeReturnAmount", Value: "3"})
	at := time.Now().Add(-30 * time.Second).UnixMilli()
	a.freeData().quote, _ = json.Marshal(map[string]any{"observedAt": at, "balances": []wizardBalance{{AccountType: "UNIFIED", Coin: "USDC", Available: "9.5"}}})
	backend.handler = func(_ uint64, method string, _, out any) error {
		if method != "exchangeFundingBalances" {
			t.Error("balance read requested another method", method)
		}
		return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "UNIFIED", Coin: "USDC", Available: "5"}}})
	}
	if !a.nextFreeBalanceRefresh() {
		t.Fatal("automatic stablecoin overview did not start")
	}
	ui.apply(t)
	var quote struct{ ObservedAt int64 }
	if json.Unmarshal(a.freeData().quote, &quote) != nil || quote.ObservedAt != at || a.fields["freeReturnAmount"] != "3" {
		t.Fatal("balance read renewed market authority or overwrote the entered draft")
	}
}
