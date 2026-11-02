package bybit

import (
	"context"
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

func TestDepositCreditRequiresExplicitRestrictionStateAndCanonicalTransactionIdentity(t *testing.T) {
	const result = `{"rows":[{"id":"original","coin":"GRAM","chain":"TON","amount":"1","txID":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=","status":3,"toAddress":"EQpublicSyntheticAddress","tag":"","depositType":"0","travelRuleStatus":0}],"nextPageCursor":""}`
	page, err := testClient(t, result).DepositRecords(context.Background(), "GRAM", strings.Repeat("00", 32), "")
	if err != nil || len(page.Rows) != 1 {
		t.Fatal("same 32-byte TON transaction rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(result, `,"travelRuleStatus":0`, "", 1), strings.Replace(result, `"travelRuleStatus":0`, `"travelRuleStatus":null`, 1),
		strings.Replace(result, `"tag":"",`, "", 1), strings.Replace(result, `"depositType":"0",`, "", 1),
		strings.Replace(result, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "EREREREREREREREREREREREREREREREREREREREREREQ=", 1),
	} {
		if _, err := testClient(t, bad).DepositRecords(context.Background(), "GRAM", strings.Repeat("00", 32), ""); err == nil {
			t.Fatal("missing credit evidence or foreign transaction accepted")
		}
	}
}
