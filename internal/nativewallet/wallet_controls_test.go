package nativewallet

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
	"localwallet/internal/webui"
)

func TestTrackTokenDialogRoutesOnlyItsExactOwnedContract(t *testing.T) {
	for _, family := range []string{"evm", "near"} {
		t.Run(family, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			contract := publicUSDCContract
			if family == "near" {
				account = nearAccount(t, a)
				contract = "usd.token.near"
			}
			a.active = "wallet/" + account.ID
			var calls atomic.Int32
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				if p.Kind != "publicInput" || len(p.Labels) != 1 || !strings.Contains(p.Labels[0], a.network(account.Network).Name) {
					t.Fatal("token selector was not a network-scoped public dialog")
				}
				return uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{"  " + contract + "  "}}, nil
			}
			core.handler = func(_ uint64, method string, body, out any) error {
				calls.Add(1)
				if method != "importToken" || viewText(body) != viewText(map[string]string{"accountId": account.ID, "contract": contract}) {
					t.Error("public input acquired metadata or transaction authority", method, body)
				}
				return errors.New("controlled unavailable metadata")
			}
			a.walletEvent("trackToken/" + account.ID)
			ui.apply(t)
			if calls.Load() != 1 || a.busy || a.fields["tokenContract/"+account.ID] != "" {
				t.Fatal("token dialog did not delegate once or retained its input")
			}
		})
	}
}

func TestTrackTokenDialogCancellationAndStaleStateNeverImport(t *testing.T) {
	for _, outcome := range []string{"cancel", "invalid", "malformed", "lock", "close", "generation", "navigate", "descriptor", "private-revision", "lost-confirmation"} {
		t.Run(outcome, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active = "wallet/" + account.ID
			core.handler = func(uint64, string, any, any) error {
				t.Error("stale/cancelled input reached backend")
				return errors.New("forbidden")
			}
			ui.prompt = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				reply := uiprompt.Reply{PromptID: p.ID, Accepted: true, Values: []string{publicUSDCContract}}
				switch outcome {
				case "cancel":
					reply.Accepted = false
					reply.Values = nil
				case "invalid":
					reply.Values[0] = "https://foreign.example/token"
				case "malformed":
					reply.PromptID = "stale"
				case "lock":
					a.lock()
				case "close":
					a.close()
				case "generation":
					core.Kill()
				case "navigate":
					a.active = "group/0"
				case "descriptor":
					for i := range a.state.Accounts {
						if a.state.Accounts[i].ID == account.ID {
							a.state.Accounts[i].Address = "other"
						}
					}
				case "private-revision":
					a.privateRevision.Add(1)
				case "lost-confirmation":
					a.state.RecoveryConfirmed = false
				}
				return reply, nil
			}
			a.walletEvent("trackToken/" + account.ID)
			if a.busy && !a.closed || a.fields["tokenContract/"+account.ID] != "" {
				t.Fatal("stale token dialog retained busy state or selector")
			}
		})
	}
}

func TestWalletControlsAndHistoryScopeMoveToExplicitActions(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	const scope = "Unique long provider scope: account blocks, receipts and incomplete archival coverage."
	a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Chain: model.HistoryPage{Scope: scope}}}
	page := a.walletPage(account)
	for _, id := range []string{"tokenContract/" + account.ID, "importToken/" + account.ID, "configure/" + account.ID} {
		if _, ok := findNode(page.Nodes, "field/"+id); ok {
			t.Fatal("old inline token form remains", id)
		}
		if _, ok := findNode(page.Nodes, id); ok {
			t.Fatal("old inline control remains", id)
		}
	}
	if n, ok := findNode(page.Nodes, "trackToken/"+account.ID); !ok || n.Text != "Track new token" {
		t.Fatal("token dialog action missing")
	}
	toolbar, ok := findNode(page.Nodes, "historyToolbar/"+account.ID)
	if !ok || len(toolbar.Children) != 3 || toolbar.Children[0].Text != "Transaction history" {
		t.Fatal("history actions are not grouped beside heading")
	}
	info, ok := findNode(toolbar.Children, "historyInfo/"+account.ID)
	if !ok || !strings.Contains(info.Tooltip, scope) {
		t.Fatal("provider scope missing from info disclosure")
	}
	var inline func([]webui.Node)
	inline = func(nodes []webui.Node) {
		for _, n := range nodes {
			if n.Text == scope {
				t.Error("provider essay is still inline")
			}
			inline(n.Children)
		}
	}
	inline(page.Nodes)
	if copy, ok := findNode(page.Nodes, "copyReceive/"+account.ID); !ok || copy.Text != "⧉" || copy.Value != account.Address {
		t.Fatal("wallet copy lost exact address or kept a text button")
	}
}

func TestSettingsRPCSelectsOnlyOwnedActiveNetworkAndNeverExposesEndpoint(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active = "wallet/" + account.ID
	a.event(webui.Event{ID: "tab", Value: "settings"})
	a.event(webui.Event{ID: "settingsNetworks"})
	page := a.settingsPage()
	selector, ok := findNode(page, "field/rpcNetwork")
	if !ok || selector.Value != account.Network || len(selector.Options) != 8 {
		t.Fatal("RPC settings did not select the originating wallet network")
	}
	for _, value := range []string{"foreign-mainnet", "ethereum-sepolia", "decred-mainnet"} {
		a.fields["rpcNetwork"] = value
		a.configureSelectedRPC()
		if a.busy {
			t.Fatal("unowned or retired RPC selection dispatched")
		}
	}
	a.fields["rpcNetwork"] = account.Network
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "configure" || viewText(body) != viewText(map[string]string{"network": account.Network}) {
			t.Error("wrong RPC network or public credential payload", method, body)
		}
		return errors.New("controlled configuration cancellation")
	}
	a.configureSelectedRPC()
	ui.apply(t)
	a.state.Locked = true
	a.configureSelectedRPC()
	if a.busy {
		t.Fatal("locked RPC settings dispatched")
	}
}
