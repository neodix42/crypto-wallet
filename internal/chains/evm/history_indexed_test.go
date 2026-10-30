package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

type indexedHistoryFixture struct {
	f             *nodeFixture
	account       model.Account
	index         func(*http.Request) (string, int)
	tx            func(string) any
	receipt       func(string) any
	block         func(string) any
	transfers     func(map[string]any) any
	indexRequests int
}

func indexedID(i int) string { return fmt.Sprintf("0x%064x", i) }

func newIndexedHistory(t *testing.T, network string) *indexedHistoryFixture {
	t.Helper()
	f := fakeNode(t, network)
	account, _ := f.a.Derive(syntheticSeed(), 0, network)
	h := &indexedHistoryFixture{f: f, account: account}
	owner := common.BytesToHash(common.HexToAddress(account.Address).Bytes()).Hex()
	peer := "0x1111111111111111111111111111111111111111"
	n, _ := f.a.network(network)
	h.index = func(r *http.Request) (string, int) {
		if r.Method != "GET" || r.Body != nil || r.Header.Get("Authorization") != "" || r.URL.Query().Get("address") != strings.ToLower(account.Address) || r.URL.Query().Get("startblock") != "0" || r.URL.Query().Get("endblock") != "4096" || r.URL.Query().Get("offset") != "20" || r.URL.Query().Get("sort") != "desc" {
			t.Error("candidate query changed public address, snapshot, bounds or authority", r.URL)
		}
		return `{"status":"1","message":"OK","result":[{"hash":"` + indexedID(1) + `","value":"999999999999","tokenDecimal":"77","to":"UNTRUSTED"}]}`, 200
	}
	h.tx = func(id string) any {
		return map[string]any{"hash": id, "from": account.Address, "to": peer, "value": "0x1", "input": "0x", "blockHash": indexedID(500), "blockNumber": "0x1"}
	}
	h.receipt = func(id string) any {
		logs := []any{}
		if len(n.Assets) > 1 {
			logs = append(logs, map[string]any{"address": n.Assets[1].Contract, "topics": []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), owner, common.BytesToHash(common.HexToAddress(peer).Bytes()).Hex()}, "data": "0x" + hex.EncodeToString(word(big.NewInt(800000))), "transactionHash": id, "blockHash": indexedID(500), "blockNumber": "0x1", "logIndex": "0x5", "removed": false})
		}
		return map[string]any{"transactionHash": id, "blockHash": indexedID(500), "blockNumber": "0x1", "status": "0x1", "logs": logs}
	}
	h.block = func(tag string) any {
		if tag == "latest" || tag == "finalized" || tag == "0x1000" {
			return map[string]any{"number": "0x1000", "hash": indexedID(600), "timestamp": quantity(big.NewInt(time.Now().Unix())), "baseFeePerGas": "0x1"}
		}
		ids := []string{}
		for i := 1; i <= 30; i++ {
			ids = append(ids, indexedID(i))
		}
		return map[string]any{"number": "0x1", "hash": indexedID(500), "timestamp": "0x6553f100", "transactions": ids}
	}
	f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		switch method {
		case "eth_getBlockByNumber":
			var tag string
			_ = json.Unmarshal(args[0], &tag)
			return h.block(tag), true
		case "eth_getTransactionByHash":
			var id string
			_ = json.Unmarshal(args[0], &id)
			return h.tx(id), true
		case "eth_getTransactionReceipt":
			var id string
			_ = json.Unmarshal(args[0], &id)
			return h.receipt(id), true
		case "alchemy_getAssetTransfers":
			var query map[string]any
			_ = json.Unmarshal(args[0], &query)
			if h.transfers != nil {
				return h.transfers(query), true
			}
			return nil, true
		}
		return nil, false
	}
	local, _ := url.Parse(f.server.URL)
	f.a.endpoints[network] = "https://history-rpc.invalid"
	f.a.HTTPClient.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "history-rpc.invalid" {
			copy := r.Clone(r.Context())
			u := *r.URL
			u.Scheme, u.Host = local.Scheme, local.Host
			copy.URL = &u
			return http.DefaultTransport.RoundTrip(copy)
		}
		h.indexRequests++
		body, status := h.index(r)
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return h
}

