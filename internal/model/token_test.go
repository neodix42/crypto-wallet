package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTokenPolicyZeroPrecisionIsExplicitAndCannotBecomeAMissingDefault(t *testing.T) {
	valid := `{"network":"ethereum-sepolia","asset":{"id":"token","symbol":"TEST","decimals":0,"contract":"contract"},"codeHash":"hash"}`
	var token ERC20Token
	if json.Unmarshal([]byte(valid), &token) != nil || token.Asset.Decimals != 0 || token.Asset.Symbol != "TEST" {
		t.Fatal("explicit zero-decimal metadata was rejected")
	}
	for _, invalid := range []string{
		`null`, `{}`, strings.Replace(valid, `"decimals":0`, `"decimals":null`, 1),
		strings.Replace(valid, `"decimals":0,`, "", 1), strings.Replace(valid, `"decimals":0`, `"decimals":256`, 1),
		strings.Replace(valid, `"decimals":0`, `"decimals":-1`, 1), strings.Replace(valid, `"decimals":0`, `"decimals":"0"`, 1),
		strings.Replace(valid, `"network":"ethereum-sepolia"`, `"network":null`, 1),
		strings.Replace(valid, `"codeHash":"hash"`, `"codeHash":null`, 1),
		strings.Replace(valid, `"symbol":"TEST"`, `"symbol":null`, 1),
		strings.Replace(valid, `"symbol":"TEST"`, `"symbol":"TEST","symbol":"OTHER"`, 1),
		strings.Replace(valid, `"codeHash":"hash"`, `"extra":"ignored","codeHash":"hash"`, 1),
	} {
		if json.Unmarshal([]byte(invalid), &token) == nil {
			t.Fatal("ambiguous/null/missing metadata became an authorized token policy")
		}
	}
}
