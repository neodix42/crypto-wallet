package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
)

const publicAccessKey = "PUBLIC_EXCHANGE_KEY_18247"
const publicAccessSecret = "PUBLIC_ONLY_EXCHANGE_SECRET_5713984"
const publicAccessPassphrase = "PUBLIC_ONLY_API_PASSPHRASE_2519"

func TestProvidersAreFixedIndependentProductionServices(t *testing.T) {
	providers := Providers()
	if len(providers) != 3 {
		t.Fatal("missing selected exchange")
	}
	for _, provider := range providers {
		for _, endpoint := range provider.Endpoints {
			if !ValidateProviderEndpoint(provider.ID, endpoint) {
				t.Fatal("reviewed endpoint rejected")
			}
			for _, other := range providers {
				if other.ID != provider.ID && ValidateProviderEndpoint(other.ID, endpoint) {
					t.Fatal("credential crossed service")
				}
			}
			for _, suffix := range []string{"/", "/api", "?x=1", "#x", ":443", ".evil.example"} {
				if ValidateProviderEndpoint(provider.ID, endpoint+suffix) {
					t.Fatal("noncanonical endpoint accepted")
				}
			}
		}
	}
	providers[0].Endpoints[0] = "https://changed.invalid"
	if Providers()[0].Endpoints[0] == providers[0].Endpoints[0] {
		t.Fatal("caller mutated shared allowlist")
	}
	for _, endpoint := range []string{"http://api.binance.com", "https://api.binance.com@evil.invalid", "https://api.kucoin.com.evil.invalid", "https://api.binance.us", "https://api-futures.kucoin.com", "https://127.0.0.1"} {
		if _, ok := ProviderForEndpoint(endpoint); ok {
			t.Fatal("unreviewed service accepted")
		}
	}
}

func accessRequest(provider string) bybit.AuthRequest {
	r := bybit.AuthRequest{Method: "GET", TimestampMS: 1700000000123, ReceiveWindowMS: 5000}
	if provider == "binance" {
		r.Endpoint = "https://api.binance.com"
		r.Path = "/sapi/v1/account/apiRestrictions"
	} else {
		r.Endpoint = "https://api.kucoin.com"
		r.Path = "/api/v1/user/api-key"
	}
	return r
}

func TestReadOnlyServiceAuthenticationMatchesIndependentHMACs(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		for _, version := range []int{2, 3} {
			t.Run(provider+string(rune('0'+version)), func(t *testing.T) {
				request := accessRequest(provider)
				var passphrase []byte
				keyVersion := 0
				message := "recvWindow=5000&timestamp=1700000000123"
				if provider == "kucoin" {
					passphrase = []byte(publicAccessPassphrase)
					keyVersion = version
					message = "1700000000123GET/api/v1/user/api-key"
				}
				auth, err := AccessAuth(publicAccessKey, []byte(publicAccessSecret), passphrase, keyVersion, request)
				if err != nil {
					t.Fatal(err)
				}
				mac := hmac.New(sha256.New, []byte(publicAccessSecret))
				mac.Write([]byte(message))
				expected := hex.EncodeToString(mac.Sum(nil))
				if provider == "kucoin" {
					expected = base64.StdEncoding.EncodeToString(mac.Sum(nil))
				}
				if auth.Signature != expected || auth.APIKey != publicAccessKey {
					t.Fatal("independent signature mismatch")
				}
				if provider == "kucoin" {
					mac.Reset()
					mac.Write([]byte(publicAccessPassphrase))
					if auth.Passphrase != base64.StdEncoding.EncodeToString(mac.Sum(nil)) || auth.Passphrase == publicAccessPassphrase || auth.KeyVersion != version {
						t.Fatal("API passphrase was not transformed")
					}
				}
			})
		}
	}
}

func TestReadOnlyAccessCannotAuthorizeAnySpendingOrCrossServicePayload(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		for _, mutate := range []func(*bybit.AuthRequest){
			func(r *bybit.AuthRequest) { r.Method = "POST" }, func(r *bybit.AuthRequest) { r.Path = "/api/v3/order" }, func(r *bybit.AuthRequest) { r.Path = "/api/v3/withdrawals" }, func(r *bybit.AuthRequest) { r.Payload = "amount=1" }, func(r *bybit.AuthRequest) { r.Payload = `{"amount":"1"}` }, func(r *bybit.AuthRequest) { r.Endpoint = "https://attacker.invalid" }, func(r *bybit.AuthRequest) { r.Endpoint += "/" }, func(r *bybit.AuthRequest) { r.ReceiveWindowMS = 60000 }, func(r *bybit.AuthRequest) { r.TimestampMS = 0 },
		} {
			request := accessRequest(provider)
			mutate(&request)
			if ValidateAccessRequest(request) == nil {
				t.Fatal("unexpected capability accepted")
			}
		}
	}
	for _, version := range []int{0, 1, 4} {
		if _, err := AccessAuth(publicAccessKey, []byte(publicAccessSecret), []byte(publicAccessPassphrase), version, accessRequest("kucoin")); err == nil {
			t.Fatal("unsupported key version accepted")
		}
	}
	if _, err := AccessAuth(publicAccessKey, []byte(publicAccessSecret), []byte(publicAccessPassphrase), 2, accessRequest("binance")); err == nil {
		t.Fatal("KuCoin passphrase attached to Binance")
	}
}

