package ton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"

	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

// Each opaque cursor binds both independent hash-linked account chains to the
// owned regular account. It contains identities only, never provider URLs.
type usdtHistoryCursor struct {
	Version        int                  `json:"v"`
	Account        string               `json:"a"`
	Native         *transactionPosition `json:"n,omitempty"`
	Token          *transactionPosition `json:"t,omitempty"`
	NativeDone     bool                 `json:"nd"`
	TokenDone      bool                 `json:"td"`
	NativeComplete bool                 `json:"nc"`
	TokenComplete  bool                 `json:"tc"`
}

func encodeUSDTPageCursor(c usdtHistoryCursor) string {
	if c.Native != nil {
		pos := *c.Native
		pos.Account = ""
		c.Native = &pos
	}
	if c.Token != nil {
		pos := *c.Token
		pos.Account = ""
		c.Token = &pos
	}
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}
func decodeUSDTPageCursor(ac model.Account, cursor string) (usdtHistoryCursor, error) {
	out := usdtHistoryCursor{Version: 2, Account: positionAccount(ac.Network, ac.Address)}
	if cursor == "" {
		return out, nil
	}
	if len(cursor) > 512 {
		return out, errors.New("TON history cursor exceeds bound")
	}
	raw, e := base64.RawURLEncoding.DecodeString(cursor)
	if e != nil {
		return out, errors.New("invalid TON history cursor")
	}
	var candidate usdtHistoryCursor
	if strictjson.Decode(raw, &candidate) == nil && candidate.Version == 2 {
		if candidate.Account != out.Account || (candidate.NativeDone && candidate.Native != nil) || (candidate.TokenDone && candidate.Token != nil) || (candidate.NativeComplete && !candidate.NativeDone) || (candidate.TokenComplete && !candidate.TokenDone) {
			return out, errors.New("TON mixed history cursor context mismatch")
		}
		out = candidate
		return out, nil
	}
	// Native-only continuation identities from existing releases remain valid.
	native, e := decodePosition(cursor, ac.Network, ac.Address)
	if e != nil {
		return out, e
	}
	out.Native = native
	return out, nil
}
func checkedCursorPosition(pos *transactionPosition, network, account string) (*transactionPosition, error) {
	if pos == nil {
		return nil, nil
	}
	copy := *pos
	if copy.Account != "" && copy.Account != positionAccount(network, account) {
		return nil, errors.New("TON history position owner mismatch")
	}
	copy.Account = positionAccount(network, account)
	raw, _ := json.Marshal(copy)
	return decodePosition(base64.RawURLEncoding.EncodeToString(raw), network, account)
}
func (a *Adapter) usdtHistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	c, e := decodeUSDTPageCursor(ac, cursor)
	if e != nil {
		return model.HistoryPage{}, e
	}
	if _, e = a.verifyNetwork(ctx, ac.Network); e != nil {
		return model.HistoryPage{}, e
	}
	page := model.HistoryPage{Scope: a.HistoryScope()}
	advanced := false
	if !c.NativeDone {
		pos, e := checkedCursorPosition(c.Native, ac.Network, ac.Address)
		if e != nil {
			return model.HistoryPage{}, e
		}
		txs, e := a.transactionsAt(ctx, ac.Network, ac.Address, pos)
		if e != nil {
			return model.HistoryPage{}, e
		}
		next, complete, e := linkedTransactions(txs, pos)
		if e != nil {
			return model.HistoryPage{}, e
		}
		entries, e := a.historyReceipts(ac, txs)
		if e != nil {
			return model.HistoryPage{}, e
		}
		page.Entries = append(page.Entries, entries...)
		c.Native = next
		c.NativeDone = next == nil
		c.NativeComplete = complete
		advanced = true
	}
	if !c.TokenDone {
		entries, next, complete, tokenErr := a.usdtHistoryPart(ctx, ac, c.Token)
		if tokenErr == nil {
			page.Entries = append(page.Entries, entries...)
			c.Token = next
			c.TokenDone = next == nil
			c.TokenComplete = complete
			advanced = true
		}
		if tokenErr != nil {
			if !advanced {
				return model.HistoryPage{}, tokenErr
			}
			page.Scope += " Canonical USDT history is currently unavailable; successful native observations are retained, and token history must be retried."
		}
	}
	page.Complete = c.NativeDone && c.TokenDone && c.NativeComplete && c.TokenComplete
	if !c.NativeDone || !c.TokenDone {
		page.NextCursor = encodeUSDTPageCursor(c)
	}
	sort.SliceStable(page.Entries, func(i, j int) bool { return page.Entries[i].Timestamp > page.Entries[j].Timestamp })
	return page, nil
}

func (a *Adapter) usdtHistoryPart(ctx context.Context, ac model.Account, position *transactionPosition) ([]model.Receipt, *transactionPosition, bool, error) {
	if e := a.verifyUSDT(ctx, ac.Network); e != nil {
		return nil, nil, false, e
	}
	wallet, e := a.jettonState(ctx, ac.Network, ac.Address)
	if e != nil {
		return nil, nil, false, e
	}
	pos, e := checkedCursorPosition(position, ac.Network, wallet.Address)
	if e != nil {
		return nil, nil, false, e
	}
	txs, e := a.transactionsAt(ctx, ac.Network, wallet.Address, pos)
	if e != nil {
		return nil, nil, false, e
	}
	next, complete, e := linkedTransactions(txs, pos)
	if e != nil {
		return nil, nil, false, e
	}
	entries, e := jettonHistoryReceipts(ac, txs)
	return entries, next, complete, e
}
