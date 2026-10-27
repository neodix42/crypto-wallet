// Package signer owns all secret state and native authorization. The network
// coordinator never receives an Engine, passwords, entropy or seed buffers.
package signer

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/skip2/go-qrcode"
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/near"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/qr"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

var ErrDenied = errors.New("request denied, cancelled or invalid")

type UI interface {
	Input(string, ...string) ([][]byte, error)
	Confirm(string, string) bool
	File(string, bool) (string, error)
	Receive(string, string, []byte) bool
}
type Engine struct {
	Environment            string
	Files                  *vault.Files
	UI                     UI
	Adapters               []model.Adapter
	data                   *vault.Data
	password               []byte
	parameters             vault.Parameters
	persistFailed          bool
	initializing           bool
	backupInProgress       bool
	exchangeAuthorizations map[string]string
}
type Snapshot struct {
	Locked            bool                `json:"locked"`
	WalletExists      bool                `json:"walletExists"`
	VaultState        string              `json:"vaultState"`
	Environment       string              `json:"environment"`
	RecoveryConfirmed bool                `json:"recoveryConfirmed"`
	Accounts          []model.Account     `json:"accounts"`
	Operations        []journal.Operation `json:"operations"`
}

func New(f *vault.Files, ui UI, adapters []model.Adapter) *Engine {
	return &Engine{Files: f, UI: ui, Adapters: adapters, parameters: vault.Default}
}
func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic("OS random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func (e *Engine) adapter(network string) (model.Adapter, error) {
	for _, a := range e.Adapters {
		for _, n := range a.Networks() {
			if n.ID == network {
				return a, nil
			}
		}
	}
	return nil, ErrDenied
}

// walletPresence never opens ciphertext. A surviving previous generation is
// recovery data, not an empty destination for a different wallet root.
func (e *Engine) walletPresence() (bool, string) {
	exists, err := e.Files.Exists("vault.lwv")
	if err != nil {
		return true, "unavailable"
	}
	if exists {
		return true, "present"
	}
	previous, err := e.Files.Exists("vault.previous.lwv")
	if err != nil || previous {
		return true, "unavailable"
	}
	return false, "missing"
}

