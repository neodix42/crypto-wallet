package stress

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"localwallet/internal/chains"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
)

const publicStressPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
const publicStressPassword = "public synthetic stress password 79326"

// This UI supplies only public synthetic material and approvals for the bounded
// harness. Native GTK input/confirmation has a separate process/display gate.
type stressUI struct {
	approvals int
}

func (u *stressUI) Input(_ string, labels ...string) ([][]byte, error) {
	if len(labels) == 4 {
		return [][]byte{[]byte(publicStressPassword), []byte(publicStressPassword), []byte(publicStressPhrase), {}}, nil
	}
	if len(labels) == 1 {
		return [][]byte{[]byte(publicStressPassword)}, nil
	}
	return nil, fmt.Errorf("unexpected synthetic UI request")
}
func (u *stressUI) Confirm(title, _ string) bool {
	if title == "Authorize exact transfer — Go signer" {
		u.approvals++
	}
	return true
}
func (u *stressUI) File(string, bool) (string, error) {
	return "", fmt.Errorf("unexpected synthetic file dialog")
}
func (u *stressUI) Recovery(string, []byte) bool        { return true }
func (u *stressUI) Receive(string, string, []byte) bool { return true }

type lifecycle struct {
	engine    *signer.Engine
	files     *vault.Files
	dir       string
	ui        *stressUI
	node      *stressNode
	arbitrum  *stressNode
	dcr       *stressDCR
	adapters  []model.Adapter
	times     []int64
	confirmed map[string]model.Signed
}

func newLifecycle(t *testing.T, r *report) *lifecycle {
	return newScopedLifecycle(t, r, true)
}

