package signer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func publicPagingFixture(t *testing.T, accounts, operations int) *Engine {
	t.Helper()
	e, u, _ := newEngine(t, chains.All())
	createImported(t, e, u, "testnet")
	base := e.data.Accounts[0]
	// Public synthetic metadata stresses the paging boundary, not derivation.
	e.data.Accounts = []model.Account{}
	for i := 0; i < accounts; i++ {
		ac := base
		ac.ID = fmt.Sprintf("public-account-%05d", i)
		ac.Index = uint32(i)
		e.data.Accounts = append(e.data.Accounts, ac)
	}
	for i := 0; i < operations; i++ {
		p := model.Plan{ID: fmt.Sprintf("public-operation-%05d", i), Account: e.data.Accounts[i%len(e.data.Accounts)], Asset: base.Network + ":native", Amount: "1", Recipient: base.Address, Payload: json.RawMessage(`{"public":"synthetic"}`)}
		e.data.Operations = append(e.data.Operations, journal.Operation{Plan: p, Digest: journal.Digest(p), State: "UNKNOWN", Signed: &model.Signed{Identity: p.ID, Bytes: []byte("PUBLIC-SYNTHETIC-SIGNED-BYTES")}})
	}
	return e
}
func TestPublicSnapshotPagesRemainBoundedWithTenThousandAccountsAndLargeJournal(t *testing.T) {
	e := publicPagingFixture(t, 10000, 1000)
	cursor := ""
	seen := map[string]bool{}
	pages := 0
	for {
		page, err := e.SnapshotPage(cursor)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > PublicPageBytes || len(page.Accounts) > PublicPageRecords || len(page.Operations) != 0 || page.TotalAccounts != 10000 || page.TotalOperations != 1000 || page.Revision == "" {
			t.Fatal("unbounded/incomplete public page metadata", len(raw), err)
		}
		for _, ac := range page.Accounts {
			if seen[ac.ID] {
				t.Fatal("duplicate account")
			}
			seen[ac.ID] = true
		}
		if bytes.Contains(raw, []byte("SIGNED-BYTES")) || bytes.Contains(raw, e.data.Entropy) || bytes.Contains(raw, []byte(syntheticPassword)) {
			t.Fatal("nonpublic data in account page")
		}
		pages++
		cursor = page.AccountsCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 10000 || pages < 79 {
		t.Fatal("account history truncated", len(seen), pages)
	}
	ac, err := e.PublicAccount("public-account-09999")
	if err != nil || ac.Index != 9999 {
		t.Fatal("typed late account lookup failed", err)
	}
	if _, err = e.PublicAccount("missing"); err == nil {
		t.Fatal("unknown account accepted")
	}
}
func TestPublicJournalPagesRedactArtifactsWithoutFreeingReservations(t *testing.T) {
	e := publicPagingFixture(t, 3, 401)
	before, _ := json.Marshal(e.Pending())
	cursor := ""
	seen := map[string]bool{}
	count := 0
	for {
		page, err := e.JournalPage("", "", cursor)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if len(raw) > PublicPageBytes || len(page.Operations) > PublicPageRecords || page.Total != 401 {
			t.Fatal("journal page bounds")
		}
		for _, op := range page.Operations {
			if op.Signed != nil || seen[op.Plan.ID] {
				t.Fatal("signed/duplicate public operation")
			}
			seen[op.Plan.ID] = true
			count++
		}
		if bytes.Contains(raw, []byte("SIGNED-BYTES")) {
			t.Fatal("private broadcast artifact exposed")
		}
		cursor = page.NextCursor
		if cursor == "" {
			if !page.Complete {
				t.Fatal("missing completeness")
			}
			break
		}
		if page.Complete {
			t.Fatal("premature completeness")
		}
	}
	if count != 401 {
		t.Fatal("journal silently truncated", count)
	}
	account := e.data.Accounts[1]
	page, err := e.JournalPage(account.ID, account.Network+":native", "")
	if err != nil || page.Total != 134 {
		t.Fatal("asset/account filter", page.Total, err)
	}
	for _, op := range page.Operations {
		if op.Plan.Account.ID != account.ID {
			t.Fatal("foreign account page")
		}
	}
	exact, err := e.Operation("public-operation-00400")
	if err != nil || exact.Signed == nil || string(exact.Signed.Bytes) != "PUBLIC-SYNTHETIC-SIGNED-BYTES" {
		t.Fatal("exact archived artifact lost", err)
	}
	if _, err = e.Operation("missing"); err == nil {
		t.Fatal("unknown operation accepted")
	}
	after, _ := json.Marshal(e.Pending())
	if !bytes.Equal(before, after) || !journal.Conflicts(e.Pending(), e.Pending()[0].Plan) {
		t.Fatal("paging changed durable reservation")
	}
}
func TestPublicPagingRejectsStaleCrossKindFilterTamperAndOversizeRecords(t *testing.T) {
	e := publicPagingFixture(t, 130, 260)
	accounts, err := e.SnapshotPage("")
	if err != nil {
		t.Fatal(err)
	}
	journalFirst, err := e.JournalPage("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []string{"!", strings.Repeat("a", 2049), base64.RawURLEncoding.EncodeToString([]byte(`{"revision":"1","revision":"2"}`)), journalFirst.NextCursor} {
		if _, err = e.SnapshotPage(cursor); err == nil {
			t.Fatal("invalid/cross-kind cursor accepted")
		}
	}
	if _, err = e.JournalPage(e.data.Accounts[0].ID, e.data.Accounts[0].Network+":native", journalFirst.NextCursor); err == nil {
		t.Fatal("cross-filter cursor accepted")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(accounts.AccountsCursor)
	var pos publicPosition
	_ = json.Unmarshal(raw, &pos)
	for _, offset := range []int{-1, 0, 130} {
		p := pos
		p.Offset = offset
		raw, _ := json.Marshal(p)
		if _, err = e.SnapshotPage(base64.RawURLEncoding.EncodeToString(raw)); err == nil {
			t.Fatal("out-of-bounds cursor")
		}
	}
	e.data.Generation++
	if _, err = e.SnapshotPage(accounts.AccountsCursor); err == nil {
		t.Fatal("stale account generation accepted")
	}
	if _, err = e.JournalPage("", "", journalFirst.NextCursor); err == nil {
		t.Fatal("stale journal generation accepted")
	}
	for _, query := range [][2]string{{"missing", "asset"}, {e.data.Accounts[0].ID, "unsupported-asset"}, {e.data.Accounts[0].ID, ""}, {"", "asset"}} {
		if _, err = e.JournalPage(query[0], query[1], ""); err == nil {
			t.Fatal("invalid journal scope accepted")
		}
	}
	e.data.Accounts[0].Network = "unsupported"
	if _, err = e.JournalPage(e.data.Accounts[0].ID, "unsupported:native", ""); err == nil {
		t.Fatal("unregistered adapter accepted")
	}
	e.data.Accounts[0].Parameters = json.RawMessage(`{"oversized":"` + strings.Repeat("x", PublicPageBytes) + `"}`)
	if _, err = e.SnapshotPage(""); err == nil {
		t.Fatal("oversize account emitted")
	}
	e.data.Operations[259].Plan.Memo = strings.Repeat("x", PublicPageBytes)
	if _, err = e.JournalPage("", "", ""); err == nil {
		t.Fatal("oversize journal entry emitted")
	}
	e.Lock()
	if _, err = e.PublicAccount("x"); err == nil {
		t.Fatal("locked account lookup")
	}
	if _, err = e.Operation("x"); err == nil {
		t.Fatal("locked operation lookup")
	}
	if _, err = e.JournalPage("", "", ""); err == nil {
		t.Fatal("locked journal lookup")
	}
	if _, err = e.SnapshotPage(accounts.AccountsCursor); err == nil {
		t.Fatal("locked continuation accepted")
	}
	locked, err := e.SnapshotPage("")
	if err != nil || !locked.Locked || len(locked.Accounts) != 0 || locked.AccountsCursor != "" {
		t.Fatal("locked page leaked accounts", locked, err)
	}
}
func TestPublicJournalByteBoundaryAndSparseFilterProgress(t *testing.T) {
	e := publicPagingFixture(t, 2, 260)
	for i := range e.data.Operations {
		e.data.Operations[i].Plan.Memo = strings.Repeat("p", 16000)
	}
	first, err := e.JournalPage("", "", "")
	if err != nil || len(first.Operations) >= 128 || first.NextCursor == "" {
		t.Fatal("byte bound ignored", len(first.Operations), err)
	}
	filtered := e.data.Accounts[1]
	for i := range e.data.Operations {
		e.data.Operations[i].Plan.Account = e.data.Accounts[0]
	}
	e.data.Operations[0].Plan.Account = filtered
	page, err := e.JournalPage(filtered.ID, filtered.Network+":native", "")
	if err != nil || !page.Complete || len(page.Operations) != 1 || page.Total != 1 {
		t.Fatal("sparse journal filter failed", page, err)
	}
	e.data.Operations = nil
	empty, err := e.JournalPage("", "", "")
	if err != nil || !empty.Complete || empty.Total != 0 || len(empty.Operations) != 0 {
		t.Fatal("empty local journal", empty, err)
	}
}
