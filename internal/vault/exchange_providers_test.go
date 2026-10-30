package vault

import (
	"bytes"
	"encoding/json"
	"testing"
)

func exchangeProvidersFixture() *Data {
	d := exchangeVaultFixture()
	d.Exchange.SelectedProvider = "kucoin"
	d.Exchange.ProviderCredentials = []*ExchangeCredentials{
		{Provider: "binance", Endpoint: "https://api.binance.com", APIKey: []byte("PUBLIC_BINANCE_API_KEY_84752"), Secret: []byte("PUBLIC_BINANCE_API_SECRET_8472574")},
		{Provider: "kucoin", Endpoint: "https://api.kucoin.com", APIKey: []byte("PUBLIC_KUCOIN_API_KEY_84752"), Secret: []byte("PUBLIC_KUCOIN_API_SECRET_8472574"), Passphrase: []byte("PUBLIC_KUCOIN_API_PASSPHRASE_8472574"), KeyVersion: 3},
	}
	return d
}

func TestExchangeProvidersRoundtripEncryptedIdentityAndDestruction(t *testing.T) {
	d := exchangeProvidersFixture()
	raw, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, credential := range d.Exchange.ProviderCredentials {
		for _, private := range [][]byte{credential.APIKey, credential.Secret, credential.Passphrase} {
			if len(private) > 0 && bytes.Contains(sealed, private) {
				t.Fatal("provider credential outside encrypted envelope")
			}
		}
	}
	opened, err := Open(sealed, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil || VerifyRecovery(d, restored) != nil || restored.Exchange.Selected() != "kucoin" {
		t.Fatal("multi-provider identity did not survive authenticated roundtrip", err)
	}
	defer restored.Destroy()
	restored.Exchange.ProviderCredentials[1].Passphrase[0] ^= 1
	if VerifyRecovery(d, restored) == nil {
		t.Fatal("recovery comparison ignored API passphrase")
	}
	key := d.Exchange.ProviderCredentials[1].APIKey
	secret := d.Exchange.ProviderCredentials[1].Secret
	phrase := d.Exchange.ProviderCredentials[1].Passphrase
	d.Destroy()
	for _, value := range [][]byte{key, secret, phrase} {
		if !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatal("new provider private buffer survived destruction")
		}
	}
}

func TestExchangeProvidersRejectWrongDomainDuplicatedServiceAndUnboundedSecrets(t *testing.T) {
	for _, change := range []func(*ExchangeData){
		func(x *ExchangeData) { x.SelectedProvider = "arbitrary" }, func(x *ExchangeData) { x.ProviderCredentials = append(x.ProviderCredentials, x.ProviderCredentials[0]) }, func(x *ExchangeData) { x.ProviderCredentials[0].Endpoint = "https://api.kucoin.com" }, func(x *ExchangeData) { x.ProviderCredentials[0].Provider = "bybit" }, func(x *ExchangeData) { x.ProviderCredentials[0].Provider = "" }, func(x *ExchangeData) { x.ProviderCredentials[0] = nil }, func(x *ExchangeData) { x.ProviderCredentials[0].Passphrase = []byte("foreign-passphrase") }, func(x *ExchangeData) { x.ProviderCredentials[0].KeyVersion = 3 }, func(x *ExchangeData) { x.ProviderCredentials[1].Passphrase = nil }, func(x *ExchangeData) { x.ProviderCredentials[1].Passphrase = make([]byte, 257) }, func(x *ExchangeData) { x.ProviderCredentials[1].KeyVersion = 1 }, func(x *ExchangeData) { x.Credentials = x.ProviderCredentials[0] }, func(x *ExchangeData) { x.ProviderCredentials[1].Endpoint = "https://api.kucoin.com/" },
	} {
		d := exchangeProvidersFixture()
		change(d.Exchange)
		if _, err := d.Encode(); err == nil {
			t.Fatal("malformed provider storage accepted")
		}
		d.Destroy()
	}
	d := exchangeProvidersFixture()
	raw, _ := d.Encode()
	var value map[string]any
	json.Unmarshal(raw, &value)
	for _, key := range []string{"secret", "passphrase", "keyVersion"} {
		copy := bytes.Replace(raw, []byte(`"`+key+`":`), []byte(`"unknownProviderAuthority":false,"`+key+`":`), 1)
		if _, err := Decode(copy); err == nil {
			t.Fatal("unknown credential authority accepted")
		}
	}
}

func TestExchangeProviderSelectionDoesNotRequireOrCreateRemoteCredentials(t *testing.T) {
	x := &ExchangeData{}
	if x.Selected() != "bybit" || x.Credential("binance") != nil {
		t.Fatal("legacy default changed")
	}
	x.SelectedProvider = "binance"
	if x.Selected() != "binance" || x.Credential("binance") != nil {
		t.Fatal("selection invented credentials")
	}
	c := &ExchangeCredentials{Provider: "binance"}
	x.SetCredential("binance", c)
	if x.Credential("binance") != c {
		t.Fatal("credential not stored")
	}
	x.SetCredential("binance", nil)
	if len(x.ProviderCredentials) != 0 {
		t.Fatal("credential not removed")
	}
}
