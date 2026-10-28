package nativewallet

import (
	"encoding/json"
	"errors"
	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/webui"
	"strings"
	"time"
)

const astroStakersMainnet = "astro-stakers.poolv1.near"

func (a *App) stakingPool(account model.Account) string {
	if pool, chosen := a.fields["stakingPool/"+account.ID]; chosen {
		return pool
	}
	if account.Network == "near-mainnet" {
		return astroStakersMainnet
	}
	return ""
}

func validStakingState(state near.StakingState, account model.Account, pool string) bool {
	if state.AccountID != account.Address || state.Validator != pool || state.BlockHash == "" || state.BlockHeight == "" {
		return false
	}
	for _, value := range []string{state.Staked, state.Unstaked} {
		n, err := amount.Atomic(value)
		if err != nil || n.BitLen() > 128 {
			return false
		}
	}
	_, err := amount.Atomic(state.BlockHeight)
	return err == nil
}

func nearUnits(atomic string) string {
	formatted, err := amount.Format(atomic, 24)
	if err != nil {
		return "Unknown"
	}
	return trimDecimal(formatted) + " NEAR"
}

func (a *App) pendingStaking(accountID string) (model.Plan, bool) {
	if pending, ok := a.stakingPending[accountID]; ok {
		return pending, true
	}
	if a.plan != nil && a.planIsStaking && a.plan.Account.ID == accountID {
		return *a.plan, true
	}
	return model.Plan{}, false
}

// Staking plans describe effects inside the pool, not ordinary transfers to its
// contract. In particular, a withdrawal returns funds to the account, and ALL
// amounts are observations at preparation rather than fixed signed quantities.
func stakingJournalDescription(plan model.Plan) (string, bool) {
	if plan.Account.Family != "near" {
		return "", false
	}
	var payload near.Payload
	if json.Unmarshal(plan.Payload, &payload) != nil || payload.Staking == nil {
		return "", false
	}
	call := payload.Staking
	description := "Network: " + plan.Account.Network + "\nAccount: " + plan.Account.Address + "\nStaking pool contract: " + plan.Recipient + "\nMethod: " + call.Method + "\nAttached NEAR: " + nearUnits(call.Deposit) + "\nGas: " + call.Gas + " gas units"
	switch call.Method {
	case "deposit_and_stake":
		description += "\nPlanned effect: move " + nearUnits(plan.Amount) + " from this wallet into its staked pool balance."
	case "unstake":
		description += "\nPlanned effect: move " + nearUnits(plan.Amount) + " from staked to unstaked balance inside the pool. Withdrawal is separate."
	case "unstake_all":
		description += "\nObserved all-funds amount at preparation: " + nearUnits(plan.Amount) + " (actual amount follows contract execution)\nPlanned effect: unstake all; funds remain inside the pool until withdrawn."
	case "withdraw_all":
		description += "\nObserved all-funds amount at preparation: " + nearUnits(plan.Amount) + " (actual amount follows contract execution)\nPlanned effect: return all available unstaked funds from the pool to this wallet.\nWithdrawal destination: " + plan.Account.Address
	default:
		description += "\nUnsupported staking method. No transfer effect has been established."
	}
	return description, true
}

