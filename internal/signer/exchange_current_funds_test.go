package signer

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func currentFundsGRAMEngine(t *testing.T) (*Engine, *testUI, model.Account) {
	t.Helper()
	e, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, e, ui, "mainnet")
	account := findAccount(t, e, "ton-mainnet")
	ui.inputs = append(ui.inputs, input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	return e, ui, account
}
func currentFundsGRAMIntent(account model.Account, kind string) ExchangeIntent {
	intent := ExchangeIntent{Kind: kind, AccountID: account.ID, AssetID: "ton-mainnet:TON", FeeCoin: "GRAM", Fee: "0"}
	switch kind {
	case "transfer":
		intent.Transfer = &bybit.TransferRequest{Coin: "GRAM", Amount: "0.5", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
	case "return":
		intent.Kind = "transfer"
		intent.Transfer = &bybit.TransferRequest{Coin: "GRAM", Amount: "5", FromAccountType: "UNIFIED", ToAccountType: "FUND"}
	case "swap":
		intent.Order = &bybit.SpotOrder{Category: "spot", Symbol: "GRAMUSDT", Side: "Sell", OrderType: "Market", Qty: "5", MarketUnit: "baseCoin", IsLeverage: 0, SlippageToleranceType: "Percent", SlippageTolerance: "0.5"}
		intent.LastPrice, intent.Fee, intent.FeeCoin = "1.176", "0.00588", "USDT"
	case "withdraw":
		intent.Withdrawal = &bybit.WithdrawalRequest{Coin: "GRAM", Chain: "TON", Address: account.Address, Amount: "0.5", ForceChain: 1, AccountType: "FUND", FeeType: 1}
		intent.ChainType, intent.Fee = "TON", "0.001"
	}
	return intent
}
func currentFundsOldUnknown(t *testing.T, e *Engine, ui *testUI, account model.Account, state string) vault.ExchangeOperation {
	t.Helper()
	intent := currentFundsGRAMIntent(account, "transfer")
	intent.Transfer.Amount = "5"
	old, err := e.PrepareExchange(intent)
	if err != nil {
		t.Fatal(err)
	}
	old = authorizeExchange(t, e, ui, old)
	if _, err = e.ExchangeAuth(exchangeRequest(old)); err != nil {
		t.Fatal(err)
	}
	if state != "SUBMITTING" {
		remote := ""
		if state == "PENDING" {
			remote = exchangeUUID(old.ID)
		}
		if err = e.UpdateExchange(old.ID, state, remote); err != nil {
			t.Fatal(err)
		}
	}
	old, err = e.ExchangeOperation(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	return old
}

func TestCurrentFundsAllowsDistinctApprovedGRAMActionsWithoutWaitingForOldOutcome(t *testing.T) {
	for _, state := range []string{"UNKNOWN", "PENDING", "SUBMITTING"} {
		t.Run(state, func(t *testing.T) {
			e, ui, account := currentFundsGRAMEngine(t)
			old := currentFundsOldUnknown(t, e, ui, account, state)
			for _, kind := range []string{"transfer", "swap", "return", "withdraw"} {
				intent := currentFundsGRAMIntent(account, kind)
				if _, err := e.PrepareExchange(intent); err == nil {
					t.Fatal("legacy global uncertainty policy changed", kind)
				}
				op, err := e.PrepareExchangeCurrentFunds(intent)
				if err != nil || !op.CurrentFunds || op.ID == old.ID || op.Digest != vault.ExchangeDigest(op) {
					t.Fatal("separate current-funds request blocked or failed to bind policy", kind, err)
				}
				if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
					t.Fatal("preparation authorized a new debit without password", kind)
				}
				op = authorizeExchange(t, e, ui, op)
				if !strings.Contains(ui.summaries[len(ui.summaries)-1], "Only this new operation is authorized") {
					t.Fatal("current request approval retained old wait policy")
				}
				request := exchangeRequest(op)
				auth, err := e.ExchangeAuth(request)
				if err != nil {
					t.Fatal("new exact request refused", kind, err)
				}
				assertExchangeHMAC(t, request, auth)
				if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
					t.Fatal("new request HMAC replayed", kind)
				}
				if err = e.UpdateExchange(op.ID, "UNKNOWN", ""); err != nil {
					t.Fatal(err)
				}
				unchanged, err := e.ExchangeOperation(old.ID)
				if err != nil || !reflect.DeepEqual(unchanged, old) {
					t.Fatal("new request rewrote old ID, payload, digest or state", kind, err)
				}
				if _, err = e.AuthorizeExchange(old.ID); err == nil {
					t.Fatal("old uncertain request reauthorized")
				}
				if _, err = e.ExchangeAuth(exchangeRequest(old)); err == nil {
					t.Fatal("new consent replayed old request")
				}
				if err = e.CancelExchange(old.ID); err == nil {
					t.Fatal("new action cancelled uncertain original")
				}
			}
			before := append([]vault.ExchangeOperation(nil), e.data.Exchange.Operations...)
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, e.data.Exchange.Operations) {
				t.Fatal("encrypted restart changed immutable identities, policy or unresolved outcomes")
			}
		})
	}
}

