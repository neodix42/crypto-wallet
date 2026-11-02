package nativewallet

// Synthetic public quote fixtures exercise local form gates, not exchange
// listing, funding, authenticated API acceptance or public-network execution.
import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func exchangeValidationQuote(a *App, account model.Account, asset model.Asset, side, quoteCoin string, now time.Time) exchangeInputQuote {
	q := exchangeInputQuote{Provider: a.selectedExchangeProvider().ID, AccountID: account.ID, AssetID: asset.ID, Coin: asset.Symbol, QuoteCoin: quoteCoin, Side: side, Symbol: asset.Symbol + quoteCoin, BasePrecision: "0.000001", QuotePrecision: "0.01", BidPrice: "100", MinOrderAmount: "5", MaxOrderQty: "100", ObservedAt: nowToExchangeMilliseconds(now)}
	q.ObservedAt = nowToExchangeMilliseconds(now)
	q.Route.Supported, q.Route.DepositEnabled, q.Route.WithdrawEnabled = true, true, true
	q.Route.MinDeposit, q.Route.MinWithdraw = "0.000001", "0.001"
	return q
}
func nowToExchangeMilliseconds(now time.Time) string {
	raw, _ := json.Marshal(now.UnixMilli())
	return string(raw)
}
func setExchangeValidationQuote(a *App, q exchangeInputQuote) { a.quote, _ = json.Marshal(q) }
func selectedExchangeValidationAsset(t *testing.T, a *App) (model.Account, model.Asset) {
	t.Helper()
	account, ok := a.account(a.fields["exchangeAccount"])
	if !ok {
		t.Fatal("fixture wallet missing")
	}
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID == a.fields["exchangeAsset"] {
			return account, asset
		}
	}
	t.Fatal("fixture asset missing")
	return account, model.Asset{}
}
func exchangeValidationRequest(a *App, method string) map[string]string {
	body := map[string]string{"provider": a.selectedExchangeProvider().ID, "accountId": a.fields["exchangeAccount"], "assetId": a.fields["exchangeAsset"], "amount": a.fields["exchangeAmount"]}
	if method == "exchangeSwap" {
		side, quote, _ := spotDirection(a.fields["side"])
		body["side"], body["quoteCoin"], body["slippage"] = side, quote, a.fields["slippage"]
	}
	return body
}

func TestExchangeAmountValidationAllProvidersDirectionsAndEligibleAssets(t *testing.T) {
	a, _, _ := fixture(t)
	now := time.Now()
	eligible := 0
	for _, account := range a.state.Accounts {
		for _, asset := range a.network(account.Network).Assets {
			if !exchangeAssetAllowed(account, asset) {
				continue
			}
			eligible++
			for _, provider := range []string{"bybit", "binance", "kucoin"} {
				for _, direction := range []string{"Sell", "Buy", "SellUSDC", "BuyUSDC"} {
					t.Run(asset.ID+"/"+provider+"/"+direction, func(t *testing.T) {
						a.fields["exchangeAccount"], a.fields["exchangeAsset"], a.fields["exchangeProvider"], a.fields["side"], a.fields["exchangeAmount"] = account.ID, asset.ID, provider, direction, "10.25"
						side, quote, _ := spotDirection(direction)
						q := exchangeValidationQuote(a, account, asset, side, quote, now)
						setExchangeValidationQuote(a, q)
						out := a.exchangeAmountValidationAt(now)
						wantSwap := provider != "kucoin" && asset.Symbol != quote
						if out.SwapValid != wantSwap || !out.DepositValid || !out.WithdrawValid || !strings.Contains(out.InputLabel, asset.Symbol) {
							t.Fatal("provider/asset amount rules changed", out)
						}
						if side == "Buy" && !strings.Contains(out.InputLabel, quote) {
							t.Fatal("buy budget lacks quote currency", out.InputLabel)
						}
					})
				}
			}
		}
	}
	if eligible != 17 {
		t.Fatal("reviewed default route eligibility changed", eligible)
	}
}

