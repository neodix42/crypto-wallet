package aptos

import (
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

type historyEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// The framework fixes native APT metadata at 0xA. Primary stores use the Move
// object module's 0xFC derivation domain, not an authentication-key derivation.
func nativePrimaryStore(owner string) (string, error) {
	account, err := address(owner)
	if err != nil {
		return "", err
	}
	metadata, _ := address("0xa")
	b := append(append(append([]byte{}, account[:]...), metadata[:]...), 0xfc)
	h := sha3.Sum256(b)
	return "0x" + hex.EncodeToString(h[:]), nil
}

func frameworkEventType(value string) string {
	parts := strings.Split(value, "::")
	if len(parts) != 3 {
		return ""
	}
	module, err := address(parts[0])
	one, _ := address("0x1")
	if err != nil || module != one {
		return ""
	}
	return parts[1] + "::" + parts[2]
}

// Recognized committed framework events include contract-mediated native APT
// activity. Each event proves one owned side; unrelated events are never paired
// by equal amounts to invent a counterparty. Nonprimary stores and untyped
// legacy Coin DepositEvent/WithdrawEvent remain outside this interpretation.
func nativeHistoryEvents(ac model.Account, tx historyTransaction) ([]model.HistoryTransfer, error) {
	if len(tx.Events) > 1000 {
		return nil, errInvalid
	}
	if tx.Success == nil || !*tx.Success {
		return nil, nil
	}
	store, err := nativePrimaryStore(ac.Address)
	if err != nil {
		return nil, err
	}
	out := []model.HistoryTransfer{}
	for index, event := range tx.Events {
		kind := frameworkEventType(event.Type)
		deposit, withdraw, owner := false, false, ""
		var amount string
		switch kind {
		case "fungible_asset::Deposit", "fungible_asset::Withdraw":
			var data struct {
				Store  string `json:"store"`
				Amount string `json:"amount"`
			}
			if chainutil.DecodeStrict(event.Data, &data) != nil {
				return nil, errInvalid
			}
			observed, err := address(data.Store)
			if err != nil {
				return nil, errInvalid
			}
			if observed.StringLong() != store {
				continue
			}
			owner, amount = ac.Address, data.Amount
			deposit, withdraw = kind == "fungible_asset::Deposit", kind == "fungible_asset::Withdraw"
		case "coin::CoinDeposit", "coin::CoinWithdraw":
			var data struct {
				Coin    string `json:"coin_type"`
				Account string `json:"account"`
				Amount  string `json:"amount"`
			}
			if chainutil.DecodeStrict(event.Data, &data) != nil {
				return nil, errInvalid
			}
			if data.Coin != "0x1::aptos_coin::AptosCoin" {
				continue
			}
			observed, err := address(data.Account)
			if err != nil {
				return nil, errInvalid
			}
			if observed.StringLong() != ac.Address {
				continue
			}
			owner, amount = ac.Address, data.Amount
			deposit, withdraw = kind == "coin::CoinDeposit", kind == "coin::CoinWithdraw"
		default:
			continue
		}
		if _, err := chainutil.Uint(amount, 64); err != nil {
			return nil, err
		}
		effect := model.HistoryTransfer{ID: fmt.Sprintf("event:%d", index), Asset: model.Asset{ID: asset(ac.Network), Symbol: "APT", Decimals: 8}, Atomic: amount}
		if deposit {
			effect.Destination = owner
			effect.Comment = "Native APT deposit event; source is not established by this event"
		}
		if withdraw {
			effect.Source = owner
			effect.Comment = "Native APT withdrawal event; destination is not established by this event"
		}
		out = append(out, effect)
	}
	return out, nil
}
