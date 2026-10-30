// Package near implements the native Ed25519 NEAR transfer protocol. Network
// methods are public-only; seed methods are restricted to the secure signer.
package near

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

const Profile = "near-bip39-slip10-v1"

var errInvalid = errors.New("invalid NEAR transfer")
var accountPattern = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

type Adapter struct {
	endpoints map[string]string
	client    *chainutil.Client
	tokens    []model.NEP141Token
}

func New() *Adapter {
	return &Adapter{endpoints: map[string]string{"near-mainnet": "https://rpc.mainnet.near.org", "near-testnet": "https://rpc.testnet.near.org", "near-localnet": "http://127.0.0.1:3030"}, client: chainutil.NewClient()}
}
func (a *Adapter) SetEndpoint(network, endpoint string) error {
	if !known(network) {
		return errInvalid
	}
	if e := chainutil.Endpoint(endpoint); e != nil {
		return e
	}
	a.endpoints[network] = endpoint
	return nil
}
func known(s string) bool   { return s == "near-mainnet" || s == "near-testnet" || s == "near-localnet" }
func chain(s string) string { return strings.TrimPrefix(s, "near-") }
func asset(s string) string { return s + ":native" }
func (a *Adapter) Networks() []model.Network {
	out := []model.Network{}
	for _, id := range []string{"near-mainnet", "near-testnet", "near-localnet"} {
		out = append(out, model.Network{ID: id, Family: "near", Name: "NEAR " + chain(id), Testnet: id != "near-mainnet", ChainID: chain(id), RPC: a.endpoints[id], Trust: "TLS RPC; provider assertion, not verified consensus", Assets: []model.Asset{{ID: asset(id), Symbol: "NEAR", Decimals: 24}}})
		for _, token := range a.networkTokens(id) {
			out[len(out)-1].Assets = append(out[len(out)-1].Assets, token.Asset)
		}
	}
	return out
}

type Parameters struct {
	Path     string `json:"path"`
	Implicit string `json:"implicit"`
	Named    bool   `json:"named"`
}

func (a *Adapter) Derive(seed []byte, index uint32, network string) (model.Account, error) {
	if !known(network) {
		return model.Account{}, errInvalid
	}
	k, e := derivation.Ed25519(seed, []uint32{44, 397, index})
	if e != nil {
		return model.Account{}, e
	}
	defer clear(k)
	pub := k.Public().(ed25519.PublicKey)
	address := hex.EncodeToString(pub)
	p, _ := json.Marshal(Parameters{Path: fmt.Sprintf("m/44'/397'/%d'", index), Implicit: address})
	return model.Account{ID: fmt.Sprintf("%s:%d", network, index), Family: "near", Profile: Profile, Index: index, Network: network, Address: address, PublicKey: "ed25519:" + base58.Encode(pub), Parameters: p}, nil
}
func (a *Adapter) ValidateAddress(network, address string) error {
	if !known(network) || len(address) < 2 || len(address) > 64 || !accountPattern.MatchString(address) {
		return errInvalid
	}
	return nil
}
func (a *Adapter) account(ac model.Account) error {
	if ac.Family != "near" || ac.Profile != Profile || ac.Index >= 1<<31 || a.ValidateAddress(ac.Network, ac.Address) != nil {
		return errInvalid
	}
	pub, e := public(ac.PublicKey)
	if e != nil {
		return e
	}
	var p Parameters
	if e = chainutil.DecodeStrict(ac.Parameters, &p); e != nil {
		return e
	}
	if p.Implicit != hex.EncodeToString(pub) || p.Path != fmt.Sprintf("m/44'/397'/%d'", ac.Index) {
		return errInvalid
	}
	if p.Named {
		if ac.Address == p.Implicit {
			return errInvalid
		}
	} else if ac.Address != p.Implicit {
		return errInvalid
	}
	return nil
}
func public(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "ed25519:") || len(s) > 64 {
		return nil, errInvalid
	}
	b := base58.Decode(strings.TrimPrefix(s, "ed25519:"))
	if len(b) != 32 || "ed25519:"+base58.Encode(b) != s {
		return nil, errInvalid
	}
	return b, nil
}

