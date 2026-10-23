package near

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/decred/base58"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const historyBlocks = 12

func (*Adapter) HistoryScope() string {
	return "Last 12 final blocks on the selected chain; direct transactions containing exactly one native NEAR Transfer action only. Contract-mediated receipts, multi-action transfers and older blocks remain unresolved; an empty result never proves no funds."
}

type historyBlock struct {
	Header struct {
		Hash     string      `json:"hash"`
		Previous string      `json:"prev_hash"`
		Height   json.Number `json:"height"`
	} `json:"header"`
	Chunks []struct {
		Hash   string      `json:"chunk_hash"`
		Height json.Number `json:"height_included"`
	} `json:"chunks"`
}
type historyChunk struct {
	Header struct {
		Hash string `json:"chunk_hash"`
	} `json:"header"`
	Transactions []struct {
		Hash     string            `json:"hash"`
		Signer   string            `json:"signer_id"`
		Receiver string            `json:"receiver_id"`
		Actions  []json.RawMessage `json:"actions"`
	} `json:"transactions"`
}

func blockHash(s string) bool {
	return len(s) <= 44 && len(base58.Decode(s)) == 32 && base58.Encode(base58.Decode(s)) == s
}

// History follows final block parents and new chunks only, so reused chunks do
// not duplicate payments. Execution is queried by the original native identity.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	state, _, e := a.state(ctx, ac)
	if e != nil {
		return nil, e
	}
	next := state.Hash
	lastHeight, e := chainutil.Uint(state.Height.String(), 64)
	if e != nil {
		return nil, e
	}
	out := []model.Receipt{}
	seen := map[string]bool{}
	for i := 0; i < historyBlocks; i++ {
		var b historyBlock
		if e = a.rpc(ctx, ac.Network, "block", map[string]string{"block_id": next}, &b); e != nil {
			return nil, e
		}
		height, e := chainutil.Uint(b.Header.Height.String(), 64)
		if e != nil || (i == 0 && height != lastHeight) || height > lastHeight || (i > 0 && height == lastHeight) || b.Header.Hash != next || !blockHash(b.Header.Hash) || len(b.Chunks) > 32 {
			return nil, errInvalid
		}
		for _, c := range b.Chunks {
			included, e := chainutil.Uint(c.Height.String(), 64)
			if e != nil || included > height || !blockHash(c.Hash) {
				return nil, errInvalid
			}
			if included != height {
				continue
			}
			var chunk historyChunk
			if e = a.rpc(ctx, ac.Network, "chunk", map[string]string{"chunk_id": c.Hash}, &chunk); e != nil {
				return nil, e
			}
			if chunk.Header.Hash != c.Hash || len(chunk.Transactions) > 512 {
				return nil, errInvalid
			}
			for _, tx := range chunk.Transactions {
				if tx.Signer != ac.Address && tx.Receiver != ac.Address {
					continue
				}
				if !blockHash(tx.Hash) || a.ValidateAddress(ac.Network, tx.Signer) != nil || a.ValidateAddress(ac.Network, tx.Receiver) != nil {
					return nil, errInvalid
				}
				if len(tx.Actions) != 1 {
					continue
				}
				var action struct {
					Transfer *struct {
						Deposit string `json:"deposit"`
					} `json:"Transfer"`
				} // Other native actions are intentionally outside this direct-transfer scan.
				var tags map[string]json.RawMessage
				if e = json.Unmarshal(tx.Actions[0], &tags); e != nil || len(tags) != 1 {
					return nil, errInvalid
				}
				if _, ok := tags["Transfer"]; !ok {
					continue
				}
				if e = chainutil.DecodeStrict(tx.Actions[0], &action); e != nil || action.Transfer == nil {
					return nil, errInvalid
				}
				if _, e = chainutil.Big(action.Transfer.Deposit, 128); e != nil {
					return nil, e
				}
				if seen[tx.Hash] {
					return nil, errInvalid
				}
				seen[tx.Hash] = true
				var execution execution
				if e = a.rpc(ctx, ac.Network, "tx", map[string]string{"tx_hash": tx.Hash, "sender_account_id": tx.Signer, "wait_until": "FINAL"}, &execution); e != nil {
					return nil, e
				}
				if execution.Transaction.Signer != tx.Signer || execution.Transaction.Receiver != tx.Receiver {
					return nil, errInvalid
				}
				r, e := receipt(tx.Hash, execution)
				if e != nil {
					return nil, e
				}
				r.Detail = fmt.Sprintf("Native NEAR direct transfer: %s -> %s; %s yoctoNEAR; %s", tx.Signer, tx.Receiver, action.Transfer.Deposit, r.State)
				out = append(out, r)
			}
		}
		if height == 0 {
			break
		}
		if !blockHash(b.Header.Previous) {
			return nil, errInvalid
		}
		next = b.Header.Previous
		lastHeight = height
	}
	return out, nil
}
