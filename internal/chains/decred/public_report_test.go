package decred

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"localwallet/internal/strictjson"
)

func TestPublicDiscoveryAtomicStringsAndPrivateProofBoundary(t *testing.T) {
	r := SyncReport{Height: 20, HighestReceive: -1, ImmatureAtoms: math.MaxInt64, Spendable: []Input{{TxID: "public-id", Value: math.MaxInt64, Previous: "public-proof-must-stay-in-Go", Script: "public-script-proof"}}, ImmatureOutputs: []ImmatureOutput{{Atoms: math.MaxInt64, RemainingBlocks: 1, Reason: "transaction-expiry"}}, History: []HistoryItem{{Received: math.MaxInt64, Spent: 1}}}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var decoded any
	if e = strictjson.Decode(b, &decoded); e != nil {
		t.Fatal("duplicate or malformed public DTO", e)
	}
	m := decoded.(map[string]any)
	if m["immatureAtoms"] != "9223372036854775807" || m["spendable"].([]any)[0].(map[string]any)["value"] != "9223372036854775807" || m["immatureOutputs"].([]any)[0].(map[string]any)["atoms"] != "9223372036854775807" || m["history"].([]any)[0].(map[string]any)["received"] != "9223372036854775807" || m["history"].([]any)[0].(map[string]any)["spent"] != "1" {
		t.Fatal("atomic quantity crossed JSON as numeric", string(b))
	}
	if strings.Contains(string(b), "must-stay-in-Go") || strings.Contains(string(b), "script-proof") {
		t.Fatal("creation proof leaked into public report")
	}
	if _, ok := m["height"].(float64); !ok {
		t.Fatal("bounded height is not numeric")
	}
	// Protocol spend plans retain their original numeric Go-only payload types.
	_, _, p := testPlan(t)
	var protocol map[string]any
	if e = json.Unmarshal(p.Payload, &protocol); e != nil {
		t.Fatal(e)
	}
	if _, ok := protocol["inputs"].([]any)[0].(map[string]any)["value"].(float64); !ok {
		t.Fatal("public DTO changed protocol payload")
	}
}
func TestPublicReportDoesNotRepeatLargeCreationTransactions(t *testing.T) {
	proof := strings.Repeat("00", 1<<19)
	r := SyncReport{}
	for i := 0; i < 10000; i++ {
		r.Spendable = append(r.Spendable, Input{TxID: strings.Repeat("a", 64), Vout: uint32(i), Value: 1, Previous: proof})
	}
	b, e := json.Marshal(r)
	if e != nil || len(b) > 3<<20 || strings.Contains(string(b), "previous") {
		t.Fatal("public report repeated large shared creation proofs", len(b), e)
	}
}
