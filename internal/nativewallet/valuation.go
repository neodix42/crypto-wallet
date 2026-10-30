package nativewallet

import (
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"localwallet/internal/amount"
	"localwallet/internal/marketdata"
	"localwallet/internal/model"
)

// PortfolioValuation is an indicative display-only subtotal of visible available
// wallet balances. It excludes staking positions, hidden wallets and exchange
// custody. Unknown balances/identities/prices cannot become a complete zero.
type PortfolioValuation struct {
	USDT           string             `json:"usdt,omitempty"`
	State          string             `json:"state"` // loading, ready, partial, unavailable, testnet
	PricedAssets   int                `json:"pricedAssets"`
	UnpricedAssets int                `json:"unpricedAssets"`
	ObservedAt     int64              `json:"observedAt,omitempty"`
	Detail         string             `json:"detail"`
	Missing        []PortfolioMissing `json:"missing,omitempty"`
}

type PortfolioMissing struct {
	NetworkID string `json:"networkId"`
	Network   string `json:"network"`
	AssetID   string `json:"assetId"`
	Symbol    string `json:"symbol"`
	Reason    string `json:"reason"`
}

// clearMarketPrices invalidates a pending UI callback; coordinator cancellation
// follows its existing session lifecycle. Market data is public, never durable.
func (a *App) clearMarketPrices() {
	a.marketPricesRevision++
	a.marketPrices = marketdata.Snapshot{}
	a.marketPricesAttempt = time.Time{}
	a.marketPricesLoading = false
}

// refreshMarketPrices is called once after an unlocked snapshot and on the UI
// tick. It does not mark global wallet work busy, request confirmation, reset
// user inactivity or load exchange credentials. The coordinator caches attempts.
func (a *App) refreshMarketPrices() {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.environment != "mainnet" || a.marketPricesLoading || (!a.marketPricesAttempt.IsZero() && time.Since(a.marketPricesAttempt) < marketdata.RefreshInterval) {
		return
	}
	a.marketPricesLoading = true
	a.marketPricesAttempt = time.Now()
	a.marketPricesRevision++
	revision, epoch, g := a.marketPricesRevision, a.epoch, a.core.Generation()
	a.render()
	go func() {
		var prices marketdata.Snapshot
		err := a.callAt(g, "marketPrices", struct{}{}, &prices)
		a.ui.Dispatch(func() {
			if a.closed || a.state.Locked || a.epoch != epoch || a.marketPricesRevision != revision {
				return
			}
			if a.core.Generation() != g {
				a.lock()
				return
			}
			a.marketPricesLoading = false
			if err != nil {
				a.marketPrices = marketdata.Snapshot{}
			} else {
				a.marketPrices = prices
			}
			a.render()
		})
	}()
}

