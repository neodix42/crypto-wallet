package signer

import (
	"localwallet/internal/exchange"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
)

// CancelExchangeConfirmed cancels only an original request which has never
// released a POST signature. The exact signer-authored operation is reviewed
// first; neither provider observations nor navigation can cancel an unknown
// outcome or create a replacement.
func (e *Engine) CancelExchangeConfirmed(id string) error {
	x, err := e.exchangeData()
	if err != nil {
		return err
	}
	o, err := e.exchangeOperation(id)
	credential := x.Credential(x.Selected())
	if err != nil || credential == nil || o.State != "PREPARED" && o.State != "AUTHORIZED" || o.CredentialID != vault.ExchangeCredentialID(credential) || o.Endpoint != credential.Endpoint || o.Digest != vault.ExchangeDigest(*o) {
		return ErrDenied
	}
	provider, known := exchange.ProviderForEndpoint(o.Endpoint)
	if !known || provider.ID != x.Selected() {
		return ErrDenied
	}
	account, asset, err := e.exchangeAsset(o.AccountID, o.AssetID)
	if err != nil {
		return err
	}
	summary, err := exchangeApproval(*o, account, asset)
	if err != nil {
		return err
	}
	data, generation, digest := e.data, e.data.Generation, o.Digest
	if !e.UI.Confirm("Cancel original unsigned "+provider.Name+" operation", summary+"\n\nCancel this original unsigned request? Its record is retained. No network request or replacement operation is sent.") {
		return uiprompt.ErrCancelled
	}
	if !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	current, err := e.exchangeOperation(id)
	credential = e.data.Exchange.Credential(e.data.Exchange.Selected())
	if err != nil || credential == nil || current.Digest != digest || current.Digest != vault.ExchangeDigest(*current) || current.CredentialID != vault.ExchangeCredentialID(credential) || current.Endpoint != credential.Endpoint || current.State != "PREPARED" && current.State != "AUTHORIZED" {
		return ErrDenied
	}
	return e.CancelExchange(id)
}
