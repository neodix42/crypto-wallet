package nativewallet

import (
	"fmt"
	"localwallet/internal/derivation"
	"localwallet/internal/ipc"
	"localwallet/internal/webui"
	"strconv"
	"strings"
	"unicode/utf8"
)

func recoveryRange(from, count string) (uint32, uint32, bool) {
	start, e1 := strconv.ParseUint(from, 10, 32)
	number, e2 := strconv.ParseUint(count, 10, 32)
	if e1 != nil || e2 != nil || strconv.FormatUint(start, 10) != from || strconv.FormatUint(number, 10) != count || number == 0 || number > 20 || start >= uint64(derivation.MaxIndex) || start+number > uint64(derivation.MaxIndex) {
		return 0, 0, false
	}
	return uint32(start), uint32(start + number - 1), true
}

func (a *App) reportError(operation string, err error) {
	code := ipc.ClassifyErrorCode(err)
	if code == "REQUEST_CANCELLED" {
		a.notice("Request cancelled.")
		return
	}
	a.queueError(operation, code, safeError(err))
}
func (a *App) errorText(operation, message string) {
	a.queueError(operation, "REQUEST_DENIED", message)
}
func (a *App) queueError(operation, code, message string) {
	if a.closed {
		return
	}
	code = ipc.SafeErrorCode(code)
	// These are backend-authored public strings, never err.Error(), provider
	// bodies, entered secrets or credential-bearing URLs.
	if len(operation) > 256 || !utf8.ValidString(operation) || strings.ContainsRune(operation, 0) {
		operation = "Wallet operation"
	}
	if len(message) == 0 || len(message) > 8192 || !utf8.ValidString(message) || strings.ContainsRune(message, 0) {
		message = "The operation failed without a bounded public diagnostic. Check its original state before retrying."
	}
	a.status = message
	a.statusIsError = true
	a.statusRevision++
	// Count every report, including a deduplicated one, so custom operation
	// feedback suppresses the generic fallback for that same failure.
	a.errorSequence++
	for _, old := range a.errorQueue {
		if old.Operation == operation && old.Code == code && old.Message == message {
			if a.ui != nil {
				a.render()
			}
			return
		}
	}
	notice := webui.ErrorNotice{ID: strconv.FormatUint(a.errorSequence, 10), Title: "Operation failed", Operation: operation, Code: code, Message: message}
	if len(a.errorQueue) < 32 {
		a.errorQueue = append(a.errorQueue, notice)
	} else {
		// Foreground actions are blocked until acknowledgement, so only a burst of
		// already-running callbacks can reach this bounded overflow notice.
		notice.Message = "Additional failures occurred while error details were open. Most recent reason:\n" + message
		if len(notice.Message) > 8192 {
			notice.Message = "Additional failures occurred while error details were open. The newest diagnostic exceeded the display budget. Code: " + code + ". Dismiss the earlier errors, then inspect the selected operation again."
		}
		a.errorQueue[len(a.errorQueue)-1] = notice
	}
	if a.ui != nil {
		a.render()
	}
}

