package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

const depositResponseCanary = "PRIVATE_PROVIDER_RESPONSE_CANARY"

func depositRecordDiagnosticFixture(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"id": "credited-original", "coin": "GRAM", "chain": "TON", "amount": "5.99998566",
		"txID": strings.Repeat("ab", 32), "status": 3,
		"toAddress": "EQpublicSyntheticAddress", "tag": "", "depositType": "0",
		"fromAddress": "EQpublicSyntheticSource", "depositFee": "",
		"successAt": "1700000000000", "confirmations": "64", "travelRuleStatus": 0,
	}
}

func depositRecordDiagnosticRead(t *testing.T, result any, cursor string) (DepositPage, error) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return testClient(t, string(raw)).DepositRecords(context.Background(), "GRAM", strings.Repeat("ab", 32), cursor)
}

func assertDepositRecordDiagnostic(t *testing.T, err error, field string) {
	t.Helper()
	var typed *DepositRecordError
	if !errors.Is(err, ErrInvalidResponse) || !errors.As(err, &typed) || typed.Field != field {
		t.Fatalf("lost fixed validation category %s: %T %v", field, err, err)
	}
	if strings.Contains(err.Error(), depositResponseCanary) || strings.Contains(err.Error(), "EQpublicSynthetic") {
		t.Fatal("raw provider values escaped through the error")
	}
}

func TestDepositRecordDiagnosticsIdentifyRejectedFieldWithoutReturningProviderValues(t *testing.T) {
	for _, scenario := range []struct {
		name, field, property string
		value                 any
	}{
		{"id-type", "ID", "id", 123},
		{"id-control", "ID", "id", depositResponseCanary + "\n"},
		{"id-empty", "ID", "id", ""},
		{"id-bounds", "ID", "id", strings.Repeat("a", 129)},
		{"coin-foreign", "COIN", "coin", depositResponseCanary},
		{"transaction-foreign", "TRANSACTION", "txID", strings.Repeat("cd", 32)},
		{"chain-value", "CHAIN", "chain", depositResponseCanary + "/"},
		{"amount-negative", "AMOUNT", "amount", "-1"},
		{"amount-zero", "AMOUNT", "amount", "0"},
		{"amount-type", "AMOUNT", "amount", 6},
		{"address-empty", "ADDRESS", "toAddress", ""},
		{"address-bounds", "ADDRESS", "toAddress", strings.Repeat("a", 257)},
		{"source-control", "SOURCE", "fromAddress", depositResponseCanary + "\n"},
		{"memo-control", "MEMO", "tag", depositResponseCanary + "\n"},
		{"status-type", "STATUS", "status", "3"},
		{"status-range", "STATUS", "status", 101},
		{"fee-negative", "FEE", "depositFee", "-0.00001434"},
		{"fee-type", "FEE", "depositFee", 0},
		{"time-control", "TIME", "successAt", depositResponseCanary + "\n"},
		{"confirmations-control", "CONFIRMATIONS", "confirmations", depositResponseCanary + "\n"},
		{"type-bounds", "TYPE", "depositType", depositResponseCanary},
		{"review-null", "REVIEW", "travelRuleStatus", nil},
		{"review-type", "REVIEW", "travelRuleStatus", "approved"},
		{"review-range", "REVIEW", "travelRuleStatus", 4},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			row := depositRecordDiagnosticFixture(t)
			row[scenario.property] = scenario.value
			_, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}, "nextPageCursor": ""}, "")
			assertDepositRecordDiagnostic(t, err, scenario.field)
		})
	}
	for property, category := range map[string]string{"id": "ID", "coin": "COIN", "chain": "CHAIN", "amount": "AMOUNT", "txID": "TRANSACTION", "status": "STATUS", "toAddress": "ADDRESS", "tag": "MEMO", "depositType": "TYPE"} {
		for _, absent := range []bool{false, true} {
			name := property + "/null"
			if absent {
				name = property + "/missing"
			}
			t.Run(name, func(t *testing.T) {
				row := depositRecordDiagnosticFixture(t)
				row[property] = nil
				if absent {
					delete(row, property)
				}
				_, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
				assertDepositRecordDiagnostic(t, err, category)
			})
		}
	}
}

func TestDepositReviewKnownNumericStringsPreserveEveryReportedState(t *testing.T) {
	for state := 0; state <= 3; state++ {
		for _, stringForm := range []bool{false, true} {
			name := "integer-" + strconv.Itoa(state)
			var value any = state
			if stringForm {
				name = "string-" + strconv.Itoa(state)
				value = strconv.Itoa(state)
			}
			t.Run(name, func(t *testing.T) {
				row := depositRecordDiagnosticFixture(t)
				row["travelRuleStatus"] = value
				page, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
				if err != nil || len(page.Rows) != 1 || !page.Rows[0].TravelRuleStatusPresent || page.Rows[0].TravelRuleStatus != state {
					t.Fatal("review state was rejected or changed during representation normalization", state, err)
				}
				if state != 0 && page.Rows[0].TravelRuleStatus == 0 {
					t.Fatal("a pending, collecting or rejected review became approved")
				}
			})
		}
	}
}

