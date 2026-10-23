// Package aptos implements allowlisted native APT transfers using the pinned
// official Go SDK's Legacy Ed25519 BCS transaction primitives.
package aptos

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha3"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdk "github.com/aptos-labs/aptos-go-sdk"
	"github.com/aptos-labs/aptos-go-sdk/bcs"
	crypto "github.com/aptos-labs/aptos-go-sdk/crypto"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

const Profile = "aptos-bip39-legacy-ed25519-v1"

var errInvalid = errors.New("invalid Aptos native transfer")

type Adapter struct {
	endpoints map[string]string
	client    *chainutil.Client
}

func New() *Adapter {
	return &Adapter{map[string]string{"aptos-mainnet": "https://fullnode.mainnet.aptoslabs.com/v1", "aptos-testnet": "https://fullnode.testnet.aptoslabs.com/v1", "aptos-localnet": "http://127.0.0.1:8080/v1"}, chainutil.NewClient()}
}
func chainID(network string) (uint8, error) {
	switch network {
	case "aptos-mainnet":
		return 1, nil
	case "aptos-testnet":
		return 2, nil
	case "aptos-localnet":
		return 4, nil
	}
	return 0, errInvalid
}
func asset(n string) string { return n + ":native" }
func (a *Adapter) SetEndpoint(network, endpoint string) error {
	if _, e := chainID(network); e != nil {
		return e
	}
	if e := chainutil.Endpoint(endpoint); e != nil {
		return e
	}
	a.endpoints[network] = strings.TrimRight(endpoint, "/")
	return nil
}
func (a *Adapter) Networks() []model.Network {
	out := []model.Network{}
	for _, id := range []string{"aptos-mainnet", "aptos-testnet", "aptos-localnet"} {
		c, _ := chainID(id)
		out = append(out, model.Network{ID: id, Family: "aptos", Name: "Aptos " + strings.TrimPrefix(id, "aptos-"), Testnet: id != "aptos-mainnet", ChainID: strconv.Itoa(int(c)), RPC: a.endpoints[id], Trust: "TLS RPC; provider assertion, not verified consensus", Assets: []model.Asset{{ID: asset(id), Symbol: "APT", Decimals: 8}}})
	}
	return out
}
func address(s string) (sdk.AccountAddress, error) {
	var out sdk.AccountAddress
	if len(s) < 3 || len(s) > 66 || !strings.HasPrefix(s, "0x") {
		return out, errInvalid
	}
	v := s[2:]
	if len(v)%2 == 1 {
		v = "0" + v
	}
	b, e := hex.DecodeString(v)
	if e != nil || len(b) > 32 {
		return out, errInvalid
	}
	copy(out[32-len(b):], b)
	return out, nil
}
func (a *Adapter) ValidateAddress(network, s string) error {
	if _, e := chainID(network); e != nil {
		return e
	}
	_, e := address(s)
	return e
}

type Parameters struct {
	Path       string `json:"path"`
	Scheme     string `json:"scheme"`
	AuthKey    string `json:"authenticationKey"`
	Associated bool   `json:"associated"`
}

func authkey(pub []byte) string {
	b := append(append([]byte{}, pub...), 0)
	hash := sha3.Sum256(b)
	return "0x" + hex.EncodeToString(hash[:])
}
func (a *Adapter) Derive(seed []byte, index uint32, network string) (model.Account, error) {
	if _, e := chainID(network); e != nil {
		return model.Account{}, e
	}
	k, e := derivation.Ed25519(seed, []uint32{44, 637, index, 0, 0})
	if e != nil {
		return model.Account{}, e
	}
	defer clear(k)
	pub := k.Public().(ed25519.PublicKey)
	addr := authkey(pub)
	p, _ := json.Marshal(Parameters{Path: fmt.Sprintf("m/44'/637'/%d'/0'/0'", index), Scheme: "legacy-ed25519", AuthKey: addr})
	return model.Account{ID: fmt.Sprintf("%s:%d", network, index), Family: "aptos", Profile: Profile, Index: index, Network: network, Address: addr, PublicKey: hex.EncodeToString(pub), Parameters: p}, nil
}
func (a *Adapter) account(ac model.Account) error {
	if ac.Family != "aptos" || ac.Profile != Profile || ac.Index >= 1<<31 {
		return errInvalid
	}
	if _, e := chainID(ac.Network); e != nil {
		return e
	}
	addr, e := address(ac.Address)
	if e != nil || ac.Address != addr.StringLong() {
		return errInvalid
	}
	pub, e := hex.DecodeString(ac.PublicKey)
	if e != nil || len(pub) != 32 || hex.EncodeToString(pub) != ac.PublicKey {
		return errInvalid
	}
	var p Parameters
	if e = chainutil.DecodeStrict(ac.Parameters, &p); e != nil {
		return e
	}
	if p.Path != fmt.Sprintf("m/44'/637'/%d'/0'/0'", ac.Index) || p.Scheme != "legacy-ed25519" || p.AuthKey != authkey(pub) {
		return errInvalid
	}
	if !p.Associated && ac.Address != p.AuthKey {
		return errInvalid
	}
	return nil
}
func (a *Adapter) request(ctx context.Context, network, method, path string, body any, out any) error {
	if _, e := chainID(network); e != nil {
		return e
	}
	var b []byte
	content := "application/json"
	if raw, ok := body.([]byte); ok {
		b = raw
		content = "application/x.aptos.signed_transaction+bcs"
	} else if body != nil {
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			return errInvalid
		}
	}
	return a.client.Do(ctx, method, a.endpoints[network]+path, content, b, out)
}

