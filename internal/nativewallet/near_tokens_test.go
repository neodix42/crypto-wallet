package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func nearTokenFixture(t *testing.T, a *App) (model.Account, model.Asset, model.Asset) {
	t.Helper()
	account := nearAccount(t, a)
	first := model.Asset{ID: account.Network + ":nep141:usd.token.near", Contract: "usd.token.near", Symbol: "USD", Decimals: 6}
	second := model.Asset{ID: account.Network + ":nep141:other.token.near", Contract: "other.token.near", Symbol: "USD", Decimals: 24}
	for i := range a.networks {
		if a.networks[i].ID == account.Network {
			a.networks[i].Assets = append(a.networks[i].Assets, first, second)
		}
	}
	a.fields["asset/"+account.ID] = first.ID
	a.active = "wallet/" + account.ID
	return account, first, second
}

func nearStorageFixture(account model.Account, asset model.Asset) near.TokenStorageState {
	return near.TokenStorageState{Network: account.Network, Asset: asset.ID, Contract: asset.Contract, AccountID: account.Address, MinDeposit: "1250000000000000000000", Total: "0", Available: "0", BlockHash: base58.Encode([]byte(strings.Repeat("h", 32))), BlockHeight: "123"}
}

func nearRegistrationFixture(account model.Account, asset model.Asset) model.Plan {
	deposit := "1250000000000000000000"
	call := near.TokenCall{Policy: model.NEP141Token{Network: account.Network, Asset: asset, CodeHash: base58.Encode([]byte(strings.Repeat("c", 32)))}, Method: "storage_deposit", Deposit: deposit, Gas: near.TokenGas, Args: `{"account_id":"` + account.Address + `","registration_only":true}`}
	payload, _ := json.Marshal(near.Payload{Token: &call})
	return model.Plan{ID: "public-token-registration", Account: account, Asset: account.Network + ":native", FeeAsset: account.Network + ":native", Amount: deposit, MaxFee: "100000000000000000000", Recipient: account.Address, Payload: payload, Expires: time.Now().Add(time.Minute).Unix()}
}

func TestNearTokensShowIndependentBalancesExactContractsAndNativeOverview(t *testing.T) {
	a, _, _ := fixture(t)
	account, first, second := nearTokenFixture(t, a)
	a.balances[account.ID] = []model.Balance{{Asset: account.Network + ":native", Atomic: "2000000000000000000000000", State: "provider-observed", At: "block"}, {Asset: first.ID, Atomic: "1250001", State: "provider-observed", At: "block"}, {Asset: second.ID, State: "unavailable", At: "block"}}
	page := a.walletPage(account)
	text := viewText(page)
	for _, required := range []string{"1.250001", "Unknown", "Track a NEAR token", "Token contract account", "NEP-141 contract", first.Contract, second.Contract, account.Address} {
		if !strings.Contains(text, required) {
			t.Fatal("missing token view data", required)
		}
	}
	if !strings.Contains(viewText(a.groupPage(0)), "2") || strings.Contains(viewText(a.groupPage(0)), "1.250001") {
		t.Fatal("token selection replaced native overview balance")
	}
	if exchangeAssetAllowed(account, first) || exchangeAssetAllowed(account, second) || !exchangeAssetAllowed(account, a.network(account.Network).Assets[0]) {
		t.Fatal("imported NEAR token gained a Bybit route or native route was lost")
	}
	for _, asset := range []model.Asset{first, second} {
		selection, ok := findNode(page.Nodes, "asset/"+account.ID+"/"+asset.ID)
		if !ok || !strings.Contains(selection.Tooltip, account.Network) || !strings.Contains(selection.Tooltip, asset.Contract) {
			t.Fatal("same-symbol token identity was hidden")
		}
	}
	a.fields["walletSection/"+account.ID] = "Staking"
	if strings.HasPrefix(a.walletPage(account).Title, "USD") {
		t.Fatal("staking header showed selected FT instead of native NEAR")
	}
}

func TestNearTokenImportUsesOnlyExactContractAndExistingApprovalRoute(t *testing.T) {
	a, ui, core := fixture(t)
	account := nearAccount(t, a)
	a.fields["tokenContract/"+account.ID] = "  usd.token.near  "
	a.fields["tokenDecimals/"+account.ID] = "99"
	routeFixture(t, a, core, "importToken", map[string]string{"accountId": account.ID, "contract": "usd.token.near"}, a.state)
	a.walletEvent("importToken/" + account.ID)
	ui.apply(t)
	if a.busy {
		t.Fatal("token import did not complete its ordinary approval/snapshot path")
	}
	for _, invalid := range []string{"", "https://token.invalid", "TOKEN.near", "bad..near", "a", strings.Repeat("a", 65)} {
		a.fields["tokenContract/"+account.ID] = invalid
		a.walletEvent("importToken/" + account.ID)
		if a.busy {
			t.Fatal("invalid contract reached backend")
		}
	}
}

