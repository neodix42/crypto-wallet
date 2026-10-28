package nativewallet

import (
	"localwallet/internal/amount"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

type tokenDiscoveryState struct {
	Result evm.TokenDiscovery
	Failed bool
}

func baseTokenNetwork(account model.Account) bool {
	return account.Family == "evm" && (account.Network == "base-mainnet" || account.Network == "base-sepolia")
}

func validDiscoveredTokens(result evm.TokenDiscovery, account model.Account) bool {
	if len(result.Items) > evm.MaxImportedTokens || result.Unavailable < 0 || result.Unavailable > 256 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		if evm.ValidateTokenPolicy(item.Token) != nil || item.Token.Network != account.Network || item.Balance.Asset != item.Token.Asset.ID || item.Balance.State != "provider-observed" || !completeObservation(item.Balance) || seen[item.Token.Asset.ID] {
			return false
		}
		if _, err := amount.Atomic(item.Balance.Atomic); err != nil {
			return false
		}
		seen[item.Token.Asset.ID] = true
	}
	return true
}

func (a *App) discoverBaseTokens(id string) {
	account, ok := a.account(id)
	if !ok || !baseTokenNetwork(account) || a.state.Locked || a.busy || a.closed {
		return
	}
	a.work("Checking Base token candidates…", func(g uint64) (func(), error) {
		var result evm.TokenDiscovery
		err := a.callAt(g, "discoverTokens", map[string]string{"accountId": id}, &result)
		failed := err != nil || !validDiscoveredTokens(result, account)
		if failed {
			result = evm.TokenDiscovery{}
		}
		return func() {
			if a.tokenDiscovery == nil {
				a.tokenDiscovery = map[string]tokenDiscoveryState{}
			}
			a.tokenDiscovery[id] = tokenDiscoveryState{Result: result, Failed: failed}
		}, nil
	})
}

func (a *App) discoveredTokenRows(account model.Account) []webui.Node {
	if !baseTokenNetwork(account) {
		return nil
	}
	nodes := []webui.Node{heading("Discover Base tokens"), label("Blockscout learns this public Base address and supplies candidate contracts. Go verifies metadata and balances through Base RPC. Discovery may omit tokens; it never authorizes spending. Ignore unsolicited spam tokens."), a.button("discoverTokens/"+account.ID, "Refresh token discovery", false)}
	state, ok := a.tokenDiscovery[account.ID]
	if !ok {
		return append(nodes, label("Token discovery has not completed. ZEN and tracked balances appear above; any other standard token can be added by its exact Base contract."))
	}
	if state.Failed {
		return append(nodes, label("Token discovery unavailable. Inventory is unknown; add tokens by exact Base contract address. Tracked balances remain independent."))
	}
	if state.Result.Limited || state.Result.Unavailable > 0 {
		nodes = append(nodes, label("Partial token discovery: some candidates could not be verified or exceeded the bounded query. Missing tokens and failed queries are not zero balances."))
	}
	if len(state.Result.Items) == 0 {
		nodes = append(nodes, label("No additional positive token balances were verified in this indexer response. This is not a complete token inventory."))
	}
	for _, item := range state.Result.Items {
		asset := item.Token.Asset
		value, err := amount.Format(item.Balance.Atomic, asset.Decimals)
		if err != nil {
			continue
		}
		nodes = append(nodes, row(label(trimDecimal(value)+" "+asset.Symbol+" · unapproved token"), node("address", "", "Base ERC-20 contract", asset.Contract), a.button("importDiscoveredToken/"+account.ID+"/"+asset.Contract, "Verify and add token", !a.state.RecoveryConfirmed)))
	}
	return nodes
}