func detailedError(code string) (string, bool) {
	if reason, ok := exchangeReadReason(code); ok {
		return reason, true
	}
	messages := map[string]string{
		"EXCHANGE_DEPOSIT_DESTINATION_CHANGED": "Bybit's deposit address or memo changed after the displayed preview. Refresh the selected network's deposit information and review the new destination. No payment was prepared or sent.",
		"EXCHANGE_WIZARD_PERMISSIONS":          "This four-step workflow needs Bybit Spot Trade, Wallet Account Transfer and Withdrawal permissions, plus a supported master-account region. The wallet refused to deposit because the remaining steps cannot currently be completed with this key. Existing funds remain available through the account controls.",
		"EXCHANGE_WIZARD_STALE":                "The saved exchange workflow no longer matches this session or API connection. Refresh the exchange state. Original operations remain in encrypted storage; do not repeat an uncertain payment.",
		"EXCHANGE_DEPOSIT_PENDING":             "The original deposit is not yet verified as credited, or its Funding transfer already exists. Check the original operation instead of creating another transfer.",
		"EXCHANGE_UNIFIED_REQUIRED":            "Funding-to-Unified transfers and this spot workflow require a Bybit Unified Trading Account. The API key's account is not reported as Unified.",
		"EXCHANGE_TRANSFER_PERMISSION":         "The Bybit API key does not permit the Account Transfer operation. Enable Wallet Account Transfer for this account; derivatives permissions do not grant it.",
		"EXCHANGE_FUNDING_INSUFFICIENT":        "The selected amount is not available for transfer in Bybit Funding. Wait for credit or any restrictions to clear. Bybit's auto-transfer setting can move deposits to another account; the wallet does not silently change that global setting.",
		"EXCHANGE_WITHDRAW_UNAVAILABLE":        "A verified owned-wallet withdrawal route is unavailable for this key or Bybit region. Check master-account withdrawal permission, supported networks and regional requirements before moving funds.",
		"EXCHANGE_DEPOSIT_NETWORK_MISMATCH":    "The deposit-address response identifies a different network or provider chain from the selected wallet. No payment was prepared. Different reviewed display names for the same exact network are accepted.",
		"EXCHANGE_DEPOSIT_CONTRACT_MISMATCH":   "The deposit-address token contract or suffix does not corroborate the full verified coin/network contract. No payment was prepared.",
		"EXCHANGE_DEPOSIT_MEMO_INVALID":        "The deposit memo is missing or unsupported. A TON address may omit a memo only when the authenticated service explicitly supplies an empty memo field; omitted or null data is rejected. No payment was prepared.",
		"EXCHANGE_STATE_INVALID":               "The exchange request does not match an available owned wallet, asset, amount or authorization state. Check the current selection and original pending operations before retrying.",
		"EXCHANGE_DEPOSIT_ROUTE_UNAVAILABLE":   "No unique deposit route matches this exact wallet network and asset. The wallet will not substitute another network or a token with the same symbol. No new deposit was sent.",
		"EXCHANGE_DEPOSIT_SUSPENDED":           "The exchange reports deposits suspended on this exact network. Wait for deposits to be enabled. No new deposit was sent.",
		"EXCHANGE_DEPOSIT_BELOW_MINIMUM":       "The amount is below the exchange's current minimum deposit for this network. Refresh the network limits and increase the amount. No new deposit was sent.",
		"EXCHANGE_DEPOSIT_ADDRESS_INVALID":     "The exchange deposit address, memo or token/network identity did not match the verified route. The wallet refused to prepare a payment. No new deposit was sent.",
		"EXCHANGE_DEPOSIT_MASTER_REQUIRED":     "The supported Bybit deposit-address endpoint requires a master-account API key. A subaccount key cannot fetch this destination. No new deposit was sent.",
		"ACCOUNT_RECOVERY_INVALID_RANGE":       fmt.Sprintf("The recovery range is invalid. Choose a starting index from 0 through %d and a count from 1 through 20. The last index must remain below %d. Existing accounts are kept and hidden accounts in the range are restored.", derivation.MaxIndex-1, derivation.MaxIndex),
		"ACCOUNT_RECOVERY_LIMIT":               "Recovering this range would exceed the wallet's 10,000 stored account-descriptor limit. Choose a smaller range. Existing keys and records are unchanged.",
		"ACCOUNT_RECOVERY_DERIVATION":          "A required network wallet could not be derived for this range. No staged account or visibility changes were applied. Check the supported derivation profiles before retrying.",
		"ACCOUNT_RECOVERY_STORAGE":             "The recovered account state could not be committed to encrypted storage. Check storage permissions and free space, then unlock again to verify the last durable state.",
		"ACCOUNT_RECOVERY_STALE":               "The wallet changed or locked while recovery confirmation was open. Nothing was applied to another wallet. Unlock and review the range again.",
		"REQUEST_DENIED":                       "The wallet rejected the operation because its current state or authorization requirements were not satisfied. Confirm recovery setup, select the intended owned wallet, and review any original pending operation before retrying.",
		"INVALID_ARGUMENT":                     "The operation's input or request shape is invalid. Review the selected wallet, network, asset and field values before retrying.",
		"WALLET_LOCKED":                        "The wallet is locked or its signer session ended. Unlock the same local wallet before retrying. Do not assume a pending payment was cancelled or sent.",
		"AUTHENTICATION_FAILED":                "Authentication failed. The entered password or exchange credentials were not accepted. Check the selected account and credentials; no secret value is included in this diagnostic.",
		"ACCESS_DENIED":                        "Access to a required local resource or operation was denied. Check permissions and the selected wallet state.",
		"STORAGE_PERMISSION_DENIED":            "The operating system denied access to encrypted wallet storage. Check directory/file ownership and write permissions. Do not delete the vault or its recovery generation.",
		"STORAGE_UNAVAILABLE":                  "Encrypted storage could not be read or written. Check disk space, storage availability and concurrent writers, then reopen the wallet to verify the last committed state.",
		"STORAGE_CORRUPT":                      "Encrypted wallet storage failed its format or integrity checks. Keep the original files intact and use a trusted recovery copy or the original recovery phrase.",
		"NETWORK_UNAVAILABLE":                  "The configured service could not be reached. Check connectivity and the selected RPC/provider configuration. An unavailable response does not establish a zero balance or transaction failure.",
		"NETWORK_TIMEOUT":                      "The configured service did not respond before the request deadline. Check its status and reconcile any original pending operation before retrying a payment.",
		"NETWORK_TLS_FAILED":                   "The service's TLS identity could not be verified. Check the configured host and its certificate. Do not bypass certificate verification.",
		"PROVIDER_RATE_LIMITED":                "The provider rejected the request because its rate limit was reached. Wait before refreshing or configure a trusted provider with sufficient limits.",
		"PROVIDER_AUTHENTICATION_FAILED":       "The provider rejected authentication. Check its API credentials and selected region; credential values and response bodies are not included here.",
		"PROVIDER_ACCESS_DENIED":               "The provider refused access to this endpoint or operation. Check API permissions, region and provider availability; a refused query is not an empty history or zero balance.",
		"PROVIDER_UNAVAILABLE":                 "The provider reported an unavailable service. Wait or use another trusted endpoint and reconcile original pending operations before any retry.",
		"PROVIDER_REQUEST_REJECTED":            "The provider rejected this request. Check the selected network, asset, request limits and API permissions. Provider response bodies are withheld because they can contain private data.",
		"PROVIDER_REDIRECT_DENIED":             "The provider tried to redirect a request that must stay bound to its configured endpoint. Verify the trusted RPC/provider URL instead of following the redirect.",
		"PROVIDER_INVALID_RESPONSE":            "The provider returned a malformed or inconsistent response. The wallet refused to use it; verify the service and selected network before retrying.",
		"PROVIDER_CLOCK_SKEW":                  "The provider rejected the request timestamp. Check the system clock and retry after it is synchronized.",
		"CHAIN_DATA_INVALID":                   "The provider returned invalid or inconsistent chain data. The wallet refused to use it for balances, history or signing. Check the configured network/provider before retrying.",
	}
	message, ok := messages[code]
	return message, ok
}
