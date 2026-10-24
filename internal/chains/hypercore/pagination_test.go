package hypercore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"localwallet/internal/model"
	"strings"
	syncatomic "sync/atomic"
	"testing"
	"time"
)

func observedLedger(count int, start uint64, owner, recipient string) []ledger {
	rows := make([]ledger, count)
	for i := range rows {
		u := &rows[i]
		u.Time = start + uint64(i/3)
		u.Hash = fmt.Sprintf("0x%064x", i+1)
		u.Delta.Type = "spotTransfer"
		u.Delta.Token = "HYPE"
		u.Delta.Amount = json.RawMessage(`"0.12345678"`)
		u.Delta.User = owner
		u.Delta.Destination = recipient
		u.Delta.Fee = json.RawMessage(`"0"`)
		u.Delta.NativeTokenFee = json.RawMessage(`"0"`)
	}
	return rows
}

func serveLedgerPages(f *coreNode, rows *[]ledger, requests *syncatomic.Uint32) {
	f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
		if typ != "userNonFundingLedgerUpdates" {
			return nil, false
		}
		if requests != nil {
			requests.Add(1)
		}
		var start, end uint64
		_ = json.Unmarshal(req["startTime"], &start)
		_ = json.Unmarshal(req["endTime"], &end)
		out := []ledger{}
		for _, u := range *rows {
			if u.Time >= start && u.Time <= end {
				out = append(out, u)
				if len(out) == ledgerResponseLimit {
					break
				}
			}
		}
		return out, true
	}
}

func TestTP050CorePagedHistoryKeepsTimestampTiesAndWindow(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	at := uint64(time.Now().UnixMilli())
	f.override["exchangeStatus"] = map[string]uint64{"time": at}
	rows := observedLedger(1205, at-10_000, ac.Address, "0x1111111111111111111111111111111111111111")
	var requests syncatomic.Uint32
	serveLedgerPages(f, &rows, &requests)
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	for {
		page, e := f.a.HistoryPage(context.Background(), ac, cursor)
		if e != nil || len(page.Entries) > 128 || len(page.NextCursor) > 1024 || !strings.Contains(page.Scope, "provider window only") {
			t.Fatal("bounded history page", len(page.Entries), e)
		}
		pages++
		for _, row := range page.Entries {
			if seen[row.Identity] || row.State != "OBSERVED" {
				t.Fatal("duplicate or invented finality", row.Identity)
			}
			seen[row.Identity] = true
		}
		if page.Complete {
			if page.NextCursor != "" {
				t.Fatal("complete page has continuation")
			}
			break
		}
		if page.NextCursor == "" || pages > 20 {
			t.Fatal("missing continuation or unbounded scan")
		}
		cursor = page.NextCursor
		// A subsequent server observation must not move the original scan end.
		f.override["exchangeStatus"] = map[string]uint64{"time": at + 1000}
	}
	if len(seen) != 1205 || pages < 10 || requests.Load() != uint32(pages) {
		t.Fatal("timestamp pagination lost or repeated events", len(seen), pages, requests.Load())
	}
	all, e := f.a.History(context.Background(), ac)
	if e != nil || len(all) != len(seen) {
		t.Fatal("legacy bounded snapshot", len(all), e)
	}
}

