package kucoin

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) CoinInfo(ctx context.Context, currency string) (bybit.Coin, error) {
	var data struct {
		Currency string `json:"currency"`
		FullName string `json:"fullName"`
		Chains   []struct {
			ChainID           string  `json:"chainId"`
			ChainName         string  `json:"chainName"`
			ContractAddress   *string `json:"contractAddress"`
			WithdrawPrecision *int    `json:"withdrawPrecision"`
			DepositMinSize    *string `json:"depositMinSize"`
			WithdrawMinSize   *string `json:"withdrawMinSize"`
			WithdrawMinFee    *string `json:"withdrawMinFee"`
			WithdrawFeeRate   *string `json:"withdrawFeeRate"`
			MaxWithdraw       *string `json:"maxWithdraw"`
			IsWithdrawEnabled *bool   `json:"isWithdrawEnabled"`
			IsDepositEnabled  *bool   `json:"isDepositEnabled"`
			Confirms          *int    `json:"confirms"`
			PreConfirms       *int    `json:"preConfirms"`
			NeedTag           *bool   `json:"needTag"`
		} `json:"chains"`
	}
	if !coin(currency) {
		return bybit.Coin{}, ErrRequest
	}
	if err := c.get(ctx, "/api/v3/currencies/"+currency, nil, false, &data); err != nil {
		return bybit.Coin{}, err
	}
	if data.Currency != currency || len(data.FullName) == 0 || len(data.FullName) > 120 || len(data.Chains) == 0 || len(data.Chains) > 128 {
		return bybit.Coin{}, ErrResponse
	}
	result := bybit.Coin{Coin: currency, Name: data.FullName}
	seen := map[string]bool{}
	for _, chain := range data.Chains {
		if !identifier(chain.ChainID, 40) || seen[chain.ChainID] || len(chain.ChainName) == 0 || len(chain.ChainName) > 120 || chain.ContractAddress == nil || len(*chain.ContractAddress) > 256 || chain.WithdrawPrecision == nil || *chain.WithdrawPrecision < 0 || *chain.WithdrawPrecision > 36 || chain.IsDepositEnabled == nil || chain.IsWithdrawEnabled == nil || chain.WithdrawMinSize == nil || chain.WithdrawMinFee == nil || chain.WithdrawFeeRate == nil || !decimal(*chain.WithdrawMinSize, false) || !decimal(*chain.WithdrawMinFee, false) || !decimal(*chain.WithdrawFeeRate, false) || chain.NeedTag == nil {
			return bybit.Coin{}, ErrResponse
		}
		seen[chain.ChainID] = true
		depositMin := "0"
		depositEnabled := *chain.IsDepositEnabled
		// Missing published minimum is unknown; never fabricate a supported
		// deposit threshold from a symbol or another network's metadata.
		if chain.DepositMinSize == nil {
			depositEnabled = false
		} else if !decimal(*chain.DepositMinSize, false) {
			return bybit.Coin{}, ErrResponse
		} else {
			depositMin = *chain.DepositMinSize
		}
		maximum := "-1"
		if chain.MaxWithdraw != nil {
			if !decimal(*chain.MaxWithdraw, false) {
				return bybit.Coin{}, ErrResponse
			}
			maximum = *chain.MaxWithdraw
		}
		confirmations, safe := "0", "0"
		if chain.Confirms != nil {
			if *chain.Confirms < 0 {
				return bybit.Coin{}, ErrResponse
			}
			confirmations = strconv.Itoa(*chain.Confirms)
		}
		if chain.PreConfirms != nil {
			if *chain.PreConfirms < 0 {
				return bybit.Coin{}, ErrResponse
			}
			safe = strconv.Itoa(*chain.PreConfirms)
		}
		mapped := bybit.Chain{Chain: chain.ChainID, ChainType: chain.ChainName, ContractAddress: *chain.ContractAddress, DepositMin: depositMin, WithdrawMin: *chain.WithdrawMinSize, WithdrawFee: *chain.WithdrawMinFee, WithdrawPercentageFee: *chain.WithdrawFeeRate, MinAccuracy: strconv.Itoa(*chain.WithdrawPrecision), WithdrawMax: maximum, Confirmation: confirmations, SafeConfirmNumber: safe, ChainDeposit: "0", ChainWithdraw: "0"}
		if depositEnabled {
			mapped.ChainDeposit = "1"
		}
		if *chain.IsWithdrawEnabled {
			mapped.ChainWithdraw = "1"
		}
		result.Chains = append(result.Chains, mapped)
	}
	return result, nil
}

