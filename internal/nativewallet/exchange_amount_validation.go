package nativewallet

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/model"
)

type exchangeInputValidation struct {
	InputLabel, InputError, SlippageError  string
	SwapValid, DepositValid, WithdrawValid bool
	SwapError, DepositError, WithdrawError string
}

type exchangeInputQuote struct {
	Provider, AccountID, AssetID, Coin, QuoteCoin, Symbol, Side          string
	BasePrecision, QuotePrecision, BidPrice, MinOrderAmount, MaxOrderQty string
	ObservedAt, TradingUnavailable                                       string
	Route                                                                struct {
		Supported, DepositEnabled, WithdrawEnabled bool
		MinDeposit, MinWithdraw                    string
	}
}

// These local checks provide feedback only. Every economic operation still
// fetches fresh authenticated provider rules and passes the coordinator/signer's
// exact approval, fee, route and reservation checks before any debit.
func (a *App) exchangeAmountValidation() exchangeInputValidation {
	return a.exchangeAmountValidationAt(time.Now())
}

func (a *App) exchangeAmountValidationAt(now time.Time) exchangeInputValidation {
	return a.exchangeAmountValidationFieldsAt(a.fields, a.quote, now)
}

func (a *App) exchangeAmountValidationFieldsAt(fields map[string]string, data json.RawMessage, now time.Time) exchangeInputValidation {
	result := exchangeInputValidation{InputLabel: "Amount to spend / withdraw"}
	account, owned := a.account(fields["exchangeAccount"])
	network := a.network(account.Network)
	var asset model.Asset
	for _, candidate := range network.Assets {
		if candidate.ID == fields["exchangeAsset"] && exchangeAssetAllowed(account, candidate) {
			asset = exchange.LedgerAsset(candidate)
			break
		}
	}
	if !owned || asset.ID == "" || network.Testnet {
		result.SwapError = "Select an available wallet asset first."
		result.DepositError, result.WithdrawError = result.SwapError, result.SwapError
		return result
	}
	side, quoteCoin, directionValid := spotDirection(fields["side"])
	spendCoin := asset.Symbol
	if side == "Buy" {
		spendCoin = quoteCoin
	}
	result.InputLabel = "Amount to spend / withdraw (" + asset.Symbol + ")"
	if side == "Buy" {
		result.InputLabel = "Amount to spend (" + quoteCoin + ") / deposit or withdraw (" + asset.Symbol + ")"
	}
	slipText, slipErr := amount.Parse(fields["slippage"], 2)
	slip, invalidSlip := amount.Atomic(slipText)
	slipValid := slipErr == nil && invalidSlip == nil && slip.Sign() > 0 && slip.Cmp(big.NewInt(100)) <= 0
	if !slipValid && fields["slippage"] != "" {
		result.SlippageError = "Enter a slippage limit from 0.01% to 1%."
	}
	value := fields["exchangeAmount"]
	quantity, commonErr := exchangeInputAtomic(value)
	assetAtomic, assetErr := amount.Parse(value, asset.Decimals)
	assetValid := commonErr == nil && quantity.Sign() > 0 && assetErr == nil && assetAtomic != "0"
	result.DepositValid, result.WithdrawValid = assetValid, assetValid
	if !assetValid {
		result.DepositError = fmt.Sprintf("Enter a positive %s amount with at most %d decimal places.", asset.Symbol, asset.Decimals)
		result.WithdrawError = result.DepositError
	}
	if commonErr != nil || quantity.Sign() <= 0 {
		result.SwapError = "Enter a positive decimal amount; signs, exponents and separators are not supported."
		if value != "" {
			result.InputError = result.SwapError
		}
		return result
	}
	if side == "Sell" && !assetValid {
		result.InputError = result.DepositError
		result.SwapError = result.InputError
		return result
	}
	if !directionValid {
		result.SwapError = "Select a supported spot direction."
		return result
	}
	quote, fresh := a.exchangeInputQuoteFromAt(account, asset, side, quoteCoin, now, data)
	if !fresh {
		result.SwapError = "Refresh market data to obtain this pair's current trading limits."
		return result
	}
	if quote.Route.Supported {
		if !quote.Route.DepositEnabled {
			result.DepositValid = false
			result.DepositError = "Deposits are unavailable for this network."
		}
		if !quote.Route.WithdrawEnabled {
			result.WithdrawValid = false
			result.WithdrawError = "Withdrawals are unavailable for this network or API permission."
		}
		if result.DepositValid && !exchangeInputAtLeast(value, quote.Route.MinDeposit) {
			result.DepositValid = false
			result.DepositError = "Amount is below the exchange minimum deposit of " + quote.Route.MinDeposit + " " + asset.Symbol + "."
		}
		if result.WithdrawValid && !exchangeInputAtLeast(value, quote.Route.MinWithdraw) {
			result.WithdrawValid = false
			result.WithdrawError = "Amount is below the exchange minimum withdrawal of " + quote.Route.MinWithdraw + " " + asset.Symbol + "."
		}
	} else {
		result.DepositValid, result.WithdrawValid = false, false
		result.DepositError = "No verified deposit route matches this wallet network and asset."
		result.WithdrawError = "No verified withdrawal route matches this wallet network and asset."
	}
	if quote.TradingUnavailable != "" || quote.Symbol != asset.Symbol+quoteCoin || asset.Symbol == quoteCoin || quote.Provider == "kucoin" {
		result.SwapError = "Spot trading is unavailable for this selected pair or provider."
		return result
	}
	if !slipValid {
		result.SwapError = "Enter a slippage limit from 0.01% to 1%."
		return result
	}
	stepText := quote.BasePrecision
	if side == "Buy" {
		stepText = quote.QuotePrecision
	}
	step, e1 := exchangeInputAtomic(stepText)
	minimum, e2 := exchangeInputAtomic(quote.MinOrderAmount)
	maximum, e3 := exchangeInputAtomic(quote.MaxOrderQty)
	bid, e4 := exchangeInputAtomic(quote.BidPrice)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || step.Sign() <= 0 || minimum.Sign() <= 0 || maximum.Sign() <= 0 || bid.Sign() <= 0 {
		result.SwapError = "Refresh market data; the current trading limits are unavailable."
		return result
	}
	if new(big.Int).Mod(quantity, step).Sign() != 0 {
		result.SwapError = "Amount must be a multiple of " + stepText + " " + spendCoin + "."
	} else if side == "Buy" {
		if quantity.Cmp(minimum) < 0 {
			result.SwapError = "Amount is below the exchange minimum order value of " + quote.MinOrderAmount + " " + quoteCoin + "."
		} else if new(big.Int).Mul(quantity, exchangeInputScale()).Cmp(new(big.Int).Mul(maximum, bid)) > 0 {
			result.SwapError = "Amount exceeds the current maximum order size."
		}
	} else {
		notional := new(big.Int).Mul(quantity, bid)
		notional.Mul(notional, new(big.Int).Sub(big.NewInt(10000), slip))
		notional.Div(notional, new(big.Int).Mul(exchangeInputScale(), big.NewInt(10000)))
		if quantity.Cmp(maximum) > 0 {
			result.SwapError = "Amount exceeds the current maximum order size."
		} else if notional.Cmp(minimum) < 0 {
			result.SwapError = "Amount is below the exchange minimum order value of " + quote.MinOrderAmount + " " + quoteCoin + "."
		}
	}
	result.SwapValid = result.SwapError == ""
	result.InputError = result.SwapError
	return result
}

