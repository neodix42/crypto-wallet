package walletcore

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func equalTONResumeJSON(a, b any) bool {
	x, ex := json.Marshal(a)
	y, ey := json.Marshal(b)
	var left, right any
	return ex == nil && ey == nil && json.Unmarshal(x, &left) == nil && json.Unmarshal(y, &right) == nil && reflect.DeepEqual(left, right)
}

// The generic public-state fake stores nested JSON objects in map-key order.
// Match that codec before binding a synthetic plan's authenticated digests.
func tonResumeFixturePlan(plan model.Plan) model.Plan {
	raw, _ := json.Marshal(plan)
	var generic any
	_ = json.Unmarshal(raw, &generic)
	raw, _ = json.Marshal(generic)
	_ = json.Unmarshal(raw, &plan)
	plan.ID = tonDeploymentDigest(plan)
	return plan
}

func TestTONDeploymentSecondClickResumesExactPreparedOperationWithoutRPC(t *testing.T) {
	c, path, account, queries := tonDeploymentCoordinator(t)
	first, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	plan := first.(model.Plan)
	before := readPublicState(t, path)["ops"]
	beforeBytes, _ := json.Marshal(before)
	beforeQueries := queries.Load()
	for attempt := 0; attempt < 2; attempt++ {
		resumed, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
		if err != nil || !equalTONResumeJSON(resumed, plan) {
			t.Fatal("existing prepared deployment was lost or replaced", resumed, err)
		}
	}
	after := readPublicState(t, path)
	afterBytes, _ := json.Marshal(after["ops"])
	if string(beforeBytes) != string(afterBytes) || queries.Load() != beforeQueries {
		t.Fatal("repeat Deploy rewrote the durable operation or contacted RPC")
	}
	prepares := 0
	for _, call := range after["calls"].([]any) {
		switch call {
		case "prepare":
			prepares++
		case "sign", "update", "send", "cancelPrepared":
			t.Fatal("resuming preview gained signature, cancellation or broadcast authority", call)
		}
	}
	if prepares != 1 {
		t.Fatal("repeat Deploy attempted another signer preparation", prepares)
	}
}

func TestTONDeploymentReservationsNeverReplaceSignedOrUnrelatedOperations(t *testing.T) {
	for _, kind := range []string{"signed-not-submitted", "submitting", "unknown", "submitted", "awaiting-confirmation", "prepared-with-signature", "nondeployment", "changed-profile", "changed-parameters", "bad-digest", "bad-plan-id", "expired-signed"} {
		t.Run(kind, func(t *testing.T) {
			c, path, account, queries := tonDeploymentCoordinator(t)
			plan := tonResumeFixturePlan(fundedPointTwoTONDeployment(account))
			op := journal.Operation{Plan: plan, State: "PREPARED", CreatedAt: time.Now().Unix()}
			want := "TON_DEPLOYMENT_PENDING"
			switch kind {
			case "signed-not-submitted":
				op.State = "SIGNED_NOT_SUBMITTED"
			case "submitting":
				op.State = "SUBMITTING"
			case "unknown":
				op.State = "UNKNOWN"
			case "submitted":
				op.State = "SUBMITTED"
			case "awaiting-confirmation":
				op.State = "AWAITING_CONFIRMATION"
			case "nondeployment":
				var payload ton.Payload
				_ = json.Unmarshal(op.Plan.Payload, &payload)
				payload.Kind = ""
				op.Plan.Payload, _ = json.Marshal(payload)
			case "changed-profile":
				op.Plan.Account.Profile = ton.Profile
				want = "TON_DEPLOYMENT_RECORD_FAILED"
			case "changed-parameters":
				op.Plan.Account.Parameters = json.RawMessage(`{}`)
				want = "TON_DEPLOYMENT_RECORD_FAILED"
			case "bad-plan-id":
				op.Plan.ID = "different-canonical-id"
				want = "TON_DEPLOYMENT_RECORD_FAILED"
			case "expired-signed":
				op.Plan.Expires = time.Now().Add(-time.Minute).Unix()
				op.Plan.ID = tonDeploymentDigest(op.Plan)
			}
			if kind != "bad-plan-id" {
				op.Plan = tonResumeFixturePlan(op.Plan)
			}
			switch kind {
			case "signed-not-submitted", "submitting", "unknown", "submitted", "prepared-with-signature", "expired-signed":
				op.Signed = &model.Signed{Identity: "public-original-signed-identity", Bytes: []byte("public-synthetic-original-artifact")}
			}
			op.Digest = journal.Digest(op.Plan)
			if kind == "bad-digest" {
				op.Digest = "corrupt"
				want = "TON_DEPLOYMENT_RECORD_FAILED"
			}
			state := readPublicState(t, path)
			state["ops"] = []journal.Operation{op}
			writePublicState(t, path, state)
			before, _ := json.Marshal(readPublicState(t, path)["ops"])
			result, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
			if result != nil || ipc.PublicErrorCode(err) != want || queries.Load() != 0 {
				t.Fatal("reserved account reached fresh RPC or failed to preserve original", result, err)
			}
			after := readPublicState(t, path)
			current, _ := json.Marshal(after["ops"])
			if string(before) != string(current) {
				t.Fatal("pending bytes/state rewritten")
			}
			for _, call := range after["calls"].([]any) {
				if call == "prepare" || call == "sign" || call == "update" || call == "cancelPrepared" {
					t.Fatal("pending state gained replacement/replay authority", call)
				}
			}
		})
	}
}