func TestExchangeAmountValidationRejectsMalformedNumbersWithoutBlockingRuleDiscovery(t *testing.T) {
	for _, provider := range []string{"bybit", "binance", "kucoin"} {
		for _, direction := range []string{"Sell", "Buy", "SellUSDC", "BuyUSDC"} {
			for _, value := range []string{"", "0", "0.0", "-1", "+1", "NaN", "Infinity", "1e2", "1,5", " 1", "1 ", "01", ".5", "1.", "1..2", "１", "1\x00", strings.Repeat("9", 159)} {
				a, ui, core := fixture(t)
				a.fields["exchangeProvider"], a.fields["side"], a.fields["exchangeAmount"] = provider, direction, value
				out := a.exchangeAmountValidation()
				if out.SwapValid || out.DepositValid || out.WithdrawValid || value != "" && out.InputError == "" || value == "" && out.InputError != "" {
					t.Fatal("malformed value enabled economic operation or premature error", provider, direction, value, out)
				}
				for _, method := range []string{"exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
					if a.validateExchangeAmountAction(method, exchangeValidationRequest(a, method)) {
						t.Fatal("invalid amount reached operation", method, value)
					}
				}
				called := 0
				core.handler = func(_ uint64, method string, _, reply any) error {
					called++
					if method != "exchangeQuote" {
						t.Error("rule discovery changed API operation", method)
					}
					return answer(reply, json.RawMessage(`{}`))
				}
				a.event(webui.Event{ID: "exchangeQuote"})
				ui.apply(t)
				if called != 1 || a.busy {
					t.Fatal("invalid numeric draft blocked metadata discovery", value, called)
				}
			}
		}
	}
}

func TestExchangeAmountValidationBindsFreshProviderWalletAssetAndPairRules(t *testing.T) {
	for _, change := range []func(*exchangeInputQuote){
		func(q *exchangeInputQuote) { q.Provider = "binance" }, func(q *exchangeInputQuote) { q.AccountID = "foreign" }, func(q *exchangeInputQuote) { q.AssetID = "foreign" }, func(q *exchangeInputQuote) { q.Coin = "NEAR" }, func(q *exchangeInputQuote) { q.Side = "Buy" }, func(q *exchangeInputQuote) { q.QuoteCoin = "USDC" }, func(q *exchangeInputQuote) { q.ObservedAt = "0" }, func(q *exchangeInputQuote) { q.ObservedAt = nowToExchangeMilliseconds(time.Now().Add(3 * time.Second)) }, func(q *exchangeInputQuote) { q.ObservedAt = "01" },
	} {
		a, _, _ := fixture(t)
		account, asset := selectedExchangeValidationAsset(t, a)
		now := time.Now()
		a.fields["exchangeAmount"] = "1.25"
		q := exchangeValidationQuote(a, account, asset, "Sell", "USDT", now)
		change(&q)
		setExchangeValidationQuote(a, q)
		out := a.exchangeAmountValidationAt(now)
		if out.SwapValid || !strings.Contains(out.SwapError, "Refresh market data") {
			t.Fatal("stale/foreign cached rules enabled Swap", out)
		}
	}
}

func TestExchangeSpendUsesExactVenueStepAndNotionalBounds(t *testing.T) {
	for _, tc := range []struct {
		direction, value, want string
		valid                  bool
	}{
		{"Sell", "0.0000001", "multiple", false}, {"Sell", "101", "maximum", false}, {"Sell", "0.05", "minimum", false}, {"Sell", "0.050252", "", true}, {"Sell", "100", "", true},
		{"Buy", "5.001", "multiple", false}, {"Buy", "4.99", "minimum", false}, {"Buy", "10001", "maximum", false}, {"Buy", "5", "", true}, {"Buy", "10000", "", true},
	} {
		a, _, _ := fixture(t)
		account, asset := selectedExchangeValidationAsset(t, a)
		now := time.Now()
		a.fields["exchangeAmount"], a.fields["side"] = tc.value, tc.direction
		q := exchangeValidationQuote(a, account, asset, tc.direction, "USDT", now)
		setExchangeValidationQuote(a, q)
		out := a.exchangeAmountValidationAt(now)
		if out.SwapValid != tc.valid || !strings.Contains(out.SwapError, tc.want) {
			t.Fatal("exact step/notional bounds changed", tc, out)
		}
	}
}

func TestExchangeBuyQuotePrecisionIsSeparateFromDepositWithdrawalAssetPrecision(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	asset := a.network(account.Network).Assets[2]
	now := time.Now()
	a.fields["exchangeAccount"], a.fields["exchangeAsset"], a.fields["side"], a.fields["exchangeAmount"] = account.ID, asset.ID, "Buy", "0.00000001"
	q := exchangeValidationQuote(a, account, asset, "Buy", "USDT", now)
	q.QuotePrecision, q.MinOrderAmount = "0.00000001", "0.00000001"
	setExchangeValidationQuote(a, q)
	out := a.exchangeAmountValidationAt(now)
	if asset.Decimals != 6 || !out.SwapValid || out.DepositValid || out.WithdrawValid {
		t.Fatal("offchain quote budget confused with network token precision", asset, out)
	}
	a.fields["side"] = "Sell"
	q.Side = "Sell"
	q.BasePrecision = "0.00000001"
	setExchangeValidationQuote(a, q)
	if out = a.exchangeAmountValidationAt(now); out.SwapValid || out.InputError == "" {
		t.Fatal("sell accepted fractions below selected asset quantum", out)
	}
}

