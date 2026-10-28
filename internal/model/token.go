package model

import (
	"errors"
	"localwallet/internal/strictjson"
)

// UnmarshalJSON distinguishes a genuine zero-decimal token from missing/null
// precision. Token policies are exact typed authorization metadata.
func (token *ERC20Token) UnmarshalJSON(raw []byte) error {
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
	if strictjson.Decode(raw, &wire) != nil || wire.Network == nil || wire.Asset == nil || wire.CodeHash == nil ||
		wire.Asset.ID == nil || wire.Asset.Symbol == nil || wire.Asset.Decimals == nil || wire.Asset.Contract == nil {
		return errors.New("incomplete ERC-20 token policy")
	}
	*token = ERC20Token{Network: *wire.Network, Asset: Asset{ID: *wire.Asset.ID, Symbol: *wire.Asset.Symbol,
		Decimals: *wire.Asset.Decimals, Contract: *wire.Asset.Contract}, CodeHash: *wire.CodeHash}
	return nil
}
