package nativewallet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xssnick/tonutils-go/address"

	"localwallet/internal/amount"
	"localwallet/internal/chains"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

const walletHistoryLimit = 2048

// Each open wallet retains its own read-only observations. Selection and asset
// changes never reuse another account's cursor, nor mutate payment authority.
type walletHistoryState struct {
	Chain                                     model.HistoryPage
	Local                                     journalPage
	Loaded, Loading, ChainFailed, LocalFailed bool
	Request                                   string
	DirtyLocal                                bool
	ChainPaused                               bool
}

func (a *App) clearWalletHistory() {
	if a.historyCancel != nil {
		a.historyCancel()
		a.historyCancel = nil
	}
	a.walletHistory = nil
	a.walletHistoryRevision++
}

// A payment can change its durable journal even when its acknowledgement is
// lost. Refresh that journal without repeating the provider's block scan.
func (a *App) refreshWalletLocalHistory(id string) {
	if a.state.Locked || a.closed {
		return
	}
	if _, ok := a.account(id); !ok {
		return
	}
	state, exists := a.walletHistory[id]
	if !exists {
		a.queueWalletHistory(id, "initial")
		return
	}
	if state.Loading {
		state.DirtyLocal = true
	} else if state.Request == "" || state.Request == "localMore" {
		state.Request = "localRefresh"
	}
	a.walletHistory[id] = state
}

func (a *App) queueWalletHistory(id, request string) {
	if a.state.Locked || a.closed {
		return
	}
	if _, ok := a.account(id); !ok {
		return
	}
	if a.walletHistory == nil {
		a.walletHistory = map[string]walletHistoryState{}
	}
	state := a.walletHistory[id]
	if state.Loading || state.Request != "" || request == "initial" && state.Loaded {
		return
	}
	if request == "chainMore" && state.Chain.NextCursor == "" || request == "localMore" && state.Local.NextCursor == "" {
		return
	}
	if _, exists := a.walletHistory[id]; !exists && len(a.walletHistory) >= 16 {
		// The active wallet has priority; provider continuations are volatile
		// observations. Eviction never changes a durable payment reservation.
		for other, cached := range a.walletHistory {
			if other != id && !cached.Loading {
				delete(a.walletHistory, other)
				break
			}
		}
	}
	if request == "refresh" {
		a.fields["historyRows/"+id] = "0"
	}
	state.Request = request
	a.walletHistory[id] = state
}

// A normal tick starts the observation after current work. Opening a wallet or
// selecting a token does not put a confirmation spinner on read-only history.
func (a *App) nextWalletHistory() bool {
	if a.state.Locked || a.busy || a.background {
		return false
	}
	id := strings.TrimPrefix(a.active, "wallet/")
	if id == a.active {
		return false
	}
	state, exists := a.walletHistory[id]
	account, owned := a.account(id)
	if !exists || !owned || state.Request == "" || state.Loading {
		return false
	}
	request := state.Request
	state.Request, state.Loading = "", true
	a.walletHistory[id] = state
	a.background = true
	ctx, cancel := context.WithCancel(context.Background())
	a.historyCancel = cancel
	a.render()
	epoch, revision, generation := a.epoch, a.walletHistoryRevision, a.core.Generation()
	go func() {
		defer cancel()
		updated := state
		updated.Loading, updated.Loaded = false, true
		if request != "chainMore" {
			cursor := ""
			if request == "localMore" {
				cursor = state.Local.NextCursor
			}
			var page journalPage
			err := a.callAt(generation, "history", map[string]string{"accountId": id, "cursor": cursor}, &page)
			if err == nil {
				err = validateWalletJournal(account, state.Local, page, request == "localMore")
			}
			updated.LocalFailed = err != nil
			if err == nil {
				if request == "localMore" {
					page.Operations = append(append([]journal.Operation(nil), state.Local.Operations...), page.Operations...)
				}
				updated.Local = page
			} else if request != "localMore" {
				updated.Local = journalPage{}
			} else {
				updated.Local.NextCursor = ""
			}
		}
		if request != "localMore" && request != "localRefresh" && ctx.Err() == nil {
			cursor := ""
			if request == "chainMore" {
				cursor = state.Chain.NextCursor
			}
			var page model.HistoryPage
			body := map[string]string{"accountId": id, "cursor": cursor}
			var err error
			if core, ok := a.core.(interface {
				CallHistoryAt(context.Context, uint64, any, any) error
			}); ok {
				err = core.CallHistoryAt(ctx, generation, body, &page)
			} else {
				err = a.callAt(generation, "chainHistory", body, &page)
			}
			if err == nil {
				err = validateWalletChain(state.Chain, page, request == "chainMore")
			}
			updated.ChainPaused = errors.Is(err, context.Canceled)
			updated.ChainFailed = err != nil && !updated.ChainPaused
			if err == nil {
				if request == "chainMore" {
					page.Entries = append(append([]model.Receipt(nil), state.Chain.Entries...), page.Entries...)
				}
				updated.Chain = page
			} else if request != "chainMore" {
				updated.Chain = model.HistoryPage{}
			} else {
				updated.Chain.NextCursor = ""
			}
		} else if request != "localMore" && request != "localRefresh" {
			updated.ChainPaused = true
		}
		a.ui.Dispatch(func() {
			if a.closed || epoch != a.epoch {
				return
			}
			a.background = false
			a.historyCancel = nil
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			if revision != a.walletHistoryRevision || a.state.Locked {
				return
			}
			current, ok := a.account(id)
			if !ok || current.Network != account.Network || current.Address != account.Address || current.PublicKey != account.PublicKey {
				return
			}
			if a.walletHistory[id].DirtyLocal {
				updated.Request, updated.DirtyLocal = "localRefresh", false
			}
			a.walletHistory[id] = updated
			a.render()
		})
	}()
	return true
}

