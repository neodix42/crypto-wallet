package ton

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type pacingTransport struct {
	mu    sync.Mutex
	calls []time.Time
	keys  []string
}

func (p *pacingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.calls = append(p.calls, time.Now())
	p.keys = append(p.keys, request.Header.Get("X-API-Key"))
	p.mu.Unlock()
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{},"id":1}`))}, nil
}

// The HTTP transport is local and synthetic: no provider or wallet is queried.
func TestUnauthenticatedTONCenterPacingAndCancellation(t *testing.T) {
	a, transport := New(), &pacingTransport{}
	a.publicScheduler = newPublicRateScheduler()
	if err := a.Configure("ton-mainnet", Endpoint{URL: "https://toncenter.com/api/v2/jsonRPC", Client: &http.Client{Transport: transport}}); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := a.rpc(context.Background(), "ton-mainnet", "getMasterchainInfo", struct{}{}, &out); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.rpc(cancelled, "ton-mainnet", "getMasterchainInfo", struct{}{}, &out); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled public request reached provider", err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := a.rpc(deadline, "ton-mainnet", "getMasterchainInfo", struct{}{}, &out); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("pacing ignored request deadline", err)
	}
	if err := a.rpc(context.Background(), "ton-mainnet", "getMasterchainInfo", struct{}{}, &out); err != nil {
		t.Fatal(err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.calls) != 2 || transport.calls[1].Sub(transport.calls[0]) < time.Second {
		t.Fatal("public TON Center requests exceeded one request per second")
	}
}

func TestTONCenterPacingWaiterCancellationAndConfiguredAPIKey(t *testing.T) {
	a := New()
	a.publicScheduler = newPublicRateScheduler()
	a.publicScheduler.permit <- struct{}{}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := a.pacePublic(deadline, Endpoint{URL: "https://testnet.toncenter.com/api/v2/jsonRPC"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("pacing queue ignored cancellation", err)
	}
	<-a.publicScheduler.permit
	transport := &pacingTransport{}
	if err := a.Configure("ton-mainnet", Endpoint{URL: "https://toncenter.com/api/v2/jsonRPC", APIKey: "PUBLIC-SYNTHETIC-API-KEY", Client: &http.Client{Transport: transport}}); err != nil {
		t.Fatal(err)
	}
	// A future public timestamp would block unkeyed requests; authenticated
	// requests follow the user's provider quota instead of this public limit.
	a.publicScheduler.last = time.Now().Add(time.Hour)
	var out map[string]any
	for range 2 {
		if err := a.rpc(context.Background(), "ton-mainnet", "getMasterchainInfo", struct{}{}, &out); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.calls) != 2 || transport.keys[0] != "PUBLIC-SYNTHETIC-API-KEY" || transport.keys[1] != "PUBLIC-SYNTHETIC-API-KEY" {
		t.Fatal("configured TON API key was not used exclusively in HTTP header")
	}
	if err := a.pacePublic(context.Background(), Endpoint{URL: "http://127.0.0.1:8080"}); err != nil {
		t.Fatal("custom node unnecessarily paced", err)
	}
}

func TestOfficialKeylessTONCenterConnectionsKeepTLSProxyAndCustomTransport(t *testing.T) {
	for _, network := range New().Networks() {
		adapter := New()
		endpoint := adapter.endpoints[network.ID]
		transport, ok := endpoint.Client.Transport.(*http.Transport)
		if !ok || !transport.DisableKeepAlives || transport.Proxy == nil || (transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify) {
			t.Fatal("official unauthenticated transport reuses connections or weakens TLS/proxy")
		}
	}
	original := &http.Client{Transport: &pacingTransport{}}
	if got := officialPublicHTTP("https://toncenter.com/api/v2/jsonRPC", "", original); got != original || got.Transport != original.Transport {
		t.Fatal("provided custom transport changed")
	}
	for _, endpoint := range []Endpoint{{URL: "https://custom.example/rpc"}, {URL: "https://toncenter.com/api/v2/jsonRPC", APIKey: "PUBLIC-TEST-KEY"}} {
		original := defaultHTTP()
		if got := officialPublicHTTP(endpoint.URL, endpoint.APIKey, original); got != original {
			t.Fatal("custom/authenticated endpoint policy changed")
		}
	}
}

func TestTONCenterQuotaSurvivesAdapterReplacement(t *testing.T) {
	first, second := New(), New()
	endpoint := Endpoint{URL: "https://testnet.toncenter.com/api/v2/jsonRPC"}
	if e := first.pacePublic(context.Background(), endpoint); e != nil {
		t.Fatal(e)
	}
	started := time.Now()
	if e := second.pacePublic(context.Background(), endpoint); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) < time.Second {
		t.Fatal("replacing the adapter reset the official host quota")
	}
}

type leaseBody struct {
	ctx     context.Context
	wait    <-chan struct{}
	entered chan struct{}
	reader  io.Reader
	once    sync.Once
	closed  func()
}

func (b *leaseBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.wait:
		return b.reader.Read(p)
	}
}
func (b *leaseBody) Close() error { b.closed(); return nil }

type leaseTransport struct {
	mu                sync.Mutex
	calls             int
	completed, second time.Time
	bodyEntered       chan struct{}
	releaseBody       chan struct{}
}

func (x *leaseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	x.mu.Lock()
	x.calls++
	call := x.calls
	if call == 2 {
		x.second = time.Now()
	}
	x.mu.Unlock()
	var body io.ReadCloser = io.NopCloser(strings.NewReader(`{"ok":true,"result":{},"id":1}`))
	if call == 1 {
		body = &leaseBody{ctx: r.Context(), wait: x.releaseBody, entered: x.bodyEntered, reader: strings.NewReader(`{"ok":true,"result":{},"id":1}`), closed: func() { x.mu.Lock(); x.completed = time.Now(); x.mu.Unlock() }}
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
}
func TestTONCenterLeaseSpansSlowResponseBodyAcrossConcurrentAdapters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	transport := &leaseTransport{bodyEntered: make(chan struct{}), releaseBody: make(chan struct{})}
	first, second := New(), New()
	for _, adapter := range []*Adapter{first, second} {
		if e := adapter.Configure("ton-testnet", Endpoint{URL: "https://testnet.toncenter.com/api/v2/jsonRPC", Client: &http.Client{Transport: transport}}); e != nil {
			t.Fatal(e)
		}
	}
	done := make(chan error, 2)
	invoke := func(a *Adapter) {
		var out any
		done <- a.rpc(ctx, "ton-testnet", "getMasterchainInfo", struct{}{}, &out)
	}
	go invoke(first)
	select {
	case <-transport.bodyEntered:
	case <-ctx.Done():
		t.Fatal("first response never started")
	}
	go invoke(second)
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	transport.mu.Lock()
	calls := transport.calls
	transport.mu.Unlock()
	if calls != 1 {
		close(transport.releaseBody)
		t.Fatal("another adapter sent while first response body remained open")
	}
	close(transport.releaseBody)
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.completed.IsZero() || transport.second.Sub(transport.completed) < time.Second {
		t.Fatal("next network request arrived before completion plus public quota interval")
	}
}
