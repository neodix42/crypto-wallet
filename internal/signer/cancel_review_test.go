package signer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func TestTP060CancelPreparedIsDurableAndReleasesOnlyUnsignedReservation(t *testing.T) {
	e, u, _ := newEngine(t, []model.Adapter{evm.New()})
	if e.CancelPrepared("missing") == nil {
		t.Fatal("locked cancellation succeeded")
	}
	createImported(t, e, u, "testnet")
	if e.CancelPrepared("missing") == nil {
		t.Fatal("unknown cancellation succeeded")
	}
	p := evmPlan(findAccount(t, e, "ethereum-sepolia"))
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelPrepared(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Sign(p.ID); err == nil || e.CancelPrepared(p.ID) == nil {
		t.Fatal("cancelled plan remained authorizable or cancellable")
	}
	b, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(b, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	d, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Destroy()
	if len(d.Operations) != 1 || d.Operations[0].State != "CANCELLED" || d.Operations[0].Signed != nil {
		t.Fatal("unsigned cancellation was not durably recorded")
	}
	p.ID = ""
	p.Expires++
	p.ID = "evm:" + journal.Digest(p)
	if err = e.Prepare(p); err != nil {
		t.Fatal("durably cancelled reservation was not released", err)
	}
	u.inputs = append(u.inputs, input(syntheticPassword))
	signed, err := e.Sign(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"SIGNED_NOT_SUBMITTED", "SUBMITTING", "UNKNOWN", "SUBMITTED", "CONFIRMED", "FAILED", "REPLACED", "REORGED", "PREPARED"} {
		// PREPARED with a signed artifact represents a corrupt/inconsistent
		// journal. It must also retain its artifact and reservation.
		e.data.Operations[1].State = state
		if e.CancelPrepared(p.ID) == nil || e.data.Operations[1].State != state || e.data.Operations[1].Signed.Identity != signed.Identity || !bytes.Equal(e.data.Operations[1].Signed.Bytes, signed.Bytes) {
			t.Fatal("cancellation discarded a signed/ambiguous payment", state)
		}
	}
}

func TestTP031FailedCancellationPersistenceLocksAndPreservesOldGeneration(t *testing.T) {
	e, u, dir := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, u, "testnet")
	p := evmPlan(findAccount(t, e, "ethereum-sepolia"))
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Files.Close(); err != nil {
		t.Fatal(err)
	}
	if e.CancelPrepared(p.ID) == nil || !e.Snapshot().Locked || e.Pending() != nil {
		t.Fatal("undurable cancellation remained unlocked")
	}
	after, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed cancellation damaged old authenticated generation", err)
	}
	files, err := vault.NewFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	fresh := New(files, u, []model.Adapter{evm.New()})
	defer fresh.Lock()
	fresh.parameters = e.parameters
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err = fresh.Unlock(); err != nil {
		t.Fatal("old generation could not recover", err)
	}
	operations := fresh.Snapshot().Operations
	if len(operations) != 1 || operations[0].State != "CANCELLED" {
		t.Fatal("unsigned old generation did not safely recover cancellation")
	}
}
