package walletcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

// This interface permits deterministic hostile-provider tests. Production has
// no endpoint/transport override: only the fixed Bybit client is constructed.
type exchangeAPI interface {
	Close()
	Endpoint() string
	APIKeyInfo(context.Context) (bybit.APIKeyInformation, error)
	CoinInfo(context.Context, string) (bybit.Coin, error)
	DepositAddress(context.Context, string, string) (bybit.Deposit, error)
	AccountBalance(context.Context, string, string) (bybit.Balance, error)
	SpotInstrument(context.Context, string) (bybit.Instrument, error)
	SpotTicker(context.Context, string) (bybit.Ticker, error)
	SpotFeeRate(context.Context, string) (bybit.FeeRate, error)
	CreateOrder(context.Context, bybit.SpotOrder) (bybit.OrderAcknowledgement, error)
	FindOrder(context.Context, string, string) (*bybit.Order, error)
	Executions(context.Context, string, string, string) (bybit.ExecutionPage, error)
	Transfer(context.Context, bybit.TransferRequest) (bybit.TransferAcknowledgement, error)
	TransferStatus(context.Context, string) (*bybit.TransferRecord, error)
	Withdraw(context.Context, bybit.WithdrawalRequest) (bybit.WithdrawalAcknowledgement, error)
	WithdrawalRecords(context.Context, string, string, string) (bybit.WithdrawalPage, error)
	DepositRecords(context.Context, string, string, string) (bybit.DepositPage, error)
}
type exchangeFactory func(string, bybit.Authorizer) (exchangeAPI, error)
type exchangePrivateStatus struct {
	Connected      bool                      `json:"connected"`
	Endpoint       string                    `json:"endpoint"`
	CredentialID   string                    `json:"credentialId"`
	KeySuffix      string                    `json:"keySuffix"`
	Operations     []vault.ExchangeOperation `json:"operations"`
	OperationCount int                       `json:"operationCount"`
}
type exchangePublicOperation struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	AccountID string `json:"accountId"`
	AssetID   string `json:"assetId"`
	Coin      string `json:"coin"`
	Symbol    string `json:"symbol,omitempty"`
	Amount    string `json:"amount"`
	RemoteID  string `json:"remoteId,omitempty"`
	CreatedAt string `json:"createdAt"`
}
type exchangePublicStatus struct {
	Connected   bool                      `json:"connected"`
	Configured  bool                      `json:"configured"`
	Endpoint    string                    `json:"endpoint"`
	KeySuffix   string                    `json:"keySuffix"`
	Environment string                    `json:"environment"`
	Message     string                    `json:"message"`
	Operations  []exchangePublicOperation `json:"operations"`
}
type exchangeRoute struct {
	Supported             bool   `json:"supported"`
	Reason                string `json:"reason"`
	Chain                 string `json:"chain"`
	ChainType             string `json:"chainType"`
	DepositEnabled        bool   `json:"depositEnabled"`
	WithdrawEnabled       bool   `json:"withdrawEnabled"`
	MinDeposit            string `json:"minDeposit"`
	MinWithdraw           string `json:"minWithdraw"`
	WithdrawFee           string `json:"withdrawFee"`
	WithdrawPercentageFee string `json:"withdrawPercentageFee"`
	ContractAddress       string `json:"contractAddress"`
}
type exchangeBalance struct {
	AccountType   string `json:"accountType"`
	Coin          string `json:"coin"`
	Available     string `json:"available"`
	WalletBalance string `json:"walletBalance"`
}
type exchangeQuote struct {
	AccountID      string            `json:"accountId"`
	AssetID        string            `json:"assetId"`
	Coin           string            `json:"coin"`
	Symbol         string            `json:"symbol"`
	Side           string            `json:"side"`
	LastPrice      string            `json:"lastPrice"`
	BidPrice       string            `json:"bidPrice"`
	AskPrice       string            `json:"askPrice"`
	BasePrecision  string            `json:"basePrecision"`
	QuotePrecision string            `json:"quotePrecision"`
	MinOrderAmount string            `json:"minOrderAmount"`
	MaxOrderQty    string            `json:"maxOrderQty"`
	FeeRate        string            `json:"feeRate"`
	Route          exchangeRoute     `json:"route"`
	Balances       []exchangeBalance `json:"balances"`
	ObservedAt     string            `json:"observedAt"`
}
type exchangeResult struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	State          string            `json:"state"`
	Message        string            `json:"message"`
	RemoteID       string            `json:"remoteId,omitempty"`
	TransactionID  string            `json:"transactionId,omitempty"`
	FilledBase     string            `json:"filledBase,omitempty"`
	FilledQuote    string            `json:"filledQuote,omitempty"`
	Fees           map[string]string `json:"fees,omitempty"`
	ReportedAmount string            `json:"reportedAmount,omitempty"`
	ReportedFee    string            `json:"reportedFee,omitempty"`
}
type exchangeIntent struct {
	Kind            string                   `json:"kind"`
	AccountID       string                   `json:"accountId"`
	AssetID         string                   `json:"assetId"`
	Order           *bybit.SpotOrder         `json:"order,omitempty"`
	Transfer        *bybit.TransferRequest   `json:"transfer,omitempty"`
	Withdrawal      *bybit.WithdrawalRequest `json:"withdrawal,omitempty"`
	LastPrice       string                   `json:"lastPrice,omitempty"`
	Fee             string                   `json:"fee,omitempty"`
	FeeCoin         string                   `json:"feeCoin,omitempty"`
	ChainType       string                   `json:"chainType,omitempty"`
	ContractAddress string                   `json:"contractAddress,omitempty"`
}

var errExchange = errors.New("Bybit request unavailable or invalid; inspect original operation before retrying")

