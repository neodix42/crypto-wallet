package hypercore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"strings"
	"time"
)

const historyWindow = uint64(30 * 24 * time.Hour / time.Millisecond)
const historyPageSize = 128
const historyPageBudget = 128

func (*Adapter) HistoryScope() string {
	return "Provider-observed spot HYPE transfers in a fixed window covering the latest 30 days, with bounded inclusive timestamp pagination. Completion describes that provider window only; it does not establish complete historical recovery or the outcome of an unresolved signed action. A saturated single timestamp cannot be skipped safely."
}

type historyCursor struct {
	Version int    `json:"v"`
	Account string `json:"account"`
	End     uint64 `json:"end"`
	Next    uint64 `json:"next"`
	Offset  int    `json:"offset"`
	Page    uint32 `json:"page"`
	Hash    string `json:"hash"`
}

func historyStart(end uint64) uint64 {
	if end > historyWindow {
		return end - historyWindow
	}
	return 0
}
func accountCommitment(ac model.Account) string {
	raw, _ := json.Marshal(ac)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func encodeHistoryCursor(c historyCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeHistoryCursor(raw string, ac model.Account, at uint64) (historyCursor, error) {
	var c historyCursor
	if len(raw) == 0 || len(raw) > 1024 {
		return c, errors.New("invalid Core history cursor")
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || evm.StrictJSON(b, &c) != nil || encodeHistoryCursor(c) != raw || c.Version != 1 || c.Account != accountCommitment(ac) || c.End == 0 || c.End > at+30_000 || c.Next < historyStart(c.End) || c.Next > c.End || c.Offset < 0 || c.Offset >= ledgerResponseLimit || c.Page >= historyPageBudget {
		return historyCursor{}, errors.New("invalid or exhausted Core history cursor")
	}
	if c.Offset == 0 && c.Hash != "" || c.Offset > 0 && !validHash("0x"+c.Hash) {
		return historyCursor{}, errors.New("invalid Core history page commitment")
	}
	return c, nil
}

// HistoryPage returns no more than 128 public receipts. Continuations pin both
// the account descriptor and original time window. Splitting a fetched API page
// additionally commits its contents, refusing inconsistent provider rereads.
func (a *Adapter) HistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	n, e := a.validateAccount(ac)
	if e != nil {
		return model.HistoryPage{}, e
	}
	_, at, e := a.identity(ctx, n)
	if e != nil {
		return model.HistoryPage{}, e
	}
	c := historyCursor{Version: 1, Account: accountCommitment(ac), End: at, Next: historyStart(at)}
	if cursor != "" {
		c, e = decodeHistoryCursor(cursor, ac, at)
		if e != nil {
			return model.HistoryPage{}, e
		}
	}
	page, e := a.ledgerPage(ctx, n, ac.Address, c.Next, c.End)
	if e != nil {
		return model.HistoryPage{}, e
	}
	if c.Offset > len(page.Events) || c.Offset > 0 && c.Hash != page.Hash {
		return model.HistoryPage{}, errors.New("Core history provider changed a committed page; restart this scan")
	}
	end := c.Offset + historyPageSize
	if end > len(page.Events) {
		end = len(page.Events)
	}
	out := model.HistoryPage{Entries: []model.Receipt{}, Scope: a.HistoryScope()}
	seen := map[string]bool{}
	// Check every candidate in this fetched page before emitting any slice. The
	// provider cannot hide an ambiguous duplicate behind a later UI continuation.
	for i, u := range page.Events {
		r, include, e := historyReceipt(n, ac, u, historyStart(c.End), c.End)
		if e != nil {
			return model.HistoryPage{}, e
		}
		if !include {
			continue
		}
		if seen[r.Identity] {
			return model.HistoryPage{}, errors.New("duplicate Core history identity")
		}
		seen[r.Identity] = true
		if i >= c.Offset && i < end {
			out.Entries = append(out.Entries, r)
		}
	}
	if end < len(page.Events) {
		c.Offset = end
		c.Hash = page.Hash
	} else if !page.Complete {
		c.Next = page.Next
		c.Offset = 0
		c.Hash = ""
	} else {
		out.Complete = true
		return out, nil
	}
	c.Page++
	if c.Page >= historyPageBudget {
		return model.HistoryPage{}, errors.New("Core history page budget exhausted; scan is incomplete")
	}
	out.NextCursor = encodeHistoryCursor(c)
	return out, nil
}

func historyReceipt(n model.Network, ac model.Account, u ledger, start, end uint64) (model.Receipt, bool, error) {
	d := u.Delta
	if d.Type != "spotTransfer" || d.Token != "HYPE" && d.Token != n.Assets[0].Contract {
		return model.Receipt{}, false, nil
	}
	if !strings.EqualFold(d.User, ac.Address) && !strings.EqualFold(d.Destination, ac.Address) {
		return model.Receipt{}, false, errors.New("Core history does not involve account")
	}
	if e := evm.New().ValidateAddress("ethereum-mainnet", d.User); e != nil {
		return model.Receipt{}, false, e
	}
	if e := evm.New().ValidateAddress("ethereum-mainnet", d.Destination); e != nil {
		return model.Receipt{}, false, e
	}
	value, e := decimalJSON(d.Amount)
	if e != nil {
		return model.Receipt{}, false, e
	}
	fee, e := decimalJSON(d.Fee)
	if e != nil {
		return model.Receipt{}, false, e
	}
	nativeFee := "not provided"
	if len(d.NativeTokenFee) > 0 {
		v, e := decimalJSON(d.NativeTokenFee)
		if e != nil {
			return model.Receipt{}, false, e
		}
		nativeFee = v.String()
	}
	direction, peer := "received", d.User
	if strings.EqualFold(d.User, ac.Address) {
		direction, peer = "sent", d.Destination
	}
	detail := fmt.Sprintf("provider-observed %s %s atomic %s; peer %s; provider fee %s (fee asset not established), native HYPE fee %s atomic; bounded times %d..%d", direction, value.String(), n.Assets[0].ID, peer, formatUnits(fee), nativeFee, start, end)
	return model.Receipt{Identity: u.Hash, State: "OBSERVED", Detail: detail, Timestamp: int64(u.Time / 1000), Explorer: chainutil.TransactionExplorer(ac.Network, u.Hash), Transfers: []model.HistoryTransfer{{ID: "spotTransfer", Source: d.User, Destination: d.Destination, Asset: n.Assets[0], Atomic: value.String()}}}, true, nil
}

// History preserves the legacy bounded snapshot API for nonpaged callers. The
// desktop uses HistoryPage; hitting this compatibility budget returns an error.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	out := []model.Receipt{}
	cursor := ""
	for i := 0; i < ledgerRequestBudget; i++ {
		page, e := a.HistoryPage(ctx, ac, cursor)
		if e != nil {
			return nil, e
		}
		out = append(out, page.Entries...)
		if page.Complete {
			return out, nil
		}
		cursor = page.NextCursor
	}
	return nil, errors.New("Core history snapshot budget exhausted; use paginated history")
}

var _ model.HistoryPager = (*Adapter)(nil)
