package walletcore

import (
	"encoding/json"
	"errors"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

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
	ethereum, ok := adapter.(*evm.Adapter)
	if !ok {
		return nil, errors.New("token import is available only for Ethereum")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	token, err := ethereum.InspectToken(ctx, account, string(request.Contract))
	if err != nil {
		return nil, err
	}
	var output json.RawMessage
	if err = c.childCallAt(epoch, "importToken", struct {
		AccountID string           `json:"accountId"`
		Token     model.ERC20Token `json:"token"`
	}{account.ID, token}, &output); err != nil {
		return nil, err
	}
	if err = c.applySettingsAt(epoch); err != nil {
		return nil, err
	}
	return c.publicSnapshotResultAt(epoch, message.Session, output)
}
