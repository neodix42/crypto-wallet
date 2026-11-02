package signer

import (
	"fmt"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

// Authorization for local presentation changes never authorizes a payment.
// Recheck the exact vault/generation after the confirmation.
// Reversible display metadata does not require another storage password.
func (e *Engine) approvePresentation(title, summary string) bool {
	if !e.canAuthorize() {
		return false
	}
	data, generation := e.data, e.data.Generation
	if !e.UI.Confirm(title, summary) || e.data != data || !e.canAuthorize() || e.data.Generation != generation {
		return false
	}
	return true
}
func (e *Engine) AddNextAccount() error {
	if !e.canAuthorize() {
		return ErrDenied
	}
	next, ok := e.nextAccountIndex()
	if !ok {
		return ErrDenied
	}
	data, generation := e.data, e.data.Generation
	if !e.UI.Confirm("Add deterministic account", fmt.Sprintf("Create canonical account index %d (after the latest stored index). Existing hidden and archived indices are not reused. This derives wallets locally and sends no transaction.", next)) || e.data != data || !e.canAuthorize() || e.data.Generation != generation {
		return ErrDenied
	}
	return e.AddAccount(next)
}
func (e *Engine) RenameWallet(id, name string) error {
	account, err := e.account(id)
	if err != nil || !model.AddressBookText(name, 80, false) {
		return ErrDenied
	}
	if !e.approvePresentation("Rename wallet", fmt.Sprintf("Network: %s\nReceiving address: %s\nNew local name: %s\nKeys, addresses and transactions remain unchanged.", account.Network, account.Address, name)) {
		return ErrDenied
	}
	if e.data.AccountPresentation.WalletLabels == nil {
		e.data.AccountPresentation.WalletLabels = map[string]string{}
	}
	e.data.AccountPresentation.WalletLabels[id] = name
	return e.persist()
}
func (e *Engine) groupExists(index uint32) bool {
	if e.data == nil {
		return false
	}
	for _, account := range e.data.Accounts {
		if account.Index == index {
			return true
		}
	}
	return false
}
func (e *Engine) RenameGroup(index uint32, name string) error {
	if !e.groupExists(index) || !model.AddressBookText(name, 80, false) {
		return ErrDenied
	}
	if !e.approvePresentation("Rename account group", fmt.Sprintf("Account index: %d\nNew local name: %s\nAll network wallet identities and saved operations remain unchanged.", index, name)) {
		return ErrDenied
	}
	if e.data.AccountPresentation.GroupLabels == nil {
		e.data.AccountPresentation.GroupLabels = map[string]string{}
	}
	e.data.AccountPresentation.GroupLabels[model.AccountGroupKey(index)] = name
	return e.persist()
}
func (e *Engine) DeleteWallet(id string) error {
	account, err := e.account(id)
	if err != nil {
		return ErrDenied
	}
	if !e.approvePresentation("Delete wallet from visible account", fmt.Sprintf("Network: %s\nReceiving address: %s\nRemove this wallet from the visible account list. Keys, recovery identity, funds and all pending/signed operation records are retained. No transaction is sent. Use Show hidden wallets to restore it.", account.Network, account.Address)) {
		return ErrDenied
	}
	return e.HideAccount(id, true)
}
func (e *Engine) DeleteGroup(index uint32) error {
	if !e.groupExists(index) {
		return ErrDenied
	}
	if !e.approvePresentation("Delete account group from visible list", fmt.Sprintf("Account index: %d\nHide all network wallets in this group. Keys, recovery indices, funds and pending/signed operations are retained. No transaction is sent. Show hidden wallets can restore the group.", index)) {
		return ErrDenied
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].Index == index {
			e.data.Accounts[i].Hidden = true
		}
	}
	if e.data.AccountPresentation.DeletedGroups == nil {
		e.data.AccountPresentation.DeletedGroups = map[string]bool{}
	}
	e.data.AccountPresentation.DeletedGroups[model.AccountGroupKey(index)] = true
	return e.persist()
}

func (e *Engine) nextAccountIndex() (uint32, bool) {
	if e.data == nil {
		return 0, false
	}
	next := uint32(0)
	for _, account := range e.data.Accounts {
		if account.Index >= derivation.MaxIndex-1 {
			return 0, false
		}
		if account.Index >= next {
			next = account.Index + 1
		}
	}
	return next, true
}
