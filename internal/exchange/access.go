package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
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

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/exchange/bybit"
)

var ErrAccess = errors.New("exchange authentication or response unavailable")

// ValidateAccessRequest is a read-only allowlist. It cannot authorize trading,
// withdrawals, key management, account-mode changes or arbitrary HTTP requests.
func ValidateAccessRequest(request bybit.AuthRequest) error {
	provider, ok := ProviderForEndpoint(request.Endpoint)
	if !ok || request.Method != "GET" || request.Payload != "" || request.TimestampMS <= 0 || request.ReceiveWindowMS != 5000 {
		return ErrAccess
	}
	if provider.ID == "binance" && request.Path == "/sapi/v1/account/apiRestrictions" || provider.ID == "kucoin" && request.Path == "/api/v1/user/api-key" {
		return nil
	}
	return ErrAccess
}

// AccessAuth derives short-lived service authorization; it never returns the
// secret or the KuCoin API passphrase in plaintext. Binance signs its exact
// canonical timestamp/window query. KuCoin additionally signs method and path.
func AccessAuth(key string, secret, passphrase []byte, version int, request bybit.AuthRequest) (bybit.Auth, error) {
	if ValidateAccessRequest(request) != nil || len(key) < 8 || len(key) > 128 || len(secret) < 16 || len(secret) > 512 {
		return bybit.Auth{}, ErrAccess
	}
	provider, _ := ProviderForEndpoint(request.Endpoint)
	mac := hmac.New(sha256.New, secret)
	auth := bybit.Auth{APIKey: key}
	if provider.ID == "binance" {
		if len(passphrase) != 0 || version != 0 {
			return bybit.Auth{}, ErrAccess
		}
		mac.Write([]byte(accessQuery(request)))
		auth.Signature = hex.EncodeToString(mac.Sum(nil))
	} else {
		if len(passphrase) < 1 || len(passphrase) > 256 || version != 2 && version != 3 {
			return bybit.Auth{}, ErrAccess
		}
		mac.Write([]byte(strconv.FormatInt(request.TimestampMS, 10) + request.Method + request.Path))
		auth.Signature = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		mac.Reset()
		mac.Write(passphrase)
		auth.Passphrase = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		auth.KeyVersion = version
	}
	return auth, nil
}

func accessQuery(request bybit.AuthRequest) string {
	return url.Values{"recvWindow": {"5000"}, "timestamp": {strconv.FormatInt(request.TimestampMS, 10)}}.Encode()
}

type AccessClient struct {
	endpoint  string
	authorize bybit.Authorizer
	http      *http.Client
	now       func() time.Time
}

