package walletcore

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

// Metadata deliberately fails exactly as in the reported account. Spot reads
// and trades must not consult it, while deposits/withdrawals must still refuse
// an unverified route. These are inherited-pipe coordinator fixtures, not live
// exchange acceptance or real-money tests.
type spotCandidateExchange struct {
	*wizardExchange
	coinReads, instrumentReads int
	mutateInstrument           func(*bybit.Instrument)
}

func (api *spotCandidateExchange) CoinInfo(context.Context, string) (bybit.Coin, error) {
	api.coinReads++
	return bybit.Coin{}, &bybit.CoinMetadataError{Field: "IDENTITY"}
}

func (api *spotCandidateExchange) SpotInstrument(ctx context.Context, symbol string) (bybit.Instrument, error) {
	api.instrumentReads++
	instrument, err := api.wizardExchange.SpotInstrument(ctx, symbol)
	if err == nil && api.mutateInstrument != nil {
		api.mutateInstrument(&instrument)
	}
	return instrument, err
}

func spotCandidateCoordinator(t *testing.T) (*core, *spotCandidateExchange, *publicAdapter, string) {
	t.Helper()
	c, api, adapter, path, _ := wizardCoordinator(t)
	wrapped := &spotCandidateExchange{wizardExchange: api}
	factory := c.newExchange
	c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
		_, err := factory(endpoint, authorize)
		return wrapped, err
	}
	return c, wrapped, adapter, path
}

func TestExchangeSpotCandidatesListBothDirectionsWithoutCoinOrWithdrawalReads(t *testing.T) {
	c, api, adapter, path := spotCandidateCoordinator(t)
	adapter.networks[0].Assets = append(adapter.networks[0].Assets, model.Asset{ID: "ethereum-mainnet:usdc", Symbol: "USDC", Decimals: 6})
	adapter.networks = append(adapter.networks,
		model.Network{ID: "ton-mainnet", Family: "ton", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}}},
		model.Network{ID: "near-mainnet", Family: "near", Assets: []model.Asset{{ID: "near-mainnet:native", Symbol: "NEAR", Decimals: 24}}})
	state := readPublicState(t, path)
	state["accounts"] = append(state["accounts"].([]any),
		model.Account{ID: "ton-owned", Network: "ton-mainnet", Family: "ton", Index: 0},
		model.Account{ID: "other-group", Network: "near-mainnet", Family: "near", Index: 7})
	writePublicState(t, path, state)
	api.listed = map[string]bool{"GRAMUSDT": true, "ETHUSDT": true, "USDCUSDT": true, "NEARUSDT": true}
	// Discovery is read-only and does not require withdrawal permission.
	api.info.Permissions.Wallet = nil
	_ = loadWizard(t, c) // Initialize the fixture session before the read snapshot.
	before := readPublicState(t, path)
	out, err := c.handle(exchangeMessage("exchangeSwapOptions", map[string]any{"spotOnly": true}))
	if err != nil {
		t.Fatal(err)
	}
	options := out.([]exchangeSwapChoice)
	after := readPublicState(t, path)
	delete(before, "calls")
	delete(after, "calls")
	if len(options) != 6 || api.coinReads != 0 || api.posts != 0 || !reflect.DeepEqual(after, before) {
		t.Fatal("spot candidates consulted route metadata, changed state or lost both directions", options, api.coinReads)
	}
	want := map[string]bool{"ETHUSDT/Sell": true, "ETHUSDT/Buy": true, "GRAMUSDT/Sell": true, "GRAMUSDT/Buy": true, "USDCUSDT/Sell": true, "USDCUSDT/Buy": true}
	for _, option := range options {
		key := option.Symbol + "/" + option.Side
		if !want[key] || option.AccountID == "other-group" {
			t.Fatal("invented, duplicate or foreign-group candidate", option)
		}
		delete(want, key)
		base, quote, _ := bybit.SpotPair(option.Symbol)
		output := quote
		if option.Side == "Buy" {
			output = base
		}
		if option.OutputCoin != output {
			t.Fatal("direction has wrong receiving coin", option)
		}
	}
	if len(want) != 0 {
		t.Fatal("missing candidates", want)
	}
}

