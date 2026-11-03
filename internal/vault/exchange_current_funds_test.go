package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCurrentFundsPolicyIsOptionalDigestBoundAndBybitOnly(t *testing.T) {
	credential := &ExchangeCredentials{Endpoint: "https://api.bybit.com", APIKey: []byte("PUBLIC_TEST_KEY_928312"), Secret: []byte("PUBLIC_TEST_ONLY_SECRET_12345")}
	operation := ExchangeOperation{ID: strings.Repeat("a", 32), Kind: "transfer", Endpoint: credential.Endpoint, CredentialID: ExchangeCredentialID(credential), AccountID: "owned", AssetID: "ethereum-mainnet:native", Payload: `{"coin":"ETH"}`, State: "UNKNOWN", CreatedAt: 1, ExpiresAt: 301}
	operation.Digest = ExchangeDigest(operation)
	data := &ExchangeData{Credentials: credential, SelectedProvider: "bybit", Operations: []ExchangeOperation{operation}}
	if err := validateExchange(data, map[string]bool{"owned": true}); err != nil {
		t.Fatal(err)
	}
	// The previous serializer's exact field order/omitempty policy is retained
	// when the new optional mode is absent, preserving existing authenticated IDs.
	raw, _ := json.Marshal(operation)
	if strings.Contains(string(raw), "currentFunds") {
		t.Fatal("legacy operation serialization changed")
	}
	oldJSON := `{"id":"` + operation.ID + `","kind":"transfer","endpoint":"https://api.bybit.com","credentialId":"` + operation.CredentialID + `","accountId":"owned","assetId":"ethereum-mainnet:native","payload":"{\"coin\":\"ETH\"}","digest":"","state":"","createdAt":1,"expiresAt":301}`
	digest := sha256.Sum256([]byte(oldJSON))
	if operation.Digest != hex.EncodeToString(digest[:]) {
		t.Fatal("legacy immutable digest changed")
	}
	data.Operations[0].CurrentFunds = true
	if err := validateExchange(data, map[string]bool{"owned": true}); err == nil {
		t.Fatal("policy changed without authenticating new digest")
	}
	data.Operations[0].Digest = ExchangeDigest(data.Operations[0])
	if data.Operations[0].Digest == operation.Digest {
		t.Fatal("current-funds flag omitted from immutable digest")
	}
	if err := validateExchange(data, map[string]bool{"owned": true}); err != nil {
		t.Fatal("new optional policy failed encrypted-state validation", err)
	}
	data.Operations[0].Endpoint = "https://api.binance.com"
	data.Operations[0].Digest = ExchangeDigest(data.Operations[0])
	if err := validateExchange(data, map[string]bool{"owned": true}); err == nil {
		t.Fatal("Bybit-only authority accepted another provider")
	}
}
