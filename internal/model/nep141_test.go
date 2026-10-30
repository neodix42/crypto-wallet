package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNEP141PolicyRequiresExplicitCompleteMetadata(t *testing.T) {
	valid := `{"network":"near-testnet","asset":{"id":"token","symbol":"TEST","decimals":0,"contract":"token.testnet"},"codeHash":"hash"}`
	var token NEP141Token
	if json.Unmarshal([]byte(valid), &token) != nil || token.Asset.Decimals != 0 {
		t.Fatal("explicit zero decimals refused")
	}
	for _, raw := range []string{`null`, `{}`, strings.Replace(valid, `"decimals":0`, `"decimals":null`, 1), strings.Replace(valid, `"decimals":0,`, "", 1), strings.Replace(valid, `"decimals":0`, `"decimals":256`, 1), strings.Replace(valid, `"decimals":0`, `"decimals":"0"`, 1), strings.Replace(valid, `"symbol":"TEST"`, `"symbol":"TEST","symbol":"OTHER"`, 1), strings.Replace(valid, `"network":"near-testnet"`, `"network":null`, 1), strings.Replace(valid, `"contract":"token.testnet"`, `"contract":null`, 1), strings.Replace(valid, `"codeHash":"hash"`, `"extra":true,"codeHash":"hash"`, 1), valid + `{}`} {
		if json.Unmarshal([]byte(raw), &token) == nil {
			t.Fatal("incomplete/ambiguous policy accepted")
		}
	}
}
