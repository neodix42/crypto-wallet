package ton

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func TestTONPublicProfileValidationPreservesExactContractIdentity(t *testing.T) {
	_, seed := fixtures(t)
	for _, network := range []string{"ton-mainnet", "ton-testnet"} {
		for _, profile := range []string{Profile, ProfileSubwallet42} {
			account, err := New().DeriveProfile(seed, 0, network, profile)
			if err != nil || ValidateAccount(account) != nil {
				t.Fatal("valid immutable public account rejected", err)
			}
			for name, change := range map[string]func(*model.Account){
				"network":    func(a *model.Account) { a.Network = "ton-foreign" },
				"profile":    func(a *model.Account) { a.Profile = "raw-wallet-id42" },
				"index":      func(a *model.Account) { a.Index++ },
				"family":     func(a *model.Account) { a.Family = "evm" },
				"id":         func(a *model.Account) { a.ID += "changed" },
				"public key": func(a *model.Account) { a.PublicKey = strings.Repeat("00", 32) },
				"address":    func(a *model.Account) { a.Address = "0:" + strings.Repeat("00", 32) },
				"wallet id": func(a *model.Account) {
					var p Parameters
					_ = json.Unmarshal(a.Parameters, &p)
					p.WalletID ^= 1
					a.Parameters, _ = json.Marshal(p)
				},
			} {
				t.Run(network+"/"+profile+"/"+name, func(t *testing.T) {
					changed := account
					change(&changed)
					if ValidateAccount(changed) == nil {
						t.Fatal("changed public recovery identity was accepted")
					}
				})
			}
		}
	}
}
