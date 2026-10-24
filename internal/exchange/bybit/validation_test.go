package bybit

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func readRequest(path, payload string) AuthRequest {
	return AuthRequest{Endpoint: "https://api.bybit.com", Method: "GET", Path: path, Payload: payload, TimestampMS: 1700000000000, ReceiveWindowMS: 5000}
}
func postRequest(path string, value any) AuthRequest {
	b, _ := json.Marshal(value)
	r := readRequest(path, string(b))
	r.Method = "POST"
	return r
}

func TestIndependentPythonHMACVector(t *testing.T) {
	// Expected independently calculated with Python 3 hashlib/hmac following the
	// official V5 timestamp+apiKey+recvWindow+queryString specification.
	r := readRequest("/v5/market/tickers", "category=spot&symbol=ETHUSDT")
	a, e := HMAC(fixtureKey, []byte(fixtureSecret), r)
	if e != nil || a.APIKey != fixtureKey || a.Signature != "d8702e59ebc8cb70e21f34e5edebdf31cba41302ae5250ec84bfe1696a8e927f" {
		t.Fatal(a, e)
	}
	for _, mutate := range []func(*AuthRequest){func(r *AuthRequest) { r.TimestampMS++ }, func(r *AuthRequest) { r.Payload = "category=spot&symbol=TONUSDT" }} {
		other := r
		mutate(&other)
		b, e := HMAC(fixtureKey, []byte(fixtureSecret), other)
		if e != nil || b.Signature == a.Signature {
			t.Fatal("signature unbound", e)
		}
	}
	for _, key := range []string{"", "key\n", strings.Repeat("a", 129)} {
		if _, e := HMAC(key, []byte(fixtureSecret), r); e == nil {
			t.Fatal("unsafe key accepted")
		}
	}
	for _, secret := range [][]byte{nil, []byte("short"), make([]byte, 513)} {
		if _, e := HMAC(fixtureKey, secret, r); e == nil {
			t.Fatal("invalid secret accepted")
		}
	}
	r.Path = "/v5/order/cancel-all"
	if _, e := HMAC(fixtureKey, []byte(fixtureSecret), r); e == nil {
		t.Fatal("generic signing allowed")
	}
}

func TestAllowlistedReadSchemasAndCanonicalization(t *testing.T) {
	valid := []AuthRequest{
		readRequest("/v5/user/query-api", ""),
		readRequest("/v5/asset/coin/query-info", "coin=ETH"),
		readRequest("/v5/asset/deposit/query-address", "chainType=TON&coin=TON"),
		readRequest("/v5/asset/transfer/query-account-coin-balance", "accountType=FUND&coin=ETH&toAccountType=UNIFIED&withTransferSafeAmount=1"),
		readRequest("/v5/market/instruments-info", "category=spot&symbol=ETHUSDT"),
		readRequest("/v5/market/tickers", "category=spot&symbol=ETHUSDT"),
		readRequest("/v5/account/fee-rate", "category=spot&symbol=TONUSDT"),
		readRequest("/v5/order/realtime", "category=spot&orderLinkId=order1&symbol=ETHUSDT"),
		readRequest("/v5/order/history", "category=spot&orderLinkId=order1&symbol=ETHUSDT"),
		readRequest("/v5/execution/list", "category=spot&cursor=a%2Bb%2Fc%3Dd&limit=100&orderLinkId=order1&symbol=ETHUSDT"),
		readRequest("/v5/asset/transfer/query-inter-transfer-list", "limit=50&transferId="+fixtureUUID),
		readRequest("/v5/asset/withdraw/query-record", "coin=ETH&limit=50&withdrawID=withdrawal1&withdrawType=0"),
		readRequest("/v5/asset/deposit/query-record", "coin=TON&limit=50&txID=abcdef1234"),
	}
	for _, r := range valid {
		t.Run(r.Path, func(t *testing.T) {
			if e := ValidateAuthRequest(r); e != nil {
				t.Fatal(e)
			}
			for _, mutate := range []func(*AuthRequest){func(r *AuthRequest) { r.Endpoint = "https://evil.invalid" }, func(r *AuthRequest) { r.TimestampMS = 0 }, func(r *AuthRequest) { r.ReceiveWindowMS = 50000 }, func(r *AuthRequest) { r.Method = "DELETE" }, func(r *AuthRequest) { r.Payload = strings.Repeat("x", 8193) }, func(r *AuthRequest) { r.Path += "/extra" }, func(r *AuthRequest) {
				q, _ := url.ParseQuery(r.Payload)
				q.Set("arbitrary", "1")
				r.Payload = q.Encode()
			}, func(r *AuthRequest) {
				q, _ := url.ParseQuery(r.Payload)
				q["coin"] = []string{"ETH", "TON"}
				r.Payload = q.Encode()
			}, func(r *AuthRequest) { r.Payload = "coin=%GG" }, func(r *AuthRequest) { r.Payload = "coin=ETH;other=x" }, func(r *AuthRequest) { r.Payload = "coin=%0a" }, func(r *AuthRequest) { r.Payload = "coin=" }} {
				bad := r
				mutate(&bad)
				if ValidateAuthRequest(bad) == nil {
					t.Fatalf("invalid read accepted: %+v", bad)
				}
			}
			q, _ := url.ParseQuery(r.Payload)
			for key := range q {
				changed := r
				copyQ, _ := url.ParseQuery(r.Payload)
				copyQ.Del(key)
				changed.Payload = copyQ.Encode()
				if key != "cursor" && ValidateAuthRequest(changed) == nil {
					t.Fatal("missing required", key)
				}
				copyQ, _ = url.ParseQuery(r.Payload)
				copyQ.Set(key, "invalid?value")
				changed.Payload = copyQ.Encode()
				if key != "cursor" && ValidateAuthRequest(changed) == nil {
					t.Fatal("bad value", key)
				}
			}
		})
	}
	if ValidateAuthRequest(readRequest("/v5/market/tickers", "symbol=ETHUSDT&category=spot")) == nil {
		t.Fatal("noncanonical order accepted")
	}
}

