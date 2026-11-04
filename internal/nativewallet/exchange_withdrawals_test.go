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
			if state == "UNKNOWN" {
				result.Code, result.Message = "EXCHANGE_WITHDRAWAL_UNKNOWN", "The submission acknowledgement was lost; the original outcome is unknown."
			} else {
				result.RemoteID = "acknowledged-then-rejected-request"
			}
			a.freeWork("Withdraw to TON", true, func(uint64) (freeExchangeUpdate, error) {
				return freeExchangeUpdate{result: &freeExchangeOperation{Stage: 5, wizardResult: result}}, nil
			})
			ui.apply(t)
			if len(a.errorQueue) != 1 || !strings.Contains(a.errorQueue[0].Message, result.ID) || !strings.Contains(a.errorQueue[0].Message, result.Message) || a.errorQueue[0].Code != result.Code {
				t.Fatal("withdrawal outcome or its original identity disappeared", a.errorQueue)
			}
			if !a.statusIsError || strings.Contains(a.status, "Request completed") || len(a.freeData().operations) != 1 || a.freeData().operations[0].ID != result.ID {
				t.Fatal("rejection became success info or lost its original operation")
			}
			if state == "UNKNOWN" && a.errorQueue[0].Title != "Submission status unknown" {
				t.Fatal("unacknowledged uncertainty was mislabeled an execution failure")
			}
		})
	}
}

func TestAcknowledgedBybitRequestsNeverBecomeFailureDialogsFromUnverifiedFollowup(t *testing.T) {
	for _, kind := range []string{"transfer", "swap", "withdraw"} {
		for _, state := range []string{"PENDING", "UNKNOWN"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				a, ui, backend, _, saved := freeNativeFixture(t)
				backend.handler = func(_ uint64, method string, _, out any) error {
					if method != "exchangeWizardLoad" {
						t.Error("unexpected method", method)
					}
					return freeSavedAnswer(out, saved, []any{})
				}
				result := wizardResult{ID: strings.Repeat("e", 32), RemoteID: "exact-bybit-acknowledgement", Kind: kind, State: state, Message: "No complete exact exchange result is established."}
				stage := map[string]int{"transfer": 2, "swap": 3, "withdraw": 5}[kind]
				a.freeWork("Submit "+kind, true, func(uint64) (freeExchangeUpdate, error) {
					return freeExchangeUpdate{result: &freeExchangeOperation{Stage: stage, wizardResult: result}}, nil
				})
				ui.apply(t)
				if len(a.errorQueue) != 0 || a.statusIsError || !strings.Contains(a.status, "acknowledged") || !strings.Contains(a.status, result.RemoteID) {
					t.Fatal("acknowledged request produced a false failure", a.errorQueue, a.status)
				}
				if strings.Contains(a.status, "CONFIRMED") || len(a.freeData().operations) != 1 || a.freeData().operations[0].RemoteID != result.RemoteID {
					t.Fatal("pending observation invented completion or lost identity", a.status)
				}
			})
		}
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
	status := table.Children[1].Children[5]
	if status.Text != "failed" || !strings.Contains(status.Tooltip, "Raw status: Rejected") || !strings.Contains(status.Tooltip, "New address locked") || !strings.Contains(status.Tooltip, "EXCHANGE_BYBIT_131098") || table.Children[1].Tooltip != "" {
		t.Fatal("status details escaped the information disclosure or were lost", status)
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

func TestBybitWithdrawalStatusIsOneCanonicalWordWithCompleteInformation(t *testing.T) {
	for _, state := range []string{"PENDING", "SUCCESS", "REJECTED", "FAILED", "CANCELLED", "UNKNOWN", "REQUESTED", "UNRECOGNIZED", "", "success details"} {
		t.Run(state, func(t *testing.T) {
			request := freeWithdrawalRow{Source: "bybit+local", State: state, Status: "HighValueReviewRejectedRefunded", Reason: "Public reason with multiple words\nand a complete second line", Code: "EXCHANGE_BYBIT_131098", OperationID: "public-local-id", WithdrawalID: "public-provider-id", CreatedAt: "2026-11-04T05:00:00.123Z", UpdatedAt: "2026-11-04T05:01:00.456Z"}
			cell := withdrawalStatusCell(request)
			want := strings.ToLower(state)
			if state == "UNRECOGNIZED" || state == "" || state == "success details" {
				want = "unknown"
			}
			if cell.Kind != "label" || cell.Text != want || len(strings.Fields(cell.Text)) != 1 {
				t.Fatal("withdrawal status was not a canonical inert word", cell)
			}
			for _, detail := range []string{request.Source, request.Status, request.Reason, request.Code, request.OperationID, request.WithdrawalID, request.CreatedAt, request.UpdatedAt} {
				if !strings.Contains(cell.Tooltip, detail) {
					t.Fatal("information disclosure lost a complete public detail", detail, cell.Tooltip)
				}
			}
		})
	}
	if cell := withdrawalStatusCell(freeWithdrawalRow{State: "UNKNOWN", Status: "success"}); cell.Text != "unknown" || !strings.Contains(cell.Tooltip, "Raw status: success") {
		t.Fatal("raw provider text promoted an unknown canonical state", cell)
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