func exchangeInputAtomic(value string) (*big.Int, error) {
	parsed, err := amount.Parse(value, 36)
	if err != nil {
		return nil, err
	}
	return amount.Atomic(parsed)
}
func exchangeInputScale() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(36), nil) }
func exchangeInputAtLeast(value, minimum string) bool {
	a, err := exchangeInputAtomic(value)
	b, invalid := exchangeInputAtomic(minimum)
	return err == nil && invalid == nil && a.Cmp(b) >= 0
}
func (a *App) exchangeInputQuoteAt(account model.Account, asset model.Asset, side, quoteCoin string, now time.Time) (exchangeInputQuote, bool) {
	return a.exchangeInputQuoteFromAt(account, asset, side, quoteCoin, now, a.quote)
}

func (a *App) exchangeInputQuoteFromAt(account model.Account, asset model.Asset, side, quoteCoin string, now time.Time, data json.RawMessage) (exchangeInputQuote, bool) {
	var quote exchangeInputQuote
	if len(data) > 131072 || json.Unmarshal(data, &quote) != nil || quote.Provider != a.selectedExchangeProvider().ID || quote.AccountID != account.ID || quote.AssetID != asset.ID || quote.Coin != asset.Symbol || quote.Side != side || quote.QuoteCoin != quoteCoin {
		return quote, false
	}
	observed, err := strconv.ParseInt(quote.ObservedAt, 10, 64)
	if err != nil || strconv.FormatInt(observed, 10) != quote.ObservedAt || observed > now.Add(time.Second).UnixMilli() || observed < now.Add(-2*time.Minute).UnixMilli() {
		return quote, false
	}
	return quote, true
}

// The caller supplies its exact original request before starting any API work.
// A market/route refresh never consumes the amount and remains available for an
// incomplete or invalid draft.
func (a *App) validateExchangeAmountAction(method string, body map[string]string) bool {
	if method != "exchangeSwap" && method != "exchangeDeposit" && method != "exchangeWithdraw" {
		return true
	}
	result := a.exchangeAmountValidation()
	valid, message := result.SwapValid, result.SwapError
	if method == "exchangeDeposit" {
		valid, message = result.DepositValid, result.DepositError
	}
	if method == "exchangeWithdraw" {
		valid, message = result.WithdrawValid, result.WithdrawError
	}
	identity := body["provider"] == a.selectedExchangeProvider().ID && body["accountId"] == a.fields["exchangeAccount"] && body["assetId"] == a.fields["exchangeAsset"] && body["amount"] == a.fields["exchangeAmount"]
	if method == "exchangeSwap" {
		side, quote, ok := spotDirection(a.fields["side"])
		identity = identity && ok && body["side"] == side && body["quoteCoin"] == quote && body["slippage"] == a.fields["slippage"]
	}
	if valid && identity {
		return true
	}
	if message == "" || !identity {
		message = "The amount or selected exchange asset changed. Review the current fields before continuing."
	}
	a.errorText("Exchange amount validation", message)
	a.render()
	return false
}
