package chains

import (
	"errors"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

// Retired networks retain authenticated recovery data, never an active RPC route.
func IsRetiredNetwork(network string) bool {
	return evm.IsRetiredNetwork(network) || network == "decred-mainnet" || network == "decred-testnet3" || network == "decred-simnet"
}

func ValidateRetiredAccount(seed []byte, account model.Account) error {
	if evm.IsRetiredNetwork(account.Network) {
		return evm.ValidateRetiredAccount(seed, account)
	}
	if !IsRetiredNetwork(account.Network) || account.Family != "decred" {
		return errors.New("invalid retired account")
	}
	expected, err := decred.New().Derive(seed, account.Index, account.Network)
	if err != nil || decred.ValidateRecovery(expected, account) != nil || decred.New().ValidateAddress(account.Network, account.Address) != nil {
		return errors.New("retired Decred recovery descriptor mismatch")
	}
	return nil
}

// ValidateRetiredContact authenticates stored contacts without making them usable.
func ValidateRetiredContact(network, address string) bool {
	return IsRetiredNetwork(network) && !evm.IsRetiredNetwork(network) && decred.New().ValidateAddress(network, address) == nil
}
