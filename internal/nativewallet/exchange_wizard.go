package nativewallet

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
	"localwallet/internal/webui"
)

type wizardDepositPreview struct {
	Provider, AccountID, AssetID, Network, Coin, Address, Memo, Chain, MinDeposit, ObservedAt string
	Unified, CanTransfer, CanTrade, CanWithdraw                                               bool
}
type wizardBalance struct{ AccountType, Coin, Available, WalletBalance string }
type wizardDepositCredit struct {
	Ready, ChainConfirmed                                           bool
	ChainState, Message, ReportedAmount, DepositFee                 string
	Coin, DepositedAmount, TransactionID, ReadError, TransferBudget string
	Balances                                                        []wizardBalance
}
type wizardPair struct{ ID, AccountID, AssetID, Side, QuoteCoin, OutputCoin, Symbol string }
type wizardDestination struct {
	AccountID, AssetID, Network, NetworkName, Coin, Address, Chain, Minimum, Fee, PercentageFee, Precision string
	Index                                                                                                  uint32
}
type wizardResult struct {
	ID, Kind, State, Message, TransactionID, FilledBase, FilledQuote string
	Fees                                                             map[string]string
}
type exchangeWizardUI struct {
	state             *exchange.WizardState
	depositState      string
	preview           *wizardDepositPreview
	credit            *wizardDepositCredit
	balances          []wizardBalance
	pairs             []wizardPair
	destinations      []wizardDestination
	depositChecking   bool
	depositCheckedAt  time.Time
	depositNotified   bool
	depositPollSerial uint64
}

func (a *App) exchangeWizardData() *exchangeWizardUI {
	if a.wizard == nil {
		a.wizard = &exchangeWizardUI{}
	}
	return a.wizard
}
func (a *App) adoptExchangeWizard(raw json.RawMessage) {
	var status struct {
		Provider           string
		Wizard             *exchange.WizardState
		WizardDepositState string
	}
	if json.Unmarshal(raw, &status) != nil || status.Provider != "" && status.Provider != "bybit" {
		return
	}
	a.adoptWizardState(status.Wizard)
	a.exchangeWizardData().depositState = status.WizardDepositState
}
func (a *App) adoptWizardState(w *exchange.WizardState) {
	u := a.exchangeWizardData()
	if w == nil {
		u.state, u.credit, u.balances = nil, nil, nil
		u.depositPollSerial++
		u.depositChecking, u.depositNotified = false, false
		u.depositCheckedAt = time.Time{}
		return
	}
	if !w.Valid() {
		a.errorText("Restore exchange workflow", "The saved workflow is invalid. Original wallet and exchange operations remain intact.")
		return
	}
	old := u.state
	if old == nil || old.ID != w.ID {
		u.credit, u.balances = nil, nil
		u.depositPollSerial++
		u.depositChecking, u.depositNotified = false, false
		u.depositCheckedAt = time.Time{}
	}
	changed := old == nil || old.ID != w.ID || old.Step != w.Step || old.DepositOperation != w.DepositOperation || old.TransferOperation != w.TransferOperation || old.SwapOperation != w.SwapOperation || old.WithdrawOperation != w.WithdrawOperation
	copy := *w
	u.state = &copy
	if !changed {
		return
	}
	a.fields["exchangeAccount"], a.fields["exchangeAsset"], a.fields["exchangeAmount"] = w.SourceAccount, w.SourceAsset, w.DepositAmount
	a.fields["wizardTransferAmount"] = w.TransferAmount
	if a.fields["wizardTransferAmount"] == "" {
		a.fields["wizardTransferAmount"] = w.DepositAmount
	}
	a.fields["wizardTradeAmount"] = w.TradeAmount
	if a.fields["wizardTradeAmount"] == "" {
		a.fields["wizardTradeAmount"] = w.TransferAmount
	}
	a.fields["wizardWithdrawAmount"] = w.WithdrawAmount
	if a.fields["wizardWithdrawAmount"] == "" {
		a.fields["wizardWithdrawAmount"] = w.OutputAmount
	}
	if w.WithdrawAccount != "" {
		a.fields["wizardDestination"] = w.WithdrawAccount + "|" + w.WithdrawAsset
	}
	u.pairs = nil
	u.destinations = nil
	a.quote = nil
}
func (a *App) wizardSource() (model.Account, model.Asset, bool) {
	accountID, assetID := a.fields["exchangeAccount"], a.fields["exchangeAsset"]
	if a.wizard != nil && a.wizard.state != nil && (a.wizard.state.Step > 1 || a.wizard.state.DepositOperation != "") {
		accountID, assetID = a.wizard.state.SourceAccount, a.wizard.state.SourceAsset
	}
	account, ok := a.account(accountID)
	if !ok {
		return account, model.Asset{}, false
	}
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID == assetID && exchangeAssetAllowed(account, asset) {
			return account, exchange.LedgerAsset(asset), true
		}
	}
	return account, model.Asset{}, false
}
func wizardAvailable(balances []wizardBalance, kind, coin string) string {
	for _, b := range balances {
		if b.AccountType == kind && b.Coin == coin {
			return b.Available
		}
	}
	return ""
}
func wizardPositive(value string, decimals uint8) bool {
	v, e := amount.Parse(value, decimals)
	return e == nil && v != "0"
}
func wizardAtMost(value, cap string) bool {
	x, e := exchangeInputAtomic(value)
	y, f := exchangeInputAtomic(cap)
	return e == nil && f == nil && x.Sign() > 0 && x.Cmp(y) <= 0
}
func (a *App) wizardPreviewCurrent() bool {
	u := a.exchangeWizardData()
	p := u.preview
	if p == nil || p.Provider != "bybit" || p.AccountID != a.fields["exchangeAccount"] || p.AssetID != a.fields["exchangeAsset"] || p.Address == "" {
		return false
	}
	t, e := time.Parse(time.RFC3339, p.ObservedAt)
	return e == nil && time.Since(t) >= -time.Second && time.Since(t) < 2*time.Minute
}
func (a *App) wizardChosenPair() (wizardPair, bool) {
	for _, p := range a.exchangeWizardData().pairs {
		if p.ID == a.fields["wizardPair"] {
			return p, true
		}
	}
	return wizardPair{}, false
}
func (a *App) wizardChosenDestination() (wizardDestination, bool) {
	for _, d := range a.exchangeWizardData().destinations {
		if d.AccountID+"|"+d.AssetID == a.fields["wizardDestination"] {
			return d, true
		}
	}
	return wizardDestination{}, false
}
func (a *App) wizardTradeValidation() exchangeInputValidation {
	p, ok := a.wizardChosenPair()
	if !ok {
		return exchangeInputValidation{SwapError: "Refresh the available pairs and select the asset to receive."}
	}
	side := p.Side
	if p.QuoteCoin == "USDC" {
		side += "USDC"
	}
	fields := map[string]string{"exchangeAccount": p.AccountID, "exchangeAsset": p.AssetID, "side": side, "exchangeAmount": a.fields["wizardTradeAmount"], "slippage": a.fields["slippage"]}
	v := a.exchangeAmountValidationFieldsAt(fields, a.quote, time.Now())
	if w := a.exchangeWizardData().state; w != nil && !wizardAtMost(a.fields["wizardTradeAmount"], w.TransferAmount) {
		v.SwapValid = false
		v.SwapError = "Amount exceeds the assets moved by this workflow to Unified."
		v.InputError = v.SwapError
	}
	return v
}
func (a *App) wizardDestinationNodes() []webui.Node {
	p := a.exchangeWizardData().preview
	if p == nil {
		return nil
	}
	copy := node("button", "copyReceive/exchange-deposit", "Copy address", p.Address)
	nodes := []webui.Node{row(node("address", "", "Bybit deposit address", p.Address), copy), label("Deposit network: " + p.Chain + " · " + p.Coin)}
	if p.Memo != "" {
		nodes = append(nodes, node("address", "", "Required deposit memo", p.Memo))
	} else {
		nodes = append(nodes, label("Bybit explicitly reports no deposit memo for this address."))
	}
	return nodes
}

