package nativewallet

// These are public controller fixtures. They verify selection, display and
// typed routing; actual ERC-20 metadata/signing/node acceptance is separate.
import (
	"encoding/json"
	"errors"
	"localwallet/internal/model"
	"localwallet/internal/webui"
	"strings"
	"testing"
	"time"
)

const publicUSDCContract = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
const publicDAIContract = "0x6B175474E89094C44Da98b954EedeAC495271d0F"

func tokenFixture(t *testing.T, a *App) (model.Account, model.Asset, model.Asset) {
	t.Helper()
	account := ethAccount(t, a)
	usdc := model.Asset{ID: account.Network + ":erc20:" + publicUSDCContract, Symbol: "USDC", Decimals: 6, Contract: publicUSDCContract}
	dai := model.Asset{ID: account.Network + ":erc20:" + publicDAIContract, Symbol: "DAI", Decimals: 18, Contract: publicDAIContract}
	for i := range a.networks {
		if a.networks[i].ID == account.Network {
			a.networks[i].Assets = append(a.networks[i].Assets, usdc, dai)
		}
	}
	return account, usdc, dai
}

func TestEthereumWalletShowsEveryConfiguredTokenAndNativeBalanceWithExactIdentity(t *testing.T) {
	a, _, _ := fixture(t)
	account, usdc, dai := tokenFixture(t, a)
	native := a.network(account.Network).Assets[0]
	a.balances[account.ID] = []model.Balance{
		{Asset: native.ID, Atomic: "3000000000000000000", State: "provider-observed", At: "public-block"},
		{Asset: usdc.ID, Atomic: "1250001", State: "provider-observed", At: "public-block"},
		{Asset: dai.ID, Atomic: "2000000000000000001", State: "provider-observed", At: "public-block"},
	}
	page := a.walletPage(account)
	for _, check := range []struct {
		asset   model.Asset
		balance string
	}{
		{native, "3 ETH"}, {usdc, "1.250001 USDC"}, {dai, "2.000000000000000001 DAI"},
	} {
		row, ok := findNode(page.Nodes, "walletAsset/"+account.ID+"/"+check.asset.ID)
		if !ok || !strings.Contains(viewText(row), check.balance) {
			t.Fatal("configured token or exact decimal balance missing")
		}
		if row.Children[0].Disabled != (check.asset.ID == native.ID) {
			t.Fatal("selected asset was not explicit")
		}
		if check.asset.Contract != "" && row.Children[2].Value != check.asset.Contract {
			t.Fatal("token contract was truncated or confused with receive address")
		}
	}
	text := viewText(page)
	for _, required := range []string{account.Address, "Decimals: 6", "Decimals: 18", "configured assets only", "same receive address", "never use it as your receive address", "Verify and add token"} {
		if !strings.Contains(text, required) {
			t.Fatal("wallet asset identity or tracking limitation missing")
		}
	}
	if strings.Contains(text, "Provider observed") {
		t.Fatal("provider details were inserted into wallet balances")
	}
	a.selectWalletAsset(account.ID + "/" + usdc.ID)
	overview, _ := findNode(a.groupPage(0).Nodes, "open/"+account.ID)
	if overview.Children[1].Text != "3 ETH" {
		t.Fatal("token selection changed native overview balance")
	}
}

func TestTokenObservationsStayIndependentAndUnavailableIsNeverZero(t *testing.T) {
	a, _, _ := fixture(t)
	account, usdc, dai := tokenFixture(t, a)
	a.balances[account.ID] = []model.Balance{{Asset: usdc.ID, Atomic: "0", State: "provider-observed", At: "public-block"}}
	assertRow := func(asset model.Asset, expected string) {
		t.Helper()
		row, ok := findNode(a.walletPage(account).Nodes, "walletAsset/"+account.ID+"/"+asset.ID)
		if !ok || row.Children[1].Text != expected+" "+asset.Symbol {
			t.Fatal("token's unavailable or observed zero state changed")
		}
	}
	assertRow(usdc, "0")
	assertRow(dai, "Unknown")
	a.balances[account.ID] = append(a.balances[account.ID], a.balances[account.ID][0])
	assertRow(usdc, "Unknown")
	a.balances[account.ID] = []model.Balance{{Asset: usdc.ID, Atomic: "0", State: "unavailable", At: "public-block"}}
	assertRow(usdc, "Unknown")
	a.balanceErrors[account.ID] = true
	assertRow(usdc, "Unknown")
}

