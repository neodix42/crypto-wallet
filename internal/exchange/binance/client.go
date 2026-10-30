package binance

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/exchange/bybit"
)

const maxResponseBytes = 1 << 20

type Client struct {
	endpoint  string
	authorize bybit.Authorizer
	http      *http.Client
	now       func() time.Time
}

func New(endpoint string, authorize bybit.Authorizer) (*Client, error) {
	if endpoint != Endpoint {
		return nil, ErrInvalidRequest
	}
	t := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxConnsPerHost: 4, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	return &Client{endpoint: endpoint, authorize: authorize, now: time.Now, http: &http.Client{Transport: t, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close()           { c.http.CloseIdleConnections() }
func (c *Client) Endpoint() string { return c.endpoint }

type APIError struct {
	Code       int64
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.HTTPStatus != 0 {
		return "Binance rejected request (HTTP " + strconv.Itoa(e.HTTPStatus) + ")"
	}
	return "Binance rejected request (code " + strconv.FormatInt(e.Code, 10) + ")"
}
func (c *Client) do(ctx context.Context, method, path string, q url.Values, private bool, out any) error {
	r := bybit.AuthRequest{Endpoint: c.endpoint, Method: method, Path: path, Payload: q.Encode(), TimestampMS: c.now().UnixMilli(), ReceiveWindowMS: 5000}
	if ValidateAuthRequest(r) != nil {
		return ErrInvalidRequest
	}
	payload := r.Payload
	var auth bybit.Auth
	if private {
		if c.authorize == nil {
			return ErrAuthorization
		}
		var e error
		auth, e = c.authorize(ctx, r)
		sig, se := hex.DecodeString(auth.Signature)
		if e != nil || !identifier(auth.APIKey, 128) || se != nil || len(sig) != 32 || strings.ToLower(auth.Signature) != auth.Signature || auth.Passphrase != "" || auth.KeyVersion != 0 {
			return ErrAuthorization
		}
		payload = wireQuery(r) + "&signature=" + auth.Signature
	}
	uri := c.endpoint + path
	var body io.Reader
	if method == "POST" {
		body = strings.NewReader(payload)
	} else if payload != "" {
		uri += "?" + payload
	}
	req, e := http.NewRequestWithContext(ctx, method, uri, body)
	if e != nil {
		return ErrInvalidRequest
	}
	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if private {
		req.Header.Set("X-MBX-APIKEY", auth.APIKey)
		defer req.Header.Del("X-MBX-APIKEY")
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{HTTPStatus: resp.StatusCode}
	}
	limit := maxResponseBytes
	if path == "/sapi/v1/capital/config/getall" {
		limit = 8 << 20
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if e != nil || len(b) > limit || !utf8.Valid(b) || chainutil.CheckJSON(b) != nil {
		return ErrInvalidResponse
	}
	var envelope struct {
		Code *int64 `json:"code"`
	}
	if len(bytes.TrimSpace(b)) > 0 && bytes.TrimSpace(b)[0] == '{' && json.Unmarshal(b, &envelope) == nil && envelope.Code != nil && *envelope.Code != 0 {
		return &APIError{Code: *envelope.Code}
	}
	if json.Unmarshal(b, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}
func (c *Client) get(ctx context.Context, path string, q url.Values, private bool, out any) error {
	return c.do(ctx, "GET", path, q, private, out)
}
func required(b []byte, names ...string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil || obj == nil {
		return false
	}
	for _, name := range names {
		v, ok := obj[name]
		if !ok || strings.TrimSpace(string(v)) == "null" {
			return false
		}
	}
	return true
}
func canonicalCursor(cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	v, e := strconv.ParseUint(cursor, 10, 32)
	if e != nil || strconv.FormatUint(v, 10) != cursor || v > 10000 {
		return 0, ErrInvalidRequest
	}
	return v, nil
}
