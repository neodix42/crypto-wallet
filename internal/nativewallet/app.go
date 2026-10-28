// Package nativewallet presents public state and routes private signer prompts.
// Its View/fields never contain passwords, phrases, API secrets or private keys.
package nativewallet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"localwallet/internal/amount"
	"localwallet/internal/buildinfo"
	"localwallet/internal/chains/near"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/sessionwatch"
	"localwallet/internal/supervisor"
	"localwallet/internal/uiprompt"
	"localwallet/internal/webui"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type frontend interface {
	Render(webui.View)
	Dispatch(func())
	DispatchControl(func())
	Prompt(uiprompt.Prompt) (uiprompt.Reply, error)
	CancelPrompt()
	Close()
}
type backend interface {
	Call(string, any, any) error
	Generation() uint64
	CallAt(uint64, string, any, any) error
	Kill()
	ReplyPrompt(json.RawMessage) error
	SendActivity() error
}
type snapshot struct {
	Locked            bool            `json:"locked"`
	WalletExists      bool            `json:"walletExists"`
	VaultState        string          `json:"vaultState"`
	Environment       string          `json:"environment"`
	RecoveryConfirmed bool            `json:"recoveryConfirmed"`
	Accounts          []model.Account `json:"accounts"`
	AccountsCursor    string          `json:"accountsCursor"`
	TotalAccounts     int             `json:"totalAccounts"`
	Revision          string          `json:"revision"`
}
type journalPage struct {
	Operations []journal.Operation `json:"operations"`
	NextCursor string              `json:"nextCursor"`
	Revision   string              `json:"revision"`
	Total      int                 `json:"total"`
	Complete   bool                `json:"complete"`
}
type App struct {
	ui                                    frontend
	core                                  backend
	environment                           string
	state                                 snapshot
	networks                              []model.Network
	balances                              map[string][]model.Balance
	balanceErrors                         map[string]bool
	fields                                map[string]string
	active                                string
	tabs                                  []string
	sendForms                             map[string]bool
	plan                                  *model.Plan
	planIsStaking                         bool
	planRevision                          uint64
	staking                               map[string]near.StakingState
	stakingErrors                         map[string]bool
	stakingPending                        map[string]model.Plan
	stakingReading                        string
	stakingRevision                       uint64
	journal                               []journal.Operation
	journalCursor, journalRevision        string
	journalOffset                         int
	chain                                 model.HistoryPage
	exchange                              json.RawMessage
	quote                                 json.RawMessage
	exchangeRevision                      uint64
	result                                string
	resultOwner, chainOwner               string
	busy, background, closed              bool
	epoch                                 uint64
	coreGeneration                        uint64
	privateRevision                       atomic.Uint64
	status                                string
	statusRevision                        uint64
	monitorState                          string
	statusUntil, lastActivity, lastStatus time.Time
	balanceQueue                          []string
}

func Run(environment string) error {
	if environment != "mainnet" && environment != "testnet" {
		return errors.New("invalid profile")
	}
	// Reject a headless launch before starting the coordinator or opening the
	// profile. Production Wails initializes GTK in Run and panics when neither
	// display is configured; its development build returns an error instead.
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" && strings.TrimSpace(os.Getenv("WAYLAND_DISPLAY")) == "" {
		return errors.New("wallet desktop unavailable: no X11 or Wayland display configured")
	}
	app := &App{environment: environment, state: snapshot{Locked: true, VaultState: "checking"}, fields: map[string]string{"region": "https://api.bybit.com", "side": "Sell", "slippage": "0.5", "index": "1", "from": "0", "count": "10", "gap": "20", "limit": "1000", "birthday": "0", "blockBudget": "1000"}, balances: map[string][]model.Balance{}, balanceErrors: map[string]bool{}, sendForms: map[string]bool{}, active: "group/0", lastActivity: time.Now()}
	child := &supervisor.Child{Path: "/proc/self/exe", SelfRole: "--internal-core", Environment: environment}
	app.core = child
	window, err := webui.New(app.event)
	if err != nil {
		return err
	}
	app.ui = window
	monitorContext, stopMonitoring := context.WithCancel(context.Background())
	defer stopMonitoring()
	app.startSessionMonitor(monitorContext)
	child.OnPrompt = app.privatePrompt
	child.OnActivity = func() {}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-stop:
				window.DispatchControl(func() { app.close() })
				return
			case <-ticker.C:
				window.DispatchControl(app.tick)
			}
		}
	}()
	app.render()
	app.refresh()
	err = window.Run()
	app.core.Kill()
	return err
}

const unavailableMonitoring = "OS screen-lock monitoring is unavailable. Lock the wallet manually before leaving this session."

