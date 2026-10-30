package near

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

func tokenHistoryFixture(t *testing.T, mutate func(map[string]any), fullScan ...bool) (*Adapter, model.Account) {
	t.Helper()
	account, err := New().Derive(seed(), 0, "near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	adapter, server := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
		switch method {
		case "status":
			return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
		case "query":
			kind, contract := stringParam(params, "request_type"), stringParam(params, "account_id")
			if kind == "view_access_key" {
				return map[string]any{"permission": "FullAccess", "nonce": 42, "block_hash": historyHash(2), "block_height": 2}, nil
			}
			if kind == "view_account" {
				return map[string]any{"amount": "1000000000000000000000000000", "locked": "0", "storage_usage": 182, "code_hash": tokenPolicy("token.testnet", 6).CodeHash, "block_hash": historyHash(2), "block_height": 2}, nil
			}
			if stringParam(params, "method_name") == "ft_metadata" {
				decimals := 6
				if contract == "second.testnet" {
					decimals = 18
				}
				raw, _ := json.Marshal(map[string]any{"spec": "ft-1.0.0", "name": "Token", "symbol": "TEST", "decimals": decimals})
				values := make([]int, len(raw))
				for i, value := range raw {
					values[i] = int(value)
				}
				return map[string]any{"result": values, "block_hash": historyHash(2), "block_height": 2}, nil
			}
		case "tx":
			var requested, signer, final string
			_ = json.Unmarshal(params["tx_hash"], &requested)
			_ = json.Unmarshal(params["sender_account_id"], &signer)
			_ = json.Unmarshal(params["wait_until"], &final)
			if requested == historyHash(22) && len(fullScan) > 0 {
				return map[string]any{"status": map[string]any{"SuccessValue": ""}, "final_execution_status": "FINAL", "transaction": map[string]any{"hash": requested, "signer_id": account.Address, "receiver_id": "receiver.testnet", "nonce": 43, "public_key": account.PublicKey, "actions": []any{map[string]any{"Transfer": map[string]any{"deposit": "1"}}}}, "transaction_outcome": map[string]any{"block_hash": historyHash(2), "outcome": map[string]any{"receipt_ids": []string{historyHash(32)}}}, "receipts_outcome": []any{map[string]any{"id": historyHash(32), "block_hash": historyHash(2), "outcome": map[string]any{"executor_id": "receiver.testnet", "status": map[string]any{"SuccessValue": ""}, "receipt_ids": []string{}, "logs": []string{}}}}}, nil
			}
			if requested != historyHash(21) || signer != "sender.testnet" || final != "FINAL" {
				t.Errorf("wrong transaction authority %s %s %s", requested, signer, final)
			}
			event := `EVENT_JSON:{"standard":"nep141","version":"1.0.0","event":"ft_transfer","data":[{"old_owner_id":"sender.testnet","new_owner_id":"` + account.Address + `","amount":"1234567","memo":"invoice <script>"},{"old_owner_id":"` + account.Address + `","new_owner_id":"receiver.testnet","amount":"2"}]}`
			result := map[string]any{
				"status": map[string]any{"SuccessValue": ""}, "final_execution_status": "FINAL",
				// A nested token receipt may originate at a different contract.
				"transaction":         map[string]any{"hash": historyHash(21), "signer_id": "sender.testnet", "receiver_id": "router.testnet", "nonce": 42, "public_key": account.PublicKey, "actions": []any{map[string]any{"FunctionCall": map[string]any{"method_name": "ft_transfer", "args": "e30=", "deposit": "1", "gas": 30000000000000}}}},
				"transaction_outcome": map[string]any{"block_hash": historyHash(2), "outcome": map[string]any{"receipt_ids": []string{historyHash(31)}}},
				"receipts_outcome":    []any{map[string]any{"id": historyHash(31), "block_hash": historyHash(2), "outcome": map[string]any{"executor_id": "token.testnet", "status": map[string]any{"SuccessValue": ""}, "receipt_ids": []string{}, "logs": []string{event}}}},
			}
			if mutate != nil {
				mutate(result)
			}
			if len(fullScan) > 0 {
				result["transaction"].(map[string]any)["receiver_id"] = "token.testnet"
			}
			return result, nil
		case "EXPERIMENTAL_receipt":
			if stringParam(params, "receipt_id") == historyHash(32) {
				return map[string]any{"receipt_id": historyHash(32), "predecessor_id": account.Address, "receiver_id": "receiver.testnet", "receipt": map[string]any{"Action": map[string]any{"signer_id": account.Address, "actions": []any{map[string]any{"Transfer": map[string]any{"deposit": "1"}}}}}}, nil
			}
			return map[string]any{"receipt_id": historyHash(31), "predecessor_id": "sender.testnet", "receiver_id": "token.testnet", "receipt": map[string]any{"Action": map[string]any{"signer_id": "sender.testnet", "actions": []any{map[string]any{"FunctionCall": map[string]any{"method_name": "ft_transfer", "args": "e30=", "deposit": "1", "gas": 30000000000000}}}}}}, nil
		case "block":
			if len(fullScan) > 0 {
				id := stringParam(params, "block_id")
				height := byte(2)
				if id == historyHash(1) {
					height = 1
				}
				if id == historyHash(0) {
					height = 0
				}
				chunks := []any{}
				if height == 2 {
					chunks = append(chunks, map[string]any{"chunk_hash": historyHash(12), "height_included": 2})
				}
				return map[string]any{"header": map[string]any{"hash": id, "prev_hash": historyHash(height - 1), "height": height, "timestamp_nanosec": "1700000000000000000"}, "chunks": chunks}, nil
			}
			return map[string]any{"header": map[string]any{"hash": historyHash(2), "height": 2, "timestamp_nanosec": "1700000000000000000"}}, nil
		case "chunk":
			return map[string]any{"header": map[string]any{"chunk_hash": historyHash(12)}, "transactions": []any{
				map[string]any{"hash": historyHash(22), "signer_id": account.Address, "receiver_id": "receiver.testnet", "actions": []any{map[string]any{"Transfer": map[string]any{"deposit": "1"}}}},
				map[string]any{"hash": historyHash(21), "signer_id": "sender.testnet", "receiver_id": "token.testnet", "actions": []any{map[string]any{"FunctionCall": map[string]any{}}}},
			}}, nil
		}
		t.Errorf("unexpected history RPC %s", method)
		return nil, &rpcError{Code: -1}
	})
	t.Cleanup(server.Close)
	adapter, err = adapter.WithTokens([]model.NEP141Token{tokenPolicy("token.testnet", 6), tokenPolicy("second.testnet", 18)})
	if err != nil {
		t.Fatal(err)
	}
	return adapter, account
}

