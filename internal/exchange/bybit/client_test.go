package bybit

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// All credentials/addresses in these tests are public synthetic fixtures.
const fixtureKey = "fixtureApiKey123456789"
const fixtureSecret = "PUBLIC_SYNTHETIC_SECRET_123456789"
const fixtureUUID = "6ba7b810-9dad-41d1-80b4-00c04fd430c8"
const fixtureWithdrawID = "0123456789abcdef0123456789abcdef"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func resultResponse(result string) *http.Response {
	return response(`{"retCode":0,"retMsg":"OK","result":` + result + `}`)
}
func testClient(t *testing.T, result string) *Client {
	t.Helper()
	c, e := New("https://api.bybit.com", func(_ context.Context, r AuthRequest) (Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), r)
	})
	if e != nil {
		t.Fatal(e)
	}
	c.now = func() time.Time { return time.UnixMilli(1700000000000) }
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return resultResponse(result), nil })
	return c
}
func fixtureOrder() SpotOrder {
	return SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Market", Qty: "0.012345678912345678", MarketUnit: "baseCoin", IsLeverage: 0, SlippageToleranceType: "Percent", SlippageTolerance: "0.50", OrderLinkID: "fixtureOrder1"}
}
func fixtureTransfer() TransferRequest {
	return TransferRequest{TransferID: fixtureUUID, Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
}
func fixtureWithdrawal() WithdrawalRequest {
	return WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: "0x1111111111111111111111111111111111111111", Amount: "0.1", Timestamp: 1700000000000, ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: fixtureWithdrawID}
}

type methodFixture struct {
	name, result string
	call         func(*Client) error
}

