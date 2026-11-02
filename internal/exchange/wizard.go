package exchange

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"localwallet/internal/amount"
)

// WizardState is encrypted navigation/operation-correlation metadata. It is
// never transaction authority or evidence of a current exchange balance.
type WizardState struct {
	ID                string `json:"id"`
	Step              int    `json:"step"`
	SourceAccount     string `json:"sourceAccount"`
	SourceAsset       string `json:"sourceAsset"`
	DepositAmount     string `json:"depositAmount"`
	DepositOperation  string `json:"depositOperation,omitempty"`
	TransferOperation string `json:"transferOperation,omitempty"`
	TransferAmount    string `json:"transferAmount,omitempty"`
	TradeAccount      string `json:"tradeAccount,omitempty"`
	TradeAsset        string `json:"tradeAsset,omitempty"`
	TradeSide         string `json:"tradeSide,omitempty"`
	TradeAmount       string `json:"tradeAmount,omitempty"`
	QuoteCoin         string `json:"quoteCoin,omitempty"`
	SwapOperation     string `json:"swapOperation,omitempty"`
	OutputCoin        string `json:"outputCoin,omitempty"`
	OutputAmount      string `json:"outputAmount,omitempty"`
	WithdrawAccount   string `json:"withdrawAccount,omitempty"`
	WithdrawAsset     string `json:"withdrawAsset,omitempty"`
	WithdrawOperation string `json:"withdrawOperation,omitempty"`
	WithdrawAmount    string `json:"withdrawAmount,omitempty"`
}

func (w WizardState) Valid() bool {
	if w.Step < 1 || w.Step > 5 || len(w.ID) != 32 || strings.ToLower(w.ID) != w.ID {
		return false
	}
	if _, err := hex.DecodeString(w.ID); err != nil {
		return false
	}
	for _, s := range []string{w.SourceAccount, w.SourceAsset, w.DepositOperation, w.TransferOperation, w.TradeAccount, w.TradeAsset, w.SwapOperation, w.WithdrawAccount, w.WithdrawAsset, w.WithdrawOperation} {
		if len(s) > 256 || !utf8.ValidString(s) || strings.IndexFunc(s, unicode.IsControl) >= 0 {
			return false
		}
	}
	if w.SourceAccount == "" || w.SourceAsset == "" {
		return false
	}
	for _, s := range []string{w.DepositAmount, w.TransferAmount, w.TradeAmount, w.OutputAmount, w.WithdrawAmount} {
		if s == "" {
			continue
		}
		n, err := amount.Parse(s, 36)
		if err != nil || n == "0" || len(s) > 80 {
			return false
		}
	}
	if w.DepositAmount == "" || w.TradeSide != "" && w.TradeSide != "Buy" && w.TradeSide != "Sell" || w.QuoteCoin != "" && w.QuoteCoin != "USDT" && w.QuoteCoin != "USDC" {
		return false
	}
	if len(w.OutputCoin) > 24 || strings.IndexFunc(w.OutputCoin, func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') }) >= 0 {
		return false
	}
	if w.Step >= 2 && w.DepositOperation == "" || w.Step >= 3 && (w.TransferOperation == "" || w.TransferAmount == "") || w.Step >= 4 && (w.SwapOperation == "" || w.OutputCoin == "" || w.OutputAmount == "") || w.Step == 5 && w.WithdrawOperation == "" {
		return false
	}
	return true
}
