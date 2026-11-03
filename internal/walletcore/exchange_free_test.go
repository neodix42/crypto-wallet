package walletcore

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func independentExchangeOperationCount(t *testing.T, path string) int {
	t.Helper()
	raw := readPublicState(t, path)["exchangeOps"]
	if raw == nil {
		return 0
	}
	ops, ok := raw.([]any)
	if !ok {
		t.Fatal("invalid independent operation fixture")
	}
	return len(ops)
}

func TestExchangeIndependentFundingMovesAvailableCustodyWithoutChangingOldWizard(t *testing.T) {
	c, api, adapter, path, w := wizardCoordinator(t)
	w = depositWizard(t, c, api, w)
	before := loadWizard(t, c)
	api.depositPage = func(string, string) (bybit.DepositPage, error) {
		t.Fatal("independent Funding move tried to infer a previous deposit's credit")
		return bybit.DepositPage{}, nil
	}
	api.balances["FUND:ETH"] = "5.99998566"
	api.balances["UNIFIED:ETH"] = "0"
	result := exchangeResultCall(t, c, "exchangeFundingMove", map[string]any{"amount": "5"})
	if result.Kind != "transfer" || result.State != "CONFIRMED" || api.posts != 1 || api.transfer.Amount != "5" || api.transfer.FromAccountType != "FUND" || api.transfer.ToAccountType != "UNIFIED" || api.transfer.Coin != "ETH" {
		t.Fatal("independent move changed exact account/coin/amount", result, api.transfer)
	}
	if loadWizard(t, c) != before || adapter.prepares != 1 || adapter.submits != 1 {
		t.Fatal("independent transfer overwrote/replayed the original on-chain workflow")
	}
	state := readPublicState(t, path)
	ops := state["exchangeOps"].([]any)
	if len(ops) != 1 || ops[0].(map[string]any)["id"] != result.ID {
		t.Fatal("independent move lost its own durable identity")
	}
}

func TestExchangeIndependentFundingRefusesUnsafeAmountsPermissionsAndForeignInputs(t *testing.T) {
	for _, scenario := range []string{"insufficient", "safe-restriction", "readonly", "permission", "classic", "zero", "precision", "foreign", "extra-direction"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			api.balances["FUND:ETH"] = "5.99998566"
			body := map[string]any{"amount": "5"}
			switch scenario {
			case "insufficient":
				body["amount"] = "6"
			case "safe-restriction":
				api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
					var balance bybit.Balance
					balance.AccountType = kind
					balance.Balance.Coin = coin
					balance.Balance.WalletBalance, balance.Balance.TransferBalance, balance.Balance.TransferSafeAmount = "6", "6", "0"
					return balance, nil
				}
			case "readonly":
				api.info.ReadOnly = 1
			case "permission":
				api.info.Permissions.Wallet = nil
			case "classic":
				api.info.UTA = 0
			case "zero":
				body["amount"] = "0"
			case "precision":
				body["amount"] = "1.0000000000000000001"
			case "foreign":
				body["accountId"] = "not-owned"
			case "extra-direction":
				body["toAccountType"] = "CONTRACT"
			}
			if _, err := c.handle(exchangeMessage("exchangeFundingMove", body)); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("unaffordable or unauthorized independent move reached preparation", err)
			}
		})
	}
}

