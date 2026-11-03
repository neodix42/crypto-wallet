package nativewallet

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"localwallet/internal/webui"
)

func autoStatus(t *testing.T, a *App, operations ...freePublicOperation) {
	t.Helper()
	a.exchange, _ = json.Marshal(map[string]any{"provider": "bybit", "connected": true, "configured": true, "endpoint": "https://api.bybit.com", "keySuffix": "test", "operations": operations})
}

func TestFreeExchangeRestoredTransferAutomaticallyConfirmsAndRefreshesFiveCoinDraft(t *testing.T) {
	for _, draft := range []string{"untouched", "entered", "cleared"} {
		t.Run(draft, func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			a.event(webui.Event{ID: "wizardStep/4"})
			u := a.freeData()
			a.setFreeBalances([]wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "0"}})
			_ = a.exchangePage()
			if a.fields["freeReturnAmount"] != "0" {
				t.Fatal("initial suggestion was not the known zero balance")
			}
			expected := "5"
			if draft == "entered" {
				expected = "3.25"
				a.event(webui.Event{ID: "field/freeReturnAmount", Value: expected})
			}
			if draft == "cleared" {
				expected = ""
				a.event(webui.Event{ID: "field/freeReturnAmount", Value: ""})
			}
			original := freePublicOperation{Provider: "bybit", ID: strings.Repeat("a", 32), Kind: "transfer", State: "UNKNOWN", AccountID: source.ID, AssetID: saved.SourceAsset}
			autoStatus(t, a, original)
			var methods []string
			backend.handler = func(_ uint64, method string, body, out any) error {
				methods = append(methods, method)
				switch method {
				case "exchangeReconcile":
					f := freeRequestFields(t, body)
					if f["id"] != original.ID || f["provider"] != "bybit" {
						t.Error("automatic check changed original identity", f)
					}
					return answer(out, wizardResult{ID: original.ID, Kind: "transfer", State: "CONFIRMED"})
				case "exchangeFundingBalances":
					f := freeRequestFields(t, body)
					if f["accountId"] != source.ID || f["assetId"] != saved.SourceAsset {
						t.Error("refresh read a different owned source", f)
					}
					return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0.99998566"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "5"}}})
				default:
					t.Error("automatic status acquired economic authority", method)
					return errors.New("unexpected economic request")
				}
			}
			activity := a.lastActivity
			status, revision := a.status, a.statusRevision
			if !a.nextFreeExchangeCheck() || a.busy || a.background {
				t.Fatal("restored operation was not checked read-only")
			}
			ui.apply(t)
			if strings.Join(methods, ",") != "exchangeReconcile,exchangeFundingBalances" || len(u.operations) != 1 || u.operations[0].ID != original.ID || !u.operations[0].restored || u.operations[0].State != "CONFIRMED" {
				t.Fatal("restore did not retain its independent original", methods, u.operations)
			}
			if a.fields["freeReturnAmount"] != expected {
				t.Fatal("automatic observation replaced a user draft", draft, a.fields)
			}
			if !a.lastActivity.Equal(activity) || a.view().ErrorNotice != nil || len(a.errorQueue) != 0 || ui.cancels.Load() != 0 || a.status != status || a.statusRevision != revision {
				t.Fatal("restored background observation interrupted the current-funds UI")
			}
			if draft != "cleared" {
				if button, _ := findNode(a.exchangePage(), "freeReturn"); button.Disabled {
					t.Fatal("affordable confirmed-source return stayed disabled")
				}
			}
		})
	}
}

