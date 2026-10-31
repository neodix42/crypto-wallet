package nativewallet

import (
	"errors"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/webui"
)

type tonObservation struct {
	State           model.TONWalletState
	Loading, Failed bool
	FailureCode     string
}

func validTONState(state model.TONWalletState, account model.Account) bool {
	var meta ton.Parameters
	if strictjson.Decode(account.Parameters, &meta) != nil {
		return false
	}
	if state.AccountID != account.ID || state.Network != account.Network || state.Address != account.Address || state.Version != meta.Version || state.WalletID != strconv.FormatUint(uint64(meta.WalletID), 10) || state.Subwallet != strconv.FormatUint(uint64(meta.Subwallet), 10) {
		return false
	}
	balance, err := amount.Atomic(state.Balance)
	height, e := strconv.ParseUint(state.At, 10, 32)
	if err != nil || e != nil || balance.BitLen() > 120 || height == 0 {
		return false
	}
	switch state.Status {
	case "active", "frozen":
		return !state.CanDeploy
	case "uninit":
		return state.CanDeploy == (balance.Cmp(big.NewInt(10_000_000)) > 0)
	}
	return false
}

func (a *App) readTONState(account model.Account) {
	if a.closed || a.state.Locked || account.Family != "ton" {
		return
	}
	if a.tonStates == nil {
		a.tonStates = map[string]tonObservation{}
	}
	if a.tonStates[account.ID].Loading {
		return
	}
	a.tonStates[account.ID] = tonObservation{Loading: true}
	revision, epoch, g := a.tonRevision, a.epoch, a.core.Generation()
	a.render()
	go func() {
		var state model.TONWalletState
		err := a.callAt(g, "tonWalletState", map[string]string{"accountId": account.ID}, &state)
		if err == nil && !validTONState(state, account) {
			err = errors.New("invalid TON contract observation")
		}
		a.ui.Dispatch(func() {
			if a.closed || a.state.Locked || a.epoch != epoch || a.tonRevision != revision {
				return
			}
			if a.core.Generation() != g {
				a.lock()
				return
			}
			current, ok := a.account(account.ID)
			if !ok || !reflect.DeepEqual(current, account) {
				return
			}
			if err != nil {
				a.tonStates[account.ID] = tonObservation{Failed: true, FailureCode: ipc.PublicErrorCode(err)}
			} else {
				a.tonStates[account.ID] = tonObservation{State: state}
			}
			a.render()
		})
	}()
}

func (a *App) tonEvent(kind, id string) bool {
	if kind != "tonState" && kind != "tonDeploy" && kind != "tonDeploySend" {
		return false
	}
	account, ok := a.account(id)
	if !ok || account.Family != "ton" || a.state.Locked || a.busy || !a.state.RecoveryConfirmed {
		return true
	}
	if kind == "tonState" {
		a.readTONState(account)
		return true
	}
	if kind == "tonDeploySend" {
		if pending, ok := a.tonPending[id]; ok {
			if !validTONPending(pending, account) {
				a.notice("This deployment preview expired or its account changed. Cancel it or review the original operation in Journal.")
				a.render()
				return true
			}
			a.paymentAction("send", pending.ID)
		}
		return true
	}
	if _, pending := a.tonPending[id]; pending {
		a.notice("Review or cancel the prepared deployment first.")
		a.render()
		return true
	}
	observed, ok := a.tonStates[id]
	if !ok || observed.Loading || observed.Failed || !validTONState(observed.State, account) || !observed.State.CanDeploy {
		return true
	}
	a.work("Preparing exact TON deployment…", func(g uint64) (func(), error) {
		var plan model.Plan
		err := a.callAt(g, "prepareTonDeployment", map[string]string{"accountId": id}, &plan)
		if err == nil && (!reflect.DeepEqual(plan.Account, account) || !ton.IsDeploymentPlan(plan) || ton.New().ValidatePlan(plan) != nil || plan.Expires <= time.Now().Unix()) {
			err = ipc.NewRequestError("TON_DEPLOYMENT_INVALID_PLAN")
		}
		return func() {
			if a.tonPending == nil {
				a.tonPending = map[string]model.Plan{}
			}
			a.tonPending[id] = plan
		}, err
	})
	return true
}

func (a *App) tonNodes(account model.Account) []webui.Node {
	observed, ok := a.tonStates[account.ID]
	text := "Contract status: unknown"
	if ok {
		switch {
		case observed.Loading:
			text = "Contract status: checking…"
		case observed.Failed:
			text = "Contract status: unavailable — RPC query failed"
			if observed.FailureCode == "TON_RPC_RATE_LIMITED" {
				text = "Contract status: unavailable — TON provider rate limit; wait briefly or configure a trusted RPC/API key"
			}
		default:
			text = "Contract status: " + observed.State.Status + " · Wallet " + observed.State.Version + " · subwallet " + observed.State.Subwallet + " · on-chain wallet ID " + observed.State.WalletID
		}
	}
	status := label(text)
	status.ID = "tonContractStatus/" + account.ID
	nodes := []webui.Node{status, a.button("tonState/"+account.ID, "Refresh contract status", observed.Loading)}
	if ok && !observed.Loading && !observed.Failed && observed.State.Status == "uninit" {
		nodes = append(nodes, label(observed.State.Reason), a.button("tonDeploy/"+account.ID, "Deploy Wallet V5R1 contract", !a.state.RecoveryConfirmed || !observed.State.CanDeploy))
	}
	if pending, ok := a.tonPending[account.ID]; ok {
		fee, err := amount.Format(pending.MaxFee, 9)
		if err != nil {
			fee = "Unknown"
		}
		nodes = append(nodes, heading("Prepared TON deployment"), label(ton.DeploymentEffects(pending)), label("Estimated fee reserve: "+trimDecimal(fee)+" GRAM (not a signed fee cap)"), row(a.button("tonDeploySend/"+account.ID, "Review and deploy", !a.state.RecoveryConfirmed || !validTONPending(pending, account)), a.button("cancelPrepared/"+pending.ID, "Cancel deployment", false)))
	}
	return nodes
}

func validTONPending(plan model.Plan, account model.Account) bool {
	return reflect.DeepEqual(plan.Account, account) && ton.IsDeploymentPlan(plan) && ton.New().ValidatePlan(plan) == nil && plan.Expires > time.Now().Unix()
}

// Called after a successful account snapshot or an operation on the selected
// wallet, so ordinary Refresh and deployment reconciliation update its status.
func (a *App) refreshActiveTONState() {
	if account, ok := a.account(strings.TrimPrefix(a.active, "wallet/")); ok && account.Family == "ton" {
		a.readTONState(account)
	}
}
