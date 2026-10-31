package ton

import "testing"

func TestGRAMCurrencyRenameKeepsTONNetworkAndPersistedNativePaymentIdentity(t *testing.T) {
	adapter, seed, archivedNativePlan := testPlan(t)
	for _, network := range adapter.Networks() {
		if len(network.Assets) == 0 || network.Assets[0].Symbol != "GRAM" || network.Assets[0].ID != network.ID+":TON" || network.Assets[0].Decimals != 9 {
			t.Fatal("currency rename changed the immutable native asset or atomic precision", network)
		}
		if network.Name != "TON Mainnet" && network.Name != "TON Testnet" {
			t.Fatal("currency rename incorrectly changed the TON blockchain identity")
		}
	}
	if archivedNativePlan.Asset != "ton-testnet:TON" || archivedNativePlan.Amount != "123456789" {
		t.Fatal("legacy payment fixture identity changed")
	}
	signed, err := adapter.Sign(seed, archivedNativePlan)
	if err != nil {
		t.Fatal("existing native TON-ID payment became unsignable after display rename", err)
	}
	if _, _, err := adapter.validateSigned(archivedNativePlan, signed); err != nil {
		t.Fatal("native payment units or exact signing changed after the GRAM display rename", err)
	}
}
