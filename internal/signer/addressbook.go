package signer

import (
	"strings"

	"localwallet/internal/model"
)

func (e *Engine) AddressBook(accountID string) ([]model.AddressBookEntry, error) {
	if !e.canAuthorize() {
		return nil, ErrDenied
	}
	if _, err := e.account(accountID); err != nil {
		return nil, ErrDenied
	}
	entries := []model.AddressBookEntry{}
	for _, entry := range e.data.AddressBook {
		if entry.AccountID == accountID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// SaveAddress never accepts a network from its caller: the selected owned
// account fixes it. Address-book metadata grants no signing authority.
func (e *Engine) SaveAddress(accountID, id, name, address, memo string) (model.AddressBookEntry, error) {
	if !e.canAuthorize() {
		return model.AddressBookEntry{}, ErrDenied
	}
	account, err := e.account(accountID)
	if err != nil {
		return model.AddressBookEntry{}, ErrDenied
	}
	adapter, err := e.adapter(account.Network)
	if err != nil || adapter.ValidateAddress(account.Network, address) != nil {
		return model.AddressBookEntry{}, ErrDenied
	}
	updating := id != ""
	if !updating {
		id = ID()
	}
	entry := model.AddressBookEntry{ID: id, AccountID: account.ID, Network: account.Network, Name: name, Address: address, Memo: memo}
	if !entry.ValidFor(account) {
		return model.AddressBookEntry{}, ErrDenied
	}
	position, count := -1, 0
	for i, saved := range e.data.AddressBook {
		if saved.ID == id {
			if saved.AccountID != accountID {
				return model.AddressBookEntry{}, ErrDenied
			}
			position = i
		}
		if saved.AccountID != accountID {
			continue
		}
		count++
		if saved.ID != id && (strings.EqualFold(saved.Name, name) || saved.Address == address && saved.Memo == memo) {
			return model.AddressBookEntry{}, ErrDenied
		}
	}
	if updating && position < 0 || !updating && (len(e.data.AddressBook) >= model.MaxAddressBookEntries || count >= model.MaxAccountAddressBookEntries) {
		return model.AddressBookEntry{}, ErrDenied
	}
	if position < 0 {
		e.data.AddressBook = append(e.data.AddressBook, entry)
	} else {
		e.data.AddressBook[position] = entry
	}
	if err := e.persist(); err != nil {
		return model.AddressBookEntry{}, err
	}
	return entry, nil
}

func (e *Engine) RemoveAddress(accountID, id string) error {
	if !e.canAuthorize() {
		return ErrDenied
	}
	if _, err := e.account(accountID); err != nil {
		return ErrDenied
	}
	for i, entry := range e.data.AddressBook {
		if entry.ID == id && entry.AccountID == accountID {
			entries := make([]model.AddressBookEntry, 0, len(e.data.AddressBook)-1)
			entries = append(entries, e.data.AddressBook[:i]...)
			entries = append(entries, e.data.AddressBook[i+1:]...)
			// The optional encrypted field is omitted when empty. Preserve its
			// nil representation so an encoded/decoded vault compares identically.
			if len(entries) == 0 {
				entries = nil
			}
			e.data.AddressBook = entries
			return e.persist()
		}
	}
	return ErrDenied
}
