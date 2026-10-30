package near

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/model"
)

type historicalFixture struct {
	adapter  *Adapter
	account  model.Account
	index    func(http.ResponseWriter, *http.Request)
	mutate   func(string, map[string]any)
	requests []string
	unknown  string
	signer   string
}

func newHistoricalFixture(t *testing.T, tokens bool) *historicalFixture {
	t.Helper()
	f := &historicalFixture{}
	f.account, _ = New().Derive(seed(), 0, "near-testnet")
	a, server := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
		var value map[string]any
		switch method {
		case "status":
			return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
		case "query":
			kind := stringParam(params, "request_type")
			if kind == "view_access_key" {
				return map[string]any{"permission": "FullAccess", "nonce": 42, "block_hash": historyHash(200), "block_height": 1000}, nil
			}
			if kind == "view_account" {
				return map[string]any{"amount": "1000000000000000000000000000", "storage_usage": 182, "code_hash": tokenPolicy("token.testnet", 6).CodeHash, "block_hash": historyHash(200), "block_height": 1000}, nil
			}
			if stringParam(params, "method_name") == "ft_metadata" {
				raw, _ := json.Marshal(map[string]any{"spec": "ft-1.0.0", "name": "Token", "symbol": "TEST", "decimals": 6})
				values := make([]int, len(raw))
				for i, b := range raw {
					values[i] = int(b)
				}
				return map[string]any{"result": values, "block_hash": historyHash(200), "block_height": 1000}, nil
			}
		case "block":
			height, timestamp := 1, "1690000000000000000"
			if stringParam(params, "block_id") == historyHash(200) {
				height, timestamp = 1000, "1700000000000000000"
			}
			value = map[string]any{"header": map[string]any{"hash": stringParam(params, "block_id"), "height": height, "timestamp_nanosec": timestamp}, "chunks": []any{}}
		case "tx":
			hash := stringParam(params, "tx_hash")
			if hash == f.unknown {
				return nil, &rpcError{Code: -32000, Name: "UNKNOWN_TRANSACTION"}
			}
			if stringParam(params, "wait_until") != "FINAL" {
				t.Error("historical query did not require FINAL execution")
			}
			var id byte
			for i := byte(1); i <= 70; i++ {
				if hash == historyHash(i) {
					id = i
				}
			}
			if id == 0 {
				t.Errorf("unexpected transaction %s", hash)
			}
			value = f.execution(id)
			expectedSigner := value["transaction"].(map[string]any)["signer_id"]
			if f.signer != "" {
				expectedSigner = f.signer
			}
			if stringParam(params, "sender_account_id") != expectedSigner {
				t.Error("candidate signer did not bind RPC transaction")
			}
		case "EXPERIMENTAL_receipt":
			receipt := stringParam(params, "receipt_id")
			for i := byte(1); i <= 70; i++ {
				if receipt == historyHash(100+i) {
					value = f.receipt(i, false)
					break
				}
				if i == 31 && receipt == historyHash(231) || i > 40 && receipt == historyHash(160+i) {
					value = f.receipt(i, true)
					break
				}
			}
			if value == nil {
				t.Errorf("unexpected receipt %s", receipt)
			}
		default:
			t.Errorf("unexpected historical RPC %s", method)
		}
		if value == nil {
			return nil, &rpcError{Code: -1}
		}
		if f.mutate != nil {
			f.mutate(method, value)
		}
		return value, nil
	})
	t.Cleanup(server.Close)
	f.adapter = a
	if tokens {
		var err error
		f.adapter, err = a.WithTokens([]model.NEP141Token{tokenPolicy("token.testnet", 6)})
		if err != nil {
			t.Fatal(err)
		}
	}
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.RequestURI())
		if f.index != nil {
			f.index(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v3/txns/") {
			hash := strings.TrimPrefix(r.URL.Path, "/v3/txns/")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"transaction_hash": hash, "signer_account_id": "sender.testnet"}})
			return
		}
		if r.URL.Query().Get("limit") != "25" || r.URL.Query().Get("before_ts") != "1700000000000000001" {
			t.Errorf("unbounded/unfrozen query %s", r.URL.RawQuery)
		}
		next := ""
		candidates := []any{}
		phase := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		switch phase {
		case "txns":
			start, end := byte(1), byte(25)
			if r.URL.Query().Get("next") == "" {
				next = base64.StdEncoding.EncodeToString([]byte("older native transactions"))
			} else {
				start, end = 26, 30
			}
			for id := start; id <= end; id++ {
				candidates = append(candidates, f.candidate(id, phase))
			}
		case "receipts":
			candidates = append(candidates, f.candidate(31, phase), f.candidate(1, phase))
		case "ft-txns":
			start, end := byte(41), byte(65)
			if r.URL.Query().Get("next") == "" {
				next = base64.StdEncoding.EncodeToString([]byte("older token events"))
			} else {
				start, end = 66, 70
			}
			for id := start; id <= end; id++ {
				candidates = append(candidates, f.candidate(id, phase))
			}
		default:
			t.Errorf("unexpected indexed route %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": candidates, "meta": map[string]any{"next_page": next}})
	}))
	t.Cleanup(index.Close)
	f.adapter.historyEndpoints[f.account.Network] = index.URL
	return f
}

