package ton

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

// These tests mutate real emulator-generated transaction BOCs as hostile RPC
// responses. A mutated trace is never presented as consensus acceptance evidence.
func receiptFixture(t *testing.T) (*Adapter, model.Plan, model.Signed, *tlb.Transaction, *tlb.Transaction) {
	t.Helper()
	a, _, p := testPlan(t)
	raw, e := os.ReadFile("../../../tests/oracles/ton/ton-emulator.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Transactions []struct {
			Data string `json:"data"`
		} `json:"transactions"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	var source, recipient *tlb.Transaction
	for _, row := range f.Transactions {
		c, e := decodeBOC(row.Data)
		if e != nil {
			t.Fatal(e)
		}
		s, e := c.BeginParse()
		if e != nil {
			t.Fatal(e)
		}
		tx := new(tlb.Transaction)
		if e = tlb.LoadFromCell(tx, s); e != nil {
			t.Fatal(e)
		}
		if tx.IO.In != nil && tx.IO.In.MsgType == tlb.MsgTypeExternalIn {
			source = tx
		} else {
			recipient = tx
		}
	}
	if source == nil || recipient == nil {
		t.Fatal("retained actual emulator trace is incomplete")
	}
	body := source.IO.In.AsExternalIn().Body
	sl, e := body.BeginParse()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = sl.LoadUInt(64); e != nil {
		t.Fatal(e)
	}
	expiry, e := sl.LoadUInt(32)
	if e != nil {
		t.Fatal(e)
	}
	p.Expires = int64(expiry)
	p.ID = planID(p)
	x, _ := payload(p)
	ext, e := external(p, x, body)
	if e != nil {
		t.Fatal(e)
	}
	return a, p, model.Signed{Identity: hex.EncodeToString(ext.Hash()), Bytes: ext.ToBOC()}, source, recipient
}
func txBOC(t *testing.T, tx *tlb.Transaction) string {
	t.Helper()
	c, e := tlb.ToCell(tx)
	if e != nil {
		t.Fatal("serialize hostile trace", e)
	}
	return base64.StdEncoding.EncodeToString(c.ToBOC())
}
func outgoing(t *testing.T, tx *tlb.Transaction) *tlb.InternalMessage {
	t.Helper()
	rows, e := tx.IO.Out.List.LoadAll()
	if e != nil || len(rows) != 1 {
		t.Fatal("fixture outputs", e)
	}
	s, e := rows[0].Value.LoadRef()
	if e != nil {
		t.Fatal(e)
	}
	var m tlb.Message
	if e = m.LoadFromCell(s); e != nil {
		t.Fatal(e)
	}
	return m.AsInternal()
}
func setOutgoing(t *testing.T, tx *tlb.Transaction, im *tlb.InternalMessage) {
	t.Helper()
	c, e := tlb.ToCell(im)
	if e != nil {
		t.Fatal(e)
	}
	setOutValue(t, tx, cell.BeginCell().MustStoreRef(c).EndCell())
}
func setOutValue(t *testing.T, tx *tlb.Transaction, value *cell.Cell) {
	t.Helper()
	d := cell.NewDict(15)
	if e := d.SetIntKey(big.NewInt(0), value); e != nil {
		t.Fatal(e)
	}
	tx.IO.Out = &tlb.MessagesList{List: d}
}
func ordinary(t *testing.T, tx *tlb.Transaction, mutate func(*tlb.TransactionDescriptionOrdinary)) {
	t.Helper()
	d, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		t.Fatal("expected actual ordinary fixture")
	}
	mutate(&d)
	tx.Description = d
}

func TestHostileExecutionAndCreditTraces(t *testing.T) {
	cases := []struct {
		name, state string
		fail        bool
		mutate      func(*testing.T, *tlb.Transaction, *tlb.Transaction)
	}{
		{"source-no-action-phase", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, s, func(d *tlb.TransactionDescriptionOrdinary) { d.ActionPhase = nil })
		}},
		{"alternative-compute-success", "CONFIRMED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, s, func(d *tlb.TransactionDescriptionOrdinary) {
				v := d.ComputePhase.Phase.(tlb.ComputePhaseVM)
				v.Details.ExitCode = 1
				d.ComputePhase.Phase = v
			})
		}},
		{"changed-bounce-policy", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.Bounce = !im.Bounce
			setOutgoing(t, s, im)
		}},
		{"changed-comment", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.Body = cell.BeginCell().EndCell()
			setOutgoing(t, s, im)
		}},
		{"foreign-source-address", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.SrcAddr = im.DstAddr
			setOutgoing(t, s, im)
		}},
		{"unexpected-recipient-deployment", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.StateInit = s.IO.In.AsExternalIn().StateInit
			setOutgoing(t, s, im)
		}},
		{"baseline", "CONFIRMED", false, func(*testing.T, *tlb.Transaction, *tlb.Transaction) {}},
		{"no-source-incoming", "UNKNOWN", false, func(t *testing.T, s, r *tlb.Transaction) { s.IO.In = nil }},
		{"other-incoming-type", "UNKNOWN", false, func(t *testing.T, s, r *tlb.Transaction) { s.IO.In = r.IO.In }},
		{"other-external-body", "UNKNOWN", false, func(t *testing.T, s, r *tlb.Transaction) { s.IO.In.AsExternalIn().Body = cell.BeginCell().EndCell() }},
		{"unknown-source-description", "UNKNOWN", false, func(t *testing.T, s, r *tlb.Transaction) {
			s.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
		}},
		{"skipped-source-compute", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, s, func(d *tlb.TransactionDescriptionOrdinary) {
				d.ComputePhase.Phase = tlb.ComputePhaseSkipped{Reason: tlb.ComputeSkipReason{Type: tlb.ComputeSkipReasonNoGas}}
			})
		}},
		{"aborted-source", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, s, func(d *tlb.TransactionDescriptionOrdinary) { d.Aborted = true })
		}},
		{"missing-source-output", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) { s.IO.Out = nil; s.OutMsgCount = 0 }},
		{"missing-action-dictionary", "", true, func(t *testing.T, s, r *tlb.Transaction) { s.IO.Out = &tlb.MessagesList{List: cell.NewDict(15)} }},
		{"no-outgoing-ref", "", true, func(t *testing.T, s, r *tlb.Transaction) { setOutValue(t, s, cell.BeginCell().EndCell()) }},
		{"malformed-outgoing-message", "", true, func(t *testing.T, s, r *tlb.Transaction) {
			setOutValue(t, s, cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell())
		}},
		{"external-outgoing-message", "", true, func(t *testing.T, s, r *tlb.Transaction) {
			c, e := s.IO.In.ToCell()
			if e != nil {
				t.Fatal(e)
			}
			setOutValue(t, s, cell.BeginCell().MustStoreRef(c).EndCell())
		}},
		{"changed-amount", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.Amount = tlb.FromNanoTONU(1)
			setOutgoing(t, s, im)
		}},
		{"changed-recipient", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.DstAddr = im.SrcAddr
			setOutgoing(t, s, im)
		}},
		{"bounced-output", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			im := outgoing(t, s)
			im.Bounced = true
			setOutgoing(t, s, im)
		}},
		{"recipient-no-incoming", "SUBMITTED", false, func(t *testing.T, s, r *tlb.Transaction) { r.IO.In = nil }},
		{"recipient-other-message-type", "SUBMITTED", false, func(t *testing.T, s, r *tlb.Transaction) { r.IO.In = s.IO.In }},
		{"recipient-unrelated-internal-message", "SUBMITTED", false, func(t *testing.T, s, r *tlb.Transaction) { r.IO.In.AsInternal().CreatedLT++ }},
		{"recipient-no-credit", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, r, func(d *tlb.TransactionDescriptionOrdinary) { d.CreditPhase = nil })
		}},
		{"recipient-short-credit", "FAILED", false, func(t *testing.T, s, r *tlb.Transaction) {
			ordinary(t, r, func(d *tlb.TransactionDescriptionOrdinary) { d.CreditPhase.Credit.Coins = tlb.FromNanoTONU(1) })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, p, s, source, recipient := receiptFixture(t)
			tc.mutate(t, source, recipient)
			sourceBOC, recipientBOC := txBOC(t, source), txBOC(t, recipient)
			n, _ := netInfo(p.Account.Network)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string `json:"method"`
					Params struct {
						Address string `json:"address"`
					} `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&q)
				var result any
				if q.Method == "getMasterchainInfo" {
					result = map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
				} else {
					data := recipientBOC
					if q.Params.Address == p.Account.Address {
						data = sourceBOC
					}
					result = []map[string]string{{"data": data}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
			}))
			defer server.Close()
			if e := a.Configure(p.Account.Network, Endpoint{URL: server.URL}); e != nil {
				t.Fatal(e)
			}
			r, e := a.Reconcile(context.Background(), p, s)
			if tc.fail {
				if e == nil {
					t.Fatal("malformed execution trace accepted", r)
				}
				return
			}
			if e != nil || r.State != tc.state {
				t.Fatalf("wanted %s: %v %+v", tc.state, e, r)
			}
		})
	}
}

