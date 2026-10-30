package binance

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func authRequest(method, path, payload string) bybit.AuthRequest {
	return bybit.AuthRequest{Endpoint: Endpoint, Method: method, Path: path, Payload: payload, TimestampMS: 1700000000000, ReceiveWindowMS: 5000}
}
func TestExactNativeOperationReconstructionRejectsAdditionalAuthority(t *testing.T) {
	for _, f := range []struct {
		kind, path string
		value      any
	}{{"swap", "/api/v3/order", spotOrder()}, {"transfer", "/sapi/v1/asset/transfer", transfer()}, {"withdraw", "/sapi/v1/capital/withdraw/apply", withdrawal()}} {
		b, _ := json.Marshal(f.value)
		r, e := OperationRequest(Endpoint, f.kind, string(b), 1700000000000)
		if e != nil || r.Path != f.path || r.Method != "POST" || IsReadOnly(r) || ValidateAuthRequest(r) != nil {
			t.Fatal("valid durable request rejected", r, e)
		}
		q, _ := url.ParseQuery(r.Payload)
		if q.Has("timestamp") || q.Has("recvWindow") || q.Has("signature") {
			t.Fatal("auth data became business authority")
		}
		for _, bad := range []string{`null`, `{}`, string(b) + ` {}`, `{"unexpected":"authority",` + string(b)[1:], strings.Replace(string(b), `"amount":`, `"amount":null,"amount":`, 1), " " + string(b), strings.Replace(string(b), `"category":"spot"`, `"category":null`, 1)} {
			if bad == string(b) {
				continue
			}
			if _, e := OperationRequest(Endpoint, f.kind, bad, 1700000000000); e == nil {
				t.Fatal("noncanonical/extra durable payload accepted", bad)
			}
		}
		if _, e := OperationRequest(Endpoint+"/", f.kind, string(b), 1700000000000); e == nil {
			t.Fatal("endpoint mismatch")
		}
		if _, e := OperationRequest(Endpoint, f.kind, string(b), 0); e == nil {
			t.Fatal("timestamp mismatch")
		}
	}
	if _, e := OperationRequest(Endpoint, "arbitrary", `{}`, 1); e == nil {
		t.Fatal("new operation kind")
	}
	for _, change := range []func(*bybit.SpotOrder){
		func(o *bybit.SpotOrder) { o.Side = "Margin" }, func(o *bybit.SpotOrder) { o.OrderType = "Market" }, func(o *bybit.SpotOrder) { o.Category = "linear" }, func(o *bybit.SpotOrder) { o.IsLeverage = 1 }, func(o *bybit.SpotOrder) { o.MarketUnit = "quoteCoin" }, func(o *bybit.SpotOrder) { o.Symbol = "ETHBTC" }, func(o *bybit.SpotOrder) { o.Qty = "0" }, func(o *bybit.SpotOrder) { o.BaseQty = "0.2" }, func(o *bybit.SpotOrder) { o.BaseQty = "0.09" }, func(o *bybit.SpotOrder) { o.LimitPrice = "0" }, func(o *bybit.SpotOrder) { o.OrderLinkID = "bad/id" }, func(o *bybit.SpotOrder) { o.SlippageToleranceType = "TickSize" }, func(o *bybit.SpotOrder) { o.SlippageTolerance = "0.001" }, func(o *bybit.SpotOrder) { o.SlippageTolerance = "10.01" },
	} {
		o := spotOrder()
		change(&o)
		b, _ := json.Marshal(o)
		if _, e := OperationRequest(Endpoint, "swap", string(b), 1); e == nil {
			t.Fatal("invalid order accepted", o)
		}
	}
	buy := spotOrder()
	buy.Side = "Buy"
	buy.MarketUnit = "quoteCoin"
	buy.Qty = "200"
	b, _ := json.Marshal(buy)
	if _, e := OperationRequest(Endpoint, "swap", string(b), 1); e != nil {
		t.Fatal("exact buy spend bound rejected", e)
	}
	buy.Qty = "199.999999999999999999"
	b, _ = json.Marshal(buy)
	if _, e := OperationRequest(Endpoint, "swap", string(b), 1); e == nil {
		t.Fatal("buy exceeded original quote budget")
	}
	for _, change := range []func(*bybit.WithdrawalRequest){func(w *bybit.WithdrawalRequest) { w.FeeType = 1 }, func(w *bybit.WithdrawalRequest) { w.FeeMode = "exclusive" }, func(w *bybit.WithdrawalRequest) { w.AccountType = "UNIFIED" }, func(w *bybit.WithdrawalRequest) { w.ForceChain = 0 }, func(w *bybit.WithdrawalRequest) { w.Timestamp = 0 }, func(w *bybit.WithdrawalRequest) { w.Beneficiary = &bybit.UAEBeneficiary{} }, func(w *bybit.WithdrawalRequest) { w.RequestID = "../replay" }, func(w *bybit.WithdrawalRequest) { w.Address = "x\nHeader" }, func(w *bybit.WithdrawalRequest) { w.Tag = "\x00" }} {
		w := withdrawal()
		change(&w)
		b, _ := json.Marshal(w)
		if _, e := OperationRequest(Endpoint, "withdraw", string(b), 1); e == nil {
			t.Fatal("Bybit/native withdrawal policy confusion")
		}
	}
	w := withdrawal()
	w.Tag = "12345"
	b, _ = json.Marshal(w)
	r, e := OperationRequest(Endpoint, "withdraw", string(b), 1)
	if e != nil || !strings.Contains(r.Payload, "addressTag=12345") || strings.Contains(r.Payload, "transactionFeeFlag") {
		t.Fatal("exact memo or fee semantics changed", e)
	}
}
func TestAuthenticationAllowlistRejectsEncodingPathsAndDuplicatedParameters(t *testing.T) {
	queries := []bybit.AuthRequest{
		authRequest("GET", "/sapi/v1/account/apiRestrictions", ""), authRequest("GET", "/sapi/v1/capital/config/getall", ""), authRequest("GET", "/api/v3/account", ""), authRequest("GET", "/sapi/v1/localentity/questionnaire-requirements", ""),
		authRequest("GET", "/sapi/v1/capital/deposit/address", "coin=ETH&network=ETH"), authRequest("GET", "/api/v3/exchangeInfo", "symbol=ETHUSDT"), authRequest("GET", "/api/v3/ticker/24hr", "symbol=ETHUSDT"), authRequest("GET", "/api/v3/account/commission", "symbol=ETHUSDT"), authRequest("GET", "/api/v3/order", "origClientOrderId=fixtureOrder1&symbol=ETHUSDT"), authRequest("GET", "/api/v3/myTrades", "limit=100&orderId=123&symbol=ETHUSDT"), authRequest("GET", "/api/v3/myTrades", "fromId=0&limit=100&orderId=123&symbol=ETHUSDT"), authRequest("GET", "/sapi/v1/asset/transfer", "current=1&size=100&type=MAIN_FUNDING"), authRequest("GET", "/sapi/v1/capital/withdraw/history", "coin=ETH&idList=remoteWithdrawal1&limit=50&offset=0"), authRequest("GET", "/sapi/v1/capital/deposit/hisrec", "coin=ETH&limit=50&offset=0&txId=0xabcdef"), authRequest("POST", "/sapi/v1/asset/get-funding-asset", "asset=ETH&needBtcValuation=false"),
	}
	for _, r := range queries {
		if ValidateAuthRequest(r) != nil || !IsReadOnly(r) {
			t.Fatal("valid read request rejected", r)
		}
		for _, alter := range []func(*bybit.AuthRequest){func(r *bybit.AuthRequest) { r.Endpoint = "https://api.binance.us" }, func(r *bybit.AuthRequest) { r.Method = "DELETE" }, func(r *bybit.AuthRequest) { r.Path += "/" }, func(r *bybit.AuthRequest) { r.TimestampMS = 0 }, func(r *bybit.AuthRequest) { r.ReceiveWindowMS = 60000 }, func(r *bybit.AuthRequest) {
			q, _ := url.ParseQuery(r.Payload)
			q.Set("timestamp", "1")
			r.Payload = q.Encode()
		}, func(r *bybit.AuthRequest) {
			q, _ := url.ParseQuery(r.Payload)
			q.Set("signature", "abc")
			r.Payload = q.Encode()
		}, func(r *bybit.AuthRequest) { r.Payload += "&extra=authority" }, func(r *bybit.AuthRequest) { r.Payload = strings.Repeat("x", 8193) }} {
			bad := r
			alter(&bad)
			if ValidateAuthRequest(bad) == nil || IsReadOnly(bad) {
				t.Fatal("expanded native auth authority", bad)
			}
		}
	}
	for _, payload := range []string{"symbol=ETHUSDT&symbol=BTCUSDT", "symbol=%45THUSDT", "symbol=ETHUSDT%00", "symbol=ETHUSDT;foo=bar", "symbol=", "symbol=ETHBTC"} {
		if ValidateAuthRequest(authRequest("GET", "/api/v3/ticker/24hr", payload)) == nil {
			t.Fatal("noncanonical query", payload)
		}
	}
	for _, payload := range []string{"amount=1&asset=ETH&type=MAIN_UMFUTURE", "amount=1&asset=ETH&type=MAIN_FUNDING&transferId=chosen", "amount=0&asset=ETH&type=MAIN_FUNDING"} {
		if ValidateAuthRequest(authRequest("POST", "/sapi/v1/asset/transfer", payload)) == nil {
			t.Fatal("unsupported transfer authority")
		}
	}
	q, _ := orderQuery(spotOrder())
	valid := q.Encode()
	for _, change := range []func(url.Values){func(q url.Values) { q.Set("type", "MARKET") }, func(q url.Values) { q.Set("timeInForce", "GTC") }, func(q url.Values) { q.Set("quantity", "0") }, func(q url.Values) { q.Set("price", "0") }, func(q url.Values) { q.Set("quoteOrderQty", "100") }, func(q url.Values) { q.Set("newClientOrderId", "../x") }, func(q url.Values) { q.Set("side", "MARGIN") }} {
		v, _ := url.ParseQuery(valid)
		change(v)
		if ValidateAuthRequest(authRequest("POST", "/api/v3/order", v.Encode())) == nil {
			t.Fatal("order authority expanded", v)
		}
	}
}
func TestHMACFixedVectorAndSecretBoundaries(t *testing.T) {
	r := authRequest("GET", "/api/v3/order", "origClientOrderId=fixtureOrder1&symbol=ETHUSDT")
	a, e := HMAC(fixtureKey, []byte(fixtureSecret), r)
	if e != nil {
		t.Fatal(e)
	}
	// Independently calculated with Python hmac/sha256 over this exact canonical
	// URL-encoded query; no API key participates in Binance's signed message.
	const expected = "1995144cb99824921ca27472a8c6355e47b90392d81d8e062e559ea39c469f52"
	if a.Signature != expected {
		t.Fatal("signature vector disagreement", a.Signature)
	}
	if a.APIKey != fixtureKey || a.Passphrase != "" || a.KeyVersion != 0 {
		t.Fatal("unexpected secret projection")
	}
	for _, key := range []string{"", "bad\nHeader", strings.Repeat("x", 129)} {
		if _, e := HMAC(key, []byte(fixtureSecret), r); e == nil {
			t.Fatal("bad API header")
		}
	}
	for _, secret := range [][]byte{nil, make([]byte, 15), make([]byte, 513)} {
		if _, e := HMAC(fixtureKey, secret, r); e == nil {
			t.Fatal("unbounded secret")
		}
	}
	r.Path = "/sapi/v1/userDataStream"
	if _, e := HMAC(fixtureKey, []byte(fixtureSecret), r); e == nil {
		t.Fatal("arbitrary request signed")
	}
}
