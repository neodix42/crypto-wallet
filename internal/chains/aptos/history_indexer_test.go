package aptos

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

type historyRoundTripper func(*http.Request) (*http.Response, error)

func (r historyRoundTripper) RoundTrip(q *http.Request) (*http.Response, error) { return r(q) }

type accountHistoryFixture struct {
	adapter      *Adapter
	account      model.Account
	mode         string
	queries      int
	reads        []uint64
	before       []string
	archiveReads int
}

func newAccountHistoryFixture(t *testing.T, mode string) *accountHistoryFixture {
	t.Helper()
	f := &accountHistoryFixture{adapter: New(), mode: mode}
	f.account, _ = f.adapter.Derive(seed(), 0, "aptos-mainnet")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var out any
		switch {
		case r.URL.Path == "/v1":
			chain := 1
			if f.mode == "rest-chain" || f.mode == "archive-chain" && r.Header.Get("X-Fixture-Provider") == "archive.mainnet.aptoslabs.com" {
				chain = 2
			}
			version := "9000000"
			if f.mode == "head-overflow" {
				version = "9223372036854775807"
			}
			stamp := time.Now().UnixMicro()
			if r.Header.Get("X-Fixture-Provider") == "archive.mainnet.aptoslabs.com" {
				f.archiveReads++
				if f.mode == "archive-behind" {
					version = "20"
				}
				if f.mode == "archive-stale" {
					stamp = time.Now().Add(-time.Hour).UnixMicro()
				}
				if f.mode == "archive-http" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
			}
			out = map[string]any{"chain_id": chain, "ledger_version": version, "ledger_timestamp": fmt.Sprint(stamp)}
		case strings.HasPrefix(r.URL.Path, "/v1/accounts/"):
			key := f.account.Address
			if f.mode == "authority" {
				key = "0x1"
			}
			out = map[string]any{"sequence_number": "42", "authentication_key": key}
		case r.URL.Path == "/v1/graphql":
			f.queries++
			if f.mode == "indexer-http" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			var query struct {
				Query     string `json:"query"`
				Variables struct {
					Address string `json:"address"`
					Head    string `json:"head"`
					Before  string `json:"before"`
					Limit   int    `json:"limit"`
				} `json:"variables"`
			}
			if json.NewDecoder(r.Body).Decode(&query) != nil || query.Variables.Address != f.account.Address || query.Variables.Head != "9000000" || query.Variables.Limit != indexedHistoryLimit || !strings.Contains(query.Query, "transaction_version: {_lte: $head, _lt: $before}") {
				t.Error("history query lost exact account/snapshot/bounds")
			}
			f.before = append(f.before, query.Variables.Before)
			rows := []any{}
			versions := []uint64{73, 44}
			if f.mode == "pages" || f.mode == "repeat-page" || f.mode == "effect-bound" {
				versions = nil
				for i := uint64(0); i < indexedHistoryLimit; i++ {
					version := uint64(1000) - i
					if f.queries > 1 && f.mode == "pages" {
						if i > 0 {
							break
						}
						version = 73
					}
					versions = append(versions, version)
				}
			}
			if f.mode == "empty" {
				versions = nil
			}
			for _, version := range versions {
				owner := f.account.Address
				if f.mode == "indexer-owner" {
					owner = "0x1"
				}
				var candidateVersion any = version
				if f.mode == "bad-candidate-version" {
					candidateVersion = -1
				}
				if f.mode == "future-candidate" {
					candidateVersion = 9000001
				}
				rows = append(rows, map[string]any{"account_address": owner, "transaction_version": candidateVersion})
			}
			if f.mode == "duplicate-candidate" {
				rows[1] = rows[0]
			}
			if f.mode == "ascending-candidate" {
				rows[0], rows[1] = rows[1], rows[0]
			}
			if f.mode == "overlong" {
				for len(rows) <= indexedHistoryLimit {
					rows = append(rows, rows[0])
				}
			}
			chain := 1
			if f.mode == "indexer-chain" {
				chain = 2
			}
			data := map[string]any{"ledger_infos": []any{map[string]any{"chain_id": chain}}, "account_transactions": rows}
			out = map[string]any{"data": data}
			switch f.mode {
			case "graphql-errors":
				out = map[string]any{"errors": []any{map[string]any{"message": "unavailable"}}, "data": data}
			case "unsupported-schema":
				delete(data, "account_transactions")
			case "null-rows":
				data["account_transactions"] = nil
			case "no-data":
				out = map[string]any{}
			case "empty-ledgers":
				data["ledger_infos"] = []any{}
			}
		case strings.HasPrefix(r.URL.Path, "/v1/transactions/by_version/"):
			v, _ := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/v1/transactions/by_version/"), 10, 64)
			f.reads = append(f.reads, v)
			if f.mode == "rest-unavailable" || strings.HasPrefix(f.mode, "archive") && r.Header.Get("X-Fixture-Provider") != "archive.mainnet.aptoslabs.com" {
				w.WriteHeader(http.StatusGone)
				return
			}
			out = f.transaction(v)
		default:
			t.Error("unexpected history request", r.URL.String())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	f.adapter.client.HTTP.Transport = historyRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.mainnet.aptoslabs.com" && r.URL.Host != "archive.mainnet.aptoslabs.com" {
			t.Error("unexpected provider", r.URL.Host)
		}
		cloned := r.Clone(r.Context())
		cloned.Header.Set("X-Fixture-Provider", r.URL.Host)
		cloned.URL.Scheme, cloned.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(cloned)
	})
	return f
}

