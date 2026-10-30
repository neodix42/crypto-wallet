package walletcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

type legacyTONHistoryAPI struct {
	*fixtureExchangeAPI
	mode    string
	lookups []string
}

func (a *legacyTONHistoryAPI) FindOrder(_ context.Context, symbol, id string) (*bybit.Order, error) {
	a.lookups = append(a.lookups, symbol+":"+id)
	if a.mode == "unavailable" {
		return nil, bybit.ErrUnavailable
	}
	if a.mode == "foreign" {
		symbol = "GRAMUSDT"
	}
	return &bybit.Order{OrderID: "public-old-remote", OrderLinkID: id, Symbol: symbol, Side: "Sell", OrderType: "Market", Qty: "0.1", MarketUnit: "baseCoin", IsLeverage: "0", OrderStatus: "Filled", CumExecQty: "0.1", CumExecValue: "0.2"}, nil
}
func (a *legacyTONHistoryAPI) Executions(_ context.Context, symbol, id, cursor string) (bybit.ExecutionPage, error) {
	a.lookups = append(a.lookups, symbol+":"+id)
	return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{{OrderID: "public-old-remote", OrderLinkID: id, Symbol: symbol, Side: "Sell", OrderType: "Market", ExecID: "old-ton-fill", ExecQty: "0.1", ExecValue: "0.2", ExecPrice: "2", ExecFee: "0.0002", FeeCurrency: "USDT", ExtraFees: json.RawMessage(`""`)}}}, nil
}
func (a *legacyTONHistoryAPI) TransferStatus(_ context.Context, id string) (*bybit.TransferRecord, error) {
	a.lookups = append(a.lookups, "TON:"+id)
	if a.mode == "unavailable" {
		return nil, bybit.ErrUnavailable
	}
	coin := "TON"
	if a.mode == "foreign" {
		coin = "GRAM"
	}
	return &bybit.TransferRecord{TransferID: id, Coin: coin, Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED", Status: "SUCCESS"}, nil
}
func (a *legacyTONHistoryAPI) WithdrawalRecords(_ context.Context, coin, id, cursor string) (bybit.WithdrawalPage, error) {
	a.lookups = append(a.lookups, coin+":"+id)
	if a.mode == "unavailable" {
		return bybit.WithdrawalPage{}, bybit.ErrUnavailable
	}
	if a.mode == "foreign" {
		coin = "GRAM"
	}
	return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{WithdrawID: id, Coin: coin, Chain: "TON", Amount: "0.099", WithdrawFee: "0.001", Status: "success", ToAddress: "public-owned-TON-address", TxID: "public-old-onchain-id"}}}, nil
}
func TestGRAMCurrentRegistryReconcilesOnlyTheOriginalLegacyTONOperation(t *testing.T) {
	for _, kind := range []string{"swap", "transfer", "withdraw"} {
		for _, mode := range []string{"exact", "unavailable", "foreign"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				c, base, adapter, path := fixtureExchangeCoordinator(t)
				adapter.networks = []model.Network{{ID: "ton-mainnet", Family: "ton", ChainID: "-239", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}}}}
				api := &legacyTONHistoryAPI{fixtureExchangeAPI: base, mode: mode}
				c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
					api.endpoint = endpoint
					api.authorize = authorize
					return api, nil
				}
				id := strings.Repeat("1", 32)
				transferID := "11111111-1111-1111-1111-111111111111"
				var payload any
				remote := "public-old-remote"
				switch kind {
				case "swap":
					payload = bybit.SpotOrder{Category: "spot", Symbol: "TONUSDT", Side: "Sell", OrderType: "Market", Qty: "0.1", MarketUnit: "baseCoin", OrderLinkID: id}
				case "transfer":
					remote = transferID
					payload = bybit.TransferRequest{TransferID: transferID, Coin: "TON", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
				case "withdraw":
					payload = bybit.WithdrawalRequest{Coin: "TON", Chain: "TON", Address: "public-owned-TON-address", Amount: "0.1", RequestID: id}
				}
				raw, _ := json.Marshal(payload)
				original := vault.ExchangeOperation{ID: id, Kind: kind, Endpoint: "https://api.bybit.com", CredentialID: "fixture-credential", AccountID: "exchange-account", AssetID: "ton-mainnet:TON", Payload: string(raw), State: "UNKNOWN", ExternalID: remote, Digest: "frozen-original-digest"}
				state := readPublicState(t, path)
				state["exchangeOps"] = []vault.ExchangeOperation{original}
				state["accounts"] = []model.Account{{ID: "exchange-account", Family: "ton", Network: "ton-mainnet", Address: "public-owned-TON-address"}}
				writePublicState(t, path, state)
				out, err := c.handle(message("exchangeReconcile", map[string]string{"id": id}))
				if mode == "foreign" {
					if err == nil {
						t.Fatal("GRAM response substituted for old TON operation")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					result := out.(exchangeResult)
					expected := "UNKNOWN"
					if mode == "exact" {
						expected = "CONFIRMED"
						if kind == "withdraw" {
							expected = "EXCHANGE_COMPLETED"
						}
					}
					if result.State != expected {
						t.Fatalf("state %s != %s", result.State, expected)
					}
				}
				if api.posts != 0 || len(api.lookups) == 0 {
					t.Fatal("reconciliation submitted a replacement or never queried original")
				}
				for _, lookup := range api.lookups {
					if strings.Contains(lookup, "GRAM") || !strings.Contains(lookup, "TON") {
						t.Fatalf("renamed original lookup %q", lookup)
					}
				}
				stored := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
				if stored["payload"] != original.Payload || stored["digest"] != original.Digest || stored["externalId"] != original.ExternalID {
					t.Fatal("reconciliation rewrote original bytes/digest/remote ID")
				}
				if mode != "exact" && stored["state"] != "UNKNOWN" {
					t.Fatal("unavailable/foreign renamed history released UNKNOWN")
				}
				if kind == "swap" && mode == "exact" {
					result := out.(exchangeResult)
					if result.FilledBase != "0.1" || result.FilledQuote != "0.2" || result.Fees["USDT"] != "0.0002" {
						t.Fatal(fmt.Sprintf("historical fill units changed %+v", result))
					}
				}
			})
		}
	}
}
