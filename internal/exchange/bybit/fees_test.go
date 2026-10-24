package bybit

import (
	"encoding/json"
	"strings"
	"testing"
)

// This public fixture is copied from the primary execution-stream example:
// https://bybit-exchange.github.io/docs/v5/websocket/private/execution .
const officialExtraFeeExample = `[{"feeCoin":"USDT","feeType":"GST","subFeeType":"IND_GST","feeRate":"0.0000675","fee":"0.006403779"}]`

func TestExecutionExtraFeesIndependentOfficialRESTAndStreamForms(t *testing.T) {
	encoded, _ := json.Marshal(officialExtraFeeExample)
	for _, raw := range []json.RawMessage{json.RawMessage(officialExtraFeeExample), encoded} {
		fees, err := ExecutionExtraFees(raw)
		if err != nil || len(fees) != 1 || fees[0].FeeCoin != "USDT" || fees[0].FeeType != "GST" || fees[0].SubFeeType != "IND_GST" || fees[0].FeeRate != "0.0000675" || fees[0].Fee != "0.006403779" {
			t.Fatal("independent published fee fixture changed")
		}
	}
	for _, raw := range []string{`""`, `[]`, `"[]"`, ` [ ] `} {
		fees, err := ExecutionExtraFees(json.RawMessage(raw))
		if err != nil || fees == nil || len(fees) != 0 {
			t.Fatal("explicit documented empty fees rejected")
		}
	}
	pairs := [][2]string{{"VAT", "ARE_VAT"}, {"GST", "IND_GST"}, {"TAX", "TAX_PNN"}, {"TAX", "TAX_PPH"}, {"CFX", "CFX_FIEE"}, {"WHT", "AUT_WITHHOLDING_TAX"}}
	list := []ExtraFee{}
	for _, pair := range pairs {
		list = append(list, ExtraFee{FeeCoin: "USDT", FeeType: pair[0], SubFeeType: pair[1], FeeRate: "0.05", Fee: "0.123456789123456789"})
	}
	raw, _ := json.Marshal(list)
	fees, err := ExecutionExtraFees(raw)
	if err != nil || len(fees) != 6 {
		t.Fatal("known tax fee enum pair lost")
	}
	for _, fee := range fees {
		if fee.Fee != "0.123456789123456789" {
			t.Fatal("tax precision changed")
		}
	}
}

func TestExecutionExtraFeesRejectsMissingNullUnknownDuplicateAndMalformedFees(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage{}, json.RawMessage(`null`), json.RawMessage(`"null"`), json.RawMessage(` `), json.RawMessage(`" "`), json.RawMessage(`{}`), json.RawMessage(`true`), json.RawMessage(`1`), json.RawMessage(`"["`), json.RawMessage(`[null]`), json.RawMessage(`[{}]`), json.RawMessage(`"unterminated`), json.RawMessage(`[] false`), json.RawMessage(string([]byte{'[', 255, ']'})), json.RawMessage(strings.Repeat(" ", 4097))} {
		if out, err := ExecutionExtraFees(raw); err != ErrInvalidResponse || out != nil {
			t.Fatal("missing/ambiguous fees treated as zero")
		}
	}
	for _, bad := range []string{
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":null`, 1),
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":0.006403779`, 1),
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":"NaN"`, 1),
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":"-0.1"`, 1),
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":"1e-5"`, 1),
		strings.Replace(officialExtraFeeExample, `"fee":"0.006403779"`, `"fee":"0.1","fee":"0.2"`, 1),
		strings.Replace(officialExtraFeeExample, `,"fee":"0.006403779"`, `,"other":"0.006403779"`, 1),
		strings.Replace(officialExtraFeeExample, `,"fee":"0.006403779"`, ``, 1),
		strings.Replace(officialExtraFeeExample, `"feeCoin":"USDT"`, `"feeCoin":"usdt"`, 1),
		strings.Replace(officialExtraFeeExample, `"feeType":"GST"`, `"feeType":"UNKNOWN"`, 1),
		strings.Replace(officialExtraFeeExample, `"subFeeType":"IND_GST"`, `"subFeeType":"ARE_VAT"`, 1),
		strings.Replace(officialExtraFeeExample, `"feeRate":"0.0000675"`, `"feeRate":null`, 1),
		strings.Replace(officialExtraFeeExample, `"feeRate":"0.0000675"`, `"feeRate":"NaN"`, 1),
		strings.Replace(officialExtraFeeExample, `"feeRate":"0.0000675"`, `"feeRate":"1.000000000000000001"`, 1),
		strings.TrimSuffix(officialExtraFeeExample, "]") + "," + strings.TrimPrefix(officialExtraFeeExample, "["),
	} {
		for _, raw := range []json.RawMessage{json.RawMessage(bad), mustFeeString(t, bad)} {
			if fees, err := ExecutionExtraFees(raw); err != ErrInvalidResponse || fees != nil {
				t.Fatal("unknown/incomplete fee accepted")
			}
		}
	}
	list := []ExtraFee{}
	for i := 0; i < 17; i++ {
		list = append(list, ExtraFee{FeeCoin: "USDT", FeeType: "GST", SubFeeType: "IND_GST", FeeRate: "0.05", Fee: "0"})
	}
	raw, _ := json.Marshal(list)
	if fees, err := ExecutionExtraFees(raw); err != ErrInvalidResponse || fees != nil {
		t.Fatal("unbounded tax count accepted")
	}
}
func mustFeeString(t *testing.T, value string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
