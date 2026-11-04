package walletcore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

type acknowledgementAPI struct {
	*fixtureExchangeAPI
	scenario string
	ready    bool
}

func (api *acknowledgementAPI) visibilityError() error {
	if !api.ready && api.scenario == "temporary" {
		return &bybit.APIError{Code: 10006}
	}
	if !api.ready && api.scenario == "malformed" {
		return bybit.ErrInvalidResponse
	}
	return nil
}
func (api *acknowledgementAPI) FindOrder(ctx context.Context, symbol, link string) (*bybit.Order, error) {
	if err := api.visibilityError(); err != nil {
		return nil, err
	}
	if !api.ready && api.scenario == "missing" {
		return nil, nil
	}
	row, err := api.fixtureExchangeAPI.FindOrder(ctx, symbol, link)
	if row != nil && !api.ready && api.scenario == "foreign" {
		row.OrderID = "different-provider-order"
	}
	return row, err
}
func (api *acknowledgementAPI) TransferStatus(ctx context.Context, id string) (*bybit.TransferRecord, error) {
	if err := api.visibilityError(); err != nil {
		return nil, err
	}
	if !api.ready && api.scenario == "missing" {
		return nil, nil
	}
	row, err := api.fixtureExchangeAPI.TransferStatus(ctx, id)
	if row != nil && !api.ready && api.scenario == "foreign" {
		row.Amount = "200"
	}
	return row, err
}
func (api *acknowledgementAPI) WithdrawalRecords(ctx context.Context, coin, id, cursor string) (bybit.WithdrawalPage, error) {
	if err := api.visibilityError(); err != nil {
		return bybit.WithdrawalPage{}, err
	}
	if !api.ready && api.scenario == "missing" {
		return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{}}, nil
	}
	page, err := api.fixtureExchangeAPI.WithdrawalRecords(ctx, coin, id, cursor)
	if len(page.Rows) > 0 && !api.ready && api.scenario == "foreign" {
		page.Rows[0].ToAddress = "different-owned-effect"
	}
	return page, err
}

func TestAcknowledgedBybitActionsStayPendingUntilExactProofWithoutAnotherPost(t *testing.T) {
	for _, kind := range []string{"funding", "return", "swap", "withdraw"} {
		for _, scenario := range []string{"missing", "temporary", "malformed", "foreign"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				c, base, _, path := fixtureExchangeCoordinator(t)
				api := &acknowledgementAPI{fixtureExchangeAPI: base, scenario: scenario}
				factory := c.newExchange
				c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
					_, err := factory(endpoint, authorize)
					return api, err
				}
				method, body := "exchangeFundingMove", map[string]any{"amount": "0.1"}
				switch kind {
				case "return":
					method = "exchangeUnifiedMove"
				case "swap":
					method, body = "exchangeSwap", map[string]any{"amount": "0.1", "side": "Sell", "slippage": "0.5", "unifiedOnly": true}
				case "withdraw":
					method, body = "exchangeWithdraw", map[string]any{"amount": "0.1", "existingFunds": true, "fundingOnly": true}
				}
				first := exchangeResultCall(t, c, method, body)
				if first.State != "PENDING" || first.RemoteID == "" || first.ID == "" || api.posts != 1 || first.FilledBase != "" || first.FilledQuote != "" {
					t.Fatal("accepted asynchronous request became unknown/failed/completed from an unverified follow-up", first, api.posts)
				}
				if scenario != "missing" && first.Code == "" {
					t.Fatal("malformed, mismatched or unavailable follow-up silently lost its fixed diagnostic", first)
				}
				before := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
				if before["state"] != "PENDING" || before["externalId"] != first.RemoteID {
					t.Fatal("known acknowledgement was erased from durable original state", before)
				}
				api.ready = true
				out, err := c.handle(message("exchangeReconcile", map[string]string{"id": first.ID, "provider": "bybit"}))
				if err != nil {
					t.Fatal(err)
				}
				final := out.(exchangeResult)
				want := "CONFIRMED"
				if kind == "withdraw" {
					want = "EXCHANGE_COMPLETED"
				}
				after := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
				if final.State != want || final.ID != first.ID || final.RemoteID != first.RemoteID || api.posts != 1 || before["payload"] != after["payload"] || before["accountId"] != after["accountId"] || before["assetId"] != after["assetId"] {
					t.Fatal("later exact proof replayed or replaced the original economic action", first, final, after)
				}
			})
		}
	}
}