func (a *App) startSessionMonitor(ctx context.Context) {
	a.monitorState = "Checking OS screen-lock monitoring…"
	go func() {
		var failed atomic.Bool
		watcher, err := sessionwatch.Start(ctx, sessionwatch.Callbacks{
			Lock: func() {
				// Terminate the secret-bearing roles before waiting for the desktop
				// event loop, including while the OS is preparing to suspend.
				a.core.Kill()
				a.ui.DispatchControl(func() {
					if !a.closed {
						a.lock()
						a.notice("Wallet locked by the OS session monitor.")
						a.render()
					}
				})
			},
			Fault: func() {
				failed.Store(true)
				a.ui.DispatchControl(func() {
					if !a.closed {
						a.monitorState = unavailableMonitoring
						a.notice(unavailableMonitoring)
						a.render()
					}
				})
			},
		})
		a.ui.DispatchControl(func() {
			if a.closed {
				return
			}
			if err != nil || failed.Load() {
				a.monitorState = unavailableMonitoring
				a.notice(unavailableMonitoring)
			} else {
				a.monitorState = "OS screen-lock, suspend and shutdown monitoring is active."
			}
			a.render()
		})
		if err == nil {
			defer watcher.Close()
			<-ctx.Done()
		}
	}()
}
func (a *App) privatePrompt(raw json.RawMessage) error {
	revision := a.privateRevision.Load()
	p, err := uiprompt.DecodePrompt(raw)
	if err != nil {
		return err
	}
	generation := a.core.Generation()
	// The serialized UI queue checks lifecycle identity immediately before opening the
	// modal. A goroutine-side check alone would race a lock before queuing it.
	// Prompt data never enters public View, fields, status, logs or storage.
	a.ui.Dispatch(func() {
		if a.closed || a.core.Generation() != generation || a.privateRevision.Load() != revision {
			p.Phrase, p.Summary = "", ""
			return
		}
		reply, err := a.ui.Prompt(p)
		if err != nil {
			reply = uiprompt.Reply{PromptID: p.ID, Accepted: false}
		}
		p.Phrase = ""
		p.PNG = nil
		encoded, err := json.Marshal(reply)
		for i := range reply.Values {
			reply.Values[i] = ""
		}
		reply.Path = ""
		if err == nil {
			go func() {
				defer clear(encoded)
				if a.core.Generation() == generation && a.privateRevision.Load() == revision {
					_ = a.core.ReplyPrompt(encoded)
				}
			}()
		}
	})
	return nil
}
func (a *App) close() {
	if a.closed {
		return
	}
	a.closed = true
	a.epoch++
	a.privateRevision.Add(1)
	a.ui.CancelPrompt()
	a.core.Kill()
	a.coreGeneration = a.core.Generation()
	a.ui.Close()
}
func (a *App) lock() {
	a.epoch++
	a.privateRevision.Add(1)
	a.ui.CancelPrompt()
	a.core.Kill()
	a.coreGeneration = a.core.Generation()
	a.state.Locked = true
	a.state.Accounts = nil
	a.tabs = nil
	a.plan = nil
	a.planIsStaking = false
	a.planRevision++
	a.staking = nil
	a.stakingErrors = nil
	a.stakingPending = nil
	a.stakingReading = ""
	a.stakingRevision++
	a.balances = map[string][]model.Balance{}
	a.balanceQueue = nil
	a.exchange = nil
	a.quote = nil
	a.journal = nil
	a.journalCursor, a.journalRevision, a.journalOffset = "", "", 0
	a.chain = model.HistoryPage{}
	a.chainOwner, a.resultOwner = "", ""
	a.sendForms = map[string]bool{}
	a.result = ""
	a.busy = false
	a.background = false
	a.active = "group/0"
	for key := range a.fields {
		if strings.Contains(key, "recipient") || strings.Contains(key, "amount") || strings.Contains(key, "memo") || strings.HasPrefix(key, "stakingAmount/") {
			delete(a.fields, key)
		}
	}
	a.notice("Wallet locked.")
	a.render()
}
func (a *App) tick() {
	if a.closed {
		return
	}
	// This priority tick runs inside private modal loops too. Invalidate their
	// display/copy actions promptly if their coordinator session has ended.
	if a.core.Generation() != a.coreGeneration {
		a.lock()
		return
	}
	if time.Since(a.lastActivity) >= 5*time.Minute && (!a.state.Locked || a.busy) {
		a.lock()
		return
	}
	if a.status != "" && time.Now().After(a.statusUntil) {
		a.status = ""
		a.render()
	}
	if a.busy || a.background {
		return
	}
	if len(a.balanceQueue) > 0 {
		a.nextBalance()
		return
	}
	if time.Since(a.lastStatus) >= 5*time.Second {
		a.lastStatus = time.Now()
		a.pollStatus()
	}
}
func (a *App) notice(text string) {
	a.status = text
	a.statusRevision++
	a.statusUntil = time.Now().Add(7 * time.Second)
}
func safeError(err error) string {
	switch ipc.PublicErrorCode(err) {
	case "PASSWORD_POLICY":
		return "Use an app password of 12–4096 UTF-8 bytes."
	case "PASSWORD_MISMATCH":
		return "The passwords did not match."
	case "INVALID_MNEMONIC":
		return "Enter a valid English BIP39 recovery phrase."
	case "INPUT_TOO_LONG":
		return "An input exceeds 4,096 UTF-8 bytes."
	case "REQUEST_CANCELLED":
		return "Request cancelled."
	}
	return "Request failed or is unavailable. Balance remains unknown until verified; reconcile pending operations before retrying."
}
func (a *App) work(title string, fn func(uint64) (func(), error)) {
	if a.busy || a.closed {
		return
	}
	a.busy = true
	a.notice(title)
	a.render()
	epoch := a.epoch
	generation := a.core.Generation()
	go func() {
		apply, err := fn(generation)
		dispatch := a.ui.Dispatch
		if err != nil {
			// The signer can fail while its private modal is still open even
			// though the coordinator process survived. Revoke that modal through
			// the lifecycle queue; ordinary callbacks wait for modal dismissal.
			dispatch = a.ui.DispatchControl
		}
		dispatch(func() {
			if a.closed || a.epoch != epoch {
				return
			}
			if a.core.Generation() != generation {
				a.lock()
				return
			}
			a.busy = false
			a.stakingReading = ""
			if err != nil {
				a.privateRevision.Add(1)
				a.ui.CancelPrompt()
				a.notice(safeError(err))
			} else {
				if apply != nil {
					apply()
				}
				a.notice("Request completed. Check its returned state and any limitations.")
			}
			a.render()
		})
	}()
}
func (a *App) callAt(g uint64, method string, body any, out any) error {
	return a.core.CallAt(g, method, body, out)
}
func readAccounts(core backend, g uint64) (snapshot, error) {
	var first snapshot
	if err := core.CallAt(g, "accounts", struct{}{}, &first); err != nil {
		return first, err
	}
	if first.Locked {
		return first, nil
	}
	all := append([]model.Account(nil), first.Accounts...)
	seen := map[string]bool{}
	cursors := map[string]bool{}
	for _, account := range all {
		if account.ID == "" || seen[account.ID] {
			return snapshot{}, errors.New("invalid accounts")
		}
		seen[account.ID] = true
	}
	cursor := first.AccountsCursor
	for cursor != "" {
		if cursors[cursor] || len(cursors) >= 256 || len(all) >= 10000 {
			return snapshot{}, errors.New("invalid continuation")
		}
		cursors[cursor] = true
		var page snapshot
		if err := core.CallAt(g, "accounts", map[string]string{"cursor": cursor}, &page); err != nil {
			return snapshot{}, err
		}
		if page.Locked || page.Revision != first.Revision || page.TotalAccounts != first.TotalAccounts {
			return snapshot{}, errors.New("account generation changed")
		}
		for _, account := range page.Accounts {
			if account.ID == "" || seen[account.ID] {
				return snapshot{}, errors.New("duplicate account")
			}
			seen[account.ID] = true
			all = append(all, account)
		}
		cursor = page.AccountsCursor
	}
	if len(all) != first.TotalAccounts || len(all) > 10000 {
		return snapshot{}, errors.New("incomplete account page")
	}
	first.Accounts = all
	first.AccountsCursor = ""
	return first, nil
}
func (a *App) applySnapshot(state snapshot, networks []model.Network) {
	a.state = state
	a.networks = networks
	a.plan = nil
	a.planIsStaking = false
	a.planRevision++
	a.staking = nil
	a.stakingErrors = nil
	a.stakingRevision++
	a.quote = nil
	a.balances = map[string][]model.Balance{}
	a.balanceErrors = map[string]bool{}
	a.balanceQueue = nil
	if state.Locked {
		a.stakingPending = nil
		a.tabs = nil
		a.journal = nil
		a.exchange = nil
		a.active = "group/0"
		return
	}
	for id, plan := range a.stakingPending {
		account, ok := a.account(id)
		if !ok || account.Address != plan.Account.Address || account.PublicKey != plan.Account.PublicKey || account.Network != plan.Account.Network || account.Profile != plan.Account.Profile {
			delete(a.stakingPending, id)
		}
	}
	for _, account := range state.Accounts {
		if !account.Hidden {
			a.balanceQueue = append(a.balanceQueue, account.ID)
		}
	}
	a.ensureExchangeSelection()
}
func (a *App) refresh() {
	a.work("Reading local wallet…", func(g uint64) (func(), error) {
		state, err := readAccounts(a.core, g)
		if err != nil {
			return nil, err
		}
		var networks []model.Network
		if !state.Locked {
			err = a.callAt(g, "networks", struct{}{}, &networks)
		}
		return func() { a.applySnapshot(state, networks) }, err
	})
}
func (a *App) mutation(method string, body any) {
	a.work("Waiting for wallet confirmation…", func(g uint64) (func(), error) {
		var raw json.RawMessage
		if err := a.callAt(g, method, body, &raw); err != nil {
			return nil, err
		}
		clear(raw)
		switch method {
		case "receive", "copyReceive", "saveQR", "backupPhrase":
			// These authenticated presentation actions leave wallet state intact.
			// In particular, copying an address must not discard a prepared plan.
			return nil, nil
		}
		state, err := readAccounts(a.core, g)
		if err != nil {
			return nil, err
		}
		var networks []model.Network
		if !state.Locked {
			err = a.callAt(g, "networks", struct{}{}, &networks)
		}
		return func() { a.applySnapshot(state, networks) }, err
	})
}
func (a *App) pollStatus() {
	a.background = true
	epoch := a.epoch
	g := a.core.Generation()
	go func() {
		var state snapshot
		err := a.callAt(g, "status", struct{}{}, &state)
		a.ui.Dispatch(func() {
			if epoch != a.epoch || a.closed {
				return
			}
			if a.core.Generation() != g {
				a.lock()
				return
			}
			a.background = false
			if err != nil {
				return
			}
			if state.Locked && !a.state.Locked {
				a.lock()
			} else if state.Locked {
				a.state = state
				a.render()
			}
		})
	}()
}
func (a *App) nextBalance() {
	id := a.balanceQueue[0]
	a.balanceQueue = a.balanceQueue[1:]
	a.background = true
	epoch := a.epoch
	g := a.core.Generation()
	go func() {
		var values []model.Balance
		err := a.callAt(g, "balances", map[string]string{"accountId": id}, &values)
		a.ui.Dispatch(func() {
			if epoch != a.epoch || a.closed {
				return
			}
			if a.core.Generation() != g {
				a.lock()
				return
			}
			a.background = false
			if err != nil {
				a.balanceErrors[id] = true
				delete(a.balances, id)
			} else {
				delete(a.balanceErrors, id)
				a.balances[id] = values
			}
			a.render()
		})
	}()
}
func (a *App) account(id string) (model.Account, bool) {
	for _, v := range a.state.Accounts {
		if v.ID == id {
			return v, true
		}
	}
	return model.Account{}, false
}
func (a *App) network(id string) model.Network {
	for _, v := range a.networks {
		if v.ID == id {
			return v
		}
	}
	return model.Network{ID: id, Name: id}
}
func (a *App) asset(account model.Account) model.Asset {
	n := a.network(account.Network)
	selected := a.fields["asset/"+account.ID]
	for _, asset := range n.Assets {
		if asset.ID == selected {
			return asset
		}
	}
	if len(n.Assets) > 0 {
		return n.Assets[0]
	}
	return model.Asset{Symbol: "?"}
}
func (a *App) balance(account model.Account, asset model.Asset) (string, string) {
	matches := 0
	var observed model.Balance
	for _, b := range a.balances[account.ID] {
		if b.Asset == asset.ID {
			matches++
			observed = b
		}
	}
	if matches == 1 && !a.balanceErrors[account.ID] && completeObservation(observed) {
		value, err := amount.Format(observed.Atomic, asset.Decimals)
		if err == nil {
			return trimDecimal(value), "Provider observed · " + observed.State + " · " + observed.At
		}
	}
	if account.Family == "decred" && a.network(account.Network).RPC == "" {
		return "Connection required", "Configure authenticated TLS dcrd RPC before reading a verified balance."
	}
	return "Unknown", "Balance unavailable; no balance has been verified."
}

