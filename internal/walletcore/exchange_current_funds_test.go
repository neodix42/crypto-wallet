package walletcore

// Inherited-pipe coordinator and public REST fixtures only. Real password,
// durable digest, HMAC and encrypted restart are tested in package signer.
import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

func currentFundsCoordinator(t *testing.T) (*core, *fixtureExchangeAPI, string) {
	t.Helper()
	c, api, adapter, path := fixtureExchangeCoordinator(t)
	adapter.networks = []model.Network{{ID: "ton-mainnet", Family: "ton", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}}}}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{{ID: "exchange-account", Family: "ton", Network: "ton-mainnet", Address: "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ"}}
	writePublicState(t, path, state)
	api.chain.Chain, api.chain.ChainType, api.chain.MinAccuracy = "TON", "TON", "9"
	api.chain.WithdrawMax = "100"
	api.ticker = &bybit.Ticker{Symbol: "GRAMUSDT", LastPrice: "1.176", Bid1Price: "1.175", Ask1Price: "1.177"}
	instrument := bybit.Instrument{Symbol: "GRAMUSDT", BaseCoin: "GRAM", QuoteCoin: "USDT", Status: "Trading"}
	instrument.LotSizeFilter.BasePrecision, instrument.LotSizeFilter.QuotePrecision = "0.01", "0.00001"
	instrument.LotSizeFilter.MinOrderAmt, instrument.LotSizeFilter.MaxMarketOrderQty = "5", "100200"
	instrument.PriceFilter.TickSize = "0.001"
	api.instrument = &instrument
	return c, api, path
}
func currentFundsCall(t *testing.T, c *core, method string, body map[string]any) exchangeResult {
	t.Helper()
	request := map[string]any{"accountId": "exchange-account", "assetId": "ton-mainnet:TON"}
	if method == "exchangeSwap" || method == "exchangeWithdraw" {
		request["provider"] = "bybit"
	}
	for key, value := range body {
		request[key] = value
	}
	out, err := c.handle(message(method, request))
	if err != nil {
		t.Fatal(method, err)
	}
	return out.(exchangeResult)
}
func currentFundsUnknownCoordinator(t *testing.T) (*core, *fixtureExchangeAPI, string, map[string]any) {
	t.Helper()
	c, api, path := currentFundsCoordinator(t)
	api.balances["FUND:GRAM"], api.balances["UNIFIED:GRAM"] = "6", "0"
	api.fail["/v5/asset/transfer/inter-transfer"] = errors.New("synthetic lost response")
	old := currentFundsCall(t, c, "exchangeFundingMove", map[string]any{"amount": "5"})
	if old.State != "UNKNOWN" || api.posts != 1 {
		t.Fatal("unknown original fixture", old)
	}
	if strings.Contains(old.Message, "blocked") {
		t.Fatal("independent response instructs global waiting")
	}
	delete(api.fail, "/v5/asset/transfer/inter-transfer")
	state := readPublicState(t, path)
	original := state["exchangeOps"].([]any)[0].(map[string]any)
	return c, api, path, original
}

func TestCurrentFundsCoordinatorUsesFreshAvailableGRAMWithoutCheckingOlderUnknown(t *testing.T) {
	for _, kind := range []string{"funding", "swap", "return", "withdraw"} {
		t.Run(kind, func(t *testing.T) {
			c, api, path, original := currentFundsUnknownCoordinator(t)
			// The prior five-unit transfer may already have executed. The next
			// separate action is budgeted solely by these current safe balances.
			api.balances["FUND:GRAM"], api.balances["UNIFIED:GRAM"] = "0.99998566", "5"
			method, body := "exchangeFundingMove", map[string]any{"amount": "0.5"}
			switch kind {
			case "swap":
				method, body = "exchangeSwap", map[string]any{"side": "Sell", "amount": "5", "slippage": "0.5", "unifiedOnly": true}
			case "return":
				method, body = "exchangeUnifiedMove", map[string]any{"amount": "5"}
			case "withdraw":
				method, body = "exchangeWithdraw", map[string]any{"amount": "0.5", "existingFunds": true, "fundingOnly": true}
			}
			result := currentFundsCall(t, c, method, body)
			if api.posts != 2 || result.ID == original["id"] || result.Kind == "" {
				t.Fatal("distinct affordable current action blocked or reused original", kind, result)
			}
			state := readPublicState(t, path)
			ops := state["exchangeOps"].([]any)
			if len(ops) != 2 || !reflect.DeepEqual(ops[0], original) {
				t.Fatal("new action changed original immutable operation")
			}
			fresh := ops[1].(map[string]any)
			if fresh["currentFunds"] != true || fresh["id"] != result.ID || fresh["payload"] == original["payload"] {
				t.Fatal("current policy/ID not bound to distinct preparation", fresh)
			}
			methods := state["exchangePreparationMethods"].([]any)
			if !reflect.DeepEqual(methods, []any{"exchangePrepareCurrentFunds", "exchangePrepareCurrentFunds"}) {
				t.Fatal("free mode did not use trusted current-funds preparation", methods)
			}
		})
	}
}