func methodFixtures() []methodFixture {
	ctx := context.Background()
	return []methodFixture{
		{"key", `{"userID":123,"readOnly":0,"isMaster":true,"uta":1,"permissions":{"Spot":["SpotTrade"],"Wallet":["AccountTransfer","Withdraw"]}}`, func(c *Client) error {
			v, e := c.APIKeyInfo(ctx)
			if e == nil && v.UserID != 123 {
				return errors.New("wrong UID")
			}
			return e
		}},
		{"coin", `{"rows":[{"name":"Ethereum","coin":"ETH","chains":[{"chain":"ETH","chainType":"Ethereum (ERC20)","confirmation":"12","withdrawFee":"0.0002","depositMin":"0.0001","withdrawMin":"0.0004","minAccuracy":"18","chainDeposit":"1","chainWithdraw":"1","withdrawPercentageFee":"0","contractAddress":"","safeConfirmNumber":"64","withdrawMax":"-1"}]}]}`, func(c *Client) error { _, e := c.CoinInfo(ctx, "ETH"); return e }},
		{"deposit", `{"coin":"GRAM","chains":[{"chain":"TON","chainType":"TON","addressDeposit":"EQpublicSyntheticAddress","tagDeposit":"1234567","contractAddress":"","batchReleaseLimit":"-1"}]}`, func(c *Client) error {
			v, e := c.DepositAddress(ctx, "GRAM", "TON")
			if e == nil && v.Tag != "1234567" {
				return errors.New("lost TON memo")
			}
			return e
		}},
		{"balance", `{"accountType":"FUND","accountId":"10001","memberId":"123","balance":{"coin":"ETH","walletBalance":"1.234567890123456789","transferBalance":"1.2","transferSafeAmount":"1.1"}}`, func(c *Client) error {
			v, e := c.AccountBalance(ctx, "FUND", "ETH")
			if e == nil && v.Balance.WalletBalance != "1.234567890123456789" {
				return errors.New("precision lost")
			}
			return e
		}},
		{"instrument", `{"category":"spot","list":[{"symbol":"ETHUSDT","baseCoin":"ETH","quoteCoin":"USDT","status":"Trading","lotSizeFilter":{"basePrecision":"0.000001","quotePrecision":"0.01","minOrderAmt":"5","maxMarketOrderQty":"100"},"priceFilter":{"tickSize":"0.01"}}]}`, func(c *Client) error { _, e := c.SpotInstrument(ctx, "ETHUSDT"); return e }},
		{"ticker", `{"category":"spot","list":[{"symbol":"ETHUSDT","lastPrice":"2345.123456789123456789","bid1Price":"2345.12","ask1Price":"2345.13"}]}`, func(c *Client) error {
			v, e := c.SpotTicker(ctx, "ETHUSDT")
			if e == nil && v.LastPrice != "2345.123456789123456789" {
				return errors.New("precision lost")
			}
			return e
		}},
		{"fees", `{"list":[{"symbol":"ETHUSDT","takerFeeRate":"0.001","makerFeeRate":"0.001"}]}`, func(c *Client) error { _, e := c.SpotFeeRate(ctx, "ETHUSDT"); return e }},
		{"create", `{"orderId":"bybitOrder1","orderLinkId":"fixtureOrder1"}`, func(c *Client) error { _, e := c.CreateOrder(ctx, fixtureOrder()); return e }},
		{"order", `{"category":"spot","list":[{"orderId":"bybitOrder1","orderLinkId":"fixtureOrder1","symbol":"ETHUSDT","side":"Sell","orderType":"Market","qty":"0.012345678912345678","marketUnit":"baseCoin","isLeverage":"0","orderStatus":"Filled","cumExecQty":"0.012345678912345678","cumExecValue":"28.95","cumExecFee":"0.02895","avgPrice":"2345.1"}],"nextPageCursor":""}`, func(c *Client) error {
			v, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1")
			if e == nil && v == nil {
				return errors.New("missing order")
			}
			return e
		}},
		{"executions", `{"category":"spot","list":[{"symbol":"ETHUSDT","orderId":"bybitOrder1","orderLinkId":"fixtureOrder1","side":"Sell","orderType":"Market","orderQty":"0.012345678912345678","execId":"execution1","execPrice":"2345.1","execQty":"0.012345678912345678","execValue":"28.95","execFee":"0.02895","feeCurrency":"USDT","execTime":"1700000000000"}],"nextPageCursor":"next/page=1+2"}`, func(c *Client) error {
			v, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "")
			if e == nil && v.NextPageCursor != "next/page=1+2" {
				return errors.New("lost cursor")
			}
			return e
		}},
		{"transfer", `{"transferId":"` + fixtureUUID + `","status":"PENDING"}`, func(c *Client) error { _, e := c.Transfer(ctx, fixtureTransfer()); return e }},
		{"transferstatus", `{"list":[{"transferId":"` + fixtureUUID + `","coin":"ETH","amount":"0.1","fromAccountType":"FUND","toAccountType":"UNIFIED","status":"SUCCESS","timestamp":"1700000000000"}],"nextPageCursor":""}`, func(c *Client) error {
			v, e := c.TransferStatus(ctx, fixtureUUID)
			if e == nil && v == nil {
				return errors.New("missing transfer")
			}
			return e
		}},
		{"withdraw", `{"id":"withdrawal1"}`, func(c *Client) error { _, e := c.Withdraw(ctx, fixtureWithdrawal()); return e }},
		{"withdrawstatus", `{"rows":[{"withdrawId":"withdrawal1","coin":"ETH","chain":"ETH","amount":"0.1","withdrawFee":"0.0002","status":"success","toAddress":"0x1111111111111111111111111111111111111111","tag":"","withdrawType":0,"txID":"0xabcdef"}],"nextPageCursor":""}`, func(c *Client) error { _, e := c.WithdrawalRecords(ctx, "ETH", "withdrawal1", ""); return e }},
		{"depositstatus", `{"rows":[{"id":"deposit1","coin":"TON","chain":"TON","amount":"1.234567891","txID":"abcdef1234","status":3,"toAddress":"EQpublicSyntheticAddress","tag":"1234567","successAt":"1700000000000","confirmations":"64"}],"nextPageCursor":""}`, func(c *Client) error { _, e := c.DepositRecords(ctx, "TON", "abcdef1234", ""); return e }},
	}
}