func TestIndexedEthereumAndBaseRecoverOldNativeAndTrackedTokenEffects(t *testing.T) {
	for _, network := range []string{"ethereum-mainnet", "base-mainnet", "arbitrum-mainnet"} {
		t.Run(network, func(t *testing.T) {
			h := newIndexedHistory(t, network)
			page, err := h.f.a.HistoryPage(context.Background(), h.account, "")
			if err != nil || len(page.Entries) != 1 || len(page.Entries[0].Transfers) != 2 || page.NextCursor != "" || page.Complete {
				t.Fatal("older indexed native/token history lost", page, err)
			}
			row := page.Entries[0]
			if row.Timestamp != 1700000000 || row.State != "CONFIRMED" || row.Transfers[0].Atomic != "1" || row.Transfers[1].Atomic != "800000" || row.Transfers[1].Asset.Decimals != map[string]uint8{"ethereum-mainnet": 6, "base-mainnet": 18, "arbitrum-mainnet": 18}[network] || h.f.calls["eth_getTransactionReceipt"] != 1 {
				t.Fatal("index metadata trusted or duplicate candidate not merged", row, h.f.calls)
			}
			if h.f.sent != "" || len(h.f.a.tokens) != 0 || h.f.calls["eth_getLogs"] != 0 {
				t.Fatal("history expanded authority or scanned recent blocks")
			}
		})
	}
}

func TestIndexedHistoryPagesFreezeHeadAndDecodeAllEffectsBeforeCrossStreamDuplicates(t *testing.T) {
	h := newIndexedHistory(t, "base-mainnet")
	h.index = func(r *http.Request) (string, int) {
		items := []map[string]string{}
		if r.URL.Query().Get("action") == "txlist" && r.URL.Query().Get("page") == "1" {
			for i := 1; i <= 20; i++ {
				items = append(items, map[string]string{"hash": indexedID(i)})
			}
		} else {
			items = append(items, map[string]string{"hash": indexedID(1)})
		}
		body, _ := json.Marshal(map[string]any{"status": "1", "message": "OK", "result": items})
		return string(body), 200
	}
	first, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(first.Entries) != 20 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := h.f.a.HistoryPage(context.Background(), h.account, first.NextCursor)
	if err != nil || len(second.Entries) != 1 || second.NextCursor != "" || !reflect.DeepEqual(first.Entries[0], second.Entries[0]) {
		t.Fatal("same full receipt changed across streams/pages", first, second, err)
	}
	var cursor indexedHistoryCursor
	_ = json.Unmarshal([]byte(first.NextCursor), &cursor)
	for _, change := range []func(*indexedHistoryCursor){func(c *indexedHistoryCursor) { c.Owner = "foreign" }, func(c *indexedHistoryCursor) { c.Network = "ethereum-mainnet" }, func(c *indexedHistoryCursor) { c.Hash = indexedID(999) }, func(c *indexedHistoryCursor) { c.NativePage = 501 }, func(c *indexedHistoryCursor) { c.Final = "0x1001" }, func(c *indexedHistoryCursor) { c.FromKey = "forged" }} {
		bad := cursor
		change(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := h.f.a.HistoryPage(context.Background(), h.account, string(raw)); err == nil {
			t.Fatal("invalid continuation accepted", bad)
		}
	}
}

func TestIndexedHistoryRejectsForgedCandidateRPCBindingsAndMalformedEffects(t *testing.T) {
	for _, mutation := range []string{"transaction", "block", "inclusion", "receipt", "status", "time", "event", "amount", "duplicate-log", "foreign-event"} {
		t.Run(mutation, func(t *testing.T) {
			h := newIndexedHistory(t, "base-mainnet")
			tx, block, receipt := h.tx, h.block, h.receipt
			h.tx = func(id string) any {
				v := tx(id).(map[string]any)
				if mutation == "transaction" {
					v["hash"] = indexedID(999)
				}
				return v
			}
			h.block = func(tag string) any {
				v := block(tag).(map[string]any)
				if tag == "0x1" {
					if mutation == "block" {
						v["hash"] = indexedID(999)
					}
					if mutation == "inclusion" {
						v["transactions"] = []string{}
					}
					if mutation == "time" {
						v["timestamp"] = "0xffffffffffffffff"
					}
				}
				return v
			}
			h.receipt = func(id string) any {
				v := receipt(id).(map[string]any)
				if mutation == "receipt" {
					v["blockNumber"] = "0x2"
				}
				if mutation == "status" {
					v["status"] = "0x0"
				}
				logs := v["logs"].([]any)
				log := logs[0].(map[string]any)
				if mutation == "event" {
					log["transactionHash"] = indexedID(999)
				}
				if mutation == "amount" {
					log["data"] = "0x1234"
				}
				if mutation == "duplicate-log" {
					v["logs"] = append(logs, log)
				}
				if mutation == "foreign-event" {
					log["topics"] = []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), common.BytesToHash(common.HexToAddress(h.account.Address).Bytes()).Hex(), "0x" + strings.Repeat("f", 64)}
				}
				return v
			}
			if _, err := h.f.a.HistoryPage(context.Background(), h.account, ""); err == nil {
				t.Fatal("forged indexed history accepted", mutation)
			}
		})
	}
}

