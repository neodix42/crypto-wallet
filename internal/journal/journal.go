// Package journal defines durable public payment identities and reservations.
package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"localwallet/internal/model"
)

var ErrState = errors.New("invalid or conflicting payment state")

type Operation struct {
	Plan      model.Plan     `json:"plan"`
	Digest    string         `json:"digest"`
	State     string         `json:"state"`
	Signed    *model.Signed  `json:"signed,omitempty"`
	Receipt   *model.Receipt `json:"receipt,omitempty"`
	CreatedAt int64          `json:"createdAt,omitempty"`
}

func Digest(p model.Plan) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

var next = map[string]map[string]bool{
	"PREPARED":              {"AWAITING_CONFIRMATION": true, "CANCELLED": true},
	"AWAITING_CONFIRMATION": {"SIGNED_NOT_SUBMITTED": true, "CANCELLED": true},
	"SIGNED_NOT_SUBMITTED":  {"SUBMITTING": true},
	"SUBMITTING":            {"SUBMITTED": true, "UNKNOWN": true, "FAILED": true, "CONFIRMED": true},
	"UNKNOWN":               {"SUBMITTING": true, "SUBMITTED": true, "CONFIRMED": true, "FAILED": true, "REPLACED": true},
	"SUBMITTED":             {"CONFIRMED": true, "FAILED": true, "UNKNOWN": true, "REPLACED": true, "REORGED": true},
	"CONFIRMED":             {"REORGED": true}, "REORGED": {"SUBMITTED": true, "CONFIRMED": true, "UNKNOWN": true, "FAILED": true},
}

func (o *Operation) Transition(state string) error {
	if !next[o.State][state] {
		return ErrState
	}
	o.State = state
	return nil
}
func Terminal(s string) bool {
	return s == "CANCELLED" || s == "FAILED" || s == "REPLACED" || s == "CONFIRMED"
}

// Conflicts conservatively reserves the whole account while an operation is
// unresolved. This also prevents double spends across DCR outpoint selections.
func Conflicts(ops []Operation, p model.Plan) bool {
	for _, o := range ops {
		if o.Plan.Account.ID == p.Account.ID && !Terminal(o.State) {
			return true
		}
	}
	return false
}

// Recover reports whether restart recovery changed durable payment state.
// Unchanged journals require no new encrypted generation merely to read them.
func Recover(ops []Operation) bool {
	changed := false
	for i := range ops {
		switch ops[i].State {
		case "PREPARED", "AWAITING_CONFIRMATION":
			ops[i].State = "CANCELLED"
			changed = true
		case "SUBMITTING":
			ops[i].State = "UNKNOWN"
			changed = true
		}
	}
	return changed
}
