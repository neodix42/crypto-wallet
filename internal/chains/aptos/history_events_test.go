package aptos

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

func historyFrameworkEvent(kind string, data any) historyEvent {
	b, _ := json.Marshal(data)
	return historyEvent{Type: kind, Data: b}
}

func TestAPTPrimaryStoreMatchesIndependentLiveFrameworkView(t *testing.T) {
	// Public BIP39 interoperability vector only. On 2026-10-30 both official
	// mainnet/testnet primary_store_address views returned this exact value;
	// independent Python hashlib.sha3_256 also matched (no secret/fund use).
	const owner = "0xeb663b681209e7087d681c5d3eed12aaa8e1915e7c87794542c3f96e94b3d3bf"
	const want = "0x7c7850b59fc9fdd9c5069d21afe02a50e080bf713e1775106812ce4f3bbec33f"
	got, err := nativePrimaryStore(owner)
	if err != nil || got != want {
		t.Fatal("native APT primary-store derivation disagrees with framework", got, err)
	}
	if _, err := nativePrimaryStore("invalid"); err == nil {
		t.Fatal("malformed store owner accepted")
	}
}

func TestCommittedAPTEventsPreserveOwnedSideWithoutGuessingCounterparties(t *testing.T) {
	ac, _ := New().Derive(seed(), 0, "aptos-mainnet")
	store, _ := nativePrimaryStore(ac.Address)
	success := true
	tx := historyTransaction{execution: execution{Type: "user_transaction", Hash: "0x" + strings.Repeat("1", 64), Success: &success, Version: "44", Sender: "0x1", Sequence: "1"}, Timestamp: "1700000000000000"}
	tx.Events = []historyEvent{
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": store, "amount": "999"}),
		historyFrameworkEvent("0x1::fungible_asset::Withdraw", map[string]any{"store": store, "amount": "77"}),
		historyFrameworkEvent("0x1::coin::CoinDeposit", map[string]any{"coin_type": "0x1::aptos_coin::AptosCoin", "account": ac.Address, "amount": "23"}),
		historyFrameworkEvent("0x1::coin::CoinWithdraw", map[string]any{"coin_type": "0x1::aptos_coin::AptosCoin", "account": ac.Address, "amount": "19"}),
		// Same-looking events cannot import a token or another owner's effect.
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": "0xa", "amount": "100"}),
		historyFrameworkEvent("0xbeef::fungible_asset::Deposit", map[string]any{"store": store, "amount": "100"}),
		historyFrameworkEvent("0x1::coin::CoinDeposit", map[string]any{"coin_type": "0xbeef::coin::APT", "account": ac.Address, "amount": "100"}),
		historyFrameworkEvent("0x1::coin::CoinDeposit", map[string]any{"coin_type": "0x1::aptos_coin::AptosCoin", "account": "0x1", "amount": "100"}),
		historyFrameworkEvent("0x1::coin::DepositEvent", map[string]any{"amount": "100"}),
	}
	row, relevant, err := nativeHistoryTransaction(ac, tx)
	if err != nil || !relevant || len(row.Transfers) != 4 || row.State != "CONFIRMED" || row.Timestamp != 1700000000 || row.Explorer == "" {
		t.Fatal("native framework events lost", row, relevant, err)
	}
	for i, effect := range row.Transfers {
		if effect.ID == "" || effect.Asset != (model.Asset{ID: asset(ac.Network), Symbol: "APT", Decimals: 8}) || effect.Comment == "" {
			t.Fatal("event identity, native precision or uncertainty lost", effect)
		}
		if i%2 == 0 && (effect.Source != "" || effect.Destination != ac.Address) || i%2 == 1 && (effect.Source != ac.Address || effect.Destination != "") {
			t.Fatal("guessed unestablished counterparty", effect)
		}
	}
	if row.Transfers[0].Atomic != "999" || row.Transfers[1].Atomic != "77" {
		t.Fatal("event amount changed", row)
	}
	failed := false
	tx.Success = &failed
	if _, relevant, err := nativeHistoryTransaction(ac, tx); err != nil || relevant {
		t.Fatal("failed contract events credited", relevant, err)
	}
}

