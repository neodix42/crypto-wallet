package chains

import (
	"bytes"
	"localwallet/internal/chains/decred"
	"localwallet/internal/model"
	"testing"
)

func TestRetiredDecredDescriptorsRemainRootAuthenticatedWithoutAnActiveRoute(t *testing.T) {
	seed := bytes.Repeat([]byte{0x27}, 64)
	for _, network := range []string{"decred-mainnet", "decred-testnet3", "decred-simnet"} {
		account, err := decred.New().Derive(seed, 127, network)
		if err != nil {
			t.Fatal(err)
		}
		if !IsRetiredNetwork(network) || ValidateRetiredAccount(seed, account) != nil || !ValidateRetiredContact(network, account.Address) {
			t.Fatal("valid historical descriptor/contact rejected", network)
		}
		wrong := bytes.Repeat([]byte{0x28}, 64)
		if ValidateRetiredAccount(wrong, account) == nil || ValidateRetiredContact(network, "invalid-address") {
			t.Fatal("foreign/invalid historical recovery accepted")
		}
		for _, adapter := range All() {
			for _, active := range adapter.Networks() {
				if active.ID == network {
					t.Fatal("retired network became an active RPC route")
				}
			}
		}
	}
	for _, network := range []string{"other-mainnet", "arbitrum-mainnet", "ethereum-mainnet"} {
		if IsRetiredNetwork(network) || ValidateRetiredContact(network, "any") || ValidateRetiredAccount(seed, model.Account{Network: network}) == nil {
			t.Fatal("unknown/active identity treated as retired")
		}
	}
}
