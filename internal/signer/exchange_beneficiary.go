package signer

import (
	"fmt"
	"localwallet/internal/exchange/bybit"
)

func (e *Engine) exchangeUAEBeneficiary() (*bybit.UAEBeneficiary, error) {
	if !e.canAuthorize() || e.data.Exchange == nil || e.data.Exchange.Credentials == nil || e.data.Exchange.Credentials.Endpoint != "https://api.bybit.ae" {
		return nil, ErrDenied
	}
	if !e.UI.Confirm("Bybit UAE withdrawal information", "Bybit UAE requires beneficiary identity and address information for this withdrawal. The next two masked wallet dialogs collect eight required fields for your own noncustodial wallet. Your data remains encrypted in the local operation journal and is sent only to https://api.bybit.ae after the final exact withdrawal approval and app-password check. It is not shared with the portfolio, analytics, blockchain recipient or another service. Cancelling sends no withdrawal. Enter your own accurate information; the wallet cannot bypass Bybit's checks.") || !e.canAuthorize() {
		return nil, ErrDenied
	}
	first, err := e.UI.Input("Bybit UAE beneficiary — 1 of 2", "Country — ISO 3166 alpha-3, for example ARE", "State or emirate", "City", "Building address")
	if err != nil {
		return nil, err
	}
	defer wipeInputs(first)
	if len(first) != 4 || !e.canAuthorize() {
		return nil, ErrDenied
	}
	second, err := e.UI.Input("Bybit UAE beneficiary — 2 of 2", "Street address", "Postal code", "Date of birth — YYYY-MM-DD", "Place of birth")
	if err != nil {
		return nil, err
	}
	defer wipeInputs(second)
	if len(second) != 4 || !e.canAuthorize() {
		return nil, ErrDenied
	}
	b := &bybit.UAEBeneficiary{WalletType: "1", UnhostedWalletType: "0", LegalType: "individual", Country: string(first[0]), State: string(first[1]), City: string(first[2]), Building: string(first[3]), Street: string(second[0]), PostalCode: string(second[1]), DateOfBirth: string(second[2]), PlaceOfBirth: string(second[3])}
	if bybit.ValidateUAEBeneficiary(*b) != nil {
		return nil, ErrDenied
	}
	return b, nil
}
func exchangeBeneficiarySummary(b *bybit.UAEBeneficiary) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("\nBeneficiary data sent only to Bybit UAE (https://api.bybit.ae):\nWallet ownership: your own individual noncustodial wallet\nCountry: %s\nState/emirate: %s\nCity: %s\nBuilding: %s\nStreet: %s\nPostal code: %s\nDate of birth: %s\nPlace of birth: %s\n", b.Country, b.State, b.City, b.Building, b.Street, b.PostalCode, b.DateOfBirth, b.PlaceOfBirth)
}
