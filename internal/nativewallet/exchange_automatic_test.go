package nativewallet

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func automaticNativeFixture(t *testing.T) (*App, *testFrontend, *testBackend, model.Account, exchange.WizardState) {
	t.Helper()
	a, ui, backend, source, saved := freeNativeFixture(t)
	autoStatus(t, a)
	a.wizard.viewStep = 1
	return a, ui, backend, source, saved
}

func TestFreeAutomaticDepositReadIsSelectedReadOnlyAndNonblocking(t *testing.T) {
	a, ui, backend, source, saved := automaticNativeFixture(t)
	a.fields["freeDepositAmount"] = "NaN" // A draft never blocks a destination read.
	activity, status, statusRevision := a.lastActivity, a.status, a.statusRevision
	methods := []string{}
	backend.handler = func(g uint64, method string, body, out any) error {
		methods = append(methods, method)
		if method != "exchangeDepositPreview" || g != backend.Generation() || !reflect.DeepEqual(body, map[string]string{"accountId": source.ID, "assetId": saved.SourceAsset}) {
			t.Error("automatic preview acquired unrelated authority", method, body)
			return errors.New("unexpected authority")
		}
		return answer(out, freePreview(source, saved.SourceAsset))
	}
	if !a.nextFreeAutomaticRead() || a.busy || a.background || a.nextFreeAutomaticRead() {
		t.Fatal("selected destination read blocked UI or duplicated its request")
	}
	if !strings.Contains(viewText(a.freeAutomaticReadNodes()), "Reading the selected Bybit deposit address") {
		t.Fatal("pending destination read was not visible")
	}
	ui.apply(t)
	if strings.Join(methods, ",") != "exchangeDepositPreview" || !a.freePreviewCurrent() || a.fields["freeDepositAmount"] != "NaN" || len(a.freeData().operations) != 0 {
		t.Fatal("automatic destination read changed a draft/operation or failed to preserve its selected identity", methods)
	}
	if !a.lastActivity.Equal(activity) || a.status != status || a.statusRevision != statusRevision || ui.cancels.Load() != 0 || len(a.errorQueue) != 0 {
		t.Fatal("background destination observation extended activity or interrupted the UI")
	}
	for _, step := range []int{1, 2, 4} {
		a.wizard.viewStep = step
		page := a.exchangePage()
		for _, id := range []string{"freeRefreshDeposit", "freeRefreshFunding", "exchangeStatus"} {
			if _, found := findNode(page, id); found {
				t.Fatal("removed duplicate/manual read control is still exposed", step, id)
			}
		}
		if _, found := findNode(page, "freeRefreshBalances"); !found {
			t.Fatal("common explicit balance refresh was removed", step)
		}
	}
}

func TestFreeAutomaticDepositDropsEveryStaleContext(t *testing.T) {
	for _, change := range []string{"source", "asset", "revision", "draft", "view", "provider", "region", "endpoint", "key", "raw-status", "profile", "free-state", "generation", "busy", "background", "locked", "closed", "inactive"} {
		t.Run(change, func(t *testing.T) {
			a, ui, backend, source, saved := automaticNativeFixture(t)
			u := a.freeData()
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method != "exchangeDepositPreview" {
					t.Error("unexpected background authority", method)
				}
				return answer(out, freePreview(source, saved.SourceAsset))
			}
			if !a.nextFreeAutomaticRead() {
				t.Fatal("automatic preview did not start")
			}
			callback := depositPollCallback(t, ui)
			switch change {
			case "source":
				a.fields["freeAccount"] = "another-owned-source"
			case "asset":
				a.fields["freeAsset"] = source.Network + ":usdt"
			case "revision":
				a.exchangeRevision++
			case "draft":
				a.event(webui.Event{ID: "field/freeDepositAmount", Value: "2.5"})
			case "view":
				a.wizard.viewStep = 3
			case "provider":
				a.fields["exchangeProvider"] = "binance"
			case "region":
				a.fields["region"] = "https://api.bybit.eu"
			case "endpoint", "key", "raw-status":
				var status map[string]any
				_ = json.Unmarshal(a.exchange, &status)
				field, value := "message", "a newer public observation"
				if change == "endpoint" {
					field, value = "endpoint", "https://api.bybit.eu"
				}
				if change == "key" {
					field, value = "keySuffix", "next"
				}
				status[field] = value
				a.exchange, _ = json.Marshal(status)
			case "profile":
				a.epoch++
			case "free-state":
				a.wizard.free = &exchangeFreeUI{}
			case "generation":
				backend.generation.Add(1)
			case "busy":
				a.busy = true
			case "background":
				a.background = true
			case "locked":
				a.state.Locked = true
			case "closed":
				a.closed = true
			case "inactive":
				a.active = "group/0"
			}
			callback()
			if u.preview != nil || change == "draft" && a.fields["freeDepositAmount"] != "2.5" {
				t.Fatal("stale destination observation replaced the current selected context", change)
			}
		})
	}
}

