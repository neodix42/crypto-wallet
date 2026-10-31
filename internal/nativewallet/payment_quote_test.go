package nativewallet

// Controller peers below prove read-only routing and lifecycle behavior, not
// provider fee accuracy or protocol acceptance.
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/ton"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func quoteRecipient(account model.Account) string {
	switch account.Family {
	case "evm", "hypercore":
		return "0x" + strings.Repeat("1", 40)
	case "near":
		return strings.Repeat("a", 64)
	case "aptos":
		return "0x1"
	case "ton":
		return "0:" + strings.Repeat("1", 64)
	}
	return "unsupported"
}

func quotePlan(a *App, account model.Account, request paymentQuoteRequest) model.Plan {
	asset := a.asset(account)
	value, _ := amount.Parse(request.Amount, asset.Decimals)
	if request.Maximum {
		value, _ = amount.Parse("2.5", asset.Decimals)
	}
	plan := model.Plan{ID: "read-only-quote", Account: account, Asset: asset.ID, Recipient: request.Recipient, Amount: value, Memo: request.Memo, FeeAsset: a.network(account.Network).Assets[0].ID, MaxFee: "21000000000000", Expires: time.Now().Add(time.Minute).Unix(), Payload: json.RawMessage(`{}`)}
	if account.Family == "hypercore" {
		plan.FeeAsset, plan.MaxFee = asset.ID, "0"
	}
	if account.Family == "ton" {
		payload := ton.Payload{EstimatedFee: "1000000", StorageReserve: "10000000"}
		plan.MaxFee = "3000000"
		if asset.ID != account.Network+":TON" {
			payload.Kind = "jetton_transfer"
			payload.Jetton = &ton.JettonPayload{Attached: "100000000", Forward: "1"}
			plan.MaxFee = "103000000"
		}
		plan.Payload, _ = json.Marshal(payload)
	}
	return plan
}

func startDueQuote(t *testing.T, a *App) {
	t.Helper()
	if a.nextPaymentQuote() {
		t.Fatal("quote skipped initial debounce")
	}
	if a.nextPaymentQuote() {
		t.Fatal("quote ignored debounce deadline")
	}
	a.paymentQuote.Due = time.Now().Add(-time.Millisecond)
	if !a.nextPaymentQuote() {
		t.Fatal("valid due quote was not scheduled")
	}
}

func TestAutomaticPaymentQuotesCoverEveryCurrentNativeAndTokenAssetWithoutAuthority(t *testing.T) {
	base, _, _ := fixture(t)
	assets := 0
	for _, descriptor := range base.state.Accounts {
		for _, selected := range base.network(descriptor.Network).Assets {
			t.Run(selected.ID, func(t *testing.T) {
				a, ui, core := fixture(t)
				account, _ := a.account(descriptor.ID)
				a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
				a.fields["asset/"+account.ID] = selected.ID
				a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID], a.fields["memo/"+account.ID] = quoteRecipient(account), "1.25", "public comment"
				calls := 0
				core.handler = func(_ uint64, method string, body, out any) error {
					calls++
					request, ok := body.(paymentQuoteRequest)
					if method != "quotePayment" || !ok || request.Maximum || request.AccountID != account.ID || request.Asset != selected.ID || request.Amount != "1.25" || request.Recipient != quoteRecipient(account) || request.Memo != "public comment" {
						t.Error("fee observation acquired mutation authority or changed units", method, body)
					}
					return answer(out, quotePlan(a, account, request))
				}
				startDueQuote(t, a)
				ui.apply(t)
				if calls != 1 || a.plan != nil || a.paymentQuote.Plan == nil || !a.paymentQuote.Complete || a.busy || a.background || a.nextPaymentQuote() {
					t.Fatal("completed observation repeated, blocked UX or became a prepared payment", a.paymentQuote)
				}
				text := viewText(a.paymentFeeNodes(account))
				if !strings.Contains(text, "fee") || strings.Contains(text, "unavailable") || strings.Contains(a.status, "confirmation") {
					t.Fatal("automatic fee did not show an honest read-only estimate", text, a.status)
				}
				if account.Family == "ton" && (!strings.Contains(text, "0.001 GRAM") || !strings.Contains(text, "0.01 GRAM")) {
					t.Fatal("TON network fee or separate storage reserve lost", text)
				}
				assets++
			})
		}
	}
	if assets != 19 {
		t.Fatal("native/token quote coverage silently changed", assets)
	}
}

