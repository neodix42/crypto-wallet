package bybit

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestDepositAddressRequiresExplicitMemoFieldAndAllowsDocumentedEmptyMemo(t *testing.T) {
	const result = `{"coin":"GRAM","chains":[{"chain":"TON","chainType":"TON","addressDeposit":"EQpublicSyntheticAddress","tagDeposit":"","contractAddress":"","batchReleaseLimit":"-1"}]}`
	deposit, err := testClient(t, result).DepositAddress(context.Background(), "GRAM", "TON")
	if err != nil || !deposit.MemoPresent || deposit.Tag != "" {
		t.Fatal("explicit no-memo address rejected", err)
	}
	for _, bad := range []string{strings.Replace(result, `"tagDeposit":"",`, "", 1), strings.Replace(result, `"tagDeposit":""`, `"tagDeposit":null`, 1), strings.Replace(result, `"tagDeposit":""`, `"tagDeposit":123`, 1)} {
		if _, err := testClient(t, bad).DepositAddress(context.Background(), "GRAM", "TON"); err == nil {
			t.Fatal("missing, null or non-string memo accepted")
		}
	}
}

func TestDepositCreditTransactionHashNormalizesOnlyCanonicalSameBinaryIdentity(t *testing.T) {
	raw := []byte{255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251, 255, 251}
	canonical := hex.EncodeToString(raw)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		encoded := encoding.EncodeToString(raw)
		if !SameDepositTransactionHash(canonical, encoded) || !SameDepositTransactionHash(encoded, canonical) {
			t.Fatal("canonical representation of the same 32-byte transaction rejected")
		}
		if SameDepositTransactionHash(canonical, encoded+"\n") || SameDepositTransactionHash(canonical, encoded+"==") {
			t.Fatal("noncanonical hash representation accepted")
		}
	}
	if SameDepositTransactionHash(canonical, strings.Repeat("11", 32)) || SameDepositTransactionHash(canonical, "external-message") || SameDepositTransactionHash("", "") {
		t.Fatal("foreign or empty deposit identity accepted")
	}
}

func TestDepositCreditRequiresDocumentedRestrictionStateAndCanonicalTransactionIdentity(t *testing.T) {
	const result = `{"rows":[{"id":"original","coin":"GRAM","chain":"TON","amount":"1","txID":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","status":3,"toAddress":"EQpublicSyntheticAddress","tag":"","depositType":"0","travelRuleStatus":0}],"nextPageCursor":""}`
	page, err := testClient(t, result).DepositRecords(context.Background(), "GRAM", strings.Repeat("00", 32), "")
	if err != nil || len(page.Rows) != 1 {
		t.Fatal("same 32-byte TON transaction rejected", err)
	}
	if !page.Rows[0].TravelRuleStatusPresent {
		t.Fatal("explicit travel review state was not retained")
	}
	// The official response example omits travelRuleStatus; its field
	// description says non-compliance areas can ignore it. Missing is distinct
	// from a reported null/invalid state, which must not become approved.
	withoutTravel := strings.Replace(result, `,"travelRuleStatus":0`, "", 1)
	page, err = testClient(t, withoutTravel).DepositRecords(context.Background(), "GRAM", strings.Repeat("00", 32), "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].TravelRuleStatusPresent {
		t.Fatal("documented non-compliance response rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(result, `"travelRuleStatus":0`, `"travelRuleStatus":null`, 1),
		strings.Replace(result, `"travelRuleStatus":0`, `"travelRuleStatus":"0"`, 1),
		strings.Replace(result, `"travelRuleStatus":0`, `"travelRuleStatus":4`, 1),
		strings.Replace(result, `"tag":"",`, "", 1), strings.Replace(result, `"depositType":"0",`, "", 1),
		strings.Replace(result, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "EREREREREREREREREREREREREREREREREREREREREREQ=", 1),
	} {
		if _, err := testClient(t, bad).DepositRecords(context.Background(), "GRAM", strings.Repeat("00", 32), ""); err == nil {
			t.Fatal("missing credit evidence or foreign transaction accepted")
		}
	}
}