// stakingEvent accepts only the four typed staking intents. Contract arguments,
// gas, attached deposit and approval text are constructed and validated in Go.
func (a *App) stakingEvent(kind, id string) bool {
	if kind == "walletSection" {
		parts := strings.SplitN(id, "/", 2)
		if len(parts) != 2 || parts[1] != "Wallet" && parts[1] != "Staking" || a.state.Locked || a.busy {
			return true
		}
		account, ok := a.account(parts[0])
		if !ok || account.Family != "near" {
			return true
		}
		a.fields["walletSection/"+account.ID] = parts[1]
		a.render()
		return true
	}
	if kind != "stakingState" && kind != "stakingPrepare" && kind != "stakingSend" {
		return false
	}
	parts := strings.SplitN(id, "/", 2)
	account, ok := a.account(parts[0])
	if !ok || account.Family != "near" || a.state.Locked || a.busy {
		return true
	}
	pool := a.stakingPool(account)
	if kind == "stakingSend" {
		if pending, ok := a.pendingStaking(account.ID); ok && a.state.RecoveryConfirmed {
			a.paymentAction("send", pending.ID)
		}
		return true
	}
	if pool == "" {
		a.notice("Enter the exact staking pool contract for this NEAR network.")
		a.render()
		return true
	}
	revision := a.stakingRevision
	if kind == "stakingState" {
		if a.staking == nil {
			a.staking = map[string]near.StakingState{}
		}
		if a.stakingErrors == nil {
			a.stakingErrors = map[string]bool{}
		}
		delete(a.staking, account.ID)
		a.stakingErrors[account.ID] = true
		a.stakingReading = account.ID
		a.work("Reading staking pool account state…", func(g uint64) (func(), error) {
			var state near.StakingState
			err := a.callAt(g, "nearStakingState", map[string]string{"accountId": account.ID, "pool": pool}, &state)
			if err == nil && !validStakingState(state, account, pool) {
				err = errors.New("invalid staking observation")
			}
			return func() {
				if revision != a.stakingRevision || pool != a.stakingPool(account) {
					return
				}
				a.staking[account.ID] = state
				delete(a.stakingErrors, account.ID)
			}, err
		})
		return true
	}
	if !a.state.RecoveryConfirmed || len(parts) != 2 {
		return true
	}
	if _, pending := a.pendingStaking(account.ID); pending {
		a.notice("Review or cancel the original prepared staking operation before preparing another one.")
		a.render()
		return true
	}
	action := parts[1]
	switch action {
	case "stake", "unstake", "unstake_all", "withdraw_all":
	default:
		return true
	}
	value := ""
	if action == "stake" || action == "unstake" {
		value = a.fields["stakingAmount/"+account.ID]
		parsed, err := amount.Parse(value, 24)
		if err != nil || parsed == "0" {
			a.notice("Enter a positive NEAR amount with at most 24 decimal places.")
			a.render()
			return true
		}
	}
	planRevision := a.planRevision
	a.work("Preparing exact NEAR staking operation…", func(g uint64) (func(), error) {
		var plan model.Plan
		err := a.callAt(g, "prepareNearStaking", map[string]string{"accountId": account.ID, "pool": pool, "action": action, "amount": value}, &plan)
		if err == nil && (plan.Account.ID != account.ID || plan.Recipient != pool || plan.Account.Network != account.Network) {
			err = errors.New("invalid staking plan binding")
		}
		return func() {
			if a.stakingPending == nil {
				a.stakingPending = map[string]model.Plan{}
			}
			// The signer has durably reserved this exact unsigned operation.
			// Even if its form changed in flight, retain its ID and explicit
			// cancellation/review controls instead of orphaning the reservation.
			a.stakingPending[account.ID] = plan
			if revision != a.stakingRevision || planRevision != a.planRevision || pool != a.stakingPool(account) {
				return
			}
			a.plan = &plan
			a.planIsStaking = true
		}, err
	})
	return true
}

