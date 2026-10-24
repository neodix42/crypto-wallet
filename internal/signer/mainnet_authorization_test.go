package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

type backupRevocationUI struct {
	*testUI
	afterConfirm func()
}

func (u *backupRevocationUI) Confirm(title, summary string) bool {
	approved := u.testUI.Confirm(title, summary)
	if strings.HasPrefix(title, "Authorize exact transfer") && u.afterConfirm != nil {
		u.afterConfirm()
	}
	return approved
}

func TestPreparedMainnetTransferRequiresRecordedPhraseAtEveryAuthorizationBoundary(t *testing.T) {
	for _, revokeAt := range []string{"before-sign", "during-confirmation", "during-reauthentication"} {
		t.Run(revokeAt, func(t *testing.T) {
			e, ui, _ := newEngine(t, []model.Adapter{evm.New()})
			e.Environment = "mainnet"
			createImported(t, e, ui, "mainnet")
			backupDir := filepath.Join(t.TempDir(), "backups")
			if err := os.Mkdir(backupDir, 0755); err != nil {
				t.Fatal(err)
			}
			ui.path = filepath.Join(backupDir, "before-account-change.lwv")
			if err := e.Backup(); err != nil {
				t.Fatal(err)
			}
			plan := evmPlan(findAccount(t, e, "ethereum-mainnet"))
			plan.Payload = bytes.Replace(plan.Payload, []byte(`"chainId":"11155111"`), []byte(`"chainId":"1"`), 1)
			plan.ID = ""
			plan.ID = "evm:" + journal.Digest(plan)
			if err := e.Prepare(plan); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(e.Pending())
			ui.summaries = nil
			ui.inputs = append(ui.inputs, input(syntheticPassword))
			revoke := func() {
				e.data.RecoveryConfirmed = false
				if e.Snapshot().RecoveryConfirmed {
					t.Fatal("pending recovery retained authorization")
				}
			}
			switch revokeAt {
			case "before-sign":
				revoke()
			case "during-confirmation":
				e.UI = &backupRevocationUI{testUI: ui, afterConfirm: revoke}
			case "during-reauthentication":
				e.UI = &reviewInputUI{testUI: ui, hook: func(title string) {
					if strings.HasPrefix(title, "Reauthenticate") {
						revoke()
					}
				}}
			}
			if signed, err := e.Sign(plan.ID); err == nil || signed.Identity != "" || len(signed.Bytes) != 0 {
				t.Fatal("mainnet signature escaped after verified backup was revoked")
			}
			if revokeAt == "before-sign" {
				after, _ := json.Marshal(e.Pending())
				if !bytes.Equal(before, after) || len(ui.summaries) != 0 || len(ui.inputs) != 1 {
					t.Fatal("stale mainnet backup reached native input or mutated the prepared journal")
				}
				// The same plan can reach native authorization again only after
				// authenticating a fresh backup of the changed account metadata.
				ui.path = filepath.Join(backupDir, "after-account-change.lwv")
				if err := e.Backup(); err != nil {
					t.Fatal(err)
				}
				ui.approve = false
				if signed, err := e.Sign(plan.ID); err == nil || len(signed.Bytes) != 0 || len(ui.summaries) != 1 {
					t.Fatal("fresh verified backup failed to restore native authorization")
				}
			} else {
				if len(ui.summaries) != 1 || e.Pending()[0].State != "CANCELLED" || e.Pending()[0].Signed != nil {
					t.Fatal("native backup revocation did not cancel the unsigned transfer")
				}
				if revokeAt == "during-confirmation" && len(ui.inputs) != 1 {
					t.Fatal("confirmation-time backup revocation still opened password input")
				}
				if revokeAt == "during-reauthentication" && len(ui.inputs) != 0 {
					t.Fatal("reauthentication revocation hook was not exercised")
				}
			}
		})
	}
}
