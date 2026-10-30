package walletcore

import (
	"errors"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

// The caller selects an owned account only. Its immutable profile determines
// the contract, StateInit, wallet ID, fee policy and zero-outgoing actions.
func (c *core) tonDeploymentAt(message ipc.Message, epoch uint64) (any, error) {
	if message.Method != "tonWalletState" && message.Method != "prepareTonDeployment" {
		return nil, errors.New("unsupported TON deployment operation")
	}
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" || len(request.AccountID) > 512 {
		return nil, errors.New("TON deployment requires an owned account")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	deployer, ok := adapter.(model.TONDeployer)
	if !ok || account.Family != "ton" {
		return nil, errors.New("contract deployment is available only for supported TON wallets")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	if message.Method == "tonWalletState" {
		state, err := deployer.WalletState(ctx, account)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		valid := c.active && c.epoch == epoch && ctx.Err() == nil
		c.mu.Unlock()
		if !valid {
			return nil, errors.New("wallet session changed during TON contract observation")
		}
		return state, nil
	}
	plan, err := deployer.PrepareDeployment(ctx, account)
	if err != nil {
		return nil, err
	}
	// TON signs a canonical digest as its Plan.ID. Keep it unchanged; the
	// signer independently validates that digest and records it before consent.
	if err := c.childCallAt(epoch, "prepare", plan, nil); err != nil {
		return nil, err
	}
	return plan, nil
}
