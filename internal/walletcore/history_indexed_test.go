package walletcore

import (
	"fmt"
	"strings"
	"testing"

	"localwallet/internal/model"
)

type overlappingHistoryAdapter struct{ *pagedHistoryAdapter }

func (*overlappingHistoryAdapter) HistoryPagesOverlap() bool { return true }

func TestIndexedHistoryAdvancesEmptyFilteredPagesBeforeReturningAnObservation(t *testing.T) {
	reads := 0
	a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(cursor string) (model.HistoryPage, error) {
		reads++
		if reads < 3 {
			return model.HistoryPage{Scope: "Address-indexed candidates filtered by owned asset", NextCursor: fmt.Sprint(reads)}, nil
		}
		return model.HistoryPage{Scope: "Address-indexed historical account activity", Entries: []model.Receipt{{Identity: "old-transfer-beyond-recent-chain-window", State: "OBSERVED"}}, Complete: true}, nil
	}}
	c, _ := fakeHistoryCoordinator(t, a)
	page, err := getHistory(t, c, "")
	if err != nil || reads != 3 || len(page.Entries) != 1 || page.Entries[0].Identity != "old-transfer-beyond-recent-chain-window" || !page.Complete || page.NextCursor != "" {
		t.Fatal("filtered candidate pages hid older activity", page, reads, err)
	}
}

func TestIndexedHistoryEmptyAdvancementIsBoundedAndRetainsAContinuation(t *testing.T) {
	reads := 0
	a := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(string) (model.HistoryPage, error) {
		reads++
		return model.HistoryPage{Scope: "Filtered address candidates; more pages remain", NextCursor: fmt.Sprint(reads)}, nil
	}}
	c, _ := fakeHistoryCoordinator(t, a)
	page, err := getHistory(t, c, "")
	if err != nil || reads != historyFetchPages || len(page.Entries) != 0 || page.Complete || page.NextCursor == "" {
		t.Fatal("unbounded filter scan or false complete empty result", page, reads, err)
	}
	for i := historyFetchPages; i < historyAdapterPages; i += historyFetchPages {
		page, err = getHistory(t, c, page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = getHistory(t, c, page.NextCursor); err == nil || reads != historyAdapterPages {
		t.Fatal("adapter request budget did not terminate observation", reads, err)
	}
}

func TestIndexedHistoryOnlySuppressesIdenticalReviewedStreamOverlap(t *testing.T) {
	for _, mode := range []string{"identical", "changed-description-only", "changed-state", "changed-block", "changed-time", "cursor-cycle", "within-page-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			original := model.Receipt{Identity: "same-transaction", State: "OBSERVED", Block: "canonical-block", Timestamp: 1700000000}
			reads := 0
			base := &pagedHistoryAdapter{publicAdapter: &publicAdapter{}, page: func(cursor string) (model.HistoryPage, error) {
				reads++
				if cursor == "" {
					return model.HistoryPage{Entries: []model.Receipt{original}, NextCursor: "native-page", Scope: "indexed"}, nil
				}
				next := original
				switch mode {
				case "changed-description-only":
					next.Detail = "Untrusted descriptive prose does not define an effect"
				case "changed-state":
					next.State = "CONFIRMED"
				case "changed-block":
					next.Block = "different-block"
				case "changed-time":
					next.Timestamp++
				case "cursor-cycle":
					if cursor == "native-page" {
						return model.HistoryPage{NextCursor: "token-page", Scope: "indexed"}, nil
					}
					return model.HistoryPage{NextCursor: "native-page", Scope: "indexed"}, nil
				case "within-page-duplicate":
					return model.HistoryPage{Entries: []model.Receipt{next, next}, Scope: "indexed"}, nil
				}
				return model.HistoryPage{Entries: []model.Receipt{next, {Identity: "older-independent-transfer", State: "OBSERVED"}}, Complete: true, Scope: "indexed"}, nil
			}}
			c, _ := fakeHistoryCoordinator(t, base)
			c.adapters = []model.Adapter{&overlappingHistoryAdapter{base}}
			first, err := getHistory(t, c, "")
			if err != nil {
				t.Fatal(err)
			}
			last, err := getHistory(t, c, first.NextCursor)
			if mode == "identical" || mode == "changed-description-only" {
				if err != nil || len(last.Entries) != 1 || last.Entries[0].Identity != "older-independent-transfer" || !last.Complete {
					t.Fatal("legitimate stream overlap hid an older transfer", last, err)
				}
			} else if err == nil {
				t.Fatal("conflicting or repeated provider observation accepted", mode, last)
			}
			if reads > historyFetchPages+1 {
				t.Fatal("unbounded overlap work")
			}
		})
	}
}

func TestHistoryObservationFingerprintDoesNotChangeStructuredValueAuthority(t *testing.T) {
	a := model.Receipt{Identity: "tx", State: "OBSERVED", Transfers: []model.HistoryTransfer{{ID: "effect", Asset: model.Asset{ID: "ETH", Decimals: 18}, Atomic: "1", Destination: "owned"}}}
	for _, change := range []func(*model.Receipt){
		func(r *model.Receipt) { r.Transfers[0].Atomic = "2" },
		func(r *model.Receipt) { r.Transfers[0].Destination = "foreign" },
		func(r *model.Receipt) { r.Transfers[0].Asset.Decimals = 6 },
		func(r *model.Receipt) { r.Transfers[0].Asset.Contract = "different-token" },
		func(r *model.Receipt) { r.Transfers[0].Comment = "different public comment" },
		func(r *model.Receipt) { r.Explorer = "https://foreign.invalid/" + strings.Repeat("x", 10) },
	} {
		b := a
		b.Transfers = append([]model.HistoryTransfer(nil), a.Transfers...)
		change(&b)
		if historyObservationHash(a) == historyObservationHash(b) {
			t.Fatal("changed structured authority was treated as identical")
		}
	}
}

func TestHistoryObservationFingerprintCanonicalizesOnlyEffectOrderAndDescription(t *testing.T) {
	a := model.Receipt{Identity: "tx", State: "OBSERVED", Transfers: []model.HistoryTransfer{{ID: "native", Asset: model.Asset{ID: "ETH", Decimals: 18}, Atomic: "3"}, {ID: "erc20:0", Asset: model.Asset{ID: "USDT", Decimals: 6}, Atomic: "2000000"}}}
	b := a
	b.Detail = "Another index's descriptive summary"
	b.Transfers = []model.HistoryTransfer{a.Transfers[1], a.Transfers[0]}
	if historyObservationHash(a) != historyObservationHash(b) {
		t.Fatal("the same checked effects became conflicting because of stream order or prose")
	}
}