func (a *App) portfolioValuation(index uint32, now time.Time) PortfolioValuation {
	result := PortfolioValuation{State: "unavailable", Detail: "Approximate USDT value of last observed visible available native and tracked-token balances; excludes staking positions and exchange custody. Prices are public exchange observations, not executable quotes."}
	if a.environment != "mainnet" {
		result.State = "testnet"
		result.Detail = "Testnet assets are not assigned a real USDT value."
		return result
	}
	if a.state.Locked {
		result.Detail = "Unlock to observe balances and indicative public market prices."
		return result
	}
	total := new(big.Rat)
	seen := map[string]bool{}
	loading := a.marketPricesLoading
	assetBudget := 512
	missing := func(network model.Network, asset model.Asset, reason string) {
		result.UnpricedAssets++
		if len(result.Missing) < 512 {
			result.Missing = append(result.Missing, PortfolioMissing{NetworkID: network.ID, Network: valuationLabel(network.Name, network.ID, 80), AssetID: asset.ID, Symbol: valuationLabel(asset.Symbol, "Unknown token", 24), Reason: reason})
		}
	}
	for _, account := range a.state.Accounts {
		if account.Index != index || account.Hidden {
			continue
		}
		network := a.network(account.Network)
		if !marketdata.ActiveMainnet(network) {
			continue
		}
		address := account.Address
		if account.Family == "evm" || account.Family == "hypercore" {
			address = strings.ToLower(address)
		}
		accountKey := account.Network + "\x00" + address
		// Equal EVM addresses on different chains remain independent balance domains;
		// duplicate descriptors on the same chain must not double-count funds.
		if seen[accountKey] {
			continue
		}
		seen[accountKey] = true
		observations, loaded := a.balances[account.ID]
		if !loaded && !a.balanceErrors[account.ID] {
			loading = true
		}
		seenAssets := map[string]bool{}
		for _, asset := range network.Assets {
			if seenAssets[asset.ID] {
				missing(network, asset, "duplicate asset metadata")
				continue
			}
			seenAssets[asset.ID] = true
			if assetBudget == 0 {
				missing(network, asset, "valuation limit reached")
				continue
			}
			assetBudget--
			var balance model.Balance
			matches := 0
			for _, candidate := range observations {
				if candidate.Asset == asset.ID {
					balance = candidate
					matches++
				}
			}
			atomic, err := amount.Atomic(balance.Atomic)
			if a.balanceErrors[account.ID] || matches != 1 || !completeObservation(balance) || err != nil || asset.Decimals > 77 {
				missing(network, asset, "balance unavailable or not yet loaded")
				continue
			}
			if atomic.Sign() == 0 {
				result.PricedAssets++
				continue
			}
			pair, known := marketdata.IdentityPair(network, asset)
			if !known {
				missing(network, asset, "unrecognized pricing identity")
				continue
			}
			price := new(big.Rat).SetInt64(1)
			if pair != "USDT" {
				quote, ok := marketdata.FreshQuote(a.marketPrices, pair, now)
				if !ok {
					missing(network, asset, "fresh USDT price unavailable")
					continue
				}
				price, ok = price.SetString(quote.Price)
				if !ok {
					missing(network, asset, "fresh USDT price unavailable")
					continue
				}
				if result.ObservedAt == 0 || quote.ObservedAt < result.ObservedAt {
					result.ObservedAt = quote.ObservedAt
				}
			}
			units := new(big.Rat).SetFrac(atomic, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(asset.Decimals)), nil))
			total.Add(total, new(big.Rat).Mul(units, price))
			result.PricedAssets++
		}
	}
	if result.PricedAssets > 0 {
		result.USDT = formatUSDT(total)
	}
	switch {
	case loading:
		result.State = "loading"
	case result.UnpricedAssets > 0 && result.PricedAssets > 0:
		result.State = "partial"
	case result.UnpricedAssets > 0 || result.PricedAssets == 0:
		result.State = "unavailable"
	default:
		result.State = "ready"
	}
	if result.UnpricedAssets > 0 {
		result.Detail += " This is a partial subtotal."
		for _, group := range []struct{ reason, title string }{
			{"balance unavailable or not yet loaded", "Balances unavailable or not yet loaded"},
			{"fresh USDT price unavailable", "Fresh USDT prices unavailable"},
			{"unrecognized pricing identity", "Unrecognized pricing identities"},
			{"duplicate asset metadata", "Duplicate asset metadata"},
			{"valuation limit reached", "Valuation limit reached"},
		} {
			var names []string
			for _, entry := range result.Missing {
				if entry.Reason == group.reason {
					names = append(names, entry.Network+" / "+entry.Symbol)
				}
			}
			if len(names) > 0 {
				result.Detail += " " + group.title + ": " + strings.Join(names, "; ") + "."
			}
		}
		if extra := result.UnpricedAssets - len(result.Missing); extra > 0 {
			result.Detail += fmt.Sprintf(" %d additional observations exceed the detail limit.", extra)
		}
	}

	if result.ObservedAt > 0 {
		result.Detail += " Oldest included market observation: " + time.UnixMilli(result.ObservedAt).UTC().Format(time.RFC3339) + "."
	}
	return result
}

// Round only the final exact rational subtotal to cents for its approximate
// display. No balance, atomic quantity or transaction amount is rounded.
func formatUSDT(total *big.Rat) string {
	scaled := new(big.Rat).Mul(total, big.NewRat(100, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(scaled.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	cents := new(big.Int)
	wholeValue := new(big.Int)
	wholeValue.QuoRem(quotient, big.NewInt(100), cents)
	whole := wholeValue.String()
	var grouped strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(c)
	}
	return fmt.Sprintf("%s.%02d", grouped.String(), cents.Int64())
}

// Metadata labels are display-only. Pricing uses exact identities above; control
// characters and oversized labels cannot disguise an unavailable currency.
func valuationLabel(value, fallback string, limit int) string {
	if value == "" || len(value) > limit || !utf8.ValidString(value) || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		if len(fallback) > limit || !utf8.ValidString(fallback) {
			return "Unknown"
		}
		return fallback
	}
	return value
}