func validateWalletJournal(account model.Account, previous, page journalPage, more bool) error {
	count := len(page.Operations)
	if more {
		count += len(previous.Operations)
	}
	if len(page.Operations) > 128 || page.Total < count || page.Total > 10000 || page.Revision == "" || page.Complete && (page.NextCursor != "" || count != page.Total) || !page.Complete && (page.NextCursor == "" || count >= page.Total) || more && (page.Revision != previous.Revision || page.Total != previous.Total || count > walletHistoryLimit) {
		return errors.New("invalid wallet journal page")
	}
	seen := map[string]bool{}
	if more {
		for _, operation := range previous.Operations {
			seen[operation.Plan.ID] = true
		}
	}
	for _, operation := range page.Operations {
		if operation.Signed != nil || operation.Plan.ID == "" || seen[operation.Plan.ID] || operation.Plan.Account.ID != account.ID || operation.Plan.Account.Network != account.Network {
			return errors.New("wallet journal account or identity mismatch")
		}
		seen[operation.Plan.ID] = true
	}
	retained := page.Operations
	if more {
		retained = append(append([]journal.Operation(nil), previous.Operations...), retained...)
	}
	raw, _ := json.Marshal(retained)
	if len(raw) > 4<<20 {
		return errors.New("wallet journal memory bound")
	}
	return nil
}

func validateWalletChain(previous, page model.HistoryPage, more bool) error {
	if len(page.Entries) > 128 || len(page.Scope) > 16384 || page.Scope == "" || len(page.NextCursor) > 4096 || page.Complete && page.NextCursor != "" || more && len(previous.Entries)+len(page.Entries) > walletHistoryLimit {
		return errors.New("invalid wallet chain history page")
	}
	seen := map[string]bool{}
	if more {
		for _, entry := range previous.Entries {
			seen[entry.Identity] = true
		}
	}
	for _, entry := range page.Entries {
		if entry.Identity == "" || !historyText(entry.Identity, 512) || seen[entry.Identity] || !historyText(entry.State, 128) || !historyText(entry.Detail, 256<<10) || entry.Timestamp < 0 || entry.Timestamp > time.Now().Unix()+300 || len(entry.Transfers) > 1000 {
			return errors.New("invalid history transaction observation")
		}
		seen[entry.Identity] = true
		effects := map[string]bool{}
		for _, transfer := range entry.Transfers {
			atomic, err := amount.Atomic(transfer.Atomic)
			if transfer.ID == "" || !historyText(transfer.ID, 512) || effects[transfer.ID] || !historyText(transfer.Source, 4096) || !historyText(transfer.Destination, 4096) || !historyText(transfer.Comment, 4096) || err != nil || atomic.BitLen() > 256 {
				return errors.New("invalid history transfer observation")
			}
			effects[transfer.ID] = true
		}
	}
	retained := page.Entries
	if more {
		retained = append(append([]model.Receipt(nil), previous.Entries...), retained...)
	}
	raw, _ := json.Marshal(retained)
	if len(raw) > 4<<20 {
		return errors.New("wallet history memory bound")
	}
	return nil
}

func historyText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

type walletHistoryRow struct {
	id, transaction, source, destination, amount, comment, state, explorer string
	timestamp                                                              int64
	operation                                                              *journal.Operation
}

