package walletcore

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

func TestExchangeDepositPreviewAndPaymentAcceptOnlyReviewedNetworkAliases(t *testing.T) {
	for _, label := range []string{"Ethereum (ERC20)", "Ethereum(ERC20)", "ERC20", "ETH"} {
		t.Run(label, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			api.deposit.ChainType = label
			out, err := c.handle(exchangeMessage("exchangeDepositPreview", nil))
			if err != nil {
				t.Fatal("reviewed same-network alias rejected", err)
			}
			preview := out.(exchangeDepositPreview)
			if preview.Address != api.deposit.Address || preview.Network != "ethereum-mainnet" || preview.Coin != "ETH" || preview.Chain != "ETH" || adapter.prepares != 0 || api.posts != 0 || len(readPublicState(t, path)["ops"].([]any)) != 0 {
				t.Fatal("preview lost its binding or authorized a payment")
			}
			result := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": "0.1"})
			if result.State != "CONFIRMED" || adapter.lastTransfer.Recipient != preview.Address || adapter.submits != 1 {
				t.Fatal("fresh payment did not use verified destination")
			}
		})
	}
}

func TestExchangeSelectedInvalidMetadataHasFixedFieldReasonAndNoPreparation(t *testing.T) {
	c, api, adapter, _ := fixtureExchangeCoordinator(t)
	api.coinOverride = &bybit.Coin{Coin: "ETH", Chains: []bybit.Chain{api.chain}, InvalidChains: map[string]string{"ETH": "PRECISION"}}
	_, err := c.handle(exchangeMessage("exchangeDepositPreview", nil))
	if publicRequestErrorCode(err) != "EXCHANGE_READ_COIN__METADATA_PRECISION" || adapter.prepares != 0 || api.posts != 0 {
		t.Fatal("selected invalid route lost reason or acquired authority", err)
	}
	for _, field := range []string{"IDENTITY", "DUPLICATE_CHAIN", "FLAGS", "DEPOSIT_LIMIT", "WITHDRAWAL_LIMIT", "PRECISION", "CONFIRMATIONS"} {
		code := publicRequestErrorCode(exchangeReadFailure("COIN", &bybit.CoinMetadataError{Field: field}))
		if code != "EXCHANGE_READ_COIN__METADATA_"+field {
			t.Fatal("fixed metadata category lost", code)
		}
	}
	code := publicRequestErrorCode(exchangeReadFailure("COIN", &bybit.CoinMetadataError{Field: "PRIVATE_CANARY"}))
	raw, _ := json.Marshal(code)
	if strings.Contains(string(raw), "PRIVATE_CANARY") {
		t.Fatal("unreviewed provider metadata leaked")
	}
}

func TestExchangeHyperEVMAddressDisplayAliasRequiresOriginalVerifiedChain(t *testing.T) {
	for _, chain := range []string{"HYPEREVM", "HYPERCORE"} {
		c, api, adapter, path := fixtureExchangeCoordinator(t)
		state := readPublicState(t, path)
		state["accounts"] = []model.Account{{ID: "exchange-account", Network: "hyperevm-mainnet", Family: "evm", Address: "0x1111111111111111111111111111111111111111"}}
		writePublicState(t, path, state)
		adapter.networks = []model.Network{{ID: "hyperevm-mainnet", Family: "evm", Assets: []model.Asset{{ID: "hyperevm-mainnet:native", Symbol: "HYPE", Decimals: 18}}}}
		api.chain.Chain, api.chain.ChainType = "HYPEREVM", "HyperEVM"
		api.deposit.Chain, api.deposit.ChainType = chain, "Hyperliquid"
		out, err := c.handle(exchangeMessage("exchangeDepositPreview", map[string]any{"assetId": "hyperevm-mainnet:native"}))
		if chain == "HYPEREVM" {
			if err != nil || out.(exchangeDepositPreview).Coin != "HYPE" {
				t.Fatal("verified HyperEVM alias rejected", err)
			}
		} else if publicRequestErrorCode(err) != "EXCHANGE_DEPOSIT_NETWORK_MISMATCH" {
			t.Fatal("HyperCore crossed network", err)
		}
		if adapter.prepares != 0 || adapter.submits != 0 || api.posts != 0 {
			t.Fatal("read-only network preview moved funds")
		}
	}
}

func TestExchangeTONExplicitNoMemoDestinationIsDifferentFromMissingMemo(t *testing.T) {
	for _, present := range []bool{false, true} {
		c, api, adapter, path := fixtureExchangeCoordinator(t)
		state := readPublicState(t, path)
		state["accounts"] = []model.Account{{ID: "exchange-account", Network: "ton-mainnet", Family: "ton", Address: "EQpublicSyntheticOwnedTON"}}
		writePublicState(t, path, state)
		adapter.networks = []model.Network{{ID: "ton-mainnet", Family: "ton", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "GRAM", Decimals: 9}}}}
		api.chain.Chain, api.chain.ChainType = "TON", "TON"
		api.deposit = bybit.Deposit{Chain: "TON", ChainType: "Toncoin", Address: "EQpublicSyntheticUniqueBybitTON", MemoPresent: present}
		out, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"assetId": "ton-mainnet:TON", "amount": "1"}))
		if present {
			if err != nil || out.(exchangeResult).State != "CONFIRMED" || adapter.lastTransfer.Memo != "" || adapter.submits != 1 {
				t.Fatal("explicit unique TON address rejected", err)
			}
		} else if publicRequestErrorCode(err) != "EXCHANGE_DEPOSIT_MEMO_INVALID" || adapter.prepares != 0 || adapter.submits != 0 {
			t.Fatal("missing memo data authorized a payment", err)
		}
	}
}
