package walletcore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

// Public coordinator fixtures exercise the same typed calls as the wizard.
// No credentials, wallet secrets or live exchange account are used.
type stablecoinPoolsAPI struct {
	*fixtureExchangeAPI
	coins     map[string]bybit.Coin
	coinReads []string
}

func (api *stablecoinPoolsAPI) CoinInfo(_ context.Context, coin string) (bybit.Coin, error) {
	api.coinReads = append(api.coinReads, coin)
	return api.coins[coin], api.fail["coin"]
}

func (api *stablecoinPoolsAPI) WithdrawalRecords(_ context.Context, coin, id, _ string) (bybit.WithdrawalPage, error) {
	// The test sends an inclusive debit of 1.01 with a 0.01 fixed fee.
	return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{WithdrawID: id, Coin: coin, Chain: api.withdrawal.Chain, Amount: "1", WithdrawFee: "0.01", Status: "success", ToAddress: api.withdrawal.Address, WithdrawType: 0, TxID: "public-stablecoin-withdrawal-hash"}}}, api.fail["withdrawstatus"]
}

func stablecoinPoolsCoordinator(t *testing.T) (*core, *stablecoinPoolsAPI, string) {
	t.Helper()
	c, api, adapter, path, w := wizardCoordinator(t)
	adapter.networks = []model.Network{
		{ID: "ethereum-mainnet", Family: "evm", Name: "Ethereum", Assets: []model.Asset{{ID: "ethereum-mainnet:native", Symbol: "ETH", Decimals: 18}, {ID: evm.USDTAsset, Symbol: "USDT", Decimals: 6, Contract: evm.USDTContract}, {ID: evm.USDCAsset, Symbol: "USDC", Decimals: 6, Contract: evm.USDCContract}}},
		{ID: "arbitrum-mainnet", Family: "evm", Name: "Arbitrum One", Assets: []model.Asset{{ID: evm.ArbitrumUSDTAsset, Symbol: "USDT0", Decimals: 6, Contract: evm.ArbitrumUSDTContract}, {ID: evm.ArbitrumUSDCAsset, Symbol: "USDC", Decimals: 6, Contract: evm.ArbitrumUSDCContract}}},
		{ID: "ton-mainnet", Family: "ton", Name: "TON Mainnet", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}, {ID: "ton-mainnet:USDT", Symbol: "USDT", Decimals: 6, Contract: ton.USDTMaster}}},
	}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{
		{ID: "exchange-account", Family: "evm", Network: "ethereum-mainnet", Index: 0, Address: "0x1111111111111111111111111111111111111111"},
		{ID: "arbitrum-owned", Family: "evm", Network: "arbitrum-mainnet", Index: 0, Address: "0x1111111111111111111111111111111111111111"},
		{ID: "gram-source", Family: "ton", Network: "ton-mainnet", Index: 0, Address: "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ"},
		{ID: "foreign-group", Family: "evm", Network: "ethereum-mainnet", Index: 1, Address: "0x3333333333333333333333333333333333333333"},
		{ID: "hidden-wallet", Family: "evm", Network: "ethereum-mainnet", Index: 0, Hidden: true, Address: "0x4444444444444444444444444444444444444444"},
	}
	w.SourceAccount, w.SourceAsset, w.DepositAmount = "gram-source", "ton-mainnet:TON", "6"
	state["wizard"] = w
	writePublicState(t, path, state)
	chain := func(name, display, contract string) bybit.Chain {
		return bybit.Chain{Chain: name, ChainType: display, ContractAddress: contract, ChainDeposit: "1", ChainWithdraw: "1", DepositMin: "0.001", WithdrawMin: "0.1", WithdrawMax: "1000", WithdrawFee: "0.01", WithdrawPercentageFee: "0", MinAccuracy: "6"}
	}
	wrapped := &stablecoinPoolsAPI{fixtureExchangeAPI: api.fixtureExchangeAPI, coins: map[string]bybit.Coin{
		"USDT": {Coin: "USDT", Chains: []bybit.Chain{chain("ETH", "Ethereum", evm.USDTContract), chain("ARBI", "Arbitrum One", evm.ArbitrumUSDTContract), chain("TON", "TON", ton.USDTMaster)}},
		"USDC": {Coin: "USDC", Chains: []bybit.Chain{chain("ETH", "Ethereum", evm.USDCContract), chain("ARBI", "Arbitrum One", evm.ArbitrumUSDCContract)}},
	}}
	factory := c.newExchange
	c.newExchange = func(endpoint string, auth bybit.Authorizer) (exchangeAPI, error) {
		_, err := factory(endpoint, auth)
		return wrapped, err
	}
	return c, wrapped, path
}

