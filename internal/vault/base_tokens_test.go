package vault

import (
	"localwallet/internal/model"
	"testing"
)

func TestBaseTokenPoliciesAreBoundToEncryptedVaultEnvironment(t *testing.T) {
	for _, environment := range []string{"mainnet", "testnet"} {
		data := synthetic()
		data.Environment = environment
		defer data.Destroy()
		for _, network := range []string{"base-mainnet", "base-sepolia"} {
			data.ERC20Tokens = []model.ERC20Token{publicVaultToken(0, network)}
			raw, err := data.Encode()
			if (err == nil) != (network == "base-sepolia" && environment == "testnet" || network == "base-mainnet" && environment == "mainnet") {
				t.Fatal("Base token crossed profile environment")
			}
			clear(raw)
		}
	}
}
