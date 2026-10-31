package nativewallet

import (
	"encoding/json"
	"errors"
	"localwallet/internal/chains/near"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/webui"
	"reflect"
	"strings"
	"testing"
	"time"
)

func nearAccount(t *testing.T, a *App) model.Account {
	t.Helper()
	for _, account := range a.state.Accounts {
		if account.Family == "near" {
			return account
		}
	}
	t.Fatal("NEAR wallet missing")
	return model.Account{}
}

func stakingObservation(account model.Account, pool string) near.StakingState {
	return near.StakingState{AccountID: account.Address, Validator: pool, Staked: "2500000000000000000000000", Unstaked: "1000000000000000000000000", CanWithdraw: true, BlockHash: strings.Repeat("1", 32), BlockHeight: "123"}
}

func stakingPlan(account model.Account, pool, method string) model.Plan {
	deposit, args := "0", "{}"
	if method == "deposit_and_stake" {
		deposit = "1250000000000000000000000"
	}
	if method == "unstake" {
		args = `{"amount":"1250000000000000000000000"}`
	}
	payload, _ := json.Marshal(near.Payload{Staking: &near.StakingCall{Method: method, Args: args, Deposit: deposit, Gas: near.StakingGas}})
	return model.Plan{ID: "public-staking-plan", Account: account, Asset: account.Network + ":native", FeeAsset: account.Network + ":native", Recipient: pool, Amount: "1250000000000000000000000", MaxFee: "1000000000000000000000", Payload: payload, Expires: time.Now().Add(time.Minute).Unix()}
}

func TestNearStakingIsOnlyInNearWalletAndNeverGuessesTestnetValidator(t *testing.T) {
	a, _, _ := fixture(t)
	account := nearAccount(t, a)
	for _, current := range a.state.Accounts {
		_, found := findNode(a.walletPage(current).Nodes, "walletSection/"+current.ID+"/Staking")
		if found != (current.Family == "near") {
			t.Fatal("staking tab crossed network family")
		}
	}
	a.walletEvent("walletSection/" + account.ID + "/Staking")
	page := a.walletPage(account)
	text := viewText(page)
	for _, required := range []string{"AstroStakers", astroStakersMainnet, "Staked: Unknown", "Unstaked / waiting: Unknown", "Rewards: unavailable", "four epochs", account.Address} {
		if !strings.Contains(text, required) {
			t.Fatalf("staking disclosure missing: %s", required)
		}
	}
	if strings.Contains(text, "Recipient address") || strings.Contains(text, "Verify and associate") {
		t.Fatal("staking tab mixed ordinary transfer/association form")
	}
	account.Network = "near-testnet"
	if a.stakingPool(account) != "" || strings.Contains(viewText(a.stakingNodes(account)), astroStakersMainnet) {
		t.Fatal("testnet inferred/reused mainnet pool")
	}
	a.fields["stakingPool/"+account.ID] = "chosen.pool.f863973.m0"
	if a.stakingPool(account) != "chosen.pool.f863973.m0" {
		t.Fatal("explicit network pool selection lost")
	}
}

func TestNearStakingObservationBindsWalletPoolAndDisplaysExactWholeNear(t *testing.T) {
	a, ui, core := fixture(t)
	account := nearAccount(t, a)
	pool := a.stakingPool(account)
	state := stakingObservation(account, pool)
	routeFixture(t, a, core, "nearStakingState", map[string]string{"accountId": account.ID, "pool": pool}, state)
	a.walletEvent("stakingState/" + account.ID)
	if !strings.Contains(viewText(a.stakingNodes(account)), "Reading the pool account state") || strings.Contains(viewText(a.stakingNodes(account)), "state is unavailable") {
		t.Fatal("in-flight staking query was classified as a completed failure")
	}
	ui.apply(t)
	text := viewText(a.stakingNodes(account))
	for _, required := range []string{"Staked: 2.5 NEAR", "Unstaked / waiting: 1 NEAR", "Ready to withdraw: Available", "block 123"} {
		if !strings.Contains(text, required) {
			t.Fatal("pool observation or whole-NEAR units missing:", required)
		}
	}
	withdraw, _ := findNode(a.stakingNodes(account), "stakingPrepare/"+account.ID+"/withdraw_all")
	if withdraw.Disabled {
		t.Fatal("observed withdrawable funds stayed disabled")
	}
	state.CanWithdraw = false
	a.staking[account.ID] = state
	withdraw, _ = findNode(a.stakingNodes(account), "stakingPrepare/"+account.ID+"/withdraw_all")
	if !withdraw.Disabled || !strings.Contains(viewText(a.stakingNodes(account)), "Waiting for the pool unlock period") {
		t.Fatal("locked unstaked funds were presented as withdrawable")
	}
	state.CanWithdraw, state.Unstaked = true, "0"
	a.staking[account.ID] = state
	withdraw, _ = findNode(a.stakingNodes(account), "stakingPrepare/"+account.ID+"/withdraw_all")
	if !withdraw.Disabled {
		t.Fatal("zero unstaked funds became withdrawable")
	}
}