func TestRepeatedMaximumQuotesAllAssetsKeepExactUnitsAndOriginalReservation(t *testing.T) {
	base, _, _ := fixture(t)
	for _, descriptor := range base.state.Accounts {
		for _, selected := range base.network(descriptor.Network).Assets {
			t.Run(selected.ID, func(t *testing.T) {
				a, ui, core := fixture(t)
				account, _ := a.account(descriptor.ID)
				a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
				a.fields["asset/"+account.ID], a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = selected.ID, quoteRecipient(account), "1.25"
				original := journal.Operation{Plan: model.Plan{ID: "signed-original", Account: account, Asset: selected.ID}, State: "UNKNOWN"}
				a.walletHistory = map[string]walletHistoryState{account.ID: {Loaded: true, Local: journalPage{Operations: []journal.Operation{original}}}}
				before := viewText(a.walletHistory)
				calls := 0
				core.handler = func(_ uint64, method string, body, out any) error {
					calls++
					request, ok := body.(paymentQuoteRequest)
					if method != "quotePayment" || !ok || !request.Maximum || request.Asset != selected.ID {
						t.Error("Max reserved, prepared, signed or replaced a payment", method, body)
					}
					return answer(out, quotePlan(a, account, request))
				}
				for i := 0; i < 2; i++ {
					a.walletEvent("max/" + account.ID)
					ui.apply(t)
					atomic, err := amount.Parse(a.fields["amount/"+account.ID], selected.Decimals)
					expected, _ := amount.Parse("2.5", selected.Decimals)
					if err != nil || atomic != expected || a.plan != nil || a.paymentQuote.Plan == nil || viewText(a.walletHistory) != before {
						t.Fatal("Max lost denomination or mutated an unresolved reservation", i, a.fields["amount/"+account.ID])
					}
				}
				if calls != 2 {
					t.Fatal("repeated Max did not remain read-only", calls)
				}
			})
		}
	}
}

type cancelledQuoteBackend struct {
	*testBackend
	started, cancelled chan struct{}
}

func (b *cancelledQuoteBackend) CallPaymentQuoteAt(ctx context.Context, _ uint64, _, _ any) error {
	close(b.started)
	<-ctx.Done()
	close(b.cancelled)
	return ctx.Err()
}

func TestAutomaticQuoteCancellationRejectsEditedNavigatedHiddenAndLockedResults(t *testing.T) {
	for _, mode := range []string{"recipient", "amount", "memo", "tab", "closeTab", "hideSend", "lock", "close", "explicitCancel"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active, a.sendForms[account.ID], a.tabs = "wallet/"+account.ID, true, []string{account.ID}
			a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = quoteRecipient(account), "1"
			peer := &cancelledQuoteBackend{testBackend: core, started: make(chan struct{}), cancelled: make(chan struct{})}
			a.core = peer
			startDueQuote(t, a)
			<-peer.started
			switch mode {
			case "recipient", "amount", "memo":
				value := "2"
				if mode == "recipient" {
					value = "0x" + strings.Repeat("2", 40)
				}
				a.event(webui.Event{ID: "field/" + mode + "/" + account.ID, Value: value})
			case "tab":
				a.event(webui.Event{ID: "tab", Value: "group/0"})
			case "closeTab":
				a.event(webui.Event{ID: "closeTab", Value: "wallet/" + account.ID})
			case "hideSend":
				a.walletEvent("sendForm/" + account.ID)
			case "lock":
				a.lock()
			case "close":
				a.close()
			case "explicitCancel":
				a.stopPaymentQuote()
			}
			select {
			case <-peer.cancelled:
			case <-time.After(time.Second):
				t.Fatal("obsolete quote retained its network request", mode)
			}
			ui.apply(t)
			if a.plan != nil || a.paymentQuote.Plan != nil || strings.Contains(viewText(a.paymentFeeNodes(account)), "Estimated transaction fee budget:") {
				t.Fatal("cancelled quote restored spend authority or stale fee", mode)
			}
		})
	}
}

