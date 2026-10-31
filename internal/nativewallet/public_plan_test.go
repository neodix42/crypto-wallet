package nativewallet

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

type publicPlanQuotePeer struct {
	*testBackend
	wire []byte
}

func (p *publicPlanQuotePeer) CallPaymentQuoteAt(_ context.Context, _ uint64, _, out any) error {
	return json.Unmarshal(p.wire, out)
}

func TestPublicNativeClientHooksNormalizeSerializedCoordinatorPlansEverywhere(t *testing.T) {
	a, _, core, account := tonFixture(t)
	original := nativeTONDeploymentPlan(account)
	wire := serializedPublicPlan(t, original)
	core.handler = func(_ uint64, method string, _, out any) error {
		switch method {
		case "prepare":
			return json.Unmarshal(wire, out)
		case "history":
			body, _ := json.Marshal(map[string]any{"operations": []any{map[string]any{"plan": json.RawMessage(wire), "state": "UNKNOWN"}}, "revision": "1", "total": 1, "complete": true})
			return json.Unmarshal(body, out)
		case "pendingPayment":
			body, _ := json.Marshal(map[string]any{"operation": map[string]any{"plan": json.RawMessage(wire), "state": "UNKNOWN"}})
			return json.Unmarshal(body, out)
		}
		return nil
	}
	check := func(plan model.Plan) {
		t.Helper()
		if ton.New().ValidatePlan(plan) != nil || !bytes.Equal(plan.Payload, original.Payload) || journal.Digest(plan) != journal.Digest(original) {
			t.Fatal("native coordinator client left protocol payload quoted or altered original bytes")
		}
	}
	var prepared model.Plan
	if a.callAt(core.Generation(), "prepare", nil, &prepared) != nil {
		t.Fatal("prepare wire failed")
	}
	check(prepared)
	var page journalPage
	if a.callAt(core.Generation(), "history", nil, &page) != nil || len(page.Operations) != 1 {
		t.Fatal("history wire failed")
	}
	check(page.Operations[0].Plan)
	var pending pendingPaymentResponse
	if a.callAt(core.Generation(), "pendingPayment", nil, &pending) != nil || pending.Operation == nil {
		t.Fatal("pending wire failed")
	}
	check(pending.Operation.Plan)
	a.core = &publicPlanQuotePeer{testBackend: core, wire: wire}
	var quoted model.Plan
	if a.callPaymentQuote(context.Background(), core.Generation(), model.Transfer{}, false, &quoted) != nil {
		t.Fatal("optional context quote wire failed")
	}
	check(quoted)
}

func serializedPublicPlan(t *testing.T, plan model.Plan) []byte {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("fixture plan cannot encode")
	}
	fields["payload"], _ = json.Marshal(string(plan.Payload))
	wire, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestPublicTONPlanWireNormalizationPreservesActualValidationIDAndDigest(t *testing.T) {
	_, _, _, account := tonFixture(t)
	original := nativeTONDeploymentPlan(account)
	if err := ton.New().ValidatePlan(original); err != nil {
		t.Fatal("public synthetic deployment fixture must be a real valid plan", err)
	}
	var decoded model.Plan
	if json.Unmarshal(serializedPublicPlan(t, original), &decoded) != nil {
		t.Fatal("wire fixture failed")
	}
	if ton.New().ValidatePlan(decoded) == nil {
		t.Fatal("wire-string regression fixture unexpectedly already validates")
	}
	if err := normalizePublicPlans(&decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) || decoded.ID != original.ID || journal.Digest(decoded) != journal.Digest(original) {
		t.Fatal("normalizing public wire changed original digest-bound bytes")
	}
	if err := ton.New().ValidatePlan(decoded); err != nil {
		t.Fatal("actual Go TON validation still fails after native normalization", err)
	}
	if err := normalizePublicPlans(&decoded); err != nil || !bytes.Equal(decoded.Payload, original.Payload) {
		t.Fatal("already-object normalization changed plan", err)
	}
}

func TestPublicPlanNormalizationPreservesUint64KeyOrderAndEveryOtherStatementField(t *testing.T) {
	payload := json.RawMessage(`{"zNonce":18446744073709551615,"aQuantity":"340282366920938463463374607431768211455","nested":{"z":4294967295,"a":"last"}}`)
	original := model.Plan{ID: "unsigned-original", Account: model.Account{ID: "same-account", Network: "same-network"}, Asset: "same-asset", Recipient: "exact-recipient", Amount: "123", FeeAsset: "same-fee", MaxFee: "456", Nonce: "18446744073709551615", Memo: "unchanged comment", Expires: 1234567890, Payload: payload}
	var decoded model.Plan
	if json.Unmarshal(serializedPublicPlan(t, original), &decoded) != nil {
		t.Fatal("fixture")
	}
	if normalizePublicPlans(&decoded) != nil || viewText(decoded) != viewText(original) || journal.Digest(decoded) != journal.Digest(original) {
		t.Fatal("public normalization remapped amounts, nonce, identity or payload key order")
	}
	page := journalPage{Operations: []journal.Operation{{Plan: decoded, State: "UNKNOWN", Digest: journal.Digest(original)}}}
	quoted, _ := json.Marshal(string(payload))
	page.Operations[0].Plan.Payload = quoted
	if normalizePublicPlans(&page) != nil || !bytes.Equal(page.Operations[0].Plan.Payload, payload) || page.Operations[0].State != "UNKNOWN" || page.Operations[0].Digest != journal.Digest(original) {
		t.Fatal("history normalization changed original operation authority")
	}
	pending := pendingPaymentResponse{Operation: &page.Operations[0]}
	pending.Operation.Plan.Payload = quoted
	if normalizePublicPlans(&pending) != nil || !bytes.Equal(pending.Operation.Plan.Payload, payload) {
		t.Fatal("pending original wire payload remained quoted")
	}
}

func TestPublicPlanNormalizationRejectsMalformedAmbiguousAndDoubleEncodedPayloadsAtomically(t *testing.T) {
	valid := json.RawMessage(`{"seqno":0}`)
	quoted, _ := json.Marshal(string(valid))
	doubleEncoded, _ := json.Marshal(string(quoted))
	for _, text := range []string{"null", `"null"`, `[]`, `"[]"`, string(doubleEncoded), `{"seqno":0,"seqno":1}`, `"{bad}"`, `{"a":1} {"b":2}`, `42`, string([]byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}), strings.Repeat(" ", 1<<20+1)} {
		t.Run(text[:min(len(text), 35)], func(t *testing.T) {
			plan := model.Plan{ID: "original", Payload: json.RawMessage(text)}
			before := append([]byte(nil), plan.Payload...)
			if normalizePublicPlans(&plan) == nil || !bytes.Equal(plan.Payload, before) {
				t.Fatal("invalid public plan payload accepted or altered on error")
			}
			page := journalPage{Operations: []journal.Operation{{Plan: model.Plan{Payload: quoted}, State: "UNKNOWN"}, {Plan: plan, State: "PREPARED"}}}
			if normalizePublicPlans(&page) == nil || !bytes.Equal(page.Operations[0].Plan.Payload, quoted) {
				t.Fatal("invalid history page partially normalized prior records")
			}
		})
	}
	for _, value := range []any{nil, (*model.Plan)(nil), (*journalPage)(nil), (*pendingPaymentResponse)(nil), &pendingPaymentResponse{}, &model.Plan{}, &struct{ Payload string }{"unrelated data"}} {
		if normalizePublicPlans(value) != nil {
			t.Fatal("unrelated or absent DTO changed")
		}
	}
}