func TestTokenSelectionRequiresRegisteredNetworkContractAndInvalidatesOnlyChangedIntent(t *testing.T) {
	a, _, _ := fixture(t)
	account, usdc, _ := tokenFixture(t, a)
	original := &model.Plan{ID: "public-original", Account: account}
	a.plan, a.fields["amount/"+account.ID] = original, "1"
	for _, id := range []string{"missing", "foreign-account/" + usdc.ID, account.ID + "/foreign-token", account.ID + "/base-mainnet:native", account.ID + "/" + a.asset(account).ID} {
		a.selectWalletAsset(id)
		if a.plan != original || a.fields["amount/"+account.ID] != "1" {
			t.Fatal("invalid or unchanged selection altered payment intent")
		}
	}
	a.state.Locked = true
	a.selectWalletAsset(account.ID + "/" + usdc.ID)
	if a.plan != original {
		t.Fatal("locked asset event altered intent")
	}
	a.state.Locked = false
	revision := a.planRevision
	a.selectWalletAsset(account.ID + "/" + usdc.ID)
	if a.asset(account).ID != usdc.ID || a.plan != nil || a.planRevision != revision+1 || a.fields["amount/"+account.ID] != "" {
		t.Fatal("changed token retained old currency amount or authorization")
	}
	a.sendForms[account.ID] = true
	text := viewText(a.walletPage(account))
	if !strings.Contains(text, "Amount (1 means 1 USDC)") || !strings.Contains(text, publicUSDCContract) {
		t.Fatal("selected token units or exact contract missing from wallet")
	}
}

func TestDifferentContractsWithTheSameSymbolRemainSeparateAssets(t *testing.T) {
	a, _, _ := fixture(t)
	account, usdc, dai := tokenFixture(t, a)
	for i := range a.networks {
		if a.networks[i].ID == account.Network {
			for j := range a.networks[i].Assets {
				if a.networks[i].Assets[j].ID == dai.ID {
					a.networks[i].Assets[j].Symbol = usdc.Symbol
				}
			}
		}
	}
	for _, asset := range []model.Asset{usdc, dai} {
		a.walletEvent("asset/" + account.ID + "/" + asset.ID)
		if a.asset(account).ID != asset.ID {
			t.Fatal("symbol collision selected another contract")
		}
		row, ok := findNode(a.walletPage(account).Nodes, "walletAsset/"+account.ID+"/"+asset.ID)
		if !ok || row.Children[2].Value != asset.Contract {
			t.Fatal("colliding symbol lost full contract distinction")
		}
	}
}

func TestImportTokenUsesOnlyContractSelectorThenRefreshesCanonicalNetworksAndBalances(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.fields["tokenContract/"+account.ID] = "  " + publicUSDCContract + "  "
	a.fields["tokenSymbol/"+account.ID] = "Untrusted symbol"
	a.fields["tokenDecimals/"+account.ID] = "0"
	networks := append([]model.Network(nil), a.networks...)
	for i := range networks {
		if networks[i].ID == account.Network {
			networks[i].Assets = append(append([]model.Asset(nil), networks[i].Assets...), model.Asset{ID: account.Network + ":erc20:" + publicUSDCContract, Symbol: "USDC", Decimals: 6, Contract: publicUSDCContract})
		}
	}
	state := a.state
	calls := make(chan string, 3)
	core.handler = func(_ uint64, method string, body, out any) error {
		calls <- method
		switch method {
		case "importToken":
			if viewText(body) != viewText(map[string]string{"accountId": account.ID, "contract": publicUSDCContract}) {
				return errors.New("public import supplied metadata or another account")
			}
			return answer(out, state)
		case "accounts":
			return answer(out, state)
		case "networks":
			return answer(out, networks)
		}
		return errors.New("token import attempted a payment")
	}
	a.walletEvent("importToken/" + account.ID)
	ui.apply(t)
	if a.busy || len(a.balanceQueue) != len(a.state.Accounts) || !strings.Contains(viewText(a.walletPage(account)), publicUSDCContract) {
		t.Fatal("confirmed import did not refresh network asset policy and all balances")
	}
	for _, expected := range []string{"importToken", "accounts", "networks"} {
		if <-calls != expected {
			t.Fatal("token import did not use the normal mutation/snapshot pipeline")
		}
	}
}

func TestTokenImportRejectsMalformedContractsAndUnavailableWalletsBeforeBackend(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	for _, invalid := range []string{"", "0x123", "https://untrusted.invalid/token", "0x" + strings.Repeat("z", 40)} {
		a.fields["tokenContract/"+account.ID] = invalid
		a.walletEvent("importToken/" + account.ID)
		if a.busy || !strings.Contains(a.status, "complete ERC-20 contract address") {
			t.Fatal("malformed contract reached backend")
		}
	}
	a.fields["tokenContract/"+account.ID] = publicUSDCContract
	for _, guard := range []string{"locked", "busy", "pending", "foreign", "other-network"} {
		a.state.Locked, a.busy, a.state.RecoveryConfirmed = guard == "locked", guard == "busy", guard != "pending"
		id := account.ID
		if guard == "foreign" {
			id = "foreign-account"
		}
		if guard == "other-network" {
			for _, current := range a.state.Accounts {
				if current.Network == "base-mainnet" {
					id = current.ID
					if _, found := findNode(a.walletPage(current).Nodes, "importToken/"+id); found {
						t.Fatal("Ethereum-only importer appeared on another network")
					}
				}
			}
		}
		a.walletEvent("importToken/" + id)
		if a.busy != (guard == "busy") {
			t.Fatal("unavailable import entered backend")
		}
	}
}

