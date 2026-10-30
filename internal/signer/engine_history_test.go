package signer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestAccountJournalIncludesEveryAssetAndBindsContinuationToFilter(t *testing.T) {
	e := publicPagingFixture(t, 2, 400)
	account := e.data.Accounts[0]
	for index := range e.data.Operations {
		if index%4 == 0 {
			e.data.Operations[index].Plan.Asset = account.Network + ":token:historical-import"
		}
		if index == 0 {
			e.data.Operations[index].CreatedAt = 1700000000
		}
	}
	before, _ := json.Marshal(e.Pending())
	page, err := e.JournalPage(account.ID, "", "")
	if err != nil || page.Total != 200 || len(page.Operations) != 128 || page.NextCursor == "" {
		t.Fatal("account-wide journal unavailable", page.Total, err)
	}
	if _, err = e.JournalPage(account.ID, account.Network+":native", page.NextCursor); err == nil {
		t.Fatal("account-wide cursor accepted for a single asset")
	}
	if _, err = e.JournalPage(e.data.Accounts[1].ID, "", page.NextCursor); err == nil {
		t.Fatal("account-wide cursor accepted for a different account")
	}
	count := len(page.Operations)
	tokens := 0
	for _, operation := range page.Operations {
		if operation.Plan.Account.ID != account.ID || operation.Signed != nil {
			t.Fatal("wrong account or signed artifact")
		}
		if strings.Contains(operation.Plan.Asset, "historical-import") {
			tokens++
		}
	}
	last, err := e.JournalPage(account.ID, "", page.NextCursor)
	if err != nil || !last.Complete || last.Total != 200 {
		t.Fatal(last, err)
	}
	count += len(last.Operations)
	for _, operation := range last.Operations {
		if strings.Contains(operation.Plan.Asset, "historical-import") {
			tokens++
		}
	}
	if count != 200 || tokens != 100 || last.Operations[len(last.Operations)-1].CreatedAt != 1700000000 {
		t.Fatal("asset/time rows lost", count, tokens)
	}
	after, _ := json.Marshal(e.Pending())
	if string(before) != string(after) {
		t.Fatal("account history changed encrypted journal state")
	}
}

func TestNewPaymentsRecordCreationTimeAndLegacyHistoryRemainsUnknown(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	var account model.Account
	for _, candidate := range e.Snapshot().Accounts {
		if candidate.Network == "ethereum-sepolia" {
			account = candidate
		}
	}
	plan := evmPlan(account)
	before := time.Now().Unix()
	if err := e.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	operation, err := e.Operation(plan.ID)
	if err != nil || operation.CreatedAt < before || operation.CreatedAt > time.Now().Unix() || operation.Digest != journal.Digest(plan) {
		t.Fatal("creation timestamp changed plan identity", operation, err)
	}
	raw, _ := json.Marshal(operation)
	var roundtrip journal.Operation
	if json.Unmarshal(raw, &roundtrip) != nil || roundtrip.CreatedAt != operation.CreatedAt {
		t.Fatal("creation time not persisted")
	}
	var legacy journal.Operation
	if json.Unmarshal([]byte(`{"plan":{"id":"legacy"},"state":"CONFIRMED"}`), &legacy) != nil || legacy.CreatedAt != 0 {
		t.Fatal("legacy time was invented")
	}
	raw, _ = json.Marshal(legacy)
	if strings.Contains(string(raw), "createdAt") {
		t.Fatal("unknown legacy time emitted as a timestamp")
	}
}
