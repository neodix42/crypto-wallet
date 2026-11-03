package nativewallet

import (
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

// Visible steps are independent views, not evidence that a payment completed.
// Economic operations and their original identities stay in their own journals.
type exchangeFreeUI struct {
	preview                                 *wizardDepositPreview
	balances                                []wizardBalance
	pairs                                   []wizardPair
	quote                                   json.RawMessage
	destinations                            []wizardDestination
	operations                              []freeExchangeOperation
	writing                                 bool
	checking                                bool
	checkedAt                               time.Time
	lastCheckedID                           string
	outputCoin, outputAmount, outputAccount string
	transferDefaults                        map[string]string
	transferEdited                          map[string]bool
}

type freeExchangeOperation struct {
	Stage              int
	AccountID, AssetID string
	revision           uint64
	restored           bool
	pair               wizardPair
	wizardResult
}

func (a *App) freeData() *exchangeFreeUI {
	u := a.exchangeWizardData()
	if u.free == nil {
		u.free = &exchangeFreeUI{}
	}
	return u.free
}

func (a *App) freeNavigateStep(id string) bool {
	if !strings.HasPrefix(id, "wizardStep/") {
		return false
	}
	step, err := strconv.Atoi(strings.TrimPrefix(id, "wizardStep/"))
	if err != nil || step < 1 || step > 5 || a.selectedExchangeProvider().ID != "bybit" {
		return true
	}
	if a.busy && a.freeData().writing {
		return true
	}
	a.exchangeWizardData().viewStep = step
	a.exchangeRevision++ // Any in-flight read belongs to the previous view.
	a.ensureFreeSource()
	if step == 4 || step == 5 {
		a.freeSelectOutput()
	}
	a.render()
	return true
}

func (a *App) ensureFreeSource() {
	if _, _, ok := a.freeSource(); ok {
		return
	}
	if w := a.exchangeWizardData().state; w != nil {
		a.fields["freeAccount"], a.fields["freeAsset"] = w.SourceAccount, w.SourceAsset
		if _, _, ok := a.freeSource(); ok {
			return
		}
	}
	a.fields["freeAccount"], a.fields["freeAsset"] = a.fields["exchangeAccount"], a.fields["exchangeAsset"]
	if _, _, ok := a.freeSource(); ok {
		return
	}
	for _, account := range a.state.Accounts {
		if account.Hidden {
			continue
		}
		for _, asset := range a.network(account.Network).Assets {
			if exchangeAssetAllowed(account, asset) {
				a.fields["freeAccount"], a.fields["freeAsset"] = account.ID, asset.ID
				return
			}
		}
	}
}

func (a *App) freeSource() (model.Account, model.Asset, bool) {
	account, ok := a.account(a.fields["freeAccount"])
	if !ok {
		return account, model.Asset{}, false
	}
	for _, asset := range a.network(account.Network).Assets {
		if asset.ID == a.fields["freeAsset"] && exchangeAssetAllowed(account, asset) {
			return account, exchange.LedgerAsset(asset), true
		}
	}
	return account, model.Asset{}, false
}

func (a *App) freeExchangeField(key string) bool {
	if !strings.HasPrefix(key, "free") {
		return false
	}
	u := a.freeData()
	a.exchangeRevision++
	if key == "freeMoveAmount" || key == "freeReturnAmount" {
		if u.transferEdited == nil {
			u.transferEdited = map[string]bool{}
		}
		u.transferEdited[key] = true
	}
	if key == "freeAccount" || key == "freeAsset" {
		u.preview, u.balances, u.pairs, u.quote, u.destinations = nil, nil, nil, nil, nil
		u.outputCoin, u.outputAmount, u.outputAccount = "", "", ""
		u.transferDefaults, u.transferEdited = nil, nil
		if account, ok := a.account(a.fields["freeAccount"]); ok && key == "freeAccount" {
			for _, asset := range a.network(account.Network).Assets {
				if exchangeAssetAllowed(account, asset) {
					a.fields["freeAsset"] = asset.ID
					break
				}
			}
		}
		for _, field := range []string{"freeDepositAmount", "freeMoveAmount", "freeReturnAmount", "freeTradeAmount", "freeWithdrawAmount", "freePair", "freeDestination"} {
			delete(a.fields, field)
		}
	}
	if key == "freePair" {
		u.quote = nil
	}
	if key == "freeDestination" {
		a.freeWithdrawDefault()
	}
	a.render()
	return true
}

func (a *App) freePreviewCurrent() bool {
	account, asset, ok := a.freeSource()
	p := a.freeData().preview
	if !ok || p == nil || p.Provider != "bybit" || p.AccountID != account.ID || p.AssetID != asset.ID || p.Address == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, p.ObservedAt)
	return err == nil && time.Since(at) >= -time.Second && time.Since(at) < 2*time.Minute
}

func (a *App) freePair() (wizardPair, bool) {
	for _, p := range a.freeData().pairs {
		if p.ID == a.fields["freePair"] {
			return p, true
		}
	}
	return wizardPair{}, false
}

func (a *App) freeDestination() (wizardDestination, bool) {
	for _, d := range a.freeData().destinations {
		if d.AccountID+"|"+d.AssetID == a.fields["freeDestination"] {
			return d, true
		}
	}
	return wizardDestination{}, false
}

