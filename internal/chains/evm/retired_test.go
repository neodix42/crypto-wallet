package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"localwallet/internal/model"
)

func TestRetiredHorizenRecoveryNeverEnablesNetworkAuthority(t *testing.T) {
	a := New()
	for _, network := range []string{"horizen-mainnet", "horizen-testnet"} {
		for _, index := range []uint32{0, 1, 127} {
			account, err := a.Derive(syntheticSeed(), index, "base-mainnet")
			if err != nil {
				t.Fatal(err)
			}
			account.Network, account.ID = network, fmt.Sprintf("evm:%d:%s", index, network)
			if ValidateRetiredAccount(syntheticSeed(), account) != nil {
				t.Fatal("historical root identity lost")
			}
			if _, err := a.Derive(syntheticSeed(), index, network); err == nil {
				t.Fatal("retired derivation offered as an active wallet")
			}
			if _, err := NewWithEndpoints(map[string]string{network: "https://untrusted.invalid"}); err == nil {
				t.Fatal("retired endpoint accepted")
			}
			if a.ValidateAddress(network, account.Address) == nil {
				t.Fatal("retired network address authorized")
			}
			if _, err := a.Balance(context.Background(), account); err == nil {
				t.Fatal("retired network RPC reachable")
			}
			for _, mutate := range []func(*model.Account){
				func(a *model.Account) { a.ID = "foreign" }, func(a *model.Account) { a.Network = "horizen-future" },
				func(a *model.Account) { a.Profile = "other" }, func(a *model.Account) { a.Family = "near" },
				func(a *model.Account) { a.Address = "0x1111111111111111111111111111111111111111" },
				func(a *model.Account) { a.PublicKey = "0x00" }, func(a *model.Account) { a.Index = 1 << 31 },
				func(a *model.Account) { a.Parameters = json.RawMessage(`{}`) },
			} {
				bad := account
				mutate(&bad)
				if ValidateRetiredAccount(syntheticSeed(), bad) == nil {
					t.Fatal("altered retired descriptor authenticated")
				}
			}
			if ValidateRetiredAccount(nil, account) == nil {
				t.Fatal("invalid root authenticated")
			}
		}
	}
	for _, n := range a.Networks() {
		if IsRetiredNetwork(n.ID) {
			t.Fatal("retired network exposed in catalog")
		}
	}
}
