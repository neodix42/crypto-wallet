package nativewallet

import (
	"localwallet/internal/webui"
	"strings"
	"testing"
	"time"
)

func TestAccountPagesKeepGroupOwnershipAndPagingWithoutSelector(t *testing.T) {
	a, _, _ := fixture(t)
	original := a.state.Accounts[0]
	for index := uint32(1); index < 10; index++ {
		copy := original
		copy.ID = "group-wallet-" + groupName(index)
		copy.Index = index
		a.state.Accounts = append(a.state.Accounts, copy)
	}
	last := a.state.Accounts[len(a.state.Accounts)-1]
	a.tabs = []string{original.ID, last.ID}
	a.active = "wallet/" + last.ID
	v := a.view()
	if v.AccountGroupID != "group/9" || a.fields["group"] != "9" {
		t.Fatal("wallet lost exact owning group", v.AccountGroupID)
	}
	groups := 0
	for _, p := range v.Pages {
		if strings.HasPrefix(p.ID, "group/") {
			groups++
			if _, ok := findNode(p.Nodes, "field/group"); ok {
				t.Fatal("selector still visible")
			}
		}
		if p.ID == "wallet/"+last.ID && p.ParentID != "group/9" {
			t.Fatal("wrong wallet parent", p.ParentID)
		}
	}
	if groups != 2 {
		t.Fatal("unbounded or wrong group page", groups)
	}
	a.event(webui.Event{ID: "tab", Value: "settings"})
	if a.view().AccountGroupID != "group/9" {
		t.Fatal("settings lost remembered parent")
	}
	a.event(webui.Event{ID: "closeTab", Value: "wallet/" + original.ID})
	if a.active != "settings" {
		t.Fatal("closing inactive wallet changed selection", a.active)
	}
	a.active = "wallet/" + last.ID
	a.event(webui.Event{ID: "closeTab", Value: a.active})
	if a.active != "group/9" {
		t.Fatal("close returned to wrong group", a.active)
	}
	a.event(webui.Event{ID: "prevGroupPage/0"})
	if a.active != "group/0" {
		t.Fatal("cannot return to first group page")
	}
	a.event(webui.Event{ID: "nextGroupPage/8"})
	if a.active != "group/8" {
		t.Fatal("cannot access later groups without selector")
	}
	a.event(webui.Event{ID: "nextGroupPage/999"})
	if a.active != "group/8" {
		t.Fatal("foreign group accepted")
	}
}

func TestAccountBalancePresentationAndDistinctNetworkCount(t *testing.T) {
	a := valuationFixture(time.Now())
	a.fields = map[string]string{}
	page := a.groupPage(0)
	n, _ := findNode(page.Nodes, "portfolioValuation")
	if n.Text != "Balance: 5,526.13$" || strings.Contains(n.Text, "~") || !strings.Contains(n.Tooltip, "USDT") {
		t.Fatal("balance display changed pricing meaning", n)
	}
	copy := a.state.Accounts[0]
	copy.ID = "another-profile"
	a.state.Accounts = append(a.state.Accounts, copy)
	n, _ = findNode(a.groupPage(0).Nodes, "networkCount")
	if n.Text != "3 networks" {
		t.Fatal("wallet profiles counted as separate networks", n.Text)
	}
	a.balanceErrors[copy.ID] = true
	n, _ = findNode(a.groupPage(0).Nodes, "portfolioValuation")
	if strings.Contains(n.Text, "~") {
		t.Fatal("approximation prefix returned")
	}
}

func TestWalletManagementActionsShareOneRowAndKeepTheirGuards(t *testing.T) {
	a, _, _ := fixture(t)
	for _, account := range a.state.Accounts {
		page := a.walletPage(account)
		actions, ok := findNode(page.Nodes, "walletActions/"+account.ID)
		if !ok {
			t.Fatal("missing action row", account.Network)
		}
		want := []string{"sendForm/", "receive/", "hideAccount/"}
		if ethereumTokenNetwork(account) || nearTokenNetwork(account) {
			want = append(want, "trackToken/")
		}
		if baseTokenNetwork(account) {
			want = append(want, "discoverTokens/")
		}
		if account.Family == "ton" {
			want = append(want, "tonState/")
		}
		for _, prefix := range want {
			found := 0
			for _, n := range actions.Children {
				if n.ID == prefix+account.ID {
					found++
				}
			}
			if found != 1 {
				t.Fatal("management action not in same row", account.Network, prefix, found)
			}
		}
		if page.ParentID != "group/0" {
			t.Fatal("missing explicit parent", page.ParentID)
		}
	}
}

func TestSendFieldErrorsUpdateImmediatelyWithoutProviderWork(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	a.sendForms[account.ID] = true
	called := false
	core.handler = func(uint64, string, any, any) error { called = true; return nil }
	for _, e := range []webui.Event{{ID: "field/recipient/" + account.ID, Value: "not-an-ethereum-address"}, {ID: "field/amount/" + account.ID, Value: "1e3"}} {
		before := len(ui.views)
		a.event(e)
		if len(ui.views) <= before {
			t.Fatal("validation not rendered immediately")
		}
	}
	page := a.walletPage(account)
	recipient, _ := findNode(page.Nodes, "field/recipient/"+account.ID)
	amount, _ := findNode(page.Nodes, "field/amount/"+account.ID)
	send, _ := findNode(page.Nodes, "prepare/"+account.ID)
	max, _ := findNode(page.Nodes, "max/"+account.ID)
	if recipient.Error == "" || amount.Error == "" || amount.InputMode != "decimal" || !send.Disabled || !max.Disabled || called {
		t.Fatal("invalid fields authorized work or lack feedback", recipient, amount, send, max, called)
	}
	a.event(webui.Event{ID: "field/recipient/" + account.ID, Value: "0x1111111111111111111111111111111111111111"})
	a.event(webui.Event{ID: "field/amount/" + account.ID, Value: "1.25"})
	page = a.walletPage(account)
	send, _ = findNode(page.Nodes, "prepare/"+account.ID)
	max, _ = findNode(page.Nodes, "max/"+account.ID)
	if send.Disabled || max.Disabled || called {
		t.Fatal("valid local draft did not enable review without RPC", send, max)
	}
}

func TestNearStakingKeepsItsOwnActionsAndWalletSendStillOpens(t *testing.T) {
	a, _, _ := fixture(t)
	account := nearAccount(t, a)
	a.active = "wallet/" + account.ID
	a.fields["walletSection/"+account.ID] = "Staking"
	page := a.walletPage(account)
	if _, ok := findNode(page.Nodes, "walletActions/"+account.ID); ok {
		t.Fatal("staking page exposes ordinary controls with no transfer form")
	}
	if _, ok := findNode(page.Nodes, "stakingPrepare/"+account.ID+"/stake"); !ok {
		t.Fatal("staking actions lost")
	}
	a.walletEvent("walletSection/" + account.ID + "/Wallet")
	a.walletEvent("sendForm/" + account.ID)
	if _, ok := findNode(a.walletPage(account).Nodes, "field/recipient/"+account.ID); !ok {
		t.Fatal("ordinary wallet Send no longer opens")
	}
}