func TestUnacknowledgedBybitSubmissionRemainsUnknownWithoutAStatusProof(t *testing.T) {
	c, base, _, path := fixtureExchangeCoordinator(t)
	api := &acknowledgementAPI{fixtureExchangeAPI: base, scenario: "missing"}
	factory := c.newExchange
	c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
		_, err := factory(endpoint, authorize)
		return api, err
	}
	api.fail["/v5/order/create"] = errors.New("synthetic missing acknowledgement")
	first := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"amount": "0.1", "side": "Sell", "slippage": "0.5", "unifiedOnly": true})
	if first.State != "UNKNOWN" || first.RemoteID != "" {
		t.Fatal("a lost acknowledgement was invented", first)
	}
	out, err := c.handle(message("exchangeReconcile", map[string]string{"id": first.ID}))
	if err != nil || out.(exchangeResult).State != "UNKNOWN" || out.(exchangeResult).RemoteID != "" || api.posts != 1 {
		t.Fatal("missing status supplied acceptance or retried an unacknowledged original", out, err)
	}
	if op := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any); op["state"] != "UNKNOWN" || op["externalId"] != "" {
		t.Fatal("unknown outcome was discarded", op)
	}
}

func TestLegacyUnknownWithAcknowledgedIDRecoversToPendingWithoutChangingIdentity(t *testing.T) {
	c, api, _, path := fixtureExchangeCoordinator(t)
	request := fixtureRemoteOrder()
	op := fixtureRemoteOperation("swap", request)
	op.ExternalID = "remote-order-1"
	storeFixtureOperation(t, path, op)
	api.findOrder = func(string, string) (*bybit.Order, error) { return nil, nil }
	result, err := c.reconcileExchangeAt(0, api, op)
	if err != nil || result.State != "PENDING" || result.ID != op.ID || result.RemoteID != op.ExternalID || api.posts != 0 || result.FilledBase != "" {
		t.Fatal("historical UNKNOWN state erased an already acknowledged request", result, err)
	}
	after := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
	if after["payload"] != op.Payload || after["externalId"] != op.ExternalID || after["state"] != "PENDING" {
		t.Fatal("read-only status repair replaced the original", after)
	}
}

func TestReportedGRAMBuyKeepsQuoteBudgetAndVerifiesGrossFillsAndBaseCoinFee(t *testing.T) {
	for _, quantity := range []string{"5", "4.25"} {
		t.Run(quantity, func(t *testing.T) {
			c, api, path := currentFundsCoordinator(t)
			api.balances["UNIFIED:USDT"] = "5"
			api.findOrder = func(symbol, link string) (*bybit.Order, error) {
				return &bybit.Order{OrderID: "remote-order-1", OrderLinkID: link, Symbol: symbol, Side: "Buy", OrderType: "Market", Qty: quantity, MarketUnit: "quoteCoin", IsLeverage: "0", OrderStatus: "Filled", CumExecQty: "4.25", CumExecValue: "5", CumExecFee: ""}, nil
			}
			api.executions = func(symbol, link, _ string) (bybit.ExecutionPage, error) {
				return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{{OrderID: "remote-order-1", OrderLinkID: link, Symbol: symbol, Side: "Buy", OrderType: "Market", OrderQty: "5", ExecID: "public-gram-fill", ExecQty: "4.25", ExecValue: "5", ExecPrice: "1.176470588", ExecFee: "0.00425", FeeCurrency: "GRAM", ExtraFees: json.RawMessage(`""`)}}}, nil
			}
			result := currentFundsCall(t, c, "exchangeSwap", map[string]any{"side": "Buy", "amount": "5", "slippage": "0.5", "unifiedOnly": true})
			if quantity == "4.25" {
				if result.State != "PENDING" || result.Code != "EXCHANGE_READ_ORDER_STATUS__PROVIDER_INVALID_RESPONSE" || result.FilledBase != "" {
					t.Fatal("returned base quantity was silently reinterpreted as the approved quote budget", result)
				}
				return
			}
			if result.State != "CONFIRMED" || result.FilledBase != "4.25" || result.FilledQuote != "5" || !reflect.DeepEqual(result.Fees, map[string]string{"GRAM": "0.00425"}) || api.order.Qty != "5" || api.order.MarketUnit != "quoteCoin" || api.posts != 1 || independentExchangeOperationCount(t, path) != 1 {
				t.Fatal("verified GRAM gross fill, quote spending budget or base-currency fee changed", result, api.order)
			}
			if strings.Contains(result.Message, "balance") {
				t.Fatal("trade completion was inferred from a balance")
			}
		})
	}
}
