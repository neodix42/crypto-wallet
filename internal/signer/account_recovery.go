package signer

import (
	"errors"
	"fmt"

	"localwallet/internal/chains"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

// Recovery failures are fixed categories. Never include entropy, passwords,
// selected paths or an adapter's underlying error in a public response.
var (
	ErrAccountRecoveryInvalidRange = errors.New("account recovery range is invalid")
	ErrAccountRecoveryLimit        = errors.New("account recovery exceeds stored account limit")
	ErrAccountRecoveryDerivation   = errors.New("account recovery derivation failed")
	ErrAccountRecoveryStorage      = errors.New("account recovery could not be saved")
	ErrAccountRecoveryStale        = errors.New("account recovery session changed")
	ErrAccountRecoveryCancelled    = errors.New("account recovery cancelled")
	ErrAccountRecoveryLocked       = errors.New("account recovery requires an unlocked wallet")
)

// DiscoverAccounts restores visibility and derives only missing network/index
// slots in a deliberately bounded range. Repeating an already visible range is
// successful without replacing either encrypted vault generation. Existing
// descriptors (including named associations and alternate TON profiles), local
// labels, reservations and original signed operation bytes are never replaced.
func (e *Engine) DiscoverAccounts(start, end uint32) error {
	if start > end || end >= derivation.MaxIndex || end-start >= 20 {
		return ErrAccountRecoveryInvalidRange
	}
	if e.persistFailed {
		return ErrAccountRecoveryStorage
	}
	if e.data == nil {
		return ErrAccountRecoveryLocked
	}
	data, generation := e.data, e.data.Generation
	type slot struct {
		network string
		index   uint32
	}
	type missingAccount struct {
		slot
		adapter model.Adapter
	}
	stored := make(map[slot]bool, len(data.Accounts))
	ids := make(map[string]bool, len(data.Accounts))
	changed := false
	for _, account := range data.Accounts {
		stored[slot{account.Network, account.Index}] = true
		ids[account.ID] = true
		if account.Index >= start && account.Index <= end && account.Hidden && !chains.IsRetiredNetwork(account.Network) {
			changed = true
		}
	}
	for index := start; index <= end; index++ {
		if data.AccountPresentation.DeletedGroups[model.AccountGroupKey(index)] {
			changed = true
		}
	}
	var missing []missingAccount
	available := false
	for index := start; index <= end; index++ {
		for _, adapter := range e.Adapters {
			for _, network := range adapter.Networks() {
				if chains.IsRetiredNetwork(network.ID) || network.Testnet != (data.Environment == "testnet") {
					continue
				}
				available = true
				key := slot{network.ID, index}
				if !stored[key] {
					missing = append(missing, missingAccount{key, adapter})
					stored[key] = true
				}
			}
		}
	}
	if !available {
		return ErrAccountRecoveryDerivation
	}
	if len(data.Accounts)+len(missing) > 10000 {
		return ErrAccountRecoveryLimit
	}
	if !e.UI.Confirm("Recover bounded account index range", fmt.Sprintf("Canonical account indices %d through %d (inclusive). Existing stored wallets in this range will be shown again; only missing network/index identities are derived. Existing keys, profiles, names and original pending operations are preserved. This is local recovery, not a blockchain balance or history scan. Named NEAR and rotated Aptos associations on another installation require separate verified recovery metadata. No payment or deployment is performed.", start, end)) {
		return ErrAccountRecoveryCancelled
	}
	if e.data != data || e.persistFailed || data.Generation != generation {
		return ErrAccountRecoveryStale
	}
	if !changed && len(missing) == 0 {
		return nil
	}
	var additions []model.Account
	if len(missing) != 0 {
		seed, err := derivation.Seed(data.Entropy, data.Passphrase)
		if err != nil {
			return ErrAccountRecoveryDerivation
		}
		defer clear(seed)
		for _, item := range missing {
			account, err := item.adapter.Derive(seed, item.index, item.network)
			if err != nil || account.ID == "" || ids[account.ID] || account.Network != item.network || account.Index != item.index || account.Profile == "" || account.Address == "" || account.PublicKey == "" {
				return ErrAccountRecoveryDerivation
			}
			ids[account.ID] = true
			additions = append(additions, account)
		}
	}
	if e.data != data || e.persistFailed || data.Generation != generation {
		return ErrAccountRecoveryStale
	}
	for index := range data.Accounts {
		account := &data.Accounts[index]
		if account.Index >= start && account.Index <= end && !chains.IsRetiredNetwork(account.Network) {
			account.Hidden = false
		}
	}
	for index := start; index <= end; index++ {
		delete(data.AccountPresentation.DeletedGroups, model.AccountGroupKey(index))
	}
	data.Accounts = append(data.Accounts, additions...)
	if err := e.persist(); err != nil {
		return ErrAccountRecoveryStorage
	}
	return nil
}
