package aptos

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTP050BoundedNativeHistory(t *testing.T) {
	for _, mode := range []string{"valid", "window", "max-ledger", "empty-ledger", "wrong-version", "bad-version", "missing-row", "extra-row", "rpc", "missing-success", "non-entry", "non-native", "token-transfer", "bad-module", "type-arguments", "bad-arguments", "bad-argument-json", "bad-destination", "bad-sender", "bad-amount", "bad-sequence", "bad-hash", "foreign-transfer"} {
		t.Run(mode, func(t *testing.T) {
			ac, _ := New().Derive(seed(), 0, "aptos-testnet")
			end := uint64(4)
			if mode == "window" {
				end = 300
			}
			if mode == "max-ledger" {
				end = ^uint64(0)
			}
			if mode == "empty-ledger" {
				end = 0
			}
			requests := 0
			a, srv := aptosServer(t, func(r *http.Request) any {
				switch {
				case r.URL.Path == "/":
					return map[string]any{"chain_id": 2, "ledger_version": fmt.Sprint(end), "ledger_timestamp": fmt.Sprint(time.Now().UnixMicro())}
				case strings.HasPrefix(r.URL.Path, "/accounts/"):
					return map[string]any{"sequence_number": "42", "authentication_key": ac.Address}
				case r.URL.Path == "/transactions":
					requests++
					if mode == "rpc" {
						return map[string]any{"error": "synthetic"}
					}
					start, _ := strconv.ParseUint(r.URL.Query().Get("start"), 10, 64)
					count, _ := strconv.ParseUint(r.URL.Query().Get("limit"), 10, 64)
					rows := []any{}
					for j := uint64(0); j < count; j++ {
						version := start + j
						tx := map[string]any{"type": "block_metadata_transaction", "version": fmt.Sprint(version)}
						if version == end || version == end-1 {
							success := version == end
							args := []any{"0x" + strings.Repeat("0", 63) + "a", "12345"}
							sender := ac.Address
							if version == end-1 {
								sender = "0x1"
								args[0] = ac.Address
							}
							payload := map[string]any{"type": "entry_function_payload", "function": "0x1::aptos_account::transfer", "type_arguments": []string{}, "arguments": args}
							tx = map[string]any{"type": "user_transaction", "version": fmt.Sprint(version), "sender": sender, "sequence_number": "42", "success": success, "hash": "0x" + fmt.Sprintf("%064x", version), "payload": payload, "timestamp": "1700000000000000"}
							switch mode {
							case "wrong-version":
								tx["version"] = "9999"
							case "bad-version":
								tx["version"] = "bad"
							case "missing-success":
								delete(tx, "success")
							case "non-entry":
								payload["type"] = "script_payload"
							case "non-native":
								payload["function"] = "0x2::other::transfer"
							case "token-transfer":
								payload["function"] = "0x1::coin::transfer"
								payload["type_arguments"] = []string{"0x2::other::Token"}
							case "bad-module":
								payload["function"] = "invalid::aptos_account::transfer"
							case "type-arguments":
								payload["type_arguments"] = []string{"bad"}
							case "bad-arguments":
								payload["arguments"] = []any{}
							case "bad-argument-json":
								args[1] = map[string]any{"value": "12345"}
							case "bad-destination":
								args[0] = "invalid"
							case "bad-sender":
								tx["sender"] = "invalid"
							case "bad-amount":
								args[1] = "-1"
							case "bad-sequence":
								tx["sequence_number"] = "-1"
							case "bad-hash":
								tx["hash"] = "0x11"
							case "foreign-transfer":
								tx["sender"] = "0x1"
								args[0] = "0x2"
							}
						}
						rows = append(rows, tx)
					}
					if mode == "missing-row" {
						return rows[:len(rows)-1]
					}
					if mode == "extra-row" {
						rows = append(rows, rows[0])
					}
					return rows
				}
				return map[string]any{}
			})
			defer srv.Close()
			out, e := a.History(context.Background(), ac)
			valid := mode == "valid" || mode == "window" || mode == "max-ledger" || mode == "empty-ledger" || mode == "non-entry" || mode == "non-native" || mode == "token-transfer" || mode == "foreign-transfer"
			if !valid {
				if e == nil {
					t.Fatal("hostile history accepted: " + mode)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "valid" || mode == "window" || mode == "max-ledger" {
				if len(out) != 2 || out[0].State != "FAILED" || out[1].State != "CONFIRMED" || !strings.Contains(out[0].Detail, "12345 octas") {
					t.Fatalf("native history %+v", out)
				}
				if out[0].Timestamp != 1700000000 || out[0].Explorer == "" || len(out[0].Transfers) != 1 || out[0].Transfers[0].Source != "0x"+strings.Repeat("0", 63)+"1" || out[0].Transfers[0].Destination != ac.Address || out[0].Transfers[0].Atomic != "12345" || out[0].Transfers[0].Asset.Decimals != 8 {
					t.Fatal("APT structured history changed", out[0])
				}
			} else if len(out) != 0 {
				t.Fatal("unrecognized transfer credited")
			}
			if (mode == "window" || mode == "max-ledger") && requests != 8 {
				t.Fatalf("unbounded or incomplete pagination %d", requests)
			}
			if !strings.Contains(a.HistoryScope(), "256") {
				t.Fatal("scope lacks bound")
			}
		})
	}
}
func TestTP022HistoryNativeFunctionSchemes(t *testing.T) {
	for _, tc := range []struct {
		function string
		types    []string
		want     bool
		err      bool
	}{{"other", nil, false, false}, {"0x1::aptos_account::transfer_coins", []string{"0x1::aptos_coin::AptosCoin"}, true, false}, {"0x1::coin::transfer", nil, false, true}, {"0x1::unrelated::call", nil, false, false}} {
		ok, e := nativeFunction(tc.function, tc.types)
		if ok != tc.want || (e != nil) != tc.err {
			t.Fatal(tc.function)
		}
	}
	a := New()
	ac, _ := a.Derive(seed(), 0, "aptos-testnet")
	ac.Profile = "unknown"
	if _, e := a.History(context.Background(), ac); e == nil {
		t.Fatal("unsupported history profile")
	}
}