func TestNearStakingMalformedOrFailedRefreshErasesPreviousObservation(t *testing.T) {
	for _, mode := range []string{"rpc-error", "wrong-account", "wrong-pool", "negative", "overflow", "missing-block", "invalid-height"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := nearAccount(t, a)
			pool := a.stakingPool(account)
			state := stakingObservation(account, pool)
			a.staking = map[string]near.StakingState{account.ID: state}
			core.handler = func(_ uint64, _ string, _ any, anyOut any) error {
				if mode == "rpc-error" {
					return errors.New("private-RPC-canary")
				}
				switch mode {
				case "wrong-account":
					state.AccountID = "another.near"
				case "wrong-pool":
					state.Validator = "another.poolv1.near"
				case "negative":
					state.Staked = "-1"
				case "overflow":
					state.Staked = "340282366920938463463374607431768211456"
				case "missing-block":
					state.BlockHash = ""
				case "invalid-height":
					state.BlockHeight = "1e5"
				}
				return answer(anyOut, state)
			}
			a.walletEvent("stakingState/" + account.ID)
			ui.apply(t)
			text := viewText(a.stakingNodes(account))
			if strings.Contains(text, "2.5 NEAR") || !strings.Contains(text, "Staked: Unknown") || !strings.Contains(text, "state is unavailable") || strings.Contains(viewText(a.view()), "private-RPC-canary") {
				t.Fatal("failed/malformed observation retained invented authority or leaked error")
			}
		})
	}
}

func TestNearStakingTypedPreparationDoesNotSendAndShowsZeroAttachedForUnstake(t *testing.T) {
	for _, action := range []string{"stake", "unstake", "unstake_all", "withdraw_all"} {
		t.Run(action, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := nearAccount(t, a)
			pool, value, method := a.stakingPool(account), "", action
			if action == "stake" || action == "unstake" {
				value = "1.25"
			}
			if action == "stake" {
				method = "deposit_and_stake"
			}
			a.fields["stakingAmount/"+account.ID] = "1.25"
			plan := stakingPlan(account, pool, method)
			routeFixture(t, a, core, "prepareNearStaking", map[string]string{"accountId": account.ID, "pool": pool, "action": action, "amount": value}, plan)
			a.walletEvent("stakingPrepare/" + account.ID + "/" + action)
			ui.apply(t)
			if a.plan == nil || !a.planIsStaking || a.plan.ID != plan.ID {
				t.Fatal("typed staking preparation lost plan")
			}
			text := viewText(a.stakingNodes(account))
			for _, required := range []string{method, pool, "Gas: 100000000000000 gas units", "not a signed fee cap", "Review and confirm staking transaction"} {
				if !strings.Contains(text, required) {
					t.Fatal("exact staking review omitted:", required)
				}
			}
			if action != "stake" && !strings.Contains(text, "Attached NEAR: 0 NEAR") {
				t.Fatal("unstake/withdraw quantity confused with attached deposit")
			}
			if (action == "unstake_all" || action == "withdraw_all") && !strings.Contains(text, "actual amount follows contract execution") {
				t.Fatal("ALL operation promised a stale fixed amount")
			}
			a.walletEvent("send/" + account.ID)
			if a.busy {
				t.Fatal("ordinary transfer control sent a staking plan")
			}
		})
	}
}

