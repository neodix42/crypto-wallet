package evm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"localwallet/internal/model"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type discoveryReadError struct{}

func (discoveryReadError) Read([]byte) (int, error) {
	return 0, errors.New("PUBLIC-provider-read-canary")
}
func (discoveryReadError) Close() error { return nil }

func discoveryNode(t *testing.T, body string, status int, fail bool) (*nodeFixture, model.Account, *int) {
	t.Helper()
	f, account := importedNode(t)
	account.Network = "base-sepolia"
	account.ID = "evm:0:base-sepolia"
	// fakeNode's network identity is captured during construction; rebuild it
	// with Base and retain only the existing deterministic token metadata hook.
	base := fakeNode(t, "base-sepolia")
	base.intercept = f.intercept
	requests := new(int)
	transport := base.a.HTTPClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	base.a.HTTPClient.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "base-sepolia.blockscout.com" {
			return transport.RoundTrip(r)
		}
		*requests++
		if r.Method != "GET" || r.URL.Path != "/api/v2/addresses/"+account.Address+"/token-balances" || r.URL.RawQuery != "" || r.Body != nil || r.Header.Get("Authorization") != "" {
			t.Error("discovery carried secret/authority or foreign account")
		}
		if fail {
			return nil, errors.New("PUBLIC-provider-private-error-canary")
		}
		response := &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
		if body == "READ_ERROR" {
			response.Body = discoveryReadError{}
		}
		if status == 302 {
			response.Header.Set("Location", "https://untrusted.invalid/secret")
		}
		return response, nil
	})
	return base, account, requests
}

func discoveryBody(contract string) string {
	return `[{"token":{"address_hash":"` + contract + `","type":"ERC-20","symbol":"UNTRUSTED-DO-NOT-DISPLAY","decimals":"18"},"value":"999999999999999999999999999999"}]`
}

func TestBaseTokenDiscoveryVerifiesIndexerCandidatesWithoutAuthorizingSpending(t *testing.T) {
	contract := importedTokenFixture().Asset.Contract
	f, account, requests := discoveryNode(t, discoveryBody(contract), 200, false)
	before := fmt.Sprint(f.a.Networks())
	result, err := f.a.DiscoverTokens(context.Background(), account)
	if err != nil || len(result.Items) != 1 || result.Unavailable != 0 || result.Limited || *requests != 1 {
		t.Fatal("valid Base token discovery failed", err, result)
	}
	item := result.Items[0]
	if item.Token.Network != account.Network || item.Token.Asset.Decimals != 6 || item.Token.Asset.Symbol != "TEST" || item.Balance.Asset != item.Token.Asset.ID || item.Balance.Atomic != "1000000000000000000000000" || item.Balance.At == "" {
		t.Fatal("indexer metadata/balance trusted instead of RPC")
	}
	if fmt.Sprint(f.a.Networks()) != before || len(f.a.tokens) != 0 {
		t.Fatal("read-only discovery installed spend policy")
	}
	plan := fixturePlan(t, f.a, account.Network)
	plan.Asset = item.Token.Asset.ID
	plan.ID = planID(plan)
	if f.a.ValidatePlan(plan) == nil {
		t.Fatal("unapproved discovered token could spend")
	}
	tracked, err := f.a.WithTokens([]model.ERC20Token{item.Token})
	if err != nil {
		t.Fatal(err)
	}
	result, err = tracked.DiscoverTokens(context.Background(), account)
	if err != nil || len(result.Items) != 0 {
		t.Fatal("already approved token duplicated in candidates", err)
	}
}

func TestBaseTokenDiscoveryRejectsUnavailableMalformedAndOversizedIndexer(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		fail       bool
	}{
		{"network", "", 200, true}, {"denied", "PRIVATE-provider-text", 403, false}, {"redirect", "", 302, false}, {"read", "READ_ERROR", 200, false},
		{"bytes", strings.Repeat(" ", (256<<10)+1), 200, false}, {"schema", `{}`, 200, false}, {"null", `null`, 200, false}, {"duplicate", `[{"token":{"type":"ERC-20","type":"ERC-20"}}]`, 200, false},
		{"array limit", "[" + strings.TrimSuffix(strings.Repeat(`{},`, 257), ",") + "]", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, account, _ := discoveryNode(t, tc.body, tc.status, tc.fail)
			result, err := f.a.DiscoverTokens(context.Background(), account)
			if err == nil || len(result.Items) != 0 || strings.Contains(err.Error(), "canary") || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("bad indexer became balance/secret error", err)
			}
		})
	}
}

