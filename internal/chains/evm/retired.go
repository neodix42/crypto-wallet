package evm

import (
	"errors"
	"fmt"

	"localwallet/internal/model"
)

// IsRetiredNetwork recognizes only historical vault identities. Retired networks
// are absent from the registry and can never select an RPC or signing domain.
func IsRetiredNetwork(network string) bool {
	return network == "horizen-mainnet" || network == "horizen-testnet"
}

// ValidateRetiredAccount preserves authentication of old vaults without enabling
// an adapter for the removed network. Its frozen BIP32 profile is identical to
// Ethereum/Base at the same index. No descriptor, journal or key is migrated.
func ValidateRetiredAccount(seed []byte, account model.Account) error {
	if !IsRetiredNetwork(account.Network) || len(account.Parameters) != 0 {
		return errors.New("invalid retired account")
	}
	expected, err := New().Derive(seed, account.Index, "base-mainnet")
	if err != nil {
		return err
	}
	expected.ID = fmt.Sprintf("evm:%d:%s", account.Index, account.Network)
	if account.ID != expected.ID || account.Family != expected.Family || account.Profile != expected.Profile || account.Address != expected.Address || account.PublicKey != expected.PublicKey {
		return errors.New("retired account recovery descriptor mismatch")
	}
	return nil
}
