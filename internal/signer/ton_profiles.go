package signer

import (
	"fmt"

	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
	"localwallet/internal/uiprompt"
)

// AddTONAccount explicitly restores one registered TON contract identity. It
// never changes a funded account's contract/ID/address or derives arbitrary
// caller-selected paths, StateInit, wallet IDs or code.
func (e *Engine) AddTONAccount(index uint32, profile string) error {
	if !e.canAuthorize() || index >= derivation.MaxIndex || (profile != ton.Profile && profile != ton.ProfileSubwallet42) {
		return ErrDenied
	}
	data, generation := e.data, e.data.Generation
	network := "ton-mainnet"
	if e.data.Environment == "testnet" {
		network = "ton-testnet"
	}
	adapter, err := e.adapter(network)
	if err != nil {
		return ErrDenied
	}
	chain, ok := adapter.(*ton.Adapter)
	if !ok {
		return ErrDenied
	}
	seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
	if err != nil {
		return ErrDenied
	}
	defer clear(seed)
	account, err := chain.DeriveProfile(seed, index, network, profile)
	if err != nil {
		return ErrDenied
	}
	for i, existing := range data.Accounts {
		if existing.ID == account.ID {
			if ton.ValidateRecovery(seed, existing) != nil {
				return ErrDenied
			}
			visibility := "This exact profile is already available. Open its existing wallet; no duplicate will be created."
			if existing.Hidden {
				visibility = "This exact profile is already stored but hidden. Recovering it makes this existing wallet visible; no duplicate will be created."
			}
			if !e.UI.Confirm("Recover existing TON profile", fmt.Sprintf("Network: %s\nIndex: %d\nRecovery profile: %s\nReceiving address: %s\n%s\nExisting contract parameters, addresses and pending operations stay unchanged. No network request, deployment or payment is performed.", network, index, profile, existing.Address, visibility)) {
				return uiprompt.ErrCancelled
			}
			if e.data != data || !e.canAuthorize() || data.Generation != generation {
				return ErrDenied
			}
			if !existing.Hidden {
				return nil
			}
			data.Accounts[i].Hidden = false
			return e.persist()
		}
	}
	if len(data.Accounts) >= 10000 {
		return ErrDenied
	}
	if !e.UI.Confirm("Add exact TON recovery profile", fmt.Sprintf("Network: %s\nIndex: %d\nRecovery profile: %s\nReceiving address: %s\nThis derives a separate Wallet V5R1 contract. Existing addresses, contract parameters and pending payments stay unchanged. Keep this profile and index with your recovery record; a different wallet ID has a different receiving address. No network request or deployment is performed.", network, index, profile, account.Address)) {
		return uiprompt.ErrCancelled
	}
	if e.data != data || !e.canAuthorize() || data.Generation != generation {
		return ErrDenied
	}
	data.Accounts = append(data.Accounts, account)
	return e.persist()
}