func TestTP041CoreHistoryCursorBindingAndProviderChanges(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	at := uint64(time.Now().UnixMilli())
	f.override["exchangeStatus"] = map[string]uint64{"time": at}
	rows := observedLedger(200, at-1000, ac.Address, "0x1111111111111111111111111111111111111111")
	serveLedgerPages(f, &rows, nil)
	first, e := f.a.HistoryPage(context.Background(), ac, "")
	if e != nil || len(first.Entries) != 128 || first.Complete {
		t.Fatal(first, e)
	}
	good, e := decodeHistoryCursor(first.NextCursor, ac, at)
	if e != nil || good.End != at || good.Offset != 128 {
		t.Fatal(good, e)
	}
	other, _ := f.a.Derive(syntheticSeed(), 1, "hypercore-testnet")
	if _, e = f.a.HistoryPage(context.Background(), other, first.NextCursor); e == nil {
		t.Fatal("cursor crossed account descriptor")
	}
	for _, raw := range []string{"!", strings.Repeat("a", 1025), base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"v":1}`)), base64.RawURLEncoding.EncodeToString([]byte(`{"unexpected":true}`))} {
		if _, e = f.a.HistoryPage(context.Background(), ac, raw); e == nil {
			t.Fatal("malformed cursor")
		}
	}
	for _, mutate := range []func(*historyCursor){
		func(c *historyCursor) { c.Version++ },
		func(c *historyCursor) { c.Account = "foreign" },
		func(c *historyCursor) { c.End = 0 },
		func(c *historyCursor) { c.End = at + 30_001 },
		func(c *historyCursor) { c.Next = historyStart(c.End) - 1 },
		func(c *historyCursor) { c.Next = c.End + 1 },
		func(c *historyCursor) { c.Offset = -1 },
		func(c *historyCursor) { c.Offset = 500 },
		func(c *historyCursor) { c.Offset = 400 },
		func(c *historyCursor) { c.Page = historyPageBudget },
		func(c *historyCursor) { c.Hash = "bad" },
		func(c *historyCursor) { c.Hash = "" },
		func(c *historyCursor) { c.Offset = 0 },
	} {
		bad := good
		mutate(&bad)
		if _, e = f.a.HistoryPage(context.Background(), ac, encodeHistoryCursor(bad)); e == nil {
			t.Fatal("unsafe history cursor accepted", bad)
		}
	}
	rows[0].Delta.Amount = json.RawMessage(`"0.5"`)
	if _, e = f.a.HistoryPage(context.Background(), ac, first.NextCursor); e == nil {
		t.Fatal("provider changed committed page without detection")
	}
	good.Offset, good.Hash, good.Page = 0, "", historyPageBudget-1
	if _, e = f.a.HistoryPage(context.Background(), ac, encodeHistoryCursor(good)); e == nil || !strings.Contains(e.Error(), "page budget exhausted") {
		t.Fatal("history page budget silently truncated", e)
	}
	if _, e = decodeHistoryCursor("", ac, at); e == nil || historyStart(1) != 0 {
		t.Fatal("empty cursor or short-window handling")
	}
}

func TestTP052CoreLegacySnapshotReportsItsBound(t *testing.T) {
	f := fakeCore(t)
	ac, _ := f.a.Derive(syntheticSeed(), 0, "hypercore-testnet")
	at := uint64(time.Now().UnixMilli())
	f.override["exchangeStatus"] = map[string]uint64{"time": at}
	rows := observedLedger(5000, at-10_000, ac.Address, "0x1111111111111111111111111111111111111111")
	serveLedgerPages(f, &rows, nil)
	if out, e := f.a.History(context.Background(), ac); e == nil || out != nil || !strings.Contains(e.Error(), "use paginated history") {
		t.Fatal("legacy history silently truncated", len(out), e)
	}
}

func FuzzTP012CoreHistoryCursor(f *testing.F) {
	ac := model.Account{ID: "public-test-account", Network: "hypercore-mainnet", Address: "0x1111111111111111111111111111111111111111"}
	const at = uint64(1792733000000)
	good := historyCursor{Version: 1, Account: accountCommitment(ac), End: at, Next: historyStart(at)}
	f.Add(encodeHistoryCursor(good))
	f.Add("")
	f.Add("!")
	f.Add(base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"v":1}`)))
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 2048 {
			return
		}
		c, e := decodeHistoryCursor(raw, ac, at)
		if e == nil && (encodeHistoryCursor(c) != raw || c.Account != accountCommitment(ac) || c.Next > c.End || c.Next < historyStart(c.End) || c.End > at+30_000 || c.Page >= historyPageBudget || c.Offset < 0 || c.Offset >= ledgerResponseLimit) {
			t.Fatal("accepted cursor escaped its account/window/resource bounds")
		}
	})
}

