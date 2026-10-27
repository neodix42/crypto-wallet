package walletcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestPublicAccountPagesAndLateTypedLookupNeverTransportWholeArchive(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	accounts := make([]model.Account, 1000)
	for i := range accounts {
		accounts[i] = publicAccount()
		accounts[i].ID = fmt.Sprintf("public-account-%04d", i)
		accounts[i].Index = uint32(i)
	}
	state["accounts"] = accounts
	writePublicState(t, path, state)
	cursor := ""
	seen := map[string]bool{}
	count := 0
	for {
		out, err := c.handle(message("accounts", map[string]string{"cursor": cursor}))
		if err != nil {
			t.Fatal(err)
		}
		page := out.(snapshot)
		raw, _ := publicJSON(page)
		if len(raw) > historyPageBytes || len(page.Accounts) > 128 || page.TotalAccounts != 1000 || len(page.Operations) != 0 {
			t.Fatal("unbounded public account page")
		}
		for _, account := range page.Accounts {
			if seen[account.ID] {
				t.Fatal("duplicate account")
			}
			seen[account.ID] = true
			count++
		}
		previous := cursor
		cursor = page.AccountsCursor
		if previous != "" {
			if _, err = c.handle(message("accounts", map[string]string{"cursor": previous})); err == nil {
				t.Fatal("account continuation replay")
			}
		}
		if cursor == "" {
			break
		}
		if len(cursor) != 32 || !lowerHex(cursor) {
			t.Fatal("private account position exposed")
		}
	}
	if count != 1000 {
		t.Fatal("large account portfolio truncated", count)
	}
	account, err := c.account("public-account-0999")
	if err != nil || account.Index != 999 {
		t.Fatal("late typed account lookup", account, err)
	}
	if _, err = c.account("missing"); err == nil {
		t.Fatal("unknown late account accepted")
	}
}
func TestPublicJournalPagesPreserveScopedRecordsAndKeepSignedArtifactsPrivate(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	operations := make([]journal.Operation, 400)
	for i := range operations {
		p := publicPlan()
		p.ID = fmt.Sprintf("public-payment-%04d", i)
		operations[i] = journal.Operation{Plan: p, State: "UNKNOWN", Signed: &model.Signed{Identity: p.ID, Bytes: []byte("PUBLIC-SYNTHETIC-ARCHIVED-BYTES")}}
	}
	state["ops"] = operations
	writePublicState(t, path, state)
	body := map[string]string{"accountId": publicAccount().ID, "asset": publicPlan().Asset}
	cursor := ""
	seen := map[string]bool{}
	for {
		body["cursor"] = cursor
		out, err := c.handle(message("history", body))
		if err != nil {
			t.Fatal(err)
		}
		page := out.(journalPage)
		raw, _ := publicJSON(page)
		if len(raw) > historyPageBytes || bytes.Contains(raw, []byte("ARCHIVED-BYTES")) || page.Total != 400 {
			t.Fatal("journal transport violation")
		}
		for _, operation := range page.Operations {
			if operation.Signed != nil || seen[operation.Plan.ID] {
				t.Fatal("artifact leaked or duplicate")
			}
			seen[operation.Plan.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			if !page.Complete {
				t.Fatal("journal never completed")
			}
			break
		}
	}
	if len(seen) != 400 {
		t.Fatal("journal truncated", len(seen))
	}
	// A narrow operation lookup still retrieves and reconciles the original bytes.
	if _, err := c.payment("reconcile", operations[399].Plan.ID); err != nil {
		t.Fatal("late original operation unavailable", err)
	}
	state = readPublicState(t, path)
	calls := fmt.Sprint(state["calls"])
	if strings.Contains(calls, "pending") || !strings.Contains(calls, "operation") {
		t.Fatal("payment still requested a whole signed journal", calls)
	}
}
func TestPublicContinuationsRejectCrossQuerySessionGenerationExpiryAndLock(t *testing.T) {
	for _, scenario := range []string{"kind", "filter", "session", "revision", "expiry", "refresh", "lock"} {
		t.Run(scenario, func(t *testing.T) {
			c, path := fakeCoordinator(t, &publicAdapter{}, nil)
			state := readPublicState(t, path)
			operations := make([]journal.Operation, 129)
			for i := range operations {
				p := publicPlan()
				p.ID = fmt.Sprint(i)
				operations[i] = journal.Operation{Plan: p, State: "PREPARED"}
			}
			state["ops"] = operations
			writePublicState(t, path, state)
			body := map[string]string{"accountId": publicAccount().ID, "asset": publicPlan().Asset}
			out, err := c.handle(message("history", body))
			if err != nil {
				t.Fatal(err)
			}
			cursor := out.(journalPage).NextCursor
			body["cursor"] = cursor
			request := message("history", body)
			switch scenario {
			case "kind":
				request = message("accounts", map[string]string{"cursor": cursor})
			case "filter":
				request = message("history", map[string]string{"accountId": publicAccount().ID, "asset": "other:asset", "cursor": cursor})
			case "session":
				request.Session = "other-session"
			case "revision":
				state = readPublicState(t, path)
				state["revision"] = 2
				writePublicState(t, path, state)
			case "expiry":
				scan := c.publicScans[cursor]
				scan.expires = time.Now().Add(-time.Second)
				c.publicScans[cursor] = scan
			case "refresh":
				delete(body, "cursor")
				if _, err = c.handle(message("history", body)); err != nil {
					t.Fatal(err)
				}
			case "lock":
				c.lock()
				if len(c.publicScans) != 0 {
					t.Fatal("lock retained public cursor")
				}
			}
			if _, err = c.handle(request); err == nil {
				t.Fatal("stale/mismatched cursor accepted")
			}
		})
	}
}
func TestPublicJournalSchemaAndForbiddenArtifactFailures(t *testing.T) {
	for _, override := range []map[string]any{
		{"total": -1}, {"total": 10001}, {"total": 0, "operations": []journal.Operation{{Plan: publicPlan()}}}, {"complete": false, "nextCursor": ""}, {"complete": true, "nextCursor": "position"},
		{"operations": []journal.Operation{{Plan: publicPlan(), Signed: &model.Signed{Bytes: []byte{1}}}}},
		{"operations": []journal.Operation{{Plan: model.Plan{}}}},
		{"operations": []journal.Operation{{Plan: publicPlan()}, {Plan: publicPlan()}}, "total": 2},
		{"operations": []journal.Operation{{Plan: func() model.Plan { p := publicPlan(); p.Asset = "foreign"; return p }()}}},
		{"operations": []journal.Operation{{Plan: func() model.Plan { p := publicPlan(); p.Memo = strings.Repeat("x", historyPageBytes); return p }()}}, "total": 1},
	} {
		t.Run(fmt.Sprint(len(override)), func(t *testing.T) {
			c, path := fakeCoordinator(t, &publicAdapter{}, nil)
			state := readPublicState(t, path)
			state["journalOverride"] = override
			writePublicState(t, path, state)
			if _, err := c.handle(message("history", map[string]string{"accountId": publicAccount().ID, "asset": publicPlan().Asset})); err == nil {
				t.Fatal("invalid journal response accepted")
			}
		})
	}
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	for _, body := range []map[string]string{{"accountId": "only-account"}, {"asset": "only-asset"}, {"accountId": "missing", "asset": "asset"}, {"cursor": "https://evil.invalid"}} {
		if _, err := c.handle(message("history", body)); err == nil {
			t.Fatal("invalid journal query accepted")
		}
	}
	if _, err := c.retainPublicPage(c.epoch, "s", "kind", "query", "position", ""); err == nil {
		t.Fatal("missing revision accepted")
	}
	if _, err := c.retainPublicPage(c.epoch+1, "s", "kind", "query", "position", "1"); err == nil {
		t.Fatal("stale epoch retained")
	}
	c.publicScans = map[string]publicContinuation{"expired": {expires: time.Now().Add(-time.Second)}}
	for i := 0; i < 64; i++ {
		if _, err := c.retainPublicPage(c.epoch, "s", "kind", fmt.Sprint(i), "position", "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.retainPublicPage(c.epoch, "s", "kind", "overflow", "position", "1"); err == nil {
		t.Fatal("public cursor bound ignored")
	}
	raw, _ := json.Marshal(c.publicScans)
	if bytes.Contains(raw, []byte("PUBLIC-SYNTHETIC-ARCHIVED-BYTES")) {
		t.Fatal("cursor cache retained signed artifacts")
	}
}

func TestPublicAccountGenerationAndOutwardMutationPositionsFailClosed(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	accounts := make([]model.Account, 129)
	for i := range accounts {
		accounts[i] = publicAccount()
		accounts[i].ID = fmt.Sprint(i)
	}
	state["accounts"] = accounts
	writePublicState(t, path, state)
	out, err := c.handle(message("accounts", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	cursor := out.(snapshot).AccountsCursor
	state = readPublicState(t, path)
	state["revision"] = 2
	writePublicState(t, path, state)
	if _, err = c.handle(message("accounts", map[string]string{"cursor": cursor})); err == nil {
		t.Fatal("account generation switched during continuation")
	}
	raw, _ := json.Marshal(snapshot{Accounts: accounts[:1], AccountsCursor: "PRIVATE-SIGNER-POSITION", Revision: "9", TotalAccounts: 129})
	result, err := c.publicSnapshotResultAt(c.epoch, "public-test", raw)
	if err != nil || result.AccountsCursor == "PRIVATE-SIGNER-POSITION" || len(result.AccountsCursor) != 32 {
		t.Fatal("metadata mutation exposed private position", result, err)
	}
	if _, err = c.publicSnapshotResultAt(c.epoch, "public-test", []byte("{")); err == nil {
		t.Fatal("malformed metadata snapshot accepted")
	}
	if _, err := c.chainHistoryAt(c.epoch+1, "public-test", accounts[0].ID, ""); err == nil {
		t.Fatal("stale history epoch accepted")
	}
	state = readPublicState(t, path)
	state["reject"] = []string{"journalPage"}
	writePublicState(t, path, state)
	if _, err = c.handle(message("history", struct{}{})); err == nil {
		t.Fatal("journal failure converted to empty success")
	}
}

func TestPublicPageNullFieldsNeverBecomeUnfilteredOrFreshReads(t *testing.T) {
	c, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	for method, bodies := range map[string][]string{
		"accounts":     {`{"cursor":null}`},
		"history":      {`{"cursor":null}`, `{"accountId":null,"asset":null}`, `{"accountId":"a","asset":null}`, `{"accountId":"\u0000","asset":"asset"}`},
		"chainHistory": {`{"accountId":"a","cursor":null}`, `{"accountId":null}`, `{"accountId":123}`},
	} {
		for _, body := range bodies {
			request := message(method, struct{}{})
			request.Body = []byte(body)
			if _, err := c.handle(request); err == nil {
				t.Fatal("invalid public page field accepted", method)
			}
		}
	}
}
