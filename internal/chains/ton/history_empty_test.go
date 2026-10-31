package ton

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
)

func TestTONEmptyHistoryChecksStateAndRecoversOnlyExactRecordedHead(t *testing.T) {
	for _, mode := range []string{"unused", "recorded-head", "head-still-missing", "wrong-head", "wrong-account", "null-array", "null-anchored-array", "missing-head", "invalid-lt", "invalid-hash", "zero-lt-nonzero-hash", "positive-lt-zero-hash", "funded-zero-head", "active-zero-head", "stale-state", "state-outage", "healthy-nonempty"} {
		t.Run(mode, func(t *testing.T) {
			a, p, _, source, recipient := receiptFixture(t)
			chain := archivalChain(t, source, 2)
			info, _ := netInfo(p.Account.Network)
			state := map[string]any{"state": "uninitialized", "balance": "0", "sync_utime": time.Now().Unix(), "last_transaction_id": map[string]string{"lt": "0", "hash": base64.StdEncoding.EncodeToString(make([]byte, 32))}}
			last := state["last_transaction_id"].(map[string]string)
			anchored := mode == "recorded-head" || mode == "head-still-missing" || mode == "wrong-head" || mode == "wrong-account" || mode == "null-anchored-array"
			if anchored {
				state["state"], state["balance"] = "active", "123456789"
				last["lt"], last["hash"] = strconv.FormatUint(chain[0].LT, 10), base64.StdEncoding.EncodeToString(chain[0].Hash)
			}
			switch mode {
			case "missing-head":
				delete(state, "last_transaction_id")
			case "invalid-lt":
				last["lt"] = "01"
			case "invalid-hash":
				last["hash"] = "not a transaction hash"
			case "zero-lt-nonzero-hash":
				last["hash"] = base64.StdEncoding.EncodeToString(chain[0].Hash)
			case "positive-lt-zero-hash":
				last["lt"] = "1"
			case "funded-zero-head":
				state["balance"] = "1"
			case "active-zero-head":
				state["state"] = "active"
			case "stale-state":
				state["sync_utime"] = time.Now().Add(-time.Hour).Unix()
			}
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				var q struct {
					Method string `json:"method"`
					Params struct {
						Address string `json:"address"`
						LT      string `json:"lt"`
						Hash    string `json:"hash"`
					} `json:"params"`
				}
				if json.NewDecoder(request.Body).Decode(&q) != nil {
					t.Error("invalid public request")
					return
				}
				mu.Lock()
				calls = append(calls, q.Method)
				mu.Unlock()
				var result any
				switch q.Method {
				case "getMasterchainInfo":
					result = map[string]any{"init": map[string]any{"root_hash": info.root, "file_hash": info.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
				case "getAddressInformation":
					if q.Params.Address != p.Account.Address {
						t.Error("state request changed account")
					}
					if mode == "state-outage" || mode == "healthy-nonempty" {
						http.Error(w, "state unavailable", 503)
						return
					}
					result = state
				case "getTransactions":
					if q.Params.Address != p.Account.Address {
						t.Error("history request changed account")
					}
					rows := []*tlb.Transaction{}
					if q.Params.LT != "" {
						if !anchored || q.Params.LT != last["lt"] || q.Params.Hash != hex.EncodeToString(chain[0].Hash) {
							t.Error("fallback did not request the exact state-recorded LT/hash")
						}
						rows = chain
						if mode == "head-still-missing" {
							rows = nil
						}
						if mode == "wrong-head" {
							rows = chain[1:]
						}
						if mode == "wrong-account" {
							rows = []*tlb.Transaction{recipient}
						}
					}
					if mode == "healthy-nonempty" {
						rows = chain
					}
					entries := []map[string]string{}
					for _, tx := range rows {
						entries = append(entries, map[string]string{"data": txBOC(t, tx)})
					}
					result = entries
					if mode == "null-array" || mode == "null-anchored-array" && q.Params.LT != "" {
						result = nil
					}
				default:
					t.Errorf("unexpected method %s", q.Method)
					http.Error(w, "unexpected method", 400)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
			}))
			defer server.Close()
			if err := a.Configure(p.Account.Network, Endpoint{URL: server.URL}); err != nil {
				t.Fatal(err)
			}
			page, err := a.HistoryPage(context.Background(), p.Account, "")
			wantOK := mode == "unused" || mode == "recorded-head" || mode == "healthy-nonempty"
			if !wantOK {
				if err == nil {
					t.Fatal("missing or contradictory historical evidence became success", page)
				}
			} else if err != nil {
				t.Fatal(err)
			} else if mode == "unused" {
				if len(page.Entries) != 0 || page.Complete || page.NextCursor != "" || !strings.Contains(page.Scope, "uninitialized with zero balance and no recorded last transaction") {
					t.Fatal("unused-account observation lost its limits", page)
				}
			} else if len(page.Entries) != 2 || !page.Complete || page.Entries[0].Identity != hex.EncodeToString(chain[0].Hash) {
				t.Fatal("actual matching account-chain BOCs were not recovered", page)
			}
			mu.Lock()
			defer mu.Unlock()
			counts := map[string]int{}
			for _, method := range calls {
				counts[method]++
			}
			if mode == "healthy-nonempty" || mode == "null-array" {
				if counts["getAddressInformation"] != 0 || counts["getTransactions"] != 1 {
					t.Fatal("healthy or invalid result acquired an unnecessary fallback", calls)
				}
			} else if counts["getAddressInformation"] != 1 || counts["getTransactions"] != 1+btoi(anchored) {
				t.Fatal("empty history did not use a bounded state-bound lookup", calls)
			}
		})
	}
}

func btoi(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestTONSamePublicKeyProfilesKeepSeparateHistory(t *testing.T) {
	a, legacy, _, source, _ := receiptFixture(t)
	_, seed := fixtures(t)
	newAccount, err := a.DeriveProfile(seed, legacy.Account.Index, legacy.Account.Network, ProfileSubwallet42)
	if err != nil || newAccount.PublicKey != legacy.Account.PublicKey || newAccount.Address == legacy.Account.Address {
		t.Fatal("fixture profiles do not share a key and have distinct addresses", err)
	}
	chain := archivalChain(t, source, 1)
	_, close := archivalServer(t, a, legacy, map[string][]*tlb.Transaction{legacy.Account.Address: chain}, 100, nil)
	defer close()
	original, err := a.HistoryPage(context.Background(), legacy.Account, "")
	if err != nil || len(original.Entries) != 1 || !original.Complete {
		t.Fatal("legacy profile lost its original history", original, err)
	}
	newPage, err := a.HistoryPage(context.Background(), newAccount, "")
	if err != nil || len(newPage.Entries) != 0 || newPage.Complete || !strings.Contains(newPage.Scope, "no native activity was observed for this address") {
		t.Fatal("new subwallet reused another profile's history", newPage, err)
	}
	unchanged, err := a.HistoryPage(context.Background(), legacy.Account, "")
	if err != nil || len(unchanged.Entries) != 1 || unchanged.Entries[0].Identity != original.Entries[0].Identity {
		t.Fatal("reading a separate subwallet changed original history", unchanged, err)
	}
}
