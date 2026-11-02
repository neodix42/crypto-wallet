package vault

import (
	"bytes"
	"encoding/json"
	"localwallet/internal/model"
	"testing"
)

func TestEncryptedAccountPresentationCompatibilityAndRecoveryBinding(t *testing.T) {
	original := synthetic()
	raw, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("accountPresentation")) {
		t.Fatal("legacy empty vault format grew presentation")
	}
	data, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Destroy()
	data.AccountPresentation = model.AccountPresentation{WalletLabels: map[string]string{data.Accounts[0].ID: "Private savings"}, GroupLabels: map[string]string{"0": "Main account"}, DeletedGroups: map[string]bool{"0": true}}
	plain, err := data.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	sealed, err := Seal(plain, []byte("public-test-only-storage-password"), Parameters{Memory: 64 * 1024, Passes: 3, Lanes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("Private savings")) {
		t.Fatal("label stored outside ciphertext")
	}
	opened, err := Open(sealed, []byte("public-test-only-storage-password"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(opened)
	restored, err := Decode(opened)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Destroy()
	if VerifyRecovery(data, restored) != nil {
		t.Fatal("presentation lost during authenticated recovery")
	}
	restored.AccountPresentation.WalletLabels[data.Accounts[0].ID] = "changed"
	if VerifyRecovery(data, restored) == nil {
		t.Fatal("recovery comparison ignored presentation")
	}
	var wire map[string]json.RawMessage
	if json.Unmarshal(plain, &wire) != nil {
		t.Fatal("decode fixture")
	}
	wire["accountPresentation"] = json.RawMessage(`{"groupLabels":{"00":"not canonical"}}`)
	bad, _ := json.Marshal(wire)
	if invalid, err := Decode(bad); err == nil {
		invalid.Destroy()
		t.Fatal("malformed metadata opened")
	}
}
