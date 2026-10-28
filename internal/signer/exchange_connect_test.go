package signer

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

type exchangeConnectUI struct {
	*testUI
	titles      []string
	labels      [][]string
	returned    [][][]byte
	inputHook   func()
	confirmHook func()
}

func (ui *exchangeConnectUI) Input(title string, labels ...string) ([][]byte, error) {
	ui.titles = append(ui.titles, title)
	ui.labels = append(ui.labels, append([]string(nil), labels...))
	values, err := ui.testUI.Input(title, labels...)
	ui.returned = append(ui.returned, values)
	if ui.inputHook != nil {
		ui.inputHook()
	}
	return values, err
}

func (ui *exchangeConnectUI) Confirm(title, summary string) bool {
	accepted := ui.testUI.Confirm(title, summary)
	if ui.confirmHook != nil {
		ui.confirmHook()
	}
	return accepted
}

func TestExchangeConnectUsesOnlyPrivateAPICredentialsAndCustodyConfirmation(t *testing.T) {
	e, original, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, original, "mainnet")
	ui := &exchangeConnectUI{testUI: original}
	e.UI = ui
	original.inputs = append(original.inputs, input(exchangeTestKey, exchangeTestSecret))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal("unlocked wallet required an extra app password to connect", err)
	}
	if len(ui.titles) != 1 || ui.titles[0] != "Connect Bybit — enter your API key and matching API secret" ||
		!reflect.DeepEqual(ui.labels[0], []string{"Bybit API key", "Bybit API secret"}) || len(original.inputs) != 0 {
		t.Fatal("connection asked for data beyond its private API key and matching secret")
	}
	for _, values := range ui.returned {
		for _, value := range values {
			if !bytes.Equal(value, make([]byte, len(value))) {
				t.Fatal("private API credential input survived connection completion")
			}
		}
	}
	confirmation := original.summaries[len(original.summaries)-1]
	if !strings.Contains(confirmation, "https://api.bybit.com") || !strings.Contains(confirmation, "No deposit, trade, transfer or withdrawal is sent by connecting") {
		t.Fatal("connection omitted the exact endpoint or its limited effect")
	}
	status, err := e.ExchangeStatus()
	if err != nil || !status.Connected || len(e.data.Exchange.Operations) != 0 {
		t.Fatal("connecting failed or created a funds-moving operation")
	}
	public, _ := json.Marshal(status)
	for _, secret := range []string{exchangeTestKey, exchangeTestSecret} {
		if bytes.Contains(public, []byte(secret)) || strings.Contains(confirmation, secret) {
			t.Fatal("API credentials escaped the private input/encrypted storage boundary")
		}
		for _, name := range []string{"vault.lwv", "vault.previous.lwv"} {
			ciphertext, err := e.Files.Read(name)
			if err != nil || bytes.Contains(ciphertext, []byte(secret)) {
				t.Fatal("connection credentials were not persisted encrypted")
			}
		}
	}
	account := findAccount(t, e, "ethereum-mainnet")
	operation, err := e.PrepareExchange(exchangeSwap(account))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := e.AuthorizeExchange(operation.ID); err == nil || result.ID != "" {
		t.Fatal("connect-only password exception authorized a trade without its password")
	}
	if e.DisconnectExchange() == nil || e.data.Exchange.Credentials == nil {
		t.Fatal("connect-only password exception removed disconnection authentication")
	}
}

func TestExchangeConnectRejectsCancelledChangedAndUnavailableSessions(t *testing.T) {
	for _, mode := range []string{"cancel-input", "cancel-confirm", "locked", "recovery", "testnet", "lock-input", "lock-confirm", "generation-input", "generation-confirm", "data-input", "exchange-input", "persist"} {
		t.Run(mode, func(t *testing.T) {
			e, original, _ := exchangeEngine(t)
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			ui := &exchangeConnectUI{testUI: original}
			e.UI = ui
			if mode != "cancel-input" {
				original.inputs = append(original.inputs, input("NEW_PUBLIC_TEST_APIKEY_57193", exchangeTestSecret))
			}
			switch mode {
			case "cancel-confirm":
				original.approve = false
			case "locked":
				e.Lock()
			case "recovery":
				e.data.RecoveryConfirmed = false
			case "testnet":
				e.data.Environment = "testnet"
			case "lock-input":
				ui.inputHook = e.Lock
			case "lock-confirm":
				ui.confirmHook = e.Lock
			case "generation-input":
				ui.inputHook = func() { e.data.Generation++ }
			case "generation-confirm":
				ui.confirmHook = func() { e.data.Generation++ }
			case "data-input":
				ui.inputHook = func() { copied := *e.data; e.data = &copied }
			case "exchange-input":
				ui.inputHook = func() { e.data.Exchange = &vault.ExchangeData{} }
			case "persist":
				ui.confirmHook = func() { e.parameters = vault.Parameters{} }
			}
			if err := e.ConnectExchange("https://api.bybit.com"); err == nil {
				if mode == "cancel-confirm" {
					t.Fatal("cancelled Bybit custody approval granted connection")
				}
				t.Fatal("Bybit connection accepted a changed wallet session")
			}
			after, err := e.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected Bybit connection changed encrypted credentials")
			}
			if (mode == "locked" || mode == "recovery" || mode == "testnet") && len(ui.titles) != 0 {
				t.Fatal("unavailable wallet requested private API credentials")
			}
		})
	}
}