func TestFreeExchangeRestoredChecksAreFairAndTransientFailuresStayNonmodal(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	a.freeData()
	first := freePublicOperation{Provider: "bybit", ID: strings.Repeat("a", 32), Kind: "transfer", State: "UNKNOWN", AccountID: source.ID, AssetID: saved.SourceAsset}
	second := first
	second.ID = strings.Repeat("b", 32)
	second.Kind = "swap"
	autoStatus(t, a, first, second)
	var identifiers []string
	backend.handler = func(_ uint64, method string, body, out any) error {
		if method != "exchangeReconcile" {
			t.Error("retry requested financial authority", method)
		}
		id := freeRequestFields(t, body)["id"].(string)
		identifiers = append(identifiers, id)
		if id == first.ID {
			return errors.New("PRIVATE_PROVIDER_TEXT_NOT_FOR_UI")
		}
		return answer(out, wizardResult{ID: id, Kind: "swap", State: "CONFIRMED"})
	}
	for index := 0; index < 3; index++ {
		a.freeData().checkedAt = time.Now().Add(-6 * time.Second)
		if !a.nextFreeExchangeCheck() {
			t.Fatal("an unresolved original stopped checking", index)
		}
		ui.apply(t)
	}
	if strings.Join(identifiers, ",") != first.ID+","+second.ID+","+first.ID {
		t.Fatal("one unavailable request starved another original", identifiers)
	}
	if a.freeData().operations[0].State != "UNKNOWN" || a.freeData().operations[1].State != "CONFIRMED" || a.view().ErrorNotice != nil || strings.Contains(viewText(a.exchangePage()), "PRIVATE_PROVIDER_TEXT") {
		t.Fatal("a read failure became completion, interruption or private text")
	}
}

func TestFreeExchangeOnlyOwnedUnambiguousBybitRemoteIDsResume(t *testing.T) {
	a, _, _, source, saved := freeNativeFixture(t)
	a.freeData()
	valid := freePublicOperation{Provider: "bybit", ID: strings.Repeat("a", 32), Kind: "transfer", State: "PENDING", AccountID: source.ID, AssetID: saved.SourceAsset}
	operations := []freePublicOperation{valid}
	for index, change := range []func(*freePublicOperation){
		func(o *freePublicOperation) { o.Provider = "binance" }, func(o *freePublicOperation) { o.AccountID = "unowned" }, func(o *freePublicOperation) { o.AssetID = "unowned" }, func(o *freePublicOperation) { o.ID = "wallet-payment-id" }, func(o *freePublicOperation) { o.Kind = "deposit" }, func(o *freePublicOperation) { o.State = "PREPARED" }, func(o *freePublicOperation) { o.State = "AUTHORIZED" }, func(o *freePublicOperation) { o.State = "CONFIRMED" },
	} {
		operation := valid
		operation.ID = strings.Repeat(string(rune('b'+index)), 32)
		change(&operation)
		operations = append(operations, operation)
	}
	duplicate := valid
	duplicate.ID = strings.Repeat("c", 32)
	operations = append(operations, duplicate, duplicate)
	autoStatus(t, a, operations...)
	if !a.resumeFreeExchangeOperations() || len(a.freeData().operations) != 1 || a.freeData().operations[0].ID != valid.ID {
		t.Fatal("unowned, unsigned, ambiguous or on-chain identity was resumed", a.freeData().operations)
	}
	if a.resumeFreeExchangeOperations() {
		t.Fatal("unchanged status duplicated the automatic work")
	}
}

func TestFreeExchangeRestoredReadRejectsConnectionIdentityAndNewerPublicSnapshots(t *testing.T) {
	for _, changed := range []string{"endpoint", "key", "provider", "public-state", "public-source", "wrong-returned-id", "unknown-returned-state"} {
		t.Run(changed, func(t *testing.T) {
			a, ui, backend, source, saved := freeNativeFixture(t)
			u := a.freeData()
			original := freePublicOperation{Provider: "bybit", ID: strings.Repeat("a", 32), Kind: "transfer", State: "UNKNOWN", AccountID: source.ID, AssetID: saved.SourceAsset}
			autoStatus(t, a, original)
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method == "exchangeFundingBalances" {
					return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "5"}}})
				}
				id, state := original.ID, "CONFIRMED"
				if changed == "wrong-returned-id" {
					id = strings.Repeat("b", 32)
				}
				if changed == "unknown-returned-state" {
					state = "SUCCESS_IS_NOT_A_LOCAL_RECORD_STATE"
				}
				return answer(out, wizardResult{ID: id, Kind: "transfer", State: state})
			}
			if !a.nextFreeExchangeCheck() {
				t.Fatal("automatic read did not begin")
			}
			callback := depositPollCallback(t, ui)
			var status map[string]any
			_ = json.Unmarshal(a.exchange, &status)
			switch changed {
			case "endpoint":
				status["endpoint"] = "https://api.bybit.eu"
			case "key":
				status["keySuffix"] = "next"
			case "provider":
				a.fields["exchangeProvider"] = "binance"
			case "public-state":
				status["operations"].([]any)[0].(map[string]any)["state"] = "CONFIRMED"
			case "public-source":
				status["operations"].([]any)[0].(map[string]any)["accountId"] = "different-source"
			}
			a.exchange, _ = json.Marshal(status)
			before := a.status
			callback()
			if u.operations[0].State != "UNKNOWN" || u.balances != nil || a.status != before {
				t.Fatal("stale or malformed original outcome changed current state", changed, u.operations)
			}
		})
	}
}