type ledger struct {
	ChainID   json.Number `json:"chain_id"`
	Version   string      `json:"ledger_version"`
	Timestamp string      `json:"ledger_timestamp"`
}

func (a *Adapter) identity(ctx context.Context, network string) (ledger, error) {
	var st ledger
	e := a.request(ctx, network, "GET", "", nil, &st)
	if e != nil {
		return st, e
	}
	id, _ := chainID(network)
	n, e := chainutil.Uint(st.ChainID.String(), 8)
	if e != nil || n != uint64(id) {
		return st, errors.New("Aptos wrong chain ID")
	}
	if _, e = chainutil.Uint(st.Version, 64); e != nil {
		return st, e
	}
	ts, e := chainutil.Uint(st.Timestamp, 64)
	if e != nil || ts > math.MaxInt64 {
		return st, errInvalid
	}
	t := time.UnixMicro(int64(ts))
	if time.Since(t) > 2*time.Minute || time.Until(t) > time.Minute {
		return st, errors.New("Aptos stale ledger")
	}
	return st, nil
}

type accountState struct {
	Sequence          string `json:"sequence_number"`
	AuthenticationKey string `json:"authentication_key"`
}

func (a *Adapter) authority(ctx context.Context, ac model.Account, version string) (accountState, error) {
	var st accountState
	if e := a.account(ac); e != nil {
		return st, e
	}
	e := a.request(ctx, ac.Network, "GET", "/accounts/"+ac.Address+"?ledger_version="+version, nil, &st)
	if e != nil {
		return st, e
	}
	pub, _ := hex.DecodeString(ac.PublicKey)
	key, e := address(st.AuthenticationKey)
	if e != nil || key.StringLong() != authkey(pub) {
		return st, errors.New("Aptos key rotated or unsupported authentication scheme")
	}
	if _, e = chainutil.Uint(st.Sequence, 64); e != nil {
		return st, e
	}
	return st, nil
}

// Associate records a verified original address after key rotation. It does not
// derive a different key or change the explicit Legacy Ed25519 scheme.
func (a *Adapter) Associate(ctx context.Context, ac model.Account, original string) (model.Account, error) {
	if e := a.account(ac); e != nil {
		return model.Account{}, e
	}
	addr, e := address(original)
	if e != nil {
		return model.Account{}, e
	}
	var p Parameters
	_ = json.Unmarshal(ac.Parameters, &p)
	p.Associated = addr.StringLong() != p.AuthKey
	ac.Address = addr.StringLong()
	ac.Parameters, _ = json.Marshal(p)
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		return model.Account{}, e
	}
	_, e = a.authority(ctx, ac, l.Version)
	return ac, e
}

