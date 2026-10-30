package signer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/vault"
)

func nativeExchangeEngine(t *testing.T, provider string) (*Engine, *testUI, string) {
	t.Helper()
	engine, ui, account := exchangeEngine(t)
	values := []string{"PUBLIC_NATIVE_API_KEY_82745", "PUBLIC_NATIVE_API_SECRET_957245827"}
	endpoint := "https://api.binance.com"
	if provider == "kucoin" {
		endpoint = "https://api.kucoin.com"
		values = append(values, "PUBLIC_API_PASSPHRASE_82475", "3")
	}
	ui.inputs = append(ui.inputs, input(values...))
	if err := engine.ConnectExchangeProvider(provider, endpoint); err != nil {
		t.Fatal(err)
	}
	return engine, ui, account.ID
}

func nativeExchangeSwap(account string) ExchangeIntent {
	return ExchangeIntent{Kind: "swap", AccountID: account, AssetID: "ethereum-mainnet:native", LastPrice: "2500", Fee: "0.01", FeeCoin: "USDT", Order: &bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Limit", Qty: "0.01", MarketUnit: "baseCoin", IsLeverage: 0, SlippageToleranceType: "Percent", SlippageTolerance: "1", BaseQty: "0.01", LimitPrice: "2475"}}
}

func TestNativeExchangeExactIOCApprovalDurableAuthorizationReplayAndRestart(t *testing.T) {
	for _, provider := range []string{"binance"} {
		t.Run(provider, func(t *testing.T) {
			e, u, account := nativeExchangeEngine(t, provider)
			operation, err := e.PrepareExchange(nativeExchangeSwap(account))
			if err != nil {
				t.Fatal("native policy not prepared", err)
			}
			request, err := exchangeOperationRequest(operation, time.Now().UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.ExchangeAuth(request); err == nil {
				t.Fatal("unapproved native operation authenticated")
			}
			u.inputs = append(u.inputs, input("wrong-public-password"))
			if _, err = e.AuthorizeExchange(operation.ID); err == nil {
				t.Fatal("wrong password approved native operation")
			}
			operation, err = e.PrepareExchange(nativeExchangeSwap(account))
			if err != nil {
				t.Fatal(err)
			}
			operation = authorizeExchange(t, e, u, operation)
			summary := u.summaries[len(u.summaries)-1]
			for _, field := range []string{"0.01 ETH", "2475 USDT per ETH", "IOC", "cancel unfilled remainder", operation.ID, operation.Endpoint} {
				if !strings.Contains(summary, field) {
					t.Fatal("native approval omitted exact policy", field)
				}
			}
			request, err = exchangeOperationRequest(operation, time.Now().UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*bybit.AuthRequest){func(r *bybit.AuthRequest) { r.Path += "/arbitrary" }, func(r *bybit.AuthRequest) { r.Payload = strings.Replace(r.Payload, "2475", "2476", 1) }, func(r *bybit.AuthRequest) { r.Endpoint = "https://api.bybit.com" }, func(r *bybit.AuthRequest) { r.Method = "DELETE" }} {
				bad := request
				mutate(&bad)
				if _, err = e.ExchangeAuth(bad); err == nil {
					t.Fatal("changed native request authenticated")
				}
			}
			if _, err = e.ExchangeAuth(request); err != nil {
				t.Fatal("approved exact native bytes denied", err)
			}
			stored, _ := e.ExchangeOperation(operation.ID)
			if stored.State != "SUBMITTING" {
				t.Fatal("native signature escaped before durable submit state")
			}
			if _, err = e.ExchangeAuth(request); err == nil {
				t.Fatal("native write signature replayed")
			}
			if err = e.RejectExchange(operation.ID, 10001); err == nil {
				t.Fatal("Bybit numeric rejection code released another provider operation")
			}
			if err = e.UpdateExchange(operation.ID, "UNKNOWN", ""); err != nil {
				t.Fatal(err)
			}
			e.Lock()
			u.inputs = append(u.inputs, input(syntheticPassword))
			if err = e.Unlock(); err != nil {
				t.Fatal(err)
			}
			stored, _ = e.ExchangeOperation(operation.ID)
			if stored.State != "UNKNOWN" || stored.Digest != operation.Digest || stored.Payload != operation.Payload {
				t.Fatal("restart lost original native identity")
			}
			if _, err = e.ExchangeAuth(request); err == nil {
				t.Fatal("restart retried ambiguous native operation")
			}
			if e.SelectExchange("bybit") == nil {
				t.Fatal("native unknown bypassed by provider selection")
			}
			if _, err = e.PrepareExchange(nativeExchangeSwap(account)); err == nil {
				t.Fatal("ambiguous native operation replaced")
			}
			if err = e.UpdateExchange(operation.ID, "CONFIRMED", "exact_original_remote"); err != nil {
				t.Fatal(err)
			}
			if err = e.SelectExchange("bybit"); err != nil {
				t.Fatal("resolved native operation blocked original service", err)
			}
		})
	}
}

func TestNativeExchangeRejectsUnboundedOrderAndForeignWithdrawalPolicies(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		t.Run(provider, func(t *testing.T) {
			e, _, account := nativeExchangeEngine(t, provider)
			for _, change := range []func(*ExchangeIntent){func(i *ExchangeIntent) { i.Order.LimitPrice = "" }, func(i *ExchangeIntent) { i.Order.OrderType = "Market" }, func(i *ExchangeIntent) { i.Order.BaseQty = "0.02" }, func(i *ExchangeIntent) { i.Order.IsLeverage = 1 }, func(i *ExchangeIntent) { i.Order.Symbol = "ETHBTC" }, func(i *ExchangeIntent) {
				i.Order.Side = "Buy"
				i.Order.MarketUnit = "quoteCoin"
				i.Order.Qty = "1"
				i.Order.BaseQty = "1"
			}, func(i *ExchangeIntent) { i.FeeCoin = "BNB" }} {
				intent := nativeExchangeSwap(account)
				change(&intent)
				if _, err := e.PrepareExchange(intent); err == nil {
					t.Fatal("unsafe native order was prepared")
				}
			}
			owned := findAccount(t, e, "ethereum-mainnet")
			mode := "binance-native"
			if provider == "kucoin" {
				mode = "kucoin-external"
			}
			withdrawal := bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: owned.Address, Amount: "1", ForceChain: 1, AccountType: "FUND", FeeType: 0, FeeMode: mode}
			if provider == "kucoin" {
				withdrawal.Chain = "eth"
			}
			intent := ExchangeIntent{Kind: "withdraw", AccountID: owned.ID, AssetID: "ethereum-mainnet:native", Withdrawal: &withdrawal, Fee: "0.01", FeeCoin: "ETH", ChainType: "Ethereum"}
			for _, change := range []func(*bybit.WithdrawalRequest){func(w *bybit.WithdrawalRequest) { w.Address = "0x0000000000000000000000000000000000000001" }, func(w *bybit.WithdrawalRequest) { w.FeeType = 1 }, func(w *bybit.WithdrawalRequest) { w.FeeMode = "" }, func(w *bybit.WithdrawalRequest) { w.Chain = "BSC" }, func(w *bybit.WithdrawalRequest) { w.Tag = "foreign-memo" }} {
				copy := withdrawal
				change(&copy)
				intent.Withdrawal = &copy
				if _, err := e.PrepareExchange(intent); err == nil {
					t.Fatal("foreign native withdrawal policy prepared")
				}
			}
			intent.Withdrawal = &withdrawal
			operation, err := e.PrepareExchange(intent)
			if err != nil {
				t.Fatal("owned native withdrawal denied", err)
			}
			var restored bybit.WithdrawalRequest
			if json.Unmarshal([]byte(operation.Payload), &restored) != nil || restored.RequestID != operation.ID || restored.FeeMode != mode {
				t.Fatal("withdrawal identity was not persisted")
			}
			if operation.Digest != vault.ExchangeDigest(operation) {
				t.Fatal("native withdrawal digest does not bind fee policy")
			}
		})
	}
}

