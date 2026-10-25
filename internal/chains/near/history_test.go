package near

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
)

func historyHash(i byte) string { b := make([]byte, 32); b[31] = i; return base58.Encode(b) }
func TestTP050BoundedNEARNativeHistory(t *testing.T) {
	for _, mode := range []string{"valid", "state-height", "block-rpc", "height", "height-future", "height-first-mismatch", "hash", "many-chunks", "previous", "nondecreasing", "chunk-height", "chunk-future", "reused-chunk", "chunk-hash", "chunk-rpc", "chunk-header", "many-transactions", "irrelevant", "non-transfer", "multi-action", "transaction-hash", "signer", "action-null", "action-shape", "action-unknown", "deposit", "duplicate", "tx-rpc", "receipt-signer", "receipt-hash", "receipt-status"} {
		t.Run(mode, func(t *testing.T) {
			ac, _ := New().Derive(seed(), 0, "near-testnet")
			a, srv := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
				failure := func() (any, *rpcError) { return nil, &rpcError{Code: -1} }
				switch method {
				case "status":
					return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
				case "query":
					var kind string
					_ = json.Unmarshal(params["request_type"], &kind)
					if kind == "view_access_key" {
						return map[string]any{"nonce": 42, "permission": "FullAccess", "block_hash": historyHash(2), "block_height": 2}, nil
					}
					height := any(2)
					if mode == "state-height" {
						height = 1.5
					}
					return map[string]any{"amount": "10000000000000000000000000", "storage_usage": 182, "block_height": height, "block_hash": historyHash(2)}, nil
				case "block":
					if mode == "block-rpc" {
						return failure()
					}
					var id string
					_ = json.Unmarshal(params["block_id"], &id)
					height := byte(0)
					for _, h := range []byte{0, 1, 2} {
						if historyHash(h) == id {
							height = h
						}
					}
					blockHeight := any(height)
					hash := id
					previous := historyHash(height - 1)
					included := any(height)
					chunkHash := historyHash(height + 10)
					if height == 2 {
						switch mode {
						case "height":
							blockHeight = 1.5
						case "height-future":
							blockHeight = 3
						case "height-first-mismatch":
							blockHeight = 1
						case "hash":
							hash = historyHash(22)
						case "previous":
							previous = "bad"
						case "chunk-height":
							included = 1.5
						case "chunk-future":
							included = 3
						case "reused-chunk":
							included = 1
						case "chunk-hash":
							chunkHash = "bad"
						}
					}
					if height == 1 && mode == "nondecreasing" {
						blockHeight = 2
					}
					chunks := []any{map[string]any{"chunk_hash": chunkHash, "height_included": included}}
					if height == 0 {
						chunks = []any{}
					}
					if mode == "many-chunks" {
						for len(chunks) < 33 {
							chunks = append(chunks, chunks[0])
						}
					}
					return map[string]any{"header": map[string]any{"hash": hash, "prev_hash": previous, "height": blockHeight}, "chunks": chunks}, nil
				case "chunk":
					if mode == "chunk-rpc" {
						return failure()
					}
					var id string
					_ = json.Unmarshal(params["chunk_id"], &id)
					height := byte(1)
					if id == historyHash(12) {
						height = 2
					}
					signer := ac.Address
					receiver := "receiver.testnet"
					if height == 2 {
						signer = "sender.testnet"
						receiver = ac.Address
					}
					hash := historyHash(height + 20)
					actions := []any{map[string]any{"Transfer": map[string]any{"deposit": "12345"}}}
					switch mode {
					case "irrelevant":
						signer = "foreign.testnet"
						receiver = "other.testnet"
					case "non-transfer":
						actions = []any{map[string]any{"FunctionCall": map[string]any{}}}
					case "multi-action":
						actions = append(actions, actions[0])
					case "transaction-hash":
						hash = "bad"
					case "signer":
						signer = "BAD"
					case "action-null":
						actions = []any{map[string]any{"Transfer": nil}}
					case "action-shape":
						actions = []any{map[string]any{"Transfer": map[string]any{"deposit": 1}}}
					case "action-unknown":
						actions = []any{map[string]any{"Transfer": map[string]any{"deposit": "1", "unknown": true}}}
					case "deposit":
						actions = []any{map[string]any{"Transfer": map[string]any{"deposit": "-1"}}}
					}
					tx := map[string]any{"hash": hash, "signer_id": signer, "receiver_id": receiver, "actions": actions}
					transactions := []any{tx}
					if mode == "duplicate" {
						transactions = append(transactions, tx)
					}
					if mode == "many-transactions" {
						for len(transactions) < 513 {
							transactions = append(transactions, tx)
						}
					}
					if mode == "chunk-header" {
						id = "bad"
					}
					return map[string]any{"header": map[string]any{"chunk_hash": id}, "transactions": transactions}, nil
				case "tx":
					if mode == "tx-rpc" {
						return failure()
					}
					var hash string
					_ = json.Unmarshal(params["tx_hash"], &hash)
					signer := ac.Address
					receiver := "receiver.testnet"
					status := any(map[string]any{"Failure": map[string]any{"ActionError": "synthetic"}})
					if hash == historyHash(22) {
						signer = "sender.testnet"
						receiver = ac.Address
						status = map[string]any{"SuccessValue": ""}
					}
					if mode == "receipt-signer" {
						signer = "foreign.testnet"
					}
					if mode == "receipt-hash" {
						hash = historyHash(33)
					}
					if mode == "receipt-status" {
						status = "Unknown"
					}
					return map[string]any{"transaction": map[string]any{"hash": hash, "signer_id": signer, "receiver_id": receiver}, "status": status, "final_execution_status": "FINAL", "transaction_outcome": map[string]any{"block_hash": historyHash(2)}}, nil
				}
				return failure()
			})
			defer srv.Close()
			out, e := a.History(context.Background(), ac)
			valid := mode == "valid" || mode == "reused-chunk" || mode == "irrelevant" || mode == "non-transfer" || mode == "multi-action"
			if !valid {
				if e == nil {
					t.Fatal("hostile history accepted: " + mode)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "valid" {
				if len(out) != 2 || out[0].State != "CONFIRMED" || out[1].State != "FAILED" || !strings.Contains(out[0].Detail, "12345 yoctoNEAR") {
					t.Fatalf("native history %+v", out)
				}
			} else if mode == "reused-chunk" {
				if len(out) != 1 {
					t.Fatal("reused chunk duplicated")
				}
			} else if len(out) != 0 {
				t.Fatal("unrecognized effects credited")
			}
			if !strings.Contains(a.HistoryScope(), "12 final blocks") {
				t.Fatal("missing explicit scan bound")
			}
		})
	}
}
func TestTP051HistoryUnknownAuthority(t *testing.T) {
	a := New()
	ac, _ := a.Derive(seed(), 0, "near-testnet")
	ac.Profile = "unsupported"
	if _, e := a.History(context.Background(), ac); e == nil {
		t.Fatal("unsupported history authority")
	}
	if blockHash("bad") {
		t.Fatal("bad block hash")
	}
}
