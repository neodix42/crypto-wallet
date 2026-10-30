package kucoin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
)

func replaceFixture(t *testing.T, body, old, replacement string) string {
	t.Helper()
	if !strings.Contains(body, old) {
		t.Fatal("invalid fixture mutation", old)
	}
	return strings.Replace(body, old, replacement, 1)
}
func TestHostileKuCoinObservationsStayUnavailable(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name, path string
		call       func(*Client) error
		changes    [][2]string
	}{
		{"key", "/api/v1/user/api-key", func(c *Client) error { _, e := c.APIKeyInfo(ctx); return e }, [][2]string{{`"uid":123`, `"uid":0`}, {`"isMaster":true`, `"isMaster":null`}, {`"apiVersion":2`, `"apiVersion":1`}, {`"siteType":"global"`, `"siteType":"eu"`}, {`General,Spot,Transfer,InnerTransfer`, `Spot,Transfer,InnerTransfer`}, {`General,Spot,Transfer,InnerTransfer`, `General,Spot,Spot,Transfer`}}},
		{"coin", "/api/v3/currencies/ETH", func(c *Client) error { _, e := c.CoinInfo(ctx, "ETH"); return e }, [][2]string{{`"currency":"ETH"`, `"currency":"BTC"`}, {`"fullName":"Ethereum"`, `"fullName":""`}, {`"chains":[`, `"chains":null,"ignored":[`}, {`"contractAddress":""`, `"contractAddress":null`}, {`"withdrawPrecision":8`, `"withdrawPrecision":37`}, {`"depositMinSize":"0.0001"`, `"depositMinSize":"bad"`}, {`"withdrawFeeRate":"0"`, `"withdrawFeeRate":"-1"`}, {`"withdrawMinSize":"0.001"`, `"withdrawMinSize":null`}, {`"maxWithdraw":"100"`, `"maxWithdraw":"bad"`}, {`"confirms":12`, `"confirms":-1`}, {`"preConfirms":64`, `"preConfirms":-1`}, {`"needTag":false`, `"needTag":null`}}},
		{"address", "/api/v3/deposit-addresses", func(c *Client) error { _, e := c.DepositAddress(ctx, "ETH", "eth"); return e }, [][2]string{{`"currency":"ETH"`, `"currency":"BTC"`}, {`"chainId":"eth"`, `"chainId":"base"`}, {`"address":"0x1111111111111111111111111111111111111111"`, `"address":""`}, {`"contractAddress":""`, `"contractAddress":null`}, {`"to":"MAIN"`, `"to":"MARGIN"`}, {`"expirationDate":0`, `"expirationDate":1`}, {`"expirationDate":0`, `"expirationDate":null`}}},
		{"balance", "/api/v1/accounts", func(c *Client) error { _, e := c.AccountBalance(ctx, "FUND", "ETH"); return e }, [][2]string{{`"id":"account-1"`, `"id":""`}, {`"currency":"ETH"`, `"currency":"BTC"`}, {`"type":"main"`, `"type":"margin"`}, {`"available":"1.1"`, `"available":"-1"`}, {`"balance":"1.123456789123456789"`, `"balance":null`}}},
		{"instrument", "/api/v2/symbols/ETH-USDT", func(c *Client) error { _, e := c.SpotInstrument(ctx, "ETHUSDT"); return e }, [][2]string{{`"symbol":"ETH-USDT"`, `"symbol":"BTC-USDT"`}, {`"baseCurrency":"ETH"`, `"baseCurrency":"BTC"`}, {`"enableTrading":true`, `"enableTrading":false`}, {`"callauctionIsEnabled":false`, `"callauctionIsEnabled":true`}, {`"baseIncrement":"0.0001"`, `"baseIncrement":"0"`}, {`"priceIncrement":"0.01"`, `"priceIncrement":"bad"`}, {`"baseMaxSize":"100"`, `"baseMaxSize":"bad"`}}},
		{"ticker", "/api/v1/market/orderbook/level1", func(c *Client) error { _, e := c.SpotTicker(ctx, "ETHUSDT"); return e }, [][2]string{{`"price":"2000.123456789123456789"`, `"price":"0"`}, {`"bestBid":"2000.01"`, `"bestBid":"bad"`}, {`"time":1700000000000`, `"time":1699999969999`}, {`"time":1700000000000`, `"time":1700000001001`}}},
		{"order", "/api/v1/hf/orders/client-order/" + fixtureClientID, func(c *Client) error { _, e := c.FindOrder(ctx, "ETHUSDT", fixtureClientID); return e }, [][2]string{{`"id":"remote-order-1"`, `"id":""`}, {`"clientOid":"` + fixtureClientID + `"`, `"clientOid":"wrong"`}, {`"symbol":"ETH-USDT"`, `"symbol":"BTC-USDT"`}, {`"type":"limit"`, `"type":"market"`}, {`"timeInForce":"IOC"`, `"timeInForce":"GTC"`}, {`"tradeType":"TRADE"`, `"tradeType":"MARGIN_TRADE"`}, {`"side":"sell"`, `"side":"borrow"`}, {`"dealSize":"0.1"`, `"dealSize":"0.2"`}, {`"dealSize":"0.1"`, `"dealSize":"0.01"`}, {`"active":false`, `"active":null`}, {`"cancelExist":false`, `"cancelExist":null`}, {`"createdAt":1700000000000`, `"createdAt":null`}, {`"lastUpdatedAt":1700000001000`, `"lastUpdatedAt":1`}}},
		{"fills", "/api/v1/hf/fills", func(c *Client) error { _, e := c.Executions(ctx, "ETHUSDT", fixtureClientID, ""); return e }, [][2]string{{`"lastId":100`, `"lastId":101`}, {`"lastId":100`, `"lastId":null`}, {`"id":100`, `"id":null`}, {`"orderId":"remote-order-1"`, `"orderId":"other"`}, {`"symbol":"ETH-USDT"`, `"symbol":"BTC-USDT"`}, {`"type":"limit"`, `"type":"market"`}, {`"side":"sell"`, `"side":"buy"`}, {`"funds":"200"`, `"funds":"-1"`}, {`"feeCurrency":"USDT"`, `"feeCurrency":"bad/currency"`}, {`"tax":"0"`, `"tax":"0.01"`}, {`"createdAt":1700000000000`, `"createdAt":null`}}},
		{"deposits", "/api/v1/deposits", func(c *Client) error { _, e := c.DepositRecords(ctx, "ETH", "0xabcdef", ""); return e }, [][2]string{{`"currentPage":1`, `"currentPage":2`}, {`"pageSize":50`, `"pageSize":null`}, {`"totalPage":1`, `"totalPage":2`}, {`"items":[`, `"items":null,"ignored":[`}, {`"currency":"ETH"`, `"currency":"BTC"`}, {`"status":"SUCCESS"`, `"status":"FABRICATED"`}, {`"isInner":false`, `"isInner":null`}, {`"amount":"0.1"`, `"amount":"-1"`}, {`"createdAt":1700000000000`, `"createdAt":null`}, {`"arrears":false`, `"arrears":null`}}},
	}
	for _, f := range tests {
		t.Run(f.name, func(t *testing.T) {
			for i, change := range f.changes {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					c := testClient(t, func(r *http.Request) string {
						b := wireFixture(r)
						if r.URL.Path == f.path {
							return replaceFixture(t, b, change[0], change[1])
						}
						return b
					})
					if e := f.call(c); e != ErrResponse {
						t.Fatal("malformed provider result accepted", e)
					}
				})
			}
			c := testClient(t, wireFixture)
			c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("SECRET_CANARY") })
			if e := f.call(c); e != ErrUnavailable {
				t.Fatal("provider failure promoted/echoed", e)
			}
		})
	}
}
func TestKuCoinPaginationUsesExactOriginalOrderAndTransaction(t *testing.T) {
	ctx := context.Background()
	for _, cursor := range []string{"-1", "01", "bad"} {
		c := testClient(t, wireFixture)
		if _, e := c.Executions(ctx, "ETHUSDT", fixtureClientID, cursor); e != ErrRequest {
			t.Fatal(e)
		}
		if _, e := c.DepositRecords(ctx, "ETH", "0xabcdef", cursor); e != ErrRequest {
			t.Fatal(e)
		}
	}
	for _, invalid := range []struct{ currency, tx, cursor string }{{"eth", "0xabcdef", ""}, {"ETH", "", ""}, {"ETH", "0xabcdef", "0"}, {"ETH", "0xabcdef", "201"}} {
		c := testClient(t, wireFixture)
		if _, e := c.DepositRecords(ctx, invalid.currency, invalid.tx, invalid.cursor); e != ErrRequest {
			t.Fatal(e)
		}
	}
	for _, change := range [][2]string{{`"walletTxId":"0xabcdef"`, `"walletTxId":"foreign"`}, {`"isInner":false`, `"isInner":true`}, {`"walletTxId":"0xabcdef"`, `"walletTxId":null`}} {
		c := testClient(t, func(r *http.Request) string {
			b := wireFixture(r)
			if r.URL.Path == "/api/v1/deposits" {
				return replaceFixture(t, b, change[0], change[1])
			}
			return b
		})
		v, e := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
		if e != nil || len(v.Rows) != 0 {
			t.Fatal("unrelated deposit credited", v, e)
		}
	}
	for _, status := range []string{"PROCESSING", "WALLET_PROCESSING", "FAILURE"} {
		c := testClient(t, func(r *http.Request) string {
			return strings.Replace(wireFixture(r), `"status":"SUCCESS"`, `"status":"`+status+`"`, 1)
		})
		if _, e := c.DepositRecords(ctx, "ETH", "0xabcdef", ""); e != nil {
			t.Fatal(e)
		}
	}
	for _, mutate := range []func(string) string{func(s string) string { return strings.Replace(s, `"arrears":false`, `"arrears":true`, 1) }, func(s string) string { return strings.Replace(s, `"needSubmitWht":null`, `"needSubmitWht":true`, 1) }} {
		c := testClient(t, func(r *http.Request) string { return mutate(wireFixture(r)) })
		v, e := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
		if e != nil || v.Rows[0].TravelRuleStatus != 1 {
			t.Fatal("withheld funds promoted to free", v, e)
		}
	}
	rows := []string{}
	for i := 200; i > 100; i-- {
		rows = append(rows, strings.Replace(fixtureFill, `"id":100`, `"id":`+fmt.Sprint(i), 1))
	}
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Path == "/api/v1/hf/fills" {
			return `{"items":[` + strings.Join(rows, ",") + `],"lastId":101}`
		}
		return wireFixture(r)
	})
	v, e := c.Executions(ctx, "ETHUSDT", fixtureClientID, "")
	if e != nil || len(v.List) != 100 || v.NextPageCursor != "101" {
		t.Fatal("lost bounded cursor", e)
	}
	c = testClient(t, wireFixture)
	if _, e = c.Executions(ctx, "ETHUSDT", fixtureClientID, "100"); e != ErrResponse {
		t.Fatal("replayed fill boundary")
	}
	for _, items := range []string{fixtureFill + `,` + fixtureFill, strings.Replace(fixtureFill, `"id":100`, `"id":99`, 1) + `,` + fixtureFill} {
		c = testClient(t, func(r *http.Request) string {
			if r.URL.Path == "/api/v1/hf/fills" {
				return `{"items":[` + items + `],"lastId":100}`
			}
			return wireFixture(r)
		})
		if _, e = c.Executions(ctx, "ETHUSDT", fixtureClientID, ""); e != ErrResponse {
			t.Fatal("duplicate/reordered fills accepted")
		}
	}
	for _, active := range []bool{false, true} {
		for _, cancel := range []bool{false, true} {
			c = testClient(t, func(r *http.Request) string {
				b := wireFixture(r)
				if strings.Contains(r.URL.Path, "/client-order/") {
					b = strings.Replace(b, `"side":"sell"`, `"side":"buy"`, 1)
					b = strings.Replace(b, `"active":false`, `"active":`+fmt.Sprint(active), 1)
					b = strings.Replace(b, `"cancelExist":false`, `"cancelExist":`+fmt.Sprint(cancel), 1)
				}
				return b
			})
			if _, e = c.FindOrder(ctx, "ETHUSDT", fixtureClientID); e != nil {
				t.Fatal(e)
			}
		}
	}
}
func TestKuCoinSecretHeadersAndNativePayloadsHaveIndependentBounds(t *testing.T) {
	ctx := context.Background()
	r := bybit.AuthRequest{Endpoint: endpoint, Method: "GET", Path: "/api/v1/hf/orders/client-order/" + fixtureClientID, Payload: "symbol=ETH-USDT", TimestampMS: 1700000000000, ReceiveWindowMS: 5000}
	a, e := HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), 3, r)
	if e != nil {
		t.Fatal(e)
	}
	if a.Signature != "LuuVBGh9UO4KA/UaeDsjmyuQURPIqlDIbLM8TLMnOjg=" || a.Passphrase != "9XvFgZ31PqX/QeZWcYw/nLEDArOeU6gEjJYu/79C/EQ=" {
		t.Fatal("independent native auth vector mismatch")
	}
	for _, version := range []int{0, 1, 4} {
		if _, e := HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), version, r); e == nil {
			t.Fatal("unsupported key version")
		}
	}
	c := testClient(t, wireFixture)
	for _, auth := range []bybit.Auth{{}, {APIKey: "bad\nKey"}, {APIKey: fixtureKey, KeyVersion: 2, Signature: "not-base64", Passphrase: "not-base64"}, {APIKey: fixtureKey, KeyVersion: 1, Signature: a.Signature, Passphrase: a.Passphrase}, {APIKey: fixtureKey, KeyVersion: 3, Signature: a.Signature, Passphrase: fixturePassphrase}} {
		c.authorize = func(context.Context, bybit.AuthRequest) (bybit.Auth, error) { return auth, nil }
		if _, e := c.APIKeyInfo(ctx); e != ErrRequest {
			t.Fatal("secret header accepted", e)
		}
	}
	c.authorize = nil
	if _, e := c.APIKeyInfo(ctx); e != ErrRequest {
		t.Fatal(e)
	}
	for _, kind := range []string{"swap", "transfer", "withdraw"} {
		if _, e := OperationRequest(endpoint, kind, `null`, 1); e == nil {
			t.Fatal("null immutable payload")
		}
		if _, e := OperationRequest(endpoint, kind, `{"extra":"authority"}`, 1); e == nil {
			t.Fatal("unknown immutable payload")
		}
	}
	if _, e := OperationRequest(endpoint, "arbitrary", `{}`, 1); e == nil {
		t.Fatal("unknown operation")
	}
	w := fixtureWithdrawal()
	w.Tag = "123"
	encoded, _ := json.Marshal(w)
	req, e := OperationRequest(endpoint, "withdraw", string(encoded), 1)
	if e != nil || !strings.Contains(req.Payload, `"memo":"123"`) || !strings.Contains(req.Payload, `"remark":`) {
		t.Fatal("memo/original identity lost", e)
	}
	for _, mutate := range []func(*bybit.WithdrawalRequest){func(w *bybit.WithdrawalRequest) { w.FeeMode = "binance-native" }, func(w *bybit.WithdrawalRequest) { w.FeeType = 1 }, func(w *bybit.WithdrawalRequest) { w.Address = "bad\nHeader" }, func(w *bybit.WithdrawalRequest) { w.Beneficiary = &bybit.UAEBeneficiary{} }, func(w *bybit.WithdrawalRequest) { w.Timestamp = 0 }} {
		w := fixtureWithdrawal()
		mutate(&w)
		b, _ := json.Marshal(w)
		if _, e := OperationRequest(endpoint, "withdraw", string(b), 1); e == nil {
			t.Fatal("withdrawal authority expanded")
		}
	}
	tx := bybit.TransferRequest{TransferID: "11111111-1111-4111-8111-111111111111", Coin: "ETH", Amount: "1", FromAccountType: "UNIFIED", ToAccountType: "FUND"}
	b, _ := json.Marshal(tx)
	req, e = OperationRequest(endpoint, "transfer", string(b), 1)
	if e != nil || !strings.Contains(req.Payload, `"fromAccountType":"TRADE","toAccountType":"MAIN"`) {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v3/currencies/ETH", "/api/v2/symbols/ETH-USDT", "/api/v1/withdrawals/known", "/api/v1/trade-fees", "/api/v1/withdrawals/quotas", "/api/v1/accounts"} {
		bad := r
		bad.Path = path
		bad.Payload = "extra=authority"
		if ValidateAuthRequest(bad) == nil {
			t.Fatal("read authority expanded", path)
		}
	}
}