// Discover uses the chain's proven-rotation originating-address view. Unproven
// rotations require explicit association; no empty derived account is proof of recovery.
func (a *Adapter) Discover(ctx context.Context, ac model.Account) (model.Account, error) {
	if e := a.account(ac); e != nil {
		return model.Account{}, e
	}
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		return model.Account{}, e
	}
	var p Parameters
	_ = json.Unmarshal(ac.Parameters, &p)
	var r []struct {
		Vec []string `json:"vec"`
	}
	e = a.request(ctx, ac.Network, "POST", "/view?ledger_version="+l.Version, map[string]any{"function": "0x1::account::originating_address", "type_arguments": []string{}, "arguments": []string{p.AuthKey}}, &r)
	if e != nil {
		return model.Account{}, e
	}
	if len(r) != 1 || len(r[0].Vec) > 1 {
		return model.Account{}, errInvalid
	}
	if len(r[0].Vec) == 1 {
		return a.Associate(ctx, ac, r[0].Vec[0])
	}
	return ac, nil
}
func (a *Adapter) balanceAt(ctx context.Context, ac model.Account, version string) (string, error) {
	var b []string
	e := a.request(ctx, ac.Network, "POST", "/view?ledger_version="+version, map[string]any{"function": "0x1::coin::balance", "type_arguments": []string{"0x1::aptos_coin::AptosCoin"}, "arguments": []string{ac.Address}}, &b)
	if e != nil {
		return "", e
	}
	if len(b) != 1 {
		return "", errInvalid
	}
	if _, e = chainutil.Uint(b[0], 64); e != nil {
		return "", e
	}
	return b[0], nil
}
func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		return nil, e
	}
	if _, e = a.authority(ctx, ac, l.Version); e != nil {
		return nil, e
	}
	b, e := a.balanceAt(ctx, ac, l.Version)
	if e != nil {
		return nil, e
	}
	return []model.Balance{{Asset: asset(ac.Network), Atomic: b, State: "COMMITTED", At: l.Version}}, nil
}

type Payload struct {
	Version  int    `json:"version"`
	Raw      string `json:"rawTransactionBCS"`
	GasUnits string `json:"maxGasUnits"`
	GasPrice string `json:"gasUnitPrice"`
	Ledger   string `json:"ledgerVersion"`
	Prepared int64  `json:"prepared"`
}

func raw(p model.Plan, d Payload) (*sdk.RawTransaction, error) {
	from, e := address(p.Account.Address)
	if e != nil {
		return nil, e
	}
	to, e := address(p.Recipient)
	if e != nil {
		return nil, e
	}
	amount, e := chainutil.Uint(p.Amount, 64)
	if e != nil || amount == 0 {
		return nil, errInvalid
	}
	nonce, e := chainutil.Uint(p.Nonce, 64)
	if e != nil {
		return nil, e
	}
	gas, e := chainutil.Uint(d.GasUnits, 64)
	if e != nil || gas < 1 || gas > 2_000_000 {
		return nil, errInvalid
	}
	price, e := chainutil.Uint(d.GasPrice, 64)
	if e != nil || price < 1 || price > 1_000_000 {
		return nil, errInvalid
	}
	payload, e := sdk.CoinTransferPayload(nil, to, amount)
	if e != nil {
		return nil, errInvalid
	}
	id, e := chainID(p.Account.Network)
	if e != nil {
		return nil, e
	}
	return &sdk.RawTransaction{Sender: from, SequenceNumber: nonce, Payload: sdk.TransactionPayload{Payload: payload}, MaxGasAmount: gas, GasUnitPrice: price, ExpirationTimestampSeconds: uint64(p.Expires), ChainId: id}, nil
}
func signed(raw *sdk.RawTransaction, pub, sig []byte) (*sdk.SignedTransaction, error) {
	pk := &crypto.Ed25519PublicKey{}
	if e := pk.FromBytes(pub); e != nil {
		return nil, errInvalid
	}
	signature := &crypto.Ed25519Signature{}
	if e := signature.FromBytes(sig); e != nil {
		return nil, errInvalid
	}
	return raw.SignedTransactionWithAuthenticator(&crypto.AccountAuthenticator{Variant: crypto.AccountAuthenticatorEd25519, Auth: &crypto.Ed25519Authenticator{PubKey: pk, Sig: signature}})
}

type execution struct {
	Type     string `json:"type"`
	Hash     string `json:"hash"`
	Success  *bool  `json:"success"`
	GasUsed  string `json:"gas_used"`
	Version  string `json:"version"`
	Sender   string `json:"sender"`
	Sequence string `json:"sequence_number"`
	VM       string `json:"vm_status"`
}

