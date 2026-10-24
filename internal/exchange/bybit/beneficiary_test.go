package bybit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testUAEBeneficiary() UAEBeneficiary {
	return UAEBeneficiary{WalletType: "1", UnhostedWalletType: "0", LegalType: "individual", Country: "ARE", State: "PUBLIC-TEST-STATE", City: "PUBLIC-TEST-CITY", Building: "PUBLIC-TEST-BUILDING-7931", Street: "PUBLIC-TEST-STREET-1842", PostalCode: "00000", DateOfBirth: "1990-02-15", PlaceOfBirth: "PUBLIC-TEST-BIRTHPLACE"}
}
func TestExchangeUAEFieldsFollowDocumentedLegacyWireContract(t *testing.T) {
	b := testUAEBeneficiary()
	if ValidateUAEBeneficiary(b) != nil {
		t.Fatal("valid synthetic beneficiary")
	}
	w := WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: "0x1111111111111111111111111111111111111111", Amount: "1", Timestamp: time.Now().UnixMilli(), ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: strings.Repeat("a", 32), Beneficiary: &b}
	raw, _ := json.Marshal(w)
	r := AuthRequest{Endpoint: "https://api.bybit.ae", Method: "POST", Path: "/v5/asset/withdraw/create", Payload: string(raw), TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	if ValidateAuthRequest(r) != nil {
		t.Fatal("documented UAE withdrawal rejected")
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(raw, &fields)
	var beneficiary map[string]any
	json.Unmarshal(fields["beneficiary"], &beneficiary)
	for _, key := range []string{"beneficiaryAddressCountry", "beneficiaryAddressState", "beneficiaryAddressCity", "beneficiaryAddressBuilding", "beneficiaryAddressStreet", "beneficiaryAddressPostalCode", "beneficiaryDateOfBirth", "beneficiaryPlaceOfBirth"} {
		if _, ok := beneficiary[key]; !ok {
			t.Fatal("documented wire field missing")
		}
	}
	if len(beneficiary) != 11 || fields["questionnaire"] != nil {
		t.Fatal("invented or unrelated travel-rule schema")
	}
	if _, err := HMAC("PUBLIC_TEST_ONLY_KEY", []byte("PUBLIC_TEST_ONLY_SECRET_12345"), r); err != nil {
		t.Fatal("narrow typed UAE request not signable")
	}
	r.Endpoint = "https://api.bybit.com"
	if ValidateAuthRequest(r) == nil {
		t.Fatal("UAE beneficiary disclosed to other endpoint")
	}
	w.Beneficiary = nil
	raw, _ = json.Marshal(w)
	r.Payload = string(raw)
	r.Endpoint = "https://api.bybit.ae"
	if ValidateAuthRequest(r) == nil {
		t.Fatal("UAE mandatory fields omitted")
	}
	r.Endpoint = "https://api.bybit.com"
	if ValidateAuthRequest(r) != nil {
		t.Fatal("global withdrawal forced to send UAE PII")
	}
	w.Beneficiary = &b
	raw, _ = json.Marshal(w)
	r.Endpoint = "https://api.bybit.ae"
	for _, bad := range []string{strings.Replace(string(raw), `"beneficiaryAddressCountry":"ARE"`, `"beneficiaryAddressCountry":"ARE","arbitraryURL":"https://attacker.invalid"`, 1), strings.Replace(string(raw), `"beneficiaryAddressCountry":"ARE"`, `"beneficiaryAddressCountry":null`, 1), strings.Replace(string(raw), `"beneficiaryWalletType":"1"`, `"beneficiaryWalletType":1`, 1), strings.Replace(string(raw), `"beneficiary":{`, `"beneficiary":{"beneficiaryAddressCountry":"ARE",`, 1)} {
		r.Payload = bad
		if ValidateAuthRequest(r) == nil {
			t.Fatal("ambiguous/untyped beneficiary accepted")
		}
	}
}
func TestExchangeUAEValidationRejectsMalformedPrivateFields(t *testing.T) {
	for _, mutate := range []func(*UAEBeneficiary){func(b *UAEBeneficiary) { b.WalletType = "0" }, func(b *UAEBeneficiary) { b.UnhostedWalletType = "1" }, func(b *UAEBeneficiary) { b.LegalType = "company" }, func(b *UAEBeneficiary) { b.Country = "AE" }, func(b *UAEBeneficiary) { b.Country = "are" }, func(b *UAEBeneficiary) { b.Country = "ZZZ" }, func(b *UAEBeneficiary) { b.Country = "001" }, func(b *UAEBeneficiary) { b.State = "" }, func(b *UAEBeneficiary) { b.City = "<script>\x00</script>" }, func(b *UAEBeneficiary) { b.Building = strings.Repeat("x", 257) }, func(b *UAEBeneficiary) { b.Street = "leading space " }, func(b *UAEBeneficiary) { b.PostalCode = strings.Repeat("1", 33) }, func(b *UAEBeneficiary) { b.DateOfBirth = "1990-02-30" }, func(b *UAEBeneficiary) { b.DateOfBirth = "1990-2-15" }, func(b *UAEBeneficiary) { b.DateOfBirth = "2100-01-01" }, func(b *UAEBeneficiary) { b.DateOfBirth = "1800-01-01" }, func(b *UAEBeneficiary) { b.PlaceOfBirth = string([]byte{255}) }} {
		b := testUAEBeneficiary()
		mutate(&b)
		if ValidateUAEBeneficiary(b) == nil {
			t.Fatal("malformed beneficiary accepted")
		}
		w := WithdrawalRequest{Beneficiary: &b}
		if ValidateWithdrawal(w) == nil {
			t.Fatal("malformed beneficiary withdrawal accepted")
		}
	}
}
