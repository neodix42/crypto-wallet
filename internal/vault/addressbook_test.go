package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func addressBookVaultFixture(t *testing.T) *Data {
	t.Helper()
	data := tokenVaultFixture(t)
	account := data.Accounts[1]
	data.AddressBook = []model.AddressBookEntry{{ID: strings.Repeat("a", 32), AccountID: account.ID, Network: account.Network,
		Name: "Public contact encryption canary", Address: "0x1111111111111111111111111111111111111111"}}
	return data
}

func TestAddressBookEncryptedRoundtripAndLegacySchemaCompatibility(t *testing.T) {
	data := addressBookVaultFixture(t)
	defer data.Destroy()
	raw, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	ciphertext, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	for _, contact := range data.AddressBook {
		if bytes.Contains(ciphertext, []byte(contact.Name)) || bytes.Contains(ciphertext, []byte(contact.Address)) {
			t.Fatal("contact data leaked outside encryption")
		}
	}
	opened, err := Open(ciphertext, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if !reflect.DeepEqual(restored.AddressBook, data.AddressBook) || VerifyRecovery(data, restored) != nil {
		t.Fatal("contact encryption changed metadata/recovery identity")
	}
	restored.AddressBook[0].Name = "changed"
	if VerifyRecovery(data, restored) == nil {
		t.Fatal("recovery verification ignored contact metadata")
	}
	restored.Destroy()
	if restored.AddressBook != nil {
		t.Fatal("locked vault retained contact metadata")
	}
	data.AddressBook = nil
	legacy, err := data.Encode()
	if err != nil || bytes.Contains(legacy, []byte(`"addressBook"`)) {
		t.Fatal("optional book became mandatory")
	}
	defer clear(legacy)
	decoded, err := Decode(legacy)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Destroy()
	if decoded.AddressBook != nil || VerifyRecovery(data, decoded) != nil {
		t.Fatal("legacy schema changed existing root")
	}
}

func TestAddressBookVaultRejectsCrossWalletChainMalformedAndAmbiguousEntries(t *testing.T) {
	for name, alter := range map[string]func(*Data){
		"foreign account":           func(d *Data) { d.AddressBook[0].AccountID = "foreign" },
		"different account network": func(d *Data) { d.AddressBook[0].Network = "base-sepolia" },
		"different environment":     func(d *Data) { d.Environment = "mainnet" },
		"invalid chain address":     func(d *Data) { d.AddressBook[0].Address = "receiver.near" },
		"zero EVM destination":      func(d *Data) { d.AddressBook[0].Address = "0x0000000000000000000000000000000000000000" },
		"bad EIP55":                 func(d *Data) { d.AddressBook[0].Address = "0x73410EA1FcbE1b8bCA883762dC56beF46403A46a" },
		"duplicate id":              func(d *Data) { d.AddressBook = append(d.AddressBook, d.AddressBook[0]) },
		"duplicate case insensitive name": func(d *Data) {
			e := d.AddressBook[0]
			e.ID = strings.Repeat("b", 32)
			e.Name = strings.ToUpper(e.Name)
			e.Address = "0x2222222222222222222222222222222222222222"
			d.AddressBook = append(d.AddressBook, e)
		},
		"duplicate recipient": func(d *Data) {
			e := d.AddressBook[0]
			e.ID = strings.Repeat("b", 32)
			e.Name = "Different"
			d.AddressBook = append(d.AddressBook, e)
		},
		"too many": func(d *Data) { d.AddressBook = make([]model.AddressBookEntry, model.MaxAddressBookEntries+1) },
		"per wallet bound": func(d *Data) {
			base := d.AddressBook[0]
			d.AddressBook = nil
			for i := 0; i <= model.MaxAccountAddressBookEntries; i++ {
				e := base
				e.ID = fmt.Sprintf("%032x", i+1)
				e.Name = fmt.Sprintf("Contact %d", i)
				e.Address = fmt.Sprintf("0x%040x", i+1)
				d.AddressBook = append(d.AddressBook, e)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := addressBookVaultFixture(t)
			defer d.Destroy()
			alter(d)
			if _, err := d.Encode(); err == nil {
				t.Fatal("unsafe contact metadata entered encrypted schema")
			}
			// Authenticated ciphertext can still contain deliberately malformed
			// structured data. Unlock must reject it before exposing contacts.
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(raw)
			if decoded, err := Decode(raw); err == nil || decoded != nil {
				t.Fatal("unsafe contact metadata decoded from authenticated plaintext")
			}
		})
	}
	data := addressBookVaultFixture(t)
	defer data.Destroy()
	raw, _ := json.Marshal(data)
	defer clear(raw)
	for _, raw := range []string{
		strings.Replace(string(raw), `"addressBook":[`, `"addressBook":[],"addressBook":[`, 1),
		strings.Replace(string(raw), `"name":"Public contact encryption canary"`, `"name":"Public contact encryption canary","name":"altered"`, 1),
		strings.Replace(string(raw), `"name":"Public contact encryption canary"`, `"name":null`, 1),
	} {
		if decoded, err := Decode([]byte(raw)); err == nil || decoded != nil {
			t.Fatal("ambiguous nested address book accepted")
		}
	}
}

func TestAddressBookRetiredHorizenContactsCannotReactivateHistoricalWallets(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		t.Run(environment, func(t *testing.T) {
			data := synthetic()
			defer data.Destroy()
			data.Environment = environment
			seed, err := derivation.Seed(data.Entropy, data.Passphrase)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(seed)
			network := "base-mainnet"
			if environment == "testnet" {
				network = "base-sepolia"
			}
			retired, err := evm.New().Derive(seed, 0, network)
			if err != nil {
				t.Fatal(err)
			}
			retired.Network = "horizen-" + environment
			retired.ID = "evm:0:" + retired.Network
			if err := evm.ValidateRetiredAccount(seed, retired); err != nil {
				t.Fatal("historical account does not authenticate against its original root", err)
			}
			data.Accounts = []model.Account{retired}
			data.Endpoints = map[string]Endpoint{retired.Network: {URL: "https://retired.invalid"}}
			raw, err := data.Encode()
			if err != nil {
				t.Fatal("retirement destroyed compatibility with historical descriptors", err)
			}
			defer clear(raw)
			decoded, err := Decode(raw)
			if err != nil {
				t.Fatal("historical vault without contacts failed to decode", err)
			}
			defer decoded.Destroy()
			if !reflect.DeepEqual(decoded.Accounts, data.Accounts) || VerifyRecovery(data, decoded) != nil {
				t.Fatal("historical descriptors or recovery identity changed")
			}
			data.AddressBook = []model.AddressBookEntry{{ID: strings.Repeat("c", 32), AccountID: retired.ID, Network: retired.Network, Name: "Retired destination", Address: retired.Address}}
			if _, err := data.Encode(); err == nil {
				t.Fatal("retired Horizen network gained a saved destination route")
			}
			malformed, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(malformed)
			if restored, err := Decode(malformed); err == nil || restored != nil {
				t.Fatal("authenticated malformed vault reactivated a retired network contact")
			}
		})
	}
}
