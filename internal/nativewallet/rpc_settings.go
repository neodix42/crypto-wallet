package nativewallet

import (
	"sort"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func (a *App) rpcSettingsNetworks() []model.Network {
	owned := map[string]bool{}
	for _, account := range a.state.Accounts {
		owned[account.Network] = true
	}
	var networks []model.Network
	for _, network := range a.networks {
		if owned[network.ID] && network.Testnet == (a.environment == "testnet") {
			networks = append(networks, network)
		}
	}
	sort.Slice(networks, func(i, j int) bool { return networks[i].Name < networks[j].Name })
	return networks
}

func (a *App) rpcSettingsPage() []webui.Node {
	networks := a.rpcSettingsNetworks()
	options := make([]webui.Option, 0, len(networks))
	selected := false
	for _, network := range networks {
		options = append(options, webui.Option{ID: network.ID, Text: network.Name})
		selected = selected || a.fields["rpcNetwork"] == network.ID
	}
	if !selected && len(networks) > 0 {
		a.fields["rpcNetwork"] = networks[0].ID
	}
	return []webui.Node{heading("Trusted RPC connections"), a.combo("rpcNetwork", "Network", options), label("Select the network for your wallet. Its RPC setting applies to all accounts on that network. Endpoint and credentials are stored encrypted; enter them in the configuration dialog."), a.button("configureRPC", "Configure trusted RPC", len(networks) == 0)}
}

func (a *App) configureSelectedRPC() {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return
	}
	for _, network := range a.rpcSettingsNetworks() {
		if network.ID == a.fields["rpcNetwork"] {
			a.mutation("configure", map[string]string{"network": network.ID})
			return
		}
	}
	a.errorText("Configure trusted RPC", "Choose one of your wallet networks before configuring its RPC.")
	a.render()
}
