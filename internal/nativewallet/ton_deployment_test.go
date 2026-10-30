package nativewallet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

func tonFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account) {
	t.Helper()
	a, ui, core := fixture(t)
	account, err := ton.New().Derive(bytes.Repeat([]byte{0x56}, 64), 0, "ton-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	for i, old := range a.state.Accounts {
		if old.Family == "ton" {
			a.state.Accounts[i] = account
		}
	}
	return a, ui, core, account
}
func tonStateFixture(t *testing.T, account model.Account, status, balance string, deploy bool) model.TONWalletState {
	t.Helper()
	var meta ton.Parameters
	if err := json.Unmarshal(account.Parameters, &meta); err != nil {
		t.Fatal(err)
	}
	return model.TONWalletState{AccountID: account.ID, Network: account.Network, Address: account.Address, Status: status, Balance: balance, Version: meta.Version, WalletID: strconv.FormatUint(uint64(meta.WalletID), 10), Subwallet: strconv.FormatUint(uint64(meta.Subwallet), 10), CanDeploy: deploy, At: "100"}
}

func nativeTONDeploymentPlan(account model.Account) model.Plan {
	payload, _ := json.Marshal(ton.Payload{Kind: "deployment", Deploy: true, SourceBalance: "1000000000", EstimatedFee: "1000000", StorageReserve: "10000000", MasterSeqno: 123})
	plan := model.Plan{Account: account, Asset: account.Network + ":TON", Recipient: account.Address, Amount: "0", MaxFee: "3000000", FeeAsset: account.Network + ":TON", Nonce: "0", Expires: time.Now().Add(time.Minute).Unix(), Payload: payload}
	raw, _ := json.Marshal(plan)
	hash := sha256.Sum256(raw)
	plan.ID = hex.EncodeToString(hash[:])
	return plan
}

func TestTONDeploymentControllerPreparesWithoutSendingAndKeepsExactOperationForReviewCancel(t *testing.T) {
	for _, action := range []string{"tonDeploySend", "cancelPrepared"} {
		t.Run(action, func(t *testing.T) {
			a, ui, core, account := tonFixture(t)
			a.active = "wallet/" + account.ID
			a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "1000000000", true)}}
			plan := nativeTONDeploymentPlan(account)
			routeFixture(t, a, core, "prepareTonDeployment", map[string]string{"accountId": account.ID}, plan)
			a.tonEvent("tonDeploy", account.ID)
			ui.apply(t)
			if pending, ok := a.tonPending[account.ID]; !ok || !reflect.DeepEqual(pending, plan) {
				t.Fatal("prepared deployment lost immutable original operation")
			}
			page := a.tonNodes(account)
			for _, exact := range []string{"no outgoing transfers", account.Address, "0.003 GRAM", "not a signed fee cap"} {
				if !strings.Contains(viewText(page), exact) {
					t.Fatal("deployment preview omitted exact effect", exact)
				}
			}
			method := "send"
			if action == "cancelPrepared" {
				method = action
			}
			var calls atomic.Int32
			core.handler = func(_ uint64, actual string, body, out any) error {
				if actual == "tonWalletState" {
					return errors.New("synthetic offline post-operation status")
				}
				calls.Add(1)
				if actual != method || viewText(body) != `{"id":"`+plan.ID+`"}` {
					return errors.New("original TON deployment identity changed")
				}
				return answer(out, json.RawMessage(`{"state":"UNKNOWN"}`))
			}
			if action == "tonDeploySend" {
				a.tonEvent(action, account.ID)
			} else {
				a.walletEvent("cancelPrepared/" + plan.ID)
			}
			ui.apply(t)
			ui.apply(t) // Post-operation contract refresh has its own callback.
			if calls.Load() != 1 || len(a.tonPending) != 0 || a.resultOwner != account.ID || !a.tonStates[account.ID].Failed {
				t.Fatal("explicit deployment review/cancel bypassed original operation")
			}
		})
	}
}

