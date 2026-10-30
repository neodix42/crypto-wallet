package signer

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

// The on-chain asset ID and prior remote-operation bytes are durable identities.
// A display/market rename must not rewrite an old TON order into a GRAM order.
func TestGRAMRenamePreservesEncryptedLegacyTONOperationsAndReadOnlyLookup(t *testing.T) {
	for _, provider := range []string{"bybit", "binance", "kucoin"} {
		t.Run(provider, func(t *testing.T) {
			e, ui, _ := newEngine(t, []model.Adapter{ton.New()})
			createImported(t, e, ui, "mainnet")
			account := findAccount(t, e, "ton-mainnet")
			endpoint := "https://api." + provider + ".com"
			credential := &vault.ExchangeCredentials{Provider: provider, Endpoint: endpoint, APIKey: []byte("PUBLIC_TEST_ONLY_KEY_91825"), Secret: []byte("PUBLIC_TEST_ONLY_SECRET_849256324")}
			if provider == "kucoin" {
				credential.Passphrase = []byte("PUBLIC_ONLY_PASSPHRASE_927364")
				credential.KeyVersion = 3
			}
			e.data.Exchange = &vault.ExchangeData{SelectedProvider: provider}
			e.data.Exchange.SetCredential(provider, credential)
			now := time.Now().Unix()
			for i, kind := range []string{"swap", "transfer", "withdraw"} {
				id := fmt.Sprintf("%032x", i+1)
				var payload any
				switch kind {
				case "swap":
					payload = bybit.SpotOrder{Category: "spot", Symbol: "TONUSDT", Side: "Sell", OrderType: "Market", Qty: "1", MarketUnit: "baseCoin", SlippageToleranceType: "Percent", SlippageTolerance: "0.5", OrderLinkID: id}
				case "transfer":
					payload = bybit.TransferRequest{TransferID: exchangeUUID(id), Coin: "TON", Amount: "1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
				case "withdraw":
					payload = bybit.WithdrawalRequest{Coin: "TON", Chain: "TON", Address: account.Address, Amount: "1", ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: id}
				}
				raw, _ := json.Marshal(payload)
				operation := vault.ExchangeOperation{ID: id, Kind: kind, Endpoint: endpoint, CredentialID: vault.ExchangeCredentialID(credential), AccountID: account.ID, AssetID: "ton-mainnet:TON", Payload: string(raw), State: "UNKNOWN", CreatedAt: now, ExpiresAt: now + 300, ExternalID: "public-original-remote-" + id, FeeCoin: "TON", ChainType: "TON"}
				operation.Digest = vault.ExchangeDigest(operation)
				e.data.Exchange.Operations = append(e.data.Exchange.Operations, operation)
			}
			before := append([]vault.ExchangeOperation(nil), e.data.Exchange.Operations...)
			if err := e.persist(); err != nil {
				t.Fatal(err)
			}
			e.Lock()
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal("market rename prevented legacy vault unlock", err)
			}
			status, err := e.ExchangeStatus()
			if err != nil || !reflect.DeepEqual(status.Operations, before) {
				t.Fatal("rename rewrote old payload, digest, state or provider identity", err)
			}
			stamp := time.Now().UnixMilli()
			query := url.Values{}
			request := bybit.AuthRequest{Endpoint: endpoint, Method: "GET", TimestampMS: stamp, ReceiveWindowMS: 5000}
			switch provider {
			case "bybit":
				request.Path = "/v5/order/realtime"
				query = url.Values{"category": {"spot"}, "symbol": {"TONUSDT"}, "orderLinkId": {before[0].ID}}
			case "binance":
				request.Path = "/api/v3/order"
				query = url.Values{"symbol": {"TONUSDT"}, "origClientOrderId": {before[0].ID}}
			case "kucoin":
				request.Path = "/api/v1/hf/orders/client-order/" + before[0].ID
				query = url.Values{"symbol": {"TON-USDT"}}
			}
			request.Payload = query.Encode()
			if auth, err := e.ExchangeAuth(request); err != nil || auth.Signature == "" {
				t.Fatal("original TON read-only lookup was lost", err)
			}
			if !reflect.DeepEqual(before, e.data.Exchange.Operations) {
				t.Fatal("read-only legacy lookup changed original operation")
			}
			if _, err = e.AuthorizeExchange(before[0].ID); err == nil {
				t.Fatal("UNKNOWN legacy operation was reauthorized")
			}
			for _, operation := range before {
				if operation.State != "UNKNOWN" || strings.Contains(operation.Payload, "GRAM") {
					t.Fatal("legacy request was automatically replaced")
				}
			}
		})
	}
}
func TestNewGRAMOperationsRejectRetiredTONMarketAndCoinAliases(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, e, ui, "mainnet")
	account := findAccount(t, e, "ton-mainnet")
	ui.inputs = append(ui.inputs, input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"swap", "transfer", "withdraw"} {
		in := ExchangeIntent{Kind: kind, AccountID: account.ID, AssetID: "ton-mainnet:TON"}
		switch kind {
		case "swap":
			in.Order = &bybit.SpotOrder{Category: "spot", Symbol: "TONUSDT", Side: "Sell", OrderType: "Market", Qty: "1", MarketUnit: "baseCoin", SlippageToleranceType: "Percent", SlippageTolerance: "0.5"}
		case "transfer":
			in.Transfer = &bybit.TransferRequest{Coin: "TON", Amount: "1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
		case "withdraw":
			in.ChainType = "TON"
			in.Withdrawal = &bybit.WithdrawalRequest{Coin: "TON", Chain: "TON", Address: account.Address, Amount: "1", ForceChain: 1, AccountType: "FUND", FeeType: 1}
		}
		if _, err := e.PrepareExchange(in); err == nil || len(e.data.Exchange.Operations) != 0 {
			t.Fatal("retired TON alias granted a new operation", kind)
		}
	}
	var native model.Asset
	for _, network := range ton.New().Networks() {
		if network.ID == account.Network {
			native = network.Assets[0]
		}
	}
	if native.ID != "ton-mainnet:TON" || native.Symbol != "GRAM" || native.Decimals != 9 {
		t.Fatal("GRAM rename changed underlying identity/precision")
	}
}
