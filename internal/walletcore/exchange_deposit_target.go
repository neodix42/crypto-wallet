package walletcore

import (
	"context"
	"errors"
	"strings"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

type exchangeDepositTarget struct {
	info     bybit.APIKeyInformation
	account  model.Account
	asset    model.Asset
	adapter  model.Adapter
	chain    bybit.Chain
	deposit  bybit.Deposit
	provider string
}

type exchangeDepositExpectation struct{ WizardID, Address, Memo string }

type exchangeDepositPreview struct {
	Provider, AccountID, AssetID, Network, Coin  string
	Address, Memo, Chain, MinDeposit, ObservedAt string
	Unified, CanTransfer, CanTrade, CanWithdraw  bool
}

func (c *core) exchangeDepositTargetAt(epoch uint64, client exchangeAPI, accountID, assetID string) (exchangeDepositTarget, error) {
	var target exchangeDepositTarget
	account, asset, adapter, err := c.exchangeAsset(accountID, assetID)
	if err != nil {
		return target, err
	}
	service, known := exchange.ProviderForEndpoint(client.Endpoint())
	if !known {
		return target, errExchange
	}
	ctx, cancel := c.networkContextAt(90*time.Second, epoch)
	defer cancel()
	info, err := client.APIKeyInfo(ctx)
	if err != nil {
		return target, exchangeReadFailure("KEY", err)
	}
	if service.ID == "bybit" && !info.IsMaster {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_MASTER_REQUIRED")
	}
	coin, err := exchangeDepositCoinInfo(ctx, client, asset.Symbol)
	if err != nil {
		return target, exchangeReadFailure("COIN", err)
	}
	chain, err := exchange.MatchProviderRoute(service.ID, account, asset, coin)
	if err != nil {
		var metadata *bybit.CoinMetadataError
		if errors.As(err, &metadata) {
			return target, exchangeReadFailure("COIN", err)
		}
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_ROUTE_UNAVAILABLE")
	}
	if chain.ChainDeposit != "1" {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_SUSPENDED")
	}
	deposit, err := client.DepositAddress(ctx, asset.Symbol, chain.Chain)
	if err != nil {
		return target, exchangeReadFailure("DEPOSIT_ADDRESS", err)
	}
	contractMatches := deposit.ContractAddress == "" && asset.Contract == ""
	if asset.Contract != "" {
		contractMatches = exchange.ContractMatches(account.Network, deposit.ContractAddress, asset.Contract)
		if !contractMatches && service.ID == "bybit" && len(chain.ContractAddress) >= 6 && len(deposit.ContractAddress) == 6 {
			suffix := chain.ContractAddress[len(chain.ContractAddress)-6:]
			contractMatches = deposit.ContractAddress == suffix || account.Family == "evm" && strings.EqualFold(deposit.ContractAddress, suffix)
		}
	}
	if !contractMatches {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_CONTRACT_MISMATCH")
	}
	// Coin-info and address endpoints use different display aliases. Require
	// both to independently match the reviewed network, not identical prose.
	if exchange.ValidateProviderRoute(service.ID, account, asset, deposit.Chain, deposit.ChainType, chain.ContractAddress) != nil || deposit.Chain != chain.Chain {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_NETWORK_MISMATCH")
	}
	if adapter.ValidateAddress(account.Network, deposit.Address) != nil {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_ADDRESS_INVALID")
	}
	if deposit.Tag != "" && account.Family != "ton" || account.Family == "ton" && deposit.Tag == "" && !deposit.MemoPresent {
		return target, ipc.NewRequestError("EXCHANGE_DEPOSIT_MEMO_INVALID")
	}
	return exchangeDepositTarget{info: info, account: account, asset: asset, adapter: adapter, chain: chain, deposit: deposit, provider: service.ID}, nil
}

func exchangeDepositCoinInfo(ctx context.Context, client exchangeAPI, coin string) (bybit.Coin, error) {
	if provider, ok := exchange.ProviderForEndpoint(client.Endpoint()); ok && provider.ID == "bybit" {
		if scoped, ok := client.(interface {
			CoinInfoForDeposit(context.Context, string) (bybit.Coin, error)
		}); ok {
			return scoped.CoinInfoForDeposit(ctx, coin)
		}
	}
	return client.CoinInfo(ctx, coin)
}

func (c *core) exchangeDepositPreviewAt(epoch uint64, client exchangeAPI, accountID, assetID string) (exchangeDepositPreview, error) {
	target, err := c.exchangeDepositTargetAt(epoch, client, accountID, assetID)
	if err != nil {
		return exchangeDepositPreview{}, err
	}
	return exchangeDepositPreview{Provider: target.provider, AccountID: accountID, AssetID: assetID, Network: target.account.Network, Coin: target.asset.Symbol, Address: target.deposit.Address, Memo: target.deposit.Tag, Chain: target.chain.Chain, MinDeposit: target.chain.DepositMin, ObservedAt: time.Now().UTC().Format(time.RFC3339), Unified: target.info.UTA == 1, CanTransfer: exchangePermission(target.info, "AccountTransfer"), CanTrade: exchangePermission(target.info, "SpotTrade"), CanWithdraw: exchangeWithdrawalAuthority(target.provider, target.info) && exchangePermission(target.info, "Withdraw")}, nil
}