func (a *App) exchangeWizardPage() []webui.Node {
	u := a.exchangeWizardData()
	step := 1
	if u.state != nil {
		step = u.state.Step
	}
	var connection struct {
		Connected bool
		Provider  string
	}
	_ = json.Unmarshal(a.exchange, &connection)
	disabled := a.state.Locked || !a.state.RecoveryConfirmed || !connection.Connected || connection.Provider != "" && connection.Provider != "bybit"
	steps := node("row", "exchangeSteps", "Exchange steps", strconv.Itoa(step))
	for _, text := range []string{"Deposit", "Funding → Unified", "Swap", "Withdraw"} {
		steps.Children = append(steps.Children, label(text))
	}
	nodes := []webui.Node{a.exchangeProviderSelector(), heading("Bybit Exchange"), steps}
	if disabled {
		nodes = append(nodes, label("Connect your Bybit account in Settings → Exchange to enable these actions."))
	}
	account, asset, sourceOK := a.wizardSource()
	switch step {
	case 1:
		nodes = append(nodes, heading("1. Deposit to Funding"))
		accounts := []webui.Option{}
		for _, account := range a.state.Accounts {
			if !account.Hidden || u.state != nil && u.state.DepositOperation != "" && u.state.SourceAccount == account.ID {
				accounts = append(accounts, webui.Option{ID: account.ID, Text: a.network(account.Network).Name + " · " + a.groupTitle(account.Index)})
			}
		}
		assets := []webui.Option{}
		if account.ID != "" {
			for _, candidate := range a.network(account.Network).Assets {
				if exchangeAssetAllowed(account, candidate) {
					assets = append(assets, webui.Option{ID: candidate.ID, Text: candidate.Symbol})
				}
			}
		}
		locked := u.state != nil && u.state.DepositOperation != ""
		network := a.combo("exchangeAccount", "Network / source account", accounts)
		network.Disabled = network.Disabled || locked
		currency := a.combo("exchangeAsset", "Asset to deposit", assets)
		currency.Disabled = currency.Disabled || locked
		nodes = append(nodes, row(network, currency))
		if sourceOK {
			nodes = append(nodes, row(node("address", "", "Owned wallet receive address", account.Address), a.addressCopyButton(account, "Copy address")))
			if asset.Contract != "" {
				nodes = append(nodes, node("address", "", "Token contract (not your receive address)", asset.Contract))
			}
		}
		if sourceOK {
			value, hint := a.balance(account, asset)
			balance := label("Wallet available: " + value + " " + asset.Symbol)
			balance.Tooltip = hint + ". Native transaction fees and required reserves are checked before approval."
			nodes = append(nodes, balance)
		}
		nodes = append(nodes, a.button("wizardRefreshDeposit", "Refresh market / deposit address", disabled || !sourceOK || locked))
		nodes = append(nodes, a.wizardDestinationNodes()...)
		field := a.entry("exchangeAmount", "Deposit amount ("+asset.Symbol+")")
		field.InputMode = "decimal"
		field.Disabled = field.Disabled || locked
		valid := sourceOK && wizardPositive(field.Value, asset.Decimals)
		if field.Value != "" && !valid {
			field.Error = fmt.Sprintf("Enter a positive %s amount with at most %d decimals.", asset.Symbol, asset.Decimals)
		}
		ready := a.wizardPreviewCurrent()
		if ready {
			p := u.preview
			if !p.Unified || !p.CanTransfer || !p.CanTrade || !p.CanWithdraw {
				valid = false
				nodes = append(nodes, label("This workflow needs a Unified master account with Spot Trade, Account Transfer and Withdrawal permissions."))
			}
			if valid && !exchangeInputAtLeast(field.Value, p.MinDeposit) {
				valid = false
				field.Error = "Minimum deposit: " + p.MinDeposit + " " + asset.Symbol + "."
			}
		}
		nodes = append(nodes, field)
		if !locked {
			nodes = append(nodes, a.button("wizardDeposit", "Deposit", disabled || !valid || !ready))
		} else {
			nodes = append(nodes, label("Original deposit: "+u.state.DepositOperation), label("Checking the original transaction and Bybit credit automatically."), a.button("wizardCheckDeposit", "Refresh deposit confirmation", disabled))
			if u.depositState == "PREPARED" || u.depositState == "AWAITING_CONFIRMATION" || u.depositState == "SIGNED_NOT_SUBMITTED" {
				nodes = append(nodes, a.button("wizardPendingDeposit", "Resume original deposit", disabled))
			}
			if u.depositState == "PREPARED" || u.depositState == "CANCELLED" || u.depositState == "MISSING" {
				nodes = append(nodes, a.wizardClearButton(u.state.DepositOperation, disabled)...)
			}
		}
		if u.credit != nil {
			nodes = append(nodes, label(u.credit.Message))
		}
	case 2:
		w := u.state
		nodes = append(nodes, heading("2. Move Funding to Unified"), label("Asset: "+asset.Symbol+" · Network: "+a.network(account.Network).Name))
		deposited := w.DepositAmount
		if u.credit != nil && u.credit.DepositedAmount != "" {
			deposited = u.credit.DepositedAmount
		}
		nodes = append(nodes, label("On-chain deposit: "+deposited+" "+asset.Symbol), label("Original deposit: "+w.DepositOperation))
		if u.credit != nil {
			nodes = append(nodes, label(u.credit.Message))
			if u.credit.TransactionID != "" {
				nodes = append(nodes, node("address", "", "Deposit transaction", u.credit.TransactionID))
			}
			if u.credit.Ready && u.credit.ReportedAmount != "" {
				nodes = append(nodes, label("Bybit reported deposit amount: "+u.credit.ReportedAmount+" "+asset.Symbol))
				if u.credit.DepositFee != "" {
					nodes = append(nodes, label("Bybit reported deposit fee: "+u.credit.DepositFee+" "+asset.Symbol))
				} else {
					nodes = append(nodes, label("Bybit deposit fee: not reported"))
				}
				nodes = append(nodes, label("Conservative transfer limit: "+wizardTransferLimit(u.credit)+" "+asset.Symbol))
			}
		} else {
			nodes = append(nodes, label("Deposit confirmed on-chain. Checking Bybit credit and account balances…"))
		}
		for _, kind := range []string{"FUND", "UNIFIED"} {
			name := "Funding"
			if kind == "UNIFIED" {
				name = "Unified"
			}
			available := wizardAvailable(u.balances, kind, asset.Symbol)
			if available == "" {
				nodes = append(nodes, label(name+" balance: not yet available"))
			} else {
				nodes = append(nodes, label(name+" available: "+available+" "+asset.Symbol))
				for _, balance := range u.balances {
					if balance.AccountType == kind && balance.Coin == asset.Symbol && balance.WalletBalance != "" {
						nodes = append(nodes, label(name+" total: "+balance.WalletBalance+" "+asset.Symbol))
					}
				}
			}
		}
		if len(u.balances) > 0 && !exchangeInputAtLeast(wizardAvailable(u.balances, "FUND", asset.Symbol), a.fields["wizardTransferAmount"]) {
			nodes = append(nodes, label("The amount to move exceeds the current Funding availability. Receiving or deposit fees can make this lower than the on-chain amount. Once credit is verified, the suggested amount uses the available Funding balance."))
		}
		field := a.entry("wizardTransferAmount", "Amount to move ("+asset.Symbol+")")
		field.InputMode = "decimal"
		field.Disabled = field.Disabled || w.TransferOperation != ""
		ceiling := w.DepositAmount
		if u.credit != nil && u.credit.Ready && u.credit.ReportedAmount != "" {
			ceiling = wizardTransferLimit(u.credit)
		}
		valid := wizardPositive(field.Value, asset.Decimals) && wizardAtMost(field.Value, ceiling)
		if field.Value != "" && !valid {
			field.Error = "Enter a positive amount no greater than the credited deposit."
		} else if field.Value != "" && wizardAvailable(u.balances, "FUND", asset.Symbol) != "" && !exchangeInputAtLeast(wizardAvailable(u.balances, "FUND", asset.Symbol), field.Value) {
			field.Error = "Amount exceeds the currently available Funding balance."
		}
		nodes = append(nodes, field, a.button("wizardRefreshFunding", "Refresh Funding / Unified balances", disabled))
		if w.TransferOperation == "" {
			nodes = append(nodes, a.button("wizardMove", "Move to Unified", disabled || !wizardCreditReady(u.credit) || !valid || !exchangeInputAtLeast(wizardAvailable(u.balances, "FUND", asset.Symbol), field.Value)))
		} else {
			nodes = append(nodes, label("Original Funding transfer: "+w.TransferOperation), a.button("wizardCheckTransfer", "Check transfer and continue", disabled))
			nodes = append(nodes, a.wizardClearButton(w.TransferOperation, disabled)...)
		}
	case 3:
		w := u.state
		nodes = append(nodes, heading("3. Swap on Spot"), label("Moved to Unified: "+w.TransferAmount+" "+asset.Symbol))
		opts := []webui.Option{}
		for _, p := range u.pairs {
			opts = append(opts, webui.Option{ID: p.ID, Text: p.OutputCoin + " · " + p.Symbol})
		}
		selector := a.combo("wizardPair", "Asset to receive / available pair", opts)
		selector.Disabled = selector.Disabled || w.SwapOperation != ""
		nodes = append(nodes, a.button("wizardRefreshPairs", "Find available pairs with wallet withdrawal routes", disabled || w.SwapOperation != ""), selector)
		p, chosen := a.wizardChosenPair()
		if chosen {
			nodes = append(nodes, a.button("wizardRefreshMarket", "Refresh market", disabled || w.SwapOperation != ""))
		}
		nodes = append(nodes, wizardMarketNodes(a.quote)...)
		nodes = append(nodes, a.wizardDestinationNodes()...)
		validation := a.wizardTradeValidation()
		field := a.entry("wizardTradeAmount", "Amount to spend ("+asset.Symbol+")")
		field.InputMode = "decimal"
		field.Error = validation.InputError
		field.Disabled = field.Disabled || w.SwapOperation != ""
		slip := a.entry("slippage", "Slippage limit (%)")
		slip.InputMode = "decimal"
		slip.Error = validation.SlippageError
		slip.Disabled = slip.Disabled || w.SwapOperation != ""
		nodes = append(nodes, row(field, slip))
		if w.SwapOperation == "" {
			button := a.button("wizardSwap", "Swap", disabled || !chosen || !validation.SwapValid)
			button.Tooltip = validation.SwapError
			nodes = append(nodes, button)
		} else {
			nodes = append(nodes, label("Original spot order: "+w.SwapOperation), a.button("wizardCheckSwap", "Check swap and continue", disabled))
			nodes = append(nodes, a.wizardClearButton(w.SwapOperation, disabled)...)
		}
		_ = p
	case 4:
		w := u.state
		nodes = append(nodes, heading("4. Withdraw to your wallet"), label("Received after swap fees: "+w.OutputAmount+" "+w.OutputCoin), a.button("wizardRefreshWithdraw", "Refresh withdrawal networks and fees", disabled || w.WithdrawOperation != ""))
		opts := []webui.Option{}
		for _, d := range u.destinations {
			opts = append(opts, webui.Option{ID: d.AccountID + "|" + d.AssetID, Text: d.NetworkName + " · " + a.groupTitle(d.Index)})
		}
		selector := a.combo("wizardDestination", "Network / destination account", opts)
		selector.Disabled = selector.Disabled || w.WithdrawOperation != ""
		nodes = append(nodes, selector)
		d, chosen := a.wizardChosenDestination()
		if chosen {
			nodes = append(nodes, label("Asset: "+d.Coin), row(node("address", "", "Withdrawal address", d.Address), a.addressCopyButton(model.Account{ID: "exchange-withdrawal", Address: d.Address}, "Copy address")), label("Minimum: "+d.Minimum+" "+d.Coin+" · Network fee: "+d.Fee+" "+d.Coin+" · Percentage fee: "+d.PercentageFee))
			if full, e := exchangeInputAtomic(w.OutputAmount); e == nil {
				if debit, e := exchangeInputAtomic(a.fields["wizardWithdrawAmount"]); e == nil && full.Cmp(debit) > 0 {
					remainder, _ := amount.Format(new(big.Int).Sub(full, debit).String(), 36)
					nodes = append(nodes, label("Remaining on Bybit after this debit: "+trimDecimal(remainder)+" "+w.OutputCoin+". The default debit is rounded down only to the selected network's withdrawal precision."))
				}
			}
		}
		field := a.entry("wizardWithdrawAmount", "Total withdrawal debit ("+w.OutputCoin+")")
		field.InputMode = "decimal"
		field.Disabled = field.Disabled || w.WithdrawOperation != ""
		precision, e := strconv.ParseUint(d.Precision, 10, 8)
		valid := chosen && e == nil && precision <= 36 && wizardPositive(field.Value, uint8(precision)) && wizardAtMost(field.Value, w.OutputAmount) && exchangeInputAtLeast(field.Value, d.Minimum)
		if field.Value != "" && chosen && !valid {
			field.Error = "Use the network's withdrawal precision and minimum, within the received amount. The debit includes the Bybit withdrawal fee."
		}
		nodes = append(nodes, field)
		if w.WithdrawOperation == "" {
			nodes = append(nodes, a.button("wizardWithdraw", "Withdraw", disabled || !valid))
		} else {
			nodes = append(nodes, label("Original withdrawal: "+w.WithdrawOperation), a.button("wizardCheckWithdraw", "Check original withdrawal", disabled))
			nodes = append(nodes, a.wizardClearButton(w.WithdrawOperation, disabled)...)
		}
	case 5:
		nodes = append(nodes, heading("Exchange workflow completed"), label("Bybit reports the original withdrawal completed. Refresh the destination wallet to verify incoming chain activity."), a.button("wizardRestart", "Start another exchange", disabled))
	}
	if a.result != "" && a.resultOwner == "exchange" {
		nodes = append(nodes, label(a.result))
	}
	nodes = append(nodes, a.button("exchangeStatus", "Refresh Bybit status", false))
	nodes = append(nodes, a.button("wizardAdvanced", "Manage existing Bybit funds", false))
	return append(nodes, a.exchangePendingOperationNodes()...)
}