func (f *accountHistoryFixture) transaction(version uint64) map[string]any {
	sender, destination := f.account.Address, "0xa"
	if version == 44 {
		sender, destination = "0x1", f.account.Address
	}
	tx := map[string]any{"type": "user_transaction", "hash": "0x" + fmt.Sprintf("%064x", version), "version": fmt.Sprint(version), "sender": sender, "sequence_number": "42", "success": true, "timestamp": "1700000000123456", "payload": map[string]any{"type": "entry_function_payload", "function": "0x1::aptos_account::transfer", "type_arguments": []string{}, "arguments": []any{destination, "123456789"}}}
	switch f.mode {
	case "wrong-rest-version", "archive-wrong-version":
		tx["version"] = "77"
	case "bad-rest-hash":
		tx["hash"] = "0xab"
	case "duplicate-rest-hash":
		tx["hash"] = "0x" + strings.Repeat("1", 64)
	case "missing-timestamp":
		delete(tx, "timestamp")
	case "future-timestamp":
		tx["timestamp"] = fmt.Sprint(time.Now().Add(time.Hour).UnixMicro())
	case "no-success":
		delete(tx, "success")
	case "pending":
		tx["type"] = "pending_transaction"
	case "failed":
		tx["success"] = false
	case "uninterpreted":
		tx["payload"] = map[string]any{"type": "entry_function_payload", "function": "0xbeef::dex::swap"}
	case "events", "effect-bound":
		store, _ := nativePrimaryStore(f.account.Address)
		tx["payload"] = map[string]any{"type": "entry_function_payload", "function": "0xbeef::dex::swap"}
		events := []any{map[string]any{"type": "0x1::fungible_asset::Deposit", "data": map[string]any{"store": store, "amount": "456789"}}}
		if f.mode == "effect-bound" {
			for len(events) < 1000 {
				events = append(events, events[0])
			}
		}
		tx["events"] = events
	}
	return tx
}

func TestAccountIndexedAptosHistoryIncludesOldIncomingAndOutgoing(t *testing.T) {
	f := newAccountHistoryFixture(t, "valid")
	page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
	if err != nil || len(page.Entries) != 2 || page.Complete || page.NextCursor != "" {
		t.Fatal("indexed account history failed", page, err)
	}
	if len(f.reads) != 2 || f.reads[0] != 73 || f.reads[1] != 44 || f.before[0] != "9000001" {
		t.Fatal("older candidates were not independently loaded by their exact committed version", f.reads, f.before)
	}
	out, incoming := page.Entries[0], page.Entries[1]
	if out.Transfers[0].Source != f.account.Address || incoming.Transfers[0].Destination != f.account.Address || incoming.Transfers[0].Source == f.account.Address || incoming.Transfers[0].Atomic != "123456789" || out.Timestamp != 1700000000 || out.Explorer == "" || !strings.Contains(page.Scope, "Account-indexed") {
		t.Fatal("owned incoming/outgoing amount, time, address or scope lost", page)
	}
}

