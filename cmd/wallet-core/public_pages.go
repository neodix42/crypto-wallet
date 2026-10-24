package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"localwallet/internal/journal"
)

type publicContinuation struct {
	kind, query, position, revision, session string
	epoch                                    uint64
	expires                                  time.Time
}
type journalPage struct {
	Operations []journal.Operation `json:"operations"`
	NextCursor string              `json:"nextCursor"`
	Complete   bool                `json:"complete"`
	Revision   string              `json:"revision"`
	Total      int                 `json:"total"`
}

func (c *core) consumePublicPage(epoch uint64, session, kind, query, cursor string) (publicContinuation, error) {
	if len(cursor) != 32 || !lowerHex(cursor) {
		return publicContinuation{}, errors.New("invalid public continuation")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	position, ok := c.publicScans[cursor]
	if !ok || c.epoch != epoch || position.epoch != epoch || position.session != session || position.kind != kind || position.query != query || !time.Now().Before(position.expires) {
		return publicContinuation{}, errors.New("public continuation expired or context changed; refresh")
	}
	delete(c.publicScans, cursor)
	return position, nil
}
func (c *core) retainPublicPage(epoch uint64, session, kind, query, position, revision string) (string, error) {
	if position == "" {
		return "", nil
	}
	if len(position) > 4096 || len(revision) > 64 || revision == "" {
		return "", errors.New("invalid signer page position")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", errors.New("continuation unavailable")
	}
	token := hex.EncodeToString(nonce[:])
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != epoch {
		return "", errors.New("wallet session changed")
	}
	for key, scan := range c.publicScans {
		if !time.Now().Before(scan.expires) {
			delete(c.publicScans, key)
		}
	}
	if len(c.publicScans) >= 64 {
		return "", errors.New("too many public page continuations; refresh after expiry")
	}
	if c.publicScans == nil {
		c.publicScans = map[string]publicContinuation{}
	}
	c.publicScans[token] = publicContinuation{kind: kind, query: query, position: position, revision: revision, session: session, epoch: epoch, expires: time.Now().Add(5 * time.Minute)}
	return token, nil
}
func (c *core) dropPublicPages(session, kind, query string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, page := range c.publicScans {
		if page.session == session && page.kind == kind && page.query == query {
			delete(c.publicScans, key)
		}
	}
}
func (c *core) publicAccountsAt(epoch uint64, session, cursor string) (snapshot, error) {
	var s snapshot
	var err error
	if cursor == "" {
		c.dropPublicPages(session, "accounts", "")
		s, err = c.current()
	} else {
		var position publicContinuation
		position, err = c.consumePublicPage(epoch, session, "accounts", "", cursor)
		if err == nil {
			err = c.childCallAt(epoch, "accountPage", struct {
				Cursor string `json:"cursor"`
			}{position.position}, &s)
			if err == nil && (s.Locked || s.Revision != position.revision) {
				err = errors.New("wallet generation changed during account paging")
			}
		}
	}
	if err != nil {
		return snapshot{}, err
	}
	s.Operations = nil
	s.AccountsCursor, err = c.retainPublicPage(epoch, session, "accounts", "", s.AccountsCursor, s.Revision)
	return s, err
}
func (c *core) publicJournalAt(epoch uint64, session, accountID, asset, cursor string) (journalPage, error) {
	if (accountID == "") != (asset == "") {
		return journalPage{}, errors.New("journal account and asset must be selected together")
	}
	if accountID != "" {
		if _, err := c.account(accountID); err != nil {
			return journalPage{}, err
		}
	}
	query := accountID + "\x00" + asset
	privateCursor, revision := "", ""
	if cursor == "" {
		c.dropPublicPages(session, "journal", query)
	}
	if cursor != "" {
		position, err := c.consumePublicPage(epoch, session, "journal", query, cursor)
		if err != nil {
			return journalPage{}, err
		}
		privateCursor, revision = position.position, position.revision
	}
	var page journalPage
	err := c.childCallAt(epoch, "journalPage", struct {
		AccountID string `json:"accountId"`
		Asset     string `json:"asset"`
		Cursor    string `json:"cursor"`
	}{accountID, asset, privateCursor}, &page)
	if err != nil {
		return journalPage{}, err
	}
	if len(page.Operations) > 128 || page.Total < 0 || page.Total > 10000 || page.Total < len(page.Operations) || revision != "" && page.Revision != revision || page.Complete && page.NextCursor != "" || !page.Complete && page.NextCursor == "" || page.NextCursor != "" && page.NextCursor == privateCursor {
		return journalPage{}, errors.New("invalid journal continuation or generation")
	}
	seen := map[string]bool{}
	for _, operation := range page.Operations {
		if operation.Signed != nil || operation.Plan.ID == "" || seen[operation.Plan.ID] || accountID != "" && (operation.Plan.Account.ID != accountID || operation.Plan.Asset != asset) {
			return journalPage{}, errors.New("journal page contains forbidden or mismatched payment data")
		}
		seen[operation.Plan.ID] = true
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > historyPageBytes {
		return journalPage{}, errors.New("journal page exceeds public IPC bound")
	}
	page.NextCursor, err = c.retainPublicPage(epoch, session, "journal", query, page.NextCursor, page.Revision)
	return page, err
}

// Every outward state response uses coordinator capabilities. Signer positions
// stay on the inherited private channel, including metadata mutation replies.
func (c *core) publicSnapshotResultAt(epoch uint64, session string, raw json.RawMessage) (snapshot, error) {
	var state snapshot
	if json.Unmarshal(raw, &state) != nil {
		return snapshot{}, errors.New("invalid wallet state response")
	}
	state.Operations = nil
	var err error
	state.AccountsCursor, err = c.retainPublicPage(epoch, session, "accounts", "", state.AccountsCursor, state.Revision)
	return state, err
}
