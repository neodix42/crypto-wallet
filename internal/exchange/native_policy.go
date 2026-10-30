package exchange

import (
	"math/big"

	"localwallet/internal/amount"
	"localwallet/internal/exchange/bybit"
)

// ValidateIOCOrder retains the approved original spending ceiling while
// binding a native base quantity and explicit limit price. Only immediate-or-
// cancel spot orders are constructed by the provider's fixed wire encoder.
func ValidateIOCOrder(order bybit.SpotOrder) error {
	if order.OrderType != "Limit" || order.LimitPrice == "" || order.BaseQty == "" {
		return ErrAccess
	}
	base, err := amount.Parse(order.BaseQty, 36)
	price, pErr := amount.Parse(order.LimitPrice, 36)
	budget, bErr := amount.Parse(order.Qty, 36)
	if err != nil || pErr != nil || bErr != nil || base == "0" || price == "0" || budget == "0" {
		return ErrAccess
	}
	legacy := order
	legacy.OrderType = "Market"
	legacy.LimitPrice = ""
	legacy.BaseQty = ""
	if bybit.ValidateSpotOrder(legacy) != nil {
		return ErrAccess
	}
	baseN, _ := new(big.Int).SetString(base, 10)
	priceN, _ := new(big.Int).SetString(price, 10)
	budgetN, _ := new(big.Int).SetString(budget, 10)
	if order.Side == "Sell" {
		if baseN.Cmp(budgetN) != 0 {
			return ErrAccess
		}
	} else {
		scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(36), nil)
		if new(big.Int).Mul(baseN, priceN).Cmp(new(big.Int).Mul(budgetN, scale)) > 0 {
			return ErrAccess
		}
	}
	return nil
}
