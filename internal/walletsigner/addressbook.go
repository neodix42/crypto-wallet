package walletsigner

import (
	"encoding/json"

	"localwallet/internal/signer"
	"localwallet/internal/strictjson"
)

func handleAddressBook(engine *signer.Engine, method string, raw []byte) (json.RawMessage, error) {
	var out any
	var err error
	switch method {
	case "addressBook":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
		}
		if signer.DecodeBody(raw, &body) != nil {
			return nil, signer.ErrDenied
		}
		out, err = engine.AddressBook(string(body.AccountID))
	case "saveAddress":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
			ID        strictjson.Text `json:"id,omitempty"`
			Name      strictjson.Text `json:"name"`
			Address   strictjson.Text `json:"address"`
			Memo      strictjson.Text `json:"memo,omitempty"`
		}
		if signer.DecodeBody(raw, &body) != nil {
			return nil, signer.ErrDenied
		}
		out, err = engine.SaveAddress(string(body.AccountID), string(body.ID), string(body.Name), string(body.Address), string(body.Memo))
	case "removeAddress":
		var body struct {
			AccountID strictjson.Text `json:"accountId"`
			ID        strictjson.Text `json:"id"`
		}
		if signer.DecodeBody(raw, &body) != nil {
			return nil, signer.ErrDenied
		}
		err = engine.RemoveAddress(string(body.AccountID), string(body.ID))
	default:
		return nil, signer.ErrDenied
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