func TestNearTokenStorageReadBindsSelectedContractAndNeverInventsRegistration(t *testing.T) {
	a, ui, core := fixture(t)
	account, token, _ := nearTokenFixture(t, a)
	state := nearStorageFixture(account, token)
	routeFixture(t, a, core, "nearTokenStorage", map[string]string{"accountId": account.ID, "asset": token.ID}, state)
	a.walletEvent("nearTokenStorage/" + account.ID)
	if !strings.Contains(viewText(a.nearTokenNodes(account, token)), "Checking token registration") {
		t.Fatal("read did not show pending state")
	}
	ui.apply(t)
	button, ok := findNode(a.nearTokenNodes(account, token), "nearTokenRegister/"+account.ID)
	if !ok || button.Disabled || !strings.Contains(viewText(a.nearTokenNodes(account, token)), "0.00125 NEAR") {
		t.Fatal("unregistered token did not expose exact storage cost/action")
	}
	state.Registered, state.Total = true, state.MinDeposit
	routeFixture(t, a, core, "nearTokenStorage", map[string]string{"accountId": account.ID, "asset": token.ID}, state)
	a.walletEvent("nearTokenStorage/" + account.ID)
	ui.apply(t)
	button, _ = findNode(a.nearTokenNodes(account, token), "nearTokenRegister/"+account.ID)
	if !button.Disabled || !strings.Contains(viewText(a.nearTokenNodes(account, token)), "registered to receive") {
		t.Fatal("already registered state allowed another deposit")
	}
	core.handler = func(_ uint64, _ string, _, _ any) error { return errors.New("SYNTHETIC-PRIVATE-DETAIL") }
	a.walletEvent("nearTokenStorage/" + account.ID)
	ui.apply(t)
	text := viewText(a.nearTokenNodes(account, token)) + a.status
	if !strings.Contains(text, "unknown") || strings.Contains(text, "SYNTHETIC") || strings.Contains(text, "0.00125") || strings.Contains(text, "registered to receive") {
		t.Fatal("failed query retained stale registration/cost or leaked error")
	}
}

func TestNearTokenStorageRejectsForeignMalformedAndInconsistentObservations(t *testing.T) {
	a, _, _ := fixture(t)
	account, token, _ := nearTokenFixture(t, a)
	valid := nearStorageFixture(account, token)
	if !validNearTokenStorage(valid, account, token) {
		t.Fatal("fixture invalid")
	}
	for _, mutate := range []func(*near.TokenStorageState){func(s *near.TokenStorageState) { s.Network = "near-testnet" }, func(s *near.TokenStorageState) { s.Asset = "foreign" }, func(s *near.TokenStorageState) { s.Contract = "foreign.near" }, func(s *near.TokenStorageState) { s.AccountID = "foreign.near" }, func(s *near.TokenStorageState) { s.BlockHash = "" }, func(s *near.TokenStorageState) { s.BlockHeight = "0123" }, func(s *near.TokenStorageState) { s.MinDeposit = "-1" }, func(s *near.TokenStorageState) { s.Total = "1" }, func(s *near.TokenStorageState) { s.Available = "1" }, func(s *near.TokenStorageState) { s.Registered = true }, func(s *near.TokenStorageState) { s.Total = strings.Repeat("9", 40) }} {
		bad := valid
		mutate(&bad)
		if validNearTokenStorage(bad, account, token) {
			t.Fatal("invalid token registration observation accepted")
		}
	}
}

func TestNearTokenRegistrationRetainsOriginalOperationAcrossSelectionAndRequiresExplicitReview(t *testing.T) {
	a, ui, core := fixture(t)
	account, token, other := nearTokenFixture(t, a)
	plan := nearRegistrationFixture(account, token)
	called := make(chan struct{})
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "prepareNearTokenRegistration" || viewText(body) != viewText(map[string]string{"accountId": account.ID, "asset": token.ID}) {
			return errors.New("registration attempted a spend or arbitrary call")
		}
		close(called)
		return answer(out, plan)
	}
	a.walletEvent("nearTokenRegister/" + account.ID)
	<-called
	a.fields["asset/"+account.ID] = other.ID
	ui.apply(t)
	if a.plan != nil || a.nearTokenPending[account.ID].ID != plan.ID {
		t.Fatal("prepared registration signed automatically or lost after selection change")
	}
	text := viewText(a.nearTokenNodes(account, other))
	if !strings.Contains(text, plan.ID) || !strings.Contains(text, "Cancel token registration") || !strings.Contains(text, "Review and register") {
		t.Fatal("original registration review/cancel path disappeared")
	}
	routeFixture(t, a, core, "send", map[string]string{"id": plan.ID}, json.RawMessage(`{"state":"CONFIRMED"}`))
	a.walletEvent("nearTokenRegisterSend/" + account.ID)
	ui.apply(t)
	if len(a.nearTokenPending) != 0 {
		t.Fatal("completed registration was not removed")
	}
}

