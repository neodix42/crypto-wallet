package walletcore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"localwallet/internal/amount"
	"localwallet/internal/buildinfo"
	"localwallet/internal/chains"
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/hypercore"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/isolation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/profilepath"
	"localwallet/internal/strictjson"
	"localwallet/internal/supervisor"
	"localwallet/internal/vault"
)

type snapshot struct {
	Locked            bool                      `json:"locked"`
	WalletExists      bool                      `json:"walletExists"`
	VaultState        string                    `json:"vaultState"`
	Environment       string                    `json:"environment"`
	RecoveryConfirmed bool                      `json:"recoveryConfirmed"`
	NextAccountIndex  *uint32                   `json:"nextAccountIndex,omitempty"`
	Presentation      model.AccountPresentation `json:"presentation,omitempty,omitzero"`
	Accounts          []model.Account           `json:"accounts"`
	Operations        []journal.Operation       `json:"operations"`
	AccountsCursor    string                    `json:"accountsCursor"`
	TotalAccounts     int                       `json:"totalAccounts"`
	TotalOperations   int                       `json:"totalOperations"`
	Revision          string                    `json:"revision"`
}
type core struct {
	child           *supervisor.Child
	adapters        []model.Adapter
	adapterRevision uint64
	mu              sync.Mutex
	last            time.Time
	epoch           uint64
	active          bool
	promptPending   bool
	requestID       string
	requestSession  string
	requestEpoch    uint64
	publicChannel   *ipc.Channel
	sessionCtx      context.Context
	sessionCancel   context.CancelFunc
	historyScans    map[string]*historyScan
	historyControls map[string]*historyControl
	publicScans     map[string]publicContinuation
	newExchange     exchangeFactory
	marketData      marketPriceReader
}

func (c *core) lock() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endSession()
}

// endSession is called with mu held. Keep the old child from answering a new
// epoch's snapshot until it is dead, and drop configured RPC credential copies.
func (c *core) endSession() {
	c.epoch++
	c.active = false
	c.promptPending = false
	c.requestID, c.requestSession = "", ""
	if c.sessionCancel != nil {
		c.sessionCancel()
	}
	c.sessionCtx = nil
	c.sessionCancel = nil
	c.adapters = chains.All()
	c.adapterRevision++
	c.historyScans = nil
	c.historyControls = nil
	c.publicScans = nil
	c.child.Kill()
}

func (c *core) activity() {
	c.mu.Lock()
	c.last = time.Now()
	c.mu.Unlock()
}

// nativeActivity carries timing alone. Ordinary polling and clipboard events
// cannot extend a prompt; the private child reports key/button events only.
func (c *core) nativeActivity() {
	c.mu.Lock()
	if !c.promptPending || c.requestEpoch != c.epoch {
		c.mu.Unlock()
		return
	}
	c.last = time.Now()
	channel, id, session := c.publicChannel, c.requestID, c.requestSession
	c.mu.Unlock()
	if channel != nil && id != "" && session != "" {
		_ = channel.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "activity", Body: []byte(`{}`)})
	}
}

// relayPrompt forwards only a supervisor-validated signer prompt. The opaque
// transient body intentionally bypasses publicJSON and all persistent state.
func (c *core) relayPrompt(body json.RawMessage) error {
	c.mu.Lock()
	if !c.promptPending || c.requestEpoch != c.epoch || c.requestID == "" || c.requestSession == "" || c.publicChannel == nil {
		c.mu.Unlock()
		return errors.New("no active wallet prompt")
	}
	channel, id, session := c.publicChannel, c.requestID, c.requestSession
	c.mu.Unlock()
	return channel.Write(ipc.Message{Version: 1, ID: id, Session: session, Method: "prompt", Body: body})
}

func (c *core) replyPrompt(m ipc.Message) error {
	c.mu.Lock()
	valid := m.Version == 1 && m.Method == "promptReply" && m.Error == "" && c.promptPending && c.requestEpoch == c.epoch && c.requestID == m.ID && c.requestSession == m.Session
	c.mu.Unlock()
	if !valid {
		return errors.New("no active wallet prompt")
	}
	return c.child.ReplyPrompt(m.Body)
}

func (c *core) lockIfIdle(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if (!c.active && !c.promptPending) || now.Sub(c.last) < 5*time.Minute {
		return false
	}
	c.endSession()
	return true
}

func (c *core) adaptersSnapshot() []model.Adapter {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]model.Adapter(nil), c.adapters...)
}

func (c *core) adapter(network string) (model.Adapter, error) {
	return findAdapter(c.adaptersSnapshot(), network)
}

func findAdapter(adapters []model.Adapter, network string) (model.Adapter, error) {
	for _, a := range adapters {
		for _, n := range a.Networks() {
			if n.ID == network {
				return a, nil
			}
		}
	}
	return nil, errors.New("unsupported network")
}

