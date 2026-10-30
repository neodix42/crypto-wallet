package walletcore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"localwallet/internal/chains/decred"
	"localwallet/internal/model"
)

const historyPageBytes = 256 << 10 // Leaves ample room for the 1 MiB IPC envelope.
const historyPageEntries = 128
const historyScanEntries = 10000
const historyRetainedBytes = 4 << 20
const historyAllRetainedBytes = 16 << 20
const historyContexts = 16
const historyFetchPages = 4
const historyAdapterPages = 256

// A continuation is a one-use capability, never a URL or adapter-provided token.
// Its state is volatile and expires on lock, RPC reconfiguration or inactivity.
type historyScan struct {
	epoch            uint64
	session, account string
	expires          time.Time
	pending          []model.Receipt
	next, scope      string
	complete         bool
	seen             map[string][32]byte
	positions        map[string]bool
	pages            int
	bytes            int
}

func accountHistoryKey(ac model.Account) string {
	raw, _ := json.Marshal(ac)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (c *core) chainHistoryAt(epoch uint64, session, accountID, cursor string) (any, error) {
	if len(cursor) != 0 && (len(cursor) != 32 || !lowerHex(cursor)) {
		return nil, errors.New("invalid history continuation")
	}
	ac, err := c.account(accountID)
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(ac.Network)
	if err != nil {
		return nil, err
	}
	var dcrReport *decred.SyncReport
	key := accountHistoryKey(ac)
	c.mu.Lock()
	if c.epoch != epoch {
		c.mu.Unlock()
		return nil, errors.New("wallet session changed")
	}
	for token, cached := range c.historyScans {
		if !time.Now().Before(cached.expires) {
			delete(c.historyScans, token)
		}
	}
	var scan *historyScan
	if cursor != "" {
		scan = c.historyScans[cursor]
		if scan == nil || scan.epoch != epoch || scan.session != session || scan.account != key {
			c.mu.Unlock()
			return nil, errors.New("history continuation expired or context changed; restart history")
		}
		// Consume before any network request. Retry starts a fresh scan after error.
		delete(c.historyScans, cursor)
	} else {
		for token, cached := range c.historyScans {
			if cached.account == key && cached.session == session {
				delete(c.historyScans, token)
			}
		}
		scan = &historyScan{epoch: epoch, session: session, account: key, seen: map[string][32]byte{}, positions: map[string]bool{}}
	}
	c.mu.Unlock()
	ctx, cancel := c.historyContextAt(2*time.Minute, epoch, session)
	defer cancel()
	initial := cursor == ""
	for fetched := 0; fetched < historyFetchPages && len(scan.pending) == 0 && (initial || scan.next != ""); fetched++ {
		initial = false
		if scan.pages >= historyAdapterPages {
			return nil, errors.New("history provider page bound reached; refresh or narrow the query")
		}
		scan.pages++
		var page model.HistoryPage
		var err error
		if d, ok := adapter.(*decred.Adapter); ok {
			// A single authenticated scan supplies the same bounded transaction
			// pages as other families. Raw creation proofs never enter cursors.
			report, syncErr := d.SyncWithProgress(ctx, ac, nil)
			err = syncErr
			if err == nil {
				dcrReport = &report
				page = model.HistoryPage{Entries: decred.HistoryReceipts(ac.Network, report), Scope: d.HistoryScope()}
			}
		} else {
			page, err = historyAdapterPage(ctx, adapter, ac, scan.next)
		}
		if err != nil {
			return nil, err
		}
		if len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) || len(page.Scope) > 16384 || !utf8.ValidString(page.Scope) || page.Scope == "" || page.Complete && page.NextCursor != "" || page.NextCursor != "" && page.NextCursor == scan.next {
			return nil, errors.New("history provider returned an invalid or nonprogressing page")
		}
		if page.NextCursor != "" {
			if scan.positions[page.NextCursor] {
				return nil, errors.New("history provider repeated an earlier position")
			}
			scan.positions[page.NextCursor] = true
		}
		if len(page.Entries) > historyScanEntries {
			return nil, errors.New("history scan reached its record bound; narrow the query")
		}
		raw, err := json.Marshal(page.Entries)
		if err != nil || len(raw) > historyRetainedBytes {
			return nil, errors.New("history snapshot exceeds memory bound")
		}
		scan.bytes = len(raw)
		overlap := false
		if pager, ok := adapter.(interface{ HistoryPagesOverlap() bool }); ok {
			overlap = pager.HistoryPagesOverlap()
		}
		withinPage := map[string]bool{}
		unique := make([]model.Receipt, 0, len(page.Entries))
		for _, row := range page.Entries {
			if err := validateHistoryReceipt(adapter, ac, row); err != nil {
				return nil, err
			}
			if row.Identity == "" || len(row.Identity) > 512 || !utf8.ValidString(row.Identity) || withinPage[row.Identity] {
				return nil, errors.New("history provider returned a duplicate or invalid transaction identity")
			}
			withinPage[row.Identity] = true
			fingerprint := historyObservationHash(row)
			if previous, exists := scan.seen[row.Identity]; exists {
				if !overlap || fingerprint != previous {
					return nil, errors.New("history provider repeated or contradicted an earlier observation")
				}
				continue
			}
			if len(scan.seen) >= historyScanEntries {
				return nil, errors.New("history scan reached its record bound; narrow the query")
			}
			scan.seen[row.Identity] = fingerprint
			unique = append(unique, row)
		}
		scan.pending, scan.next, scan.scope, scan.complete = unique, page.NextCursor, page.Scope, page.Complete
	}
	page := model.HistoryPage{Entries: []model.Receipt{}, Scope: scan.scope}
	used := 0
	for used < len(scan.pending) && used < historyPageEntries {
		page.Entries = append(page.Entries, scan.pending[used])
		raw, err := json.Marshal(page)
		if err != nil || len(raw) > historyPageBytes-1024 {
			page.Entries = page.Entries[:len(page.Entries)-1]
			break
		}
		used++
	}
	if used == 0 && len(scan.pending) != 0 {
		return nil, errors.New("single history record exceeds page bound")
	}
	scan.pending = append([]model.Receipt(nil), scan.pending[used:]...)
	page.Complete = scan.complete && len(scan.pending) == 0
	if len(scan.pending) > 0 || scan.next != "" {
		// Recompute actual retained public-data size; old pages are never accumulated.
		retained, _ := json.Marshal(scan.pending)
		scan.bytes = len(retained)
		for identity := range scan.seen {
			scan.bytes += len(identity) + 64
		}
		for position := range scan.positions {
			scan.bytes += len(position) + 32
		}
		if scan.bytes > historyRetainedBytes {
			return nil, errors.New("history scan reached memory bound; restart with a narrower query")
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, errors.New("history continuation unavailable")
		}
		token := hex.EncodeToString(nonce[:])
		scan.expires = time.Now().Add(5 * time.Minute)
		c.mu.Lock()
		total := scan.bytes
		for _, cached := range c.historyScans {
			total += cached.bytes
		}
		if c.epoch != epoch || len(c.historyScans) >= historyContexts || total > historyAllRetainedBytes {
			c.mu.Unlock()
			return nil, errors.New("history context or memory bound reached; restart history")
		}
		if c.historyScans == nil {
			c.historyScans = map[string]*historyScan{}
		}
		c.historyScans[token] = scan
		c.mu.Unlock()
		page.NextCursor = token
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > historyPageBytes {
		return nil, errors.New("history page exceeds IPC bound")
	}
	if dcrReport != nil {
		// Preserve the legacy detailed report on an initial page when it fits;
		// larger scans retain usable transaction pagination rather than failing
		// merely because the additional report duplicates every history row.
		result := struct {
			model.HistoryPage
			Report *decred.SyncReport `json:"report"`
		}{page, dcrReport}
		if envelope, encodeErr := json.Marshal(result); encodeErr == nil && len(envelope) <= historyPageBytes {
			return result, nil
		}
		page.Scope += " Detailed synchronization report omitted from this page to retain the response-size bound."
	}
	return page, nil
}

// Reviewed multi-stream indexes can repeat the same transaction on their
// native/receipt/token pages. Only an identical interpreted observation may be
// suppressed; descriptions do not define an amount, effect or finality state.
func historyObservationHash(row model.Receipt) [32]byte {
	row.Detail = ""
	row.Transfers = append([]model.HistoryTransfer(nil), row.Transfers...)
	sort.Slice(row.Transfers, func(i, j int) bool { return row.Transfers[i].ID < row.Transfers[j].ID })
	raw, _ := json.Marshal(row)
	return sha256.Sum256(raw)
}
func lowerHex(s string) bool {
	for _, ch := range s {
		if ch < '0' || ch > '9' && ch < 'a' || ch > 'f' {
			return false
		}
	}
	return true
}

func historyAdapterPage(ctx context.Context, adapter model.Adapter, ac model.Account, cursor string) (model.HistoryPage, error) {
	if pager, ok := adapter.(model.HistoryPager); ok {
		return pager.HistoryPage(ctx, ac, cursor)
	}
	if cursor != "" {
		return model.HistoryPage{}, errors.New("history continuation unavailable")
	}
	if h, ok := adapter.(interface {
		History(context.Context, model.Account) ([]model.Receipt, error)
		HistoryScope() string
	}); ok {
		entries, err := h.History(ctx, ac)
		return model.HistoryPage{Entries: entries, Scope: h.HistoryScope()}, err
	}
	return model.HistoryPage{Entries: []model.Receipt{}, Scope: "Network incoming history is not implemented for this family; local outgoing journal only. An empty journal is not proof of no funds."}, nil
}
