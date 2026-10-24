package hypercore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The official activation page establishes the cost but the published HYPE
// action/ledger schemas do not bind its quote-token selection or debit. A funded
// quote balance alone is not sufficient evidence to approve an unspecified fee.
func TestTP052FreshCoreRecipientNeverAssumesFeeTokenOrFreeActivation(t *testing.T) {
	f := fakeCore(t)
	f.override["spotClearinghouseState"] = map[string]any{"balances": []any{
		map[string]any{"coin": "HYPE", "token": 1105, "total": "10", "hold": "0"},
		map[string]any{"coin": "USDC", "token": 0, "total": "1000", "hold": "0"},
	}}
	f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
		if typ == "userRole" && string(req["user"]) == `"0x1111111111111111111111111111111111111111"` {
			return map[string]string{"role": "missing"}, true
		}
		return nil, false
	}
	if _, e := f.prepare(t); e == nil || !strings.Contains(e.Error(), "fee-token selection or its debit") {
		t.Fatal("unpriced first-transfer fee was authorized", e)
	}
	if f.submitCount != 0 {
		t.Fatal("prerequisite probe broadcast a transaction")
	}
	// Once the provider establishes an existing standard owner, a recipient does
	// not need an existing HYPE token balance to receive an ordinary HYPE transfer.
	f.intercept = nil
	p, e := f.prepare(t)
	if e != nil || p.MaxFee != "0" {
		t.Fatal("activated owner without recipient HYPE rejected", e)
	}
	var payload map[string]any
	_ = json.Unmarshal(p.Payload, &payload)
	payload["feeToken"] = "USDC"
	p.Payload, _ = json.Marshal(payload)
	p.ID = planID(p)
	if f.a.ValidatePlan(p) == nil {
		t.Fatal("invented fee selector accepted")
	}
	if _, e = f.a.Sign(syntheticSeed(), p); e == nil {
		t.Fatal("invented fee selector signed")
	}
	if _, e = f.a.Balance(context.Background(), p.Account); e != nil {
		t.Fatal("required HYPE balance path regressed", e)
	}
}