func (a *App) exchangePendingOperationNodes() []webui.Node {
	var status struct {
		Operations []struct{ ID, Kind, State string }
	}
	if json.Unmarshal(a.exchange, &status) != nil {
		return nil
	}
	nodes := []webui.Node{}
	for _, o := range status.Operations {
		switch o.State {
		case "UNKNOWN", "SUBMITTED", "SUBMITTING", "PENDING", "PARTIALLY_FILLED", "ACCEPTED":
			nodes = append(nodes, a.button("exchangeReconcile/"+o.ID, "Check original "+o.Kind+" · "+o.ID, false))
		}
	}
	return nodes
}

func wizardNetOutput(result wizardResult, choice wizardPair) (string, error) {
	value := result.FilledQuote
	if choice.Side == "Buy" {
		value = result.FilledBase
	}
	v, e := exchangeInputAtomic(value)
	if e != nil || v.Sign() <= 0 {
		return "", ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
	}
	if fee := result.Fees[choice.OutputCoin]; fee != "" {
		f, e := exchangeInputAtomic(fee)
		if e != nil || f.Sign() < 0 || f.Cmp(v) >= 0 {
			return "", ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
		}
		v.Sub(v, f)
	}
	formatted, err := amount.Format(v.String(), 36)
	return trimDecimal(formatted), err
}

