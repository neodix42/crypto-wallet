package nativewallet

import (
	"fmt"
	"strings"

	"localwallet/internal/amount"
	"localwallet/internal/chains"
	"localwallet/internal/model"
)

type paymentInputValidation struct {
	RecipientValid, AmountValid bool
	RecipientError, AmountError string
}

// Payment fields are checked locally with the same address validators and
// bounded decimal parser used by the chain adapters. This provides immediate
// feedback without querying a provider or granting preparation/signing authority.
// Compatible address syntax does not prove ownership, existence or funding;
// several chains deliberately share the same address representation.
func (a *App) paymentValidation(account model.Account, draft model.Transfer) paymentInputValidation {
	var result paymentInputValidation
	if draft.AccountID != account.ID {
		result.RecipientError = "The payment no longer belongs to this wallet."
		result.AmountError = result.RecipientError
		return result
	}
	result.RecipientValid = paymentRecipientValid(account, draft.Recipient)
	if draft.Recipient != "" && !result.RecipientValid {
		result.RecipientError = "Enter a valid recipient address for " + a.network(account.Network).Name + "."
	}
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID != draft.Asset {
			continue
		}
		atomic, err := amount.Parse(draft.Amount, asset.Decimals)
		result.AmountValid = err == nil && atomic != "0"
		if draft.Amount != "" && !result.AmountValid {
			switch {
			case err == nil:
				result.AmountError = "Enter an amount greater than zero."
			case asset.Decimals == 0:
				result.AmountError = "Enter a positive whole number."
			default:
				result.AmountError = fmt.Sprintf("Enter a positive decimal amount with at most %d decimal places.", asset.Decimals)
			}
		}
		return result
	}
	result.AmountError = "Select an available asset for this wallet."
	return result
}

func paymentRecipientValid(account model.Account, recipient string) bool {
	if recipient == "" || len(recipient) > 256 || strings.TrimSpace(recipient) != recipient {
		return false
	}
	for _, adapter := range chains.All() {
		for _, network := range adapter.Networks() {
			if network.ID == account.Network {
				return adapter.ValidateAddress(account.Network, recipient) == nil
			}
		}
	}
	return false
}