func TestExchangeStablecoinPoolsAlwaysIncludeBothFundingAndUnifiedWithoutRouteReads(t *testing.T) {
	for _, selected := range []struct{ account, asset, coin string }{{"gram-source", "ton-mainnet:TON", "GRAM"}, {"exchange-account", evm.USDTAsset, "USDT"}, {"exchange-account", evm.USDCAsset, "USDC"}} {
		t.Run(selected.coin, func(t *testing.T) {
			c, api, path := stablecoinPoolsCoordinator(t)
			before := loadWizard(t, c)
			api.fail["coin"] = &bybit.CoinMetadataError{Field: "IDENTITY"}
			values := map[string]string{"FUND:GRAM": "0.160919023", "UNIFIED:GRAM": "5", "FUND:USDC": "25.125001", "UNIFIED:USDC": "7.000001", "FUND:USDT": "12.34", "UNIFIED:USDT": "5.67"}
			reads := []string{}
			api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
				reads = append(reads, kind+":"+coin)
				return custodyTestBalance(kind, coin, values[kind+":"+coin]), nil
			}
			for observation := 0; observation < 2; observation++ {
				reads = nil
				out, err := c.handle(message("exchangeFundingBalances", map[string]string{"accountId": selected.account, "assetId": selected.asset}))
				if err != nil {
					t.Fatal(err)
				}
				response := out.(struct {
					Coin     string            `json:"coin"`
					Balances []exchangeBalance `json:"balances"`
				})
				wantCoins := map[string]bool{selected.coin: true, "USDC": true, "USDT": true}
				if response.Coin != selected.coin || len(response.Balances) != len(wantCoins)*2 || len(reads) != len(wantCoins)*2 {
					t.Fatal("selected coin or stablecoin pools were omitted, duplicated or relabeled", response, reads)
				}
				for coin := range wantCoins {
					for _, kind := range []string{"FUND", "UNIFIED"} {
						if exchangeAvailable(response.Balances, kind, coin) != values[kind+":"+coin] {
							t.Fatal("stablecoin observation used another asset, stale value or pool", response)
						}
					}
				}
				values["FUND:USDC"], values["UNIFIED:USDT"] = "0", "0.000001"
			}
			if api.posts != 0 || len(api.coinReads) != 0 || independentExchangeOperationCount(t, path) != 0 || loadWizard(t, c) != before {
				t.Fatal("balance display inferred network-route/payment authority or changed original GRAM workflow")
			}
		})
	}
}

func TestExchangeStablecoinPoolsRejectPartialOrMismatchedFreshBalances(t *testing.T) {
	for _, failure := range []string{"read", "coin", "account", "number"} {
		t.Run(failure, func(t *testing.T) {
			c, api, path := stablecoinPoolsCoordinator(t)
			api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
				b := custodyTestBalance(kind, coin, "5")
				if kind == "UNIFIED" && coin == "USDC" {
					switch failure {
					case "read":
						return bybit.Balance{}, errors.New("synthetic unavailable USDC")
					case "coin":
						b.Balance.Coin = "USDT"
					case "account":
						b.AccountType = "FUND"
					case "number":
						b.Balance.TransferSafeAmount = "NaN"
					}
				}
				return b, nil
			}
			out, err := c.handle(message("exchangeFundingBalances", map[string]string{"accountId": "gram-source", "assetId": "ton-mainnet:TON"}))
			if err == nil || out != nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("partial/malformed stablecoin pools were reported as a complete spendable snapshot", out, err)
			}
		})
	}
}

func TestExchangeStablecoinTransfersUseSelectedOwnedCoinDespiteOriginalGRAMDeposit(t *testing.T) {
	for _, coin := range []struct{ symbol, asset string }{{"USDT", evm.USDTAsset}, {"USDC", evm.USDCAsset}} {
		for _, method := range []string{"exchangeFundingMove", "exchangeUnifiedMove"} {
			t.Run(coin.symbol+"/"+method, func(t *testing.T) {
				c, api, path := stablecoinPoolsCoordinator(t)
				before := loadWizard(t, c)
				api.fail["coin"] = &bybit.CoinMetadataError{Field: "IDENTITY"}
				api.balances["FUND:GRAM"], api.balances["UNIFIED:GRAM"] = "0", "0"
				api.balances["FUND:"+coin.symbol], api.balances["UNIFIED:"+coin.symbol] = "2.123456", "2.123456"
				result := exchangeResultCall(t, c, method, map[string]any{"assetId": coin.asset, "amount": "1.123456"})
				from, to := "FUND", "UNIFIED"
				if method == "exchangeUnifiedMove" {
					from, to = to, from
				}
				if result.Kind != "transfer" || result.State != "CONFIRMED" || api.posts != 1 || api.transfer.Coin != coin.symbol || api.transfer.Amount != "1.123456" || api.transfer.FromAccountType != from || api.transfer.ToAccountType != to || len(api.coinReads) != 0 {
					t.Fatal("selected stablecoin was replaced by original GRAM or unapproved transfer direction", result, api.transfer)
				}
				op := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
				if op["assetId"] != coin.asset || op["accountId"] != "exchange-account" || op["currentFunds"] != true || loadWizard(t, c) != before {
					t.Fatal("transfer reused original deposit metadata/authority instead of selected owned stablecoin", op)
				}
			})
		}
	}
}