func (f *historicalFixture) actions(id byte) []any {
	if id <= 30 {
		return []any{map[string]any{"Transfer": map[string]any{"deposit": strconv.Itoa(10000 + int(id))}}}
	}
	return []any{map[string]any{"FunctionCall": map[string]any{"method_name": "route", "args": "e30=", "deposit": "0", "gas": 30000000000000}}}
}

func (f *historicalFixture) peers(id byte) (string, string) {
	if id <= 30 {
		return "sender.testnet", f.account.Address
	}
	if id == 31 {
		return "sender.testnet", "pool.testnet"
	}
	return "sender.testnet", "router.testnet"
}

func (f *historicalFixture) execution(id byte) map[string]any {
	signer, receiver := f.peers(id)
	children := []string{}
	logs := []string{}
	receipts := []any{}
	if id == 31 {
		children = append(children, historyHash(231))
		receipts = append(receipts, map[string]any{"id": historyHash(231), "block_hash": historyHash(250), "outcome": map[string]any{"executor_id": f.account.Address, "status": map[string]any{"SuccessValue": ""}, "receipt_ids": []string{}, "logs": []string{}}})
	} else if id > 40 {
		children = append(children, historyHash(160+id))
		logs = []string{fmt.Sprintf(`EVENT_JSON:{"standard":"nep141","version":"1.0.0","event":"ft_transfer","data":[{"old_owner_id":"sender.testnet","new_owner_id":"%s","amount":"%d","memo":"actual RPC event"}]}`, f.account.Address, 1000+int(id))}
		receipts = append(receipts, map[string]any{"id": historyHash(160 + id), "block_hash": historyHash(250), "outcome": map[string]any{"executor_id": "token.testnet", "status": map[string]any{"SuccessValue": ""}, "receipt_ids": []string{}, "logs": logs}})
	}
	root := map[string]any{"id": historyHash(100 + id), "block_hash": historyHash(250), "outcome": map[string]any{"executor_id": receiver, "status": map[string]any{"SuccessValue": ""}, "receipt_ids": children, "logs": []string{}}}
	receipts = append([]any{root}, receipts...)
	return map[string]any{"status": map[string]any{"SuccessValue": ""}, "final_execution_status": "FINAL", "transaction": map[string]any{"hash": historyHash(id), "signer_id": signer, "receiver_id": receiver, "nonce": 100 + int(id), "public_key": f.account.PublicKey, "actions": f.actions(id)}, "transaction_outcome": map[string]any{"block_hash": historyHash(250), "outcome": map[string]any{"receipt_ids": []string{historyHash(100 + id)}}}, "receipts_outcome": receipts}
}

func (f *historicalFixture) receipt(id byte, child bool) map[string]any {
	signer, receiver := f.peers(id)
	predecessor, receiptID, actions := signer, historyHash(100+id), f.actions(id)
	if child {
		predecessor = receiver
		if id == 31 {
			receiver, receiptID = f.account.Address, historyHash(231)
			actions = []any{map[string]any{"Transfer": map[string]any{"deposit": "555"}}}
		} else {
			receiver, receiptID = "token.testnet", historyHash(160+id)
			actions = []any{map[string]any{"FunctionCall": map[string]any{"method_name": "ft_transfer", "args": "e30=", "deposit": "1", "gas": 30000000000000}}}
		}
	}
	return map[string]any{"receipt_id": receiptID, "predecessor_id": predecessor, "receiver_id": receiver, "receipt": map[string]any{"Action": map[string]any{"signer_id": signer, "actions": actions}}}
}

func (f *historicalFixture) candidate(id byte, phase string) map[string]any {
	signer, receiver := f.peers(id)
	value := map[string]any{"transaction_hash": historyHash(id), "amount": "999999999999999999999999999999999", "status": "invented", "url": "https://evil.invalid/"}
	switch phase {
	case "txns":
		value["signer_account_id"], value["receiver_account_id"] = signer, receiver
	case "receipts":
		if id == 31 {
			value["predecessor_account_id"], value["receiver_account_id"], value["receipt_id"] = "pool.testnet", f.account.Address, historyHash(231)
		} else {
			value["predecessor_account_id"], value["receiver_account_id"], value["receipt_id"] = signer, receiver, historyHash(100+id)
		}
	case "ft-txns":
		value["contract_account_id"], value["affected_account_id"], value["receipt_id"] = "token.testnet", f.account.Address, historyHash(160+id)
	}
	return value
}