type wizardUpdate struct {
	preview        *wizardDepositPreview
	credit         *wizardDepositCredit
	balances       []wizardBalance
	pairs          []wizardPair
	destinations   []wizardDestination
	quote          json.RawMessage
	result         json.RawMessage
	walletBalances []model.Balance
	walletAccount  string
	walletError    bool
}

func wizardMarketNodes(raw json.RawMessage) []webui.Node {
	var q struct {
		Symbol, QuoteCoin, LastPrice, BidPrice, AskPrice, FeeRate, BasePrecision, QuotePrecision, MinOrderAmount, MaxOrderQty string
		Balances                                                                                                              []wizardBalance
	}
	if len(raw) == 0 || json.Unmarshal(raw, &q) != nil || q.Symbol == "" {
		return nil
	}
	nodes := []webui.Node{label("Spot pair: " + q.Symbol), label("Last price: " + q.LastPrice + " " + q.QuoteCoin), label("Bid: " + q.BidPrice + " · Ask: " + q.AskPrice + " · Taker fee: " + q.FeeRate), label("Base step: " + q.BasePrecision + " · Quote step: " + q.QuotePrecision + " · Minimum order: " + q.MinOrderAmount + " " + q.QuoteCoin)}
	for _, b := range q.Balances {
		if b.AccountType == "UNIFIED" {
			nodes = append(nodes, label("Unified available: "+b.Available+" "+b.Coin))
		}
	}
	return nodes
}

