package nativewallet

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"

	"localwallet/internal/journal"
	"localwallet/internal/model"
)

// This peer exercises the same cancellable interface used by the real
// supervisor. Responses are explicit controller fixtures, not RPC evidence.
type tonHistoryRefreshBackend struct {
	*testBackend
	history func(context.Context, uint64, any, any) error
}

func (b *tonHistoryRefreshBackend) CallHistoryAt(ctx context.Context, generation uint64, body, out any) error {
	return b.history(ctx, generation, body, out)
}

func TestTONHistoryRefreshUsesOwnedAccountAndKeepsGRAMAndUSDTAcrossSelection(t *testing.T) {
	a, ui, core := fixture(t)
	var account model.Account
	for i, candidate := range a.state.Accounts {
		if candidate.Network == "ton-mainnet" {
			account = candidate
			// A synthetic public descriptor only; no supplied wallet is tracked.
			account.Address = address.MustParseRawAddr("0:" + strings.Repeat("1", 64)).Bounce(false).String()
			a.state.Accounts[i] = account
			break
		}
	}
	assets := a.network(account.Network).Assets
	if account.ID == "" || len(assets) != 2 || assets[0].Symbol != "GRAM" || assets[0].Decimals != 9 || assets[1].Symbol != "USDT" || assets[1].Decimals != 6 {
		t.Fatal("TON fixture does not contain the actual registered native/jetton identities")
	}
	a.active = "wallet/" + account.ID
	core.handler = func(generation uint64, method string, body, out any) error {
		request, ok := body.(map[string]string)
		if generation != core.Generation() || method != "history" || !ok || len(request) != 2 || request["accountId"] != account.ID || request["cursor"] != "" {
			return errors.New("unexpected mutation, foreign account or non-fresh local request")
		}
		return answer(out, journalPage{Complete: true, Revision: "1"})
	}
	pageNumber := 0
	a.core = &tonHistoryRefreshBackend{testBackend: core, history: func(ctx context.Context, generation uint64, body, out any) error {
		request, ok := body.(map[string]string)
		if ctx == nil || ctx.Err() != nil || generation != core.Generation() || !ok || len(request) != 2 || request["accountId"] != account.ID || request["cursor"] != "" {
			return errors.New("TON refresh reused a continuation, selected asset or foreign account")
		}
		pageNumber++
		identity := strings.Repeat("a", 64)
		nativeAmount, tokenAmount := "1000000000", "1250000"
		if pageNumber == 2 {
			identity = strings.Repeat("b", 64)
			nativeAmount, tokenAmount = "2000000000", "2500000"
		}
		owner := address.MustParseAddr(account.Address).StringRaw()
		peer := "0:" + strings.Repeat("2", 64)
		transfers := []model.HistoryTransfer{
			{ID: "native", Source: peer, Destination: owner, Asset: assets[0], Atomic: nativeAmount, Comment: "actual native memo"},
			{ID: "usdt", Source: owner, Destination: peer, Asset: assets[1], Atomic: tokenAmount, Comment: "actual jetton memo"},
		}
		if pageNumber == 1 {
			// An outdated symbol or a copied symbol on another jetton does not
			// authorize a displayed asset. Valid effects in the same page survive.
			wrongNative := assets[0]
			wrongNative.Symbol = "TON"
			wrongToken := assets[1]
			wrongToken.Contract = "0:" + strings.Repeat("3", 64)
			transfers = append(transfers,
				model.HistoryTransfer{ID: "wrong-native-policy", Asset: wrongNative, Atomic: "999999999999"},
				model.HistoryTransfer{ID: "wrong-token-policy", Asset: wrongToken, Atomic: "999999999999"})
		}
		page := model.HistoryPage{Scope: "Explicit controller TON native and canonical USDT fixture", Entries: []model.Receipt{{Identity: identity, State: "OBSERVED", Timestamp: 1700000000, Transfers: transfers}}, Complete: pageNumber == 2}
		if pageNumber == 1 {
			page.NextCursor = strings.Repeat("c", 32)
		}
		return answer(out, page)
	}}
	refresh := func() {
		t.Helper()
		a.walletEvent("walletHistory/" + account.ID)
		if a.busy || !a.nextWalletHistory() {
			t.Fatal("Refresh history did not start its read-only observation")
		}
		ui.apply(t)
		state := a.walletHistory[account.ID]
		if state.Loading || !state.Loaded || state.ChainFailed || state.LocalFailed || state.ChainPaused || a.background || a.state.Locked {
			t.Fatal("valid typed TON response was lost or treated as failed/paused", state)
		}
	}
	refresh()
	rows := a.walletHistoryRows(account, a.walletHistory[account.ID])
	if len(rows) != 2 || rows[0].amount != "1 GRAM" || rows[1].amount != "1.25 USDT" || rows[0].comment != "actual native memo" || rows[1].comment != "actual jetton memo" {
		t.Fatal("registered TON effects were filtered or foreign identities displayed", rows)
	}
	a.walletEvent("asset/" + account.ID + "/" + assets[1].ID)
	if a.asset(account) != assets[1] || len(a.walletHistoryRows(account, a.walletHistory[account.ID])) != 2 {
		t.Fatal("selecting USDT narrowed the wallet-wide history")
	}
	refresh()
	state := a.walletHistory[account.ID]
	rows = a.walletHistoryRows(account, state)
	if pageNumber != 2 || state.Chain.NextCursor != "" || !state.Chain.Complete || len(rows) != 2 || rows[0].amount != "2 GRAM" || rows[1].amount != "2.5 USDT" {
		t.Fatal("fresh refresh retained the old page/cursor or omitted an asset", state, rows)
	}
	text := viewText(a.walletHistoryNodes(account))
	if strings.Contains(text, "No matching transfers") || strings.Contains(text, "has not been fully checked") || strings.Contains(text, "wrong-token-policy") {
		t.Fatal("rendered TON history lost its verified fixture rows")
	}
}

