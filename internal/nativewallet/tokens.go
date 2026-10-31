package nativewallet

import (
	"encoding/hex"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
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
	table := node("column", "walletAssets/"+account.ID, "Wallet assets", "")
	for _, asset := range network.Assets {
		value, _ := a.balance(account, asset)
		identity := "ERC-20 contract · " + account.Network + " · " + asset.Contract
		if account.Family == "near" {
			identity = "NEP-141 contract · " + account.Network + " · " + asset.Contract
		} else if account.Family == "ton" {
			identity = "Jetton master · " + account.Network + " · " + asset.Contract
		} else if account.Family == "hypercore" {
			identity = "HyperCore spot token · " + account.Network + " · " + asset.Contract
		}
		if asset.Contract == "" {
			identity = "Native currency · " + account.Network
		}
		selection := a.button("asset/"+account.ID+"/"+asset.ID, asset.Symbol, selected.ID == asset.ID)
		selection.Tooltip = identity
		assetRow := row(selection, label(value), label(strconv.Itoa(int(asset.Decimals))))
		assetRow.ID = "walletAsset/" + account.ID + "/" + asset.ID
		assetRow.Tooltip = identity
		table.Children = append(table.Children, assetRow)
	}
	nodes = append(nodes, table)
	if ethereumTokenNetwork(account) {
		nodes = append(nodes, heading("Track an ERC-20 token"), a.entry("tokenContract/"+account.ID, "Token contract address on "+network.Name), a.button("importToken/"+account.ID, "Verify and add token", !a.state.RecoveryConfirmed))
	} else if nearTokenNetwork(account) {
		nodes = append(nodes, heading("Track a NEAR token"), a.entry("tokenContract/"+account.ID, "Token contract account on "+network.Name), a.button("importToken/"+account.ID, "Verify and add token", !a.state.RecoveryConfirmed), label("Add a NEP-141 token by its exact contract account. The recovery phrase restores this wallet address; add tracked contracts again after a fresh installation."))
	}
	return append(nodes, a.discoveredTokenRows(account)...)
}

func ethereumTokenNetwork(account model.Account) bool {
	return account.Family == "evm" && evm.SupportsTokenImport(account.Network)
}

func nearTokenNetwork(account model.Account) bool {
	return account.Family == "near" && near.SupportsTokenImport(account.Network)
}

func exchangeAssetAllowed(account model.Account, asset model.Asset) bool {
	if account.Family == "near" {
		return asset.ID == account.Network+":native" && asset.Contract == ""
	}
	// New wallet defaults do not add an exchange's jetton or Core spot route.
	// Those ledgers need separately reviewed provider metadata and fee policies.
	if account.Family == "ton" {
		return asset.ID == account.Network+":TON" && asset.Contract == ""
	}
	if account.Family == "hypercore" {
		return strings.HasPrefix(asset.ID, account.Network+":HYPE:") && asset.Symbol == "HYPE"
	}
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
	if !ok || !ethereumTokenNetwork(account) && !nearTokenNetwork(account) || a.state.Locked || a.busy || !a.state.RecoveryConfirmed {
		return true
	}
	contract := strings.TrimSpace(a.fields["tokenContract/"+id])
	if nearTokenNetwork(account) {
		if near.New().ValidateAddress(account.Network, contract) != nil {
			a.notice("Enter the exact NEAR token contract account for this network.")
			a.render()
			return true
		}
	} else if len(contract) != 42 || !strings.HasPrefix(contract, "0x") {
		a.notice("Enter the complete ERC-20 contract address for this network.")
		a.render()
		return true
	} else if _, err := hex.DecodeString(contract[2:]); err != nil {
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
		if nearTokenNetwork(account) && asset.Contract != "" {
			a.readNearTokenStorage(account, asset)
		}
		return
	}
}
