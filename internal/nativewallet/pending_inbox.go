package nativewallet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"localwallet/internal/exchange"
	"localwallet/internal/journal"
	"localwallet/internal/uiprompt"
)

const pendingInboxPageSize = 16

type pendingInboxState struct {
	Request, GlobalRevision         string
	ActiveQuery                     string
	GlobalChecked, Loading, Showing bool
	Seen                            map[string][32]byte
	SeenOwners                      map[string]string
	Cancel                          context.CancelFunc
	Serial                          uint64
}

func (a *App) clearPendingInbox() {
	serial := a.pendingInbox.Serial + 1
	if a.pendingInbox.Cancel != nil {
		a.pendingInbox.Cancel()
	}
	a.pendingInbox = pendingInboxState{Serial: serial}
}

// Local journal reads finish on their inherited pipe; invalidating their UI
// callback must not cancel prompts, signatures or another background owner.
func (a *App) pausePendingPayments() {
	if !a.pendingInbox.Loading {
		return
	}
	if a.pendingInbox.Cancel != nil {
		a.pendingInbox.Cancel()
	}
	a.pendingInbox.Serial++
	a.pendingInbox.Cancel, a.pendingInbox.Loading = nil, false
	a.background = false
	if a.pendingInbox.Request == "" {
		a.pendingInbox.Request = a.pendingInbox.ActiveQuery
	}
	a.pendingInbox.ActiveQuery = ""
}

func (a *App) queuePendingPayments(accountID string) {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.pendingInbox.Showing {
		return
	}
	if accountID != "" {
		if _, ok := a.account(accountID); !ok {
			return
		}
	}
	if accountID == "" && a.pendingInbox.GlobalChecked && a.pendingInbox.GlobalRevision == a.state.Revision {
		return
	}
	query := accountID
	if query == "" {
		query = "*"
	}
	if a.pendingInbox.Request == "*" {
		return
	}
	a.pendingInbox.Request = query
}

// An explicit conflict/check must be actionable even after an earlier Later.
func (a *App) forcePendingPayments(accountID string) {
	if a.pendingInbox.Seen != nil {
		// Operation IDs contain no ownership authority. Clearing this display
		// acknowledgement changes neither a record nor any reservation.
		if accountID == "" {
			a.pendingInbox.Seen, a.pendingInbox.SeenOwners = nil, nil
		} else {
			for id, owner := range a.pendingInbox.SeenOwners {
				if owner == accountID {
					delete(a.pendingInbox.Seen, id)
					delete(a.pendingInbox.SeenOwners, id)
				}
			}
		}
	}
	a.pendingInbox.GlobalChecked = false
	a.stopPaymentQuote()
	if a.historyCancel != nil {
		a.historyCancel()
	}
	a.queuePendingPayments(accountID)
}

func pendingOperationFingerprint(operation journal.Operation) [32]byte {
	identity := ""
	if operation.Receipt != nil {
		identity = operation.Receipt.Identity
	}
	raw, _ := json.Marshal([]string{operation.Plan.ID, operation.Plan.Account.ID, operation.Digest, operation.State, identity})
	return sha256.Sum256(raw)
}

// Only local, public journal pages are read. No provider, signature or ledger
// transition is requested; complete paging is required before announcing empty.
func readPendingOperations(ctx context.Context, core backend, generation uint64, accountID string) ([]journal.Operation, error) {
	cursor, revision, total := "", "", -1
	seen, positions := map[string]bool{}, map[string]bool{}
	var pending []journal.Operation
	for pages := 0; pages < 256; pages++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var page journalPage
		// Global journal positions are independent of the per-wallet table's
		// local cursor. A first account-scoped query would revoke that cursor.
		if err := core.CallAt(generation, "history", map[string]string{"accountId": "", "cursor": cursor}, &page); err != nil {
			return nil, err
		}
		if err := normalizePublicPlans(&page); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(page)
		if err != nil || len(encoded) > 256<<10 {
			return nil, errors.New("pending journal page memory bound")
		}
		if len(page.Operations) > 128 || page.Total < 0 || page.Total > 10000 || page.Revision == "" || revision != "" && page.Revision != revision || total >= 0 && total != page.Total || page.Complete != (page.NextCursor == "") || !page.Complete && (len(page.Operations) == 0 || !uiprompt.ValidID(page.NextCursor) || positions[page.NextCursor]) {
			return nil, errors.New("invalid pending journal page")
		}
		revision, total = page.Revision, page.Total
		for _, operation := range page.Operations {
			if operation.Signed != nil || !historyText(operation.Plan.ID, 128) || operation.Plan.ID == "" || seen[operation.Plan.ID] || operation.Plan.Account.ID == "" {
				return nil, errors.New("invalid pending journal operation")
			}
			plan := operation.Plan
			if !historyText(plan.Account.Network, 128) || !historyText(plan.Account.Address, 4096) || !historyText(plan.Recipient, 4096) || !historyText(plan.Memo, 4096) || !historyText(plan.Asset, 512) || !historyText(plan.Amount, 256) || !historyText(operation.Digest, 128) || operation.Receipt != nil && !historyText(operation.Receipt.Identity, 512) {
				return nil, errors.New("invalid pending operation text")
			}
			seen[operation.Plan.ID] = true
			if len(seen) > total {
				return nil, errors.New("pending journal total changed")
			}
			if journal.Terminal(operation.State) {
				continue
			}
			switch operation.State {
			case "PREPARED", "AWAITING_CONFIRMATION", "SIGNED_NOT_SUBMITTED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "REORGED":
			default:
				return nil, errors.New("invalid pending operation state")
			}
			if accountID == "" || operation.Plan.Account.ID == accountID {
				pending = append(pending, operation)
			}
		}
		retained, _ := json.Marshal(pending)
		if len(retained) > 4<<20 {
			return nil, errors.New("pending operation memory bound")
		}
		if page.Complete {
			if len(seen) != total {
				return nil, errors.New("incomplete pending journal")
			}
			return pending, nil
		}
		positions[page.NextCursor], cursor = true, page.NextCursor
	}
	return nil, errors.New("pending journal page bound")
}

