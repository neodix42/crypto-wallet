package walletcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

type pagedHistoryAdapter struct {
	*publicAdapter
	page  func(string) (model.HistoryPage, error)
	calls []string
}

func (a *pagedHistoryAdapter) HistoryPage(_ context.Context, _ model.Account, cursor string) (model.HistoryPage, error) {
	a.calls = append(a.calls, cursor)
	return a.page(cursor)
}
func fakeHistoryCoordinator(t *testing.T, adapter *pagedHistoryAdapter) (*core, string) {
	c, path := fakeCoordinator(t, adapter.publicAdapter, nil)
	c.adapters = []model.Adapter{adapter}
	return c, path
}
func historyRows(n int, detail string) []model.Receipt {
	rows := make([]model.Receipt, n)
	for i := range rows {
		rows[i] = model.Receipt{Identity: fmt.Sprintf("synthetic-public-history-%06d", i), State: "OBSERVED", Detail: detail}
	}
	return rows
}
func getHistory(t *testing.T, c *core, cursor string) (model.HistoryPage, error) {
	t.Helper()
	body := map[string]string{"accountId": publicAccount().ID}
	if cursor != "" {
		body["cursor"] = cursor
	}
	out, err := c.handle(message("chainHistory", body))
	if err != nil {
		return model.HistoryPage{}, err
	}
	page, ok := out.(model.HistoryPage)
	if !ok {
		t.Fatalf("unexpected page type %T", out)
	}
	return page, nil
}
func TestHistoryPagesPreserveExactRowsAndBoundEveryIPCFrame(t *testing.T) {
	rows := historyRows(420, strings.Repeat("public detail", 300))
	a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(cursor string) (model.HistoryPage, error) {
		if cursor != "" {
			t.Fatal("snapshot unnecessarily fetched again")
		}
		return model.HistoryPage{Entries: rows, Complete: true, Scope: "Synthetic complete provider window, no consensus proof"}, nil
	}}
	c, _ := fakeHistoryCoordinator(t, a)
	var collected []model.Receipt
	cursor := ""
	pages := 0
	for {
		page, err := getHistory(t, c, cursor)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := publicJSON(page)
		if err != nil || len(raw) > historyPageBytes || len(page.Entries) > historyPageEntries {
			t.Fatal("unbounded page", len(raw), err)
		}
		var wire bytes.Buffer
		channel := ipc.Channel{W: &wire}
		if err = channel.Write(ipc.Message{Version: 1, ID: "history", Session: "public-test", Method: "chainHistory", Body: raw}); err != nil || wire.Len() > ipc.MaxFrame {
			t.Fatal("page cannot cross actual framed IPC", err, wire.Len())
		}
		collected = append(collected, page.Entries...)
		pages++
		if page.NextCursor == "" {
			if !page.Complete {
				t.Fatal("complete snapshot lost terminal status")
			}
			break
		}
		if page.Complete || !lowerHex(page.NextCursor) || len(page.NextCursor) != 32 {
			t.Fatal("invalid public continuation")
		}
		previous := cursor
		cursor = page.NextCursor
		if previous != "" {
			if _, err = getHistory(t, c, previous); err == nil {
				t.Fatal("replayed continuation accepted")
			}
		}
	}
	if pages < 5 || len(a.calls) != 1 || len(collected) != len(rows) {
		t.Fatal("pagination lost or duplicated rows", pages, len(a.calls), len(collected))
	}
	for i := range rows {
		if rows[i] != collected[i] {
			t.Fatal("changed/out-of-order snapshot", i)
		}
	}
	if len(c.historyScans) != 0 {
		t.Fatal("terminal scan retained")
	}
}
func TestHistoryContinuationBindsAccountSessionEpochAndExpiry(t *testing.T) {
	for _, scenario := range []string{"malformed", "unknown", "session", "account", "expired", "refresh", "lock"} {
		t.Run(scenario, func(t *testing.T) {
			a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(string) (model.HistoryPage, error) {
				return model.HistoryPage{Entries: historyRows(129, "public"), Scope: "bounded"}, nil
			}}
			c, path := fakeHistoryCoordinator(t, a)
			page, err := getHistory(t, c, "")
			if err != nil {
				t.Fatal(err)
			}
			cursor := page.NextCursor
			request := message("chainHistory", map[string]string{"accountId": publicAccount().ID, "cursor": cursor})
			switch scenario {
			case "malformed":
				request.Body = []byte(`{"accountId":"a","cursor":"https://evil.invalid/private"}`)
			case "unknown":
				request.Body = []byte(fmt.Sprintf(`{"accountId":%q,"cursor":%q}`, publicAccount().ID, strings.Repeat("a", 32)))
			case "session":
				request.Session = "other-session"
			case "account":
				state := readPublicState(t, path)
				ac := publicAccount()
				ac.Address = "changed-descriptor"
				state["accounts"] = []model.Account{ac}
				writePublicState(t, path, state)
			case "expired":
				c.historyScans[cursor].expires = time.Now().Add(-time.Second)
			case "refresh":
				if _, err = getHistory(t, c, ""); err != nil {
					t.Fatal(err)
				}
			case "lock":
				c.lock()
				if len(c.historyScans) != 0 {
					t.Fatal("lock retained history")
				}
			}
			if _, err = c.handle(request); err == nil {
				t.Fatal("invalid continuation accepted")
			}
		})
	}
}
func TestHistoryAdapterPagesRejectDuplicateNonprogressAndResourceAbuse(t *testing.T) {
	cases := map[string]model.HistoryPage{
		"duplicate":             {Entries: []model.Receipt{{Identity: "same"}, {Identity: "same"}}, Scope: "bounded"},
		"empty-identity":        {Entries: []model.Receipt{{}}, Scope: "bounded"},
		"long-identity":         {Entries: []model.Receipt{{Identity: strings.Repeat("x", 513)}}, Scope: "bounded"},
		"invalid-identity-utf8": {Entries: []model.Receipt{{Identity: string([]byte{255})}}, Scope: "bounded"},
		"large-record":          {Entries: historyRows(1, strings.Repeat("x", historyPageBytes)), Scope: "bounded"},
		"large-snapshot":        {Entries: historyRows(1, strings.Repeat("x", historyRetainedBytes)), Scope: "bounded"},
		"too-many":              {Entries: historyRows(historyScanEntries+1, ""), Scope: "bounded"},
		"long-cursor":           {NextCursor: strings.Repeat("x", 4097), Scope: "bounded"},
		"invalid-cursor":        {NextCursor: string([]byte{255}), Scope: "bounded"},
		"long-scope":            {Scope: strings.Repeat("x", 16385)},
		"invalid-scope":         {Scope: string([]byte{255})},
		"empty-scope":           {},
		"contradictory":         {NextCursor: "next", Complete: true, Scope: "bounded"},
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(string) (model.HistoryPage, error) { return page, nil }}
			c, _ := fakeHistoryCoordinator(t, a)
			if _, err := getHistory(t, c, ""); err == nil {
				t.Fatal("invalid page accepted")
			}
			if len(c.historyScans) != 0 {
				t.Fatal("invalid page retained")
			}
		})
	}
	for _, mode := range []string{"duplicate-across-pages", "nonprogress", "network-error"} {
		t.Run(mode, func(t *testing.T) {
			a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(cursor string) (model.HistoryPage, error) {
				if cursor == "" {
					return model.HistoryPage{Entries: historyRows(1, ""), NextCursor: "position-1", Scope: "bounded"}, nil
				}
				if mode == "network-error" {
					return model.HistoryPage{}, errors.New("synthetic outage")
				}
				if mode == "nonprogress" {
					return model.HistoryPage{NextCursor: cursor, Scope: "bounded"}, nil
				}
				return model.HistoryPage{Entries: historyRows(1, ""), Scope: "bounded"}, nil
			}}
			c, _ := fakeHistoryCoordinator(t, a)
			page, err := getHistory(t, c, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = getHistory(t, c, page.NextCursor); err == nil {
				t.Fatal("invalid continuation page accepted")
			}
			if _, err = getHistory(t, c, page.NextCursor); err == nil {
				t.Fatal("failed continuation remained reusable")
			}
		})
	}
}
func TestHistoryAdapterContinuationAndUnknownEmptyWindow(t *testing.T) {
	a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(cursor string) (model.HistoryPage, error) {
		if cursor == "" {
			return model.HistoryPage{Entries: []model.Receipt{{Identity: "latest"}}, NextCursor: "private-adapter-position", Scope: "synthetic linked chain"}, nil
		}
		if cursor != "private-adapter-position" {
			t.Fatal("untrusted browser cursor reached adapter", cursor)
		}
		return model.HistoryPage{Entries: []model.Receipt{{Identity: "oldest"}}, Complete: true, Scope: "synthetic linked chain"}, nil
	}}
	c, _ := fakeHistoryCoordinator(t, a)
	first, err := getHistory(t, c, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(first)
	if bytes.Contains(raw, []byte("private-adapter-position")) {
		t.Fatal("adapter position leaked")
	}
	second, err := getHistory(t, c, first.NextCursor)
	if err != nil || !second.Complete || second.NextCursor != "" || len(second.Entries) != 1 || second.Entries[0].Identity != "oldest" {
		t.Fatal(second, err)
	}
	a.page = func(string) (model.HistoryPage, error) {
		return model.HistoryPage{Scope: "Empty bounded provider observation; not proof of no funds"}, nil
	}
	empty, err := getHistory(t, c, "")
	if err != nil || empty.Complete || empty.NextCursor != "" {
		t.Fatal("empty query fabricated completion", empty, err)
	}
}