func TestExchangeKnownRouteMinimaAndSlippageDoNotAuthorizeUnknownRules(t *testing.T) {
	a, _, _ := fixture(t)
	account, asset := selectedExchangeValidationAsset(t, a)
	now := time.Now()
	a.fields["exchangeAmount"] = "0.1"
	q := exchangeValidationQuote(a, account, asset, "Sell", "USDT", now)
	q.Route.MinDeposit, q.Route.MinWithdraw = "0.2", "0.3"
	setExchangeValidationQuote(a, q)
	out := a.exchangeAmountValidationAt(now)
	if out.DepositValid || out.WithdrawValid || !strings.Contains(out.DepositError, "minimum deposit") || !strings.Contains(out.WithdrawError, "minimum withdrawal") {
		t.Fatal("known network route minima ignored", out)
	}
	q.Route.DepositEnabled, q.Route.WithdrawEnabled = false, false
	setExchangeValidationQuote(a, q)
	out = a.exchangeAmountValidationAt(now)
	if out.DepositValid || out.WithdrawValid {
		t.Fatal("known paused route enabled", out)
	}
	for _, input := range []string{"0", "0.001", "1.01", "1e-1", "-1", "NaN"} {
		a.fields["slippage"] = input
		out = a.exchangeAmountValidationAt(now)
		if out.SwapValid || out.SlippageError == "" {
			t.Fatal("invalid slippage enabledSwap", input, out)
		}
	}
	for _, step := range []string{"0", "-1", "1e2", "invalid"} {
		a.fields["slippage"] = "0.5"
		q.BasePrecision = step
		setExchangeValidationQuote(a, q)
		out = a.exchangeAmountValidationAt(now)
		if out.SwapValid || !strings.Contains(out.SwapError, "Refresh market data") {
			t.Fatal("malformed metadata became amount approval", step, out)
		}
	}
	a.quote = nil
	a.fields["slippage"] = "1e-1"
	if out := a.exchangeAmountValidationAt(now); out.SwapValid || out.SlippageError == "" {
		t.Fatal("invalid slippage feedback incorrectly requires a provider query", out)
	}
}

func TestExchangeFundingGuardsRejectInvalidValuesAndChangedRequestBeforeAPI(t *testing.T) {
	a, ui, core := fixture(t)
	account, asset := selectedExchangeValidationAsset(t, a)
	a.fields["exchangeAmount"] = "1e2"
	var calls atomic.Int32
	core.handler = func(uint64, string, any, any) error { calls.Add(1); return nil }
	for _, method := range []string{"exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
		a.event(webui.Event{ID: method})
		if a.busy || calls.Load() != 0 || len(ui.queue) != 0 {
			t.Fatal("invalid amount reached API or approval", method)
		}
	}
	a.fields["exchangeAmount"] = "1.25"
	q := exchangeValidationQuote(a, account, asset, "Sell", "USDT", time.Now())
	setExchangeValidationQuote(a, q)
	for _, method := range []string{"exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
		body := exchangeValidationRequest(a, method)
		if !a.validateExchangeAmountAction(method, body) {
			t.Fatal("valid unchanged request denied", method, a.status)
		}
		for _, key := range []string{"provider", "accountId", "assetId", "amount"} {
			body := exchangeValidationRequest(a, method)
			body[key] = "changed"
			if a.validateExchangeAmountAction(method, body) {
				t.Fatal("changed payload authorized", method, key)
			}
		}
	}
}

func TestExchangeAmountValidationRetainsMainnetOnlyExchangePolicy(t *testing.T) {
	a, _, _ := fixture(t)
	account, asset := selectedExchangeValidationAsset(t, a)
	a.fields["exchangeAmount"] = "10.25"
	setExchangeValidationQuote(a, exchangeValidationQuote(a, account, asset, "Sell", "USDT", time.Now()))
	for i := range a.networks {
		if a.networks[i].ID == account.Network {
			a.networks[i].Testnet = true
		}
	}
	if out := a.exchangeAmountValidation(); out.DepositValid || out.WithdrawValid || out.SwapValid {
		t.Fatal("testnet network acquired a mainnet exchange route", out)
	}
}