func (a *App) nextPendingPayments() bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.background || a.pendingInbox.Loading || a.pendingInbox.Showing || a.pendingInbox.Request == "" {
		return false
	}
	query := a.pendingInbox.Request
	a.pendingInbox.Request, a.pendingInbox.Loading = "", true
	a.pendingInbox.ActiveQuery = query
	accountID := query
	if accountID == "*" {
		accountID = ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.pendingInbox.Cancel = cancel
	a.pendingInbox.Serial++
	serial, epoch, generation, revision := a.pendingInbox.Serial, a.epoch, a.core.Generation(), a.state.Revision
	checkWizard := query == "*" && a.selectedExchangeProvider().ID == "bybit"
	a.background = true
	go func() {
		defer cancel()
		operations, err := readPendingOperations(ctx, a.core, generation, accountID)
		var savedWizard *exchange.WizardState
		if err == nil && len(operations) > 0 && checkWizard && ctx.Err() == nil {
			var saved struct{ Wizard *exchange.WizardState }
			// This local navigation read needs no network request or approval.
			// On fresh unlock the Exchange page has not loaded its state yet.
			if a.core.CallAt(generation, "exchangeWizardLoad", struct{}{}, &saved) == nil {
				savedWizard = saved.Wizard
			}
		}
		a.ui.Dispatch(func() {
			if a.closed || a.epoch != epoch || a.pendingInbox.Serial != serial {
				return
			}
			a.pendingInbox.Loading, a.pendingInbox.Cancel, a.background = false, nil, false
			a.pendingInbox.ActiveQuery = ""
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			if a.state.Locked || !a.state.RecoveryConfirmed {
				return
			}
			if a.state.Revision != revision {
				a.queuePendingPayments("")
				return
			}
			if a.busy || a.pendingInbox.Showing {
				a.pendingInbox.Request = query
				return
			}
			if query == "*" {
				a.pendingInbox.GlobalChecked, a.pendingInbox.GlobalRevision = true, revision
			}
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					a.pendingInboxFailure(accountID)
				}
				return
			}
			if query == "*" {
				filtered := operations[:0]
				for _, operation := range operations {
					if !a.wizardManagesPendingDeposit(operation) && !wizardOwnsPendingDeposit(savedWizard, operation) {
						filtered = append(filtered, operation)
					}
				}
				operations = filtered
			}
			var unseen []journal.Operation
			for _, operation := range operations {
				if a.pendingInbox.Seen[operation.Plan.ID] != pendingOperationFingerprint(operation) {
					unseen = append(unseen, operation)
				}
			}
			if len(unseen) > 0 {
				a.showPendingOperations(operations)
			}
		})
	}()
	return true
}

func newPendingChoice(label, summary string) (uiprompt.PendingChoice, error) {
	if label == "" || !historyText(label, 512) || !utf8.ValidString(summary) {
		return uiprompt.PendingChoice{}, errors.New("invalid pending choice text")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return uiprompt.PendingChoice{}, err
	}
	if len(summary) > 16384 {
		summary = summary[:16300]
		for !utf8.ValidString(summary) {
			summary = summary[:len(summary)-1]
		}
		summary += "\nOpen the original operation to review its complete details."
	}
	if !historyText(summary, 16384) {
		return uiprompt.PendingChoice{}, errors.New("invalid pending choice summary")
	}
	return uiprompt.PendingChoice{ID: hex.EncodeToString(nonce[:]), Label: label, Summary: summary}, nil
}

