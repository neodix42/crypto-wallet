package walletcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestPendingPaymentVisibilityChangeKeepsOriginalSignedPlanAndReservation(t *testing.T) {
	for _, hiddenAtPreparation := range []bool{false, true} {
		for _, status := range []string{"SIGNED_NOT_SUBMITTED", "SUBMITTED", "UNKNOWN"} {
			t.Run(fmt.Sprintf("original-hidden=%t/%s", hiddenAtPreparation, status), func(t *testing.T) {
				plan := publicPlan()
				plan.Account.Hidden = hiddenAtPreparation
				plan.Account.Parameters = json.RawMessage(`{"networkId":1,"walletId":42}`)
				original := journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: status, Signed: &model.Signed{Identity: "PUBLIC-ORIGINAL-SIGNED-IDENTITY", Bytes: []byte("PUBLIC-ORIGINAL-SIGNED-BYTES")}}
				adapter := &publicAdapter{}
				c, path := fakeCoordinator(t, adapter, &original)
				current := plan.Account
				current.Hidden = !hiddenAtPreparation
				state := readPublicState(t, path)
				state["accounts"] = []model.Account{current}
				writePublicState(t, path, state)
				before, _ := json.Marshal(readPublicState(t, path)["ops"])
				value, err := c.handle(message("pendingPayment", map[string]string{"accountId": current.ID}))
				raw, _ := json.Marshal(value)
				var response struct {
					Operation *journal.Operation `json:"operation"`
				}
				if err != nil || json.Unmarshal(raw, &response) != nil || response.Operation == nil || response.Operation.Signed != nil || response.Operation.State != status || response.Operation.Digest != original.Digest || !equalTONResumeJSON(response.Operation.Plan, original.Plan) || response.Operation.Plan.Account.Hidden != hiddenAtPreparation || bytes.Contains(raw, original.Signed.Bytes) || bytes.Contains(raw, []byte(original.Signed.Identity)) {
					t.Fatal("visibility change orphaned or rewrote the original signed operation", string(raw), err)
				}
				// A new explicit preparation remains blocked by this exact original;
				// lookup never grants a replacement signature or payment reservation.
				draft := model.Transfer{AccountID: current.ID, Asset: plan.Asset, Recipient: plan.Recipient, Amount: "1"}
				if _, err := c.handle(message("prepare", draft)); ipc.PublicErrorCode(err) != "PAYMENT_PENDING" {
					t.Fatal("visibility change released the original reservation", err)
				}
				afterState := readPublicState(t, path)
				after, _ := json.Marshal(afterState["ops"])
				if !bytes.Equal(before, after) || adapter.prepares != 0 || adapter.submits != 0 || adapter.reconciles != 0 {
					t.Fatal("read-only pending lookup changed or sent the original operation")
				}
				for _, call := range afterState["calls"].([]any) {
					if call == "prepare" || call == "sign" || call == "update" || call == "cancelPrepared" {
						t.Fatal("visibility recovery acquired mutation authority", call)
					}
				}
			})
		}
	}
}

func TestPendingPaymentVisibilityExceptionRetainsEveryImmutableAccountBinding(t *testing.T) {
	for _, field := range []string{"family", "profile", "index", "network", "address", "public-key", "parameters"} {
		t.Run(field, func(t *testing.T) {
			plan := publicPlan()
			plan.Account.Parameters = json.RawMessage(`{"walletId":42}`)
			original := journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: "UNKNOWN", Signed: &model.Signed{Identity: "public-identity", Bytes: []byte("public-original")}}
			c, path := fakeCoordinator(t, &publicAdapter{}, &original)
			current := plan.Account
			current.Hidden = true
			switch field {
			case "family":
				current.Family = "foreign-family"
			case "profile":
				current.Profile = "foreign-profile"
			case "index":
				current.Index++
			case "network":
				current.Network = "unit-mainnet"
			case "address":
				current.Address = "foreign-address"
			case "public-key":
				current.PublicKey = "foreign-key"
			case "parameters":
				current.Parameters = json.RawMessage(`{"walletId":0}`)
			}
			state := readPublicState(t, path)
			state["accounts"] = []model.Account{current}
			writePublicState(t, path, state)
			before, _ := json.Marshal(readPublicState(t, path)["ops"])
			if value, err := c.handle(message("pendingPayment", map[string]string{"accountId": current.ID})); value != nil || ipc.PublicErrorCode(err) != "PAYMENT_PENDING_UNAVAILABLE" {
				t.Fatal("visibility exception weakened an immutable account binding", value, err)
			}
			after, _ := json.Marshal(readPublicState(t, path)["ops"])
			if !bytes.Equal(before, after) {
				t.Fatal("failed binding check rewrote original signed bytes")
			}
		})
	}
}
