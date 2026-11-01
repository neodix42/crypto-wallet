package signer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

type tonProfileConsentUI struct {
	*testUI
	after func()
}

func (u *tonProfileConsentUI) Confirm(title, summary string) bool {
	accepted := u.testUI.Confirm(title, summary)
	if u.after != nil {
		u.after()
	}
	return accepted
}

func TestTONProfileRecoveryRejectsStaleConsentWithoutWritingAnotherRoot(t *testing.T) {
	for _, mode := range []string{"new", "existing visible", "existing hidden"} {
		t.Run(mode, func(t *testing.T) {
			engine, ui, dir := newEngine(t, []model.Adapter{ton.New()})
			createImported(t, engine, ui, "mainnet")
			if mode != "new" {
				if err := engine.AddTONAccount(0, ton.Profile); err != nil {
					t.Fatal(err)
				}
				if mode == "existing hidden" {
					if err := engine.HideAccount(engine.Snapshot().Accounts[1].ID, true); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
			if err != nil {
				t.Fatal(err)
			}
			engine.UI = &tonProfileConsentUI{testUI: ui, after: engine.Lock}
			if err := engine.AddTONAccount(0, ton.Profile); err == nil || !engine.Snapshot().Locked {
				t.Fatal("consent survived locking its original root")
			}
			after, err := os.ReadFile(filepath.Join(dir, "vault.lwv"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("stale consent rewrote the encrypted generation", err)
			}
		})
	}
}

func TestTONProfileRecoveryRejectsMetadataChangedDuringConsent(t *testing.T) {
	engine, ui, _ := newEngine(t, []model.Adapter{ton.New()})
	createImported(t, engine, ui, "mainnet")
	original := engine.Snapshot().Accounts[0]
	engine.UI = &tonProfileConsentUI{testUI: ui, after: func() {
		if err := engine.HideAccount(original.ID, true); err != nil {
			t.Fatal(err)
		}
	}}
	if err := engine.AddTONAccount(0, ton.Profile); err == nil {
		t.Fatal("an older approval survived another metadata generation")
	}
	accounts := engine.Snapshot().Accounts
	if len(accounts) != 1 || accounts[0].ID != original.ID || !accounts[0].Hidden {
		t.Fatal("stale profile approval changed unrelated identities")
	}
}
