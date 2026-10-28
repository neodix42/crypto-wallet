package signer

import (
	"encoding/base64"
	"encoding/json"
	"strconv"

	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

const PublicPageBytes = 256 << 10
const PublicPageRecords = 128

// SnapshotPage deliberately omits the journal. Reservations and exact signed
// artifacts remain in the engine; a public page never changes or frees them.
type SnapshotPage struct {
	Snapshot
	AccountsCursor  string `json:"accountsCursor"`
	TotalAccounts   int    `json:"totalAccounts"`
	TotalOperations int    `json:"totalOperations"`
	Revision        string `json:"revision"`
}
type JournalPage struct {
	Operations []journal.Operation `json:"operations"`
	NextCursor string              `json:"nextCursor"`
	Complete   bool                `json:"complete"`
	Revision   string              `json:"revision"`
	Total      int                 `json:"total"`
}
type publicPosition struct {
	Revision string `json:"revision"`
	Kind     string `json:"kind"`
	Account  string `json:"account"`
	Asset    string `json:"asset"`
	Offset   int    `json:"offset"`
}

func (e *Engine) publicPosition(cursor, kind, account, asset string, limit int) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if e.data == nil || len(cursor) > 2048 {
		return 0, ErrDenied
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	var position publicPosition
	if err != nil || strictjson.Decode(raw, &position) != nil || position.Revision != strconv.FormatUint(e.data.Generation, 10) || position.Kind != kind || position.Account != account || position.Asset != asset || position.Offset < 1 || position.Offset >= limit {
		return 0, ErrDenied
	}
	return position.Offset, nil
}
func (e *Engine) publicCursor(kind, account, asset string, offset int) string {
	raw, _ := json.Marshal(publicPosition{Revision: strconv.FormatUint(e.data.Generation, 10), Kind: kind, Account: account, Asset: asset, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func (e *Engine) SnapshotPage(cursor string) (SnapshotPage, error) {
	exists, state := e.walletPresence()
	page := SnapshotPage{Snapshot: Snapshot{Locked: e.data == nil, WalletExists: exists, VaultState: state, Environment: e.Environment}}
	if e.data == nil {
		if cursor != "" {
			return SnapshotPage{}, ErrDenied
		}
		return page, nil
	}
	if !exists {
		page.WalletExists = true
		page.VaultState = "unavailable"
	}
	page.Environment, page.RecoveryConfirmed = e.data.Environment, e.data.RecoveryConfirmed
	page.Accounts = []model.Account{}
	page.Revision = strconv.FormatUint(e.data.Generation, 10)
	accounts := e.activeAccounts()
	page.TotalAccounts, page.TotalOperations = len(accounts), len(e.data.Operations)
	offset, err := e.publicPosition(cursor, "accounts", "", "", len(accounts))
	if err != nil {
		return SnapshotPage{}, err
	}
	used := 0
	for offset+used < len(accounts) && used < PublicPageRecords {
		page.Accounts = append(page.Accounts, accounts[offset+used])
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > PublicPageBytes-4096 {
			page.Accounts = page.Accounts[:len(page.Accounts)-1]
			break
		}
		used++
	}
	if used == 0 && offset < len(accounts) {
		return SnapshotPage{}, ErrDenied
	}
	if offset+used < len(accounts) {
		page.AccountsCursor = e.publicCursor("accounts", "", "", offset+used)
	}
	return page, nil
}
func (e *Engine) PublicAccount(id string) (model.Account, error) {
	if e.data == nil {
		return model.Account{}, ErrDenied
	}
	for _, account := range e.data.Accounts {
		if account.ID == id && !evm.IsRetiredNetwork(account.Network) {
			return account, nil
		}
	}
	return model.Account{}, ErrDenied
}
func (e *Engine) Operation(id string) (journal.Operation, error) {
	if e.data == nil {
		return journal.Operation{}, ErrDenied
	}
	for _, operation := range e.data.Operations {
		if operation.Plan.ID == id {
			return operation, nil
		}
	}
	return journal.Operation{}, ErrDenied
}
func (e *Engine) JournalPage(account, asset, cursor string) (JournalPage, error) {
	if e.data == nil {
		return JournalPage{}, ErrDenied
	}
	if (account == "") != (asset == "") {
		return JournalPage{}, ErrDenied
	}
	if account != "" {
		ac, err := e.PublicAccount(account)
		if err != nil {
			return JournalPage{}, err
		}
		adapter, err := e.adapter(ac.Network)
		if err != nil {
			return JournalPage{}, err
		}
		found := false
		for _, network := range adapter.Networks() {
			if network.ID == ac.Network {
				for _, registered := range network.Assets {
					if registered.ID == asset {
						found = true
					}
				}
			}
		}
		if !found {
			return JournalPage{}, ErrDenied
		}
	}
	offset, err := e.publicPosition(cursor, "journal", account, asset, len(e.data.Operations))
	if err != nil {
		return JournalPage{}, err
	}
	page := JournalPage{Operations: []journal.Operation{}, Revision: strconv.FormatUint(e.data.Generation, 10)}
	for _, operation := range e.data.Operations {
		if account == "" || operation.Plan.Account.ID == account && operation.Plan.Asset == asset {
			page.Total++
		}
	}
	// Reverse append order is stable within an authenticated vault generation.
	examined := offset
	for examined < len(e.data.Operations) {
		operation := e.data.Operations[len(e.data.Operations)-1-examined]
		if account != "" && (operation.Plan.Account.ID != account || operation.Plan.Asset != asset) {
			examined++
			continue
		}
		operation.Signed = nil
		page.Operations = append(page.Operations, operation)
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > PublicPageBytes-4096 {
			page.Operations = page.Operations[:len(page.Operations)-1]
			break
		}
		examined++
		if len(page.Operations) >= PublicPageRecords {
			break
		}
	}
	if examined == offset && examined < len(e.data.Operations) {
		return JournalPage{}, ErrDenied
	}
	page.Complete = examined == len(e.data.Operations)
	if !page.Complete {
		page.NextCursor = e.publicCursor("journal", account, asset, examined)
	}
	return page, nil
}
