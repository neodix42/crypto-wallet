package nativewallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBybitWithdrawalRefusalsAndUnknownResultsCannotDisappearBehindCompletedInfo(t *testing.T) {
	for _, state := range []string{"FAILED", "UNKNOWN"} {
		t.Run(state, func(t *testing.T) {
			a, ui, backend, _, saved := freeNativeFixture(t)
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method != "exchangeWizardLoad" {
					t.Error("unexpected authority", method)
				}
				return freeSavedAnswer(out, saved, []any{})
			}
			result := wizardResult{ID: strings.Repeat("d", 32), Kind: "withdraw", State: state, Code: "EXCHANGE_BYBIT_131098", Message: "Bybit currently blocks withdrawals to this newly added address."}
			a.freeWork("Withdraw to TON", true, func(uint64) (freeExchangeUpdate, error) {
				return freeExchangeUpdate{result: &freeExchangeOperation{Stage: 5, wizardResult: result}}, nil
			})
			ui.apply(t)
			if len(a.errorQueue) != 1 || !strings.Contains(a.errorQueue[0].Message, result.ID) || !strings.Contains(a.errorQueue[0].Message, "newly added address") || a.errorQueue[0].Code != result.Code {
				t.Fatal("withdrawal outcome or its original identity disappeared", a.errorQueue)
			}
			if !a.statusIsError || strings.Contains(a.status, "Request completed") || len(a.freeData().operations) != 1 || a.freeData().operations[0].ID != result.ID {
				t.Fatal("rejection became success info or lost its original operation")
			}
		})
	}
}

func TestBybitRecentWithdrawalsAreInertBoundedAndDistinguishPlannedAndReportedAmounts(t *testing.T) {
	a, _, backend, _, _ := freeNativeFixture(t)
	a.wizard.viewStep = 5
	u := a.freeWithdrawalData()
	u.connection, _, _ = a.freeTrackingStatus()
	u.generation = backend.Generation()
	u.history = &freeWithdrawalHistory{Scope: "bounded fixture scope"}
	for i := 0; i < 12; i++ {
		u.history.Rows = append(u.history.Rows, freeWithdrawalRow{Source: "local", OperationID: fmt.Sprintf("original-%02d", i), Coin: "USDT", Chain: "TON", Address: "public-owned-address", Amount: "3", AmountBasis: "plannedDebit", Fee: "0.15", FeeBasis: "estimated", Status: "Rejected", State: "FAILED", Reason: "New address locked", Code: "EXCHANGE_BYBIT_131098", CreatedAt: "2026-11-04T05:00:00Z"})
	}
	page := a.freeWithdrawalHistoryNodes(false)
	table, ok := findNode(page, "freeWithdrawalRequests")
	if !ok || len(table.Children) != 11 || strings.Contains(viewText(table), "original-10") {
		t.Fatal("withdrawal display exceeded ten requests or lost its table", table)
	}
	if table.Children[1].Children[2].Tooltip != "plannedDebit" || table.Children[1].Children[3].Tooltip != "estimated" || strings.Contains(viewText(table), "2.85") {
		t.Fatal("planned debit or fee was relabeled as received funds", table)
	}
	for _, child := range table.Children {
		for _, cell := range child.Children {
			if cell.Kind != "label" {
				t.Fatal("history exposes transaction authority", cell)
			}
		}
	}
	a.exchange = json.RawMessage(`{"provider":"bybit","connected":true,"endpoint":"https://api.bybit.ae","keySuffix":"changed"}`)
	if strings.Contains(viewText(a.freeWithdrawalHistoryNodes(false)), "original-00") {
		t.Fatal("another connection received an older withdrawal history")
	}
}

func TestBybitWithdrawalAddressStatusIsExactAndUnknownDoesNotBecomeListed(t *testing.T) {
	a, _, backend, source, _ := freeNativeFixture(t)
	d := wizardDestination{AccountID: source.ID, AssetID: source.Network + ":native", Coin: "ETH", Chain: "ETH", Address: source.Address}
	u := a.freeWithdrawalData()
	u.connection, _, _ = a.freeTrackingStatus()
	u.generation, u.checkedAt, u.addressScope = backend.Generation(), time.Now(), freeWithdrawalScope(d)
	verified := true
	u.address = &freeWithdrawalAddress{State: "locked24h", Verified: &verified}
	if !a.freeWithdrawalBlocked(d) || !strings.Contains(viewText(a.freeWithdrawalAddressNodes(d)), "24 hours") {
		t.Fatal("known new-address lock was hidden or ignored")
	}
	foreign := d
	foreign.Address = "another-public-address"
	if a.freeWithdrawalBlocked(foreign) || strings.Contains(viewText(a.freeWithdrawalAddressNodes(foreign)), "listed ·") {
		t.Fatal("address status was reused for a different destination")
	}
	u.address.State = "listed"
	if a.freeWithdrawalBlocked(d) || !strings.Contains(viewText(a.freeWithdrawalAddressNodes(d)), "verified") {
		t.Fatal("normal address status was not displayed")
	}
	u.address.State, u.address.Reason = "unknown", "Read unavailable"
	if a.freeWithdrawalBlocked(d) || !strings.Contains(viewText(a.freeWithdrawalAddressNodes(d)), "status unavailable") {
		t.Fatal("unknown status became a whitelist decision")
	}
}

func TestBybitWithdrawalBackgroundReadsAreReadOnlyAndRejectStaleViews(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			a, ui, backend, _, _ := freeNativeFixture(t)
			a.wizard.viewStep = 5
			a.freeData()
			before := a.lastActivity
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method != "exchangeWithdrawals" {
					t.Error("background history acquired another authority", method)
					return errors.New("unexpected method")
				}
				return answer(out, freeWithdrawalHistory{Rows: []freeWithdrawalRow{{Source: "bybit", WithdrawalID: "public-request", Status: "SecurityCheck"}}, ObservedAt: time.Now().UTC().Format(time.RFC3339)})
			}
			if !a.nextFreeWithdrawalRefresh(false) || a.busy || !a.lastActivity.Equal(before) || a.nextFreeWithdrawalRefresh(false) {
				t.Fatal("history read blocked the UI, extended activity or duplicated its request")
			}
			if stale {
				a.exchangeRevision++
			}
			ui.apply(t)
			if stale && a.freeWithdrawalData().history != nil {
				t.Fatal("stale background rows were adopted")
			}
			if !stale && (a.freeWithdrawalData().history == nil || len(a.freeWithdrawalData().history.Rows) != 1) {
				t.Fatal("current read-only history was not adopted")
			}
			if a.busy || !a.lastActivity.Equal(before) || len(a.freeData().operations) != 0 {
				t.Fatal("history read prepared a payment or changed activity")
			}
		})
	}
}