func TestTP052CoreLedgerPaginationRejectsUnsafeRangesAndSaturation(t *testing.T) {
	f := fakeCore(t)
	n, _ := network("hypercore-testnet")
	at := uint64(time.Now().UnixMilli())
	ac, _ := f.a.Derive(syntheticSeed(), 0, n.ID)
	base := observedLedger(3, at-1000, ac.Address, "0x1111111111111111111111111111111111111111")
	if _, e := f.a.ledgerPage(context.Background(), n, ac.Address, at, at-1); e == nil {
		t.Fatal("inverted range")
	}
	for _, mutate := range []func([]ledger){
		func(rows []ledger) { rows[0].Time = 0 },
		func(rows []ledger) { rows[0].Time = at - 2000 },
		func(rows []ledger) { rows[0].Time = at + 1 },
		func(rows []ledger) { rows[0].Hash = "bad" },
		func(rows []ledger) { rows[0].Delta.Type = "" },
		func(rows []ledger) { rows[1] = rows[0] },
		func(rows []ledger) { rows[0].Time = at - 500; rows[1].Time = at - 600 },
	} {
		rows := append([]ledger(nil), base...)
		mutate(rows)
		f.override["userNonFundingLedgerUpdates"] = rows
		if _, e := f.a.ledgerPage(context.Background(), n, ac.Address, at-1000, at); e == nil {
			t.Fatal("unsafe event page")
		}
	}
	rows := observedLedger(500, at-1000, ac.Address, "0x1111111111111111111111111111111111111111")
	for i := range rows {
		rows[i].Time = at - 1000
	}
	f.override["userNonFundingLedgerUpdates"] = rows
	if _, e := f.a.ledgerWindow(context.Background(), n, ac.Address, at-1000, at); e == nil || !strings.Contains(e.Error(), "timestamp saturated") {
		t.Fatal("unpageable timestamp silently skipped", e)
	}
	delete(f.override, "userNonFundingLedgerUpdates")
	var requests syncatomic.Uint32
	f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
		if typ != "userNonFundingLedgerUpdates" {
			return nil, false
		}
		requests.Add(1)
		var start uint64
		_ = json.Unmarshal(req["startTime"], &start)
		return observedLedger(500, start+1, ac.Address, "0x1111111111111111111111111111111111111111"), true
	}
	if _, e := f.a.ledgerWindow(context.Background(), n, ac.Address, 1, at); e == nil || !strings.Contains(e.Error(), "request budget") || requests.Load() != 32 {
		t.Fatal("unbounded ledger scan", requests.Load(), e)
	}
}

func TestTP060CoreReconcileTraversesBothLedgersAndRejectsExtraEffects(t *testing.T) {
	f := fakeCore(t)
	p, e := f.prepare(t)
	if e != nil {
		t.Fatal(e)
	}
	s, e := f.a.Sign(syntheticSeed(), p)
	if e != nil {
		t.Fatal(e)
	}
	var env envelope
	_ = json.Unmarshal(s.Bytes, &env)
	f.action, f.owner = env.Action, p.Account.Address
	at := uint64(time.Now().UnixMilli())
	f.override["exchangeStatus"] = map[string]uint64{"time": at}
	rows := observedLedger(1100, at-10_000, f.owner, p.Recipient)
	exact := observedLedger(1, env.Nonce, f.owner, p.Recipient)[0]
	exact.Hash = f.hash
	rows = append(rows, exact)
	var requests syncatomic.Uint32
	serveLedgerPages(f, &rows, &requests)
	r, e := f.a.Reconcile(context.Background(), p, s)
	if e != nil || r.State != "CONFIRMED" || requests.Load() != 6 {
		t.Fatal("saturated sender and recipient ledgers did not reconcile", r, requests.Load(), e)
	}
	for _, mutate := range []func(*ledger){
		func(u *ledger) { u.Delta.Type = "activationFee" },
		func(u *ledger) { u.Delta.Token = "USDC" },
		func(u *ledger) { u.Delta.Amount = json.RawMessage(`"1"`) },
		func(u *ledger) { u.Delta.Fee = json.RawMessage(`"1"`) },
		func(u *ledger) { u.Delta.NativeTokenFee = json.RawMessage(`"1"`) },
		func(u *ledger) { u.Delta.Destination = f.owner },
	} {
		extra := exact
		mutate(&extra)
		rows = []ledger{exact, extra}
		r, e = f.a.Reconcile(context.Background(), p, s)
		if e != nil || r.State != "UNKNOWN" {
			t.Fatal("same-hash unapproved effect incorrectly confirmed", r, e)
		}
	}
	rows = []ledger{exact, exact}
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("duplicate exact effect confirmed", r, e)
	}
	rows = []ledger{exact}
	wrongTime := uint64(1)
	f.override["userDetails"] = map[string]any{"type": "userDetails", "txs": []any{map[string]any{"time": wrongTime, "user": f.owner, "action": f.action, "block": 123, "hash": f.hash, "error": nil}}}
	f.override["txDetails"] = map[string]any{"type": "txDetails", "tx": map[string]any{"time": wrongTime, "user": f.owner, "action": f.action, "block": 123, "hash": f.hash, "error": nil}}
	r, e = f.a.Reconcile(context.Background(), p, s)
	if e == nil || r.State != "UNKNOWN" {
		t.Fatal("inclusion outside verified window", r, e)
	}
}

var _ model.HistoryPager = (*Adapter)(nil)
