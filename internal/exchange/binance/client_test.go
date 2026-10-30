package binance

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
)

// Public synthetic values only. These fixtures never contact Binance or carry
// credentials, wallet profiles, user addresses or funds.
const fixtureKey = "publicSyntheticAPIKey123"
const fixtureSecret = "PUBLIC_SYNTHETIC_SECRET_123456789"
const fixtureUUID = "6ba7b810-9dad-41d1-80b4-00c04fd430c8"
const fixtureID = "0123456789abcdef0123456789abcdef"
const fixtureCoin = `[{"coin":"ETH","name":"Ethereum","networkList":[{"network":"ETH","coin":"ETH","name":"Ethereum (ERC20)","depositEnable":true,"withdrawEnable":true,"withdrawFee":"0.0002","withdrawMin":"0.001","withdrawMax":"100","withdrawIntegerMultiple":"0.00000001","depositDust":"0.00001","minConfirm":12,"unLockConfirm":64,"contractAddress":"","denomination":1}]}]`
const fixtureCommission = `{"symbol":"ETHUSDT","standardCommission":{"maker":"0.001","taker":"0.002","buyer":"0.0001","seller":"0.0002"},"specialCommission":{"maker":"0","taker":"0","buyer":"0","seller":"0"},"taxCommission":{"maker":"0.00001","taker":"0.00002","buyer":"0.00003","seller":"0.00004"},"discount":{"enabledForAccount":false,"enabledForSymbol":true}}`
const fixtureOrder = `{"symbol":"ETHUSDT","orderId":123,"clientOrderId":"fixtureOrder1","price":"2000","origQty":"0.1","executedQty":"0.1","cummulativeQuoteQty":"200","status":"FILLED","timeInForce":"IOC","type":"LIMIT","side":"SELL","time":1700000000000,"updateTime":1700000001000}`
const fixtureWithdrawal = `[{"id":"remoteWithdrawal1","coin":"ETH","network":"ETH","amount":"0.1","transactionFee":"0.0002","status":6,"address":"0x1111111111111111111111111111111111111111","addressTag":"","txId":"0xabcdef","applyTime":"2023-11-14 22:13:20","completeTime":"2023-11-14 22:14:20","transferType":0}]`
const fixtureDeposit = `[{"id":"deposit1","coin":"ETH","network":"ETH","amount":"0.1","status":1,"address":"0x1111111111111111111111111111111111111111","addressTag":"","txId":"0xabcdef","insertTime":1700000000000,"completeTime":1700000001000,"transferType":0,"confirmTimes":"64/64","travelRuleStatus":0}]`
const fixtureTransfer = `{"total":1,"rows":[{"asset":"ETH","amount":"0.1","type":"FUNDING_MAIN","status":"CONFIRMED","tranId":456,"timestamp":1700000000000}]}`
const fixtureTrades = `[{"symbol":"ETHUSDT","id":1,"orderId":123,"price":"2000","qty":"0.1","quoteQty":"200","commission":"0.2","commissionAsset":"USDT","time":1700000000000,"isBuyer":false}]`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func testClient(t *testing.T, body func(*http.Request) string) *Client {
	t.Helper()
	c, e := New(Endpoint, func(_ context.Context, r bybit.AuthRequest) (bybit.Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), r)
	})
	if e != nil {
		t.Fatal(e)
	}
	c.now = func() time.Time { return time.UnixMilli(1700000000000) }
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(body(r)), nil })
	t.Cleanup(c.Close)
	return c
}
func spotOrder() bybit.SpotOrder {
	return bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Limit", Qty: "0.1", MarketUnit: "baseCoin", SlippageToleranceType: "Percent", SlippageTolerance: "0.50", OrderLinkID: "fixtureOrder1", LimitPrice: "2000", BaseQty: "0.1"}
}
func transfer() bybit.TransferRequest {
	return bybit.TransferRequest{TransferID: fixtureUUID, Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
}
func withdrawal() bybit.WithdrawalRequest {
	return bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: "0x1111111111111111111111111111111111111111", Amount: "0.1", Timestamp: 1700000000000, ForceChain: 1, AccountType: "FUND", FeeType: 0, RequestID: fixtureID, FeeMode: "binance-native"}
}
func wireFixture(r *http.Request) string {
	switch r.URL.Path {
	case "/sapi/v1/account/apiRestrictions":
		return `{"enableReading":true,"enableSpotAndMarginTrading":true,"enableWithdrawals":true,"permitsUniversalTransfer":true}`
	case "/sapi/v1/capital/config/getall":
		return fixtureCoin
	case "/sapi/v1/capital/deposit/address":
		return `{"coin":"ETH","address":"0x1111111111111111111111111111111111111111","tag":"","url":"https://attacker.invalid/ignored"}`
	case "/sapi/v1/asset/get-funding-asset":
		return `[{"asset":"ETH","free":"1.123456789123456789","locked":"2","freeze":"3","withdrawing":"4"}]`
	case "/api/v3/account":
		return `{"accountType":"SPOT","balances":[{"asset":"ETH","free":"1.123456789123456789","locked":"9"}]}`
	case "/api/v3/exchangeInfo":
		return `{"symbols":[{"symbol":"ETHUSDT","status":"TRADING","baseAsset":"ETH","quoteAsset":"USDT","quoteAssetPrecision":8,"isSpotTradingAllowed":true,"orderTypes":["LIMIT","MARKET"],"filters":[{"filterType":"PRICE_FILTER","tickSize":"0.01"},{"filterType":"LOT_SIZE","minQty":"0.0001","maxQty":"100","stepSize":"0.0001"},{"filterType":"NOTIONAL","minNotional":"5"}]}]}`
	case "/api/v3/ticker/24hr":
		return `{"symbol":"ETHUSDT","lastPrice":"2000.123456789123456789","bidPrice":"2000.01","askPrice":"2000.02"}`
	case "/api/v3/account/commission":
		return fixtureCommission
	case "/api/v3/order":
		if r.Method == "POST" {
			return `{"symbol":"ETHUSDT","orderId":123,"clientOrderId":"fixtureOrder1"}`
		}
		return fixtureOrder
	case "/api/v3/myTrades":
		return fixtureTrades
	case "/sapi/v1/asset/transfer":
		if r.Method == "POST" {
			return `{"tranId":456}`
		}
		return fixtureTransfer
	case "/sapi/v1/localentity/questionnaire-requirements":
		return `{"questionnaireCountryCode":"NIL"}`
	case "/sapi/v1/capital/withdraw/apply":
		return `{"id":"remoteWithdrawal1"}`
	case "/sapi/v1/capital/withdraw/history":
		return fixtureWithdrawal
	case "/sapi/v1/capital/deposit/hisrec":
		return fixtureDeposit
	}
	return `{}`
}
func TestAllMethodsExactWireAndTypedResults(t *testing.T) {
	ctx := context.Background()
	calls := []struct {
		name string
		call func(*Client) error
	}{
		{"key", func(c *Client) error {
			v, e := c.APIKeyInfo(ctx)
			if e == nil && (v.UserID != 0 || v.IsMaster || !reflect.DeepEqual(v.Permissions.Wallet, []string{"Withdraw", "AccountTransfer"})) {
				t.Fatal("fabricated account or lost capabilities")
			}
			return e
		}},
		{"coin", func(c *Client) error {
			v, e := c.CoinInfo(ctx, "ETH")
			if e == nil && (v.Chains[0].MinAccuracy != "8" || v.Chains[0].Confirmation != "12") {
				t.Fatal(v)
			}
			return e
		}},
		{"deposit", func(c *Client) error {
			v, e := c.DepositAddress(ctx, "ETH", "ETH")
			if e == nil && (v.Address != withdrawal().Address || v.Chain != "ETH") {
				t.Fatal(v)
			}
			return e
		}},
		{"fund", func(c *Client) error {
			v, e := c.AccountBalance(ctx, "FUND", "ETH")
			if e == nil && v.Balance.TransferSafeAmount != "1.123456789123456789" {
				t.Fatal("rounded or unlocked frozen balance")
			}
			return e
		}},
		{"spot", func(c *Client) error {
			v, e := c.AccountBalance(ctx, "UNIFIED", "ETH")
			if e == nil && v.Balance.WalletBalance != "1.123456789123456789" {
				t.Fatal(v)
			}
			return e
		}},
		{"instrument", func(c *Client) error { _, e := c.SpotInstrument(ctx, "ETHUSDT"); return e }},
		{"ticker", func(c *Client) error {
			v, e := c.SpotTicker(ctx, "ETHUSDT")
			if e == nil && v.LastPrice != "2000.123456789123456789" {
				t.Fatal(v)
			}
			return e
		}},
		{"fees", func(c *Client) error {
			v, e := c.SpotFeeRate(ctx, "ETHUSDT")
			if e == nil && (rat(v.MakerFeeRate).Cmp(rat("0.00125")) != 0 || rat(v.TakerFeeRate).Cmp(rat("0.00226")) != 0) {
				t.Fatal(v)
			}
			return e
		}},
		{"create", func(c *Client) error {
			v, e := c.CreateOrder(ctx, spotOrder())
			if e == nil && (v.OrderID != "123" || v.OrderLinkID != "fixtureOrder1") {
				t.Fatal(v)
			}
			return e
		}},
		{"order", func(c *Client) error {
			v, e := c.FindOrder(ctx, "ETHUSDT", "fixtureOrder1")
			if e == nil && (v.Qty != "0.1" || v.LimitPrice != "2000" || v.TimeInForce != "IOC" || v.OrderType != "Limit") {
				t.Fatal(v)
			}
			return e
		}},
		{"fills", func(c *Client) error {
			v, e := c.Executions(ctx, "ETHUSDT", "fixtureOrder1", "")
			if e == nil && (len(v.List) != 1 || v.List[0].OrderLinkID != "fixtureOrder1" || v.List[0].FeeCurrency != "USDT") {
				t.Fatal(v)
			}
			return e
		}},
		{"transfer", func(c *Client) error {
			v, e := c.Transfer(ctx, transfer())
			if e == nil && v.TransferID != "456" {
				t.Fatal("lost server transfer identity")
			}
			return e
		}},
		{"transfer-status", func(c *Client) error {
			v, e := c.TransferStatusFor(ctx, transfer(), "456")
			if e == nil && (v == nil || v.TransferID != fixtureUUID || v.Status != "SUCCESS") {
				t.Fatal(v)
			}
			return e
		}},
		{"allowed", func(c *Client) error { return c.WithdrawalAllowed(ctx) }},
		{"withdraw", func(c *Client) error { _, e := c.Withdraw(ctx, withdrawal()); return e }},
		{"withdraw-history", func(c *Client) error {
			v, e := c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", "")
			if e == nil && (len(v.Rows) != 1 || v.Rows[0].Amount != "0.1" || v.Rows[0].WithdrawFee != "0.0002" || v.Rows[0].Status != "success") {
				t.Fatal(v)
			}
			return e
		}},
		{"deposit-history", func(c *Client) error {
			v, e := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
			if e == nil && (len(v.Rows) != 1 || v.Rows[0].Status != 3) {
				t.Fatal(v)
			}
			return e
		}},
	}
	for _, f := range calls {
		t.Run(f.name, func(t *testing.T) {
			c := testClient(t, wireFixture)
			requests := 0
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Scheme != "https" || r.URL.Host != "api.binance.com" || r.Header.Get("Accept") != "application/json" {
					t.Fatal("wrong authority")
				}
				public := r.URL.Path == "/api/v3/exchangeInfo" || r.URL.Path == "/api/v3/ticker/24hr"
				payload := r.URL.RawQuery
				if r.Method == "POST" {
					b, e := io.ReadAll(r.Body)
					if e != nil {
						t.Fatal(e)
					}
					payload = string(b)
					if r.GetBody != nil || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Fatal("POST replay/encoding")
					}
				}
				q, e := url.ParseQuery(payload)
				if e != nil {
					t.Fatal(e)
				}
				if public {
					if r.Header.Get("X-MBX-APIKEY") != "" || q.Has("signature") {
						t.Fatal("public metadata leaked auth")
					}
				} else {
					sig := q.Get("signature")
					if r.Header.Get("X-MBX-APIKEY") != fixtureKey || q.Get("timestamp") != "1700000000000" || q.Get("recvWindow") != "5000" {
						t.Fatal("wrong native auth")
					}
					q.Del("signature")
					q.Del("timestamp")
					q.Del("recvWindow")
					a, e := HMAC(fixtureKey, []byte(fixtureSecret), bybit.AuthRequest{Endpoint: Endpoint, Method: r.Method, Path: r.URL.Path, Payload: q.Encode(), TimestampMS: 1700000000000, ReceiveWindowMS: 5000})
					if e != nil || a.Signature != sig {
						t.Fatal("signature not bound to exact wire")
					}
				}
				return response(wireFixture(r)), nil
			})
			if e := f.call(c); e != nil {
				t.Fatal(e)
			}
			if requests < 1 || requests > 2 {
				t.Fatal("unbounded/unexpected requests", requests)
			}
		})
	}
}

