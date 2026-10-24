package vault

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func exchangeVaultFixture() *Data {
	d := synthetic()
	c := &ExchangeCredentials{Endpoint: "https://api.bybit.com", APIKey: []byte("PUBLIC_TEST_ONLY_API_KEY"), Secret: []byte("PUBLIC_TEST_ONLY_SECRET_128734")}
	o := ExchangeOperation{ID: strings.Repeat("a", 32), Kind: "swap", Endpoint: c.Endpoint, CredentialID: ExchangeCredentialID(c), AccountID: "a", AssetID: "near-mainnet:native", Payload: `{"category":"spot"}`, State: "SUBMITTING", CreatedAt: 1, ExpiresAt: 301}
	o.Digest = ExchangeDigest(o)
	d.Exchange = &ExchangeData{Credentials: c, Operations: []ExchangeOperation{o}}
	return d
}
func TestExchangeTP030OptionalEncryptedExtensionAndCredentialDestruction(t *testing.T) {
	d := exchangeVaultFixture()
	raw, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, d.Exchange.Credentials.APIKey) || bytes.Contains(cipher, d.Exchange.Credentials.Secret) {
		t.Fatal("exchange plaintext outside encrypted vault")
	}
	opened, err := Open(cipher, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifyRecovery(d, restored); err != nil {
		t.Fatal(err)
	}
	restored.Exchange.Credentials.Secret[0] ^= 1
	if VerifyRecovery(d, restored) == nil {
		t.Fatal("credential change ignored in recovery identity")
	}
	key, secret := d.Exchange.Credentials.APIKey, d.Exchange.Credentials.Secret
	d.Destroy()
	if d.Exchange != nil || !bytes.Equal(key, make([]byte, len(key))) || !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatal("secret buffers survived destroy")
	}
	bare := synthetic()
	raw, err = bare.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"exchange"`)) {
		t.Fatal("legacy schema gained mandatory exchange state")
	}
	if _, err = Decode(raw); err != nil {
		t.Fatal("legacy schema not readable")
	}
	x := &ExchangeData{}
	x.Destroy()
	if ExchangeCredentialID(nil) != "" {
		t.Fatal("nil identity")
	}
}
func TestExchangeTP030RejectsChangedIntentUnboundedJournalAndMalformedCredentials(t *testing.T) {
	for _, mutate := range []func(*Data){
		func(d *Data) { d.Exchange.Operations = make([]ExchangeOperation, 1001) },
		func(d *Data) { d.Exchange.Credentials.Endpoint = "" }, func(d *Data) { d.Exchange.Credentials.APIKey = []byte("short") }, func(d *Data) { d.Exchange.Credentials.Secret = []byte("short") },
		func(d *Data) { d.Exchange.Operations[0].ID = "invalid" }, func(d *Data) { d.Exchange.Operations[0].ID = strings.Repeat("A", 32) }, func(d *Data) {
			d.Exchange.Operations[0].Kind = "arbitrary"
			d.Exchange.Operations[0].Digest = ExchangeDigest(d.Exchange.Operations[0])
		}, func(d *Data) { d.Exchange.Operations[0].State = "RETRY_NEW_ID" },
		func(d *Data) { d.Exchange.Operations[0].Payload = strings.Repeat("x", 8193) }, func(d *Data) { d.Exchange.Operations[0].AccountID = "foreign" }, func(d *Data) { d.Exchange.Operations[0].AssetID = "" }, func(d *Data) { d.Exchange.Operations[0].CredentialID = "invalid" }, func(d *Data) { d.Exchange.Operations[0].CreatedAt = 0 }, func(d *Data) { d.Exchange.Operations[0].ExpiresAt = 1000 }, func(d *Data) { d.Exchange.Operations[0].ExternalID = "bad\x00value" }, func(d *Data) { d.Exchange.Operations[0].Payload = `{"qty":"2"}` }, func(d *Data) { d.Exchange.Operations = append(d.Exchange.Operations, d.Exchange.Operations[0]) },
		func(d *Data) { d.Exchange.Operations[0].RejectionCode = 10001 },
		func(d *Data) {
			d.Exchange.Operations[0].State = "FAILED"
			d.Exchange.Operations[0].RejectionCode = 10000
		},
		func(d *Data) {
			d.Exchange.Operations[0].State = "FAILED"
			d.Exchange.Operations[0].RejectionCode = 10003
			d.Exchange.Operations[0].ExternalID = "known-remote"
		},
	} {
		d := exchangeVaultFixture()
		mutate(d)
		if _, err := d.Encode(); err == nil {
			t.Fatal("malformed exchange vault accepted")
		}
		d.Destroy()
	}
	d := exchangeVaultFixture()
	raw, _ := d.Encode()
	for _, bad := range []string{strings.Replace(string(raw), `"credentials":`, `"credentials":null,"credentials":`, 1), strings.Replace(string(raw), `"endpoint":`, `"unknown":false,"endpoint":`, 1)} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Fatal("duplicate/unknown exchange field accepted")
		}
	}
	for _, kind := range []string{"swap", "transfer", "withdraw"} {
		for _, state := range []string{"PREPARED", "AUTHORIZED", "SUBMITTING", "PENDING", "UNKNOWN", "CONFIRMED", "FAILED", "CANCELLED"} {
			d := exchangeVaultFixture()
			d.Exchange.Operations[0].Kind = kind
			d.Exchange.Operations[0].State = state
			d.Exchange.Operations[0].Digest = ExchangeDigest(d.Exchange.Operations[0])
			if _, err := d.Encode(); err != nil {
				t.Fatal("valid journal enum rejected")
			}
		}
	}
	if exchangeText("bad\rvalue", 100, true) || exchangeText(string([]byte{255}), 100, true) || exchangeHex(strings.Repeat("g", 32), 32) {
		t.Fatal("invalid encoded text accepted")
	}
	var unknown map[string]any
	for _, code := range []int64{10001, 10003, 10004, 10005} {
		d := exchangeVaultFixture()
		before := d.Exchange.Operations[0].Digest
		d.Exchange.Operations[0].State = "FAILED"
		d.Exchange.Operations[0].RejectionCode = code
		if _, err := d.Encode(); err != nil || ExchangeDigest(d.Exchange.Operations[0]) != before {
			t.Fatal("documented rejection changed immutable approval")
		}
	}
	json.Unmarshal(raw, &unknown)
	unknown["exchange"] = nil
	if d, err := Decode(mustJSON(t, unknown)); err != nil || d.Exchange != nil {
		t.Fatal("optional nil exchange")
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