func TestNEARHistoricalPagesReadOlderNativeReceiptsAndMoreThan25TokenCandidates(t *testing.T) {
	f := newHistoricalFixture(t, true)
	seen := map[string]model.Receipt{}
	cursor := ""
	pages, overlap := 0, 0
	for {
		page, err := f.adapter.HistoryPage(context.Background(), f.account, cursor)
		if err != nil || page.Complete || len(page.Entries) > 25 || len(page.NextCursor) > 4096 {
			t.Fatalf("bounded historical page %+v: %v", page, err)
		}
		pages++
		for _, row := range page.Entries {
			if old, exists := seen[row.Identity]; exists {
				if !reflect.DeepEqual(old, row) {
					t.Fatal("overlapping index changed a whole-transaction observation", old, row)
				}
				overlap++
			} else {
				seen[row.Identity] = row
			}
			if row.Timestamp != 1690000000 || row.State != "OBSERVED" || len(row.Transfers) != 1 || row.Explorer != "https://testnet.nearblocks.io/txns/"+row.Identity {
				t.Fatal("indexer rather than historical RPC established fields", row)
			}
		}
		if page.NextCursor == "" {
			if !strings.Contains(page.Scope, "positions are exhausted") {
				t.Fatal("terminal index scope missing")
			}
			break
		}
		cursor = page.NextCursor
		if pages > 6 {
			t.Fatal("nonprogressing historical pagination")
		}
	}
	if pages != 5 || overlap != 1 || len(seen) != 61 {
		t.Fatalf("missing older address-indexed history: %d pages, %d overlap, %d transactions", pages, overlap, len(seen))
	}
	for id := byte(1); id <= 30; id++ {
		effect := seen[historyHash(id)].Transfers[0]
		if effect.Atomic != strconv.Itoa(10000+int(id)) || effect.Source != "sender.testnet" || effect.Destination != f.account.Address || effect.Asset.ID != asset(f.account.Network) {
			t.Fatal("older native amount/peers missing", id, effect)
		}
	}
	if effect := seen[historyHash(31)].Transfers[0]; effect.Atomic != "555" || effect.Source != "pool.testnet" || effect.Destination != f.account.Address || effect.Comment != "" {
		t.Fatal("contract Transfer was lost, duplicated or invented a reward label", effect)
	}
	for id := byte(41); id <= 70; id++ {
		effect := seen[historyHash(id)].Transfers[0]
		if effect.Atomic != strconv.Itoa(1000+int(id)) || effect.Asset != tokenPolicy("token.testnet", 6).Asset || effect.Comment != "actual RPC event" {
			t.Fatal("older tracked-token event missing", id, effect)
		}
	}
}

func TestNEARHistoricalCanonicalRowIncludesNativeAndTokenEffectsBeforeIndexOverlap(t *testing.T) {
	f := newHistoricalFixture(t, true)
	f.signer = f.account.Address
	f.mutate = func(method string, value map[string]any) {
		if method == "tx" {
			tx := value["transaction"].(map[string]any)
			tx["signer_id"] = f.account.Address
			tx["actions"].([]any)[0].(map[string]any)["FunctionCall"].(map[string]any)["deposit"] = "77"
		}
		if method == "EXPERIMENTAL_receipt" {
			value["receipt"].(map[string]any)["Action"].(map[string]any)["signer_id"] = f.account.Address
			if value["receipt_id"] == historyHash(141) {
				value["predecessor_id"] = f.account.Address
				value["receipt"].(map[string]any)["Action"].(map[string]any)["actions"].([]any)[0].(map[string]any)["FunctionCall"].(map[string]any)["deposit"] = "77"
			}
		}
	}
	p, err := f.adapter.historyPosition(context.Background(), f.account, "")
	if err != nil {
		t.Fatal(err)
	}
	native := historicalCandidate{Hash: historyHash(41), Signer: f.account.Address, Receiver: "router.testnet"}
	first, err := f.adapter.historicalTransaction(context.Background(), f.account, native.Hash, native.Signer, native, p)
	if err != nil || len(first.Row.Transfers) != 2 {
		t.Fatal("native candidate omitted same-transaction token effects", first, err)
	}
	p.Phase = "ft-txns"
	token := historicalCandidate{Hash: native.Hash, Receipt: historyHash(201), Contract: "token.testnet", Affected: f.account.Address}
	second, err := f.adapter.historicalTransaction(context.Background(), f.account, native.Hash, native.Signer, token, p)
	if err != nil || !reflect.DeepEqual(first.Row, second.Row) {
		t.Fatal("native/token indexes do not yield the same canonical full observation", first, second, err)
	}
}

