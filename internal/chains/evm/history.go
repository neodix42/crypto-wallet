package evm

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"math/big"
	"strings"
)

func (*Adapter) HistoryScope() string {
	return "Bounded RPC scan of the latest 128 blocks and registered ZEN Transfer events. Earlier, internal contract, and bridge history requires an independent archival explorer or wider explicit rescan; an empty scan is not proof of no historical funds."
}

type historyTx struct {
	Hash  string  `json:"hash"`
	From  string  `json:"from"`
	To    *string `json:"to"`
	Value string  `json:"value"`
	Input string  `json:"input"`
}
type historyBlock struct {
	Number       string      `json:"number"`
	Hash         string      `json:"hash"`
	Transactions []historyTx `json:"transactions"`
}

func hashValid(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, e := hex.DecodeString(s[2:])
	return e == nil
}

// History returns observed transfers in a fixed, explicitly limited window. It
// neither discovers arbitrary assets nor treats absence as complete recovery.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	n, e := a.validateAccount(ac)
	if e != nil {
		return nil, e
	}
	head, e := a.current(ctx, n)
	if e != nil {
		return nil, e
	}
	height, _ := hexInteger(head.Number)
	end := height.Uint64()
	start := uint64(0)
	if end >= 127 {
		start = end - 127
	}
	scope := fmt.Sprintf("bounded RPC scan blocks %d..%d", start, end)
	var final *block
	finalHeight := new(big.Int).SetInt64(-1)
	if e = a.rpc(ctx, n, "eth_getBlockByNumber", []any{"finalized", false}, &final); e == nil && final != nil {
		v, e := hexInteger(final.Number)
		if e != nil {
			return nil, e
		}
		finalHeight = v
	}
	out := []model.Receipt{}
	seen := map[string]bool{}
	observedEvents := map[string]string{}
	hashes := map[string]string{}
	confirm := func(id, blockHash, number, detail string, mustSucceed bool) (model.Receipt, error) {
		r := model.Receipt{Identity: id, Block: blockHash, State: "SUBMITTED", Detail: scope + "; " + detail}
		if !hashValid(id) || !hashValid(blockHash) {
			return r, errors.New("invalid history identity")
		}
		bn, e := hexInteger(number)
		if e != nil || !bn.IsUint64() || bn.Uint64() < start || bn.Uint64() > end {
			return r, errors.New("history block outside requested range")
		}
		if canonical, ok := hashes[number]; !ok || canonical != blockHash {
			return r, errors.New("history inclusion disagrees with canonical block")
		}
		var receipt *struct {
			TransactionHash string `json:"transactionHash"`
			BlockHash       string `json:"blockHash"`
			BlockNumber     string `json:"blockNumber"`
			Status          string `json:"status"`
		}
		if e = a.rpc(ctx, n, "eth_getTransactionReceipt", []any{id}, &receipt); e != nil {
			return r, e
		}
		if receipt == nil || receipt.TransactionHash != id || receipt.BlockHash != blockHash || receipt.BlockNumber != number {
			return r, errors.New("history receipt binding failed")
		}
		switch receipt.Status {
		case "0x0":
			if mustSucceed {
				return r, errors.New("reverted receipt contradicted token transfer event")
			}
			if bn.Cmp(finalHeight) <= 0 {
				r.State = "FAILED"
				r.Detail += "; finalized execution reverted and gas was spent"
			} else {
				r.Detail += "; execution reverted and gas was spent; awaiting finality"
			}
		case "0x1":
			if bn.Cmp(finalHeight) <= 0 {
				r.State = "CONFIRMED"
			} else {
				r.Detail += "; awaiting finality"
			}
		default:
			return r, errors.New("invalid history execution status")
		}
		return r, nil
	}
	for number := start; ; number++ {
		tag := quantity(new(big.Int).SetUint64(number))
		var b *historyBlock
		if e = a.rpc(ctx, n, "eth_getBlockByNumber", []any{tag, true}, &b); e != nil {
			return nil, e
		}
		if b == nil || b.Number != tag || !hashValid(b.Hash) {
			return nil, errors.New("incomplete history block scan")
		}
		hashes[tag] = b.Hash
		for _, tx := range b.Transactions {
			if tx.To == nil {
				continue
			}
			outgoing := strings.EqualFold(tx.From, ac.Address)
			incoming := strings.EqualFold(*tx.To, ac.Address)
			if !outgoing && !incoming {
				continue
			}
			if e = a.ValidateAddress(n.ID, tx.From); e != nil {
				return nil, e
			}
			if e = a.ValidateAddress(n.ID, *tx.To); e != nil {
				return nil, e
			}
			value, e := hexInteger(tx.Value)
			if e != nil {
				return nil, e
			}
			if value.Sign() == 0 {
				continue
			}
			if seen[tx.Hash+":native"] {
				return nil, errors.New("duplicate native transaction identity in history")
			}
			direction := "received"
			peer := tx.From
			if outgoing {
				direction = "sent"
				peer = *tx.To
			}
			r, e := confirm(tx.Hash, b.Hash, b.Number, fmt.Sprintf("%s %s atomic %s; peer %s", direction, value.String(), n.Assets[0].ID, peer), false)
			if e != nil {
				return nil, e
			}
			if len(out) >= 4096 {
				return nil, errors.New("history payment count limit; narrower explicit scan required")
			}
			out = append(out, r)
			seen[tx.Hash+":native"] = true
		}
		if number == end {
			break
		}
	}
	for _, token := range n.Assets {
		if token.Contract == "" {
			continue
		}
		if e = a.tokenIdentity(ctx, n, token, head.Number); e != nil {
			return nil, e
		}
		event := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
		owner := common.BytesToHash(common.HexToAddress(ac.Address).Bytes()).Hex()
		for _, topics := range []any{[]any{event, owner}, []any{event, nil, owner}} {
			var logs []struct {
				Address         string   `json:"address"`
				Topics          []string `json:"topics"`
				Data            string   `json:"data"`
				TransactionHash string   `json:"transactionHash"`
				BlockHash       string   `json:"blockHash"`
				BlockNumber     string   `json:"blockNumber"`
				LogIndex        string   `json:"logIndex"`
				Removed         bool     `json:"removed"`
			}
			filter := map[string]any{"address": token.Contract, "fromBlock": quantity(new(big.Int).SetUint64(start)), "toBlock": head.Number, "topics": topics}
			if e = a.rpc(ctx, n, "eth_getLogs", []any{filter}, &logs); e != nil {
				return nil, e
			}
			if len(logs) > 10000 {
				return nil, errors.New("token history response limit")
			}
			for _, log := range logs {
				if log.Removed {
					continue
				}
				if !strings.EqualFold(log.Address, token.Contract) || len(log.Topics) != 3 || log.Topics[0] != event || (log.Topics[1] != owner && log.Topics[2] != owner) {
					return nil, errors.New("token history filter mismatch")
				}
				if _, e = hexInteger(log.LogIndex); e != nil {
					return nil, e
				}
				id := log.TransactionHash + ":" + log.LogIndex
				value, e := abiInteger(log.Data)
				if e != nil {
					return nil, e
				}
				if value.Sign() == 0 {
					continue
				}
				from, e := historyAddress(log.Topics[1])
				if e != nil {
					return nil, e
				}
				to, e := historyAddress(log.Topics[2])
				if e != nil {
					return nil, e
				}
				fingerprint := token.ID + "|" + log.BlockHash + "|" + log.BlockNumber + "|" + log.Data + "|" + log.Topics[1] + "|" + log.Topics[2]
				if previous, ok := observedEvents[id]; ok {
					if previous != fingerprint {
						return nil, errors.New("conflicting duplicate token event")
					}
					continue
				}
				direction := "received"
				peer := from
				if log.Topics[1] == owner {
					direction = "sent"
					peer = to
				}
				r, e := confirm(log.TransactionHash, log.BlockHash, log.BlockNumber, fmt.Sprintf("%s %s atomic %s; peer %s", direction, value.String(), token.ID, peer), true)
				if e != nil {
					return nil, e
				}
				if len(out) >= 4096 {
					return nil, errors.New("history payment count limit; narrower explicit scan required")
				}
				out = append(out, r)
				seen[id] = true
				observedEvents[id] = fingerprint
			}
		}
	}
	return out, nil
}
func historyAddress(topic string) (string, error) {
	if !hashValid(topic) || topic[2:26] != strings.Repeat("0", 24) {
		return "", errors.New("malformed indexed transfer address")
	}
	return common.HexToAddress("0x" + topic[26:]).Hex(), nil
}