func completeObservation(b model.Balance) bool {
	if b.State == "" || b.At == "" || b.Atomic == "" || len(b.Atomic) > 256 || len(b.Atomic) > 1 && b.Atomic[0] == '0' {
		return false
	}
	for _, digit := range b.Atomic {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	state := strings.ToLower(b.State)
	for _, invalid := range []string{"unknown", "unavailable", "synchroniz", "stale", "pending", "error", "failed", "incomplete"} {
		if strings.Contains(state, invalid) {
			return false
		}
	}
	return true
}
func (a *App) event(e webui.Event) {
	if a.closed {
		return
	}
	if e.ID == "activity" {
		a.lastActivity = time.Now()
		pulse := !a.busy && !a.background && !a.state.Locked
		generation := a.core.Generation()
		go func() {
			_ = a.core.SendActivity()
			if pulse {
				_ = a.core.CallAt(generation, "activity", struct{}{}, nil)
			}
		}()
		return
	}
	a.lastActivity = time.Now()
	if e.ID == "close" {
		a.close()
		return
	}
	if e.ID == "lock" {
		a.lock()
		return
	}
	if e.ID == "tab" {
		a.active = e.Value
		a.render()
		if e.Value == "exchange" && !a.state.Locked {
			a.exchangeAction("exchangeStatus", map[string]string{})
		}
		return
	}
	if e.ID == "closeTab" {
		if strings.HasPrefix(e.Value, "wallet/") {
			id := strings.TrimPrefix(e.Value, "wallet/")
			for i, v := range a.tabs {
				if v == id {
					a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
					break
				}
			}
			a.active = "group/0"
			a.render()
		}
		return
	}
	if strings.HasPrefix(e.ID, "field/") {
		key := strings.TrimPrefix(e.ID, "field/")
		for _, prefix := range []string{"stakingPool/", "stakingAmount/"} {
			if strings.HasPrefix(key, prefix) {
				if _, pending := a.pendingStaking(strings.TrimPrefix(key, prefix)); pending {
					a.notice("Cancel the original prepared staking operation before changing its contract or amount.")
					a.render()
					return
				}
			}
		}
		if len(e.Value) <= 4096 {
			a.fields[key] = e.Value
		}
		if strings.HasPrefix(key, "recipient/") || strings.HasPrefix(key, "amount/") || strings.HasPrefix(key, "memo/") {
			hadPlan := a.plan != nil
			a.plan = nil
			a.planRevision++
			if hadPlan {
				a.render()
			}
		}
		if strings.HasPrefix(key, "stakingPool/") || strings.HasPrefix(key, "stakingAmount/") {
			a.stakingRevision++
			if a.planIsStaking {
				a.plan = nil
				a.planIsStaking = false
				a.planRevision++
			}
			if strings.HasPrefix(key, "stakingPool/") {
				id := strings.TrimPrefix(key, "stakingPool/")
				delete(a.staking, id)
				delete(a.stakingErrors, id)
			}
			a.render()
		}
		if key == "group" {
			a.active = "group/" + e.Value
			a.render()
		}
		if key == "showHidden" {
			a.render()
		}
		if key == "exchangeAccount" {
			a.exchangeRevision++
			a.fields["exchangeAsset"] = ""
			a.quote = nil
			a.ensureExchangeSelection()
			a.render()
		}
		if key == "exchangeAsset" || key == "side" || key == "slippage" {
			a.exchangeRevision++
			a.quote = nil
			a.render()
		}
		return
	}
	if a.busy {
		return
	}
	switch e.ID {
	case "refresh":
		a.refresh()
	case "create12", "create24", "restore":
		method, words := "create", 12
		if e.ID == "create24" {
			words = 24
		}
		if e.ID == "restore" {
			method = "import"
		}
		a.mutation(method, map[string]int{"words": words})
	case "unlock", "confirmRecovery", "backupPhrase", "password":
		a.mutation(e.ID, struct{}{})
	case "addAccount":
		index, err := strconv.ParseUint(a.fields["index"], 10, 32)
		if err != nil {
			a.notice("Enter a valid account index.")
			a.render()
			return
		}
		a.mutation("addAccount", map[string]uint32{"index": uint32(index)})
	case "discoverAccounts":
		start, e1 := strconv.ParseUint(a.fields["from"], 10, 32)
		count, e2 := strconv.ParseUint(a.fields["count"], 10, 32)
		if e1 != nil || e2 != nil || count == 0 || start+count-1 > 4294967295 {
			a.notice("Enter valid bounded account indexes.")
			a.render()
			return
		}
		a.mutation("discoverAccounts", map[string]uint32{"start": uint32(start), "end": uint32(start + count - 1)})
	case "showHidden":
		a.fields["showHidden"] = e.Value
		a.render()
	case "settingsWallet", "settingsExchange", "settingsJournal":
		a.fields["settings"] = strings.TrimPrefix(e.ID, "settings")
		if e.ID == "settingsJournal" {
			a.loadJournal(false)
		} else if e.ID == "settingsExchange" {
			a.exchangeAction("exchangeStatus", struct{}{})
		} else {
			a.render()
		}
	case "journalRefresh":
		a.loadJournal(false)
	case "journalMore":
		a.loadJournal(true)
	case "journalPrevious":
		a.journalOffset = max(0, a.journalOffset-128)
		a.render()
	case "journalNext":
		if a.journalOffset+128 < len(a.journal) {
			a.journalOffset += 128
			a.render()
		} else if a.journalCursor != "" {
			a.loadJournal(true)
		}
	case "exchangeStatus":
		a.exchangeAction("exchangeStatus", struct{}{})
	case "exchangeConnect":
		a.exchangeAction("exchangeConnect", map[string]string{"endpoint": a.fields["region"]})
	case "exchangeDisconnect":
		a.exchangeAction("exchangeDisconnect", struct{}{})
	case "exchangeQuote", "exchangeDeposit", "exchangeWithdraw", "exchangeSwap":
		body := map[string]string{"accountId": a.fields["exchangeAccount"], "assetId": a.fields["exchangeAsset"]}
		if e.ID == "exchangeQuote" || e.ID == "exchangeSwap" {
			body["side"] = a.fields["side"]
		}
		if e.ID != "exchangeQuote" {
			body["amount"] = a.fields["exchangeAmount"]
		}
		if e.ID == "exchangeSwap" {
			body["slippage"] = a.fields["slippage"]
		}
		a.exchangeAction(e.ID, body)
	default:
		a.walletEvent(e.ID)
	}
}
func (a *App) walletEvent(action string) {
	parts := strings.SplitN(action, "/", 2)
	if len(parts) != 2 {
		return
	}
	kind, id := parts[0], parts[1]
	if a.tokenEvent(kind, id) {
		return
	}
	if a.stakingEvent(kind, id) {
		return
	}
	if kind == "open" {
		if _, ok := a.account(id); !ok {
			return
		}
		found := false
		for _, v := range a.tabs {
			found = found || v == id
		}
		if !found {
			if len(a.tabs) >= 64 {
				a.notice("Close a wallet tab before opening another.")
				a.render()
				return
			}
			a.tabs = append(a.tabs, id)
		}
		a.active = "wallet/" + id
		a.render()
		return
	}
	if kind == "nextRows" || kind == "prevRows" {
		offset, _ := strconv.Atoi(a.fields[id])
		if kind == "nextRows" {
			offset += 100
		} else {
			offset = max(0, offset-100)
		}
		a.fields[id] = strconv.Itoa(offset)
		a.render()
		return
	}
	if kind == "sendOperation" {
		a.paymentAction("send", id)
		return
	}
	if kind == "asset" {
		a.selectWalletAsset(id)
		return
	}
	if kind == "retry" || kind == "reconcile" || kind == "cancelPrepared" {
		a.paymentAction(kind, id)
		return
	}
	if kind == "exchangeReconcile" {
		a.exchangeAction(kind, map[string]string{"id": id})
		return
	}
	account, ok := a.account(id)
	if !ok {
		return
	}
	switch kind {
	case "sendForm":
		a.sendForms[id] = !a.sendForms[id]
		a.render()
	case "receive", "copyReceive", "saveQR":
		a.mutation(kind, map[string]string{"accountId": id})
	case "hideAccount":
		a.mutation(kind, map[string]any{"accountId": id, "hidden": !account.Hidden})
	case "configure":
		a.mutation(kind, map[string]string{"network": account.Network})
	case "associate":
		a.mutation(kind, map[string]string{"accountId": id, "address": a.fields["associate/"+id]})
	case "chainHistory", "chainMore":
		a.loadChain(id, kind == "chainMore")
	case "discovery":
		birthday, e1 := strconv.ParseUint(a.fields["birthday"], 10, 32)
		gap, e2 := strconv.ParseUint(a.fields["gap"], 10, 32)
		limit, e3 := strconv.ParseUint(a.fields["limit"], 10, 32)
		budget, e4 := strconv.ParseUint(a.fields["blockBudget"], 10, 32)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			a.notice("Enter valid discovery limits.")
			a.render()
			return
		}
		a.mutation(kind, map[string]any{"accountId": id, "birthday": birthday, "gap": gap, "limit": limit, "blockBudget": budget})
	case "prepare", "max":
		asset := a.asset(account)
		revision := a.planRevision
		transfer := model.Transfer{AccountID: id, Asset: asset.ID, Recipient: a.fields["recipient/"+id], Amount: a.fields["amount/"+id], Memo: a.fields["memo/"+id]}
		a.work("Preparing exact wallet payment…", func(g uint64) (func(), error) {
			var plan model.Plan
			err := a.callAt(g, kind, transfer, &plan)
			return func() {
				if a.planRevision != revision {
					return
				}
				a.plan = &plan
				a.planIsStaking = false
				if kind == "max" {
					value, e := amount.Format(plan.Amount, asset.Decimals)
					if e == nil {
						a.fields["amount/"+id] = value
					}
				}
			}, err
		})
	case "send":
		if a.plan != nil && !a.planIsStaking && a.plan.Account.ID == id {
			a.paymentAction("send", a.plan.ID)
		}
	}
}
func (a *App) paymentAction(method, id string) {
	owner := strings.TrimPrefix(a.active, "wallet/")
	for accountID, pending := range a.stakingPending {
		if pending.ID == id {
			owner = accountID
			break
		}
	}
	for _, operation := range a.journal {
		if operation.Plan.ID == id {
			owner = operation.Plan.Account.ID
			break
		}
	}
	a.work("Waiting for exact payment review…", func(g uint64) (func(), error) {
		var result json.RawMessage
		err := a.callAt(g, method, map[string]string{"id": id}, &result)
		value := publicText(result)
		return func() {
			a.result = value
			a.resultOwner = owner
			a.plan = nil
			a.planIsStaking = false
			for accountID, pending := range a.stakingPending {
				if pending.ID == id {
					delete(a.stakingPending, accountID)
				}
			}
			delete(a.staking, owner)
			delete(a.stakingErrors, owner)
			a.stakingRevision++
			if account, ok := a.account(strings.TrimPrefix(a.active, "wallet/")); ok {
				a.balanceQueue = append(a.balanceQueue, account.ID)
			}
		}, err
	})
}
func publicText(raw json.RawMessage) string {
	if len(raw) > 131072 {
		return "Result exceeds the display limit; inspect the paginated journal."
	}
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		return "Result unavailable."
	}
	if b.Len() > 65536 {
		return "Result exceeds the display limit; inspect the paginated journal."
	}
	return b.String()
}

