package hypercore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"localwallet/internal/model"
)

const ledgerResponseLimit = 500
const ledgerRequestBudget = 32

// ledgerPage follows the API's inclusive timestamp pagination. A saturated
// response's final timestamp is deferred in full until the next query, so equal
// timestamps cannot be dropped by advancing one millisecond or counted twice.
// The documented API has no offset for a single saturated timestamp: that case
// remains explicitly incomplete instead of skipping records.
type ledgerPage struct {
	Events   []ledger
	Next     uint64
	Complete bool
	Hash     string
}

func (a *Adapter) ledgerPage(ctx context.Context, n model.Network, address string, start, end uint64) (ledgerPage, error) {
	var out ledgerPage
	if start > end {
		return out, errors.New("invalid Core ledger time range")
	}
	var updates []ledger
	if e := a.request(ctx, n, "/info", map[string]any{"type": "userNonFundingLedgerUpdates", "user": address, "startTime": start, "endTime": end}, &updates); e != nil {
		return out, e
	}
	if updates == nil || len(updates) > ledgerResponseLimit {
		return out, errors.New("missing or oversized Core ledger page")
	}
	seen := make(map[string]bool, len(updates))
	previous := start
	for _, u := range updates {
		if u.Time == 0 || u.Time < previous || u.Time > end || !validHash(u.Hash) || u.Delta.Type == "" {
			return out, errors.New("invalid Core ledger identity, range or ordering")
		}
		previous = u.Time
		raw, _ := json.Marshal(u)
		fingerprint := sha256.Sum256(raw)
		key := hex.EncodeToString(fingerprint[:])
		if seen[key] {
			return out, errors.New("duplicate Core ledger event")
		}
		seen[key] = true
	}
	raw, _ := json.Marshal(updates)
	sum := sha256.Sum256(raw)
	out.Hash = hex.EncodeToString(sum[:])
	out.Events = updates
	out.Complete = len(updates) < ledgerResponseLimit
	if !out.Complete {
		last := updates[len(updates)-1].Time
		keep := len(updates)
		for keep > 0 && updates[keep-1].Time == last {
			keep--
		}
		if keep == 0 || last <= start {
			return ledgerPage{}, errors.New("Core ledger timestamp saturated; documented pagination cannot establish completeness")
		}
		out.Events = updates[:keep]
		out.Next = last
	}
	return out, nil
}

// ledgerWindow is bounded independently of the public history UI. Reconciliation
// never interprets a truncated scan or a provider failure as payment failure.
func (a *Adapter) ledgerWindow(ctx context.Context, n model.Network, address string, start, end uint64) ([]ledger, error) {
	var out []ledger
	for requests := 0; requests < ledgerRequestBudget; requests++ {
		page, e := a.ledgerPage(ctx, n, address, start, end)
		if e != nil {
			return nil, e
		}
		out = append(out, page.Events...)
		if page.Complete {
			return out, nil
		}
		start = page.Next
	}
	return nil, errors.New("Core ledger pagination request budget exhausted; outcome remains unknown")
}
