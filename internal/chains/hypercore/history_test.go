package hypercore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTP050BoundedCoreHistory(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	f.owner = ac.Address
	f.action = action{Destination: "0x1111111111111111111111111111111111111111", Amount: "0.12345678", Time: uint64(time.Now().UnixMilli())}
	f.ledger = true
	rows, e := f.a.History(context.Background(), ac)
	if e != nil || len(rows) != 1 || rows[0].State != "OBSERVED" || !strings.Contains(rows[0].Detail, "sent 12345678") {
		t.Fatal(rows, e)
	}
	if !strings.Contains(f.a.HistoryScope(), "30 days") {
		t.Fatal("window hidden")
	}
	f.owner = "0x2222222222222222222222222222222222222222"
	f.action.Destination = ac.Address
	rows, e = f.a.History(context.Background(), ac)
	if e != nil || len(rows) != 1 || !strings.Contains(rows[0].Detail, "received") {
		t.Fatal("system funding history", rows, e)
	}
	f.ledger = false
	rows, e = f.a.History(context.Background(), ac)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
}
func TestTP052CoreHistoryRejectsUnreliableEvents(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	f.owner = ac.Address
	f.action = action{Destination: "0x1111111111111111111111111111111111111111", Amount: "0.12345678", Time: uint64(time.Now().UnixMilli())}
	f.ledger = true
	original := func() map[string]any {
		return map[string]any{"time": time.Now().UnixMilli(), "hash": f.hash, "delta": map[string]any{"type": "spotTransfer", "token": testToken, "amount": "0.12345678", "user": ac.Address, "destination": f.action.Destination, "fee": "0", "nativeTokenFee": "0"}}
	}
	bad := ac
	bad.ID = "foreign"
	if _, e := f.a.History(context.Background(), bad); e == nil {
		t.Fatal("invalid history descriptor")
	}
	f.fail = "spotMeta"
	if _, e := f.a.History(context.Background(), ac); e == nil {
		t.Fatal("unverified history ledger")
	}
	f.fail = "userNonFundingLedgerUpdates"
	if _, e := f.a.History(context.Background(), ac); e == nil {
		t.Fatal("missing history")
	}
	f.fail = ""
	for _, events := range []any{nil, make([]any, 501)} {
		f.override["userNonFundingLedgerUpdates"] = events
		if _, e := f.a.History(context.Background(), ac); e == nil {
			t.Fatal("missing/oversized history")
		}
	}
	for _, mutate := range []func(map[string]any){func(x map[string]any) { x["hash"] = "bad" }, func(x map[string]any) { x["time"] = 1 }, func(x map[string]any) { x["time"] = time.Now().Add(time.Hour).UnixMilli() }, func(x map[string]any) { x["delta"].(map[string]any)["user"] = f.action.Destination }, func(x map[string]any) { x["delta"].(map[string]any)["user"] = "bad" }, func(x map[string]any) { x["delta"].(map[string]any)["destination"] = "bad" }, func(x map[string]any) { x["delta"].(map[string]any)["amount"] = "1e99" }, func(x map[string]any) { x["delta"].(map[string]any)["fee"] = "bad" }, func(x map[string]any) { x["delta"].(map[string]any)["nativeTokenFee"] = "bad" }} {
		x := original()
		mutate(x)
		f.override["userNonFundingLedgerUpdates"] = []any{x}
		if _, e := f.a.History(context.Background(), ac); e == nil {
			t.Fatal("hostile history event")
		}
	}
	f.override["userNonFundingLedgerUpdates"] = []any{original(), original()}
	if _, e := f.a.History(context.Background(), ac); e == nil {
		t.Fatal("duplicate identity")
	}
	for _, field := range []string{"type", "token"} {
		x := original()
		x["delta"].(map[string]any)[field] = "other"
		f.override["userNonFundingLedgerUpdates"] = []any{x}
		rows, e := f.a.History(context.Background(), ac)
		if e != nil || len(rows) != 0 {
			t.Fatal("unregistered history asset/action", rows, e)
		}
	}
	x := original()
	x["delta"].(map[string]any)["token"] = "HYPE"
	delete(x["delta"].(map[string]any), "nativeTokenFee")
	f.override["userNonFundingLedgerUpdates"] = []any{x}
	rows, e := f.a.History(context.Background(), ac)
	if e != nil || len(rows) != 1 || !strings.Contains(rows[0].Detail, "native HYPE fee not provided") {
		t.Fatal(rows, e)
	}
}
