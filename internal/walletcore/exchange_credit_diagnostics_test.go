package walletcore

import (
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestExchangeWizardDepositCreditMismatchHasExactFixedReasonAndNoDebit(t *testing.T) {
	for _, field := range []string{"TRANSACTION", "COIN", "CHAIN", "ADDRESS", "SOURCE", "MEMO", "DUPLICATE", "PAGINATION", "AMOUNT", "FEE"} {
		t.Run(field, func(t *testing.T) {
			c, api, adapter, w := tonCreditCoordinator(t)
			if field == "TRANSACTION" {
				adapter.receipt.Block, adapter.receipt.SourceTransaction = "", ""
			}
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				r := bybit.DepositRecord{ID: "exact-original", Coin: "GRAM", Chain: "TON", TxID: tx, Amount: "6", Status: 3, DepositType: "0", ToAddress: api.deposit.Address, FromAddress: "UQAomnBshnaKi0Uu645Qg-hXScddgZwXa2U__ba7kf8UvZ05"}
				switch field {
				case "COIN":
					r.Coin = "USDT"
				case "CHAIN":
					r.Chain = "ETH"
				case "ADDRESS":
					r.ToAddress = r.FromAddress
				case "SOURCE":
					r.FromAddress = r.ToAddress
				case "MEMO":
					r.Tag = "not-the-signed-memo"
				case "AMOUNT":
					r.Amount = "6.000000001"
				case "FEE":
					r.DepositFee = "6.000000001"
				}
				page := bybit.DepositPage{Rows: []bybit.DepositRecord{r}}
				if field == "DUPLICATE" {
					r.ID = "different-record"
					page.Rows = append(page.Rows, r)
				}
				if field == "PAGINATION" {
					page.Rows = nil
					page.NextPageCursor = "repeated-cursor"
				}
				return page, nil
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal(err)
			}
			check := out.(exchangeDepositCheck)
			want := "EXCHANGE_READ_DEPOSIT_CREDIT__CREDIT_" + field
			if !check.ChainConfirmed || check.Ready || check.ReadError != want || exchangeDepositCreditReason(want) == "" || !strings.Contains(check.Message, exchangeDepositCreditReason(want)) {
				t.Fatalf("exact blocker lost for %s: %+v", field, check)
			}
			if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": "1"})); publicRequestErrorCode(err) != want {
				t.Fatal("Funding debit ignored the verification failure", err)
			}
			if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 || loadWizard(t, c).Step != 2 {
				t.Fatal("diagnostic check replaced the original deposit, moved funds or lost confirmed navigation")
			}
		})
	}
}

func TestExchangeWizardDepositParserFailureHasExactSanitizedPublicReason(t *testing.T) {
	for _, field := range []string{"RESPONSE", "PAGE", "CURSOR", "ID", "COIN", "TRANSACTION", "CHAIN", "AMOUNT", "ADDRESS", "SOURCE", "MEMO", "STATUS", "FEE", "TIME", "CONFIRMATIONS", "TYPE", "REVIEW", "PRIVATE_PROVIDER_RESPONSE_CANARY"} {
		t.Run(field, func(t *testing.T) {
			c, api, adapter, w := tonCreditCoordinator(t)
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				return bybit.DepositPage{}, fmt.Errorf("PRIVATE_API_KEY_CANARY: %w", &bybit.DepositRecordError{Field: field})
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal(err)
			}
			check := out.(exchangeDepositCheck)
			want := "EXCHANGE_READ_DEPOSIT_CREDIT__DEPOSIT_RECORD_" + field
			if field == "PRIVATE_PROVIDER_RESPONSE_CANARY" {
				want = "EXCHANGE_READ_DEPOSIT_CREDIT__PROVIDER_INVALID_RESPONSE"
			}
			if !check.ChainConfirmed || check.Ready || check.ReadError != want || strings.Contains(check.Message+check.ReadError, "CANARY") {
				t.Fatalf("private response or wrong classification escaped: %+v", check)
			}
			if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": "1"})); publicRequestErrorCode(err) != want {
				t.Fatal("malformed credit response authorized a debit", err)
			}
			if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 {
				t.Fatal("parser diagnostics created a payment or exchange write")
			}
		})
	}
}

func TestExchangeDepositCreditDiagnosticCategoriesCannotCrossStagesOrCarryPrivateText(t *testing.T) {
	for _, cause := range []error{&bybit.DepositRecordError{Field: "SOURCE"}, &exchangeDepositCreditError{Field: "SOURCE"}} {
		code := publicRequestErrorCode(exchangeReadFailure("BALANCE", cause))
		if code != "EXCHANGE_READ_BALANCE__PROVIDER_INVALID_RESPONSE" {
			t.Fatal("deposit-only category crossed a read stage", code)
		}
	}
	for _, cause := range []error{&bybit.DepositRecordError{Field: "PRIVATE_BODY_CANARY"}, &exchangeDepositCreditError{Field: "PRIVATE_BODY_CANARY"}} {
		code := publicRequestErrorCode(exchangeReadFailure("DEPOSIT_CREDIT", cause))
		if code != "EXCHANGE_READ_DEPOSIT_CREDIT__PROVIDER_INVALID_RESPONSE" || exchangeDepositCreditReason(code) != "" {
			t.Fatal("unreviewed diagnostic entered the public message", code)
		}
	}
}
