package nativewallet

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"localwallet/internal/marketdata"
	"localwallet/internal/model"
)

func valuationFixture(now time.Time) *App {
	const usdt = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	const usdc = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	const zen = "0xf43eB8De897Fbc7F2502483B2Bef7Bb9EA179229"
	asset := func(network, contract string, decimals uint8) model.Asset {
		kind := ":native"
		if contract != "" {
			kind = ":erc20:" + contract
		}
		return model.Asset{ID: network + kind, Contract: contract, Decimals: decimals}
	}
	eth := model.Network{ID: "ethereum-mainnet", Family: "evm", ChainID: "1", Assets: []model.Asset{asset("ethereum-mainnet", "", 18), asset("ethereum-mainnet", usdt, 6), asset("ethereum-mainnet", usdc, 6)}}
	base := model.Network{ID: "base-mainnet", Family: "evm", ChainID: "8453", Assets: []model.Asset{asset("base-mainnet", "", 18), asset("base-mainnet", zen, 18)}}
	arb := model.Network{ID: "arbitrum-mainnet", Family: "evm", ChainID: "42161", Assets: []model.Asset{asset("arbitrum-mainnet", "", 18)}}
	a := &App{environment: "mainnet", state: snapshot{RecoveryConfirmed: true, Accounts: []model.Account{{ID: "eth", Index: 0, Family: "evm", Network: eth.ID, Address: "0x123"}, {ID: "base", Index: 0, Family: "evm", Network: base.ID, Address: "0x123"}, {ID: "arb", Index: 0, Family: "evm", Network: arb.ID, Address: "0x123"}}}, networks: []model.Network{eth, base, arb}, balances: map[string][]model.Balance{}, balanceErrors: map[string]bool{}}
	values := map[string][]string{"eth": {"1500000000000000000", "12345678", "3000000"}, "base": {"250000000000000000", "2000000000000000000"}, "arb": {"1000000000000000000"}}
	for _, account := range a.state.Accounts {
		for i, asset := range a.network(account.Network).Assets {
			a.balances[account.ID] = append(a.balances[account.ID], model.Balance{Asset: asset.ID, Atomic: values[account.ID][i], State: "provider-observed", At: "checked-chain-position"})
		}
	}
	a.marketPrices = marketdata.Snapshot{FetchedAt: now.UnixMilli(), Quotes: map[string]marketdata.Quote{}}
	for pair, price := range map[string]string{"ETHUSDT": "2000.125", "USDCUSDT": "0.999", "ZENUSDT": "5.22"} {
		a.marketPrices.Quotes[pair] = marketdata.Quote{Pair: pair, Price: price, ObservedAt: now.UnixMilli(), Source: "Bybit public spot"}
	}
	return a
}
func TestPortfolioValuationUsesExactWholeUnitsAndIndependentNetworkBalances(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	before := viewText(a.balances)
	total := a.portfolioValuation(0, now)
	if total.USDT != "5,526.13" || total.State != "ready" || total.PricedAssets != 6 || total.UnpricedAssets != 0 {
		t.Fatalf("exact total %+v", total)
	}
	if !strings.Contains(total.Detail, "excludes staking") || !strings.Contains(total.Detail, "exchange custody") || total.ObservedAt == 0 {
		t.Fatalf("valuation scope missing %+v", total)
	}
	if viewText(a.balances) != before {
		t.Fatal("valuation changed balance authority")
	}
	// A repeated descriptor cannot count a same-chain address twice.
	duplicate := a.state.Accounts[0]
	duplicate.ID = "duplicate"
	duplicate.Address = "0x123"
	a.state.Accounts = append(a.state.Accounts, duplicate)
	a.balances[duplicate.ID] = a.balances["eth"]
	if total = a.portfolioValuation(0, now); total.USDT != "5,526.13" || total.PricedAssets != 6 {
		t.Fatalf("double counted same chain %+v", total)
	}
	// Another account index is a different aggregate and cannot leak into index0.
	other := a.state.Accounts[0]
	other.ID = "other-account"
	other.Index = 1
	other.Address = "0x456"
	a.state.Accounts = append(a.state.Accounts, other)
	a.balances[other.ID] = a.balances["eth"]
	if total = a.portfolioValuation(1, now); total.USDT != "3,015.53" || total.PricedAssets != 3 {
		t.Fatalf("account aggregate wrong %+v", total)
	}
}
func TestPortfolioValuationNeverPricesAnUnreviewedTokenByTicker(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	fake := model.Asset{ID: "ethereum-mainnet:erc20:0x1111111111111111111111111111111111111111", Contract: "0x1111111111111111111111111111111111111111", Symbol: "USDT", Decimals: 6}
	a.networks[0].Assets = append(a.networks[0].Assets, fake)
	a.balances["eth"] = append(a.balances["eth"], model.Balance{Asset: fake.ID, Atomic: "99999999999999999999999999", State: "observed", At: "block"})
	total := a.portfolioValuation(0, now)
	if total.USDT != "5,526.13" || total.State != "partial" || total.UnpricedAssets != 1 || !strings.Contains(total.Detail, "partial subtotal") {
		t.Fatalf("spoof valued %+v", total)
	}
	// Zero has zero economic value without guessing an issuer or ticker.
	a.balances["eth"][3].Atomic = "0"
	if total = a.portfolioValuation(0, now); total.State != "ready" || total.USDT != "5,526.13" {
		t.Fatalf("known zero should not require a price %+v", total)
	}
}
func TestPortfolioUnknownsAndStalePricesRemainExplicit(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		change func(*App)
		state  string
	}{
		{"balance failure", func(a *App) { a.balanceErrors["base"] = true }, "partial"},
		{"missing balance still loading", func(a *App) { delete(a.balances, "base") }, "loading"},
		{"stale price", func(a *App) {
			quote := a.marketPrices.Quotes["ETHUSDT"]
			quote.ObservedAt = now.Add(-3 * time.Minute).UnixMilli()
			a.marketPrices.Quotes["ETHUSDT"] = quote
		}, "partial"},
		{"pending balance", func(a *App) { a.balances["eth"][0].State = "pending" }, "partial"},
		{"invalid atomic amount", func(a *App) { a.balances["eth"][0].Atomic = "01" }, "partial"},
		{"duplicate balance", func(a *App) { a.balances["eth"] = append(a.balances["eth"], a.balances["eth"][0]) }, "partial"},
		{"duplicate asset", func(a *App) { a.networks[0].Assets = append(a.networks[0].Assets, a.networks[0].Assets[0]) }, "partial"},
		{"price fetch pending", func(a *App) { a.marketPricesLoading = true }, "loading"},
		{"unavailable all balances", func(a *App) {
			for _, ac := range a.state.Accounts {
				a.balanceErrors[ac.ID] = true
			}
		}, "unavailable"},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			a := valuationFixture(now)
			row.change(a)
			out := a.portfolioValuation(0, now)
			if out.State != row.state {
				t.Fatalf("%+v", out)
			}
			if out.State == "unavailable" && out.USDT != "" {
				t.Fatalf("unknown became a priced zero %+v", out)
			}
		})
	}
}
func TestPortfolioUSDTReportingUnitIsOnlyThePinnedTokenAndUSDCUsesMarket(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	a.state.Accounts = a.state.Accounts[:1]
	a.networks[0].Assets = a.networks[0].Assets[1:]
	a.balances["eth"] = a.balances["eth"][1:]
	a.marketPrices.Quotes = map[string]marketdata.Quote{}
	total := a.portfolioValuation(0, now)
	if total.USDT != "12.35" || total.State != "partial" || total.UnpricedAssets != 1 {
		t.Fatalf("USDC assumed one USDT %+v", total)
	}
	a.networks[0].Assets = a.networks[0].Assets[:1]
	a.balances["eth"] = a.balances["eth"][:1]
	if total = a.portfolioValuation(0, now); total.USDT != "12.35" || total.State != "ready" {
		t.Fatalf("USDT reporting unit %+v", total)
	}
	a.networks[0].Assets[0].Contract = "0x1111111111111111111111111111111111111111"
	a.networks[0].Assets[0].ID = "ethereum-mainnet:erc20:" + a.networks[0].Assets[0].Contract
	a.balances["eth"][0].Asset = a.networks[0].Assets[0].ID
	if total = a.portfolioValuation(0, now); total.USDT != "" || total.State != "unavailable" {
		t.Fatalf("untrusted USDT assumed reportingunit %+v", total)
	}
}
func TestPortfolioExcludesTestnetHiddenAndRetiredDescriptors(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	a.state.Accounts = append(a.state.Accounts, model.Account{ID: "retired", Network: "decred-mainnet", Address: "public-retired"})
	a.networks = append(a.networks, model.Network{ID: "decred-mainnet", Family: "decred", Assets: []model.Asset{{ID: "DCR", Symbol: "DCR", Decimals: 8}}})
	a.balances["retired"] = []model.Balance{{Asset: "DCR", Atomic: "99999999999999999999999999", State: "observed", At: "block"}}
	if total := a.portfolioValuation(0, now); total.USDT != "5,526.13" || total.UnpricedAssets != 0 {
		t.Fatalf("retired descriptor included %+v", total)
	}
	a.state.Accounts[0].Hidden = true
	if total := a.portfolioValuation(0, now); total.USDT != "2,510.60" || total.PricedAssets != 3 {
		t.Fatalf("hidden balances included %+v", total)
	}
	a.environment = "testnet"
	if total := a.portfolioValuation(0, now); total.State != "testnet" || total.USDT != "" {
		t.Fatalf("testnet assigned real value %+v", total)
	}
	a.environment = "mainnet"
	a.state.Locked = true
	if total := a.portfolioValuation(0, now); total.USDT != "" || total.State != "unavailable" {
		t.Fatalf("locked aggregate visible %+v", total)
	}
}
func TestPortfolioRoundingOnlyAfterSummingAndBoundedLargeValues(t *testing.T) {
	for input, want := range map[string]string{"0": "0.00", "0.0049": "0.00", "0.005": "0.01", "1.999": "2.00", "3564.23": "3,564.23", "999999999999999999999999999999.995": "1,000,000,000,000,000,000,000,000,000,000.00"} {
		value, _ := new(big.Rat).SetString(input)
		if got := formatUSDT(value); got != want {
			t.Errorf("%s: %s != %s", input, got, want)
		}
	}
	now := time.Now()
	a := valuationFixture(now)
	a.state.Accounts = a.state.Accounts[:1]
	a.networks[0].Assets = a.networks[0].Assets[:1]
	a.balances["eth"] = a.balances["eth"][:1]
	a.balances["eth"][0].Atomic = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)).String()
	if total := a.portfolioValuation(0, now); total.State != "ready" || len(total.USDT) < 60 {
		t.Fatalf("large exact amount failed %+v", total)
	}
}
func TestPublicPriceRefreshRunsWithoutConfirmationAndRespectsLifecycle(t *testing.T) {
	for _, transition := range []string{"success", "failure", "lock", "generation"} {
		t.Run(transition, func(t *testing.T) {
			a, ui, core := fixture(t)
			a.marketPricesAttempt = time.Time{}
			entered, release := make(chan struct{}), make(chan struct{})
			core.handler = func(g uint64, method string, body, out any) error {
				if method != "marketPrices" || viewText(body) != "{}" {
					t.Errorf("unexpected public price authorization/request %s %s", method, viewText(body))
					return errors.New("unexpected")
				}
				close(entered)
				<-release
				if transition == "failure" {
					return marketdata.ErrUnavailable
				}
				return answer(out, marketdata.Snapshot{Quotes: map[string]marketdata.Quote{"ETHUSDT": {Pair: "ETHUSDT", Price: "2000", ObservedAt: time.Now().UnixMilli(), Source: "Bybit public spot"}}})
			}
			lastActivity := a.lastActivity
			a.refreshMarketPrices()
			<-entered
			if !a.marketPricesLoading || a.busy || a.background || a.lastActivity != lastActivity {
				t.Fatal("public market read requested confirmation or reset idle/blocked UI")
			}
			// While in flight and during its minute cache window, ticks cannot enqueue duplicates.
			a.refreshMarketPrices()
			if transition == "lock" {
				a.lock()
			}
			if transition == "generation" {
				core.generation.Add(1)
			}
			close(release)
			ui.apply(t)
			if a.marketPricesLoading {
				t.Fatal("loading state retained after completion/cancellation")
			}
			if transition == "success" && len(a.marketPrices.Quotes) != 1 {
				t.Fatal("fresh result was lost")
			}
			if transition != "success" && len(a.marketPrices.Quotes) != 0 {
				t.Fatal("failed/stale price result crossed lifecycle")
			}
		})
	}
}
func TestPublicPriceRefreshNeverRunsForLockedPendingOrTestProfiles(t *testing.T) {
	for _, kind := range []string{"locked", "pending", "testnet", "closed", "cache"} {
		t.Run(kind, func(t *testing.T) {
			a, _, core := fixture(t)
			a.marketPricesAttempt = time.Time{}
			switch kind {
			case "locked":
				a.state.Locked = true
			case "pending":
				a.state.RecoveryConfirmed = false
			case "testnet":
				a.environment = "testnet"
			case "closed":
				a.closed = true
			case "cache":
				a.marketPricesAttempt = time.Now()
			}
			core.handler = func(uint64, string, any, any) error { t.Error("ineligible public polling"); return context.Canceled }
			a.refreshMarketPrices()
			if a.marketPricesLoading {
				t.Fatal("ineligible public price request started")
			}
		})
	}
}

