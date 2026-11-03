package walletcore

import (
	"errors"
	"reflect"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestExchangeUnifiedMoveIsSeparateExactCustodyTransfer(t *testing.T) {
	c, api, _, path, _ := wizardCoordinator(t)
	before := loadWizard(t, c)
	api.fail["coin"] = &bybit.CoinMetadataError{Field: "IDENTITY"}
	api.balances["FUND:ETH"], api.balances["UNIFIED:ETH"] = "100", "0.123456789012345678"
	result := exchangeResultCall(t, c, "exchangeUnifiedMove", map[string]any{"amount": "0.123456789012345678"})
	if result.Kind != "transfer" || result.State != "CONFIRMED" || api.posts != 1 || api.transfer.Coin != "ETH" || api.transfer.Amount != "0.123456789012345678" || api.transfer.FromAccountType != "UNIFIED" || api.transfer.ToAccountType != "FUND" {
		t.Fatal("return-to-Funding changed the exact coin, amount or account direction", result, api.transfer)
	}
	if loadWizard(t, c) != before || independentExchangeOperationCount(t, path) != 1 {
		t.Fatal("independent return transfer changed historical wizard authority or lost its own durable operation")
	}
	op := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
	if op["id"] != result.ID || op["kind"] != "transfer" {
		t.Fatal("return transfer is not bound to its own original durable identity")
	}
}

func TestExchangeUnifiedMoveRejectsUnsafeOrUnownedFundsBeforePreparation(t *testing.T) {
	for _, scenario := range []string{"unfunded", "safe-restriction", "wrong-balance-account", "wrong-balance-coin", "invalid-balance", "read-failure", "readonly", "permission", "classic", "zero", "negative", "precision", "foreign", "direction", "wizard-authority"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			api.balances["FUND:ETH"], api.balances["UNIFIED:ETH"] = "1000", "0.1"
			body := map[string]any{"amount": "0.1"}
			switch scenario {
			case "unfunded":
				api.balances["UNIFIED:ETH"] = "0"
			case "safe-restriction", "wrong-balance-account", "wrong-balance-coin", "invalid-balance":
				api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
					b := custodyTestBalance(kind, coin, "1000")
					if kind == "UNIFIED" {
						switch scenario {
						case "safe-restriction":
							b.Balance.TransferSafeAmount = "0"
						case "wrong-balance-account":
							b.AccountType = "FUND"
						case "wrong-balance-coin":
							b.Balance.Coin = "USDT"
						case "invalid-balance":
							b.Balance.TransferBalance = "NaN"
						}
					}
					return b, nil
				}
			case "read-failure":
				api.fail["balance"] = errors.New("synthetic read failure")
			case "readonly":
				api.info.ReadOnly = 1
			case "permission":
				api.info.Permissions.Wallet = []string{"Withdraw"}
			case "classic":
				api.info.UTA = 0
			case "zero":
				body["amount"] = "0"
			case "negative":
				body["amount"] = "-1"
			case "precision":
				body["amount"] = "0.1000000000000000001"
			case "foreign":
				body["accountId"] = "not-owned"
			case "direction":
				body["fromAccountType"], body["toAccountType"] = "FUND", "CONTRACT"
			case "wizard-authority":
				body["wizardId"] = "old-operation"
			}
			if _, err := c.handle(exchangeMessage("exchangeUnifiedMove", body)); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("invalid return transfer reached economic preparation", err, api.posts)
			}
		})
	}
}

func custodyTestBalance(kind, coin, value string) bybit.Balance {
	var b bybit.Balance
	b.AccountType, b.AccountID, b.MemberID = kind, "1", "123"
	b.Balance.Coin = coin
	b.Balance.WalletBalance, b.Balance.TransferBalance, b.Balance.TransferSafeAmount = value, value, value
	return b
}