func TestTypedAPIWireAndExactAmounts(t *testing.T) {
	for _, f := range methodFixtures() {
		t.Run(f.name, func(t *testing.T) {
			c := testClient(t, f.result)
			called := 0
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				called++
				if r.URL.Scheme != "https" || r.URL.Host != "api.bybit.com" || r.Header.Get("Accept") != "application/json" {
					t.Fatal("unexpected request authority")
				}
				public := strings.HasPrefix(r.URL.Path, "/v5/market/")
				if public {
					if r.Header.Get("X-BAPI-API-KEY") != "" || r.Header.Get("X-BAPI-SIGN") != "" {
						t.Fatal("public request leaked authorization")
					}
				} else {
					if r.Header.Get("X-BAPI-API-KEY") != fixtureKey || r.Header.Get("X-BAPI-TIMESTAMP") != "1700000000000" || r.Header.Get("X-BAPI-RECV-WINDOW") != "5000" {
						t.Fatal("missing private request auth")
					}
					payload := r.URL.RawQuery
					if r.Method == "POST" {
						b, e := io.ReadAll(r.Body)
						if e != nil {
							t.Fatal(e)
						}
						payload = string(b)
						if r.GetBody != nil {
							t.Fatal("POST body replay enabled")
						}
					}
					a, e := HMAC(fixtureKey, []byte(fixtureSecret), AuthRequest{Endpoint: c.endpoint, Method: r.Method, Path: r.URL.Path, Payload: payload, TimestampMS: 1700000000000, ReceiveWindowMS: 5000})
					if e != nil || a.Signature != r.Header.Get("X-BAPI-SIGN") {
						t.Fatal("signature not bound to exact wire")
					}
				}
				return resultResponse(f.result), nil
			})
			if e := f.call(c); e != nil {
				t.Fatal(e)
			}
			if called != 1 {
				t.Fatalf("unexpected requests %d", called)
			}
		})
	}
}

func TestTransportAndEnvelopeRejection(t *testing.T) {
	badBodies := []string{`{}`, `{"retCode":null,"result":{}}`, `{"retCode":"0","result":{}}`, `{"retCode":0,"retCode":0,"result":{}}`, `{"retCode":0,"result":null}`, `{"retCode":0,"result":[]}`, `{"retCode":0,"result":{}} {}`, `{"retCode":0,"result":{"x":1,"x":2}}`, `{"retCode":0,"result":{"x":` + strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34) + `}}`, strings.Repeat(" ", maxResponseBytes+1), string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})}
	for i, b := range badBodies {
		t.Run(strconvInt(i), func(t *testing.T) {
			c := testClient(t, `{}`)
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(b), nil })
			if _, e := c.APIKeyInfo(context.Background()); !errors.Is(e, ErrInvalidResponse) {
				t.Fatalf("invalid JSON accepted: %v", e)
			}
		})
	}
	for _, f := range methodFixtures() {
		t.Run("network-"+f.name, func(t *testing.T) {
			c := testClient(t, f.result)
			attempts := 0
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { attempts++; return nil, errors.New("SECRET_CANARY") })
			e := f.call(c)
			if !errors.Is(e, ErrUnavailable) || strings.Contains(e.Error(), "SECRET_CANARY") || attempts != 1 {
				t.Fatalf("unsafe error/replay: %v/%d", e, attempts)
			}
		})
	}
	for _, status := range []int{301, 302, 307, 308, 400, 403, 429, 500} {
		t.Run("HTTP-"+strconvInt(status), func(t *testing.T) {
			c := testClient(t, `{}`)
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				r := response("SECRET_CANARY")
				r.StatusCode = status
				return r, nil
			})
			_, e := c.APIKeyInfo(context.Background())
			var api *APIError
			if !errors.As(e, &api) || api.HTTPStatus != status || strings.Contains(e.Error(), "SECRET_CANARY") {
				t.Fatal(e)
			}
		})
	}
	c := testClient(t, `{}`)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(`{"retCode":10005,"retMsg":"SECRET_CANARY","result":{}}`), nil
	})
	_, e := c.APIKeyInfo(context.Background())
	if e.Error() != "Bybit rejected request (code 10005)" {
		t.Fatal(e)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(errorReader{}), Header: http.Header{}}, nil
	})
	if _, e = c.APIKeyInfo(context.Background()); !errors.Is(e, ErrInvalidResponse) {
		t.Fatal(e)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("SECRET_CANARY") }
func strconvInt(n int) string                { b, _ := json.Marshal(n); return string(b) }

