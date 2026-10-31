package ton

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

func TestTONRateLimitClassificationNeverRetries(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{{429, `{"error":"private provider text"}`}, {200, `{"ok":false,"code":429,"error":"private provider text"}`}, {200, `{"ok":false,"error":{"code":429,"message":"private provider text"}}`}} {
		t.Run(response.body, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(response.status)
				_, _ = io.WriteString(w, response.body)
			}))
			defer s.Close()
			a := New()
			_ = a.Configure("ton-mainnet", Endpoint{URL: s.URL})
			var result any
			if e := a.rpc(context.Background(), "ton-mainnet", "getMasterchainInfo", map[string]any{}, &result); !errors.Is(e, ErrRateLimited) || calls != 1 || strings.Contains(e.Error(), "private") {
				t.Fatal("rate limit lost safe classification or automatically retried", e, calls)
			}
		})
	}
}

type quotaTransport struct {
	mu       sync.Mutex
	last     time.Time
	calls    int
	rejected int
}

type providerProbeUA string

func (ua providerProbeUA) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	copy.Header = request.Header.Clone()
	copy.Header.Set("User-Agent", string(ua))
	if os.Getenv("WALLET_TON_PUBLIC_CAPTURE") == "1" && copy.Body != nil {
		raw, e := io.ReadAll(copy.Body)
		if e != nil {
			return nil, e
		}
		copy.Body = io.NopCloser(bytes.NewReader(raw))
		var q struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &q)
		if q.Method == "estimateFee" {
			_ = os.WriteFile("../../../reports/ton-deploy-public-go-estimate-request.json", raw, 0600)
		}
	}
	return http.DefaultTransport.RoundTrip(copy)
}

func (q *quotaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	status, body := 200, `{"ok":true,"result":{},"id":1}`
	now := time.Now()
	if !q.last.IsZero() && now.Sub(q.last) < time.Second {
		q.rejected++
		status = 429
		body = `{"ok":false,"error":"quota"}`
	}
	q.last = now
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func TestTONPublicQuotaSharedAcrossConcurrentOperations(t *testing.T) {
	a := New()
	transport := &quotaTransport{}
	_ = a.Configure("ton-mainnet", Endpoint{URL: "https://toncenter.com/api/v2/jsonRPC", Client: &http.Client{Transport: transport}})
	var wg sync.WaitGroup
	for _, method := range []string{"getMasterchainInfo", "getAddressInformation", "estimateFee"} {
		wg.Add(1)
		go func(method string) {
			defer wg.Done()
			var out any
			if e := a.rpc(context.Background(), "ton-mainnet", method, map[string]any{}, &out); e != nil {
				t.Error(e)
			}
		}(method)
	}
	wg.Wait()
	if transport.calls != 3 || transport.rejected != 0 {
		t.Fatal("shared adapter concurrency exceeded real1RPS policy", transport.calls, transport.rejected)
	}
}

// Opt-in, read-only public provider acceptance. This never sends a BOC, funds an
// account, accepts secrets or loads application storage. Public fixture seed only.
func TestTONPublicDeploymentReadOnlyEstimate(t *testing.T) {
	if os.Getenv("WALLET_TON_PUBLIC_PROBE") != "1" {
		t.Skip("NOT_RUN: WALLET_TON_PUBLIC_PROBE=1 enables public read-only default TON provider requests")
	}
	a := New()
	if os.Getenv("WALLET_TON_PUBLIC_PROVIDER") == "toncenter" {
		client := defaultHTTP()
		switch os.Getenv("WALLET_TON_PUBLIC_UA") {
		case "wallet":
			client.Transport = providerProbeUA("CryptoWallet/read-only-provider-acceptance")
		case "python":
			client.Transport = providerProbeUA("Python-urllib/3.14")
		}
		if e := a.Configure("ton-mainnet", Endpoint{URL: "https://toncenter.com/api/v2/jsonRPC", Client: client}); e != nil {
			t.Fatal(e)
		}
	}
	_, seed := fixtures(t)
	account, e := a.DeriveProfile(seed, 127, "ton-mainnet", ProfileSubwallet42)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	checkpoint, e := a.verifyNetwork(ctx, account.Network)
	if e != nil {
		t.Fatal("verify network:", e)
	}
	auth, e := a.authority(ctx, account)
	if e != nil {
		t.Fatal("read owned contract:", e)
	}
	if !auth.Deploy {
		t.Fatal("public fixture unexpectedly active; no state changes attempted")
	}
	x := Payload{Kind: "deployment", Deploy: true, SourceBalance: auth.Balance, StorageReserve: "10000000", MasterSeqno: checkpoint}
	p := model.Plan{Account: account, Asset: "ton-mainnet:TON", Recipient: account.Address, Amount: "0", FeeAsset: "ton-mainnet:TON", Nonce: "0", Expires: time.Now().Add(5 * time.Minute).Unix()}
	unsigned, _, e := message(p, x)
	if e != nil {
		t.Fatal(e)
	}
	body := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(make([]byte, 64), 512).EndCell()
	fee, e := a.estimate(ctx, p, x, body, true)
	if e != nil {
		t.Fatal("estimate deployment:", e)
	}
	if e = a.verifyUSDT(ctx, account.Network); e != nil {
		t.Fatal("verify canonical USDT master:", e)
	}
	jetton, e := a.jettonState(ctx, account.Network, account.Address)
	if e != nil {
		t.Fatal("verify derived USDT wallet:", e)
	}
	t.Logf("PASS read-only default-provider identity/state/V5R1 StateInit deployment estimate=%d nanograms; required reserve=%d; 0.2GRAM sufficient=%t; no broadcast", fee, fee*2+1_000_000+10_000_000, uint64(200_000_000) >= fee*2+1_000_000+10_000_000)
	t.Logf("PASS canonical USDT master code/library + exact derived owner-wallet verification; observed token balance=%s", jetton.Balance)
}