func (a *App) saveWizardAt(g uint64, w *exchange.WizardState) error {
	var result struct{ Wizard *exchange.WizardState }
	if err := a.callAt(g, "exchangeWizardSave", w, &result); err != nil {
		return err
	}
	if result.Wizard == nil || !result.Wizard.Valid() || result.Wizard.ID != w.ID {
		return ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
	}
	*w = *result.Wizard
	return nil
}

func (a *App) wizardWork(title string, fn func(uint64) (wizardUpdate, error)) {
	if a.wizard != nil {
		// A delayed read must not overwrite an action or its freshly loaded state.
		a.wizard.depositPollSerial++
		a.wizard.depositChecking = false
	}
	revision := a.exchangeRevision
	var update wizardUpdate
	var saved *exchange.WizardState
	var operations json.RawMessage
	var depositState string
	loaded := false
	apply := func() {
		if loaded {
			a.adoptWizardState(saved)
			a.exchangeWizardData().depositState = depositState
			var status map[string]json.RawMessage
			if json.Unmarshal(a.exchange, &status) == nil && len(operations) != 0 {
				status["operations"] = operations
				a.exchange, _ = json.Marshal(status)
			}
		}
		if a.exchangeRevision != revision {
			return
		}
		u := a.exchangeWizardData()
		if update.preview != nil {
			u.preview = update.preview
		}
		if update.credit != nil {
			u.credit = update.credit
		}
		if update.balances != nil {
			u.balances = update.balances
		}
		if update.pairs != nil {
			u.pairs = update.pairs
			selected := ""
			if saved != nil {
				for _, p := range u.pairs {
					if p.AccountID == saved.TradeAccount && p.AssetID == saved.TradeAsset && p.Side == saved.TradeSide && p.QuoteCoin == saved.QuoteCoin {
						selected = p.ID
					}
				}
			}
			if selected == "" && len(u.pairs) > 0 {
				selected = u.pairs[0].ID
			}
			a.fields["wizardPair"] = selected
		}
		if update.destinations != nil {
			u.destinations = update.destinations
			selected := ""
			for _, d := range u.destinations {
				if d.AccountID+"|"+d.AssetID == a.fields["wizardDestination"] {
					selected = a.fields["wizardDestination"]
				}
			}
			if selected == "" && len(u.destinations) > 0 {
				selected = u.destinations[0].AccountID + "|" + u.destinations[0].AssetID
				source, _ := a.account(a.fields["exchangeAccount"])
				for _, d := range u.destinations {
					if d.Index == source.Index {
						selected = d.AccountID + "|" + d.AssetID
						break
					}
				}
				for _, d := range u.destinations {
					if d.AccountID == source.ID {
						selected = d.AccountID + "|" + d.AssetID
						break
					}
				}
			}
			a.fields["wizardDestination"] = selected
			a.wizardSetWithdrawalDefault()
		}
		if update.quote != nil {
			a.quote = append(json.RawMessage(nil), update.quote...)
		}
		if update.result != nil {
			a.result = exchangeResultSummary(update.result)
			a.resultOwner = "exchange"
		}
		if update.walletAccount != "" {
			if update.walletError {
				delete(a.balances, update.walletAccount)
				a.balanceErrors[update.walletAccount] = true
			} else {
				a.balances[update.walletAccount] = update.walletBalances
				delete(a.balanceErrors, update.walletAccount)
			}
		}
	}
	a.workWithFeedback(title, func(g uint64) (func(), error) {
		var err error
		update, err = fn(g)
		var result struct {
			Wizard             *exchange.WizardState
			Operations         json.RawMessage
			WizardDepositState string
		}
		if loadErr := a.callAt(g, "exchangeWizardLoad", struct{}{}, &result); loadErr == nil {
			saved = result.Wizard
			operations = result.Operations
			depositState = result.WizardDepositState
			loaded = true
		} else if err == nil {
			err = loadErr
		}
		return apply, err
	}, func(err error) {
		if err != nil {
			apply()
		} else if update.credit != nil {
			a.notifyWizardDepositConfirmed(a.exchangeWizardData(), update.credit)
		}
	})
}