func TestExchangeUnifiedOnlySwapUsesExistingUnifiedAndNeverImplicitFunding(t *testing.T) {
	for _, quote := range []string{"USDT", "USDC"} {
		for _, side := range []string{"Sell", "Buy"} {
			for _, funded := range []bool{true, false} {
				t.Run(quote+"/"+side+map[bool]string{true: "/funded", false: "/unfunded"}[funded], func(t *testing.T) {
					c, api, _, path, _ := wizardCoordinator(t)
					if quote == "USDC" {
						configureUSDCFixture(api.fixtureExchangeAPI)
						api.listed["ETHUSDC"] = true
					}
					before := loadWizard(t, c)
					coin, value := "ETH", "0.1"
					if side == "Buy" {
						coin, value = quote, "200"
					}
					api.balances["FUND:"+coin] = "1000"
					api.balances["UNIFIED:"+coin] = "0"
					if funded {
						api.balances["UNIFIED:"+coin] = "1000"
					}
					out, err := c.handle(exchangeMessage("exchangeSwap", map[string]any{"side": side, "amount": value, "slippage": "0.5", "quoteCoin": quote, "unifiedOnly": true}))
					if !funded {
						if publicRequestErrorCode(err) != "EXCHANGE_UNIFIED_INSUFFICIENT" || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
							t.Fatal("Swap implicitly moved Funding assets or prepared an unfunded order", err, api.posts)
						}
					} else if err != nil || out.(exchangeResult).Kind != "swap" || out.(exchangeResult).State != "CONFIRMED" || api.posts != 1 || api.transfer.TransferID != "" || api.order.Qty != value {
						t.Fatal("existing Unified trade did not execute as its own exact approved order", out, err)
					}
					if loadWizard(t, c) != before {
						t.Fatal("independent swap changed the older workflow")
					}
				})
			}
		}
	}
}

func TestExchangeFreeModesRejectAmbiguousBooleansAndWizardAuthorityMixing(t *testing.T) {
	for _, value := range []any{nil, "true", "false", 0, 1, []any{}, map[string]any{}} {
		for _, method := range []string{"exchangeSwap", "exchangeWithdraw", "exchangeWithdrawOptions"} {
			c, api, _, path := fixtureExchangeCoordinator(t)
			flag := "existingFunds"
			body := map[string]any{"amount": "0.1"}
			if method == "exchangeSwap" {
				flag = "unifiedOnly"
				body["side"], body["slippage"] = "Sell", "0.5"
			}
			var messageBody any = body
			if method == "exchangeWithdrawOptions" {
				body = map[string]any{"coin": "ETH", "sourceAccountId": "exchange-account"}
				messageBody = body
			}
			body[flag] = value
			request := exchangeMessage(method, body)
			if method == "exchangeWithdrawOptions" {
				request = message(method, messageBody)
			}
			if _, err := c.handle(request); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("nonboolean mode selected an economic policy", method, value, err)
			}
		}
	}
	for _, method := range []string{"exchangeSwap", "exchangeWithdraw"} {
		c, api, _, _ := fixtureExchangeCoordinator(t)
		body := map[string]any{"wizardId": "existing", "amount": "0.1", "existingFunds": true}
		if method == "exchangeSwap" {
			delete(body, "existingFunds")
			body["unifiedOnly"], body["side"], body["slippage"] = true, "Sell", "0.5"
		}
		if _, err := c.handle(exchangeMessage(method, body)); err == nil || api.posts != 0 {
			t.Fatal("independent funds mode acquired an old wizard's authority", method, err)
		}
	}
	for _, raw := range []string{
		`{"accountId":"exchange-account","assetId":"ethereum-mainnet:native","amount":"0.1","side":"Sell","slippage":"0.5","unifiedOnly":true,"UnifiedOnly":false}`,
		`{"accountId":"exchange-account","assetId":"ethereum-mainnet:native","amount":"0.1","side":"Sell","slippage":"0.5","UnifiedOnly":true}`,
	} {
		c, api, _, _ := fixtureExchangeCoordinator(t)
		request := message("exchangeSwap", map[string]any{})
		request.Body = []byte(raw)
		if _, err := c.handle(request); err == nil || api.posts != 0 {
			t.Fatal("case-shadowed funds mode selected another policy", err)
		}
	}
}