func (c *Client) DepositAddress(ctx context.Context, currency, chain string) (bybit.Deposit, error) {
	if err := c.classic(ctx); err != nil {
		return bybit.Deposit{}, err
	}
	var rows []struct {
		Currency        string  `json:"currency"`
		Address         string  `json:"address"`
		Memo            string  `json:"memo"`
		ChainID         string  `json:"chainId"`
		ChainName       string  `json:"chainName"`
		ContractAddress *string `json:"contractAddress"`
		To              string  `json:"to"`
		ExpirationDate  *int64  `json:"expirationDate"`
	}
	if err := c.get(ctx, "/api/v3/deposit-addresses", url.Values{"currency": {currency}, "chain": {chain}}, true, &rows); err != nil {
		return bybit.Deposit{}, err
	}
	if len(rows) != 1 {
		return bybit.Deposit{}, ErrResponse
	}
	row := rows[0]
	if row.Currency != currency || row.ChainID != chain || row.ChainName == "" || row.ContractAddress == nil || row.Address == "" || len(row.Address) > 256 || len(row.Memo) > 120 || row.To != "MAIN" && row.To != "TRADE" || row.ExpirationDate == nil || *row.ExpirationDate != 0 && *row.ExpirationDate <= c.now().UnixMilli() {
		return bybit.Deposit{}, ErrResponse
	}
	return bybit.Deposit{Chain: row.ChainID, ChainType: row.ChainName, Address: row.Address, Tag: row.Memo, ContractAddress: *row.ContractAddress}, nil
}

func (c *Client) AccountBalance(ctx context.Context, kind, currency string) (bybit.Balance, error) {
	if kind != "FUND" && kind != "UNIFIED" || !coin(currency) {
		return bybit.Balance{}, ErrRequest
	}
	if err := c.classic(ctx); err != nil {
		return bybit.Balance{}, err
	}
	accountType := "main"
	if kind == "UNIFIED" {
		accountType = "trade"
	}
	var rows []struct {
		ID        string `json:"id"`
		Currency  string `json:"currency"`
		Type      string `json:"type"`
		Balance   string `json:"balance"`
		Available string `json:"available"`
		Holds     string `json:"holds"`
	}
	if err := c.get(ctx, "/api/v1/accounts", url.Values{"currency": {currency}, "type": {accountType}}, true, &rows); err != nil {
		return bybit.Balance{}, err
	}
	if len(rows) != 1 {
		return bybit.Balance{}, ErrResponse
	}
	row := rows[0]
	if !identifier(row.ID, 128) || row.Currency != currency || row.Type != accountType || !decimal(row.Balance, false) || !decimal(row.Available, false) || !decimal(row.Holds, false) {
		return bybit.Balance{}, ErrResponse
	}
	result := bybit.Balance{AccountType: kind, AccountID: row.ID}
	result.Balance.Coin = currency
	result.Balance.WalletBalance = row.Balance
	result.Balance.TransferBalance = row.Available
	result.Balance.TransferSafeAmount = row.Available
	return result, nil
}

