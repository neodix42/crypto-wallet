package walletcore

import (
	"fmt"
	"testing"
	"time"

	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestAccountWidePublicJournalIncludesAllAssetsWithoutChangingScope(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	foreign := publicAccount()
	foreign.ID = "other-account"
	state["accounts"] = []model.Account{publicAccount(), foreign}
	operations := make([]journal.Operation, 260)
	for index := range operations {
		plan := publicPlan()
		plan.ID = fmt.Sprintf("all-assets-%03d", index)
		if index%2 == 0 {
			plan.Asset = "unit-testnet:token:approved-import"
		}
		if index == 0 {
			plan.Account = foreign
		}
		operations[index] = journal.Operation{Plan: plan, State: "UNKNOWN", CreatedAt: 1700000000 + int64(index), Signed: &model.Signed{Bytes: []byte("PRIVATE-ARTIFACT")}}
	}
	state["ops"] = operations
	writePublicState(t, path, state)
	first, err := c.handle(message("history", map[string]string{"accountId": publicAccount().ID}))
	if err != nil {
		t.Fatal(err)
	}
	page := first.(journalPage)
	if page.Total != 259 || page.NextCursor == "" {
		t.Fatal("missing account-wide history", page.Total)
	}
	for _, query := range []map[string]string{
		{"accountId": publicAccount().ID, "asset": publicPlan().Asset, "cursor": page.NextCursor},
		{"accountId": foreign.ID, "cursor": page.NextCursor},
	} {
		if _, err := c.handle(message("history", query)); err == nil {
			t.Fatal("account-wide cursor crossed a query boundary")
		}
	}
	count, tokens := 0, 0
	for {
		for _, operation := range page.Operations {
			if operation.Plan.Account.ID != publicAccount().ID || operation.Signed != nil || operation.CreatedAt == 0 {
				t.Fatal("account-wide transport changed the record")
			}
			count++
			if operation.Plan.Asset != publicPlan().Asset {
				tokens++
			}
		}
		if page.NextCursor == "" {
			break
		}
		out, err := c.handle(message("history", map[string]string{"accountId": publicAccount().ID, "cursor": page.NextCursor}))
		if err != nil {
			t.Fatal(err)
		}
		page = out.(journalPage)
	}
	if count != 259 || tokens != 129 || !page.Complete {
		t.Fatal("account-wide history lost assets", count, tokens, page.Complete)
	}
}

func TestPublicJournalRejectsInvalidCreationTimesAndKeepsLegacyTimeUnknown(t *testing.T) {
	for _, stamp := range []int64{-1, time.Now().Unix() + 600} {
		c, path := fakeCoordinator(t, &publicAdapter{}, nil)
		state := readPublicState(t, path)
		state["journalOverride"] = map[string]any{"total": 1, "operations": []journal.Operation{{Plan: publicPlan(), CreatedAt: stamp}}}
		writePublicState(t, path, state)
		if _, err := c.handle(message("history", map[string]string{"accountId": publicAccount().ID})); err == nil {
			t.Fatal("invalid journal timestamp reached public IPC", stamp)
		}
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	state["ops"] = []journal.Operation{{Plan: publicPlan(), State: "CONFIRMED"}}
	writePublicState(t, path, state)
	out, err := c.handle(message("history", map[string]string{"accountId": publicAccount().ID}))
	if err != nil || len(out.(journalPage).Operations) != 1 || out.(journalPage).Operations[0].CreatedAt != 0 {
		t.Fatal("legacy time was invented", out, err)
	}
}
