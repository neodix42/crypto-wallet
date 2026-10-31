package nativewallet

// Public controller contracts only. Real token bytes, signatures and network
// executions are verified by the adapter/oracle suites, not by these fixtures.
import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func TestStablecoinDefaultsAllNineteenAssetsHavePreciseRowsAndOwnedReceive(t *testing.T) {
	a, _, _ := fixture(t)
	expected := map[string]int{"ethereum-mainnet": 3, "arbitrum-mainnet": 4, "base-mainnet": 4, "hyperevm-mainnet": 2, "hypercore-mainnet": 2, "ton-mainnet": 2, "near-mainnet": 1, "aptos-mainnet": 1}
	count := 0
	for _, account := range a.state.Accounts {
		network := a.network(account.Network)
		if len(network.Assets) != expected[network.ID] {
			t.Fatal("default assets missing", network.ID, network.Assets)
		}
		for _, asset := range network.Assets {
			count++
			a.balances[account.ID] = append(a.balances[account.ID], model.Balance{Asset: asset.ID, Atomic: "1" + strings.Repeat("0", int(asset.Decimals)), State: "provider-observed", At: "public-verified-position"})
		}
		if len(network.Assets) < 2 {
			continue
		}
		for _, asset := range network.Assets {
			a.selectWalletAsset(account.ID + "/" + asset.ID)
			a.sendForms[account.ID] = true
			page := a.walletPage(account)
			row, ok := findNode(page.Nodes, "walletAsset/"+account.ID+"/"+asset.ID)
			if !ok || len(row.Children) != 3 || row.Children[0].Text != asset.Symbol || row.Children[1].Text != "1" || row.Children[2].Text != strconv.Itoa(int(asset.Decimals)) || !row.Children[0].Disabled {
				t.Fatal("default asset missing/incorrect selected whole units", asset, row)
			}
			if !strings.Contains(row.Tooltip, network.ID) || (asset.Contract != "" && !strings.Contains(row.Tooltip, asset.Contract)) {
				t.Fatal("exact asset identity missing", asset, row.Tooltip)
			}
			if asset.Contract != "" {
				want := "ERC-20 contract"
				if account.Family == "ton" {
					want = "Jetton master"
				}
				if account.Family == "hypercore" {
					want = "HyperCore spot token"
				}
				if !strings.Contains(row.Tooltip, want) {
					t.Fatal("token kind confused with ERC20 contract", asset, row.Tooltip)
				}
			}
			maximum, ok := findNode(page.Nodes, "max/"+account.ID)
			if !ok || maximum.Text != "Max" || maximum.Disabled || !strings.Contains(maximum.Tooltip, "native gas balance") || !strings.Contains(maximum.Tooltip, "remainder") {
				t.Fatal("compact Max or honest fee hint absent", maximum)
			}
			input, ok := findNode(page.Nodes, "field/amount/"+account.ID)
			if !ok || input.Text != "Amount (1 means 1 "+asset.Symbol+")" {
				t.Fatal("whole-token input denomination wrong", asset, input)
			}
			compact := false
			for _, item := range page.Nodes {
				if item.Kind == "row" && len(item.Children) == 2 && item.Children[0].ID == "field/amount/"+account.ID && item.Children[1].ID == "max/"+account.ID {
					compact = true
				}
			}
			if !compact {
				t.Fatal("Max is not beside Amount", asset)
			}
			copy, ok := findNode(page.Nodes, "copyReceive/"+account.ID)
			if !ok || copy.Value != account.Address || copy.Disabled {
				t.Fatal("receiving/copying does not use the owned account", asset, copy)
			}
			text := viewText(page)
			if !strings.Contains(text, account.Address) || strings.Contains(text, "Provider observed") {
				t.Fatal("receive address missing or hidden observation leaked into balance", text)
			}
		}
	}
	if count != 19 {
		t.Fatal("default network asset coverage changed", count)
	}
}
func TestStablecoinDefaultMaxUsesTypedSelectedAssetAndRegularOwnedReceiveWithoutSpending(t *testing.T) {
	for _, networkID := range []string{"ethereum-mainnet", "arbitrum-mainnet", "base-mainnet", "hyperevm-mainnet", "hypercore-mainnet", "ton-mainnet"} {
		a, ui, core := fixture(t)
		var account model.Account
		for _, candidate := range a.state.Accounts {
			if candidate.Network == networkID {
				account = candidate
			}
		}
		for _, asset := range a.network(networkID).Assets {
			if asset.Symbol != "USDC" && asset.Symbol != "USDT" && asset.Symbol != "USDT0" {
				continue
			}
			t.Run(asset.ID, func(t *testing.T) {
				a.selectWalletAsset(account.ID + "/" + asset.ID)
				a.active = "wallet/" + account.ID
				a.fields["recipient/"+account.ID] = "public-recipient-for-" + networkID
				a.fields["amount/"+account.ID] = "2.5"
				request := model.Transfer{AccountID: account.ID, Asset: asset.ID, Recipient: a.fields["recipient/"+account.ID], Amount: "2.5"}
				atomic := "1" + strings.Repeat("0", int(asset.Decimals)-1) + "1"
				plan := model.Plan{ID: "public-exact-default-" + asset.ID, Account: account, Asset: asset.ID, Amount: atomic, Recipient: request.Recipient, FeeAsset: a.network(networkID).Assets[0].ID, MaxFee: "1", Expires: time.Now().Add(time.Minute).Unix()}
				routeFixture(t, a, core, "max", request, plan)
				a.event(webui.Event{ID: "max/" + account.ID})
				ui.apply(t)
				want := "1." + strings.Repeat("0", int(asset.Decimals)-1) + "1"
				if a.plan == nil || a.plan.ID != plan.ID || a.plan.Asset != asset.ID || a.fields["amount/"+account.ID] != want {
					t.Fatal("default Max mixed atomic and whole units", a.plan, a.fields)
				}
				routeFixture(t, a, core, "receive", map[string]string{"accountId": account.ID}, json.RawMessage(`{"ok":true}`))
				a.event(webui.Event{ID: "receive/" + account.ID})
				ui.apply(t)
				if a.plan == nil || a.plan.ID != plan.ID || a.asset(account).ID != asset.ID || !strings.Contains(viewText(a.walletPage(account)), account.Address) {
					t.Fatal("receive changed token intent or used contract as owned address")
				}
				// Editing the selected currency invalidates old authorization; it does not
				// invoke send, signing or any automatic replacement of the prepared bytes.
				native := a.network(networkID).Assets[0]
				a.selectWalletAsset(account.ID + "/" + native.ID)
				if a.plan != nil || a.fields["amount/"+account.ID] != "" {
					t.Fatal("currency switch retained old payment")
				}
			})
		}
	}
}
func TestStablecoinDefaultMaxRequiresRecipientAndCancellationNeverSends(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	asset := a.network(account.Network).Assets[2]
	a.selectWalletAsset(account.ID + "/" + asset.ID)
	core.handler = func(_ uint64, method string, body, out any) error {
		t.Errorf("recipient-less Max called backend %s", method)
		return nil
	}
	a.event(webui.Event{ID: "max/" + account.ID})
	if a.busy || a.plan != nil || !strings.Contains(a.status, "Enter a recipient first") {
		t.Fatal("Max estimated without a recipient")
	}
	a.fields["recipient/"+account.ID] = "public-recipient"
	routeFixture(t, a, core, "max", model.Transfer{AccountID: account.ID, Asset: asset.ID, Recipient: "public-recipient"}, model.Plan{ID: "public-cancelled-default", Account: account, Asset: asset.ID, Amount: "1000000"})
	// A failed/cancelled operation must not install its returned placeholder plan.
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "max" {
			t.Errorf("cancelled Max made unrelated request %s", method)
		}
		return errors.New("synthetic user cancelled estimate")
	}
	a.event(webui.Event{ID: "max/" + account.ID})
	ui.apply(t)
	if a.plan != nil || a.fields["amount/"+account.ID] != "" {
		t.Fatal("cancelled Max installed a spendable plan")
	}
}