// A network observation never grants payment authority. Controls refer only to
// an original local operation for this exact wallet and a currently supported
// asset. Hidden is presentation metadata and may change after preparation.
func (a *App) walletHistoryOperation(account model.Account, operation journal.Operation) bool {
	if operation.Signed != nil || operation.Plan.ID == "" || !historyText(operation.Plan.ID, 128) || chains.IsRetiredNetwork(account.Network) {
		return false
	}
	original, current := operation.Plan.Account, account
	original.Hidden, current.Hidden = false, false
	left, le := json.Marshal(original)
	right, re := json.Marshal(current)
	if le != nil || re != nil || !bytes.Equal(left, right) {
		return false
	}
	network := a.network(account.Network)
	if network.ID != account.Network || network.Family != account.Family {
		return false
	}
	for _, asset := range network.Assets {
		if asset.ID == operation.Plan.Asset {
			return true
		}
	}
	return false
}

func historyOperationKinds(state string) []string {
	switch state {
	case "PREPARED":
		return []string{"sendOperation", "cancelPrepared"}
	case "SIGNED_NOT_SUBMITTED", "UNKNOWN":
		return []string{"retry", "reconcile"}
	case "SUBMITTED", "SUBMITTING", "REORGED":
		return []string{"reconcile"}
	default:
		return nil
	}
}

// Recheck the currently selected wallet and saved operation when a history
// control is clicked. The signer independently reloads and validates its
// original durable record; no transaction identity or bytes come from a row.
func (a *App) historyPaymentAction(kind, id string) bool {
	if a.state.Locked || a.closed || a.busy {
		return false
	}
	if !a.state.RecoveryConfirmed && (kind == "sendOperation" || kind == "retry") {
		return false
	}
	account, owned := a.account(strings.TrimPrefix(a.active, "wallet/"))
	if !owned || a.active != "wallet/"+account.ID {
		return false
	}
	state, ok := a.walletHistory[account.ID]
	if !ok || !state.Loaded || state.LocalFailed || state.Loading || state.Request != "" {
		return false
	}
	var original *journal.Operation
	for _, operation := range state.Local.Operations {
		if operation.Plan.ID != id {
			continue
		}
		if original != nil || !a.walletHistoryOperation(account, operation) {
			return false
		}
		copy := operation
		original = &copy
	}
	if original == nil {
		return false
	}
	for _, allowed := range historyOperationKinds(original.State) {
		if kind == allowed {
			method := kind
			if kind == "sendOperation" {
				method = "send"
			}
			a.paymentAction(method, id)
			return true
		}
	}
	return false
}

// A just-prepared preview is authoritative even before its read-only history
// refresh finishes. Cancellation is unsigned and remains possible after expiry.
// It cannot cancel another wallet's preview or an operation observed on-chain.
func (a *App) cancelPreparedAction(id string) bool {
	if a.state.Locked || a.closed || a.busy || id == "" {
		return false
	}
	var prepared *model.Plan
	consider := func(candidate model.Plan) bool {
		if candidate.ID != id {
			return true
		}
		if prepared != nil {
			left, _ := json.Marshal(*prepared)
			right, _ := json.Marshal(candidate)
			return bytes.Equal(left, right)
		}
		copy := candidate
		prepared = &copy
		return true
	}
	if a.plan != nil && !consider(*a.plan) {
		return false
	}
	for _, plans := range []map[string]model.Plan{a.tonPending, a.stakingPending, a.nearTokenPending} {
		for owner, candidate := range plans {
			if candidate.ID == id && (owner != candidate.Account.ID || !consider(candidate)) {
				return false
			}
		}
	}
	if prepared == nil {
		return a.historyPaymentAction("cancelPrepared", id)
	}
	account, owned := a.account(prepared.Account.ID)
	if !owned || a.active != "wallet/"+account.ID || !a.walletHistoryOperation(account, journal.Operation{Plan: *prepared}) {
		return false
	}
	a.paymentAction("cancelPrepared", id)
	return true
}