func TestBaseTokenDiscoveryBoundsDuplicatesUnsupportedAndIncompleteCandidates(t *testing.T) {
	contract := importedTokenFixture().Asset.Contract
	for _, tc := range []struct {
		name, body  string
		unavailable int
		items       int
	}{
		{"empty", `[]`, 0, 0}, {"NFT", `[{"token":{"type":"ERC-721","address_hash":"` + contract + `"}}]`, 0, 0},
		{"invalid", discoveryBody("0xinvalid"), 1, 0}, {"duplicate", `[` + strings.Trim(discoveryBody(contract), "[]") + `,` + strings.Trim(discoveryBody(strings.ToLower(contract)), "[]") + `]`, 0, 1},
		{"pinned", discoveryBody("0x107fdE93838e3404934877935993782F977324BB"), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, account, _ := discoveryNode(t, tc.body, 200, false)
			result, err := f.a.DiscoverTokens(context.Background(), account)
			if err != nil || len(result.Items) != tc.items || result.Unavailable != tc.unavailable {
				t.Fatal("candidate identity handling failed", err, result)
			}
		})
	}
	entries := make([]map[string]any, 33)
	for i := range entries {
		entries[i] = map[string]any{"token": map[string]string{"type": "ERC-20", "address_hash": fmt.Sprintf("0x%040x", i+100)}}
	}
	raw, _ := json.Marshal(entries)
	f, account, _ := discoveryNode(t, string(raw), 200, false)
	f.intercept = func(method string, _ []json.RawMessage) (any, bool) {
		if method == "eth_getCode" {
			return "0x", true
		}
		return nil, false
	}
	result, err := f.a.DiscoverTokens(context.Background(), account)
	if err != nil || !result.Limited || result.Unavailable != 32 || len(result.Items) != 0 {
		t.Fatal("discovery fanout was unbounded", err, result)
	}
}

func TestBaseTokenDiscoveryRPCFailuresRemainUnknownAndNoUnsupportedNetworks(t *testing.T) {
	for _, fault := range []string{"chain", "metadata", "balance", "zero", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			f, account, _ := discoveryNode(t, discoveryBody(importedTokenFixture().Asset.Contract), 200, false)
			original := f.intercept
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.intercept = func(method string, args []json.RawMessage) (any, bool) {
				if fault == "chain" && method == "eth_chainId" {
					return "0x1", true
				}
				if fault == "cancel" && method == "eth_getBlockByNumber" {
					cancel()
				}
				if method == "eth_call" {
					var call map[string]string
					_ = json.Unmarshal(args[0], &call)
					if fault == "metadata" && call["data"] == callData("symbol()") {
						return "0x", true
					}
					if strings.HasPrefix(call["data"], "0x70a08231") {
						if fault == "balance" {
							return "0x", true
						}
						if fault == "zero" {
							return "0x" + strings.Repeat("0", 64), true
						}
					}
				}
				return original(method, args)
			}
			result, err := f.a.DiscoverTokens(ctx, account)
			if fault == "chain" || fault == "cancel" {
				if err == nil && !result.Limited {
					t.Fatal("wrong chain/cancel became complete inventory")
				}
				return
			}
			want := 1
			if fault == "zero" {
				want = 0
			}
			if err != nil || len(result.Items) != 0 || result.Unavailable != want {
				t.Fatal("failed RPC became zero balance", err, result)
			}
		})
	}
	f, account, requests := discoveryNode(t, `[]`, 200, false)
	for _, network := range []string{"ethereum-sepolia", "horizen-mainnet", "base-unknown"} {
		bad := account
		bad.Network = network
		if _, err := f.a.DiscoverTokens(context.Background(), bad); err == nil {
			t.Fatal("foreign discovery accepted")
		}
	}
	f.a.HTTPClient = nil
	if _, err := f.a.DiscoverTokens(context.Background(), account); err == nil || *requests != 0 {
		t.Fatal("unavailable client or foreign network contacted indexer")
	}
	if discoveryEndpoint("base-mainnet") != "https://base.blockscout.com" {
		t.Fatal("Base provider identity changed")
	}
}