func (e *Engine) Snapshot() Snapshot {
	exists, state := e.walletPresence()
	if !exists && e.data != nil {
		// Storage uncertainty cannot authorize creating a replacement wallet.
		exists, state = true, "unavailable"
	}
	s := Snapshot{Locked: e.data == nil, WalletExists: exists, VaultState: state, Environment: e.Environment}
	if e.data == nil {
		return s
	}
	s.Environment, s.RecoveryConfirmed, s.Accounts = e.data.Environment, e.data.RecoveryConfirmed, e.data.Accounts
	for _, op := range e.data.Operations {
		op.Signed = nil
		s.Operations = append(s.Operations, op)
	}
	return s
}
func (e *Engine) Lock() {
	e.exchangeAuthorizations = nil
	if e.data != nil {
		e.data.Destroy()
		e.data = nil
	}
	clear(e.password)
	e.password = nil
}
func wipeInputs(in [][]byte) {
	for _, p := range in {
		clear(p)
	}
}
func (e *Engine) persist() (result error) {
	defer func() {
		if result != nil {
			e.persistFailed = true
			e.Lock()
		}
	}()
	if e.data == nil || e.persistFailed {
		return ErrDenied
	}
	e.data.Generation++
	plain, err := e.data.Encode()
	if err != nil {
		return err
	}
	defer clear(plain)
	encrypted, err := vault.Seal(plain, e.password, e.parameters)
	if err != nil {
		return err
	}
	opened, err := vault.Open(encrypted, e.password)
	if err != nil {
		return err
	}
	defer clear(opened)
	if !bytes.Equal(plain, opened) {
		return ErrDenied
	}
	if e.initializing {
		// Native file/password input and KDF work can take time. Recheck at
		// commit and use the kernel's no-replace rename even after that check.
		// Initialization must never consume either existing generation.
		if exists, state := e.walletPresence(); exists || state != "missing" {
			return ErrDenied
		}
		if err = e.Files.Write("vault.lwv", encrypted, false); err != nil {
			return err
		}
		if previous, err := e.Files.Exists("vault.previous.lwv"); err != nil || previous {
			// An uncoordinated filesystem writer appeared during commit. Keep
			// both files intact and stop this session rather than replacing it.
			return ErrDenied
		}
		return nil
	}
	if old, err := e.Files.Read("vault.lwv"); err == nil {
		if err = e.Files.Write("vault.previous.lwv", old, true); err != nil {
			e.persistFailed = true
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		e.persistFailed = true
		return err
	}
	if err = e.Files.Write("vault.lwv", encrypted, true); err != nil {
		e.persistFailed = true
		return err
	}
	return nil
}
func (e *Engine) validateAccounts(d *vault.Data) error {
	seed, err := derivation.Seed(d.Entropy, d.Passphrase)
	if err != nil {
		return err
	}
	defer clear(seed)
	for _, a := range d.Accounts {
		adapter, err := e.adapter(a.Network)
		if err != nil {
			return err
		}
		expected, err := adapter.Derive(seed, a.Index, a.Network)
		if err != nil {
			return err
		}
		if expected.ID != a.ID || expected.Profile != a.Profile || (a.Family != "decred" && expected.PublicKey != a.PublicKey) || expected.Family != a.Family {
			return ErrDenied
		}
		if a.Family == "near" {
			if near.ValidateRecovery(expected, a) != nil {
				return ErrDenied
			}
		}
		if a.Family == "aptos" {
			if aptos.ValidateRecovery(expected, a) != nil {
				return ErrDenied
			}
		}
		if a.Family == "decred" {
			if decred.ValidateRecovery(expected, a) != nil {
				return ErrDenied
			}
		}
		if a.Family != "near" && a.Family != "aptos" && a.Family != "decred" && expected.Address != a.Address {
			return ErrDenied
		}
		if adapter.ValidateAddress(a.Network, a.Address) != nil {
			return ErrDenied
		}
	}
	return nil
}
func (e *Engine) Create(environment string, words int, importing bool) error {
	if e.data != nil || (environment != "testnet" && environment != "mainnet") || (e.Environment != "" && e.Environment != environment) {
		return ErrDenied
	}
	if exists, state := e.walletPresence(); exists || state != "missing" {
		return ErrDenied
	}
	labels := []string{"New storage password (12–4096 UTF-8 bytes)", "Repeat storage password"}
	if importing {
		labels = append(labels, "Known English BIP39 phrase (masked, input only)", "Optional BIP39 passphrase (distinct from storage password)")
	}
	in, err := e.UI.Input("Create local wallet — set app password", labels...)
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != len(labels) {
		return ErrDenied
	}
	if !vault.PasswordValid(in[0]) {
		return ErrPasswordPolicy
	}
	if !hmac.Equal(in[0], in[1]) {
		return ErrPasswordMismatch
	}
	var entropy, pass []byte
	if importing {
		entropy, err = derivation.Import(in[2])
		if err != nil {
			return ErrInvalidPhrase
		}
		pass = append([]byte(nil), in[3]...)
	} else {
		entropy, err = derivation.Generate(words)
	}
	if err != nil {
		return ErrDenied
	}
	d := &vault.Data{Version: 1, WalletID: ID(), Generation: 1, Scheme: "bip39-english-v1", Entropy: entropy, Passphrase: pass, Environment: environment, Birthday: time.Now().Unix(), Endpoints: map[string]vault.Endpoint{}}
	e.initializing = true
	e.data = d
	e.password = append([]byte(nil), in[0]...)
	if err = e.AddAccount(0); err != nil {
		e.initializing = false
		e.Lock()
		return err
	}
	// The encrypted root must survive cancellation or process failure while the
	// native phrase window is open. Subsequent writes update this same root.
	e.initializing = false
	if importing {
		if !e.UI.Confirm("Imported account recovery limits", "Keep your recovery phrase and any BIP39 passphrase offline. The app password encrypts this installation and is not part of key derivation. Check every public address. A mistyped BIP39 passphrase produces a different valid wallet. Phrase import does not discover unknown NEAR names, rotated Aptos accounts, historical TON contracts or DCR address gaps. Offline or empty results never establish zero funds.") {
			return nil
		}
		if e.data != d || e.persistFailed || d.RecoveryConfirmed {
			return ErrDenied
		}
		d.RecoveryConfirmed = true
		return e.persist()
	}
	return e.recordRecovery()
}

// ConfirmRecovery resumes interrupted setup only. An acknowledged wallet uses
// the independently authenticated BackupPhrase flow for subsequent recording.
// None of the phrase contents enter ordinary public IPC.
func (e *Engine) ConfirmRecovery() error {
	if e.data == nil || e.persistFailed || e.data.RecoveryConfirmed {
		return ErrDenied
	}
	d := e.data
	in, err := e.UI.Input("Complete recovery setup", "App password")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if e.data != d || e.persistFailed || d.RecoveryConfirmed || len(in) != 1 || !hmac.Equal(in[0], e.password) {
		return ErrDenied
	}
	return e.recordRecovery()
}

// BackupPhrase permits a fresh, password-authenticated on-screen recording of
// the existing root. It neither exports a file nor changes recovery authority,
// wallet generation, accounts or journal. Engine operations are serialized by
// the signer; the guard also rejects reentry from a UI callback. A changed or
// locked session invalidates authorization at every prompt boundary.
func (e *Engine) BackupPhrase() error {
	if !e.canAuthorize() || e.backupInProgress {
		return ErrDenied
	}
	ui, ok := e.UI.(interface{ BackupRecovery(string, []byte) bool })
	if !ok {
		return ErrDenied
	}
	e.backupInProgress = true
	defer func() { e.backupInProgress = false }()
	d, generation := e.data, e.data.Generation
	current := func() bool {
		return e.data == d && e.canAuthorize() && d.Generation == generation
	}
	accepted := e.UI.Confirm("Back up recovery phrase", "The recovery phrase controls every wallet derived from this root. Anyone who sees it can take the assets. Continue only in a private place, write every numbered word in order and keep the copy offline. Your app password will be required before the phrase is shown.")
	if !current() {
		return ErrDenied
	}
	if !accepted {
		return nil
	}
	in, err := e.UI.Input("Confirm app password to back up recovery phrase", "App password")
	defer wipeInputs(in)
	if !current() {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if len(in) != 1 || !hmac.Equal(in[0], e.password) {
		return ErrDenied
	}
	// Authentication inputs are no longer needed while the words are displayed.
	wipeInputs(in)
	if len(d.Passphrase) != 0 {
		accepted = e.UI.Confirm("Keep your separate BIP39 passphrase", "This wallet uses an optional BIP39 passphrase. Keep that exact passphrase separately from the recovery words. These words alone cannot restore the same wallets. The app password only encrypts this installation and cannot replace the original BIP39 passphrase.")
		if !current() {
			return ErrDenied
		}
		if !accepted {
			return nil
		}
	}
	phrase, err := derivation.Phrase(d.Entropy)
	if err != nil {
		return ErrDenied
	}
	defer clear(phrase)
	ui.BackupRecovery("Back up recovery phrase — Go signer", phrase)
	if !current() {
		return ErrDenied
	}
	return nil
}

func (e *Engine) recordRecovery() error {
	if e.data == nil || e.persistFailed || e.data.RecoveryConfirmed {
		return ErrDenied
	}
	d := e.data
	ui, ok := e.UI.(interface{ Recovery(string, []byte) bool })
	if !ok {
		return ErrDenied
	}
	if len(d.Passphrase) != 0 && !e.UI.Confirm("Keep your separate BIP39 passphrase", "This wallet uses an optional BIP39 passphrase. You must keep that exact passphrase separately from the recovery words. The app password only encrypts this installation; it cannot replace the BIP39 passphrase. Without both the words and original BIP39 passphrase, recovery produces a different wallet.") {
		return nil
	}
	if e.data != d || e.persistFailed || d.RecoveryConfirmed {
		return ErrDenied
	}
	phrase, err := derivation.Phrase(d.Entropy)
	if err != nil {
		return ErrDenied
	}
	defer clear(phrase)
	if !ui.Recovery("Record recovery phrase — Go signer", phrase) {
		return nil
	}
	if e.data != d || e.persistFailed || d.RecoveryConfirmed {
		return ErrDenied
	}
	d.RecoveryConfirmed = true
	return e.persist()
}
func (e *Engine) Unlock() error {
	if e.data != nil {
		return ErrDenied
	}
	b, err := e.Files.Read("vault.lwv")
	if err != nil {
		return ErrDenied
	}
	in, err := e.UI.Input("Unlock local wallet", "Storage password")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != 1 {
		return ErrDenied
	}
	plain, err := vault.Open(b, in[0])
	if err != nil {
		return vault.ErrUnlock
	}
	defer clear(plain)
	d, err := vault.Decode(plain)
	if err != nil {
		return ErrDenied
	}
	if e.Environment != "" && d.Environment != e.Environment {
		d.Destroy()
		return ErrDenied
	}
	if err = e.validateAccounts(d); err != nil {
		d.Destroy()
		return err
	}
	// Old portable-file verification never substitutes for acknowledging the
	// recovery words. Clear the retired flag without changing the root or paths.
	d.DeprecatedBackupVerified = false
	e.data = d
	e.password = append([]byte(nil), in[0]...)
	journal.Recover(d.Operations)
	if d.Exchange != nil {
		for i := range d.Exchange.Operations {
			if d.Exchange.Operations[i].State == "PREPARED" || d.Exchange.Operations[i].State == "AUTHORIZED" {
				d.Exchange.Operations[i].State = "CANCELLED"
			}
		}
	}
	return e.persist()
}
func (e *Engine) AddAccount(index uint32) error {
	return e.addAccounts(index, index)
}

// DiscoverAccounts reconstructs a deliberately bounded, explicitly chosen range.
// It derives canonical profiles only; associations still require verification.
func (e *Engine) DiscoverAccounts(start, end uint32) error {
	if e.data == nil || start > end || end >= derivation.MaxIndex || end-start >= 20 {
		return ErrDenied
	}
	if !e.UI.Confirm("Recover bounded account index range", fmt.Sprintf("Canonical account indices %d through %d (inclusive). Existing identities are preserved. This derives public accounts locally; balances remain unknown until queried. Record the indices and any noncanonical associations separately. NEAR names, rotated Aptos associations and Decred address gaps need separate discovery.", start, end)) {
		return ErrDenied
	}
	return e.addAccounts(start, end)
}

func (e *Engine) addAccounts(index, end uint32) error {
	if e.data == nil || index >= derivation.MaxIndex {
		return ErrDenied
	}
	seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
	if err != nil {
		return err
	}
	defer clear(seed)
	var additions []model.Account
	for ; index <= end; index++ {
		for _, adapter := range e.Adapters {
			for _, n := range adapter.Networks() {
				if n.Testnet != (e.data.Environment == "testnet") {
					continue
				}
				duplicate := false
				for _, a := range e.data.Accounts {
					if a.Network == n.ID && a.Index == index {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				a, err := adapter.Derive(seed, index, n.ID)
				if err != nil {
					return err
				}
				if a.ID == "" {
					return ErrDenied
				}
				additions = append(additions, a)
			}
		}
	}
	if len(additions) == 0 || len(e.data.Accounts)+len(additions) > 10000 {
		return ErrDenied
	}
	e.data.Accounts = append(e.data.Accounts, additions...)
	return e.persist()
}

// HideAccount retains the recovery descriptor, all reservations and its index.
func (e *Engine) HideAccount(id string, hidden bool) error {
	if e.data == nil {
		return ErrDenied
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == id {
			if e.data.Accounts[i].Hidden == hidden {
				return nil
			}
			e.data.Accounts[i].Hidden = hidden
			return e.persist()
		}
	}
	return ErrDenied
}
func (e *Engine) account(id string) (model.Account, error) {
	if e.data == nil {
		return model.Account{}, ErrDenied
	}
	for _, a := range e.data.Accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return model.Account{}, ErrDenied
}
func (e *Engine) Receive(id string) ([]byte, error) {
	a, err := e.account(id)
	if err != nil {
		return nil, err
	}
	if !e.canAuthorize() {
		return nil, ErrDenied
	}
	if a.Family == "decred" {
		next, err := decred.AdvanceReceive(a)
		if err != nil {
			return nil, err
		}
		for i := range e.data.Accounts {
			if e.data.Accounts[i].ID == id {
				e.data.Accounts[i] = next
			}
		}
		if err = e.persist(); err != nil {
			return nil, err
		}
		a = next
	}
	adapter, err := e.adapter(a.Network)
	if err != nil || adapter.ValidateAddress(a.Network, a.Address) != nil {
		return nil, ErrDenied
	}
	png, err := qrcode.Encode(a.Address, qrcode.Medium, 512)
	if err != nil {
		return nil, ErrDenied
	}
	guidance := ""
	switch a.Family {
	case "hypercore":
		guidance = "\nHyperCore spot is separate from HyperEVM. Activation is NOT established by this address. Initial fresh-account activation is not implemented; check the documented recipient prerequisites before any deposit."
	case "ton":
		guidance = "\nTON BIP39 Wallet V5R1, workchain0, network-specific wallet ID. This is a non-bounceable funding address suitable for an uninitialized wallet."
	case "decred":
		guidance = "\nReceive index was durably advanced before this display. Phrase recovery may require a wider bounded address rescan and an earlier wallet birthday."
	}
	title := "Verify receive address — Go signer"
	summary := fmt.Sprintf("Network: %s\nAccount: %s\nAddress: %s\nProfile: %s\nChoose this exact network at the sender. A plain QR does not encode the network.\nActivation/account prerequisites must be checked before funding.", a.Network, a.ID, a.Address, a.Profile) + guidance
	var accepted bool
	if ui, ok := e.UI.(interface {
		ReceiveAddress(string, string, string, []byte) bool
	}); ok {
		accepted = ui.ReceiveAddress(title, summary, a.Address, png)
	} else {
		accepted = e.UI.Receive(title, summary, png)
	}
	if !accepted {
		return nil, ErrDenied
	}
	return png, nil
}
func (e *Engine) ChangePassword() error {
	if e.data == nil {
		return ErrDenied
	}
	in, err := e.UI.Input("Change app storage password", "Current password", "New password", "Repeat new password")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != 3 || !hmac.Equal(in[0], e.password) {
		return ErrDenied
	}
	if !vault.PasswordValid(in[1]) {
		return ErrPasswordPolicy
	}
	if !hmac.Equal(in[1], in[2]) {
		return ErrPasswordMismatch
	}
	old := e.password
	e.password = append([]byte(nil), in[1]...)
	if err = e.persist(); err != nil {
		clear(e.password)
		e.password = old
		e.Lock()
		return err
	}
	clear(old)
	return e.persist()
}
func (e *Engine) Prepare(p model.Plan) error {
	encoded, err := json.Marshal(p)
	if err != nil {
		return ErrDenied
	}
	if strictjson.Decode(encoded, &p) != nil {
		return ErrDenied
	}
	if e.data == nil || e.persistFailed || p.ID == "" || p.Expires <= time.Now().Unix() || p.Expires > time.Now().Add(30*time.Minute).Unix() {
		return ErrDenied
	}
	a, err := e.account(p.Account.ID)
	if err != nil {
		return err
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(p.Account)
	if !bytes.Equal(x, y) || journal.Conflicts(e.data.Operations, p) {
		return ErrDenied
	}
	if !e.canAuthorize() {
		return ErrDenied
	}
	adapter, err := e.adapter(a.Network)
	if err != nil || adapter.ValidatePlan(p) != nil {
		return ErrDenied
	}
	for _, o := range e.data.Operations {
		if o.Plan.ID == p.ID {
			return ErrDenied
		}
	}
	e.data.Operations = append(e.data.Operations, journal.Operation{Plan: p, Digest: journal.Digest(p), State: "PREPARED"})
	return e.persist()
}

// canAuthorize is checked at every authorization boundary. Metadata and storage
// password changes preserve acknowledgement because they do not change the root.
func (e *Engine) canAuthorize() bool {
	return e.data != nil && !e.persistFailed && e.data.RecoveryConfirmed
}

func (e *Engine) Sign(id string) (model.Signed, error) {
	if !e.canAuthorize() {
		return model.Signed{}, ErrDenied
	}
	for i := range e.data.Operations {
		o := &e.data.Operations[i]
		if o.Plan.ID != id {
			continue
		}
		if o.State != "PREPARED" || journal.Digest(o.Plan) != o.Digest || o.Plan.Expires <= time.Now().Unix() {
			return model.Signed{}, ErrDenied
		}
		if err := o.Transition("AWAITING_CONFIRMATION"); err != nil {
			return model.Signed{}, err
		}
		if err := e.persist(); err != nil {
			return model.Signed{}, err
		}
		p := o.Plan
		feeLabel := "Maximum network fee"
		if p.Account.Family == "ton" {
			feeLabel = "Estimated fee reserve (TON protocol has no signed fee cap)"
		}
		if p.Account.Family == "evm" && (strings.HasPrefix(p.Account.Network, "base-") || strings.HasPrefix(p.Account.Network, "horizen-")) {
			feeLabel = "Signed execution cap + estimated rollup reserve (rollup charges are protocol-assessed)"
		}
		if p.Account.Family == "near" {
			feeLabel = "Estimated fee reserve (NEAR protocol has no signed fee cap)"
		}
		summary := fmt.Sprintf("Network: %s\nSource: %s\nDestination: %s\nAsset identity: %s\nAtomic amount: %s\n%s: %s %s\nNonce/sequence: %s\nExpiry (Unix): %d\nMemo: %q\nExact chain effects:\n%s", p.Account.Network, p.Account.Address, p.Recipient, p.Asset, p.Amount, feeLabel, p.MaxFee, p.FeeAsset, p.Nonce, p.Expires, p.Memo, nativeEffects(p))
		if !e.UI.Confirm("Authorize exact transfer — Go signer", summary) {
			o.State = "CANCELLED"
			return model.Signed{}, e.cancelError()
		}
		if !e.canAuthorize() {
			return model.Signed{}, e.cancelOperation(o)
		}
		in, err := e.UI.Input("Reauthenticate this transfer", "Storage password")
		if err != nil {
			return model.Signed{}, e.cancelOperation(o)
		}
		defer wipeInputs(in)
		if len(in) != 1 || !hmac.Equal(in[0], e.password) || p.Expires <= time.Now().Unix() || !e.canAuthorize() {
			return model.Signed{}, e.cancelOperation(o)
		}
		adapter, err := e.adapter(p.Account.Network)
		if err != nil || adapter.ValidatePlan(p) != nil {
			return model.Signed{}, e.cancelOperation(o)
		}
		seed, err := derivation.Seed(e.data.Entropy, e.data.Passphrase)
		if err != nil {
			return model.Signed{}, err
		}
		defer clear(seed)
		signed, err := adapter.Sign(seed, p)
		if err != nil {
			return model.Signed{}, e.cancelOperation(o)
		}
		if signed.Identity == "" || len(signed.Bytes) == 0 {
			return model.Signed{}, e.cancelOperation(o)
		}
		o.Signed = &signed
		o.State = "SIGNED_NOT_SUBMITTED"
		if p.Account.Family == "decred" {
			for j := range e.data.Accounts {
				if e.data.Accounts[j].ID == p.Account.ID {
					next, err := decred.AdvanceChange(e.data.Accounts[j])
					if err != nil {
						clear(signed.Bytes)
						e.Lock()
						return model.Signed{}, ErrDenied
					}
					e.data.Accounts[j] = next
				}
			}
		}
		if err = e.persist(); err != nil {
			return model.Signed{}, err
		}
		return signed, nil
	}
	return model.Signed{}, ErrDenied
}

// CancelPrepared releases only an unsigned plan which has not entered native
// authorization. Signed, ambiguous and in-flight records retain reservations.
func (e *Engine) CancelPrepared(id string) error {
	if e.data == nil || e.persistFailed {
		return ErrDenied
	}
	for i := range e.data.Operations {
		o := &e.data.Operations[i]
		if o.Plan.ID == id {
			if o.State != "PREPARED" || o.Signed != nil {
				return ErrDenied
			}
			if err := o.Transition("CANCELLED"); err != nil {
				return err
			}
			return e.persist()
		}
	}
	return ErrDenied
}
func (e *Engine) cancelError() error {
	if err := e.persist(); err != nil {
		return err
	}
	return ErrDenied
}
func (e *Engine) cancelOperation(o *journal.Operation) error {
	o.State = "CANCELLED"
	return e.cancelError()
}
func (e *Engine) Update(id, state string, r *model.Receipt) error {
	if e.data == nil || e.persistFailed {
		return ErrDenied
	}
	for i := range e.data.Operations {
		o := e.data.Operations[i]
		if o.Plan.ID != id {
			continue
		}
		if r != nil {
			if o.Signed == nil || r.Identity != o.Signed.Identity || r.State != state {
				return ErrDenied
			}
		} else if state != "SUBMITTING" {
			return ErrDenied
		}
		if o.State != state {
			if err := o.Transition(state); err != nil {
				return err
			}
		}
		if r != nil {
			copyReceipt := *r
			o.Receipt = &copyReceipt
		}
		e.data.Operations[i] = o
		return e.persist()
	}
	return ErrDenied
}

func (e *Engine) Pending() []journal.Operation {
	if e.data == nil {
		return nil
	}
	return e.data.Operations
}

// DecodeBody centralizes rejection of secret-bearing/unknown public API fields.
func DecodeBody(b []byte, out any) error { return strictjson.Decode(b, out) }

func (e *Engine) CopyReceive(id string) error {
	if _, err := e.Receive(id); err != nil {
		return err
	}
	a, err := e.account(id)
	if err != nil {
		return err
	}
	u, ok := e.UI.(interface{ CopyPublic(string) })
	if !ok {
		return ErrDenied
	}
	u.CopyPublic(a.Address)
	return nil
}
func (e *Engine) SaveQR(id string) error {
	png, err := e.Receive(id)
	if err != nil {
		return err
	}
	path, err := e.UI.File("Save verified public QR image (new file)", true)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrDenied
	}
	defer f.Close()
	if n, err := f.Write(png); err != nil || n != len(png) {
		return ErrDenied
	}
	return f.Sync()
}
func (e *Engine) ScanQR() (string, error) {
	if e.data == nil {
		return "", ErrDenied
	}
	path, err := e.UI.File("Decode local recipient QR — this never sends", false)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrDenied
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() > 5<<20 || !st.Mode().IsRegular() {
		return "", ErrDenied
	}
	b := make([]byte, st.Size())
	if _, err = io.ReadFull(f, b); err != nil {
		return "", ErrDenied
	}
	return qr.Decode(b)
}

// Settings is available only on inherited core/signer IPC, never Electron IPC.
func (e *Engine) Settings() map[string]vault.Endpoint {
	if e.data == nil {
		return nil
	}
	return e.data.Endpoints
}
func (e *Engine) Configure(network string) error {
	if e.data == nil {
		return ErrDenied
	}
	adapter, err := e.adapter(network)
	if err != nil {
		return err
	}
	allowed := false
	for _, n := range adapter.Networks() {
		if n.ID == network && n.Testnet == (e.data.Environment == "testnet") {
			allowed = true
		}
	}
	if !allowed {
		return ErrDenied
	}
	in, err := e.UI.Input("Configure trusted RPC for "+network, "TLS URL (HTTP allowed only for loopback node)", "Optional authenticated node username", "Optional authenticated node password", "Optional API key")
	if err != nil {
		return ErrDenied
	}
	defer wipeInputs(in)
	if len(in) != 4 || chainutil.Endpoint(string(in[0])) != nil {
		return ErrDenied
	}
	// Validate the authenticated-node shape before committing encrypted settings.
	// Decred deliberately has no unauthenticated public default.
	if _, ok := adapter.(*decred.Adapter); ok {
		if err := decred.New().Configure(network, decred.Endpoint{URL: string(in[0]), User: string(in[1]), Password: string(in[2])}); err != nil {
			return ErrDenied
		}
	}
	if !e.UI.Confirm("Approve RPC privacy and trust", "Network: "+network+"\nEndpoint: "+string(in[0])+"\nThe provider learns queried public addresses and may lie or censor. Network identity is checked before account queries; it does not prove consensus.") {
		return ErrDenied
	}
	if e.data.Endpoints == nil {
		e.data.Endpoints = map[string]vault.Endpoint{}
	}
	e.data.Endpoints[network] = vault.Endpoint{URL: string(in[0]), User: string(in[1]), Password: string(in[2]), APIKey: string(in[3])}
	return e.persist()
}
func (e *Engine) Associate(updated model.Account) error {
	old, err := e.account(updated.ID)
	if err != nil {
		return err
	}
	if updated.Profile != old.Profile || updated.Index != old.Index || updated.Network != old.Network || updated.PublicKey != old.PublicKey || (old.Family != "near" && old.Family != "aptos") {
		return ErrDenied
	}
	if old.Family == "near" && near.ValidateRecovery(old, updated) != nil {
		return ErrDenied
	}
	if old.Family == "aptos" && aptos.ValidateRecovery(old, updated) != nil {
		return ErrDenied
	}
	adapter, err := e.adapter(old.Network)
	if err != nil || adapter.ValidateAddress(updated.Network, updated.Address) != nil {
		return ErrDenied
	}
	if !e.UI.Confirm("Associate verified public account", "Network: "+old.Network+"\nDerived public key: "+old.PublicKey+"\nVerified account: "+updated.Address+"\nThe Go network core has checked current signing authority.") {
		return ErrDenied
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == old.ID {
			e.data.Accounts[i] = updated
		}
	}
	return e.persist()
}

func nativeEffects(p model.Plan) string {
	if p.Account.Family != "decred" {
		return string(p.Payload)
	}
	var x decred.Payload
	if strictjson.Decode(p.Payload, &x) != nil {
		return "Invalid effects — approval must be rejected"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Output recipient %s: %s DCR atoms\nChange output %s: %d atoms (owned change index %d)\nFee: %d atoms; rate %d atoms/kB; source %s\nTransaction expires at height %d\n", p.Recipient, p.Amount, x.ChangeAddress, x.Change, x.ChangeIndex, x.Fee, x.FeeRate, x.FeeSource, x.Expiry)
	if x.Expiry != 0 {
		maturity := chaincfg.MainNetParams().CoinbaseMaturity
		if p.Account.Network == "decred-testnet3" {
			maturity = chaincfg.TestNet3Params().CoinbaseMaturity
		}
		if p.Account.Network == "decred-simnet" {
			maturity = chaincfg.SimNetParams().CoinbaseMaturity
		}
		fmt.Fprintf(&b, "Spendability delay: this transaction uses a wire expiry. Its recipient and change outputs require %d confirmations before spending under this network's consensus rules.\n", maturity)
	}
	for _, in := range x.Inputs {
		fmt.Fprintf(&b, "Input %s:%d tree %d; %d atoms; owned branch/index %d/%d; height %d\n", in.TxID, in.Vout, in.Tree, in.Value, in.Branch, in.Index, in.Height)
	}
	return b.String()
}

func (e *Engine) Discovery(id string, birthday, gap, limit uint32, blockBudget ...uint32) error {
	a, err := e.account(id)
	if err != nil || a.Family != "decred" {
		return ErrDenied
	}
	updated, err := decred.SetDiscovery(a, birthday, gap, limit)
	if err != nil {
		return ErrDenied
	}
	if len(blockBudget) > 1 {
		return ErrDenied
	}
	budget := uint32(0)
	if len(blockBudget) == 1 {
		budget = blockBudget[0]
	}
	updated, err = decred.SetDiscoveryBudget(updated, budget)
	if err != nil {
		return ErrDenied
	}
	if !e.UI.Confirm("Change bounded Decred discovery", fmt.Sprintf("Network: %s\nStart height: %d\nAddress gap: %d\nAddress limit per branch: %d\nBlock budget: %d (0 uses default 10000)\nPhrase recovery may need earlier heights and wider address ranges. An incomplete scan is never a zero balance.", a.Network, birthday, gap, limit, budget)) {
		return ErrDenied
	}
	for i := range e.data.Accounts {
		if e.data.Accounts[i].ID == id {
			e.data.Accounts[i] = updated
		}
	}
	return e.persist()
}