func TestDepositReviewMalformedOrUnreportedRepresentationsNeverBecomeApproved(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		value        any
	}{
		{"null", "NULL", nil},
		{"empty", "EMPTY", ""},
		{"true", "TYPE", true},
		{"false", "TYPE", false},
		{"float", "TYPE", json.RawMessage("0.0")},
		{"object", "TYPE", map[string]any{"status": 0}},
		{"array", "TYPE", []int{0}},
		{"unknown-number", "VALUE", 4},
		{"negative-number", "VALUE", -1},
		{"unknown-string", "VALUE", depositResponseCanary},
		{"padded-string", "VALUE", "00"},
		{"sign-string", "VALUE", "+0"},
		{"negative-zero-string", "VALUE", "-0"},
		{"float-string", "VALUE", "0.0"},
		{"exponent-string", "VALUE", "0e0"},
		{"space-string", "VALUE", " 0 "},
		{"control-string", "VALUE", "0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := depositRecordDiagnosticFixture(t)
			row["travelRuleStatus"] = test.value
			_, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
			assertDepositRecordDiagnostic(t, err, "REVIEW")
			var typed *DepositRecordError
			if !errors.As(err, &typed) || typed.Reason != test.reason {
				t.Fatal("review representation reason was lost", test.reason, err)
			}
		})
	}
	row := depositRecordDiagnosticFixture(t)
	row["travelRuleStatus"], row["TravelRuleStatus"] = "2", "0"
	_, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
	var typed *DepositRecordError
	if !errors.As(err, &typed) || typed.Field != "REVIEW" || typed.Reason != "SHADOW" {
		t.Fatal("shadowed pending review was accepted or lost its fixed reason", err)
	}
}

func TestDepositRecordDiagnosticsKeepEmptyPagesAndOptionalFieldsDistinctFromCredit(t *testing.T) {
	page, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{}, "nextPageCursor": ""}, "")
	if err != nil || page.Rows == nil || len(page.Rows) != 0 {
		t.Fatal("an explicitly empty page became unavailable or invented credit", err)
	}
	row := depositRecordDiagnosticFixture(t)
	for _, key := range []string{"fromAddress", "depositFee", "successAt", "confirmations", "travelRuleStatus"} {
		delete(row, key)
	}
	page, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].DepositFee != "" || page.Rows[0].FromAddress != "" || page.Rows[0].TravelRuleStatusPresent {
		t.Fatal("documented optional values were required or synthesized", page, err)
	}
	for _, result := range []any{map[string]any{}, map[string]any{"rows": nil}, map[string]any{"rows": depositResponseCanary}, map[string]any{"rows": []any{nil}}} {
		_, err = depositRecordDiagnosticRead(t, result, "")
		assertDepositRecordDiagnostic(t, err, "PAGE")
	}
	for _, cursor := range []any{"cursor1", depositResponseCanary + "\n", 123} {
		_, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{}, "nextPageCursor": cursor}, "cursor1")
		assertDepositRecordDiagnostic(t, err, "CURSOR")
	}
	_, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{}, "Rows": []any{depositRecordDiagnosticFixture(t)}}, "")
	assertDepositRecordDiagnostic(t, err, "PAGE")
	_, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{}, "nextPageCursor": "", "NextPageCursor": "another-page"}, "")
	assertDepositRecordDiagnostic(t, err, "CURSOR")
	row = depositRecordDiagnosticFixture(t)
	_, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row, row}}, "")
	assertDepositRecordDiagnostic(t, err, "ID")
}

func TestDepositRecordAcceptsDocumentedOpaqueIDAndRejectsShadowedIdentityFields(t *testing.T) {
	for _, id := range []string{"TON:original/0", "deposit.original.1", "opaque-識別子"} {
		row := depositRecordDiagnosticFixture(t)
		row["id"] = id
		page, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
		if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != id {
			t.Fatal("documented opaque ID was constrained to an invented charset", err)
		}
		_, err = depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row, row}}, "")
		assertDepositRecordDiagnostic(t, err, "ID")
	}
	for _, field := range []struct{ key, category string }{{"Status", "STATUS"}, {"Coin", "COIN"}, {"TxID", "TRANSACTION"}, {"TravelRuleStatus", "REVIEW"}} {
		row := depositRecordDiagnosticFixture(t)
		row[field.key] = depositResponseCanary
		_, err := depositRecordDiagnosticRead(t, map[string]any{"rows": []any{row}}, "")
		assertDepositRecordDiagnostic(t, err, field.category)
	}
}

func TestDepositRecordDiagnosticsClassifyMalformedEnvelopeAndRetainNumericProviderErrors(t *testing.T) {
	_, err := testClient(t, `{"rows":[]`).DepositRecords(context.Background(), "GRAM", strings.Repeat("ab", 32), "")
	assertDepositRecordDiagnostic(t, err, "RESPONSE")
	api := &APIError{Code: 10005}
	if depositRecordResponseError(api) != api || depositRecordResponseError(ErrUnavailable) != ErrUnavailable {
		t.Fatal("response diagnostic hid an API permission or transport reason")
	}
	if strings.Contains((&DepositRecordError{Field: depositResponseCanary}).Error(), depositResponseCanary) {
		t.Fatal("a diagnostic category was treated as printable provider text")
	}
}