func TestTONDeploymentExpiredOrChangedPreviewKeepsCancellationWithoutSigning(t *testing.T) {
	for _, mode := range []string{"expired", "changed-account"} {
		t.Run(mode, func(t *testing.T) {
			a, _, core, account := tonFixture(t)
			plan := nativeTONDeploymentPlan(account)
			if mode == "expired" {
				plan.Expires = time.Now().Add(-time.Second).Unix()
			} else {
				plan.Account.Address = "another-contract"
			}
			a.tonPending = map[string]model.Plan{account.ID: plan}
			core.handler = func(_ uint64, method string, body, out any) error {
				t.Error("invalid deployment preview reached signing", method)
				return nil
			}
			a.tonEvent("tonDeploySend", account.ID)
			button, _ := findNode(a.tonNodes(account), "tonDeploySend/"+account.ID)
			cancel, exists := findNode(a.tonNodes(account), "cancelPrepared/"+plan.ID)
			if !button.Disabled || !exists || cancel.Disabled || len(a.tonPending) != 1 {
				t.Fatal("invalid deployment discarded original cancellation or allowed signing")
			}
		})
	}
}
func TestTONWalletStatusFundingAndDeploymentButton(t *testing.T) {
	for _, row := range []struct {
		status, balance string
		deploy          bool
	}{{"uninit", "0", false}, {"uninit", "100000000", true}, {"active", "100000000", false}, {"frozen", "100000000", false}} {
		t.Run(row.status+row.balance, func(t *testing.T) {
			a, ui, core, account := tonFixture(t)
			state := tonStateFixture(t, account, row.status, row.balance, row.deploy)
			routeFixture(t, a, core, "tonWalletState", map[string]string{"accountId": account.ID}, state)
			a.readTONState(account)
			ui.apply(t)
			page := a.walletPage(account)
			if !strings.Contains(viewText(page), "Contract status: "+row.status) {
				t.Fatal(viewText(page))
			}
			button, ok := findNode(page.Nodes, "tonDeploy/"+account.ID)
			if row.status == "uninit" {
				if !ok || button.Disabled == row.deploy {
					t.Fatal("funding gate mismatch", button)
				}
			} else if ok {
				t.Fatal("active/frozen wallet can redeploy")
			}
		})
	}
}
func TestTONStateRejectsForeignDescriptorAndUnsafeFundingClaims(t *testing.T) {
	_, _, _, account := tonFixture(t)
	good := tonStateFixture(t, account, "uninit", "100000000", true)
	if !validTONState(good, account) {
		t.Fatal("valid observation rejected")
	}
	for _, mutate := range []func(*model.TONWalletState){func(s *model.TONWalletState) { s.AccountID = "foreign" }, func(s *model.TONWalletState) { s.Network = "ton-testnet" }, func(s *model.TONWalletState) { s.Address = "foreign" }, func(s *model.TONWalletState) { s.WalletID = "42" }, func(s *model.TONWalletState) { s.Version = "V4R2" }, func(s *model.TONWalletState) { s.Balance = "0" }, func(s *model.TONWalletState) { s.At = "0" }, func(s *model.TONWalletState) { s.Status = "active" }} {
		bad := good
		mutate(&bad)
		if validTONState(bad, account) {
			t.Fatal("unbound/unsafe state accepted", bad)
		}
	}
}
func TestTONLateStatusCannotSurviveLockOrGenerationChange(t *testing.T) {
	for _, lock := range []bool{true, false} {
		a, ui, core, account := tonFixture(t)
		release := make(chan struct{})
		core.handler = func(_ uint64, method string, body, out any) error {
			<-release
			return answer(out, tonStateFixture(t, account, "uninit", "100000000", true))
		}
		a.readTONState(account)
		if lock {
			a.lock()
		} else {
			core.generation.Add(1)
		}
		close(release)
		ui.apply(t)
		if !a.state.Locked || len(a.tonStates) != 0 {
			t.Fatal("late TON observation revived session")
		}
	}
}
func TestTONDeployCannotPrepareWithoutVerifiedFunding(t *testing.T) {
	a, _, core, account := tonFixture(t)
	core.handler = func(_ uint64, method string, body, out any) error {
		t.Error("unfunded/unobserved deployment reached backend")
		return nil
	}
	a.tonEvent("tonDeploy", account.ID)
	a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "0", false)}}
	a.tonEvent("tonDeploy", account.ID)
	if a.busy {
		t.Fatal("unfunded deployment queued")
	}
	a.tonStates[account.ID] = tonObservation{State: tonStateFixture(t, account, "active", "100000000", false)}
	a.tonEvent("tonDeploy", account.ID)
	if a.busy || len(a.tonPending) != 0 {
		t.Fatal("active deployment queued")
	}
}
