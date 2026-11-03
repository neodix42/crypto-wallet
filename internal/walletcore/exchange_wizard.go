package walletcore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

type exchangeWithdrawalChoice struct {
	AccountID     string `json:"accountId"`
	AssetID       string `json:"assetId"`
	Network       string `json:"network"`
	NetworkName   string `json:"networkName"`
	Index         uint32 `json:"index"`
	Coin          string `json:"coin"`
	Address       string `json:"address"`
	Chain         string `json:"chain"`
	Minimum       string `json:"minimum"`
	Fee           string `json:"fee"`
	PercentageFee string `json:"percentageFee"`
	Precision     string `json:"precision"`
}
type exchangeSwapChoice struct {
	ID         string `json:"id"`
	AccountID  string `json:"accountId"`
	AssetID    string `json:"assetId"`
	Side       string `json:"side"`
	QuoteCoin  string `json:"quoteCoin"`
	OutputCoin string `json:"outputCoin"`
	Symbol     string `json:"symbol"`
}
type exchangeDepositCheck struct {
	Ready           bool              `json:"ready"`
	ChainConfirmed  bool              `json:"chainConfirmed"`
	ChainState      string            `json:"chainState"`
	Message         string            `json:"message"`
	Coin            string            `json:"coin,omitempty"`
	DepositedAmount string            `json:"depositedAmount,omitempty"`
	TransactionID   string            `json:"transactionId,omitempty"`
	ReadError       string            `json:"readError,omitempty"`
	ReportedAmount  string            `json:"reportedAmount,omitempty"`
	DepositFee      string            `json:"depositFee,omitempty"`
	TransferBudget  string            `json:"transferBudget,omitempty"`
	Balances        []exchangeBalance `json:"balances,omitempty"`
}