func TestNEARHistoricalPagesRejectUnboundCandidateAndReceiptEvidence(t *testing.T) {
	for _, mode := range []string{"candidate-hash", "candidate-signer", "candidate-receiver", "candidate-no-owned-peer", "page-null", "page-oversize", "page-error", "cursor-invalid", "cursor-oversize", "empty-with-cursor", "tx-hash", "tx-signer", "tx-nonce", "tx-key", "tx-no-actions", "not-final", "missing-child", "cyclic-child", "duplicate-child", "root-mismatch", "receipt-id", "receipt-executor", "receipt-predecessor", "receipt-signer", "bad-native-amount", "bad-root-action", "future-time", "wrong-block"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoricalFixture(t, false)
			f.index = func(w http.ResponseWriter, r *http.Request) {
				c := f.candidate(1, "txns")
				page := map[string]any{"data": []any{c}}
				switch mode {
				case "candidate-hash":
					c["transaction_hash"] = "bad"
				case "candidate-signer":
					c["signer_account_id"] = "INVALID"
				case "candidate-receiver":
					c["receiver_account_id"] = "other.testnet"
				case "candidate-no-owned-peer":
					c["signer_account_id"], c["receiver_account_id"] = "foreign.testnet", "other.testnet"
				case "page-null":
					page["data"] = nil
				case "page-oversize":
					list := []any{}
					for i := 0; i < 26; i++ {
						list = append(list, c)
					}
					page["data"] = list
				case "page-error":
					page["errors"] = []any{map[string]any{"message": "not available"}}
				case "cursor-invalid":
					page["meta"] = map[string]any{"next_page": "https://evil.invalid"}
				case "cursor-oversize":
					page["meta"] = map[string]any{"next_page": strings.Repeat("A", 1028)}
				case "empty-with-cursor":
					page["data"] = []any{}
					page["meta"] = map[string]any{"next_page": base64.StdEncoding.EncodeToString([]byte("empty"))}
				}
				_ = json.NewEncoder(w).Encode(page)
			}
			f.mutate = func(method string, value map[string]any) {
				if method == "tx" {
					tx := value["transaction"].(map[string]any)
					receipts := value["receipts_outcome"].([]any)
					root := receipts[0].(map[string]any)["outcome"].(map[string]any)
					switch mode {
					case "tx-hash":
						tx["hash"] = historyHash(99)
					case "tx-signer":
						tx["signer_id"] = "forged.testnet"
					case "tx-nonce":
						tx["nonce"] = 0
					case "tx-key":
						tx["public_key"] = "ed25519:bad"
					case "tx-no-actions":
						tx["actions"] = []any{}
					case "not-final":
						value["final_execution_status"] = "EXECUTED"
					case "missing-child":
						root["receipt_ids"] = []string{historyHash(99)}
					case "cyclic-child":
						root["receipt_ids"] = []string{historyHash(101)}
					case "duplicate-child":
						value["receipts_outcome"] = append(receipts, receipts[0])
					case "root-mismatch":
						root["executor_id"] = "foreign.testnet"
					case "bad-native-amount":
						tx["actions"].([]any)[0].(map[string]any)["Transfer"].(map[string]any)["deposit"] = "-1"
					case "bad-root-action":
						tx["actions"].([]any)[0].(map[string]any)["Transfer"].(map[string]any)["deposit"] = "10002"
					}
				}
				if method == "EXPERIMENTAL_receipt" {
					switch mode {
					case "receipt-id":
						value["receipt_id"] = historyHash(99)
					case "receipt-executor":
						value["receiver_id"] = "foreign.testnet"
					case "receipt-predecessor":
						value["predecessor_id"] = "foreign.testnet"
					case "receipt-signer":
						value["receipt"].(map[string]any)["Action"].(map[string]any)["signer_id"] = "INVALID"
					}
				}
				if method == "block" {
					switch mode {
					case "future-time":
						value["header"].(map[string]any)["timestamp_nanosec"] = strconv.FormatInt((time.Now().Unix()+1000)*1_000_000_000, 10)
					case "wrong-block":
						value["header"].(map[string]any)["hash"] = historyHash(99)
					}
				}
			}
			if _, err := f.adapter.HistoryPage(context.Background(), f.account, ""); err == nil {
				t.Fatal("unbound/hostile indexed historical evidence accepted")
			}
		})
	}
}