// The retired Decred parser is optional supplementary compatibility evidence.
// It never creates an active Decred wallet or restores a signing route.
func newScopedLifecycle(t *testing.T, r *report, includeDecred bool) *lifecycle {
	t.Helper()
	dir := t.TempDir()
	f, e := vault.NewFiles(filepath.Join(dir, "wallet"))
	if e != nil {
		t.Fatal(e)
	}
	l := &lifecycle{dir: dir, files: f, ui: &stressUI{}, node: newStressNode(t), confirmed: map[string]model.Signed{}}
	l.arbitrum = newStressNodeForNetwork(t, "arbitrum-sepolia")
	combined, err := evm.NewWithEndpoints(map[string]string{"ethereum-sepolia": l.node.server.URL, "arbitrum-sepolia": l.arbitrum.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	l.adapters = []model.Adapter{combined}
	if includeDecred {
		l.dcr = newStressDCR(t)
		// Retired protocol parser remains supplementary direct-adapter evidence.
		// It must never be registered as an active wallet or signing route.
	}
	l.engine = signer.New(f, l.ui, l.adapters)
	l.engine.Environment = "testnet"
	if e = l.engine.Create("testnet", 12, true); e != nil {
		t.Fatal("production signer creation", e)
	}
	t.Cleanup(func() { l.engine.Lock(); _ = l.files.Close() })
	for _, account := range l.engine.Snapshot().Accounts {
		if chains.IsRetiredNetwork(account.Network) {
			t.Fatal("retired wallet became active during stress creation")
		}
	}
	r.Workloads["productionSignerCreate"]++
	return l
}
func (l *lifecycle) account(t *testing.T, network string) model.Account {
	t.Helper()
	for _, ac := range l.engine.Snapshot().Accounts {
		if ac.Network == network && ac.Index == 0 {
			return ac
		}
	}
	t.Fatal("synthetic account missing", network)
	return model.Account{}
}
func (l *lifecycle) restart(t *testing.T, r *report) {
	t.Helper()
	l.engine.Lock()
	if !l.engine.Snapshot().Locked {
		t.Fatal("signer did not lock")
	}
	_ = l.files.Close()
	f, e := vault.NewFiles(filepath.Join(l.dir, "wallet"))
	if e != nil {
		t.Fatal(e)
	}
	l.files = f
	l.engine = signer.New(f, l.ui, l.adapters)
	l.engine.Environment = "testnet"
	if e = l.engine.Unlock(); e != nil {
		t.Fatal("production encrypted restart", e)
	}
	r.Workloads["encryptedSignerRestartUnlock"]++
}
func (l *lifecycle) cycle(t *testing.T, r *report, cycle int) {
	t.Helper()
	begin := time.Now()
	network, node := "ethereum-sepolia", l.node
	if cycle%2 == 1 {
		network, node = "arbitrum-sepolia", l.arbitrum
	}
	ac := l.account(t, network)
	node.sourceSet(ac.Address)
	intent := model.Transfer{AccountID: ac.ID, Asset: network + ":native", Recipient: "0x1111111111111111111111111111111111111111", Amount: fmt.Sprint(1000 + cycle*2)}
	for _, mode := range []string{"outage", "rate-limit"} {
		node.modeSet(mode)
		if _, e := node.adapter.Prepare(context.Background(), ac, intent); e == nil {
			t.Fatal("failed RPC authorized a plan", mode)
		}
		r.Workloads["lifecycleRPC-"+mode]++
	}
	node.modeSet("")
	// PREPARED approval cannot survive a lock. The production unlock recovery
	// cancels it; it must never become a usable signature or a lost reservation.
	cancelled := node.prepare(t, ac, intent.Amount)
	if e := l.engine.Prepare(cancelled); e != nil {
		t.Fatal(e)
	}
	l.engine.Lock()
	before := l.ui.approvals
	if _, e := l.engine.Sign(cancelled.ID); e == nil || l.ui.approvals != before {
		t.Fatal("locked signer authorized transfer")
	}
	r.Workloads["lockedSignatureDenied"]++
	l.restart(t, r)
	snap := l.engine.Snapshot()
	found := false
	for _, op := range snap.Operations {
		if op.Plan.ID == cancelled.ID {
			found = op.State == "CANCELLED"
		}
	}
	if !found {
		t.Fatal("unconfirmed prepared operation survived restart")
	}
	p := node.prepare(t, ac, fmt.Sprint(1001+cycle*2))
	if e := l.engine.Prepare(p); e != nil {
		t.Fatal(e)
	}
	signed, e := l.engine.Sign(p.ID)
	if e != nil {
		t.Fatal("production signer transfer", e)
	}
	if e = l.engine.Update(p.ID, "SUBMITTING", nil); e != nil {
		t.Fatal(e)
	}
	node.modeSet("accept-timeout")
	receipt, e := node.adapter.Submit(context.Background(), p, signed)
	if e == nil || receipt.State != "UNKNOWN" {
		t.Fatal("submission ambiguity lost", e, receipt)
	}
	// Crash before an acknowledgement was journaled; use the reopened encrypted
	// signed artifact, not a newly derived or newly signed transaction.
	l.restart(t, r)
	pending := l.engine.Pending()
	var restored *model.Signed
	for _, op := range pending {
		if op.Plan.ID == p.ID && op.State == "UNKNOWN" {
			restored = op.Signed
		}
	}
	if restored == nil || restored.Identity != signed.Identity || !bytes.Equal(restored.Bytes, signed.Bytes) {
		t.Fatal("restart replaced signed transaction")
	}
	node.modeSet("rate-limit")
	if _, e = node.adapter.Reconcile(context.Background(), p, *restored); e == nil {
		t.Fatal("rate-limited reconciliation invented state")
	}
	node.modeSet("")
	receipt, e = node.adapter.Reconcile(context.Background(), p, *restored)
	if e != nil || receipt.State != "CONFIRMED" {
		t.Fatal("final receipt reconciliation", e, receipt)
	}
	if e = l.engine.Update(p.ID, receipt.State, &receipt); e != nil {
		t.Fatal(e)
	}
	l.confirmed[p.ID] = signed
	r.Workloads["productionSignedJournalReconciled"]++
	r.Workloads["productionSignedJournalReconciled-"+network]++
	if l.dcr != nil {
		// Historical Decred parser compatibility only: no active signer account,
		// receive-address mutation or retired-network signing capability exists.
		publicSeed := make([]byte, 64)
		for index := range publicSeed {
			publicSeed[index] = byte(index)
		}
		legacy, err := l.dcr.adapter.Derive(publicSeed, 0, "decred-simnet")
		clear(publicSeed)
		if err != nil {
			t.Fatal(err)
		}
		legacy, err = decred.SetDiscovery(legacy, 1, 5, uint32(20+cycle))
		if err != nil {
			t.Fatal(err)
		}
		legacy, err = decred.SetDiscoveryBudget(legacy, 64)
		if err != nil {
			t.Fatal(err)
		}
		l.dcr.mu.Lock()
		l.dcr.height = uint32(5 + cycle%55)
		l.dcr.mu.Unlock()
		discovery, err := l.dcr.adapter.Sync(context.Background(), legacy)
		if err != nil || discovery.ScannedFrom != 1 || discovery.AddressLimit != uint32(20+cycle) {
			t.Fatal("historical bounded parser failed", err)
		}
		r.Workloads["LegacyDCRParserScans"]++
		r.Workloads["LegacyDCRParserBlocks"] += int(discovery.Height)
	}

	if cycle < 16 {
		if e = l.engine.AddAccount(uint32(cycle + 1)); e != nil {
			t.Fatal("account index growth", e)
		}
		r.Workloads["additionalAccountGroups"]++
	}
	history, e := node.adapter.History(context.Background(), ac)
	if e != nil || len(history) == 0 {
		t.Fatal("growing history scan", e)
	}
	visible := false
	for _, item := range history {
		if item.Identity == signed.Identity && item.State == "CONFIRMED" {
			visible = true
		}
	}
	if !visible {
		t.Fatal("new payment missing from bounded history")
	}
	r.Workloads["growingHistoryScans"]++
	r.Workloads["observedHistoryItems"] += len(history)
	// Each cycle restores the known synthetic root into a new private profile
	// using production KDF settings; no original vault files are copied.
	recoveryFiles, e := vault.NewFiles(filepath.Join(l.dir, fmt.Sprintf("phrase-recovery-%d", cycle)))
	if e != nil {
		t.Fatal(e)
	}
	recovered := signer.New(recoveryFiles, &stressUI{}, l.adapters)
	recovered.Environment = "testnet"
	if e = recovered.Create("testnet", 12, true); e != nil {
		t.Fatal("production phrase recovery", e)
	}
	matched := false
	for _, candidate := range recovered.Snapshot().Accounts {
		if candidate.ID == ac.ID && candidate.Address == ac.Address && candidate.Profile == ac.Profile {
			matched = true
		}
	}
	recovered.Lock()
	if e = recoveryFiles.Close(); e != nil {
		t.Fatal(e)
	}
	if !matched {
		t.Fatal("fresh phrase recovery changed canonical wallet identity")
	}
	r.Workloads["productionSignerPhraseRecoveries"]++
	runtime.GC()
	currentRSS, _, e := rss()
	if e != nil {
		t.Fatal(e)
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	r.Checkpoints = append(r.Checkpoints, checkpoint{Cycle: cycle + 1, RSSBytes: currentRSS, HeapBytes: memory.HeapAlloc, Goroutines: runtime.NumGoroutine(), JournalRecords: len(l.engine.Snapshot().Operations)})
	l.times = append(l.times, time.Since(begin).Microseconds())
	r.Workloads["cycles"]++
}
func (l *lifecycle) reconcile(t *testing.T, r *report) {
	t.Helper()
	l.node.modeSet("")
	l.arbitrum.modeSet("")
	confirmed, cancelled := 0, 0
	for _, op := range l.engine.Snapshot().Operations {
		if op.State == "CANCELLED" {
			cancelled++
			continue
		}
		signed, ok := l.confirmed[op.Plan.ID]
		if !ok {
			t.Fatal("unaccounted signed operation", op.State)
		}
		node := l.node
		if op.Plan.Account.Network == "arbitrum-sepolia" {
			node = l.arbitrum
		}
		receipt, e := node.adapter.Reconcile(context.Background(), op.Plan, signed)
		if e != nil || receipt.State != "CONFIRMED" || op.State != "CONFIRMED" {
			t.Fatal("final operation reconciliation", e, receipt, op.State)
		}
		confirmed++
	}
	payments, calls, _ := l.node.stats()
	arbPayments, arbCalls, _ := l.arbitrum.stats()
	payments += arbPayments
	calls += arbCalls
	if confirmed != payments || confirmed != r.Workloads["cycles"] || cancelled != confirmed || l.ui.approvals != confirmed {
		t.Fatal("final ledger/journal/approval counts differ", confirmed, cancelled, payments, l.ui.approvals)
	}
	r.FinalState["lifecycleConfirmed"] = confirmed
	r.FinalState["lifecycleCancelledBeforeSignature"] = cancelled
	r.FinalState["lifecycleFakeLedgerPayments"] = payments
	r.FinalState["lifecycleUnresolved"] = 0
	r.FinalState["lifecycleNativeUIReplacedByPublicTestUI"] = 1
	r.Workloads["boundedFakeRPCCalls"] += calls
	r.Latencies["productionSignerLifecycle"] = summarize(l.times, 60_000_000)
	if r.Latencies["productionSignerLifecycle"].P99Micros > 60_000_000 {
		t.Fatal("production lifecycle p99 exceeded declared 60-second budget")
	}
}
