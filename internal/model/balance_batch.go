package model

import (
	"strings"
	"unicode/utf8"
)

// Balance batches contain public observations only. They cannot prepare,
// reserve, authorize or submit a payment.
const BalanceBatchLimit = 64

type BalanceBatchResult struct {
	AccountID string    `json:"accountId"`
	Network   string    `json:"network"`
	Values    []Balance `json:"values"`
	Error     string    `json:"error,omitempty"`
}

type BalanceBatchSummary struct {
	Completed int `json:"completed"`
}

// ValidBalanceBatchResult bounds the streaming DTO independently of provider
// text. The caller must additionally match the selected owned account/network.
func ValidBalanceBatchResult(result BalanceBatchResult) bool {
	if !balanceText(result.AccountID, 256, false) || !balanceText(result.Network, 128, false) {
		return false
	}
	if result.Error != "" {
		return len(result.Values) == 0 && (result.Error == "BALANCE_UNAVAILABLE" || result.Error == "TON_RPC_RATE_LIMITED")
	}
	if len(result.Values) == 0 || len(result.Values) > 64 {
		return false
	}
	seen := make(map[string]bool, len(result.Values))
	for _, value := range result.Values {
		if !balanceText(value.Asset, 256, false) || !strings.HasPrefix(value.Asset, result.Network+":") || seen[value.Asset] || !balanceText(value.State, 1024, true) || !balanceText(value.At, 256, true) || len(value.Atomic) > 160 {
			return false
		}
		seen[value.Asset] = true
		if value.Atomic != "" {
			if len(value.Atomic) > 1 && value.Atomic[0] == '0' {
				return false
			}
			for _, digit := range value.Atomic {
				if digit < '0' || digit > '9' {
					return false
				}
			}
		}
	}
	return true
}

func balanceText(value string, maximum int, empty bool) bool {
	if (!empty && value == "") || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return r < 32 || r == 127 }) < 0
}