func TestNEARHistoryPreservesCheckedRowsOnlyForIndexerAvailabilityFailures(t *testing.T) {
	for _, mode := range []string{"http-403", "http-503", "transport", "malformed-json", "malformed-schema", "forged-candidate"} {
		t.Run(mode, func(t *testing.T) {
			adapter, account := tokenHistoryFixture(t, nil, true)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "http-403":
					http.Error(w, "denied", http.StatusForbidden)
				case "http-503":
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
				case "malformed-json":
					_, _ = w.Write([]byte(`{"data":`))
				case "malformed-schema":
					_, _ = w.Write([]byte(`{"data":null}`))
				case "forged-candidate":
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"transaction_hash": historyHash(21), "receipt_id": historyHash(99), "contract_account_id": "token.testnet", "affected_account_id": account.Address}}})
				}
			}))
			defer server.Close()
			adapter.historyEndpoints[account.Network] = server.URL
			if mode == "transport" {
				server.Close()
			}
			page, err := adapter.HistoryPage(context.Background(), account, "")
			if mode == "malformed-json" || mode == "malformed-schema" || mode == "forged-candidate" {
				if err == nil {
					t.Fatal("hostile indexer silently became a partial observation")
				}
				return
			}
			if err != nil || len(page.Entries) != 2 || page.Complete || page.NextCursor == "" || !strings.Contains(page.Scope, "unavailable/incomplete") {
				t.Fatal("availability failure hid checked rows or claimed completion", page, err)
			}
			position, positionErr := adapter.historyPosition(context.Background(), account, page.NextCursor)
			if positionErr != nil || position.Phase != "receipts" || position.Next != "" {
				t.Fatal("failed native index did not preserve the independent receipt/token continuation", position, positionErr)
			}
			if len(page.Entries[0].Transfers)+len(page.Entries[1].Transfers) != 3 {
				t.Fatal("native/direct-token effects lost", page.Entries)
			}
		})
	}
}

func historyOutcome(result map[string]any) map[string]any {
	return result["receipts_outcome"].([]any)[0].(map[string]any)["outcome"].(map[string]any)
}

