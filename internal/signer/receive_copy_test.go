package signer

import (
	"bytes"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/qr"
)

type receiveCopyUI struct {
	*testUI
	addresses []string
	images    [][]byte
}

func (u *receiveCopyUI) ReceiveAddress(title, summary, address string, png []byte) bool {
	u.addresses = append(u.addresses, address)
	u.images = append(u.images, bytes.Clone(png))
	return u.Receive(title, summary, png)
}

func TestVerifiedReceiveCopyBindsEachNetworkAddressAndQR(t *testing.T) {
	e, base, _ := newEngine(t, chains.All())
	createImported(t, e, base, "testnet")
	ui := &receiveCopyUI{testUI: base}
	e.UI = ui
	const unchanged = "public synthetic prior clipboard 45182"
	ui.copied = unchanged
	for _, original := range e.Snapshot().Accounts {
		image, err := e.Receive(original.ID)
		if err != nil {
			t.Fatal("verified receive failed", original.Network, err)
		}
		current, err := e.account(original.ID)
		if err != nil {
			t.Fatal(err)
		}
		copiedAddress := ui.addresses[len(ui.addresses)-1]
		decoded, err := qr.Decode(image)
		if err != nil || decoded != current.Address || copiedAddress != current.Address || !bytes.Equal(image, ui.images[len(ui.images)-1]) {
			t.Fatal("native copy address and QR differ from the exact public account", original.Network, err)
		}
		if ui.copied != unchanged {
			t.Fatal("opening or completing receive automatically copied an address")
		}
	}
	// The older explicit copy endpoint still requires an accepted native view.
	a := findAccount(t, e, "ethereum-sepolia")
	ui.approve = false
	if e.CopyReceive(a.ID) == nil || ui.copied != unchanged {
		t.Fatal("cancelled native view permitted endpoint clipboard copy")
	}
	ui.approve = true
	if err := e.CopyReceive(a.ID); err != nil || ui.copied != a.Address {
		t.Fatal("explicit verified copy endpoint changed address", err)
	}
}

func TestVerifiedReceiveCopyKeepsMainnetBackupGate(t *testing.T) {
	e, base, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, base, "mainnet")
	ui := &receiveCopyUI{testUI: base}
	e.UI = ui
	a := findAccount(t, e, "ethereum-mainnet")
	if _, err := e.Receive(a.ID); err == nil || len(ui.addresses) != 0 {
		t.Fatal("mainnet receive/copy window opened before verified backup")
	}
	if e.CopyReceive(a.ID) == nil || ui.copied != "" || len(ui.addresses) != 0 {
		t.Fatal("mainnet address copied before verified backup")
	}
}

func TestVerifiedReceiveCopyCancellationAdvancesDecredOnlyOnce(t *testing.T) {
	e, base, _ := newEngine(t, chains.All())
	createImported(t, e, base, "testnet")
	ui := &receiveCopyUI{testUI: base}
	e.UI = ui
	before := findAccount(t, e, "decred-testnet3")
	expected, err := decred.AdvanceReceive(before)
	if err != nil {
		t.Fatal(err)
	}
	ui.approve = false
	if _, err := e.Receive(before.ID); err == nil {
		t.Fatal("cancelled receive window accepted")
	}
	after, err := e.account(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after.Parameters, expected.Parameters) || after.Address != expected.Address || len(ui.addresses) != 1 || ui.addresses[0] != after.Address || after.Address == before.Address {
		t.Fatal("native copy capability advanced Decred twice or displayed the previous address")
	}
	if ui.copied != "" {
		t.Fatal("cancelled view implicitly copied a Decred address")
	}
}