func (a *App) freeBalances() []wizardBalance {
	if balances := a.freeData().balances; balances != nil {
		return balances
	}
	w := a.exchangeWizardData().state
	account, asset, ok := a.freeSource()
	if ok && w != nil && w.SourceAccount == account.ID && w.SourceAsset == asset.ID && a.wizard.credit != nil {
		return a.wizard.credit.Balances
	}
	return nil
}

func freePending(state string) bool {
	switch state {
	case "PREPARED", "AUTHORIZED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "PENDING", "ACCEPTED", "PARTIALLY_FILLED", "SIGNED_NOT_SUBMITTED", "REORGED":
		return true
	}
	return false
}

func (a *App) freeHasPending(stage int, account, asset string) bool {
	for _, op := range a.freeData().operations {
		if op.Stage == stage && op.AccountID == account && op.AssetID == asset && freePending(op.State) {
			return true
		}
	}
	if stage >= 2 {
		var status struct {
			Operations []struct{ Kind, State string }
		}
		if json.Unmarshal(a.exchange, &status) == nil {
			for _, operation := range status.Operations {
				if operation.Kind != "deposit" && freePending(operation.State) {
					return true
				}
			}
		}
	}
	return false
}

func (a *App) freeWithdrawalAvailable(coin string) string {
	// Step 5 spends Funding only. Unified balances require an explicit step-4
	// transfer; viewing or withdrawing never moves them implicitly.
	value := wizardAvailable(a.freeData().balances, "FUND", coin)
	if _, err := exchangeInputAtomic(value); err != nil {
		return ""
	}
	return value
}

func freeFloor(value string, precision uint8) string {
	v, err := exchangeInputAtomic(value)
	if err != nil || precision > 36 {
		return ""
	}
	step := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(36-precision)), nil)
	v.Quo(v, step).Mul(v, step)
	out, err := amount.Format(v.String(), 36)
	if err != nil {
		return ""
	}
	return trimDecimal(out)
}

func (a *App) freeTradeValidation() exchangeInputValidation {
	p, ok := a.freePair()
	if !ok {
		account, asset, owned := a.freeSource()
		if !owned {
			return exchangeInputValidation{SwapError: "Select a supported wallet asset first."}
		}
		// Syntax/slippage feedback does not require a listing or credentials.
		return a.exchangeAmountValidationFieldsAt(map[string]string{"exchangeAccount": account.ID, "exchangeAsset": asset.ID, "side": "Sell", "exchangeAmount": a.fields["freeTradeAmount"], "slippage": a.fields["slippage"]}, nil, time.Now())
	}
	side := p.Side
	if p.QuoteCoin == "USDC" {
		side += "USDC"
	}
	fields := map[string]string{"exchangeAccount": p.AccountID, "exchangeAsset": p.AssetID, "side": side, "exchangeAmount": a.fields["freeTradeAmount"], "slippage": a.fields["slippage"]}
	v := a.exchangeAmountValidationFieldsAt(fields, a.freeData().quote, time.Now())
	var quote struct {
		Coin, QuoteCoin, Side string
		Balances              []wizardBalance
	}
	if json.Unmarshal(a.freeData().quote, &quote) != nil {
		v.SwapValid = false
		return v
	}
	coin := quote.Coin
	if quote.Side == "Buy" {
		coin = quote.QuoteCoin
	}
	if !exchangeInputAtLeast(wizardAvailable(quote.Balances, "UNIFIED", coin), a.fields["freeTradeAmount"]) {
		v.SwapValid, v.SwapError = false, "Amount exceeds available Unified funds. Move funds in step 2, then refresh the market."
		v.InputError = v.SwapError
	}
	return v
}

