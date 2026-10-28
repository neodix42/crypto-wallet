package walletcore

import (
	"encoding/json"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

func (c *core) addressBookAt(message ipc.Message, epoch uint64) (any, error) {
	var accountID string
	switch message.Method {
	case "addressBook":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
		}
		if strictjson.Decode(message.Body, &body) != nil {
			return nil, ipc.NewRequestError("ADDRESS_BOOK_INVALID_INPUT")
		}
		accountID = string(body.AccountID)
	case "saveAddress":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
			ID        strictjson.Text `json:"id,omitempty"`
			Name      strictjson.Text `json:"name"`
			Address   strictjson.Text `json:"address"`
			Memo      strictjson.Text `json:"memo,omitempty"`
		}
		if strictjson.Decode(message.Body, &body) != nil || !model.AddressBookText(string(body.Name), 80, false) || !model.AddressBookText(string(body.Address), 256, false) || !model.AddressBookText(string(body.Memo), 120, true) {
			return nil, ipc.NewRequestError("ADDRESS_BOOK_INVALID_INPUT")
		}
		accountID = string(body.AccountID)
	case "removeAddress":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
			ID        strictjson.Text `json:"id"`
		}
		if strictjson.Decode(message.Body, &body) != nil || len(body.ID) != 32 {
			return nil, ipc.NewRequestError("ADDRESS_BOOK_INVALID_INPUT")
		}
		accountID = string(body.AccountID)
	default:
		return nil, ipc.NewRequestError("ADDRESS_BOOK_INVALID_INPUT")
	}
	if !model.AddressBookText(accountID, 256, false) {
		return nil, ipc.NewRequestError("ADDRESS_BOOK_INVALID_INPUT")
	}
	// The signer is the authority for ownership, exact network and durable
	// encrypted persistence. Existing epoch checks discard late locked replies.
	var out json.RawMessage
	if err := c.childCallAt(epoch, message.Method, json.RawMessage(message.Body), &out); err != nil {
		return nil, err
	}
	return out, nil
}
