package signer

import (
	"localwallet/internal/derivation"
	"localwallet/internal/vault"
	"sort"
)

// addSupportedNetworks adds the requested Arbitrum domain to existing canonical
// account indices. It never changes an existing descriptor or retired record.
// The caller persists this once with other authenticated unlock transitions.
func (e *Engine) addSupportedNetworks(d *vault.Data) (bool, error) {
	network := "arbitrum-mainnet"
	if d.Environment == "testnet" {
		network = "arbitrum-sepolia"
	}
	adapter, err := e.adapter(network)
	if err != nil {
		return false, nil
	}
	indices, present, visible := map[uint32]bool{}, map[uint32]bool{}, map[uint32]bool{}
	for _, account := range d.Accounts {
		indices[account.Index] = true
		if !account.Hidden {
			visible[account.Index] = true
		}
		if account.Network == network {
			present[account.Index] = true
		}
	}
	var missing []uint32
	for index := range indices {
		if !present[index] {
			missing = append(missing, index)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	if len(d.Accounts)+len(missing) > 10000 {
		return false, ErrDenied
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
	seed, err := derivation.Seed(d.Entropy, d.Passphrase)
	if err != nil {
		return false, err
	}
	defer clear(seed)
	for _, index := range missing {
		account, err := adapter.Derive(seed, index, network)
		if err != nil {
			return false, err
		}
		account.Hidden = !visible[index]
		d.Accounts = append(d.Accounts, account)
	}
	return true, nil
}
