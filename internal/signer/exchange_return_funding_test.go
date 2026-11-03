package signer

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestExchangeUnifiedReturnApprovalIsExactDurableAndDoesNotAuthorizeWithdrawal(t *testing.T) {
	e, ui, account := exchangeEngine(t)
	intent := ExchangeIntent{Kind: "transfer", AccountID: account.ID, AssetID: account.Network + ":native", Fee: "0", FeeCoin: "ETH", Transfer: &bybit.TransferRequest{Coin: "ETH", Amount: "0.123456789012345678", FromAccountType: "UNIFIED", ToAccountType: "FUND"}}
	op, err := e.PrepareExchange(intent)
	if err != nil {
		t.Fatal(err)
	}
	var request bybit.TransferRequest
	if json.Unmarshal([]byte(op.Payload), &request) != nil || request.TransferID != exchangeUUID(op.ID) || request.FromAccountType != "UNIFIED" || request.ToAccountType != "FUND" || request.Amount != intent.Transfer.Amount || request.Coin != "ETH" {
		t.Fatal("reverse transfer's original payload lost its exact durable identity or direction")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(op)); err == nil {
		t.Fatal("navigation or unsigned preparation authorized return transfer")
	}
	ui.inputs = append(ui.inputs, input("incorrect-public-fixture-password"))
	if _, err = e.AuthorizeExchange(op.ID); err == nil {
		t.Fatal("reverse transfer accepted a password other than current storage password")
	}
	cancelledID := op.ID
	stored, err := e.ExchangeOperation(cancelledID)
	if err != nil || stored.State != "CANCELLED" {
		t.Fatal("failed password left a reverse transfer authorized", err)
	}
	op, err = e.PrepareExchange(intent)
	if err != nil || op.ID == cancelledID || json.Unmarshal([]byte(op.Payload), &request) != nil || request.TransferID != exchangeUUID(op.ID) {
		t.Fatal("new explicit reverse transfer reused failed approval authority", err)
	}
	op = authorizeExchange(t, e, ui, op)
	if len(ui.summaries) == 0 {
		t.Fatal("reverse transfer omitted exact approval")
	}
	summary := ui.summaries[len(ui.summaries)-1]
	for _, required := range []string{"0.123456789012345678 ETH", "From account: UNIFIED", "To account: FUND", "does not place a trade"} {
		if !strings.Contains(summary, required) {
			t.Fatal("reverse transfer approval omitted its exact effect", required)
		}
	}
	if _, err = e.ExchangeAuth(exchangeRequest(op)); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateExchange(op.ID, "UNKNOWN", ""); err != nil {
		t.Fatal(err)
	}
	e.Lock()
	ui.inputs = append(ui.inputs, input(syntheticPassword))
	if err = e.Unlock(); err != nil {
		t.Fatal(err)
	}
	restored, err := e.ExchangeOperation(op.ID)
	if err != nil || restored.State != "UNKNOWN" || restored.Payload != op.Payload || restored.Digest != op.Digest {
		t.Fatal("encrypted restart changed or lost the unknown reverse transfer", err)
	}
	withdraw := ExchangeIntent{Kind: "withdraw", AccountID: account.ID, AssetID: account.Network + ":native", ChainType: "Ethereum", Fee: "0.0002", FeeCoin: "ETH", Withdrawal: &bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: account.Address, Amount: "0.1", ForceChain: 1, AccountType: "FUND", FeeType: 1}}
	if _, err = e.PrepareExchange(withdraw); err == nil {
		t.Fatal("unknown reverse transfer permitted a replacement economic action")
	}
	if err = e.UpdateExchange(op.ID, "CONFIRMED", request.TransferID); err != nil {
		t.Fatal(err)
	}
	withdrawal, err := e.PrepareExchange(withdraw)
	if err != nil {
		t.Fatal(err)
	}
	if withdrawal.ID == op.ID {
		t.Fatal("withdrawal reused reverse transfer's identity")
	}
	if _, err = e.ExchangeAuth(exchangeRequest(withdrawal)); err == nil {
		t.Fatal("Unified→Funding approval authorized a distinct Funding withdrawal")
	}
}