func TestCanonicalPostSchemasRejectAdditionalAuthority(t *testing.T) {
	posts := []AuthRequest{postRequest("/v5/order/create", fixtureOrder()), postRequest("/v5/asset/transfer/inter-transfer", fixtureTransfer()), postRequest("/v5/asset/withdraw/create", fixtureWithdrawal())}
	for _, r := range posts {
		t.Run(r.Path, func(t *testing.T) {
			if e := ValidateAuthRequest(r); e != nil {
				t.Fatal(e)
			}
			for _, payload := range []string{`{}`, `null`, `[]`, r.Payload + ` {}`, strings.Replace(r.Payload, `{`, `{"unauthorized":true,`, 1), strings.Replace(r.Payload, `{`, `{ `, 1), strings.Replace(r.Payload, `{`, `{"coin":"ETH","coin":"TON",`, 1)} {
				bad := r
				bad.Payload = payload
				if ValidateAuthRequest(bad) == nil {
					t.Fatal("unsafe JSON accepted", payload)
				}
			}
			bad := r
			bad.Path = "/v5/order/cancel-all"
			if ValidateAuthRequest(bad) == nil {
				t.Fatal("unknown state change")
			}
		})
	}
	orderMutations := []func(*SpotOrder){func(o *SpotOrder) { o.Category = "linear" }, func(o *SpotOrder) { o.Symbol = "ETHBTC" }, func(o *SpotOrder) { o.Side = "Transfer" }, func(o *SpotOrder) { o.OrderType = "Limit" }, func(o *SpotOrder) { o.Qty = "0" }, func(o *SpotOrder) { o.Qty = "1e5" }, func(o *SpotOrder) { o.MarketUnit = "quoteCoin" }, func(o *SpotOrder) { o.IsLeverage = 1 }, func(o *SpotOrder) { o.SlippageToleranceType = "TickSize" }, func(o *SpotOrder) { o.SlippageTolerance = "0.001" }, func(o *SpotOrder) { o.SlippageTolerance = "10.01" }, func(o *SpotOrder) { o.OrderLinkID = "" }}
	for _, mutate := range orderMutations {
		o := fixtureOrder()
		mutate(&o)
		if ValidateSpotOrder(o) == nil || ValidateAuthRequest(postRequest("/v5/order/create", o)) == nil {
			t.Fatal("unsafe order accepted", o)
		}
	}
	buy := fixtureOrder()
	buy.Side = "Buy"
	buy.MarketUnit = "quoteCoin"
	buy.SlippageTolerance = "10"
	if ValidateSpotOrder(buy) != nil {
		t.Fatal("valid buy rejected")
	}
	for _, mutate := range []func(*TransferRequest){func(v *TransferRequest) { v.TransferID = "not-a-uuid" }, func(v *TransferRequest) { v.Coin = "eth" }, func(v *TransferRequest) { v.Amount = "-1" }, func(v *TransferRequest) { v.FromAccountType = "CONTRACT" }, func(v *TransferRequest) { v.ToAccountType = "FUND" }} {
		v := fixtureTransfer()
		mutate(&v)
		if ValidateTransfer(v) == nil || ValidateAuthRequest(postRequest("/v5/asset/transfer/inter-transfer", v)) == nil {
			t.Fatal("unsafe transfer", v)
		}
	}
	for _, mutate := range []func(*WithdrawalRequest){func(v *WithdrawalRequest) { v.Coin = "eth" }, func(v *WithdrawalRequest) { v.Chain = "" }, func(v *WithdrawalRequest) { v.Address = "" }, func(v *WithdrawalRequest) { v.Tag = "\n" }, func(v *WithdrawalRequest) { v.Amount = "0" }, func(v *WithdrawalRequest) { v.Timestamp = 0 }, func(v *WithdrawalRequest) { v.ForceChain = 0 }, func(v *WithdrawalRequest) { v.AccountType = "UTA" }, func(v *WithdrawalRequest) { v.FeeType = 0 }, func(v *WithdrawalRequest) { v.RequestID = fixtureUUID }} {
		v := fixtureWithdrawal()
		mutate(&v)
		if ValidateWithdrawal(v) == nil || ValidateAuthRequest(postRequest("/v5/asset/withdraw/create", v)) == nil {
			t.Fatal("unsafe withdrawal", v)
		}
	}
	w := fixtureWithdrawal()
	w.Tag = "1234567"
	if ValidateAuthRequest(postRequest("/v5/asset/withdraw/create", w)) != nil {
		t.Fatal("TON tag rejected")
	}
}