func TestKuCoinOwnedTLSAndDeadlineNeverRedirectOrRetry(t *testing.T) {
	requests := 0
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	client, err := New(endpoint, func(_ context.Context, r bybit.AuthRequest) (bybit.Auth, error) {
		return HMAC(fixtureKey, []byte(fixtureSecret), []byte(fixturePassphrase), 2, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	transport := client.http.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, authority string) (net.Conn, error) {
		if authority != "api.kucoin.com:443" {
			t.Error("changed TLS authority")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	if _, err = client.APIKeyInfo(context.Background()); err != ErrUnavailable || requests != 0 {
		t.Fatal("untrusted TLS reached API", err, requests)
	}
	started, release := make(chan struct{}), make(chan struct{})
	client = fundingFixture(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := client.APIKeyInfo(ctx); done <- e }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("owned timeout fixture never started")
	}
	select {
	case e := <-done:
		close(release)
		if e != ErrUnavailable {
			t.Fatal("deadline leaked detail or succeeded", e)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("deadline did not bound request")
	}
}

func TestKuCoinDepositContinuationDoesNotTruncateOrCreditForeignTransactions(t *testing.T) {
	rows := []string{}
	for i := 0; i < 50; i++ {
		rows = append(rows, strings.Replace(fixtureDeposit, `"id":"deposit-1"`, `"id":"deposit-`+fmt.Sprint(i)+`"`, 1))
	}
	client := testClient(t, func(r *http.Request) string {
		if r.URL.Query().Get("currentPage") == "2" {
			return `{"currentPage":2,"pageSize":50,"totalNum":51,"totalPage":2,"items":[` + fixtureDeposit + `]}`
		}
		return `{"currentPage":1,"pageSize":50,"totalNum":51,"totalPage":2,"items":[` + strings.Join(rows, ",") + `]}`
	})
	page, err := client.DepositRecords(context.Background(), "ETH", "0xabcdef", "")
	if err != nil || page.NextPageCursor != "2" || len(page.Rows) != 50 {
		t.Fatal("lost deposit continuation", page, err)
	}
	page, err = client.DepositRecords(context.Background(), "ETH", "0xabcdef", "2")
	if err != nil || page.NextPageCursor != "" || len(page.Rows) != 1 {
		t.Fatal("last deposit page incomplete", page, err)
	}
	for _, items := range []string{strings.Join(rows[:49], ","), fixtureDeposit + "," + fixtureDeposit} {
		client = testClient(t, func(*http.Request) string {
			return `{"currentPage":1,"pageSize":50,"totalNum":51,"totalPage":2,"items":[` + items + `]}`
		})
		if _, err = client.DepositRecords(context.Background(), "ETH", "0xabcdef", ""); err != ErrResponse {
			t.Fatal("truncated/duplicate deposit page accepted")
		}
	}
}
