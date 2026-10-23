package signer

import (
	"strings"
	"testing"

	"localwallet/internal/chains"
)

func TestTP033VerifiedReceiveKeepsActivationAndProfileGuidance(t *testing.T) {
	e, u, _ := newEngine(t, chains.All())
	createImported(t, e, u, "testnet")
	for _, tc := range []struct{ network, required string }{
		{"hypercore-testnet", "Activation is NOT established by this address"},
		{"ton-testnet", "TON BIP39 Wallet V5R1"},
	} {
		a := findAccount(t, e, tc.network)
		if png, err := e.Receive(a.ID); err != nil || len(png) == 0 {
			t.Fatal("verified receive failed", tc.network, err)
		}
		summary := u.summaries[len(u.summaries)-1]
		if !strings.Contains(summary, tc.required) || !strings.Contains(summary, a.Network) || !strings.Contains(summary, a.Address) {
			t.Fatal("verified receive omitted exact account or prerequisite guidance", tc.network)
		}
	}
}
