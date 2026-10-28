package walletcore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/ipc"
	"localwallet/internal/strictjson"
)

// The public interface accepts a staking intent, never a method name, gas,
// attached deposit or arbitrary contract arguments. The NEAR adapter constructs
// and the signer independently validates the only supported function calls.
func (c *core) nearStakingAt(m ipc.Message, epoch uint64) (any, error) {
	var accountID, pool, action, value string
	if m.Method == "nearStakingState" {
		var request struct {
			AccountID strictjson.Text `json:"accountId"`
			Pool      strictjson.Text `json:"pool"`
		}
		if err := strictjson.Decode(m.Body, &request); err != nil {
			return nil, err
		}
		accountID, pool = string(request.AccountID), string(request.Pool)
	} else if m.Method == "prepareNearStaking" {
		var request struct {
			AccountID strictjson.Text `json:"accountId"`
			Pool      strictjson.Text `json:"pool"`
			Action    strictjson.Text `json:"action"`
			Amount    strictjson.Text `json:"amount"`
		}
		if err := strictjson.Decode(m.Body, &request); err != nil {
			return nil, err
		}
		accountID, pool, action, value = string(request.AccountID), string(request.Pool), string(request.Action), string(request.Amount)
	} else {
		return nil, errors.New("unsupported NEAR staking operation")
	}
	if accountID == "" || pool == "" {
		return nil, errors.New("NEAR account and staking pool are required")
	}
	account, err := c.account(accountID)
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	nearAdapter, ok := adapter.(*near.Adapter)
	if !ok || account.Family != "near" {
		return nil, errors.New("staking is available only for NEAR wallets")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	if m.Method == "nearStakingState" {
		return nearAdapter.StakingState(ctx, account, pool)
	}
	switch action {
	case "stake", "unstake":
		value, err = amount.Parse(value, 24)
		if err != nil || value == "0" {
			return nil, errors.New("enter a positive NEAR amount with at most 24 decimal places")
		}
	case "unstake_all", "withdraw_all":
		if value != "" {
			return nil, errors.New("all-balance staking operations do not accept an amount")
		}
	default:
		return nil, errors.New("unsupported NEAR staking action")
	}
	plan, err := nearAdapter.PrepareStaking(ctx, account, near.StakingRequest{AccountID: accountID, Validator: pool, Action: action, Amount: value})
	if err != nil {
		return nil, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return nil, err
	}
	plan.ID = hex.EncodeToString(id[:])
	if err = c.childCallAt(epoch, "prepare", plan, nil); err != nil {
		return nil, err
	}
	// send, retry and reconcile use the same durable signer-owned operation and
	// signed artifact as ordinary payments. Preparation itself never broadcasts.
	return plan, nil
}