func TestNEARHistoricalContinuationBindsAccountEndpointPoliciesAndCheckedHead(t *testing.T) {
	for _, mode := range []string{"valid", "other-account", "rpc", "indexer", "tokens", "head", "height", "before", "phase", "unknown-field", "oversized", "repeated-provider-position"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoricalFixture(t, false)
			page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
			if err != nil || page.NextCursor == "" {
				t.Fatal("positive initial-page control failed", page, err)
			}
			cursor, account := page.NextCursor, f.account
			position, err := f.adapter.historyPosition(context.Background(), account, cursor)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "other-account":
				account, _ = f.adapter.Derive(seed(), 1, account.Network)
			case "rpc":
				f.adapter.endpoints[account.Network] += "/changed"
			case "indexer":
				f.adapter.historyEndpoints[account.Network] += "/changed"
			case "tokens":
				f.adapter, _ = f.adapter.WithTokens([]model.NEP141Token{tokenPolicy("token.testnet", 6)})
			case "head":
				position.Head = "bad"
			case "height":
				position.Height = "999"
			case "before":
				position.Before = "1800000000000000001"
			case "phase":
				position.Phase = "arbitrary"
			case "oversized":
				cursor = strings.Repeat("A", 4097)
			case "repeated-provider-position":
				f.index = func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{f.candidate(26, "txns")}, "meta": map[string]any{"next_page": position.Next}})
				}
			}
			if mode == "head" || mode == "height" || mode == "before" || mode == "phase" {
				cursor, _ = encodeHistoricalPosition(position)
			}
			if mode == "unknown-field" {
				raw, _ := base64.RawURLEncoding.DecodeString(cursor)
				cursor = base64.RawURLEncoding.EncodeToString(append(append(raw[:len(raw)-1], []byte(`,"url":"https://evil.invalid"`)...), '}'))
			}
			older, err := f.adapter.HistoryPage(context.Background(), account, cursor)
			if mode == "valid" {
				if err != nil || len(older.Entries) != 5 || older.NextCursor == "" {
					t.Fatal("valid native continuation failed", older, err)
				}
			} else if err == nil {
				t.Fatal("changed/cyclic historical continuation accepted", mode)
			}
		})
	}
}

func TestNEARHistoricalArchiveOutageRetainsCheckedRowsAndNeverClaimsComplete(t *testing.T) {
	f := newHistoricalFixture(t, false)
	f.unknown = historyHash(2)
	page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
	if err != nil || len(page.Entries) != 24 || page.Complete || page.NextCursor == "" || !strings.Contains(page.Scope, "archive provider") {
		t.Fatal("archive gap hid checked native rows or became complete empty history", page, err)
	}
	for _, row := range page.Entries {
		if row.Identity == f.unknown {
			t.Fatal("unavailable historical transaction was invented")
		}
	}
	older, err := f.adapter.HistoryPage(context.Background(), f.account, page.NextCursor)
	if err != nil || len(older.Entries) != 5 || !strings.Contains(older.Scope, "scan has unavailable") {
		t.Fatal("archive uncertainty disappeared across continuation", older, err)
	}
}

func TestNEARHistoryActionEqualityRetainsExactLargeIntegers(t *testing.T) {
	left := []json.RawMessage{json.RawMessage(`{"FunctionCall":{"gas":9007199254740992,"deposit":"1"}}`)}
	right := []json.RawMessage{json.RawMessage(`{"FunctionCall":{"deposit":"1","gas":9007199254740993}}`)}
	if sameHistoricalActions(left, right) || !sameHistoricalActions(left, left) {
		t.Fatal("historical action equality rounded public gas quantities")
	}
}

func TestNEARHistoricalFallbackMatchesLaterIndexedObservation(t *testing.T) {
	f, account := tokenHistoryFixture(t, nil, true)
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/txns") && !strings.HasPrefix(r.URL.Path, "/v3/txns/"):
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "/receipts"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"transaction_hash": historyHash(22), "receipt_id": historyHash(32), "predecessor_account_id": account.Address, "receiver_account_id": "receiver.testnet"}}})
		case r.URL.Path == "/v3/txns/"+historyHash(22):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"transaction_hash": historyHash(22), "signer_account_id": account.Address}})
		default:
			t.Errorf("unexpected fallback route %s", r.URL.Path)
		}
	}))
	defer index.Close()
	f.historyEndpoints[account.Network] = index.URL
	first, err := f.HistoryPage(context.Background(), account, "")
	if err != nil || len(first.Entries) != 2 || first.NextCursor == "" {
		t.Fatal("canonical recent fallback failed", first, err)
	}
	second, err := f.HistoryPage(context.Background(), account, first.NextCursor)
	if err != nil || len(second.Entries) != 1 {
		t.Fatal("later independent receipt index failed", second, err)
	}
	for _, row := range first.Entries {
		if row.Identity == second.Entries[0].Identity {
			if !reflect.DeepEqual(row, second.Entries[0]) {
				t.Fatal("recent fallback and indexed observation conflict", row, second.Entries[0])
			}
			return
		}
	}
	t.Fatal("later indexed observation was not present in checked fallback")
}

