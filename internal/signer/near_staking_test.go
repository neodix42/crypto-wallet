package signer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

// Every key/password used here is the existing public synthetic recovery
// fixture. Signing is real, while the blockchain is intentionally not contacted.
func stakingSignerPlan(account model.Account, method string) model.Plan {
	const atomic = "1000000000000000000000000"
	deposit, args := "0", "{}"
	if method == "deposit_and_stake" {
		deposit = atomic
	}
	if method == "unstake" {
		args = `{"amount":"` + atomic + `"}`
	}
	now := time.Now().Unix()
	payload, _ := json.Marshal(map[string]any{
		"version": 1, "blockHash": base58.Encode(make([]byte, 32)), "blockHeight": "123", "protocol": "88",
		"gas": "100000000000000", "gasPrice": "1000000000", "storageReserve": "0", "recipientExists": true,
		"prepared": now, "feePolicy": "protocol-assessed fee; estimate reserve is not a signed fee cap",
		"staking": map[string]string{"method": method, "args": args, "deposit": deposit, "gas": "100000000000000"},
	})
	return model.Plan{ID: "staking-" + method, Account: account, Asset: account.Network + ":native", FeeAsset: account.Network + ":native", Recipient: "test.pool.f863973.m0", Amount: atomic, MaxFee: "200000000000000000000000", Nonce: "42", Expires: now + 60, Payload: payload}
}

func stakingMutation(p model.Plan, field string, value any) model.Plan {
	var payload map[string]any
	_ = json.Unmarshal(p.Payload, &payload)
	payload["staking"].(map[string]any)[field] = value
	p.Payload, _ = json.Marshal(payload)
	return p
}

func TestNEARStakingSignerExplicitApprovalAndDurableOriginalIdentity(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "testnet")
	account := findAccount(t, e, "near-testnet")
	var last model.Signed
	var lastPlan model.Plan
	for index, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		plan := stakingSignerPlan(account, method)
		plan.Nonce = fmt.Sprint(42 + index)
		if err := e.Prepare(plan); err != nil {
			t.Fatal(method, err)
		}
		ui.inputs = append(ui.inputs, input(syntheticPassword))
		signed, err := e.Sign(plan.ID)
		if err != nil || signed.Identity == "" || len(signed.Bytes) == 0 {
			t.Fatalf("%s real signing: %v", method, err)
		}
		summary := ui.summaries[len(ui.summaries)-1]
		for _, exact := range []string{account.Address, plan.Recipient, method, "100000000000000", "NEAR protocol has no signed fee cap"} {
			if !strings.Contains(summary, exact) {
				t.Fatalf("%s omitted exact approved effect %q", method, exact)
			}
		}
		if method != "deposit_and_stake" && !strings.Contains(summary, "0 NEAR") {
			t.Fatalf("%s did not disclose zero attached NEAR", method)
		}
		if method == "unstake_all" || method == "withdraw_all" {
			if !strings.Contains(strings.ToLower(summary), "all") || !strings.Contains(strings.ToLower(summary), "observ") {
				t.Fatalf("%s treated its observed balance as a signed amount", method)
			}
		}
		if _, err = e.Sign(plan.ID); err == nil {
			t.Fatal("one-use staking authorization replayed")
		}
		operation, err := e.Operation(plan.ID)
		if err != nil || operation.State != "SIGNED_NOT_SUBMITTED" || operation.Signed == nil || !bytes.Equal(operation.Signed.Bytes, signed.Bytes) || operation.Signed.Identity != signed.Identity {
			t.Fatal("exact signed artifact was not retained before returning it")
		}
		if err = e.Update(plan.ID, "SUBMITTING", nil); err != nil {
			t.Fatal(err)
		}
		state := "CONFIRMED"
		if method == "withdraw_all" {
			state = "UNKNOWN"
		}
		if err = e.Update(plan.ID, state, &model.Receipt{Identity: signed.Identity, State: state}); err != nil {
			t.Fatal(err)
		}
		last, lastPlan = signed, plan
	}
	page, err := e.JournalPage(account.ID, lastPlan.Asset, "")
	if err != nil || len(page.Operations) != 4 {
		t.Fatal("staking operations missing from ordinary payment journal", err)
	}
	for _, operation := range page.Operations {
		if operation.Signed != nil {
			t.Fatal("public journal exposed signed broadcast payload")
		}
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	restored, err := e.Operation(lastPlan.ID)
	if err != nil || restored.State != "UNKNOWN" || restored.Signed == nil || !bytes.Equal(restored.Signed.Bytes, last.Bytes) || restored.Signed.Identity != last.Identity {
		t.Fatal("restart changed the unresolved original transaction")
	}
	replacement := stakingSignerPlan(account, "deposit_and_stake")
	replacement.ID = "forbidden-unresolved-replacement"
	replacement.Nonce = "99"
	if err = e.Prepare(replacement); err == nil {
		t.Fatal("ambiguous staking operation lost its nonce/account reservation")
	}
}