func (c *Client) SpotInstrument(ctx context.Context, symbol string) (bybit.Instrument, error) {
	native, ok := nativeSymbol(symbol)
	if !ok {
		return bybit.Instrument{}, ErrRequest
	}
	var data struct {
		Symbol               string `json:"symbol"`
		BaseCurrency         string `json:"baseCurrency"`
		QuoteCurrency        string `json:"quoteCurrency"`
		BaseIncrement        string `json:"baseIncrement"`
		QuoteIncrement       string `json:"quoteIncrement"`
		PriceIncrement       string `json:"priceIncrement"`
		BaseMaxSize          string `json:"baseMaxSize"`
		MinFunds             string `json:"minFunds"`
		EnableTrading        *bool  `json:"enableTrading"`
		CallAuctionIsEnabled *bool  `json:"callauctionIsEnabled"`
		AssetClass           string `json:"assetClass"`
	}
	if err := c.get(ctx, "/api/v2/symbols/"+native, nil, false, &data); err != nil {
		return bybit.Instrument{}, err
	}
	if data.Symbol != native || data.BaseCurrency+"-"+data.QuoteCurrency != native || data.EnableTrading == nil || !*data.EnableTrading || data.CallAuctionIsEnabled != nil && *data.CallAuctionIsEnabled || !decimal(data.BaseIncrement, true) || !decimal(data.QuoteIncrement, true) || !decimal(data.PriceIncrement, true) || !decimal(data.BaseMaxSize, true) || !decimal(data.MinFunds, false) {
		return bybit.Instrument{}, ErrResponse
	}
	result := bybit.Instrument{Symbol: symbol, BaseCoin: data.BaseCurrency, QuoteCoin: data.QuoteCurrency, Status: "Trading"}
	result.LotSizeFilter.BasePrecision = data.BaseIncrement
	result.LotSizeFilter.QuotePrecision = data.QuoteIncrement
	result.LotSizeFilter.MinOrderAmt = data.MinFunds
	result.LotSizeFilter.MaxMarketOrderQty = data.BaseMaxSize
	result.PriceFilter.TickSize = data.PriceIncrement
	return result, nil
}

func (c *Client) SpotTicker(ctx context.Context, symbol string) (bybit.Ticker, error) {
	native, ok := nativeSymbol(symbol)
	if !ok {
		return bybit.Ticker{}, ErrRequest
	}
	var data struct {
		Price   string `json:"price"`
		BestBid string `json:"bestBid"`
		BestAsk string `json:"bestAsk"`
		Time    *int64 `json:"time"`
	}
	if err := c.get(ctx, "/api/v1/market/orderbook/level1", url.Values{"symbol": {native}}, false, &data); err != nil {
		return bybit.Ticker{}, err
	}
	if !decimal(data.Price, true) || !decimal(data.BestBid, true) || !decimal(data.BestAsk, true) || data.Time == nil || c.now().UnixMilli()-*data.Time > 30000 || *data.Time-c.now().UnixMilli() > 1000 {
		return bybit.Ticker{}, ErrResponse
	}
	return bybit.Ticker{Symbol: symbol, LastPrice: data.Price, Bid1Price: data.BestBid, Ask1Price: data.BestAsk}, nil
}

func (c *Client) SpotFeeRate(ctx context.Context, symbol string) (bybit.FeeRate, error) {
	native, ok := nativeSymbol(symbol)
	if !ok {
		return bybit.FeeRate{}, ErrRequest
	}
	if err := c.classic(ctx); err != nil {
		return bybit.FeeRate{}, err
	}
	_ = native
	// Classic trade-fees reports rates but cannot establish that independent
	// Funding/Trading/staked KCS debits are disabled. No extra asset authority
	// follows from those rates or from observing a later quote-currency fill.
	return bybit.FeeRate{}, ErrFeeCurrency
}

func (c *Client) WithdrawalAllowed(ctx context.Context) error { return c.classic(ctx) }

func normalizedSide(side string) string {
	if side == "buy" {
		return "Buy"
	}
	if side == "sell" {
		return "Sell"
	}
	return ""
}
func nativeKind(kind string) string {
	if kind == "limit" {
		return "Limit"
	}
	return ""
}
func normalizedSymbol(symbol string) string { return strings.ReplaceAll(symbol, "-", "") }
func marshal(value any) string              { b, _ := json.Marshal(value); return string(b) }
