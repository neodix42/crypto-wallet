package journal

import (
	"localwallet/internal/model"
	"testing"
)

func TestTP060StateAndReservation(t *testing.T) {
	p := model.Plan{ID: "p", Account: model.Account{ID: "account"}, Recipient: "recipient", Nonce: "1"}
	o := Operation{Plan: p, Digest: Digest(p), State: "PREPARED"}
	if !Conflicts([]Operation{o}, p) {
		t.Fatal("unreserved")
	}
	for _, s := range []string{"AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "SUBMITTING", "UNKNOWN", "SUBMITTING", "SUBMITTED", "CONFIRMED", "REORGED", "CONFIRMED"} {
		if e := o.Transition(s); e != nil {
			t.Fatal(s, e)
		}
	}
	if Conflicts([]Operation{o}, p) {
		t.Fatal("confirmed reservation")
	}
	if o.Transition("SIGNED_NOT_SUBMITTED") == nil {
		t.Fatal("illegal resign")
	}
	q := p
	q.Recipient = "changed"
	if Digest(q) == Digest(p) {
		t.Fatal("recipient binding")
	}
	q = p
	q.Nonce = "2"
	if Digest(q) == Digest(p) {
		t.Fatal("nonce binding")
	}
	ops := []Operation{{State: "PREPARED"}, {State: "AWAITING_CONFIRMATION"}, {State: "SUBMITTING"}, {State: "SIGNED_NOT_SUBMITTED"}}
	Recover(ops)
	for i, s := range []string{"CANCELLED", "CANCELLED", "UNKNOWN", "SIGNED_NOT_SUBMITTED"} {
		if ops[i].State != s {
			t.Fatal("crash reconciliation")
		}
	}
	if Conflicts([]Operation{{State: "UNKNOWN", Plan: model.Plan{Account: model.Account{ID: "other"}}}}, p) {
		t.Fatal("account identity")
	}
}

func TestTP060AmbiguousIdentityRemainsReserved(t *testing.T) {
	p := model.Plan{ID: "original", Account: model.Account{ID: "account"}, Nonce: "7"}
	q := p
	q.ID = "new"
	q.Nonce = "8"
	if Terminal("UNKNOWN") || !Conflicts([]Operation{{Plan: p, State: "UNKNOWN"}}, q) {
		t.Fatal("unknown payment allowed new signature")
	}
}
