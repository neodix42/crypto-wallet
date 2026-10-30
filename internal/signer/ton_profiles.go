package signer

import (
	"fmt"

	"localwallet/internal/chains/ton"
	"localwallet/internal/derivation"
)

// AddTONAccount explicitly restores one registered TON contract identity. It
// never changes a funded account's contract/ID/address or derives arbitrary
// caller-selected paths, StateInit, wallet IDs or code.
func (e *Engine) AddTONAccount(index uint32, profile string) error {
	if !e.canAuthorize() || index >= derivation.MaxIndex || (profile != ton.Profile && profile != ton.ProfileSubwallet42) || len(e.data.Accounts) >= 10000 {
		return ErrDenied
	}
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
	for _, existing := range e.data.Accounts {
		if existing.ID == account.ID {
			return ErrDenied
		}
	}
	if !e.UI.Confirm("Add exact TON recovery profile", fmt.Sprintf("Network: %s\nIndex: %d\nRecovery profile: %s\nReceiving address: %s\nThis derives a separate Wallet V5R1 contract. Existing addresses, contract parameters and pending payments stay unchanged. Keep this profile and index with your recovery record; a different wallet ID has a different receiving address. No network request or deployment is performed.", network, index, profile, account.Address)) || !e.canAuthorize() {
		return ErrDenied
	}
	e.data.Accounts = append(e.data.Accounts, account)
	return e.persist()
}
