package aptos

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const historyVersions uint64 = 256

func (*Adapter) HistoryScope() string {
	return "Last 256 committed ledger versions; recognized direct native APT transfer entry functions only. Contract-mediated transfers and older ledger versions remain unresolved; an empty result never proves no funds."
}

type historyTransaction struct {
	execution
	Payload struct {
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

// History scans a fixed, explicit public ledger window. It does not infer a full
// account history from the node's sender-only account-transactions endpoint.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
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
			if tx.Type != "user_transaction" {
				continue
			}
			if tx.Success == nil {
				return nil, errInvalid
			}
			if tx.Payload.Type != "entry_function_payload" {
				continue
			}
			recognized, e := nativeFunction(tx.Payload.Function, tx.Payload.Types)
			if e != nil {
				return nil, e
			}
			if !recognized {
				continue
			}
			if len(tx.Payload.Args) != 2 {
				return nil, errInvalid
			}
			var destination, amount string
			if json.Unmarshal(tx.Payload.Args[0], &destination) != nil || json.Unmarshal(tx.Payload.Args[1], &amount) != nil {
				return nil, errInvalid
			}
			to, e := address(destination)
			if e != nil {
				return nil, e
			}
			from, e := address(tx.Sender)
			if e != nil {
				return nil, e
			}
			if _, e = chainutil.Uint(amount, 64); e != nil {
				return nil, e
			}
			if _, e = chainutil.Uint(tx.Sequence, 64); e != nil {
				return nil, e
			}
			if from.StringLong() != ac.Address && to.StringLong() != ac.Address {
				continue
			}
			hash, e := hex.DecodeString(strings.TrimPrefix(tx.Hash, "0x"))
			if e != nil || len(hash) != 32 || tx.Hash != "0x"+hex.EncodeToString(hash) {
				return nil, errInvalid
			}
			state := "FAILED"
			if *tx.Success {
				state = "CONFIRMED"
			}
			out = append(out, model.Receipt{Identity: tx.Hash, State: state, Block: tx.Version, Detail: fmt.Sprintf("Native APT direct transfer: %s -> %s; %s octas; %s", from.StringLong(), to.StringLong(), amount, state)})
		}
		if start > ^uint64(0)-count {
			break
		}
		start += count
	}
	return out, nil
}