func TestCurrentFundsSerializesOnlyLiveUnconsumedAuthorization(t *testing.T) {
	e, ui, account := currentFundsGRAMEngine(t)
	first, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "transfer"))
	if err != nil {
		t.Fatal(err)
	}
	first = authorizeExchange(t, e, ui, first)
	if _, err = e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "swap")); err == nil {
		t.Fatal("parallel live authorization capability permitted")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(first)); err != nil {
		t.Fatal(err)
	}
	second, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "swap"))
	if err != nil {
		t.Fatal("spent capability incorrectly kept global wait", err)
	}
	if err = e.CancelExchange(second.ID); err != nil {
		t.Fatal(err)
	}
	// A persisted AUTHORIZED marker without its one-use in-memory grant cannot
	// sign and must not become an indefinite waiting requirement.
	e.data.Exchange.Operations[0].State = "AUTHORIZED"
	e.exchangeAuthorizations = nil
	third, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "return"))
	if err != nil {
		t.Fatal("unusable historic authorization blocked independent action", err)
	}
	third = authorizeExchange(t, e, ui, third)
	if _, err = e.ExchangeAuth(exchangeRequest(first)); err == nil {
		t.Fatal("historic authorization recovered a capability")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(third)); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentFundsStillRequiresExactSessionPolicyAndCurrentGeneration(t *testing.T) {
	for _, scenario := range []string{"cancel", "generation-confirm", "recovery-confirm", "credential", "policy-digest", "payload-digest"} {
		t.Run(scenario, func(t *testing.T) {
			e, ui, account := currentFundsGRAMEngine(t)
			old := currentFundsOldUnknown(t, e, ui, account, "UNKNOWN")
			op, err := e.PrepareExchangeCurrentFunds(currentFundsGRAMIntent(account, "transfer"))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "cancel":
				ui.approve = false
			case "generation-confirm":
				e.UI = &exchangeHookUI{testUI: ui, confirmHook: func() { e.data.Generation++ }}
			case "recovery-confirm":
				e.UI = &exchangeHookUI{testUI: ui, confirmHook: func() { e.data.RecoveryConfirmed = false }}
			case "credential":
				e.UI = &exchangeHookUI{testUI: ui, confirmHook: func() { e.data.Exchange.Credentials.APIKey = []byte("DIFFERENT_PUBLIC_FIXTURE_KEY") }}
			default:
				op = authorizeExchange(t, e, ui, op)
				stored, _ := e.exchangeOperation(op.ID)
				if scenario == "policy-digest" {
					stored.CurrentFunds = false
				} else {
					var request bybit.TransferRequest
					if json.Unmarshal([]byte(stored.Payload), &request) != nil {
						t.Fatal("payload")
					}
					request.Amount = "0.6"
					raw, _ := json.Marshal(request)
					stored.Payload = string(raw)
				}
				stored.Digest = vault.ExchangeDigest(*stored)
				if _, err = e.ExchangeAuth(exchangeRequest(*stored)); err == nil {
					t.Fatal("old approval authorized mutated policy or amount")
				}
				return
			}
			if result, err := e.AuthorizeExchange(op.ID); err == nil || result.ID != "" {
				t.Fatal("invalid consent released a current-funds operation", scenario)
			}
			if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
				t.Fatal("invalid consent signed", scenario)
			}
			if scenario == "recovery-confirm" {
				e.data.RecoveryConfirmed = true // Inspect the unchanged original after testing the closed gate.
			}
			unchanged, err := e.ExchangeOperation(old.ID)
			if err != nil || !reflect.DeepEqual(old, unchanged) {
				t.Fatal("failed new consent changed original", err)
			}
		})
	}
}

