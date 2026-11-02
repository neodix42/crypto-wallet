package vault

import (
	"bytes"
	"strings"
	"testing"

	"localwallet/internal/exchange"
)

func TestExchangeWizardEncryptedRoundtripRecoveryAndOptionalCompatibility(t *testing.T) {
	d := exchangeVaultFixture()
	defer d.Destroy()
	d.Exchange.Wizard = &exchange.WizardState{ID: strings.Repeat("b", 32), Step: 1, SourceAccount: "a", SourceAsset: "near-mainnet:native", DepositAmount: "1.25"}
	d.Exchange.WizardCredentialID = ExchangeCredentialID(d.Exchange.Credentials)
	originalDigest := d.Exchange.Operations[0].Digest
	raw, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	cipher, err := Seal(raw, testPassword, testParams)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte(d.Exchange.Wizard.ID)) || bytes.Contains(cipher, []byte("sourceAccount")) {
		t.Fatal("wizard escaped encrypted storage")
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
	defer restored.Destroy()
	if restored.Exchange.Wizard == nil || *restored.Exchange.Wizard != *d.Exchange.Wizard || VerifyRecovery(d, restored) != nil || restored.Exchange.Operations[0].Digest != originalDigest {
		t.Fatal("wizard recovery or immutable old operation changed")
	}
	restored.Exchange.Wizard.DepositAmount = "2"
	if VerifyRecovery(d, restored) == nil {
		t.Fatal("recovery ignored wizard corruption")
	}
	for _, mutate := range []func(*Data){
		func(d *Data) { d.Exchange.Wizard.SourceAccount = "foreign" }, func(d *Data) { d.Exchange.Wizard.TradeAccount = "foreign" },
		func(d *Data) { d.Exchange.WizardCredentialID = "" }, func(d *Data) { d.Exchange.Wizard.Step = 4 },
		func(d *Data) { d.Exchange.Wizard = nil },
	} {
		copy, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		mutate(copy)
		_, err = copy.Encode()
		copy.Destroy()
		if err == nil {
			t.Fatal("invalid wizard storage admitted")
		}
	}
	d.Exchange.Wizard = nil
	d.Exchange.WizardCredentialID = ""
	legacy, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(legacy, []byte(`"wizard"`)) || ExchangeDigest(d.Exchange.Operations[0]) != originalDigest {
		t.Fatal("optional extension changed old schema/digest")
	}
}
