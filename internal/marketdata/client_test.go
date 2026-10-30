package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}
}
func bybitFixture(now time.Time, prices map[string]string) string {
	var rows []map[string]string
	for _, pair := range pairs {
		if price, ok := prices[pair]; ok {
			rows = append(rows, map[string]string{"symbol": pair, "lastPrice": price, "futureMetadata": "permitted"})
		}
	}
	raw, _ := json.Marshal(map[string]any{"retCode": 0, "retMsg": "OK", "result": map[string]any{"category": "spot", "list": rows}, "time": now.UnixMilli(), "retExtInfo": map[string]any{}})
	return string(raw)
}
func completeFixture(now time.Time) string {
	values := map[string]string{}
	for _, pair := range pairs {
		values[pair] = "1.25"
	}
	return bybitFixture(now, values)
}

func TestPublicPricesUseFixedUnauthenticatedURLsAndCacheCopies(t *testing.T) {
	now := time.Unix(1793371000, 0)
	var requests atomic.Int64
	client := NewClient()
	client.now = func() time.Time { return now }
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.String() != bybitURL || r.Body != nil || r.URL.User != nil {
			t.Errorf("unexpected public request: %s", r.URL.String())
		}
		for key := range r.Header {
			if key != "Accept" && key != "User-Agent" {
				t.Errorf("unexpected header %s", key)
			}
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-BAPI-API-KEY") != "" || r.Header.Get("X-MBX-APIKEY") != "" {
			t.Error("private authentication reached public market request")
		}
		return response(completeFixture(now)), nil
	})
	got, err := client.Prices(context.Background())
	if err != nil || len(got.Quotes) != len(pairs) || len(got.Missing) != 0 {
		t.Fatalf("prices=%+v err=%v", got, err)
	}
	delete(got.Quotes, "ETHUSDT")
	got.Missing = append(got.Missing, "spoof")
	cached, err := client.Prices(context.Background())
	if err != nil || len(cached.Quotes) != len(pairs) || len(cached.Missing) != 0 || requests.Load() != 1 {
		t.Fatalf("mutable cache or repeated request: %+v %v calls=%d", cached, err, requests.Load())
	}
	if client.http.CheckRedirect == nil || client.http.Timeout != 4*time.Second {
		t.Fatal("missing transport safeguards")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = client.Prices(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached data ignored cancellation: %v", err)
	}
}
func TestFallbackUsesOnlyFreshMissingPairsWithoutReplacingPrimaryQuotes(t *testing.T) {
	now := time.Unix(1793371000, 0)
	client := NewClient()
	client.now = func() time.Time { return now }
	var urls []string
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		if r.URL.String() == bybitURL {
			return response(bybitFixture(now, map[string]string{"ETHUSDT": "2"})), nil
		}
		if r.URL.String() != binanceURL {
			t.Fatal("endpoint changed")
		}
		return response(fmt.Sprintf(`[{"symbol":"ETHUSDT","lastPrice":"999","closeTime":%d},{"symbol":"NEARUSDT","lastPrice":"4.125","closeTime":%d},{"symbol":"GRAMUSDT","lastPrice":"1.6","closeTime":%d}]`, now.UnixMilli(), now.UnixMilli(), now.Add(-24*time.Hour).UnixMilli())), nil
	})
	out, err := client.Prices(context.Background())
	if err != nil || len(urls) != 2 || len(out.Quotes) != 2 || out.Quotes["ETHUSDT"].Price != "2" || out.Quotes["NEARUSDT"].Source != "Binance public spot" {
		t.Fatalf("out=%+v err=%v urls=%v", out, err, urls)
	}
	if _, ok := out.Quotes["GRAMUSDT"]; ok {
		t.Fatal("stale/delisted ticker valued as current")
	}
}
func TestFailedAttemptsAreCachedAndNeverReuseAnOldSnapshot(t *testing.T) {
	now := time.Unix(1793371000, 0)
	client := NewClient()
	client.now = func() time.Time { return now }
	calls := 0
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return response(completeFixture(now)), nil })
	if _, err := client.Prices(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(RefreshInterval)
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("private transport diagnostics must not escape")
	})
	out, err := client.Prices(context.Background())
	if err != ErrUnavailable || len(out.Quotes) != 0 || calls != 3 {
		t.Fatalf("old data/error leaked: %+v %v %d", out, err, calls)
	}
	_, err = client.Prices(context.Background())
	if err != ErrUnavailable || calls != 3 {
		t.Fatal("negative cache failed")
	}
}
func TestConcurrentReadsCoalesceAndWaitersCanCancel(t *testing.T) {
	now := time.Now()
	client := NewClient()
	client.now = func() time.Time { return now }
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return response(completeFixture(now)), nil
	})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = client.Prices(context.Background()) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Prices(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := client.Prices(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	close(release)
	<-done
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate requests %d", calls.Load())
	}
}
func TestPublicHTTPRejectsRedirectErrorAndOversizeBodies(t *testing.T) {
	for _, kind := range []string{"redirect", "status", "declared-size", "stream-size", "read-error"} {
		t.Run(kind, func(t *testing.T) {
			client := NewClient()
			calls := 0
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				res := response(`{}`)
				switch kind {
				case "redirect":
					res.StatusCode = 302
					res.Header.Set("Location", "https://untrusted.invalid/collect")
				case "status":
					res.StatusCode = 403
				case "declared-size":
					res.ContentLength = maxResponse + 1
				case "stream-size":
					res.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxResponse+1)))
				case "read-error":
					res.Body = io.NopCloser(errorReader{})
				}
				return res, nil
			})
			if _, err := client.get(context.Background(), bybitURL); err != ErrUnavailable || calls != 1 {
				t.Fatalf("unsafe HTTP result %v requests %d", err, calls)
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture read error") }
func TestPriceParsersRejectHostileOrStaleData(t *testing.T) {
	now := time.Unix(1793371000, 0)
	at := now.UnixMilli()
	for _, raw := range []string{
		`null`, `{}`, `[]`, `{"retCode":0,"retCode":0}`, `{"retCode":0,"time":null}`, `{"retCode":1}`, completeFixture(now) + `{}`,
		strings.Replace(completeFixture(now), `"spot"`, `"linear"`, 1),
		bybitFixture(now.Add(-MaxQuoteAge-time.Millisecond), map[string]string{"ETHUSDT": "2"}),
		bybitFixture(now.Add(31*time.Second), map[string]string{"ETHUSDT": "2"}),
		fmt.Sprintf(`{"retCode":0,"time":%d,"result":{"category":"spot","list":[{"symbol":"ETHUSDT","lastPrice":"1"},{"symbol":"ETHUSDT","lastPrice":"2"}]}}`, at),
		fmt.Sprintf(`{"retCode":0,"time":%d,"result":{"category":"spot","list":[{"symbol":12,"lastPrice":"1"}]}}`, at),
	} {
		if _, err := parseBybit([]byte(raw), now); err == nil {
			t.Errorf("accepted hostile Bybit data %s", raw)
		}
	}
	for _, price := range []string{"", "0", "-1", "+1", "01", "1e4", "NaN", "1.", ".2", "0.0000000000000000001", strings.Repeat("1", 41)} {
		out, err := parseBybit([]byte(bybitFixture(now, map[string]string{"ETHUSDT": price})), now)
		if err == nil && len(out) > 0 {
			t.Errorf("accepted price %q", price)
		}
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `[{"symbol":"ETHUSDT","lastPrice":"1","lastPrice":"2"}]`, fmt.Sprintf(`[{"symbol":"ETHUSDT","lastPrice":"1","closeTime":%d},{"symbol":"ETHUSDT","lastPrice":"2","closeTime":%d}]`, at, at)} {
		if _, err := parseBinance([]byte(raw), now); err == nil {
			t.Errorf("accepted malformed Binance %s", raw)
		}
	}
}
func TestFreshQuoteRechecksTimeIdentityAndDecimal(t *testing.T) {
	now := time.Now()
	s := Snapshot{Quotes: map[string]Quote{"ETHUSDT": {Pair: "ETHUSDT", Price: "2.123456789123456789", ObservedAt: now.UnixMilli(), Source: "Bybit public spot"}}}
	if _, ok := FreshQuote(s, "ETHUSDT", now); !ok {
		t.Fatal("fresh valid quote rejected")
	}
	if _, ok := FreshQuote(s, "ETHUSDT", now.Add(MaxQuoteAge+time.Second)); ok {
		t.Fatal("stale cached quote accepted")
	}
	for _, mutate := range []func(*Quote){func(q *Quote) { q.Pair = "BTCUSDT" }, func(q *Quote) { q.Source = "arbitrary" }, func(q *Quote) { q.Price = "0" }, func(q *Quote) { q.ObservedAt = now.Add(time.Minute).UnixMilli() }} {
		q := s.Quotes["ETHUSDT"]
		mutate(&q)
		bad := Snapshot{Quotes: map[string]Quote{"ETHUSDT": q}}
		if _, ok := FreshQuote(bad, "ETHUSDT", now); ok {
			t.Fatal("mutated quote accepted")
		}
	}
}
func TestLivePublicPrices(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_LIVE_MARKETDATA") != "1" {
		t.Skip("explicit read-only public market-data acceptance not selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := NewClient().Prices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range pairs {
		q, ok := FreshQuote(result, pair, time.Now())
		if ok {
			t.Logf("%s %s source=%s provider_time=%s", pair, q.Price, q.Source, time.UnixMilli(q.ObservedAt).UTC().Format(time.RFC3339))
		} else {
			t.Logf("UNAVAILABLE %s: no fresh public spot quote", pair)
		}
	}
	if _, ok := FreshQuote(result, "ETHUSDT", time.Now()); !ok {
		t.Fatal("ETH public quote unavailable")
	}
	if _, ok := FreshQuote(result, "NEARUSDT", time.Now()); !ok {
		t.Fatal("NEAR public quote unavailable")
	}
}

func TestPublicMarketReadDeadlineCancelsTransportWithoutCachingAnError(t *testing.T) {
	client := NewClient()
	calls := 0
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 8*time.Second {
			t.Error("public market query lacks its total deadline")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if result, err := client.Prices(ctx); !errors.Is(err, context.DeadlineExceeded) || len(result.Quotes) != 0 || calls != 1 || !client.attempted.IsZero() {
		t.Fatalf("deadline/fallback/cache mismatch %+v %v calls=%d", result, err, calls)
	}
}
func FuzzPublicPriceJSON(f *testing.F) {
	now := time.Unix(1793371000, 0)
	f.Add([]byte(completeFixture(now)))
	f.Add([]byte(fmt.Sprintf(`[{"symbol":"ETHUSDT","lastPrice":"2000.25","closeTime":%d}]`, now.UnixMilli())))
	f.Add([]byte(`{"retCode":0,"retCode":1}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > maxResponse {
			return
		}
		for _, parse := range []func([]byte, time.Time) (map[string]Quote, error){parseBybit, parseBinance} {
			quotes, err := parse(raw, now)
			if err != nil {
				continue
			}
			if len(quotes) > len(pairs) {
				t.Fatal("unbounded accepted quote set")
			}
			for pair, quote := range quotes {
				if _, ok := FreshQuote(Snapshot{Quotes: map[string]Quote{pair: quote}}, pair, now); !ok {
					t.Fatal("parser emitted invalid public price")
				}
			}
		}
	})
}
