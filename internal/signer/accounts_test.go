package signer

import (
	"bytes"
	"encoding/json"
	"localwallet/internal/chains"
	"os"
	"path/filepath"
	"testing"
)

func TestTP032OlderBackupManualRangeAndHiddenRecovery(t *testing.T) {
	e, u, _ := newEngine(t, chains.All())
	if e.DiscoverAccounts(0, 1) == nil || e.HideAccount("missing", true) == nil {
		t.Fatal("locked account mutation")
	}
	createImported(t, e, u, "testnet")
	original := e.Snapshot().Accounts
	for _, r := range [][2]uint32{{2, 1}, {0, 20}, {999999, 1000000}} {
		if e.DiscoverAccounts(r[0], r[1]) == nil {
			t.Fatal("unbounded discovery")
		}
	}
	u.approve = false
	if e.DiscoverAccounts(1, 2) == nil || len(e.Snapshot().Accounts) != len(original) {
		t.Fatal("unapproved discovery")
	}
	u.approve = true
	if e.DiscoverAccounts(0, 2) != nil {
		t.Fatal("bounded discovery")
	}
	if len(e.Snapshot().Accounts) != 3*len(original) {
		t.Fatal("missing deterministic groups")
	}
	if e.DiscoverAccounts(0, 2) == nil {
		t.Fatal("duplicate discovery succeeded")
	}
	a := original[0]
	if e.HideAccount("missing", true) == nil || e.HideAccount(a.ID, true) != nil || e.HideAccount(a.ID, true) != nil {
		t.Fatal("hide lifecycle")
	}
	hidden := e.Snapshot().Accounts[0]
	hidden.Hidden = false
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(hidden)
	if !bytes.Equal(x, y) {
		t.Fatal("hiding changed recovery identity")
	}
	folder := filepath.Join(t.TempDir(), "backup")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	u.path = filepath.Join(folder, "range.lwv")
	if e.Backup() != nil {
		t.Fatal("backup range")
	}
	fresh, v, _ := newEngine(t, chains.All())
	v.path = u.path
	v.inputs = append(v.inputs, input(syntheticPassword))
	if fresh.Restore() != nil {
		t.Fatal("restore range")
	}
	if !fresh.Snapshot().Accounts[0].Hidden || len(fresh.Snapshot().Accounts) != 3*len(original) {
		t.Fatal("hidden/range not durable")
	}
	if fresh.HideAccount(a.ID, false) != nil || fresh.Snapshot().Accounts[0].Hidden || fresh.Snapshot().Accounts[0].Index != a.Index {
		t.Fatal("unhide renumbered account")
	}
}

func TestRetiredDecredDiscoveryBudgetRemainsDurableAndInactive(t *testing.T) {
	e, u, _ := newEngine(t, chains.All())
	createImported(t, e, u, "testnet")
	account := legacyDecredAccount(t, e, "decred-testnet3", 0)
	before := append([]byte(nil), account.Parameters...)
	for _, args := range [][]uint32{{2000001}, {1, 2}, {12345}} {
		if e.Discovery(account.ID, 123, 20, 100, args...) == nil {
			t.Fatal("retired discovery budget accepted")
		}
	}
	if !bytes.Equal(before, e.data.Accounts[len(e.data.Accounts)-1].Parameters) {
		t.Fatal("retired discovery budget mutated")
	}
	if err := e.persist(); err != nil {
		t.Fatal(err)
	}
	e.Lock()
	u.inputs = append(u.inputs, input(syntheticPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PublicAccount(account.ID); err == nil {
		t.Fatal("retired discovery account exposed")
	}
	for _, historical := range e.data.Accounts {
		if historical.ID == account.ID && !bytes.Equal(before, historical.Parameters) {
			t.Fatal("retired budget changed on reopen")
		}
	}
}