func (a *App) freeExchangePage(step int, disabled bool) []webui.Node {
	a.ensureFreeSource()
	u := a.freeData()
	account, asset, sourceOK := a.freeSource()
	var accounts, assets []webui.Option
	for _, owned := range a.state.Accounts {
		if !owned.Hidden || owned.ID == account.ID {
			accounts = append(accounts, webui.Option{ID: owned.ID, Text: a.network(owned.Network).Name + " · " + a.groupTitle(owned.Index)})
		}
	}
	for _, candidate := range a.network(account.Network).Assets {
		if exchangeAssetAllowed(account, candidate) {
			assets = append(assets, webui.Option{ID: candidate.ID, Text: candidate.Symbol})
		}
	}
	assetLabel := "Asset"
	if step == 1 {
		assetLabel = "Asset to deposit"
	}
	nodes := []webui.Node{row(a.combo("freeAccount", "Network / source account", accounts), a.combo("freeAsset", assetLabel, assets))}
	blocked := disabled || !sourceOK || a.busy
	switch step {
	case 1:
		nodes = append(nodes, heading("1. Deposit to Funding"))
		if sourceOK {
			value, hint := a.balance(account, asset)
			balance := label("Wallet available: " + value + " " + asset.Symbol)
			balance.Tooltip = hint
			nodes = append(nodes, balance)
		}
		nodes = append(nodes, a.button("freeRefreshDeposit", "Refresh market / deposit address", blocked))
		if p := u.preview; p != nil {
			nodes = append(nodes, row(node("address", "", "Bybit deposit address", p.Address), node("button", "copyReceive/exchange-free-deposit", "Copy address", p.Address)), label("Deposit network: "+p.Chain+" · "+p.Coin))
			if p.Memo == "" {
				nodes = append(nodes, label("Bybit explicitly reports no deposit memo for this address."))
			} else {
				nodes = append(nodes, node("address", "", "Required deposit memo", p.Memo))
			}
		}
		field := a.entry("freeDepositAmount", "Deposit amount ("+asset.Symbol+")")
		field.InputMode = "decimal"
		valid := sourceOK && wizardPositive(field.Value, asset.Decimals)
		if field.Value != "" && !valid {
			field.Error = "Enter a positive " + asset.Symbol + " amount with at most " + strconv.Itoa(int(asset.Decimals)) + " decimal places."
		}
		nodes = append(nodes, field, a.button("freeDeposit", "Deposit", blocked || !valid || !a.freePreviewCurrent() || a.freeHasPending(1, account.ID, asset.ID)))
	case 2, 4:
		from, fieldID, actionID := "FUND", "freeMoveAmount", "freeMove"
		title, buttonText, sourceName := "2. Move Funding to Unified", "Move to Unified", "Funding"
		if step == 4 {
			from, fieldID, actionID = "UNIFIED", "freeReturnAmount", "freeReturn"
			title, buttonText, sourceName = "4. Move Unified to Funding", "Move to Funding", "Unified"
		}
		nodes = append(nodes, heading(title), label("Move the selected asset's currently available "+sourceName+" funds between your Bybit accounts. This is a separately approved transfer. This action does not certify the credit of a particular wallet deposit."))
		balances := a.freeBalances()
		a.freeTransferDraft(fieldID, balances, from, asset)
		for _, kind := range []string{"FUND", "UNIFIED"} {
			name := "Funding"
			if kind == "UNIFIED" {
				name = "Unified"
			}
			value := wizardAvailable(balances, kind, asset.Symbol)
			if value == "" {
				nodes = append(nodes, label(name+" available: not yet verified"))
			} else {
				nodes = append(nodes, label(name+" available: "+value+" "+asset.Symbol))
			}
		}
		field := a.entry(fieldID, "Amount to move ("+asset.Symbol+")")
		field.InputMode = "decimal"
		valid := sourceOK && wizardPositive(field.Value, asset.Decimals) && exchangeInputAtLeast(wizardAvailable(balances, from, asset.Symbol), field.Value)
		if field.Value != "" && !valid {
			field.Error = "Enter a positive amount within the verified " + sourceName + " balance and asset precision."
		}
		pending := a.freeHasPending(step, account.ID, asset.ID)
		reason := ""
		if pending {
			reason = "Checking the previous exchange operation automatically. This action becomes available when its outcome is verified."
		} else if !valid && sourceOK {
			reason = "Enter an amount within the available " + sourceName + " balance."
			if wizardAvailable(balances, from, asset.Symbol) == "" {
				reason = "Refresh balances to verify the available " + sourceName + " funds."
			}
		}
		button := a.button(actionID, buttonText, blocked || !valid || pending)
		button.Tooltip = reason
		nodes = append(nodes, field, a.button("freeRefreshFunding", "Refresh Funding / Unified balances", blocked), button)
		if reason != "" && !blocked {
			nodes = append(nodes, label(reason))
		}
		if step == 2 && a.wizard.credit != nil && a.wizard.state != nil && a.wizard.state.SourceAccount == account.ID && a.wizard.state.SourceAsset == asset.ID {
			nodes = append(nodes, heading("Original deposit status"), label(a.wizard.credit.Message))
		}
	case 3:
		nodes = append(nodes, heading("3. Swap on Spot"), label("Spend currently available Unified funds. Visiting this step does not deposit or move Funding funds."), a.button("freeRefreshPairs", "Refresh available pairs", blocked))
		var choices []webui.Option
		for _, p := range u.pairs {
			choices = append(choices, webui.Option{ID: p.ID, Text: p.Symbol + " · " + p.Side + " · receive " + p.OutputCoin})
		}
		nodes = append(nodes, a.combo("freePair", "Spot direction", choices), a.button("freeRefreshMarket", "Refresh market", blocked || len(choices) == 0))
		var q struct {
			Coin, QuoteCoin, Side, Symbol, LastPrice string
			Balances                                 []wizardBalance
		}
		if json.Unmarshal(u.quote, &q) == nil {
			coin := q.Coin
			if q.Side == "Buy" {
				coin = q.QuoteCoin
			}
			nodes = append(nodes, label("Pair: "+q.Symbol+" · Last price: "+q.LastPrice+" "+q.QuoteCoin), label("Unified available: "+wizardAvailable(q.Balances, "UNIFIED", coin)+" "+coin))
		}
		validation := a.freeTradeValidation()
		field := a.entry("freeTradeAmount", "Amount to spend")
		field.InputMode, field.Error = "decimal", validation.InputError
		if field.Error == "" && field.Value != "" && !validation.SwapValid {
			field.Error = validation.SwapError
		}
		pair, chosen := a.freePair()
		slippage := a.entry("slippage", "Maximum slippage (%)")
		slippage.InputMode, slippage.Error = "decimal", validation.SlippageError
		pending := a.freeHasPending(3, pair.AccountID, pair.AssetID)
		button := a.button("freeSwap", "Swap", blocked || !chosen || !validation.SwapValid || pending)
		button.Tooltip = validation.SwapError
		nodes = append(nodes, field, slippage, button)
		if pending {
			nodes = append(nodes, label("Checking the previous exchange operation automatically. Swap becomes available when its outcome is verified."))
		}
	case 5:
		nodes = append(nodes, heading("5. Withdraw to wallet"), label("Withdraw from Funding only. Move Unified funds in step 4 first. Choose a supported owned-wallet destination; the total debit includes the withdrawal fee."), a.button("freeRefreshWithdraw", "Refresh withdrawal networks and balances", blocked))
		var destinations []webui.Option
		for _, d := range u.destinations {
			destinations = append(destinations, webui.Option{ID: d.AccountID + "|" + d.AssetID, Text: d.NetworkName + " · " + d.Coin + " · " + a.groupTitle(d.Index)})
		}
		nodes = append(nodes, a.combo("freeDestination", "Withdrawal network / wallet", destinations))
		d, chosen := a.freeDestination()
		if chosen {
			nodes = append(nodes, row(node("address", "", "Owned receiving address", d.Address), node("button", "copyReceive/exchange-free-withdrawal", "Copy address", d.Address)), label("Withdrawal fee: "+d.Fee+" "+d.Coin+" · Percentage fee: "+d.PercentageFee+" · Minimum: "+d.Minimum+" · Decimals: "+d.Precision))
		}
		field := a.entry("freeWithdrawAmount", "Total withdrawal debit ("+asset.Symbol+")")
		field.InputMode = "decimal"
		precision, err := strconv.ParseUint(d.Precision, 10, 8)
		available := a.freeWithdrawalAvailable(d.Coin)
		if available != "" {
			nodes = append(nodes, label("Funding available: "+available+" "+d.Coin))
		}
		valid := chosen && err == nil && precision <= 36 && wizardPositive(field.Value, uint8(precision)) && exchangeInputAtLeast(field.Value, d.Minimum) && exchangeInputAtLeast(available, field.Value)
		if field.Value != "" && !valid {
			field.Error = "Use the selected network's minimum and precision, within the verified available balance."
		}
		nodes = append(nodes, field, a.button("freeWithdraw", "Withdraw", blocked || !valid || a.freeHasPending(5, d.AccountID, d.AssetID)))
	}
	for _, op := range u.operations {
		if op.Stage == step {
			nodes = append(nodes, label(exchangeResultSummary(mustFreeResult(op.wizardResult))))
		}
	}
	nodes = append(nodes, a.exchangePendingOperationNodes()...)
	nodes = append(nodes, a.button("exchangeStatus", "Refresh Bybit status", a.busy))
	return nodes
}