func TestFreeExchangeLatePendingSnapshotCannotReopenConfirmedOriginal(t *testing.T) {
	a, _, _, source, saved := freeNativeFixture(t)
	u := a.freeData()
	original := freePublicOperation{Provider: "bybit", ID: strings.Repeat("a", 32), Kind: "transfer", State: "UNKNOWN", AccountID: source.ID, AssetID: saved.SourceAsset}
	autoStatus(t, a, original)
	a.resumeFreeExchangeOperations()
	u.operations[0].State = "CONFIRMED" // A newer original-ID result precedes an older status reply.
	if !a.resumeFreeExchangeOperations() || u.operations[0].State != "CONFIRMED" {
		t.Fatal("cached pending status reopened a verified terminal request")
	}
	public, unique := a.freePublicOperationSnapshot(original.ID)
	if !unique || public.State != "CONFIRMED" {
		t.Fatal("cached public row replaced known confirmed metadata")
	}
}

func TestFreeExchangeAsyncConfirmedSwapInvalidatesItsSpendingQuote(t *testing.T) {
	a, ui, backend, source, saved := freeNativeFixture(t)
	u := a.freeData()
	u.quote = json.RawMessage(`{"balances":[{"accountType":"UNIFIED","coin":"ETH","available":"5"}]}`)
	u.operations = []freeExchangeOperation{{Stage: 3, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "current-session-original-order", Kind: "swap", State: "PENDING"}}}
	backend.handler = func(_ uint64, method string, _, out any) error {
		if method != "exchangeReconcile" {
			t.Error("filled-order check requested unrelated authority", method)
		}
		return answer(out, wizardResult{ID: "current-session-original-order", Kind: "swap", State: "CONFIRMED"})
	}
	if !a.nextFreeExchangeCheck() {
		t.Fatal("pending order not checked")
	}
	ui.apply(t)
	if u.quote != nil || u.operations[0].State != "CONFIRMED" {
		t.Fatal("confirmed order retained a stale spendable quote")
	}
}

func TestFreeExchangeAutomaticCurrentSessionCompletionPreservesLaterUserDrafts(t *testing.T) {
	for _, field := range []string{"freeMoveAmount", "freeReturnAmount"} {
		for _, value := range []string{"3.25", ""} {
			t.Run(field+"/"+value, func(t *testing.T) {
				a, ui, backend, source, saved := freeNativeFixture(t)
				u := a.freeData()
				a.fields[field] = "5"
				u.transferDefaults = map[string]string{field: "5"} // This was consumed by an earlier explicit submission.
				u.operations = []freeExchangeOperation{{Stage: 2, AccountID: source.ID, AssetID: saved.SourceAsset, wizardResult: wizardResult{ID: "original-submitted-transfer", Kind: "transfer", State: "PENDING"}}}
				backend.handler = func(_ uint64, method string, _, out any) error {
					switch method {
					case "exchangeReconcile":
						return answer(out, wizardResult{ID: "original-submitted-transfer", Kind: "transfer", State: "CONFIRMED"})
					case "exchangeFundingBalances":
						return answer(out, map[string]any{"balances": []wizardBalance{{AccountType: "FUND", Coin: "ETH", Available: "0.25"}, {AccountType: "UNIFIED", Coin: "ETH", Available: "4.75"}}})
					default:
						t.Error("automatic completion requested financial authority", method)
						return errors.New("unexpected automatic method")
					}
				}
				if !a.nextFreeExchangeCheck() {
					t.Fatal("original status was not tracked")
				}
				callback := depositPollCallback(t, ui)
				a.event(webui.Event{ID: "field/" + field, Value: value})
				callback()
				if a.fields[field] != value || u.operations[0].State != "CONFIRMED" || !u.transferEdited[field] {
					t.Fatal("background status erased an explicitly entered or cleared next draft", field, value, a.fields)
				}
			})
		}
	}
}
