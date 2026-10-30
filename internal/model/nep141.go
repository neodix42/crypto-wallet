package model

import (
	"errors"
	"localwallet/internal/strictjson"
)

// NEP141Token is an explicitly approved network/contract/code policy. Metadata
// describes that contract; it does not authenticate its issuer or discover funds.
type NEP141Token struct {
	Network  string `json:"network"`
	Asset    Asset  `json:"asset"`
	CodeHash string `json:"codeHash"`
}

// Required pointers preserve genuine zero precision while refusing absent/null
// fields, duplicate keys, unknown keys and incomplete authorization metadata.
func (token *NEP141Token) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Network *string `json:"network"`
		Asset   *struct {
			ID       *string `json:"id"`
			Symbol   *string `json:"symbol"`
			Decimals *uint8  `json:"decimals"`
			Contract *string `json:"contract"`
		} `json:"asset"`
		CodeHash *string `json:"codeHash"`
	}
	if strictjson.Decode(raw, &wire) != nil || wire.Network == nil || wire.Asset == nil || wire.CodeHash == nil || wire.Asset.ID == nil || wire.Asset.Symbol == nil || wire.Asset.Decimals == nil || wire.Asset.Contract == nil {
		return errors.New("incomplete NEP-141 token policy")
	}
	*token = NEP141Token{Network: *wire.Network, Asset: Asset{ID: *wire.Asset.ID, Symbol: *wire.Asset.Symbol, Decimals: *wire.Asset.Decimals, Contract: *wire.Asset.Contract}, CodeHash: *wire.CodeHash}
	return nil
}
