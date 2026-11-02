package walletcore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestExchangeWizardConfirmedOriginalAdvancesBeforeExchangeCredit(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "record-not-yet-visible"
		if pending {
			name = "credit-pending"
		}
		t.Run(name, func(t *testing.T) {
			c, api, adapter, path, w := wizardCoordinator(t)
			w = depositWizard(t, c, api, w)
			api.balances["FUND:ETH"], api.balances["UNIFIED:ETH"] = "12.5", "2.25"
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				if !pending {
					return bybit.DepositPage{}, nil
				}
				return bybit.DepositPage{Rows: []bybit.DepositRecord{{ID: "pending-original", Coin: "ETH", Chain: "ETH", ToAddress: api.deposit.Address, TxID: tx, Amount: w.DepositAmount, DepositFee: "0", Status: 1, DepositType: "0"}}}, nil
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal("confirmed deposit navigation was blocked by pending exchange credit", err)
			}
			check := out.(exchangeDepositCheck)
			if !check.ChainConfirmed || check.ChainState != "CONFIRMED" || check.Ready || check.Coin != "ETH" || check.DepositedAmount != "0.1" || check.TransactionID == "" {
				t.Fatalf("chain confirmation and exchange credit were not separated: %+v", check)
			}
			if len(check.Balances) != 2 || exchangeAvailable(check.Balances, "FUND", "ETH") != "12.5" || exchangeAvailable(check.Balances, "UNIFIED", "ETH") != "2.25" {
				t.Fatalf("current Funding/Unified balances absent while waiting for credit: %+v", check.Balances)
			}
			resumed := loadWizard(t, c)
			if resumed.Step != 2 || resumed.DepositOperation != w.DepositOperation || resumed.DepositAmount != w.DepositAmount {
				t.Fatalf("confirmed original could not resume at step two: %+v", resumed)
			}
			// Existing exchange funds cannot be mistaken for this deposit's credit.
			if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": w.DepositAmount})); err == nil {
				t.Fatal("exchange balance authorized a transfer before exact credit was verified")
			}
			if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 || len(readPublicState(t, path)["ops"].([]any)) != 1 {
				t.Fatal("confirmation tracking created another payment or exchange debit")
			}
			// Exchange credit can arrive later while the user remains on step two.
			api.depositPage = nil
			out, err = c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil || !out.(exchangeDepositCheck).Ready || loadWizard(t, c).Step != 2 || api.posts != 0 || adapter.submits != 1 {
				t.Fatal("later exact credit failed to refresh or replayed the payment", err)
			}
		})
	}
}

func TestExchangeWizardConfirmedOriginalReadErrorsKeepStepTwoWithoutAuthorization(t *testing.T) {
	for _, failure := range []string{"coin", "balance", "deposit-record"} {
		t.Run(failure, func(t *testing.T) {
			c, api, adapter, _, w := wizardCoordinator(t)
			w = depositWizard(t, c, api, w)
			if failure == "deposit-record" {
				api.depositPage = func(string, string) (bybit.DepositPage, error) {
					return bybit.DepositPage{}, context.DeadlineExceeded
				}
			} else {
				api.fail[failure] = fmt.Errorf("PRIVATE_PROVIDER_ERROR_CANARY: %w", &bybit.APIError{Code: 10005})
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal("exchange read outage hid already confirmed original payment", err)
			}
			check := out.(exchangeDepositCheck)
			if !check.ChainConfirmed || check.Ready || check.ReadError == "" || check.Message == "" {
				t.Fatalf("partial confirmation status absent or outage authorized credit: %+v", check)
			}
			if strings.Contains(check.ReadError+check.Message, "PRIVATE_PROVIDER_ERROR_CANARY") {
				t.Fatal("raw exchange read failure leaked into public partial status")
			}
			if loadWizard(t, c).Step != 2 {
				t.Fatal("exchange read outage prevented persisted navigation after chain confirmation")
			}
			if failure == "balance" && len(check.Balances) != 0 {
				t.Fatal("unavailable balances were presented as current values")
			}
			if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": w.DepositAmount})); err == nil {
				t.Fatal("exchange read failure was ignored for an actual debit")
			}
			if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 {
				t.Fatal("read failure created another signed payment or exchange transfer")
			}
		})
	}
}

func TestExchangeWizardSubmittedOriginalDoesNotAdvanceOrCreateAnotherDeposit(t *testing.T) {
	c, api, adapter, path, w := wizardCoordinator(t)
	adapter.receiptState = "SUBMITTED"
	r := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": w.DepositAmount, "wizardId": w.ID, "expectedAddress": api.deposit.Address, "expectedMemo": api.deposit.Tag})
	if r.State != "SUBMITTED" {
		t.Fatal("fixture did not submit a pending original", r)
	}
	out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
	if err != nil {
		t.Fatal(err)
	}
	check := out.(exchangeDepositCheck)
	if check.ChainConfirmed || check.Ready || check.ChainState != "SUBMITTED" || len(check.Balances) != 0 {
		t.Fatalf("submission was mistaken for confirmed delivery or credited exchange funds: %+v", check)
	}
	resumed := loadWizard(t, c)
	if resumed.Step != 1 || resumed.DepositOperation != r.ID {
		t.Fatal("unconfirmed original advanced or lost its persisted identity", resumed)
	}
	if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 || len(readPublicState(t, path)["ops"].([]any)) != 1 {
		t.Fatal("pending original was replaced or resent")
	}
}
