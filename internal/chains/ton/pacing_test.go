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
	a.publicPermit <- struct{}{}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := a.pacePublic(deadline, Endpoint{URL: "https://testnet.toncenter.com/api/v2/jsonRPC"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("pacing queue ignored cancellation", err)
	}
	<-a.publicPermit
	transport := &pacingTransport{}
	if err := a.Configure("ton-mainnet", Endpoint{URL: "https://toncenter.com/api/v2/jsonRPC", APIKey: "PUBLIC-SYNTHETIC-API-KEY", Client: &http.Client{Transport: transport}}); err != nil {
		t.Fatal(err)
	}
	// A future public timestamp would block unkeyed requests; authenticated
	// requests follow the user's provider quota instead of this public limit.
	a.lastPublicRequest = time.Now().Add(time.Hour)
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
