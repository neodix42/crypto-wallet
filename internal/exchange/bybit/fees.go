package bybit

import (
	"bytes"
	"encoding/json"
	"math/big"
	"unicode/utf8"

	"localwallet/internal/amount"
	"localwallet/internal/strictjson"
)

// ExtraFee is the complete extraFees item documented by Bybit's execution
// stream. REST encodes the same list as a JSON string. Tax charges are distinct
// from execFee and must be included in the order's currency-specific accounting.
type ExtraFee struct {
	FeeCoin    string `json:"feeCoin"`
	FeeType    string `json:"feeType"`
	SubFeeType string `json:"subFeeType"`
	FeeRate    string `json:"feeRate"`
	Fee        string `json:"fee"`
}

func extraFeePair(kind, subkind string) bool {
	switch kind {
	case "VAT":
		return subkind == "ARE_VAT"
	case "GST":
		return subkind == "IND_GST"
	case "TAX":
		return subkind == "TAX_PNN" || subkind == "TAX_PPH"
	case "CFX":
		return subkind == "CFX_FIEE"
	case "WHT":
		return subkind == "AUT_WITHHOLDING_TAX"
	}
	return false
}

// ExecutionExtraFees accepts only an explicit empty string/list or a complete
// known, bounded fee list. Missing/null or an unknown tax never means zero fees.
// No untrusted response text is included in errors.
func ExecutionExtraFees(raw json.RawMessage) ([]ExtraFee, error) {
	if len(raw) == 0 || len(raw) > 4096 || !utf8.Valid(raw) {
		return nil, ErrInvalidResponse
	}
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return nil, ErrInvalidResponse
	}
	if b[0] == '"' {
		var encoded string
		if strictjson.Decode(b, &encoded) != nil || len(encoded) > 4096 {
			return nil, ErrInvalidResponse
		}
		if encoded == "" {
			return []ExtraFee{}, nil
		}
		b = bytes.TrimSpace([]byte(encoded))
	}
	if len(b) == 0 || b[0] != '[' {
		return nil, ErrInvalidResponse
	}
	var fields []map[string]json.RawMessage
	if strictjson.Decode(b, &fields) != nil || len(fields) > 16 {
		return nil, ErrInvalidResponse
	}
	var fees []ExtraFee
	if strictjson.Decode(b, &fees) != nil {
		return nil, ErrInvalidResponse
	}
	seen := map[string]bool{}
	for i, fee := range fees {
		if len(fields[i]) != 5 || !coinName(fee.FeeCoin) || !extraFeePair(fee.FeeType, fee.SubFeeType) || !decimal(fee.Fee, false) {
			return nil, ErrInvalidResponse
		}
		// All five fields are required quoted strings. Encoding/json otherwise
		// accepts null as the string zero value, which could conceal a charge.
		for _, name := range []string{"feeCoin", "feeType", "subFeeType", "feeRate", "fee"} {
			value, ok := fields[i][name]
			if !ok || len(value) == 0 || value[0] != '"' {
				return nil, ErrInvalidResponse
			}
		}
		rate, err := amount.Parse(fee.FeeRate, 36)
		if err != nil {
			return nil, ErrInvalidResponse
		}
		n, _ := new(big.Int).SetString(rate, 10)
		if n.Cmp(new(big.Int).Exp(big.NewInt(10), big.NewInt(36), nil)) > 0 {
			return nil, ErrInvalidResponse
		}
		identity := fee.FeeCoin + "\x00" + fee.FeeType + "\x00" + fee.SubFeeType
		if seen[identity] {
			return nil, ErrInvalidResponse
		}
		seen[identity] = true
	}
	if fees == nil {
		fees = []ExtraFee{}
	}
	return fees, nil
}
