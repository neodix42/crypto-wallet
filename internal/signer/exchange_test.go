package signer

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/vault"
	"strconv"
	"strings"
	"testing"
	"time"
)

const exchangeTestKey = "PUBLIC_TEST_Bybit_key_39125"
const exchangeTestSecret = "PUBLIC_TEST_ONLY_Bybit_secret_8127354"

func exchangeEngine(t *testing.T) (*Engine, *testUI, model.Account) {
	t.Helper()
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "mainnet")
	var a model.Account
	for _, ac := range e.Snapshot().Accounts {
		if ac.Network == "ethereum-mainnet" {
			a = ac
		}
	}
	if a.ID == "" {
		t.Fatal("missing Ethereum account")
	}
	u.inputs = append(u.inputs, input(syntheticPassword), input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	return e, u, a
}
func exchangeSwap(a model.Account) ExchangeIntent {
	return ExchangeIntent{Kind: "swap", AccountID: a.ID, AssetID: a.Network + ":native", LastPrice: "2500.1234", Fee: "0.001", FeeCoin: "USDT", Order: &bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Market", Qty: "0.012345678912345678", MarketUnit: "baseCoin", SlippageToleranceType: "Percent", SlippageTolerance: "1"}}
}
func exchangeRequest(o vault.ExchangeOperation) bybit.AuthRequest {
	return bybit.AuthRequest{Endpoint: o.Endpoint, Method: "POST", Path: exchangePath(o.Kind), Payload: o.Payload, TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
}
func authorizeExchange(t *testing.T, e *Engine, u *testUI, o vault.ExchangeOperation) vault.ExchangeOperation {
	t.Helper()
	u.inputs = append(u.inputs, input(syntheticPassword))
	o, err := e.AuthorizeExchange(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func assertExchangeHMAC(t *testing.T, req bybit.AuthRequest, got bybit.Auth) {
	t.Helper()
	h := hmac.New(sha256.New, []byte(exchangeTestSecret))
	h.Write([]byte(strconv.FormatInt(req.TimestampMS, 10) + exchangeTestKey + "5000" + req.Payload))
	if got.APIKey != exchangeTestKey || got.Signature != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("Bybit authentication differs from independent HMAC")
	}
}

func TestExchangeTP040TP060CredentialsAndCrashSafeSwap(t *testing.T) {
	e, u, a := exchangeEngine(t)
	status, err := e.ExchangeStatus()
	if err != nil || !status.Connected || status.Endpoint != "https://api.bybit.com" || status.KeySuffix != "9125" {
		t.Fatal("connection status")
	}
	b, _ := json.Marshal(status)
	ordinary, _ := json.Marshal(e.Snapshot())
	for _, secret := range []string{exchangeTestKey, exchangeTestSecret} {
		if bytes.Contains(b, []byte(secret)) || bytes.Contains(ordinary, []byte(secret)) {
			t.Fatal("credential escaped public state")
		}
	}
	for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
		raw, err := e.Files.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(exchangeTestSecret)) || bytes.Contains(raw, []byte(exchangeTestKey)) {
			t.Fatal("plaintext credential on disk")
		}
	}
	read := bybit.AuthRequest{Endpoint: status.Endpoint, Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	auth, err := e.ExchangeAuth(read)
	if err != nil {
		t.Fatal(err)
	}
	assertExchangeHMAC(t, read, auth)
	o, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil || o.State != "PREPARED" || len(o.ID) != 32 || !strings.Contains(o.Payload, o.ID) {
		t.Fatal("durable random exchange ID")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err == nil {
		t.Fatal("unsigned operation authenticated")
	}
	o = authorizeExchange(t, e, u, o)
	if !strings.Contains(u.summaries[len(u.summaries)-1], "0.012345678912345678 ETH") || !strings.Contains(u.summaries[len(u.summaries)-1], "Sell ETHUSDT") || !strings.Contains(u.summaries[len(u.summaries)-1], a.Address) || strings.Contains(u.summaries[len(u.summaries)-1], o.Payload) {
		t.Fatal("approval omitted exact trade/account")
	}
	bad := exchangeRequest(o)
	bad.Payload = strings.Replace(bad.Payload, "0.012345678912345678", "0.012345678912345679", 1)
	if _, err = e.ExchangeAuth(bad); err == nil {
		t.Fatal("changed exact amount authenticated")
	}
	req := exchangeRequest(o)
	auth, err = e.SignExchangeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	assertExchangeHMAC(t, req, auth)
	if _, err = e.ExchangeAuth(req); err == nil {
		t.Fatal("POST authentication replayed")
	}
	stored, _ := e.ExchangeOperation(o.ID)
	if stored.State != "SUBMITTING" {
		t.Fatal("authentication released before SUBMITTING")
	}
	keyBytes, secretBytes := e.data.Exchange.Credentials.APIKey, e.data.Exchange.Credentials.Secret
	e.Lock()
	if !bytes.Equal(keyBytes, make([]byte, len(keyBytes))) || !bytes.Equal(secretBytes, make([]byte, len(secretBytes))) {
		t.Fatal("credential bytes not cleared on lock")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	stored, err = e.ExchangeOperation(o.ID)
	if err != nil || stored.State != "SUBMITTING" {
		t.Fatal("restart lost unresolved request")
	}
	if _, err = e.ExchangeAuth(req); err == nil {
		t.Fatal("restart enabled duplicate POST")
	}
	if _, err = e.PrepareExchange(exchangeSwap(a)); err == nil {
		t.Fatal("ambiguous operation allowed new ID")
	}
	if err = e.UpdateExchange(o.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "CONFIRMED", ""); err == nil {
		t.Fatal("unidentified remote operation called confirmed")
	}
	u.inputs = append(u.inputs, input(syntheticPassword), input("DIFFERENT_PUBLIC_API_KEY", exchangeTestSecret))
	if err = e.ConnectExchange(status.Endpoint); err == nil {
		t.Fatal("different credential namespace replaced pending operation")
	}
	if err = e.UpdateExchange(o.ID, "PENDING", "remote-order-1"); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "CONFIRMED", "wrong-order"); err == nil {
		t.Fatal("changed remote identity accepted")
	}
	if err = e.UpdateExchange(o.ID, "CONFIRMED", "remote-order-1"); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "UNKNOWN", "remote-order-1"); err == nil {
		t.Fatal("terminal operation regressed")
	}
	if _, err = e.ExchangeOperation("unknown"); err == nil {
		t.Fatal("unknown operation")
	}
}

func TestExchangeTP042TransferWithdrawalAndCancellation(t *testing.T) {
	e, u, a := exchangeEngine(t)
	transfer := ExchangeIntent{Kind: "transfer", AccountID: a.ID, AssetID: a.Network + ":native", Transfer: &bybit.TransferRequest{Coin: "USDT", Amount: "12.123456", FromAccountType: "FUND", ToAccountType: "UNIFIED"}}
	o, err := e.PrepareExchange(transfer)
	if err != nil {
		t.Fatal(err)
	}
	var tr bybit.TransferRequest
	json.Unmarshal([]byte(o.Payload), &tr)
	if tr.TransferID != exchangeUUID(o.ID) {
		t.Fatal("transfer ID not durable UUID")
	}
	o = authorizeExchange(t, e, u, o)
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err != nil {
		t.Fatal(err)
	}
	if e.CancelExchange(o.ID) == nil {
		t.Fatal("submitted operation cancelled")
	}
	if err = e.UpdateExchange(o.ID, "FAILED", tr.TransferID); err != nil {
		t.Fatal(err)
	}
	withdraw := ExchangeIntent{Kind: "withdraw", AccountID: a.ID, AssetID: a.Network + ":native", ChainType: "Ethereum(ERC20)", Fee: "0.0001", FeeCoin: "ETH", Withdrawal: &bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "0.1", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	o, err = e.PrepareExchange(withdraw)
	if err != nil {
		t.Fatal(err)
	}
	o = authorizeExchange(t, e, u, o)
	var wr bybit.WithdrawalRequest
	json.Unmarshal([]byte(o.Payload), &wr)
	if wr.Address != a.Address || wr.RequestID != o.ID || wr.Timestamp <= 0 {
		t.Fatal("withdrawal request not bound to owned address")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.DisconnectExchange(); err != nil {
		t.Fatal(err)
	}
	s, _ := e.ExchangeStatus()
	if s.Connected || len(s.Operations) != 2 || s.Operations[1].State != "UNKNOWN" {
		t.Fatal("disconnect erased uncertain withdrawal")
	}
	u.inputs = append(u.inputs, input(syntheticPassword), input(exchangeTestKey, exchangeTestSecret))
	if err = e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(o.ID, "CONFIRMED", "remote-withdraw-1"); err != nil {
		t.Fatal(err)
	}
	for _, authorized := range []bool{false, true} {
		o, err = e.PrepareExchange(exchangeSwap(a))
		if err != nil {
			t.Fatal(err)
		}
		if authorized {
			o = authorizeExchange(t, e, u, o)
		}
		if err = e.CancelExchange(o.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = e.AuthorizeExchange(o.ID); err == nil {
			t.Fatal("cancelled operation reauthorized")
		}
	}
	if err = e.CancelExchange("missing"); err == nil {
		t.Fatal("missing cancellation accepted")
	}
}

func TestExchangeTP042MalformedPlansAndUntrustedNetworkMetadata(t *testing.T) {
	e, _, a := exchangeEngine(t)
	base := exchangeSwap(a)
	for _, mutate := range []func(*ExchangeIntent){
		func(i *ExchangeIntent) { i.Kind = "arbitrary" }, func(i *ExchangeIntent) { i.AccountID = "foreign" }, func(i *ExchangeIntent) { i.AssetID = "ethereum-mainnet:erc20:foreign" }, func(i *ExchangeIntent) { i.LastPrice = "NaN" }, func(i *ExchangeIntent) { i.Fee = "-1" }, func(i *ExchangeIntent) { i.FeeCoin = "BTC" }, func(i *ExchangeIntent) { i.Order = nil }, func(i *ExchangeIntent) { i.Transfer = &bybit.TransferRequest{} }, func(i *ExchangeIntent) { i.Order.Symbol = "TONUSDT" }, func(i *ExchangeIntent) { i.Order.IsLeverage = 1 }, func(i *ExchangeIntent) { i.Order.SlippageTolerance = "1.01" }, func(i *ExchangeIntent) { i.Order.Side = "Buy" },
	} {
		raw, _ := json.Marshal(base)
		var in ExchangeIntent
		json.Unmarshal(raw, &in)
		mutate(&in)
		if _, err := e.PrepareExchange(in); err == nil {
			t.Fatal("malformed/changed economic intent accepted")
		}
	}
	for _, in := range []ExchangeIntent{
		{Kind: "transfer", AccountID: a.ID, AssetID: base.AssetID},
		{Kind: "transfer", AccountID: a.ID, AssetID: base.AssetID, Transfer: &bybit.TransferRequest{Coin: "BTC", Amount: "1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}},
		{Kind: "withdraw", AccountID: a.ID, AssetID: base.AssetID},
	} {
		if _, err := e.PrepareExchange(in); err == nil {
			t.Fatal("missing/foreign request accepted")
		}
	}
	w := bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "1", Timestamp: 1, ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: strings.Repeat("a", 32)}
	asset := model.Asset{ID: a.Network + ":native", Symbol: "ETH"}
	if !exchangeWithdrawalNetwork(a, asset, w, "Ethereum(ERC20)", "") {
		t.Fatal("valid network rejected")
	}
	for _, change := range []func(*bybit.WithdrawalRequest){func(w *bybit.WithdrawalRequest) { w.Coin = "USDT" }, func(w *bybit.WithdrawalRequest) { w.Address = "foreign" }, func(w *bybit.WithdrawalRequest) { w.Tag = "memo" }, func(w *bybit.WithdrawalRequest) { w.Chain = "BSC" }} {
		bad := w
		change(&bad)
		if exchangeWithdrawalNetwork(a, asset, bad, "Ethereum(ERC20)", "") {
			t.Fatal("wrong withdrawal destination/network accepted")
		}
	}
	if exchangeWithdrawalNetwork(a, asset, w, "BNB Smart Chain", "") || exchangeWithdrawalNetwork(a, asset, w, "Ethereum(ERC20)", "foreign") {
		t.Fatal("untrusted chain/token metadata accepted")
	}
	for network, names := range map[string][]string{"base-mainnet": {"Base", "BASE"}, "horizen-mainnet": {"Horizen", "Horizen Chain"}, "hyperevm-mainnet": {"HyperEVM", "Hyperliquid HyperEVM"}, "near-mainnet": {"NEAR", "NEAR Protocol", "Near Protocol"}, "aptos-mainnet": {"APTOS", "Aptos"}, "ton-mainnet": {"TON", "Toncoin", "TON (The Open Network)"}} {
		ac := a
		ac.Network = network
		asset.ID = network + ":native"
		if network == "ton-mainnet" {
			asset.ID = "ton-mainnet:TON"
		}
		for _, name := range names {
			if !exchangeWithdrawalNetwork(ac, asset, w, name, "") {
				t.Fatal("known ledger label rejected")
			}
		}
	}
	asset.ID = a.Network + ":native"
	asset.Contract = "0xAbCd"
	if !exchangeWithdrawalNetwork(a, asset, w, "Ethereum", "0xaBcD") || exchangeWithdrawalNetwork(a, asset, w, "Ethereum", "0x1111") {
		t.Fatal("token contract binding")
	}
	x := e.data.Exchange
	x.Operations = make([]vault.ExchangeOperation, 1000)
	if _, err := e.PrepareExchange(base); err == nil {
		t.Fatal("unbounded remote journal")
	}
}

func TestExchangeTP042ActualTONRegistryOwnedWithdrawalAuthorization(t *testing.T) {
	adapter := ton.New()
	e, u, _ := newEngine(t, []model.Adapter{adapter})
	createImported(t, e, u, "mainnet")
	var account model.Account
	for _, a := range e.Snapshot().Accounts {
		if a.Network == "ton-mainnet" {
			account = a
		}
	}
	var asset model.Asset
	for _, network := range adapter.Networks() {
		if network.ID == account.Network {
			for _, item := range network.Assets {
				if item.Symbol == "TON" {
					asset = item
				}
			}
		}
	}
	if account.ID == "" || asset.ID != "ton-mainnet:TON" || adapter.ValidateAddress(account.Network, account.Address) != nil {
		t.Fatal("actual TON wallet/asset identity unavailable")
	}
	u.inputs = append(u.inputs, input(syntheticPassword), input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	in := ExchangeIntent{Kind: "withdraw", AccountID: account.ID, AssetID: asset.ID, ChainType: "TON", Fee: "0.001", FeeCoin: "TON", Withdrawal: &bybit.WithdrawalRequest{Coin: "TON", Chain: "TON", Address: account.Address, Amount: "0.123456789", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	fake := in
	fake.AssetID = "ton-mainnet:native"
	if _, err := e.PrepareExchange(fake); err == nil {
		t.Fatal("invented TON native asset identity accepted")
	}
	op, err := e.PrepareExchange(in)
	if err != nil {
		t.Fatal("actual TON route could not prepare")
	}
	op = authorizeExchange(t, e, u, op)
	var request bybit.WithdrawalRequest
	if json.Unmarshal([]byte(op.Payload), &request) != nil || request.Coin != "TON" || request.Chain != "TON" || request.Address != account.Address || request.Amount != "0.123456789" || request.RequestID != op.ID {
		t.Fatal("actual TON withdrawal changed its owned destination or exact amount")
	}
	authRequest := exchangeRequest(op)
	auth, err := e.ExchangeAuth(authRequest)
	if err != nil {
		t.Fatal("actual TON route could not authorize exact API request")
	}
	assertExchangeHMAC(t, authRequest, auth)
	stored, err := e.ExchangeOperation(op.ID)
	if err != nil || stored.State != "SUBMITTING" || stored.AssetID != asset.ID {
		t.Fatal("actual TON authorization lacked durable submission identity")
	}
	if _, err := e.ExchangeAuth(authRequest); err == nil {
		t.Fatal("actual TON request authenticated twice")
	}
}

type exchangeHookUI struct {
	*testUI
	confirmHook func()
	inputHook   func()
}

func (u *exchangeHookUI) Confirm(title, summary string) bool {
	ok := u.testUI.Confirm(title, summary)
	if u.confirmHook != nil {
		u.confirmHook()
	}
	return ok
}
func (u *exchangeHookUI) Input(title string, labels ...string) ([][]byte, error) {
	v, e := u.testUI.Input(title, labels...)
	if u.inputHook != nil {
		u.inputHook()
	}
	return v, e
}

func TestExchangeTP043AuthorizationFailuresLockAndPersistence(t *testing.T) {
	for _, scenario := range []string{"cancel", "wrong-password", "missing-password", "lock-confirm", "lock-password", "expired", "mutated", "persist"} {
		t.Run(scenario, func(t *testing.T) {
			e, u, a := exchangeEngine(t)
			o, err := e.PrepareExchange(exchangeSwap(a))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "cancel":
				u.approve = false
			case "wrong-password":
				u.inputs = append(u.inputs, input("wrong"))
			case "missing-password":
			case "lock-confirm":
				e.UI = &exchangeHookUI{testUI: u, confirmHook: e.Lock}
			case "lock-password":
				u.inputs = append(u.inputs, input(syntheticPassword))
				e.UI = &exchangeHookUI{testUI: u, inputHook: e.Lock}
			case "expired":
				e.data.Exchange.Operations[0].ExpiresAt = time.Now().Unix() - 1
			case "mutated":
				e.data.Exchange.Operations[0].Payload = strings.Replace(o.Payload, "ETHUSDT", "TONUSDT", 1)
			case "persist":
				u.inputs = append(u.inputs, input(syntheticPassword))
				e.UI = &exchangeHookUI{testUI: u, inputHook: func() { e.parameters = vault.Parameters{} }}
			}
			if out, err := e.AuthorizeExchange(o.ID); err == nil || out.ID != "" {
				t.Fatal("rejected authorization released operation")
			}
			if auth, err := e.ExchangeAuth(exchangeRequest(o)); err == nil || auth.Signature != "" {
				t.Fatal("rejected operation signed")
			}
		})
	}
}

func TestExchangeTP040CredentialValidationAndReadOnlyRequestBounds(t *testing.T) {
	e, u, a := exchangeEngine(t)
	for _, req := range []bybit.AuthRequest{{Endpoint: "https://attacker.invalid", Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}, {Endpoint: "https://api.bybit.com", Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().Add(-time.Minute).UnixMilli(), ReceiveWindowMS: 5000}, {Endpoint: "https://api.bybit.com", Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().Add(time.Minute).UnixMilli(), ReceiveWindowMS: 5000}} {
		if _, err := e.ExchangeAuth(req); err == nil {
			t.Fatal("wrong endpoint/time authenticated")
		}
	}
	if e.ConnectExchange("https://attacker.invalid") == nil {
		t.Fatal("arbitrary credential destination")
	}
	for _, values := range [][][]byte{input("bad"), input(syntheticPassword)} {
		u.inputs = append(u.inputs, values)
		if e.ConnectExchange("https://api.bybit.com") == nil {
			t.Fatal("missing credentials/password accepted")
		}
	}
	for _, pair := range [][]string{{"short", exchangeTestSecret}, {exchangeTestKey, "short"}, {"key with space", exchangeTestSecret}, {exchangeTestKey, "secret\x00CANARY123456789"}, {exchangeTestKey, "secret with space 123456789"}} {
		u.inputs = append(u.inputs, input(syntheticPassword), input(pair...))
		if e.ConnectExchange("https://api.bybit.com") == nil {
			t.Fatal("malformed credential accepted")
		}
	}
	u.approve = false
	u.inputs = append(u.inputs, input(syntheticPassword), input(exchangeTestKey, exchangeTestSecret))
	if e.ConnectExchange("https://api.bybit.com") == nil {
		t.Fatal("cancelled custody approval accepted")
	}
	u.approve = true
	u.inputs = append(u.inputs, input("wrong"))
	if e.DisconnectExchange() == nil {
		t.Fatal("wrong-password disconnect")
	}
	o, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	o = authorizeExchange(t, e, u, o)
	u.inputs = append(u.inputs, input(syntheticPassword))
	if _, err = e.AuthorizeExchange(second.ID); err == nil {
		t.Fatal("parallel authorized operation")
	}
	if _, err = e.PrepareExchange(exchangeSwap(a)); err == nil {
		t.Fatal("new prepare during authorization")
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	if e.DisconnectExchange() != nil {
		t.Fatal("disconnect")
	}
	for _, op := range e.data.Exchange.Operations {
		if op.State != "CANCELLED" {
			t.Fatal("disconnect retained unsigned authorization")
		}
	}
	if _, err = e.PrepareExchange(exchangeSwap(a)); err == nil {
		t.Fatal("disconnected preparation")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err == nil {
		t.Fatal("disconnected signing")
	}
	if e.UpdateExchange("missing", "UNKNOWN", "") == nil {
		t.Fatal("missing update")
	}
	u.inputs = nil
	if e.exchangePassword("none") == nil {
		t.Fatal("missing password")
	}
	e.Lock()
	if _, err = e.ExchangeStatus(); err == nil {
		t.Fatal("locked status revealed encrypted history")
	}
	if e.ConnectExchange("https://api.bybit.com") == nil || e.DisconnectExchange() == nil || e.CancelExchange(o.ID) == nil {
		t.Fatal("locked credential mutation")
	}
	if exchangePath("arbitrary") != "" {
		t.Fatal("arbitrary path")
	}
	if exchangeCredentialBytes(make([]byte, 513), false) {
		t.Fatal("credential allocation bound")
	}
}

func TestExchangeTP060FailedDurableSubmissionReturnsNoSignature(t *testing.T) {
	e, u, a := exchangeEngine(t)
	o, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	o = authorizeExchange(t, e, u, o)
	e.parameters = vault.Parameters{}
	if auth, err := e.ExchangeAuth(exchangeRequest(o)); err == nil || auth.APIKey != "" || auth.Signature != "" {
		t.Fatal("undurable authentication escaped")
	}
	if !e.Snapshot().Locked || !e.persistFailed {
		t.Fatal("persistence failure did not quarantine credentials")
	}
}

func TestExchangeTP040StatusIsBoundedAndCannotMutateHistory(t *testing.T) {
	e, u, a := exchangeEngine(t)
	o, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		p := o
		p.ID = ID()
		p.State = "CANCELLED"
		e.data.Exchange.Operations = append(e.data.Exchange.Operations, p)
	}
	e.data.Exchange.Operations[0].State = "UNKNOWN"
	s, err := e.ExchangeStatus()
	if err != nil || len(s.Operations) != 51 || s.OperationCount != 101 {
		t.Fatal("bounded history omitted unresolved/too many rows")
	}
	s.Operations[0].State = "CONFIRMED"
	if e.data.Exchange.Operations[0].State != "UNKNOWN" {
		t.Fatal("public status aliased authority")
	}
	// Avoid retaining the synthetic malformed journal: this test only examines
	// the public page and never persists the deliberately repeated digests.
	e.data.Exchange.Operations = e.data.Exchange.Operations[:1]
	e.data.Exchange.Operations[0].State = "CANCELLED"
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.DisconnectExchange(); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeTP043UnlockInvalidatesUnsignedExchangeAuthorizations(t *testing.T) {
	e, u, a := exchangeEngine(t)
	prepared, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	authorized := authorizeExchange(t, e, u, prepared)
	req := exchangeRequest(authorized)
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ExchangeAuth(req); err == nil {
		t.Fatal("authorization survived lock/restart")
	}
	op, err := e.ExchangeOperation(prepared.ID)
	if err != nil || op.State != "CANCELLED" {
		t.Fatal("unsigned authorization not cancelled durably")
	}
	pending, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	op, err = e.ExchangeOperation(pending.ID)
	if err != nil || op.State != "CANCELLED" {
		t.Fatal("unapproved prepared intent survived restart")
	}
}

func TestExchangeTP042CurrentOwnedWithdrawalDescriptorMustStillMatch(t *testing.T) {
	e, u, a := exchangeEngine(t)
	in := ExchangeIntent{Kind: "withdraw", AccountID: a.ID, AssetID: a.Network + ":native", ChainType: "Ethereum", Withdrawal: &bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "0.2", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	o, err := e.PrepareExchange(in)
	if err != nil {
		t.Fatal(err)
	}
	o = authorizeExchange(t, e, u, o)
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == a.ID {
			e.data.Accounts[i].Address = "0x1111111111111111111111111111111111111111"
		}
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err == nil {
		t.Fatal("withdrawal authenticated after owned address changed")
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == a.ID {
			e.data.Accounts[i].Address = a.Address
		}
	}
	if _, err = e.ExchangeAuth(exchangeRequest(o)); err != nil {
		t.Fatal("unchanged owned address rejected")
	}
}

func TestExchangeTP042ApprovalInterpretsTheExactCanonicalOperation(t *testing.T) {
	e, _, a := exchangeEngine(t)
	asset := model.Asset{ID: a.Network + ":native", Symbol: "ETH"}
	base, err := e.PrepareExchange(exchangeSwap(a))
	if err != nil {
		t.Fatal(err)
	}
	if summary, err := exchangeApproval(base, a, asset); err != nil || !strings.Contains(summary, "Maximum slippage: 1%") || strings.Contains(summary, "{\"category\"") {
		t.Fatal("trade approval semantics")
	}
	var buy bybit.SpotOrder
	json.Unmarshal([]byte(base.Payload), &buy)
	buy.Side = "Buy"
	buy.MarketUnit = "quoteCoin"
	buy.Qty = "123.123456"
	raw, _ := json.Marshal(buy)
	op := base
	op.Payload = string(raw)
	if summary, err := exchangeApproval(op, a, asset); err != nil || !strings.Contains(summary, "123.123456 USDT") {
		t.Fatal("market buy displayed wrong spending currency")
	}
	for _, mutate := range []func(*vault.ExchangeOperation){func(o *vault.ExchangeOperation) { o.Payload = "{" }, func(o *vault.ExchangeOperation) { o.Payload = strings.Replace(o.Payload, "ETHUSDT", "TONUSDT", 1) }, func(o *vault.ExchangeOperation) { o.ID = ID() }} {
		bad := base
		mutate(&bad)
		if _, err := exchangeApproval(bad, a, asset); err == nil {
			t.Fatal("foreign/malformed order interpreted")
		}
	}
	tr := bybit.TransferRequest{TransferID: exchangeUUID(base.ID), Coin: "USDT", Amount: "12.34", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
	raw, _ = json.Marshal(tr)
	op = base
	op.Kind = "transfer"
	op.Payload = string(raw)
	if summary, err := exchangeApproval(op, a, asset); err != nil || !strings.Contains(summary, "From account: FUND") || !strings.Contains(summary, "To account: UNIFIED") {
		t.Fatal("transfer account semantics")
	}
	for _, coin := range []string{"BTC", "USDT"} {
		bad := op
		tr.Coin = coin
		if coin == "USDT" {
			tr.TransferID = exchangeUUID(ID())
		}
		raw, _ = json.Marshal(tr)
		bad.Payload = string(raw)
		if _, err := exchangeApproval(bad, a, asset); err == nil {
			t.Fatal("foreign transfer interpreted")
		}
	}
	w := bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: a.Address, Amount: "1.23456789", Timestamp: time.Now().UnixMilli(), ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: base.ID}
	raw, _ = json.Marshal(w)
	op = base
	op.Kind = "withdraw"
	op.ChainType = "Ethereum"
	op.Payload = string(raw)
	op.Fee = "0.00056789"
	op.FeeCoin = "ETH"
	if summary, err := exchangeApproval(op, a, asset); err != nil || !strings.Contains(summary, "Estimated amount received: 1.234 ETH") || !strings.Contains(summary, "Total debit including withdrawal fee: 1.23456789 ETH") {
		t.Fatal("withdrawal fee arithmetic or semantics")
	}
	unknown := op
	unknown.Fee = ""
	if summary, err := exchangeApproval(unknown, a, asset); err != nil || !strings.Contains(summary, "Estimated amount received: unknown") {
		t.Fatal("missing fee invented recipient amount")
	}
	for _, fee := range []string{"NaN", "2"} {
		bad := op
		bad.Fee = fee
		if _, err := exchangeApproval(bad, a, asset); err == nil {
			t.Fatal("invalid/over-gross withdrawal fee")
		}
	}
	for _, mutate := range []func(*bybit.WithdrawalRequest){func(w *bybit.WithdrawalRequest) { w.RequestID = ID() }, func(w *bybit.WithdrawalRequest) { w.Address = "foreign" }} {
		bad := op
		copy := w
		mutate(&copy)
		raw, _ = json.Marshal(copy)
		bad.Payload = string(raw)
		if _, err := exchangeApproval(bad, a, asset); err == nil {
			t.Fatal("withdrawal changed since prepare")
		}
	}
}