func mustFreeResult(result wizardResult) json.RawMessage { raw, _ := json.Marshal(result); return raw }

func (a *App) freeRememberOutput(operation freeExchangeOperation) {
	if operation.Stage != 3 || operation.State != "CONFIRMED" || operation.pair.ID == "" || a.fields["freePair"] != operation.pair.ID {
		return
	}
	value, err := wizardNetOutput(operation.wizardResult, operation.pair)
	if err != nil {
		return
	}
	u := a.freeData()
	u.outputCoin, u.outputAmount, u.outputAccount = operation.pair.OutputCoin, value, operation.pair.AccountID
}

func (a *App) freeTransferDefault(balances []wizardBalance, from string, asset model.Asset) string {
	value := wizardAvailable(balances, from, asset.Symbol)
	if u := a.freeData(); from == "UNIFIED" && u.outputCoin == asset.Symbol && u.outputAmount != "" && wizardAtMost(u.outputAmount, value) {
		value = u.outputAmount
	}
	return freeFloor(value, asset.Decimals)
}

// Untouched suggestions follow newly observed balances, including a former
// zero balance. An entered or intentionally cleared draft remains the user's.
func (a *App) freeTransferDraft(field string, balances []wizardBalance, from string, asset model.Asset) {
	u := a.freeData()
	if u.transferEdited[field] {
		return
	}
	previous, managed := u.transferDefaults[field]
	if a.fields[field] != "" && (!managed || a.fields[field] != previous) {
		return
	}
	if u.transferDefaults == nil {
		u.transferDefaults = map[string]string{}
	}
	value := a.freeTransferDefault(balances, from, asset)
	a.fields[field], u.transferDefaults[field] = value, value
}

func (a *App) resetFreeTransferDrafts() {
	u := a.freeData()
	u.transferDefaults, u.transferEdited = nil, nil
	a.fields["freeMoveAmount"], a.fields["freeReturnAmount"], a.fields["freeWithdrawAmount"] = "", "", ""
}

