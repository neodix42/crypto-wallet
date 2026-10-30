package aptos

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const historyVersions uint64 = 256

func (*Adapter) HistoryScope() string {
	return "Account-indexed native APT history on mainnet/testnet, with descending older-page continuation and exact committed transaction checks through the selected REST node; HTTP 410 pruning uses a separately chain-checked fixed official Aptos Labs archive. Recognized direct native transfer entry functions and committed standard framework APT coin/primary-fungible-store deposit/withdraw events are interpreted. An event establishes its owned side only; unknown counterparties are not guessed. Nonprimary stores, untyped legacy Coin events and other token standards remain unresolved. A local/loopback node instead scans its last 256 ledger versions without contacting a public indexer. Indexer availability, retention and omissions do not establish complete history; an empty result never proves no funds."
}

type historyTransaction struct {
	execution
	Timestamp string         `json:"timestamp"`
	Events    []historyEvent `json:"events"`
	Payload   struct {
		Type     string            `json:"type"`
		Function string            `json:"function"`
		Types    []string          `json:"type_arguments"`
		Args     []json.RawMessage `json:"arguments"`
	} `json:"payload"`
}

func nativeFunction(function string, types []string) (bool, error) {
	parts := strings.Split(function, "::")
	if len(parts) != 3 {
		return false, nil
	}
	module, e := address(parts[0])
	if e != nil {
		return false, errInvalid
	}
	one, _ := address("0x1")
	if module != one {
		return false, nil
	}
	switch parts[1] + "::" + parts[2] {
	case "aptos_account::transfer":
		if len(types) != 0 {
			return false, errInvalid
		}
		return true, nil
	case "aptos_account::transfer_coins", "coin::transfer":
		if len(types) != 1 {
			return false, errInvalid
		}
		return types[0] == "0x1::aptos_coin::AptosCoin", nil
	}
	return false, nil
}

// localHistory keeps the explicit bounded fallback for owned local nodes.
// Production account history uses indexedHistoryPage rather than a global tip window.
func (a *Adapter) localHistory(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	if e := a.account(ac); e != nil {
		return nil, e
	}
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		return nil, e
	}
	if _, e = a.authority(ctx, ac, l.Version); e != nil {
		return nil, e
	}
	end, e := chainutil.Uint(l.Version, 64)
	if e != nil {
		return nil, e
	}
	start := uint64(1)
	if end >= historyVersions {
		start = end - historyVersions + 1
	}
	out := []model.Receipt{}
	for start <= end {
		count := uint64(32)
		if end-start+1 < count {
			count = end - start + 1
		}
		var rows []historyTransaction
		if e = a.request(ctx, ac.Network, "GET", "/transactions?start="+strconv.FormatUint(start, 10)+"&limit="+strconv.FormatUint(count, 10), nil, &rows); e != nil {
			return nil, e
		}
		if uint64(len(rows)) != count {
			return nil, errInvalid
		}
		for i, tx := range rows {
			v, e := chainutil.Uint(tx.Version, 64)
			if e != nil || v != start+uint64(i) {
				return nil, errInvalid
			}
			row, relevant, e := nativeHistoryTransaction(ac, tx)
			if e != nil {
				return nil, e
			}
			if relevant {
				out = append(out, row)
			}
		}
		if start > ^uint64(0)-count {
			break
		}
		start += count
	}
	return out, nil
}

// nativeHistoryTransaction derives display fields from a committed REST
// transaction, never from an indexer's amount, status, timestamp or URL.
func nativeHistoryTransaction(ac model.Account, tx historyTransaction) (model.Receipt, bool, error) {
	if tx.Type != "user_transaction" {
		return model.Receipt{}, false, nil
	}
	if tx.Success == nil {
		return model.Receipt{}, false, errInvalid
	}
	if tx.Payload.Type != "entry_function_payload" {
		return nativeEventHistoryReceipt(ac, tx)
	}
	recognized, e := nativeFunction(tx.Payload.Function, tx.Payload.Types)
	if e != nil {
		return model.Receipt{}, false, e
	}
	if !recognized {
		return nativeEventHistoryReceipt(ac, tx)
	}
	if len(tx.Payload.Args) != 2 {
		return model.Receipt{}, false, errInvalid
	}
	var destination, amount string
	if json.Unmarshal(tx.Payload.Args[0], &destination) != nil || json.Unmarshal(tx.Payload.Args[1], &amount) != nil {
		return model.Receipt{}, false, errInvalid
	}
	to, e := address(destination)
	if e != nil {
		return model.Receipt{}, false, e
	}
	from, e := address(tx.Sender)
	if e != nil {
		return model.Receipt{}, false, e
	}
	if _, e = chainutil.Uint(amount, 64); e != nil {
		return model.Receipt{}, false, e
	}
	if _, e = chainutil.Uint(tx.Sequence, 64); e != nil {
		return model.Receipt{}, false, e
	}
	if from.StringLong() != ac.Address && to.StringLong() != ac.Address {
		return model.Receipt{}, false, nil
	}
	hash, e := hex.DecodeString(strings.TrimPrefix(tx.Hash, "0x"))
	if e != nil || len(hash) != 32 || tx.Hash != "0x"+hex.EncodeToString(hash) {
		return model.Receipt{}, false, errInvalid
	}
	state := "FAILED"
	if *tx.Success {
		state = "CONFIRMED"
	}
	var stamp int64
	if tx.Timestamp != "" {
		micros, err := chainutil.Uint(tx.Timestamp, 64)
		if err != nil || micros/1_000_000 > uint64(time.Now().Unix()+300) {
			return model.Receipt{}, false, errInvalid
		}
		stamp = int64(micros / 1_000_000)
	}
	return model.Receipt{Timestamp: stamp, Explorer: chainutil.TransactionExplorer(ac.Network, tx.Hash), Transfers: []model.HistoryTransfer{{ID: "native", Source: from.StringLong(), Destination: to.StringLong(), Asset: model.Asset{ID: asset(ac.Network), Symbol: "APT", Decimals: 8}, Atomic: amount}}, Identity: tx.Hash, State: state, Block: tx.Version, Detail: fmt.Sprintf("Native APT direct transfer: %s -> %s; %s octas; %s", from.StringLong(), to.StringLong(), amount, state)}, true, nil
}

func nativeEventHistoryReceipt(ac model.Account, tx historyTransaction) (model.Receipt, bool, error) {
	effects, err := nativeHistoryEvents(ac, tx)
	if err != nil || len(effects) == 0 {
		return model.Receipt{}, false, err
	}
	if _, err := address(tx.Sender); err != nil {
		return model.Receipt{}, false, errInvalid
	}
	if _, err := chainutil.Uint(tx.Sequence, 64); err != nil {
		return model.Receipt{}, false, errInvalid
	}
	hash, err := hex.DecodeString(strings.TrimPrefix(tx.Hash, "0x"))
	if err != nil || len(hash) != 32 || tx.Hash != "0x"+hex.EncodeToString(hash) {
		return model.Receipt{}, false, errInvalid
	}
	micros, err := chainutil.Uint(tx.Timestamp, 64)
	if err != nil || micros/1_000_000 > uint64(time.Now().Unix()+300) {
		return model.Receipt{}, false, errInvalid
	}
	return model.Receipt{Identity: tx.Hash, Block: tx.Version, Timestamp: int64(micros / 1_000_000), Explorer: chainutil.TransactionExplorer(ac.Network, tx.Hash), State: "CONFIRMED", Transfers: effects, Detail: "Committed native APT framework events; unknown counterparties remain undisclosed"}, true, nil
}
