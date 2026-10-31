package ton

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

// TON Center's lt/hash start is inclusive. Request the oldest BOC's *previous*
// identity instead, and verify every BOC link and the requested first identity.
// Provider-supplied URLs/transaction_id metadata never control continuation.
type transactionPosition struct {
	LT      string `json:"lt"`
	Hash    string `json:"hash"`
	Account string `json:"account,omitempty"`
}

func positionAccount(network, address string) string {
	sum := sha256.Sum256([]byte(network + "\x00" + address))
	return hex.EncodeToString(sum[:])
}
func decodePosition(cursor, network, address string) (*transactionPosition, error) {
	if len(cursor) == 0 {
		return nil, nil
	}
	if len(cursor) > 512 {
		return nil, errors.New("invalid TON history cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	var pos transactionPosition
	if err != nil || strictjson.Decode(raw, &pos) != nil {
		return nil, errors.New("invalid TON history cursor")
	}
	lt, err := strconv.ParseUint(pos.LT, 10, 64)
	hash, hashErr := hex.DecodeString(pos.Hash)
	if err != nil || lt == 0 || strconv.FormatUint(lt, 10) != pos.LT || hashErr != nil || len(hash) != 32 || hex.EncodeToString(hash) != pos.Hash || pos.Account != positionAccount(network, address) {
		return nil, errors.New("invalid TON history cursor context or identity")
	}
	return &pos, nil
}
func linkedTransactions(txs []*tlb.Transaction, position *transactionPosition) (*transactionPosition, bool, error) {
	if len(txs) == 0 {
		if position != nil {
			return nil, false, errors.New("TON archival continuation omitted expected transaction")
		}
		// No head BOC has been authenticated, so an empty response cannot prove that
		// this account never had history. Preserve that uncertainty explicitly.
		return nil, false, nil
	}
	seen := map[string]bool{}
	for i, tx := range txs {
		identity := hex.EncodeToString(tx.Hash)
		if tx.LT == 0 || len(tx.Hash) != 32 || seen[identity] || len(tx.PrevTxHash) != 32 || tx.PrevTxLT >= tx.LT && tx.PrevTxLT != 0 {
			return nil, false, errors.New("TON archival transaction identity, order or previous link invalid")
		}
		seen[identity] = true
		if i == 0 && position != nil && (strconv.FormatUint(tx.LT, 10) != position.LT || identity != position.Hash) {
			return nil, false, errors.New("TON archival continuation did not match requested identity")
		}
		if i > 0 && (txs[i-1].PrevTxLT != tx.LT || !bytes.Equal(txs[i-1].PrevTxHash, tx.Hash)) {
			return nil, false, errors.New("TON archival account chain has a gap, duplicate or reordered transaction")
		}
	}
	last := txs[len(txs)-1]
	if last.PrevTxLT == 0 {
		if !bytes.Equal(last.PrevTxHash, make([]byte, 32)) {
			return nil, false, errors.New("TON archival terminal link has nonzero hash")
		}
		return nil, true, nil
	}
	return &transactionPosition{LT: strconv.FormatUint(last.PrevTxLT, 10), Hash: hex.EncodeToString(last.PrevTxHash)}, false, nil
}
func (a *Adapter) HistoryPage(ctx context.Context, ac model.Account, cursor string) (model.HistoryPage, error) {
	if _, _, err := descriptor(ac); err != nil {
		return model.HistoryPage{}, err
	}
	if ac.Network == "ton-mainnet" {
		return a.usdtHistoryPage(ctx, ac, cursor)
	}
	position, err := decodePosition(cursor, ac.Network, ac.Address)
	if err != nil {
		return model.HistoryPage{}, err
	}
	if _, err = a.verifyNetwork(ctx, ac.Network); err != nil {
		return model.HistoryPage{}, err
	}
	txs, err := a.transactionsAt(ctx, ac.Network, ac.Address, position)
	if err != nil {
		return model.HistoryPage{}, err
	}
	next, complete, err := linkedTransactions(txs, position)
	if err != nil {
		return model.HistoryPage{}, err
	}
	entries, err := a.historyReceipts(ac, txs)
	if err != nil {
		return model.HistoryPage{}, err
	}
	page := model.HistoryPage{Entries: entries, Complete: complete, Scope: a.HistoryScope()}
	if next != nil {
		next.Account = positionAccount(ac.Network, ac.Address)
		raw, _ := json.Marshal(next)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

const reconciliationPages = 32 // Explicit per-attempt budget; no automatic resend.

// Search the account's exact hash-linked archival chain with bounded memory.
// A missing transaction remains UNKNOWN, even when the chain beginning is seen.
func (a *Adapter) findArchivedTransaction(ctx context.Context, network, address string, match func(*tlb.Transaction) bool) (*tlb.Transaction, bool, error) {
	var position *transactionPosition
	for page := 0; page < reconciliationPages; page++ {
		txs, err := a.transactionsAt(ctx, network, address, position)
		if err != nil {
			return nil, false, err
		}
		next, complete, err := linkedTransactions(txs, position)
		if err != nil {
			return nil, false, err
		}
		for _, tx := range txs {
			if match(tx) {
				return tx, complete, nil
			}
		}
		if next == nil {
			return nil, complete, nil
		}
		position = next
	}
	return nil, false, nil
}