func TestExchangeFundingOnlyWithdrawalNeverQueriesOrMovesUnified(t *testing.T) {
	for _, scenario := range []string{"funded", "unfunded", "safe-restriction", "wrong-account", "wrong-coin", "invalid-balance", "balance-failure"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path, _ := wizardCoordinator(t)
			before := loadWizard(t, c)
			api.fail["instrument"], api.fail["ticker"], api.fail["fee"] = errors.New("unrelated market read"), errors.New("unrelated market read"), errors.New("unrelated market read")
			// Withdrawal requires Withdraw, but never AccountTransfer.
			api.info.Permissions.Wallet = []string{"Withdraw"}
			queried := []string{}
			api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
				queried = append(queried, kind+":"+coin)
				if kind != "FUND" {
					t.Fatal("Funding-only withdrawal queried another custody source", kind)
				}
				b := custodyTestBalance(kind, coin, "0.1")
				switch scenario {
				case "unfunded":
					b = custodyTestBalance(kind, coin, "0")
				case "safe-restriction":
					b.Balance.TransferSafeAmount = "0"
				case "wrong-account":
					b.AccountType = "UNIFIED"
				case "wrong-coin":
					b.Balance.Coin = "USDT"
				case "invalid-balance":
					b.Balance.TransferBalance = "NaN"
				case "balance-failure":
					return bybit.Balance{}, errors.New("synthetic private balance error")
				}
				return b, nil
			}
			out, err := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1", "existingFunds": true, "fundingOnly": true}))
			if !reflect.DeepEqual(queried, []string{"FUND:ETH"}) || loadWizard(t, c) != before || api.transfer.TransferID != "" {
				t.Fatal("withdrawal changed its account source, inferred transfer authority or changed historical wizard", queried, api.transfer)
			}
			if scenario == "funded" {
				if err != nil || out.(exchangeResult).Kind != "withdraw" || out.(exchangeResult).State != "EXCHANGE_COMPLETED" || api.posts != 1 || api.withdrawal.AccountType != "FUND" || api.withdrawal.Coin != "ETH" || api.withdrawal.Amount != "0.1" || api.withdrawal.Address != "0x1111111111111111111111111111111111111111" || independentExchangeOperationCount(t, path) != 1 {
					t.Fatal("Funding-only exact owned withdrawal failed", out, err, api.withdrawal)
				}
			} else if err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("Funding-only withdrawal prepared or submitted against invalid/unavailable safe balance", out, err)
			}
			if scenario == "unfunded" || scenario == "safe-restriction" {
				if publicRequestErrorCode(err) != "EXCHANGE_FUNDING_INSUFFICIENT" {
					t.Fatal("unavailable Funding source did not preserve its explicit cause", err)
				}
			}
		})
	}
}

func TestExchangeFundingOnlyModeRejectsAmbiguityAndAuthorityMixing(t *testing.T) {
	for _, flag := range []any{nil, "true", "false", 0, 1, []any{}, map[string]any{}} {
		c, api, _, path := fixtureExchangeCoordinator(t)
		if _, err := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1", "existingFunds": true, "fundingOnly": flag})); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
			t.Fatal("nonboolean funding policy reached preparation", flag, err)
		}
	}
	for _, body := range []map[string]any{
		{"fundingOnly": true},
		{"fundingOnly": true, "existingFunds": false},
		{"fundingOnly": true, "existingFunds": true, "wizardId": "older-workflow"},
		{"fundingOnly": true, "existingFunds": true, "FundingOnly": false},
		{"FundingOnly": true, "existingFunds": true},
		{"fundingOnly": true, "existingFunds": true, "accountId": "foreign"},
		{"fundingOnly": true, "existingFunds": true, "expectedAddress": "caller-selected"},
	} {
		c, api, _, path := fixtureExchangeCoordinator(t)
		body["amount"] = "0.1"
		if _, err := c.handle(exchangeMessage("exchangeWithdraw", body)); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
			t.Fatal("ambiguous funding policy or foreign authority reached preparation", body, err)
		}
	}
	c, api, _, path := fixtureExchangeCoordinator(t)
	if _, err := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1", "fundingOnly": true})); err == nil || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
		t.Fatal("withdrawal mode was accepted for on-chain deposit", err)
	}
}