func TestAuthenticationAndEndpointBoundaries(t *testing.T) {
	for _, endpoint := range Endpoints() {
		c, e := New(endpoint, nil)
		if e != nil || c.Endpoint() != endpoint {
			t.Fatal(endpoint, e)
		}
		transport := c.http.Transport.(*http.Transport)
		if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || c.http.Jar != nil || c.http.Timeout <= 0 || transport.MaxResponseHeaderBytes <= 0 {
			t.Fatal("unsafe transport")
		}
		if e = c.http.CheckRedirect(nil, nil); !errors.Is(e, http.ErrUseLastResponse) {
			t.Fatal("redirects enabled")
		}
		c.Close()
	}
	endpoints := Endpoints()
	endpoints[0] = "https://evil.invalid"
	if ValidateEndpoint(endpoints[0]) == nil {
		t.Fatal("mutable allowlist")
	}
	for _, endpoint := range []string{"", "http://api.bybit.com", "https://api.bybit.com/", "https://api.bybit.com:443", "https://api.bybit.com.evil.invalid", "https://api.bybit.com@evil.invalid", "https://api-testnet.bybit.com", "https://api.bybit.com?x=1", "https://127.0.0.1", "https://API.BYBIT.COM"} {
		if _, e := New(endpoint, nil); e == nil {
			t.Fatal("untrusted endpoint accepted", endpoint)
		}
	}
	for _, auth := range []Authorizer{nil, func(context.Context, AuthRequest) (Auth, error) { return Auth{}, errors.New("SECRET_CANARY") }, func(context.Context, AuthRequest) (Auth, error) {
		return Auth{APIKey: "key\r\nInjected: yes", Signature: strings.Repeat("0", 64)}, nil
	}, func(context.Context, AuthRequest) (Auth, error) {
		return Auth{APIKey: fixtureKey, Signature: "00"}, nil
	}, func(context.Context, AuthRequest) (Auth, error) {
		return Auth{APIKey: fixtureKey, Signature: strings.Repeat("Z", 64)}, nil
	}, func(context.Context, AuthRequest) (Auth, error) {
		return Auth{APIKey: fixtureKey, Signature: strings.Repeat("A", 64)}, nil
	}} {
		c := testClient(t, `{}`)
		c.authorize = auth
		c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("unauthorized request reached network")
			return nil, nil
		})
		if _, e := c.APIKeyInfo(context.Background()); !errors.Is(e, ErrAuthorization) {
			t.Fatal(e)
		}
	}
	c := testClient(t, `{}`)
	c.authorize = func(context.Context, AuthRequest) (Auth, error) {
		t.Fatal("invalid request reached signer")
		return Auth{}, nil
	}
	if _, e := c.SpotTicker(context.Background(), "ETHBTC"); !errors.Is(e, ErrInvalidRequest) {
		t.Fatal(e)
	}
	if e := c.do(context.Background(), "POST", "/v5/order/create", nil, make(chan int), true, &struct{}{}); !errors.Is(e, ErrInvalidRequest) {
		t.Fatal(e)
	}
}

