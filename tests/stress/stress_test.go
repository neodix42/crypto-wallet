package stress

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

var soak = flag.Duration("soak", 0, "explicit deterministic soak duration; production gate requires 24 hours")

type report struct {
	ExecutionStatus                string         `json:"executionStatus"`
	Instrumentation                string         `json:"instrumentation"`
	CPUModel                       string         `json:"cpuModel"`
	Checkpoints                    []checkpoint   `json:"settledCheckpoints"`
	Status                         string         `json:"status"`
	Seed                           string         `json:"seed"`
	Go                             string         `json:"go"`
	OS                             string         `json:"os"`
	CPUs                           int            `json:"cpus"`
	Workloads                      map[string]int `json:"workloads"`
	Elapsed                        float64        `json:"elapsedSeconds"`
	RequestedSeconds               float64        `json:"requestedSeconds,omitempty"`
	Campaign24hStatus              string         `json:"campaign24hStatus,omitempty"`
	P50Micros                      int64          `json:"p50Micros"`
	P99Micros                      int64          `json:"p99Micros"`
	RSSBudgetMiB                   int            `json:"rssBudgetMiB"`
	GoMemoryLimitMiB               int            `json:"goMemoryLimitMiB"`
	StartHeap, EndHeap             uint64
	StartGoroutines, EndGoroutines int
	StartRSSBytes                  uint64             `json:"startRSSBytes"`
	EndRSSBytes                    uint64             `json:"endRSSBytes"`
	PeakObservedRSSBytes           uint64             `json:"peakObservedRSSBytes"`
	KernelHighWaterRSSBytes        uint64             `json:"kernelHighWaterRSSBytes"`
	ResourceSamples                int                `json:"resourceSamples"`
	PeakGoroutines                 int                `json:"peakGoroutines"`
	Latencies                      map[string]latency `json:"latencies"`
	FinalState                     map[string]int     `json:"finalState"`
	Scope                          string             `json:"scope"`
}

