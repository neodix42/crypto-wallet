package near

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const indexedHistoryLimit = 25

var errIndexedHistoryUnavailable = errors.New("NEAR indexed token history unavailable")

func indexedHistoryError(err error) error {
	// Malformed JSON/schema is not an availability fallback. HTTP rejection and
	// transport failure supply no usable candidate data and may leave a partial
	// native/direct-call observation available through HistoryPage.
	if errors.Is(err, chainutil.ErrInvalid) {
		return err
	}
	return fmt.Errorf("%w: %v", errIndexedHistoryUnavailable, err)
}

// NearBlocks v3 schema and route reviewed at nearblocks/nearblocks commit
// 7cbb64eedadee0011ea3cb7a90040003f8bb1c1d, packages/nb-schemas/src/accounts/
// fts/response.ts and apps/api/src/routes/v3/accounts/fts.ts. Indexer metadata,
// balances, amounts, status and URLs never establish a transfer or token policy.
// Only transaction/receipt candidates are used; final RPC evidence below is
// independently interpreted. This snapshot deliberately does not claim archive
// completeness or turn an unavailable/lagging indexer into empty token history.
func (a *Adapter) indexedTokenHistory(ctx context.Context, ac model.Account, scanned map[string]bool) ([]model.Receipt, error) {
	endpoint := a.historyEndpoints[ac.Network]
	if chainutil.Endpoint(endpoint) != nil {
		return nil, errors.New("NEAR indexed token history unavailable")
	}
	var page struct {
		Data *[]struct {
			Hash     string `json:"transaction_hash"`
			Receipt  string `json:"receipt_id"`
			Contract string `json:"contract_account_id"`
			Affected string `json:"affected_account_id"`
		} `json:"data"`
	}
	if err := a.client.Do(ctx, "GET", endpoint+"/v3/accounts/"+url.PathEscape(ac.Address)+"/ft-txns?limit="+strconv.Itoa(indexedHistoryLimit), "", nil, &page); err != nil {
		return nil, indexedHistoryError(err)
	}
	if page.Data == nil || len(*page.Data) > indexedHistoryLimit {
		return nil, errInvalid
	}
	out := []model.Receipt{}
	// A single transaction may have multiple indexed events. Verify each claimed
	// receipt, but fetch/interpret that transaction once, including every approved
	// token effect. Stable receipt/log/data identities preserve same-tx transfers.
	cached := map[string][]model.Receipt{}
	candidates := map[string]bool{}
	for _, candidate := range *page.Data {
		if candidate.Affected != ac.Address || a.ValidateAddress(ac.Network, candidate.Contract) != nil || !blockHash(candidate.Receipt) {
			return nil, errInvalid
		}
		if _, ok := a.token(ac.Network, ac.Network+":nep141:"+candidate.Contract); !ok {
			continue
		}
		if !blockHash(candidate.Hash) {
			return nil, errors.New("NEAR indexer transaction binding is missing or delayed; history incomplete")
		}
		key := candidate.Hash + ":" + candidate.Receipt + ":" + candidate.Contract
		if candidates[key] {
			continue
		} // one account event per debit/credit; same receipt may contain several
		candidates[key] = true
		rows, ok := cached[candidate.Hash]
		if !ok {
			var detail struct {
				Data *struct {
					Hash   string `json:"transaction_hash"`
					Signer string `json:"signer_account_id"`
				} `json:"data"`
			}
			if err := a.client.Do(ctx, "GET", endpoint+"/v3/txns/"+url.PathEscape(candidate.Hash), "", nil, &detail); err != nil {
				return nil, indexedHistoryError(err)
			}
			if detail.Data == nil || detail.Data.Hash != candidate.Hash || a.ValidateAddress(ac.Network, detail.Data.Signer) != nil {
				return nil, errInvalid
			}
			var err error
			rows, err = a.tokenHistoryTransaction(ctx, ac, candidate.Hash, detail.Data.Signer, "", "")
			if err != nil {
				return nil, err
			}
			cached[candidate.Hash] = rows
		}
		matched := false
		for _, r := range rows {
			for _, effect := range r.Transfers {
				if effect.Asset.Contract == candidate.Contract && strings.HasPrefix(effect.ID, "nep141:"+candidate.Receipt+":") {
					matched = true
				}
			}
		}
		if !matched {
			return nil, errors.New("NEAR indexed token event has no exact successful owned RPC effect")
		}
	}
	for hash, rows := range cached {
		if !scanned[hash] {
			out = append(out, rows...)
			scanned[hash] = true
		}
	}
	return out, nil
}

func (a *Adapter) historyTime(ctx context.Context, network, hash string) (int64, error) {
	if !blockHash(hash) {
		return 0, errInvalid
	}
	var b historyBlock
	if err := a.rpc(ctx, network, "block", map[string]string{"block_id": hash}, &b); err != nil {
		return 0, err
	}
	if b.Header.Hash != hash {
		return 0, errInvalid
	}
	if _, err := chainutil.Uint(b.Header.Height.String(), 64); err != nil {
		return 0, err
	}
	nanos, err := chainutil.Uint(b.Header.Timestamp.String(), 64)
	if err != nil || nanos/1_000_000_000 > uint64(time.Now().Unix()+300) {
		return 0, errInvalid
	}
	return int64(nanos / 1_000_000_000), nil
}