func (a *App) wizardClearButton(id string, disabled bool) []webui.Node {
	if id == "" {
		return nil
	}
	if a.wizard != nil && a.wizard.state != nil && id == a.wizard.state.DepositOperation {
		// The signer independently verifies that no signed transaction exists.
		// Clearing a draft is always an explicit dialog, never a retry.
		return []webui.Node{a.button("wizardClearDeposit", "Clear unsigned deposit draft", disabled)}
	}
	var status struct{ Operations []struct{ ID, State string } }
	if json.Unmarshal(a.exchange, &status) != nil {
		return nil
	}
	for _, o := range status.Operations {
		if o.ID != id {
			continue
		}
		switch o.State {
		case "PREPARED", "AUTHORIZED":
			return []webui.Node{a.button("wizardCancelUnsigned", "Cancel original unsigned operation", disabled)}
		case "FAILED", "CANCELLED":
			return []webui.Node{a.button("wizardCancelUnsigned", "Clear failed / cancelled operation", disabled)}
		}
	}
	return nil
}

func (a *App) wizardSetWithdrawalDefault() {
	w := a.exchangeWizardData().state
	d, ok := a.wizardChosenDestination()
	if w == nil || !ok || w.WithdrawOperation != "" {
		return
	}
	precision, e := strconv.ParseUint(d.Precision, 10, 8)
	if e != nil || precision > 36 {
		return
	}
	value, e := exchangeInputAtomic(w.OutputAmount)
	if e != nil {
		return
	}
	step := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(36-precision)), nil)
	value.Div(value, step)
	value.Mul(value, step)
	formatted, e := amount.Format(value.String(), 36)
	if e == nil {
		a.fields["wizardWithdrawAmount"] = trimDecimal(formatted)
	}
}

// The authenticated Funding balance can be smaller than the original chain
// amount after receiving fees. It limits the suggested debit, never proves
// deposit credit and never changes a recorded transfer.
func (a *App) wizardSuggestedTransfer(check *wizardDepositCredit) string {
	if check == nil {
		return ""
	}
	limit := wizardTransferLimit(check)
	_, asset, ok := a.wizardSource()
	if !ok || asset.Decimals > 36 {
		return limit
	}
	available := wizardAvailable(check.Balances, "FUND", asset.Symbol)
	if available == "" || !wizardAtMost(available, limit) {
		return limit
	}
	value, err := exchangeInputAtomic(available)
	if err != nil {
		return limit
	}
	step := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(36-asset.Decimals)), nil)
	value.Quo(value, step).Mul(value, step)
	formatted, err := amount.Format(value.String(), 36)
	if err != nil {
		return limit
	}
	return trimDecimal(formatted)
}

func (a *App) wizardField(key string) bool {
	if !strings.HasPrefix(key, "wizard") {
		return false
	}
	a.exchangeRevision++
	if key == "wizardPair" {
		a.quote = nil
	}
	if key == "wizardDestination" {
		a.wizardSetWithdrawalDefault()
	}
	a.render()
	return true
}