func (c *core) exchangeClientAt(epoch uint64) (exchangeAPI, exchangePrivateStatus, error) {
	var status exchangePrivateStatus
	if c.child.Environment != "mainnet" {
		return nil, status, errExchange
	}
	s, err := c.current()
	if err != nil || s.Locked || !s.RecoveryConfirmed {
		return nil, status, errExchange
	}
	if err = c.childCallAt(epoch, "exchangeStatus", struct{}{}, &status); err != nil || !status.Connected || bybit.ValidateEndpoint(status.Endpoint) != nil {
		return nil, status, errExchange
	}
	authorize := func(ctx context.Context, request bybit.AuthRequest) (bybit.Auth, error) {
		var auth bybit.Auth
		if ctx.Err() != nil || request.Endpoint != status.Endpoint {
			return auth, errExchange
		}
		c.mu.Lock()
		valid := c.active && c.epoch == epoch
		c.mu.Unlock()
		if !valid {
			return auth, errExchange
		}
		if err := c.childCallAt(epoch, "exchangeAuth", request, &auth); err != nil {
			return bybit.Auth{}, errExchange
		}
		c.mu.Lock()
		valid = c.active && c.epoch == epoch
		c.mu.Unlock()
		if !valid || ctx.Err() != nil {
			return bybit.Auth{}, errExchange
		}
		return auth, nil
	}
	if c.newExchange != nil {
		client, err := c.newExchange(status.Endpoint, authorize)
		return client, status, err
	}
	client, err := bybit.New(status.Endpoint, authorize)
	return client, status, err
}
func (c *core) exchangeAsset(id, assetID string) (model.Account, model.Asset, model.Adapter, error) {
	account, err := c.account(id)
	if err != nil {
		return model.Account{}, model.Asset{}, nil, errExchange
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return account, model.Asset{}, nil, errExchange
	}
	for _, network := range adapter.Networks() {
		if network.ID != account.Network || network.Testnet {
			continue
		}
		for _, asset := range network.Assets {
			if asset.ID == assetID {
				return account, asset, adapter, nil
			}
		}
	}
	return account, model.Asset{}, nil, errExchange
}
func exchangePermission(info bybit.APIKeyInformation, permission string) bool {
	if info.ReadOnly != 0 {
		return false
	}
	values := info.Permissions.Wallet
	if permission == "SpotTrade" {
		values = info.Permissions.Spot
	}
	for _, value := range values {
		if value == permission {
			return true
		}
	}
	return false
}