func (a *App) pendingChoiceDialog(title, summary string, choices []uiprompt.PendingChoice) (string, bool, bool) {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy {
		return "", false, false
	}
	id, err := newPendingChoice("dialog", "")
	if err != nil {
		return "", false, false
	}
	prompt := uiprompt.Prompt{ID: id.ID, Kind: "pending", Title: title, Summary: summary, Choices: choices}
	epoch, generation, privateRevision := a.epoch, a.core.Generation(), a.privateRevision.Load()
	a.busy = true
	a.render()
	reply, err := a.ui.Prompt(prompt)
	if a.closed || a.state.Locked || a.epoch != epoch || a.core.Generation() != generation || a.privateRevision.Load() != privateRevision || !a.state.RecoveryConfirmed {
		if !a.closed && a.epoch == epoch {
			a.busy = false
			if a.core.Generation() != generation {
				a.lock()
			}
		}
		return "", false, false
	}
	a.busy = false
	if err != nil {
		return "", false, false
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return "", false, false
	}
	defer clear(raw)
	checked, err := uiprompt.DecodeReply(raw, prompt)
	if err != nil {
		return "", false, false
	}
	if !checked.Accepted {
		return "", false, true
	}
	return checked.Values[0], true, true
}

func (a *App) pendingInboxFailure(accountID string) {
	choice, err := newPendingChoice("Retry loading saved operations", "Retry the local encrypted operation list. Nothing is signed, sent or cancelled.")
	if err != nil {
		return
	}
	a.pendingInbox.Showing = true
	selected, accepted, _ := a.pendingChoiceDialog("Saved operations unavailable", "The complete list of pending operations could not be verified. Their original records and reservations remain. Choose Retry, or Later to leave them unchanged.", []uiprompt.PendingChoice{choice})
	a.pendingInbox.Showing = false
	if accepted && selected == choice.ID {
		a.pendingInbox.GlobalChecked = false
		a.queuePendingPayments(accountID)
	}
}

func (a *App) showPendingOperations(operations []journal.Operation) {
	a.pendingInbox.Showing = true
	defer func() { a.pendingInbox.Showing = false; a.render() }()
	if a.pendingInbox.Seen == nil {
		a.pendingInbox.Seen = map[string][32]byte{}
	}
	if a.pendingInbox.SeenOwners == nil {
		a.pendingInbox.SeenOwners = map[string]string{}
	}
	for offset := 0; ; {
		end := min(offset+pendingInboxPageSize, len(operations))
		choices, selectedOperations := []uiprompt.PendingChoice{}, map[string]journal.Operation{}
		nextID, previousID := "", ""
		for _, operation := range operations[offset:end] {
			label := operation.Plan.Account.Network + " · " + operation.State
			choice, err := newPendingChoice(label, a.pendingSummary(operation))
			if err != nil {
				return
			}
			choices = append(choices, choice)
			selectedOperations[choice.ID] = operation
		}
		if offset > 0 {
			choice, err := newPendingChoice("Previous saved operations", "Show the previous page.")
			if err != nil {
				return
			}
			previousID = choice.ID
			choices = append(choices, choice)
		}
		if end < len(operations) {
			choice, err := newPendingChoice("More saved operations", "Show the next page.")
			if err != nil {
				return
			}
			nextID = choice.ID
			choices = append(choices, choice)
		}
		selected, accepted, displayed := a.pendingChoiceDialog("Pending operations", fmt.Sprintf("%d saved operations need attention. Showing %d–%d. Choose an operation to review its original details. Later leaves every operation unchanged.", len(operations), offset+1, end), choices)
		if displayed {
			// Later acknowledges this bounded, paged inbox snapshot, including
			// pages available through More. An unrelated metadata revision must
			// not reopen unchanged operations; explicit Check always can.
			for _, operation := range operations {
				a.pendingInbox.Seen[operation.Plan.ID] = pendingOperationFingerprint(operation)
				a.pendingInbox.SeenOwners[operation.Plan.ID] = operation.Plan.Account.ID
			}
		}
		if !accepted {
			return
		}
		if selected == nextID && nextID != "" {
			offset = end
			continue
		}
		if selected == previousID && previousID != "" {
			offset = max(0, offset-pendingInboxPageSize)
			continue
		}
		operation, ok := selectedOperations[selected]
		if !ok {
			return
		}
		account, owned := a.account(operation.Plan.Account.ID)
		if !owned || !a.walletHistoryOperation(account, operation) || len(historyOperationKinds(operation.State)) == 0 {
			choice, err := newPendingChoice("Back to saved operations", "This operation remains unchanged. Its original network/profile is unavailable for an action in the current wallet.")
			if err != nil {
				return
			}
			if _, accepted, _ := a.pendingChoiceDialog("Original operation details", a.pendingSummary(operation), []uiprompt.PendingChoice{choice}); accepted {
				continue
			}
			return
		}
		// This explicit selection navigates to the original owned wallet. The
		// Showing guard prevents its ordinary open hook from reopening the inbox.
		a.walletEvent("open/" + account.ID)
		a.resolvePendingPaymentExpected(account, nil, a.planRevision, operation.Plan.ID)
		return
	}
}
