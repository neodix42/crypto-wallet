package nativewallet

import (
	"encoding/json"
	"sort"
	"strings"

	"localwallet/internal/exchange"
	"localwallet/internal/webui"
)

func (a *App) selectedExchangeProvider() exchange.Provider {
	provider, ok := exchange.ProviderByID(a.fields["exchangeProvider"])
	if !ok {
		provider, _ = exchange.ProviderByID("bybit")
	}
	return provider
}

func (a *App) exchangeProviderSelector() webui.Node {
	var options []webui.Option
	for _, provider := range exchange.Providers() {
		options = append(options, webui.Option{ID: provider.ID, Text: provider.Name})
	}
	selector := a.combo("exchangeProvider", "Exchange", options)
	selector.Value = a.selectedExchangeProvider().ID
	return selector
}

func (a *App) exchangeEndpoint() string {
	provider := a.selectedExchangeProvider()
	if exchange.ValidateProviderEndpoint(provider.ID, a.fields["region"]) {
		return a.fields["region"]
	}
	return provider.Endpoints[0]
}

func (a *App) selectExchangeProvider(id string) {
	provider, ok := exchange.ProviderByID(id)
	if !ok || id == "" || a.state.Locked || a.busy || !a.state.RecoveryConfirmed || provider.ID == a.selectedExchangeProvider().ID {
		return
	}
	a.exchangeRevision++
	// A lost selection acknowledgement must leave no connected view or quote
	// authorizing a subsequent operation under an uncertain provider context.
	a.exchange = nil
	a.quote = nil
	a.result = ""
	a.resultOwner = ""
	a.exchangeAction("exchangeSelect", map[string]string{"provider": provider.ID})
}

func spotDirection(value string) (side, quote string, ok bool) {
	switch value {
	case "Sell", "Buy":
		return value, "USDT", true
	case "SellUSDC":
		return "Sell", "USDC", true
	case "BuyUSDC":
		return "Buy", "USDC", true
	default:
		return "", "", false
	}
}

func exchangeConnectionSummary(raw json.RawMessage) string {
	var status struct {
		Connected bool   `json:"connected"`
		Provider  string `json:"provider"`
		Endpoint  string `json:"endpoint"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(raw, &status) != nil {
		return "Connection status has not been checked."
	}
	if !status.Connected {
		provider, ok := exchange.ProviderByID(status.Provider)
		if !ok {
			return "Exchange connection is unavailable."
		}
		return provider.Name + " is disconnected. " + status.Message
	}
	return "Connected to " + status.Endpoint + ". " + status.Message
}

func exchangeQuoteNodes(raw json.RawMessage) []webui.Node {
	var quote struct {
		Provider, Symbol, Coin, QuoteCoin, Side, LastPrice, BidPrice, AskPrice, FeeRate, ObservedAt string
		TradingUnavailable                                                                          string
		Route                                                                                       struct {
			Supported                                                      bool
			Reason, Chain, MinWithdraw, WithdrawFee, WithdrawPercentageFee string
		}
		Balances []struct{ AccountType, Coin, Available string }
	}
	if len(raw) == 0 || json.Unmarshal(raw, &quote) != nil {
		return nil
	}
	if quote.QuoteCoin == "" {
		quote.QuoteCoin = "USDT"
	}
	var nodes []webui.Node
	if quote.Symbol == "" {
		nodes = append(nodes, label("Spot trading unavailable. "+quote.TradingUnavailable))
	} else {
		nodes = append(nodes, label("Spot pair: "+quote.Symbol+" · "+quote.Side), label("Last price: "+quote.LastPrice+" "+quote.QuoteCoin), label("Bid: "+quote.BidPrice+" · Ask: "+quote.AskPrice+" · Taker fee rate: "+quote.FeeRate), label("Observed: "+quote.ObservedAt))
	}
	if quote.Provider == "binance" || quote.Provider == "kucoin" {
		nodes = append(nodes, label("Spot orders use a price limit; any unfilled remainder is cancelled."))
	}
	for i, balance := range quote.Balances {
		if i >= 32 {
			break
		}
		accountType := balance.AccountType
		if accountType == "FUND" {
			accountType = "Funding"
		} else if accountType == "UNIFIED" && quote.Provider != "" && quote.Provider != "bybit" {
			accountType = "Spot/trading"
		}
		nodes = append(nodes, label("Exchange "+accountType+" available: "+balance.Available+" "+balance.Coin))
	}
	if quote.Route.Supported {
		nodes = append(nodes, label("Withdrawal network: "+quote.Route.Chain+" · Minimum: "+quote.Route.MinWithdraw+" "+quote.Coin+" · Fee: "+quote.Route.WithdrawFee+" "+quote.Coin+" · Percentage fee: "+quote.Route.WithdrawPercentageFee))
	} else {
		nodes = append(nodes, label("Deposit / withdrawal route unavailable: "+quote.Route.Reason))
	}
	return nodes
}

func exchangeResultSummary(raw json.RawMessage) string {
	var result struct {
		ID, Kind, State, Message, RemoteID, TransactionID, FilledBase, FilledQuote, ReportedAmount, ReportedFee string
		Fees                                                                                                    map[string]string
	}
	if len(raw) > 131072 || json.Unmarshal(raw, &result) != nil {
		return "Exchange result unavailable. Reconcile the original operation before retrying."
	}
	lines := []string{result.Kind + " · " + result.State, result.Message, "Operation: " + result.ID}
	for _, field := range [][2]string{{"Remote ID", result.RemoteID}, {"Transaction", result.TransactionID}, {"Filled base", result.FilledBase}, {"Filled quote", result.FilledQuote}, {"Reported amount", result.ReportedAmount}, {"Reported fee", result.ReportedFee}} {
		if field[1] != "" {
			lines = append(lines, field[0]+": "+field[1])
		}
	}
	coins := make([]string, 0, len(result.Fees))
	for coin := range result.Fees {
		coins = append(coins, coin)
	}
	sort.Strings(coins)
	for _, coin := range coins {
		lines = append(lines, "Fee: "+result.Fees[coin]+" "+coin)
	}
	return strings.Join(lines, "\n")
}