func TestTONEmptyIncompleteHistoryExplainsExactAddressAndExplicitRecoveryProfiles(t *testing.T) {
	a, _, core := fixture(t)
	var tonAccount model.Account
	for _, account := range a.state.Accounts {
		if account.Network == "ton-mainnet" {
			tonAccount = account
			break
		}
	}
	if tonAccount.ID == "" {
		t.Fatal("TON fixture missing")
	}
	const hint = "TON history belongs to this receive address. Another wallet ID creates a different address and history from the same recovery phrase. For an earlier wallet, check Settings → Wallet → TON recovery profiles and its original account index."
	requests := 0
	core.handler = func(uint64, string, any, any) error {
		requests++
		return errors.New("history guidance must not discover, create or query another wallet")
	}
	for _, name := range []string{"incomplete", "other-network", "not-loaded", "loading", "queued", "chain-error", "local-error", "paused", "chain-more", "local-more", "completed", "chain-rows", "local-rows", "inactive"} {
		t.Run(name, func(t *testing.T) {
			account := tonAccount
			state := walletHistoryState{Loaded: true, Chain: model.HistoryPage{Scope: "Checked bounded TON native and canonical USDT scope"}}
			a.active = "wallet/" + account.ID
			switch name {
			case "other-network":
				account = ethAccount(t, a)
				a.active = "wallet/" + account.ID
			case "not-loaded":
				state.Loaded = false
			case "loading":
				state.Loading = true
			case "queued":
				state.Request = "refresh"
			case "chain-error":
				state.ChainFailed = true
			case "local-error":
				state.LocalFailed = true
			case "paused":
				state.ChainPaused = true
			case "chain-more":
				state.Chain.NextCursor = strings.Repeat("a", 32)
			case "local-more":
				state.Local.NextCursor = strings.Repeat("b", 32)
			case "completed":
				state.Chain.Complete = true
			case "chain-rows":
				state.Chain.Entries = []model.Receipt{{Identity: strings.Repeat("a", 64), State: "OBSERVED", Transfers: []model.HistoryTransfer{{ID: "native", Asset: a.network(account.Network).Assets[0], Atomic: "1"}}}}
			case "local-rows":
				state.Local.Operations = []journal.Operation{{Plan: model.Plan{ID: "existing-operation", Account: account, Asset: a.network(account.Network).Assets[0].ID, Amount: "1"}, State: "UNKNOWN"}}
			case "inactive":
				a.active = "settings"
			}
			a.walletHistory = map[string]walletHistoryState{account.ID: state}
			before := viewText(a.state)
			nodes := a.walletHistoryNodes(account)
			foundHint := false
			for _, n := range nodes {
				if n.Kind == "label" && n.Text == hint {
					if n.ID != "" || len(n.Children) != 0 {
						t.Fatal("profile guidance acquired an action")
					}
					foundHint = true
				}
			}
			if foundHint != (name == "incomplete") {
				t.Fatal("profile guidance appeared outside a successful empty incomplete TON observation", foundHint)
			}
			if name == "incomplete" && (!strings.Contains(viewText(nodes), state.Chain.Scope) || !strings.Contains(viewText(nodes), "Historical activity has not been fully checked")) {
				t.Fatal("profile guidance replaced the actual observation scope or uncertainty")
			}
			table, found := findNode(nodes, "walletHistoryTable/"+account.ID)
			if !found || strings.Contains(viewText(table), "TON recovery profiles") {
				t.Fatal("profile guidance was placed inside the inert transaction table")
			}
			if requests != 0 || viewText(a.state) != before {
				t.Fatal("guidance queried or changed another recovery profile")
			}
		})
	}
}
