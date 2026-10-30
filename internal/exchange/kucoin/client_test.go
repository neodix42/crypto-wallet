package kucoin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
)

const fixtureKey = "publicSyntheticKucoinKey123"
const fixtureSecret = "PUBLIC_SYNTHETIC_KUCOIN_SECRET_123456789"
const fixturePassphrase = "PUBLIC_SYNTHETIC_KUCOIN_PASSPHRASE"
const fixtureClientID = "0123456789abcdef0123456789abcdef"
const fixtureOrder = `{"id":"remote-order-1","symbol":"ETH-USDT","clientOid":"` + fixtureClientID + `","type":"limit","side":"sell","tradeType":"TRADE","timeInForce":"IOC","price":"2000","size":"0.1","dealSize":"0.1","dealFunds":"200","fee":"0.2","feeCurrency":"USDT","tax":"0","active":false,"cancelExist":false,"createdAt":1700000000000,"lastUpdatedAt":1700000001000}`
const fixtureFill = `{"id":100,"orderId":"remote-order-1","symbol":"ETH-USDT","side":"sell","type":"limit","tradeType":"TRADE","price":"2000","size":"0.1","funds":"200","fee":"0.2","feeCurrency":"USDT","tax":"0","taxRate":"0","createdAt":1700000000000}`
const fixtureDeposit = `{"id":"deposit-1","currency":"ETH","chain":"eth","status":"SUCCESS","address":"0x1111111111111111111111111111111111111111","memo":"","isInner":false,"amount":"0.1","fee":"0","walletTxId":"0xabcdef","createdAt":1700000000000,"updatedAt":1700000001000,"arrears":false,"needSubmitWht":null}`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(data string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":"200000","data":` + data + `}`))}
}
func testClient(t *testing.T, body func(*http.Request) string) *Client {
	t.Helper()
	c, e := New(endpoint, func(_ context.Context, r bybit.AuthRequest) (bybit.Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), 2, r)
	})
	if e != nil {
		t.Fatal(e)
	}
	c.now = func() time.Time { return time.UnixMilli(1700000000000) }
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(body(r)), nil })
	t.Cleanup(c.Close)
	return c
}
func wireFixture(r *http.Request) string {
	switch r.URL.Path {
	case "/api/v1/user/api-key":
		return `{"uid":123,"isMaster":true,"permission":"General,Spot,Transfer,InnerTransfer","apiVersion":2,"siteType":"global"}`
	case "/api/ua/v2/account/mode":
		return `{"selfAccountMode":"CLASSIC"}`
	case "/api/v3/currencies/ETH":
		return `{"currency":"ETH","fullName":"Ethereum","chains":[{"chainId":"eth","chainName":"ERC20","contractAddress":"","withdrawPrecision":8,"depositMinSize":"0.0001","withdrawMinSize":"0.001","withdrawMinFee":"0.0002","withdrawFeeRate":"0","maxWithdraw":"100","isWithdrawEnabled":true,"isDepositEnabled":true,"confirms":12,"preConfirms":64,"needTag":false}]}`
	case "/api/v3/deposit-addresses":
		return `[{"currency":"ETH","address":"0x1111111111111111111111111111111111111111","memo":"","chainId":"eth","chainName":"ERC20","contractAddress":"","to":"MAIN","expirationDate":0}]`
	case "/api/v1/accounts":
		return `[{"id":"account-1","currency":"ETH","type":"main","balance":"1.123456789123456789","available":"1.1","holds":"0.023456789123456789"}]`
	case "/api/v2/symbols/ETH-USDT":
		return `{"symbol":"ETH-USDT","baseCurrency":"ETH","quoteCurrency":"USDT","baseIncrement":"0.0001","quoteIncrement":"0.01","priceIncrement":"0.01","baseMaxSize":"100","minFunds":"5","enableTrading":true,"callauctionIsEnabled":false,"assetClass":"TOKEN"}`
	case "/api/v1/market/orderbook/level1":
		return `{"price":"2000.123456789123456789","bestBid":"2000.01","bestAsk":"2000.02","time":1700000000000}`
	case "/api/v1/hf/orders/client-order/" + fixtureClientID:
		return fixtureOrder
	case "/api/v1/hf/fills":
		return `{"items":[` + fixtureFill + `],"lastId":100}`
	case "/api/v1/deposits":
		return `{"currentPage":1,"pageSize":50,"totalNum":1,"totalPage":1,"items":[` + fixtureDeposit + `]}`
	}
	return `{}`
}
func order() bybit.SpotOrder {
	return bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Limit", Qty: "0.1", MarketUnit: "baseCoin", SlippageToleranceType: "Percent", SlippageTolerance: "0.5", OrderLinkID: fixtureClientID, LimitPrice: "2000", BaseQty: "0.1"}
}
func TestObservedReadsBindNativeWireAndPreserveExactIdentities(t *testing.T) {
	ctx := context.Background()
	for _, f := range []struct {
		name string
		call func(*Client) error
	}{
		{"key", func(c *Client) error {
			v, e := c.APIKeyInfo(ctx)
			if e == nil && (v.UserID != 123 || len(v.Permissions.Wallet) != 2) {
				t.Fatal(v)
			}
			return e
		}},
		{"coin", func(c *Client) error {
			v, e := c.CoinInfo(ctx, "ETH")
			if e == nil && (len(v.Chains) != 1 || v.Chains[0].MinAccuracy != "8" || v.Chains[0].ContractAddress != "") {
				t.Fatal(v)
			}
			return e
		}},
		{"address", func(c *Client) error {
			v, e := c.DepositAddress(ctx, "ETH", "eth")
			if e == nil && v.Address != "0x1111111111111111111111111111111111111111" {
				t.Fatal(v)
			}
			return e
		}},
		{"balance", func(c *Client) error {
			v, e := c.AccountBalance(ctx, "FUND", "ETH")
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
		{"order", func(c *Client) error {
			v, e := c.FindOrder(ctx, "ETHUSDT", fixtureClientID)
			if e == nil && (v.OrderID != "remote-order-1" || v.OrderLinkID != fixtureClientID || v.LimitPrice != "2000" || v.TimeInForce != "IOC") {
				t.Fatal(v)
			}
			return e
		}},
		{"fills", func(c *Client) error {
			v, e := c.Executions(ctx, "ETHUSDT", fixtureClientID, "")
			if e == nil && (len(v.List) != 1 || v.List[0].OrderID != "remote-order-1" || string(v.List[0].ExtraFees) != "[]") {
				t.Fatal(v)
			}
			return e
		}},
		{"deposits", func(c *Client) error {
			v, e := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
			if e == nil && (len(v.Rows) != 1 || v.Rows[0].Status != 3 || v.Rows[0].TxID != "0xabcdef") {
				t.Fatal(v)
			}
			return e
		}},
	} {
		t.Run(f.name, func(t *testing.T) {
			c := testClient(t, wireFixture)
			requests := 0
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.URL.Scheme != "https" || r.URL.Host != "api.kucoin.com" {
					t.Fatal("changed authority")
				}
				private := r.URL.Path != "/api/v3/currencies/ETH" && r.URL.Path != "/api/v2/symbols/ETH-USDT" && r.URL.Path != "/api/v1/market/orderbook/level1"
				if private {
					expected, e := HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), 2, bybit.AuthRequest{Endpoint: endpoint, Method: r.Method, Path: r.URL.Path, Payload: r.URL.RawQuery, TimestampMS: 1700000000000, ReceiveWindowMS: 5000})
					if e != nil || r.Header.Get("KC-API-SIGN") != expected.Signature || r.Header.Get("KC-API-PASSPHRASE") != expected.Passphrase || r.Header.Get("KC-API-PASSPHRASE") == fixturePassphrase || r.Header.Get("KC-API-KEY-VERSION") != "2" {
						t.Fatal("native HMAC/passphrase mismatch", e)
					}
				} else if r.Header.Get("KC-API-KEY") != "" {
					t.Fatal("public request leaked auth")
				}
				return response(wireFixture(r)), nil
			})
			if e := f.call(c); e != nil {
				t.Fatal(e)
			}
			if requests < 1 || requests > 2 {
				t.Fatal("unexpected read count", requests)
			}
		})
	}
}
func TestUnknownKCSModeAndNonClassicAccountsCannotTrade(t *testing.T) {
	c := testClient(t, func(*http.Request) string {
		t.Fatal("CreateOrder reached network with unverified KCS authority")
		return `{}`
	})
	if _, e := c.CreateOrder(context.Background(), order()); e != ErrFeeCurrency {
		t.Fatal(e)
	}
	c = testClient(t, wireFixture)
	if _, e := c.SpotFeeRate(context.Background(), "ETHUSDT"); e != ErrFeeCurrency {
		t.Fatal(e)
	}
	for _, mode := range []string{"UNIFIED", "UTA", "", "CLASSIC2"} {
		c = testClient(t, func(*http.Request) string { return `{"selfAccountMode":"` + mode + `"}` })
		if e := c.WithdrawalAllowed(context.Background()); e != ErrAccountMode {
			t.Fatal("mode changed/fabricated", e)
		}
	}
}
func TestNativeQueryAuthenticationAndOperationTamperingAreDenied(t *testing.T) {
	for _, version := range []int{2, 3} {
		r := bybit.AuthRequest{Endpoint: endpoint, Method: "GET", Path: "/api/v1/hf/orders/client-order/" + fixtureClientID, Payload: "symbol=ETH-USDT", TimestampMS: 1700000000000, ReceiveWindowMS: 5000}
		a, e := HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), version, r)
		if e != nil || a.Passphrase == fixturePassphrase || a.KeyVersion != version {
			t.Fatal(e)
		}
		for _, mutate := range []func(*bybit.AuthRequest){func(r *bybit.AuthRequest) { r.Endpoint += "/" }, func(r *bybit.AuthRequest) { r.Method = "DELETE" }, func(r *bybit.AuthRequest) { r.Path += "/../cancel" }, func(r *bybit.AuthRequest) { r.Payload = "symbol=ETH-USDT&symbol=BTC-USDT" }, func(r *bybit.AuthRequest) { r.Payload = "symbol=%45TH-USDT" }, func(r *bybit.AuthRequest) { r.Payload = "symbol=ETH-USDT&enabled=true" }, func(r *bybit.AuthRequest) { r.TimestampMS = 0 }, func(r *bybit.AuthRequest) { r.ReceiveWindowMS = 60000 }} {
			bad := r
			mutate(&bad)
			if ValidateAuthRequest(bad) == nil || IsReadOnly(bad) {
				t.Fatal("expanded auth authority", bad)
			}
		}
	}
	b, _ := json.Marshal(order())
	r, e := OperationRequest(endpoint, "swap", string(b), 1700000000000)
	if e != nil || IsReadOnly(r) || !strings.Contains(r.Payload, `"timeInForce":"IOC"`) {
		t.Fatal(r, e)
	}
	for _, key := range []string{"postOnly", "leverage", "cancelAfter", "hidden"} {
		var v map[string]any
		json.Unmarshal([]byte(r.Payload), &v)
		v[key] = true
		bad, _ := json.Marshal(v)
		r.Payload = string(bad)
		if ValidateAuthRequest(r) == nil {
			t.Fatal("arbitrary native order authority", key)
		}
	}
	o := order()
	o.Qty = "0.01"
	b, _ = json.Marshal(o)
	if _, e := OperationRequest(endpoint, "swap", string(b), 1); e == nil {
		t.Fatal("native order exceeded budget")
	}
	if _, e := HMAC(fixtureKey, []byte(fixtureSecret), nil, 2, bybit.AuthRequest{}); e == nil {
		t.Fatal("missing passphrase accepted")
	}
}
func TestTLSProxyRedirectAndMalformedEnvelopeControls(t *testing.T) {
	for _, url := range []string{"http://api.kucoin.com", endpoint + "/", "https://api.kucoin.eu", "https://api.kucoin.com.attacker.invalid"} {
		if _, e := New(url, nil); e == nil {
			t.Fatal("custom endpoint", url)
		}
	}
	c, e := New(endpoint, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	tr := c.http.Transport.(*http.Transport)
	if tr.Proxy != nil || tr.TLSClientConfig.InsecureSkipVerify || c.http.Jar != nil || c.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse || c.Endpoint() != endpoint {
		t.Fatal("unsafe transport")
	}
	for _, body := range []string{`null`, `{}`, `{"code":"200000","data":null}`, `{"code":"200000","code":"200000","data":{}}`, `{"code":"400100","msg":"SECRET_CANARY","data":{}}`, `{"code":"200000","data":{}} {}`, strings.Repeat(" ", 1<<20+1)} {
		c = testClient(t, wireFixture)
		c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})
		if _, e := c.APIKeyInfo(context.Background()); e != ErrResponse {
			t.Fatal("bad envelope", e)
		}
	}
	c = testClient(t, wireFixture)
	attempts := 0
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { attempts++; return nil, errors.New("SECRET_CANARY") })
	if _, e := c.APIKeyInfo(context.Background()); e != ErrUnavailable || strings.Contains(e.Error(), "CANARY") || attempts != 1 {
		t.Fatal("unredacted/retried", e)
	}
}
