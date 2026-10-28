package nativewallet

import (
	"errors"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func bookFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account, model.AddressBookEntry) {
	t.Helper()
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	entry := model.AddressBookEntry{ID: strings.Repeat("a", 32), AccountID: account.ID, Network: account.Network, Name: "Public fixture friend", Address: "0x2222222222222222222222222222222222222222"}
	return a, ui, core, account, entry
}

func TestAddressBookSaveLoadEditRemoveUsesOnlyOwnedAccountScopeAndNoPayment(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	var saved []model.AddressBookEntry
	core.handler = func(_ uint64, method string, body, out any) error {
		request := body.(map[string]string)
		if request["accountId"] != account.ID {
			return errors.New("wrong source account")
		}
		switch method {
		case "addressBook":
			return answer(out, saved)
		case "saveAddress":
			if len(request) != 5 || request["name"] != entry.Name || request["address"] != entry.Address || request["memo"] != "" {
				return errors.New("invalid contact fields or caller-assigned network")
			}
			saved = []model.AddressBookEntry{entry}
			return answer(out, entry)
		case "removeAddress":
			if len(request) != 2 || request["id"] != entry.ID {
				return errors.New("invalid deletion")
			}
			saved = nil
			return nil
		default:
			return errors.New("contact action attempted signing or another operation")
		}
	}
	a.fields["recipient/"+account.ID] = entry.Address
	a.event(webui.Event{ID: "addressBookSave/" + account.ID})
	ui.apply(t)
	if a.active != "addressbook" || a.fields["bookAddress"] != entry.Address || a.fields["bookAccount"] != account.ID || a.bookTarget != account.ID {
		t.Fatal("save icon lost recipient or exact source scope")
	}
	a.fields["bookName"] = entry.Name
	a.event(webui.Event{ID: "saveBookAddress"})
	ui.apply(t)
	if a.bookOwner != account.ID || len(a.bookEntries) != 1 || a.fields["bookName"] != "" {
		t.Fatal("successful durable save did not refresh contacts or clear composer")
	}
	a.plan = &model.Plan{ID: "old-approved-intent", Account: account}
	a.fields["memo/"+account.ID] = "old comment"
	a.event(webui.Event{ID: "useBookAddress/" + entry.ID})
	if a.active != "wallet/"+account.ID || !a.sendForms[account.ID] || a.fields["recipient/"+account.ID] != entry.Address || a.fields["memo/"+account.ID] != "" || a.plan != nil {
		t.Fatal("selected address failed to fill same wallet, clear old memo or invalidate preview")
	}
	a.event(webui.Event{ID: "addressBookLoad/" + account.ID})
	ui.apply(t)
	a.event(webui.Event{ID: "editBookAddress/" + entry.ID})
	if a.bookEdit != entry.ID || a.fields["bookName"] != entry.Name || a.fields["bookAddress"] != entry.Address {
		t.Fatal("contact editor changed identity")
	}
	a.event(webui.Event{ID: "saveBookAddress"})
	ui.apply(t)
	a.event(webui.Event{ID: "removeBookAddress/" + entry.ID})
	ui.apply(t)
	if len(a.bookEntries) != 0 || a.bookOwner != account.ID || a.bookEdit != "" {
		t.Fatal("deletion retained stale selectable contact")
	}
}

func TestAddressBookSelectionRejectsForeignNetworkAccountAndTarget(t *testing.T) {
	for _, invalid := range []string{"account", "network", "target", "owner", "locked", "pending", "different-page"} {
		t.Run(invalid, func(t *testing.T) {
			a, _, _, account, entry := bookFixture(t)
			a.fields["bookAccount"], a.bookOwner, a.bookTarget, a.active = account.ID, account.ID, account.ID, "addressbook"
			switch invalid {
			case "account":
				entry.AccountID = "foreign-account"
			case "network":
				entry.Network = "base-mainnet"
			case "target":
				a.bookTarget = "foreign-wallet"
			case "owner":
				a.bookOwner = "foreign-owner"
			case "locked":
				a.state.Locked = true
			case "pending":
				a.state.RecoveryConfirmed = false
			case "different-page":
				a.active = "settings"
			}
			a.bookEntries = []model.AddressBookEntry{entry}
			a.plan = &model.Plan{ID: "unchanged"}
			a.addressBookEvent("useBookAddress", entry.ID)
			if a.fields["recipient/"+account.ID] != "" || a.plan == nil || len(a.tabs) != 0 {
				t.Fatal("foreign or unavailable contact altered payment intent")
			}
		})
	}
}

func TestAddressBookReadRejectsForeignDuplicateAndOversizedEntries(t *testing.T) {
	for _, invalid := range []string{"foreign", "duplicate", "oversized", "failure"} {
		t.Run(invalid, func(t *testing.T) {
			a, ui, core, account, entry := bookFixture(t)
			entries := []model.AddressBookEntry{entry}
			switch invalid {
			case "foreign":
				entries[0].Network = "base-mainnet"
			case "duplicate":
				entries = append(entries, entry)
			case "oversized":
				entries = make([]model.AddressBookEntry, model.MaxAccountAddressBookEntries+1)
			}
			core.handler = func(_ uint64, _ string, _, out any) error {
				if invalid == "failure" {
					return errors.New("provider-private-error")
				}
				return answer(out, entries)
			}
			a.bookEntries, a.bookOwner = []model.AddressBookEntry{entry}, account.ID
			a.fields["bookAccount"] = account.ID
			a.loadAddressBook()
			ui.apply(t)
			if a.bookOwner != "" || len(a.bookEntries) != 0 || a.bookError == "" || strings.Contains(a.status, "provider-private-error") {
				t.Fatal("failed contact read retained stale selectable entries or leaked failure detail")
			}
		})
	}
}