func (c *core) childCallAt(epoch uint64, method string, body, out any) error {
	c.mu.Lock()
	if c.epoch != epoch {
		c.mu.Unlock()
		return errors.New("wallet session changed")
	}
	interactive := false
	switch method {
	case "create", "import", "unlock", "confirmRecovery", "backupPhrase", "password", "addNextAccount", "renameWallet", "renameGroup", "deleteWallet", "deleteGroup", "addTonAccount", "receive", "copyReceive", "saveQR", "scanQR", "configure", "discovery", "discoverAccounts", "associate", "sign", "importToken", "importNearToken", "exchangeConnect", "exchangeDisconnect", "exchangePrepare", "exchangeAuthorize", "exchangeWizardCancel":
		interactive = true
	}
	if interactive {
		c.promptPending = true
		if !c.active {
			// A locked prompt needs an initial budget even before its first key.
			// Active sessions retain their last real input time.
			c.last = time.Now()
		}
	}
	generation := c.child.Generation()
	c.mu.Unlock()
	if interactive {
		defer func() {
			c.mu.Lock()
			if c.epoch == epoch {
				c.promptPending = false
			}
			c.mu.Unlock()
		}()
	}
	return c.child.CallAt(generation, method, body, out)
}
func (c *core) current() (snapshot, error) {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	var s snapshot
	e := c.childCallAt(epoch, "snapshot", struct{}{}, &s)
	if e == nil {
		c.mu.Lock()
		if c.epoch != epoch {
			c.mu.Unlock()
			return snapshot{Locked: true}, errors.New("wallet session changed")
		}
		if s.Locked && c.active {
			// A fail-closed persistence error can lock the signer independently
			// of the UI. End the coordinator epoch and drop RPC credentials too.
			c.endSession()
			c.mu.Unlock()
			return s, nil
		}
		c.active = !s.Locked
		if !c.active {
			if c.sessionCancel != nil {
				c.sessionCancel()
			}
			c.sessionCtx, c.sessionCancel = nil, nil
		}
		if c.active && c.sessionCtx == nil {
			c.sessionCtx, c.sessionCancel = context.WithCancel(context.Background())
		}
		c.mu.Unlock()
	}
	return s, e
}
func (c *core) account(id string) (model.Account, error) {
	s, e := c.current()
	if e != nil || s.Locked {
		return model.Account{}, errors.New("wallet locked")
	}
	for _, a := range s.Accounts {
		if a.ID == id {
			return a, nil
		}
	}
	if s.AccountsCursor != "" {
		c.mu.Lock()
		epoch := c.epoch
		c.mu.Unlock()
		var account model.Account
		err := c.childCallAt(epoch, "account", struct {
			AccountID string `json:"accountId"`
		}{id}, &account)
		if err != nil || account.ID != id {
			return model.Account{}, errors.New("unknown account")
		}
		return account, nil
	}
	return model.Account{}, errors.New("unknown account")
}

// Main runs the legacy coordinator with its manifest-checked sibling signer.
func Main() { run(false) }

// MainPortable runs the same coordinator with a private signer subprocess of
// the current executable. No additional executable or manifest is required.
// The caller removes its internal role argument before entering this method.
func MainPortable() { run(true) }

// Keep the existing command-level test entry point inside the moved package.
func main() { Main() }

func run(portable bool) {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return
	}
	environment := flag.String("environment", "mainnet", "mainnet or explicitly selected testnet profile")
	flag.Parse()
	if *environment != "testnet" && *environment != "mainnet" {
		os.Exit(2)
	}
	if isolation.ProtectMemory() != nil {
		os.Exit(4)
	}
	exe, e := os.Executable()
	if e != nil {
		os.Exit(3)
	}
	home, e := os.UserConfigDir()
	if e != nil || profilepath.Prepare(home) != nil {
		os.Exit(3)
	}
	c := &core{child: signerChild(exe, home, *environment, portable), adapters: chains.All(), last: time.Now()}
	defer c.lock()
	done := make(chan struct{})
	defer close(done)
	ch := ipc.Channel{R: os.Stdin, W: os.Stdout}
	c.publicChannel = &ch
	c.child.OnActivity = c.nativeActivity
	c.child.OnPrompt = c.relayPrompt
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-tick.C:
				c.lockIfIdle(now)
			}
		}
	}()
	type queuedRequest struct {
		message ipc.Message
		epoch   uint64
	}
	work := make(chan queuedRequest, 16)
	go func() {
		for {
			var request queuedRequest
			select {
			case <-done:
				return
			case request = <-work:
			}
			select {
			case <-done:
				return
			default:
			}
			m := request.message
			out, e := c.handleAt(m, request.epoch)
			b, _ := publicJSON(out)
			r := ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: b}
			if e != nil {
				r.Body = []byte(`null`)
				r.Error = publicRequestErrorCode(e)
			}
			_ = ch.Write(r)
		}
	}()
	for {
		m, e := ch.Read()
		if e != nil {
			return
		}
		// Prompt answers reuse the active original request identity and must be
		// consumed while its worker is blocked. All normal actions still pass
		// the request replay guard below. Invalid controls close the session.
		if m.Method == "promptReply" {
			if !objectBody(m.Body) || c.replyPrompt(m) != nil {
				return
			}
			clear(m.Body)
			continue
		}
		if m.Method == "cancelHistory" {
			if c.cancelHistory(m) != nil {
				return
			}
			clear(m.Body)
			continue
		}
		if ch.Accept(m) != nil {
			return
		}
		if !objectBody(m.Body) || ((m.Method == "lock" || m.Method == "activity") && strictjson.Decode(m.Body, &struct{}{}) != nil) {
			_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`null`), Error: "INVALID_ARGUMENT"})
			continue
		}
		switch m.Method {
		case "lock":
			c.lock()
			_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`{"locked":true}`)})
		case "activity":
			c.activity()
			_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`null`)})
		default:
			c.mu.Lock()
			epoch := c.epoch
			c.mu.Unlock()
			if cancellableReadMethod(m.Method) {
				if c.registerHistoryControl(m, epoch) != nil {
					_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`null`), Error: "BUSY"})
					continue
				}
			}
			select {
			case work <- queuedRequest{m, epoch}:
			default:
				if cancellableReadMethod(m.Method) {
					c.finishHistoryControl(m, epoch)
				}
				_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`null`), Error: "BUSY"})
			}
		}
	}

}

