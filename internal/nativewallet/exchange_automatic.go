package nativewallet

import (
	"encoding/json"
	"time"

	"localwallet/internal/exchange"
	"localwallet/internal/ipc"
	"localwallet/internal/webui"
)

const (
	freeAutomaticStatusTTL  = 15 * time.Second
	freeAutomaticDepositTTL = time.Minute
	freeAutomaticRetry      = 5 * time.Second
)

type freeAutomaticIdentity struct {
	provider, endpoint, account, asset string
	connection                         freeTrackingConnection
	connectionValid                    bool
}

type freeExchangeAutomatic struct {
	checking, statusFailed, depositFailed bool
	kind                                  string
	serial                                uint64
	statusScope, depositScope             freeAutomaticIdentity
	statusAt, depositAt                   time.Time
	statusReason, depositReason           string
}

func (a *App) freeAutomaticIdentity() freeAutomaticIdentity {
	connection, _, valid := a.freeTrackingStatus()
	return freeAutomaticIdentity{a.selectedExchangeProvider().ID, a.exchangeEndpoint(), a.fields["freeAccount"], a.fields["freeAsset"], connection, valid}
}

func freeAutomaticDue(at time.Time, failed bool, ttl time.Duration, now time.Time) bool {
	if failed {
		ttl = freeAutomaticRetry
	}
	return at.IsZero() || now.Before(at) || now.Sub(at) >= ttl
}

// This only observes connection state and the selected deposit destination.
// It never prepares an operation, asks for approval, sends funds or activity,
// changes a draft, or occupies the foreground work gate.
func (a *App) nextFreeAutomaticRead() bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.background || a.active != "exchange" || a.selectedExchangeProvider().ID != "bybit" {
		return false
	}
	w := a.exchangeWizardData()
	u := a.freeData()
	if u.automatic == nil {
		u.automatic = &freeExchangeAutomatic{}
	}
	r := u.automatic
	a.ensureFreeSource()
	identity := a.freeAutomaticIdentity()
	statusScope := identity
	statusScope.account, statusScope.asset = "", ""
	if r.statusScope != statusScope {
		r.statusScope, r.statusAt, r.statusReason, r.statusFailed = statusScope, time.Time{}, "", false
	}
	if r.depositScope != identity {
		r.depositScope, r.depositAt, r.depositReason, r.depositFailed = identity, time.Time{}, "", false
		u.preview = nil
	}
	if r.checking {
		return false
	}
	now := time.Now()
	account, asset, sourceOK := a.freeSource()
	connected := identity.connectionValid && identity.connection.Provider == "bybit" && identity.connection.Connected && exchange.ValidateProviderEndpoint("bybit", identity.connection.Endpoint)
	kind := ""
	if (w.viewStep == 1 || w.viewStep == 0) && connected && !r.statusFailed && sourceOK {
		// A source edit can clear a previously successful preview before the
		// next tick, including an away-and-back edit to the same descriptor.
		if !a.freePreviewCurrent() && !r.depositFailed {
			u.preview = nil
			r.depositAt = time.Time{}
		}
		if freeAutomaticDue(r.depositAt, r.depositFailed, freeAutomaticDepositTTL, now) {
			kind = "deposit"
		}
	}
	if kind == "" && freeAutomaticDue(r.statusAt, r.statusFailed, freeAutomaticStatusTTL, now) {
		kind = "status"
	}
	if kind == "" {
		return false
	}
	r.checking, r.kind = true, kind
	r.serial++
	serial := r.serial
	if kind == "deposit" {
		r.depositAt = now
	} else {
		r.statusAt = now
	}
	epoch, generation, revision, view := a.epoch, a.core.Generation(), a.exchangeRevision, w.viewStep
	priorStatus := string(a.exchange)
	var priorWizard exchange.WizardState
	hadWizard := w.state != nil
	if hadWizard {
		priorWizard = *w.state
	}
	a.render()
	go func() {
		var preview wizardDepositPreview
		var raw json.RawMessage
		var err error
		if kind == "deposit" {
			err = a.callAt(generation, "exchangeDepositPreview", map[string]string{"accountId": account.ID, "assetId": asset.ID}, &preview)
		} else {
			err = a.callAt(generation, "exchangeStatus", struct{}{}, &raw)
		}
		a.ui.Dispatch(func() {
			if r.serial != serial {
				return
			}
			r.checking, r.kind = false, ""
			stale := func() {
				if kind == "deposit" {
					r.depositAt = time.Time{}
				} else {
					r.statusAt = time.Time{}
				}
			}
			if a.closed || a.epoch != epoch {
				stale()
				return
			}
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			if a.state.Locked || a.busy || a.background || !a.state.RecoveryConfirmed || a.active != "exchange" || a.wizard != w || w.free != u || u.automatic != r || w.viewStep != view || a.exchangeRevision != revision || a.freeAutomaticIdentity() != identity || string(a.exchange) != priorStatus {
				stale()
				return
			}
			if kind == "deposit" {
				r.depositAt = time.Now()
				at, timestampErr := time.Parse(time.RFC3339, preview.ObservedAt)
				if err == nil && (preview.Provider != "bybit" || preview.AccountID != account.ID || preview.AssetID != asset.ID || preview.Network != account.Network || preview.Coin != asset.Symbol || preview.Chain == "" || preview.Address == "" || timestampErr != nil || time.Since(at) < -time.Second || time.Since(at) >= 2*time.Minute) {
					err = ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				r.depositFailed = err != nil
				if err != nil {
					u.preview = nil
					r.depositReason = safeError(err)
				} else {
					u.preview, r.depositReason = &preview, ""
				}
			} else {
				r.statusAt = time.Now()
				var status struct {
					Provider, Endpoint, WizardDepositState string
					Connected                              *bool
					Wizard                                 *exchange.WizardState
				}
				if err == nil && (len(raw) > 1<<20 || json.Unmarshal(raw, &status) != nil || status.Provider != "bybit" || status.Connected == nil || status.Endpoint != "" && !exchange.ValidateProviderEndpoint("bybit", status.Endpoint) || status.Connected != nil && *status.Connected && status.Endpoint == "") {
					err = ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
				}
				r.statusFailed = err != nil
				if err != nil {
					u.preview = nil
					r.statusReason = safeError(err)
				} else {
					a.exchange = append(json.RawMessage(nil), raw...)
					r.statusReason = ""
					current, _, valid := a.freeTrackingStatus()
					if !valid || current != identity.connection {
						u.preview, u.quote, u.pairs, u.destinations = nil, nil, nil, nil
						a.setFreeBalances([]wizardBalance{})
						u.balanceScope = ""
						u.balanceCheckedAt = time.Time{}
					}
					// Public status can advance the same original chronology, but
					// cannot restore an old visible step or overwrite entered drafts.
					if status.Wizard != nil && status.Wizard.Valid() && (!hadWizard && w.state == nil || hadWizard && w.state != nil && *w.state == priorWizard && freeAutomaticWizardContinuation(priorWizard, *status.Wizard)) {
						copy := *status.Wizard
						w.state, w.depositState = &copy, status.WizardDepositState
					}
					if w.viewStep == 0 {
						w.viewStep = 1
					}
					a.resumeFreeExchangeOperations()
					// The new connection tuple is now the successful status scope.
					r.statusScope = a.freeAutomaticIdentity()
					r.statusScope.account, r.statusScope.asset = "", ""
				}
			}
			a.render()
		})
	}()
	return true
}