func TestUnavailableIndexedHistoryNeverBecomesSuccessfulEmptyRecentWindow(t *testing.T) {
	h := newIndexedHistory(t, "base-mainnet")
	h.index = func(*http.Request) (string, int) { return "PRIVATE-PROVIDER-ERROR", 403 }
	h.f.rpcError = "alchemy_getAssetTransfers"
	page, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err == nil || len(page.Entries) != 0 || page.Complete || !strings.Contains(err.Error(), "Alchemy-compatible") || strings.Contains(err.Error(), "PRIVATE") || h.f.calls["eth_getLogs"] != 0 {
		t.Fatal("unavailable index disguised as empty history", page, err)
	}
	h.index = func(*http.Request) (string, int) { return `{"status":"1","message":"OK","result":[]}`, 200 }
	page, err = h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(page.Entries) != 0 || page.Complete || page.NextCursor != "" {
		t.Fatal("valid exhausted empty index failed", page, err)
	}
}

func TestBaseConfiguredRPCTransferIndexFallbackKeepsExactBoundsAndOpaquePagination(t *testing.T) {
	h := newIndexedHistory(t, "base-mainnet")
	h.index = func(*http.Request) (string, int) { return "", 403 }
	calls := 0
	h.transfers = func(q map[string]any) any {
		calls++
		if q["toBlock"] != "0x1000" || q["fromBlock"] != "0x0" || q["maxCount"] != "0x14" || q["order"] != "desc" || q["withMetadata"] != false || q["excludeZeroValue"] != false {
			t.Error("RPC history bounds changed", q)
		}
		if q["fromAddress"] == h.account.Address {
			return map[string]any{"transfers": []any{map[string]string{"hash": indexedID(1)}}, "pageKey": ""}
		}
		if q["toAddress"] != h.account.Address {
			t.Error("foreign RPC account", q)
		}
		key := "next-owned-page"
		if q["pageKey"] == key {
			key = ""
		}
		return map[string]any{"transfers": []any{map[string]string{"hash": indexedID(1)}}, "pageKey": key}
	}
	first, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(first.Entries) != 1 || first.NextCursor == "" || calls != 2 {
		t.Fatal(first, err, calls)
	}
	second, err := h.f.a.HistoryPage(context.Background(), h.account, first.NextCursor)
	if err != nil || len(second.Entries) != 1 || second.NextCursor != "" || calls != 3 || h.indexRequests != 1 || !reflect.DeepEqual(first.Entries, second.Entries) {
		t.Fatal("RPC fallback continuation restarted or lost effects", second, err, calls)
	}
	h.transfers = func(q map[string]any) any { return map[string]any{"transfers": []any{}, "pageKey": "next-owned-page"} }
	if _, err = h.f.a.HistoryPage(context.Background(), h.account, first.NextCursor); err == nil {
		t.Fatal("non-progressing RPC continuation accepted")
	}
}