func TestExchangeSpotQuoteAndFreshUnifiedSwapIgnoreUnrelatedCoinMetadata(t *testing.T) {
	for _, side := range []string{"Sell", "Buy"} {
		t.Run(side, func(t *testing.T) {
			c, api, _, path := spotCandidateCoordinator(t)
			before := loadWizard(t, c)
			out, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": side, "unifiedOnly": true}))
			if err != nil {
				t.Fatal(err)
			}
			quote := out.(exchangeQuote)
			if quote.Symbol != "ETHUSDT" || quote.Side != side || quote.Route.Supported || quote.LastPrice != "2000" || len(quote.Balances) != 4 || api.coinReads != 0 || api.posts != 0 {
				t.Fatal(quote, api.coinReads)
			}
			value, spending := "0.1", "ETH"
			if side == "Buy" {
				value, spending = "200", "USDT"
			}
			api.balances["FUND:"+spending], api.balances["UNIFIED:"+spending] = "0", "1000"
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": side, "amount": value, "slippage": "0.5", "unifiedOnly": true})
			if result.Kind != "swap" || result.State != "CONFIRMED" || api.posts != 1 || api.order.Side != side || api.coinReads != 0 || api.instrumentReads != 2 || loadWizard(t, c) != before || independentExchangeOperationCount(t, path) != 1 {
				t.Fatal("spot order reused route/cached market or changed old workflow", result, api.coinReads, api.instrumentReads)
			}
			if _, err = c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1", "existingFunds": true})); err == nil || api.coinReads != 1 || api.posts != 1 {
				t.Fatal("spot read authorized an unverified withdrawal route", err)
			}
		})
	}
}

func TestExchangeSpotCandidatesAndQuotesRejectMalformedLiveInstrumentRules(t *testing.T) {
	for _, field := range []string{"symbol", "base", "quote", "status", "base-precision", "quote-precision", "minimum", "maximum", "tick"} {
		for _, method := range []string{"exchangeSwapOptions", "exchangeQuote", "exchangeSwap"} {
			t.Run(method+"/"+field, func(t *testing.T) {
				c, api, _, path := spotCandidateCoordinator(t)
				api.mutateInstrument = func(i *bybit.Instrument) {
					switch field {
					case "symbol":
						i.Symbol = "BTCUSDT"
					case "base":
						i.BaseCoin = "BTC"
					case "quote":
						i.QuoteCoin = "USDC"
					case "status":
						i.Status = "PreLaunch"
					case "base-precision":
						i.LotSizeFilter.BasePrecision = "0"
					case "quote-precision":
						i.LotSizeFilter.QuotePrecision = "NaN"
					case "minimum":
						i.LotSizeFilter.MinOrderAmt = "-1"
					case "maximum":
						i.LotSizeFilter.MaxMarketOrderQty = "0"
					case "tick":
						i.PriceFilter.TickSize = ""
					}
				}
				body := map[string]any{"spotOnly": true}
				if method != "exchangeSwapOptions" {
					body = map[string]any{"unifiedOnly": true, "side": "Sell"}
				}
				if method == "exchangeSwap" {
					body["amount"], body["slippage"] = "0.1", "0.5"
				}
				if _, err := c.handle(exchangeMessage(method, body)); err == nil || !strings.Contains(publicRequestErrorCode(err), "INSTRUMENT") || api.coinReads != 0 || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
					t.Fatal("malformed instrument offered or authorized", err)
				}
			})
		}
	}
}

func TestExchangeSpotSubmissionRereadsInstrumentAndUnifiedBalance(t *testing.T) {
	for _, changed := range []string{"listing", "rules", "balance", "permission"} {
		t.Run(changed, func(t *testing.T) {
			c, api, _, path := spotCandidateCoordinator(t)
			if _, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Sell", "unifiedOnly": true})); err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "listing":
				api.listed = map[string]bool{}
			case "rules":
				api.mutateInstrument = func(i *bybit.Instrument) { i.LotSizeFilter.MinOrderAmt = "1000000" }
			case "balance":
				api.balances["UNIFIED:ETH"] = "0"
			case "permission":
				api.info.Permissions.Spot = nil
			}
			if _, err := c.handle(exchangeMessage("exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5", "unifiedOnly": true})); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("old quote bypassed fresh economic checks", err)
			}
		})
	}
}

func TestExchangeSpotReadModeFlagsAreStrictAndCannotSelectOtherProviders(t *testing.T) {
	for _, method := range []string{"exchangeSwapOptions", "exchangeQuote"} {
		field := "spotOnly"
		if method == "exchangeQuote" {
			field = "unifiedOnly"
		}
		for _, raw := range []string{`null`, `"true"`, `1`, `[]`, `{}`} {
			c, api, _, _ := spotCandidateCoordinator(t)
			body := map[string]any{"accountId": "exchange-account", "assetId": "ethereum-mainnet:native", field: json.RawMessage(raw)}
			if method == "exchangeQuote" {
				body["side"] = "Sell"
			}
			if _, err := c.handle(message(method, body)); err == nil || api.posts != 0 || api.instrumentReads != 0 || api.coinReads != 0 {
				t.Fatal("nonboolean selected spot-only authority", method, raw, err)
			}
		}
		for _, shadow := range []string{strings.Title(field), strings.ToUpper(field)} {
			c, api, _, _ := spotCandidateCoordinator(t)
			body := map[string]any{"accountId": "exchange-account", "assetId": "ethereum-mainnet:native", shadow: true}
			if method == "exchangeQuote" {
				body["side"] = "Sell"
			}
			if _, err := c.handle(message(method, body)); err == nil || api.instrumentReads != 0 || api.coinReads != 0 {
				t.Fatal("case-shadowed mode selected route policy", err)
			}
		}
	}
}