func TestNEARTokenHistoryInterpretsOnlyFinalOwnedTrackedReceiptEffects(t *testing.T) {
	adapter, account := tokenHistoryFixture(t, nil)
	rows, err := adapter.tokenHistoryTransaction(context.Background(), account, historyHash(21), "sender.testnet", historyHash(31), "token.testnet")
	if err != nil || len(rows) != 1 || len(rows[0].Transfers) != 2 {
		t.Fatalf("history %+v %v", rows, err)
	}
	row := rows[0]
	if row.State != "OBSERVED" || row.Timestamp != 1700000000 || row.Explorer != "https://testnet.nearblocks.io/txns/"+row.Identity || row.Transfers[0].Asset != tokenPolicy("token.testnet", 6).Asset || row.Transfers[0].Atomic != "1234567" || row.Transfers[0].Comment != "invoice <script>" || row.Transfers[1].Atomic != "2" || row.Transfers[0].ID == row.Transfers[1].ID {
		t.Fatalf("structured token observation changed %+v", row)
	}
}

func TestNEARTokenHistoryReadsOwnedCanonicalMintAndBurnWithoutGuessingPeers(t *testing.T) {
	for _, event := range []string{"ft_mint", "ft_burn"} {
		for _, mode := range []string{"valid", "foreign-owner", "invalid-owner", "negative-amount", "numeric-amount", "unknown-field"} {
			t.Run(event+"/"+mode, func(t *testing.T) {
				account, _ := New().Derive(seed(), 0, "near-testnet")
				adapter, owned := tokenHistoryFixture(t, func(result map[string]any) {
					effect := map[string]any{"owner_id": account.Address, "amount": "123", "memo": "wrapped funds"}
					switch mode {
					case "foreign-owner":
						effect["owner_id"] = "foreign.testnet"
					case "invalid-owner":
						effect["owner_id"] = "INVALID"
					case "negative-amount":
						effect["amount"] = "-1"
					case "numeric-amount":
						effect["amount"] = 123
					case "unknown-field":
						effect["allowance"] = "123"
					}
					raw, _ := json.Marshal(map[string]any{"standard": "nep141", "version": "1.0.0", "event": event, "data": []any{effect}})
					historyOutcome(result)["logs"] = []string{"EVENT_JSON:" + string(raw)}
				})
				rows, err := adapter.tokenHistoryTransaction(context.Background(), owned, historyHash(21), "sender.testnet", historyHash(31), "token.testnet")
				if mode != "valid" {
					if err == nil {
						t.Fatal("unbound or malformed mint/burn accepted")
					}
					return
				}
				if err != nil || len(rows) != 1 || len(rows[0].Transfers) != 1 {
					t.Fatal(rows, err)
				}
				effect := rows[0].Transfers[0]
				if effect.Atomic != "123" || effect.Asset != tokenPolicy("token.testnet", 6).Asset {
					t.Fatal("mint/burn quantity/identity changed", effect)
				}
				if event == "ft_mint" && (effect.Source != "" || effect.Destination != owned.Address || effect.Comment != "Mint: wrapped funds") || event == "ft_burn" && (effect.Source != owned.Address || effect.Destination != "" || effect.Comment != "Burn: wrapped funds") {
					t.Fatal("mint/burn peer guessed or event type lost", effect)
				}
			})
		}
	}
}

func TestNEARTokenHistoryRejectsHostileReceiptGraphsAndEventData(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong-hash":   func(r map[string]any) { r["transaction"].(map[string]any)["hash"] = historyHash(99) },
		"wrong-signer": func(r map[string]any) { r["transaction"].(map[string]any)["signer_id"] = "foreign.testnet" },
		"not-final":    func(r map[string]any) { r["final_execution_status"] = "EXECUTED" },
		"failed-child": func(r map[string]any) {
			historyOutcome(r)["status"] = map[string]any{"Failure": map[string]any{"error": "failed"}}
		},
		"missing-child": func(r map[string]any) { historyOutcome(r)["receipt_ids"] = []string{historyHash(32)} },
		"duplicate-receipt": func(r map[string]any) {
			r["receipts_outcome"] = append(r["receipts_outcome"].([]any), r["receipts_outcome"].([]any)[0])
		},
		"unbound-receipt": func(r map[string]any) {
			r["transaction_outcome"].(map[string]any)["outcome"].(map[string]any)["receipt_ids"] = []string{historyHash(32)}
		},
		"cyclic-receipt":        func(r map[string]any) { historyOutcome(r)["receipt_ids"] = []string{historyHash(31)} },
		"wrong-executor":        func(r map[string]any) { historyOutcome(r)["executor_id"] = "untracked.testnet" },
		"noncanonical-quantity": func(r map[string]any) { replaceTokenHistoryLog(r, `"1234567"`, `"01234567"`) },
		"negative-quantity":     func(r map[string]any) { replaceTokenHistoryLog(r, `"1234567"`, `"-1"`) },
		"large-quantity": func(r map[string]any) {
			replaceTokenHistoryLog(r, `"1234567"`, `"340282366920938463463374607431768211456"`)
		},
		"invalid-peer": func(r map[string]any) { replaceTokenHistoryLog(r, "sender.testnet", "INVALID") },
		"duplicate-key": func(r map[string]any) {
			replaceTokenHistoryLog(r, `"amount":"1234567"`, `"amount":"1","amount":"1234567"`)
		},
		"unknown-event-field": func(r map[string]any) {
			replaceTokenHistoryLog(r, `"event":"ft_transfer"`, `"event":"ft_transfer","payload":true`)
		},
		"nul-comment":          func(r map[string]any) { replaceTokenHistoryLog(r, "invoice <script>", `invoice\u0000`) },
		"large-comment":        func(r map[string]any) { replaceTokenHistoryLog(r, "invoice <script>", strings.Repeat("a", 4097)) },
		"unsupported-standard": func(r map[string]any) { replaceTokenHistoryLog(r, "nep141", "nep999") },
		"unsupported-version":  func(r map[string]any) { replaceTokenHistoryLog(r, "1.0.0", "2.0.0") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			adapter, account := tokenHistoryFixture(t, mutate)
			if _, err := adapter.tokenHistoryTransaction(context.Background(), account, historyHash(21), "sender.testnet", historyHash(31), "token.testnet"); err == nil {
				t.Fatal("hostile token effect accepted")
			}
		})
	}
}