func TestNEARHistoricalFailedNativeReceiptRemainsAFailedAttempt(t *testing.T) {
	f := newHistoricalFixture(t, false)
	f.mutate = func(method string, result map[string]any) {
		if method == "tx" {
			result["status"] = map[string]any{"Failure": map[string]any{"ActionError": "insufficient balance"}}
			result["receipts_outcome"].([]any)[0].(map[string]any)["outcome"].(map[string]any)["status"] = map[string]any{"Failure": map[string]any{"ActionError": "insufficient balance"}}
		}
	}
	p, err := f.adapter.historyPosition(context.Background(), f.account, "")
	if err != nil {
		t.Fatal(err)
	}
	p.Phase = "receipts"
	candidate := historicalCandidate{Hash: historyHash(1), Receipt: historyHash(101), Predecessor: "sender.testnet", Receiver: f.account.Address}
	observed, err := f.adapter.historicalTransaction(context.Background(), f.account, candidate.Hash, "sender.testnet", candidate, p)
	if err != nil || observed.Row.State != "FAILED" || len(observed.Row.Transfers) != 1 || observed.Row.Transfers[0].Atomic != "10001" {
		t.Fatal("legitimate failed owned receipt hid the history page or became a credited effect", observed, err)
	}
	if len(observed.Receipts) != 1 {
		t.Fatal("failed receipt identity was not independently bound")
	}
}

func TestNEARHistoricalFrozenHeadRejectsLaterBlockWithinSameSecond(t *testing.T) {
	for _, mode := range []string{"height", "nanoseconds", "descendant-height"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoricalFixture(t, false)
			f.mutate = func(method string, value map[string]any) {
				if method == "tx" && mode == "descendant-height" {
					value["receipts_outcome"].([]any)[1].(map[string]any)["block_hash"] = historyHash(249)
				}
				if method != "block" || value["header"].(map[string]any)["hash"] == historyHash(200) {
					return
				}
				header := value["header"].(map[string]any)
				if mode == "descendant-height" && header["hash"] != historyHash(249) {
					return
				}
				header["timestamp_nanosec"] = "1700000000000000000"
				if mode == "height" || mode == "descendant-height" {
					header["height"] = 1001
				} else {
					header["timestamp_nanosec"] = "1700000000000000002"
				}
			}
			if mode == "descendant-height" {
				p, err := f.adapter.historyPosition(context.Background(), f.account, "")
				if err != nil {
					t.Fatal(err)
				}
				p.Phase = "receipts"
				c := historicalCandidate{Hash: historyHash(31), Receipt: historyHash(231), Predecessor: "pool.testnet", Receiver: f.account.Address}
				if _, err := f.adapter.historicalTransaction(context.Background(), f.account, c.Hash, "sender.testnet", c, p); err == nil {
					t.Fatal("receipt executed after frozen head entered the historical scope")
				}
				return
			}
			if _, err := f.adapter.HistoryPage(context.Background(), f.account, ""); err == nil {
				t.Fatal("later same-second execution entered the frozen historical scope")
			}
		})
	}
}

func TestNEARHistoricalPublicMetadataAcceptsIncomingKeySchemesWithoutSigningAuthority(t *testing.T) {
	for _, key := range []struct {
		kind string
		size int
	}{{"ed25519", 32}, {"secp256k1", 64}, {"ml-dsa-65", 1952}} {
		data := make([]byte, key.size)
		data[len(data)-1] = 1
		encoded := key.kind + ":" + base58.Encode(data)
		if !historicalPublicKey(encoded) || historicalPublicKey(key.kind+":"+base58.Encode(data[:len(data)-1])) {
			t.Fatal("incoming historical key metadata size/encoding changed", key.kind)
		}
	}
	for _, invalid := range []string{"", "ed25519", "ed25519:0invalid", "unknown:" + historyHash(1), strings.Repeat("x", 2701)} {
		if historicalPublicKey(invalid) {
			t.Fatal("invalid public historical metadata accepted")
		}
	}
}