func trimDecimal(value string) string {
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	if value == "" {
		return "0"
	}
	return value
}
func (a *App) loadJournal(more bool) {
	previous := append([]journal.Operation(nil), a.journal...)
	cursor, revision := a.journalCursor, a.journalRevision
	if !more {
		cursor = ""
		revision = ""
	}
	a.work("Reading local payment journal…", func(g uint64) (func(), error) {
		var page journalPage
		err := a.callAt(g, "history", map[string]string{"cursor": cursor}, &page)
		if err != nil {
			return nil, err
		}
		if len(page.Operations) > 128 || more && page.Revision != revision || more && len(previous)+len(page.Operations) > 10000 {
			return nil, errors.New("journal generation or limit")
		}
		return func() {
			if more {
				a.journalOffset = len(previous)
				a.journal = append(previous, page.Operations...)
			} else {
				a.journalOffset = 0
				a.journal = page.Operations
			}
			a.journalCursor = page.NextCursor
			a.journalRevision = page.Revision
			a.fields["settings"] = "Journal"
			a.active = "settings"
		}, nil
	})
}
func (a *App) loadChain(id string, more bool) {
	if more && a.chainOwner != id {
		a.notice("Refresh this wallet's history before requesting its next page.")
		a.render()
		return
	}
	previous := append([]model.Receipt(nil), a.chain.Entries...)
	cursor := ""
	if more {
		cursor = a.chain.NextCursor
	}
	a.work("Reading provider-observed chain history…", func(g uint64) (func(), error) {
		var page model.HistoryPage
		err := a.callAt(g, "chainHistory", map[string]string{"accountId": id, "cursor": cursor}, &page)
		if err != nil {
			return nil, err
		}
		if more && len(previous)+len(page.Entries) > 2048 {
			return nil, errors.New("history display budget")
		}
		return func() {
			if more {
				page.Entries = append(previous, page.Entries...)
			}
			a.chain = page
			a.chainOwner = id
		}, nil
	})
}
func (a *App) ensureExchangeSelection() {
	if _, ok := a.account(a.fields["exchangeAccount"]); !ok {
		for _, account := range a.state.Accounts {
			if !account.Hidden {
				a.fields["exchangeAccount"] = account.ID
				break
			}
		}
	}
	if account, ok := a.account(a.fields["exchangeAccount"]); ok {
		n := a.network(account.Network)
		for _, asset := range n.Assets {
			if asset.ID == a.fields["exchangeAsset"] && exchangeAssetAllowed(account, asset) {
				return
			}
		}
		a.fields["exchangeAsset"] = ""
		for _, asset := range n.Assets {
			if exchangeAssetAllowed(account, asset) {
				a.fields["exchangeAsset"] = asset.ID
				break
			}
		}
	}
}
func (a *App) exchangeAction(method string, body any) {
	revision := a.exchangeRevision
	a.work("Waiting for Bybit observation or exact approval…", func(g uint64) (func(), error) {
		var result json.RawMessage
		err := a.callAt(g, method, body, &result)
		if err != nil {
			return nil, err
		}
		return func() {
			switch method {
			case "exchangeStatus", "exchangeConnect", "exchangeDisconnect":
				a.exchange = append(a.exchange[:0], result...)
				a.quote = nil
			case "exchangeQuote":
				if a.exchangeRevision == revision {
					a.quote = append(a.quote[:0], result...)
				}
			default:
				a.result = publicText(result)
				a.resultOwner = "exchange"
				a.quote = nil
			}
		}, nil
	})
}
func node(kind, id, text, value string) webui.Node {
	return webui.Node{Kind: kind, ID: id, Text: text, Value: value}
}
func label(text string) webui.Node   { return node("label", "", text, "") }
func heading(text string) webui.Node { return node("heading", "", text, "") }
func row(children ...webui.Node) webui.Node {
	return webui.Node{Kind: "row", Children: children}
}
func (a *App) button(id, text string, disabled bool) webui.Node {
	n := node("button", id, text, "")
	n.Disabled = disabled || a.busy
	return n
}
func (a *App) entry(key, title string) webui.Node {
	n := node("entry", "field/"+key, title, a.fields[key])
	n.Disabled = a.busy
	return n
}
func (a *App) combo(key, title string, options []webui.Option) webui.Node {
	n := node("combo", "field/"+key, title, a.fields[key])
	n.Options = options
	n.Disabled = a.busy
	return n
}
func (a *App) check(key, title string) webui.Node {
	n := node("check", "field/"+key, title, a.fields[key])
	n.Disabled = a.busy
	return n
}
func (a *App) groupSelector() webui.Node {
	seen := map[uint32]bool{}
	var ids []int
	for _, account := range a.state.Accounts {
		if !seen[account.Index] {
			seen[account.Index] = true
			ids = append(ids, int(account.Index))
		}
	}
	sort.Ints(ids)
	options := make([]webui.Option, 0, len(ids))
	for _, index := range ids {
		options = append(options, webui.Option{ID: strconv.Itoa(index), Text: groupName(uint32(index))})
	}
	return a.combo("group", "Account group", options)
}
func groupName(index uint32) string {
	if index == 0 {
		return "Main account"
	}
	return fmt.Sprintf("Account %d", uint64(index)+1)
}
func (a *App) render() {
	if a.closed {
		return
	}
	v := a.view()
	a.ui.Render(v)
}
func (a *App) view() webui.View {
	view := webui.View{Title: "CryptoWallet · " + buildinfo.Version + " · " + a.environment, Status: a.status, StatusRevision: a.statusRevision, Active: a.active}
	if a.state.Locked {
		title := "Unlock local wallet"
		nodes := []webui.Node{heading(title)}
		switch a.state.VaultState {
		case "missing":
			nodes = []webui.Node{heading("Create your app password"), label("Choose your app password in the private wallet dialog. Go generates one recovery phrase for all supported wallets. Record the words offline, then confirm them. Restore an existing phrase instead to recover the same wallets."), row(a.button("create12", "Create wallet · 12 words", false), a.button("create24", "Create wallet · 24 words", false), a.button("restore", "Restore from recovery phrase", false))}
		case "present":
			nodes = append(nodes, label("Your encrypted wallet is on this device. Unlock through its private password dialog."), a.button("unlock", "Unlock", false))
		case "checking":
			nodes = append(nodes, label("Checking encrypted wallet state…"))
		default:
			nodes = append(nodes, label("Wallet storage is unavailable. Creation and restoration are blocked until its state can be read safely."))
		}
		nodes = append(nodes, a.button("refresh", "Retry storage check", false))
		view.Pages = []webui.Page{{ID: "group/0", Title: "Wallet", Nodes: nodes}, {ID: "settings", Title: "Settings", Nodes: []webui.Node{label("Unlock the local wallet to configure it."), label(a.monitorState), label("Portable Go desktop. No Electron, SUID helper or sandbox setup. Wallet data stays in your existing localwallet configuration directory.")}}}
		if view.Active != "settings" {
			view.Active = "group/0"
		}
		return view
	}
	seen := map[uint32]bool{}
	var indexes []uint32
	for _, account := range a.state.Accounts {
		if !seen[account.Index] {
			seen[account.Index] = true
			indexes = append(indexes, account.Index)
		}
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })
	activeGroup := a.fields["group"]
	if strings.HasPrefix(a.active, "group/") {
		activeGroup = strings.TrimPrefix(a.active, "group/")
		a.fields["group"] = activeGroup
	}
	position := 0
	for i, index := range indexes {
		if strconv.FormatUint(uint64(index), 10) == activeGroup {
			position = i
			break
		}
	}
	start := position / 8 * 8
	end := start + 8
	if end > len(indexes) {
		end = len(indexes)
	}
	for _, index := range indexes[start:end] {
		view.Pages = append(view.Pages, a.groupPage(index))
	}
	for _, id := range a.tabs {
		if account, ok := a.account(id); ok && (!account.Hidden || a.fields["showHidden"] == "true") {
			view.Pages = append(view.Pages, a.walletPage(account))
		}
	}
	view.Pages = append(view.Pages, webui.Page{ID: "exchange", Title: "Exchange", Nodes: a.exchangePage()}, webui.Page{ID: "settings", Title: "Settings", Nodes: a.settingsPage()})
	found := false
	for _, page := range view.Pages {
		found = found || page.ID == view.Active
	}
	if !found && len(view.Pages) > 0 {
		view.Active = view.Pages[0].ID
	}
	return view
}
func (a *App) groupPage(index uint32) webui.Page {
	page := webui.Page{ID: "group/" + strconv.FormatUint(uint64(index), 10), Title: groupName(index), Nodes: []webui.Node{a.groupSelector(), row(a.button("refresh", "Refresh wallet state", false), a.button("lock", "Lock wallet", false))}}
	if !a.state.RecoveryConfirmed {
		page.Nodes = append(page.Nodes, label("Record and confirm the recovery phrase to enable receiving and spending."), a.button("confirmRecovery", "Record and confirm recovery phrase", false))
	}
	page.Nodes = append(page.Nodes, row(heading("Network"), heading("Native balance"), heading("Receive address")))
	count := 0
	for _, account := range a.state.Accounts {
		if account.Index == index && (!account.Hidden || a.fields["showHidden"] == "true") {
			count++
		}
	}
	key := "rows/" + strconv.FormatUint(uint64(index), 10)
	offset, _ := strconv.Atoi(a.fields[key])
	if offset < 0 || offset >= count {
		offset = 0
	}
	cursor := 0
	for _, account := range a.state.Accounts {
		if account.Index != index || account.Hidden && a.fields["showHidden"] != "true" {
			continue
		}
		cursor++
		if cursor <= offset || cursor > offset+100 {
			continue
		}
		network := a.network(account.Network)
		native := model.Asset{Symbol: "?"}
		if len(network.Assets) > 0 {
			// The registry lists the native asset first. HyperCore also pins
			// its native asset identity in Contract, unlike EVM native assets.
			native = network.Assets[0]
		}
		for _, asset := range network.Assets {
			if asset.Contract == "" {
				native = asset
				break
			}
		}
		value, observation := a.balance(account, native)
		balance := label(value + " " + native.Symbol)
		balance.Tooltip = observation
		address := node("address", "", "", account.Address)
		r := row(a.button("open/"+account.ID, network.Name, false), balance, address, a.button("copyReceive/"+account.ID, "⧉", !a.state.RecoveryConfirmed))
		r.ID = "open/" + account.ID
		page.Nodes = append(page.Nodes, r)
	}
	if count > 100 {
		page.Nodes = append(page.Nodes, label(fmt.Sprintf("Showing %d–%d of %d wallets; use the page controls to access the rest.", offset+1, min(offset+100, count), count)), row(a.button("prevRows/"+key, "Previous wallets", offset == 0), a.button("nextRows/"+key, "Next wallets", offset+100 >= count)))
	}
	return page
}
func (a *App) walletPage(account model.Account) webui.Page {
	asset := a.asset(account)
	network := a.network(account.Network)
	value, _ := a.balance(account, asset)
	page := webui.Page{ID: "wallet/" + account.ID, Title: asset.Symbol + " · " + network.Name}
	page.Nodes = []webui.Node{heading(network.Name + " · " + asset.Symbol), row(node("address", "", "Receive address", account.Address), a.button("copyReceive/"+account.ID, "⧉ Copy address", !a.state.RecoveryConfirmed)), heading(value + " " + asset.Symbol)}
	if account.Family == "near" {
		section := a.fields["walletSection/"+account.ID]
		if section != "Staking" {
			section = "Wallet"
		}
		tabs := row(a.button("walletSection/"+account.ID+"/Wallet", "Wallet", section == "Wallet"), a.button("walletSection/"+account.ID+"/Staking", "Staking", section == "Staking"))
		tabs.ID, tabs.Text, tabs.Value = "walletSections/"+account.ID, "NEAR wallet sections", section
		page.Nodes = append(page.Nodes, tabs)
		if section == "Staking" {
			page.Nodes = append(page.Nodes, a.stakingNodes(account)...)
			return page
		}
	}
	if account.Family == "evm" {
		if asset.Contract != "" {
			page.Nodes = append(page.Nodes, node("address", "", "Selected ERC-20 contract · "+account.Network+" (not your receive address)", asset.Contract), label("Decimals: "+strconv.Itoa(int(asset.Decimals))+". Native ETH pays transaction fees on this network."))
		}
	}
	if !a.state.RecoveryConfirmed {
		page.Nodes = append(page.Nodes, label("Receiving and spending require recorded-word confirmation."), a.button("confirmRecovery", "Record and confirm recovery phrase", false))
	}
	page.Nodes = append(page.Nodes, row(a.button("sendForm/"+account.ID, "Send", false), a.button("receive/"+account.ID, "Receive", false), a.button("saveQR/"+account.ID, "Save receive QR", !a.state.RecoveryConfirmed), a.button("refresh", "Refresh", false)))
	if a.sendForms[account.ID] {
		page.Nodes = append(page.Nodes, heading("Send "+asset.Symbol), a.entry("recipient/"+account.ID, "Recipient address"), a.entry("amount/"+account.ID, "Amount (1 means 1 "+asset.Symbol+")"), a.entry("memo/"+account.ID, "Memo / tag (when required)"), row(a.button("prepare/"+account.ID, "Prepare payment", !a.state.RecoveryConfirmed), a.button("max/"+account.ID, "Maximum spend", !a.state.RecoveryConfirmed)))
		if a.plan != nil && !a.planIsStaking && a.plan.Account.ID == account.ID {
			amountText, _ := amount.Format(a.plan.Amount, asset.Decimals)
			fee := a.plan.MaxFee
			symbol := a.plan.FeeAsset
			for _, item := range network.Assets {
				if item.ID == a.plan.FeeAsset {
					fee, _ = amount.Format(a.plan.MaxFee, item.Decimals)
					symbol = item.Symbol
				}
			}
			page.Nodes = append(page.Nodes, label("Prepared payment\nNetwork: "+account.Network+"\nFrom: "+account.Address+"\nTo: "+a.plan.Recipient+"\nAmount: "+amountText+" "+asset.Symbol+"\nMaximum fee: "+fee+" "+symbol+"\nMemo: "+a.plan.Memo+"\nExpires: "+time.Unix(a.plan.Expires, 0).UTC().Format(time.RFC3339)), a.button("send/"+account.ID, "Review and send", false), a.button("cancelPrepared/"+a.plan.ID, "Cancel prepared payment", false))
		}
	}
	if account.Family == "evm" {
		page.Nodes = append(page.Nodes, a.walletAssets(account, asset)...)
	}
	page.Nodes = append(page.Nodes, row(a.button("configure/"+account.ID, "Configure trusted RPC", false), a.button("hideAccount/"+account.ID, map[bool]string{true: "Show wallet", false: "Hide wallet"}[account.Hidden], false), a.button("chainHistory/"+account.ID, "Read chain history", false)))
	if account.Family == "near" || account.Family == "aptos" {
		page.Nodes = append(page.Nodes, a.entry("associate/"+account.ID, "Named / rotated on-chain account address"), a.button("associate/"+account.ID, "Verify and associate on-chain account", false))
	}
	if account.Family == "decred" {
		page.Nodes = append(page.Nodes, label("Decred requires an authenticated TLS dcrd connection and bounded recovery scan."), row(a.entry("birthday", "Birthday height"), a.entry("gap", "Gap"), a.entry("limit", "Address limit"), a.entry("blockBudget", "Block budget")), a.button("discovery/"+account.ID, "Configure Decred discovery", false))
	}
	if a.chainOwner == account.ID {
		page.Nodes = append(page.Nodes, label("Provider-observed history: "+a.chain.Scope))
		if len(a.chain.Entries) == 0 {
			page.Nodes = append(page.Nodes, label("No entries in this observed page. This does not establish zero funds or payment finality."))
		}
		for _, receipt := range a.chain.Entries {
			page.Nodes = append(page.Nodes, label(receipt.Identity+" · "+receipt.State+" · "+receipt.Block+" · "+receipt.Detail))
		}
		if a.chain.NextCursor != "" {
			page.Nodes = append(page.Nodes, a.button("chainMore/"+account.ID, "Next chain-history page", false))
		}
	}
	if a.result != "" && a.resultOwner == account.ID {
		page.Nodes = append(page.Nodes, label(a.result))
	}
	return page
}
func (a *App) settingsPage() []webui.Node {
	nodes := []webui.Node{row(a.button("settingsWallet", "Wallet", false), a.button("settingsExchange", "Exchange", false), a.button("settingsJournal", "Journal", false)), label(a.monitorState)}
	switch a.fields["settings"] {
	case "Exchange":
		options := []webui.Option{}
		for _, endpoint := range bybit.Endpoints() {
			options = append(options, webui.Option{ID: endpoint, Text: endpoint})
		}
		nodes = append(nodes, heading("Bybit connection"), a.combo("region", "Official API region", options), label("Enter API key and HMAC secret only in the private password-authenticated wallet dialogs. Bybit credentials are stored encrypted and cannot be recovered from the wallet phrase."), row(a.button("exchangeConnect", "Connect Bybit", false), a.button("exchangeDisconnect", "Disconnect Bybit", false), a.button("exchangeStatus", "Refresh connection", false)), label(publicText(a.exchange)))
	case "Journal":
		nodes = append(nodes, heading("Local payment journal"), label("An unresolved payment must be reconciled by its original identity. A provider observation is not finality."), row(a.button("journalRefresh", "Refresh journal", false), a.button("journalPrevious", "Previous journal page", a.journalOffset == 0), a.button("journalNext", "Next journal page", a.journalOffset+128 >= len(a.journal) && a.journalCursor == "")))
		start := min(a.journalOffset, len(a.journal))
		end := min(start+128, len(a.journal))
		for _, operation := range a.journal[start:end] {
			amountText, symbol := operation.Plan.Amount+" atomic units", operation.Plan.Asset
			for _, asset := range a.network(operation.Plan.Account.Network).Assets {
				if asset.ID == operation.Plan.Asset {
					if value, err := amount.Format(operation.Plan.Amount, asset.Decimals); err == nil {
						amountText, symbol = trimDecimal(value), asset.Symbol
					}
				}
			}
			title := operation.Plan.ID + " · " + operation.State + "\n" + operation.Plan.Account.Network + " · " + operation.Plan.Account.Address + " → " + operation.Plan.Recipient + "\nAmount: " + amountText + " " + symbol
			if staking, ok := stakingJournalDescription(operation.Plan); ok {
				title = operation.Plan.ID + " · " + operation.State + "\n" + staking
			}
			if operation.Receipt != nil {
				title += "\n" + operation.Receipt.Identity + " · " + operation.Receipt.State + " · " + operation.Receipt.Detail
			}
			nodes = append(nodes, label(title))
			switch operation.State {
			case "PREPARED":
				nodes = append(nodes, row(a.button("sendOperation/"+operation.Plan.ID, "Review original payment", false), a.button("cancelPrepared/"+operation.Plan.ID, "Cancel prepared payment", false)))
			case "SIGNED_NOT_SUBMITTED", "UNKNOWN":
				nodes = append(nodes, row(a.button("retry/"+operation.Plan.ID, "Broadcast original signed payment", false), a.button("reconcile/"+operation.Plan.ID, "Reconcile original payment", false)))
			case "SUBMITTED", "SUBMITTING", "REORGED":
				nodes = append(nodes, a.button("reconcile/"+operation.Plan.ID, "Reconcile original payment", false))
			}
		}
		if a.result != "" && a.resultOwner != "exchange" {
			nodes = append(nodes, label(a.result))
		}
	default:
		nodes = append(nodes, heading("Wallet settings"), row(a.button("password", "Change app password", false), a.button("backupPhrase", "Back up recovery phrase", !a.state.RecoveryConfirmed), a.button("lock", "Lock wallet", false)), a.check("showHidden", "Show hidden wallets"), row(a.entry("index", "Additional account index"), a.button("addAccount", "Add deterministic account", false)), row(a.entry("from", "Recover indexes from"), a.entry("count", "Number of indexes"), a.button("discoverAccounts", "Recover deterministic accounts", false)), label("The phrase restores canonical wallets, with any original optional BIP39 passphrase. It does not restore RPC credentials, named/rotated associations, labels or the local payment/exchange journal."))
	}
	return nodes
}
func (a *App) exchangePage() []webui.Node {
	var connection struct {
		Connected bool `json:"connected"`
	}
	_ = json.Unmarshal(a.exchange, &connection)
	disabled := !a.state.RecoveryConfirmed || !connection.Connected
	var accounts []webui.Option
	for _, account := range a.state.Accounts {
		if !account.Hidden {
			accounts = append(accounts, webui.Option{ID: account.ID, Text: a.network(account.Network).Name + " · " + groupName(account.Index)})
		}
	}
	var assets []webui.Option
	var address, contract string
	validSelection := false
	if account, ok := a.account(a.fields["exchangeAccount"]); ok {
		address = account.Address
		for _, asset := range a.network(account.Network).Assets {
			if !exchangeAssetAllowed(account, asset) {
				continue
			}
			assets = append(assets, webui.Option{ID: asset.ID, Text: asset.Symbol})
			if asset.ID == a.fields["exchangeAsset"] {
				contract = asset.Contract
				validSelection = true
			}
		}
	}
	disabled = disabled || !validSelection
	nodes := []webui.Node{heading("Bybit Exchange"), label("Assets deposited to Bybit are held by Bybit. Each deposit, spot trade, internal transfer and withdrawal requires its own exact confirmation in your private wallet dialog."), label("Imported ERC-20 tokens remain available in Wallet. This Exchange offers only the wallet's reviewed asset routes."), row(a.combo("exchangeAccount", "Wallet / network", accounts), a.combo("exchangeAsset", "Wallet asset", assets)), node("address", "", "Owned wallet receive address", address)}
	if contract != "" {
		nodes = append(nodes, node("address", "", "Token contract (not your receive address)", contract))
	}
	nodes = append(nodes, row(a.combo("side", "Spot direction", []webui.Option{{ID: "Sell", Text: "Sell asset for USDT"}, {ID: "Buy", Text: "Buy asset with USDT"}}), a.entry("exchangeAmount", "Amount to spend / withdraw"), a.entry("slippage", "Slippage limit (%)")), a.button("exchangeQuote", "Refresh market / withdrawal network", disabled), label(publicText(a.quote)), row(a.button("exchangeDeposit", "Deposit", disabled), a.button("exchangeSwap", "Swap", disabled || len(a.quote) == 0), a.button("exchangeWithdraw", "Withdraw to owned wallet", disabled)), a.button("exchangeStatus", "Refresh Bybit status", false), label(publicText(a.exchange)))
	if !connection.Connected {
		nodes = append(nodes, label("Connect your Bybit account in Settings → Exchange to enable these actions."))
	}
	var state struct {
		Operations []struct {
			ID    string `json:"id"`
			State string `json:"state"`
			Kind  string `json:"kind"`
		} `json:"operations"`
	}
	if json.Unmarshal(a.exchange, &state) == nil {
		for _, operation := range state.Operations {
			switch operation.State {
			case "UNKNOWN", "SUBMITTED", "SUBMITTING", "PENDING", "PARTIALLY_FILLED", "ACCEPTED":
				nodes = append(nodes, a.button("exchangeReconcile/"+operation.ID, "Reconcile original "+operation.Kind+" · "+operation.ID, false))
			}
		}
	}
	if a.result != "" && a.resultOwner == "exchange" {
		nodes = append(nodes, label(a.result))
	}
	return nodes
}