func TestNearTokenStateAndPendingOperationsClearOnLockAndRejectForeignPlans(t *testing.T) {
	a, ui, core := fixture(t)
	account, token, other := nearTokenFixture(t, a)
	plan := nearRegistrationFixture(account, other)
	routeFixture(t, a, core, "prepareNearTokenRegistration", map[string]string{"accountId": account.ID, "asset": token.ID}, plan)
	a.walletEvent("nearTokenRegister/" + account.ID)
	ui.apply(t)
	if len(a.nearTokenPending) != 0 {
		t.Fatal("a different token's registration was accepted")
	}
	a.nearTokenStorage = map[string]nearTokenStorageObservation{account.ID + "/" + token.ID: {State: nearStorageFixture(account, token)}}
	a.nearTokenPending = map[string]model.Plan{account.ID: nearRegistrationFixture(account, token)}
	a.lock()
	if a.nearTokenStorage != nil || a.nearTokenPending != nil {
		t.Fatal("locked wallet retained token observations or draft IDs")
	}
	a.event(webui.Event{ID: "nearTokenRegisterSend/" + account.ID})
	if a.busy {
		t.Fatal("locked registration reached backend")
	}
}

func TestNearTokenNormalSendUsesWholeTokenUnitsNativeFeesAndSharedReceiveAddress(t *testing.T) {
	a, ui, core := fixture(t)
	account, token, _ := nearTokenFixture(t, a)
	a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = "recipient.near", "1.000001"
	want := model.Transfer{AccountID: account.ID, Asset: token.ID, Recipient: "recipient.near", Amount: "1.000001"}
	plan := model.Plan{ID: "public-ft-transfer", Account: account, Asset: token.ID, Recipient: want.Recipient, Amount: "1000001", MaxFee: "100000000000000000000", FeeAsset: account.Network + ":native", Expires: time.Now().Add(time.Minute).Unix()}
	routeFixture(t, a, core, "prepare", want, plan)
	a.walletEvent("prepare/" + account.ID)
	ui.apply(t)
	a.sendForms[account.ID] = true
	text := viewText(a.walletPage(account))
	if a.plan == nil || a.plan.Amount != "1000001" || !strings.Contains(text, "1.000001 USD") || !strings.Contains(text, "Estimated fee reserve: 0.000100000000000000000000 NEAR") {
		t.Fatal("token send mixed token units or native fees")
	}
	routeFixture(t, a, core, "receive", map[string]string{"accountId": account.ID}, json.RawMessage(`{"ok":true}`))
	a.walletEvent("receive/" + account.ID)
	ui.apply(t)
	if a.plan == nil || a.plan.ID != plan.ID || a.asset(account).ID != token.ID || !strings.Contains(viewText(a.walletPage(account)), account.Address) {
		t.Fatal("token receiving changed address, asset or prepared payment")
	}
}

func TestNearTokenSnapshotsClearObservationsButRetainOnlyMatchingOriginalReservations(t *testing.T) {
	a, _, _ := fixture(t)
	account, token, _ := nearTokenFixture(t, a)
	plan := nearRegistrationFixture(account, token)
	a.nearTokenPending = map[string]model.Plan{account.ID: plan}
	a.nearTokenStorage = map[string]nearTokenStorageObservation{account.ID + "/" + token.ID: {State: nearStorageFixture(account, token)}}
	a.applySnapshot(a.state, a.networks)
	if a.nearTokenStorage != nil || a.nearTokenPending[account.ID].ID != plan.ID {
		t.Fatal("refresh retained stale observation or orphaned original operation")
	}
	state := a.state
	state.Accounts = append([]model.Account(nil), state.Accounts...)
	for i := range state.Accounts {
		if state.Accounts[i].ID == account.ID {
			state.Accounts[i].Address = "different.near"
		}
	}
	a.applySnapshot(state, a.networks)
	if len(a.nearTokenPending) != 0 {
		t.Fatal("registration followed an account association change")
	}
}

func TestNearTokenLateReadCannotRepopulateLockedStateOrReleaseAnotherOperation(t *testing.T) {
	a, ui, core := fixture(t)
	account, token, _ := nearTokenFixture(t, a)
	routeFixture(t, a, core, "nearTokenStorage", map[string]string{"accountId": account.ID, "asset": token.ID}, nearStorageFixture(account, token))
	a.walletEvent("nearTokenStorage/" + account.ID)
	a.lock()
	ui.apply(t)
	if a.nearTokenStorage != nil || !a.state.Locked {
		t.Fatal("late token observation revived locked state")
	}
	a, ui, core = fixture(t)
	account, token, _ = nearTokenFixture(t, a)
	core.handler = func(_ uint64, _ string, _, _ any) error { return errors.New("SYNTHETIC-FAILED-REGISTRATION") }
	a.walletEvent("nearTokenRegister/" + account.ID)
	ui.apply(t)
	if len(a.nearTokenPending) != 0 || strings.Contains(a.status, "SYNTHETIC") {
		t.Fatal("failed preparation retained a phantom operation or raw error")
	}
}
