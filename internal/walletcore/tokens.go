package walletcore

import (
	"encoding/json"
	"errors"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

func (c *core) discoverTokensAt(message ipc.Message, epoch uint64) (any, error) {
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" {
		return nil, errors.New("token discovery requires an account")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	base, ok := adapter.(*evm.Adapter)
	if !ok {
		return nil, errors.New("token discovery requires a Base wallet")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	return base.DiscoverTokens(ctx, account)
}

func (c *core) importTokenAt(message ipc.Message, epoch uint64) (any, error) {
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
		Contract  strictjson.Text `json:"contract"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" || request.Contract == "" {
		return nil, errors.New("token import requires an account and exact contract")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	var body any
	method := "importToken"
	switch chain := adapter.(type) {
	case *evm.Adapter:
		token, err := chain.InspectToken(ctx, account, string(request.Contract))
		if err != nil {
			return nil, err
		}
		body = struct {
			AccountID string           `json:"accountId"`
			Token     model.ERC20Token `json:"token"`
		}{account.ID, token}
	case *near.Adapter:
		token, err := chain.InspectToken(ctx, account, string(request.Contract))
		if err != nil {
			return nil, err
		}
		method = "importNearToken"
		body = struct {
			AccountID string            `json:"accountId"`
			Token     model.NEP141Token `json:"token"`
		}{account.ID, token}
	default:
		return nil, errors.New("token import requires a supported Ethereum, Base or NEAR wallet")
	}
	var output json.RawMessage
	if err = c.childCallAt(epoch, method, body, &output); err != nil {
		return nil, err
	}
	if err = c.applySettingsAt(epoch); err != nil {
		return nil, err
	}
	return c.publicSnapshotResultAt(epoch, message.Session, output)
}
