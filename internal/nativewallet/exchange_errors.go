package nativewallet

import (
	"strings"

	"localwallet/internal/ipc"
)

func exchangeReadReason(code string) (string, bool) {
	stage, cause, ok := ipc.ExchangeReadErrorParts(code)
	if !ok {
		return "", false
	}
	name := map[string]string{"KEY": "Checking exchange API credentials", "COIN": "Reading the selected asset's deposit and withdrawal networks", "BALANCE": "Reading exchange Funding/Unified balances", "INSTRUMENT": "Reading the selected spot pair", "TICKER": "Reading the spot price", "FEE": "Reading the account's spot trading fee", "DEPOSIT_ADDRESS": "Reading the exact network's exchange deposit address", "WALLET_FEE": "Estimating the wallet payment and native network fee"}[stage]
	var reason string
	if stage == "DEPOSIT_CREDIT" {
		name = "Verifying the original deposit's Bybit credit"
	}
	if stage == "EXECUTIONS" {
		name = "Verifying the original spot order's executed amounts and fees"
	}
	if strings.HasPrefix(cause, "METADATA_") {
		field := map[string]string{"METADATA_IDENTITY": "network identity/contract", "METADATA_DUPLICATE_CHAIN": "duplicate network identifiers", "METADATA_FLAGS": "deposit/withdrawal availability flags", "METADATA_DEPOSIT_LIMIT": "minimum deposit", "METADATA_WITHDRAWAL_LIMIT": "withdrawal minimum, maximum or fee", "METADATA_PRECISION": "network decimal precision", "METADATA_CONFIRMATIONS": "deposit/security confirmation counts"}[cause]
		reason = "Bybit's selected coin/network metadata has missing or invalid " + field + ". The wallet cannot safely use that route. Other fully validated networks remain available; the response body and credentials are not logged or displayed."
	} else if strings.HasPrefix(cause, "BYBIT_") {
		number := strings.TrimPrefix(cause, "BYBIT_")
		reason = "Bybit returned API code " + number + ". "
		switch number {
		case "10003", "10007", "-2015":
			reason += "The API key was not accepted. Select the region where the key was created and check that it is active."
		case "10004":
			reason += "The request signature was rejected. Reconnect with the matching system-generated API key and API secret."
		case "10005":
			reason += "This API key lacks permission for this endpoint. Check its Spot and Wallet permissions and any symbol restrictions. Deposit does not require spot-market or trading-fee access."
		case "10008", "10028", "100028":
			reason += "This endpoint is unavailable for the account mode. Spot trading uses a Unified account; an on-chain deposit is a separate operation."
		case "10009", "10010":
			reason += "Bybit rejected the client's IP address. Check the key's IP restrictions and Bybit account access."
		case "10002", "-1":
			reason += "The request timestamp was rejected. Synchronize the system clock."
		case "10006", "429":
			reason += "The service rate limit was reached. Wait before refreshing."
		case "10001", "131001", "131002":
			reason += "The service rejected the selected asset, account or request. Check that this asset and network are available for the selected Bybit region."
		default:
			reason += "Check this numeric code with Bybit for the selected region, asset and account. The provider's private response text is not displayed."
		}
	} else if strings.HasPrefix(cause, "PAYMENT_QUOTE_") || cause == "TON_RPC_RATE_LIMITED" {
		reason = safeError(ipc.NewRequestError(cause))
	} else {
		reason, ok = detailedError(cause)
		if !ok {
			reason = "The read failed without a classified public reason. No provider message, credentials or response body is exposed."
		}
	}
	return name + " failed.\n\n" + reason + "\n\nThis read did not authorize a wallet payment, trade or withdrawal. It does not resolve any older pending operation.", true
}