func TestCurrentFundsPreparationRejectsLegacyAuthorityAndUnreviewedScope(t *testing.T) {
	for _, scenario := range []string{"wizard", "foreign-account", "foreign-asset", "transfer-coin", "transfer-direction", "borrow", "limit", "withdraw-account", "withdraw-fee-mode", "provider"} {
		t.Run(scenario, func(t *testing.T) {
			e, ui, account := currentFundsGRAMEngine(t)
			old := currentFundsOldUnknown(t, e, ui, account, "UNKNOWN")
			intent := currentFundsGRAMIntent(account, "transfer")
			switch scenario {
			case "wizard":
				intent.WizardID = strings.Repeat("a", 32)
			case "foreign-account":
				intent.AccountID = "foreign"
			case "foreign-asset":
				intent.AssetID = "ethereum-mainnet:native"
			case "transfer-coin":
				intent.Transfer.Coin = "USDT"
			case "transfer-direction":
				intent.Transfer.ToAccountType = "CONTRACT"
			case "borrow":
				intent = currentFundsGRAMIntent(account, "swap")
				intent.Order.IsLeverage = 1
			case "limit":
				intent = currentFundsGRAMIntent(account, "swap")
				intent.Order.OrderType = "Limit"
			case "withdraw-account":
				intent = currentFundsGRAMIntent(account, "withdraw")
				intent.Withdrawal.AccountType = "UTA"
			case "withdraw-fee-mode":
				intent = currentFundsGRAMIntent(account, "withdraw")
				intent.Withdrawal.FeeType = 0
			case "provider":
				e.data.Exchange.SelectedProvider = "binance"
				e.data.Exchange.ProviderCredentials = []*vault.ExchangeCredentials{{Provider: "binance", Endpoint: "https://api.binance.com", APIKey: []byte(exchangeTestKey), Secret: []byte(exchangeTestSecret)}}
			}
			if _, err := e.PrepareExchangeCurrentFunds(intent); err == nil || len(e.data.Exchange.Operations) != 1 {
				t.Fatal("unreviewed independent authority prepared", scenario, err)
			}
			if !reflect.DeepEqual(e.data.Exchange.Operations[0], old) {
				t.Fatal("refusal changed original")
			}
		})
	}
}

func TestCurrentFundsPolicyChangesDigestButAbsentLegacyFieldDoesNot(t *testing.T) {
	legacy := vault.ExchangeOperation{ID: strings.Repeat("a", 32), Kind: "transfer", CreatedAt: time.Now().Unix()}
	raw, _ := json.Marshal(legacy)
	if strings.Contains(string(raw), "currentFunds") {
		t.Fatal("legacy serialization gained a false flag")
	}
	oldDigest := vault.ExchangeDigest(legacy)
	legacy.CurrentFunds = true
	if vault.ExchangeDigest(legacy) == oldDigest {
		t.Fatal("independent policy was not digest-bound")
	}
}
