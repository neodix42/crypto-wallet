// Package marketdata reads public indicative spot prices. It never authorizes a
// payment, sends a wallet address, or reads exchange credentials.
package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/strictjson"
)

const (
	RefreshInterval = time.Minute
	MaxQuoteAge     = 2 * time.Minute
	maxResponse     = 2 << 20
	maxTickers      = 10000
	bybitURL        = "https://api.bybit.com/v5/market/tickers?category=spot"
	binanceURL      = "https://data-api.binance.vision/api/v3/ticker/24hr?type=MINI"
)

var ErrUnavailable = errors.New("public market prices unavailable")
var pairs = []string{"ETHUSDT", "BTCUSDT", "NEARUSDT", "APTUSDT", "GRAMUSDT", "HYPEUSDT", "ZENUSDT", "USDCUSDT", "ARBUSDT"}

type Quote struct {
	Pair       string `json:"pair"`
	Price      string `json:"price"`
	ObservedAt int64  `json:"observedAt"` // provider milliseconds, never a trade guarantee
	Source     string `json:"source"`
}

type Snapshot struct {
	Quotes    map[string]Quote `json:"quotes"`
	FetchedAt int64            `json:"fetchedAt"`
	Missing   []string         `json:"missing"`
}

// Client coalesces simultaneous reads and caches successful or failed attempts.
// Its endpoints and requests are fixed and independent of the wallet's holdings.
// A fallback changes the public market-data source, never the chosen trading
// account or the provider used for a signed transaction.
type Client struct {
	mu        sync.Mutex
	http      *http.Client
	now       func() time.Time
	cached    Snapshot
	attempted time.Time
	inFlight  chan struct{}
}

func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.MaxIdleConnsPerHost = 2
	return &Client{http: &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable }}, now: time.Now}
}

func SupportedPairs() []string { return append([]string(nil), pairs...) }

func (c *Client) Prices(ctx context.Context) (Snapshot, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		c.mu.Lock()
		if !c.attempted.IsZero() && c.now().Sub(c.attempted) >= 0 && c.now().Sub(c.attempted) < RefreshInterval {
			out := clone(c.cached)
			c.mu.Unlock()
			if len(out.Quotes) == 0 {
				return out, ErrUnavailable
			}
			return out, nil
		}
		if c.inFlight != nil {
			done := c.inFlight
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			case <-done:
				continue
			}
		}
		c.inFlight = make(chan struct{})
		c.mu.Unlock()
		out, err := c.fetch(ctx)
		c.mu.Lock()
		if ctx.Err() == nil {
			c.cached = clone(out)
			c.attempted = c.now()
		}
		close(c.inFlight)
		c.inFlight = nil
		c.mu.Unlock()
		return out, err
	}
}

func clone(s Snapshot) Snapshot {
	out := Snapshot{FetchedAt: s.FetchedAt, Missing: append([]string(nil), s.Missing...), Quotes: make(map[string]Quote, len(s.Quotes))}
	for k, v := range s.Quotes {
		out.Quotes[k] = v
	}
	return out
}

func (c *Client) fetch(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out := Snapshot{Quotes: map[string]Quote{}, FetchedAt: c.now().UnixMilli()}
	raw, err := c.get(ctx, bybitURL)
	if err == nil {
		if quotes, e := parseBybit(raw, c.now()); e == nil {
			out.Quotes = quotes
		}
	}
	if len(out.Quotes) < len(pairs) && ctx.Err() == nil {
		raw, err = c.get(ctx, binanceURL)
		if err == nil {
			if quotes, e := parseBinance(raw, c.now()); e == nil {
				for k, v := range quotes {
					if _, exists := out.Quotes[k]; !exists {
						out.Quotes[k] = v
					}
				}
			}
		}
	}
	for _, p := range pairs {
		if _, ok := out.Quotes[p]; !ok {
			out.Missing = append(out.Missing, p)
		}
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if len(out.Quotes) == 0 {
		return out, ErrUnavailable
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "CryptoWallet/public-market-data")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxResponse {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return nil, ErrUnavailable
	}
	return raw, nil
}

func parseBybit(raw []byte, now time.Time) (map[string]Quote, error) {
	// RawMessage fields permit additive public API metadata while strictjson
	// checks duplicate keys, nesting and the complete message before projection.
	var envelope map[string]json.RawMessage
	if strictjson.Decode(raw, &envelope) != nil {
		return nil, ErrUnavailable
	}
	var code *int
	var timestamp *int64
	if json.Unmarshal(envelope["retCode"], &code) != nil || code == nil || *code != 0 || json.Unmarshal(envelope["time"], &timestamp) != nil || timestamp == nil || !fresh(*timestamp, now) {
		return nil, ErrUnavailable
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(envelope["result"], &result) != nil {
		return nil, ErrUnavailable
	}
	var category string
	var rows []map[string]json.RawMessage
	if json.Unmarshal(result["category"], &category) != nil || category != "spot" || json.Unmarshal(result["list"], &rows) != nil || len(rows) == 0 || len(rows) > maxTickers {
		return nil, ErrUnavailable
	}
	return parseRows(rows, func(map[string]json.RawMessage) int64 { return *timestamp }, "Bybit public spot", now)
}

func parseBinance(raw []byte, now time.Time) (map[string]Quote, error) {
	var rows []map[string]json.RawMessage
	if strictjson.Decode(raw, &rows) != nil || len(rows) == 0 || len(rows) > maxTickers {
		return nil, ErrUnavailable
	}
	return parseRows(rows, func(row map[string]json.RawMessage) int64 {
		var timestamp int64
		_ = json.Unmarshal(row["closeTime"], &timestamp)
		return timestamp
	}, "Binance public spot", now)
}

func parseRows(rows []map[string]json.RawMessage, timestamp func(map[string]json.RawMessage) int64, source string, now time.Time) (map[string]Quote, error) {
	out := map[string]Quote{}
	seen := map[string]bool{}
	for _, row := range rows {
		var pair string
		if json.Unmarshal(row["symbol"], &pair) != nil || len(pair) < 1 || len(pair) > 40 {
			return nil, ErrUnavailable
		}
		if !supported(pair) {
			continue
		}
		if seen[pair] {
			return nil, ErrUnavailable
		}
		seen[pair] = true
		var price string
		at := timestamp(row)
		if json.Unmarshal(row["lastPrice"], &price) != nil || !validPrice(price) || !fresh(at, now) {
			continue
		}
		out[pair] = Quote{Pair: pair, Price: price, ObservedAt: at, Source: source}
	}
	return out, nil
}
func supported(pair string) bool {
	for _, v := range pairs {
		if v == pair {
			return true
		}
	}
	return false
}
func validPrice(price string) bool {
	if len(price) > 40 {
		return false
	}
	n, err := amount.Parse(price, 18)
	return err == nil && n != "0"
}
func fresh(at int64, now time.Time) bool {
	return at > 0 && at <= now.Add(30*time.Second).UnixMilli() && at >= now.Add(-MaxQuoteAge).UnixMilli()
}

// FreshQuote rechecks cached/public DTOs at use time. Expired observations are
// never silently reused as a live portfolio price.
func FreshQuote(s Snapshot, pair string, now time.Time) (Quote, bool) {
	quote, ok := s.Quotes[pair]
	if !ok || quote.Pair != pair || !supported(pair) || !validPrice(quote.Price) || !fresh(quote.ObservedAt, now) || (quote.Source != "Bybit public spot" && quote.Source != "Binance public spot") {
		return Quote{}, false
	}
	return quote, true
}
