package nativewallet

import (
	"errors"
	"strings"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func TestAddressBookDurableSaveAcknowledgmentNeedsNoFollowupRead(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	a.fields["bookAccount"], a.active, a.bookOwner = account.ID, "addressbook", account.ID
	a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
	calls := 0
	core.handler = func(_ uint64, method string, body, out any) error {
		calls++
		if method != "saveAddress" {
			return errors.New("read unavailable after a committed save")
		}
		if body.(map[string]string)["accountId"] != account.ID {
			return errors.New("wrong scope")
		}
		return answer(out, entry)
	}
	a.saveBookAddress()
	if strings.Contains(viewText(a.view()), "Address book unavailable") {
		t.Fatal("save predicted an unavailable book before any response")
	}
	ui.apply(t)
	if calls != 1 || a.status != "Address saved." || a.bookOwner != account.ID || a.bookPartial || len(a.bookEntries) != 1 || a.bookEntries[0] != entry || a.fields["bookName"] != "" {
		t.Fatal("durable save was reclassified by a subsequent list request or lost its acknowledged contact")
	}
	// A retry after a lost response is idempotent in the signer. Its same
	// acknowledgment must not append a duplicate row in the controller either.
	a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
	a.saveBookAddress()
	ui.apply(t)
	if calls != 2 || len(a.bookEntries) != 1 || a.status != "Address saved." {
		t.Fatal("identical durable acknowledgment duplicated or rejected a contact")
	}
}

func TestAddressBookRejectionsDescribeTheBlockerAndPreserveContactsAndDraft(t *testing.T) {
	for code, expected := range map[string]string{
		"ADDRESS_BOOK_INVALID_ADDRESS":     "Enter a valid address for the selected network.",
		"ADDRESS_BOOK_DUPLICATE_NAME":      "A contact already uses this name",
		"ADDRESS_BOOK_DUPLICATE_RECIPIENT": "This address and comment are already saved",
		"ADDRESS_BOOK_INVALID_INPUT":       "Check the contact name, address",
		"ADDRESS_BOOK_LIMIT":               "The address book is full",
	} {
		t.Run(code, func(t *testing.T) {
			a, ui, core, account, entry := bookFixture(t)
			a.fields["bookAccount"], a.active, a.bookOwner = account.ID, "addressbook", account.ID
			a.bookEntries = []model.AddressBookEntry{entry}
			a.fields["bookName"], a.fields["bookAddress"] = "Edited draft", "0x1111111111111111111111111111111111111111"
			core.handler = func(_ uint64, method string, _, _ any) error {
				if method != "saveAddress" {
					t.Error("rejected save made an unrelated request")
				}
				return ipc.NewRequestError(code)
			}
			a.saveBookAddress()
			if len(a.bookEntries) != 1 || a.bookOwner != account.ID {
				t.Fatal("pending save erased known saved contacts")
			}
			ui.apply(t)
			if !strings.Contains(a.status, expected) || strings.Contains(viewText(a.view()), "Address book unavailable") || len(a.bookEntries) != 1 || a.bookEntries[0] != entry || a.bookOwner != account.ID || a.fields["bookName"] != "Edited draft" || a.fields["bookAddress"] != "0x1111111111111111111111111111111111111111" {
				t.Fatal("rejection hid its cause, erased contacts or discarded the correctable draft")
			}
			if button, ok := findNode(a.addressBookPage(), "useBookAddress/"+entry.ID); !ok || button.Disabled {
				t.Fatal("validation error blocked an unchanged saved contact")
			}
			if a.state.Locked {
				t.Fatal("public input rejection unnecessarily locked the wallet")
			}
		})
	}
}

func TestAddressBookSaveCannotClaimSuccessForWrongAcknowledgment(t *testing.T) {
	for _, invalid := range []string{"account", "network", "address", "name", "id", "memo"} {
		t.Run(invalid, func(t *testing.T) {
			a, ui, core, account, entry := bookFixture(t)
			a.fields["bookAccount"], a.bookOwner, a.bookEdit = account.ID, account.ID, entry.ID
			a.bookEntries = []model.AddressBookEntry{entry}
			a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
			bad := entry
			switch invalid {
			case "account":
				bad.AccountID = "foreign"
			case "network":
				bad.Network = "base-mainnet"
			case "address":
				bad.Address = "0x1111111111111111111111111111111111111111"
			case "name":
				bad.Name = "another"
			case "id":
				bad.ID = strings.Repeat("b", 32)
			case "memo":
				bad.Memo = "unexpected"
			}
			core.handler = func(_ uint64, _ string, _, out any) error { return answer(out, bad) }
			a.saveBookAddress()
			ui.apply(t)
			if a.status != "The change could not be confirmed. Refresh address book before retrying." || len(a.bookEntries) != 0 || a.bookOwner != "" || a.fields["bookName"] != entry.Name {
				t.Fatal("wrong save acknowledgment was displayed as committed success or retained stale destinations")
			}
		})
	}
}

func TestAddressBookActualStorageOrSessionFailureStillLocksAndUnknownWriteDoesNotLie(t *testing.T) {
	for _, code := range []string{"ADDRESS_BOOK_UNAVAILABLE", "ADDRESS_BOOK_WRITE_FAILED", "REQUEST_FAILED_STATE_UNKNOWN"} {
		t.Run(code, func(t *testing.T) {
			a, ui, core, account, entry := bookFixture(t)
			a.fields["bookAccount"], a.bookOwner = account.ID, account.ID
			a.bookEntries = []model.AddressBookEntry{entry}
			a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
			core.handler = func(_ uint64, _ string, _, _ any) error { return ipc.NewRequestError(code) }
			a.saveBookAddress()
			ui.apply(t)
			if len(a.bookEntries) != 0 || a.bookOwner != "" || strings.Contains(viewText(a.view()), "Address book unavailable") || a.status == "Address saved." {
				t.Fatal("failed storage/session was hidden or left a selectable uncertain destination")
			}
			if code == "REQUEST_FAILED_STATE_UNKNOWN" {
				if a.state.Locked || a.fields["bookName"] != entry.Name || !strings.Contains(a.status, "could not be confirmed") {
					t.Fatal("unknown acknowledgment discarded draft or claimed no write")
				}
			} else if !a.state.Locked || a.fields["bookName"] != "" || core.killed.Load() == 0 {
				t.Fatal("known locked session/storage failure retained wallet authority")
			}
		})
	}
}

func TestAddressBookDeleteAcknowledgmentNeedsNoSecondRead(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	a.fields["bookAccount"], a.bookOwner, a.active = account.ID, account.ID, "addressbook"
	a.bookEntries = []model.AddressBookEntry{entry}
	calls := 0
	core.handler = func(_ uint64, method string, _, _ any) error {
		calls++
		if method != "removeAddress" {
			return errors.New("read unavailable after committed deletion")
		}
		return nil
	}
	a.event(webui.Event{ID: "removeBookAddress/" + entry.ID})
	ui.apply(t)
	if calls != 1 || a.status != "Address removed." || len(a.bookEntries) != 0 || a.bookOwner != account.ID {
		t.Fatal("committed deletion incorrectly depended on a second read")
	}
}

func TestAddressBookSaveWithoutACompleteListKeepsOnlyAcknowledgedScope(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	a.fields["bookAccount"] = account.ID
	a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
	foreign := entry
	foreign.AccountID, foreign.Name = "other-account", "foreign contact"
	a.bookEntries, a.bookOwner = []model.AddressBookEntry{foreign}, "other-account"
	core.handler = func(_ uint64, method string, _, out any) error {
		if method != "saveAddress" {
			return errors.New("no redundant read")
		}
		return answer(out, entry)
	}
	a.saveBookAddress()
	ui.apply(t)
	if len(a.bookEntries) != 1 || a.bookEntries[0] != entry || !a.bookPartial || a.bookOwner != account.ID || !strings.Contains(viewText(a.addressBookPage()), "complete list") || strings.Contains(viewText(a.addressBookPage()), foreign.Name) {
		t.Fatal("partial acknowledgment mixed scopes or pretended the complete book was loaded")
	}
}

func bookComposerID(t *testing.T, a *App) string {
	t.Helper()
	for _, item := range a.addressBookPage() {
		if item.Kind == "column" && strings.HasPrefix(item.ID, "bookComposer/") {
			if len(item.Children) < 2 || item.Children[0].ID != "field/bookName" || item.Children[1].ID != "field/bookAddress" {
				t.Fatal("composer replacement changed the public field identities")
			}
			return item.ID
		}
	}
	t.Fatal("address-book draft lacks an explicit keyed replacement identity")
	return ""
}

func TestAddressBookDraftReplacementSurvivesCoalescedBusyViews(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	a.fields["bookAccount"], a.bookOwner, a.active = account.ID, account.ID, "addressbook"
	a.bookEntries = []model.AddressBookEntry{entry}
	initial := bookComposerID(t, a)
	core.handler = func(_ uint64, method string, _, out any) error {
		if method == "saveAddress" {
			return answer(out, entry)
		}
		return answer(out, []model.AddressBookEntry{entry})
	}
	for i := 0; i < 2; i++ {
		before := bookComposerID(t, a)
		a.event(webui.Event{ID: "field/bookName", Value: entry.Name})
		a.event(webui.Event{ID: "field/bookAddress", Value: entry.Address})
		if bookComposerID(t, a) != before {
			t.Fatal("ordinary typing replaced the composer and would lose keyboard focus")
		}
		a.saveBookAddress()
		ui.apply(t)
		if bookComposerID(t, a) == before || a.fields["bookName"] != "" || a.fields["bookAddress"] != "" {
			t.Fatal("fast durable save did not explicitly replace the local draft independently of a transient busy view")
		}
	}
	if bookComposerID(t, a) == initial {
		t.Fatal("repeated save reused the original empty composer identity")
	}
	before := bookComposerID(t, a)
	a.fields["bookName"], a.fields["bookAddress"] = entry.Name, entry.Address
	// Correctable rejection must not remount and discard the entered draft.
	core.handler = func(_ uint64, _ string, _, _ any) error { return ipc.NewRequestError("ADDRESS_BOOK_INVALID_ADDRESS") }
	a.saveBookAddress()
	ui.apply(t)
	if bookComposerID(t, a) != before || a.fields["bookName"] != entry.Name {
		t.Fatal("a rejected save replaced the correctable draft")
	}
	a.event(webui.Event{ID: "cancelBookEdit"})
	if bookComposerID(t, a) == before || a.fields["bookName"] != "" {
		t.Fatal("explicit Clear form lacks a replacement identity")
	}
	for i := 0; i < 2; i++ {
		before = bookComposerID(t, a)
		a.event(webui.Event{ID: "editBookAddress/" + entry.ID})
		if bookComposerID(t, a) == before || a.bookEdit != entry.ID || a.fields["bookAddress"] != entry.Address {
			t.Fatal("explicit contact edit did not replace the prior local composer")
		}
	}
	before = bookComposerID(t, a)
	core.handler = func(_ uint64, _ string, _, out any) error { return answer(out, []model.AddressBookEntry{entry}) }
	a.loadAddressBook()
	ui.apply(t)
	if bookComposerID(t, a) != before || a.fields["bookName"] != entry.Name {
		t.Fatal("refresh replaced a legitimate draft")
	}
	a.selectBookAccount(account.ID)
	if bookComposerID(t, a) == before || a.fields["bookName"] != "" {
		t.Fatal("new account selection reused its former composer")
	}
	revision := a.bookDraftRevision
	a.lock()
	if a.bookDraftRevision == revision {
		t.Fatal("lock did not revoke the composer identity")
	}
}
