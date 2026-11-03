package bybit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// DepositRecordError identifies a fixed validation category, never the provider
// value, response body, request headers or credentials. Callers must allowlist
// Field before including it in a public error code.
type DepositRecordError struct {
	Field string
	// Reason is a fixed shape category for a review-field failure. It never
	// contains the provider value; callers must allowlist it before display.
	Reason string
}

func (e *DepositRecordError) Error() string { return "invalid Bybit deposit record" }
func (e *DepositRecordError) Unwrap() error { return ErrInvalidResponse }

func depositRecordResponseError(err error) error {
	if errors.Is(err, ErrInvalidResponse) {
		return &DepositRecordError{Field: "RESPONSE"}
	}
	return err
}

func parseDepositRecords(raw map[string]json.RawMessage, coin, txID, cursor string) (DepositPage, error) {
	for name := range raw {
		if name != "rows" && strings.EqualFold(name, "rows") {
			return DepositPage{}, &DepositRecordError{Field: "PAGE"}
		}
		if name != "nextPageCursor" && strings.EqualFold(name, "nextPageCursor") {
			return DepositPage{}, &DepositRecordError{Field: "CURSOR"}
		}
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw["rows"], &rows) != nil || rows == nil || len(rows) > 50 {
		return DepositPage{}, &DepositRecordError{Field: "PAGE"}
	}
	r := DepositPage{Rows: []DepositRecord{}}
	if value, exists := raw["nextPageCursor"]; exists {
		if json.Unmarshal(value, &r.NextPageCursor) != nil {
			return DepositPage{}, &DepositRecordError{Field: "CURSOR"}
		}
	}
	if !validCursor(r.NextPageCursor) || r.NextPageCursor != "" && r.NextPageCursor == cursor {
		return DepositPage{}, &DepositRecordError{Field: "CURSOR"}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		d, err := parseDepositRecord(row, coin, txID)
		if err != nil {
			return DepositPage{}, err
		}
		if seen[d.ID] {
			return DepositPage{}, &DepositRecordError{Field: "ID"}
		}
		seen[d.ID] = true
		r.Rows = append(r.Rows, d)
	}
	return r, nil
}

func depositRecordField(fields map[string]json.RawMessage, name, category string, out any, required bool) error {
	value, exists := fields[name]
	// encoding/json's struct decoder folds case. Reject competing spellings
	// explicitly so a shadowed status or coin cannot acquire another meaning
	// when this record is decoded elsewhere.
	for key := range fields {
		if key != name && strings.EqualFold(key, name) {
			return &DepositRecordError{Field: category}
		}
	}
	if !exists {
		if required {
			return &DepositRecordError{Field: category}
		}
		return nil
	}
	if required && bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, out) != nil {
		return &DepositRecordError{Field: category}
	}
	return nil
}

// Review states have identical meaning whether encoded as an integer or a
// canonical numeric string. Do not use truthiness, trim provider strings or
// default malformed/unknown values to zero: zero means an approved review.
func depositReviewStatus(fields map[string]json.RawMessage) (int, bool, error) {
	for name := range fields {
		if name != "travelRuleStatus" && strings.EqualFold(name, "travelRuleStatus") {
			return 0, false, &DepositRecordError{Field: "REVIEW", Reason: "SHADOW"}
		}
	}
	raw, present := fields["travelRuleStatus"]
	if !present {
		return 0, false, nil
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "NULL"}
	}
	var status int
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "TYPE"}
		}
		if value == "" {
			return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "EMPTY"}
		}
		if len(value) != 1 || value[0] < '0' || value[0] > '3' {
			return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "VALUE"}
		}
		status = int(value[0] - '0')
	} else if json.Unmarshal(raw, &status) != nil {
		return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "TYPE"}
	}
	if status < 0 || status > 3 {
		return 0, true, &DepositRecordError{Field: "REVIEW", Reason: "VALUE"}
	}
	return status, true, nil
}

func parseDepositRecord(row json.RawMessage, coin, txID string) (DepositRecord, error) {
	var fields map[string]json.RawMessage
	var d DepositRecord
	if json.Unmarshal(row, &fields) != nil || fields == nil {
		return d, &DepositRecordError{Field: "PAGE"}
	}
	for _, field := range []struct {
		name, category string
		out            any
		required       bool
	}{
		{"id", "ID", &d.ID, true},
		{"coin", "COIN", &d.Coin, true},
		{"chain", "CHAIN", &d.Chain, true},
		{"amount", "AMOUNT", &d.Amount, true},
		{"txID", "TRANSACTION", &d.TxID, true},
		{"status", "STATUS", &d.Status, true},
		{"toAddress", "ADDRESS", &d.ToAddress, true},
		{"tag", "MEMO", &d.Tag, true},
		{"depositType", "TYPE", &d.DepositType, true},
		{"fromAddress", "SOURCE", &d.FromAddress, false},
		{"depositFee", "FEE", &d.DepositFee, false},
		{"successAt", "TIME", &d.SuccessAt, false},
		{"confirmations", "CONFIRMATIONS", &d.Confirmations, false},
	} {
		if err := depositRecordField(fields, field.name, field.category, field.out, field.required); err != nil {
			return DepositRecord{}, err
		}
	}
	var reviewErr error
	d.TravelRuleStatus, d.TravelRuleStatusPresent, reviewErr = depositReviewStatus(fields)
	if reviewErr != nil {
		return DepositRecord{}, reviewErr
	}
	for _, check := range []struct {
		category string
		valid    bool
	}{
		// Bybit documents an opaque string ID, not an ASCII identifier. It is
		// retained only for duplicate detection, never used as a payment hash,
		// URL component or signing payload.
		{"ID", boundedText(d.ID, 128, false)},
		{"COIN", d.Coin == coin},
		{"TRANSACTION", SameDepositTransactionHash(d.TxID, txID)},
		{"CHAIN", identifier(d.Chain, 40)},
		{"AMOUNT", decimal(d.Amount, true)},
		{"ADDRESS", boundedText(d.ToAddress, 256, false)},
		{"SOURCE", boundedText(d.FromAddress, 256, true)},
		{"MEMO", boundedText(d.Tag, 120, true)},
		{"STATUS", d.Status >= 0 && d.Status <= 100},
		{"FEE", d.DepositFee == "" || decimal(d.DepositFee, false)},
		{"TIME", boundedText(d.SuccessAt, 32, true)},
		{"CONFIRMATIONS", boundedText(d.Confirmations, 64, true)},
		{"TYPE", boundedText(d.DepositType, 16, true)},
		{"REVIEW", d.TravelRuleStatus >= 0 && d.TravelRuleStatus <= 3},
	} {
		if !check.valid {
			return DepositRecord{}, &DepositRecordError{Field: check.category}
		}
	}
	return d, nil
}
