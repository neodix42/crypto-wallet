package model

import (
	"bytes"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"localwallet/internal/strictjson"
)

const MaxAddressBookEntries = 2048
const MaxAccountAddressBookEntries = 256

// AddressBookEntry is public destination metadata. It is encrypted with the
// vault, and is scoped to an owned source account and its exact network.
// Selecting an entry never authorizes a payment.
type AddressBookEntry struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	Network   string `json:"network"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Memo      string `json:"memo,omitempty"`
}

func (entry *AddressBookEntry) UnmarshalJSON(raw []byte) error {
	if body := bytes.TrimSpace(raw); len(body) == 0 || body[0] != '{' {
		return errors.New("invalid address book entry")
	}
	var wire struct {
		ID        strictjson.Text `json:"id"`
		AccountID strictjson.Text `json:"accountId"`
		Network   strictjson.Text `json:"network"`
		Name      strictjson.Text `json:"name"`
		Address   strictjson.Text `json:"address"`
		Memo      strictjson.Text `json:"memo,omitempty"`
	}
	if strictjson.Decode(raw, &wire) != nil {
		return errors.New("invalid address book entry")
	}
	*entry = AddressBookEntry{ID: string(wire.ID), AccountID: string(wire.AccountID), Network: string(wire.Network), Name: string(wire.Name), Address: string(wire.Address), Memo: string(wire.Memo)}
	return nil
}

// AddressBookText rejects invisible formatting/control characters, which can
// disguise the destination or reorder its visible network/name during review.
func AddressBookText(value string, limit int, empty bool) bool {
	if len(value) > limit || !utf8.ValidString(value) || (!empty && value == "") || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return false
		}
	}
	return true
}

// ValidFor verifies local metadata and account/network binding. The caller must
// also validate Address through that network's registered chain adapter.
func (entry AddressBookEntry) ValidFor(account Account) bool {
	if len(entry.ID) != 32 || entry.AccountID != account.ID || entry.Network != account.Network ||
		!AddressBookText(entry.Name, 80, false) || !AddressBookText(entry.Address, 256, false) ||
		!AddressBookText(entry.Memo, 120, true) || (entry.Memo != "" && account.Family != "ton") {
		return false
	}
	for _, r := range entry.ID {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