func signerChild(executable, configuration, environment string, portable bool) *supervisor.Child {
	child := &supervisor.Child{Data: profilepath.Profile(configuration, environment), Environment: environment}
	if portable {
		// Pin the running image rather than the mutable download/install path.
		child.Path = "/proc/self/exe"
		child.SelfRole = "--internal-signer"
	} else {
		child.Path = filepath.Join(filepath.Dir(executable), "wallet-signer")
	}
	return child
}

func objectBody(body []byte) bool {
	body = bytes.TrimSpace(body)
	return len(body) > 0 && body[0] == '{'
}

func (c *core) handle(m ipc.Message) (any, error) {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	return c.handleAt(m, epoch)
}

func (c *core) handleAt(m ipc.Message, epoch uint64) (out any, err error) {
	if !objectBody(m.Body) {
		return nil, errors.New("request body must be an object")
	}
	c.mu.Lock()
	changed := c.epoch != epoch
	if !changed {
		c.requestID, c.requestSession, c.requestEpoch = m.ID, m.Session, epoch
	}
	c.mu.Unlock()
	if changed {
		return nil, errors.New("wallet session changed")
	}
	defer func() {
		c.mu.Lock()
		changed := c.epoch != epoch
		if c.requestEpoch == epoch && c.requestID == m.ID && c.requestSession == m.Session {
			c.requestID, c.requestSession = "", ""
		}
		c.mu.Unlock()
		if changed {
			out, err = nil, errors.New("wallet session changed")
		}
	}()
	if cancellableReadMethod(m.Method) {
		if err := c.registerHistoryControl(m, epoch); err != nil {
			return nil, err
		}
		defer func() {
			if c.finishHistoryControl(m, epoch) {
				out, err = nil, ipc.NewRequestError("REQUEST_CANCELLED")
			}
		}()
	}
	switch m.Method {
	case "networks", "status", "confirmRecovery", "backupPhrase":
		if strictjson.Decode(m.Body, &struct{}{}) != nil {
			return nil, errors.New("invalid fields")
		}
	}
	switch m.Method {
	case "marketPrices":
		if strictjson.Decode(m.Body, &struct{}{}) != nil {
			return nil, errors.New("invalid fields")
		}
		return c.marketPricesAt(epoch)
	case "pendingPayment":
		return c.pendingPaymentAt(m, epoch)
	case "tonWalletState", "prepareTonDeployment":
		return c.tonDeploymentAt(m, epoch)
	case "addressBook", "saveAddress", "removeAddress":
		return c.addressBookAt(m, epoch)
	case "discoverTokens":
		return c.discoverTokensAt(m, epoch)
	case "importToken":
		return c.importTokenAt(m, epoch)
	case "nearStakingState", "prepareNearStaking":
		return c.nearStakingAt(m, epoch)
	case "nearTokenStorage", "prepareNearTokenRegistration":
		return c.nearTokenStorageAt(m, epoch)
	case "exchangeStatus", "exchangeConnect", "exchangeSelect", "exchangeDisconnect", "exchangeQuote", "exchangeDepositPreview", "exchangeDeposit", "exchangeSwap", "exchangeWithdraw", "exchangeReconcile", "exchangeCancel", "exchangeWizardSave", "exchangeWizardLoad", "exchangeWizardCancel", "exchangeFundingBalances", "exchangeFundingTransfer", "exchangeFundingMove", "exchangeSwapOptions", "exchangeWithdrawOptions", "exchangeDepositCheck":
		return c.exchangeAt(m, epoch)
	case "networks":
		var networks []model.Network
		for _, a := range c.adaptersSnapshot() {
			for _, n := range a.Networks() {
				if n.Testnet == (c.child.Environment == "testnet") {
					networks = append(networks, n)
				}
			}
		}
		return networks, nil
	case "accounts":
		var b struct {
			Cursor strictjson.Text `json:"cursor,omitempty"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		return c.publicAccountsAt(epoch, m.Session, string(b.Cursor))
	case "status":
		s, err := c.current()
		s.Accounts = nil
		s.Presentation = model.AccountPresentation{}
		s.Operations = nil
		s.AccountsCursor = ""
		return struct {
			Version string `json:"version"`
			snapshot
		}{buildinfo.Version, s}, err
	case "create", "import", "unlock", "confirmRecovery", "backupPhrase", "password", "addAccount", "addNextAccount", "renameWallet", "renameGroup", "deleteWallet", "deleteGroup", "addTonAccount", "hideAccount", "discoverAccounts", "cancelPrepared", "receive", "copyReceive", "saveQR", "scanQR", "configure", "discovery":
		var out json.RawMessage
		if e := c.childCallAt(epoch, m.Method, json.RawMessage(m.Body), &out); e != nil {
			return nil, e
		}
		if m.Method == "unlock" || m.Method == "configure" {
			c.mu.Lock()
			c.historyScans = nil
			c.publicScans = nil
			c.mu.Unlock()
			if err := c.applySettingsAt(epoch); err != nil {
				return nil, err
			}
		}
		c.mu.Lock()
		if c.epoch != epoch {
			c.mu.Unlock()
			return nil, errors.New("wallet session changed")
		}
		c.active = true
		if c.sessionCtx == nil {
			c.sessionCtx, c.sessionCancel = context.WithCancel(context.Background())
		}
		c.mu.Unlock()
		if m.Method != "receive" && m.Method != "scanQR" && m.Method != "copyReceive" && m.Method != "saveQR" {
			return c.publicSnapshotResultAt(epoch, m.Session, out)
		}
		return out, nil
	case "associate":
		var b struct {
			AccountID string `json:"accountId"`
			Address   string `json:"address"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		a, e := c.account(b.AccountID)
		if e != nil {
			return nil, e
		}
		adapter, e := c.adapter(a.Network)
		if e != nil {
			return nil, e
		}
		ctx, cancel := c.networkContextAt(30*time.Second, epoch)
		defer cancel()
		var updated model.Account
		switch v := adapter.(type) {
		case *near.Adapter:
			updated, e = v.Associate(ctx, a, b.Address)
		case *aptos.Adapter:
			updated, e = v.Associate(ctx, a, b.Address)
		default:
			return nil, errors.New("association unavailable")
		}
		if e != nil {
			return nil, e
		}
		var out json.RawMessage
		e = c.childCallAt(epoch, "associate", updated, &out)
		if e != nil {
			return nil, e
		}
		return c.publicSnapshotResultAt(epoch, m.Session, out)
	case "chainHistory":
		var b struct {
			AccountID strictjson.Text `json:"accountId"`
			Cursor    strictjson.Text `json:"cursor,omitempty"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		return c.chainHistoryAt(epoch, m.Session, string(b.AccountID), string(b.Cursor))
	case "balanceBatch":
		return c.balanceBatchAt(m, epoch)
	case "balances":
		var b struct {
			AccountID string `json:"accountId"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		a, e := c.account(b.AccountID)
		if e != nil {
			return nil, e
		}
		adapter, e := c.adapter(a.Network)
		if e != nil {
			return nil, e
		}
		ctx, cancel := c.networkContextAt(30*time.Second, epoch)
		defer cancel()
		return adapter.Balance(ctx, a)
	case "history":
		var b struct {
			AccountID strictjson.Text `json:"accountId,omitempty"`
			Asset     strictjson.Text `json:"asset,omitempty"`
			Cursor    strictjson.Text `json:"cursor,omitempty"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		return c.publicJournalAt(epoch, m.Session, string(b.AccountID), string(b.Asset), string(b.Cursor))
	case "prepare", "max", "quotePayment":
		var t model.Transfer
		maximum, readOnly := m.Method == "max", m.Method != "prepare"
		if m.Method == "quotePayment" {
			var request paymentQuoteRequest
			if strictjson.Decode(m.Body, &request) != nil || request.Maximum == nil {
				return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_INPUT")
			}
			t, maximum = request.transfer(), *request.Maximum
		} else if e := strictjson.Decode(m.Body, &t); e != nil {
			return nil, e
		}
		if readOnly && !validPaymentQuoteInput(t, maximum) {
			return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_INPUT")
		}
		a, e := c.account(t.AccountID)
		if e != nil {
			if readOnly {
				return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_INPUT")
			}
			return nil, e
		}
		adapter, e := c.adapter(a.Network)
		if e != nil {
			if readOnly {
				return nil, ipc.NewRequestError("PAYMENT_QUOTE_UNAVAILABLE")
			}
			return nil, e
		}
		if readOnly && adapter.ValidateAddress(a.Network, t.Recipient) != nil {
			return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_INPUT")
		}
		found := false
		for _, n := range adapter.Networks() {
			if n.ID != a.Network {
				continue
			}
			for _, asset := range n.Assets {
				if asset.ID == t.Asset {
					found = true
					if !maximum {
						t.Amount, e = amount.Parse(t.Amount, asset.Decimals)
					}
				}
			}
		}
		if !found || e != nil {
			if readOnly {
				return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_INPUT")
			}
			return nil, errors.New("invalid asset or precision")
		}
		if !readOnly {
			pending, err := c.accountPaymentReservationAt(epoch, a)
			if err != nil {
				return nil, ipc.NewRequestError("PAYMENT_PENDING_UNAVAILABLE")
			}
			if pending != nil {
				return nil, ipc.NewRequestError("PAYMENT_PENDING")
			}
		}
		ctx, cancel := c.networkContextAt(45*time.Second, epoch)
		if readOnly {
			cancel()
			ctx, cancel = c.historyContextAt(45*time.Second, epoch, m.Session)
		}
		defer cancel()
		var p model.Plan
		if maximum {
			p, e = maxPlan(ctx, adapter, a, t)
		} else {
			p, e = adapter.Prepare(ctx, a, t)
		}
		if e != nil {
			if readOnly {
				return nil, paymentQuoteError(e)
			}
			return nil, e
		}
		var id [16]byte
		if _, e = rand.Read(id[:]); e != nil {
			return nil, e
		}
		if p.ID == "" {
			p.ID = hex.EncodeToString(id[:])
		}
		if p.Expires == 0 {
			p.Expires = time.Now().Add(5 * time.Minute).Unix()
		}
		if readOnly {
			if !validPaymentQuotePlan(adapter, a, t, p, maximum) {
				return nil, ipc.NewRequestError("PAYMENT_QUOTE_INVALID_PLAN")
			}
			// A quote is not a durable operation or authorization capability.
			// Explicit Prepare must observe state and fees again before recording.
			return p, nil
		}
		if e = c.childCallAt(epoch, "prepare", p, nil); e != nil {
			return nil, e
		}
		return p, nil
	case "send", "retry", "reconcile":
		var b struct {
			ID string `json:"id"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		return c.paymentAt(m.Method, b.ID, epoch)
	default:
		return nil, errors.New("unsupported operation")
	}
}
func (c *core) payment(method, id string) (any, error) {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	return c.paymentAt(method, id, epoch)
}

func (c *core) paymentAt(method, id string, epoch uint64) (any, error) {
	var operation journal.Operation
	if err := c.childCallAt(epoch, "operation", struct {
		ID string `json:"id"`
	}{id}, &operation); err != nil {
		return nil, err
	}
	if operation.Plan.ID != id || id == "" {
		return nil, errors.New("unknown payment")
	}
	op := &operation
	a, e := c.adapter(op.Plan.Account.Network)
	if e != nil {
		return nil, e
	}
	if method == "send" {
		if op.State != "PREPARED" {
			return nil, errors.New("payment already authorized or resolved")
		}
		var signed model.Signed
		if e = c.childCallAt(epoch, "sign", struct {
			ID string `json:"id"`
		}{id}, &signed); e != nil {
			return nil, e
		}
		op.Signed = &signed
		op.State = "SIGNED_NOT_SUBMITTED"
	}
	if op.Signed == nil {
		return nil, errors.New("payment has no signed artifact")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	var r model.Receipt
	if method == "reconcile" {
		r, e = a.Reconcile(ctx, op.Plan, *op.Signed)
	} else {
		if method == "retry" {
			r, e = a.Reconcile(ctx, op.Plan, *op.Signed)
			if e == nil && r.State != "UNKNOWN" {
				return c.recordAt(id, r, epoch)
			}
		}
		if op.State != "SIGNED_NOT_SUBMITTED" && op.State != "UNKNOWN" {
			return nil, errors.New("not eligible for identical resubmission")
		}
		c.mu.Lock()
		changed := epoch != c.epoch
		c.mu.Unlock()
		if changed {
			return nil, errors.New("wallet session locked")
		}
		if e = c.childCallAt(epoch, "update", struct {
			ID      string         `json:"id"`
			State   string         `json:"state"`
			Receipt *model.Receipt `json:"receipt"`
		}{id, "SUBMITTING", nil}, nil); e != nil {
			return nil, e
		}
		c.mu.Lock()
		changed = epoch != c.epoch
		c.mu.Unlock()
		if changed {
			return nil, errors.New("wallet locked before submission")
		}
		r, e = a.Submit(ctx, op.Plan, *op.Signed)
	}
	if e != nil {
		r = model.Receipt{Identity: op.Signed.Identity, State: "UNKNOWN", Detail: "Submission or query failed; reconcile original identity. No new payment was created."}
	}
	return c.recordAt(id, r, epoch)
}
func (c *core) recordAt(id string, r model.Receipt, epoch uint64) (any, error) {
	state := r.State
	switch state {
	case "CONFIRMED", "FAILED", "SUBMITTED", "UNKNOWN", "REPLACED", "REORGED":
	default:
		state = "UNKNOWN"
		r.State = state
	}
	e := c.childCallAt(epoch, "update", struct {
		ID      string         `json:"id"`
		State   string         `json:"state"`
		Receipt *model.Receipt `json:"receipt"`
	}{id, state, &r}, nil)
	return r, e
}

func (c *core) applySettings() error {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	return c.applySettingsAt(epoch)
}

func (c *core) applySettingsAt(epoch uint64) error {
	c.mu.Lock()
	if c.epoch != epoch {
		c.mu.Unlock()
		return errors.New("wallet session changed")
	}
	adapters := append([]model.Adapter(nil), c.adapters...)
	c.mu.Unlock()
	// Configure fresh instances. Existing public requests may still be winding
	// down after cancellation; mutating their endpoint maps would race them.
	for i, a := range adapters {
		switch a.(type) {
		case *near.Adapter:
			adapters[i] = near.New()
		case *aptos.Adapter:
			adapters[i] = aptos.New()
		case *decred.Adapter:
			adapters[i] = decred.New()
		case *ton.Adapter:
			adapters[i] = ton.New()
		}
	}
	var settings map[string]vault.Endpoint
	if e := c.childCallAt(epoch, "settings", struct{}{}, &settings); e != nil {
		return e
	}
	evms := map[string]string{}
	cores := map[string]string{}
	var tokens []model.ERC20Token
	var nearTokens []model.NEP141Token
	for _, adapter := range adapters {
		if _, ok := adapter.(*evm.Adapter); ok {
			if err := c.childCallAt(epoch, "tokenPolicies", struct{}{}, &tokens); err != nil {
				return err
			}
			break
		}
	}
	for _, adapter := range adapters {
		if _, ok := adapter.(*near.Adapter); ok {
			if err := c.childCallAt(epoch, "nearTokenPolicies", struct{}{}, &nearTokens); err != nil {
				return err
			}
			break
		}
	}
	for id, ep := range settings {
		adapter, e := findAdapter(adapters, id)
		if e != nil {
			return e
		}
		switch a := adapter.(type) {
		case *evm.Adapter:
			evms[id] = ep.URL
		case *hypercore.Adapter:
			cores[id] = ep.URL
		case *near.Adapter:
			if e = a.SetEndpoint(id, ep.URL); e != nil {
				return e
			}
		case *aptos.Adapter:
			if e = a.SetEndpoint(id, ep.URL); e != nil {
				return e
			}
		case *decred.Adapter:
			if e = a.Configure(id, decred.Endpoint{URL: ep.URL, User: ep.User, Password: ep.Password}); e != nil {
				return e
			}
		case *ton.Adapter:
			if e = a.Configure(id, ton.Endpoint{URL: ep.URL, APIKey: ep.APIKey}); e != nil {
				return e
			}
		}
	}
	for i, a := range adapters {
		switch a.(type) {
		case *near.Adapter:
			n, err := a.(*near.Adapter).WithTokens(nearTokens)
			if err != nil {
				return err
			}
			adapters[i] = n
		case *evm.Adapter:
			n, e := evm.NewWithEndpoints(evms)
			if e != nil {
				return e
			}
			n, e = n.WithTokens(tokens)
			if e != nil {
				return e
			}
			adapters[i] = n
		case *hypercore.Adapter:
			n, e := hypercore.NewWithEndpoints(cores)
			if e != nil {
				return e
			}
			adapters[i] = n
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != epoch {
		return errors.New("wallet session changed")
	}
	c.adapters = adapters
	c.adapterRevision++
	return nil
}

func (c *core) networkContext(d time.Duration) (context.Context, context.CancelFunc) {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	return c.networkContextAt(d, epoch)
}

func (c *core) networkContextAt(d time.Duration, epoch uint64) (context.Context, context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.epoch != epoch {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	}
	if c.sessionCtx == nil {
		c.sessionCtx, c.sessionCancel = context.WithCancel(context.Background())
	}
	return context.WithTimeout(c.sessionCtx, d)
}

// Only fixed categories cross the public IPC boundary, including failures
// discovered during a post-approval network recheck or broadcast.

// maximumSpendable is a trusted-adapter read-only capability. Core spot totals
// include order holds, which are visible holdings but cannot be sent.
type maximumSpendable interface {
	SpendableBalance(context.Context, model.Account, string) (string, error)
}

func maximumBalance(balances []model.Balance, asset string) (*big.Int, error) {
	var value *big.Int
	for _, b := range balances {
		if b.Asset != asset {
			continue
		}
		if value != nil {
			return nil, errQuoteBalance
		}
		state := strings.ToLower(strings.TrimSpace(b.State))
		if state == "" || strings.TrimSpace(b.At) == "" {
			return nil, errQuoteBalance
		}
		for _, invalid := range []string{"unknown", "unavailable", "synchroniz", "stale", "pending", "error", "failed", "incomplete"} {
			if strings.Contains(state, invalid) {
				return nil, errQuoteBalance
			}
		}
		var err error
		value, err = amount.Atomic(b.Atomic)
		if err != nil {
			return nil, errQuoteBalance
		}
	}
	if value == nil {
		return nil, errQuoteBalance
	}
	return value, nil
}

// maximumNativeReserve keeps protocol-required account storage and token-call
// attachments in their native currency. TON jetton attachment is already part
// of MaxFee; NEP-141's exact yoctoNEAR deposit is separate from its gas reserve.
func maximumNativeReserve(p model.Plan) (*big.Int, error) {
	reserve, deposit := "0", "0"
	switch p.Account.Family {
	case "near":
		var x near.Payload
		if strictjson.Decode(p.Payload, &x) != nil {
			return nil, errors.New("invalid NEAR max reserve")
		}
		reserve = x.StorageReserve
		if x.Token != nil {
			deposit = x.Token.Deposit
		}
	case "ton":
		var x ton.Payload
		if strictjson.Decode(p.Payload, &x) != nil {
			return nil, errors.New("invalid TON max reserve")
		}
		reserve = x.StorageReserve
	}
	r, err := amount.Atomic(reserve)
	if err != nil {
		return nil, err
	}
	d, err := amount.Atomic(deposit)
	if err != nil {
		return nil, err
	}
	return r.Add(r, d), nil
}

// maximumRecipientEqual permits only the address normalization already used by
// the selected adapter. It never trims or substitutes a user's destination.
func maximumRecipientEqual(a model.Adapter, ac model.Account, requested, prepared string) bool {
	if requested == "" || requested != strings.TrimSpace(requested) || prepared != strings.TrimSpace(prepared) || a.ValidateAddress(ac.Network, requested) != nil || a.ValidateAddress(ac.Network, prepared) != nil {
		return false
	}
	switch ac.Family {
	case "evm", "hypercore":
		return strings.EqualFold(requested, prepared)
	case "aptos":
		canonical := func(s string) (*big.Int, bool) {
			if len(s) < 3 || len(s) > 66 || !strings.HasPrefix(s, "0x") {
				return nil, false
			}
			for _, digit := range s[2:] {
				if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
					return nil, false
				}
			}
			return new(big.Int).SetString(s[2:], 16)
		}
		original, ok := canonical(requested)
		normalized, valid := canonical(prepared)
		return ok && valid && original.Cmp(normalized) == 0
	case "ton":
		parse := func(s string) (*address.Address, error) {
			if strings.Contains(s, ":") {
				return address.ParseRawAddr(s)
			}
			return address.ParseAddr(s)
		}
		original, err := parse(requested)
		normalized, failure := parse(prepared)
		return err == nil && failure == nil && original.Equals(normalized)
	default:
		return requested == prepared
	}
}

func maxPlan(ctx context.Context, a model.Adapter, ac model.Account, t model.Transfer) (model.Plan, error) {
	if t.AccountID != ac.ID || t.Asset == "" {
		return model.Plan{}, errors.New("invalid maximum account or asset")
	}
	assets := map[string]bool{}
	for _, network := range a.Networks() {
		if network.ID != ac.Network {
			continue
		}
		for _, asset := range network.Assets {
			if assets[asset.ID] {
				return model.Plan{}, errors.New("ambiguous maximum asset registry")
			}
			assets[asset.ID] = true
		}
	}
	if !assets[t.Asset] {
		return model.Plan{}, errors.New("unsupported maximum asset")
	}
	balances, err := a.Balance(ctx, ac)
	if err != nil {
		return model.Plan{}, err
	}
	balance, err := maximumBalance(balances, t.Asset)
	if err != nil {
		return model.Plan{}, err
	}
	if spendable, ok := a.(maximumSpendable); ok {
		value, err := spendable.SpendableBalance(ctx, ac, t.Asset)
		if err != nil {
			return model.Plan{}, err
		}
		available, err := amount.Atomic(value)
		if err != nil || available.Cmp(balance) > 0 {
			return model.Plan{}, errors.New("invalid or changed spendable balance; refresh before retrying")
		}
		balance = available
	}
	if balance.Sign() <= 0 {
		return model.Plan{}, errQuoteInsufficient
	}
	// A half-balance probe can fail even when a small payment can afford gas.
	// Probe one atomic unit, then prepare the actual candidate again; never reuse
	// that probe's bytes or assume its estimate applies to the final amount.
	target := big.NewInt(1)
	reserveCap := new(big.Int)
	for i := 0; i < 4; i++ {
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		t.Amount = target.String()
		p, err := a.Prepare(ctx, ac, t)
		if err != nil {
			// Native encoded-value size can increase a Base/TON fee after
			// the small probe. Only an exact locally computed budget quote
			// permits another estimate; RPC/metadata failures never do.
			quotedFee, quotedStorage := "", "0"
			var evmQuote *evm.InsufficientFeeBalanceError
			var tonQuote *ton.InsufficientNativeFeeBalanceError
			switch {
			case ac.Family == "evm" && errors.As(err, &evmQuote) && evmQuote != nil && evmQuote.FeeAsset == t.Asset:
				quotedFee = evmQuote.MaxFee
			case ac.Family == "ton" && t.Asset == ac.Network+":TON" && errors.As(err, &tonQuote) && tonQuote != nil && tonQuote.StorageReserve == "10000000":
				quotedFee, quotedStorage = tonQuote.MaxFee, tonQuote.StorageReserve
			default:
				return model.Plan{}, err
			}
			fee, invalid := amount.Atomic(quotedFee)
			storage, badStorage := amount.Atomic(quotedStorage)
			if invalid != nil || badStorage != nil || fee.Sign() <= 0 {
				return model.Plan{}, err
			}
			fee.Add(fee, storage)
			if fee.Cmp(reserveCap) > 0 {
				reserveCap.Set(fee)
			}
			smaller := new(big.Int).Sub(new(big.Int).Set(balance), reserveCap)
			if smaller.Sign() <= 0 || smaller.Cmp(target) >= 0 {
				return model.Plan{}, err
			}
			target = smaller
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		accountJSON, _ := json.Marshal(ac)
		planAccountJSON, _ := json.Marshal(p.Account)
		if !bytes.Equal(accountJSON, planAccountJSON) || p.Asset != t.Asset || p.Amount != t.Amount || p.Memo != t.Memo || !maximumRecipientEqual(a, ac, t.Recipient, p.Recipient) || !assets[p.FeeAsset] {
			return model.Plan{}, errors.New("maximum plan changed account, asset, recipient, amount, comment or fee domain")
		}
		if err := a.ValidatePlan(p); err != nil {
			return model.Plan{}, err
		}
		fee, err := amount.Atomic(p.MaxFee)
		if err != nil {
			return model.Plan{}, err
		}
		reserve, err := maximumNativeReserve(p)
		if err != nil {
			return model.Plan{}, err
		}
		required := new(big.Int).Add(fee, reserve)
		next := new(big.Int).Set(balance)
		if p.FeeAsset == t.Asset {
			// Keep the largest observed estimate when amount encoding changes produce
			// small fee oscillations. The returned plan still binds its actual estimate.
			if required.Cmp(reserveCap) > 0 {
				reserveCap.Set(required)
			}
			next.Sub(next, reserveCap)
		} else {
			gas, err := maximumBalance(balances, p.FeeAsset)
			if err != nil || gas.Cmp(required) < 0 {
				if err != nil {
					return model.Plan{}, errQuoteBalance
				}
				return model.Plan{}, errQuoteInsufficient
			}
		}
		if next.Sign() <= 0 {
			return model.Plan{}, errQuoteInsufficient
		}
		if next.Cmp(target) == 0 {
			return p, nil
		}
		target = next
	}
	return model.Plan{}, errQuoteUnstable
}

// Keep payload JSON as opaque diagnostic text on the public wire. Re-decoding
// numbers into interface{} or re-marshalling object maps would alter integer
// precision or signed payload/account-parameter field order.
func publicJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return publicJSONValue(raw, 0)
}

func publicJSONValue(raw json.RawMessage, depth int) ([]byte, error) {
	if depth > 64 {
		return nil, errors.New("public JSON nesting exceeds bound")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("invalid public JSON value")
	}
	switch trimmed[0] {
	case '{':
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		var out bytes.Buffer
		out.WriteByte('{')
		first := true
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, errors.New("invalid public JSON object key")
			}
			var item json.RawMessage
			if err := decoder.Decode(&item); err != nil {
				return nil, err
			}
			var rewritten []byte
			if key == "payload" {
				// RawMessage retains order and every literal uint64 digit.
				rewritten, err = json.Marshal(string(item))
			} else if key == "rpc" && len(item) > 0 && item[0] == '"' {
				var endpoint string
				if err = json.Unmarshal(item, &endpoint); err != nil {
					return nil, err
				}
				origin := ""
				if parsed, failure := url.Parse(endpoint); failure == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
					origin = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
				}
				rewritten, err = json.Marshal(origin)
			} else {
				rewritten, err = publicJSONValue(item, depth+1)
			}
			if err != nil {
				return nil, err
			}
			if !first {
				out.WriteByte(',')
			}
			first = false
			encodedKey, _ := json.Marshal(key)
			out.Write(encodedKey)
			out.WriteByte(':')
			out.Write(rewritten)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		out.WriteByte('}')
		return out.Bytes(), nil
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		for index, item := range items {
			rewritten, err := publicJSONValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			items[index] = rewritten
		}
		return json.Marshal(items)
	default:
		return append([]byte(nil), trimmed...), nil
	}
}
