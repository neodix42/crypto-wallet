package near

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const historyBlocks = 12

func (*Adapter) HistoryScope() string {
	return "Address-indexed historical native NEAR transactions, contract Transfer receipts and tracked NEP-141 transfer/mint/burn candidates are paged through Nearblocks.io APIs and independently interpreted against FINAL RPC transactions, receipt graphs, exact actions and approved token identities. Load older chain transactions continues each bounded index. Token precision/runtime is pinned at the frozen current head; historical contract upgrades or precision changes are not independently certified. System refunds, unknown actions, untracked tokens and provider omissions are outside this observation; index exhaustion does not prove archive completeness or no funds. Without an indexer, the fallback covers the last 12 final blocks only."
}

type historyBlock struct {
	Header struct {
		Hash      string      `json:"hash"`
		Previous  string      `json:"prev_hash"`
		Height    json.Number `json:"height"`
		Timestamp json.Number `json:"timestamp_nanosec"`
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
	return a.recentHistory(ctx, ac, true)
}

func (a *Adapter) recentHistory(ctx context.Context, ac model.Account, includeIndexed bool, canonical ...historicalPosition) ([]model.Receipt, error) {
	var state accountState
	var e error
	if len(canonical) != 0 {
		if e = a.account(ac); e != nil {
			return nil, e
		}
		state.Hash, state.Height = canonical[0].Head, json.Number(canonical[0].Height)
	} else {
		state, _, e = a.state(ctx, ac)
		if e != nil {
			return nil, e
		}
	}
	next := state.Hash
	lastHeight, e := chainutil.Uint(state.Height.String(), 64)
	if e != nil {
		return nil, e
	}
	out := []model.Receipt{}
	tokenSeen := map[string]bool{}
	policies := a.networkTokens(ac.Network)
	for _, policy := range policies {
		if err := a.verifyTokenAt(ctx, policy, tokenBlock{Hash: state.Hash, Height: state.Height.String()}); err != nil {
			return nil, err
		}
	}
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
		var stamp int64
		if b.Header.Timestamp != "" {
			nanos, err := chainutil.Uint(b.Header.Timestamp.String(), 64)
			if err != nil || nanos/1_000_000_000 > uint64(time.Now().Unix()+300) {
				return nil, errInvalid
			}
			stamp = int64(nanos / 1_000_000_000)
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
				if len(canonical) != 0 {
					_, tracked := a.token(ac.Network, ac.Network+":nep141:"+tx.Receiver)
					if tx.Signer != ac.Address && tx.Receiver != ac.Address && !tracked {
						continue
					}
					if seen[tx.Hash] || !blockHash(tx.Hash) {
						return nil, errInvalid
					}
					seen[tx.Hash] = true
					position := canonical[0]
					position.Phase = "txns"
					observed, err := a.historicalTransaction(ctx, ac, tx.Hash, tx.Signer, historicalCandidate{Hash: tx.Hash, Signer: tx.Signer, Receiver: tx.Receiver}, position)
					if err != nil {
						if errors.Is(err, errHistoricalUnavailable) && ctx.Err() == nil {
							continue
						}
						return nil, err
					}
					if len(observed.Row.Transfers) != 0 {
						if len(out) >= 4096 {
							return nil, errors.New("NEAR history payment count exceeds bound; narrower scan required")
						}
						out = append(out, observed.Row)
					}
					continue
				}
				if len(policies) > 0 {
					for _, policy := range policies {
						if tx.Receiver == policy.Asset.Contract && !tokenSeen[tx.Hash] {
							tokenSeen[tx.Hash] = true
							rows, err := a.tokenHistoryTransaction(ctx, ac, tx.Hash, tx.Signer, "", "")
							if err != nil {
								return nil, err
							}
							if len(rows) > 4096-len(out) {
								return nil, fmt.Errorf("NEAR history payment count exceeds bound; narrower scan required")
							}
							out = append(out, rows...)
							break
						}
					}
				}
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
				r.Timestamp = stamp
				r.Explorer = chainutil.TransactionExplorer(ac.Network, tx.Hash)
				r.Transfers = []model.HistoryTransfer{{ID: "native", Source: tx.Signer, Destination: tx.Receiver, Asset: model.Asset{ID: asset(ac.Network), Symbol: "NEAR", Decimals: 24}, Atomic: action.Transfer.Deposit}}
				r.Detail = fmt.Sprintf("Native NEAR direct transfer: %s -> %s; %s yoctoNEAR; %s", tx.Signer, tx.Receiver, action.Transfer.Deposit, r.State)
				if len(out) >= 4096 {
					return nil, fmt.Errorf("NEAR history payment count exceeds bound; narrower scan required")
				}
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
	var indexedErr error
	if includeIndexed && len(policies) > 0 && a.historyEndpoints[ac.Network] != "" {
		rows, err := a.indexedTokenHistory(ctx, ac, tokenSeen)
		if err != nil && !errors.Is(err, errIndexedHistoryUnavailable) {
			return nil, err
		}
		indexedErr = err
		out = append(out, rows...)
	}
	merged, err := chainutil.MergeHistory(out)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Timestamp > merged[j].Timestamp })
	return merged, indexedErr
}
