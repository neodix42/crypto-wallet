package ton

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type publicRateScheduler struct {
	permit chan struct{}
	last   time.Time
}

func newPublicRateScheduler() *publicRateScheduler {
	return &publicRateScheduler{permit: make(chan struct{}, 1)}
}

var publicSchedulers = struct {
	sync.Mutex
	hosts map[string]*publicRateScheduler
}{hosts: map[string]*publicRateScheduler{}}

func officialTONCenterHost(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	switch u.Hostname() {
	case "toncenter.com", "testnet.toncenter.com":
		return u.Hostname()
	}
	return ""
}

// Keep the unauthenticated public service's TLS requests independent. Reused
// connections produced reproducible estimateFee rate-limit failures through
// an intermediary; fresh connections passed the same exact bounded request.
// TLS verification, environment proxy and explicit transports remain unchanged.
func officialPublicHTTP(raw, key string, original *http.Client) *http.Client {
	if key != "" || officialTONCenterHost(raw) == "" || original.Transport != nil {
		return original
	}
	client := *original
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.DisableKeepAlives = true
	client.Transport = transport
	return &client
}

// TON Center's unauthenticated service permits one request per second. A balance
// lookup already needs both chain identity and account state, so serialise these
// public requests with a margin. This never retries an HTTP request or broadcast.
// The limit is shared by all adapters in this process for each official host;
// other applications sharing the provider/IP may still exhaust its quota.
// An encrypted API key or custom RPC can be
// configured if the public service is unavailable.
func (a *Adapter) pacePublic(ctx context.Context, endpoint Endpoint) error {
	release, err := a.beginPublic(ctx, endpoint)
	if err == nil {
		release()
	}
	return err
}

// Hold the host lease until the response body is consumed/closed. Spacing
// request starts alone permits server arrivals to bunch when TLS latency varies.
func (a *Adapter) beginPublic(ctx context.Context, endpoint Endpoint) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host := officialTONCenterHost(endpoint.URL)
	if host == "" || endpoint.APIKey != "" {
		return func() {}, nil
	}
	scheduler := a.publicScheduler
	if scheduler == nil {
		publicSchedulers.Lock()
		scheduler = publicSchedulers.hosts[host]
		if scheduler == nil {
			scheduler = newPublicRateScheduler()
			publicSchedulers.hosts[host] = scheduler
		}
		publicSchedulers.Unlock()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case scheduler.permit <- struct{}{}:
	}
	if delay := time.Until(scheduler.last.Add(1100 * time.Millisecond)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			<-scheduler.permit
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		<-scheduler.permit
		return nil, err
	}
	return func() { scheduler.last = time.Now(); <-scheduler.permit }, nil
}