func TestFreeAutomaticSourceChangeClearsOldAddressAndLoadsNewSelection(t *testing.T) {
	a, ui, backend, source, saved := automaticNativeFixture(t)
	p := freePreview(source, saved.SourceAsset)
	a.freeData().preview = &p
	var asset model.Asset
	for _, candidate := range a.network(source.Network).Assets {
		if candidate.ID != saved.SourceAsset && exchangeAssetAllowed(source, candidate) {
			asset = exchange.LedgerAsset(candidate)
			break
		}
	}
	if asset.ID == "" {
		t.Fatal("fixture has no second reviewed asset")
	}
	a.event(webui.Event{ID: "field/freeAsset", Value: asset.ID})
	if a.freeData().preview != nil || strings.Contains(viewText(a.exchangePage()), p.Address) {
		t.Fatal("source selection retained an old exchange destination")
	}
	backend.handler = func(_ uint64, method string, body, out any) error {
		if method != "exchangeDepositPreview" || !reflect.DeepEqual(body, map[string]string{"accountId": source.ID, "assetId": asset.ID}) {
			t.Fatal("new destination read did not use the current exact selection", method, body)
		}
		p := freePreview(source, asset.ID)
		p.Coin, p.Address = asset.Symbol, "0x3333333333333333333333333333333333333333"
		return answer(out, p)
	}
	if !a.nextFreeAutomaticRead() {
		t.Fatal("new selection did not trigger a read")
	}
	ui.apply(t)
	if !a.freePreviewCurrent() || a.freeData().preview.Coin != asset.Symbol {
		t.Fatal("current selected destination was not displayed")
	}
}

func TestFreeAutomaticDestinationRejectsMalformedIdentityAndObservation(t *testing.T) {
	for _, field := range []string{"provider", "account", "asset", "network", "coin", "chain", "address", "time", "expired", "future"} {
		t.Run(field, func(t *testing.T) {
			a, ui, backend, source, saved := automaticNativeFixture(t)
			p := freePreview(source, saved.SourceAsset)
			switch field {
			case "provider":
				p.Provider = "binance"
			case "account":
				p.AccountID = "foreign"
			case "asset":
				p.AssetID = "foreign"
			case "network":
				p.Network = "ton-mainnet"
			case "coin":
				p.Coin = "USDT"
			case "chain":
				p.Chain = ""
			case "address":
				p.Address = ""
			case "time":
				p.ObservedAt = "not-a-time"
			case "expired":
				p.ObservedAt = time.Now().Add(-3 * time.Minute).Format(time.RFC3339)
			case "future":
				p.ObservedAt = time.Now().Add(time.Minute).Format(time.RFC3339)
			}
			backend.handler = func(_ uint64, _ string, _, out any) error { return answer(out, p) }
			a.nextFreeAutomaticRead()
			ui.apply(t)
			if a.freeData().preview != nil || !a.freeData().automatic.depositFailed || len(a.errorQueue) != 0 {
				t.Fatal("mismatched provider observation became a deposit destination", field)
			}
		})
	}
}

