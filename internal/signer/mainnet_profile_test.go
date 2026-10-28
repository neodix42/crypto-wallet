package signer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/chains/evm"
	"localwallet/internal/journal"
)

// No mainnet RPC or broadcast is involved. This exercises the real mainnet
// descriptors, encrypted backup gate, native decision boundary and recovery.
func TestMainnetRootAllFamiliesBackupAndNativeAuthorization(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	e.Environment = "mainnet"
	if s := e.Snapshot(); !s.Locked || s.Environment != "mainnet" || s.WalletExists || len(s.Accounts) != 0 {
		t.Fatalf("locked first-run profile metadata: %+v", s)
	}
	pending := false
	ui.recoveryApprove = &pending
	ui.inputs = append(ui.inputs, input(syntheticPassword, syntheticPassword))
	if err := e.Create("mainnet", 12, false); err != nil {
		t.Fatal(err)
	}
	initial := e.Snapshot()
	if initial.Environment != "mainnet" || initial.RecoveryConfirmed || len(initial.Accounts) != 8 {
		t.Fatal("mainnet initial account coverage or backup gate missing")
	}
	families := map[string]bool{}
	for _, ac := range initial.Accounts {
		families[ac.Family] = true
		if ac.Index != 0 || !strings.HasSuffix(ac.Network, "-mainnet") {
			t.Fatal("mainnet root mixed account index or environment", ac.ID)
		}
		if _, err := e.Receive(ac.ID); err == nil {
			t.Fatal("mainnet receive bypassed verified-backup gate", ac.Network)
		}
		if err := e.CopyReceive(ac.ID); err == nil || ui.copied != "" {
			t.Fatal("mainnet copy bypassed verified-backup gate", ac.Network)
		}
	}
	if len(families) != 6 || len(ui.summaries) != 0 {
		t.Fatal("mainnet family omitted or pre-backup receive reached native display")
	}
	ac := findAccount(t, e, "ethereum-mainnet")
	plan := evmPlan(ac)
	plan.Payload = bytes.Replace(plan.Payload, []byte(`"chainId":"11155111"`), []byte(`"chainId":"1"`), 1)
	plan.ID = ""
	plan.ID = "evm:" + journal.Digest(plan)
	if evm.New().ValidatePlan(plan) != nil {
		t.Fatal("offline mainnet protocol plan invalid")
	}
	if err := e.Prepare(plan); err == nil || len(e.Pending()) != 0 {
		t.Fatal("mainnet send bypassed verified-backup gate")
	}
	backupDir := filepath.Join(t.TempDir(), "ordinary-backups")
	if err := os.Mkdir(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	ui.path = filepath.Join(backupDir, "synthetic-mainnet.lwv")
	if err := e.Backup(); err != nil || !e.Snapshot().RecoveryConfirmed {
		t.Fatal("mainnet portable backup not authenticated", err)
	}
	if _, err := e.Receive(ac.ID); err != nil {
		t.Fatal("verified mainnet receive failed", err)
	}
	if err := e.Prepare(plan); err != nil {
		t.Fatal("verified mainnet plan rejected", err)
	}
	ui.approve = false
	if signed, err := e.Sign(plan.ID); err == nil || signed.Identity != "" || len(signed.Bytes) != 0 {
		t.Fatal("native rejection released a mainnet signature")
	}
	lastSummary := ui.summaries[len(ui.summaries)-1]
	if !strings.Contains(lastSummary, "Network: ethereum-mainnet") || !strings.Contains(lastSummary, plan.Recipient) || !strings.Contains(lastSummary, plan.Amount) || len(ui.inputs) != 0 {
		t.Fatal("mainnet native approval omitted immutable transaction effects")
	}
	if operations := e.Pending(); len(operations) != 1 || operations[0].State != "CANCELLED" || operations[0].Signed != nil {
		t.Fatal("native mainnet cancellation did not persist unsigned state")
	}
	restored, restoreUI, _ := newEngine(t, chains.All())
	restored.Environment = "mainnet"
	restoreUI.path = ui.path
	restoreUI.inputs = append(restoreUI.inputs, input(syntheticPassword))
	if err := restored.Restore(); err != nil {
		t.Fatal("mainnet all-family recovery failed", err)
	}
	before, _ := json.Marshal(initial.Accounts)
	after, _ := json.Marshal(restored.Snapshot().Accounts)
	if !bytes.Equal(before, after) || !restored.Snapshot().RecoveryConfirmed {
		t.Fatal("mainnet recovery changed deterministic descriptors or verified state")
	}
	restored.Lock()
	if s := restored.Snapshot(); !s.Locked || s.Environment != "mainnet" || len(s.Accounts) != 0 || len(s.Operations) != 0 {
		t.Fatal("locked mainnet snapshot omitted environment or retained public account state")
	}
}

func TestMainnetRPCConfigurationEnforcesProfileAndAuthenticatedDCR(t *testing.T) {
	e, ui, _ := newEngine(t, chains.All())
	e.Environment = "mainnet"
	createImported(t, e, ui, "mainnet")
	ui.summaries = nil
	for _, network := range []string{"ethereum-sepolia", "near-localnet", "aptos-localnet", "decred-simnet", "ton-testnet"} {
		ui.inputs = append(ui.inputs, input("https://rpc.example.org", "", "", ""))
		if err := e.Configure(network); err == nil || len(ui.inputs) != 1 || len(e.Settings()) != 0 {
			t.Fatal("mainnet profile accepted foreign network or opened credential input", network)
		}
		ui.inputs = nil
	}
	for _, values := range [][]string{{"https://dcr.example.org", "", "", ""}, {"http://127.0.0.1:9109", "public-user", "public-node-password", ""}} {
		ui.inputs = append(ui.inputs, input(values...))
		if err := e.Configure("decred-mainnet"); err == nil || len(e.Settings()) != 0 || len(ui.summaries) != 0 {
			t.Fatal("mainnet dcrd accepted absent authentication or plaintext transport")
		}
	}
	ui.inputs = append(ui.inputs, input("https://dcr.example.org/PUBLIC-PATH-TOKEN", "public-user", "public-node-password", ""))
	if err := e.Configure("decred-mainnet"); err != nil {
		t.Fatal("authenticated mainnet node settings failed", err)
	}
	if len(e.Settings()) != 1 || e.Settings()["decred-mainnet"].User != "public-user" {
		t.Fatal("authenticated mainnet settings not stored in encrypted profile")
	}
	public, _ := json.Marshal(e.Snapshot())
	for _, credential := range []string{"PUBLIC-PATH-TOKEN", "public-user", "public-node-password"} {
		if bytes.Contains(public, []byte(credential)) {
			t.Fatal("RPC credential appeared in public snapshot")
		}
	}
}
