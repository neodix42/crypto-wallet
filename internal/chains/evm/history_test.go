package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"math/big"
	"strings"
	"testing"
	"time"
)

type historyFixture struct {
	f        *nodeFixture
	ac       model.Account
	full     any
	logs     any
	receipt  any
	status   string
	final    any
	txs      []historyTx
	logCalls int
	hash     string
}

func newHistory(t *testing.T, net string) *historyFixture {
	t.Helper()
	f := fakeNode(t, net)
	ac, _ := f.a.Derive(syntheticSeed(), 0, net)
	h := &historyFixture{f: f, ac: ac, status: "0x1", hash: "0x" + strings.Repeat("a", 64)}
	id := "0x" + strings.Repeat("b", 64)
	to := "0x1111111111111111111111111111111111111111"
	h.txs = []historyTx{{Hash: id, From: ac.Address, To: &to, Value: "0x1", Input: "0x"}}
	h.receipt = map[string]any{"transactionHash": id, "blockHash": h.hash, "blockNumber": "0x1", "status": "0x1"}
	n, _ := network(net)
	if len(n.Assets) > 1 {
		owner := common.BytesToHash(common.HexToAddress(ac.Address).Bytes()).Hex()
		h.logs = []any{map[string]any{"address": n.Assets[1].Contract, "topics": []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), owner, common.BytesToHash(common.HexToAddress(to).Bytes()).Hex()}, "data": "0x" + hex.EncodeToString(word(big.NewInt(2))), "transactionHash": id, "blockHash": h.hash, "blockNumber": "0x1", "logIndex": "0x0", "removed": false}}
	}
	f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		switch method {
		case "eth_getBlockByNumber":
			if string(args[0]) == `"finalized"` && h.final != nil {
				return h.final, true
			}
			if string(args[1]) == "true" {
				if h.full != nil {
					return h.full, true
				}
				var tag string
				_ = json.Unmarshal(args[0], &tag)
				b := historyBlock{Number: tag, Hash: h.hash, Transactions: []historyTx{}}
				if tag == "0x1" {
					b.Transactions = h.txs
				}
				return b, true
			}
		case "eth_getTransactionReceipt":
			return h.receipt, true
		case "eth_getLogs":
			h.logCalls++
			return h.logs, true
		}
		return nil, false
	}
	return h
}
func (h *historyFixture) run() ([]model.Receipt, error) {
	return h.f.a.History(context.Background(), h.ac)
}
func TestTP050BoundedEVMHistory(t *testing.T) {
	for _, net := range []string{"ethereum-sepolia", "base-sepolia"} {
		h := newHistory(t, net)
		rows, e := h.run()
		expected := 1
		if net == "base-sepolia" {
			expected = 2
		}
		if e != nil || len(rows) != expected {
			t.Fatal(rows, e)
		}
		for _, r := range rows {
			if r.State != "CONFIRMED" || !strings.Contains(r.Detail, "blocks 0..16") {
				t.Fatal("history scope/finality", r)
			}
		}
		if net == "base-sepolia" && h.logCalls != 2 {
			t.Fatal("both incoming/outgoing token topics required")
		}
		if !strings.Contains(h.f.a.HistoryScope(), "128") {
			t.Fatal("history limits hidden")
		}
	}
	h := newHistory(t, "ethereum-sepolia")
	to := h.ac.Address
	h.txs[0].To = &to
	h.txs[0].From = "0x1111111111111111111111111111111111111111"
	h.final = map[string]any{"number": "0x0"}
	rows, e := h.run()
	if e != nil || rows[0].State != "SUBMITTED" || !strings.Contains(rows[0].Detail, "received") {
		t.Fatal(rows, e)
	}
	h.receipt.(map[string]any)["status"] = "0x0"
	rows, e = h.run()
	if e != nil || rows[0].State != "SUBMITTED" || !strings.Contains(rows[0].Detail, "reverted") {
		t.Fatal(rows, e)
	}
	h.final = map[string]string{"number": "0x10"}
	rows, e = h.run()
	if e != nil || rows[0].State != "FAILED" {
		t.Fatal(rows, e)
	}
	h = newHistory(t, "ethereum-sepolia")
	h.f.override["eth_getBlockByNumber"] = map[string]string{"number": "0x100", "hash": h.hash, "timestamp": quantity(big.NewInt(timeNowUnix())), "baseFeePerGas": "0x1"}
	h.txs = nil
	rows, e = h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func timeNowUnix() int64 { return time.Now().Unix() }
func TestTP052HistoryRejectsHostileBlocksAndReceipts(t *testing.T) {
	h := newHistory(t, "ethereum-sepolia")
	h.ac.ID = "bad"
	if _, e := h.run(); e == nil {
		t.Fatal("invalid history account")
	}
	h = newHistory(t, "ethereum-sepolia")
	h.f.rpcError = "eth_chainId"
	if _, e := h.run(); e == nil {
		t.Fatal("wrong network history")
	}
	h = newHistory(t, "ethereum-sepolia")
	h.final = map[string]string{"number": "0xz"}
	if _, e := h.run(); e == nil {
		t.Fatal("invalid finality")
	}
	for _, full := range []any{historyBlock{Number: "0x0", Hash: "bad"}, historyBlock{Number: "0x2", Hash: "0x" + strings.Repeat("a", 64)}} {
		h := newHistory(t, "ethereum-sepolia")
		h.full = full
		if _, e := h.run(); e == nil {
			t.Fatal("malformed full block")
		}
	}
	for _, mutate := range []func(*historyTx){func(tx *historyTx) { tx.Hash = "bad" }, func(tx *historyTx) { tx.From = "bad"; tx.To = &h.ac.Address }, func(tx *historyTx) { tx.To = new(string); *tx.To = "bad" }, func(tx *historyTx) { tx.Value = "0xz" }} {
		h := newHistory(t, "ethereum-sepolia")
		mutate(&h.txs[0])
		if _, e := h.run(); e == nil {
			t.Fatal("malformed native transaction")
		}
	}
	for _, receipt := range []any{nil, map[string]string{"transactionHash": "bad"}, map[string]string{"transactionHash": "0x" + strings.Repeat("b", 64), "blockHash": "0x" + strings.Repeat("a", 64), "blockNumber": "0x1", "status": "0x2"}} {
		h := newHistory(t, "ethereum-sepolia")
		h.receipt = receipt
		if _, e := h.run(); e == nil {
			t.Fatal("malformed history receipt")
		}
	}
	h = newHistory(t, "ethereum-sepolia")
	h.txs[0].To = nil
	rows, e := h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	h = newHistory(t, "ethereum-sepolia")
	h.txs[0].Value = "0x0"
	rows, e = h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	h = newHistory(t, "ethereum-sepolia")
	h.txs[0].From = "0x3333333333333333333333333333333333333333"
	rows, e = h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestTP052HistoryRejectsHostileTokenEvents(t *testing.T) {
	for _, mutate := range []func(map[string]any){func(x map[string]any) { x["address"] = "0x1111111111111111111111111111111111111111" }, func(x map[string]any) { x["topics"] = []string{} }, func(x map[string]any) { x["logIndex"] = "0xz" }, func(x map[string]any) { x["data"] = "0x01" }, func(x map[string]any) { x["blockHash"] = "0x" + strings.Repeat("c", 64) }, func(x map[string]any) { x["blockNumber"] = "0x99" }, func(x map[string]any) { x["transactionHash"] = "bad" }, func(x map[string]any) {
		topics := x["topics"].([]string)
		topics[2] = "0x" + strings.Repeat("1", 64)
		x["topics"] = topics
	}} {
		h := newHistory(t, "base-sepolia")
		h.txs = nil
		mutate(h.logs.([]any)[0].(map[string]any))
		if _, e := h.run(); e == nil {
			t.Fatal("malformed token history")
		}
	}
	h := newHistory(t, "base-sepolia")
	h.f.rpcError = "eth_getLogs"
	if _, e := h.run(); e == nil {
		t.Fatal("missing token logs")
	}
	h = newHistory(t, "base-sepolia")
	h.f.rpcError = "eth_getCode"
	if _, e := h.run(); e == nil {
		t.Fatal("wrong token code")
	}
	h = newHistory(t, "base-sepolia")
	h.receipt.(map[string]any)["status"] = "0x0"
	if _, e := h.run(); e == nil {
		t.Fatal("contradictory reverted token receipt")
	}
	h = newHistory(t, "base-sepolia")
	h.txs = nil
	h.logs.([]any)[0].(map[string]any)["removed"] = true
	rows, e := h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	h = newHistory(t, "base-sepolia")
	h.txs = nil
	h.logs.([]any)[0].(map[string]any)["data"] = "0x" + strings.Repeat("0", 64)
	rows, e = h.run()
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e = historyAddress("bad"); e == nil {
		t.Fatal("bad indexed address")
	}
	h = newHistory(t, "ethereum-sepolia")
	h.txs = append(h.txs, h.txs[0])
	if _, e = h.run(); e == nil {
		t.Fatal("duplicate native payment")
	}
	h = newHistory(t, "base-sepolia")
	original := h.f.intercept
	h.f.intercept = func(method string, args []json.RawMessage) (any, bool) {
		if method == "eth_getLogs" && h.logCalls == 1 {
			h.logs.([]any)[0].(map[string]any)["data"] = "0x" + hex.EncodeToString(word(big.NewInt(3)))
		}
		return original(method, args)
	}
	if _, e = h.run(); e == nil {
		t.Fatal("contradictory duplicate token event")
	}
}
