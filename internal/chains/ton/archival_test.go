package ton

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/model"
)

// These linked chains extend retained emulator BOCs synthetically. They verify
// pagination/exact reconciliation, not acceptance of the added transactions.
func archivalChain(t *testing.T, base *tlb.Transaction, count int) []*tlb.Transaction {
	t.Helper()
	chain := make([]*tlb.Transaction, count)
	for i := 0; i < count; i++ {
		tx := cloneHistory(t, base)
		tx.LT = base.LT + uint64(i)
		tx.Now += uint32(i)
		tx.PrevTxLT = 0
		tx.PrevTxHash = make([]byte, 32)
		if i > 0 {
			tx.PrevTxLT = chain[i-1].LT
			tx.PrevTxHash = chain[i-1].Hash
			tx.IO.In = nil
			tx.IO.Out = nil
			tx.OutMsgCount = 0
			tx.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
		}
		chain[i] = cloneHistory(t, tx)
		c, err := tlb.ToCell(chain[i])
		if err != nil {
			t.Fatal(err)
		}
		chain[i].Hash = c.Hash()
	}
	out := make([]*tlb.Transaction, count)
	for i := range chain {
		out[len(out)-1-i] = chain[i]
	}
	return out
}
func archivalServer(t *testing.T, a *Adapter, p model.Plan, chains map[string][]*tlb.Transaction, perPage int, mutate func(int, []*tlb.Transaction) []*tlb.Transaction) (*int, func()) {
	t.Helper()
	requests := 0
	n, _ := netInfo(p.Account.Network)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string `json:"method"`
			Params struct {
				Address  string `json:"address"`
				Limit    int    `json:"limit"`
				Archival bool   `json:"archival"`
				LT       string `json:"lt"`
				Hash     string `json:"hash"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
			return
		}
		var result any
		if q.Method == "getMasterchainInfo" {
			result = map[string]any{"init": map[string]any{"root_hash": n.root, "file_hash": n.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
		} else {
			if q.Method != "getTransactions" || q.Params.Limit != 100 || !q.Params.Archival {
				t.Error("incorrect archival request")
			}
			requests++
			chain := chains[q.Params.Address]
			offset := 0
			if q.Params.LT != "" {
				offset = len(chain)
				for i, tx := range chain {
					if strconv.FormatUint(tx.LT, 10) == q.Params.LT && hex.EncodeToString(tx.Hash) == q.Params.Hash {
						offset = i
						break
					}
				}
			}
			end := offset + perPage
			if end > len(chain) {
				end = len(chain)
			}
			rows := append([]*tlb.Transaction(nil), chain[offset:end]...)
			if mutate != nil {
				rows = mutate(requests, rows)
			}
			entries := []map[string]string{}
			for _, tx := range rows {
				entries = append(entries, map[string]string{"data": txBOC(t, tx)})
			}
			result = entries
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "ok": true, "result": result})
	}))
	if err := a.Configure(p.Account.Network, Endpoint{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	return &requests, server.Close
}
func TestTONArchivalHistoryFollowsBOCLinksBeyondFirstHundred(t *testing.T) {
	a, p, _, source, _ := receiptFixture(t)
	chain := archivalChain(t, source, 205)
	requests, close := archivalServer(t, a, p, map[string][]*tlb.Transaction{p.Account.Address: chain}, 100, nil)
	defer close()
	var all []model.Receipt
	cursor := ""
	for page := 0; page < 3; page++ {
		got, err := a.HistoryPage(context.Background(), p.Account, cursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, got.Entries...)
		if page < 2 && (got.NextCursor == "" || got.Complete) {
			t.Fatal("older account link omitted", got)
		}
		if page == 2 && (!got.Complete || got.NextCursor != "") {
			t.Fatal("terminal account link not recognized", got)
		}
		cursor = got.NextCursor
	}
	if *requests != 3 || len(all) != len(chain) {
		t.Fatal("wrong page count", *requests, len(all))
	}
	for i, row := range all {
		if row.Identity != hex.EncodeToString(chain[i].Hash) || row.State != "OBSERVED" {
			t.Fatal("history identity/order changed", i)
		}
	}
}
func TestTONArchivalReconciliationFindsExactSourceAndRecipientOlderThanHundred(t *testing.T) {
	a, p, s, source, recipient := receiptFixture(t)
	requests, close := archivalServer(t, a, p, map[string][]*tlb.Transaction{p.Account.Address: archivalChain(t, source, 103), p.Recipient: archivalChain(t, recipient, 107)}, 100, nil)
	defer close()
	got, err := a.Reconcile(context.Background(), p, s)
	if err != nil || got.State != "CONFIRMED" || *requests != 4 {
		t.Fatal("exact older payment not reconciled", got, err, *requests)
	}
}
func TestTONArchivalRejectsGapsOrderingRepeatsAndFalseTermination(t *testing.T) {
	for _, mode := range []string{"reordered", "gap", "repeat", "wrong-first", "missing-next", "invalid-prev", "terminal-nonzero", "zero-lt"} {
		t.Run(mode, func(t *testing.T) {
			a, p, _, source, _ := receiptFixture(t)
			chain := archivalChain(t, source, 3)
			mutate := func(request int, rows []*tlb.Transaction) []*tlb.Transaction {
				switch mode {
				case "reordered":
					return []*tlb.Transaction{chain[1], chain[0]}
				case "gap":
					return []*tlb.Transaction{chain[0], chain[2]}
				case "repeat":
					return []*tlb.Transaction{chain[0], chain[0]}
				case "wrong-first":
					if request > 1 {
						return []*tlb.Transaction{chain[0]}
					}
				case "missing-next":
					if request > 1 {
						return nil
					}
				case "invalid-prev":
					tx := cloneHistory(t, chain[0])
					tx.PrevTxLT = tx.LT
					return []*tlb.Transaction{tx}
				case "terminal-nonzero":
					tx := cloneHistory(t, chain[2])
					tx.PrevTxHash[0] = 1
					return []*tlb.Transaction{tx}
				case "zero-lt":
					tx := cloneHistory(t, chain[0])
					tx.LT = 0
					return []*tlb.Transaction{tx}
				}
				return rows
			}
			_, close := archivalServer(t, a, p, map[string][]*tlb.Transaction{p.Account.Address: chain}, 1, mutate)
			defer close()
			page, err := a.HistoryPage(context.Background(), p.Account, "")
			if mode == "wrong-first" || mode == "missing-next" {
				if err != nil || page.NextCursor == "" {
					t.Fatal(page, err)
				}
				_, err = a.HistoryPage(context.Background(), p.Account, page.NextCursor)
			}
			if err == nil {
				t.Fatal("invalid archival response accepted")
			}
		})
	}
}
func TestTONArchivalCursorStrictContextAndUnknownEmpty(t *testing.T) {
	a, p, _, source, _ := receiptFixture(t)
	chain := archivalChain(t, source, 2)
	requests, close := archivalServer(t, a, p, map[string][]*tlb.Transaction{p.Account.Address: chain}, 1, nil)
	defer close()
	first, err := a.HistoryPage(context.Background(), p.Account, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	var pos transactionPosition
	if json.Unmarshal(raw, &pos) != nil {
		t.Fatal("cursor codec")
	}
	invalid := []string{"!", strings.Repeat("a", 513), base64.RawURLEncoding.EncodeToString([]byte(`{"lt":"1","lt":"2"}`))}
	for _, edit := range []func(*transactionPosition){func(p *transactionPosition) { p.LT = "0" }, func(p *transactionPosition) { p.LT = "01" }, func(p *transactionPosition) { p.LT = "18446744073709551616" }, func(p *transactionPosition) { p.Hash = "gg" }, func(p *transactionPosition) { p.Hash = strings.ToUpper(p.Hash) }, func(p *transactionPosition) { p.Account = "other-network-or-account" }} {
		changed := pos
		edit(&changed)
		b, _ := json.Marshal(changed)
		invalid = append(invalid, base64.RawURLEncoding.EncodeToString(b))
	}
	before := *requests
	for _, cursor := range invalid {
		if _, err = a.HistoryPage(context.Background(), p.Account, cursor); err == nil {
			t.Fatal("invalid cursor accepted", len(cursor))
		}
	}
	if *requests != before {
		t.Fatal("invalid cursor reached provider")
	}
	if _, err = a.HistoryPage(context.Background(), model.Account{}, ""); err == nil {
		t.Fatal("invalid descriptor accepted")
	}
	a2, p2, _, _, _ := receiptFixture(t)
	_, close2 := archivalServer(t, a2, p2, nil, 100, nil)
	defer close2()
	empty, err := a2.HistoryPage(context.Background(), p2.Account, "")
	if err != nil || empty.Complete || empty.NextCursor != "" || len(empty.Entries) != 0 {
		t.Fatal("empty response proved nonexistent history", empty, err)
	}
}
func TestTONArchivalSearchStopsAtBudgetWithoutInventingPaymentOutcome(t *testing.T) {
	a, p, _, source, _ := receiptFixture(t)
	requests, close := archivalServer(t, a, p, map[string][]*tlb.Transaction{p.Account.Address: archivalChain(t, source, 33)}, 1, nil)
	defer close()
	tx, complete, err := a.findArchivedTransaction(context.Background(), p.Account.Network, p.Account.Address, func(*tlb.Transaction) bool { return false })
	if err != nil || tx != nil || complete || *requests != reconciliationPages {
		t.Fatal("unbounded or fictitious archival search", fmt.Sprint(tx), complete, err, *requests)
	}
}

func TestTONArchivalHistoryPreservesProviderAndInterpretationErrors(t *testing.T) {
	for _, mode := range []string{"wrong-network", "outage", "malformed-boc", "messages"} {
		t.Run(mode, func(t *testing.T) {
			a, p, _, source, _ := receiptFixture(t)
			chain := archivalChain(t, source, 1)
			rows := []string{txBOC(t, chain[0])}
			if mode == "malformed-boc" {
				rows = []string{"not-a-BOC"}
			}
			if mode == "messages" {
				chain[0].OutMsgCount = 257
				rows = []string{txBOC(t, chain[0])}
			}
			stop := historyRPC(t, a, p.Account.Network, rows, mode)
			defer stop()
			if _, err := a.HistoryPage(context.Background(), p.Account, ""); err == nil {
				t.Fatal("archive error became empty success")
			}
		})
	}
}
