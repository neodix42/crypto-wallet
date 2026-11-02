package walletcore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

// Economic execution is the existing inherited-pipe coordinator fixture, not
// live Bybit acceptance. Every unrelated trading read is counted and rejected.
type depositOnlyExchange struct {
	*fixtureExchangeAPI
	tradingReads int
}

type depositFeeFailure struct {
	model.Adapter
	failure error
}

func (a depositFeeFailure) Prepare(context.Context, model.Account, model.Transfer) (model.Plan, error) {
	return model.Plan{}, a.failure
}

func TestExchangeDepositFeeFailuresPrecedeDurablePreparation(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&evm.InsufficientFeeBalanceError{FeeAsset: "PRIVATE_CANARY", MaxFee: "PRIVATE_CANARY"}, "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"},
		{&ton.InsufficientNativeFeeBalanceError{MaxFee: "PRIVATE_CANARY", StorageReserve: "PRIVATE_CANARY"}, "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"},
		{context.DeadlineExceeded, "NETWORK_TIMEOUT"},
		{errors.New("PRIVATE_CANARY"), "PAYMENT_QUOTE_UNAVAILABLE"},
	} {
		c, api, adapter, path := fixtureExchangeCoordinator(t)
		c.adapters[0] = depositFeeFailure{Adapter: adapter, failure: tc.err}
		_, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1"}))
		if publicRequestErrorCode(err) != "EXCHANGE_READ_WALLET_FEE__"+tc.code || api.posts != 0 || adapter.submits != 0 || len(readPublicState(t, path)["ops"].([]any)) != 0 {
			t.Fatal("fee failure lost reason or prepared a durable payment", err)
		}
	}
}

func (a *depositOnlyExchange) AccountBalance(context.Context, string, string) (bybit.Balance, error) {
	a.tradingReads++
	return bybit.Balance{}, &bybit.APIError{Code: 10005}
}
func (a *depositOnlyExchange) SpotInstrument(context.Context, string) (bybit.Instrument, error) {
	a.tradingReads++
	return bybit.Instrument{}, &bybit.APIError{Code: 10005}
}
func (a *depositOnlyExchange) SpotTicker(context.Context, string) (bybit.Ticker, error) {
	a.tradingReads++
	return bybit.Ticker{}, &bybit.APIError{Code: 10005}
}
func (a *depositOnlyExchange) SpotFeeRate(context.Context, string) (bybit.FeeRate, error) {
	a.tradingReads++
	return bybit.FeeRate{}, &bybit.APIError{Code: 10005}
}

func TestExchangeDepositDoesNotRequireBalancesSpotPermissionOrTradingWhitelist(t *testing.T) {
	for _, network := range []string{"ethereum-mainnet", "ton-mainnet"} {
		t.Run(network, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			api.info.ReadOnly, api.info.UTA = 1, 0
			api.info.Permissions.Spot, api.info.Permissions.Wallet = nil, nil
			guard := &depositOnlyExchange{fixtureExchangeAPI: api}
			factory := c.newExchange
			c.newExchange = func(endpoint string, auth bybit.Authorizer) (exchangeAPI, error) {
				_, err := factory(endpoint, auth)
				return guard, err
			}
			assetID, value, atomic := "ethereum-mainnet:native", "0.1", "100000000000000000"
			if network == "ton-mainnet" {
				state := readPublicState(t, path)
				state["accounts"] = []model.Account{{ID: "exchange-account", Network: network, Family: "ton", Address: "EQpublicSyntheticOwnedTON"}}
				writePublicState(t, path, state)
				adapter.networks = []model.Network{{ID: network, Family: "ton", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}}}}
				api.chain.Chain, api.chain.ChainType = "TON", "TON"
				api.deposit = bybit.Deposit{Chain: "TON", ChainType: "TON", Address: "EQpublicSyntheticBybitTON", Tag: "1234567"}
				assetID, value, atomic = "ton-mainnet:TON", "1.25", "1250000000"
			}
			// A restricted balance read is diagnosed, not presented as zero.
			_, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"assetId": assetID, "side": "Sell"}))
			if publicRequestErrorCode(err) != "EXCHANGE_READ_BALANCE__BYBIT_10005" || guard.tradingReads != 1 || adapter.prepares != 0 {
				t.Fatal("restricted read was hidden or authorized a payment", err)
			}
			guard.tradingReads = 0
			result := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"assetId": assetID, "amount": value})
			if result.State != "CONFIRMED" || result.Kind != "deposit" || guard.tradingReads != 0 || api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 || adapter.lastTransfer.Amount != atomic || adapter.lastTransfer.Recipient != api.deposit.Address {
				t.Fatal("deposit depended on trading or bypassed the wallet pipeline", result, guard.tradingReads)
			}
			if network == "ton-mainnet" && adapter.lastTransfer.Memo != "1234567" {
				t.Fatal("required TON deposit memo lost")
			}
			if len(readPublicState(t, path)["ops"].([]any)) != 1 {
				t.Fatal("deposit lost original durable wallet identity")
			}
		})
	}
}

