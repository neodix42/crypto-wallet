package walletsigner

import (
	"encoding/json"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

func TestCurrentFundsPrivateDispatcherCreatesNewApprovedRequestWithoutReplayingOld(t *testing.T) {
	e, ui := dispatchEngine(t, "mainnet", true)
	ui.inputs = append(ui.inputs, publicInput(publicPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, publicInput("PUBLIC_TEST_ONLY_APIKEY_92731", "PUBLIC_TEST_ONLY_SECRET_912735"))
	if err := e.ConnectExchange("https://api.bybit.com"); err != nil {
		t.Fatal(err)
	}
	accountID := ""
	for _, account := range e.Snapshot().Accounts {
		if account.Network == "ethereum-mainnet" {
			accountID = account.ID
		}
	}
	intent := signer.ExchangeIntent{Kind: "transfer", AccountID: accountID, AssetID: "ethereum-mainnet:native", Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}, Fee: "0", FeeCoin: "ETH"}
	old, err := e.PrepareExchange(intent)
	if err != nil {
		t.Fatal(err)
	}
	ui.inputs = append(ui.inputs, publicInput(publicPassword))
	old, err = e.AuthorizeExchange(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestFor := func(op vault.ExchangeOperation) bybit.AuthRequest {
		return bybit.AuthRequest{Endpoint: op.Endpoint, Method: "POST", Path: "/v5/asset/transfer/inter-transfer", Payload: op.Payload, TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	}
	if _, err = e.ExchangeAuth(requestFor(old)); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(old.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	intent.Transfer.Amount = "0.05"
	raw, err := handle(e, request("exchangePrepareCurrentFunds", intent), "mainnet")
	var next vault.ExchangeOperation
	if err != nil || json.Unmarshal(raw, &next) != nil || !next.CurrentFunds || next.ID == old.ID {
		t.Fatal("trusted current-funds dispatcher lost independent scope", err)
	}
	if _, err = e.ExchangeAuth(requestFor(next)); err == nil {
		t.Fatal("prepare method granted a debit capability")
	}
	for _, method := range []string{"exchangePrepare", "exchangePrepareCurrentFunds"} {
		data, _ := json.Marshal(intent)
		var fields map[string]any
		_ = json.Unmarshal(data, &fields)
		fields["currentFunds"] = true
		if _, err = handle(e, request(method, fields), "mainnet"); err == nil {
			t.Fatal("untyped caller flag selected current-funds authority", method)
		}
	}
	ui.inputs = append(ui.inputs, publicInput(publicPassword))
	raw, err = handle(e, request("exchangeAuthorize", map[string]string{"id": next.ID}), "mainnet")
	if err != nil || json.Unmarshal(raw, &next) != nil {
		t.Fatal(err)
	}
	if _, err = e.ExchangeAuth(requestFor(old)); err == nil {
		t.Fatal("new approval signed old unknown request")
	}
	if _, err = e.ExchangeAuth(requestFor(next)); err != nil {
		t.Fatal("fresh exact approval did not authorize separate request", err)
	}
}