func (a *App) wizardAction(id string) bool {
	if !strings.HasPrefix(id, "wizard") {
		return false
	}
	if id == "wizardAdvanced" {
		a.fields["exchangeMode"] = "existing"
		a.render()
		return true
	}
	if id == "wizardReturn" {
		a.fields["exchangeMode"] = ""
		if a.wizard != nil && a.wizard.state != nil {
			w := a.wizard.state
			a.fields["exchangeAccount"], a.fields["exchangeAsset"], a.fields["exchangeAmount"] = w.SourceAccount, w.SourceAsset, w.DepositAmount
			a.quote = nil
		}
		a.render()
		return true
	}
	if a.selectedExchangeProvider().ID != "bybit" {
		return true
	}
	u := a.exchangeWizardData()
	var state exchange.WizardState
	if u.state != nil {
		state = *u.state
	}
	account, asset, sourceOK := a.wizardSource()
	fields := map[string]string{}
	for k, v := range a.fields {
		fields[k] = v
	}
	var displayedDeposit wizardDepositPreview
	if u.preview != nil {
		displayedDeposit = *u.preview
	}
	pair, pairOK := a.wizardChosenPair()
	destination, destinationOK := a.wizardChosenDestination()
	tradeOutputCoin := pair.OutputCoin
	if !pairOK && state.TradeSide == "Sell" {
		tradeOutputCoin = state.QuoteCoin
	}
	if !pairOK && state.TradeSide == "Buy" {
		if tradeAccount, ok := a.account(state.TradeAccount); ok {
			for _, asset := range a.network(tradeAccount.Network).Assets {
				if asset.ID == state.TradeAsset {
					tradeOutputCoin = asset.Symbol
				}
			}
		}
	}
	if id == "wizardPendingDeposit" {
		if state.SourceAccount != "" {
			a.forcePendingPayments(state.SourceAccount)
			// An explicit review is not the automatic unlock inbox.
			a.pendingInbox.Request = state.SourceAccount
		}
		return true
	}
	if id == "wizardCancelUnsigned" || id == "wizardClearDeposit" {
		op := state.TransferOperation
		if id == "wizardClearDeposit" {
			op = state.DepositOperation
		}
		if state.Step == 3 {
			op = state.SwapOperation
		}
		if state.Step == 4 {
			op = state.WithdrawOperation
		}
		if op != "" {
			a.wizardWork("Cancel original unsigned Bybit operation", func(g uint64) (wizardUpdate, error) {
				return wizardUpdate{}, a.callAt(g, "exchangeWizardCancel", map[string]string{"id": op}, nil)
			})
		}
		return true
	}
	if id == "wizardRestart" {
		if state.Step != 5 {
			return true
		}
		a.wizardWork("Start another exchange", func(g uint64) (wizardUpdate, error) {
			ui := uiprompt.UI{Exchange: a.ui.Prompt}
			if !ui.Confirm("Start another exchange workflow", "The completed workflow's original payments and exchange operations remain in encrypted storage. Start a separate deposit → Funding → Unified → swap → withdrawal workflow?") {
				return wizardUpdate{}, ipc.NewRequestError("REQUEST_CANCELLED")
			}
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				return wizardUpdate{}, err
			}
			w := exchange.WizardState{ID: hex.EncodeToString(random[:]), Step: 1, SourceAccount: state.SourceAccount, SourceAsset: state.SourceAsset, DepositAmount: state.DepositAmount}
			return wizardUpdate{}, a.saveWizardAt(g, &w)
		})
		return true
	}
	if id == "wizardDeposit" && (!sourceOK || !a.wizardPreviewCurrent() || !wizardPositive(fields["exchangeAmount"], asset.Decimals)) {
		a.errorText("Prepare Bybit deposit", "Refresh the selected network's deposit address and enter a valid amount first.")
		return true
	}
	if id == "wizardSwap" && (!pairOK || !a.wizardTradeValidation().SwapValid) {
		a.errorText("Prepare Bybit swap", a.wizardTradeValidation().SwapError)
		return true
	}
	if id == "wizardMove" && (!sourceOK || state.Step != 2 || !wizardCreditReady(u.credit) || !wizardPositive(fields["wizardTransferAmount"], asset.Decimals) || !wizardAtMost(fields["wizardTransferAmount"], state.DepositAmount) || !wizardAtMost(fields["wizardTransferAmount"], wizardTransferLimit(u.credit)) || !exchangeInputAtLeast(wizardAvailable(u.balances, "FUND", asset.Symbol), fields["wizardTransferAmount"])) {
		a.errorText("Prepare Funding transfer", "Wait for Bybit to credit this deposit, then enter a valid amount within its credited amount and available Funding balance.")
		return true
	}
	if id == "wizardWithdraw" && (!destinationOK || state.Step != 4 || !wizardPositive(fields["wizardWithdrawAmount"], 36) || !wizardAtMost(fields["wizardWithdrawAmount"], state.OutputAmount)) {
		a.errorText("Prepare Bybit withdrawal", "Refresh withdrawal networks and choose a valid debit within the actual swap proceeds.")
		return true
	}
	titles := map[string]string{"wizardRefreshDeposit": "Reading Bybit deposit address and wallet balance", "wizardDeposit": "Preparing deposit to Bybit", "wizardCheckDeposit": "Checking the original deposit confirmation and Bybit credit", "wizardRefreshFunding": "Reading Bybit Funding / Unified balances", "wizardMove": "Moving Funding to Unified", "wizardCheckTransfer": "Checking the original Funding transfer", "wizardRefreshPairs": "Finding available Bybit spot pairs", "wizardRefreshMarket": "Reading Bybit market and deposit address", "wizardSwap": "Preparing Bybit spot swap", "wizardCheckSwap": "Checking the original Bybit spot order", "wizardRefreshWithdraw": "Reading owned-wallet withdrawal routes", "wizardWithdraw": "Preparing Bybit withdrawal", "wizardCheckWithdraw": "Checking the original Bybit withdrawal"}
	title, known := titles[id]
	if !known {
		return true
	}
	a.wizardWork(title, func(g uint64) (wizardUpdate, error) {
		update := wizardUpdate{}
		var err error
		loadPairs := func() error {
			return a.callAt(g, "exchangeSwapOptions", map[string]string{"accountId": state.SourceAccount, "assetId": state.SourceAsset}, &update.pairs)
		}
		loadDestinations := func() error {
			return a.callAt(g, "exchangeWithdrawOptions", map[string]string{"coin": state.OutputCoin}, &update.destinations)
		}
		moveToSwap := func(result wizardResult) error {
			if result.State != "CONFIRMED" {
				return nil
			}
			var balances struct{ Balances []wizardBalance }
			if e := a.callAt(g, "exchangeFundingBalances", map[string]string{"accountId": state.SourceAccount, "assetId": state.SourceAsset}, &balances); e != nil {
				return e
			}
			update.balances = balances.Balances
			if !exchangeInputAtLeast(wizardAvailable(update.balances, "UNIFIED", asset.Symbol), state.TransferAmount) {
				return nil
			}
			state.Step = 3
			if e := a.saveWizardAt(g, &state); e != nil {
				return e
			}
			return loadPairs()
		}
		moveToWithdrawal := func(result wizardResult) error {
			if result.State != "CONFIRMED" {
				return nil
			}
			choice := pair
			if !pairOK {
				choice = wizardPair{AccountID: state.TradeAccount, AssetID: state.TradeAsset, Side: state.TradeSide, QuoteCoin: state.QuoteCoin}
				choice.OutputCoin = tradeOutputCoin
			}
			output, e := wizardNetOutput(result, choice)
			if e != nil {
				return e
			}
			state.OutputCoin, state.OutputAmount, state.Step = choice.OutputCoin, output, 4
			if e = a.saveWizardAt(g, &state); e != nil {
				return e
			}
			return loadDestinations()
		}
		switch id {
		case "wizardRefreshDeposit":
			if !sourceOK {
				return update, ipc.NewRequestError("INVALID_ARGUMENT")
			}
			var preview wizardDepositPreview
			if err = a.callAt(g, "exchangeDepositPreview", map[string]string{"provider": "bybit", "accountId": account.ID, "assetId": asset.ID}, &preview); err != nil {
				return update, err
			}
			update.preview = &preview
			update.walletAccount = account.ID
			err = a.callAt(g, "balances", map[string]string{"accountId": account.ID}, &update.walletBalances)
			update.walletError = err != nil
		case "wizardDeposit":
			if state.ID == "" {
				var random [16]byte
				if _, err = rand.Read(random[:]); err != nil {
					return update, err
				}
				state = exchange.WizardState{ID: hex.EncodeToString(random[:]), Step: 1}
			}
			state.SourceAccount, state.SourceAsset, state.DepositAmount = account.ID, asset.ID, fields["exchangeAmount"]
			if err = a.saveWizardAt(g, &state); err != nil {
				return update, err
			}
			err = a.callAt(g, "exchangeDeposit", map[string]string{"provider": "bybit", "accountId": account.ID, "assetId": asset.ID, "amount": fields["exchangeAmount"], "wizardId": state.ID, "expectedAddress": displayedDeposit.Address, "expectedMemo": displayedDeposit.Memo}, &update.result)
		case "wizardCheckDeposit":
			var check wizardDepositCredit
			if err = a.callAt(g, "exchangeDepositCheck", map[string]string{"wizardId": state.ID}, &check); err != nil {
				return update, err
			}
			update.credit = &check
			update.balances = check.Balances
		case "wizardRefreshFunding":
			var check wizardDepositCredit
			err = a.callAt(g, "exchangeDepositCheck", map[string]string{"wizardId": state.ID}, &check)
			if err == nil {
				update.credit = &check
				update.balances = check.Balances
			}
		case "wizardMove":
			err = a.callAt(g, "exchangeFundingTransfer", map[string]string{"wizardId": state.ID, "amount": fields["wizardTransferAmount"]}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				state.TransferOperation, state.TransferAmount = result.ID, fields["wizardTransferAmount"]
				err = moveToSwap(result)
			}
		case "wizardCheckTransfer":
			err = a.callAt(g, "exchangeReconcile", map[string]string{"provider": "bybit", "id": state.TransferOperation}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				err = moveToSwap(result)
			}
		case "wizardRefreshPairs":
			err = loadPairs()
		case "wizardRefreshMarket":
			if !pairOK {
				return update, ipc.NewRequestError("INVALID_ARGUMENT")
			}
			err = a.callAt(g, "exchangeQuote", map[string]string{"provider": "bybit", "accountId": pair.AccountID, "assetId": pair.AssetID, "side": pair.Side, "quoteCoin": pair.QuoteCoin}, &update.quote)
			if err != nil {
				return update, err
			}
			var preview wizardDepositPreview
			if e := a.callAt(g, "exchangeDepositPreview", map[string]string{"provider": "bybit", "accountId": state.SourceAccount, "assetId": state.SourceAsset}, &preview); e != nil {
				err = e
			} else {
				update.preview = &preview
			}
		case "wizardSwap":
			state.TradeAccount, state.TradeAsset, state.TradeSide, state.QuoteCoin, state.TradeAmount = pair.AccountID, pair.AssetID, pair.Side, pair.QuoteCoin, fields["wizardTradeAmount"]
			if err = a.saveWizardAt(g, &state); err != nil {
				return update, err
			}
			err = a.callAt(g, "exchangeSwap", map[string]string{"provider": "bybit", "wizardId": state.ID, "accountId": pair.AccountID, "assetId": pair.AssetID, "side": pair.Side, "quoteCoin": pair.QuoteCoin, "amount": fields["wizardTradeAmount"], "slippage": fields["slippage"]}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				if result.Kind == "swap" {
					state.SwapOperation = result.ID
					err = moveToWithdrawal(result)
				}
			}
		case "wizardCheckSwap":
			err = a.callAt(g, "exchangeReconcile", map[string]string{"provider": "bybit", "id": state.SwapOperation}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				err = moveToWithdrawal(result)
			}
		case "wizardRefreshWithdraw":
			err = loadDestinations()
		case "wizardWithdraw":
			state.WithdrawAccount, state.WithdrawAsset, state.WithdrawAmount = destination.AccountID, destination.AssetID, fields["wizardWithdrawAmount"]
			if err = a.saveWizardAt(g, &state); err != nil {
				return update, err
			}
			err = a.callAt(g, "exchangeWithdraw", map[string]string{"provider": "bybit", "wizardId": state.ID, "accountId": destination.AccountID, "assetId": destination.AssetID, "amount": fields["wizardWithdrawAmount"]}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				if result.Kind == "withdraw" {
					state.WithdrawOperation = result.ID
					if result.State == "EXCHANGE_COMPLETED" {
						state.Step = 5
						err = a.saveWizardAt(g, &state)
					}
				}
			}
		case "wizardCheckWithdraw":
			err = a.callAt(g, "exchangeReconcile", map[string]string{"provider": "bybit", "id": state.WithdrawOperation}, &update.result)
			if err == nil {
				var result wizardResult
				if json.Unmarshal(update.result, &result) != nil {
					return update, ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				if result.State == "EXCHANGE_COMPLETED" {
					state.Step = 5
					err = a.saveWizardAt(g, &state)
				}
			}
		}
		return update, err
	})
	return true
}
