package stress

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func actualScheduling(t *testing.T, r *report, seed []byte) {
	t.Helper()
	n := newStressNode(t)
	account, e := n.adapter.Derive(seed, 0, "ethereum-sepolia")
	if e != nil {
		t.Fatal(e)
	}
	n.sourceSet(account.Address)
	var times []int64
	for scenario := 0; scenario < 1000; scenario++ {
		begin := time.Now()
		if scenario%5 == 0 {
			n.modeSet("outage")
			if _, e = n.adapter.Balance(context.Background(), account); e == nil {
				t.Fatal("RPC outage became a balance")
			}
			r.Workloads["RPCOutageRejected"]++
		}
		if scenario%7 == 0 {
			n.modeSet("rate-limit")
			if _, e = n.adapter.Prepare(context.Background(), account, model.Transfer{AccountID: account.ID, Asset: "ethereum-sepolia:native", Recipient: "0x1111111111111111111111111111111111111111", Amount: "1000"}); e == nil {
				t.Fatal("rate limit accepted a preparation")
			}
			r.Workloads["RPCRateLimitRejected"]++
		}
		n.modeSet("")
		p := n.prepare(t, account, "1000")
		var mu sync.Mutex
		var ops []journal.Operation
		var wg sync.WaitGroup
		accepted := 0
		for worker := 0; worker < 16; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mu.Lock()
				defer mu.Unlock()
				if journal.Conflicts(ops, p) {
					return
				}
				ops = append(ops, journal.Operation{Plan: p, Digest: journal.Digest(p), State: "PREPARED"})
				accepted++
			}()
		}
		wg.Wait()
		if accepted != 1 || len(ops) != 1 {
			t.Fatal("concurrent reservation admitted duplicate")
		}
		op := &ops[0]
		if e = op.Transition("AWAITING_CONFIRMATION"); e != nil {
			t.Fatal(e)
		}
		signed, e := n.adapter.Sign(seed, p)
		if e != nil {
			t.Fatal(e)
		}
		op.Signed = &signed
		for _, state := range []string{"SIGNED_NOT_SUBMITTED", "SUBMITTING"} {
			if e = op.Transition(state); e != nil {
				t.Fatal(e)
			}
		}
		if scenario%3 == 0 {
			n.modeSet("accept-timeout")
		}
		receipt, submitErr := n.adapter.Submit(context.Background(), p, signed)
		if scenario%3 == 0 {
			if submitErr == nil || receipt.State != "UNKNOWN" {
				t.Fatal("lost acceptance response lost ambiguity")
			}
			r.Workloads["acceptedResponseLost"]++
		} else if submitErr != nil || receipt.State != "SUBMITTED" {
			t.Fatal(submitErr, receipt)
		}
		// Simulate coordinator restart using the actual durable public artifact.
		encoded, _ := json.Marshal(ops)
		var recovered []journal.Operation
		if e = json.Unmarshal(encoded, &recovered); e != nil {
			t.Fatal(e)
		}
		journal.Recover(recovered)
		if recovered[0].State != "UNKNOWN" || !journal.Conflicts(recovered, p) || recovered[0].Signed.Identity != signed.Identity {
			t.Fatal("restart lost signed reservation")
		}
		n.modeSet("")
		receipt, e = n.adapter.Reconcile(context.Background(), p, *recovered[0].Signed)
		if e != nil || receipt.State != "CONFIRMED" {
			t.Fatal("bounded ledger reconciliation", receipt, e)
		}
		if e = recovered[0].Transition(receipt.State); e != nil || journal.Conflicts(recovered, p) {
			t.Fatal("confirmed reservation did not release", e)
		}
		times = append(times, time.Since(begin).Microseconds())
		r.Workloads["concurrentReservationScenarios"]++
		r.Workloads["realSignedFakeNodeTransfers"]++
		r.Workloads["journalRestartReconciliation"]++
	}
	transfers, calls, credits := n.stats()
	if transfers != 1000 || credits != new(big.Int).Mul(big.NewInt(1000), big.NewInt(1000)).String() {
		t.Fatal("duplicate or missing fake ledger payments", transfers, credits)
	}
	r.Workloads["boundedFakeRPCCalls"] += calls
	r.FinalState["fakeLedgerPayments"] = transfers
	r.FinalState["reconciledPayments"] = 1000
	r.FinalState["unresolvedPayments"] = 0
	r.Latencies["concurrentSchedulingTransfer"] = summarize(times, 1_000_000)
	if r.Latencies["concurrentSchedulingTransfer"].P99Micros > 1_000_000 {
		t.Fatal(fmt.Sprintf("scheduling p99 exceeded declared1-second budget: %+v", r.Latencies["concurrentSchedulingTransfer"]))
	}
}
