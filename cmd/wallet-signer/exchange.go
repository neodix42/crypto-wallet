package main

import (
	"encoding/json"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/signer"
	"localwallet/internal/strictjson"
)

// These methods belong only to the inherited core/signer channel. The public
// portfolio bridge has a separate, smaller coordinator-owned allowlist.
func handleExchange(e *signer.Engine, method string, raw json.RawMessage) (json.RawMessage, error) {
	var out any
	var err error
	switch method {
	case "exchangeStatus", "exchangeDisconnect":
		err = signer.DecodeBody(raw, &struct{}{})
		if err == nil && method == "exchangeDisconnect" {
			err = e.DisconnectExchange()
		}
		if err == nil {
			out, err = e.ExchangeStatus()
		}
	case "exchangeConnect":
		var in struct {
			Endpoint strictjson.Text `json:"endpoint"`
		}
		if err = signer.DecodeBody(raw, &in); err == nil {
			err = e.ConnectExchange(string(in.Endpoint))
		}
		if err == nil {
			out, err = e.ExchangeStatus()
		}
	case "exchangePrepare":
		var in signer.ExchangeIntent
		if err = signer.DecodeBody(raw, &in); err == nil {
			out, err = e.PrepareExchange(in)
		}
	case "exchangeAuthorize", "exchangeOperation", "exchangeCancel":
		var in struct {
			ID strictjson.Text `json:"id"`
		}
		if err = signer.DecodeBody(raw, &in); err == nil {
			switch method {
			case "exchangeAuthorize":
				out, err = e.AuthorizeExchange(string(in.ID))
			case "exchangeOperation":
				out, err = e.ExchangeOperation(string(in.ID))
			case "exchangeCancel":
				err = e.CancelExchange(string(in.ID))
				if err == nil {
					out, err = e.ExchangeStatus()
				}
			}
		}
	case "exchangeAuth":
		var in bybit.AuthRequest
		if err = signer.DecodeBody(raw, &in); err == nil {
			out, err = e.ExchangeAuth(in)
		}
	case "exchangeUpdate":
		var in struct {
			ID         strictjson.Text `json:"id"`
			State      strictjson.Text `json:"state"`
			ExternalID strictjson.Text `json:"externalId"`
		}
		if err = signer.DecodeBody(raw, &in); err == nil {
			err = e.UpdateExchange(string(in.ID), string(in.State), string(in.ExternalID))
		}
		if err == nil {
			out, err = e.ExchangeOperation(string(in.ID))
		}
	case "exchangeReject":
		var in struct {
			ID   strictjson.Text `json:"id"`
			Code int64           `json:"code"`
		}
		if err = signer.DecodeBody(raw, &in); err == nil {
			err = e.RejectExchange(string(in.ID), in.Code)
		}
		if err == nil {
			out, err = e.ExchangeOperation(string(in.ID))
		}
	default:
		return nil, signer.ErrDenied
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