func TestCancelledTokenImportDoesNotInstallMetadataOrChangeWalletKeys(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	before := viewText(a.networks)
	a.fields["tokenContract/"+account.ID] = publicUSDCContract
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "importToken" {
			t.Error("cancelled import refreshed or authorized another request")
		}
		return errors.New("private-provider-error-marker")
	}
	a.walletEvent("importToken/" + account.ID)
	ui.apply(t)
	current, ok := a.account(account.ID)
	if viewText(a.networks) != before || !ok || current.Address != account.Address || strings.Contains(a.status, "private-provider-error-marker") {
		t.Fatal("failed import changed policy, account identity or exposed provider error")
	}
}

func TestImportedERC20WalletAssetsCannotBeOfferedAsReviewedBybitRoutes(t *testing.T) {
	a, _, _ := fixture(t)
	account, usdc, dai := tokenFixture(t, a)
	a.fields["exchangeAccount"], a.fields["exchangeAsset"] = account.ID, usdc.ID
	a.exchange = json.RawMessage(`{"connected":true}`)
	page := a.exchangePage()
	selector, ok := findNode(page, "field/exchangeAsset")
	if !ok || len(selector.Options) != 2 {
		t.Fatal("pinned Ethereum ETH and USDT routes changed")
	}
	for _, option := range selector.Options {
		if option.ID == usdc.ID || option.ID == dai.ID {
			t.Fatal("custom token was offered as a reviewed Bybit route")
		}
	}
	for _, id := range []string{"exchangeQuote", "exchangeSwap", "exchangeDeposit", "exchangeWithdraw"} {
		node, ok := findNode(page, id)
		if !ok || !node.Disabled {
			t.Fatal("stale unsupported selection retained a Bybit action")
		}
	}
	a.ensureExchangeSelection()
	if a.fields["exchangeAsset"] != account.Network+":native" {
		t.Fatal("exchange did not reset unsupported imported token selection")
	}
	if _, ok := findNode(a.walletPage(account).Nodes, "asset/"+account.ID+"/"+usdc.ID); !ok {
		t.Fatal("filtering exchange routes removed ordinary token transfers")
	}
}

func TestEverySelectedTokenUsesNormalExactSendAndTheOwnedReceiveAddress(t *testing.T) {
	a, ui, core := fixture(t)
	account, usdc, _ := tokenFixture(t, a)
	a.walletEvent("asset/" + account.ID + "/" + usdc.ID)
	a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = "0x2222222222222222222222222222222222222222", "1.000001"
	want := model.Transfer{AccountID: account.ID, Asset: usdc.ID, Recipient: a.fields["recipient/"+account.ID], Amount: "1.000001"}
	plan := model.Plan{ID: "public-token-plan", Account: account, Asset: usdc.ID, Amount: "1000001", Recipient: want.Recipient, FeeAsset: account.Network + ":native", MaxFee: "21000000000000", Expires: time.Now().Add(time.Minute).Unix()}
	routeFixture(t, a, core, "prepare", want, plan)
	a.event(webui.Event{ID: "prepare/" + account.ID})
	ui.apply(t)
	if a.plan == nil || a.plan.Asset != usdc.ID || a.plan.Amount != "1000001" || a.plan.Recipient != want.Recipient {
		t.Fatal("selected token did not enter exact normal prepare pipeline")
	}
	a.sendForms[account.ID] = true
	text := viewText(a.walletPage(account))
	if !strings.Contains(text, "1.000001 USDC") || !strings.Contains(text, "0.000021000000000000 ETH") {
		t.Fatal("token whole units or separate native fee currency were hidden")
	}
	routeFixture(t, a, core, "receive", map[string]string{"accountId": account.ID}, json.RawMessage(`{"ok":true}`))
	a.walletEvent("receive/" + account.ID)
	ui.apply(t)
	if a.plan == nil || a.plan.ID != plan.ID || a.asset(account).ID != usdc.ID || !strings.Contains(viewText(a.walletPage(account)), account.Address) {
		t.Fatal("token receive replaced the owned address or discarded a prepared payment")
	}
}