func TestNearStakingStalePoolAmountAndLockCannotRestorePlanOrObservation(t *testing.T) {
	for _, mode := range []string{"pool", "amount", "lock", "state-pool"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := nearAccount(t, a)
			a.fields["stakingAmount/"+account.ID] = "1.25"
			pool := a.stakingPool(account)
			if mode == "state-pool" {
				routeFixture(t, a, core, "nearStakingState", map[string]string{"accountId": account.ID, "pool": pool}, stakingObservation(account, pool))
				a.walletEvent("stakingState/" + account.ID)
			} else {
				routeFixture(t, a, core, "prepareNearStaking", map[string]string{"accountId": account.ID, "pool": pool, "action": "stake", "amount": "1.25"}, stakingPlan(account, pool, "deposit_and_stake"))
				a.walletEvent("stakingPrepare/" + account.ID + "/stake")
			}
			switch mode {
			case "pool", "state-pool":
				a.event(webui.Event{ID: "field/stakingPool/" + account.ID, Value: "another.poolv1.near"})
			case "amount":
				a.event(webui.Event{ID: "field/stakingAmount/" + account.ID, Value: "2"})
			case "lock":
				a.lock()
			}
			ui.apply(t)
			if a.plan != nil || len(a.staking) != 0 {
				t.Fatal("late staking result crossed selection/session boundary")
			}
			if mode == "pool" || mode == "amount" {
				pending, found := a.pendingStaking(account.ID)
				cancel, visible := findNode(a.stakingNodes(account), "cancelPrepared/public-staking-plan")
				if !found || pending.Recipient != pool || !visible || cancel.Disabled || !strings.Contains(viewText(a.stakingNodes(account)), "Reserved contract: "+pool) {
					t.Fatal("stale unsigned preparation orphaned its original reservation/cancellation control")
				}
			} else if len(a.stakingPending) != 0 {
				t.Fatal("locked/read-only callback invented a pending staking plan")
			}
		})
	}
}

func TestNearStakingRequiresExplicitSendAndRetainsOriginalOperationIdentity(t *testing.T) {
	a, ui, core := fixture(t)
	account := nearAccount(t, a)
	a.active = "wallet/" + account.ID
	plan := stakingPlan(account, a.stakingPool(account), "deposit_and_stake")
	a.plan, a.planIsStaking = &plan, true
	var methods []string
	core.handler = func(_ uint64, method string, body, out any) error {
		methods = append(methods, method)
		if method != "send" || viewText(body) != `{"id":"public-staking-plan"}` {
			return errors.New("staking operation identity changed")
		}
		return answer(out, json.RawMessage(`{"state":"UNKNOWN","id":"public-staking-plan"}`))
	}
	a.walletEvent("stakingSend/" + account.ID)
	ui.apply(t)
	if !reflect.DeepEqual(methods, []string{"send"}) || a.plan != nil || a.planIsStaking || a.resultOwner != account.ID || !strings.Contains(a.result, "UNKNOWN") {
		t.Fatal("staking send bypassed/replaced original journal operation")
	}
}

func TestNearStakingRejectsInvalidActionAmountAndWrongWalletBeforeBackend(t *testing.T) {
	for _, mode := range []string{"arbitrary-action", "empty", "zero", "exponent", "precision", "wrong-family", "locked", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			a, _, core := fixture(t)
			account := nearAccount(t, a)
			action, value := "stake", "1.25"
			switch mode {
			case "arbitrary-action":
				action = "function_call"
			case "empty":
				value = ""
			case "zero":
				value = "0"
			case "exponent":
				value = "1e3"
			case "precision":
				value = "0." + strings.Repeat("1", 25)
			case "wrong-family":
				account = ethAccount(t, a)
			case "locked":
				a.state.Locked = true
			case "unconfirmed":
				a.state.RecoveryConfirmed = false
			}
			a.fields["stakingAmount/"+account.ID] = value
			core.handler = func(uint64, string, any, any) error { t.Error("invalid staking request reached backend"); return nil }
			a.walletEvent("stakingPrepare/" + account.ID + "/" + action)
			if a.busy || a.plan != nil {
				t.Fatal("invalid staking request entered preparation")
			}
		})
	}
}

