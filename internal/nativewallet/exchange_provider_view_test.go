package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"localwallet/internal/webui"
)

func TestExchangeSelectionUsesAcknowledgedProviderAndSeparateOfficialCredentialForms(t *testing.T) {
	for _, provider := range []struct{ id, name, endpoint string }{{"binance", "Binance", "https://api.binance.com"}, {"kucoin", "KuCoin", "https://api.kucoin.com"}} {
		t.Run(provider.id, func(t *testing.T) {
			a, ui, core := fixture(t)
			a.exchange = json.RawMessage(`{"provider":"bybit","connected":true}`)
			a.quote = json.RawMessage(`{"symbol":"old-pair"}`)
			a.fields["region"] = "https://api.bybit.com"
			routeFixture(t, a, core, "exchangeSelect", map[string]string{"provider": provider.id}, map[string]any{"provider": provider.id, "connected": false})
			a.event(webui.Event{ID: "field/exchangeProvider", Value: provider.id})
			if a.selectedExchangeProvider().ID != "bybit" || a.exchange != nil || a.quote != nil {
				t.Fatal("pending selection installed authority before acknowledgement")
			}
			ui.apply(t)
			if a.selectedExchangeProvider().ID != provider.id {
				t.Fatal("acknowledged exchange was not selected")
			}
			a.fields["settings"] = "Exchange"
			text := viewText(a.settingsPage())
			if !strings.Contains(text, "Connect "+provider.name) || !strings.Contains(text, provider.endpoint) || strings.Contains(text, "https://api.bybit.com") {
				t.Fatal("credential view points at another provider", text)
			}
			if provider.id == "kucoin" && !strings.Contains(text, "KuCoin also requires") {
				t.Fatal("mandatory KuCoin passphrase omitted")
			}
			if _, ok := findNode(a.exchangePage(), "field/exchangeProvider"); !ok {
				t.Fatal("Exchange page cannot select provider")
			}
			routeFixture(t, a, core, "exchangeConnect", map[string]string{"provider": provider.id, "endpoint": provider.endpoint}, map[string]any{"provider": provider.id, "connected": true, "endpoint": provider.endpoint})
			a.event(webui.Event{ID: "exchangeConnect"})
			ui.apply(t)
			if !strings.Contains(a.status, "completed") {
				t.Fatal("provider credential connection did not route correctly")
			}
			for _, method := range []string{"exchangeQuote", "exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
				core.handler = func(_ uint64, m string, body, out any) error {
					if m != method || body.(map[string]string)["provider"] != provider.id {
						t.Error("operation lost selected-provider intent", m, body)
						return errors.New("wrong provider")
					}
					return answer(out, json.RawMessage(`{"state":"UNKNOWN"}`))
				}
				a.event(webui.Event{ID: method})
				ui.apply(t)
			}
		})
	}
}

func TestLostOrMismatchedExchangeSelectionLeavesNoUsableCachedConnection(t *testing.T) {
	for _, mode := range []string{"lost", "mismatch"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			a.exchange = json.RawMessage(`{"provider":"bybit","connected":true}`)
			a.quote = json.RawMessage(`{"symbol":"old-market"}`)
			core.handler = func(_ uint64, _ string, _ any, out any) error {
				if mode == "lost" {
					return errors.New("private selection error")
				}
				return answer(out, json.RawMessage(`{"provider":"kucoin","connected":true}`))
			}
			a.event(webui.Event{ID: "field/exchangeProvider", Value: "binance"})
			ui.apply(t)
			if a.selectedExchangeProvider().ID != "bybit" || a.exchange != nil || a.quote != nil {
				t.Fatal("uncertain selection retained stale provider authority")
			}
			for _, id := range []string{"exchangeDeposit", "exchangeSwap", "exchangeWithdraw"} {
				n, ok := findNode(a.exchangePage(), id)
				if !ok || !n.Disabled {
					t.Fatal("uncertain provider enabled funding action", id)
				}
			}
		})
	}
	a, _, core := fixture(t)
	core.handler = func(uint64, string, any, any) error { t.Error("unapproved provider reached backend"); return nil }
	for _, id := range []string{"", "https://evil.invalid", "binance/us"} {
		a.event(webui.Event{ID: "field/exchangeProvider", Value: id})
	}
	a.busy = true
	a.event(webui.Event{ID: "field/exchangeProvider", Value: "binance"})
	if a.selectedExchangeProvider().ID != "bybit" {
		t.Fatal("invalid or busy selection changed provider")
	}
	a.busy = false
	a.fields["exchangeProvider"] = "binance"
	a.exchange = json.RawMessage(`{"provider":"bybit","connected":true}`)
	if n, _ := findNode(a.exchangePage(), "exchangeDeposit"); !n.Disabled {
		t.Fatal("other provider connection enabled Binance deposit")
	}
}

func TestTokenVerificationProgressDescribesMetadataWithoutRemovingPolicyApproval(t *testing.T) {
	a, ui, core := fixture(t)
	account := ethAccount(t, a)
	contract := "0x" + strings.Repeat("1", 40)
	a.fields["tokenContract/"+account.ID] = contract
	started, release := make(chan struct{}), make(chan struct{})
	core.handler = func(_ uint64, method string, body, out any) error {
		switch method {
		case "importToken":
			request := body.(map[string]string)
			if request["accountId"] != account.ID || request["contract"] != contract {
				t.Error("metadata request changed identity")
			}
			close(started)
			<-release
			return answer(out, json.RawMessage(`{}`))
		case "accounts":
			return answer(out, a.state)
		case "networks":
			return answer(out, a.networks)
		default:
			return errors.New("unexpected mutation")
		}
	}
	a.tokenEvent("importToken", account.ID)
	<-started
	if a.status != "Checking token contract…" || strings.Contains(a.status, "confirmation") {
		close(release)
		t.Fatal("metadata read uses a premature confirmation message", a.status)
	}
	close(release)
	ui.apply(t)
	if !strings.Contains(a.status, "completed") {
		t.Fatal("token verification did not retain real import route")
	}
	// A token-policy import still routes through the signer, whose exact consent
	// and current-password controls are exercised by its separate security suite.
}