func (a *App) walletHistoryRows(account model.Account, state walletHistoryState) []walletHistoryRow {
	assets := map[string]model.Asset{}
	for _, asset := range a.network(account.Network).Assets {
		assets[asset.ID] = asset
	}
	localByTx := map[string]int{}
	for i, operation := range state.Local.Operations {
		if operation.Receipt != nil && operation.Receipt.Identity != "" {
			localByTx[operation.Receipt.Identity] = i
		}
	}
	represented := map[int]bool{}
	var rows []walletHistoryRow
	for _, transaction := range state.Chain.Entries {
		for _, transfer := range transaction.Transfers {
			asset, ok := assets[transfer.Asset.ID]
			if !ok || asset != transfer.Asset {
				continue
			}
			value, err := amount.Format(transfer.Atomic, asset.Decimals)
			if err != nil {
				continue
			}
			entry := walletHistoryRow{id: transaction.Identity + "/" + transfer.ID, transaction: transaction.Identity, source: transfer.Source, destination: transfer.Destination, amount: trimDecimal(value) + " " + asset.Symbol, comment: transfer.Comment, state: transaction.State, timestamp: transaction.Timestamp, explorer: chainutil.TransactionExplorer(account.Network, transaction.Identity)}
			if i, ok := localByTx[transaction.Identity]; ok {
				operation := state.Local.Operations[i]
				if operation.Plan.Asset == asset.ID && operation.Plan.Amount == transfer.Atomic && historyPeerEqual(account.Family, operation.Plan.Recipient, transfer.Destination) {
					represented[i] = true
					if operation.State != transaction.State {
						entry.state = operation.State + " · provider: " + transaction.State
					}
					if entry.comment == "" {
						entry.comment = operation.Plan.Memo
					}
					if a.walletHistoryOperation(account, operation) && historyPeerEqual(account.Family, account.Address, transfer.Source) {
						entry.operation = &state.Local.Operations[i]
					}
				}
			}
			rows = append(rows, entry)
		}
		if len(transaction.Transfers) == 0 {
			if _, local := localByTx[transaction.Identity]; !local {
				rows = append(rows, walletHistoryRow{id: transaction.Identity, transaction: transaction.Identity, state: transaction.State, timestamp: transaction.Timestamp, explorer: chainutil.TransactionExplorer(account.Network, transaction.Identity)})
			}
		}
	}
	for i, operation := range state.Local.Operations {
		if represented[i] {
			continue
		}
		plan := operation.Plan
		value := plan.Amount + " atomic units · " + plan.Asset
		if asset, ok := assets[plan.Asset]; ok {
			if formatted, err := amount.Format(plan.Amount, asset.Decimals); err == nil {
				value = trimDecimal(formatted) + " " + asset.Symbol
			}
		}
		entry := walletHistoryRow{id: "local/" + plan.ID, source: plan.Account.Address, destination: plan.Recipient, amount: value, comment: plan.Memo, state: operation.State, timestamp: operation.CreatedAt}
		if a.walletHistoryOperation(account, operation) {
			entry.operation = &state.Local.Operations[i]
		}
		if operation.Receipt != nil {
			entry.transaction = operation.Receipt.Identity
			entry.explorer = chainutil.TransactionExplorer(account.Network, entry.transaction)
			if operation.Receipt.Timestamp > 0 {
				entry.timestamp = operation.Receipt.Timestamp
			}
		}
		if description, ok := stakingJournalDescription(plan); ok {
			entry.comment = description
		}
		rows = append(rows, entry)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].timestamp != rows[j].timestamp {
			return rows[i].timestamp > rows[j].timestamp
		}
		return rows[i].id < rows[j].id
	})
	return rows
}

func historyPeerEqual(family, left, right string) bool {
	if family == "evm" || family == "hypercore" {
		return strings.EqualFold(left, right)
	}
	if family == "ton" {
		parse := func(value string) (*address.Address, error) {
			if strings.Contains(value, ":") {
				return address.ParseRawAddr(value)
			}
			return address.ParseAddr(value)
		}
		a, ae := parse(left)
		b, be := parse(right)
		return ae == nil && be == nil && a.Type() == address.StdAddress && b.Type() == address.StdAddress && a.Equals(b)
	}
	return left == right
}

