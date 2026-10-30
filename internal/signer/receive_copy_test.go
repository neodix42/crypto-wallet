package signer

import (
	"bytes"
	"testing"

	"localwallet/internal/chains"
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
	// Receive remains an explicit verified view. The public Copy icon instead
	// copies the displayed account address directly, without another dialog.
	a := findAccount(t, e, "ethereum-sepolia")
	ui.approve = false
	if _, err := e.Receive(a.ID); err == nil || ui.copied != unchanged {
		t.Fatal("cancelled receive view copied an address")
	}
	views := len(ui.addresses)
	if err := e.CopyReceive(a.ID); err != nil || ui.copied != a.Address {
		t.Fatal("explicit public copy endpoint changed address", err)
	}
	if len(ui.addresses) != views {
		t.Fatal("public copy opened a second receive window")
	}
}

func TestVerifiedReceiveCopyKeepsRecoveryGate(t *testing.T) {
	e, base, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, base, "mainnet")
	e.data.RecoveryConfirmed = false
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

func TestRetiredDecredReceiveAndCopyNeverPromptOrAdvance(t *testing.T) {
	e, base, _ := newEngine(t, chains.All())
	createImported(t, e, base, "testnet")
	account := legacyDecredAccount(t, e, "decred-testnet3", 0)
	ui := &receiveCopyUI{testUI: base}
	e.UI = ui
	before := append([]byte(nil), account.Parameters...)
	if _, err := e.Receive(account.ID); err == nil || e.CopyReceive(account.ID) == nil {
		t.Fatal("retired receive/copy accepted")
	}
	if len(ui.addresses) != 0 || ui.copied != "" || !bytes.Equal(e.data.Accounts[len(e.data.Accounts)-1].Parameters, before) {
		t.Fatal("retired view advanced address or copied clipboard")
	}
}