func TestExchangeDepositReadErrorsRetainStageAndNumericCodeBeforePreparation(t *testing.T) {
	for _, tc := range []struct{ stage, name string }{{"KEY", "key"}, {"COIN", "coin"}, {"DEPOSIT_ADDRESS", "deposit"}} {
		t.Run(tc.stage, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			api.fail[tc.name] = fmt.Errorf("API_SECRET_CANARY private provider body: %w", &bybit.APIError{Code: 10005})
			_, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1"}))
			want := "EXCHANGE_READ_" + tc.stage + "__BYBIT_10005"
			if publicRequestErrorCode(err) != want || adapter.prepares != 0 || adapter.submits != 0 || api.posts != 0 || len(readPublicState(t, path)["ops"].([]any)) != 0 {
				t.Fatal("preflight reason lost or wallet changed", err)
			}
		})
	}
	for _, tc := range []struct{ stage, cause string }{{"KEY", "EXCHANGE_STATE_INVALID"}, {"COIN", "REQUEST_FAILED_STATE_UNKNOWN"}} {
		err := errors.New("PRIVATE_PROVIDER_BODY_CANARY")
		if tc.stage == "KEY" {
			err = errExchange
		}
		code := publicRequestErrorCode(exchangeReadFailure(tc.stage, err))
		if code != "EXCHANGE_READ_"+tc.stage+"__"+tc.cause {
			t.Fatal("unclassified stage lost", code)
		}
	}
	if publicRequestErrorCode(exchangeReadFailure("KEY", context.Canceled)) != "REQUEST_CANCELLED" {
		t.Fatal("cancellation became a failure dialog")
	}
}

func TestExchangeDepositPolicyFailuresHaveSpecificReasonsAndNoPayment(t *testing.T) {
	for _, tc := range []struct{ scenario, code string }{{"master", "EXCHANGE_DEPOSIT_MASTER_REQUIRED"}, {"route", "EXCHANGE_DEPOSIT_ROUTE_UNAVAILABLE"}, {"suspended", "EXCHANGE_DEPOSIT_SUSPENDED"}, {"minimum", "EXCHANGE_DEPOSIT_BELOW_MINIMUM"}, {"address", "EXCHANGE_DEPOSIT_NETWORK_MISMATCH"}} {
		t.Run(tc.scenario, func(t *testing.T) {
			c, api, adapter, _ := fixtureExchangeCoordinator(t)
			switch tc.scenario {
			case "master":
				api.info.IsMaster = false
			case "route":
				api.chain.ChainType = "Base"
			case "suspended":
				api.chain.ChainDeposit = "0"
			case "minimum":
				api.chain.DepositMin = "1"
			case "address":
				api.deposit.Chain = "BASE"
			}
			_, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1"}))
			if ipc.PublicErrorCode(err) != tc.code || adapter.prepares != 0 || adapter.submits != 0 || api.posts != 0 {
				t.Fatal("specific rejection lost or payment started", err)
			}
		})
	}
}