func (a *App) walletHistoryNodes(account model.Account) []webui.Node {
	state := a.walletHistory[account.ID]
	nodes := []webui.Node{heading("Transaction history"), row(a.button("walletHistory/"+account.ID, "Refresh history", state.Loading || state.Request != ""))}
	if a.active != "wallet/"+account.ID {
		// Inactive pages do not allocate flattened history rows on each wallet
		// update. Activation renders their retained account-scoped cache.
		return append(nodes, node("transactionTable", "walletHistoryTable/"+account.ID, "Native currency and tracked token transactions", ""))
	}
	switch {
	case state.Loading || state.Request != "":
		nodes = append(nodes, label("Loading transactions…"))
	case !state.Loaded:
		nodes = append(nodes, label("History has not been loaded."))
	}
	if state.ChainFailed {
		nodes = append(nodes, label("Chain history is unavailable. Displayed local payments do not establish complete incoming history."))
	}
	if state.ChainPaused {
		nodes = append(nodes, label("Chain history loading was paused for your action. Refresh history when ready."))
	}
	if state.LocalFailed {
		nodes = append(nodes, label("Local payment history is unavailable. Chain observations cannot recover locally pending operations."))
	}
	if state.Chain.Scope != "" {
		nodes = append(nodes, label(state.Chain.Scope))
	}
	table := node("transactionTable", "walletHistoryTable/"+account.ID, "Native currency and tracked token transactions", "")
	rows := a.walletHistoryRows(account, state)
	offset, _ := strconv.Atoi(a.fields["historyRows/"+account.ID])
	if offset < 0 || offset >= len(rows) {
		offset = 0
	}
	end := min(offset+128, len(rows))
	for i := offset; i < end; i++ {
		entry := rows[i]
		identity := sha256.Sum256([]byte(entry.id))
		stamp := "—"
		if entry.timestamp > 0 {
			stamp = time.Unix(entry.timestamp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
		}
		unknown := func(s string) string {
			if s == "" {
				return "—"
			}
			return s
		}
		status := label(unknown(entry.state))
		if entry.operation != nil {
			operation := *entry.operation
			kinds := historyOperationKinds(operation.State)
			if len(kinds) > 0 {
				status = node("column", "walletHistoryStatus/"+hex.EncodeToString(identity[:]), operation.State, operation.Plan.ID)
				status.Children = []webui.Node{label(unknown(entry.state))}
				for _, kind := range kinds {
					text := map[string]string{"sendOperation": "Review", "cancelPrepared": "Cancel prepared", "retry": "Broadcast original signed operation", "reconcile": "Check status"}[kind]
					button := a.button(kind+"/"+operation.Plan.ID, text, !a.state.RecoveryConfirmed && (kind == "sendOperation" || kind == "retry") || !state.Loaded || state.LocalFailed || state.Loading || state.Request != "")
					button.Tooltip = map[string]string{"sendOperation": "Review the original prepared operation before signing", "cancelPrepared": "Cancel only this original unsigned prepared operation", "retry": "Broadcast the original already-approved signed operation; never create a replacement payment or signature", "reconcile": "Check the original operation identity without creating or signing a new payment"}[kind]
					status.Children = append(status.Children, button)
				}
			}
		}
		table.Children = append(table.Children, webui.Node{Kind: "row", ID: "walletHistoryRow/" + account.ID + "/" + hex.EncodeToString(identity[:]), Children: []webui.Node{label(stamp), node("address", "", "", unknown(entry.source)), node("address", "", "", unknown(entry.destination)), label(unknown(entry.amount)), node("explorer", "", unknown(entry.transaction), entry.explorer), label(unknown(entry.comment)), status}})
	}
	nodes = append(nodes, table)
	if len(rows) > 128 {
		nodes = append(nodes, row(a.button("walletHistoryPreviousRows/"+account.ID, "Previous transactions", offset == 0), label(strconv.Itoa(offset+1)+"–"+strconv.Itoa(end)+" of "+strconv.Itoa(len(rows))+" loaded transfers"), a.button("walletHistoryNextRows/"+account.ID, "Next transactions", end >= len(rows))))
	}
	if state.Loaded && !state.Loading && state.Request == "" && len(rows) == 0 {
		switch {
		case state.ChainFailed || state.LocalFailed:
			nodes = append(nodes, label("Transaction history could not be loaded. Refresh history to try again."))
		case state.ChainPaused:
			nodes = append(nodes, label("Transaction history loading is paused. Refresh history to continue."))
		case state.Chain.NextCursor != "" || state.Local.NextCursor != "":
			nodes = append(nodes, label("No matching transfers on these pages. More history is available; load older transactions."))
		case !state.Chain.Complete:
			nodes = append(nodes, label("Historical activity has not been fully checked. No matching transfers have been loaded."))
		default:
			nodes = append(nodes, label("No matching native or tracked-token transfers were found in the completed provider query."))
		}
	}
	if state.Chain.NextCursor != "" {
		nodes = append(nodes, a.button("walletHistoryMore/"+account.ID, "Load older chain transactions", state.Loading))
	}
	if state.Local.NextCursor != "" {
		nodes = append(nodes, a.button("walletHistoryLocalMore/"+account.ID, "Load older local payments", state.Loading))
	}
	return nodes
}
