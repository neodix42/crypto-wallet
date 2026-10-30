// Package kucoin implements the fixed KuCoin Classic spot/asset API. It has no
// secret-bearing constructor and cannot manage keys, account modes or leverage.
package kucoin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/amount"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/strictjson"
)

const endpoint = "https://api.kucoin.com"

var ErrRequest = errors.New("invalid KuCoin request")
var ErrResponse = errors.New("invalid or incomplete KuCoin response")
var ErrUnavailable = errors.New("KuCoin connection unavailable; reconcile the original operation before retrying")
var ErrAccountMode = errors.New("KuCoin requires a Classic spot account for this integration; no account-mode change was made")
var ErrFeeCurrency = errors.New("KuCoin Classic fee payment mode is unverified; trading is unavailable until KCS fee deductions can be bounded")

type Client struct {
	endpoint  string
	authorize bybit.Authorizer
	http      *http.Client
	now       func() time.Time
}

func New(service string, authorize bybit.Authorizer) (*Client, error) {
	if service != endpoint {
		return nil, ErrRequest
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxConnsPerHost: 4, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	return &Client{endpoint: service, authorize: authorize, now: time.Now, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close()           { c.http.CloseIdleConnections() }
func (c *Client) Endpoint() string { return c.endpoint }
func identifier(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}
func coin(value string) bool {
	if !identifier(value, 24) {
		return false
	}
	return value == strings.ToUpper(value) && !strings.ContainsAny(value, "-_")
}
func decimal(value string, positive bool) bool {
	v, err := amount.Parse(value, 36)
	return err == nil && (!positive || v != "0")
}
func nativeSymbol(symbol string) (string, bool) {
	base, quote, ok := bybit.SpotPair(symbol)
	return base + "-" + quote, ok
}
func canonicalQuery(raw string, required []string, optional ...string) bool {
	values, err := url.ParseQuery(raw)
	if err != nil || values.Encode() != raw {
		return false
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if len(values[key]) != 1 || values.Get(key) == "" {
			return false
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 || !identifier(entries[0], 120) {
			return false
		}
	}
	return true
}
func IsReadOnly(r bybit.AuthRequest) bool { return r.Method == "GET" && ValidateAuthRequest(r) == nil }
func ValidateAuthRequest(r bybit.AuthRequest) error {
	if r.Endpoint != endpoint || r.TimestampMS <= 0 || r.ReceiveWindowMS != 5000 || len(r.Payload) > 8192 {
		return ErrRequest
	}
	if r.Method == "POST" {
		var value any
		switch r.Path {
		case "/api/v1/hf/orders":
			var order wireOrder
			if strictjson.Decode([]byte(r.Payload), &order) != nil || !order.valid() {
				return ErrRequest
			}
			value = order
		case "/api/v3/accounts/universal-transfer":
			var transfer wireTransfer
			if strictjson.Decode([]byte(r.Payload), &transfer) != nil || !transfer.valid() {
				return ErrRequest
			}
			value = transfer
		case "/api/v3/withdrawals":
			var withdrawal wireWithdrawal
			if strictjson.Decode([]byte(r.Payload), &withdrawal) != nil || !withdrawal.valid() {
				return ErrRequest
			}
			value = withdrawal
		default:
			return ErrRequest
		}
		canonical, _ := json.Marshal(value)
		if string(canonical) != r.Payload {
			return ErrRequest
		}
		return nil
	}
	if r.Method != "GET" {
		return ErrRequest
	}
	query := false
	switch r.Path {
	case "/api/v1/user/api-key", "/api/ua/v2/account/mode":
		query = r.Payload == ""
	case "/api/v3/deposit-addresses", "/api/v1/withdrawals/quotas":
		query = canonicalQuery(r.Payload, []string{"currency", "chain"})
	case "/api/v1/accounts":
		query = canonicalQuery(r.Payload, []string{"currency", "type"})
		v, _ := url.ParseQuery(r.Payload)
		query = query && (v.Get("type") == "main" || v.Get("type") == "trade")
	case "/api/v1/market/orderbook/level1":
		query = canonicalQuery(r.Payload, []string{"symbol"})
	case "/api/v1/trade-fees":
		query = canonicalQuery(r.Payload, []string{"symbols"})
	case "/api/v1/hf/fills":
		query = canonicalQuery(r.Payload, []string{"symbol", "orderId", "limit"}, "lastId")
		v, _ := url.ParseQuery(r.Payload)
		query = query && v.Get("limit") == "100"
	case "/api/v1/deposits":
		query = canonicalQuery(r.Payload, []string{"currency", "currentPage", "pageSize"})
		v, _ := url.ParseQuery(r.Payload)
		query = query && v.Get("pageSize") == "50"
	default:
		for _, prefix := range []string{"/api/v3/currencies/", "/api/v2/symbols/", "/api/v1/hf/orders/client-order/", "/api/v1/withdrawals/"} {
			if strings.HasPrefix(r.Path, prefix) && identifier(strings.TrimPrefix(r.Path, prefix), 120) {
				query = r.Payload == ""
				if prefix == "/api/v1/hf/orders/client-order/" {
					query = canonicalQuery(r.Payload, []string{"symbol"})
				}
				break
			}
		}
	}
	if !query {
		return ErrRequest
	}
	return nil
}
func HMAC(key string, secret, passphrase []byte, version int, r bybit.AuthRequest) (bybit.Auth, error) {
	if ValidateAuthRequest(r) != nil || !identifier(key, 128) || len(secret) < 16 || len(secret) > 512 || len(passphrase) == 0 || len(passphrase) > 256 || version != 2 && version != 3 {
		return bybit.Auth{}, ErrRequest
	}
	path, body := r.Path, ""
	if r.Method == "GET" && r.Payload != "" {
		decoded, err := url.QueryUnescape(r.Payload)
		if err != nil {
			return bybit.Auth{}, ErrRequest
		}
		path += "?" + decoded
	} else if r.Method == "POST" {
		body = r.Payload
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(r.TimestampMS, 10) + r.Method + path + body))
	auth := bybit.Auth{APIKey: key, Signature: base64.StdEncoding.EncodeToString(mac.Sum(nil)), KeyVersion: version}
	mac.Reset()
	mac.Write(passphrase)
	auth.Passphrase = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return auth, nil
}

func (c *Client) do(ctx context.Context, method, path, payload string, private bool, out any) error {
	requestPolicy := bybit.AuthRequest{Endpoint: c.endpoint, Method: method, Path: path, Payload: payload, TimestampMS: c.now().UnixMilli(), ReceiveWindowMS: 5000}
	if ValidateAuthRequest(requestPolicy) != nil {
		return ErrRequest
	}
	uri := c.endpoint + path
	var body io.Reader
	if method == "GET" && payload != "" {
		uri += "?" + payload
	} else if method == "POST" {
		body = strings.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, uri, body)
	if err != nil {
		return ErrRequest
	}
	request.GetBody = nil
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if private {
		if c.authorize == nil {
			return ErrRequest
		}
		auth, err := c.authorize(ctx, requestPolicy)
		if err != nil || !identifier(auth.APIKey, 128) || auth.KeyVersion != 2 && auth.KeyVersion != 3 {
			return ErrRequest
		}
		for _, value := range []string{auth.Signature, auth.Passphrase} {
			decoded, err := base64.StdEncoding.DecodeString(value)
			if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != value {
				return ErrRequest
			}
		}
		request.Header.Set("KC-API-KEY", auth.APIKey)
		request.Header.Set("KC-API-SIGN", auth.Signature)
		request.Header.Set("KC-API-PASSPHRASE", auth.Passphrase)
		request.Header.Set("KC-API-KEY-VERSION", strconv.Itoa(auth.KeyVersion))
		request.Header.Set("KC-API-TIMESTAMP", strconv.FormatInt(requestPolicy.TimestampMS, 10))
		defer func() {
			for _, header := range []string{"KC-API-KEY", "KC-API-SIGN", "KC-API-PASSPHRASE"} {
				request.Header.Del(header)
			}
		}()
	}
	response, err := c.http.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || !utf8.Valid(raw) || chainutil.CheckJSON(raw) != nil {
		return ErrResponse
	}
	var envelope struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Code != "200000" || len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) || json.Unmarshal(envelope.Data, out) != nil {
		return ErrResponse
	}
	return nil
}
func (c *Client) get(ctx context.Context, path string, query url.Values, private bool, out any) error {
	return c.do(ctx, "GET", path, query.Encode(), private, out)
}
func (c *Client) classic(ctx context.Context) error {
	var mode struct {
		SelfAccountMode string `json:"selfAccountMode"`
	}
	if err := c.get(ctx, "/api/ua/v2/account/mode", nil, true, &mode); err != nil {
		return err
	}
	if mode.SelfAccountMode != "CLASSIC" {
		return ErrAccountMode
	}
	return nil
}
func (c *Client) APIKeyInfo(ctx context.Context) (bybit.APIKeyInformation, error) {
	var data struct {
		UID        int64  `json:"uid"`
		IsMaster   *bool  `json:"isMaster"`
		Permission string `json:"permission"`
		APIVersion int    `json:"apiVersion"`
		SiteType   string `json:"siteType"`
	}
	var result bybit.APIKeyInformation
	if err := c.get(ctx, "/api/v1/user/api-key", nil, true, &data); err != nil {
		return result, err
	}
	if data.UID <= 0 || data.IsMaster == nil || len(data.Permission) > 512 || data.APIVersion != 2 && data.APIVersion != 3 || data.SiteType != "" && data.SiteType != "global" {
		return result, ErrResponse
	}
	result.UserID = data.UID
	result.IsMaster = *data.IsMaster
	result.ReadOnly = 1
	seen := map[string]bool{}
	for _, permission := range strings.Split(data.Permission, ",") {
		if !identifier(permission, 40) || seen[permission] {
			return result, ErrResponse
		}
		seen[permission] = true
		switch permission {
		case "Spot":
			result.Permissions.Spot = append(result.Permissions.Spot, "SpotTrade")
			result.ReadOnly = 0
		case "Transfer":
			result.Permissions.Wallet = append(result.Permissions.Wallet, "Withdraw")
			result.ReadOnly = 0
		case "InnerTransfer":
			result.Permissions.Wallet = append(result.Permissions.Wallet, "AccountTransfer")
			result.ReadOnly = 0
		}
	}
	if !seen["General"] {
		return bybit.APIKeyInformation{}, ErrResponse
	}
	return result, nil
}

type wireOrder struct {
	ClientOID   string `json:"clientOid"`
	Symbol      string `json:"symbol"`
	Type        string `json:"type"`
	Side        string `json:"side"`
	Price       string `json:"price"`
	Size        string `json:"size"`
	TimeInForce string `json:"timeInForce"`
}

func (w wireOrder) valid() bool {
	return identifier(w.ClientOID, 36) && identifier(w.Symbol, 49) && strings.Count(w.Symbol, "-") == 1 && w.Type == "limit" && (w.Side == "buy" || w.Side == "sell") && decimal(w.Price, true) && decimal(w.Size, true) && w.TimeInForce == "IOC"
}

type wireTransfer struct {
	ClientOID       string `json:"clientOid"`
	Type            string `json:"type"`
	Currency        string `json:"currency"`
	Amount          string `json:"amount"`
	FromAccountType string `json:"fromAccountType"`
	ToAccountType   string `json:"toAccountType"`
}

func (w wireTransfer) valid() bool {
	return identifier(w.ClientOID, 36) && w.Type == "INTERNAL" && coin(w.Currency) && decimal(w.Amount, true) && (w.FromAccountType == "MAIN" && w.ToAccountType == "TRADE" || w.FromAccountType == "TRADE" && w.ToAccountType == "MAIN")
}

type wireWithdrawal struct {
	Currency      string `json:"currency"`
	ToAddress     string `json:"toAddress"`
	Amount        string `json:"amount"`
	WithdrawType  string `json:"withdrawType"`
	Chain         string `json:"chain"`
	IsInner       bool   `json:"isInner"`
	Memo          string `json:"memo,omitempty"`
	FeeDeductType string `json:"feeDeductType"`
	Remark        string `json:"remark"`
}

func (w wireWithdrawal) valid() bool {
	return coin(w.Currency) && identifier(w.Chain, 40) && len(w.ToAddress) > 0 && len(w.ToAddress) <= 256 && !strings.ContainsAny(w.ToAddress, "\x00\r\n") && decimal(w.Amount, true) && w.WithdrawType == "ADDRESS" && !w.IsInner && len(w.Memo) <= 120 && !strings.ContainsAny(w.Memo, "\x00\r\n") && w.FeeDeductType == "EXTERNAL" && identifier(w.Remark, 32)
}

// OperationRequest reconstructs exact native wire bytes from the persisted
// normalized immutable operation. No endpoint accepts arbitrary JSON or calls.
func OperationRequest(service, kind, payload string, timestampMS int64) (bybit.AuthRequest, error) {
	r := bybit.AuthRequest{Endpoint: service, Method: "POST", TimestampMS: timestampMS, ReceiveWindowMS: 5000}
	var wire any
	switch kind {
	case "swap":
		var order bybit.SpotOrder
		if strictjson.Decode([]byte(payload), &order) != nil || exchange.ValidateIOCOrder(order) != nil {
			return r, ErrRequest
		}
		symbol, _ := nativeSymbol(order.Symbol)
		wire = wireOrder{order.OrderLinkID, symbol, "limit", strings.ToLower(order.Side), order.LimitPrice, order.BaseQty, "IOC"}
		r.Path = "/api/v1/hf/orders"
	case "transfer":
		var transfer bybit.TransferRequest
		if strictjson.Decode([]byte(payload), &transfer) != nil || bybit.ValidateTransfer(transfer) != nil {
			return r, ErrRequest
		}
		from, to := "MAIN", "TRADE"
		if transfer.FromAccountType == "UNIFIED" {
			from, to = to, from
		}
		wire = wireTransfer{transfer.TransferID, "INTERNAL", transfer.Coin, transfer.Amount, from, to}
		r.Path = "/api/v3/accounts/universal-transfer"
	case "withdraw":
		var withdrawal bybit.WithdrawalRequest
		if strictjson.Decode([]byte(payload), &withdrawal) != nil || withdrawal.FeeMode != "kucoin-external" || withdrawal.FeeType != 0 || withdrawal.Beneficiary != nil || withdrawal.ForceChain != 1 || withdrawal.AccountType != "FUND" || !identifier(withdrawal.RequestID, 32) || withdrawal.Timestamp <= 0 {
			return r, ErrRequest
		}
		wire = wireWithdrawal{withdrawal.Coin, withdrawal.Address, withdrawal.Amount, "ADDRESS", withdrawal.Chain, false, withdrawal.Tag, "EXTERNAL", withdrawal.RequestID}
		r.Path = "/api/v3/withdrawals"
	default:
		return r, ErrRequest
	}
	encoded, _ := json.Marshal(wire)
	r.Payload = string(encoded)
	if ValidateAuthRequest(r) != nil {
		return r, ErrRequest
	}
	return r, nil
}
