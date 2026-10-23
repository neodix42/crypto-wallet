package main

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
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/supervisor"
	"localwallet/internal/vault"
)

type snapshot struct {
	Locked         bool                `json:"locked"`
	Environment    string              `json:"environment"`
	BackupVerified bool                `json:"backupVerified"`
	Accounts       []model.Account     `json:"accounts"`
	Operations     []journal.Operation `json:"operations"`
}
type core struct {
	child          *supervisor.Child
	adapters       []model.Adapter
	mu             sync.Mutex
	last           time.Time
	epoch          uint64
	active         bool
	promptPending  bool
	requestID      string
	requestSession string
	requestEpoch   uint64
	publicChannel  *ipc.Channel
	sessionCtx     context.Context
	sessionCancel  context.CancelFunc
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
	case "create", "import", "unlock", "backup", "restore", "password", "receive", "copyReceive", "saveQR", "scanQR", "configure", "discovery", "discoverAccounts", "associate", "sign":
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
	return model.Account{}, errors.New("unknown account")
}
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return
	}
	environment := flag.String("environment", "testnet", "testnet or explicit mainnet profile")
	flag.Parse()
	if *environment != "testnet" && *environment != "mainnet" {
		os.Exit(2)
	}
	exe, e := os.Executable()
	if e != nil {
		os.Exit(3)
	}
	home, e := os.UserConfigDir()
	if e != nil {
		os.Exit(3)
	}
	c := &core{child: &supervisor.Child{Path: filepath.Join(filepath.Dir(exe), "wallet-signer"), Data: filepath.Join(home, "localwallet", *environment), Environment: *environment}, adapters: chains.All(), last: time.Now()}
	defer c.lock()
	done := make(chan struct{})
	defer close(done)
	ch := ipc.Channel{R: os.Stdin, W: os.Stdout}
	c.publicChannel = &ch
	c.child.OnActivity = c.nativeActivity
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
				r.Error = "REQUEST_FAILED_STATE_UNKNOWN"
			}
			_ = ch.Write(r)
		}
	}()
	for {
		m, e := ch.Read()
		if e != nil || ch.Accept(m) != nil {
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
			select {
			case work <- queuedRequest{m, epoch}:
			default:
				_ = ch.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: []byte(`null`), Error: "BUSY"})
			}
		}
	}

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
	switch m.Method {
	case "networks", "accounts", "status", "history":
		if strictjson.Decode(m.Body, &struct{}{}) != nil {
			return nil, errors.New("invalid fields")
		}
	}
	switch m.Method {
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
		return c.current()
	case "status":
		s, err := c.current()
		return struct {
			Version string `json:"version"`
			snapshot
		}{buildinfo.Version, s}, err
	case "create", "import", "unlock", "backup", "restore", "password", "addAccount", "hideAccount", "discoverAccounts", "cancelPrepared", "receive", "copyReceive", "saveQR", "scanQR", "configure", "discovery":
		var out json.RawMessage
		if e := c.childCallAt(epoch, m.Method, json.RawMessage(m.Body), &out); e != nil {
			return nil, e
		}
		if m.Method == "unlock" || m.Method == "restore" || m.Method == "configure" {
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
		return out, e
	case "chainHistory":
		var b struct {
			AccountID string `json:"accountId"`
		}
		if e := strictjson.Decode(m.Body, &b); e != nil {
			return nil, e
		}
		ac, e := c.account(b.AccountID)
		if e != nil {
			return nil, e
		}
		a, e := c.adapter(ac.Network)
		if e != nil {
			return nil, e
		}
		ctx, cancel := c.networkContextAt(2*time.Minute, epoch)
		defer cancel()
		if h, ok := a.(interface {
			History(context.Context, model.Account) ([]model.Receipt, error)
			HistoryScope() string
		}); ok {
			entries, e := h.History(ctx, ac)
			return struct {
				Entries []model.Receipt `json:"entries"`
				Scope   string          `json:"scope"`
			}{entries, h.HistoryScope()}, e
		}
		if d, ok := a.(*decred.Adapter); ok {
			r, e := d.SyncWithProgress(ctx, ac, nil)
			return struct {
				Report decred.SyncReport `json:"report"`
				Scope  string            `json:"scope"`
			}{r, "Authenticated dcrd bounded scan; see scanned range/address limit. Older blocks and addresses beyond configured limits remain unresolved."}, e
		}
		return struct {
			Entries []model.Receipt `json:"entries"`
			Scope   string          `json:"scope"`
		}{nil, "Network incoming history is not implemented for this family; local outgoing journal only. An empty journal is not proof of no funds."}, nil
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
		s, e := c.current()
		return s.Operations, e
	case "prepare", "max":
		var t model.Transfer
		if e := strictjson.Decode(m.Body, &t); e != nil {
			return nil, e
		}
		a, e := c.account(t.AccountID)
		if e != nil {
			return nil, e
		}
		adapter, e := c.adapter(a.Network)
		if e != nil {
			return nil, e
		}
		found := false
		for _, n := range adapter.Networks() {
			if n.ID != a.Network {
				continue
			}
			for _, asset := range n.Assets {
				if asset.ID == t.Asset {
					found = true
					if m.Method != "max" {
						t.Amount, e = amount.Parse(t.Amount, asset.Decimals)
					}
				}
			}
		}
		if !found || e != nil {
			return nil, errors.New("invalid asset or precision")
		}
		ctx, cancel := c.networkContextAt(45*time.Second, epoch)
		defer cancel()
		var p model.Plan
		if m.Method == "max" {
			p, e = maxPlan(ctx, adapter, a, t)
		} else {
			p, e = adapter.Prepare(ctx, a, t)
		}
		if e != nil {
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
	var ops []journal.Operation
	if e := c.childCallAt(epoch, "pending", struct{}{}, &ops); e != nil {
		return nil, e
	}
	var op *journal.Operation
	for i := range ops {
		if ops[i].Plan.ID == id {
			op = &ops[i]
			break
		}
	}
	if op == nil {
		return nil, errors.New("unknown payment")
	}
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
		case *evm.Adapter:
			n, e := evm.NewWithEndpoints(evms)
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

func maxPlan(ctx context.Context, a model.Adapter, ac model.Account, t model.Transfer) (model.Plan, error) {
	balances, err := a.Balance(ctx, ac)
	if err != nil {
		return model.Plan{}, err
	}
	var balance *big.Int
	for _, b := range balances {
		if b.Asset == t.Asset {
			state := strings.ToUpper(b.State)
			if strings.Contains(state, "UNKNOWN") || strings.Contains(state, "ERROR") || strings.Contains(state, "SYNCHRONIZING") {
				return model.Plan{}, errors.New("spendable state unknown")
			}
			balance, err = amount.Atomic(b.Atomic)
			if err != nil {
				return model.Plan{}, err
			}
		}
	}
	if balance == nil || balance.Sign() == 0 {
		return model.Plan{}, errors.New("balance unknown or unavailable")
	}
	target := new(big.Int).Div(new(big.Int).Set(balance), big.NewInt(2))
	if target.Sign() == 0 {
		target.SetInt64(1)
	}
	for i := 0; i < 4; i++ {
		t.Amount = target.String()
		p, err := a.Prepare(ctx, ac, t)
		if err != nil {
			return model.Plan{}, err
		}
		next := new(big.Int).Set(balance)
		if p.FeeAsset == t.Asset {
			fee, err := amount.Atomic(p.MaxFee)
			if err != nil {
				return model.Plan{}, err
			}
			next.Sub(next, fee)
			reserve := "0"
			switch ac.Family {
			case "near":
				var x near.Payload
				if strictjson.Decode(p.Payload, &x) != nil {
					return model.Plan{}, errors.New("invalid NEAR max reserve")
				}
				reserve = x.StorageReserve
			case "ton":
				var x ton.Payload
				if strictjson.Decode(p.Payload, &x) != nil {
					return model.Plan{}, errors.New("invalid TON max reserve")
				}
				reserve = x.StorageReserve
			}
			r, err := amount.Atomic(reserve)
			if err != nil {
				return model.Plan{}, err
			}
			next.Sub(next, r)
		}
		if next.Sign() <= 0 {
			return model.Plan{}, errors.New("insufficient gas reserve")
		}
		if next.Cmp(target) == 0 {
			return p, nil
		}
		target = next
	}
	return model.Plan{}, errors.New("maximum amount could not stabilize; enter an exact amount")
}

// Keep protocol-native uint64s in the internal Go payload. Electron gets text
// for those diagnostic bytes and uses the explicit decimal Plan.Nonce field.
func publicJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var obj any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&obj); err != nil {
		return nil, err
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for key, item := range x {
				if key == "payload" {
					b, _ := json.Marshal(item)
					x[key] = string(b)
				} else if key == "rpc" {
					if s, ok := item.(string); ok {
						if u, e := url.Parse(s); e == nil {
							u.User = nil
							u.RawQuery = ""
							u.Fragment = ""
							x[key] = u.String()
						}
					}
				} else {
					walk(item)
				}
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(obj)
	return json.Marshal(obj)
}