func TestNativeExchangeApprovedWirePayloadMustMatchExactly(t *testing.T) {
	for _, provider := range []string{"binance"} {
		t.Run(provider, func(t *testing.T) {
			e, u, account := nativeExchangeEngine(t, provider)
			operation, err := e.PrepareExchange(nativeExchangeSwap(account))
			if err != nil {
				t.Fatal(err)
			}
			operation = authorizeExchange(t, e, u, operation)
			request, err := exchangeOperationRequest(operation, time.Now().UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			altered := request
			altered.Payload = strings.Replace(altered.Payload, "2475", "2476", 1)
			if altered.Payload == request.Payload {
				t.Fatal("fixture did not alter native price")
			}
			if _, err = e.ExchangeAuth(altered); err == nil {
				t.Fatal("altered native wire payload authenticated")
			}
			if _, err = e.ExchangeAuth(request); err != nil {
				t.Fatal("exact original native request denied", err)
			}
		})
	}
}

func TestKuCoinSpotFeeUncertaintyCannotPrepareApproveOrAuthenticateNewDebit(t *testing.T) {
	e, u, account := nativeExchangeEngine(t, "kucoin")
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.PrepareExchange(nativeExchangeSwap(account)); err == nil {
		t.Fatal("KuCoin unknown KCS fee setting prepared a trade")
	}
	transfer := ExchangeIntent{Kind: "transfer", AccountID: account, AssetID: "ethereum-mainnet:native", Fee: "0", FeeCoin: "ETH", Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}}
	if _, err = e.PrepareExchange(transfer); err == nil {
		t.Fatal("KuCoin unreconcilable internal transfer prepared")
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || string(before) != string(after) {
		t.Fatal("gated trade changed durable wallet state")
	}
	// An authenticated old profile may still contain a previously prepared
	// native operation. Preserve it for recovery, but never renew its write.
	intent := nativeExchangeSwap(account)
	id := "0123456789abcdef0123456789abcdef"
	intent.Order.OrderLinkID = id
	payload, _ := json.Marshal(intent.Order)
	operation := vault.ExchangeOperation{ID: id, Kind: "swap", Endpoint: "https://api.kucoin.com", CredentialID: vault.ExchangeCredentialID(e.data.Exchange.Credential("kucoin")), AccountID: account, AssetID: intent.AssetID, Payload: string(payload), State: "PREPARED", CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Minute).Unix(), LastPrice: intent.LastPrice, Fee: intent.Fee, FeeCoin: intent.FeeCoin}
	operation.Digest = vault.ExchangeDigest(operation)
	e.data.Exchange.Operations = append(e.data.Exchange.Operations, operation)
	u.inputs = append(u.inputs, input(syntheticPassword))
	if _, err = e.AuthorizeExchange(id); err == nil {
		t.Fatal("KuCoin unknown KCS fee setting approved an old trade")
	}
	e.data.Exchange.Operations[0].State = "AUTHORIZED"
	e.exchangeAuthorizations = map[string]string{id: operation.Digest}
	request, err := exchangeOperationRequest(operation, time.Now().UnixMilli())
	if err != nil {
		t.Fatal("legacy request fixture invalid", err)
	}
	if _, err = e.ExchangeAuth(request); err == nil {
		t.Fatal("KuCoin unknown KCS fee setting released a write signature")
	}
}