func TestCurrentFundsCoordinatorRefusesFreshInsufficiencyBeforeNewPreparation(t *testing.T) {
	for _, scenario := range []string{"funding-insufficient", "unified-insufficient", "safe-restriction", "balance-unavailable", "permission", "market-minimum", "withdraw-insufficient"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, path, original := currentFundsUnknownCoordinator(t)
			api.balances["FUND:GRAM"], api.balances["UNIFIED:GRAM"] = "0.99998566", "5"
			method, body := "exchangeFundingMove", map[string]any{"amount": "0.5"}
			switch scenario {
			case "funding-insufficient":
				body["amount"] = "1"
			case "unified-insufficient":
				method, body = "exchangeUnifiedMove", map[string]any{"amount": "6"}
			case "safe-restriction":
				api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
					b := custodyTestBalance(kind, coin, "100")
					b.Balance.TransferSafeAmount = "0"
					return b, nil
				}
			case "balance-unavailable":
				api.fail["balance"] = errors.New("synthetic unavailable")
			case "permission":
				api.info.Permissions.Wallet = nil
			case "market-minimum":
				method, body = "exchangeSwap", map[string]any{"side": "Sell", "amount": "5", "slippage": "0.5", "unifiedOnly": true}
				api.instrument.LotSizeFilter.MinOrderAmt = "6"
			case "withdraw-insufficient":
				method, body = "exchangeWithdraw", map[string]any{"amount": "1", "existingFunds": true, "fundingOnly": true}
			}
			body["accountId"], body["assetId"] = "exchange-account", "ton-mainnet:TON"
			if method == "exchangeSwap" || method == "exchangeWithdraw" {
				body["provider"] = "bybit"
			}
			if _, err := c.handle(message(method, body)); err == nil {
				t.Fatal("fresh current-funds check waived", scenario)
			}
			ops := readPublicState(t, path)["exchangeOps"].([]any)
			if api.posts != 1 || len(ops) != 1 || !reflect.DeepEqual(ops[0], original) {
				t.Fatal("invalid fresh request touched original or prepared another debit", scenario)
			}
		})
	}
}

func TestCurrentFundsPreparationMethodIsPrivateAndCannotBeCallerSelected(t *testing.T) {
	c, api, _, path := fixtureExchangeCoordinator(t)
	for _, method := range []string{"exchangePrepareCurrentFunds", "exchangePrepare"} {
		if _, err := c.handle(message(method, map[string]any{"kind": "transfer", "accountId": "exchange-account", "assetId": "ethereum-mainnet:native"})); err == nil {
			t.Fatal("private signer preparation exposed publicly", method)
		}
	}
	for _, method := range []string{"exchangeFundingMove", "exchangeUnifiedMove", "exchangeSwap", "exchangeWithdraw"} {
		if _, err := c.handle(exchangeMessage(method, map[string]any{"currentFunds": true, "amount": "0.1"})); err == nil {
			t.Fatal("caller-supplied current-funds authority accepted", method)
		}
	}
	if api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
		t.Fatal("public private-method probe changed economic state")
	}
}

func TestCurrentFundsDurabilityFailureStopsBeforePOST(t *testing.T) {
	c, api, path, original := currentFundsUnknownCoordinator(t)
	api.balances["FUND:GRAM"] = "0.99998566"
	state := readPublicState(t, path)
	state["reject"] = []string{"exchangePrepareCurrentFunds"}
	writePublicState(t, path, state)
	body := map[string]any{"accountId": "exchange-account", "assetId": "ton-mainnet:TON", "amount": "0.5"}
	if _, err := c.handle(message("exchangeFundingMove", body)); err == nil || api.posts != 1 {
		t.Fatal("undurable current operation reached POST")
	}
	state = readPublicState(t, path)
	raw, _ := json.Marshal(state["exchangeOps"].([]any)[0])
	want, _ := json.Marshal(original)
	if string(raw) != string(want) {
		t.Fatal("prepare failure altered original")
	}
}
