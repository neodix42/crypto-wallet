package vault

import (
	"strings"

	"localwallet/internal/chains"
	"localwallet/internal/model"
)

func (d *Data) validateAddressBook() error {
	if len(d.AddressBook) > model.MaxAddressBookEntries {
		return ErrFormat
	}
	if len(d.AddressBook) == 0 {
		return nil
	}
	accounts := make(map[string]model.Account, len(d.Accounts))
	for _, account := range d.Accounts {
		accounts[account.ID] = account
	}
	validators := map[string]model.Adapter{}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if network.Testnet == (d.Environment == "testnet") {
				validators[network.ID] = adapter
			}
		}
	}
	ids, recipients := map[string]bool{}, map[string]bool{}
	accountEntries := map[string][]model.AddressBookEntry{}
	counts := map[string]int{}
	for _, entry := range d.AddressBook {
		account, exists := accounts[entry.AccountID]
		adapter := validators[entry.Network]
		recipient := entry.AccountID + "\x00" + entry.Address + "\x00" + entry.Memo
		counts[entry.AccountID]++
		validAddress := adapter != nil && adapter.ValidateAddress(entry.Network, entry.Address) == nil || chains.ValidateRetiredContact(entry.Network, entry.Address)
		if !exists || !entry.ValidFor(account) || !validAddress ||
			ids[entry.ID] || recipients[recipient] || counts[entry.AccountID] > model.MaxAccountAddressBookEntries {
			return ErrFormat
		}
		for _, previous := range accountEntries[entry.AccountID] {
			if strings.EqualFold(previous.Name, entry.Name) {
				return ErrFormat
			}
		}
		accountEntries[entry.AccountID] = append(accountEntries[entry.AccountID], entry)
		ids[entry.ID], recipients[recipient] = true, true
	}
	return nil
}