func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	var z model.Plan
	if t.AccountID != ac.ID || t.Asset != asset(ac.Network) || t.Memo != "" {
		return z, errInvalid
	}
	if e := a.account(ac); e != nil {
		return z, e
	}
	recipient, e := address(t.Recipient)
	if e != nil {
		return z, e
	}
	if n, e := chainutil.Uint(t.Amount, 64); e != nil || n == 0 {
		return z, errInvalid
	}
	l, e := a.identity(ctx, ac.Network)
	if e != nil {
		return z, e
	}
	st, e := a.authority(ctx, ac, l.Version)
	if e != nil {
		return z, e
	}
	nonce, _ := chainutil.Uint(st.Sequence, 64)
	if nonce == math.MaxUint64 {
		return z, errInvalid
	}
	balance, e := a.balanceAt(ctx, ac, l.Version)
	if e != nil {
		return z, e
	}
	var estimate struct {
		Price json.Number `json:"gas_estimate"`
	}
	if e = a.request(ctx, ac.Network, "GET", "/estimate_gas_price", nil, &estimate); e != nil {
		return z, e
	}
	price, e := chainutil.Uint(estimate.Price.String(), 64)
	if e != nil || price == 0 || price > 1_000_000 {
		return z, errInvalid
	}
	now := time.Now().Unix()
	d := Payload{Version: 1, GasUnits: "100000", GasPrice: strconv.FormatUint(price, 10), Ledger: l.Version, Prepared: now}
	z = model.Plan{Account: ac, Asset: t.Asset, Recipient: recipient.StringLong(), Amount: t.Amount, FeeAsset: t.Asset, Nonce: st.Sequence, Expires: now + 120}
	r, e := raw(z, d)
	if e != nil {
		return model.Plan{}, e
	}
	pub, _ := hex.DecodeString(ac.PublicKey)
	sim, e := signed(r, pub, make([]byte, 64))
	if e != nil {
		return model.Plan{}, e
	}
	simBCS, e := bcs.Serialize(sim)
	if e != nil {
		return model.Plan{}, errInvalid
	}
	var results []execution
	if e = a.request(ctx, ac.Network, "POST", "/transactions/simulate?estimate_gas_unit_price=false&estimate_max_gas_amount=false", simBCS, &results); e != nil {
		return model.Plan{}, e
	}
	if len(results) != 1 || results[0].Success == nil || !*results[0].Success {
		return model.Plan{}, errors.New("Aptos transfer simulation failed")
	}
	used, e := chainutil.Uint(results[0].GasUsed, 64)
	if e != nil || used == 0 || used > 100000 {
		return model.Plan{}, errInvalid
	}
	gas := used + used/2 + 100
	if gas > 2_000_000 {
		return model.Plan{}, errInvalid
	}
	d.GasUnits = strconv.FormatUint(gas, 10)
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gas), new(big.Int).SetUint64(price))
	required, _ := chainutil.Big(t.Amount, 64)
	required.Add(required, fee)
	bal, _ := chainutil.Big(balance, 64)
	if bal.Cmp(required) < 0 {
		return model.Plan{}, errors.New("insufficient APT including maximum gas")
	}
	z.MaxFee = fee.String()
	r, e = raw(z, d)
	if e != nil {
		return model.Plan{}, e
	}
	b, e := bcs.Serialize(r)
	if e != nil {
		return model.Plan{}, errInvalid
	}
	d.Raw = hex.EncodeToString(b)
	z.Payload, _ = json.Marshal(d)
	return z, a.ValidatePlan(z)
}
func (a *Adapter) ValidatePlan(p model.Plan) error {
	if e := a.account(p.Account); e != nil {
		return e
	}
	if p.Asset != asset(p.Account.Network) || p.FeeAsset != p.Asset || p.Memo != "" {
		return errInvalid
	}
	to, e := address(p.Recipient)
	if e != nil || p.Recipient != to.StringLong() {
		return errInvalid
	}
	var d Payload
	if e = chainutil.DecodeStrict(p.Payload, &d); e != nil {
		return e
	}
	if d.Version != 1 || d.Prepared <= 0 || p.Expires != d.Prepared+120 || p.Expires <= time.Now().Unix() || p.Expires > time.Now().Unix()+120 || len(d.Raw) > 1024 {
		return errInvalid
	}
	if _, e = chainutil.Uint(d.Ledger, 64); e != nil {
		return e
	}
	r, e := raw(p, d)
	if e != nil {
		return e
	}
	b, e := bcs.Serialize(r)
	if e != nil || hex.EncodeToString(b) != d.Raw {
		return errInvalid
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(r.MaxGasAmount), new(big.Int).SetUint64(r.GasUnitPrice))
	if fee.String() != p.MaxFee {
		return errInvalid
	}
	return nil
}
func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	k, e := derivation.Ed25519(seed, []uint32{44, 637, p.Account.Index, 0, 0})
	if e != nil {
		return model.Signed{}, e
	}
	defer clear(k)
	pub, _ := hex.DecodeString(p.Account.PublicKey)
	if !bytes.Equal(k.Public().(ed25519.PublicKey), pub) {
		return model.Signed{}, errors.New("Aptos recovery descriptor key mismatch")
	}
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	r, e := raw(p, d)
	if e != nil {
		return model.Signed{}, e
	}
	message, e := r.SigningMessage()
	if e != nil {
		return model.Signed{}, errInvalid
	}
	sig := ed25519.Sign(k, message)
	s, e := signed(r, pub, sig)
	if e != nil || s.Verify() != nil {
		return model.Signed{}, errInvalid
	}
	b, e := bcs.Serialize(s)
	if e != nil {
		return model.Signed{}, errInvalid
	}
	return model.Signed{Identity: identity(b), Bytes: b}, nil
}
func identity(b []byte) string {
	prefix := sha3.Sum256([]byte("APTOS::Transaction"))
	data := append(prefix[:], 0)
	data = append(data, b...)
	hash := sha3.Sum256(data)
	return "0x" + hex.EncodeToString(hash[:])
}
func (a *Adapter) verify(p model.Plan, s model.Signed) error {
	var d Payload
	if e := chainutil.DecodeStrict(p.Payload, &d); e != nil {
		return e
	}
	r, e := raw(p, d)
	if e != nil {
		return e
	}
	b, e := bcs.Serialize(r)
	if e != nil {
		return errInvalid
	}
	pub, e := hex.DecodeString(p.Account.PublicKey)
	if e != nil || len(pub) != 32 {
		return errInvalid
	}
	if len(s.Bytes) != len(b)+99 || !bytes.Equal(s.Bytes[:len(b)], b) || s.Bytes[len(b)] != 0 || s.Bytes[len(b)+1] != 32 || !bytes.Equal(s.Bytes[len(b)+2:len(b)+34], pub) || s.Bytes[len(b)+34] != 64 || identity(s.Bytes) != s.Identity {
		return errInvalid
	}
	msg, e := r.SigningMessage()
	if e != nil || !ed25519.Verify(pub, msg, s.Bytes[len(b)+35:]) {
		return errInvalid
	}
	return nil
}
func (a *Adapter) Submit(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	z := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	if e := a.ValidatePlan(p); e != nil {
		return z, e
	}
	if e := a.verify(p, s); e != nil {
		return z, e
	}
	l, e := a.identity(ctx, p.Account.Network)
	if e != nil {
		return z, e
	}
	st, e := a.authority(ctx, p.Account, l.Version)
	if e != nil {
		return z, e
	}
	if st.Sequence != p.Nonce {
		return z, errors.New("Aptos sequence changed; reconcile original payment")
	}
	var r execution
	if e = a.request(ctx, p.Account.Network, "POST", "/transactions", s.Bytes, &r); e != nil {
		return z, e
	}
	if r.Hash != s.Identity {
		return z, errInvalid
	}
	if r.Type == "" && r.Success == nil {
		sender, e := address(r.Sender)
		if e != nil || sender.StringLong() != p.Account.Address || r.Sequence != p.Nonce {
			return z, errInvalid
		}
		r.Type = "pending_transaction"
	}
	return receipt(p, s.Identity, r)
}
func receipt(p model.Plan, id string, r execution) (model.Receipt, error) {
	z := model.Receipt{Identity: id, State: "SUBMITTED"}
	if r.Hash != id {
		return z, errInvalid
	}
	switch r.Type {
	case "pending_transaction":
		return z, nil
	case "user_transaction":
		sender, e := address(r.Sender)
		if e != nil || sender.StringLong() != p.Account.Address || r.Sequence != p.Nonce || r.Success == nil {
			return z, errInvalid
		}
		if _, e = chainutil.Uint(r.Version, 64); e != nil {
			return z, e
		}
		z.Block = r.Version
		if *r.Success {
			z.State = "CONFIRMED"
			z.Detail = "Committed execution success"
		} else {
			z.State = "FAILED"
			z.Detail = "Committed execution failure; gas and sequence consumed"
		}
		return z, nil
	default:
		return z, errInvalid
	}
}
func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	z := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	if e := a.account(p.Account); e != nil {
		return z, e
	}
	if e := a.verify(p, s); e != nil {
		return z, e
	}
	if _, e := a.identity(ctx, p.Account.Network); e != nil {
		return z, e
	}
	var r execution
	if e := a.request(ctx, p.Account.Network, "GET", "/transactions/by_hash/"+url.PathEscape(s.Identity), nil, &r); e != nil {
		return z, e
	}
	return receipt(p, s.Identity, r)
}

var _ model.Adapter = (*Adapter)(nil)

// ValidateRecovery preserves an original address association after rotation and
// checks the immutable Legacy Ed25519 derivation/authentication metadata. Online
// authority must still be verified before a send or named association is accepted.
func ValidateRecovery(expected, restored model.Account) error {
	if expected.ID != restored.ID || expected.Network != restored.Network || expected.Index != restored.Index || expected.Profile != restored.Profile || expected.PublicKey != restored.PublicKey || expected.Family != restored.Family {
		return errInvalid
	}
	return New().account(restored)
}
