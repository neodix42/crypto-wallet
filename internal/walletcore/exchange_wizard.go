package walletcore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
	Ready          bool              `json:"ready"`
	ChainState     string            `json:"chainState"`
	Message        string            `json:"message"`
	ReportedAmount string            `json:"reportedAmount,omitempty"`
	DepositFee     string            `json:"depositFee,omitempty"`
	Balances       []exchangeBalance `json:"balances,omitempty"`
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
	case "exchangeFundingBalances", "exchangeSwapOptions":
		var in struct {
			AccountID strictjson.Text `json:"accountId"`
			AssetID   strictjson.Text `json:"assetId"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, errExchange
		}
		account, asset, _, err := c.exchangeAsset(string(in.AccountID), string(in.AssetID))
		if err != nil {
			return nil, err
		}
		if message.Method == "exchangeSwapOptions" {
			return c.exchangeSwapOptionsAt(ctx, client, account, asset)
		}
		balances, err := exchangeBalances(ctx, client, asset.Symbol)
		if err != nil {
			return nil, exchangeReadFailure("BALANCE", err)
		}
		return struct {
			Coin     string            `json:"coin"`
			Balances []exchangeBalance `json:"balances"`
		}{asset.Symbol, balances}, nil
	case "exchangeWithdrawOptions":
		var in struct {
			Coin strictjson.Text `json:"coin"`
		}
		if strictjson.Decode(message.Body, &in) != nil {
			return nil, errExchange
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
		if w.Step != 2 || w.TransferOperation != "" || !check.Ready {
			return nil, ipc.NewRequestError("EXCHANGE_DEPOSIT_PENDING")
		}
		_, asset, _, err := c.exchangeAsset(w.SourceAccount, w.SourceAsset)
		if err != nil {
			return nil, err
		}
		atomic, e := amount.Parse(string(in.Amount), asset.Decimals)
		if e != nil || atomic == "0" || !exchangeAtLeast(w.DepositAmount, string(in.Amount)) {
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
		b, e := base64.StdEncoding.DecodeString(s)
		if e != nil || len(b) != 32 {
			return nil
		}
		return b
	}
	x, y := decode(a), decode(b)
	return len(x) == 32 && len(y) == 32 && string(x) == string(y)
}

func (c *core) exchangeDepositCheckAt(epoch uint64, ctx context.Context, client exchangeAPI, w exchange.WizardState) (exchangeDepositCheck, error) {
	check := exchangeDepositCheck{Message: "The original deposit has not yet been verified as credited by Bybit."}
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
	check.ChainState = op.State
	if op.Signed == nil {
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
	if receipt.State != "CONFIRMED" {
		return check, nil
	}
	account, asset, _, err := c.exchangeAsset(w.SourceAccount, w.SourceAsset)
	if err != nil {
		return check, err
	}
	coin, err := client.CoinInfo(ctx, asset.Symbol)
	if err != nil {
		return check, exchangeReadFailure("COIN", err)
	}
	chain, err := exchange.MatchProviderRoute("bybit", account, asset, coin)
	if err != nil {
		return check, ipc.NewRequestError("EXCHANGE_DEPOSIT_ROUTE_UNAVAILABLE")
	}
	tx := receipt.Identity
	if account.Family == "ton" {
		tx = receipt.Block
		if len(tx) != 64 {
			return check, nil
		}
	}
	queries := []string{tx}
	if account.Family == "ton" {
		if raw, e := hex.DecodeString(tx); e == nil && len(raw) == 32 {
			queries = append(queries, base64.StdEncoding.EncodeToString(raw))
		}
	}
	var matched *bybit.DepositRecord
	for _, query := range queries {
		cursor := ""
		seen := map[string]bool{}
		for page := 0; page < 10; page++ {
			result, err := client.DepositRecords(ctx, asset.Symbol, query, cursor)
			if err != nil {
				return check, exchangeReadFailure("DEPOSIT_CREDIT", err)
			}
			for _, record := range result.Rows {
				sameTx := record.TxID == tx
				if account.Family == "ton" {
					sameTx = exchangeSameTONHash(record.TxID, tx)
				}
				if !sameTx {
					continue
				}
				if record.Coin != asset.Symbol || record.Chain != chain.Chain || !exchangeSameAddress(account.Network, record.ToAddress, op.Plan.Recipient) || record.Tag != op.Plan.Memo {
					return check, exchangeReadFailure("DEPOSIT_CREDIT", bybit.ErrInvalidResponse)
				}
				if matched != nil {
					return check, exchangeReadFailure("DEPOSIT_CREDIT", bybit.ErrInvalidResponse)
				}
				copy := record
				matched = &copy
			}
			if result.NextPageCursor == "" {
				break
			}
			if seen[result.NextPageCursor] || page == 9 {
				return check, exchangeReadFailure("DEPOSIT_CREDIT", bybit.ErrInvalidResponse)
			}
			seen[result.NextPageCursor] = true
			cursor = result.NextPageCursor
		}
		if matched != nil {
			break
		}
	}
	if matched == nil {
		return check, nil
	}
	check.ReportedAmount, check.DepositFee = matched.Amount, matched.DepositFee
	deposited, parseErr := amount.Format(op.Plan.Amount, asset.Decimals)
	credited, creditErr := exchangeAtomic(matched.Amount)
	if parseErr != nil || creditErr != nil || credited.Sign() <= 0 || !exchangeAtLeast(deposited, matched.Amount) {
		return check, exchangeReadFailure("DEPOSIT_CREDIT", bybit.ErrInvalidResponse)
	}
	if matched.Status != 3 || matched.DepositType != "0" || matched.TravelRuleStatus != 0 {
		check.Message = "Bybit reports this deposit pending, restricted or under review. Complete any required checks with Bybit; no transfer is authorized."
		return check, nil
	}
	check.Balances, err = exchangeBalances(ctx, client, asset.Symbol)
	if err != nil {
		return check, exchangeReadFailure("BALANCE", err)
	}
	check.Ready = true
	check.Message = "Bybit reports the exact original deposit credited. Verify the Funding available amount before the separately approved move to Unified."
	return check, nil
}
