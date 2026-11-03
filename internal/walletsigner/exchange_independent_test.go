package walletsigner

import (
	"encoding/json"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

func TestIndependentExchangeCancelDispatchIsTypedAndExplicitlyConfirmed(t *testing.T) {
	e, ui := dispatchEngine(t, "mainnet", true)
	ui.inputs = append(ui.inputs, publicInput(publicPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, publicInput("PUBLIC_TEST_ONLY_APIKEY_92731", "PUBLIC_TEST_ONLY_SECRET_912735"))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	var accountID string
	for _, account := range e.Snapshot().Accounts {
		if account.Network == "ethereum-mainnet" {
			accountID = account.ID
		}
	}
	op, err := e.PrepareExchange(signer.ExchangeIntent{Kind: "transfer", AccountID: accountID, AssetID: "ethereum-mainnet:native", Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}, Fee: "0", FeeCoin: "ETH"})
	if err != nil {
		t.Fatal(err)
	}
	before := ui.confirmCalls
	for _, body := range []any{map[string]any{"id": nil}, map[string]any{"id": 1}, map[string]any{"id": op.ID, "amount": "2"}} {
		if _, err := handle(e, request("exchangeCancelConfirmed", body), "mainnet"); err == nil || ui.confirmCalls != before {
			t.Fatal("malformed cancel changed authority or opened a misleading prompt", body, err)
		}
	}
	raw, err := handle(e, request("exchangeCancelConfirmed", map[string]string{"id": op.ID}), "mainnet")
	var status signer.ExchangeStatus
	if err != nil || json.Unmarshal(raw, &status) != nil || ui.confirmCalls != before+1 || len(status.Operations) != 1 || status.Operations[0].ID != op.ID || status.Operations[0].State != "CANCELLED" {
		t.Fatal("exact unsigned cancellation was not confirmed and retained", string(raw), err)
	}
	var after vault.ExchangeOperation
	operation, err := handle(e, request("exchangeOperation", map[string]string{"id": op.ID}), "mainnet")
	if err != nil || json.Unmarshal(operation, &after) != nil || after.Payload != op.Payload || after.Digest != op.Digest {
		t.Fatal("cancel dispatch rewrote the original immutable request")
	}
}