func TestNearStakingRejectsMismatchedPreparedPlanAndNeverShowsItsReview(t *testing.T) {
	for _, mode := range []string{"account", "pool", "network"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := nearAccount(t, a)
			pool := a.stakingPool(account)
			plan := stakingPlan(account, pool, "deposit_and_stake")
			switch mode {
			case "account":
				plan.Account.ID = "foreign-owned-account"
			case "pool":
				plan.Recipient = "foreign.poolv1.near"
			case "network":
				plan.Account.Network = "near-testnet"
			}
			a.fields["stakingAmount/"+account.ID] = "1.25"
			routeFixture(t, a, core, "prepareNearStaking", map[string]string{"accountId": account.ID, "pool": pool, "action": "stake", "amount": "1.25"}, plan)
			a.walletEvent("stakingPrepare/" + account.ID + "/stake")
			ui.apply(t)
			if a.plan != nil || strings.Contains(viewText(a.stakingNodes(account)), "Review and confirm staking transaction") {
				t.Fatal("foreign staking plan became locally actionable")
			}
		})
	}
}

func TestNearStakingSelectionGuardsAndPendingSetupHaveNoSigningAuthority(t *testing.T) {
	a, _, core := fixture(t)
	account := nearAccount(t, a)
	core.handler = func(uint64, string, any, any) error { t.Error("unavailable staking control reached core"); return nil }
	for _, id := range []string{"walletSection/" + account.ID, "walletSection/" + account.ID + "/Other", "walletSection/" + ethAccount(t, a).ID + "/Staking", "stakingPrepare/unknown/stake", "stakingSend/" + account.ID} {
		a.walletEvent(id)
	}
	a.fields["stakingPool/"+account.ID] = ""
	a.walletEvent("stakingState/" + account.ID)
	if !strings.Contains(a.status, "exact staking pool") {
		t.Fatal("empty pool lacked actionable feedback")
	}
	a.walletEvent("stakingPrepare/" + account.ID + "/stake")
	delete(a.fields, "stakingPool/"+account.ID)
	a.state.RecoveryConfirmed = false
	if !strings.Contains(viewText(a.stakingNodes(account)), "Record and confirm the recovery phrase") {
		t.Fatal("pending setup staking hid its authority prerequisite")
	}
	a.walletEvent("stakingPrepare/" + account.ID)
	if a.busy || a.plan != nil {
		t.Fatal("pending/missing staking intent gained authority")
	}
	a.state.RecoveryConfirmed = true
	plan := stakingPlan(account, a.stakingPool(account), "deposit_and_stake")
	a.plan, a.planIsStaking = &plan, true
	a.event(webui.Event{ID: "field/stakingAmount/" + account.ID, Value: "2"})
	if a.plan != &plan || !a.planIsStaking || a.fields["stakingAmount/"+account.ID] == "2" {
		t.Fatal("prepared staking form changed without explicit original-operation cancellation")
	}
	if nearUnits("malformed") != "Unknown" {
		t.Fatal("malformed staking amounts acquired a displayed value")
	}
}

func TestNearStakingOriginalReservationRemainsVisibleUntilExplicitCancellation(t *testing.T) {
	a, ui, core := fixture(t)
	account := nearAccount(t, a)
	a.active = "wallet/" + account.ID
	pool := a.stakingPool(account)
	a.fields["stakingAmount/"+account.ID] = "1.25"
	plan := stakingPlan(account, pool, "deposit_and_stake")
	routeFixture(t, a, core, "prepareNearStaking", map[string]string{"accountId": account.ID, "pool": pool, "action": "stake", "amount": "1.25"}, plan)
	a.walletEvent("stakingPrepare/" + account.ID + "/stake")
	ui.apply(t)
	for _, id := range []string{"field/stakingAmount/" + account.ID, "field/stakingPool/" + account.ID, "stakingPrepare/" + account.ID + "/stake"} {
		control, found := findNode(a.stakingNodes(account), id)
		if !found || !control.Disabled {
			t.Fatal("prepared staking contract/amount or replacement control remained editable")
		}
	}
	a.event(webui.Event{ID: "field/stakingPool/" + account.ID, Value: "foreign.poolv1.near"})
	if a.stakingPool(account) != pool {
		t.Fatal("prepared staking form changed its original pool")
	}
	a.walletEvent("stakingPrepare/" + account.ID + "/stake")
	if a.busy || !strings.Contains(a.status, "original prepared") {
		t.Fatal("prepared staking operation permitted implicit replacement")
	}
	a.applySnapshot(a.state, a.networks)
	if _, pending := a.pendingStaking(account.ID); !pending {
		t.Fatal("public metadata refresh orphaned prepared staking reservation")
	}
	routeFixture(t, a, core, "cancelPrepared", map[string]string{"id": plan.ID}, json.RawMessage(`{"state":"CANCELLED"}`))
	a.walletEvent("cancelPrepared/" + plan.ID)
	ui.apply(t)
	if _, pending := a.pendingStaking(account.ID); pending {
		t.Fatal("explicit durable cancellation retained unsigned staking authority")
	}
	control, _ := findNode(a.stakingNodes(account), "field/stakingAmount/"+account.ID)
	if control.Disabled {
		t.Fatal("explicit cancellation did not release staking form")
	}
}

