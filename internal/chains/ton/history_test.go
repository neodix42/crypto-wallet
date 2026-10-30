package ton

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

func historyRPC(t *testing.T, a *Adapter, network string, rows []string, mode string) func() {
	t.Helper()
	n, _ := netInfo(network)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		if q.Method == "getMasterchainInfo" {
			root := n.root
			if mode == "wrong-network" {
				root = "wrong"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "ok": true, "result": map[string]any{"init": map[string]any{"root_hash": root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}})
			return
		}
		if mode == "outage" {
			http.Error(w, "public synthetic outage", 503)
			return
		}
		if q.Method != "getTransactions" || q.Params["limit"] != float64(100) || q.Params["archival"] != true {
			t.Error("unexpected history request", q.Method, q.Params)
		}
		entries := []map[string]string{}
		for _, data := range rows {
			entries = append(entries, map[string]string{"data": data})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "ok": true, "result": entries})
	}))
	if e := a.Configure(network, Endpoint{URL: s.URL}); e != nil {
		t.Fatal(e)
	}
	return s.Close
}
func cloneHistory(t *testing.T, tx *tlb.Transaction) *tlb.Transaction {
	t.Helper()
	c, e := decodeBOC(txBOC(t, tx))
	if e != nil {
		t.Fatal(e)
	}
	s, e := c.BeginParse()
	if e != nil {
		t.Fatal(e)
	}
	out := new(tlb.Transaction)
	if e = tlb.LoadFromCell(out, s); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestTONHistoryUsesActualEmulatorObservations(t *testing.T) {
	a, p, _, source, recipient := receiptFixture(t)
	for _, tc := range []struct {
		name  string
		tx    *tlb.Transaction
		index uint32
		want  string
	}{{"source", source, 0, "native outbound message123456789"}, {"recipient", recipient, 1, "account native credit phase123456789"}} {
		t.Run(tc.name, func(t *testing.T) {
			_, seed, _ := testPlan(t)
			ac, e := a.DeriveProfile(seed, tc.index, p.Account.Network, Profile)
			if e != nil {
				t.Fatal(e)
			}
			stop := historyRPC(t, a, ac.Network, []string{txBOC(t, tc.tx)}, "")
			defer stop()
			out, e := a.History(context.Background(), ac)
			if e != nil || len(out) != 1 || out[0].State != "OBSERVED" || out[0].Block != "" || !strings.Contains(strings.ReplaceAll(out[0].Detail, " ", ""), strings.ReplaceAll(tc.want, " ", "")) {
				t.Fatal("actual trace summary", out, e)
			}
			if !strings.Contains(out[0].Detail, "not recipient-payment confirmation") {
				t.Fatal("history claimed payment confirmation")
			}
			if out[0].Timestamp != int64(tc.tx.Now) || out[0].Explorer == "" || len(out[0].Transfers) != 1 || out[0].Transfers[0].Source == "" || out[0].Transfers[0].Destination == "" || out[0].Transfers[0].Atomic != "123456789" || out[0].Transfers[0].Asset.Decimals != 9 || out[0].Transfers[0].Comment != p.Memo {
				t.Fatal("TON structured history changed", out[0])
			}
		})
	}
	if !strings.Contains(a.HistoryScope(), "100") || !strings.Contains(a.HistoryScope(), "jettons") {
		t.Fatal("history bounds not disclosed")
	}
}
func TestTONHistoryRejectsHostileRPCAndMessageBindings(t *testing.T) {
	cases := []struct {
		name, mode string
		mutate     func(*testing.T, *tlb.Transaction, *tlb.Transaction) []*tlb.Transaction
	}{
		{"zero-logical-time", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction { s.LT = 0; return []*tlb.Transaction{s} }},
		{"duplicate-transaction", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction { return []*tlb.Transaction{s, s} }},
		{"duplicate-logical-time", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			c := cloneHistory(t, s)
			c.Now++
			return []*tlb.Transaction{s, c}
		}},
		{"wrong-account", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction { return []*tlb.Transaction{r} }},
		{"network-identity", "wrong-network", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction { return []*tlb.Transaction{s} }},
		{"outage", "outage", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction { return []*tlb.Transaction{s} }},
		{"too-many-messages", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			s.OutMsgCount = 257
			return []*tlb.Transaction{s}
		}},
		{"inbound-account-mismatch", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			s.IO.In = r.IO.In
			return []*tlb.Transaction{s}
		}},
		{"inbound-source-none", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			im := r.IO.In.AsInternal()
			im.SrcAddr = address.NewAddressNone()
			im.DstAddr = s.IO.In.AsExternalIn().DstAddr
			s.IO.In = r.IO.In
			return []*tlb.Transaction{s}
		}},
		{"missing-outgoing-dictionary", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			s.IO.Out = nil
			return []*tlb.Transaction{s}
		}},
		{"mismatched-message-count", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			s.OutMsgCount = 0
			return []*tlb.Transaction{s}
		}},
		{"malformed-dictionary", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			s.IO.Out.List = cell.BeginCell().EndCell().AsDict(15)
			return []*tlb.Transaction{s}
		}},
		{"missing-message-reference", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			setOutValue(t, s, cell.BeginCell().EndCell())
			return []*tlb.Transaction{s}
		}},
		{"trailing-message-value", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			c, e := tlb.ToCell(outgoing(t, s))
			if e != nil {
				t.Fatal(e)
			}
			setOutValue(t, s, cell.BeginCell().MustStoreBoolBit(true).MustStoreRef(c).EndCell())
			return []*tlb.Transaction{s}
		}},
		{"malformed-message", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			setOutValue(t, s, cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).EndCell())
			return []*tlb.Transaction{s}
		}},
		{"external-in-as-outgoing", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			c, e := s.IO.In.ToCell()
			if e != nil {
				t.Fatal(e)
			}
			setOutValue(t, s, cell.BeginCell().MustStoreRef(c).EndCell())
			return []*tlb.Transaction{s}
		}},
		{"outgoing-source-mismatch", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			im := outgoing(t, s)
			im.SrcAddr = im.DstAddr
			setOutgoing(t, s, im)
			return []*tlb.Transaction{s}
		}},
		{"outgoing-destination-none", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			im := outgoing(t, s)
			im.DstAddr = address.NewAddressNone()
			setOutgoing(t, s, im)
			return []*tlb.Transaction{s}
		}},
		{"external-out-source-mismatch", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			log := &tlb.ExternalMessageOut{SrcAddr: outgoing(t, s).DstAddr, DstAddr: address.NewAddressExt(0, 8, []byte{1}), Body: cell.BeginCell().EndCell()}
			c, e := tlb.ToCell(log)
			if e != nil {
				t.Fatal(e)
			}
			setOutValue(t, s, cell.BeginCell().MustStoreRef(c).EndCell())
			return []*tlb.Transaction{s}
		}},
		{"aggregate-message-bound", "", func(t *testing.T, s, r *tlb.Transaction) []*tlb.Transaction {
			im := outgoing(t, s)
			c, e := tlb.ToCell(im)
			if e != nil {
				t.Fatal(e)
			}
			d := cell.NewDict(15)
			for i := 0; i < 256; i++ {
				if e = d.SetIntKey(big.NewInt(int64(i)), cell.BeginCell().MustStoreRef(c).EndCell()); e != nil {
					t.Fatal(e)
				}
			}
			s.IO.Out = &tlb.MessagesList{List: d}
			s.OutMsgCount = 256
			var txs []*tlb.Transaction
			for i := 0; i < 5; i++ {
				tx := cloneHistory(t, s)
				tx.LT += uint64(i)
				txs = append(txs, tx)
			}
			return txs
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, p, _, source, recipient := receiptFixture(t)
			var rows []string
			for _, tx := range tc.mutate(t, source, recipient) {
				rows = append(rows, txBOC(t, tx))
			}
			stop := historyRPC(t, a, p.Account.Network, rows, tc.mode)
			defer stop()
			if out, e := a.History(context.Background(), p.Account); e == nil || len(out) != 0 {
				t.Fatal("hostile history accepted", out, e)
			}
		})
	}
	a, _, p := testPlan(t)
	if _, e := a.History(context.Background(), model.Account{}); e == nil {
		t.Fatal("invalid descriptor accepted")
	}
	stop := historyRPC(t, a, p.Account.Network, []string{"not-a-BOC"}, "")
	defer stop()
	if _, e := a.History(context.Background(), p.Account); e == nil {
		t.Fatal("malformed BOC accepted")
	}
}
func TestTONHistoryOrderAndUnsupportedExecution(t *testing.T) {
	a, p, _, source, _ := receiptFixture(t)
	unknown := cloneHistory(t, source)
	unknown.LT++
	unknown.IO.In = nil
	unknown.IO.Out = nil
	unknown.OutMsgCount = 0
	unknown.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
	noAction := cloneHistory(t, source)
	noAction.LT += 2
	ordinary(t, noAction, func(d *tlb.TransactionDescriptionOrdinary) { d.ActionPhase = nil })
	log := cloneHistory(t, source)
	log.LT += 3
	ext := &tlb.ExternalMessageOut{SrcAddr: source.IO.In.AsExternalIn().DstAddr, DstAddr: address.NewAddressExt(0, 8, []byte{1}), Body: cell.BeginCell().EndCell()}
	c, e := tlb.ToCell(ext)
	if e != nil {
		t.Fatal(e)
	}
	setOutValue(t, log, cell.BeginCell().MustStoreRef(c).EndCell())
	stop := historyRPC(t, a, p.Account.Network, []string{txBOC(t, source), txBOC(t, unknown), txBOC(t, noAction), txBOC(t, log)}, "")
	defer stop()
	out, e := a.History(context.Background(), p.Account)
	if e != nil || len(out) != 4 {
		t.Fatal(out, e)
	}
	if !strings.Contains(out[0].Detail, "external outgoing log") || !strings.Contains(out[1].Detail, "no successful source action") || !strings.Contains(out[2].Detail, "not interpreted") {
		t.Fatal("history ordering/execution distinctions", out)
	}
	stopEmpty := historyRPC(t, a, p.Account.Network, nil, "")
	defer stopEmpty()
	out, e = a.History(context.Background(), p.Account)
	if e != nil || len(out) != 0 {
		t.Fatal("empty bounded window", out, e)
	}
}
