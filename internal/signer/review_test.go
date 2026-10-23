package signer

import (
	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
	"strings"
	"testing"
)

// A rejected status must never release a reservation by mutating live memory.
func TestReviewTP060ForeignReceiptCannotMutateState(t *testing.T) {
	p := model.Plan{ID: "review-op", Account: model.Account{ID: "review-account"}}
	e := &Engine{data: &vault.Data{Operations: []journal.Operation{{Plan: p, Digest: journal.Digest(p), State: "SUBMITTING", Signed: &model.Signed{Identity: "expected", Bytes: []byte{1}}}}}}
	for _, tc := range []struct {
		state string
		r     *model.Receipt
	}{{"FAILED", &model.Receipt{Identity: "foreign", State: "FAILED"}}, {"CONFIRMED", &model.Receipt{Identity: "expected", State: "UNKNOWN"}}, {"FAILED", nil}} {
		if err := e.Update(p.ID, tc.state, tc.r); err == nil {
			t.Fatal("unbound receipt accepted")
		}
		if e.data.Operations[0].State != "SUBMITTING" {
			t.Fatal("rejected update changed live state")
		}
		if !journal.Conflicts(e.data.Operations, p) {
			t.Fatal("rejected update released reservation")
		}
	}
}

type reviewInputUI struct {
	*testUI
	hook func(string)
}

func (u *reviewInputUI) Input(title string, labels ...string) ([][]byte, error) {
	result, e := u.testUI.Input(title, labels...)
	if u.hook != nil {
		u.hook(title)
	}
	return result, e
}

// Inject an encryption-stage failure after approval/signing but before a durable
// signed record. Neither Sign nor a subsequent Pending call may expose artifacts.
func TestReviewTP060FailedPersistenceQuarantinesSignedArtifact(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	var ac model.Account
	for _, a := range e.Snapshot().Accounts {
		if a.Network == "ethereum-sepolia" {
			ac = a
		}
	}
	p := evmPlan(ac)
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	e.UI = &reviewInputUI{testUI: u, hook: func(title string) {
		if strings.HasPrefix(title, "Reauthenticate") {
			e.parameters = vault.Parameters{}
		}
	}}
	if signed, err := e.Sign(p.ID); err == nil || len(signed.Bytes) != 0 {
		t.Fatal("undurable signature released")
	}
	if len(e.Pending()) != 0 {
		t.Fatal("undurable artifact reachable through Pending")
	}
	if !e.Snapshot().Locked || !e.persistFailed {
		t.Fatal("failed persistence did not quarantine session")
	}
}