func TestExchangeIndependentWithdrawalReadsOwnedRoutesWithoutPreviousSwapOrMarket(t *testing.T) {
	c, api, adapter, _, _ := wizardCoordinator(t)
	before := loadWizard(t, c)
	api.fail["instrument"], api.fail["ticker"], api.fail["fee"] = errors.New("unrelated spot read denied"), errors.New("unrelated spot read denied"), errors.New("unrelated spot read denied")
	out, err := c.handle(message("exchangeWithdrawOptions", map[string]any{"coin": "ETH", "existingFunds": true, "sourceAccountId": "exchange-account"}))
	if err != nil || len(out.([]exchangeWithdrawalChoice)) != 1 || out.([]exchangeWithdrawalChoice)[0].AccountID != "exchange-account" {
		t.Fatal("owned withdrawal options required a past swap", out, err)
	}
	result := exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"amount": "0.1", "existingFunds": true})
	if result.Kind != "withdraw" || result.State != "EXCHANGE_COMPLETED" || api.posts != 1 || api.withdrawal.Address != "0x1111111111111111111111111111111111111111" || api.withdrawal.AccountType != "FUND" || adapter.prepares != 0 || loadWizard(t, c) != before {
		t.Fatal("independent withdrawal changed destination, replayed a deposit or mutated old workflow", result, api.withdrawal)
	}
	for _, body := range []map[string]any{{"coin": "ETH", "existingFunds": true}, {"coin": "ETH", "existingFunds": true, "sourceAccountId": "not-owned"}, {"coin": "ETH", "sourceAccountId": "exchange-account"}} {
		if _, err := c.handle(message("exchangeWithdrawOptions", body)); err == nil {
			t.Fatal("unbound or foreign account selected withdrawal routes", body)
		}
	}
}

func TestExchangeIndependentDepositsHaveDistinctOriginalIDsAndRetainOlderWorkflow(t *testing.T) {
	c, api, adapter, path, w := wizardCoordinator(t)
	w = depositWizard(t, c, api, w)
	before := loadWizard(t, c)
	first := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": "0.2", "expectedAddress": api.deposit.Address, "expectedMemo": api.deposit.Tag})
	second := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": "0.3", "expectedAddress": api.deposit.Address, "expectedMemo": api.deposit.Tag})
	if first.ID == second.ID || first.ID == w.DepositOperation || second.ID == w.DepositOperation || first.Kind != "deposit" || second.Kind != "deposit" || adapter.prepares != 3 || adapter.submits != 3 || loadWizard(t, c) != before {
		t.Fatal("explicit additional deposits reused an original identity or replaced old metadata")
	}
	ops := readPublicState(t, path)["ops"].([]any)
	if len(ops) != 3 {
		t.Fatal("independent deposits lost durable original records", ops)
	}
	for i, expected := range []string{w.DepositOperation, first.ID, second.ID} {
		if ops[i].(map[string]any)["plan"].(map[string]any)["id"] != expected {
			t.Fatal("an old wallet operation was overwritten")
		}
	}
}

func TestExchangeIndependentReadRefreshNeverPosts(t *testing.T) {
	c, api, _, path, _ := wizardCoordinator(t)
	before := readPublicState(t, path)
	for _, request := range []struct {
		method string
		body   any
	}{{"exchangeFundingBalances", map[string]string{"accountId": "exchange-account", "assetId": "ethereum-mainnet:native"}}, {"exchangeSwapOptions", map[string]string{"accountId": "exchange-account", "assetId": "ethereum-mainnet:native"}}, {"exchangeWithdrawOptions", map[string]any{"coin": "ETH", "existingFunds": true, "sourceAccountId": "exchange-account"}}} {
		if _, err := c.handle(message(request.method, request.body)); err != nil {
			t.Fatal(err)
		}
	}
	after := readPublicState(t, path)
	if api.posts != 0 || !reflect.DeepEqual(before["wizard"], after["wizard"]) || independentExchangeOperationCount(t, path) != 0 {
		t.Fatal("read-only independent step navigation created economic authority")
	}
}