func TestPortfolioHintNamesEachUnavailableCurrencyAndItsNetwork(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	a.networks[0].Name = "Ethereum"
	a.networks[1].Name = "Base"
	a.networks[2].Name = "Arbitrum One"
	names := [][]string{{"ETH", "USDT", "USDC"}, {"ETH", "ZEN"}, {"ETH"}}
	for i := range a.networks {
		for j := range a.networks[i].Assets {
			a.networks[i].Assets[j].Symbol = names[i][j]
		}
	}
	a.balanceErrors["base"] = true
	a.balances["eth"][0].State = "unavailable"
	delete(a.marketPrices.Quotes, "USDCUSDT")
	fake := model.Asset{ID: "arbitrum-mainnet:erc20:0x1111111111111111111111111111111111111111", Contract: "0x1111111111111111111111111111111111111111", Symbol: "MYTOKEN", Decimals: 6}
	a.networks[2].Assets = append(a.networks[2].Assets, fake)
	a.balances["arb"] = append(a.balances["arb"], model.Balance{Asset: fake.ID, Atomic: "1000000", State: "observed", At: "block"})
	out := a.portfolioValuation(0, now)
	for _, text := range []string{"Balances unavailable or not yet loaded: Ethereum / ETH; Base / ETH; Base / ZEN", "Fresh USDT prices unavailable: Ethereum / USDC", "Unrecognized pricing identities: Arbitrum One / MYTOKEN"} {
		if !strings.Contains(out.Detail, text) {
			t.Errorf("missing named currency/reason %q in %s", text, out.Detail)
		}
	}
	if len(out.Missing) != 5 || out.UnpricedAssets != 5 || out.State != "partial" {
		t.Fatalf("typed missing details %+v", out)
	}
	if out.Missing[0].AssetID != "ethereum-mainnet:native" || out.Missing[1].Reason != "fresh USDT price unavailable" {
		t.Fatalf("missing exact identities/reasons %+v", out.Missing)
	}
}
func TestPortfolioMissingHintIsBoundedAndNeverInjectsMetadataControls(t *testing.T) {
	now := time.Now()
	a := valuationFixture(now)
	a.state.Accounts = a.state.Accounts[:1]
	a.balances["eth"] = nil
	a.balanceErrors["eth"] = true
	a.networks[0].Name = strings.Repeat("N", 80)
	a.networks[0].Assets = nil
	for i := 0; i < 600; i++ {
		a.networks[0].Assets = append(a.networks[0].Assets, model.Asset{ID: "public-unpriced-" + big.NewInt(int64(i)).String(), Symbol: strings.Repeat("T", 24), Decimals: 18})
	}
	out := a.portfolioValuation(0, now)
	if len(out.Missing) != 512 || out.UnpricedAssets != 600 || len(out.Detail) > 65536 || !strings.Contains(out.Detail, "88 additional observations") {
		t.Fatalf("unbounded portfolio hint entries=%d count=%d bytes=%d", len(out.Missing), out.UnpricedAssets, len(out.Detail))
	}
	if got := valuationLabel("ETH\nwrong", "Unknown token", 24); got != "Unknown token" {
		t.Fatal("control character reached public hint")
	}
	if got := valuationLabel("", strings.Repeat("x", 100), 24); got != "Unknown" {
		t.Fatal("oversized fallback reached public hint")
	}
}
