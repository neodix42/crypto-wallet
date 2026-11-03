package walletcore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
)

// Owned inherited-pipe/HTTP protocol fixtures only. Real vault authorization
// and wire parsing have independent signer/client tests; no funds are sent.
type wizardExchange struct {
	*fixtureExchangeAPI
	depositPage func(string, string) (bybit.DepositPage, error)
	listed      map[string]bool
}

func (a *wizardExchange) DepositRecords(_ context.Context, coin, tx, cursor string) (bybit.DepositPage, error) {
	if a.depositPage != nil {
		return a.depositPage(tx, cursor)
	}
	return bybit.DepositPage{Rows: []bybit.DepositRecord{{ID: "original-credit", Coin: coin, Chain: "ETH", ToAddress: a.deposit.Address, TxID: tx, Amount: "0.1", DepositFee: "0", Status: 3, DepositType: "0", TravelRuleStatus: 0}}}, nil
}
func (a *wizardExchange) SpotInstrument(_ context.Context, symbol string) (bybit.Instrument, error) {
	if a.listed != nil && !a.listed[symbol] {
		return bybit.Instrument{}, bybit.ErrNotListed
	}
	base, quote, ok := bybit.SpotPair(symbol)
	if !ok {
		return bybit.Instrument{}, bybit.ErrNotListed
	}
	v := bybit.Instrument{Symbol: symbol, BaseCoin: base, QuoteCoin: quote, Status: "Trading"}
	v.LotSizeFilter.BasePrecision = "0.000001"
	v.LotSizeFilter.QuotePrecision = "0.01"
	v.LotSizeFilter.MinOrderAmt = "5"
	v.LotSizeFilter.MaxMarketOrderQty = "100"
	v.PriceFilter.TickSize = "0.01"
	return v, nil
}
func wizardCoordinator(t *testing.T) (*core, *wizardExchange, *publicAdapter, string, exchange.WizardState) {
	t.Helper()
	c, api, adapter, path := fixtureExchangeCoordinator(t)
	wrapped := &wizardExchange{fixtureExchangeAPI: api, listed: map[string]bool{"ETHUSDT": true}}
	factory := c.newExchange
	c.newExchange = func(endpoint string, auth bybit.Authorizer) (exchangeAPI, error) {
		_, err := factory(endpoint, auth)
		return wrapped, err
	}
	w := exchange.WizardState{ID: strings.Repeat("a", 32), Step: 1, SourceAccount: "exchange-account", SourceAsset: "ethereum-mainnet:native", DepositAmount: "0.1"}
	if _, err := c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	return c, wrapped, adapter, path, w
}
func loadWizard(t *testing.T, c *core) exchange.WizardState {
	t.Helper()
	out, err := c.handle(message("exchangeWizardLoad", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	w := out.(exchangeWizardSnapshot).Wizard
	if w == nil {
		t.Fatal("missing persisted workflow")
	}
	return *w
}
func depositWizard(t *testing.T, c *core, api *wizardExchange, w exchange.WizardState) exchange.WizardState {
	t.Helper()
	r := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": w.DepositAmount, "wizardId": w.ID, "expectedAddress": api.deposit.Address, "expectedMemo": api.deposit.Tag})
	if r.Kind != "deposit" || r.State != "CONFIRMED" {
		t.Fatal(r)
	}
	w = loadWizard(t, c)
	if w.DepositOperation != r.ID {
		t.Fatal("deposit lost original identity")
	}
	return w
}

func TestExchangeWizardCoordinatorFourSeparatelyAuthorizedStepsAndResume(t *testing.T) {
	c, api, adapter, path, w := wizardCoordinator(t)
	w = depositWizard(t, c, api, w)
	out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
	if err != nil || !out.(exchangeDepositCheck).Ready {
		t.Fatal("exact deposit credit missing", err)
	}
	w.Step = 2
	if _, err = c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	out, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": "0.1"}))
	if err != nil {
		t.Fatal(err)
	}
	transfer := out.(exchangeResult)
	if transfer.State != "CONFIRMED" || api.transfer.FromAccountType != "FUND" || api.transfer.ToAccountType != "UNIFIED" || api.transfer.Amount != "0.1" {
		t.Fatal("wrong funding move", transfer)
	}
	w = loadWizard(t, c)
	w.Step = 3
	if _, err = c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	out, err = c.handle(exchangeMessage("exchangeSwapOptions", nil))
	if err != nil {
		t.Fatal(err)
	}
	pairs := out.([]exchangeSwapChoice)
	if len(pairs) != 1 || pairs[0].Symbol != "ETHUSDT" || pairs[0].OutputCoin != "USDT" {
		t.Fatal("invented pair or unsupported output", pairs)
	}
	w.TradeAccount, w.TradeAsset, w.TradeSide, w.TradeAmount, w.QuoteCoin = w.SourceAccount, w.SourceAsset, "Sell", "0.1", "USDT"
	if _, err = c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	swap := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"wizardId": w.ID, "side": "Sell", "amount": "0.1", "slippage": "0.5"})
	if swap.State != "CONFIRMED" || swap.FilledQuote != "200" || swap.Fees["USDT"] != "0.2" {
		t.Fatal("actual fills or fees absent", swap)
	}
	// Reloading a terminal order reconstructs the actual proceeds through GETs.
	posts := api.posts
	out, err = c.handle(message("exchangeReconcile", map[string]string{"id": swap.ID}))
	if err != nil {
		t.Fatal(err)
	}
	resumed := out.(exchangeResult)
	if resumed.FilledBase != swap.FilledBase || resumed.FilledQuote != swap.FilledQuote || resumed.Fees["USDT"] != "0.2" || api.posts != posts {
		t.Fatal("resume lost execution result or replayed order", resumed)
	}
	w = loadWizard(t, c)
	w.Step = 4
	w.OutputCoin, w.OutputAmount = "USDT", "199.8"
	if _, err = c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	out, err = c.handle(message("exchangeWithdrawOptions", map[string]string{"coin": "USDT"}))
	if err != nil {
		t.Fatal(err)
	}
	choices := out.([]exchangeWithdrawalChoice)
	if len(choices) != 1 || choices[0].Address != "0x1111111111111111111111111111111111111111" || choices[0].AssetID != "ethereum-mainnet:usdt" {
		t.Fatal("withdrawal address not owned", choices)
	}
	w.WithdrawAccount, w.WithdrawAsset, w.WithdrawAmount = choices[0].AccountID, choices[0].AssetID, "199.8"
	api.chain.WithdrawMax = "1000" // synthetic USDT route, not the ETH fixture's 10-coin cap
	if _, err = c.handle(message("exchangeWizardSave", w)); err != nil {
		t.Fatal(err)
	}
	withdraw := exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"wizardId": w.ID, "assetId": w.WithdrawAsset, "amount": w.WithdrawAmount})
	if withdraw.State != "EXCHANGE_COMPLETED" || api.withdrawal.Address != choices[0].Address || api.withdrawal.AccountType != "FUND" || api.withdrawal.FeeType != 1 {
		t.Fatal("withdrawal lost route or inclusive fee policy", withdraw)
	}
	if api.posts != 3 || adapter.submits != 1 || len(readPublicState(t, path)["ops"].([]any)) != 1 {
		t.Fatal("flow sent implicit duplicate payments")
	}
}

