package nativewallet

import (
	"errors"
	"sort"
	"strings"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func (a *App) clearBookDraft() {
	a.bookEdit = ""
	for _, key := range []string{"bookName", "bookAddress", "bookMemo"} {
		delete(a.fields, key)
	}
}

func (a *App) clearAddressBook() {
	a.bookRevision++
	a.bookEntries = nil
	a.bookOwner, a.bookTarget, a.bookError = "", "", ""
	a.clearBookDraft()
	delete(a.fields, "bookAccount")
}

func (a *App) selectBookAccount(id string) {
	a.bookRevision++
	a.bookEntries = nil
	a.bookOwner, a.bookTarget, a.bookError = "", "", ""
	a.clearBookDraft()
	a.fields["bookAccount"] = id
}

func (a *App) openAddressBook(id string, save bool) {
	if a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return
	}
	fromWallet := id != ""
	if id == "" {
		id = a.fields["bookAccount"]
		if _, ok := a.account(id); ok {
			// Ordinary navigation preserves the current composer and any
			// in-flight context. Explicit wallet icons start a new selection.
			a.active = "addressbook"
			if a.bookOwner != id {
				a.loadAddressBook()
			}
			a.render()
			return
		}
		if _, ok := a.account(id); !ok {
			for _, account := range a.state.Accounts {
				if !account.Hidden {
					id = account.ID
					break
				}
			}
		}
	}
	account, ok := a.account(id)
	if !ok {
		return
	}
	a.selectBookAccount(id)
	if fromWallet {
		a.bookTarget = id
	}
	if save {
		a.fields["bookAddress"] = a.fields["recipient/"+id]
		if account.Family == "ton" {
			a.fields["bookMemo"] = a.fields["memo/"+id]
		}
	}
	a.active = "addressbook"
	a.loadAddressBook()
	a.render()
}

// No stale list can become selectable while another account is being read.
// Both work's session guard and this selection revision bind the response.
func (a *App) loadAddressBook() {
	id := a.fields["bookAccount"]
	account, ok := a.account(id)
	if !ok || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return
	}
	a.bookRevision++
	revision := a.bookRevision
	a.bookEntries, a.bookOwner, a.bookError = nil, "", "Address book unavailable. Refresh to retry."
	a.work("Reading address book…", func(g uint64) (func(), error) {
		entries, err := a.readAddressBook(g, account)
		return func() {
			if a.bookRevision == revision && a.fields["bookAccount"] == id {
				a.bookEntries, a.bookOwner, a.bookError = entries, id, ""
			}
		}, err
	})
}

func (a *App) readAddressBook(g uint64, account model.Account) ([]model.AddressBookEntry, error) {
	var entries []model.AddressBookEntry
	if err := a.callAt(g, "addressBook", map[string]string{"accountId": account.ID}, &entries); err != nil {
		return nil, err
	}
	if len(entries) > model.MaxAccountAddressBookEntries {
		return nil, errors.New("invalid address book")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.ValidFor(account) || seen[entry.ID] {
			return nil, errors.New("invalid address book")
		}
		seen[entry.ID] = true
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].ID < entries[j].ID
		}
		return left < right
	})
	return entries, nil
}

func (a *App) saveBookAddress() {
	id := a.fields["bookAccount"]
	account, ok := a.account(id)
	if !ok || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return
	}
	name, address, memo := strings.TrimSpace(a.fields["bookName"]), strings.TrimSpace(a.fields["bookAddress"]), a.fields["bookMemo"]
	if !model.AddressBookText(name, 80, false) || !model.AddressBookText(address, 256, false) || !model.AddressBookText(memo, 120, true) || memo != "" && account.Family != "ton" {
		a.notice("Enter a name (up to 80 bytes) and a complete address for the selected network. TON comments may contain up to 120 bytes.")
		a.render()
		return
	}
	revision := a.bookRevision
	body := map[string]string{"accountId": id, "id": a.bookEdit, "name": name, "address": address, "memo": memo}
	a.bookEntries, a.bookOwner, a.bookError = nil, "", "Address book unavailable. Refresh to retry."
	a.work("Saving encrypted address book…", func(g uint64) (func(), error) {
		var saved model.AddressBookEntry
		if err := a.callAt(g, "saveAddress", body, &saved); err != nil {
			return nil, err
		}
		entries, err := a.readAddressBook(g, account)
		return func() {
			if a.bookRevision == revision && a.fields["bookAccount"] == id {
				a.bookEntries, a.bookOwner, a.bookError = entries, id, ""
				a.clearBookDraft()
			}
		}, err
	})
}