func TestAddressBookLateResponseCannotRestoreAnotherSelectionOrLockedContacts(t *testing.T) {
	for _, change := range []string{"selection", "lock", "snapshot"} {
		t.Run(change, func(t *testing.T) {
			a, ui, core, account, entry := bookFixture(t)
			started, release := make(chan struct{}), make(chan struct{})
			core.handler = func(_ uint64, _ string, _, out any) error {
				close(started)
				<-release
				return answer(out, []model.AddressBookEntry{entry})
			}
			a.fields["bookAccount"] = account.ID
			a.loadAddressBook()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request not started")
			}
			switch change {
			case "selection":
				a.selectBookAccount("different-account")
			case "lock":
				a.lock()
			case "snapshot":
				a.applySnapshot(a.state, a.networks)
			}
			close(release)
			ui.apply(t)
			if a.bookOwner != "" || len(a.bookEntries) != 0 || strings.Contains(viewText(a.view()), entry.Name) {
				t.Fatal("stale contact response resurfaced after invalidation")
			}
		})
	}
}

func TestAddressBookTONMemoAndWalletTabOrder(t *testing.T) {
	a, ui, core := fixture(t)
	var account model.Account
	for _, candidate := range a.state.Accounts {
		if candidate.Family == "ton" {
			account = candidate
			break
		}
	}
	entry := model.AddressBookEntry{ID: strings.Repeat("b", 32), AccountID: account.ID, Network: account.Network, Name: "Public TON friend", Address: "public-ton-destination", Memo: "invoice 71"}
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "addressBook" || body.(map[string]string)["accountId"] != account.ID {
			return errors.New("unexpected action")
		}
		return answer(out, []model.AddressBookEntry{entry})
	}
	a.fields["recipient/"+account.ID], a.fields["memo/"+account.ID] = entry.Address, entry.Memo
	a.event(webui.Event{ID: "addressBookSave/" + account.ID})
	ui.apply(t)
	if a.fields["bookMemo"] != entry.Memo {
		t.Fatal("save icon dropped TON comment")
	}
	a.event(webui.Event{ID: "useBookAddress/" + entry.ID})
	if a.fields["memo/"+account.ID] != entry.Memo {
		t.Fatal("TON contact selection dropped memo")
	}
	pages := a.view().Pages
	if pages[len(pages)-2].ID != "settings" || pages[len(pages)-1].ID != "addressbook" {
		t.Fatal("address book is not next to Settings")
	}
	a.fields["bookName"] = "public draft"
	a.lock()
	if a.bookOwner != "" || a.bookTarget != "" || a.bookEdit != "" || len(a.bookEntries) != 0 || a.fields["bookMemo"] != "" || a.fields["bookName"] != "" {
		t.Fatal("lock retained contact metadata/draft")
	}
}

func TestAddressBookTabNavigationPreservesDraftAndPendingRead(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	core.handler = func(_ uint64, method string, _, out any) error {
		if method != "addressBook" {
			return errors.New("unexpected request")
		}
		close(started)
		<-release
		return answer(out, []model.AddressBookEntry{entry})
	}
	a.fields["bookAccount"], a.fields["bookName"], a.fields["bookAddress"] = account.ID, "Public unsaved draft", entry.Address
	a.loadAddressBook()
	<-started
	revision := a.bookRevision
	a.event(webui.Event{ID: "tab", Value: "settings"})
	a.event(webui.Event{ID: "tab", Value: "addressbook"})
	if a.bookRevision != revision || a.fields["bookName"] != "Public unsaved draft" {
		t.Fatal("busy tab navigation reset draft or invalidated pending read")
	}
	close(release)
	ui.apply(t)
	if a.bookOwner != account.ID || len(a.bookEntries) != 1 {
		t.Fatal("tab navigation discarded pending contacts")
	}
	a.event(webui.Event{ID: "tab", Value: "settings"})
	a.event(webui.Event{ID: "tab", Value: "addressbook"})
	if a.fields["bookName"] != "Public unsaved draft" || a.fields["bookAddress"] != entry.Address || a.busy {
		t.Fatal("ordinary reentry destroyed draft or needlessly reloaded")
	}
}

func TestAddressBookFirstVisitWaitsForExistingWorkAndPreservesSelectedToken(t *testing.T) {
	a, ui, core, account, entry := bookFixture(t)
	_, usdc, _ := tokenFixture(t, a)
	a.selectWalletAsset(account.ID + "/" + usdc.ID)
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "addressBook" {
			return errors.New("unexpected action")
		}
		id := body.(map[string]string)["accountId"]
		if id == account.ID {
			return answer(out, []model.AddressBookEntry{entry})
		}
		return answer(out, []model.AddressBookEntry{})
	}
	a.work("Unrelated operation", func(uint64) (func(), error) { return nil, nil })
	a.event(webui.Event{ID: "tab", Value: "addressbook"})
	ui.apply(t) // Completion starts first scoped contact read.
	ui.apply(t)
	if a.bookOwner == "" || a.busy {
		t.Fatal("first address-book visit during work was stranded")
	}
	a.event(webui.Event{ID: "addressBookLoad/" + account.ID})
	ui.apply(t)
	a.event(webui.Event{ID: "useBookAddress/" + entry.ID})
	if a.asset(account).ID != usdc.ID || !strings.Contains(viewText(a.walletPage(account)), "Amount (1 means 1 USDC)") {
		t.Fatal("contact selection changed token or its units")
	}
}
