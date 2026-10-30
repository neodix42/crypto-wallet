package nativewallet

import (
	"localwallet/internal/webui"
	"testing"
)

func displayedAddress(nodes []webui.Node) string {
	for _, n := range nodes {
		if n.Kind == "address" {
			return n.Value
		}
		if value := displayedAddress(n.Children); value != "" {
			return value
		}
	}
	return ""
}

func TestEveryWalletCopiesItsDisplayedAddressEvenDuringBackendWork(t *testing.T) {
	a, _, core := fixture(t)
	core.handler = func(_ uint64, method string, _, _ any) error {
		t.Errorf("rendering public clipboard data reached backend: %s", method)
		return nil
	}
	if len(a.state.Accounts) != 8 {
		t.Fatal("fixture lost supported network coverage")
	}
	for _, busy := range []bool{false, true} {
		a.busy = busy
		overview := a.groupPage(0)
		for _, account := range a.state.Accounts {
			row, ok := findNode(overview.Nodes, "open/"+account.ID)
			if !ok {
				t.Fatal("wallet missing", account.Network)
			}
			for _, nodes := range [][]webui.Node{row.Children, a.walletPage(account).Nodes} {
				copy, ok := findNode(nodes, "copyReceive/"+account.ID)
				if !ok || copy.Disabled || copy.Value != account.Address || copy.Value != displayedAddress(nodes) {
					t.Fatal("copy target differs from shown address or unnecessarily waits for backend", account.Network, busy)
				}
			}
		}
	}
}

func TestAddressCopyDataFollowsTheLatestDisplayedDescriptorAndAvailability(t *testing.T) {
	a, _, _ := fixture(t)
	account := ethAccount(t, a)
	for _, address := range []string{account.Address, "0x2222222222222222222222222222222222222222"} {
		account.Address = address
		nodes := a.walletPage(account).Nodes
		copy, ok := findNode(nodes, "copyReceive/"+account.ID)
		if !ok || copy.Value != address || copy.Value != displayedAddress(nodes) || copy.Disabled {
			t.Fatal("updated visible address has a stale clipboard target")
		}
	}
	a.state.Locked = true
	if !a.addressCopyButton(account, "Copy").Disabled {
		t.Fatal("locked address copy remained available")
	}
	a.state.Locked = false
	a.state.RecoveryConfirmed = false
	if !a.addressCopyButton(account, "Copy").Disabled {
		t.Fatal("unconfirmed recovery exposed copy")
	}
	a.state.RecoveryConfirmed = true
	account.Address = ""
	if !a.addressCopyButton(account, "Copy").Disabled {
		t.Fatal("empty address copy remained available")
	}
}
