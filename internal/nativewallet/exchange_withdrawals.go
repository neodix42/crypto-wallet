package nativewallet

import (
	"strings"
	"time"

	"localwallet/internal/ipc"
	"localwallet/internal/webui"
)

type freeWithdrawalRow struct {
	Source, OperationID, WithdrawalID, Coin, Chain, Address string
	Amount, AmountBasis, Fee, FeeBasis                      string
	Status, State, Reason, Code, CreatedAt, UpdatedAt       string
	TransactionID                                           string
}

type freeWithdrawalHistory struct {
	Rows              []freeWithdrawalRow
	ObservedAt, Scope string
	More              bool
	ReadError         string
}

type freeWithdrawalAddress struct {
	Coin, Chain, Address, State, ObservedAt, Reason, ReadError string
	Verified                                                   *bool
}

type freeWithdrawalUI struct {
	history      *freeWithdrawalHistory
	address      *freeWithdrawalAddress
	addressScope string
	connection   freeTrackingConnection
	generation   uint64
	checking     bool
	checkedAt    time.Time
	version      uint64
}

func (a *App) freeWithdrawalData() *freeWithdrawalUI {
	u := a.freeData()
	if u.withdrawals == nil {
		u.withdrawals = &freeWithdrawalUI{}
	}
	return u.withdrawals
}

func freeWithdrawalScope(d wizardDestination) string {
	return strings.Join([]string{d.AccountID, d.AssetID, d.Coin, d.Chain, d.Address}, "|")
}

func (a *App) currentWithdrawalData() (*freeWithdrawalUI, bool) {
	u := a.freeWithdrawalData()
	connection, _, valid := a.freeTrackingStatus()
	return u, valid && connection.Connected && connection.Provider == "bybit" && u.connection == connection && u.generation == a.core.Generation()
}

func (a *App) freeWithdrawalBlocked(d wizardDestination) bool {
	u, current := a.currentWithdrawalData()
	return current && u.address != nil && u.addressScope == freeWithdrawalScope(d) && time.Since(u.checkedAt) < 30*time.Second && (u.address.State == "locked24h" || u.address.State == "notListed")
}

func (a *App) freeWithdrawalAddressNodes(d wizardDestination) []webui.Node {
	u, current := a.currentWithdrawalData()
	if !current || u.address == nil || u.addressScope != freeWithdrawalScope(d) {
		return []webui.Node{label("Bybit address book: checking this exact address and network…")}
	}
	status := "Bybit address book: status unavailable."
	switch u.address.State {
	case "listed":
		status = "Bybit address book: listed"
		if u.address.Verified != nil {
			if *u.address.Verified {
				status += " · verified"
			} else {
				status += " · unverified"
			}
		}
		status += " · no new-address lock reported."
	case "locked24h":
		status = "Bybit address book: listed · new-address withdrawal lock (24 hours)."
	case "notListed":
		status = "Bybit address book: this exact address and network are not listed."
	}
	if u.address.Reason != "" {
		status += " " + u.address.Reason
	}
	n := label(status)
	n.Tooltip = "Bybit's address-book observation: " + u.address.ObservedAt + ". The API does not expose the account's global whitelist switch."
	return []webui.Node{n}
}

