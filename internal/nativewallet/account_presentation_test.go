package nativewallet

import (
	"encoding/json"
	"errors"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
	"localwallet/internal/webui"
	"reflect"
	"strings"
	"testing"
)

func TestPresentationNamesDoNotChangeDescriptorsAndArchivedGroupsRecover(t *testing.T) {
	a, _, _ := fixture(t)
	original := append([]model.Account(nil), a.state.Accounts...)
	account := a.state.Accounts[0]
	a.state.Presentation = model.AccountPresentation{WalletLabels: map[string]string{account.ID: "Savings"}, GroupLabels: map[string]string{"0": "Household"}, DeletedGroups: map[string]bool{}}
	if a.groupPage(0).Title != "Household" || a.walletPage(account).Title != "Savings" || !reflect.DeepEqual(original, a.state.Accounts) {
		t.Fatal("presentation touched descriptors or failed titles")
	}
	a.state.Presentation.DeletedGroups["0"] = true
	for i := range a.state.Accounts {
		a.state.Accounts[i].Hidden = true
	}
	a.active = "group/0"
	v := a.view()
	if v.Pages[0].Title != "Accounts" {
		t.Fatal("last deleted group did not leave empty account shell", v.Pages[0].Title)
	}
	if _, ok := findNode(v.Pages[0].Nodes, "addNextAccount"); !ok {
		t.Fatal("cannot add after archiving all groups")
	}
	if _, ok := findNode(v.Pages[0].Nodes, "deleteGroup/0"); ok {
		t.Fatal("empty shell can delete hidden target")
	}
	a.fields["showHidden"] = "true"
	v = a.view()
	if v.Pages[0].Title != "Household" {
		t.Fatal("show hidden cannot recover archived group")
	}
}
func TestNextIndexUsesAuthoritativeSignerAndNeverReusesHiddenHoles(t *testing.T) {
	a, _, _ := fixture(t)
	a.state.Accounts[0].Index = 8
	a.state.Accounts[0].Hidden = true
	if n, ok := a.nextAccountIndex(); !ok || n != 9 {
		t.Fatal("hidden latest index reused", n, ok)
	}
	next := uint32(14)
	a.state.NextAccountIndex = &next
	a.state.Revision = "exact-signer-revision"
	if n, ok := a.nextAccountIndex(); !ok || n != 14 {
		t.Fatal("retired-only signer index ignored", n, ok)
	}
	a.state.NextAccountIndex = nil
	if _, ok := a.nextAccountIndex(); ok {
		t.Fatal("authoritative exhausted signer index invented")
	}
}
func TestPresentationPageMergeRejectsForeignAndContradictoryMetadata(t *testing.T) {
	account := model.Account{ID: "first", Index: 1}
	other := model.Account{ID: "second", Index: 1}
	var dst model.AccountPresentation
	seen := map[string]bool{}
	first := model.AccountPresentation{WalletLabels: map[string]string{"first": "One"}, GroupLabels: map[string]string{"1": "Account"}, DeletedGroups: map[string]bool{"1": true}}
	if err := mergePresentation(&dst, first, []model.Account{account}, seen); err != nil {
		t.Fatal(err)
	}
	if err := mergePresentation(&dst, model.AccountPresentation{GroupLabels: map[string]string{"1": "Account"}, DeletedGroups: map[string]bool{"1": true}}, []model.Account{other}, seen); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []model.AccountPresentation{{WalletLabels: map[string]string{"foreign": "Name"}}, {GroupLabels: map[string]string{"1": "Changed"}, DeletedGroups: map[string]bool{"1": true}}, {GroupLabels: map[string]string{"1": "Account"}}, {GroupLabels: map[string]string{"01": "Account"}}} {
		if err := mergePresentation(&dst, bad, []model.Account{other}, seen); err == nil {
			t.Fatal("foreign or changed page metadata accepted", bad)
		}
	}
}
func TestRenameInputCancellationInvalidAndStaleNeverMutate(t *testing.T) {
	for _, mode := range []string{"cancel", "invalid", "generation"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := a.state.Accounts[0]
			a.tabs = []string{account.ID}
			a.active = "settings"
			calls := 0
			core.handler = func(uint64, string, any, any) error { calls++; return errors.New("unexpected mutation") }
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if p.Kind != "publicInput" {
					t.Fatal("name dialog masked or wrong")
				}
				if mode == "generation" {
					core.generation.Add(1)
				}
				if mode == "cancel" {
					return uiprompt.Reply{}, uiprompt.ErrCancelled
				}
				name := "Name"
				if mode == "invalid" {
					name = "spoof\nNetwork: other"
				}
				return uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{name}}, nil
			}
			a.event(webui.Event{ID: "renameWallet/" + account.ID})
			if calls != 0 {
				t.Fatal("cancelled/invalid/stale name mutated storage")
			}
		})
	}
}

func TestPlusCreatesNextGroupAndDeleteClosesOnlyLocalTab(t *testing.T) {
	a, ui, core := fixture(t)
	first := ethAccount(t, a)
	second := first
	second.ID = "next-public-wallet"
	second.Index = 1
	all := append(append([]model.Account(nil), a.state.Accounts...), second)
	next := uint32(2)
	state := snapshot{Environment: "mainnet", RecoveryConfirmed: true, WalletExists: true, VaultState: "present", Accounts: all, TotalAccounts: len(all), Revision: "2", NextAccountIndex: &next}
	methods := []string{}
	core.handler = func(_ uint64, method string, body, out any) error {
		methods = append(methods, method)
		switch method {
		case "addNextAccount":
			raw, _ := json.Marshal(body)
			if string(raw) != "{}" {
				t.Error("UI supplied a manual derivation index", string(raw))
			}
			return answer(out, struct{}{})
		case "accounts":
			return answer(out, state)
		case "networks":
			return answer(out, a.networks)
		case "deleteWallet":
			for i := range state.Accounts {
				if state.Accounts[i].ID == second.ID {
					state.Accounts[i].Hidden = true
				}
			}
			return answer(out, struct{}{})
		}
		return errors.New("unexpected backend work")
	}
	a.event(webui.Event{ID: "addNextAccount"})
	ui.apply(t)
	if a.active != "group/1" || a.view().AccountGroupID != "group/1" {
		t.Fatal("+ did not select the newly created group", a.active)
	}
	add, _ := findNode(a.groupPage(1).Nodes, "addNextAccount")
	if add.Disabled || !strings.Contains(add.Tooltip, "index 2") {
		t.Fatal("new + did not use signer next index", add)
	}
	if !reflect.DeepEqual(methods, []string{"addNextAccount", "accounts", "networks"}) {
		t.Fatal("+ performed unexpected work", methods)
	}
	a.tabs = []string{first.ID, second.ID}
	a.active = "wallet/" + second.ID
	a.sendForms[second.ID] = true
	a.event(webui.Event{ID: "deleteWallet/" + second.ID})
	ui.apply(t)
	if a.active != "group/1" || len(a.tabs) != 1 || a.tabs[0] != first.ID || a.sendForms[second.ID] {
		t.Fatal("deleted tab not cleared or unrelated tab removed", a.active, a.tabs)
	}
	account, ok := a.account(second.ID)
	if !ok || !account.Hidden || account.Address != second.Address {
		t.Fatal("delete lost or changed recoverable identity", account)
	}
}
