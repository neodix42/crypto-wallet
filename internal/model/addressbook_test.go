package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAddressBookMetadataRejectsHiddenFormattingAndUnsafeBounds(t *testing.T) {
	account := Account{ID: "owned", Network: "near-mainnet", Family: "near"}
	base := AddressBookEntry{ID: strings.Repeat("a", 32), AccountID: account.ID, Network: account.Network, Name: "Мой получатель", Address: "receiver.near"}
	if !base.ValidFor(account) {
		t.Fatal("normal multilingual contact metadata rejected")
	}
	for name, alter := range map[string]func(*AddressBookEntry){
		"foreign account":     func(e *AddressBookEntry) { e.AccountID = "foreign" },
		"wrong network":       func(e *AddressBookEntry) { e.Network = "near-testnet" },
		"nonhex id":           func(e *AddressBookEntry) { e.ID = strings.Repeat("z", 32) },
		"uppercase id":        func(e *AddressBookEntry) { e.ID = strings.Repeat("A", 32) },
		"short id":            func(e *AddressBookEntry) { e.ID = "a" },
		"empty name":          func(e *AddressBookEntry) { e.Name = "" },
		"long name":           func(e *AddressBookEntry) { e.Name = strings.Repeat("a", 81) },
		"leading space":       func(e *AddressBookEntry) { e.Name = " name" },
		"trailing space":      func(e *AddressBookEntry) { e.Name = "name " },
		"line injection":      func(e *AddressBookEntry) { e.Name = "name\nNetwork: false" },
		"line separator":      func(e *AddressBookEntry) { e.Name = "name\u2028Network: false" },
		"paragraph separator": func(e *AddressBookEntry) { e.Name = "name\u2029Network: false" },
		"bidi":                func(e *AddressBookEntry) { e.Address = "receiver\u202e.near" },
		"zero width":          func(e *AddressBookEntry) { e.Name = "na\u200bme" },
		"invalid utf8":        func(e *AddressBookEntry) { e.Name = string([]byte{0xff}) },
		"empty address":       func(e *AddressBookEntry) { e.Address = "" },
		"long address":        func(e *AddressBookEntry) { e.Address = strings.Repeat("a", 257) },
		"non TON memo":        func(e *AddressBookEntry) { e.Memo = "memo" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			alter(&changed)
			if changed.ValidFor(account) {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	account.Family = "ton"
	for _, separator := range []string{"\u2028", "\u2029"} {
		for _, field := range []string{"name", "address", "memo"} {
			changed := base
			switch field {
			case "name":
				changed.Name = "first" + separator + "second"
			case "address":
				changed.Address = "first" + separator + "second"
			case "memo":
				changed.Memo = "first" + separator + "second"
			}
			if changed.ValidFor(account) {
				t.Fatal("Unicode line or paragraph separator accepted in", field)
			}
		}
	}
	base.Memo = strings.Repeat("a", 120)
	if !base.ValidFor(account) {
		t.Fatal("TON memo boundary rejected")
	}
	base.Memo += "a"
	if base.ValidFor(account) {
		t.Fatal("oversized TON memo accepted")
	}
}

func TestAddressBookJSONRejectsNullDuplicateAndUnknownFields(t *testing.T) {
	valid := `{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","accountId":"owned","network":"near-mainnet","name":"Receiver","address":"receiver.near"}`
	var entry AddressBookEntry
	if json.Unmarshal([]byte(valid), &entry) != nil {
		t.Fatal("valid entry did not decode")
	}
	for _, raw := range []string{
		strings.Replace(valid, `"name":"Receiver"`, `"name":null`, 1),
		strings.Replace(valid, `"name":"Receiver"`, `"name":"Receiver","name":"Another"`, 1),
		strings.Replace(valid, `"name":"Receiver"`, `"name":"Receiver","memo":null`, 1),
		strings.Replace(valid, `"name":"Receiver"`, `"name":"Receiver","unexpected":true`, 1),
		"null", "[]", valid + "{}",
	} {
		if json.Unmarshal([]byte(raw), &entry) == nil {
			t.Fatal("ambiguous address book entry accepted")
		}
	}
}