func TestExchangeWizardDepositPreviewChangedDestinationPreventsPreparation(t *testing.T) {
	for _, field := range []string{"address", "memo"} {
		t.Run(field, func(t *testing.T) {
			c, api, adapter, _, w := wizardCoordinator(t)
			out, err := c.handle(exchangeMessage("exchangeDepositPreview", nil))
			if err != nil {
				t.Fatal(err)
			}
			shown := out.(exchangeDepositPreview)
			if field == "address" {
				api.deposit.Address = "0x3333333333333333333333333333333333333333"
			} else {
				shown.Memo = "unexpected"
			}
			_, err = c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": w.DepositAmount, "wizardId": w.ID, "expectedAddress": shown.Address, "expectedMemo": shown.Memo}))
			if ipc.PublicErrorCode(err) != "EXCHANGE_DEPOSIT_DESTINATION_CHANGED" || adapter.prepares != 0 || adapter.submits != 0 || api.posts != 0 || loadWizard(t, c).DepositOperation != "" {
				t.Fatal("changed destination reached preparation", err)
			}
		})
	}
}

func TestExchangeWizardExactDepositCreditRejectsWrongIdentityRestrictionsAndCursorLoops(t *testing.T) {
	for _, scenario := range []string{"wrongTx", "wrongChain", "wrongAddress", "wrongMemo", "excessCredit", "pending", "aml", "travelRule", "duplicate", "cursorLoop"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, _, w := wizardCoordinator(t)
			w = depositWizard(t, c, api, w)
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				r := bybit.DepositRecord{ID: "original", Coin: "ETH", Chain: "ETH", ToAddress: api.deposit.Address, TxID: tx, Amount: "0.1", DepositFee: "0", Status: 3, DepositType: "0"}
				switch scenario {
				case "wrongTx":
					r.TxID = "different"
				case "wrongChain":
					r.Chain = "BASE"
				case "wrongAddress":
					r.ToAddress = "foreign"
				case "wrongMemo":
					r.Tag = "extra"
				case "excessCredit":
					r.Amount = "0.2"
				case "pending":
					r.Status = 1
				case "aml":
					r.DepositType = "20"
				case "travelRule":
					r.TravelRuleStatus = 1
				}
				page := bybit.DepositPage{Rows: []bybit.DepositRecord{r}}
				if scenario == "duplicate" {
					page.Rows = append(page.Rows, r)
				}
				if scenario == "cursorLoop" {
					page.NextPageCursor = "same"
				}
				return page, nil
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err == nil && out.(exchangeDepositCheck).Ready {
				t.Fatal("unverified/restricted credit advanced workflow", scenario)
			}
			if api.posts != 0 {
				t.Fatal("credit check moved exchange funds")
			}
		})
	}
}

