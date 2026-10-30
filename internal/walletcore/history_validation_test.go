package walletcore

import (
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

func TestHistoryPublicPagesRejectHostileStructuredFields(t *testing.T) {
	asset := (&publicAdapter{}).Networks()[0].Assets[0]
	valid := func() model.Receipt {
		return model.Receipt{Identity: "synthetic-transaction", State: "OBSERVED", Timestamp: 1700000000, Transfers: []model.HistoryTransfer{{ID: "effect/1", Source: "sender", Destination: "recipient", Asset: asset, Atomic: "123", Comment: "<script>inert text</script>"}}}
	}
	cases := map[string]func(*model.Receipt){
		"negative-time":       func(r *model.Receipt) { r.Timestamp = -1 },
		"future-time":         func(r *model.Receipt) { r.Timestamp = time.Now().Unix() + 600 },
		"provider-url":        func(r *model.Receipt) { r.Explorer = "https://evil.invalid/tx" },
		"identity-nul":        func(r *model.Receipt) { r.Identity += "\x00" },
		"state-utf8":          func(r *model.Receipt) { r.State = string([]byte{255}) },
		"duplicate-effect":    func(r *model.Receipt) { r.Transfers = append(r.Transfers, r.Transfers[0]) },
		"empty-effect-id":     func(r *model.Receipt) { r.Transfers[0].ID = "" },
		"source-nul":          func(r *model.Receipt) { r.Transfers[0].Source += "\x00" },
		"destination-utf8":    func(r *model.Receipt) { r.Transfers[0].Destination = string([]byte{255}) },
		"comment-bound":       func(r *model.Receipt) { r.Transfers[0].Comment = strings.Repeat("x", 4097) },
		"untracked-token":     func(r *model.Receipt) { r.Transfers[0].Asset.ID += ":unknown" },
		"false-symbol":        func(r *model.Receipt) { r.Transfers[0].Asset.Symbol = "FAKE" },
		"false-precision":     func(r *model.Receipt) { r.Transfers[0].Asset.Decimals++ },
		"false-contract":      func(r *model.Receipt) { r.Transfers[0].Asset.Contract = "fake-contract" },
		"negative-amount":     func(r *model.Receipt) { r.Transfers[0].Atomic = "-1" },
		"noncanonical-amount": func(r *model.Receipt) { r.Transfers[0].Atomic = "01" },
		"oversized-amount":    func(r *model.Receipt) { r.Transfers[0].Atomic = strings.Repeat("9", 78) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			row := valid()
			mutate(&row)
			adapter := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(string) (model.HistoryPage, error) {
				return model.HistoryPage{Entries: []model.Receipt{row}, Scope: "bounded"}, nil
			}}
			c, _ := fakeHistoryCoordinator(t, adapter)
			if _, err := getHistory(t, c, ""); err == nil {
				t.Fatal("hostile interpreted history reached public IPC")
			}
		})
	}
	adapter := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(string) (model.HistoryPage, error) {
		return model.HistoryPage{Entries: []model.Receipt{valid()}, Scope: "bounded"}, nil
	}}
	c, _ := fakeHistoryCoordinator(t, adapter)
	page, err := getHistory(t, c, "")
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Transfers[0].Comment != "<script>inert text</script>" {
		t.Fatal("valid structured history changed", page, err)
	}
}
