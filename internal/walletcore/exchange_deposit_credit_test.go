package walletcore

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
)

// Coordinator fixtures have no keys or external network. The TON adapter has
// a separate real public-BOC regression for these exact source/recipient hashes.
type wizardTONReceiptAdapter struct {
	*publicAdapter
	receipt model.Receipt
}

func (a *wizardTONReceiptAdapter) Reconcile(_ context.Context, _ model.Plan, signed model.Signed) (model.Receipt, error) {
	a.reconciles++
	r := a.receipt
	r.Identity = signed.Identity
	return r, nil
}

func tonCreditCoordinator(t *testing.T) (*core, *wizardExchange, *wizardTONReceiptAdapter, exchange.WizardState) {
	t.Helper()
	c, api, adapter, path, w := wizardCoordinator(t)
	w.SourceAsset, w.DepositAmount = "ton-mainnet:TON", "6"
	state := readPublicState(t, path)
	state["wizard"] = w
	state["accounts"] = []model.Account{{ID: w.SourceAccount, Family: "ton", Network: "ton-mainnet", Profile: "public-protocol-fixture", Address: "UQAomnBshnaKi0Uu645Qg-hXScddgZwXa2U__ba7kf8UvZ05"}}
	writePublicState(t, path, state)
	adapter.networks = []model.Network{{ID: "ton-mainnet", Family: "ton", Assets: []model.Asset{{ID: w.SourceAsset, Symbol: "GRAM", Decimals: 9}}}}
	checked := &wizardTONReceiptAdapter{publicAdapter: adapter, receipt: model.Receipt{State: "CONFIRMED", Block: "5ab9780b09037d425c9d8a4b43d17ae39582f7768ddc05c2fbc3efbdb610b974", SourceTransaction: "9aabe0765796346d0ce447f63a4ec565ab41daec90ac77316e4c21bb891722d1"}}
	c.adapters = []model.Adapter{checked}
	api.chain.Chain, api.chain.ChainType, api.chain.MinAccuracy = "TON", "TON", "9"
	api.deposit.Chain, api.deposit.ChainType, api.deposit.MemoPresent = "TON", "TON", true
	api.deposit.Address = "UQDoJhPJopCXVzyqm_wt0ELXSnP_epdc06lQuGwiE_MtHn1L"
	r := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"assetId": w.SourceAsset, "amount": w.DepositAmount, "wizardId": w.ID, "expectedAddress": api.deposit.Address, "expectedMemo": ""})
	w = loadWizard(t, c)
	if w.DepositOperation != r.ID || checked.submits != 1 {
		t.Fatal("original TON deposit identity was lost")
	}
	return c, api, checked, w
}

func TestExchangeWizardTONCreditMatchesVerifiedSourceOrRecipientWithoutMessageHash(t *testing.T) {
	for _, scenario := range []string{"recipient-hex", "source-hex", "source-base64", "message-is-not-transaction", "wrong-source", "wrong-memo", "no-verified-transaction", "multiple-records"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, adapter, w := tonCreditCoordinator(t)
			target := adapter.receipt.Block
			if scenario == "source-hex" || scenario == "source-base64" {
				target = adapter.receipt.SourceTransaction
			}
			if scenario == "source-base64" {
				raw, _ := hex.DecodeString(target)
				target = base64.StdEncoding.EncodeToString(raw)
			}
			if scenario == "no-verified-transaction" {
				adapter.receipt.Block, adapter.receipt.SourceTransaction = "", ""
			}
			queries := []string{}
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				queries = append(queries, tx)
				if scenario == "message-is-not-transaction" {
					if tx == "synthetic-identity" {
						t.Fatal("signed external message used as a Bybit transaction query")
					}
					return bybit.DepositPage{}, nil
				}
				if tx != target {
					return bybit.DepositPage{}, nil
				}
				r := bybit.DepositRecord{ID: "exact-ton-credit", Coin: "GRAM", Chain: "TON", Amount: "6", TxID: tx, Status: 3, ToAddress: api.deposit.Address, FromAddress: "UQAomnBshnaKi0Uu645Qg-hXScddgZwXa2U__ba7kf8UvZ05", Tag: "", DepositType: "0"}
				if scenario == "wrong-source" {
					r.FromAddress = api.deposit.Address
				}
				if scenario == "wrong-memo" {
					r.Tag = "not-the-signed-memo"
				}
				page := bybit.DepositPage{Rows: []bybit.DepositRecord{r}}
				if scenario == "multiple-records" {
					r.ID = "another-credit"
					page.Rows = append(page.Rows, r)
				}
				return page, nil
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal(err)
			}
			check := out.(exchangeDepositCheck)
			wantReady := scenario == "recipient-hex" || scenario == "source-hex" || scenario == "source-base64"
			if check.Ready != wantReady || !check.ChainConfirmed || check.Coin != "GRAM" || check.DepositedAmount != "6" || loadWizard(t, c).Step != 2 {
				t.Fatalf("incorrect exact deposit reconciliation: %+v", check)
			}
			if scenario == "recipient-hex" && len(queries) != 1 {
				t.Fatal("complete exact credit triggered redundant alternate hash queries", queries)
			}
			if api.posts != 0 || adapter.prepares != 1 || adapter.submits != 1 {
				t.Fatal("a read-only credit check created a replacement payment or transfer")
			}
		})
	}
}

