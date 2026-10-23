package signer

import (
	"encoding/json"
	"localwallet/internal/chains/decred"
	"localwallet/internal/model"
	"strings"
	"testing"
)

func TestTP042DecredNativeApprovalDisclosesRecipientMaturity(t *testing.T) {
	for _, test := range []struct{ network, required string }{{"decred-mainnet", "256 confirmations"}, {"decred-testnet3", "16 confirmations"}, {"decred-simnet", "16 confirmations"}} {
		payload, _ := json.Marshal(decred.Payload{Expiry: 1000})
		text := nativeEffects(model.Plan{Account: model.Account{Family: "decred", Network: test.network}, Payload: payload})
		if !strings.Contains(text, "recipient and change outputs") || !strings.Contains(text, test.required) {
			t.Fatal("missing authoritative spendability delay", test.network)
		}
	}
	payload, _ := json.Marshal(decred.Payload{})
	if strings.Contains(nativeEffects(model.Plan{Account: model.Account{Family: "decred"}, Payload: payload}), "Spendability delay") {
		t.Fatal("invented expiry maturity for expiry-zero transaction")
	}
}