func TestNEARHistoricalAccountFundingReadsUnitCreateAccountAndTransferExactlyOnce(t *testing.T) {
	for _, mode := range []string{"incoming", "outgoing", "unknown-unit", "transfer-without-deposit"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoricalFixture(t, false)
			source, destination := "sender.testnet", f.account.Address
			if mode == "incoming" {
				// This historical funding creates a named account whose public
				// current association is independently checked by state().
				var parameters Parameters
				_ = json.Unmarshal(f.account.Parameters, &parameters)
				parameters.Named = true
				f.account.Address = "new-owned.testnet"
				f.account.Parameters, _ = json.Marshal(parameters)
				destination = f.account.Address
			} else if mode == "outgoing" {
				source, destination = f.account.Address, "new-recipient.testnet"
				f.signer = source
			}
			unit := "CreateAccount"
			if mode == "unknown-unit" {
				unit = "UnknownCreateWallet"
			} else if mode == "transfer-without-deposit" {
				unit = "Transfer"
			}
			actions := []any{unit, map[string]any{"Transfer": map[string]any{"deposit": "10001"}}, map[string]any{"AddKey": map[string]any{"public_key": f.account.PublicKey, "access_key": map[string]any{"nonce": 0, "permission": "FullAccess"}}}}
			f.index = func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"transaction_hash": historyHash(1), "signer_account_id": source, "receiver_account_id": destination}}})
			}
			f.mutate = func(method string, value map[string]any) {
				if method == "tx" {
					tx := value["transaction"].(map[string]any)
					tx["signer_id"], tx["receiver_id"], tx["actions"] = source, destination, actions
					value["receipts_outcome"].([]any)[0].(map[string]any)["outcome"].(map[string]any)["executor_id"] = destination
				} else if method == "EXPERIMENTAL_receipt" {
					value["predecessor_id"], value["receiver_id"] = source, destination
					action := value["receipt"].(map[string]any)["Action"].(map[string]any)
					action["signer_id"], action["actions"] = source, actions
				}
			}
			page, err := f.adapter.HistoryPage(context.Background(), f.account, "")
			if mode == "unknown-unit" || mode == "transfer-without-deposit" {
				if err == nil {
					t.Fatal("unknown or amount-free unit action became a valid native funding record")
				}
				return
			}
			if err != nil || len(page.Entries) != 1 || len(page.Entries[0].Transfers) != 1 {
				t.Fatal("valid multi-action account funding was lost or duplicated through its root receipt", page, err)
			}
			effect := page.Entries[0].Transfers[0]
			if effect.ID != "native:transaction:1" || effect.Atomic != "10001" || effect.Source != source || effect.Destination != destination || effect.Asset.ID != "near-testnet:native" || effect.Asset.Decimals != 24 || page.Entries[0].State != "OBSERVED" {
				t.Fatal("multi-action funding changed native amount, peers, precision or root identity", effect)
			}
		})
	}
}

func TestNEARHistoricalDelayedFTBindingRetainsCheckedMixedPage(t *testing.T) {
	for _, mode := range []string{"untracked-pending", "tracked-pending", "malformed-hash", "untracked-malformed-hash", "foreign-pending", "invalid-receipt-pending"} {
		t.Run(mode, func(t *testing.T) {
			f := newHistoricalFixture(t, true)
			f.index = func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v3/txns/") {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"transaction_hash": strings.TrimPrefix(r.URL.Path, "/v3/txns/"), "signer_account_id": "sender.testnet"}})
					return
				}
				if r.URL.Query().Get("next") != "" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
					return
				}
				pending := f.candidate(42, "ft-txns")
				delete(pending, "transaction_hash")
				switch mode {
				case "untracked-pending", "untracked-malformed-hash":
					pending["contract_account_id"] = "untracked.testnet"
				case "foreign-pending":
					pending["affected_account_id"] = "foreign.testnet"
				case "invalid-receipt-pending":
					pending["receipt_id"] = "invalid"
				}
				if mode == "malformed-hash" || mode == "untracked-malformed-hash" {
					pending["transaction_hash"] = "not-a-NEAR-transaction-hash"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{f.candidate(41, "ft-txns"), pending, f.candidate(43, "ft-txns")}, "meta": map[string]any{"next_page": base64.StdEncoding.EncodeToString([]byte("older FT records"))}})
			}
			position, err := f.adapter.historyPosition(context.Background(), f.account, "")
			if err != nil {
				t.Fatal(err)
			}
			position.Phase, position.Pinned, position.Tokens = "ft-txns", true, 1
			cursor, _ := encodeHistoricalPosition(position)
			page, err := f.adapter.HistoryPage(context.Background(), f.account, cursor)
			if mode != "untracked-pending" && mode != "tracked-pending" {
				if err == nil {
					t.Fatal("malformed/foreign FT candidate was silently treated as pending")
				}
				return
			}
			if err != nil || len(page.Entries) != 2 || page.NextCursor == "" || page.Complete {
				t.Fatal("pending FT binding hid healthy historical effects or lost continuation", page, err)
			}
			for _, row := range page.Entries {
				if row.Identity != historyHash(41) && row.Identity != historyHash(43) || len(row.Transfers) != 1 || row.Transfers[0].Asset != tokenPolicy("token.testnet", 6).Asset {
					t.Fatal("pending candidate established an unverified effect", row)
				}
			}
			next, err := f.adapter.historyPosition(context.Background(), f.account, page.NextCursor)
			if err != nil || next.Partial != (mode == "tracked-pending") || strings.Contains(page.Scope, "Some indexed transaction bindings") != (mode == "tracked-pending") {
				t.Fatal("pending tracked/untracked history uncertainty was misstated", next, page.Scope, err)
			}
			terminal, err := f.adapter.HistoryPage(context.Background(), f.account, page.NextCursor)
			if err != nil || terminal.Complete || terminal.NextCursor != "" || mode == "tracked-pending" && !strings.Contains(terminal.Scope, "scan has unavailable") {
				t.Fatal("delayed FT uncertainty disappeared when an index was exhausted", terminal, err)
			}
		})
	}
}

