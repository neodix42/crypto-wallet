package nativewallet

import (
	"bytes"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/amount"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestSendValidationUsesExactDecimalsForEverySupportedNetworkAndAsset(t *testing.T) {
	a, _, _ := fixture(t)
	networks, assets := 0, 0
	for _, account := range a.state.Accounts {
		networks++
		for _, asset := range a.network(account.Network).Assets {
			assets++
			t.Run(asset.ID, func(t *testing.T) {
				draft := model.Transfer{AccountID: account.ID, Asset: asset.ID, Recipient: quoteRecipient(account), Amount: "1"}
				valid := a.paymentValidation(account, draft)
				if !valid.RecipientValid || !valid.AmountValid || valid.RecipientError != "" || valid.AmountError != "" {
					t.Fatal("whole asset amount or chain-compatible recipient rejected", valid)
				}
				minimum := "1"
				if asset.Decimals > 0 {
					minimum = "0." + strings.Repeat("0", int(asset.Decimals)-1) + "1"
				}
				draft.Amount = minimum
				if value, err := amount.Parse(minimum, asset.Decimals); err != nil || value != "1" || !a.paymentValidation(account, draft).AmountValid {
					t.Fatal("smallest exact atomic value rejected", minimum, err)
				}
				invalid := []string{"0", "0.0", "-1", "+1", "NaN", "Infinity", "1e2", "1,5", " 1", "1 ", "01", ".5", "1.", "1..2", "１", "1\x00", strings.Repeat("9", 159), "0." + strings.Repeat("0", int(asset.Decimals)) + "1"}
				for _, input := range invalid {
					draft.Amount = input
					out := a.paymentValidation(account, draft)
					if out.AmountValid || out.AmountError == "" || !out.RecipientValid {
						t.Fatal("invalid amount accepted or valid recipient overwritten", input, out)
					}
				}
				draft.Amount, draft.Recipient = "", ""
				if out := a.paymentValidation(account, draft); out.AmountValid || out.RecipientValid || out.AmountError != "" || out.RecipientError != "" {
					t.Fatal("empty draft enabled Send or displayed premature errors", out)
				}
			})
		}
	}
	if networks != 8 || assets != 19 {
		t.Fatal("validation narrowed supported networks/assets", networks, assets)
	}
}

func TestSendRecipientValidationKeepsChainChecksumsFlagsAndSharedAddressFormats(t *testing.T) {
	mainTON := address.NewAddress(0, 0, bytes.Repeat([]byte{17}, 32)).Bounce(false)
	for _, tc := range []struct {
		network string
		good    []string
		bad     []string
	}{
		{"ethereum-mainnet", []string{"0x52908400098527886E0F7030069857D2E4169EE7", "0xde709f2102306220921060314715629080e2fb77"}, []string{"alice.near", "0x5Aeda56215b167893e80B4fE645BA6d5Bab767De", "0x" + strings.Repeat("0", 40), "0x1"}},
		{"base-mainnet", []string{"0x" + strings.Repeat("1", 40)}, []string{"alice.near", "0x" + strings.Repeat("G", 40)}},
		{"arbitrum-mainnet", []string{"0x" + strings.Repeat("1", 40)}, []string{"alice.near", "0x" + strings.Repeat("1", 64)}},
		{"hyperevm-mainnet", []string{"0x" + strings.Repeat("1", 40)}, []string{"0:" + strings.Repeat("1", 64)}},
		{"hypercore-mainnet", []string{"0x" + strings.Repeat("1", 40)}, []string{"0x2222222222222222222222222222222222222222", "0x2000000000000000000000000000000000000001", "0x1"}},
		{"near-mainnet", []string{"alice.near", "example-account.near", strings.Repeat("a", 64)}, []string{"ALICE.near", "a", "a..near", "0x" + strings.Repeat("1", 64), mainTON.String()}},
		{"aptos-mainnet", []string{"0x1", "0x" + strings.Repeat("1", 40), "0x" + strings.Repeat("1", 64)}, []string{"alice.near", "0x" + strings.Repeat("1", 65), "0X1"}},
		{"ton-mainnet", []string{mainTON.String(), "0:" + strings.Repeat("1", 64)}, []string{mainTON.Copy().Testnet(true).String(), mainTON.String()[:47] + "!", "-1:" + strings.Repeat("1", 64), "alice.near"}},
	} {
		t.Run(tc.network, func(t *testing.T) {
			account := model.Account{Network: tc.network}
			for _, value := range tc.good {
				if !paymentRecipientValid(account, value) {
					t.Fatal("compatible address rejected", value)
				}
				for _, bad := range []string{" " + value, value + "\n", value + "\x00"} {
					if paymentRecipientValid(account, bad) {
						t.Fatal("mutated/padded address accepted", bad)
					}
				}
			}
			for _, value := range append(tc.bad, "", strings.Repeat("a", 257)) {
				if paymentRecipientValid(account, value) {
					t.Fatal("incompatible/checksum-invalid address accepted", value)
				}
			}
		})
	}
	if paymentRecipientValid(model.Account{Network: "unrecognized-mainnet"}, "0x"+strings.Repeat("1", 40)) {
		t.Fatal("unknown network accepted an address")
	}
}