func TestMissingMalformedAndForeignTypedResults(t *testing.T) {
	for _, f := range methodFixtures() {
		t.Run(f.name, func(t *testing.T) {
			if e := f.call(testClient(t, `{}`)); !errors.Is(e, ErrInvalidResponse) {
				t.Fatalf("empty result accepted: %v", e)
			}
		})
	}
	bad := map[string][]string{
		"key":            {`{"userID":123,"readOnly":0,"isMaster":null,"uta":1,"permissions":{}}`, `{"userID":123,"readOnly":2,"isMaster":true,"uta":1,"permissions":{}}`, `{"userID":123,"readOnly":0,"isMaster":true,"uta":1,"permissions":{"Spot":["bad\npermission"]}}`},
		"coin":           {`{"rows":[{"coin":"ETH","name":"Ethereum","chains":[{}]}]}`, strings.Replace(methodFixtures()[1].result, `"minAccuracy":"18"`, `"minAccuracy":"99"`, 1), strings.Replace(methodFixtures()[1].result, `"confirmation":"12"`, `"confirmation":"-1"`, 1)},
		"deposit":        {strings.Replace(methodFixtures()[2].result, `"chain":"TON"`, `"chain":"ETH"`, 1)},
		"balance":        {strings.Replace(methodFixtures()[3].result, `"transferSafeAmount":"1.1"`, `"transferSafeAmount":null`, 1)},
		"instrument":     {strings.Replace(methodFixtures()[4].result, `"status":"Trading"`, `"status":"PreLaunch"`, 1)},
		"ticker":         {strings.Replace(methodFixtures()[5].result, `"lastPrice":"2345.123456789123456789"`, `"lastPrice":2345.1`, 1)},
		"fees":           {strings.Replace(methodFixtures()[6].result, `"takerFeeRate":"0.001"`, `"takerFeeRate":"2"`, 1)},
		"create":         {`{"orderId":"bybitOrder1","orderLinkId":"foreignOrder"}`},
		"order":          {strings.Replace(methodFixtures()[8].result, `"marketUnit":"baseCoin"`, `"marketUnit":"quoteCoin"`, 1), strings.Replace(methodFixtures()[8].result, `"nextPageCursor":""`, `"nextPageCursor":"unexpected"`, 1)},
		"executions":     {strings.Replace(methodFixtures()[9].result, `"execFee":"0.02895"`, `"execFee":"-1"`, 1)},
		"transfer":       {`{"transferId":"` + fixtureUUID + `","status":"made-up"}`},
		"transferstatus": {strings.Replace(methodFixtures()[11].result, `"amount":"0.1"`, `"amount":"0"`, 1), strings.Replace(methodFixtures()[11].result, `"nextPageCursor":""`, `"nextPageCursor":"unexpected"`, 1)},
		"withdraw":       {`{"id":"invalid\nID"}`},
		"withdrawstatus": {strings.Replace(methodFixtures()[13].result, `"coin":"ETH"`, `"coin":"TON"`, 1)},
		"depositstatus":  {strings.Replace(methodFixtures()[14].result, `"txID":"abcdef1234"`, `"txID":"foreign"`, 1)},
	}
	for _, f := range methodFixtures() {
		for i, b := range bad[f.name] {
			t.Run(f.name+"-"+strconvInt(i), func(t *testing.T) {
				if e := f.call(testClient(t, b)); !errors.Is(e, ErrInvalidResponse) {
					t.Fatalf("unsafe result accepted: %v", e)
				}
			})
		}
	}
}

func TestReconciliationAbsentFallbackAndCursors(t *testing.T) {
	ctx := context.Background()
	c := testClient(t, `{"category":"spot","list":[],"nextPageCursor":""}`)
	if o, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1"); e != nil || o != nil {
		t.Fatal(o, e)
	}
	requests := 0
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			if r.URL.Path != "/v5/order/realtime" {
				t.Fatal(r.URL.Path)
			}
			return resultResponse(`{"category":"spot","list":[]}`), nil
		}
		if r.URL.Path != "/v5/order/history" {
			t.Fatal(r.URL.Path)
		}
		return resultResponse(methodFixtures()[8].result), nil
	})
	if o, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1"); e != nil || o == nil || requests != 2 {
		t.Fatal(o, e, requests)
	}
	c = testClient(t, `{"list":[]}`)
	if v, e := c.TransferStatus(ctx, fixtureUUID); e != nil || v != nil {
		t.Fatal(v, e)
	}
	c = testClient(t, methodFixtures()[3].result)
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("toAccountType") != "FUND" {
			t.Fatal("wrong direction")
		}
		return resultResponse(strings.Replace(methodFixtures()[3].result, `"accountType":"FUND"`, `"accountType":"UNIFIED"`, 1)), nil
	})
	if _, e := c.AccountBalance(ctx, "UNIFIED", "ETH"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"execution", "withdrawal", "deposit"} {
		t.Run(kind, func(t *testing.T) {
			result := `{"category":"spot","list":[],"rows":[],"nextPageCursor":"cursor1"}`
			c := testClient(t, result)
			switch kind {
			case "execution":
				if _, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "cursor1"); !errors.Is(e, ErrInvalidResponse) {
					t.Fatal(e)
				}
			case "withdrawal":
				if _, e := c.WithdrawalRecords(ctx, "ETH", "withdrawal1", "cursor1"); !errors.Is(e, ErrInvalidResponse) {
					t.Fatal(e)
				}
			case "deposit":
				if _, e := c.DepositRecords(ctx, "TON", "abcdef1234", "cursor1"); !errors.Is(e, ErrInvalidResponse) {
					t.Fatal(e)
				}
			}
		})
	}
	buy := fixtureOrder()
	buy.Side = "Buy"
	buy.MarketUnit = "quoteCoin"
	c = testClient(t, methodFixtures()[7].result)
	if _, e := c.CreateOrder(ctx, buy); e != nil {
		t.Fatal(e)
	}
	c = testClient(t, strings.ReplaceAll(strings.ReplaceAll(methodFixtures()[8].result, `"side":"Sell"`, `"side":"Buy"`), `"marketUnit":"baseCoin"`, `"marketUnit":"quoteCoin"`))
	if _, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1"); e != nil {
		t.Fatal(e)
	}
}