func TestBounceableRecipientExecutionOutcome(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "successful-recipient"
		want := "CONFIRMED"
		if failed {
			name = "recipient-compute-failed"
			want = "FAILED"
		}
		t.Run(name, func(t *testing.T) {
			_, _, _, source, recipient := receiptFixture(t)
			a, seed, p := testPlan(t)
			x, _ := payload(p)
			x.Bounce = true
			p.Payload, _ = json.Marshal(x)
			p.ID = planID(p)
			signed, e := a.Sign(seed, p)
			if e != nil {
				t.Fatal(e)
			}
			_, external, e := a.validateSigned(p, signed)
			if e != nil {
				t.Fatal(e)
			}
			source.IO.In = &tlb.Message{MsgType: tlb.MsgTypeExternalIn, Msg: external}
			im := outgoing(t, source)
			im.Bounce = true
			setOutgoing(t, source, im)
			recipient.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: im}
			ordinary(t, recipient, func(d *tlb.TransactionDescriptionOrdinary) {
				d.Aborted = failed
				sourceDescription := source.Description.(tlb.TransactionDescriptionOrdinary)
				vm := sourceDescription.ComputePhase.Phase.(tlb.ComputePhaseVM)
				vm.Success = !failed
				if failed {
					vm.Details.ExitCode = 13
				}
				d.ComputePhase.Phase = vm
			})
			sourceData, recipientData := txBOC(t, source), txBOC(t, recipient)
			n, _ := netInfo(p.Account.Network)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					Method string `json:"method"`
					Params struct {
						Address string `json:"address"`
					} `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&q)
				var result any
				if q.Method == "getMasterchainInfo" {
					result = map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
				} else {
					data := recipientData
					if q.Params.Address == p.Account.Address {
						data = sourceData
					}
					result = []map[string]string{{"data": data}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
			}))
			defer server.Close()
			if e = a.Configure(p.Account.Network, Endpoint{URL: server.URL}); e != nil {
				t.Fatal(e)
			}
			r, e := a.Reconcile(context.Background(), p, signed)
			if e != nil || r.State != want {
				t.Fatalf("wanted %s: %v %+v", want, e, r)
			}
		})
	}
}
