package evm

import (
	"context"
	"math/big"
	"strings"
	"testing"
)

func TestIndexedHistoryRejectsUnboundOrExcessiveReceiptData(t *testing.T) {
	for _, fault := range []string{"future-transaction", "invalid-source", "invalid-destination", "noncanonical-value", "duplicate-block-member", "invalid-topic-count", "invalid-source-padding", "excessive-effects", "transaction-rpc", "block-rpc", "receipt-rpc"} {
		t.Run(fault, func(t *testing.T) {
			h := newIndexedHistory(t, "arbitrum-mainnet")
			originalTx, originalBlock, originalReceipt := h.tx, h.block, h.receipt
			h.tx = func(id string) any {
				tx := originalTx(id).(map[string]any)
				switch fault {
				case "future-transaction":
					tx["blockNumber"] = "0x1001"
				case "invalid-source":
					tx["from"] = "provider.invalid/address"
				case "invalid-destination":
					tx["to"] = "not-an-address"
				case "noncanonical-value":
					tx["value"] = "0x01"
				}
				return tx
			}
			h.block = func(tag string) any {
				block := originalBlock(tag).(map[string]any)
				if tag == "0x1" && fault == "duplicate-block-member" {
					block["transactions"] = []string{indexedID(1), indexedID(1)}
				}
				return block
			}
			h.receipt = func(id string) any {
				receipt := originalReceipt(id).(map[string]any)
				logs := receipt["logs"].([]any)
				log := logs[0].(map[string]any)
				if fault == "invalid-topic-count" {
					log["topics"] = log["topics"].([]string)[:2]
				}
				if fault == "invalid-source-padding" {
					topics := log["topics"].([]string)
					topics[2] = "0x" + strings.Repeat("f", 64)
				}
				if fault == "excessive-effects" {
					logs = make([]any, 1001)
					for i := range logs {
						copy := map[string]any{}
						for key, value := range log {
							copy[key] = value
						}
						copy["logIndex"] = quantity(big.NewInt(int64(i)))
						logs[i] = copy
					}
					receipt["logs"] = logs
				}
				return receipt
			}
			for kind, method := range map[string]string{"transaction-rpc": "eth_getTransactionByHash", "block-rpc": "eth_getBlockByNumber", "receipt-rpc": "eth_getTransactionReceipt"} {
				if fault == kind {
					h.f.rpcError = method
				}
			}
			page, err := h.f.a.HistoryPage(context.Background(), h.account, "")
			if err == nil || len(page.Entries) != 0 || page.Complete || h.f.sent != "" {
				t.Fatal("hostile receipt became observed history", fault, page, err)
			}
		})
	}
}

func TestIndexedHistoryFiltersUnownedCandidateAndPreservesOwnedZeroValueAttempt(t *testing.T) {
	h := newIndexedHistory(t, "arbitrum-mainnet")
	original := h.tx
	h.receipt = func(id string) any {
		return map[string]any{"transactionHash": id, "blockHash": indexedID(500), "blockNumber": "0x1", "status": "0x1", "logs": []any{}}
	}
	h.tx = func(id string) any { tx := original(id).(map[string]any); tx["value"] = "0x0"; return tx }
	rows, err := h.f.a.History(context.Background(), h.account)
	if err != nil || len(rows) != 1 || len(rows[0].Transfers) != 1 || rows[0].Transfers[0].Atomic != "0" {
		t.Fatal("owned zero-value call invented funds or disappeared", rows, err)
	}
	h.tx = func(id string) any {
		tx := original(id).(map[string]any)
		tx["from"] = "0x3333333333333333333333333333333333333333"
		return tx
	}
	rows, err = h.f.a.History(context.Background(), h.account)
	if err != nil || len(rows) != 0 {
		t.Fatal("index candidate without checked owned effects became wallet activity", rows, err)
	}
}

func TestAddressIndexContinuationAuthorityAndAvailabilityBounds(t *testing.T) {
	h := newIndexedHistory(t, "arbitrum-sepolia")
	n, _ := h.f.a.network(h.account.Network)
	if !h.f.a.HistoryPagesOverlap() {
		t.Fatal("reviewed complete-receipt stream overlap not exposed")
	}
	for _, key := range []string{strings.Repeat("k", 513), "provider\nkey", "key\x00", "space key"} {
		if _, _, err := h.f.a.historyRPCCandidates(context.Background(), n, h.account, "0x1000", key, true); err == nil {
			t.Fatal("unbounded or control-bearing RPC continuation accepted")
		}
	}
	for _, hash := range []string{"bad", strings.ToUpper(indexedID(1)), indexedID(1) + "/../x"} {
		if hash == indexedID(1) {
			continue
		}
		h.transfers = func(map[string]any) any { return map[string]any{"transfers": []any{map[string]string{"hash": hash}}} }
		if _, _, err := h.f.a.historyRPCCandidates(context.Background(), n, h.account, "0x1000", "", true); err == nil {
			t.Fatal("provider controlled identity accepted", hash)
		}
	}
	for _, position := range []struct {
		head string
		page int
	}{{"0x01", 1}, {"0x1", 0}, {"0x1", 501}} {
		if _, _, err := h.f.a.historyCandidates(context.Background(), historyIndexEndpoints(n.ID)[0], h.account, position.head, position.page, false); err == nil {
			t.Fatal("invalid index query bounds accepted")
		}
	}
	bad := h.account
	bad.PublicKey = "invalid"
	if _, err := h.f.a.HistoryPage(context.Background(), bad, ""); err == nil {
		t.Fatal("unowned descriptor queried")
	}
	first, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(first.Entries) != 1 {
		t.Fatal(first, err)
	}
}
