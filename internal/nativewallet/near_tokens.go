package nativewallet

import (
	"errors"
	"strconv"
	"strings"

	"github.com/decred/base58"
	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/webui"
)

type nearTokenStorageObservation struct {
	State   near.TokenStorageState
	Failed  bool
	Reading bool
}

func (a *App) clearNearTokenStorage(clearPending bool) {
	a.nearTokenStorage = nil
	a.nearTokenRevision++
	if clearPending {
		a.nearTokenPending = nil
	}
}

func validNearTokenStorage(state near.TokenStorageState, account model.Account, asset model.Asset) bool {
	hash := base58.Decode(state.BlockHash)
	if state.Network != account.Network || state.Asset != asset.ID || state.Contract != asset.Contract || state.AccountID != account.Address || len(hash) != 32 || base58.Encode(hash) != state.BlockHash {
		return false
	}
	if height, err := strconv.ParseUint(state.BlockHeight, 10, 64); err != nil || height == 0 || strconv.FormatUint(height, 10) != state.BlockHeight {
		return false
	}
	minimum, e1 := amount.Atomic(state.MinDeposit)
	total, e2 := amount.Atomic(state.Total)
	available, e3 := amount.Atomic(state.Available)
	maximum, _ := amount.Atomic(near.MaxTokenStorageDeposit)
	if e1 != nil || e2 != nil || e3 != nil || minimum.Sign() == 0 || minimum.Cmp(maximum) > 0 || total.BitLen() > 128 || available.BitLen() > 128 || available.Cmp(total) > 0 {
		return false
	}
	if !state.Registered {
		return total.Sign() == 0 && available.Sign() == 0
	}
	return total.Cmp(minimum) >= 0
}

func (a *App) readNearTokenStorage(account model.Account, asset model.Asset) {
	if a.closed || a.state.Locked || a.busy || !nearTokenNetwork(account) || asset.Contract == "" {
		return
	}
	key := account.ID + "/" + asset.ID
	if a.nearTokenStorage == nil {
		a.nearTokenStorage = map[string]nearTokenStorageObservation{}
	}
	a.nearTokenStorage[key] = nearTokenStorageObservation{Reading: true}
	revision := a.nearTokenRevision
	a.work("Checking token registration…", func(g uint64) (func(), error) {
		var state near.TokenStorageState
		err := a.callAt(g, "nearTokenStorage", map[string]string{"accountId": account.ID, "asset": asset.ID}, &state)
		if err == nil && !validNearTokenStorage(state, account, asset) {
			err = errors.New("invalid token registration observation")
		}
		// A failed observation is an explicit unknown state. It cannot leave a
		// previous successful registration or deposit estimate on the screen.
		observation := nearTokenStorageObservation{State: state, Failed: err != nil}
		if observation.Failed {
			observation.State = near.TokenStorageState{}
		}
		return func() {
			if revision != a.nearTokenRevision {
				return
			}
			if a.nearTokenStorage == nil {
				a.nearTokenStorage = map[string]nearTokenStorageObservation{}
			}
			a.nearTokenStorage[key] = observation
			if observation.Failed {
				a.notice("Token registration is unavailable. No registration or storage deposit has been verified.")
			}
		}, nil
	})
}

func (a *App) nearTokenEvent(kind, id string) bool {
	if kind != "nearTokenStorage" && kind != "nearTokenRegister" && kind != "nearTokenRegisterSend" {
		return false
	}
	account, ok := a.account(id)
	if !ok || !nearTokenNetwork(account) || a.state.Locked || a.busy || !a.state.RecoveryConfirmed {
		return true
	}
	if kind == "nearTokenRegisterSend" {
		if pending, ok := a.nearTokenPending[id]; ok {
			a.paymentAction("send", pending.ID)
		}
		return true
	}
	asset := a.asset(account)
	if asset.Contract == "" {
		return true
	}
	if kind == "nearTokenStorage" {
		a.readNearTokenStorage(account, asset)
		return true
	}
	if _, pending := a.nearTokenPending[id]; pending {
		a.notice("Review or cancel the original token registration before preparing another one.")
		a.render()
		return true
	}
	a.work("Preparing exact token registration…", func(g uint64) (func(), error) {
		var plan model.Plan
		err := a.callAt(g, "prepareNearTokenRegistration", map[string]string{"accountId": id, "asset": asset.ID}, &plan)
		var payload near.Payload
		if err == nil && (plan.ID == "" || plan.Account.ID != id || plan.Account.Network != account.Network || plan.Account.Address != account.Address || plan.Asset != account.Network+":native" || plan.FeeAsset != plan.Asset || plan.Recipient != account.Address || !near.IsTokenRegistrationPlan(plan) || strictjson.Decode(plan.Payload, &payload) != nil || payload.Token == nil || payload.Token.Policy.Asset != asset || payload.Token.Policy.Network != account.Network || near.ValidateTokenPolicy(payload.Token.Policy) != nil || payload.Token.Deposit != plan.Amount) {
			err = errors.New("invalid token registration plan binding")
		}
		return func() {
			if a.nearTokenPending == nil {
				a.nearTokenPending = map[string]model.Plan{}
			}
			// Keep the durable operation's ID even if the selected token changed.
			// Neither selection nor editing may orphan its nonce reservation.
			a.nearTokenPending[id] = plan
		}, err
	})
	return true
}

func (a *App) nearTokenNodes(account model.Account, asset model.Asset) []webui.Node {
	var nodes []webui.Node
	if asset.Contract != "" {
		nodes = append(nodes, heading("Receive "+asset.Symbol), node("address", "", "NEP-141 contract", asset.Contract), label("Tokens use the owned receive address above. NEAR pays gas and token-contract registration; a token contract is not a receiving address."))
		observation, exists := a.nearTokenStorage[account.ID+"/"+asset.ID]
		observed := exists && !observation.Failed && !observation.Reading && validNearTokenStorage(observation.State, account, asset)
		switch {
		case exists && observation.Reading:
			nodes = append(nodes, label("Checking token registration…"))
		case observed && observation.State.Registered:
			nodes = append(nodes, label("This address is registered to receive this token."))
		case observed:
			nodes = append(nodes, label("Register this address before receiving this token. Fund the wallet with native NEAR first; a sender may register it instead. Required storage deposit: "+nearUnits(observation.State.MinDeposit)))
		default:
			nodes = append(nodes, label("Token registration is unknown. Check it before receiving; an unregistered address cannot receive this token."))
		}
		_, pending := a.nearTokenPending[account.ID]
		nodes = append(nodes, row(a.button("nearTokenStorage/"+account.ID, "Check token registration", !a.state.RecoveryConfirmed), a.button("nearTokenRegister/"+account.ID, "Register to receive token", !a.state.RecoveryConfirmed || pending || !observed || observation.State.Registered)))
	}
	if pending, ok := a.nearTokenPending[account.ID]; ok {
		nodes = append(nodes, heading("Original prepared token registration"), label("Operation ID: "+pending.ID+"\nStorage deposit: "+nearUnits(pending.Amount)+"\nEstimated fee reserve: "+nearUnits(pending.MaxFee)))
		if effects, valid := near.TokenEffects(pending); valid {
			nodes = append(nodes, label(strings.TrimSpace(effects)))
		}
		nodes = append(nodes, row(a.button("nearTokenRegisterSend/"+account.ID, "Review and register", !a.state.RecoveryConfirmed), a.button("cancelPrepared/"+pending.ID, "Cancel token registration", false)))
	}
	return nodes
}