func (a *App) stakingNodes(account model.Account) []webui.Node {
	pool := a.stakingPool(account)
	pending, hasPending := a.pendingStaking(account.ID)
	contract := a.entry("stakingPool/"+account.ID, "Validator / staking pool contract")
	contract.Value = pool
	contract.Disabled = contract.Disabled || hasPending
	nodes := []webui.Node{heading("NEAR Wallet · Staking")}
	if pool == astroStakersMainnet && account.Network == "near-mainnet" {
		nodes = append(nodes, label("Validator: AstroStakers"))
	}
	nodes = append(nodes, contract, node("address", "", "Exact contract · "+account.Network, pool), label("Only deposit_and_stake, unstake, unstake_all and withdraw_all are supported. Every transaction requires its own private review and app password."))
	if account.Network != "near-mainnet" && pool == "" {
		nodes = append(nodes, label("Enter a deployed staking pool on this test/local network. A mainnet validator address is never reused automatically."))
	}
	state, observed := a.staking[account.ID]
	observed = observed && !a.stakingErrors[account.ID] && validStakingState(state, account, pool)
	staked, unstaked, ready := "Unknown", "Unknown", "Unknown"
	if observed {
		staked, unstaked = nearUnits(state.Staked), nearUnits(state.Unstaked)
		switch {
		case state.Unstaked == "0":
			ready = "No unstaked funds"
		case state.CanWithdraw:
			ready = "Available · " + unstaked
		default:
			ready = "Waiting for the pool unlock period"
		}
	}
	nodes = append(nodes, row(a.button("stakingState/"+account.ID, "Refresh staking state", pool == "")), label("Staked: "+staked), label("Unstaked / waiting: "+unstaked), label("Ready to withdraw: "+ready), label("Rewards: unavailable separately from get_account. Accrued rewards are included in the staked balance."))
	if a.stakingReading == account.ID {
		nodes = append(nodes, label("Reading the pool account state… No new pool balance has been verified yet."))
	} else if a.stakingErrors[account.ID] {
		nodes = append(nodes, label("Staking state is unavailable. No pool balance or withdrawal availability has been verified."))
	} else if observed {
		observation := label("Observed pool state at block " + state.BlockHeight)
		observation.Tooltip = "RPC observation, not independent consensus verification. Block hash: " + state.BlockHash
		nodes = append(nodes, observation)
	} else {
		nodes = append(nodes, label("Refresh staking state to read this wallet's get_account result from the selected pool."))
	}
	nodes = append(nodes, label("Staked funds leave the available wallet balance and remain visible here. Unstaking normally unlocks after four epochs; the exact wait follows the pool's state. Withdrawal requires a separate transaction and NEAR for gas."))
	if !a.state.RecoveryConfirmed {
		nodes = append(nodes, label("Record and confirm the recovery phrase before authorizing staking."), a.button("confirmRecovery", "Record and confirm recovery phrase", false))
	}
	disabled := !a.state.RecoveryConfirmed || pool == "" || hasPending
	value := a.entry("stakingAmount/"+account.ID, "Amount (NEAR; 1 means 1 NEAR)")
	value.Disabled = value.Disabled || hasPending
	nodes = append(nodes, value, row(a.button("stakingPrepare/"+account.ID+"/stake", "Stake", disabled), a.button("stakingPrepare/"+account.ID+"/unstake", "Unstake amount", disabled || !observed || state.Staked == "0"), a.button("stakingPrepare/"+account.ID+"/unstake_all", "Unstake all", disabled || !observed || state.Staked == "0"), a.button("stakingPrepare/"+account.ID+"/withdraw_all", "Withdraw all", disabled || !observed || state.Unstaked == "0" || !state.CanWithdraw)))
	if hasPending {
		nodes = append(nodes, heading("Original prepared staking operation"), label("Operation ID: "+pending.ID+"\nReserved contract: "+pending.Recipient+"\nReview or cancel this original operation before changing its form. Unresolved signed operations require reconciliation in Settings → Journal."))
		var payload struct {
			Staking struct {
				Method  string `json:"method"`
				Args    string `json:"args"`
				Deposit string `json:"deposit"`
				Gas     string `json:"gas"`
			} `json:"staking"`
		}
		if json.Unmarshal(pending.Payload, &payload) == nil && payload.Staking.Method != "" {
			effect := "Amount: " + nearUnits(pending.Amount)
			if payload.Staking.Method == "unstake_all" || payload.Staking.Method == "withdraw_all" {
				effect = "Observed all-funds amount: " + nearUnits(pending.Amount) + " (actual amount follows contract execution)"
			}
			nodes = append(nodes, heading("Prepared staking operation"), label("Network: "+account.Network+"\nFrom: "+account.Address+"\nExact contract: "+pending.Recipient+"\nMethod: "+payload.Staking.Method+"\n"+effect+"\nAttached NEAR: "+nearUnits(payload.Staking.Deposit)+"\nGas: "+payload.Staking.Gas+" gas units\nEstimated fee reserve: "+nearUnits(pending.MaxFee)+" (not a signed fee cap)\nArguments: "+payload.Staking.Args+"\nExpires: "+time.Unix(pending.Expires, 0).UTC().Format(time.RFC3339)), a.button("stakingSend/"+account.ID, "Review and confirm staking transaction", !a.state.RecoveryConfirmed))
		}
		nodes = append(nodes, a.button("cancelPrepared/"+pending.ID, "Cancel prepared staking operation", false))
	}
	if a.result != "" && a.resultOwner == account.ID {
		nodes = append(nodes, label(a.result))
	}
	return nodes
}