func TestExchangeStablecoinWithdrawalRoutesFromGRAMSourceUseOwnedNetworkAndExactContract(t *testing.T) {
	for _, coin := range []string{"USDT", "USDC"} {
		t.Run(coin, func(t *testing.T) {
			c, api, path := stablecoinPoolsCoordinator(t)
			before := loadWizard(t, c)
			out, err := c.handle(message("exchangeWithdrawOptions", map[string]any{"coin": coin, "existingFunds": true, "sourceAccountId": "gram-source"}))
			if err != nil {
				t.Fatal(err)
			}
			choices := out.([]exchangeWithdrawalChoice)
			want := map[string]string{"exchange-account": "ETH", "arbitrum-owned": "ARBI"}
			if coin == "USDT" {
				want["gram-source"] = "TON"
			}
			if len(choices) != len(want) {
				t.Fatal("stablecoin routes inherited original GRAM token/network restriction", choices)
			}
			for _, choice := range choices {
				if want[choice.AccountID] != choice.Chain || choice.Coin != coin || choice.Index != 0 || choice.Precision != "6" || choice.Address == "" {
					t.Fatal("foreign/hidden/network-mismatched stablecoin route or wrong precision", choice)
				}
				delete(want, choice.AccountID)
			}
			if len(want) != 0 || api.posts != 0 || loadWizard(t, c) != before || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("route discovery prepared payment or changed original GRAM workflow")
			}
			if !reflect.DeepEqual(api.coinReads, []string{coin}) {
				t.Fatal("route discovery consulted original GRAM asset instead of selected stablecoin", api.coinReads)
			}
		})
	}
}

func TestExchangeStablecoinWithdrawalCannotUseBalanceOrAnotherNetworkAsContractProof(t *testing.T) {
	for _, malformed := range []string{"contract", "coin", "duplicate", "suspended"} {
		t.Run(malformed, func(t *testing.T) {
			c, api, path := stablecoinPoolsCoordinator(t)
			coin := api.coins["USDC"]
			switch malformed {
			case "contract":
				coin.Chains[0].ContractAddress = evm.ArbitrumUSDCContract
			case "coin":
				coin.Coin = "USDT"
			case "duplicate":
				coin.Chains = append(coin.Chains, coin.Chains[0])
			case "suspended":
				coin.Chains[0].ChainWithdraw = "0"
			}
			api.coins["USDC"] = coin
			api.balances["FUND:USDC"] = "100"
			out, err := c.handle(message("exchangeWithdrawOptions", map[string]any{"coin": "USDC", "existingFunds": true, "sourceAccountId": "gram-source"}))
			if err != nil {
				t.Fatal(err)
			}
			for _, choice := range out.([]exchangeWithdrawalChoice) {
				if choice.AccountID == "exchange-account" {
					t.Fatal("invalid Ethereum stablecoin identity offered despite another valid network", choice)
				}
			}
			_, err = c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"assetId": evm.USDCAsset, "amount": "1.01", "existingFunds": true, "fundingOnly": true}))
			if err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("Funding availability or valid other network bypassed selected stablecoin route identity", err)
			}
		})
	}
}

func TestExchangeStablecoinReturnThenFundingOnlyWithdrawalRetainsOriginalGRAMWorkflow(t *testing.T) {
	for _, coin := range []struct{ symbol, asset string }{{"USDT", evm.USDTAsset}, {"USDC", evm.USDCAsset}} {
		t.Run(coin.symbol, func(t *testing.T) {
			c, api, path := stablecoinPoolsCoordinator(t)
			before := loadWizard(t, c)
			api.balances["FUND:"+coin.symbol], api.balances["UNIFIED:"+coin.symbol] = "0", "1.123456"
			returned := exchangeResultCall(t, c, "exchangeUnifiedMove", map[string]any{"assetId": coin.asset, "amount": "1.123456"})
			if returned.Kind != "transfer" || returned.State != "CONFIRMED" || api.transfer.Coin != coin.symbol || api.transfer.FromAccountType != "UNIFIED" || api.transfer.ToAccountType != "FUND" {
				t.Fatal("stablecoin return-to-Funding failed", returned)
			}
			api.balances["FUND:"+coin.symbol], api.balances["UNIFIED:"+coin.symbol] = "1.123456", "0"
			withdrawn := exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"assetId": coin.asset, "amount": "1.01", "existingFunds": true, "fundingOnly": true})
			if withdrawn.Kind != "withdraw" || withdrawn.State != "EXCHANGE_COMPLETED" || withdrawn.ID == returned.ID || api.posts != 2 || api.withdrawal.Coin != coin.symbol || api.withdrawal.AccountType != "FUND" || api.withdrawal.Chain != "ETH" || api.withdrawal.Amount != "1.01" || api.withdrawal.Address != "0x1111111111111111111111111111111111111111" || loadWizard(t, c) != before || independentExchangeOperationCount(t, path) != 2 {
				t.Fatal("stablecoin withdrawal reused GRAM/transfer identity or changed exact owned destination", withdrawn, api.withdrawal)
			}
		})
	}
}