func TestOpaqueCursorEscapingAndNoAuthOnPublicReads(t *testing.T) {
	ctx := context.Background()
	c := testClient(t, `{"category":"spot","list":[],"nextPageCursor":""}`)
	cursor := "a+b/c=d&category=linear"
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if q.Get("category") != "spot" || q.Get("cursor") != cursor || len(q["category"]) != 1 {
			t.Fatal("cursor injection", r.URL.RawQuery)
		}
		return resultResponse(`{"category":"spot","list":[],"nextPageCursor":""}`), nil
	})
	if _, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", cursor); e != nil {
		t.Fatal(e)
	}
	c = testClient(t, methodFixtures()[5].result)
	c.authorize = func(context.Context, AuthRequest) (Auth, error) {
		t.Fatal("public ticker invoked secret owner")
		return Auth{}, nil
	}
	if _, e := c.SpotTicker(ctx, "ETHUSDT"); e != nil {
		t.Fatal(e)
	}
	q := url.Values{"cursor": {"opaque"}}
	if cursorQuery(q, "").Get("cursor") != "opaque" {
		t.Fatal("unexpected mutation")
	}
}

func TestRealTLSRedirectNeverReplaysCredentialsOrUsesEnvironmentProxy(t *testing.T) {
	var destinationCalls, proxyCalls int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationCalls++
		t.Error("redirect reached another authority")
	}))
	defer destination.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		t.Error("environment proxy received a private request")
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	var received int
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		if r.Host != "api.bybit.com" || r.Header.Get("X-BAPI-API-KEY") != fixtureKey || r.Header.Get("X-BAPI-SIGN") == "" {
			t.Error("private request did not retain exact API authority/auth")
		}
		w.Header().Set("Location", destination.URL+"/capture")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c, e := New("https://api.bybit.com", func(_ context.Context, r AuthRequest) (Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), r)
	})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	transport := c.http.Transport.(*http.Transport)
	pool := x509.NewCertPool()
	pool.AddCert(origin.Certificate())
	transport.TLSClientConfig.RootCAs = pool
	transport.TLSClientConfig.ServerName = "example.com" // httptest's pinned synthetic certificate only.
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
	}
	_, e = c.APIKeyInfo(context.Background())
	var api *APIError
	if !errors.As(e, &api) || api.HTTPStatus != 307 || received != 1 || destinationCalls != 0 || proxyCalls != 0 {
		t.Fatalf("redirect/proxy boundary failed: %v %d %d %d", e, received, destinationCalls, proxyCalls)
	}
}

func TestOpaqueBlockchainTransactionIDs(t *testing.T) {
	for _, s := range []string{"abcdef", "0xabcdef", "aB+c/dE=", "a-b_c", "0:abc"} {
		if !transactionID(s) {
			t.Fatal("valid blockchain ID rejected", s)
		}
	}
	for _, s := range []string{"", "a\nb", "a&coin=ETH", strings.Repeat("a", 257)} {
		if transactionID(s) {
			t.Fatal("invalid blockchain ID accepted", s)
		}
	}
	f := methodFixtures()[5]
	c := testClient(t, strings.Replace(f.result, `"lastPrice":"2345.123456789123456789"`, `"lastPrice":"bad"`, 1))
	if e := f.call(c); !errors.Is(e, ErrInvalidResponse) {
		t.Fatal("invalid ticker accepted", e)
	}
}

func TestExplicitEmptySpotListingIsDistinctFromMalformedMetadata(t *testing.T) {
	c := testClient(t, `{"category":"spot","list":[]}`)
	if _, e := c.SpotInstrument(context.Background(), "ETHUSDT"); !errors.Is(e, ErrNotListed) {
		t.Fatal(e)
	}
	for _, body := range []string{`{"category":"spot"}`, `{"category":"spot","list":null}`, `{"category":"linear","list":[]}`} {
		c = testClient(t, body)
		if _, e := c.SpotInstrument(context.Background(), "ETHUSDT"); !errors.Is(e, ErrInvalidResponse) {
			t.Fatal("malformed listing misclassified", e)
		}
	}
}
