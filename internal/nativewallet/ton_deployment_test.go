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
	"localwallet/internal/ipc"
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

func TestTONDeploymentClickOpensExactApprovalAndCancelledReviewKeepsOriginal(t *testing.T) {
	for _, profile := range []string{ton.Profile, ton.ProfileSubwallet42} {
		for _, restart := range []bool{false, true} {
			name := profile + "/same-session"
			if restart {
				name = profile + "/after-restart"
			}
			t.Run(name, func(t *testing.T) {
				a, ui, core, account := tonFixture(t)
				account, err := ton.New().DeriveProfile(bytes.Repeat([]byte{0x56}, 64), 0, account.Network, profile)
				if err != nil {
					t.Fatal(err)
				}
				for i := range a.state.Accounts {
					if a.state.Accounts[i].Family == "ton" {
						a.state.Accounts[i] = account
					}
				}
				a.active = "wallet/" + account.ID
				a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "1000000000", true)}}
				plan := nativeTONDeploymentPlan(account)
				if !restart {
					a.tonPending = map[string]model.Plan{account.ID: plan}
				}
				started, dismiss := make(chan struct{}), make(chan struct{})
				var prepareCalls, reviewCalls atomic.Int32
				core.handler = func(_ uint64, actual string, body, out any) error {
					switch actual {
					case "prepareTonDeployment":
						prepareCalls.Add(1)
						if viewText(body) != `{"accountId":"`+account.ID+`"}` {
							return errors.New("deployment account changed")
						}
						// The durable backend returns this original operation even when
						// transient UI state disappeared after restart.
						return answer(out, plan)
					case "send":
						reviewCalls.Add(1)
						if viewText(body) != `{"id":"`+plan.ID+`"}` {
							return errors.New("original TON deployment identity changed")
						}
						close(started)
						<-dismiss // Existing backend approval dialog remains open.
						return ipc.NewRequestError("REQUEST_CANCELLED")
					default:
						return errors.New("unexpected deployment route: " + actual)
					}
				}
				a.tonEvent("tonDeploy", account.ID)
				ui.apply(t)
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("one Deploy click did not open the existing exact approval")
				}
				if pending, ok := a.tonPending[account.ID]; !ok || !reflect.DeepEqual(pending, plan) || !a.busy || !strings.Contains(a.status, "exact payment review") {
					t.Fatal("direct approval lost the immutable operation or review feedback")
				}
				page := viewText(a.tonNodes(account))
				if strings.Contains(page, "Review and deploy") || strings.Contains(page, "Prepared TON deployment") || strings.Contains(page, "tonDeploySend/") {
					t.Fatal("deployment still requires a second inline review step")
				}
				// Neither a repeated click nor a delayed confirmation starts another
				// preparation, approval, cancellation or broadcast.
				a.tonEvent("tonDeploy", account.ID)
				close(dismiss)
				ui.apply(t)
				if prepareCalls.Load() != 1 || reviewCalls.Load() != 1 || !reflect.DeepEqual(a.tonPending[account.ID], plan) || a.busy || a.result != "" || a.status != "Request cancelled." {
					t.Fatal("cancelled approval discarded, replaced or sent the original operation")
				}
				if history := a.walletHistory[account.ID]; history.Request != "initial" {
					t.Fatal("cancelled review did not queue the wallet’s durable history")
				}
			})
		}
	}
}

func TestTONDeploymentRechecksSessionAndDescriptorBeforeOpeningApproval(t *testing.T) {
	for _, mode := range []string{"lock", "generation", "changed-account", "recovery-unconfirmed", "expires-in-ui-queue"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core, account := tonFixture(t)
			a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "200000000", true)}}
			plan := nativeTONDeploymentPlan(account)
			if mode == "expires-in-ui-queue" {
				plan.Expires = time.Now().Add(2 * time.Second).Unix()
				plan.ID = ""
				raw, _ := json.Marshal(plan)
				hash := sha256.Sum256(raw)
				plan.ID = hex.EncodeToString(hash[:])
			}
			var reviewCalls atomic.Int32
			core.handler = func(_ uint64, method string, body, out any) error {
				if method != "prepareTonDeployment" {
					reviewCalls.Add(1)
					return errors.New("stale deployment reached signing")
				}
				return answer(out, plan)
			}
			a.tonEvent("tonDeploy", account.ID)
			var apply func()
			select {
			case apply = <-ui.queue:
			case <-time.After(3 * time.Second):
				t.Fatal("preparation did not reach the UI lifecycle queue")
			}
			switch mode {
			case "lock":
				a.lock()
			case "generation":
				core.generation.Add(1)
			case "changed-account":
				for i := range a.state.Accounts {
					if a.state.Accounts[i].ID == account.ID {
						a.state.Accounts[i].Address = "changed-contract"
					}
				}
			case "recovery-unconfirmed":
				a.state.RecoveryConfirmed = false
			case "expires-in-ui-queue":
				deadline := time.NewTimer(time.Until(time.Unix(plan.Expires, 0)))
				<-deadline.C
			}
			apply()
			if reviewCalls.Load() != 0 || len(a.tonPending) != 0 {
				t.Fatal("stale preparation opened approval or installed a spendable operation")
			}
			if mode == "changed-account" || mode == "recovery-unconfirmed" || mode == "expires-in-ui-queue" {
				if a.busy || !strings.Contains(a.status, "transaction history") {
					t.Fatal("stale preparation lost safe history guidance", a.status)
				}
			}
		})
	}
}

func TestTONDeploymentExpiredTransientPreviewUsesBackendRefreshBeforeApproval(t *testing.T) {
	a, ui, core, account := tonFixture(t)
	a.active = "wallet/" + account.ID
	a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "200000000", true)}}
	original := nativeTONDeploymentPlan(account)
	original.Expires = time.Now().Add(-time.Minute).Unix()
	original.ID = ""
	raw, _ := json.Marshal(original)
	hash := sha256.Sum256(raw)
	original.ID = hex.EncodeToString(hash[:])
	a.tonPending = map[string]model.Plan{account.ID: original}
	fresh := nativeTONDeploymentPlan(account)
	var prepares, reviews atomic.Int32
	core.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "prepareTonDeployment":
			prepares.Add(1)
			if viewText(body) != `{"accountId":"`+account.ID+`"}` {
				return errors.New("expired preview changed its owned account")
			}
			// Only the coordinator may refresh a durably proven unsigned expired
			// deployment. The UI never cancels it or substitutes its own preview.
			return answer(out, fresh)
		case "send":
			reviews.Add(1)
			if viewText(body) != `{"id":"`+fresh.ID+`"}` || fresh.ID == original.ID {
				return errors.New("expired preview reached approval")
			}
			return ipc.NewRequestError("REQUEST_CANCELLED")
		default:
			return errors.New("UI tried to cancel or replace an operation itself")
		}
	}
	a.tonEvent("tonDeploy", account.ID)
	ui.apply(t)
	ui.apply(t)
	if prepares.Load() != 1 || reviews.Load() != 1 || !reflect.DeepEqual(a.tonPending[account.ID], fresh) || a.busy || a.status != "Request cancelled." {
		t.Fatal("explicit Deploy did not refresh through backend then open exact approval")
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
