package walletcore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"localwallet/internal/chains/near"
	"localwallet/internal/ipc"
	"localwallet/internal/strictjson"
)

// A caller selects an already approved token, never a method, attached deposit,
// registration beneficiary, arbitrary JSON or gas. The adapter authors the plan.
func (c *core) nearTokenStorageAt(message ipc.Message, epoch uint64) (any, error) {
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
		Asset     strictjson.Text `json:"asset"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" || request.Asset == "" {
		return nil, errors.New("NEAR token storage requires an account and approved asset")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	chain, ok := adapter.(*near.Adapter)
	if !ok || account.Family != "near" {
		return nil, errors.New("token storage registration requires a NEAR wallet")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	if message.Method == "nearTokenStorage" {
		return chain.TokenStorage(ctx, account, string(request.Asset))
	}
	if message.Method != "prepareNearTokenRegistration" {
		return nil, errors.New("unsupported NEAR token storage operation")
	}
	plan, err := chain.PrepareTokenRegistration(ctx, account, string(request.Asset))
	if err != nil {
		return nil, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	plan.ID = hex.EncodeToString(id[:])
	if err := c.childCallAt(epoch, "prepare", plan, nil); err != nil {
		return nil, err
	}
	return plan, nil
}