func (c *core) exchangeWizardAt(message ipc.Message, epoch uint64) (any, error) {
	client, status, err := c.exchangeClientAt(epoch)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if provider, ok := exchange.ProviderForEndpoint(client.Endpoint()); !ok || provider.ID != "bybit" {
		return nil, errExchange
	}
	ctx, cancel := c.networkContextAt(90*time.Second, epoch)
	defer cancel()
	switch message.Method {
	case "exchangeFundingMove", "exchangeUnifiedMove":
		var in struct {
			AccountID strictjson.Text `json:"accountId"`
			AssetID   strictjson.Text `json:"assetId"`
			Amount    strictjson.Text `json:"amount"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, ipc.NewRequestError("INVALID_ARGUMENT")
		}
		if message.Method == "exchangeUnifiedMove" {
			return c.exchangeUnifiedMoveAt(epoch, ctx, client, string(in.AccountID), string(in.AssetID), string(in.Amount))
		}
		return c.exchangeFundingMoveAt(epoch, ctx, client, string(in.AccountID), string(in.AssetID), string(in.Amount))
	case "exchangeFundingBalances", "exchangeSwapOptions":
		var in struct {
			AccountID strictjson.Text    `json:"accountId"`
			AssetID   strictjson.Text    `json:"assetId"`
			SpotOnly  exchangeActionFlag `json:"spotOnly,omitempty"`
		}
		if strictjson.Decode(message.Body, &in) != nil || !exactExchangeModeField(message.Body, "spotOnly") || bool(in.SpotOnly) && message.Method != "exchangeSwapOptions" {
			return nil, errExchange
		}
		account, asset, _, err := c.exchangeAsset(string(in.AccountID), string(in.AssetID))
		if err != nil {
			return nil, err
		}
		if message.Method == "exchangeSwapOptions" {
			if bool(in.SpotOnly) {
				return c.exchangeSpotCandidatesAt(ctx, client, account)
			}
			return c.exchangeSwapOptionsAt(ctx, client, account, asset)
		}
		// Stablecoin custody balances are independent of the original deposit
		// asset and wallet network. Every wizard page needs both pools even
		// when its selected source remains GRAM; no route or payment is implied.
		balances, err := exchangeBalances(ctx, client, asset.Symbol, "USDC", "USDT")
		if err != nil {
			return nil, exchangeReadFailure("BALANCE", err)
		}
		return struct {
			Coin     string            `json:"coin"`
			Balances []exchangeBalance `json:"balances"`
		}{asset.Symbol, balances}, nil
	case "exchangeWithdrawOptions":
		var in struct {
			Coin            strictjson.Text    `json:"coin"`
			ExistingFunds   exchangeActionFlag `json:"existingFunds,omitempty"`
			SourceAccountID strictjson.Text    `json:"sourceAccountId,omitempty"`
		}
		if strictjson.Decode(message.Body, &in) != nil || !exactExchangeModeField(message.Body, "existingFunds") || !bool(in.ExistingFunds) && in.SourceAccountID != "" {
			return nil, errExchange
		}
		if bool(in.ExistingFunds) {
			if in.SourceAccountID == "" {
				return nil, ipc.NewRequestError("INVALID_ARGUMENT")
			}
			source, err := c.account(string(in.SourceAccountID))
			if err != nil {
				return nil, err
			}
			return c.exchangeWithdrawalChoicesAt(ctx, client, string(in.Coin), source.Index)
		}
		if status.Wizard == nil || status.Wizard.Step != 4 || status.Wizard.OutputCoin != string(in.Coin) {
			return nil, ipc.NewRequestError("EXCHANGE_WIZARD_STALE")
		}
		source, err := c.account(status.Wizard.SourceAccount)
		if err != nil {
			return nil, err
		}
		return c.exchangeWithdrawalChoicesAt(ctx, client, string(in.Coin), source.Index)
	case "exchangeDepositCheck", "exchangeFundingTransfer":
		var in struct {
			WizardID strictjson.Text `json:"wizardId"`
			Amount   strictjson.Text `json:"amount,omitempty"`
		}
		if strictjson.Decode(message.Body, &in) != nil || status.Wizard == nil || status.Wizard.ID != string(in.WizardID) {
			return nil, ipc.NewRequestError("EXCHANGE_WIZARD_STALE")
		}
		w := *status.Wizard
		check, err := c.exchangeDepositCheckAt(epoch, ctx, client, w)
		if err != nil {
			return nil, err
		}
		if message.Method == "exchangeDepositCheck" {
			return check, nil
		}
		if check.ReadError != "" {
			return nil, ipc.NewRequestError(check.ReadError)
		}
		if w.Step != 2 || w.TransferOperation != "" || !check.Ready {
			return nil, ipc.NewRequestError("EXCHANGE_DEPOSIT_PENDING")
		}
		_, asset, _, err := c.exchangeAsset(w.SourceAccount, w.SourceAsset)
		if err != nil {
			return nil, err
		}
		atomic, e := amount.Parse(string(in.Amount), asset.Decimals)
		if e != nil || atomic == "0" || !exchangeAtLeast(w.DepositAmount, string(in.Amount)) || !exchangeAtLeast(check.TransferBudget, string(in.Amount)) {
			return nil, ipc.NewRequestError("INVALID_ARGUMENT")
		}
		info, err := client.APIKeyInfo(ctx)
		if err != nil {
			return nil, exchangeReadFailure("KEY", err)
		}
		if info.UTA != 1 {
			return nil, ipc.NewRequestError("EXCHANGE_UNIFIED_REQUIRED")
		}
		if !exchangePermission(info, "AccountTransfer") {
			return nil, ipc.NewRequestError("EXCHANGE_TRANSFER_PERMISSION")
		}
		if !exchangeAtLeast(exchangeAvailable(check.Balances, "FUND", asset.Symbol), string(in.Amount)) {
			return nil, ipc.NewRequestError("EXCHANGE_FUNDING_INSUFFICIENT")
		}
		request := bybit.TransferRequest{Coin: asset.Symbol, Amount: string(in.Amount), FromAccountType: "FUND", ToAccountType: "UNIFIED"}
		return c.executeExchangeAt(epoch, client, exchangeIntent{WizardID: w.ID, Kind: "transfer", AccountID: w.SourceAccount, AssetID: w.SourceAsset, Transfer: &request, Fee: "0", FeeCoin: asset.Symbol})
	}
	return nil, errExchange
}

func (c *core) exchangeWithdrawalChoicesAt(ctx context.Context, client exchangeAPI, coinName string, group uint32) ([]exchangeWithdrawalChoice, error) {
	choices := []exchangeWithdrawalChoice{}
	state, err := c.current()
	if err != nil {
		return nil, err
	}
	type candidate struct {
		account model.Account
		asset   model.Asset
		adapter model.Adapter
	}
	candidates := []candidate{}
	for _, account := range state.Accounts {
		if account.Hidden || account.Index != group {
			continue
		}
		adapter, err := c.adapter(account.Network)
		if err != nil {
			continue
		}
		for _, n := range adapter.Networks() {
			if n.ID != account.Network || n.Testnet {
				continue
			}
			for _, asset := range n.Assets {
				asset = exchange.LedgerAsset(asset)
				if asset.Symbol != coinName {
					continue
				}
				if _, _, _, err := c.exchangeAsset(account.ID, asset.ID); err == nil {
					candidates = append(candidates, candidate{account, asset, adapter})
				}
			}
		}
	}
	if len(candidates) == 0 {
		return choices, nil
	}
	info, err := client.APIKeyInfo(ctx)
	if err != nil {
		return nil, exchangeReadFailure("KEY", err)
	}
	if !exchangeWithdrawalAuthority("bybit", info) || !exchangePermission(info, "Withdraw") || !exchangeWithdrawalRegionSupported(client.Endpoint()) {
		return nil, ipc.NewRequestError("EXCHANGE_WITHDRAW_UNAVAILABLE")
	}
	coin, err := client.CoinInfo(ctx, coinName)
	if err != nil {
		if errors.Is(err, bybit.ErrNotListed) {
			return choices, nil
		}
		return nil, exchangeReadFailure("COIN", err)
	}
	for _, candidate := range candidates {
		chain, err := exchange.MatchProviderRoute("bybit", candidate.account, candidate.asset, coin)
		if err != nil || chain.ChainWithdraw != "1" || chain.WithdrawFee == "" {
			continue
		}
		precision, e := strconv.ParseUint(chain.MinAccuracy, 10, 8)
		if e != nil || precision > 36 {
			return nil, exchangeReadFailure("COIN", bybit.ErrInvalidResponse)
		}
		if precision > uint64(candidate.asset.Decimals) {
			precision = uint64(candidate.asset.Decimals)
		}
		chain.MinAccuracy = strconv.FormatUint(precision, 10)
		name := candidate.account.Network
		for _, n := range candidate.adapter.Networks() {
			if n.ID == candidate.account.Network {
				name = n.Name
			}
		}
		choices = append(choices, exchangeWithdrawalChoice{AccountID: candidate.account.ID, AssetID: candidate.asset.ID, Network: candidate.account.Network, NetworkName: name, Index: candidate.account.Index, Coin: coinName, Address: candidate.account.Address, Chain: chain.Chain, Minimum: chain.WithdrawMin, Fee: chain.WithdrawFee, PercentageFee: chain.WithdrawPercentageFee, Precision: chain.MinAccuracy})
	}
	return choices, nil
}

// Withdrawal is independent of spot pair/price/fee access, just as deposit is.
// Only the selected asset's current authenticated withdrawal route is needed.
func (c *core) exchangeWithdrawalQuoteAt(epoch uint64, client exchangeAPI, accountID, assetID string) (exchangeQuote, bybit.Chain, bybit.APIKeyInformation, error) {
	q := exchangeQuote{Provider: "bybit"}
	var chain bybit.Chain
	var info bybit.APIKeyInformation
	provider, known := exchange.ProviderForEndpoint(client.Endpoint())
	if !known || provider.ID != "bybit" {
		return q, chain, info, errExchange
	}
	account, asset, _, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return q, chain, info, err
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	info, err = client.APIKeyInfo(ctx)
	if err != nil {
		return q, chain, info, exchangeReadFailure("KEY", err)
	}
	coin, err := client.CoinInfo(ctx, asset.Symbol)
	if err != nil {
		return q, chain, info, exchangeReadFailure("COIN", err)
	}
	chain, err = exchange.MatchProviderRoute("bybit", account, asset, coin)
	if err != nil {
		return q, chain, info, ipc.NewRequestError("EXCHANGE_WITHDRAW_UNAVAILABLE")
	}
	q.Route.Supported = true
	q.Route.WithdrawEnabled = chain.ChainWithdraw == "1" && exchangeWithdrawalAuthority("bybit", info) && exchangePermission(info, "Withdraw") && exchangeWithdrawalRegionSupported(client.Endpoint())
	return q, chain, info, nil
}

// These are reviewed candidate ledger symbols, not claims that Bybit lists a
// pair or currently permits withdrawal. Only assets in the owned account group
// are candidates. Live SpotInstrument identity, status and current limits must
// pass before either direction is offered; withdrawals verify their routes
// separately and never inherit authority from this read-only list.
func (c *core) exchangeSpotCandidatesAt(ctx context.Context, client exchangeAPI, source model.Account) ([]exchangeSwapChoice, error) {
	state, err := c.current()
	if err != nil {
		return nil, err
	}
	reviewed := map[string]bool{"ETH": true, "HYPE": true, "NEAR": true, "APT": true, "GRAM": true, "ZEN": true, "USDC": true}
	type candidate struct{ accountID, assetID, coin string }
	baseAssets := []candidate{}
	seen := map[string]bool{}
	// Prefer the selected source when the same ledger coin has several owned
	// networks. Its wallet identity remains exact in subsequent approvals.
	accounts := append([]model.Account{source}, state.Accounts...)
	for _, account := range accounts {
		if account.Hidden || account.Index != source.Index {
			continue
		}
		adapter, err := c.adapter(account.Network)
		if err != nil {
			continue
		}
		for _, network := range adapter.Networks() {
			if network.ID != account.Network || network.Testnet {
				continue
			}
			for _, asset := range network.Assets {
				asset = exchange.LedgerAsset(asset)
				if !reviewed[asset.Symbol] || seen[asset.Symbol] {
					continue
				}
				if _, _, _, err := c.exchangeAsset(account.ID, asset.ID); err != nil {
					continue
				}
				seen[asset.Symbol] = true
				baseAssets = append(baseAssets, candidate{account.ID, asset.ID, asset.Symbol})
			}
		}
	}
	options := []exchangeSwapChoice{}
	for _, base := range baseAssets {
		for _, quote := range []string{"USDT", "USDC"} {
			if base.coin == quote {
				continue
			}
			symbol := base.coin + quote
			instrument, err := client.SpotInstrument(ctx, symbol)
			if err != nil {
				var api *bybit.APIError
				if errors.Is(err, bybit.ErrNotListed) || errors.As(err, &api) && api.Code == 10001 {
					continue
				}
				return nil, exchangeReadFailure("INSTRUMENT", err)
			}
			if !exchangeValidSpotInstrument(instrument, base.coin, quote) {
				return nil, exchangeReadFailure("INSTRUMENT", bybit.ErrInvalidResponse)
			}
			for _, side := range []string{"Sell", "Buy"} {
				output := quote
				if side == "Buy" {
					output = base.coin
				}
				options = append(options, exchangeSwapChoice{ID: strconv.Itoa(len(options)), AccountID: base.accountID, AssetID: base.assetID, Side: side, QuoteCoin: quote, OutputCoin: output, Symbol: symbol})
			}
		}
	}
	return options, nil
}

func exchangeValidSpotInstrument(instrument bybit.Instrument, base, quote string) bool {
	if instrument.Symbol != base+quote || instrument.BaseCoin != base || instrument.QuoteCoin != quote || instrument.Status != "Trading" || base == quote || !bybit.SpotQuote(quote) {
		return false
	}
	for _, value := range []string{instrument.LotSizeFilter.BasePrecision, instrument.LotSizeFilter.QuotePrecision, instrument.LotSizeFilter.MinOrderAmt, instrument.LotSizeFilter.MaxMarketOrderQty, instrument.PriceFilter.TickSize} {
		n, err := exchangeAtomic(value)
		if err != nil || n.Sign() <= 0 {
			return false
		}
	}
	return true
}

func (c *core) exchangeSwapOptionsAt(ctx context.Context, client exchangeAPI, source model.Account, input model.Asset) ([]exchangeSwapChoice, error) {
	options := []exchangeSwapChoice{}
	type proposal struct{ accountID, assetID, coin, side, quote, output string }
	proposals := []proposal{}
	if !bybit.SpotQuote(input.Symbol) {
		for _, quote := range []string{"USDT", "USDC"} {
			proposals = append(proposals, proposal{source.ID, input.ID, input.Symbol, "Sell", quote, quote})
		}
	} else {
		state, err := c.current()
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, account := range state.Accounts {
			if account.Hidden || account.Index != source.Index {
				continue
			}
			adapter, err := c.adapter(account.Network)
			if err != nil {
				continue
			}
			for _, n := range adapter.Networks() {
				if n.ID != account.Network || n.Testnet {
					continue
				}
				for _, asset := range n.Assets {
					asset = exchange.LedgerAsset(asset)
					if asset.Symbol == input.Symbol || seen[asset.Symbol] {
						continue
					}
					if _, _, _, err := c.exchangeAsset(account.ID, asset.ID); err != nil {
						continue
					}
					seen[asset.Symbol] = true
					if input.Symbol == "USDC" && asset.Symbol == "USDT" {
						proposals = append(proposals, proposal{source.ID, input.ID, "USDC", "Sell", "USDT", "USDT"})
					} else {
						proposals = append(proposals, proposal{account.ID, asset.ID, asset.Symbol, "Buy", input.Symbol, asset.Symbol})
					}
				}
			}
		}
	}
	for _, p := range proposals {
		symbol := p.coin + p.quote
		instrument, err := client.SpotInstrument(ctx, symbol)
		if err != nil {
			var api *bybit.APIError
			if errors.Is(err, bybit.ErrNotListed) || errors.As(err, &api) && api.Code == 10001 {
				continue
			}
			return nil, exchangeReadFailure("INSTRUMENT", err)
		}
		if instrument.Symbol != symbol || instrument.BaseCoin != p.coin || instrument.QuoteCoin != p.quote || instrument.Status != "Trading" {
			return nil, exchangeReadFailure("INSTRUMENT", bybit.ErrInvalidResponse)
		}
		choices, err := c.exchangeWithdrawalChoicesAt(ctx, client, p.output, source.Index)
		if err != nil {
			return nil, err
		}
		if len(choices) == 0 {
			continue
		}
		options = append(options, exchangeSwapChoice{ID: strconv.Itoa(len(options)), AccountID: p.accountID, AssetID: p.assetID, Side: p.side, QuoteCoin: p.quote, OutputCoin: p.output, Symbol: symbol})
	}
	return options, nil
}

func exchangeSameAddress(network, a, b string) bool {
	if network == "ton-mainnet" {
		parse := func(s string) *address.Address {
			var a *address.Address
			var e error
			if strings.Contains(s, ":") {
				a, e = address.ParseRawAddr(s)
			} else {
				a, e = address.ParseAddr(s)
			}
			if e != nil {
				return nil
			}
			return a
		}
		x, y := parse(a), parse(b)
		return x != nil && y != nil && x.Equals(y)
	}
	if strings.HasPrefix(a, "0x") && strings.HasPrefix(b, "0x") {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func exchangeSameTONHash(a, b string) bool {
	decode := func(s string) []byte {
		if len(s) == 64 {
			if b, e := hex.DecodeString(s); e == nil {
				return b
			}
		}
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			b, e := encoding.Strict().DecodeString(s)
			if e == nil && len(b) == 32 && encoding.EncodeToString(b) == s {
				return b
			}
		}
		return nil
	}
	x, y := decode(a), decode(b)
	return len(x) == 32 && len(y) == 32 && string(x) == string(y)
}

// Bybit does not document whether its reported deposit amount is gross or
// net of depositFee. Reserve every reported fee from the lesser of the signed
// original amount and the reported amount, so existing Funding funds cannot
// silently cover that fee. This can leave a second fee-sized reserve if the
// reported amount is already net. An empty fee remains "not reported" in the
// response; the resulting upper bound is not a claim that the fee was zero.
func exchangeDepositTransferBudget(original, reported, fee string, decimals uint8) (string, error) {
	limit, err := exchangeAtomic(original)
	observed, observedErr := exchangeAtomic(reported)
	if err != nil || observedErr != nil || decimals > 36 {
		return "", bybit.ErrInvalidResponse
	}
	if observed.Cmp(limit) < 0 {
		limit = observed
	}
	if fee != "" {
		reserved, feeErr := exchangeAtomic(fee)
		if feeErr != nil || reserved.Cmp(limit) > 0 {
			return "", bybit.ErrInvalidResponse
		}
		limit.Sub(limit, reserved)
	}
	// Any exchange-ledger precision beyond the wallet asset's precision is
	// retained as a reserve, never rounded up into an additional debit.
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(36-decimals)), nil)
	limit.Quo(limit, unit).Mul(limit, unit)
	return exchangeDecimal(limit)
}

func (c *core) exchangeDepositCheckAt(epoch uint64, ctx context.Context, client exchangeAPI, w exchange.WizardState) (exchangeDepositCheck, error) {
	check := exchangeDepositCheck{Message: "No original deposit has been submitted in this workflow."}
	if w.DepositOperation == "" {
		return check, nil
	}
	var op journal.Operation
	if err := c.childCallAt(epoch, "operation", struct {
		ID string `json:"id"`
	}{w.DepositOperation}, &op); err != nil {
		return check, err
	}
	if op.Plan.ID != w.DepositOperation || op.Plan.Account.ID != w.SourceAccount || op.Plan.Asset != w.SourceAsset {
		return check, errExchange
	}
	account, asset, _, err := c.exchangeAsset(w.SourceAccount, w.SourceAsset)
	if err != nil {
		return check, err
	}
	deposited, parseErr := amount.Format(op.Plan.Amount, asset.Decimals)
	expected, expectedErr := amount.Parse(w.DepositAmount, asset.Decimals)
	if parseErr != nil || expectedErr != nil || expected != op.Plan.Amount {
		return check, errExchange
	}
	check.Coin, check.DepositedAmount = asset.Symbol, w.DepositAmount
	check.ChainState = op.State
	if op.Signed == nil {
		check.Message = "The original deposit has not been signed or sent. Complete its original approval or clear the unsigned draft explicitly."
		return check, nil
	}
	result, err := c.paymentAt("reconcile", w.DepositOperation, epoch)
	if err != nil {
		return check, err
	}
	receipt, ok := result.(model.Receipt)
	if !ok {
		return check, errExchange
	}
	check.ChainState = receipt.State
	check.TransactionID = receipt.Identity
	if receipt.State != "CONFIRMED" {
		check.Message = "The original deposit is still awaiting verified on-chain delivery. Its original identity is retained; no new deposit is sent while checking it."
		if receipt.State == "FAILED" || receipt.State == "REPLACED" || receipt.State == "REORGED" {
			check.Message = "The original deposit's on-chain result is " + receipt.State + ". No Bybit credit has been verified; inspect the original payment before taking any further action."
		}
		return check, nil
	}
	check.ChainConfirmed = true
	if account.Family == "ton" {
		check.TransactionID = receipt.Block
	}
	check.Message = "The original deposit is confirmed on-chain. Bybit has not yet returned its matching credited deposit; an exchange balance alone does not identify this deposit."
	// The signer advances only its current original operation. It never saves
	// a stale copy of this navigation draft over concurrent foreground edits.
	if err := c.childCallAt(epoch, "exchangeWizardDepositConfirmed", struct {
		WizardID         string `json:"wizardId"`
		DepositOperation string `json:"depositOperation"`
	}{w.ID, w.DepositOperation}, nil); err != nil {
		return check, err
	}
	readFailure := func(stage string, err error) (exchangeDepositCheck, error) {
		check.Ready = false
		if check.ReadError == "" {
			check.ReadError = ipc.PublicErrorCode(exchangeReadFailure(stage, err))
		}
		reason := exchangeDepositCreditReason(check.ReadError)
		if reason == "" {
			reason = "Bybit credit or account balances could not be verified."
		}
		check.Message = "The original deposit is confirmed on-chain. " + reason + " (" + check.ReadError + ") Checking again does not send another deposit or authorize an exchange transfer."
		return check, nil
	}
	// Show both actual account balances after delivery, independently of how
	// soon Bybit indexes the exact deposit. Neither observation grants credit.
	check.Balances, err = exchangeBalances(ctx, client, asset.Symbol)
	if err != nil {
		check.Balances = nil
		check.ReadError = ipc.PublicErrorCode(exchangeReadFailure("BALANCE", err))
	}
	coin, err := exchangeDepositCoinInfo(ctx, client, asset.Symbol)
	if err != nil {
		return readFailure("COIN", err)
	}
	chain, err := exchange.MatchProviderRoute("bybit", account, asset, coin)
	if err != nil {
		return readFailure("COIN", err)
	}
	identities := []string{receipt.Identity}
	queries := []string{receipt.Identity}
	if account.Family == "ton" {
		// A TON signature identifies an external message, not a transaction.
		// Exchanges can index either verified leg of the same linked payment.
		// Never query/match the external-message identity as a deposit txID.
		identities, queries = nil, nil
		for _, tx := range []string{receipt.Block, receipt.SourceTransaction} {
			raw, decodeErr := hex.DecodeString(tx)
			if decodeErr != nil || len(raw) != 32 {
				continue
			}
			canonical := hex.EncodeToString(raw)
			duplicate := false
			for _, previous := range identities {
				duplicate = duplicate || previous == canonical
			}
			if !duplicate {
				identities = append(identities, canonical)
				queries = append(queries, canonical, base64.StdEncoding.EncodeToString(raw))
			}
		}
		if len(identities) == 0 {
			return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "TRANSACTION"})
		}
		check.TransactionID = identities[0]
	}
	var matched *bybit.DepositRecord
	for _, query := range queries {
		cursor := ""
		seen := map[string]bool{}
		for page := 0; page < 10; page++ {
			result, err := client.DepositRecords(ctx, asset.Symbol, query, cursor)
			if err != nil {
				return readFailure("DEPOSIT_CREDIT", err)
			}
			for _, record := range result.Rows {
				sameTx := record.TxID == query
				if account.Family == "ton" {
					sameTx = exchangeSameTONHash(record.TxID, query)
				}
				if !sameTx {
					continue
				}
				if record.Coin != asset.Symbol {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "COIN"})
				}
				if record.Chain != chain.Chain {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "CHAIN"})
				}
				if !exchangeSameAddress(account.Network, record.ToAddress, op.Plan.Recipient) {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "ADDRESS"})
				}
				if record.Tag != op.Plan.Memo {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "MEMO"})
				}
				if record.FromAddress != "" && !exchangeSameAddress(account.Network, record.FromAddress, account.Address) {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "SOURCE"})
				}
				if matched != nil {
					return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "DUPLICATE"})
				}
				copy := record
				matched = &copy
			}
			if result.NextPageCursor == "" {
				break
			}
			if seen[result.NextPageCursor] || page == 9 {
				return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "PAGINATION"})
			}
			seen[result.NextPageCursor] = true
			cursor = result.NextPageCursor
		}
		if matched != nil {
			// A complete exact identity match needs no alternative encoding or
			// leg query. An unsupported alternative must not hide valid credit.
			break
		}
	}
	if matched == nil {
		if check.ReadError != "" {
			return readFailure("BALANCE", bybit.ErrInvalidResponse)
		}
		return check, nil
	}
	check.ReportedAmount, check.DepositFee = matched.Amount, matched.DepositFee
	credited, creditErr := exchangeAtomic(matched.Amount)
	if creditErr != nil || credited.Sign() <= 0 || !exchangeAtLeast(deposited, matched.Amount) {
		return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "AMOUNT"})
	}
	budget, budgetErr := exchangeDepositTransferBudget(deposited, matched.Amount, matched.DepositFee, asset.Decimals)
	if budgetErr != nil {
		return readFailure("DEPOSIT_CREDIT", &exchangeDepositCreditError{Field: "FEE"})
	}
	if matched.Status != 3 || matched.DepositType != "0" || matched.TravelRuleStatus != 0 {
		check.Message = "Bybit reports this deposit pending, restricted or under review. Complete any required checks with Bybit; no transfer is authorized."
		return check, nil
	}
	if check.ReadError != "" {
		return readFailure("BALANCE", bybit.ErrInvalidResponse)
	}
	check.Ready = true
	check.TransferBudget = budget
	check.Message = "Bybit reports the exact original deposit credited. Verify the Funding available amount before the separately approved move to Unified."
	return check, nil
}
