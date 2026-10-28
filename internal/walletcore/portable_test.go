package walletcore

import (
	"path/filepath"
	"testing"
)

func TestPortableSignerRetainsStorageProfileAndUsesOnlyCurrentImage(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "cryptowallet")
	configuration := t.TempDir()
	for _, environment := range []string{"mainnet", "testnet"} {
		for _, portable := range []bool{false, true} {
			child := signerChild(executable, configuration, environment, portable)
			if child.Data != filepath.Join(configuration, "cryptowallet", environment) || child.Environment != environment {
				t.Fatal("existing wallet profile changed", child.Data, child.Environment)
			}
			if portable {
				if child.Path != "/proc/self/exe" || child.SelfRole != "--internal-signer" {
					t.Fatal("portable signer requires an external helper")
				}
			} else if child.Path != filepath.Join(filepath.Dir(executable), "wallet-signer") || child.SelfRole != "" {
				t.Fatal("legacy manifest-checked signer entry point changed")
			}
		}
	}
}