func save(t *testing.T, name string, r report) {
	t.Helper()
	dir := filepath.Join("..", "..", "reports")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, name+".json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log(string(b))
}
func beginReport(t *testing.T, name string) *report {
	t.Helper()
	if raceInstrumentation {
		name += "-race"
	}
	previousLimit := debug.SetMemoryLimit(768 << 20)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	r := &report{Status: "PASS", Seed: "PCG(0x79326,0x16482); public deterministic BIP39 fixtures; cryptographic RNG is not replaced", Go: runtime.Version(), OS: runtime.GOOS + "/" + runtime.GOARCH, CPUs: runtime.NumCPU(), RSSBudgetMiB: 1024, GoMemoryLimitMiB: 768, Workloads: map[string]int{}, Latencies: map[string]latency{}, FinalState: map[string]int{}, StartHeap: before.HeapAlloc, StartGoroutines: runtime.NumGoroutine()}
	r.Instrumentation = "ordinary Go test"
	r.CPUModel = cpuModel()
	if raceInstrumentation {
		r.Instrumentation = "Go race detector; separate 3 GiB instrumentation budget, ordinary target remains 1 GiB"
		r.RSSBudgetMiB = 3072
	}
	start := time.Now()
	monitor := watchResources()
	// Registered before fixture cleanup so resources are measured after every
	// fake-node client/server and production vault file has actually closed.
	t.Cleanup(func() {
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		r.EndHeap = after.HeapAlloc
		if e := monitor.finish(r); e != nil {
			t.Error("kernel resource measurement unavailable", e)
		}
		r.EndGoroutines = runtime.NumGoroutine()
		r.Elapsed = time.Since(start).Seconds()
		if r.EndHeap > 512<<20 || r.EndGoroutines > r.StartGoroutines+8 || r.KernelHighWaterRSSBytes > uint64(r.RSSBudgetMiB)<<20 {
			t.Error("declared heap/RSS/goroutine budget exceeded")
		}
		for workload, value := range r.Latencies {
			if value.Count == 0 || value.P99Micros > value.BudgetP99Micros {
				t.Error("declared latency budget exceeded", workload, value)
			}
		}
		r.ExecutionStatus = "PASS"
		if t.Failed() {
			r.Status = "FAIL"
			r.ExecutionStatus = "FAIL"
		}
		save(t, name, *r)
		debug.SetMemoryLimit(previousLimit)
	})
	return r
}
func TestTP071BoundedStress(t *testing.T) {
	// Before execution: RSS<=1024MiB; Go memory limit768MiB; post-GC heap512MiB;
	// <=8 residual goroutines;16 scheduling workers; parserp99<10ms,
	// schedulingp99<1s; production lifecyclep99<60s; KDF reopenp99<15s.
	r := beginReport(t, "stress")
	r.Scope = "Production IPC/amount/address parsers for every registered testnet family; real EVM encoding/signature/verification against bounded fake ledger; actual signer encrypted persistence/lock/restart/backup/discovery/history. Public synthetic test UI replaces GTK; fake node ledger is not consensus acceptance or packaged desktop stress."
	rng := rand.New(rand.NewPCG(0x79326, 0x16482))
	latencies := make([]int64, 0, 100000)
	for i := 0; i < 100000; i++ {
		begin := time.Now()
		n := rng.IntN(128)
		b := make([]byte, n+4)
		binary.BigEndian.PutUint32(b[:4], uint32(rng.IntN(1<<22)))
		for j := range b[4:] {
			b[4+j] = byte(rng.IntN(256))
		}
		c := ipc.Channel{R: bytes.NewReader(b)}
		_, _ = c.Read()
		latencies = append(latencies, time.Since(begin).Microseconds())
		r.Workloads["malformedIPC"]++
	}
	r.Latencies["boundedIPCParser"] = summarize(latencies, 10000)
	r.P50Micros = r.Latencies["boundedIPCParser"].P50Micros
	r.P99Micros = r.Latencies["boundedIPCParser"].P99Micros
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i)
	}
	defer clear(seed)
	type target struct {
		adapter model.Adapter
		account model.Account
	}
	var targets []target
	for _, a := range chains.All() {
		for _, network := range a.Networks() {
			if !network.Testnet {
				continue
			}
			ac, e := a.Derive(seed, 0, network.ID)
			if e != nil {
				t.Fatal(e)
			}
			targets = append(targets, target{a, ac})
		}
	}
	for i := 0; i < 10000; i++ {
		entry := targets[rng.IntN(len(targets))]
		if e := entry.adapter.ValidateAddress(entry.account.Network, entry.account.Address); e != nil {
			t.Fatal("derived address rejected", entry.account.Network, e)
		}
		if e := entry.adapter.ValidateAddress(entry.account.Network, "https://example.invalid/pay?to="+entry.account.Address); e == nil {
			t.Fatal("URI accepted as plain address", entry.account.Network)
		}
		s := fmt.Sprintf("%d.%08d", rng.IntN(1000000), rng.IntN(100000000))
		if i%3 == 0 {
			s += "e7"
		}
		out, e := amount.Parse(s, 8)
		if e == nil {
			if _, e = amount.Atomic(out); e != nil {
				t.Fatal("atomic parse disagreement")
			}
		}
		var obj struct {
			Address string `json:"address"`
		}
		if strictjson.Decode([]byte(`{"address":"`+s+`","address":"duplicate"}`), &obj) == nil {
			t.Fatal("duplicate metadata accepted")
		}
		r.Workloads["mixedAddressAmountURIAndHostileMetadata"]++
	}
	r.Workloads["testnetAddressProfiles"] = len(targets)
	actualScheduling(t, r, seed)
	plain := []byte(`{"publicSyntheticRecoveryCanary":"no-user-secrets"}`)
	password := []byte(publicStressPassword)
	var kdfTimes []int64
	for i := 0; i < 8; i++ {
		begin := time.Now()
		encrypted, e := vault.Seal(plain, password, vault.Default)
		if e != nil {
			t.Fatal(e)
		}
		opened, e := vault.Open(encrypted, password)
		if e != nil || !bytes.Equal(opened, plain) {
			t.Fatal("production KDF reopen failed", e)
		}
		clear(opened)
		kdfTimes = append(kdfTimes, time.Since(begin).Microseconds())
		r.Workloads["productionKDFBackupReopen"]++
	}
	clear(password)
	r.Latencies["productionKDFSealReopen"] = summarize(kdfTimes, 15_000_000)
	l := newLifecycle(t, r)
	for cycle := 0; cycle < 2; cycle++ {
		l.cycle(t, r, cycle)
	}
	l.reconcile(t, r)
}
func TestTP072Soak(t *testing.T) {
	if *soak == 0 {
		t.Skip("NOT_RUN: invoke make test-soak for explicit 24-hour campaign")
	}
	if *soak < 0 {
		t.Fatal("soak duration must be positive")
	}
	r := beginReport(t, "soak")
	r.Seed = "public BIP39 abandon/about fixture; scenario ordinal modulo 55; fixed 30-second cadence; cryptographic RNG is not replaced"
	r.Scope = "Actual signer production 256 MiB Argon2id encrypted persistence, lock/restart/backup; actual adapter RPC rejection, real signed transaction verification, bounded account discovery and growing history against fake clients. Scenario choice deterministic; no public endpoint, live funds or GTK automation. Not a packaged desktop or public-testnet soak."
	r.RequestedSeconds = soak.Seconds()
	r.Campaign24hStatus = "NOT_RUN"
	if *soak < 24*time.Hour {
		r.Status = "NOT_RUN"
	}
	l := newLifecycle(t, r)
	start := time.Now()
	for cycle := 0; time.Since(start) < *soak; cycle++ {
		l.cycle(t, r, cycle)
		if cycle >= 4998 {
			t.Fatal("bounded journal campaign reached 10000-operation limit")
		}
		// Fixed 30-second cadence keeps a 24h run below the production 10000-record
		// journal bound while preserving accumulated history for reconciliation.
		next := start.Add(time.Duration(cycle+1) * 30 * time.Second)
		if end := start.Add(*soak); next.After(end) {
			next = end
		}
		if wait := time.Until(next); wait > 0 {
			time.Sleep(wait)
		}
	}
	l.reconcile(t, r)
	if *soak >= 24*time.Hour && time.Since(start) >= 24*time.Hour {
		r.Campaign24hStatus = "PASS"
	}
}
