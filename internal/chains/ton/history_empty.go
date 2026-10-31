package ton

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"

	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/chains/chainutil"
)

const noNativeActivityScope = " The provider currently reports this native account as uninitialized with zero balance and no recorded last transaction; no native activity was observed for this address. This trusted-provider observation is not independent proof that history never existed."

// An empty unanchored response alone cannot distinguish an unused account from
// an omitted history head. Consult fresh state only for this ambiguous case;
// healthy nonempty archival pages do not acquire a new state dependency.
func (a *Adapter) historyTransactionsAt(ctx context.Context, network, account string, position *transactionPosition) ([]*tlb.Transaction, bool, error) {
	txs, err := a.transactionsAt(ctx, network, account, position)
	if err != nil || len(txs) != 0 || position != nil {
		return txs, false, err
	}
	state, err := a.state(ctx, network, account)
	if err != nil {
		return nil, false, err
	}
	lt, err := chainutil.Uint(state.Last.LT, 64)
	hash, hashErr := base64.StdEncoding.DecodeString(state.Last.Hash)
	if err != nil || hashErr != nil || len(hash) != 32 || base64.StdEncoding.EncodeToString(hash) != state.Last.Hash {
		return nil, false, errors.New("TON empty history has no valid account-state transaction identity")
	}
	zeroHash := bytes.Equal(hash, make([]byte, 32))
	if lt == 0 {
		if !zeroHash || state.State != "uninitialized" || state.Balance != "0" {
			return nil, false, errors.New("TON empty history contradicts account state")
		}
		return nil, true, nil
	}
	if zeroHash {
		return nil, false, errors.New("TON account-state history head has a zero hash")
	}
	// A public provider may omit the default latest range. Request the actual
	// state-recorded head explicitly, never a guessed LT or metadata-only row.
	head := &transactionPosition{LT: state.Last.LT, Hash: hex.EncodeToString(hash)}
	txs, err = a.transactionsAt(ctx, network, account, head)
	if err != nil {
		return nil, false, err
	}
	if _, _, err = linkedTransactions(txs, head); err != nil {
		return nil, false, err
	}
	return txs, false, nil
}