func exchangeWithdrawalRegionSupported(endpoint string) bool {
	switch endpoint {
	case "https://api.bybit.tr", "https://api.bybit.kz", "https://api.bybit.eu", "https://api.bybit.id":
		return false
	}
	return true
}
func exchangePublicOperations(operations []vault.ExchangeOperation) []exchangePublicOperation {
	out := make([]exchangePublicOperation, 0, len(operations))
	for _, op := range operations {
		row := exchangePublicOperation{ID: op.ID, Kind: op.Kind, State: op.State, AccountID: op.AccountID, AssetID: op.AssetID, RemoteID: op.ExternalID, CreatedAt: strconv.FormatInt(op.CreatedAt, 10)}
		switch op.Kind {
		case "swap":
			var request bybit.SpotOrder
			if strictjson.Decode([]byte(op.Payload), &request) != nil {
				continue
			}
			row.Symbol, row.Amount = request.Symbol, request.Qty
			row.Coin = strings.TrimSuffix(request.Symbol, "USDT")
		case "transfer":
			var request bybit.TransferRequest
			if strictjson.Decode([]byte(op.Payload), &request) != nil {
				continue
			}
			row.Coin, row.Amount = request.Coin, request.Amount
		case "withdraw":
			var request bybit.WithdrawalRequest
			if strictjson.Decode([]byte(op.Payload), &request) != nil {
				continue
			}
			row.Coin, row.Amount = request.Coin, request.Amount
			if row.State == "CONFIRMED" {
				row.State = "EXCHANGE_COMPLETED"
			}
		default:
			continue
		}
		out = append(out, row)
	}
	return out
}
func (c *core) exchangeStatusAt(epoch uint64) (exchangePublicStatus, error) {
	result := exchangePublicStatus{Environment: c.child.Environment, Operations: []exchangePublicOperation{}, Message: "Bybit is disconnected. Credentials are entered only in the dedicated wallet dialog."}
	if c.child.Environment != "mainnet" {
		result.Message = "Exchange uses a mainnet wallet profile and production Bybit account. This testnet profile cannot move exchange funds."
		return result, nil
	}
	client, status, err := c.exchangeClientAt(epoch)
	result.Configured, result.Endpoint, result.KeySuffix = status.Connected, status.Endpoint, status.KeySuffix
	result.Operations = exchangePublicOperations(status.Operations)
	if err != nil {
		if status.Connected {
			return result, errExchange
		}
		return result, nil
	}
	defer client.Close()
	ctx, cancel := c.networkContextAt(30*time.Second, epoch)
	defer cancel()
	info, err := client.APIKeyInfo(ctx)
	if err != nil {
		result.Message = "Bybit could not verify the configured API key. Check its regional endpoint, permissions, IP restrictions and availability. Pending operations still require reconciliation."
		return result, nil
	}
	result.Connected = true
	result.Message = "Bybit connection verified. Deposits use the wallet payment journal in Settings. Trading uses unleveraged spot only; withdrawal destinations must be whitelisted on Bybit."
	if !exchangePermission(info, "SpotTrade") {
		result.Message += " This key lacks spot trading permission."
	}
	if !info.IsMaster || !exchangePermission(info, "Withdraw") {
		result.Message += " Withdrawals require a master-account key with withdrawal permission."
	}
	return result, nil
}
func (c *core) exchangeAt(message ipc.Message, epoch uint64) (any, error) {
	switch message.Method {
	case "exchangeStatus", "exchangeDisconnect":
		if strictjson.Decode(message.Body, &struct{}{}) != nil {
			return nil, errExchange
		}
		if message.Method == "exchangeDisconnect" {
			if err := c.childCallAt(epoch, "exchangeDisconnect", struct{}{}, nil); err != nil {
				return nil, err
			}
		}
		return c.exchangeStatusAt(epoch)
	case "exchangeConnect":
		var in struct {
			Endpoint strictjson.Text `json:"endpoint"`
		}
		if strictjson.Decode(message.Body, &in) != nil || bybit.ValidateEndpoint(string(in.Endpoint)) != nil || c.child.Environment != "mainnet" {
			return nil, errExchange
		}
		if err := c.childCallAt(epoch, "exchangeConnect", in, nil); err != nil {
			return nil, err
		}
		return c.exchangeStatusAt(epoch)
	case "exchangeReconcile":
		var in struct {
			ID strictjson.Text `json:"id"`
		}
		if strictjson.Decode(message.Body, &in) != nil || in.ID == "" {
			return nil, errExchange
		}
		if len(in.ID) != 32 || !lowerHex(string(in.ID)) {
			return c.paymentAt("reconcile", string(in.ID), epoch)
		}
		client, status, err := c.exchangeClientAt(epoch)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		var operation vault.ExchangeOperation
		if err = c.childCallAt(epoch, "exchangeOperation", in, &operation); err != nil {
			return nil, err
		}
		if operation.CredentialID != status.CredentialID || operation.Endpoint != status.Endpoint {
			return nil, errExchange
		}
		return c.reconcileExchangeAt(epoch, client, operation)
	case "exchangeQuote":
		var in struct {
			AccountID strictjson.Text `json:"accountId"`
			AssetID   strictjson.Text `json:"assetId"`
			Side      strictjson.Text `json:"side"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, errExchange
		}
		client, _, err := c.exchangeClientAt(epoch)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		quote, _, _, err := c.exchangeQuoteAt(epoch, client, string(in.AccountID), string(in.AssetID), string(in.Side))
		return quote, err
	case "exchangeDeposit", "exchangeWithdraw":
		var in struct {
			AccountID strictjson.Text `json:"accountId"`
			AssetID   strictjson.Text `json:"assetId"`
			Amount    strictjson.Text `json:"amount"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, errExchange
		}
		client, _, err := c.exchangeClientAt(epoch)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		if message.Method == "exchangeDeposit" {
			return c.exchangeDepositAt(epoch, client, string(in.AccountID), string(in.AssetID), string(in.Amount))
		}
		return c.exchangeWithdrawAt(epoch, client, string(in.AccountID), string(in.AssetID), string(in.Amount))
	case "exchangeSwap":
		var in struct {
			AccountID strictjson.Text `json:"accountId"`
			AssetID   strictjson.Text `json:"assetId"`
			Side      strictjson.Text `json:"side"`
			Amount    strictjson.Text `json:"amount"`
			Slippage  strictjson.Text `json:"slippage"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, errExchange
		}
		client, _, err := c.exchangeClientAt(epoch)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		return c.exchangeSwapAt(epoch, client, string(in.AccountID), string(in.AssetID), string(in.Side), string(in.Amount), string(in.Slippage))
	}
	return nil, errExchange
}

// All exchange arithmetic uses a common bounded integer scale; no float or
// JSON number represents a monetary quantity across the public boundary.
func exchangeAtomic(value string) (*big.Int, error) {
	text, err := amount.Parse(value, 36)
	if err != nil {
		return nil, errExchange
	}
	return amount.Atomic(text)
}
func exchangeDecimal(value *big.Int) (string, error) {
	if value == nil {
		return "", errExchange
	}
	text, err := amount.Format(value.String(), 36)
	if err != nil {
		return "", errExchange
	}
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	return text, nil
}
func exchangeScale() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(36), nil) }
func exchangeAtLeast(value, minimum string) bool {
	a, e := exchangeAtomic(value)
	b, f := exchangeAtomic(minimum)
	return e == nil && f == nil && a.Cmp(b) >= 0
}
func exchangeSafeBalance(balance bybit.Balance) (string, error) {
	var safe *big.Int
	for _, raw := range []string{balance.Balance.WalletBalance, balance.Balance.TransferBalance, balance.Balance.TransferSafeAmount} {
		n, err := exchangeAtomic(raw)
		if err != nil {
			return "", err
		}
		if safe == nil || n.Cmp(safe) < 0 {
			safe = n
		}
	}
	return exchangeDecimal(safe)
}
func exchangeBalances(ctx context.Context, client exchangeAPI, coins ...string) ([]exchangeBalance, error) {
	out := []exchangeBalance{}
	seen := map[string]bool{}
	for _, coin := range coins {
		if seen[coin] {
			continue
		}
		seen[coin] = true
		for _, kind := range []string{"FUND", "UNIFIED"} {
			b, err := client.AccountBalance(ctx, kind, coin)
			if err != nil {
				return nil, err
			}
			if b.AccountType != kind || b.Balance.Coin != coin {
				return nil, errExchange
			}
			safe, err := exchangeSafeBalance(b)
			if err != nil {
				return nil, err
			}
			out = append(out, exchangeBalance{AccountType: kind, Coin: coin, Available: safe, WalletBalance: b.Balance.WalletBalance})
		}
	}
	return out, nil
}
func (c *core) exchangeQuoteAt(epoch uint64, client exchangeAPI, accountID, assetID, side string) (exchangeQuote, bybit.Chain, bybit.APIKeyInformation, error) {
	quote := exchangeQuote{AccountID: accountID, AssetID: assetID, Side: side, Balances: []exchangeBalance{}, ObservedAt: strconv.FormatInt(time.Now().UnixMilli(), 10)}
	if side != "Buy" && side != "Sell" {
		return quote, bybit.Chain{}, bybit.APIKeyInformation{}, errExchange
	}
	account, asset, _, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return quote, bybit.Chain{}, bybit.APIKeyInformation{}, err
	}
	quote.Coin = asset.Symbol
	ctx, cancel := c.networkContextAt(90*time.Second, epoch)
	defer cancel()
	info, err := client.APIKeyInfo(ctx)
	if err != nil {
		return quote, bybit.Chain{}, info, err
	}
	coin, err := client.CoinInfo(ctx, asset.Symbol)
	if err != nil {
		return quote, bybit.Chain{}, info, err
	}
	chain, routeErr := exchange.MatchRoute(account, asset, coin)
	if routeErr == nil {
		quote.Route = exchangeRoute{Supported: true, Chain: chain.Chain, ChainType: chain.ChainType, DepositEnabled: chain.ChainDeposit == "1", WithdrawEnabled: chain.ChainWithdraw == "1" && info.IsMaster && exchangePermission(info, "Withdraw"), MinDeposit: chain.DepositMin, MinWithdraw: chain.WithdrawMin, WithdrawFee: chain.WithdrawFee, WithdrawPercentageFee: chain.WithdrawPercentageFee, ContractAddress: chain.ContractAddress}
		if !exchangeWithdrawalRegionSupported(client.Endpoint()) {
			quote.Route.WithdrawEnabled = false
			quote.Route.Reason = "This Bybit region requires an additional withdrawal questionnaire which is not implemented. Deposit and spot trading remain separate; withdraw through Bybit after completing its required information."
		}
	} else {
		quote.Route.Reason = "No unambiguous Bybit deposit/withdrawal route matches this exact wallet network and token contract."
	}
	quote.Balances, err = exchangeBalances(ctx, client, asset.Symbol, "USDT")
	if err != nil {
		return quote, chain, info, err
	}
	if asset.Symbol == "USDT" {
		return quote, chain, info, nil
	}
	symbol := asset.Symbol + "USDT"
	instrument, err := client.SpotInstrument(ctx, symbol)
	if err != nil {
		var apiErr *bybit.APIError
		if errors.Is(err, bybit.ErrNotListed) || errors.As(err, &apiErr) && apiErr.Code == 10001 {
			return quote, chain, info, nil
		}
		return quote, chain, info, err
	}
	if instrument.Symbol != symbol || instrument.BaseCoin != asset.Symbol || instrument.QuoteCoin != "USDT" || instrument.Status != "Trading" {
		return quote, chain, info, errExchange
	}
	ticker, err := client.SpotTicker(ctx, symbol)
	if err != nil {
		return quote, chain, info, err
	}
	fee, err := client.SpotFeeRate(ctx, symbol)
	if err != nil {
		return quote, chain, info, err
	}
	if ticker.Symbol != symbol || fee.Symbol != symbol {
		return quote, chain, info, errExchange
	}
	quote.Symbol, quote.LastPrice, quote.BidPrice, quote.AskPrice, quote.FeeRate = symbol, ticker.LastPrice, ticker.Bid1Price, ticker.Ask1Price, fee.TakerFeeRate
	quote.BasePrecision, quote.QuotePrecision, quote.MinOrderAmount, quote.MaxOrderQty = instrument.LotSizeFilter.BasePrecision, instrument.LotSizeFilter.QuotePrecision, instrument.LotSizeFilter.MinOrderAmt, instrument.LotSizeFilter.MaxMarketOrderQty
	return quote, chain, info, nil
}
func (c *core) exchangeDepositAt(epoch uint64, client exchangeAPI, accountID, assetID, value string) (exchangeResult, error) {
	quote, chain, _, err := c.exchangeQuoteAt(epoch, client, accountID, assetID, "Sell")
	if err != nil {
		return exchangeResult{}, err
	}
	if !quote.Route.Supported || !quote.Route.DepositEnabled || !exchangeAtLeast(value, chain.DepositMin) {
		return exchangeResult{}, errExchange
	}
	account, asset, adapter, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return exchangeResult{}, err
	}
	atomic, err := amount.Parse(value, asset.Decimals)
	if err != nil || atomic == "0" {
		return exchangeResult{}, errExchange
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	deposit, err := client.DepositAddress(ctx, asset.Symbol, chain.Chain)
	cancel()
	if err != nil {
		return exchangeResult{}, err
	}
	// Bybit's deposit-address endpoint reports a contract suffix, not necessarily
	// the full contract. CoinInfo supplies the full already-verified identity.
	contractMatches := deposit.ContractAddress == "" && asset.Contract == ""
	if asset.Contract != "" {
		contractMatches = strings.EqualFold(deposit.ContractAddress, asset.Contract) || len(deposit.ContractAddress) == 6 && strings.EqualFold(deposit.ContractAddress, asset.Contract[len(asset.Contract)-6:])
	}
	if !contractMatches || exchange.ValidateRoute(account, asset, deposit.Chain, deposit.ChainType, chain.ContractAddress) != nil || deposit.ChainType != chain.ChainType || adapter.ValidateAddress(account.Network, deposit.Address) != nil || deposit.Tag != "" && account.Family != "ton" || account.Family == "ton" && deposit.Tag == "" {
		return exchangeResult{}, errExchange
	}
	transfer := model.Transfer{AccountID: account.ID, Asset: asset.ID, Recipient: deposit.Address, Amount: atomic, Memo: deposit.Tag}
	ctx, cancel = c.networkContextAt(45*time.Second, epoch)
	plan, err := adapter.Prepare(ctx, account, transfer)
	cancel()
	if err != nil {
		return exchangeResult{}, err
	}
	if plan.ID == "" {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return exchangeResult{}, err
		}
		plan.ID = hex.EncodeToString(id[:])
	}
	if plan.Expires == 0 {
		plan.Expires = time.Now().Add(time.Minute).Unix()
	}
	if err = c.childCallAt(epoch, "prepare", plan, nil); err != nil {
		return exchangeResult{}, err
	}
	result, err := c.paymentAt("send", plan.ID, epoch)
	if err != nil {
		return exchangeResult{}, err
	}
	receipt, ok := result.(model.Receipt)
	if !ok {
		return exchangeResult{}, errExchange
	}
	return exchangeResult{ID: plan.ID, Kind: "deposit", State: receipt.State, TransactionID: receipt.Identity, Message: "This is the original on-chain wallet payment, retained in the Local payment journal in Settings. Chain submission does not establish Bybit deposit credit; wait for exchange credit before swapping. TON includes the required deposit memo."}, nil
}
func exchangeAvailable(balances []exchangeBalance, kind, coin string) string {
	for _, b := range balances {
		if b.AccountType == kind && b.Coin == coin {
			return b.Available
		}
	}
	return ""
}
func exchangeFeeEstimate(quote exchangeQuote, value, side string) (string, string, error) {
	qty, e := exchangeAtomic(value)
	rate, f := exchangeAtomic(quote.FeeRate)
	price, g := exchangeAtomic(quote.LastPrice)
	if e != nil || f != nil || g != nil || qty.Sign() <= 0 || price.Sign() <= 0 || rate.Cmp(exchangeScale()) >= 0 || side != "Buy" && side != "Sell" {
		return "", "", errExchange
	}
	fee := new(big.Int)
	coin := quote.Coin
	if side == "Sell" {
		fee.Mul(qty, price)
		fee.Mul(fee, rate)
		fee.Div(fee, new(big.Int).Mul(exchangeScale(), exchangeScale()))
		coin = "USDT"
	} else {
		fee.Mul(qty, rate)
		fee.Div(fee, price)
	}
	formatted, err := exchangeDecimal(fee)
	if err != nil {
		return "", "", err
	}
	return formatted, coin, nil
}
func validateExchangeOrder(quote exchangeQuote, value, slippage string) error {
	qty, err := exchangeAtomic(value)
	slip, e := amount.Parse(slippage, 2)
	if err != nil || qty.Sign() <= 0 || e != nil {
		return errExchange
	}
	s, _ := amount.Atomic(slip)
	if s.Sign() <= 0 || s.Cmp(big.NewInt(100)) > 0 {
		return errExchange
	}
	step := quote.BasePrecision
	if quote.Side == "Buy" {
		step = quote.QuotePrecision
	}
	precision, err := exchangeAtomic(step)
	if err != nil || precision.Sign() <= 0 || new(big.Int).Mod(qty, precision).Sign() != 0 {
		return errExchange
	}
	minimum, err := exchangeAtomic(quote.MinOrderAmount)
	if err != nil {
		return errExchange
	}
	max, err := exchangeAtomic(quote.MaxOrderQty)
	if err != nil || max.Sign() <= 0 {
		return errExchange
	}
	if quote.Side == "Buy" {
		if qty.Cmp(minimum) < 0 {
			return errExchange
		}
		bid, err := exchangeAtomic(quote.BidPrice)
		if err != nil || bid.Sign() <= 0 {
			return errExchange
		}
		if new(big.Int).Mul(qty, exchangeScale()).Cmp(new(big.Int).Mul(max, bid)) > 0 {
			return errExchange
		}
	} else {
		bid, err := exchangeAtomic(quote.BidPrice)
		if err != nil || bid.Sign() <= 0 || qty.Cmp(max) > 0 {
			return errExchange
		}
		// Minimum notional is checked at the worst approved sell price.
		notional := new(big.Int).Mul(qty, bid)
		notional.Mul(notional, new(big.Int).Sub(big.NewInt(10000), s))
		notional.Div(notional, new(big.Int).Mul(exchangeScale(), big.NewInt(10000)))
		if notional.Cmp(minimum) < 0 {
			return errExchange
		}
	}
	return nil
}
func (c *core) ensureExchangeFunds(epoch uint64, client exchangeAPI, info bybit.APIKeyInformation, accountID, assetID, coin, value, target string) (*exchangeResult, error) {
	ctx, cancel := c.networkContextAt(30*time.Second, epoch)
	balances, err := exchangeBalances(ctx, client, coin)
	cancel()
	if err != nil {
		return nil, err
	}
	available := exchangeAvailable(balances, target, coin)
	if exchangeAtLeast(available, value) {
		return nil, nil
	}
	if !exchangePermission(info, "AccountTransfer") {
		return nil, errExchange
	}
	from := "FUND"
	if target == "FUND" {
		from = "UNIFIED"
	}
	qty, err := exchangeAtomic(value)
	existing, e := exchangeAtomic(available)
	if err != nil || e != nil {
		return nil, errExchange
	}
	difference, err := exchangeDecimal(new(big.Int).Sub(qty, existing))
	if err != nil {
		return nil, err
	}
	if !exchangeAtLeast(exchangeAvailable(balances, from, coin), difference) {
		return nil, errExchange
	}
	transfer := bybit.TransferRequest{Coin: coin, Amount: difference, FromAccountType: from, ToAccountType: target}
	result, err := c.executeExchangeAt(epoch, client, exchangeIntent{Kind: "transfer", AccountID: accountID, AssetID: assetID, Transfer: &transfer, Fee: "0", FeeCoin: coin})
	if err != nil {
		return nil, err
	}
	if result.State != "CONFIRMED" {
		return &result, nil
	}
	ctx, cancel = c.networkContextAt(30*time.Second, epoch)
	balances, err = exchangeBalances(ctx, client, coin)
	cancel()
	if err != nil {
		return nil, err
	}
	if !exchangeAtLeast(exchangeAvailable(balances, target, coin), value) {
		return &exchangeResult{ID: result.ID, Kind: "transfer", State: "CONFIRMED", Message: "The internal transfer is confirmed. Refresh balances and approve a new swap/withdrawal only after the target balance is available."}, nil
	}
	return nil, nil
}
func (c *core) exchangeSwapAt(epoch uint64, client exchangeAPI, accountID, assetID, side, value, slippage string) (exchangeResult, error) {
	quote, _, info, err := c.exchangeQuoteAt(epoch, client, accountID, assetID, side)
	if err != nil {
		return exchangeResult{}, err
	}
	if quote.Symbol == "" || !exchangePermission(info, "SpotTrade") || validateExchangeOrder(quote, value, slippage) != nil {
		return exchangeResult{}, errExchange
	}
	coin, unit := quote.Coin, "baseCoin"
	if side == "Buy" {
		coin, unit = "USDT", "quoteCoin"
	}
	if transfer, err := c.ensureExchangeFunds(epoch, client, info, accountID, assetID, coin, value, "UNIFIED"); err != nil {
		return exchangeResult{}, err
	} else if transfer != nil {
		return *transfer, nil
	}
	fee, feeCoin, err := exchangeFeeEstimate(quote, value, side)
	if err != nil {
		return exchangeResult{}, err
	}
	order := bybit.SpotOrder{Category: "spot", Symbol: quote.Symbol, Side: side, OrderType: "Market", Qty: value, MarketUnit: unit, IsLeverage: 0, SlippageToleranceType: "Percent", SlippageTolerance: slippage}
	return c.executeExchangeAt(epoch, client, exchangeIntent{Kind: "swap", AccountID: accountID, AssetID: assetID, Order: &order, LastPrice: quote.LastPrice, Fee: fee, FeeCoin: feeCoin})
}
func (c *core) exchangeWithdrawAt(epoch uint64, client exchangeAPI, accountID, assetID, value string) (exchangeResult, error) {
	quote, chain, info, err := c.exchangeQuoteAt(epoch, client, accountID, assetID, "Sell")
	if err != nil {
		return exchangeResult{}, err
	}
	if !quote.Route.Supported || !quote.Route.WithdrawEnabled || !info.IsMaster || !exchangePermission(info, "Withdraw") || chain.WithdrawFee == "" || !exchangeAtLeast(value, chain.WithdrawMin) {
		return exchangeResult{}, errExchange
	}
	account, asset, _, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return exchangeResult{}, err
	}
	precision, err := strconv.ParseUint(chain.MinAccuracy, 10, 8)
	if err != nil || precision > 36 {
		return exchangeResult{}, errExchange
	}
	if precision > uint64(asset.Decimals) {
		precision = uint64(asset.Decimals)
	}
	atomic, err := amount.Parse(value, uint8(precision))
	if err != nil || atomic == "0" {
		return exchangeResult{}, errExchange
	}
	fee, e := exchangeWithdrawalFee(value, chain.WithdrawFee, chain.WithdrawPercentageFee, uint8(precision))
	if e != nil || chain.WithdrawMax != "-1" && !exchangeAtLeast(chain.WithdrawMax, value) {
		return exchangeResult{}, errExchange
	}
	if transfer, err := c.ensureExchangeFunds(epoch, client, info, accountID, assetID, asset.Symbol, value, "FUND"); err != nil {
		return exchangeResult{}, err
	} else if transfer != nil {
		return *transfer, nil
	}
	w := bybit.WithdrawalRequest{Coin: asset.Symbol, Chain: chain.Chain, Address: account.Address, Amount: value, ForceChain: 1, AccountType: "FUND", FeeType: 1}
	return c.executeExchangeAt(epoch, client, exchangeIntent{Kind: "withdraw", AccountID: accountID, AssetID: assetID, Withdrawal: &w, Fee: fee, FeeCoin: asset.Symbol, ChainType: chain.ChainType, ContractAddress: chain.ContractAddress})
}

// Bybit's documented feeType=1 formula is fixed+(input-fixed)*rate.
// Round the estimate upward at the withdrawal precision, without rounding the
// user's amount. The service still determines its actual fee at processing.
func exchangeWithdrawalFee(value, fixed, percentage string, precision uint8) (string, error) {
	gross, e := exchangeAtomic(value)
	flat, f := exchangeAtomic(fixed)
	rate, g := exchangeAtomic(percentage)
	if e != nil || f != nil || g != nil || precision > 36 || gross.Cmp(flat) <= 0 || rate.Cmp(exchangeScale()) >= 0 {
		return "", errExchange
	}
	fee := new(big.Int).Sub(gross, flat)
	fee.Mul(fee, rate)
	fee.Add(fee, new(big.Int).Sub(exchangeScale(), big.NewInt(1)))
	fee.Div(fee, exchangeScale())
	fee.Add(fee, flat)
	step := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(36-precision)), nil)
	fee.Add(fee, new(big.Int).Sub(step, big.NewInt(1)))
	fee.Div(fee, step)
	fee.Mul(fee, step)
	if fee.Cmp(gross) >= 0 {
		return "", errExchange
	}
	return exchangeDecimal(fee)
}
func (c *core) updateExchangeAt(epoch uint64, id, state, externalID string) (vault.ExchangeOperation, error) {
	var operation vault.ExchangeOperation
	err := c.childCallAt(epoch, "exchangeUpdate", struct {
		ID         string `json:"id"`
		State      string `json:"state"`
		ExternalID string `json:"externalId"`
	}{id, state, externalID}, &operation)
	return operation, err
}
func (c *core) executeExchangeAt(epoch uint64, client exchangeAPI, intent exchangeIntent) (exchangeResult, error) {
	var operation vault.ExchangeOperation
	if err := c.childCallAt(epoch, "exchangePrepare", intent, &operation); err != nil {
		return exchangeResult{}, err
	}
	if err := c.childCallAt(epoch, "exchangeAuthorize", struct {
		ID string `json:"id"`
	}{operation.ID}, &operation); err != nil {
		return exchangeResult{}, err
	}
	ctx, cancel := c.networkContextAt(30*time.Second, epoch)
	defer cancel()
	externalID := ""
	var err error
	switch operation.Kind {
	case "swap":
		var order bybit.SpotOrder
		if strictjson.Decode([]byte(operation.Payload), &order) != nil {
			return exchangeResult{}, errExchange
		}
		var ack bybit.OrderAcknowledgement
		ack, err = client.CreateOrder(ctx, order)
		externalID = ack.OrderID
	case "transfer":
		var transfer bybit.TransferRequest
		if strictjson.Decode([]byte(operation.Payload), &transfer) != nil {
			return exchangeResult{}, errExchange
		}
		var ack bybit.TransferAcknowledgement
		ack, err = client.Transfer(ctx, transfer)
		externalID = ack.TransferID
	case "withdraw":
		var withdrawal bybit.WithdrawalRequest
		if strictjson.Decode([]byte(operation.Payload), &withdrawal) != nil {
			return exchangeResult{}, errExchange
		}
		var ack bybit.WithdrawalAcknowledgement
		ack, err = client.Withdraw(ctx, withdrawal)
		externalID = ack.ID
	default:
		return exchangeResult{}, errExchange
	}
	state := "PENDING"
	if err != nil {
		var apiError *bybit.APIError
		if errors.As(err, &apiError) && apiError.HTTPStatus == 0 {
			if message := exchangeRejectionMessage(apiError.Code); message != "" {
				var rejected vault.ExchangeOperation
				failure := c.childCallAt(epoch, "exchangeReject", struct {
					ID   string `json:"id"`
					Code int64  `json:"code"`
				}{operation.ID, apiError.Code}, &rejected)
				if failure != nil {
					return exchangeResult{}, failure
				}
				return exchangeResult{ID: operation.ID, Kind: operation.Kind, State: rejected.State, Message: message}, nil
			}
		}
		state = "UNKNOWN"
		externalID = ""
	}
	updated, persistErr := c.updateExchangeAt(epoch, operation.ID, state, externalID)
	if persistErr != nil {
		return exchangeResult{}, persistErr
	}
	if state == "UNKNOWN" {
		return exchangeResult{ID: operation.ID, Kind: operation.Kind, State: state, Message: "The request result is unknown. Its original operation is retained and new exchange writes are blocked. Reconcile it; never create a replacement after a timeout. A lost withdrawal acknowledgement cannot be located safely by requestId in Bybit's records API."}, nil
	}
	return c.reconcileExchangeAt(epoch, client, updated)
}

func exchangeRejectionMessage(code int64) string {
	switch code {
	case 10001:
		return "Bybit rejected the request parameters before acceptance. Check amount limits, the exact whitelisted destination and any regional withdrawal requirements."
	case 10003:
		return "Bybit rejected the API key. Check the account's regional API endpoint and key validity."
	case 10004:
		return "Bybit rejected request authentication. Check the API key and secret in the dedicated connection dialog."
	case 10005:
		return "Bybit denied the required API permission. Adjust the key permissions in Bybit before approving another operation."
	}
	return ""
}
func (c *core) reconcileExchangeAt(epoch uint64, client exchangeAPI, operation vault.ExchangeOperation) (exchangeResult, error) {
	result := exchangeResult{ID: operation.ID, Kind: operation.Kind, State: operation.State, RemoteID: operation.ExternalID}
	if operation.State == "CONFIRMED" || operation.State == "FAILED" || operation.State == "CANCELLED" {
		if operation.Kind == "withdraw" && operation.State == "CONFIRMED" {
			result.State = "EXCHANGE_COMPLETED"
		}
		result.Message = "Recorded exchange result. A completed withdrawal is the exchange's report, not independent on-chain finality."
		return result, nil
	}
	if operation.State != "SUBMITTING" && operation.State != "UNKNOWN" && operation.State != "PENDING" {
		return result, errExchange
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	state := "UNKNOWN"
	externalID := operation.ExternalID
	switch operation.Kind {
	case "swap":
		var request bybit.SpotOrder
		if strictjson.Decode([]byte(operation.Payload), &request) != nil {
			return result, errExchange
		}
		order, err := client.FindOrder(ctx, request.Symbol, request.OrderLinkID)
		if err != nil || order == nil {
			break
		}
		if order.OrderLinkID != request.OrderLinkID || order.Symbol != request.Symbol || order.Side != request.Side || !sameExchangeAmount(order.Qty, request.Qty) || order.MarketUnit != request.MarketUnit || order.OrderType != "Market" || order.IsLeverage != "0" || externalID != "" && order.OrderID != externalID {
			return result, errExchange
		}
		externalID = order.OrderID
		switch order.OrderStatus {
		case "New", "Created", "Untriggered", "Triggered", "PartiallyFilled":
			state = "PENDING"
		default:
			base, quote, fees, err := verifiedExchangeExecutions(ctx, client, request, *order)
			if err != nil {
				break
			}
			result.FilledBase, result.FilledQuote, result.Fees = base, quote, fees
			switch order.OrderStatus {
			case "Filled":
				if base == "0" {
					break
				}
				state = "CONFIRMED"
				result.Message = "Spot execution and fee records match the original order; the executed price may differ from the last quote."
			case "Cancelled", "Canceled", "PartiallyFilledCanceled", "Rejected", "Deactivated":
				state = "FAILED"
				result.Message = "The exchange confirms the unfilled order is terminal."
				if base != "0" {
					state = "CONFIRMED"
					result.Message = "The order filled partially and the remainder was cancelled. Only the reported executed amounts were exchanged."
				}
			}
		}
	case "transfer":
		var request bybit.TransferRequest
		if strictjson.Decode([]byte(operation.Payload), &request) != nil {
			return result, errExchange
		}
		record, err := client.TransferStatus(ctx, request.TransferID)
		if err != nil || record == nil {
			break
		}
		if record.TransferID != request.TransferID || record.Coin != request.Coin || !sameExchangeAmount(record.Amount, request.Amount) || record.FromAccountType != request.FromAccountType || record.ToAccountType != request.ToAccountType {
			return result, errExchange
		}
		externalID = record.TransferID
		switch record.Status {
		case "SUCCESS":
			state = "CONFIRMED"
			result.Message = "The exact internal Funding/Unified transfer is confirmed."
		case "FAILED":
			state = "FAILED"
		case "PENDING":
			state = "PENDING"
		}
	case "withdraw":
		var request bybit.WithdrawalRequest
		if strictjson.Decode([]byte(operation.Payload), &request) != nil {
			return result, errExchange
		}
		if externalID == "" {
			result.Message = "Withdrawal acknowledgement was lost. Bybit's records do not expose requestId; do not retry or infer a match from amount/address. Verify the original request independently with Bybit."
			break
		}
		page, err := client.WithdrawalRecords(ctx, request.Coin, externalID, "")
		if err != nil {
			break
		}
		if len(page.Rows) != 1 || page.NextPageCursor != "" {
			break
		}
		record := page.Rows[0]
		if record.WithdrawID != externalID || record.Coin != request.Coin || record.Chain != request.Chain || record.ToAddress != request.Address || record.Tag != request.Tag {
			return result, errExchange
		}
		// The record API does not define whether `amount` is gross or net for
		// feeType=1. Never invent that relation or infer an on-chain credit.
		if _, e := exchangeAtomic(record.Amount); e != nil {
			return result, errExchange
		}
		if _, e := exchangeAtomic(record.WithdrawFee); e != nil {
			return result, errExchange
		}
		result.ReportedAmount, result.ReportedFee = record.Amount, record.WithdrawFee
		result.TransactionID = record.TxID
		switch record.Status {
		case "success", "successConfirmed":
			state = "CONFIRMED"
			result.Message = "Bybit reports this exact withdrawal completed. This does not independently establish on-chain credit/finality; refresh the destination wallet balance."
		case "SecurityCheck", "Pending", "BlockchainConfirmed", "MoreInformationRequired", "Unknown":
			state = "PENDING"
		case "CancelByUser", "Reject", "Fail":
			state = "FAILED"
		}
	default:
		return result, errExchange
	}
	updated, err := c.updateExchangeAt(epoch, operation.ID, state, externalID)
	if err != nil {
		return result, err
	}
	result.State, result.RemoteID = updated.State, updated.ExternalID
	if operation.Kind == "withdraw" && result.State == "CONFIRMED" {
		result.State = "EXCHANGE_COMPLETED"
	}
	if result.Message == "" {
		result.Message = "No complete exact exchange result is established. Reconcile this original operation; no replacement was created."
	}
	return result, nil
}
func sameExchangeAmount(a, b string) bool {
	x, e := exchangeAtomic(a)
	y, f := exchangeAtomic(b)
	return e == nil && f == nil && x.Cmp(y) == 0
}
func verifiedExchangeExecutions(ctx context.Context, client exchangeAPI, request bybit.SpotOrder, order bybit.Order) (string, string, map[string]string, error) {
	base, quote := new(big.Int), new(big.Int)
	fees := map[string]*big.Int{}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	feeCoin := "USDT"
	if request.Side == "Buy" {
		feeCoin = strings.TrimSuffix(request.Symbol, "USDT")
	}
	for pageIndex := 0; pageIndex < 32; pageIndex++ {
		page, err := client.Executions(ctx, request.Symbol, request.OrderLinkID, cursor)
		if err != nil {
			return "", "", nil, err
		}
		if page.Category != "spot" {
			return "", "", nil, errExchange
		}
		for _, fill := range page.List {
			if fill.OrderID != order.OrderID || fill.OrderLinkID != request.OrderLinkID || fill.Symbol != request.Symbol || fill.Side != request.Side || fill.OrderType != "Market" || seen[fill.ExecID] || fill.ExecID == "" {
				return "", "", nil, errExchange
			}
			seen[fill.ExecID] = true
			qty, e := exchangeAtomic(fill.ExecQty)
			value, f := exchangeAtomic(fill.ExecValue)
			fee, g := exchangeAtomic(fill.ExecFee)
			price, h := exchangeAtomic(fill.ExecPrice)
			if e != nil || f != nil || g != nil || h != nil || qty.Sign() <= 0 || value.Sign() <= 0 || price.Sign() <= 0 || fill.FeeCurrency != feeCoin {
				return "", "", nil, errExchange
			}
			base.Add(base, qty)
			quote.Add(quote, value)
			if fees[fill.FeeCurrency] == nil {
				fees[fill.FeeCurrency] = new(big.Int)
			}
			fees[fill.FeeCurrency].Add(fees[fill.FeeCurrency], fee)
			extraFees, err := bybit.ExecutionExtraFees(fill.ExtraFees)
			if err != nil {
				return "", "", nil, err
			}
			for _, extra := range extraFees {
				if extra.FeeCoin != feeCoin {
					return "", "", nil, errExchange
				}
				tax, err := exchangeAtomic(extra.Fee)
				if err != nil {
					return "", "", nil, err
				}
				fees[feeCoin].Add(fees[feeCoin], tax)
			}
		}
		if page.NextPageCursor == "" {
			baseText, err := exchangeDecimal(base)
			if err != nil {
				return "", "", nil, err
			}
			quoteText, err := exchangeDecimal(quote)
			if err != nil {
				return "", "", nil, err
			}
			if !sameExchangeAmount(baseText, order.CumExecQty) || !sameExchangeAmount(quoteText, order.CumExecValue) {
				return "", "", nil, errExchange
			}
			out := map[string]string{}
			for coin, fee := range fees {
				formatted, err := exchangeDecimal(fee)
				if err != nil {
					return "", "", nil, err
				}
				out[coin] = formatted
			}
			spend, _ := exchangeAtomic(request.Qty)
			received := quote
			if request.Side == "Buy" {
				received = base
			}
			if charged := fees[feeCoin]; charged != nil && charged.Cmp(received) > 0 {
				return "", "", nil, errExchange
			}
			if request.Side == "Sell" {
				if base.Cmp(spend) > 0 || order.OrderStatus == "Filled" && base.Cmp(spend) != 0 {
					return "", "", nil, errExchange
				}
			} else if quote.Cmp(spend) > 0 {
				return "", "", nil, errExchange
			}
			return baseText, quoteText, out, nil
		}
		if cursors[page.NextPageCursor] || len(page.List) == 0 {
			return "", "", nil, errExchange
		}
		cursors[page.NextPageCursor] = true
		cursor = page.NextPageCursor
	}
	return "", "", nil, errExchange
}