// A fresh custody observation may update the corresponding spending balance
// in the displayed quote. It never renews the instrument/price timestamp.
func (a *App) setFreeBalances(balances []wizardBalance) {
	u := a.freeData()
	u.balances = balances
	var quote map[string]json.RawMessage
	if json.Unmarshal(u.quote, &quote) != nil || quote == nil {
		return
	}
	var prior []wizardBalance
	for key, value := range quote {
		if strings.EqualFold(key, "balances") {
			if json.Unmarshal(value, &prior) != nil {
				return
			}
		}
	}
	if len(balances) == 0 {
		prior = nil // A failed post-transfer refresh cannot preserve spendability.
	}
	coins := map[string]bool{}
	for _, fresh := range balances {
		coins[fresh.Coin] = true
	}
	retained := prior[:0]
	for _, old := range prior {
		if !coins[old.Coin] {
			retained = append(retained, old)
		}
	}
	prior = append(retained, balances...)
	for key := range quote {
		if strings.EqualFold(key, "balances") {
			delete(quote, key)
		}
	}
	quote["balances"], _ = json.Marshal(prior)
	u.quote, _ = json.Marshal(quote)
}

func (a *App) freeWithdrawDefault() {
	d, ok := a.freeDestination()
	if !ok {
		return
	}
	precision, err := strconv.ParseUint(d.Precision, 10, 8)
	if err != nil || precision > 36 {
		return
	}
	value := a.freeWithdrawalAvailable(d.Coin)
	if value == "" {
		return
	}
	if u := a.freeData(); u.outputCoin == d.Coin && u.outputAmount != "" && wizardAtMost(u.outputAmount, value) {
		value = u.outputAmount
	}
	if a.fields["freeWithdrawAmount"] == "" {
		a.fields["freeWithdrawAmount"] = freeFloor(value, uint8(precision))
	}
}

func (a *App) freeSelectOutput() {
	u := a.freeData()
	if u.outputCoin == "" {
		return
	}
	source, ok := a.account(u.outputAccount)
	if !ok {
		return
	}
	var chosenAccount, chosenAsset string
	for _, owned := range a.state.Accounts {
		if owned.Index != source.Index {
			continue
		}
		for _, asset := range a.network(owned.Network).Assets {
			if exchange.LedgerAsset(asset).Symbol == u.outputCoin && exchangeAssetAllowed(owned, asset) {
				if chosenAccount == "" || owned.ID == source.ID {
					chosenAccount, chosenAsset = owned.ID, asset.ID
				}
			}
		}
	}
	if chosenAccount != "" && (a.fields["freeAccount"] != chosenAccount || a.fields["freeAsset"] != chosenAsset) {
		a.fields["freeAccount"], a.fields["freeAsset"] = chosenAccount, chosenAsset
		u.preview, u.balances, u.pairs, u.quote, u.destinations = nil, nil, nil, nil, nil
		delete(a.fields, "freeDestination")
		delete(a.fields, "freeWithdrawAmount")
		delete(a.fields, "freeReturnAmount")
		u.transferDefaults, u.transferEdited = nil, nil
	}
}

type freeExchangeUpdate struct {
	preview      *wizardDepositPreview
	balances     []wizardBalance
	pairs        []wizardPair
	quote        json.RawMessage
	destinations []wizardDestination
	result       *freeExchangeOperation
}

func (a *App) freeWork(title string, writing bool, fn func(uint64) (freeExchangeUpdate, error)) {
	if a.busy || a.closed {
		return
	}
	u := a.freeData()
	revision := a.exchangeRevision
	u.writing = writing
	var update freeExchangeUpdate
	var saved struct {
		Wizard             *exchange.WizardState
		Operations         json.RawMessage
		WizardDepositState string
	}
	loaded := false
	apply := func() {
		u.writing = false
		if a.exchangeWizardData().free != u {
			return
		}
		if loaded && (writing || a.exchangeRevision == revision) {
			a.adoptWizardState(saved.Wizard)
			a.wizard.depositState = saved.WizardDepositState
			var status map[string]json.RawMessage
			if json.Unmarshal(a.exchange, &status) == nil && len(saved.Operations) > 0 {
				status["operations"] = saved.Operations
				a.exchange, _ = json.Marshal(status)
				var operations []struct{ ID, State string }
				if json.Unmarshal(saved.Operations, &operations) == nil {
					for _, operation := range operations {
						for i := range u.operations {
							if u.operations[i].ID == operation.ID && u.operations[i].State != operation.State {
								u.operations[i].State = operation.State
								u.operations[i].revision++
							}
						}
					}
				}
			}
		}
		// A completed or uncertain write must retain its original ID even if
		// the user edited a draft while its immutable request was in flight.
		if update.result != nil {
			found := false
			for i := range u.operations {
				if u.operations[i].ID == update.result.ID {
					update.result.revision = u.operations[i].revision + 1
					u.operations[i] = *update.result
					found = true
				}
			}
			if !found {
				u.operations = append(u.operations, *update.result)
			}
			a.result = exchangeResultSummary(mustFreeResult(update.result.wizardResult))
			a.resultOwner = "exchange"
		}
		if a.exchangeRevision != revision {
			return
		}
		if update.result != nil && update.result.Stage == 3 && update.result.State == "CONFIRMED" {
			u.quote = nil
			a.freeRememberOutput(*update.result)
		}
		if update.result != nil && (update.result.Stage == 2 || update.result.Stage == 4) && update.result.State == "CONFIRMED" {
			a.resetFreeTransferDrafts()
		}
		if update.result != nil && update.result.Stage == 5 && update.result.State == "EXCHANGE_COMPLETED" {
			u.balances = []wizardBalance{}
			a.fields["freeWithdrawAmount"] = ""
		}
		if update.preview != nil {
			u.preview = update.preview
		}
		if update.balances != nil {
			a.setFreeBalances(update.balances)
			_, asset, ok := a.freeSource()
			if ok {
				a.freeTransferDraft("freeMoveAmount", u.balances, "FUND", asset)
				a.freeTransferDraft("freeReturnAmount", u.balances, "UNIFIED", asset)
			}
		}
		if update.pairs != nil {
			u.pairs = update.pairs
			if len(u.pairs) > 0 {
				selected := false
				for _, p := range u.pairs {
					selected = selected || p.ID == a.fields["freePair"]
				}
				if !selected {
					a.fields["freePair"] = u.pairs[0].ID
				}
			}
		}
		if update.quote != nil {
			u.quote = update.quote
		}
		if update.destinations != nil {
			u.destinations = update.destinations
			if len(u.destinations) > 0 {
				selected := false
				for _, d := range u.destinations {
					selected = selected || d.AccountID+"|"+d.AssetID == a.fields["freeDestination"]
				}
				if !selected {
					d := u.destinations[0]
					a.fields["freeDestination"] = d.AccountID + "|" + d.AssetID
				}
				a.freeWithdrawDefault()
			}
		}
	}
	a.workWithFeedback(title, func(g uint64) (func(), error) {
		var err error
		update, err = fn(g)
		if loadErr := a.callAt(g, "exchangeWizardLoad", struct{}{}, &saved); loadErr == nil {
			loaded = true
		} else if err == nil {
			err = loadErr
		}
		return apply, err
	}, func(err error) {
		if err != nil {
			apply()
		}
	})
}