func TestFreeAutomaticReadTTLRetryAndPrivateErrorCanary(t *testing.T) {
	a, ui, backend, source, saved := automaticNativeFixture(t)
	fail := true
	backend.handler = func(_ uint64, method string, _, out any) error {
		if method != "exchangeDepositPreview" {
			t.Error("unexpected automatic request", method)
		}
		if fail {
			return errors.New("PRIVATE_API_SECRET_CANARY")
		}
		return answer(out, freePreview(source, saved.SourceAsset))
	}
	a.nextFreeAutomaticRead()
	ui.apply(t)
	r := a.freeData().automatic
	r.statusAt = time.Now()
	if strings.Contains(viewText(a.freeAutomaticReadNodes()), "CANARY") || !strings.Contains(viewText(a.freeAutomaticReadNodes()), "Checking again automatically") || len(a.errorQueue) != 0 || a.view().ErrorNotice != nil || a.nextFreeAutomaticRead() {
		t.Fatal("failed background read exposed private text, interrupted UI or ignored retry delay")
	}
	r.depositAt = time.Now().Add(-freeAutomaticRetry - time.Second)
	fail = false
	if !a.nextFreeAutomaticRead() {
		t.Fatal("expired retry delay did not permit a fresh read")
	}
	ui.apply(t)
	if !a.freePreviewCurrent() || a.nextFreeAutomaticRead() {
		t.Fatal("fresh preview was not retained for its TTL")
	}
	r.depositAt = time.Now().Add(-freeAutomaticDepositTTL - time.Second)
	if !a.nextFreeAutomaticRead() {
		t.Fatal("expired preview TTL was not refreshed automatically")
	}
	ui.apply(t)
}

func TestFreeAutomaticStatusPreservesViewDraftsAndOriginalChronology(t *testing.T) {
	a, ui, backend, _, saved := automaticNativeFixture(t)
	a.wizard.viewStep = 4
	a.fields["freeReturnAmount"], a.fields["freeDepositAmount"], a.fields["wizardTradeAmount"] = "3.25", "2.7", "preserve-original-draft"
	a.render()
	before := map[string]string{}
	for k, v := range a.fields {
		before[k] = v
	}
	updated := saved
	updated.Step, updated.TransferOperation, updated.TransferAmount = 3, "original-exchange-transfer", "6"
	if !updated.Valid() {
		t.Fatal("invalid forward chronology fixture")
	}
	backend.handler = func(_ uint64, method string, body, out any) error {
		if method != "exchangeStatus" || !reflect.DeepEqual(body, struct{}{}) {
			t.Error("status observation acquired unrelated authority", method, body)
		}
		return answer(out, map[string]any{"provider": "bybit", "connected": true, "configured": true, "endpoint": "https://api.bybit.com", "keySuffix": "test", "wizard": updated, "wizardDepositState": "CONFIRMED", "operations": []any{}})
	}
	activity, revision := a.lastActivity, a.statusRevision
	if !a.nextFreeAutomaticRead() || a.busy || a.background {
		t.Fatal("status observation occupied the foreground gate")
	}
	ui.apply(t)
	if a.wizard.viewStep != 4 || !reflect.DeepEqual(before, a.fields) || a.wizard.state.Step != 3 || a.wizard.state.DepositOperation != saved.DepositOperation || !a.lastActivity.Equal(activity) || a.statusRevision != revision {
		t.Fatal("automatic status restored an old page, changed drafts or lost original chronology", a.wizard.viewStep, a.wizard.state, "fields-before", before, "fields-after", a.fields, "activity", a.lastActivity.Equal(activity), "status", revision, a.statusRevision)
	}
}

