package walletcore

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

type tokenIndexerTransport func(*http.Request) (*http.Response, error)

func (f tokenIndexerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBaseTokenDiscoveryUsesOnlyOwnedAccountAndReadOnlyCoreTransport(t *testing.T) {
	fail := false
	server := tokenMetadataServer(t, &fail, "0x14a34")
	adapter, err := evm.NewWithEndpoints(map[string]string{"base-sepolia": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, 64)
	seed[0] = 1
	account, err := adapter.Derive(seed, 0, "base-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	requests := 0
	status := 200
	adapter.HTTPClient.Transport = tokenIndexerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "base-sepolia.blockscout.com" {
			return http.DefaultTransport.RoundTrip(r)
		}
		requests++
		if r.URL.Path != "/api/v2/addresses/"+account.Address+"/token-balances" || r.Method != "GET" || r.Body != nil {
			t.Error("indexer request crossed account/secret/write boundary")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`[]`)), Request: r}, nil
	})
	out, err := c.handle(message("discoverTokens", map[string]string{"accountId": account.ID}))
	if err != nil || len(out.(evm.TokenDiscovery).Items) != 0 || requests != 1 {
		t.Fatal("valid read-only discovery failed", err)
	}
	for _, call := range readPublicState(t, path)["calls"].([]any) {
		if fmt.Sprint(call) != "snapshot" && fmt.Sprint(call) != "accounts" {
			t.Fatal("discovery asked signer for policy or authorization", call)
		}
	}
	status = 403
	if _, err = c.handle(message("discoverTokens", map[string]string{"accountId": account.ID})); err == nil {
		t.Fatal("failed indexer presented complete inventory")
	}
	for _, body := range []string{`{}`, `{"accountId":null}`, `{"accountId":"missing"}`, `{"accountId":"x","contract":"0x123"}`} {
		m := message("discoverTokens", nil)
		m.Body = []byte(body)
		if _, err := c.handle(m); err == nil {
			t.Fatal("invalid/foreign discovery request accepted")
		}
	}
	state = readPublicState(t, path)
	state["locked"] = true
	writePublicState(t, path, state)
	if _, err := c.handle(message("discoverTokens", map[string]string{"accountId": account.ID})); err == nil {
		t.Fatal("locked discovery accepted")
	}
}

func TestBaseTokenDiscoveryRejectsNonEVMAndUnknownAdapters(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	if _, err := c.handle(message("discoverTokens", map[string]string{"accountId": publicAccount().ID})); err == nil {
		t.Fatal("non-Base adapter discovered tokens")
	}
	state := readPublicState(t, path)
	account := publicAccount()
	account.Network = "unknown"
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	if _, err := c.handle(message("discoverTokens", map[string]string{"accountId": account.ID})); err == nil {
		t.Fatal("unknown adapter discovered tokens")
	}
}
