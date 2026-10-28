package nativewallet

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func (a *App) clearBookDraft() {
	a.bookDraftRevision++
	a.bookEdit = ""
	for _, key := range []string{"bookName", "bookAddress", "bookMemo"} {
		delete(a.fields, key)
	}
}

func (a *App) clearAddressBook() {
	a.bookRevision++
	a.bookEntries = nil
	a.bookOwner, a.bookTarget, a.bookPartial = "", "", false
	a.clearBookDraft()
	delete(a.fields, "bookAccount")
}

func (a *App) selectBookAccount(id string) {
	a.bookRevision++
	a.bookEntries = nil
	a.bookOwner, a.bookTarget, a.bookPartial = "", "", false
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
			if a.bookOwner != id || a.bookPartial {
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
	a.bookWork("load", account, revision, func(g uint64) (func(), error) {
		entries, err := a.readAddressBook(g, account)
		return func() {
			if a.bookRevision == revision && a.fields["bookAccount"] == id {
				a.bookEntries, a.bookOwner, a.bookPartial = entries, id, false
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
	sortBookEntries(entries)
	return entries, nil
}

func sortBookEntries(entries []model.AddressBookEntry) {
	sort.Slice(entries, func(i, j int) bool {
		left, right := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].ID < entries[j].ID
		}
		return left < right
	})
}

func (a *App) bookWork(action string, account model.Account, revision uint64, fn func(uint64) (func(), error)) {
	titles := map[string]string{"load": "Reading address book…", "save": "Saving encrypted address book…", "update": "Updating encrypted address book…", "remove": "Removing saved address…"}
	success := map[string]string{"load": "Address book refreshed.", "save": "Address saved.", "update": "Address updated.", "remove": "Address removed."}
	a.workWithFeedback(titles[action], fn, func(err error) {
		if a.bookRevision != revision || a.fields["bookAccount"] != account.ID {
			return
		}
		if err == nil {
			a.notice(success[action])
			return
		}
		code := ipc.PublicErrorCode(err)
		var message string
		switch code {
		case "ADDRESS_BOOK_INVALID_ADDRESS":
			message = "Enter a valid address for the selected network. Check the full address and checksum."
		case "ADDRESS_BOOK_DUPLICATE_NAME":
			message = "A contact already uses this name in this wallet. Edit that contact or choose another name."
		case "ADDRESS_BOOK_DUPLICATE_RECIPIENT":
			message = "This address and comment are already saved in this wallet. Edit the existing contact."
		case "ADDRESS_BOOK_INVALID_INPUT":
			message = "Check the contact name, address and optional TON comment. Remove hidden characters and stay within the displayed limits."
		case "ADDRESS_BOOK_LIMIT":
			message = "The address book is full (256 contacts per wallet, 2,048 total). Edit or delete a contact first."
		case "ADDRESS_BOOK_STALE":
			a.bookEntries, a.bookOwner, a.bookPartial = nil, "", false
			message = "This saved contact changed or no longer exists. Refresh address book before editing it."
		case "ADDRESS_BOOK_UNAVAILABLE":
			a.lock()
			message = "The wallet session changed. Unlock the wallet before using the address book."
		case "ADDRESS_BOOK_WRITE_FAILED":
			a.lock()
			message = "Encrypted storage could not be written. The wallet is locked; check storage access and free disk space before unlocking."
		default:
			if action == "load" {
				a.bookEntries, a.bookOwner, a.bookPartial = nil, "", false
				message = "Could not refresh saved contacts. Choose Refresh address book to retry."
			} else {
				// A lost acknowledgment cannot establish whether a write committed.
				// Do not leave old destinations selectable after an uncertain edit.
				a.bookEntries, a.bookOwner, a.bookPartial = nil, "", false
				message = "The change could not be confirmed. Refresh address book before retrying."
			}
		}
		a.notice(message)
	})
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
	var entries []model.AddressBookEntry
	if a.bookOwner == id {
		entries = append(entries, a.bookEntries...)
	}
	partial := a.bookOwner != id || a.bookPartial
	action := "save"
	if a.bookEdit != "" {
		action = "update"
	}
	a.bookWork(action, account, revision, func(g uint64) (func(), error) {
		var saved model.AddressBookEntry
		if err := a.callAt(g, "saveAddress", body, &saved); err != nil {
			return nil, err
		}
		// The signer acknowledges only after the encrypted commit. A second
		// read must not turn that committed save into an apparent failure.
		if !saved.ValidFor(account) || saved.Name != name || saved.Address != address || saved.Memo != memo || body["id"] != "" && saved.ID != body["id"] {
			return nil, errors.New("invalid saved contact acknowledgment")
		}
		found := false
		for i, entry := range entries {
			if entry.ID == saved.ID {
				if body["id"] == "" && entry != saved {
					return nil, errors.New("invalid saved contact identity")
				}
				entries[i], found = saved, true
				break
			}
		}
		if !found {
			if len(entries) >= model.MaxAccountAddressBookEntries {
				return nil, errors.New("invalid saved contact count")
			}
			entries = append(entries, saved)
		}
		sortBookEntries(entries)
		return func() {
			if a.bookRevision == revision && a.fields["bookAccount"] == id {
				a.bookEntries, a.bookOwner, a.bookPartial = entries, id, partial
				a.clearBookDraft()
			}
		}, nil
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
			a.bookDraftRevision++
			a.bookEdit = id
			a.fields["bookName"], a.fields["bookAddress"], a.fields["bookMemo"] = entry.Name, entry.Address, entry.Memo
			a.render()
		case "removeBookAddress":
			revision := a.bookRevision
			entries := make([]model.AddressBookEntry, 0, len(a.bookEntries)-1)
			for _, saved := range a.bookEntries {
				if saved.ID != id {
					entries = append(entries, saved)
				}
			}
			a.bookWork("remove", account, revision, func(g uint64) (func(), error) {
				if err := a.callAt(g, "removeAddress", map[string]string{"accountId": account.ID, "id": id}, nil); err != nil {
					return nil, err
				}
				return func() {
					if a.bookRevision == revision && a.fields["bookAccount"] == account.ID {
						a.bookEntries, a.bookOwner = entries, account.ID
						if a.bookEdit == id {
							a.clearBookDraft()
						}
					}
				}, nil
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
	nodes = append(nodes, label("Network: "+a.network(account.Network).Name+" · "+account.Network))
	// Explicit draft replacement must survive batched busy/completed views.
	// A quick idempotent save can finish before React observes the busy view,
	// leaving canonical empty values unchanged from before local typing.
	composer := node("column", "bookComposer/"+strconv.FormatUint(a.bookDraftRevision, 10), "", "")
	composer.Children = []webui.Node{a.entry("bookName", "Name"), a.entry("bookAddress", "Address")}
	if account.Family == "ton" {
		composer.Children = append(composer.Children, a.entry("bookMemo", "TON comment (optional)"))
	}
	nodes = append(nodes, composer)
	action := "Save address"
	if a.bookEdit != "" {
		action = "Update address"
	}
	nodes = append(nodes, row(a.button("saveBookAddress", action, false), a.button("cancelBookEdit", "Clear form", false), a.button("refreshAddressBook", "Refresh address book", false)))
	if a.busy && a.bookOwner == "" {
		return append(nodes, label("Loading address book…"))
	}
	if a.bookOwner != account.ID {
		return append(nodes, label("Saved contacts have not been loaded. Choose Refresh address book to load them."))
	}
	if a.bookPartial {
		nodes = append(nodes, label("Choose Refresh address book to load the complete list of saved contacts."))
	}
	if len(a.bookEntries) == 0 && !a.bookPartial {
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