func TestNEARStakingSignerRejectsArbitraryMethodsDepositsAndReplay(t *testing.T) {
	e, ui, _ := newEngine(t, []model.Adapter{near.New()})
	createImported(t, e, ui, "testnet")
	p := stakingSignerPlan(findAccount(t, e, "near-testnet"), "unstake")
	for name, changed := range map[string]model.Plan{
		"arbitrary method":          stakingMutation(p, "method", "add_full_access_key"),
		"unstake attached value":    stakingMutation(p, "deposit", "1"),
		"excess gas":                stakingMutation(p, "gas", "300000000000000"),
		"arguments add recipient":   stakingMutation(p, "args", `{"amount":"1000000000000000000000000","recipient_id":"attacker.testnet"}`),
		"different argument amount": stakingMutation(p, "args", `{"amount":"2000000000000000000000000"}`),
		"duplicate amount":          stakingMutation(p, "args", `{"amount":"1000000000000000000000000","amount":"1000000000000000000000000"}`),
		"zero stake deposit":        stakingMutation(stakingSignerPlan(p.Account, "deposit_and_stake"), "deposit", "0"),
		"all attached value":        stakingMutation(stakingSignerPlan(p.Account, "withdraw_all"), "deposit", "1"),
	} {
		t.Run(name, func(t *testing.T) {
			if e.Prepare(changed) == nil {
				t.Fatal("unsafe staking intent entered durable authorization")
			}
		})
	}
	if len(e.Pending()) != 0 {
		t.Fatal("rejected staking intent changed operation journal")
	}
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, input("WRONG-public-synthetic-password"))
	if signed, err := e.Sign(p.ID); err == nil || len(signed.Bytes) != 0 {
		t.Fatal("unlocked wallet signed without current password")
	}
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if _, err := e.Sign(p.ID); err == nil {
		t.Fatal("cancelled wrong-password authorization was replayed")
	}
	if operation, _ := e.Operation(p.ID); operation.State != "CANCELLED" || operation.Signed != nil {
		t.Fatal("wrong-password staking request remained signable")
	}
}

type stakingReviewUI struct {
	*testUI
	beforeInput func()
}

type stakingSignCounter struct {
	*near.Adapter
	signs int
}

func (counter *stakingSignCounter) Sign(seed []byte, plan model.Plan) (model.Signed, error) {
	counter.signs++
	return counter.Adapter.Sign(seed, plan)
}

func (ui *stakingReviewUI) Input(title string, fields ...string) ([][]byte, error) {
	if ui.beforeInput != nil {
		hook := ui.beforeInput
		ui.beforeInput = nil
		hook()
	}
	return ui.testUI.Input(title, fields...)
}

func TestNEARStakingSignerRejectsPlanChangeAcrossConfirmation(t *testing.T) {
	counter := &stakingSignCounter{Adapter: near.New()}
	e, baseUI, _ := newEngine(t, []model.Adapter{counter})
	createImported(t, e, baseUI, "testnet")
	p := stakingSignerPlan(findAccount(t, e, "near-testnet"), "unstake_all")
	if err := e.Prepare(p); err != nil {
		t.Fatal(err)
	}
	baseUI.inputs = append(baseUI.inputs, input(syntheticPassword))
	e.UI = &stakingReviewUI{testUI: baseUI, beforeInput: func() {
		// A different valid allowlisted operation still needs its own exact
		// confirmation. Replacing a journal plan cannot inherit prior consent.
		e.data.Operations[0].Plan = stakingMutation(p, "method", "withdraw_all")
	}}
	if signed, err := e.Sign(p.ID); err == nil || len(signed.Bytes) != 0 {
		t.Fatal("plan replacement during approval released a signature")
	}
	if counter.signs != 0 {
		t.Fatal("changed staking plan reached private signing")
	}
	if !e.Snapshot().Locked {
		t.Fatal("changed journal digest did not quarantine the signer")
	}
	baseUI.inputs = append(baseUI.inputs, input(syntheticPassword))
	restarted := New(e.Files, baseUI, []model.Adapter{near.New()})
	restarted.parameters = e.parameters
	defer restarted.Lock()
	if err := restarted.Unlock(); err != nil {
		t.Fatal(err)
	}
	operation, err := restarted.Operation(p.ID)
	if err != nil || operation.Signed != nil || operation.State != "CANCELLED" || operation.Digest != journal.Digest(p) || journal.Digest(operation.Plan) != operation.Digest {
		t.Fatal("recovering the prior durable plan did not cancel the interrupted authorization")
	}
}