func TestTONDeploymentExplicitClickRefreshesOnlyCanonicalExpiredUnsignedPlan(t *testing.T) {
	for _, failCancel := range []bool{false, true} {
		c, path, account, queries := tonDeploymentCoordinator(t)
		old := fundedPointTwoTONDeployment(account)
		old.Expires = time.Now().Add(-time.Minute).Unix()
		old = tonResumeFixturePlan(old)
		original := journal.Operation{Plan: old, Digest: journal.Digest(old), State: "PREPARED", CreatedAt: time.Now().Add(-2 * time.Minute).Unix()}
		state := readPublicState(t, path)
		state["ops"] = []journal.Operation{original}
		if failCancel {
			state["reject"] = []string{"cancelPrepared"}
		}
		writePublicState(t, path, state)
		result, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
		current := readPublicState(t, path)
		var operations []journal.Operation
		raw, _ := json.Marshal(current["ops"])
		if json.Unmarshal(raw, &operations) != nil {
			t.Fatal("invalid operation fixture")
		}
		if failCancel {
			if result != nil || ipc.PublicErrorCode(err) != "TON_DEPLOYMENT_RECORD_FAILED" || !reflect.DeepEqual(operations, []journal.Operation{original}) || queries.Load() != 0 {
				t.Fatal("failed durable cancellation allowed a fresh deployment", result, err, operations)
			}
			continue
		}
		plan, ok := result.(model.Plan)
		original.State = "CANCELLED"
		owned, _ := c.account(account.ID)
		if err != nil || !ok || plan.ID == old.ID || plan.Expires <= time.Now().Unix() || !equalTONResumeJSON(plan.Account, owned) || len(operations) != 2 || !equalTONResumeJSON(operations[0], original) || operations[1].State != "PREPARED" || !equalTONResumeJSON(operations[1].Plan, plan) || queries.Load() == 0 {
			t.Fatal("explicit expired refresh changed old bytes or skipped new verified preparation", result, err, operations)
		}
		for _, call := range current["calls"].([]any) {
			if call == "sign" || call == "update" || call == "send" {
				t.Fatal("refresh signed or broadcast", call)
			}
		}
	}
}

func TestTONDeploymentReservationScanFindsOriginalBehindTerminalHistory(t *testing.T) {
	c, path, account, queries := tonDeploymentCoordinator(t)
	plan := tonResumeFixturePlan(fundedPointTwoTONDeployment(account))
	ops := []journal.Operation{{Plan: plan, Digest: journal.Digest(plan), State: "PREPARED"}}
	for i := 0; i < 130; i++ {
		terminal := plan
		terminal.Expires += int64(i + 1)
		terminal.ID = tonDeploymentDigest(terminal)
		ops = append(ops, journal.Operation{Plan: terminal, Digest: journal.Digest(terminal), State: "CANCELLED"})
	}
	state := readPublicState(t, path)
	state["ops"] = ops
	writePublicState(t, path, state)
	result, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
	if err != nil || !equalTONResumeJSON(result, plan) || queries.Load() != 0 {
		t.Fatal("older prepared deployment hidden by terminal history", result, err)
	}
}