func TestTransportEndpointAndAuthorizationFailuresAreBoundedRedactedAndNeverRetried(t *testing.T) {
	for _, endpoint := range []string{"http://api.binance.com", Endpoint + "/", "https://api.binance.us", "https://api.binance.com.attacker.invalid", Endpoint + ":443", "https://user:pass@api.binance.com"} {
		if c, e := New(endpoint, nil); e == nil || c != nil {
			t.Fatal("custom endpoint accepted", endpoint)
		}
	}
	c, e := New(Endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	transport := c.http.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || c.http.Jar != nil || transport.MaxResponseHeaderBytes <= 0 || c.http.Timeout <= 0 || c.Endpoint() != Endpoint {
		t.Fatal("unsafe production transport")
	}
	if c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect enabled")
	}
	if _, e = c.APIKeyInfo(context.Background()); e != ErrAuthorization {
		t.Fatal(e)
	}
	for _, auth := range []bybit.Auth{{}, {APIKey: "CANARY\nHeader", Signature: strings.Repeat("a", 64)}, {APIKey: fixtureKey, Signature: strings.Repeat("A", 64)}, {APIKey: fixtureKey, Signature: strings.Repeat("a", 64), Passphrase: "CANARY"}, {APIKey: fixtureKey, Signature: strings.Repeat("a", 64), KeyVersion: 2}} {
		c.authorize = func(context.Context, bybit.AuthRequest) (bybit.Auth, error) { return auth, nil }
		if _, e = c.APIKeyInfo(context.Background()); e != ErrAuthorization {
			t.Fatal(e)
		}
	}
	c.authorize = func(context.Context, bybit.AuthRequest) (bybit.Auth, error) {
		return bybit.Auth{}, errors.New("SECRET_CANARY")
	}
	if _, e = c.APIKeyInfo(context.Background()); e != ErrAuthorization {
		t.Fatal(e)
	}
	for _, body := range []string{`null`, `{}`, `{"enableReading":true,"enableReading":false}`, `{"code":null}`, `{"x":"` + string([]byte{0xff}) + `"}`, `{} {}`, strings.Repeat(" ", maxResponseBytes+1)} {
		client := testClient(t, func(*http.Request) string { return body })
		_, e = client.APIKeyInfo(context.Background())
		if e != ErrInvalidResponse {
			t.Fatalf("malformed accepted: %v", e)
		}
	}
	for _, status := range []int{301, 302, 307, 308, 400, 403, 418, 429, 500} {
		client := testClient(t, wireFixture)
		attempts := 0
		client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			r := response("SECRET_CANARY")
			r.StatusCode = status
			return r, nil
		})
		_, e = client.APIKeyInfo(context.Background())
		var api *APIError
		if !errors.As(e, &api) || api.HTTPStatus != status || strings.Contains(e.Error(), "CANARY") || attempts != 1 {
			t.Fatal("unsafe error/retry", e, attempts)
		}
	}
	client := testClient(t, func(*http.Request) string { return `{"code":-1022,"msg":"SECRET_CANARY"}` })
	_, e = client.APIKeyInfo(context.Background())
	if e == nil || e.Error() != "Binance rejected request (code -1022)" {
		t.Fatal(e)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET_CANARY") })
	if _, e = client.APIKeyInfo(context.Background()); e != ErrUnavailable {
		t.Fatal(e)
	}
}