func TestHistoryScansEnforceSharedMemoryAndContextBudgets(t *testing.T) {
	for _, mode := range []string{"contexts", "shared-bytes", "scan-bytes", "lock-during-query"} {
		t.Run(mode, func(t *testing.T) {
			a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}}
			c, _ := fakeHistoryCoordinator(t, a)
			a.page = func(string) (model.HistoryPage, error) {
				if mode == "lock-during-query" {
					c.lock()
				}
				if mode == "scan-bytes" {
					rows := historyRows(8000, "")
					for i := range rows {
						rows[i].Identity += strings.Repeat("x", 300)
					}
					return model.HistoryPage{Entries: rows, NextCursor: "next", Scope: "bounded"}, nil
				}
				return model.HistoryPage{Entries: historyRows(129, ""), Scope: "bounded"}, nil
			}
			c.historyScans = map[string]*historyScan{}
			if mode == "contexts" {
				for i := 0; i < historyContexts; i++ {
					c.historyScans[fmt.Sprint(i)] = &historyScan{expires: time.Now().Add(time.Minute), account: "other-account", bytes: 1}
				}
			}
			if mode == "shared-bytes" {
				c.historyScans["other"] = &historyScan{expires: time.Now().Add(time.Minute), account: "other-account", bytes: historyAllRetainedBytes}
			}
			if _, err := getHistory(t, c, ""); err == nil {
				t.Fatal("unbounded or stale history retained")
			}
		})
	}
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	if _, err := getHistory(t, c, strings.Repeat("g", 32)); err == nil {
		t.Fatal("nonhex capability accepted")
	}
	if _, err := historyAdapterPage(context.Background(), &publicAdapter{}, publicAccount(), "untrusted-private-cursor"); err == nil {
		t.Fatal("legacy adapter accepted continuation")
	}
}