func replaceTokenHistoryLog(result map[string]any, old, replacement string) {
	logs := historyOutcome(result)["logs"].([]string)
	logs[0] = strings.Replace(logs[0], old, replacement, 1)
}

func TestNEARIndexedHistoryUsesCandidatesWithoutTrustingProviderAmounts(t *testing.T) {
	for _, mode := range []string{"valid", "scanned", "duplicate", "missing-page", "null-page", "oversized-page", "wrong-account", "wrong-contract", "wrong-receipt", "invalid-hash", "missing-hash", "wrong-detail-hash", "wrong-detail-signer", "outage"} {
		t.Run(mode, func(t *testing.T) {
			adapter, account := tokenHistoryFixture(t, nil)
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "outage" {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v3/accounts/"+account.Address+"/ft-txns" {
					if r.URL.Query().Get("limit") != "25" || len(r.URL.Query()) != 1 {
						t.Error("unbounded or unexpected indexer query")
					}
					candidate := map[string]any{"transaction_hash": historyHash(21), "receipt_id": historyHash(31), "contract_account_id": "token.testnet", "affected_account_id": account.Address, "amount": "99999999999999999999999999999", "url": "https://evil.invalid"}
					switch mode {
					case "missing-page":
						_ = json.NewEncoder(w).Encode(map[string]any{})
						return
					case "null-page":
						_ = json.NewEncoder(w).Encode(map[string]any{"data": nil})
						return
					case "wrong-account":
						candidate["affected_account_id"] = "foreign.testnet"
					case "wrong-contract":
						candidate["contract_account_id"] = "second.testnet"
					case "wrong-receipt":
						candidate["receipt_id"] = historyHash(32)
					case "invalid-hash":
						candidate["transaction_hash"] = "invalid"
					case "missing-hash":
						candidate["transaction_hash"] = nil
					}
					data := []any{candidate}
					if mode == "duplicate" {
						data = append(data, candidate)
					}
					if mode == "oversized-page" {
						for len(data) < 26 {
							data = append(data, candidate)
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
					return
				}
				if r.URL.Path != "/v3/txns/"+historyHash(21) {
					t.Errorf("unexpected candidate lookup %s", r.URL.Path)
				}
				requests++
				detail := map[string]any{"transaction_hash": historyHash(21), "signer_account_id": "sender.testnet"}
				if mode == "wrong-detail-hash" {
					detail["transaction_hash"] = historyHash(22)
				}
				if mode == "wrong-detail-signer" {
					detail["signer_account_id"] = "INVALID"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": detail})
			}))
			defer server.Close()
			adapter.historyEndpoints[account.Network] = server.URL
			scanned := map[string]bool{}
			if mode == "scanned" {
				scanned[historyHash(21)] = true
			}
			rows, err := adapter.indexedTokenHistory(context.Background(), account, scanned)
			if mode != "valid" && mode != "duplicate" && mode != "scanned" {
				if err == nil {
					t.Fatal("hostile indexed history accepted")
				}
				return
			}
			if err != nil || requests != 1 {
				t.Fatal("candidate not independently checked", requests, err)
			}
			if mode == "scanned" {
				if len(rows) != 0 {
					t.Fatal("direct-RPC and indexed observations duplicated")
				}
			} else if len(rows) != 1 || len(rows[0].Transfers) != 2 || rows[0].Transfers[0].Atomic != "1234567" {
				t.Fatal("provider metadata established transfer", rows)
			}
		})
	}
}
