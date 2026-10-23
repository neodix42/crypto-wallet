package decred

import (
	"context"
	"encoding/json"
	"testing"

	"localwallet/internal/model"
)

func TestDurableDiscoveryBudget(t *testing.T) {
	h, closeNode := dcrNode(t)
	defer closeNode()
	if _, e := SetDiscoveryBudget(model.Account{}, 1); e == nil {
		t.Fatal("invalid account accepted")
	}
	if _, e := SetDiscoveryBudget(h.account, 2_000_001); e == nil {
		t.Fatal("unbounded persisted budget accepted")
	}
	if e := h.a.SetScanBudget(1); e != nil {
		t.Fatal(e)
	}
	if _, e := h.a.Sync(context.Background(), h.account); e == nil {
		t.Fatal("global scan limit ignored")
	}
	ac, e := SetDiscoveryBudget(h.account, 2_000_000)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateRecovery(h.account, ac); e != nil {
		t.Fatal("public scan preference changed recovery root", e)
	}
	if _, e = h.a.Sync(context.Background(), ac); e != nil {
		t.Fatal("persisted budget did not override global limit", e)
	}
	ac, e = SetDiscoveryBudget(ac, 0)
	if e != nil {
		t.Fatal(e)
	}
	if string(ac.Parameters) != string(h.account.Parameters) {
		t.Fatal("zero budget changed legacy descriptor bytes")
	}
	if _, e = h.a.Sync(context.Background(), ac); e == nil {
		t.Fatal("zero budget did not restore default")
	}
	var x Parameters
	_ = json.Unmarshal(ac.Parameters, &x)
	x.BlockBudget = 2_000_001
	ac.Parameters, _ = json.Marshal(x)
	if _, _, e = descriptor(ac); e == nil {
		t.Fatal("tampered over-limit budget accepted")
	}
}