func TestNEARHistoricalRequiredNativeIndexerHashesDoNotBecomeOptional(t *testing.T) {
	for _, phase := range []string{"txns", "receipts"} {
		for _, mode := range []string{"missing", "malformed"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				f := newHistoricalFixture(t, false)
				f.index = func(w http.ResponseWriter, r *http.Request) {
					candidate := f.candidate(1, phase)
					if mode == "missing" {
						delete(candidate, "transaction_hash")
					} else {
						candidate["transaction_hash"] = "bad"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{candidate}})
				}
				position, err := f.adapter.historyPosition(context.Background(), f.account, "")
				if err != nil {
					t.Fatal(err)
				}
				position.Phase, position.Pinned = phase, true
				cursor, _ := encodeHistoricalPosition(position)
				if _, err := f.adapter.HistoryPage(context.Background(), f.account, cursor); err == nil {
					t.Fatal("required native indexer transaction identity became a successful empty history")
				}
			})
		}
	}
}

func TestNEARHistoricalArchiveSelectionNeverBypassesCustomRPC(t *testing.T) {
	for _, test := range []struct{ network, configured, archive string }{
		{"near-mainnet", "https://rpc.mainnet.near.org", "https://archival-rpc.mainnet.fastnear.com"},
		{"near-testnet", "https://rpc.testnet.near.org/", "https://archival-rpc.testnet.fastnear.com"},
		{"near-mainnet", "https://rpc.mainnet.fastnear.com", "https://archival-rpc.mainnet.fastnear.com"},
		{"near-testnet", "https://rpc.testnet.fastnear.com", "https://archival-rpc.testnet.fastnear.com"},
		{"near-mainnet", "https://private.example/rpc", ""},
		{"near-mainnet", "http://127.0.0.1:3030", ""},
		{"near-localnet", "http://127.0.0.1:3030", ""},
		{"near-mainnet", "https://rpc.mainnet.near.org.evil.example", ""},
		{"near-mainnet", "https://rpc.mainnet.near.org/path", ""},
		{"near-mainnet", "https://rpc.mainnet.near.org?secret=fixture", ""},
	} {
		if actual := historyArchiveEndpoint(test.network, test.configured); actual != test.archive {
			t.Fatal("unexpected read-only archive selection", test.network, test.configured, actual)
		}
	}
}

// Explicitly opt in to read-only public provider acceptance. The fixed account,
// transaction and method are public historical data, never local user storage,
// keys or funds. Ordinary unit/CI/local sandbox suites make no external calls.
func TestNEARHistoricalPublicArchiveReadOnly(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_NEAR_PUBLIC_HISTORY") != "1" {
		t.Skip("explicit read-only public archive acceptance was not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a := New()
	if err := a.SetEndpoint("near-mainnet", "https://archival-rpc.mainnet.fastnear.com"); err != nil {
		t.Fatal(err)
	}
	if err := a.identity(ctx, "near-mainnet"); err != nil {
		t.Fatal(err)
	}
	var head historyBlock
	if err := a.rpc(ctx, "near-mainnet", "block", map[string]string{"finality": "final"}, &head); err != nil {
		t.Fatal(err)
	}
	nanos, err := strconv.ParseUint(head.Header.Timestamp.String(), 10, 64)
	if err != nil || !blockHash(head.Header.Hash) {
		t.Fatal("invalid public final head", err)
	}
	position := historicalPosition{Head: head.Header.Hash, Height: head.Header.Height.String(), Before: strconv.FormatUint(nanos+1, 10), Phase: "txns"}
	const signer = "fd8e54adec2753c7b1ca0beb330d15b01a6c53af9ef6d7ef5d2a9170528b9713"
	const hash = "Dhx53pGrHTC6LCd3wF2LbtfeZQsF4S5tnkMEsJT16eJH"
	// Deliberately no seed, profile or signing key: this is a public historical
	// parser/provider check, not an owned-account recovery or funded acceptance.
	account := model.Account{Family: "near", Network: "near-mainnet", Address: signer}
	candidate := historicalCandidate{Hash: hash, Signer: signer, Receiver: "wrap.near"}
	observed, err := a.historicalTransaction(ctx, account, hash, signer, candidate, position)
	if err != nil || observed.Row.State != "OBSERVED" || observed.Row.Timestamp != 1672530800 || len(observed.Row.Transfers) == 0 {
		t.Fatal("public 2022 historical execution did not pass production interpretation", observed.Row, err)
	}
	for _, effect := range observed.Row.Transfers {
		if effect.Source == signer && effect.Destination == "wrap.near" && effect.Atomic == "1" && effect.Comment == "Attached deposit: ft_transfer_call" {
			t.Logf("checked public archive transaction %s, timestamp %d, %d complete receipt payloads", hash, observed.Row.Timestamp, len(observed.Receipts))
			return
		}
	}
	t.Fatal("public exact native attached-deposit effect was not independently interpreted", observed.Row)
}
