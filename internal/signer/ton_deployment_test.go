package signer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func tonDeploymentSignerPlan(account model.Account) model.Plan {
	x, _ := json.Marshal(ton.Payload{Kind: "deployment", Deploy: true, SourceBalance: "1000000000", EstimatedFee: "1000000", StorageReserve: "10000000", MasterSeqno: 123})
	p := model.Plan{Account: account, Asset: account.Network + ":TON", Recipient: account.Address, Amount: "0", MaxFee: "3000000", FeeAsset: account.Network + ":TON", Nonce: "0", Expires: time.Now().Add(time.Minute).Unix(), Payload: x}
	return tonDeploymentPlanID(p)
}

func TestTONLegacyV1PendingDeploymentSurvivesNewSubwallet42Default(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, engine, ui, "testnet")
	seed, err := derivation.Seed(engine.data.Entropy, engine.data.Passphrase)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ton.New().DeriveProfile(seed, 0, "ton-testnet", ton.Profile)
	clear(seed)
	if err != nil {
		t.Fatal(err)
	}
	engine.data.Accounts = []model.Account{legacy}
	if err := engine.persist(); err != nil {
		t.Fatal(err)
	}
	plan := tonDeploymentSignerPlan(legacy)
	if err := engine.Prepare(plan); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	signed, err := engine.Sign(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Update(plan.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	engine.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := engine.Unlock(); err != nil {
		t.Fatal("new default rejected the original recorded V1 authority", err)
	}
	operation, err := engine.Operation(plan.ID)
	if err != nil || operation.State != "UNKNOWN" || operation.Signed == nil || operation.Signed.Identity != signed.Identity || !bytes.Equal(operation.Signed.Bytes, signed.Bytes) || operation.Plan.Account.Profile != ton.Profile || operation.Plan.Account.Address != legacy.Address {
		t.Fatal("upgrade changed the original V1 deployment or released its ambiguity", err)
	}
	if err := engine.AddTONAccount(0, ton.ProfileSubwallet42); err != nil {
		t.Fatal("explicit separate new-profile recovery failed", err)
	}
	if len(engine.Snapshot().Accounts) != 2 || engine.Snapshot().Accounts[0].Address != legacy.Address {
		t.Fatal("adding profile42 rewrote the pending legacy source")
	}
	replacement := plan
	replacement.Expires++
	replacement = tonDeploymentPlanID(replacement)
	if engine.Prepare(replacement) == nil || engine.CancelPrepared(plan.ID) == nil {
		t.Fatal("separate profile creation released a possibly submitted legacy operation")
	}
}

func tonDeploymentPlanID(p model.Plan) model.Plan {
	p.ID = ""
	raw, _ := json.Marshal(p)
	hash := sha256.Sum256(raw)
	p.ID = hex.EncodeToString(hash[:])
	return p
}

func TestTONDeploymentSignerPasswordConsentAndDurableOriginalIdentity(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, engine, ui, "testnet")
	account := findAccount(t, engine, "ton-testnet")
	plan := tonDeploymentSignerPlan(account)
	if err := engine.Prepare(plan); err != nil {
		t.Fatal("real deployment preparation", err)
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	signed, err := engine.Sign(plan.ID)
	if err != nil || signed.Identity == "" || len(signed.Bytes) == 0 {
		t.Fatal("password-authenticated deployment signing failed", err)
	}
	summary := ui.summaries[len(ui.summaries)-1]
	for _, exact := range []string{account.Address, "V5R1", "wallet ID", "no outgoing transfers", ton.CodeHash, "TON protocol has no signed fee cap"} {
		if !strings.Contains(summary, exact) {
			t.Fatalf("signer omitted exact deployment effect %q", exact)
		}
	}
	if _, err := engine.Sign(plan.ID); err == nil {
		t.Fatal("deployment approval replayed")
	}
	if err := engine.Update(plan.ID, "SUBMITTING", nil); err != nil {
		t.Fatal(err)
	}
	engine.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err := engine.Unlock(); err != nil {
		t.Fatal(err)
	}
	restored, err := engine.Operation(plan.ID)
	if err != nil || restored.State != "UNKNOWN" || restored.Signed == nil || restored.Signed.Identity != signed.Identity || !bytes.Equal(restored.Signed.Bytes, signed.Bytes) {
		t.Fatal("restart lost exact ambiguous deployment artifact", restored.State, err)
	}
	replacement := tonDeploymentSignerPlan(account)
	replacement.Expires++
	replacement = tonDeploymentPlanID(replacement)
	if err := engine.Prepare(replacement); err == nil {
		t.Fatal("unknown deployment reservation allowed automatic replacement")
	}
	if err := engine.CancelPrepared(plan.ID); err == nil {
		t.Fatal("signed unknown deployment could be cancelled/released")
	}
}

func TestTONDeploymentSignerCancellationWrongPasswordAndPendingRecovery(t *testing.T) {
	for _, action := range []string{"cancel", "wrong password", "pending recovery", "changed descriptor"} {
		t.Run(action, func(t *testing.T) {
			engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
			createImported(t, engine, ui, "testnet")
			plan := tonDeploymentSignerPlan(findAccount(t, engine, "ton-testnet"))
			if action == "pending recovery" {
				engine.data.RecoveryConfirmed = false
				if err := engine.Prepare(plan); err == nil {
					t.Fatal("unconfirmed recovery authorized deployment")
				}
				return
			}
			if action == "changed descriptor" {
				var params map[string]any
				_ = json.Unmarshal(plan.Account.Parameters, &params)
				params["walletId"] = 42
				plan.Account.Parameters, _ = json.Marshal(params)
				plan = tonDeploymentPlanID(plan)
				if err := engine.Prepare(plan); err == nil {
					t.Fatal("caller changed recorded wallet ID at an existing address")
				}
				return
			}
			if err := engine.Prepare(plan); err != nil {
				t.Fatal(err)
			}
			ui.approve = action != "cancel"
			ui.inputs = append(ui.inputs, input("wrong synthetic storage password"))
			if _, err := engine.Sign(plan.ID); err == nil {
				t.Fatal("deployment signed without exact consent/current password")
			}
			operation, err := engine.Operation(plan.ID)
			if err != nil || operation.State != "CANCELLED" || operation.Signed != nil {
				t.Fatal("denied deployment retained signature or reservation", err)
			}
		})
	}
}