func TestParsingBoundaryPredicates(t *testing.T) {
	for _, s := range []string{"", "a\n", "a\x00", string([]byte{0xff}), strings.Repeat("a", 121)} {
		if boundedText(s, 120, false) {
			t.Fatal("unsafe text", s)
		}
	}
	if !boundedText("", 120, true) || !boundedText("Ethereum (ERC20)", 120, false) {
		t.Fatal("valid text")
	}
	for _, s := range []string{"", "a/b", "a.b", strings.Repeat("a", 129)} {
		if identifier(s, 128) {
			t.Fatal("invalid identifier")
		}
	}
	for _, s := range []string{"", "eth", "ETH.USDT", strings.Repeat("A", 25)} {
		if coinName(s) {
			t.Fatal("invalid coin")
		}
	}
	for _, s := range []string{"USDT", "USDTUSDT", "ETHBTC"} {
		if symbolName(s) {
			t.Fatal("invalid symbol")
		}
	}
	for _, s := range []string{"-1", "1e3", "01", "NaN", ".1", "1.", "1.0000000000000000000000000000000000001"} {
		if decimal(s, false) {
			t.Fatal("invalid decimal", s)
		}
	}
	if decimalRange("1e2", 0, 1000, 2) || decimalRange("0", 1, 1000, 2) || decimalRange("11", 1, 1000, 2) {
		t.Fatal("invalid decimal range")
	}
	for _, s := range []string{"", strings.Replace(fixtureUUID, "-", "x", 1), strings.Replace(fixtureUUID, "6", "Z", 1), strings.ToUpper(fixtureUUID)} {
		if uuid(s) {
			t.Fatal("invalid UUID", s)
		}
	}
	for _, s := range []string{"", "abc_def", "abc-def", strings.Repeat("a", 33)} {
		if withdrawalID(s) {
			t.Fatal("invalid requestId")
		}
	}
	if required([]byte(`null`), "x") || required([]byte(`{"x":null}`), "x") || required([]byte(`{"x":1}`), "y") || required([]byte(`invalid`), "x") {
		t.Fatal("invalid required members")
	}
	if !required([]byte(`{"x":0}`), "x") {
		t.Fatal("zero member missing")
	}
}

func FuzzBybitAuthRequest(f *testing.F) {
	f.Add("/v5/user/query-api", "", int64(1700000000000))
	f.Add("/v5/asset/withdraw/create", `{"coin":"ETH"}`, int64(1))
	f.Fuzz(func(t *testing.T, path, payload string, stamp int64) {
		if len(payload) > 16384 || len(path) > 1024 {
			t.Skip()
		}
		r := readRequest(path, payload)
		r.TimestampMS = stamp
		_ = ValidateAuthRequest(r)
		r.Method = "POST"
		_ = ValidateAuthRequest(r)
	})
}

func FuzzBybitExecutionExtraFees(f *testing.F) {
	for _, seed := range []string{`""`, `[]`, `null`, `[{"feeCoin":"USDT","feeType":"VAT","subFeeType":"ARE_VAT","feeRate":"0.05","fee":"0.01"}]`, `"[]"`, `[{"fee":"1","fee":"2"}]`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 8192 {
			t.Skip()
		}
		_, e := ExecutionExtraFees(json.RawMessage(raw))
		if e != nil && e.Error() != ErrInvalidResponse.Error() {
			t.Fatal("fee parser error echoed untrusted input")
		}
	})
}