func freeAutomaticWizardContinuation(old, next exchange.WizardState) bool {
	if next.ID != old.ID || next.Step < old.Step || next.SourceAccount != old.SourceAccount || next.SourceAsset != old.SourceAsset || next.DepositAmount != old.DepositAmount {
		return false
	}
	for _, pair := range [][2]string{{old.DepositOperation, next.DepositOperation}, {old.TransferOperation, next.TransferOperation}, {old.SwapOperation, next.SwapOperation}, {old.WithdrawOperation, next.WithdrawOperation}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return true
}

func (a *App) freeAutomaticReadNodes() []webui.Node {
	if a.selectedExchangeProvider().ID != "bybit" {
		return nil
	}
	u := a.freeData()
	r := u.automatic
	identity := a.freeAutomaticIdentity()
	statusScope := identity
	statusScope.account, statusScope.asset = "", ""
	statusCurrent := r != nil && r.statusScope == statusScope
	depositCurrent := r != nil && r.depositScope == identity
	var nodes []webui.Node
	if statusCurrent && r.statusReason != "" {
		nodes = append(nodes, node("label", "freeAutomaticStatus", "Bybit connection could not be refreshed. "+r.statusReason+" Checking again automatically.", ""))
	}
	if a.exchangeWizardData().viewStep > 1 {
		return nodes
	}
	message := "The selected deposit address loads automatically."
	connection, _, valid := a.freeTrackingStatus()
	if a.state.Locked || !a.state.RecoveryConfirmed {
		message = "Unlock the wallet to read the selected Bybit deposit address."
	} else if !valid || !connection.Connected {
		message = "Connect Bybit to load the selected deposit address automatically."
	} else if depositCurrent && r.checking && r.kind == "deposit" {
		message = "Reading the selected Bybit deposit address…"
	} else if depositCurrent && r.depositReason != "" {
		message = "Deposit address unavailable. " + r.depositReason + " Checking again automatically."
	} else if a.freePreviewCurrent() {
		return nodes
	}
	return append(nodes, node("label", "freeAutomaticDeposit", message, ""))
}
