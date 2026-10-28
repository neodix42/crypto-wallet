package nativewallet

import (
	"errors"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

func baseAccount(t *testing.T, a *App) model.Account {
	t.Helper()
	for _, account := range a.state.Accounts {
		if account.Network == "base-mainnet" {
			return account
		}
	}
	t.Fatal("Base wallet missing")
	return model.Account{}
}
func publicDiscovery(account model.Account) evm.TokenDiscovery {
	token := model.ERC20Token{Network: account.Network, Asset: model.Asset{ID: account.Network + ":erc20:" + publicUSDCContract, Contract: publicUSDCContract, Symbol: "USDC", Decimals: 6}, CodeHash: "0x" + strings.Repeat("a", 64)}
	return evm.TokenDiscovery{Items: []evm.DiscoveredToken{{Token: token, Balance: model.Balance{Asset: token.Asset.ID, Atomic: "1250001", State: "provider-observed", At: "public-block"}}}}
}
func TestBaseTokenDiscoveryAutomaticallyShowsVerifiedCandidatesWithoutInstallingPolicy(t *testing.T) {
	a, ui, core := fixture(t)
	account := baseAccount(t, a)
	before := viewText(a.networks)
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "discoverTokens" || viewText(body) != viewText(map[string]string{"accountId": account.ID}) {
			t.Error("discovery crossed signing or foreign account authority")
		}
		return answer(out, publicDiscovery(account))
	}
	a.walletEvent("open/" + account.ID)
	ui.apply(t)
	text := viewText(a.walletPage(account))
	for _, required := range []string{"1.250001 USDC", publicUSDCContract, "unapproved token", "never authorizes spending", "Verify and add token"} {
		if !strings.Contains(text, required) {
			t.Fatal("verified discovered balance/identity absent", required)
		}
	}
	if viewText(a.networks) != before || a.plan != nil || exchangeAssetAllowed(account, publicDiscovery(account).Items[0].Token.Asset) {
		t.Fatal("discovery installed spend or exchange policy")
	}
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "importToken" || viewText(body) != viewText(map[string]string{"accountId": account.ID, "contract": publicUSDCContract}) {
			t.Error("candidate import supplied trusted metadata or payment")
		}
		return errors.New("public-cancel")
	}
	a.walletEvent("importDiscoveredToken/" + account.ID + "/" + publicUSDCContract)
	ui.apply(t)
	if viewText(a.networks) != before {
		t.Fatal("cancelled discovered policy persisted")
	}
}

func TestBaseDiscoveryUnknownPartialEmptyAndMalformedObservationsStayDistinct(t *testing.T) {
	for _, mode := range []string{"failure", "malformed", "partial", "empty"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := baseAccount(t, a)
			core.handler = func(_ uint64, _ string, _ any, out any) error {
				if mode == "failure" {
					return errors.New("PRIVATE-provider-canary")
				}
				result := publicDiscovery(account)
				switch mode {
				case "malformed":
					result.Items[0].Token.Network = "ethereum-mainnet"
				case "partial":
					result.Limited = true
					result.Unavailable = 2
				case "empty":
					result.Items = nil
				}
				return answer(out, result)
			}
			a.tokenEvent("discoverTokens", account.ID)
			ui.apply(t)
			text := viewText(a.discoveredTokenRows(account))
			if strings.Contains(text, "PRIVATE-provider-canary") {
				t.Fatal("provider text exposed")
			}
			if (mode == "failure" || mode == "malformed") && !strings.Contains(text, "Inventory is unknown") {
				t.Fatal("failed discovery presented zero inventory")
			}
			if mode == "partial" && !strings.Contains(text, "Partial token discovery") {
				t.Fatal("partial result presented complete inventory")
			}
			if mode == "empty" && !strings.Contains(text, "not a complete token inventory") {
				t.Fatal("empty provider inventory claimed full coverage")
			}
		})
	}
}

func TestBaseDiscoveryRejectsInvalidPublicCandidatePoliciesAndStaleSessions(t *testing.T) {
	a, ui, core := fixture(t)
	account := baseAccount(t, a)
	result := publicDiscovery(account)
	if !validDiscoveredTokens(result, account) {
		t.Fatal("valid public candidate rejected")
	}
	for _, mode := range []string{"too many", "negative", "unbounded", "wrong network", "wrong balance", "duplicate", "bad amount", "unavailable", "bad policy"} {
		bad := publicDiscovery(account)
		switch mode {
		case "too many":
			bad.Items = make([]evm.DiscoveredToken, 33)
		case "negative":
			bad.Unavailable = -1
		case "unbounded":
			bad.Unavailable = 257
		case "wrong network":
			bad.Items[0].Token.Network = "ethereum-mainnet"
		case "wrong balance":
			bad.Items[0].Balance.Asset = "foreign"
		case "duplicate":
			bad.Items = append(bad.Items, bad.Items[0])
		case "bad amount":
			bad.Items[0].Balance.Atomic = strings.Repeat("9", 79)
		case "unavailable":
			bad.Items[0].Balance.State = "unavailable"
			bad.Items[0].Balance.Atomic = ""
		case "bad policy":
			bad.Items[0].Token.CodeHash = "bad"
		}
		if validDiscoveredTokens(bad, account) {
			t.Fatal("malformed discovered policy displayed", mode)
		}
	}
	core.handler = func(_ uint64, _ string, _ any, out any) error { return answer(out, result) }
	a.tokenEvent("discoverTokens", account.ID)
	a.lock()
	ui.apply(t)
	if a.tokenDiscovery != nil || !a.state.Locked {
		t.Fatal("stale discovery restored locked account data")
	}
	for _, id := range []string{"missing", ethAccountIDForDiscovery(a)} {
		a.discoverBaseTokens(id)
		if a.busy {
			t.Fatal("locked/foreign discovery began")
		}
	}
	if a.discoveredTokenRows(model.Account{Network: "ethereum-mainnet"}) != nil {
		t.Fatal("Base indexer enabled on another network")
	}
	if !a.tokenEvent("importDiscoveredToken", "bad") || !a.tokenEvent("importDiscoveredToken", account.ID+"/foreign") {
		t.Fatal("invalid selector not consumed")
	}
}

func ethAccountIDForDiscovery(a *App) string {
	for _, account := range a.state.Accounts {
		if account.Network == "ethereum-mainnet" {
			return account.ID
		}
	}
	return "missing"
}