func TestInvalidSendFieldsNeverReachQuoteOrPreparationAndKeepOriginalRecords(t *testing.T) {
	for _, mode := range []string{"recipient", "amount", "emptyRecipient", "emptyAmount", "asset", "account"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
			a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = quoteRecipient(account), "1"
			switch mode {
			case "recipient":
				a.fields["recipient/"+account.ID] = "alice.near"
			case "amount":
				a.fields["amount/"+account.ID] = "1e2"
			case "emptyRecipient":
				a.fields["recipient/"+account.ID] = ""
			case "emptyAmount":
				a.fields["amount/"+account.ID] = ""
			case "asset":
				a.networks = nil
			case "account":
				account.ID = "changed-wallet"
			}
			a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Local: journalPage{Operations: []journal.Operation{{Plan: model.Plan{ID: "original-pending"}, State: "UNKNOWN"}}}}}
			original := viewText(a.walletHistory)
			var calls atomic.Int32
			core.handler = func(_ uint64, method string, _, _ any) error {
				calls.Add(1)
				return errors.New("invalid draft reached provider/core method " + method)
			}
			a.preparePayment(account)
			started := a.busy
			if started {
				// Drain an unexpected worker before failing, so removing the guard
				// produces this assertion rather than a late goroutine test panic.
				ui.apply(t)
			}
			if started || calls.Load() != 0 || a.nextPaymentQuote() || a.busy || a.background || a.plan != nil || len(ui.queue) != 0 || viewText(a.walletHistory) != original {
				t.Fatal("invalid draft acquired authority, scheduled work or changed original records")
			}
			for _, n := range a.paymentFeeNodes(account) {
				if n.Text != "" {
					t.Fatal("invalid/empty draft displayed fee helper or pending estimate", n.Text)
				}
			}
		})
	}
}

func TestSendValidationRejectsForeignDraftIdentityAndAssetAndUsesImportedPrecision(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	draft := model.Transfer{AccountID: account.ID, Asset: a.asset(account).ID, Recipient: quoteRecipient(account), Amount: "1"}
	for _, change := range []func(*model.Transfer){func(d *model.Transfer) { d.AccountID += "foreign" }, func(d *model.Transfer) { d.Asset = "arbitrum-mainnet:ETH" }, func(d *model.Transfer) { d.Asset = "untracked" }} {
		changed := draft
		change(&changed)
		if out := a.paymentValidation(account, changed); out.AmountValid || out.AmountError == "" {
			t.Fatal("foreign/untracked draft accepted", changed, out)
		}
	}
	for i := range a.networks {
		if a.networks[i].ID == account.Network {
			a.networks[i].Assets = append(a.networks[i].Assets, model.Asset{ID: "explicitly-imported", Symbol: "EXAMPLE", Decimals: 3})
		}
	}
	draft.Asset, draft.Amount = "explicitly-imported", "0.001"
	if out := a.paymentValidation(account, draft); !out.AmountValid {
		t.Fatal("explicit imported policy precision ignored", out)
	}
	draft.Amount = "0.0001"
	if out := a.paymentValidation(account, draft); out.AmountValid || !strings.Contains(out.AmountError, "3 decimal places") {
		t.Fatal("imported policy amount rounded or accepted", out)
	}
}
