package signer

import (
	"reflect"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

func TestTONPreparedDeploymentEncryptedPersistenceAndFreshSignerRecovery(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, engine, ui, "testnet")
	plan := tonDeploymentSignerPlan(findAccount(t, engine, "ton-testnet"))
	if err := engine.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	original, err := engine.Operation(plan.ID)
	if err != nil || original.State != "PREPARED" || original.Signed != nil || original.Digest != journal.Digest(plan) {
		t.Fatal("prepared deployment not available for exact review", original, err)
	}
	sealed, err := engine.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := vault.Open(sealed, []byte(syntheticPassword))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	disk, err := vault.Decode(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Destroy()
	if len(disk.Operations) != 1 || !reflect.DeepEqual(disk.Operations[0], original) {
		t.Fatal("real encrypted vault lost the exact prepared deployment")
	}
	if engine.Prepare(plan) == nil {
		t.Fatal("duplicate preparation bypassed the original account reservation")
	}
	if current, err := engine.Operation(plan.ID); err != nil || !reflect.DeepEqual(current, original) {
		t.Fatal("duplicate preparation rewrote the immutable original", err)
	}
	engine.Lock()
	// A genuinely fresh signer opens the existing encrypted files. Restart
	// recovery cancels only unsigned previews and never invents a signature.
	freshUI := &testUI{approve: true, inputs: [][][]byte{input(syntheticPassword)}}
	fresh := New(engine.Files, freshUI, []model.Adapter{ton.New()})
	t.Cleanup(fresh.Lock)
	if err := fresh.Unlock(); err != nil {
		t.Fatal(err)
	}
	recovered, err := fresh.Operation(plan.ID)
	original.State = "CANCELLED"
	if err != nil || !reflect.DeepEqual(recovered, original) {
		t.Fatal("restart changed prepared bytes or recovered them as payable", recovered, err)
	}
	if _, err := fresh.Sign(plan.ID); err == nil {
		t.Fatal("cancelled pre-restart preview could be replayed")
	}
	replacement := plan
	replacement.Expires++
	replacement = tonDeploymentPlanID(replacement)
	if err := fresh.Prepare(replacement); err != nil {
		t.Fatal("explicit fresh preparation remained blocked after safe recovery", err)
	}
	if current, err := fresh.Operation(plan.ID); err != nil || !reflect.DeepEqual(current, original) {
		t.Fatal("fresh deployment preparation overwrote original recovery record", err)
	}
	if current, err := fresh.Operation(replacement.ID); err != nil || current.State != "PREPARED" || current.Signed != nil || !reflect.DeepEqual(current.Plan, replacement) {
		t.Fatal("explicit replacement was not separately and durably prepared", current, err)
	}
}
