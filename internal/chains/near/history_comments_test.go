package near

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func TestNEARNativeHistoryNeverTurnsActionsOrCallArgumentsIntoComments(t *testing.T) {
	adapter := New()
	account := model.Account{Network: "near-testnet", Address: "owned.testnet"}
	for _, method := range []string{"deposit_and_stake", "ft_transfer_call", "withdraw_all", "Memo: user-visible-looking action"} {
		args := base64.StdEncoding.EncodeToString([]byte(`{"memo":"not a native protocol memo","comment":"not a native protocol comment"}`))
		action, _ := json.Marshal(map[string]any{"FunctionCall": map[string]any{"method_name": method, "args": args, "gas": uint64(30000000000000), "deposit": "123"}})
		for _, incoming := range []bool{false, true} {
			from, to := account.Address, "peer.testnet"
			if incoming {
				from, to = to, from
			}
			rows, err := adapter.historicalNativeActions(account, from, to, []json.RawMessage{action}, "native:receipt", true)
			if err != nil || len(rows) != 1 || rows[0].ID != "native:receipt:0" || rows[0].Source != from || rows[0].Destination != to || rows[0].Atomic != "123" || rows[0].Asset != (model.Asset{ID: "near-testnet:native", Symbol: "NEAR", Decimals: 24}) || rows[0].Comment != "" {
				t.Fatal("action label/arguments became a memo or attached-deposit effect changed", method, incoming, err)
			}
		}
	}
	rows, err := adapter.historicalNativeActions(account, account.Address, "peer.testnet", []json.RawMessage{json.RawMessage(`{"Transfer":{"deposit":"456"}}`)}, "native:transaction", true)
	if err != nil || len(rows) != 1 || rows[0].Atomic != "456" || rows[0].Comment != "" {
		t.Fatal("native transfer acquired a synthetic comment", err)
	}
}

func TestNEP141HistoryCommentsAreOnlyExactEventMemos(t *testing.T) {
	for _, event := range []string{"ft_transfer", "ft_mint", "ft_burn"} {
		for _, memo := range []struct {
			name    string
			present bool
			value   any
			want    string
			reject  bool
		}{
			{"absent", false, nil, "", false},
			{"null", true, nil, "", false},
			{"empty", true, "", "", false},
			{"exact-text", true, "  Mint: paid\nBurn: snowman ☃ <script>  ", "  Mint: paid\nBurn: snowman ☃ <script>  ", false},
			{"maximum-text", true, strings.Repeat("m", 4096), strings.Repeat("m", 4096), false},
			{"oversized", true, strings.Repeat("m", 4097), "", true},
			{"nul", true, "actual\x00memo", "", true},
			{"wrong-type", true, 123, "", true},
		} {
			t.Run(event+"/"+memo.name, func(t *testing.T) {
				owned, _ := New().Derive(seed(), 0, "near-testnet")
				effect := map[string]any{"amount": "123"}
				from, to := "sender.testnet", owned.Address
				if event == "ft_transfer" {
					effect["old_owner_id"], effect["new_owner_id"] = from, to
				} else {
					effect["owner_id"] = owned.Address
					if event == "ft_mint" {
						from = ""
					} else {
						from, to = owned.Address, ""
					}
				}
				if memo.present {
					effect["memo"] = memo.value
				}
				adapter, account := tokenHistoryFixture(t, func(result map[string]any) {
					raw, _ := json.Marshal(map[string]any{"standard": "nep141", "version": "1.0.0", "event": event, "data": []any{effect}})
					historyOutcome(result)["logs"] = []string{"EVENT_JSON:" + string(raw)}
				})
				rows, err := adapter.tokenHistoryTransaction(context.Background(), account, historyHash(21), "sender.testnet", historyHash(31), "token.testnet")
				if memo.reject {
					if err == nil {
						t.Fatal("invalid on-chain memo was silently changed or accepted")
					}
					return
				}
				if err != nil || len(rows) != 1 || len(rows[0].Transfers) != 1 || rows[0].State != "OBSERVED" || rows[0].Identity != historyHash(21) || rows[0].Timestamp != 1700000000 {
					t.Fatal("memo handling changed the observed transaction envelope", err)
				}
				got := rows[0].Transfers[0]
				if got.Comment != memo.want || got.Source != from || got.Destination != to || got.Atomic != "123" || got.Asset != tokenPolicy("token.testnet", 6).Asset || got.ID != "nep141:"+historyHash(31)+":0:0" {
					t.Fatal("actual event memo, direction, exact amount or token identity changed")
				}
			})
		}
	}
}
