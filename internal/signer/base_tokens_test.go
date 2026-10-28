package signer

import (
	"reflect"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/model"
)

func TestBaseERC20PolicyRequiresPrivateApprovalAndSurvivesEncryptedFreshEngine(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			e, ui, _ := newEngine(t, chains.All())
			createImported(t, e, ui, environment)
			network := "base-mainnet"
			if environment == "testnet" {
				network = "base-sepolia"
			}
			account := findAccount(t, e, network)
			before := e.Snapshot().Accounts
			token := signerToken(network)
			ui.approve = false
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if e.ImportToken(account.ID, token) == nil || len(e.TokenPolicies()) != 0 {
				t.Fatal("Base metadata authorized without confirmation")
			}
			ui.inputs = nil
			ui.approve = true
			ui.inputs = append(ui.inputs, input("public-wrong-password"))
			if e.ImportToken(account.ID, token) == nil || len(e.TokenPolicies()) != 0 {
				t.Fatal("Base import bypassed current password")
			}
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := e.ImportToken(account.ID, token); err != nil {
				t.Fatal(err)
			}
			e.Lock()
			fresh := New(e.Files, ui, chains.All())
			fresh.parameters = e.parameters
			t.Cleanup(fresh.Lock)
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			if err := fresh.Unlock(); err != nil {
				t.Fatal("Base policy prevented unlock", err)
			}
			if !reflect.DeepEqual(before, fresh.Snapshot().Accounts) || !reflect.DeepEqual(fresh.TokenPolicies(), []model.ERC20Token{token}) {
				t.Fatal("Base watchlist changed deterministic wallet or disappeared")
			}
			adapter, err := fresh.adapter(network)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, n := range adapter.Networks() {
				if n.ID == network {
					for _, asset := range n.Assets {
						found = found || asset == token.Asset
					}
				}
			}
			if !found {
				t.Fatal("Base token not reloaded into the exact adapter")
			}
		})
	}
}
