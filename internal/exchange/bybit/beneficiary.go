package bybit

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// UAEBeneficiary implements only the legacy UAE fields expressly documented in
// https://bybit-exchange.github.io/docs/v5/asset/withdraw. The newer questionnaire
// page does not publish a UAE schema. No guessed questionnaire is sent.
// This wallet permits only an individual withdrawing to their own noncustodial
// wallet. Each field is private information intended only for the chosen UAE
// Bybit endpoint and the isolated signer-authored approval prompt.
type UAEBeneficiary struct {
	WalletType         string `json:"beneficiaryWalletType"`
	UnhostedWalletType string `json:"beneficiaryUnhostedWalletType"`
	LegalType          string `json:"beneficiaryLegalType"`
	Country            string `json:"beneficiaryAddressCountry"`
	State              string `json:"beneficiaryAddressState"`
	City               string `json:"beneficiaryAddressCity"`
	Building           string `json:"beneficiaryAddressBuilding"`
	Street             string `json:"beneficiaryAddressStreet"`
	PostalCode         string `json:"beneficiaryAddressPostalCode"`
	DateOfBirth        string `json:"beneficiaryDateOfBirth"`
	PlaceOfBirth       string `json:"beneficiaryPlaceOfBirth"`
}

func beneficiaryText(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, c := range value {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func ValidateUAEBeneficiary(b UAEBeneficiary) error {
	if b.WalletType != "1" || b.UnhostedWalletType != "0" || b.LegalType != "individual" || len(b.Country) != 3 || !beneficiaryText(b.State, 128) || !beneficiaryText(b.City, 128) || !beneficiaryText(b.Building, 256) || !beneficiaryText(b.Street, 256) || !beneficiaryText(b.PostalCode, 32) || !beneficiaryText(b.PlaceOfBirth, 128) {
		return ErrInvalidRequest
	}
	region, err := language.ParseRegion(b.Country)
	if err != nil || !region.IsCountry() || region.ISO3() != b.Country {
		return ErrInvalidRequest
	}
	dob, err := time.Parse("2006-01-02", b.DateOfBirth)
	if err != nil || len(b.DateOfBirth) != 10 || dob.Year() < 1900 || dob.After(time.Now().UTC()) {
		return ErrInvalidRequest
	}
	return nil
}
