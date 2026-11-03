package walletcore

import (
	"errors"
	"strconv"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
)

// The field is a fixed validation category. Authenticated provider values and
// response bodies never become diagnostics, including when an unknown category
// is accidentally passed by a future caller.
type exchangeDepositCreditError struct{ Field string }

func (e *exchangeDepositCreditError) Error() string {
	return "Bybit deposit credit verification failed"
}
func (e *exchangeDepositCreditError) Unwrap() error { return bybit.ErrInvalidResponse }

func exchangeDepositCreditReason(code string) string {
	stage, cause, ok := ipc.ExchangeReadErrorParts(code)
	if !ok || stage != "DEPOSIT_CREDIT" {
		return ""
	}
	return map[string]string{
		"DEPOSIT_RECORD_REVIEW_NULL":   "Bybit returned null instead of a Travel Rule review result. The original deposit's review has not been classified.",
		"DEPOSIT_RECORD_REVIEW_EMPTY":  "Bybit returned an empty Travel Rule review result. The original deposit's review has not been classified.",
		"DEPOSIT_RECORD_REVIEW_TYPE":   "Bybit's Travel Rule result is neither an integer nor a canonical numeric string.",
		"DEPOSIT_RECORD_REVIEW_VALUE":  "Bybit returned an unknown Travel Rule review state. It was not treated as approved.",
		"DEPOSIT_RECORD_REVIEW_SHADOW": "Bybit returned conflicting spellings of its Travel Rule review field.",
		"DEPOSIT_RECORD_RESPONSE":      "Bybit's deposit response has an invalid JSON envelope or result.",
		"DEPOSIT_RECORD_PAGE":          "Bybit's deposit response has a missing or invalid record list.",
		"DEPOSIT_RECORD_CURSOR":        "Bybit's deposit response has an invalid or repeated continuation cursor.",
		"DEPOSIT_RECORD_ID":            "Bybit's deposit record has a missing, invalid or duplicate record ID.",
		"DEPOSIT_RECORD_COIN":          "Bybit's deposit record has a missing, invalid or different asset identity.",
		"DEPOSIT_RECORD_TRANSACTION":   "Bybit's deposit record has a missing, invalid or different transaction identity.",
		"DEPOSIT_RECORD_CHAIN":         "Bybit's deposit record has a missing or invalid network identifier.",
		"DEPOSIT_RECORD_AMOUNT":        "Bybit's deposit record has a missing or invalid amount.",
		"DEPOSIT_RECORD_ADDRESS":       "Bybit's deposit record has a missing or invalid receiving address.",
		"DEPOSIT_RECORD_SOURCE":        "Bybit's deposit record has an invalid sender-address field.",
		"DEPOSIT_RECORD_MEMO":          "Bybit's deposit record has a missing or invalid memo field.",
		"DEPOSIT_RECORD_STATUS":        "Bybit's deposit record has a missing or invalid credit status.",
		"DEPOSIT_RECORD_FEE":           "Bybit's deposit record has an invalid reported fee.",
		"DEPOSIT_RECORD_TIME":          "Bybit's deposit record has an invalid success-time field.",
		"DEPOSIT_RECORD_CONFIRMATIONS": "Bybit's deposit record has an invalid confirmation-count field.",
		"DEPOSIT_RECORD_TYPE":          "Bybit's deposit record has a missing or invalid deposit-type field.",
		"DEPOSIT_RECORD_REVIEW":        "Bybit's deposit record has an invalid Travel Rule review field.",
		"CREDIT_TRANSACTION":           "The confirmed TON receipt has no checked transaction hash suitable for Bybit credit matching.",
		"CREDIT_COIN":                  "Bybit's matching deposit record names a different asset.",
		"CREDIT_CHAIN":                 "Bybit's matching deposit record names a different network.",
		"CREDIT_ADDRESS":               "Bybit's matching deposit record names a different receiving address from the signed deposit.",
		"CREDIT_SOURCE":                "Bybit's matching deposit record names a different sender from the checked original wallet.",
		"CREDIT_MEMO":                  "Bybit's matching deposit record has a different memo from the signed deposit.",
		"CREDIT_DUPLICATE":             "Bybit returned multiple records for the same checked deposit transaction.",
		"CREDIT_PAGINATION":            "Bybit's deposit record pages repeated or exceeded the bounded verification limit.",
		"CREDIT_AMOUNT":                "Bybit's matching deposit amount is invalid or exceeds the signed original deposit.",
		"CREDIT_FEE":                   "Bybit's reported deposit fee is invalid or exceeds the verified deposit amount.",
	}[cause]
}

// Called only before a wallet payment or exchange write is authorized. Keep
// the failing read and numeric service reason without forwarding error text.
func exchangeReadFailure(stage string, err error) error {
	code := publicRequestErrorCode(err)
	if code == "REQUEST_CANCELLED" {
		return ipc.NewRequestError(code)
	}
	var api *bybit.APIError
	var metadata *bybit.CoinMetadataError
	var record *bybit.DepositRecordError
	var credit *exchangeDepositCreditError
	if stage == "COIN" && errors.As(err, &metadata) && metadata != nil {
		code = "METADATA_" + metadata.Field
	}
	if stage == "DEPOSIT_CREDIT" && errors.As(err, &record) && record != nil {
		switch record.Field {
		case "RESPONSE", "PAGE", "CURSOR", "ID", "COIN", "TRANSACTION", "CHAIN", "AMOUNT", "ADDRESS", "SOURCE", "MEMO", "STATUS", "FEE", "TIME", "CONFIRMATIONS", "TYPE", "REVIEW":
			code = "DEPOSIT_RECORD_" + record.Field
			if record.Field == "REVIEW" {
				switch record.Reason {
				case "NULL", "EMPTY", "TYPE", "VALUE", "SHADOW":
					code += "_" + record.Reason
				}
			}
		}
	}
	if stage == "DEPOSIT_CREDIT" && errors.As(err, &credit) && credit != nil {
		switch credit.Field {
		case "TRANSACTION", "COIN", "CHAIN", "ADDRESS", "SOURCE", "MEMO", "DUPLICATE", "PAGINATION", "AMOUNT", "FEE":
			code = "CREDIT_" + credit.Field
		}
	}
	if errors.As(err, &api) && api != nil && api.HTTPStatus == 0 && api.Code != 0 {
		code = "BYBIT_" + strconv.FormatInt(api.Code, 10)
	}
	return ipc.NewExchangeReadError(stage, code)
}