func TestIndependentDepositRefusesEveryNonterminalAccountPaymentAcrossAssets(t *testing.T) {
	for _, state := range []string{"PREPARED", "AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "REORGED"} {
		t.Run(state, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			account, err := c.account("exchange-account")
			if err != nil {
				t.Fatal(err)
			}
			plan := model.Plan{ID: "original-token-payment", Account: account, Asset: "ethereum-mainnet:usdt", Recipient: "0x2222222222222222222222222222222222222222", Amount: "100000", Nonce: "42", MaxFee: "1"}
			operation := journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: state}
			if state != "PREPARED" && state != "AWAITING_CONFIRMATION" {
				operation.Signed = &model.Signed{Identity: "public-original-signature-identity", Bytes: []byte("public-original-artifact")}
			}
			fixture := readPublicState(t, path)
			fixture["ops"] = []journal.Operation{operation}
			writePublicState(t, path, fixture)
			before, _ := json.Marshal(readPublicState(t, path)["ops"])
			_, err = c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1", "expectedAddress": api.deposit.Address, "expectedMemo": api.deposit.Tag}))
			after, _ := json.Marshal(readPublicState(t, path)["ops"])
			if publicRequestErrorCode(err) != "PAYMENT_PENDING" || string(before) != string(after) || adapter.prepares != 0 || adapter.submits != 0 || adapter.reconciles != 0 || api.posts != 0 {
				t.Fatal("a new exchange deposit replaced, signed, reconciled or ignored an unresolved original account payment", err)
			}
		})
	}
}

func TestPublicCancelOnlyTargetsOriginalUnsignedCurrentCredentialOperation(t *testing.T) {
	for _, scenario := range []string{"PREPARED", "AUTHORIZED", "SUBMITTING", "PENDING", "UNKNOWN", "CONFIRMED", "foreign-key", "foreign-provider", "bad-body"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path, _ := wizardCoordinator(t)
			before := loadWizard(t, c)
			op := map[string]any{"id": "11111111111111111111111111111111", "kind": "transfer", "endpoint": "https://api.bybit.com", "credentialId": "fixture-credential", "accountId": "exchange-account", "assetId": "ethereum-mainnet:native", "payload": `{}`, "state": "PREPARED"}
			if scenario == "foreign-key" {
				op["credentialId"] = "not-the-connected-key"
			} else if scenario != "foreign-provider" && scenario != "bad-body" {
				op["state"] = scenario
			}
			fixture := readPublicState(t, path)
			fixture["exchangeOps"] = []any{op}
			writePublicState(t, path, fixture)
			body := map[string]any{"id": op["id"], "provider": "bybit"}
			if scenario == "foreign-provider" {
				body["provider"] = "binance"
			}
			if scenario == "bad-body" {
				body["amount"] = "1"
			}
			_, err := c.handle(message("exchangeCancel", body))
			want := scenario == "PREPARED" || scenario == "AUTHORIZED"
			if (err == nil) != want || api.posts != 0 || loadWizard(t, c) != before {
				t.Fatal("cancel changed provider/credential, ignored original state or moved funds", err)
			}
			fixture = readPublicState(t, path)
			stored := fixture["exchangeOps"].([]any)[0].(map[string]any)
			if want && stored["state"] != "CANCELLED" || !want && stored["state"] != op["state"] {
				t.Fatal("cancel changed an ineligible original operation")
			}
			confirmedCalls := 0
			for _, call := range fixture["calls"].([]any) {
				if call == "exchangeCancelConfirmed" {
					confirmedCalls++
				}
				if call == "exchangeCancel" || call == "exchangeAuthorize" || call == "exchangeAuth" {
					t.Fatal("public cancellation bypassed confirmation or acquired payment authority")
				}
			}
			if confirmedCalls != map[bool]int{true: 1, false: 0}[want] {
				t.Fatal("public cancel did not use the exact confirmed child command")
			}
		})
	}
}