func withdrawalDate(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func withdrawalValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func (a *App) freeWithdrawalHistoryNodes(disabled bool) []webui.Node {
	u, current := a.currentWithdrawalData()
	information := node("label", "historyInfo/bybit-withdrawals", "", "")
	information.Tooltip = "Recent Bybit records cover the latest 30 days and local attempts. The table shows up to ten newest loaded requests. A completed request is Bybit's report; arrival is shown in the destination wallet."
	if current && u.history != nil && u.history.Scope != "" {
		information.Tooltip = u.history.Scope
	}
	toolbar := webui.Node{Kind: "row", ID: "historyToolbar/bybit-withdrawals", Children: []webui.Node{heading("Recent withdrawals"), information, a.button("freeRefreshWithdrawals", "Refresh withdrawals", disabled || u.checking)}}
	nodes := []webui.Node{toolbar}
	if !current || u.history == nil {
		return append(nodes, label("Withdrawal requests have not been loaded yet."))
	}
	if u.history.ReadError != "" {
		nodes = append(nodes, label("Withdrawal history is incomplete. Loaded local and verified provider records are shown. "+safeError(ipc.NewRequestError(u.history.ReadError))))
	}
	if len(u.history.Rows) == 0 {
		if u.history.ReadError == "" {
			nodes = append(nodes, label("No withdrawal requests in the loaded scope."))
		}
		return nodes
	}
	head := row(label("Date and time (UTC)"), label("Asset / network"), label("Amount"), label("Fee"), label("Destination"), label("Status"), label("Request ID"), label("Transaction ID"))
	table := webui.Node{Kind: "column", ID: "freeWithdrawalRequests", Children: []webui.Node{head}, Tooltip: u.history.Scope}
	for i, request := range u.history.Rows {
		if i == 10 {
			break
		}
		status := withdrawalValue(request.Status)
		if request.Reason != "" {
			status += "\n" + request.Reason
		}
		if request.Code != "" {
			status += "\n" + request.Code
		}
		id := request.WithdrawalID
		if id == "" && request.OperationID != "" {
			id = "Local " + request.OperationID
		}
		amount, fee := label(withdrawalValue(request.Amount)), label(withdrawalValue(request.Fee))
		amount.Tooltip, fee.Tooltip = request.AmountBasis, request.FeeBasis
		item := row(label(withdrawalDate(request.CreatedAt)), label(request.Coin+" · "+request.Chain), amount, fee, label(withdrawalValue(request.Address)), label(status), label(withdrawalValue(id)), label(withdrawalValue(request.TransactionID)))
		item.Tooltip = "Source: " + request.Source + "; state: " + request.State + "; updated: " + request.UpdatedAt
		table.Children = append(table.Children, item)
	}
	nodes = append(nodes, table)
	if u.history.More {
		nodes = append(nodes, label("Showing up to ten recent requests; older or unavailable requests may be missing."))
	}
	return nodes
}

// These reads are independent of payment authorization. The original request
// ledger is never changed by displaying a manual or app-created withdrawal.
func (a *App) nextFreeWithdrawalRefresh(manual bool) bool {
	if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.busy || a.active != "exchange" || a.selectedExchangeProvider().ID != "bybit" || a.wizard == nil || a.wizard.free == nil || a.wizard.viewStep != 5 {
		return false
	}
	connection, _, valid := a.freeTrackingStatus()
	if !valid || !connection.Connected || connection.Provider != "bybit" {
		return false
	}
	u := a.freeWithdrawalData()
	if u.checking {
		return false
	}
	destination, selected := a.freeDestination()
	scope := ""
	if selected {
		scope = freeWithdrawalScope(destination)
	}
	if !manual && u.connection == connection && u.generation == a.core.Generation() && u.addressScope == scope && time.Since(u.checkedAt) < 15*time.Second {
		return false
	}
	u.checking, u.checkedAt = true, time.Now()
	epoch, generation, revision, version := a.epoch, a.core.Generation(), a.exchangeRevision, u.version
	go func() {
		var history freeWithdrawalHistory
		err := a.callAt(generation, "exchangeWithdrawals", struct{}{}, &history)
		var address freeWithdrawalAddress
		var addressErr error
		if selected {
			addressErr = a.callAt(generation, "exchangeWithdrawalAddressStatus", map[string]string{"accountId": destination.AccountID, "assetId": destination.AssetID}, &address)
			if addressErr == nil && (address.Coin != destination.Coin || address.Chain != destination.Chain || address.Address != destination.Address) {
				addressErr = ipc.NewRequestError("PROVIDER_INVALID_RESPONSE")
			}
		}
		a.ui.Dispatch(func() {
			u.checking = false
			if a.closed || a.state.Locked || !a.state.RecoveryConfirmed || a.epoch != epoch || a.core.Generation() != generation || a.wizard == nil || a.wizard.free == nil || a.wizard.free.withdrawals != u || a.selectedExchangeProvider().ID != "bybit" {
				return
			}
			current, _, currentValid := a.freeTrackingStatus()
			if !currentValid || current != connection || a.exchangeRevision != revision || u.version != version || a.active != "exchange" || a.wizard.viewStep != 5 {
				u.checkedAt = time.Time{}
				return
			}
			u.connection, u.generation, u.addressScope = connection, generation, scope
			if err != nil {
				history = freeWithdrawalHistory{ReadError: ipc.ClassifyErrorCode(err)}
			}
			if len(history.Rows) > 10 {
				history.Rows = history.Rows[:10]
			}
			u.history = &history
			if selected {
				if addressErr != nil {
					address = freeWithdrawalAddress{State: "unknown", ReadError: ipc.ClassifyErrorCode(addressErr), Reason: safeError(addressErr)}
				}
				u.address = &address
			} else {
				u.address = nil
			}
			if manual {
				if err != nil {
					a.reportError("Read recent Bybit withdrawals", err)
				} else if history.ReadError != "" {
					a.reportError("Read recent Bybit withdrawals", ipc.NewRequestError(history.ReadError))
				} else {
					a.notice("Recent withdrawal requests updated.")
				}
			}
			a.render()
		})
	}()
	return true
}

func (a *App) freeOperationOutcome(title string, result wizardResult) {
	if result.State != "FAILED" && result.State != "UNKNOWN" {
		return
	}
	code := result.Code
	if code == "" {
		code = "REQUEST_FAILED_STATE_UNKNOWN"
	}
	message := result.Message
	if message == "" {
		message = "The original operation did not return a verified successful outcome."
	}
	message += "\nLocal operation: " + result.ID
	if result.RemoteID != "" {
		message += "\nBybit request: " + result.RemoteID
	}
	a.queueError(title, code, message)
}

func freeOperationInformation(result wizardResult) string {
	message := "Bybit " + result.Kind + ": " + result.State + "."
	if result.RemoteID != "" {
		message += " Request: " + result.RemoteID + "."
	}
	if result.Kind == "withdraw" && result.State != "EXCHANGE_COMPLETED" {
		message += " Its latest status is shown under Recent withdrawals."
	}
	return message
}
