package signer

import (
	"errors"
	"strings"

	"localwallet/internal/model"
)

// Public categories deliberately exclude the submitted name/address, vault
// location and underlying filesystem error. The IPC layer maps identities only.
var (
	ErrAddressBookInvalidInput       = errors.New("invalid address book input")
	ErrAddressBookInvalidAddress     = errors.New("invalid address for selected network")
	ErrAddressBookDuplicateName      = errors.New("address book name already exists")
	ErrAddressBookDuplicateRecipient = errors.New("address book recipient already exists")
	ErrAddressBookLimit              = errors.New("address book entry limit reached")
	ErrAddressBookStale              = errors.New("address book entry is no longer available")
	ErrAddressBookUnavailable        = errors.New("address book wallet is unavailable")
	ErrAddressBookWriteFailed        = errors.New("address book persistence failed")
)

func (e *Engine) AddressBook(accountID string) ([]model.AddressBookEntry, error) {
	if !e.canAuthorize() {
		return nil, ErrAddressBookUnavailable
	}
	if _, err := e.account(accountID); err != nil {
		return nil, ErrAddressBookUnavailable
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
		return model.AddressBookEntry{}, ErrAddressBookUnavailable
	}
	account, err := e.account(accountID)
	if err != nil {
		return model.AddressBookEntry{}, ErrAddressBookUnavailable
	}
	adapter, err := e.adapter(account.Network)
	if err != nil {
		return model.AddressBookEntry{}, ErrAddressBookUnavailable
	}
	if adapter.ValidateAddress(account.Network, address) != nil {
		return model.AddressBookEntry{}, ErrAddressBookInvalidAddress
	}
	updating := id != ""
	if !updating {
		id = ID()
	}
	entry := model.AddressBookEntry{ID: id, AccountID: account.ID, Network: account.Network, Name: name, Address: address, Memo: memo}
	if !entry.ValidFor(account) {
		return model.AddressBookEntry{}, ErrAddressBookInvalidInput
	}
	position, count := -1, 0
	for i, saved := range e.data.AddressBook {
		if saved.ID == id {
			if saved.AccountID != accountID {
				return model.AddressBookEntry{}, ErrAddressBookStale
			}
			position = i
		}
		if saved.AccountID != accountID {
			continue
		}
		count++
		// A lost successful acknowledgement must not force another contact or
		// another write. Only an exact create retry of this owned destination
		// is idempotent; changed names/addresses and edits still need identity.
		if !updating && saved.Name == name && saved.Address == address && saved.Memo == memo {
			return saved, nil
		}
		if saved.ID != id && strings.EqualFold(saved.Name, name) {
			return model.AddressBookEntry{}, ErrAddressBookDuplicateName
		}
		if saved.ID != id && saved.Address == address && saved.Memo == memo {
			return model.AddressBookEntry{}, ErrAddressBookDuplicateRecipient
		}
	}
	if updating && position < 0 {
		return model.AddressBookEntry{}, ErrAddressBookStale
	}
	if !updating && (len(e.data.AddressBook) >= model.MaxAddressBookEntries || count >= model.MaxAccountAddressBookEntries) {
		return model.AddressBookEntry{}, ErrAddressBookLimit
	}
	if position < 0 {
		e.data.AddressBook = append(e.data.AddressBook, entry)
	} else {
		e.data.AddressBook[position] = entry
	}
	if err := e.persist(); err != nil {
		return model.AddressBookEntry{}, ErrAddressBookWriteFailed
	}
	return entry, nil
}

func (e *Engine) RemoveAddress(accountID, id string) error {
	if !e.canAuthorize() {
		return ErrAddressBookUnavailable
	}
	if _, err := e.account(accountID); err != nil {
		return ErrAddressBookUnavailable
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
			if err := e.persist(); err != nil {
				return ErrAddressBookWriteFailed
			}
			return nil
		}
	}
	return ErrAddressBookStale
}