func (a *App) addressBookEvent(kind, id string) bool {
	switch kind {
	case "addressBookLoad", "addressBookSave":
		if !a.busy {
			a.openAddressBook(id, kind == "addressBookSave")
		}
		return true
	case "useBookAddress", "editBookAddress", "removeBookAddress":
	default:
		return false
	}
	account, ok := a.account(a.fields["bookAccount"])
	if !ok || a.active != "addressbook" || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.bookOwner != account.ID {
		return true
	}
	for _, entry := range a.bookEntries {
		if entry.ID != id || !entry.ValidFor(account) {
			continue
		}
		switch kind {
		case "useBookAddress":
			if a.bookTarget != "" && a.bookTarget != account.ID {
				return true
			}
			a.fields["recipient/"+account.ID], a.fields["memo/"+account.ID] = entry.Address, entry.Memo
			a.plan, a.planIsStaking = nil, false
			a.planRevision++
			a.sendForms[account.ID] = true
			a.walletEvent("open/" + account.ID)
		case "editBookAddress":
			a.bookEdit = id
			a.fields["bookName"], a.fields["bookAddress"], a.fields["bookMemo"] = entry.Name, entry.Address, entry.Memo
			a.render()
		case "removeBookAddress":
			revision := a.bookRevision
			a.bookEntries, a.bookOwner, a.bookError = nil, "", "Address book unavailable. Refresh to retry."
			a.work("Removing saved address…", func(g uint64) (func(), error) {
				if err := a.callAt(g, "removeAddress", map[string]string{"accountId": account.ID, "id": id}, nil); err != nil {
					return nil, err
				}
				entries, err := a.readAddressBook(g, account)
				return func() {
					if a.bookRevision == revision && a.fields["bookAccount"] == account.ID {
						a.bookEntries, a.bookOwner, a.bookError = entries, account.ID, ""
						if a.bookEdit == id {
							a.clearBookDraft()
						}
					}
				}, err
			})
		}
		return true
	}
	return true
}

func (a *App) addressBookPage() []webui.Node {
	nodes := []webui.Node{heading("Address book")}
	if a.state.Locked || !a.state.RecoveryConfirmed {
		return append(nodes, label("Unlock the wallet and confirm its recovery phrase to use the encrypted address book."))
	}
	options := []webui.Option{}
	for _, account := range a.state.Accounts {
		if !account.Hidden || account.ID == a.fields["bookAccount"] {
			options = append(options, webui.Option{ID: account.ID, Text: groupName(account.Index) + " · " + a.network(account.Network).Name})
		}
	}
	nodes = append(nodes, a.combo("bookAccount", "Wallet / network", options))
	account, ok := a.account(a.fields["bookAccount"])
	if !ok {
		return nodes
	}
	nodes = append(nodes, label("Network: "+a.network(account.Network).Name+" · "+account.Network), a.entry("bookName", "Name"), a.entry("bookAddress", "Address"))
	if account.Family == "ton" {
		nodes = append(nodes, a.entry("bookMemo", "TON comment (optional)"))
	}
	action := "Save address"
	if a.bookEdit != "" {
		action = "Update address"
	}
	nodes = append(nodes, row(a.button("saveBookAddress", action, false), a.button("cancelBookEdit", "Clear form", false), a.button("refreshAddressBook", "Refresh address book", false)))
	if a.busy && a.bookOwner == "" {
		return append(nodes, label("Loading address book…"))
	}
	if a.bookError != "" {
		return append(nodes, label(a.bookError))
	}
	if a.bookOwner != account.ID {
		return nodes
	}
	if len(a.bookEntries) == 0 {
		return append(nodes, label("No saved addresses for this wallet and network."))
	}
	for _, entry := range a.bookEntries {
		saved := row(label(entry.Name), node("address", "", "", entry.Address), label(a.network(entry.Network).Name), a.button("useBookAddress/"+entry.ID, "Use address", false), a.button("editBookAddress/"+entry.ID, "Edit", false), a.button("removeBookAddress/"+entry.ID, "Delete", false))
		saved.ID = "bookEntry/" + entry.ID
		nodes = append(nodes, saved)
		if entry.Memo != "" {
			nodes = append(nodes, label("TON comment: "+entry.Memo))
		}
	}
	return nodes
}
