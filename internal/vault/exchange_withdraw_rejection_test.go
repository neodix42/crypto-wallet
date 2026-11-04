package vault

import "testing"

func TestBybitWithdrawalRefusalCodesHaveExactPersistedOperationScope(t *testing.T) {
	for _, code := range []int64{131093, 131098, 131089} {
		if !ExchangeRejectionAllowed(code) || !ExchangeOperationRejectionAllowed("withdraw", code) || ExchangeOperationRejectionAllowed("swap", code) || ExchangeOperationRejectionAllowed("transfer", code) {
			t.Fatal("withdrawal refusal escaped its operation scope", code)
		}
		for _, kind := range []string{"withdraw", "swap", "transfer"} {
			d := exchangeVaultFixture()
			op := &d.Exchange.Operations[0]
			op.Kind, op.State, op.RejectionCode = kind, "FAILED", code
			op.Digest = ExchangeDigest(*op)
			_, err := d.Encode()
			if (err == nil) != (kind == "withdraw") {
				t.Fatal("persisted refusal did not authenticate exact withdrawal scope", kind, code, err)
			}
		}
	}
	for _, code := range []int64{10001, 10003, 10004, 10005} {
		for _, kind := range []string{"withdraw", "swap", "transfer"} {
			if !ExchangeOperationRejectionAllowed(kind, code) {
				t.Fatal("legacy definitive authentication refusal changed", kind, code)
			}
		}
	}
	for _, code := range []int64{10000, 10002, 10006, 10014, 131085, 131090, 131094, 131097, 131099, 131214, 429} {
		if ExchangeRejectionAllowed(code) || ExchangeOperationRejectionAllowed("withdraw", code) {
			t.Fatal("unknown or ambiguous code became definitive", code)
		}
	}
}