func TestIndexedHistoryUsesEveryImportedContractAndKeepsHealthyAssetsWhenOnePolicyFails(t *testing.T) {
	h := newIndexedHistory(t, "base-mainnet")
	first := importedTokenFixture()
	first.Network = "base-mainnet"
	first.Asset.ID = first.Network + ":erc20:" + first.Asset.Contract
	second := first
	second.Asset.Contract = common.HexToAddress("0x2234567890123456789012345678901234567890").Hex()
	second.Asset.ID = second.Network + ":erc20:" + second.Asset.Contract
	second.Asset.Decimals = 18
	second.Asset.Symbol = "SECOND"
	adapter, err := h.f.a.WithTokens([]model.ERC20Token{first, second})
	if err != nil {
		t.Fatal(err)
	}
	h.f.a = adapter
	original := h.f.intercept
	failed := false
	h.f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		if method == "eth_getCode" {
			var contract string
			_ = json.Unmarshal(args[0], &contract)
			if contract == first.Asset.Contract || contract == second.Asset.Contract {
				if failed && contract == first.Asset.Contract {
					return "0x", true
				}
				return "0x6001", true
			}
		}
		if method == "eth_call" {
			var call map[string]string
			_ = json.Unmarshal(args[0], &call)
			if call["data"] == callData("decimals()") && (call["to"] == first.Asset.Contract || call["to"] == second.Asset.Contract) {
				precision := first.Asset.Decimals
				if call["to"] == second.Asset.Contract {
					precision = second.Asset.Decimals
				}
				return "0x" + hex.EncodeToString(word(big.NewInt(int64(precision)))), true
			}
		}
		return original(method, args)
	}
	receipt := h.receipt
	h.receipt = func(id string) any {
		v := receipt(id).(map[string]any)
		logs := v["logs"].([]any)
		base := logs[0].(map[string]any)
		for i, asset := range []model.Asset{first.Asset, second.Asset} {
			log := map[string]any{}
			for k, v := range base {
				log[k] = v
			}
			log["address"], log["logIndex"] = asset.Contract, quantity(big.NewInt(int64(i+6)))
			logs = append(logs, log)
		}
		v["logs"] = logs
		return v
	}
	page, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(page.Entries) != 1 || len(page.Entries[0].Transfers) != 4 {
		t.Fatal("imported histories collapsed", page, err)
	}
	if page.Entries[0].Transfers[2].Asset != first.Asset || page.Entries[0].Transfers[3].Asset != second.Asset {
		t.Fatal("imported contract precision changed", page)
	}
	failed = true
	page, err = h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(page.Entries) != 1 || len(page.Entries[0].Transfers) != 3 || !strings.Contains(page.Scope, first.Asset.ID) || page.Entries[0].Transfers[2].Asset != second.Asset {
		t.Fatal("unavailable policy hid healthy native/token history", page, err)
	}
}

func TestIndexedHistoryCandidateSchemaTransportAndBoundsFailClosed(t *testing.T) {
	for _, body := range []string{
		`{"status":"1","status":"1","message":"OK","result":[]}`,
		`{"status":"1","message":"OK","result":null}`,
		`{"status":"1","message":"OK","result":[{"hash":"bad"}]}`,
		`{"status":"1","message":"OK","result":[{"hash":"` + indexedID(1) + `","hash":"` + indexedID(2) + `"}]}`,
		`{"status":"2","message":"OK","result":[]}`,
		strings.Repeat(" ", 2<<20+1),
	} {
		h := newIndexedHistory(t, "base-mainnet")
		h.index = func(*http.Request) (string, int) { return body, 200 }
		if _, err := h.f.a.HistoryPage(context.Background(), h.account, ""); err == nil {
			t.Fatal("malformed candidate schema accepted")
		}
		if h.f.calls["eth_getTransactionByHash"] != 0 || h.f.sent != "" {
			t.Fatal("invalid index acquired additional authority")
		}
	}
	h := newIndexedHistory(t, "base-mainnet")
	h.index = func(*http.Request) (string, int) {
		return `{"status":"0","message":"NOTOK","result":"rate limited"}`, 200
	}
	h.f.rpcError = "alchemy_getAssetTransfers"
	if _, err := h.f.a.HistoryPage(context.Background(), h.account, ""); err == nil || strings.Contains(err.Error(), "rate limited") {
		t.Fatal("quota denial or provider content trusted", err)
	}
	h = newIndexedHistory(t, "base-mainnet")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.f.a.HistoryPage(ctx, h.account, ""); err == nil {
		t.Fatal("canceled history continued")
	}
}