func NewAccess(endpoint string, authorize bybit.Authorizer) (*AccessClient, error) {
	provider, ok := ProviderForEndpoint(endpoint)
	if !ok || provider.ID == "bybit" {
		return nil, ErrAccess
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxConnsPerHost: 4,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxResponseHeaderBytes: 16 << 10, DisableCompression: true}
	return &AccessClient{endpoint: endpoint, authorize: authorize, now: time.Now, http: &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *AccessClient) Close()           { c.http.CloseIdleConnections() }
func (c *AccessClient) Endpoint() string { return c.endpoint }

// APIKeyInfo confirms the selected service's key and projects only its bounded
// account permissions. Provider echoes of keys and account/PII never escape.
func (c *AccessClient) APIKeyInfo(ctx context.Context) (bybit.APIKeyInformation, error) {
	provider, _ := ProviderForEndpoint(c.endpoint)
	path := "/sapi/v1/account/apiRestrictions"
	if provider.ID == "kucoin" {
		path = "/api/v1/user/api-key"
	}
	authRequest := bybit.AuthRequest{Endpoint: c.endpoint, Method: "GET", Path: path, TimestampMS: c.now().UnixMilli(), ReceiveWindowMS: 5000}
	if ValidateAccessRequest(authRequest) != nil || c.authorize == nil {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	auth, err := c.authorize(ctx, authRequest)
	if err != nil || !accessHeader(auth.APIKey, 128) {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	uri := c.endpoint + path
	if provider.ID == "binance" {
		signature, e := hex.DecodeString(auth.Signature)
		if e != nil || len(signature) != 32 || auth.Signature != strings.ToLower(auth.Signature) || auth.Passphrase != "" || auth.KeyVersion != 0 {
			return bybit.APIKeyInformation{}, ErrAccess
		}
		uri += "?" + accessQuery(authRequest) + "&signature=" + auth.Signature
	} else {
		for _, value := range []string{auth.Signature, auth.Passphrase} {
			decoded, e := base64.StdEncoding.DecodeString(value)
			if e != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != value {
				return bybit.APIKeyInformation{}, ErrAccess
			}
		}
		if auth.KeyVersion != 2 && auth.KeyVersion != 3 {
			return bybit.APIKeyInformation{}, ErrAccess
		}
	}
	request, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	request.GetBody = nil
	request.Header.Set("Accept", "application/json")
	if provider.ID == "binance" {
		request.Header.Set("X-MBX-APIKEY", auth.APIKey)
	} else {
		request.Header.Set("KC-API-KEY", auth.APIKey)
		request.Header.Set("KC-API-SIGN", auth.Signature)
		request.Header.Set("KC-API-TIMESTAMP", strconv.FormatInt(authRequest.TimestampMS, 10))
		request.Header.Set("KC-API-PASSPHRASE", auth.Passphrase)
		request.Header.Set("KC-API-KEY-VERSION", strconv.Itoa(auth.KeyVersion))
		request.Header.Set("Content-Type", "application/json")
	}
	defer func() {
		for _, key := range []string{"X-MBX-APIKEY", "KC-API-KEY", "KC-API-SIGN", "KC-API-PASSPHRASE"} {
			request.Header.Del(key)
		}
	}()
	response, err := c.http.Do(request)
	if err != nil {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || !utf8.Valid(body) || chainutil.CheckJSON(body) != nil {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	return accessInformation(provider.ID, body)
}

func accessHeader(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, character := range value {
		if character < 33 || character > 126 {
			return false
		}
	}
	return true
}

func accessInformation(provider string, body []byte) (bybit.APIKeyInformation, error) {
	var information bybit.APIKeyInformation
	if provider == "binance" {
		var raw struct {
			EnableReading              *bool `json:"enableReading"`
			EnableSpotAndMarginTrading *bool `json:"enableSpotAndMarginTrading"`
			EnableWithdrawals          *bool `json:"enableWithdrawals"`
			PermitsUniversalTransfer   *bool `json:"permitsUniversalTransfer"`
		}
		if json.Unmarshal(body, &raw) != nil || raw.EnableReading == nil || raw.EnableSpotAndMarginTrading == nil || raw.EnableWithdrawals == nil || raw.PermitsUniversalTransfer == nil || !*raw.EnableReading {
			return information, ErrAccess
		}
		information.ReadOnly = 1
		if *raw.EnableSpotAndMarginTrading {
			information.ReadOnly = 0
			information.Permissions.Spot = []string{"SpotTrade"}
		}
		if *raw.EnableWithdrawals {
			information.ReadOnly = 0
			information.Permissions.Wallet = append(information.Permissions.Wallet, "Withdraw")
		}
		if *raw.PermitsUniversalTransfer {
			information.ReadOnly = 0
			information.Permissions.Wallet = append(information.Permissions.Wallet, "AccountTransfer")
		}
		return information, nil
	}
	var raw struct {
		Code string `json:"code"`
		Data struct {
			UID        int64  `json:"uid"`
			IsMaster   *bool  `json:"isMaster"`
			Permission string `json:"permission"`
			APIVersion int    `json:"apiVersion"`
			SiteType   string `json:"siteType"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.Code != "200000" || raw.Data.UID <= 0 || raw.Data.IsMaster == nil || len(raw.Data.Permission) > 512 || raw.Data.APIVersion != 2 && raw.Data.APIVersion != 3 || raw.Data.SiteType != "" && raw.Data.SiteType != "global" {
		return information, ErrAccess
	}
	information.UserID = raw.Data.UID
	information.IsMaster = *raw.Data.IsMaster
	information.ReadOnly = 1
	seen := map[string]bool{}
	for _, permission := range strings.Split(raw.Data.Permission, ",") {
		if !accessHeader(permission, 40) || seen[permission] {
			return bybit.APIKeyInformation{}, ErrAccess
		}
		seen[permission] = true
		switch permission {
		case "Spot":
			information.ReadOnly = 0
			information.Permissions.Spot = []string{"SpotTrade"}
		case "Transfer":
			information.ReadOnly = 0
			information.Permissions.Wallet = append(information.Permissions.Wallet, "Withdraw")
		case "InnerTransfer":
			information.ReadOnly = 0
			information.Permissions.Wallet = append(information.Permissions.Wallet, "AccountTransfer")
		}
	}
	if !seen["General"] {
		return bybit.APIKeyInformation{}, ErrAccess
	}
	return information, nil
}