type accessRoundTrip func(*http.Request) (*http.Response, error)

func (f accessRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTLSAccessFixture(t *testing.T, provider string, handler http.HandlerFunc) *AccessClient {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	endpoint := accessRequest(provider).Endpoint
	client, err := NewAccess(endpoint, func(_ context.Context, r bybit.AuthRequest) (bybit.Auth, error) {
		var phrase []byte
		version := 0
		if provider == "kucoin" {
			phrase = []byte(publicAccessPassphrase)
			version = 3
		}
		return AccessAuth(publicAccessKey, []byte(publicAccessSecret), phrase, version, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	client.http.Transport = accessRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != strings.TrimPrefix(endpoint, "https://") {
			t.Fatal("production authority changed")
		}
		copy := request.Clone(request.Context())
		u := *request.URL
		copy.URL = &u
		copy.URL.Host = target.Host
		return transport.RoundTrip(copy)
	})
	client.now = func() time.Time { return time.UnixMilli(1700000000123) }
	t.Cleanup(client.Close)
	return client
}

func TestActualTLSAccessUsesOnlyItsExactReadOnlyWireContract(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			client := newTLSAccessFixture(t, provider, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != accessRequest(provider).Path {
					t.Fatal("wrong method or path")
				}
				body, _ := io.ReadAll(r.Body)
				if len(body) != 0 {
					t.Fatal("unexpected read body")
				}
				if strings.Contains(r.RequestURI, publicAccessSecret) || strings.Contains(r.RequestURI, publicAccessPassphrase) {
					t.Fatal("plaintext secret in URL")
				}
				if provider == "binance" {
					if r.Header.Get("X-MBX-APIKEY") != publicAccessKey || len(r.URL.Query()) != 3 || r.URL.Query().Get("recvWindow") != "5000" || r.URL.Query().Get("timestamp") != "1700000000123" || r.Header.Get("KC-API-KEY") != "" {
						t.Fatal("Binance request identity")
					}
					io.WriteString(w, `{"enableReading":true,"enableSpotAndMarginTrading":true,"enableWithdrawals":false,"permitsUniversalTransfer":true}`)
				} else {
					if r.URL.RawQuery != "" || r.Header.Get("KC-API-KEY") != publicAccessKey || r.Header.Get("KC-API-KEY-VERSION") != "3" || r.Header.Get("KC-API-PASSPHRASE") == publicAccessPassphrase || r.Header.Get("KC-API-TIMESTAMP") != "1700000000123" || r.Header.Get("X-MBX-APIKEY") != "" {
						t.Fatal("KuCoin request identity")
					}
					io.WriteString(w, `{"code":"200000","data":{"uid":123456,"isMaster":true,"permission":"General,Spot,Transfer,InnerTransfer","apiVersion":3,"siteType":"global","apiKey":"PRIVATE_ECHO_NOT_RETURNED"}}`)
				}
			})
			information, err := client.APIKeyInfo(context.Background())
			if err != nil || calls != 1 || len(information.Permissions.Spot) != 1 || information.ReadOnly != 0 {
				t.Fatal("valid account authentication failed", err)
			}
			if provider == "kucoin" && (!information.IsMaster || information.UserID != 123456 || len(information.Permissions.Wallet) != 2) {
				t.Fatal("KuCoin permissions lost")
			}
		})
	}
}

func TestActualTLSAccessRejectsIncompleteAmbiguousAndSecretEchoErrors(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		for _, payload := range []string{`{}`, `null`, `[]`, `{"code":"400001","msg":"PUBLIC_SECRET_ECHO"}`, `{"enableReading":true,"enableReading":false}`, `{"code":"200000","data":null}`, strings.Repeat("x", (1<<20)+1), `{"x":1} trailing`} {
			t.Run(provider+string(rune(len(payload)%50+65)), func(t *testing.T) {
				client := newTLSAccessFixture(t, provider, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) })
				if _, err := client.APIKeyInfo(context.Background()); err == nil || strings.Contains(err.Error(), "PUBLIC_SECRET_ECHO") {
					t.Fatal("invalid response accepted or leaked", err)
				}
			})
		}
	}
}

func TestAccessTransportRejectsRedirectsAndDisablesAmbientCredentials(t *testing.T) {
	client, err := NewAccess("https://api.binance.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	transport := client.http.Transport.(*http.Transport)
	if transport.Proxy != nil || client.http.Jar != nil || client.http.Timeout <= 0 || transport.TLSClientConfig.InsecureSkipVerify || transport.MaxResponseHeaderBytes <= 0 || client.http.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("service transport weakened")
	}
	if _, err := NewAccess("https://api.bybit.com", nil); err == nil {
		t.Fatal("new adapter took over legacy service")
	}
}