func (a *App) freeExchangeAction(id string) bool {
	if !strings.HasPrefix(id, "free") {
		return false
	}
	if a.selectedExchangeProvider().ID != "bybit" || a.state.Locked || !a.state.RecoveryConfirmed {
		return true
	}
	if strings.HasPrefix(id, "freeCancel/") {
		operationID := strings.TrimPrefix(id, "freeCancel/")
		var status struct{ Operations []struct{ ID, State string } }
		if json.Unmarshal(a.exchange, &status) != nil {
			return true
		}
		for _, operation := range status.Operations {
			if operation.ID == operationID && (operation.State == "PREPARED" || operation.State == "AUTHORIZED") {
				a.freeWork("Cancel original unsigned exchange operation", true, func(g uint64) (freeExchangeUpdate, error) {
					return freeExchangeUpdate{}, a.callAt(g, "exchangeCancel", map[string]string{"provider": "bybit", "id": operationID}, nil)
				})
				return true
			}
		}
		return true
	}
	u := a.freeData()
	account, asset, ok := a.freeSource()
	if !ok {
		a.errorText("Exchange action", "Select a supported owned wallet and asset.")
		return true
	}
	fields := map[string]string{}
	for key, value := range a.fields {
		fields[key] = value
	}
	pair, pairOK := a.freePair()
	destination, destinationOK := a.freeDestination()
	var preview wizardDepositPreview
	if u.preview != nil {
		preview = *u.preview
	}
	stage := map[string]int{"freeDeposit": 1, "freeMove": 2, "freeSwap": 3, "freeReturn": 4, "freeWithdraw": 5}[id]
	opAccount, opAsset := account.ID, asset.ID
	if stage == 3 {
		opAccount, opAsset = pair.AccountID, pair.AssetID
	}
	if stage == 5 {
		opAccount, opAsset = destination.AccountID, destination.AssetID
	}
	if stage > 0 && a.freeHasPending(stage, opAccount, opAsset) {
		a.notice("The previous exchange operation is being checked automatically. No new operation was sent.")
		return true
	}
	valid := true
	switch id {
	case "freeDeposit":
		valid = a.freePreviewCurrent() && wizardPositive(fields["freeDepositAmount"], asset.Decimals)
	case "freeMove":
		valid = wizardPositive(fields["freeMoveAmount"], asset.Decimals) && exchangeInputAtLeast(wizardAvailable(a.freeBalances(), "FUND", asset.Symbol), fields["freeMoveAmount"])
	case "freeReturn":
		valid = wizardPositive(fields["freeReturnAmount"], asset.Decimals) && exchangeInputAtLeast(wizardAvailable(a.freeBalances(), "UNIFIED", asset.Symbol), fields["freeReturnAmount"])
	case "freeSwap":
		valid = pairOK && a.freeTradeValidation().SwapValid
	case "freeRefreshMarket":
		valid = pairOK
	case "freeWithdraw":
		p, e := strconv.ParseUint(destination.Precision, 10, 8)
		valid = destinationOK && e == nil && p <= 36 && wizardPositive(fields["freeWithdrawAmount"], uint8(p)) && exchangeInputAtLeast(fields["freeWithdrawAmount"], destination.Minimum) && exchangeInputAtLeast(a.freeWithdrawalAvailable(destination.Coin), fields["freeWithdrawAmount"])
	}
	if !valid {
		a.errorText("Exchange action", "Refresh the selected balances or market and enter a valid affordable amount.")
		return true
	}
	titles := map[string]string{"freeRefreshDeposit": "Reading Bybit deposit address", "freeDeposit": "Preparing a new deposit to Bybit", "freeRefreshFunding": "Reading Funding / Unified balances", "freeMove": "Moving available Funding to Unified", "freeReturn": "Moving available Unified to Funding", "freeRefreshPairs": "Reading available spot pairs", "freeRefreshMarket": "Reading current spot market", "freeSwap": "Preparing swap from Unified funds", "freeRefreshWithdraw": "Reading owned withdrawal routes", "freeWithdraw": "Preparing withdrawal to owned wallet"}
	title, known := titles[id]
	var checking *freeExchangeOperation
	if strings.HasPrefix(id, "freeCheck/") {
		for _, op := range u.operations {
			if op.ID == strings.TrimPrefix(id, "freeCheck/") {
				copy := op
				checking = &copy
			}
		}
		if checking == nil {
			return true
		}
		title, known = "Checking original operation", true
	}
	if !known {
		return true
	}
	a.freeWork(title, stage > 0, func(g uint64) (freeExchangeUpdate, error) {
		var out freeExchangeUpdate
		var err error
		var raw json.RawMessage
		switch id {
		case "freeRefreshDeposit":
			var p wizardDepositPreview
			err = a.callAt(g, "exchangeDepositPreview", map[string]string{"accountId": account.ID, "assetId": asset.ID}, &p)
			if err == nil {
				out.preview = &p
			}
		case "freeDeposit":
			err = a.callAt(g, "exchangeDeposit", map[string]string{"accountId": account.ID, "assetId": asset.ID, "amount": fields["freeDepositAmount"], "expectedAddress": preview.Address, "expectedMemo": preview.Memo}, &raw)
		case "freeRefreshFunding":
			var b struct{ Balances []wizardBalance }
			err = a.callAt(g, "exchangeFundingBalances", map[string]string{"accountId": account.ID, "assetId": asset.ID}, &b)
			out.balances = b.Balances
			if err != nil {
				out.balances = []wizardBalance{}
			}
		case "freeMove":
			err = a.callAt(g, "exchangeFundingMove", map[string]string{"accountId": account.ID, "assetId": asset.ID, "amount": fields["freeMoveAmount"]}, &raw)
		case "freeReturn":
			err = a.callAt(g, "exchangeUnifiedMove", map[string]string{"accountId": account.ID, "assetId": asset.ID, "amount": fields["freeReturnAmount"]}, &raw)
		case "freeRefreshPairs":
			err = a.callAt(g, "exchangeSwapOptions", map[string]any{"accountId": account.ID, "assetId": asset.ID, "spotOnly": true}, &out.pairs)
		case "freeRefreshMarket":
			err = a.callAt(g, "exchangeQuote", map[string]any{"provider": "bybit", "accountId": pair.AccountID, "assetId": pair.AssetID, "side": pair.Side, "quoteCoin": pair.QuoteCoin, "unifiedOnly": true}, &out.quote)
		case "freeSwap":
			err = a.callAt(g, "exchangeSwap", map[string]any{"provider": "bybit", "accountId": pair.AccountID, "assetId": pair.AssetID, "side": pair.Side, "quoteCoin": pair.QuoteCoin, "amount": fields["freeTradeAmount"], "slippage": fields["slippage"], "unifiedOnly": true}, &raw)
		case "freeRefreshWithdraw":
			err = a.callAt(g, "exchangeWithdrawOptions", map[string]any{"coin": asset.Symbol, "existingFunds": true, "sourceAccountId": account.ID}, &out.destinations)
			if err == nil {
				var b struct{ Balances []wizardBalance }
				err = a.callAt(g, "exchangeFundingBalances", map[string]string{"accountId": account.ID, "assetId": asset.ID}, &b)
				out.balances = b.Balances
			}
		case "freeWithdraw":
			err = a.callAt(g, "exchangeWithdraw", map[string]any{"provider": "bybit", "accountId": destination.AccountID, "assetId": destination.AssetID, "amount": fields["freeWithdrawAmount"], "existingFunds": true, "fundingOnly": true}, &raw)
		default:
			if checking != nil {
				if checking.Stage == 1 {
					var receipt model.Receipt
					err = a.callAt(g, "reconcile", map[string]string{"id": checking.ID}, &receipt)
					if err == nil {
						result := *checking
						result.State, result.TransactionID, result.Message = receipt.State, receipt.Identity, receipt.Detail
						out.result = &result
					}
				} else {
					err = a.callAt(g, "exchangeReconcile", map[string]string{"provider": "bybit", "id": checking.ID}, &raw)
					stage, opAccount, opAsset = checking.Stage, checking.AccountID, checking.AssetID
				}
			}
		}
		if len(raw) > 0 {
			var result wizardResult
			if e := json.Unmarshal(raw, &result); e != nil || result.ID == "" {
				if err == nil {
					err = ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
			} else {
				out.result = &freeExchangeOperation{Stage: stage, AccountID: opAccount, AssetID: opAsset, wizardResult: result}
				if checking != nil {
					out.result.restored = checking.restored
				}
				if stage == 3 {
					out.result.pair = pair
					if checking != nil {
						out.result.pair = checking.pair
					}
				}
				if (stage == 2 || stage == 4) && result.State == "CONFIRMED" {
					var balances struct{ Balances []wizardBalance }
					if readErr := a.callAt(g, "exchangeFundingBalances", map[string]string{"accountId": account.ID, "assetId": asset.ID}, &balances); readErr == nil {
						out.balances = balances.Balances
					} else {
						out.balances = []wizardBalance{}
						out.result.Message += " Account balances could not be refreshed; use Refresh Funding / Unified balances."
					}
				}
			}
		}
		return out, err
	})
	return true
}

// Poll original identities only. No preparation, signing, cancellation,
// rebroadcast or exchange debit can originate from this read-only loop.
func (a *App) nextFreeExchangeCheck() bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.background || a.active != "exchange" || a.wizard == nil || a.wizard.free == nil || a.selectedExchangeProvider().ID != "bybit" {
		return false
	}
	u := a.wizard.free
	if a.resumeFreeExchangeOperations() {
		a.render()
	}
	if u.checking || time.Since(u.checkedAt) < 5*time.Second {
		return false
	}
	original := nextFreeTrackedOperation(u)
	if original == nil {
		return false
	}
	u.checking, u.checkedAt = true, time.Now()
	epoch, generation := a.epoch, a.core.Generation()
	connection, _, connectionValid := a.freeTrackingStatus()
	publicOriginal, publicOriginalPresent := a.freePublicOperationSnapshot(original.ID)
	go func() {
		result := original.wizardResult
		var err error
		var balances []wizardBalance
		balancesRead := false
		if original.Stage == 1 {
			var receipt model.Receipt
			err = a.callAt(generation, "reconcile", map[string]string{"id": original.ID}, &receipt)
			if err == nil {
				result.State, result.TransactionID, result.Message = receipt.State, receipt.Identity, receipt.Detail
			}
		} else {
			err = a.callAt(generation, "exchangeReconcile", map[string]string{"provider": "bybit", "id": original.ID}, &result)
		}
		if err == nil && result.ID == original.ID && (original.Stage == 2 || original.Stage == 4) && result.State == "CONFIRMED" {
			var fresh struct{ Balances []wizardBalance }
			balancesRead = true
			if readErr := a.callAt(generation, "exchangeFundingBalances", map[string]string{"accountId": original.AccountID, "assetId": original.AssetID}, &fresh); readErr == nil {
				balances = fresh.Balances
			} else {
				balances = []wizardBalance{}
			}
		}
		a.ui.Dispatch(func() {
			u.checking = false
			if a.closed || a.epoch != epoch {
				return
			}
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			if a.wizard == nil || a.wizard.free != u || a.selectedExchangeProvider().ID != "bybit" || err != nil || result.ID != original.ID {
				return
			}
			if original.Stage >= 2 && !freeCanPoll(result.State) && !freeRemoteTerminal(result.State) {
				return
			}
			currentConnection, _, currentValid := a.freeTrackingStatus()
			if connectionValid != currentValid || connection != currentConnection {
				return
			}
			publicCurrent, publicCurrentPresent := a.freePublicOperationSnapshot(original.ID)
			if publicOriginalPresent != publicCurrentPresent || publicOriginal != publicCurrent {
				return
			}
			// A newer public status snapshot must win over a delayed read.
			a.resumeFreeExchangeOperations()
			for i := range u.operations {
				if u.operations[i].ID == original.ID {
					if u.operations[i].revision != original.revision || u.operations[i].State != original.State {
						return
					}
					u.operations[i].wizardResult = result
					u.operations[i].revision++
					a.freeRememberOutput(u.operations[i])
					if original.Stage == 3 && result.State == "CONFIRMED" {
						u.quote = nil
					}
				}
			}
			if balancesRead && a.fields["freeAccount"] == original.AccountID && a.fields["freeAsset"] == original.AssetID {
				a.setFreeBalances(balances)
				if !original.restored {
					a.resetFreeTransferDrafts()
				}
				_, asset, ok := a.freeSource()
				if ok {
					a.freeTransferDraft("freeMoveAmount", balances, "FUND", asset)
					a.freeTransferDraft("freeReturnAmount", balances, "UNIFIED", asset)
				}
			}
			if original.Stage >= 2 {
				var status map[string]json.RawMessage
				if json.Unmarshal(a.exchange, &status) == nil {
					var operations []map[string]json.RawMessage
					if json.Unmarshal(status["operations"], &operations) == nil {
						for _, op := range operations {
							var id string
							_ = json.Unmarshal(op["id"], &id)
							if id == original.ID {
								op["state"], _ = json.Marshal(result.State)
							}
						}
						status["operations"], _ = json.Marshal(operations)
						a.exchange, _ = json.Marshal(status)
					}
				}
			}
			if !freePending(result.State) && freePending(original.State) {
				a.notice("Original exchange operation: " + result.State + ".")
				if original.Stage == 1 && result.State == "CONFIRMED" && a.wizard.viewStep == 1 && a.fields["freeAccount"] == original.AccountID && a.fields["freeAsset"] == original.AssetID {
					a.wizard.viewStep = 2
				}
			}
			a.render()
		})
	}()
	return true
}