func TestRealTLSRejectsUntrustedCertificatesAndRedirectsWithoutLeakingCredentials(t *testing.T) {
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "api.binance.com"}, DNSNames: []string{"api.binance.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/sapi/v1/account/apiRestrictions" {
			t.Error("followed redirect")
		}
		if r.Header.Get("X-MBX-APIKEY") != fixtureKey {
			t.Error("missing synthetic auth")
		}
		w.Header().Set("Location", "https://attacker.invalid/credentials")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	c, e := New(Endpoint, func(_ context.Context, r bybit.AuthRequest) (bybit.Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), r)
	})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	transport := c.http.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "api.binance.com:443" {
			t.Error("unexpected authority", address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	if _, e = c.APIKeyInfo(context.Background()); e != ErrUnavailable || calls.Load() != 0 {
		t.Fatal("untrusted TLS accepted", e)
	}
	certificate, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	transport.TLSClientConfig.RootCAs = roots
	_, e = c.APIKeyInfo(context.Background())
	var api *APIError
	if !errors.As(e, &api) || api.HTTPStatus != 307 || calls.Load() != 1 {
		t.Fatal("redirect followed/replayed", e, calls.Load())
	}
}

func TestInvalidOperationNeverReachesNetwork(t *testing.T) {
	c := testClient(t, func(*http.Request) string { t.Fatal("invalid operation reached network"); return `{}` })
	o := spotOrder()
	o.OrderType = "Market"
	if _, e := c.CreateOrder(context.Background(), o); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	tx := transfer()
	tx.ToAccountType = "FUTURES"
	if _, e := c.Transfer(context.Background(), tx); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	w := withdrawal()
	w.FeeType = 1
	if _, e := c.Withdraw(context.Background(), w); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	if _, e := c.CoinInfo(context.Background(), "eth"); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	if _, e := c.AccountBalance(context.Background(), "FUTURES", "ETH"); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	if _, e := c.DepositAddress(context.Background(), "ETH", "bad/network"); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	if v, e := c.TransferStatus(context.Background(), fixtureUUID); v != nil || e != nil {
		t.Fatal("lost ack guessed", v, e)
	}
	if _, e := c.TransferStatus(context.Background(), "456"); e != ErrInvalidRequest {
		t.Fatal(e)
	}
	for _, cursor := range []string{"-1", "01", "10001", "opaque"} {
		if _, e := c.DepositRecords(context.Background(), "ETH", "0xabcdef", cursor); e != ErrInvalidRequest {
			t.Fatal(e)
		}
	}
}