type rpcError struct {
	Code    int             `json:"code"`
	Name    string          `json:"name"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Cause   struct {
		Name string          `json:"name"`
		Info json.RawMessage `json:"info"`
	} `json:"cause"`
}

func (e *rpcError) Error() string { return "NEAR RPC rejected request" }
func (a *Adapter) rpc(ctx context.Context, network, method string, params any, out any) error {
	if !known(network) {
		return errInvalid
	}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "wallet", "method": method, "params": params})
	var r struct {
		Version string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if e := a.client.Do(ctx, "POST", a.endpoints[network], "application/json", b, &r); e != nil {
		return e
	}
	if r.ID != "wallet" || r.Version != "2.0" {
		return errInvalid
	}
	if r.Error != nil {
		if len(r.Result) != 0 {
			return errInvalid
		}
		return r.Error
	}
	if len(r.Result) == 0 || bytes.Equal(r.Result, []byte("null")) {
		return errInvalid
	}
	if e := json.Unmarshal(r.Result, out); e != nil {
		return errInvalid
	}
	return nil
}
func (a *Adapter) identity(ctx context.Context, network string) error {
	var s struct {
		ChainID string `json:"chain_id"`
		Sync    struct {
			Syncing bool   `json:"syncing"`
			Time    string `json:"latest_block_time"`
		} `json:"sync_info"`
	}
	if e := a.rpc(ctx, network, "status", []any{}, &s); e != nil {
		return e
	}
	if s.ChainID != chain(network) || s.Sync.Syncing {
		return errors.New("NEAR wrong network or synchronizing")
	}
	t, e := time.Parse(time.RFC3339Nano, s.Sync.Time)
	if e != nil || time.Since(t) > 2*time.Minute || time.Until(t) > time.Minute {
		return errors.New("NEAR stale or malformed head")
	}
	return nil
}

type accountState struct {
	Amount  string      `json:"amount"`
	Locked  string      `json:"locked"`
	Storage json.Number `json:"storage_usage"`
	Height  json.Number `json:"block_height"`
	Hash    string      `json:"block_hash"`
}
type keyState struct {
	Nonce      json.Number     `json:"nonce"`
	Permission json.RawMessage `json:"permission"`
	Height     json.Number     `json:"block_height"`
	Hash       string          `json:"block_hash"`
}

func (a *Adapter) state(ctx context.Context, ac model.Account) (accountState, keyState, error) {
	var st accountState
	var key keyState
	if e := a.account(ac); e != nil {
		return st, key, e
	}
	if e := a.identity(ctx, ac.Network); e != nil {
		return st, key, e
	}
	e := a.rpc(ctx, ac.Network, "query", map[string]string{"request_type": "view_account", "finality": "final", "account_id": ac.Address}, &st)
	if e != nil {
		return st, key, &accountQueryFailure{cause: e}
	}
	key, e = a.accessKey(ctx, ac, st)
	return st, key, e
}

func (a *Adapter) accessKey(ctx context.Context, ac model.Account, st accountState) (keyState, error) {
	var key keyState
	e := a.rpc(ctx, ac.Network, "query", map[string]string{"request_type": "view_access_key", "block_id": st.Hash, "account_id": ac.Address, "public_key": ac.PublicKey}, &key)
	if e != nil {
		return key, e
	}
	if string(key.Permission) != `"FullAccess"` {
		return key, errors.New("NEAR derived key lacks full-access authority")
	}
	if key.Hash != st.Hash || len(base58.Decode(st.Hash)) != 32 {
		return key, errInvalid
	}
	if _, e = chainutil.Uint(st.Height.String(), 64); e != nil || key.Height != st.Height {
		return key, errInvalid
	}
	return key, nil
}

// Associate verifies a named account against the derived key on this network.
// The returned descriptor must be retained inside the encrypted backup.
func (a *Adapter) Associate(ctx context.Context, ac model.Account, name string) (model.Account, error) {
	if e := a.account(ac); e != nil {
		return model.Account{}, e
	}
	if e := a.ValidateAddress(ac.Network, name); e != nil {
		return model.Account{}, e
	}
	var p Parameters
	_ = json.Unmarshal(ac.Parameters, &p)
	p.Named = name != p.Implicit
	ac.Address = name
	ac.Parameters, _ = json.Marshal(p)
	_, _, e := a.state(ctx, ac)
	return ac, e
}
func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	balances, err := a.nativeBalance(ctx, ac)
	if err != nil || len(a.networkTokens(ac.Network)) == 0 {
		return balances, err
	}
	block, err := a.tokenBlock(ctx, ac.Network, balances[0].At)
	for _, token := range a.networkTokens(ac.Network) {
		balance := model.Balance{Asset: token.Asset.ID, State: "UNKNOWN (token RPC or policy unavailable)"}
		if err == nil && a.verifyTokenAt(ctx, token, block) == nil {
			if value, queryErr := a.tokenBalanceAt(ctx, token, ac.Address, block); queryErr == nil {
				balance.Atomic, balance.State, balance.At = value, "FINAL", block.Hash
			}
		}
		balances = append(balances, balance)
	}
	return balances, nil
}
func (a *Adapter) nativeBalance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	st, _, e := a.state(ctx, ac)
	if e != nil {
		var query *accountQueryFailure
		if errors.As(e, &query) && unknownAccount(query.cause) {
			return a.balanceAfterMissing(ctx, ac)
		}
		return nil, e
	}
	return accountBalance(ac, st)
}

type cost struct {
	Execution json.Number `json:"execution"`
	Send      json.Number `json:"send_not_sir"`
	Same      json.Number `json:"send_sir"`
}
type protocol struct {
	ChainID string      `json:"chain_id"`
	Version json.Number `json:"protocol_version"`
	Runtime struct {
		Storage  string `json:"storage_amount_per_byte"`
		MinPrice string `json:"min_gas_purchase_price"`
		Costs    struct {
			Receipt cost `json:"action_receipt_creation_config"`
			Actions struct {
				Transfer         cost `json:"transfer_cost"`
				FunctionCall     cost `json:"function_call_cost"`
				FunctionCallByte cost `json:"function_call_cost_per_byte"`
				Create           cost `json:"create_account_cost"`
				Add              struct {
					Full cost `json:"full_access_cost"`
				} `json:"add_key_cost"`
			} `json:"action_creation_config"`
		} `json:"transaction_costs"`
	} `json:"runtime_config"`
}
type Payload struct {
	Version         int          `json:"version"`
	BlockHash       string       `json:"blockHash"`
	BlockHeight     string       `json:"blockHeight"`
	Protocol        string       `json:"protocol"`
	Gas             string       `json:"gas"`
	GasPrice        string       `json:"gasPrice"`
	StorageReserve  string       `json:"storageReserve"`
	RecipientExists bool         `json:"recipientExists"`
	Prepared        int64        `json:"prepared"`
	FeePolicy       string       `json:"feePolicy"`
	Staking         *StakingCall `json:"staking,omitempty"`
	Token           *TokenCall   `json:"token,omitempty"`
}

func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	var z model.Plan
	if t.Asset != asset(ac.Network) {
		return a.prepareTokenTransfer(ctx, ac, t)
	}
	if t.AccountID != ac.ID || t.Asset != asset(ac.Network) || t.Memo != "" || a.ValidateAddress(ac.Network, t.Recipient) != nil {
		return z, errInvalid
	}
	amount, e := chainutil.Big(t.Amount, 128)
	if e != nil || amount.Sign() == 0 {
		return z, errInvalid
	}
	st, key, e := a.state(ctx, ac)
	if e != nil {
		return z, e
	}
	nonce, e := chainutil.Uint(key.Nonce.String(), 64)
	if e != nil || nonce == math.MaxUint64 {
		return z, errInvalid
	}
	var recipient accountState
	exists := true
	e = a.rpc(ctx, ac.Network, "query", map[string]string{"request_type": "view_account", "block_id": st.Hash, "account_id": t.Recipient}, &recipient)
	if e != nil {
		var re *rpcError
		if !errors.As(e, &re) || re.Cause.Name != "UNKNOWN_ACCOUNT" {
			return z, e
		}
		exists = false
		if !implicit(t.Recipient) {
			return z, errors.New("NEAR named recipient does not exist")
		}
	}
	var cfg protocol
	if e = a.rpc(ctx, ac.Network, "EXPERIMENTAL_protocol_config", map[string]string{"block_id": st.Hash}, &cfg); e != nil {
		return z, e
	}
	if cfg.ChainID != chain(ac.Network) {
		return z, errInvalid
	}
	var gp struct {
		Price string `json:"gas_price"`
	}
	if e = a.rpc(ctx, ac.Network, "gas_price", []string{st.Hash}, &gp); e != nil {
		return z, e
	}
	price, e := chainutil.Big(gp.Price, 128)
	if e != nil {
		return z, e
	}
	if cfg.Runtime.MinPrice != "" {
		min, e := chainutil.Big(cfg.Runtime.MinPrice, 128)
		if e != nil {
			return z, e
		}
		if price.Cmp(min) < 0 {
			price = min
		}
	}
	gas := new(big.Int)
	costs := []cost{cfg.Runtime.Costs.Receipt, cfg.Runtime.Costs.Actions.Transfer}
	if !exists {
		costs = append(costs, cfg.Runtime.Costs.Actions.Create, cfg.Runtime.Costs.Actions.Add.Full)
	}
	for _, c := range costs {
		for _, s := range []string{c.Execution.String(), c.Send.String()} {
			n, e := chainutil.Big(s, 64)
			if e != nil {
				return z, e
			}
			gas.Add(gas, n)
		}
	}
	if gas.Sign() == 0 || gas.BitLen() > 64 {
		return z, errInvalid
	}
	fee := new(big.Int).Mul(gas, price)
	fee.Mul(fee, big.NewInt(2))
	storage, e := chainutil.Big(cfg.Runtime.Storage, 128)
	if e != nil {
		return z, e
	}
	usage, e := chainutil.Big(st.Storage.String(), 64)
	if e != nil {
		return z, e
	}
	reserve := new(big.Int).Mul(storage, usage)
	balance, e := chainutil.Big(st.Amount, 128)
	if e != nil {
		return z, e
	}
	required := new(big.Int).Add(amount, fee)
	required.Add(required, reserve)
	if balance.Cmp(required) < 0 {
		return z, errors.New("insufficient spendable NEAR including storage and fee reserve")
	}
	now := time.Now().Unix()
	p := Payload{FeePolicy: "protocol-assessed fee; estimate reserve is not a signed fee cap", Version: 1, BlockHash: st.Hash, BlockHeight: st.Height.String(), Protocol: cfg.Version.String(), Gas: gas.String(), GasPrice: price.String(), StorageReserve: reserve.String(), RecipientExists: exists, Prepared: now}
	payload, _ := json.Marshal(p)
	z = model.Plan{Account: ac, Asset: t.Asset, Recipient: t.Recipient, Amount: t.Amount, MaxFee: fee.String(), FeeAsset: t.Asset, Nonce: strconv.FormatUint(nonce+1, 10), Expires: now + 60, Payload: payload}
	return z, a.ValidatePlan(z)
}
func implicit(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func (a *Adapter) ValidatePlan(p model.Plan) error {
	if e := a.account(p.Account); e != nil {
		return e
	}
	if p.FeeAsset != asset(p.Account.Network) || p.Memo != "" || a.ValidateAddress(p.Account.Network, p.Recipient) != nil {
		return errInvalid
	}
	n, e := chainutil.Big(p.Amount, 128)
	if e != nil || n.Sign() == 0 {
		return errInvalid
	}
	nonce, e := chainutil.Uint(p.Nonce, 64)
	if e != nil || nonce == 0 {
		return errInvalid
	}
	var d Payload
	if e = chainutil.DecodeStrict(p.Payload, &d); e != nil {
		return e
	}
	if d.Token == nil {
		if p.Asset != asset(p.Account.Network) {
			return errInvalid
		}
	} else {
		policy, ok := a.token(p.Account.Network, d.Token.Policy.Asset.ID)
		if !ok || policy != d.Token.Policy || validateToken(p, d) != nil {
			return errInvalid
		}
	}
	if d.FeePolicy != "protocol-assessed fee; estimate reserve is not a signed fee cap" || d.Version != 1 || len(base58.Decode(d.BlockHash)) != 32 || d.BlockHash != base58.Encode(base58.Decode(d.BlockHash)) || d.Prepared <= 0 || p.Expires != d.Prepared+60 || p.Expires <= time.Now().Unix() || p.Expires > time.Now().Unix()+60 {
		return errInvalid
	}
	if !d.RecipientExists && !implicit(p.Recipient) {
		return errInvalid
	}
	for _, s := range []string{d.BlockHeight, d.Protocol} {
		if _, e = chainutil.Uint(s, 64); e != nil {
			return e
		}
	}
	gas, e := chainutil.Big(d.Gas, 64)
	maxGas := int64(30_000_000_000_000)
	if d.Staking != nil {
		maxGas = 110_000_000_000_000
		if e := validateStaking(p, d); e != nil {
			return e
		}
	}
	if d.Token != nil {
		maxGas = 40_000_000_000_000
	}
	if e != nil || gas.Sign() == 0 || gas.Cmp(big.NewInt(maxGas)) > 0 {
		return errInvalid
	}
	price, e := chainutil.Big(d.GasPrice, 128)
	if e != nil || price.Sign() == 0 {
		return errInvalid
	}
	fee := new(big.Int).Mul(gas, price)
	fee.Mul(fee, big.NewInt(2))
	if fee.String() != p.MaxFee {
		return errInvalid
	}
	if _, e = chainutil.Big(d.StorageReserve, 128); e != nil {
		return e
	}
	return nil
}
func transaction(p model.Plan) ([]byte, error) {
	var d Payload
	if e := chainutil.DecodeStrict(p.Payload, &d); e != nil {
		return nil, e
	}
	if len(base58.Decode(d.BlockHash)) != 32 {
		return nil, errInvalid
	}
	pub, e := public(p.Account.PublicKey)
	if e != nil {
		return nil, e
	}
	n, e := chainutil.Uint(p.Nonce, 64)
	if e != nil {
		return nil, e
	}
	value, e := chainutil.Big(p.Amount, 128)
	if e != nil {
		return nil, e
	}
	buf := new(bytes.Buffer)
	writeString := func(s string) { _ = binary.Write(buf, binary.LittleEndian, uint32(len(s))); buf.WriteString(s) }
	writeString(p.Account.Address)
	buf.WriteByte(0)
	buf.Write(pub)
	_ = binary.Write(buf, binary.LittleEndian, n)
	receiver := p.Recipient
	if d.Token != nil {
		receiver = d.Token.Policy.Asset.Contract
	}
	writeString(receiver)
	buf.Write(base58.Decode(d.BlockHash))
	_ = binary.Write(buf, binary.LittleEndian, uint32(1))
	if d.Token != nil {
		if e := validateToken(p, d); e != nil {
			return nil, e
		}
		buf.WriteByte(2)
		writeString(d.Token.Method)
		writeString(d.Token.Args)
		gas, _ := chainutil.Uint(d.Token.Gas, 64)
		_ = binary.Write(buf, binary.LittleEndian, gas)
		value, _ = chainutil.Big(d.Token.Deposit, 128)
	} else if d.Staking != nil {
		if e := validateStaking(p, d); e != nil {
			return nil, e
		}
		buf.WriteByte(2) // Borsh Action::FunctionCall; Transfer remains variant 3.
		writeString(d.Staking.Method)
		writeString(d.Staking.Args)
		gas, _ := chainutil.Uint(d.Staking.Gas, 64)
		_ = binary.Write(buf, binary.LittleEndian, gas)
		value, _ = chainutil.Big(d.Staking.Deposit, 128)
	} else {
		buf.WriteByte(3)
	}
	v := value.FillBytes(make([]byte, 16))
	for i := len(v) - 1; i >= 0; i-- {
		buf.WriteByte(v[i])
	}
	return buf.Bytes(), nil
}
func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	k, e := derivation.Ed25519(seed, []uint32{44, 397, p.Account.Index})
	if e != nil {
		return model.Signed{}, e
	}
	defer clear(k)
	pub, _ := public(p.Account.PublicKey)
	if !bytes.Equal(k.Public().(ed25519.PublicKey), pub) {
		return model.Signed{}, errors.New("NEAR recovery descriptor key mismatch")
	}
	b, e := transaction(p)
	if e != nil {
		return model.Signed{}, e
	}
	hash := sha256.Sum256(b)
	sig := ed25519.Sign(k, hash[:])
	if !ed25519.Verify(pub, hash[:], sig) {
		return model.Signed{}, errInvalid
	}
	b = append(b, 0)
	b = append(b, sig...)
	return model.Signed{Identity: base58.Encode(hash[:]), Bytes: b}, nil
}
func (a *Adapter) verify(p model.Plan, s model.Signed) error {
	b, e := transaction(p)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(b)
	pub, e := public(p.Account.PublicKey)
	if e != nil {
		return e
	}
	if s.Identity != base58.Encode(hash[:]) || len(s.Bytes) != len(b)+65 || !bytes.Equal(s.Bytes[:len(b)], b) || s.Bytes[len(b)] != 0 || !ed25519.Verify(pub, hash[:], s.Bytes[len(b)+1:]) {
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
	current, key, e := a.state(ctx, p.Account)
	if e != nil {
		return z, e
	}
	currentNonce, e := chainutil.Uint(key.Nonce.String(), 64)
	if e != nil || currentNonce == math.MaxUint64 || strconv.FormatUint(currentNonce+1, 10) != p.Nonce {
		return z, errors.New("NEAR access-key nonce changed; reconcile original payment")
	}
	_ = current
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	if d.Token != nil {
		if e := a.tokenPreflight(ctx, p, d, current); e != nil {
			return z, e
		}
	}
	var gp struct {
		Price string `json:"gas_price"`
	}
	if e := a.rpc(ctx, p.Account.Network, "gas_price", []any{nil}, &gp); e != nil {
		return z, e
	}
	price, e := chainutil.Big(gp.Price, 128)
	if e != nil {
		return z, e
	}
	budget, _ := chainutil.Big(d.GasPrice, 128)
	budget.Mul(budget, big.NewInt(2))
	if price.Cmp(budget) > 0 {
		return z, errors.New("NEAR gas price exceeded approved reserve; fresh approval required")
	}
	var r execution
	if e := a.rpc(ctx, p.Account.Network, "send_tx", map[string]string{"signed_tx_base64": base64.StdEncoding.EncodeToString(s.Bytes), "wait_until": "FINAL"}, &r); e != nil {
		return z, e
	}
	if d.Token != nil {
		return tokenReceipt(p, s.Identity, r)
	}
	if d.Staking != nil && (r.Transaction.Signer != p.Account.Address || r.Transaction.Receiver != p.Recipient) {
		return z, errInvalid
	}
	if d.Staking != nil {
		return stakingReceipt(p, s.Identity, r)
	}
	return receipt(s.Identity, r)
}

type execution struct {
	Status      json.RawMessage `json:"status"`
	Final       string          `json:"final_execution_status"`
	Transaction struct {
		Hash     string `json:"hash"`
		Signer   string `json:"signer_id"`
		Receiver string `json:"receiver_id"`
	} `json:"transaction"`
	Outcome struct {
		Block   string `json:"block_hash"`
		Outcome struct {
			ReceiptIDs []string `json:"receipt_ids"`
		} `json:"outcome"`
	} `json:"transaction_outcome"`
	Receipts []receiptOutcome `json:"receipts_outcome"`
}

func receipt(id string, r execution) (model.Receipt, error) {
	z := model.Receipt{Identity: id, State: "SUBMITTED", Block: r.Outcome.Block}
	if r.Transaction.Hash != id {
		return z, errInvalid
	}
	if r.Final != "FINAL" {
		return z, nil
	}
	var st map[string]json.RawMessage
	if e := json.Unmarshal(r.Status, &st); e != nil || len(st) != 1 {
		return z, errInvalid
	}
	if _, ok := st["Failure"]; ok {
		z.State = "FAILED"
		z.Detail = "Final execution failure"
		return z, nil
	}
	if _, ok := st["SuccessValue"]; ok {
		z.State = "CONFIRMED"
		z.Detail = "Final execution success"
		return z, nil
	}
	return z, errInvalid
}
func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	z := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	if e := a.account(p.Account); e != nil {
		return z, e
	}
	if e := a.verify(p, s); e != nil {
		return z, e
	}
	if e := a.identity(ctx, p.Account.Network); e != nil {
		return z, e
	}
	var r execution
	if e := a.rpc(ctx, p.Account.Network, "tx", map[string]string{"tx_hash": s.Identity, "sender_account_id": p.Account.Address, "wait_until": "FINAL"}, &r); e != nil {
		return z, e
	}
	if IsTokenPlan(p) {
		return tokenReceipt(p, s.Identity, r)
	}
	if r.Transaction.Signer != p.Account.Address || r.Transaction.Receiver != p.Recipient {
		return z, errInvalid
	}
	if IsStakingPlan(p) {
		return stakingReceipt(p, s.Identity, r)
	}
	return receipt(s.Identity, r)
}

var _ model.Adapter = (*Adapter)(nil)

// ValidateRecovery checks immutable profile data while retaining a named-account
// association. Current on-chain authority is checked by Balance/Prepare/Associate;
// offline descriptor validity must not be mistaken for current account control.
func ValidateRecovery(expected, restored model.Account) error {
	if expected.ID != restored.ID || expected.Network != restored.Network || expected.Index != restored.Index || expected.Profile != restored.Profile || expected.PublicKey != restored.PublicKey || expected.Family != restored.Family {
		return errInvalid
	}
	return New().account(restored)
}
