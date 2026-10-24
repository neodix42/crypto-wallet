package main

import (
	"encoding/json"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
	"strings"
	"testing"
	"time"
)

func TestExchangePrivateDispatchRequiresMaskedCredentialsAndDurableApproval(t *testing.T) {
	e, u := dispatchEngine(t, "mainnet", true)
	u.inputs = append(u.inputs, publicInput(publicPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	var initial signer.ExchangeStatus
	raw, err := handle(e, request("exchangeStatus", struct{}{}), "mainnet")
	if err != nil || json.Unmarshal(raw, &initial) != nil || initial.Connected {
		t.Fatal("initial exchange status")
	}
	u.inputs = append(u.inputs, publicInput(publicPassword), publicInput("PUBLIC_TEST_ONLY_APIKEY_92731", "PUBLIC_TEST_ONLY_SECRET_912735"))
	raw, err = handle(e, request("exchangeConnect", map[string]string{"endpoint": "https://api.bybit.com"}), "mainnet")
	if err != nil || strings.Contains(string(raw), "PUBLIC_TEST_ONLY_SECRET") {
		t.Fatal("masked connection")
	}
	var accountID string
	for _, a := range e.Snapshot().Accounts {
		if a.Network == "ethereum-mainnet" {
			accountID = a.ID
		}
	}
	in := signer.ExchangeIntent{Kind: "swap", AccountID: accountID, AssetID: "ethereum-mainnet:native", Order: &bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Buy", OrderType: "Market", Qty: "12.34", MarketUnit: "quoteCoin", SlippageToleranceType: "Percent", SlippageTolerance: "1"}}
	raw, err = handle(e, request("exchangePrepare", in), "mainnet")
	if err != nil {
		t.Fatal(err)
	}
	var op vault.ExchangeOperation
	if json.Unmarshal(raw, &op) != nil || op.ID == "" {
		t.Fatal("prepared identity")
	}
	raw, err = handle(e, request("exchangeOperation", map[string]string{"id": op.ID}), "mainnet")
	if err != nil || !strings.Contains(string(raw), op.ID) {
		t.Fatal("lookup")
	}
	u.inputs = append(u.inputs, publicInput(publicPassword))
	raw, err = handle(e, request("exchangeAuthorize", map[string]string{"id": op.ID}), "mainnet")
	if err != nil || json.Unmarshal(raw, &op) != nil {
		t.Fatal("authorize")
	}
	auth := bybit.AuthRequest{Endpoint: op.Endpoint, Method: "POST", Path: "/v5/order/create", Payload: op.Payload, TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	raw, err = handle(e, request("exchangeAuth", auth), "mainnet")
	if err != nil || !strings.Contains(string(raw), "signature") {
		t.Fatal("exact request authorization")
	}
	raw, err = handle(e, request("exchangeUpdate", map[string]string{"id": op.ID, "state": "UNKNOWN", "externalId": ""}), "mainnet")
	if err != nil || !strings.Contains(string(raw), "UNKNOWN") {
		t.Fatal("ambiguous outcome")
	}
	if _, err = handle(e, request("exchangeCancel", map[string]string{"id": op.ID}), "mainnet"); err == nil {
		t.Fatal("uncertain operation cancelled")
	}
	if _, err = handle(e, request("exchangeUpdate", map[string]string{"id": op.ID, "state": "CONFIRMED", "externalId": "public-remote-order"}), "mainnet"); err != nil {
		t.Fatal(err)
	}
	raw, err = handle(e, request("exchangePrepare", in), "mainnet")
	if err != nil || json.Unmarshal(raw, &op) != nil {
		t.Fatal("second distinct operation")
	}
	if _, err = handle(e, request("exchangeCancel", map[string]string{"id": op.ID}), "mainnet"); err != nil {
		t.Fatal(err)
	}
	raw, err = handle(e, request("exchangePrepare", in), "mainnet")
	if err != nil || json.Unmarshal(raw, &op) != nil {
		t.Fatal("rejection preparation")
	}
	u.inputs = append(u.inputs, publicInput(publicPassword))
	raw, err = handle(e, request("exchangeAuthorize", map[string]string{"id": op.ID}), "mainnet")
	if err != nil || json.Unmarshal(raw, &op) != nil {
		t.Fatal("rejection authorization")
	}
	auth.Payload = op.Payload
	auth.TimestampMS = time.Now().UnixMilli()
	if _, err = handle(e, request("exchangeAuth", auth), "mainnet"); err != nil {
		t.Fatal(err)
	}
	raw, err = handle(e, request("exchangeReject", map[string]any{"id": op.ID, "code": 10003}), "mainnet")
	if err != nil || !strings.Contains(string(raw), `"rejectionCode":10003`) {
		t.Fatal("documented rejection not persisted")
	}
	u.inputs = append(u.inputs, publicInput(publicPassword))
	if _, err = handle(e, request("exchangeDisconnect", struct{}{}), "mainnet"); err != nil {
		t.Fatal(err)
	}
}

func TestExchangePrivateDispatchRejectsUnknownFieldsSecretsAndLockedRequests(t *testing.T) {
	e, _ := dispatchEngine(t, "mainnet", false)
	methods := []string{"exchangeStatus", "exchangeConnect", "exchangeDisconnect", "exchangePrepare", "exchangeAuthorize", "exchangeOperation", "exchangeCancel", "exchangeAuth", "exchangeUpdate", "exchangeReject"}
	for _, method := range methods {
		for _, raw := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"secret":"CANARY-must-not-be-accepted"}`), json.RawMessage(`{"id":null}`), json.RawMessage(`{"id":"a","id":"b"}`)} {
			if out, err := handleExchange(e, method, raw); err == nil || out != nil {
				t.Fatalf("locked/malformed request accepted: %s", method)
			}
		}
	}
	if _, err := handleExchange(e, "exchangeGenericSign", json.RawMessage(`{}`)); err == nil {
		t.Fatal("arbitrary exchange signing exposed")
	}
}
