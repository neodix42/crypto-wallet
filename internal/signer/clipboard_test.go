package signer

import (
	"bytes"
	"errors"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

type publicCopyUI struct {
	*testUI
	writes, receives int
	failure          error
}

func (u *publicCopyUI) CopyPublic(address string) error {
	u.writes++
	if u.failure != nil {
		return u.failure
	}
	u.copied = address
	return nil
}

func (u *publicCopyUI) Receive(string, string, []byte) bool {
	u.receives++
	return false
}

func TestCopyReceiveCopiesShownOwnedAddressWithoutPromptOrVaultWrite(t *testing.T) {
	e, original, _ := newEngine(t, chains.All())
	createImported(t, e, original, "mainnet")
	ui := &publicCopyUI{testUI: original}
	e.UI = ui
	before, err := e.Files.Read("vault.lwv")
	if err != nil {
		t.Fatal(err)
	}
	generation := e.data.Generation
	accounts := append([]model.Account(nil), e.data.Accounts...)
	if len(accounts) != 8 {
		t.Fatal("fixture lost supported network coverage")
	}
	for _, account := range accounts {
		if err = e.CopyReceive(account.ID); err != nil || ui.copied != account.Address {
			t.Fatal("copy did not return the exact displayed owned address", account.Network, err)
		}
		current, err := e.account(account.ID)
		if err != nil || current.Address != account.Address {
			t.Fatal("copy advanced the displayed receive address", account.Network)
		}
	}
	after, err := e.Files.Read("vault.lwv")
	if err != nil || !bytes.Equal(before, after) || e.data.Generation != generation || ui.receives != 0 || ui.writes != len(accounts) {
		t.Fatal("public address copy opened a prompt or changed encrypted state")
	}
}

func TestCopyReceiveRejectsUnavailableOrInvalidAddressAndReportsClipboardFailure(t *testing.T) {
	e, original, _ := newEngine(t, []model.Adapter{evm.New()})
	createImported(t, e, original, "mainnet")
	account := findAccount(t, e, "ethereum-mainnet")
	ui := &publicCopyUI{testUI: original}
	e.UI = ui
	if err := e.CopyReceive("foreign"); err == nil || ui.writes != 0 {
		t.Fatal("unknown account obtained clipboard authority")
	}
	e.data.RecoveryConfirmed = false
	if err := e.CopyReceive(account.ID); err == nil || ui.writes != 0 {
		t.Fatal("unconfirmed root obtained clipboard authority")
	}
	e.data.RecoveryConfirmed = true
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == account.ID {
			e.data.Accounts[i].Address = "invalid-address"
		}
	}
	if err := e.CopyReceive(account.ID); err == nil || ui.writes != 0 {
		t.Fatal("invalid address reached the clipboard")
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == account.ID {
			e.data.Accounts[i].Address = account.Address
		}
	}
	ui.failure = errors.New("synthetic clipboard unavailable")
	if err := e.CopyReceive(account.ID); !errors.Is(err, ui.failure) || ui.copied != "" || ui.writes != 1 || ui.receives != 0 {
		t.Fatal("clipboard failure was discarded or opened a second prompt")
	}
	e.Lock()
	if err := e.CopyReceive(account.ID); err == nil || ui.writes != 1 {
		t.Fatal("locked wallet obtained clipboard authority")
	}
}