// No Plan/signing capability is created here. The shared receipt-graph checker
// verifies every reported descendant and the token executor; history remains
// OBSERVED rather than changing an encrypted journal reservation or balance.
func (a *Adapter) tokenHistoryTransaction(ctx context.Context, ac model.Account, hash, signer, requiredReceipt, requiredContract string) ([]model.Receipt, error) {
	if !blockHash(hash) || a.ValidateAddress(ac.Network, signer) != nil {
		return nil, errInvalid
	}
	var execution execution
	if err := a.rpc(ctx, ac.Network, "tx", map[string]string{"tx_hash": hash, "sender_account_id": signer, "wait_until": "FINAL"}, &execution); err != nil {
		return nil, err
	}
	if execution.Transaction.Signer != signer || a.ValidateAddress(ac.Network, execution.Transaction.Receiver) != nil {
		return nil, errInvalid
	}
	base, err := receipt(hash, execution)
	if err != nil {
		return nil, err
	}
	if base.State == "FAILED" {
		if requiredReceipt != "" {
			return nil, errInvalid
		}
		return []model.Receipt{}, nil
	}
	if base.State != "CONFIRMED" {
		return nil, errors.New("NEAR token history execution is not final")
	}
	transfers := []model.HistoryTransfer{}
	logs := 0
	verified := map[string]bool{}
	for _, r := range execution.Receipts {
		policy, tracked := a.token(ac.Network, ac.Network+":nep141:"+r.Outcome.Executor)
		if len(r.Outcome.Logs) > 64 {
			return nil, errInvalid
		}
		for li, line := range r.Outcome.Logs {
			logs++
			if logs > 256 || len(line) > 16384 {
				return nil, errInvalid
			}
			if !tracked || !strings.HasPrefix(line, "EVENT_JSON:") {
				continue
			}
			var event struct {
				Standard string            `json:"standard"`
				Version  string            `json:"version"`
				Event    string            `json:"event"`
				Data     []json.RawMessage `json:"data"`
			}
			if chainutil.DecodeStrict([]byte(strings.TrimSpace(strings.TrimPrefix(line, "EVENT_JSON:"))), &event) != nil {
				return nil, errInvalid
			}
			if event.Standard != "nep141" || event.Version != "1.0.0" || event.Event != "ft_transfer" && event.Event != "ft_mint" && event.Event != "ft_burn" {
				continue
			}
			if len(event.Data) == 0 || len(event.Data) > 128 {
				return nil, errInvalid
			}
			if !verified[policy.Asset.Contract] {
				checked, err := stakingReceipt(model.Plan{Account: ac, Recipient: policy.Asset.Contract}, hash, execution)
				if err != nil || checked.State != "CONFIRMED" {
					return nil, errors.New("NEAR token history receipt graph is incomplete or failed")
				}
				verified[policy.Asset.Contract] = true
			}
			for di, raw := range event.Data {
				var source, destination, atomic, label string
				var memo *string
				if event.Event == "ft_transfer" {
					var effect struct {
						Old    string  `json:"old_owner_id"`
						New    string  `json:"new_owner_id"`
						Amount string  `json:"amount"`
						Memo   *string `json:"memo,omitempty"`
					}
					if chainutil.DecodeStrict(raw, &effect) != nil || a.ValidateAddress(ac.Network, effect.Old) != nil || a.ValidateAddress(ac.Network, effect.New) != nil {
						return nil, errInvalid
					}
					source, destination, atomic, memo = effect.Old, effect.New, effect.Amount, effect.Memo
				} else {
					var effect struct {
						Owner  string  `json:"owner_id"`
						Amount string  `json:"amount"`
						Memo   *string `json:"memo,omitempty"`
					}
					if chainutil.DecodeStrict(raw, &effect) != nil || a.ValidateAddress(ac.Network, effect.Owner) != nil {
						return nil, errInvalid
					}
					atomic, memo = effect.Amount, effect.Memo
					if event.Event == "ft_mint" {
						destination, label = effect.Owner, "Mint"
					} else {
						source, label = effect.Owner, "Burn"
					}
				}
				if _, err := chainutil.Big(atomic, 128); err != nil {
					return nil, err
				}
				comment := ""
				if memo != nil {
					comment = *memo
				}
				if label != "" {
					if comment == "" {
						comment = label
					} else {
						comment = label + ": " + comment
					}
				}
				if len(comment) > 4096 || !utf8.ValidString(comment) || strings.ContainsRune(comment, 0) {
					return nil, errInvalid
				}
				if source != ac.Address && destination != ac.Address {
					continue
				}
				if len(transfers) >= 1000 {
					return nil, errors.New("NEAR token history effect count exceeds bound")
				}
				transfers = append(transfers, model.HistoryTransfer{ID: fmt.Sprintf("nep141:%s:%d:%d", r.ID, li, di), Source: source, Destination: destination, Asset: policy.Asset, Atomic: atomic, Comment: comment})
			}
		}
	}
	if len(transfers) == 0 {
		if requiredReceipt != "" {
			return nil, errors.New("NEAR required token receipt has no exact owned effect")
		}
		return []model.Receipt{}, nil
	}
	if requiredReceipt != "" {
		matched := false
		for _, effect := range transfers {
			if effect.Asset.Contract == requiredContract && strings.HasPrefix(effect.ID, "nep141:"+requiredReceipt+":") {
				matched = true
			}
		}
		if !matched {
			return nil, errInvalid
		}
	}
	stamp, err := a.historyTime(ctx, ac.Network, base.Block)
	if err != nil {
		return nil, err
	}
	base.State = "OBSERVED"
	base.Detail = "Provider-observed standard NEP-141 transfer/mint/burn events; final RPC receipt graph checked. This observation does not change local payment-journal status or prove indexer completeness."
	base.Timestamp = stamp
	base.Explorer = chainutil.TransactionExplorer(ac.Network, hash)
	base.Transfers = transfers
	return []model.Receipt{base}, nil
}