func TestNearStakingHistoryShowsMethodDepositEffectAndOriginalUnknownIdentity(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		t.Run(method, func(t *testing.T) {
			a, _, _ := fixture(t)
			account := nearAccount(t, a)
			pool := a.stakingPool(account)
			plan := stakingPlan(account, pool, method)
			a.active = "wallet/" + account.ID
			a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Local: journalPage{Operations: []journal.Operation{{Plan: plan, State: "UNKNOWN", Receipt: &model.Receipt{Identity: "original-near-chain-identity", State: "UNKNOWN", Detail: "Execution has not been established"}}}}}}
			nodes := a.walletHistoryNodes(account)
			text := viewText(nodes)
			for _, required := range []string{plan.ID, "UNKNOWN", "Method: " + method, "Staking pool contract: " + pool, "Account: " + account.Address, "Gas: 100000000000000 gas units", "original-near-chain-identity"} {
				if !strings.Contains(text, required) {
					t.Fatal("staking journal omitted original state/intent:", required)
				}
			}
			if strings.Contains(text, "→") {
				t.Fatal("staking journal reused ordinary wallet-to-pool transfer direction")
			}
			if method == "deposit_and_stake" {
				if !strings.Contains(text, "Attached NEAR: 1.25 NEAR") || !strings.Contains(text, "from this wallet into its staked pool balance") {
					t.Fatal("stake journal omitted actual attached deposit or expected effect")
				}
			} else if !strings.Contains(text, "Attached NEAR: 0 NEAR") {
				t.Fatal("unstake/withdraw journal presented effect amount as attached deposit")
			}
			if method == "unstake_all" || method == "withdraw_all" {
				if !strings.Contains(text, "Observed all-funds amount at preparation: 1.25 NEAR") || !strings.Contains(text, "actual amount follows contract execution") {
					t.Fatal("ALL journal amount was represented as a fixed withdrawal/stake amount")
				}
			}
			if method == "withdraw_all" && (!strings.Contains(text, "return all available unstaked funds from the pool to this wallet") || !strings.Contains(text, "Withdrawal destination: "+account.Address)) {
				t.Fatal("withdrawal journal omitted the actual return direction/destination")
			}
			for _, id := range []string{"reconcile/" + plan.ID, "retry/" + plan.ID} {
				if button, found := findNode(nodes, id); !found || button.Disabled {
					t.Fatal("UNKNOWN staking journal lost its explicit original-identity recovery control")
				}
			}
		})
	}
}

func TestNearStakingJournalDoesNotMislabelOrdinaryOrUnknownContractCalls(t *testing.T) {
	a, _, _ := fixture(t)
	account := nearAccount(t, a)
	plan := stakingPlan(account, a.stakingPool(account), "unknown_method")
	text, recognized := stakingJournalDescription(plan)
	if !recognized || !strings.Contains(text, "Unsupported staking method") || strings.Contains(text, "→") || strings.Contains(text, "Planned effect:") {
		t.Fatal("unknown staking contract call acquired an invented transfer effect")
	}
	for _, ordinary := range []model.Plan{{Account: ethAccount(t, a), Payload: plan.Payload}, {Account: account, Payload: json.RawMessage(`{}`)}, {Account: account, Payload: json.RawMessage(`invalid-json`)}} {
		if _, recognized := stakingJournalDescription(ordinary); recognized {
			t.Fatal("ordinary or unreadable plan was classified as known staking")
		}
	}
}