func TestAPTEventParsingRejectsHostileStandardEvents(t *testing.T) {
	ac, _ := New().Derive(seed(), 0, "aptos-mainnet")
	store, _ := nativePrimaryStore(ac.Address)
	for _, event := range []historyEvent{
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": store, "amount": "-1"}),
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": store, "amount": "18446744073709551616"}),
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": "invalid", "amount": "1"}),
		historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": store, "amount": "1", "invented": "peer"}),
		historyFrameworkEvent("0x1::coin::CoinDeposit", map[string]any{"coin_type": "0x1::aptos_coin::AptosCoin", "account": "invalid", "amount": "1"}),
		historyFrameworkEvent("0x1::coin::CoinDeposit", map[string]any{"coin_type": "0x1::aptos_coin::AptosCoin", "account": ac.Address, "amount": 1}),
	} {
		success := true
		tx := historyTransaction{execution: execution{Success: &success}, Events: []historyEvent{event}}
		if _, err := nativeHistoryEvents(ac, tx); err == nil {
			t.Fatal("hostile standard event accepted", event.Type, string(event.Data))
		}
	}
	success := true
	if _, err := nativeHistoryEvents(ac, historyTransaction{execution: execution{Success: &success}, Events: make([]historyEvent, 1001)}); err == nil {
		t.Fatal("unbounded event interpretation")
	}
}

func TestAPTEventReceiptRejectsInvalidCommittedEnvelope(t *testing.T) {
	ac, _ := New().Derive(seed(), 0, "aptos-mainnet")
	store, _ := nativePrimaryStore(ac.Address)
	for _, mode := range []string{"sender", "sequence", "hash", "time", "event"} {
		success := true
		tx := historyTransaction{execution: execution{Type: "user_transaction", Hash: "0x" + strings.Repeat("1", 64), Success: &success, Version: "44", Sender: "0x1", Sequence: "1"}, Timestamp: "1700000000000000", Events: []historyEvent{historyFrameworkEvent("0x1::fungible_asset::Deposit", map[string]any{"store": store, "amount": "1"})}}
		switch mode {
		case "sender":
			tx.Sender = "invalid"
		case "sequence":
			tx.Sequence = "-1"
		case "hash":
			tx.Hash = "0xab"
		case "time":
			tx.Timestamp = "-1"
		case "event":
			tx.Events[0].Data = []byte(`{}`)
		}
		if _, _, err := nativeHistoryTransaction(ac, tx); err == nil {
			t.Fatal("invalid committed native event envelope accepted", mode)
		}
	}
}

func FuzzAPTNativeHistoryInterpretation(f *testing.F) {
	ac, _ := New().Derive(seed(), 0, "aptos-mainnet")
	store, _ := nativePrimaryStore(ac.Address)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"type":"user_transaction","success":true,"sender":"0x1","sequence_number":"1","hash":"0x` + strings.Repeat("1", 64) + `","version":"44","timestamp":"1700000000000000","events":[{"type":"0x1::fungible_asset::Deposit","data":{"store":"` + store + `","amount":"123456789"}}]}`))
	f.Add([]byte(`{"type":"user_transaction","success":true,"payload":{"type":"entry_function_payload","function":"0x1::aptos_account::transfer","arguments":["0x1","-1"]}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 64<<10 || chainutil.CheckJSON(raw) != nil {
			return
		}
		var tx historyTransaction
		if json.Unmarshal(raw, &tx) != nil {
			return
		}
		row, relevant, err := nativeHistoryTransaction(ac, tx)
		if err != nil || !relevant {
			return
		}
		if len(row.Identity) != 66 || len(row.Transfers) == 0 || len(row.Transfers) > 1000 || row.State != "CONFIRMED" && row.State != "FAILED" {
			t.Fatal("unbounded or unidentified interpreted APT effects")
		}
		for _, effect := range row.Transfers {
			if effect.Asset != (model.Asset{ID: asset(ac.Network), Symbol: "APT", Decimals: 8}) || effect.Source != ac.Address && effect.Destination != ac.Address {
				t.Fatal("interpreted another account or asset as owned native APT")
			}
			if _, err := chainutil.Uint(effect.Atomic, 64); err != nil {
				t.Fatal("invalid interpreted native atomic amount")
			}
		}
	})
}