func TestExchangeWizardFundingTransferReservesReportedFeesWithoutSpendingExistingFunds(t *testing.T) {
	for _, test := range []struct{ name, reported, fee, budget, excessive string }{
		{"gross-report", "0.1", "0.01", "0.09", "0.1"},
		{"possibly-net-report", "0.09", "0.01", "0.08", "0.09"},
		{"unreported-fee", "0.09", "", "0.09", "0.1"},
		{"explicit-zero-fee", "0.09", "0", "0.09", "0.1"},
		{"fee-consumes-deposit", "0.1", "0.1", "0", "0.000000000000000001"},
		{"fee-exceeds-deposit", "0.1", "0.11", "", "0.000000000000000001"},
		{"negative-fee", "0.1", "-0.01", "", "0.01"},
		{"nondecimal-fee", "0.1", "NaN", "", "0.01"},
		{"exponent-fee", "0.1", "1e-2", "", "0.01"},
		{"unbounded-fee", "0.1", "999999999999999999999999999999999999999999999999999999999999999999999999999999999999", "", "0.01"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, api, adapter, _, w := wizardCoordinator(t)
			w = depositWizard(t, c, api, w)
			// Large preexisting funds must never supplement this workflow's
			// budget when the exchange reports a deposit fee separately.
			api.balances["FUND:ETH"] = "10"
			api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
				return bybit.DepositPage{Rows: []bybit.DepositRecord{{ID: "reported-credit", Coin: "ETH", Chain: "ETH", ToAddress: api.deposit.Address, TxID: tx, Amount: test.reported, DepositFee: test.fee, Status: 3, DepositType: "0"}}}, nil
			}
			out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
			if err != nil {
				t.Fatal(err)
			}
			check := out.(exchangeDepositCheck)
			if check.ReportedAmount != test.reported || check.DepositFee != test.fee || check.TransferBudget != test.budget || check.Ready != (test.budget != "") || (check.ReadError != "") != (test.budget == "") {
				t.Fatalf("raw observations confused with a conservative transfer budget: %+v", check)
			}
			if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": test.excessive})); err == nil {
				t.Fatal("unrelated existing Funding balance covered an excessive transfer")
			}
			if api.posts != 0 || adapter.submits != 1 {
				t.Fatal("excess or invalid-fee transfer reached a write")
			}
			if test.budget != "" && test.budget != "0" {
				if _, err = c.handle(message("exchangeFundingTransfer", map[string]string{"wizardId": w.ID, "amount": test.budget})); err != nil || api.posts != 1 {
					t.Fatal("conservatively budgeted amount could not be separately approved", err)
				}
			}
		})
	}
}

func TestExchangeDepositTransferBudgetUsesMinimumAndRoundsDownToWalletPrecision(t *testing.T) {
	for _, test := range []struct{ original, reported, fee, want string }{
		{"6", "6", "0.1", "5.9"},
		{"6", "5.9", "0.1", "5.8"},
		{"6", "7", "0.1", "5.9"},
		{"6", "6", "0.0000000001", "5.999999999"},
		{"0.000000001", "0.000000001", "0.0000000001", "0"},
	} {
		got, err := exchangeDepositTransferBudget(test.original, test.reported, test.fee, 9)
		if err != nil || got != test.want {
			t.Fatalf("budget(%s, %s, %s) = %s, %v; want %s", test.original, test.reported, test.fee, got, err, test.want)
		}
	}
}

func TestExchangeWizardExactCreditDoesNotDependOnUnsupportedAlternateEncoding(t *testing.T) {
	c, api, _, w := tonCreditCoordinator(t)
	calls := 0
	api.depositPage = func(tx, cursor string) (bybit.DepositPage, error) {
		calls++
		if calls > 1 {
			return bybit.DepositPage{}, errors.New("alternate encoding unsupported")
		}
		return bybit.DepositPage{Rows: []bybit.DepositRecord{{ID: "first-exact-credit", Coin: "GRAM", Chain: "TON", ToAddress: api.deposit.Address, TxID: tx, Amount: "6", Status: 3, DepositType: "0"}}}, nil
	}
	out, err := c.handle(message("exchangeDepositCheck", map[string]string{"wizardId": w.ID}))
	if err != nil || !out.(exchangeDepositCheck).Ready || calls != 1 {
		t.Fatal("redundant alternate query hid a complete exact match", err)
	}
}
