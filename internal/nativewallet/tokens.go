package nativewallet

import (
	"encoding/hex"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/webui"
	"strconv"
	"strings"
)

// Token identity is its verified network and full contract, never its symbol.
// Every configured asset is displayed, including unknown and zero balances.
func (a *App) walletAssets(account model.Account, selected model.Asset) []webui.Node {
	network := a.network(account.Network)
	nodes := []webui.Node{heading("Assets on " + network.Name)}
	for _, asset := range network.Assets {
		value, _ := a.balance(account, asset)
		identity := node("address", "", "ERC-20 contract · "+account.Network, asset.Contract)
		if asset.Contract == "" {
			identity = label("Native currency · " + account.Network)
		}
		selection := a.button("asset/"+account.ID+"/"+asset.ID, asset.Symbol, selected.ID == asset.ID)
		selection.Tooltip = "Select this exact asset for Send and Receive."
		assetRow := row(selection, label(value+" "+asset.Symbol), identity, label("Decimals: "+strconv.Itoa(int(asset.Decimals))))
		assetRow.ID = "walletAsset/" + account.ID + "/" + asset.ID
		nodes = append(nodes, assetRow)
	}
	nodes = append(nodes, label("Balances cover configured assets only. A standard EVM RPC cannot enumerate every token held by an address. Unknown balances are not zero."))
	if ethereumTokenNetwork(account) {
		nodes = append(nodes, label("ETH and these ERC-20 tokens use the wallet's same receive address on this network. The token contract identifies the asset; never use it as your receive address."), heading("Track an ERC-20 token"), a.entry("tokenContract/"+account.ID, "Token contract address on "+network.Name), a.button("importToken/"+account.ID, "Verify and add token", !a.state.RecoveryConfirmed), label("Go queries the exact contract, then shows its network, symbol and decimals in a private password-authenticated confirmation. Token names and metadata do not establish legitimacy. Adding a token does not send funds."))
	}
	return append(nodes, a.discoveredTokenRows(account)...)
}

func ethereumTokenNetwork(account model.Account) bool {
	return account.Family == "evm" && evm.SupportsTokenImport(account.Network)
}

func exchangeAssetAllowed(account model.Account, asset model.Asset) bool {
	return account.Family != "evm" || evm.IsPinnedAsset(account.Network, asset.ID)
}

func (a *App) tokenEvent(kind, id string) bool {
	if kind == "open" {
		if _, present := a.tokenDiscovery[id]; !present {
			a.discoverBaseTokens(id)
		}
		return false
	}
	if kind == "discoverTokens" {
		a.discoverBaseTokens(id)
		return true
	}
	if kind == "importDiscoveredToken" {
		parts := strings.SplitN(id, "/", 2)
		if len(parts) != 2 {
			return true
		}
		for _, item := range a.tokenDiscovery[parts[0]].Result.Items {
			if item.Token.Asset.Contract == parts[1] {
				a.fields["tokenContract/"+parts[0]] = parts[1]
				a.tokenEvent("importToken", parts[0])
				break
			}
		}
		return true
	}
	if kind != "importToken" {
		return false
	}
	account, ok := a.account(id)
	if !ok || !ethereumTokenNetwork(account) || a.state.Locked || a.busy || !a.state.RecoveryConfirmed {
		return true
	}
	contract := strings.TrimSpace(a.fields["tokenContract/"+id])
	if len(contract) != 42 || !strings.HasPrefix(contract, "0x") {
		a.notice("Enter the complete ERC-20 contract address for this network.")
		a.render()
		return true
	}
	if _, err := hex.DecodeString(contract[2:]); err != nil {
		a.notice("Enter the complete ERC-20 contract address for this network.")
		a.render()
		return true
	}
	// Only a contract selector crosses this public path. Authoritative metadata,
	// confirmation and persistence remain in Go core/signer, not form fields.
	a.mutation("importToken", map[string]string{"accountId": id, "contract": contract})
	return true
}

func (a *App) selectWalletAsset(id string) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || a.state.Locked {
		return
	}
	account, ok := a.account(parts[0])
	if !ok {
		return
	}
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID != parts[1] || asset.ID == a.asset(account).ID {
			continue
		}
		a.fields["asset/"+account.ID] = asset.ID
		a.fields["amount/"+account.ID] = ""
		a.plan = nil
		a.planIsStaking = false
		a.planRevision++
		a.render()
		return
	}
}