func TestPaymentQuoteFailureAndMalformedPlanNeverCreateSendAuthorityOrLeakProviderData(t *testing.T) {
	for _, mode := range []string{"providerFailure", "recipient", "asset", "account", "amount", "fee", "expired", "foreignFee"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			account := ethAccount(t, a)
			a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
			a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = quoteRecipient(account), "1"
			core.handler = func(_ uint64, method string, body, out any) error {
				if method != "quotePayment" {
					t.Error("quote requested a mutation", method)
				}
				if mode == "providerFailure" {
					return errors.New("private-provider-canary")
				}
				plan := quotePlan(a, account, body.(paymentQuoteRequest))
				switch mode {
				case "recipient":
					plan.Recipient = "0x" + strings.Repeat("2", 40)
				case "asset":
					plan.Asset = "untracked"
				case "account":
					plan.Account.PublicKey = "different"
				case "amount":
					plan.Amount = "2"
				case "fee":
					plan.MaxFee = "-1"
				case "expired":
					plan.Expires = time.Now().Add(-time.Minute).Unix()
				case "foreignFee":
					plan.FeeAsset = "base-mainnet:ETH"
				}
				return answer(out, plan)
			}
			startDueQuote(t, a)
			ui.apply(t)
			text := viewText(a.paymentFeeNodes(account))
			if a.plan != nil || a.paymentQuote.Plan != nil || !strings.Contains(text, "unavailable") || strings.Contains(text, "private-provider-canary") {
				t.Fatal("invalid quote gained authority or exposed provider data", mode, text)
			}
		})
	}
}

func TestExplicitWalletWorkCancelsQuoteAndAllowsFreshObservationAfterwards(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	a.active, a.sendForms[account.ID] = "wallet/"+account.ID, true
	a.fields["recipient/"+account.ID], a.fields["amount/"+account.ID] = quoteRecipient(account), "1"
	peer := &cancelledQuoteBackend{testBackend: core, started: make(chan struct{}), cancelled: make(chan struct{})}
	a.core = peer
	startDueQuote(t, a)
	<-peer.started
	a.work("Reading public wallet information…", func(uint64) (func(), error) { return nil, nil })
	select {
	case <-peer.cancelled:
	case <-time.After(time.Second):
		t.Fatal("explicit wallet work retained the quote request")
	}
	ui.apply(t)
	ui.apply(t)
	if a.paymentQuote.Loading || a.background || a.busy || a.plan != nil {
		t.Fatal("interrupted quote retained a loading spinner or payment authority", a.paymentQuote)
	}
	a.core = core
	core.handler = func(_ uint64, method string, body, out any) error {
		if method != "quotePayment" {
			return errors.New("fresh observation attempted mutation")
		}
		return answer(out, quotePlan(a, account, body.(paymentQuoteRequest)))
	}
	a.paymentQuote.Due = time.Now().Add(-time.Millisecond)
	if !a.nextPaymentQuote() {
		t.Fatal("interrupted observation could never estimate the same unchanged draft again")
	}
	ui.apply(t)
	if a.paymentQuote.Plan == nil || !a.paymentQuote.Complete || a.paymentQuote.Loading || a.plan != nil {
		t.Fatal("fresh quote failed after explicit wallet work", a.paymentQuote)
	}
}