func TestAccountIndexedAptosDescendingContinuationIsBoundedAndScoped(t *testing.T) {
	for _, mode := range []string{"pages", "repeat-page"} {
		t.Run(mode, func(t *testing.T) {
			f := newAccountHistoryFixture(t, mode)
			first, err := f.adapter.HistoryPage(context.Background(), f.account, "")
			if err != nil || len(first.Entries) != indexedHistoryLimit || first.NextCursor == "" || len(first.NextCursor) > 4096 {
				t.Fatal(first, err)
			}
			second, err := f.adapter.HistoryPage(context.Background(), f.account, first.NextCursor)
			if mode == "repeat-page" {
				if err == nil {
					t.Fatal("repeated boundary/page accepted")
				}
				return
			}
			if err != nil || len(second.Entries) != 1 || second.NextCursor != "" || second.Complete || f.before[1] != "969" {
				t.Fatal(second, err, f.before)
			}
			other, _ := f.adapter.Derive(seed(), 1, f.account.Network)
			if _, err := f.adapter.HistoryPage(context.Background(), other, first.NextCursor); err == nil {
				t.Fatal("cross-account continuation accepted")
			}
			if err := f.adapter.SetEndpoint(f.account.Network, "https://other.example/v1"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.adapter.HistoryPage(context.Background(), f.account, first.NextCursor); err == nil {
				t.Fatal("RPC replacement retained old continuation")
			}
			for _, bad := range []string{first.NextCursor + "=", strings.Repeat("x", 4097), base64.RawURLEncoding.EncodeToString([]byte(`{"version":1,"head":"9","before":"10"}`))} {
				if _, err := f.adapter.HistoryPage(context.Background(), f.account, bad); err == nil {
					t.Fatal("malformed continuation accepted")
				}
			}
		})
	}
}

func TestAccountIndexedAptosRejectsProviderFabricationAndUnavailableData(t *testing.T) {
	for _, mode := range []string{"rest-chain", "authority", "indexer-http", "indexer-chain", "indexer-owner", "bad-candidate-version", "future-candidate", "duplicate-candidate", "ascending-candidate", "overlong", "graphql-errors", "unsupported-schema", "null-rows", "no-data", "empty-ledgers", "rest-unavailable", "wrong-rest-version", "bad-rest-hash", "duplicate-rest-hash", "missing-timestamp", "future-timestamp", "no-success", "pending", "archive-chain", "archive-behind", "archive-stale", "archive-http", "archive-wrong-version", "head-overflow", "effect-bound"} {
		t.Run(mode, func(t *testing.T) {
			f := newAccountHistoryFixture(t, mode)
			if _, err := f.adapter.HistoryPage(context.Background(), f.account, ""); err == nil {
				t.Fatal("malformed/unavailable account history became verified", mode)
			}
		})
	}
}

func TestAccountIndexedAptosUnknownAndFailedEffectsAreHonest(t *testing.T) {
	for _, mode := range []string{"empty", "uninterpreted", "failed", "events"} {
		t.Run(mode, func(t *testing.T) {
			f := newAccountHistoryFixture(t, mode)
			page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
			if err != nil || page.Complete || page.Scope == "" {
				t.Fatal(page, err)
			}
			if mode == "failed" {
				if len(page.Entries) != 2 || page.Entries[0].State != "FAILED" {
					t.Fatal("failed attempt was shown as payment", page)
				}
			} else if mode == "events" {
				if len(page.Entries) != 2 || page.Entries[0].Transfers[0].Destination != f.account.Address || page.Entries[0].Transfers[0].Source != "" || page.Entries[0].Transfers[0].Atomic != "456789" {
					t.Fatal("contract-mediated APT deposit was not independently decoded", page)
				}
			} else if len(page.Entries) != 0 {
				t.Fatal("empty/unrecognized activity invented a transfer", page)
			}
		})
	}
}

func TestAccountIndexedAptosUsesOnlyCheckedFixedArchiveForPrunedVersions(t *testing.T) {
	f := newAccountHistoryFixture(t, "archive")
	page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
	if err != nil || len(page.Entries) != 2 || f.archiveReads != 1 || page.Entries[1].Block != "44" {
		t.Fatal("pruned committed versions not read through fixed separately checked archive", page, err, f.archiveReads)
	}
	// The archive is not contacted when ordinary selected-node reads succeed.
	direct := newAccountHistoryFixture(t, "valid")
	if _, err := direct.adapter.HistoryPage(context.Background(), direct.account, ""); err != nil || direct.archiveReads != 0 {
		t.Fatal("unnecessary or unchecked archive fallback", err, direct.archiveReads)
	}
}

func TestAptosHistoryCursorRejectsMalformedAndStaleSnapshot(t *testing.T) {
	f := newAccountHistoryFixture(t, "valid")
	good := indexedHistoryPosition{Version: 1, Account: historyDigest(f.account), RPC: historyDigest(f.adapter.endpoints[f.account.Network]), Head: "9000000", Before: "73"}
	for _, mode := range []string{"bad-version", "foreign-account", "endpoint", "head", "before", "zero", "past-head", "future-head", "space", "unknown-field", "too-long", "non-base64"} {
		p := good
		switch mode {
		case "bad-version":
			p.Version = 2
		case "foreign-account":
			p.Account = "other"
		case "endpoint":
			p.RPC = "other"
		case "head":
			p.Head = "x"
		case "before":
			p.Before = "x"
		case "zero":
			p.Before = "0"
		case "past-head":
			p.Before = "9000001"
		case "future-head":
			p.Head = "9000001"
		}
		b, _ := json.Marshal(p)
		if mode == "space" {
			b = append([]byte(" "), b...)
		}
		if mode == "unknown-field" {
			b = append(b[:len(b)-1], []byte(`,"unexpected":true}`)...)
		}
		cursor := base64.RawURLEncoding.EncodeToString(b)
		if mode == "too-long" {
			cursor = strings.Repeat("x", 4097)
		}
		if mode == "non-base64" {
			cursor = "***"
		}
		if _, err := f.adapter.HistoryPage(context.Background(), f.account, cursor); err == nil {
			t.Fatal("malformed/stale Aptos cursor accepted", mode)
		}
	}
	local := New()
	ac, _ := local.Derive(seed(), 0, "aptos-localnet")
	if _, err := local.HistoryPage(context.Background(), ac, "position"); err == nil {
		t.Fatal("local bound bypassed with continuation")
	}
	for _, network := range []string{"aptos-testnet", "aptos-mainnet"} {
		ac, _ := local.Derive(seed(), 0, network)
		if got := local.historyIndexer(ac); !strings.Contains(got, "api.") || !strings.HasSuffix(got, "/v1/graphql") {
			t.Fatal("official account indexer missing", network, got)
		}
	}
}