func TestFreeAutomaticConnectionDiscoveryAndUnsafeStatusRefusals(t *testing.T) {
	for _, scenario := range []string{"connect", "foreign-provider", "foreign-endpoint", "null", "empty", "null-connected", "missing-connected", "private-error", "late-key", "late-operation"} {
		t.Run(scenario, func(t *testing.T) {
			a, ui, backend, _, _ := automaticNativeFixture(t)
			a.exchange = nil
			backend.handler = func(_ uint64, method string, _, out any) error {
				if method != "exchangeStatus" {
					t.Error("connection discovery requested financial authority", method)
				}
				status := map[string]any{"provider": "bybit", "connected": true, "configured": true, "endpoint": "https://api.bybit.com", "keySuffix": "test", "operations": []any{}}
				if scenario == "foreign-provider" {
					status["provider"] = "binance"
				}
				if scenario == "foreign-endpoint" {
					status["endpoint"] = "https://foreign.invalid"
				}
				if scenario == "private-error" {
					return errors.New("PRIVATE_API_SECRET_CANARY")
				}
				if scenario == "null" {
					return answer(out, json.RawMessage(`null`))
				}
				if scenario == "empty" {
					return answer(out, map[string]any{})
				}
				if scenario == "null-connected" {
					status["connected"] = nil
				}
				if scenario == "missing-connected" {
					delete(status, "connected")
				}
				return answer(out, status)
			}
			if !a.nextFreeAutomaticRead() {
				t.Fatal("unknown connection was not checked automatically")
			}
			callback := depositPollCallback(t, ui)
			if scenario == "late-key" {
				a.exchange = json.RawMessage(`{"provider":"bybit","connected":true,"endpoint":"https://api.bybit.com","keySuffix":"next"}`)
			}
			if scenario == "late-operation" {
				a.exchange = json.RawMessage(`{"provider":"bybit","connected":false,"operations":[{"id":"newer-original"}]}`)
			}
			before := string(a.exchange)
			callback()
			if scenario == "connect" {
				connection, _, valid := a.freeTrackingStatus()
				if !valid || !connection.Connected || connection.Provider != "bybit" {
					t.Fatal("successful read did not discover the configured connection")
				}
			} else if string(a.exchange) != before || strings.Contains(viewText(a.freeAutomaticReadNodes()), "CANARY") || len(a.errorQueue) != 0 {
				t.Fatal("unsafe or stale status replaced the current connection", scenario)
			}
		})
	}
}

func TestFreeAutomaticRenderNeverAttributesOldReadToNewSelection(t *testing.T) {
	for _, observation := range []string{"pending", "failure", "status"} {
		a, _, _, source, saved := automaticNativeFixture(t)
		r := &freeExchangeAutomatic{depositScope: a.freeAutomaticIdentity(), statusScope: a.freeAutomaticIdentity()}
		r.statusScope.account, r.statusScope.asset = "", ""
		a.freeData().automatic = r
		switch observation {
		case "pending":
			r.checking, r.kind = true, "deposit"
		case "failure":
			r.depositReason = "Old selected network reason."
		case "status":
			r.statusReason = "Old connection reason."
		}
		if observation == "status" {
			a.fields["region"] = "https://api.bybit.eu"
		} else {
			for _, asset := range a.network(source.Network).Assets {
				if asset.ID != saved.SourceAsset && exchangeAssetAllowed(source, asset) {
					a.event(webui.Event{ID: "field/freeAsset", Value: asset.ID})
					break
				}
			}
		}
		text := viewText(a.freeAutomaticReadNodes())
		if strings.Contains(text, "Old selected") || strings.Contains(text, "Old connection") || strings.Contains(text, "Reading the selected Bybit deposit address") {
			t.Fatal("render applied a stale read before the next scheduler tick", observation, text)
		}
	}
}

func TestFreeAutomaticReadGatesAndChronologyCannotReopenOriginals(t *testing.T) {
	for _, scenario := range []string{"closed", "locked", "recovery", "busy", "background", "inactive", "provider"} {
		a, _, backend, _, _ := automaticNativeFixture(t)
		backend.handler = func(_ uint64, method string, _, _ any) error {
			t.Error("ineligible read reached backend", scenario, method)
			return nil
		}
		switch scenario {
		case "closed":
			a.closed = true
		case "locked":
			a.state.Locked = true
		case "recovery":
			a.state.RecoveryConfirmed = false
		case "busy":
			a.busy = true
		case "background":
			a.background = true
		case "inactive":
			a.active = "group/0"
		case "provider":
			a.fields["exchangeProvider"] = "binance"
		}
		if a.nextFreeAutomaticRead() {
			t.Fatal("ineligible automatic read started", scenario)
		}
	}
	_, _, _, _, old := automaticNativeFixture(t)
	for _, change := range []string{"id", "step", "source", "asset", "amount", "original"} {
		next := old
		switch change {
		case "id":
			next.ID = strings.Repeat("c", 32)
		case "step":
			next.Step = 1
		case "source":
			next.SourceAccount = "foreign"
		case "asset":
			next.SourceAsset = "foreign"
		case "amount":
			next.DepositAmount = "7"
		case "original":
			next.DepositOperation = "a-different-original"
		}
		if freeAutomaticWizardContinuation(old, next) {
			t.Fatal("background chronology accepted a replacement/regression", change)
		}
	}
}