func TestExchangeWizardDepositCreditTONTransactionHashAndAddressCanonicalization(t *testing.T) {
	// The signed external-message identity is not a TON transaction hash.
	// Only equivalent 32-byte transaction hashes may match credit records.
	if !exchangeSameTONHash(strings.Repeat("00", 32), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") || exchangeSameTONHash(strings.Repeat("00", 32), strings.Repeat("11", 32)) || exchangeSameTONHash("synthetic-external-message", "synthetic-external-message") {
		t.Fatal("TON credit identity comparison changed")
	}
	if !exchangeSameAddress("ethereum-mainnet", "0xAabb", "0xaabb") || exchangeSameAddress("near-mainnet", "alice.near", "ALICE.NEAR") {
		t.Fatal("address comparison crossed ledger rules")
	}
}

func TestExchangeWizardTerminalSwapReadCannotRegressOrAuthorizeReplacement(t *testing.T) {
	c, api, _, path := fixtureExchangeCoordinator(t)
	r := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
	before, _ := json.Marshal(readPublicState(t, path)["exchangeOps"])
	api.fail["find"] = context.DeadlineExceeded
	_, err := c.handle(message("exchangeReconcile", map[string]any{"id": r.ID}))
	after, _ := json.Marshal(readPublicState(t, path)["exchangeOps"])
	if err == nil || !strings.Contains(ipc.PublicErrorCode(err), "EXECUTIONS") || !jsonEqual(before, after) || api.posts != 1 {
		t.Fatal("terminal read regressed state or replayed order", err)
	}
}

func jsonEqual(a, b []byte) bool { return string(a) == string(b) }
