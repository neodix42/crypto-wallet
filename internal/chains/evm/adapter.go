package evm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
	"math/big"
	"strings"
	"time"
)

type payload struct {
	ChainID            string `json:"chainId"`
	To                 string `json:"to"`
	Value              string `json:"value"`
	Data               string `json:"data"`
	Gas                string `json:"gas"`
	FeeCap             string `json:"feeCap"`
	TipCap             string `json:"tipCap"`
	DataFeeReserve     string `json:"dataFeeReserve"`
	OperatorFeeReserve string `json:"operatorFeeReserve"`
	StateBlock         string `json:"stateBlock"`
	StateHash          string `json:"stateHash"`
	StateTime          string `json:"stateTime"`
}
type block struct {
	Number    string `json:"number"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
	BaseFee   string `json:"baseFeePerGas"`
}

func decimal(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 78 || (len(s) > 1 && s[0] == '0') {
		return nil, errors.New("noncanonical atomic integer")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, errors.New("invalid atomic integer")
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.BitLen() > 256 {
		return nil, errors.New("atomic integer overflow")
	}
	return v, nil
}
func hexInteger(s string) (*big.Int, error) {
	if len(s) < 3 || len(s) > 66 || !strings.HasPrefix(s, "0x") || (len(s) > 3 && s[2] == '0') {
		return nil, errors.New("invalid RPC quantity")
	}
	for _, c := range s[2:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return nil, errors.New("invalid RPC quantity")
		}
	}
	v, ok := new(big.Int).SetString(s[2:], 16)
	if !ok {
		return nil, errors.New("invalid RPC quantity")
	}
	return v, nil
}
func quantity(v *big.Int) string { return "0x" + v.Text(16) }
func uint64Decimal(s string) (uint64, error) {
	v, e := decimal(s)
	if e != nil || !v.IsUint64() {
		return 0, errors.New("nonce/gas overflow")
	}
	return v.Uint64(), nil
}
func word(v *big.Int) []byte { return v.FillBytes(make([]byte, 32)) }
func callData(signature string, args ...[]byte) string {
	b := append([]byte(nil), crypto.Keccak256([]byte(signature))[:4]...)
	for _, x := range args {
		b = append(b, x...)
	}
	return "0x" + hex.EncodeToString(b)
}
func transferData(to string, v *big.Int) string {
	return callData("transfer(address,uint256)", common.BytesToHash(common.HexToAddress(to).Bytes()).Bytes(), word(v))
}

func (a *Adapter) current(ctx context.Context, n model.Network) (block, error) {
	if e := a.checkChain(ctx, n); e != nil {
		return block{}, e
	}
	var b block
	if e := a.rpc(ctx, n, "eth_getBlockByNumber", []any{"latest", false}, &b); e != nil {
		return b, e
	}
	number, e := hexInteger(b.Number)
	if e != nil || !number.IsUint64() || !hashValid(b.Hash) {
		return b, errors.New("invalid block state")
	}
	ts, e := hexInteger(b.Timestamp)
	if e != nil || !ts.IsInt64() || ts.Int64() > time.Now().Unix()+30 || ts.Int64() < time.Now().Unix()-120 {
		return b, errors.New("stale or invalid block state")
	}
	if _, e = hexInteger(b.BaseFee); e != nil {
		return b, errors.New("EIP-1559 fee state unavailable")
	}
	return b, nil
}
func (a *Adapter) balance(ctx context.Context, n model.Network, ac model.Account, s model.Asset, at string) (*big.Int, error) {
	var raw string
	if s.Contract == "" {
		if e := a.rpc(ctx, n, "eth_getBalance", []any{ac.Address, at}, &raw); e != nil {
			return nil, e
		}
		return hexInteger(raw)
	}
	if e := a.tokenIdentity(ctx, n, s, at); e != nil {
		return nil, e
	}
	if e := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": s.Contract, "data": callData("balanceOf(address)", common.BytesToHash(common.HexToAddress(ac.Address).Bytes()).Bytes())}, at}, &raw); e != nil {
		return nil, e
	}
	return abiInteger(raw)
}
func abiInteger(s string) (*big.Int, error) {
	if len(s) != 66 || !cleanHex(s) {
		return nil, errors.New("invalid ABI integer")
	}
	b, e := hex.DecodeString(s[2:])
	if e != nil {
		return nil, errors.New("invalid ABI integer")
	}
	return new(big.Int).SetBytes(b), nil
}
func (a *Adapter) tokenIdentity(ctx context.Context, n model.Network, s model.Asset, at string) error {
	var code string
	if e := a.rpc(ctx, n, "eth_getCode", []any{s.Contract, at}, &code); e != nil {
		return e
	}
	if len(code) < 4 || len(code) > 2<<20 || !cleanHex(code) {
		return errors.New("registered token contract unavailable")
	}
	codeBytes, e := hex.DecodeString(code[2:])
	if e != nil {
		return errors.New("invalid contract code")
	}
	if crypto.Keccak256Hash(codeBytes).Hex() != tokenCodeHashes[n.ID] {
		return errors.New("registered token runtime code hash mismatch")
	}
	var raw string
	if e := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": s.Contract, "data": callData("decimals()")}, at}, &raw); e != nil {
		return e
	}
	v, e := abiInteger(raw)
	if e != nil || v.Uint64() != uint64(s.Decimals) || v.BitLen() > 8 {
		return errors.New("token decimals mismatch")
	}
	if s.ID == USDTAsset {
		// This original contract can forward to an issuer-selected replacement
		// without changing its code. Do not sign through an unreviewed upgrade,
		// paused contract or newly enabled transfer-fee policy.
		for _, signature := range []string{"deprecated()", "paused()", "basisPointsRate()"} {
			raw = "" // JSON null must not reuse the preceding successful read.
			if e = a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": s.Contract, "data": callData(signature)}, at}, &raw); e != nil {
				return e
			}
			state, err := abiInteger(raw)
			if err != nil || state.Sign() != 0 {
				return errors.New("USDT issuer state requires a reviewed policy update")
			}
		}
	}
	return nil
}
func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	n, e := a.validateAccount(ac)
	if e != nil {
		return nil, e
	}
	b, e := a.current(ctx, n)
	if e != nil {
		return nil, e
	}
	out := make([]model.Balance, 0, len(n.Assets))
	for _, s := range n.Assets {
		v, e := a.balance(ctx, n, ac, s, b.Number)
		if e != nil {
			return nil, e
		}
		out = append(out, model.Balance{Asset: s.ID, Atomic: v.String(), State: "provider-observed", At: b.Hash})
	}
	return out, nil
}

func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	fail := func(e error) (model.Plan, error) { return model.Plan{}, e }
	n, e := a.validateAccount(ac)
	if e != nil {
		return fail(e)
	}
	s, e := asset(n, t.Asset)
	if e != nil {
		return fail(e)
	}
	if t.AccountID != ac.ID || t.Memo != "" {
		return fail(errors.New("account mismatch or unsupported EVM memo"))
	}
	if e = a.ValidateAddress(n.ID, t.Recipient); e != nil {
		return fail(e)
	}
	recipient := common.HexToAddress(t.Recipient).Hex()
	if e = recipientPolicy(n, recipient); e != nil {
		return fail(e)
	}
	value, e := decimal(t.Amount)
	if e != nil || value.Sign() == 0 {
		return fail(errors.New("amount must be a positive atomic integer"))
	}
	b, e := a.current(ctx, n)
	if e != nil {
		return fail(e)
	}
	var sourceCode string
	if e = a.rpc(ctx, n, "eth_getCode", []any{ac.Address, b.Number}, &sourceCode); e != nil {
		return fail(e)
	}
	if sourceCode != "0x" {
		return fail(errors.New("contract or delegated EOA authority is unsupported"))
	}
	var nonceHex, tipHex string
	if e = a.rpc(ctx, n, "eth_getTransactionCount", []any{ac.Address, "pending"}, &nonceHex); e != nil {
		return fail(e)
	}
	nonce, e := hexInteger(nonceHex)
	if e != nil || !nonce.IsUint64() {
		return fail(errors.New("invalid account nonce"))
	}
	if e = a.rpc(ctx, n, "eth_maxPriorityFeePerGas", []any{}, &tipHex); e != nil {
		return fail(e)
	}
	tip, e := hexInteger(tipHex)
	if e != nil {
		return fail(e)
	}
	base, _ := hexInteger(b.BaseFee)
	cap := new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip)
	if cap.Sign() == 0 || cap.BitLen() > 256 {
		return fail(errors.New("invalid gas fee state"))
	}
	to := recipient
	native := new(big.Int).Set(value)
	data := "0x"
	if s.Contract != "" {
		to = s.Contract
		native.SetInt64(0)
		data = transferData(recipient, value)
		if e = a.tokenIdentity(ctx, n, s, b.Number); e != nil {
			return fail(e)
		}
	}
	msg := map[string]string{"from": ac.Address, "to": to, "value": quantity(native), "data": data, "maxFeePerGas": quantity(cap), "maxPriorityFeePerGas": quantity(tip)}
	var gasHex string
	if e = a.rpc(ctx, n, "eth_estimateGas", []any{msg, b.Number}, &gasHex); e != nil {
		return fail(e)
	}
	gas, e := hexInteger(gasHex)
	if e != nil || !gas.IsUint64() || gas.Uint64() < 21000 || gas.Uint64() > 25_000_000 {
		return fail(errors.New("invalid gas estimate"))
	}
	gas.Mul(gas, big.NewInt(120))
	gas.Add(gas, big.NewInt(99))
	gas.Div(gas, big.NewInt(100))
	msg["gas"] = quantity(gas)
	var simulated string
	if e = a.rpc(ctx, n, "eth_call", []any{msg, b.Number}, &simulated); e != nil {
		return fail(e)
	}
	// The pinned original Ethereum USDT contract has no return value. Only
	// this exact registered identity permits empty successful simulation data;
	// all other ERC-20 transfers still require the canonical true word.
	if s.Contract != "" && !(s.ID == USDTAsset && simulated == "0x") {
		ok, e := abiInteger(simulated)
		if e != nil || ok.Cmp(big.NewInt(1)) != 0 {
			return fail(errors.New("ERC-20 transfer simulation returned false or invalid data"))
		}
	}
	x := payload{ChainID: n.ChainID, To: to, Value: native.String(), Data: data, Gas: gas.String(), FeeCap: cap.String(), TipCap: tip.String(), DataFeeReserve: "0", OperatorFeeReserve: "0", StateBlock: b.Number, StateHash: b.Hash, StateTime: b.Timestamp}
	p := model.Plan{Account: ac, Asset: s.ID, Recipient: recipient, Amount: value.String(), Nonce: nonce.String(), Expires: time.Now().Add(60 * time.Second).Unix(), FeeAsset: n.Assets[0].ID}
	if rollup(n) {
		// The oracle accepts an unsigned encoded size and adds signature overhead.
		tx := transaction(p, x)
		raw, _ := tx.MarshalBinary()
		l1, op, e := a.rollupFees(ctx, n, uint64(len(raw)), gas.Uint64(), b.Number)
		if e != nil {
			return fail(e)
		}
		x.DataFeeReserve = new(big.Int).Mul(l1, big.NewInt(2)).String()
		x.OperatorFeeReserve = new(big.Int).Mul(op, big.NewInt(2)).String()
	}
	max := new(big.Int).Mul(gas, cap)
	d, _ := decimal(x.DataFeeReserve)
	o, _ := decimal(x.OperatorFeeReserve)
	max.Add(max, d)
	max.Add(max, o)
	if max.BitLen() > 256 {
		return fail(errors.New("fee overflow"))
	}
	p.MaxFee = max.String()
	bal, e := a.balance(ctx, n, ac, n.Assets[0], b.Number)
	if e != nil {
		return fail(e)
	}
	need := new(big.Int).Add(max, native)
	if bal.Cmp(need) < 0 {
		return fail(errors.New("insufficient gas currency balance"))
	}
	pendingNative, e := a.balance(ctx, n, ac, n.Assets[0], "pending")
	if e != nil {
		return fail(e)
	}
	if pendingNative.Cmp(need) < 0 {
		return fail(errors.New("insufficient pending spendable gas currency balance"))
	}
	if s.Contract != "" {
		tok, e := a.balance(ctx, n, ac, s, b.Number)
		if e != nil {
			return fail(e)
		}
		if tok.Cmp(value) < 0 {
			return fail(errors.New("insufficient token balance"))
		}
		pendingToken, e := a.balance(ctx, n, ac, s, "pending")
		if e != nil {
			return fail(e)
		}
		if pendingToken.Cmp(value) < 0 {
			return fail(errors.New("insufficient pending spendable token balance"))
		}
	}
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if e = a.ValidatePlan(p); e != nil {
		return fail(e)
	}
	return p, nil
}
func recipientPolicy(n model.Network, recipient string) error {
	if strings.EqualFold(recipient, "0x2222222222222222222222222222222222222222") {
		return errors.New("system bridge is not an ordinary recipient")
	}
	for _, network := range registry() {
		for _, s := range network.Assets {
			if s.Contract != "" && strings.EqualFold(recipient, s.Contract) {
				return errors.New("token/adapter contract is not an ordinary recipient")
			}
		}
	}
	return nil
}
func planID(p model.Plan) string {
	p.ID = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return "evm:" + hex.EncodeToString(h[:])
}
func (a *Adapter) ValidatePlan(p model.Plan) error {
	n, e := a.validateAccount(p.Account)
	if e != nil {
		return e
	}
	s, e := asset(n, p.Asset)
	if e != nil {
		return e
	}
	if e = a.ValidateAddress(n.ID, p.Recipient); e != nil {
		return e
	}
	if common.HexToAddress(p.Recipient).Hex() != p.Recipient {
		return errors.New("noncanonical recipient")
	}
	if e = recipientPolicy(n, p.Recipient); e != nil {
		return e
	}
	if p.Memo != "" || p.FeeAsset != n.Assets[0].ID || p.ID != planID(p) || p.Expires <= 0 {
		return errors.New("invalid plan identity or policy")
	}
	var x payload
	if e = StrictJSON(p.Payload, &x); e != nil {
		return e
	}
	if x.ChainID != n.ChainID {
		return errors.New("plan chain ID mismatch")
	}
	val, e := decimal(p.Amount)
	if e != nil || val.Sign() == 0 {
		return errors.New("invalid payment amount")
	}
	if _, e = uint64Decimal(p.Nonce); e != nil {
		return e
	}
	gas, e := uint64Decimal(x.Gas)
	if e != nil || gas < 21000 || gas > 30_000_000 {
		return errors.New("gas limit out of bounds")
	}
	cap, e := decimal(x.FeeCap)
	if e != nil || cap.Sign() == 0 {
		return errors.New("invalid fee cap")
	}
	tip, e := decimal(x.TipCap)
	if e != nil || tip.Cmp(cap) > 0 {
		return errors.New("invalid priority fee")
	}
	dataFee, e := decimal(x.DataFeeReserve)
	if e != nil {
		return e
	}
	operatorFee, e := decimal(x.OperatorFeeReserve)
	if e != nil {
		return e
	}
	if !rollup(n) && (dataFee.Sign() != 0 || operatorFee.Sign() != 0) {
		return errors.New("unexpected rollup fee")
	}
	if rollup(n) && dataFee.Sign() == 0 {
		return errors.New("rollup data fee reserve unavailable")
	}
	max := new(big.Int).Mul(new(big.Int).SetUint64(gas), cap)
	max.Add(max, dataFee)
	max.Add(max, operatorFee)
	if max.BitLen() > 256 || p.MaxFee != max.String() {
		return errors.New("maximum fee mismatch")
	}
	if _, e = hexInteger(x.StateBlock); e != nil {
		return e
	}
	if _, e = hexInteger(x.StateTime); e != nil {
		return e
	}
	if len(x.StateHash) != 66 || !cleanHex(x.StateHash) {
		return errors.New("invalid state provenance")
	}
	if _, e = hex.DecodeString(x.StateHash[2:]); e != nil {
		return errors.New("invalid state hash")
	}
	if s.Contract == "" {
		if x.To != p.Recipient || x.Value != p.Amount || x.Data != "0x" {
			return errors.New("native transfer effects mismatch")
		}
	} else {
		if x.To != s.Contract || x.Value != "0" || x.Data != transferData(p.Recipient, val) {
			return errors.New("token contract/calldata effects mismatch")
		}
	}
	return nil
}
func transaction(p model.Plan, x payload) *types.Transaction {
	chain, _ := decimal(x.ChainID)
	value, _ := decimal(x.Value)
	fee, _ := decimal(x.FeeCap)
	tip, _ := decimal(x.TipCap)
	nonce, _ := uint64Decimal(p.Nonce)
	gas, _ := uint64Decimal(x.Gas)
	to := common.HexToAddress(x.To)
	data, _ := hex.DecodeString(strings.TrimPrefix(x.Data, "0x"))
	return types.NewTx(&types.DynamicFeeTx{ChainID: chain, Nonce: nonce, GasTipCap: tip, GasFeeCap: fee, Gas: gas, To: &to, Value: value, Data: data})
}
func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	if p.Expires < time.Now().Unix() || p.Expires > time.Now().Unix()+120 {
		return model.Signed{}, errors.New("expired or invalid transaction approval")
	}
	ac, e := a.Derive(seed, p.Account.Index, p.Account.Network)
	if e != nil {
		return model.Signed{}, e
	}
	if ac.Address != p.Account.Address || ac.PublicKey != p.Account.PublicKey {
		return model.Signed{}, errors.New("signer account does not match derived key")
	}
	key, e := deriveKey(seed, p.Account.Index)
	if e != nil {
		return model.Signed{}, e
	}
	defer destroyKey(key)
	var x payload
	_ = StrictJSON(p.Payload, &x)
	chain, _ := decimal(x.ChainID)
	tx, e := types.SignTx(transaction(p, x), types.NewLondonSigner(chain), key)
	if e != nil {
		return model.Signed{}, errors.New("transaction signing failed")
	}
	raw, e := tx.MarshalBinary()
	if e != nil {
		return model.Signed{}, errors.New("transaction serialization failed")
	}
	signed := model.Signed{Identity: tx.Hash().Hex(), Bytes: raw}
	if _, e = a.verifySigned(p, signed); e != nil {
		return model.Signed{}, e
	}
	return signed, nil
}
func (a *Adapter) verifySigned(p model.Plan, s model.Signed) (*types.Transaction, error) {
	if e := a.ValidatePlan(p); e != nil {
		return nil, e
	}
	if len(s.Bytes) > 4096 || len(s.Bytes) == 0 || len(s.Auxiliary) != 0 {
		return nil, errors.New("invalid signed transaction envelope")
	}
	var tx types.Transaction
	if e := tx.UnmarshalBinary(s.Bytes); e != nil {
		return nil, errors.New("invalid signed transaction")
	}
	var x payload
	_ = StrictJSON(p.Payload, &x)
	unsigned := transaction(p, x)
	chain, _ := decimal(x.ChainID)
	signer := types.NewLondonSigner(chain)
	from, e := types.Sender(signer, &tx)
	if e != nil || from.Hex() != p.Account.Address || tx.Type() != types.DynamicFeeTxType || tx.ChainId().Cmp(chain) != 0 || signer.Hash(&tx) != signer.Hash(unsigned) || tx.Hash().Hex() != s.Identity {
		return nil, errors.New("signed transaction intent/signature mismatch")
	}
	canon, _ := tx.MarshalBinary()
	if !bytes.Equal(canon, s.Bytes) {
		return nil, errors.New("noncanonical signed transaction")
	}
	return &tx, nil
}

func (a *Adapter) rollupFees(ctx context.Context, n model.Network, size, gas uint64, at string) (*big.Int, *big.Int, error) {
	values := make([]*big.Int, 0, 2)
	for _, q := range []struct {
		method string
		arg    uint64
	}{{"getL1FeeUpperBound(uint256)", size}, {"getOperatorFee(uint256)", gas}} {
		var raw string
		e := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": "0x420000000000000000000000000000000000000F", "data": callData(q.method, word(new(big.Int).SetUint64(q.arg)))}, at}, &raw)
		if e != nil {
			return nil, nil, errors.New("trustworthy rollup fee oracle unavailable")
		}
		v, e := abiInteger(raw)
		if e != nil {
			return nil, nil, e
		}
		values = append(values, v)
	}
	return values[0], values[1], nil
}
func (a *Adapter) Submit(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	tx, e := a.verifySigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	n, _ := network(p.Account.Network)
	if e = a.checkChain(ctx, n); e != nil {
		return model.Receipt{}, e
	}
	if p.Expires < time.Now().Unix() {
		return model.Receipt{}, errors.New("plan expired; reconcile original identity before new approval")
	}
	if rollup(n) {
		var x payload
		_ = StrictJSON(p.Payload, &x)
		l1, op, e := a.rollupFees(ctx, n, uint64(len(s.Bytes)), tx.Gas(), "latest")
		if e != nil {
			return model.Receipt{}, e
		}
		d, _ := decimal(x.DataFeeReserve)
		o, _ := decimal(x.OperatorFeeReserve)
		if l1.Cmp(d) > 0 || op.Cmp(o) > 0 {
			return model.Receipt{}, errors.New("rollup fees exceed approved reserve")
		}
	}
	var hash string
	if e = a.rpc(ctx, n, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(s.Bytes)}, &hash); e != nil {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "broadcast outcome ambiguous; reconcile the original signed identity"}, e
	}
	if !strings.EqualFold(hash, s.Identity) {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN"}, errors.New("provider returned a different transaction identity")
	}
	return model.Receipt{Identity: s.Identity, State: "SUBMITTED", Detail: "accepted by RPC; execution and finality not established"}, nil
}

func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	tx, e := a.verifySigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	n, _ := network(p.Account.Network)
	r := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	if e = a.checkChain(ctx, n); e != nil {
		return r, e
	}
	var receipt *struct {
		TransactionHash string `json:"transactionHash"`
		BlockHash       string `json:"blockHash"`
		BlockNumber     string `json:"blockNumber"`
		Status          string `json:"status"`
		Logs            []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
			Removed bool     `json:"removed"`
		} `json:"logs"`
	}
	if e = a.rpc(ctx, n, "eth_getTransactionReceipt", []any{s.Identity}, &receipt); e != nil {
		return r, e
	}
	if receipt == nil {
		var known json.RawMessage
		if e = a.rpc(ctx, n, "eth_getTransactionByHash", []any{s.Identity}, &known); e != nil {
			return r, e
		}
		if string(known) != "null" {
			var observed struct {
				Hash string `json:"hash"`
			}
			if json.Unmarshal(known, &observed) != nil || !hashValid(observed.Hash) || !strings.EqualFold(observed.Hash, s.Identity) {
				return r, errors.New("known transaction identity mismatch")
			}
			r.State = "SUBMITTED"
			r.Detail = "transaction is known; no inclusion receipt"
			return r, nil
		}
		var used string
		if e = a.rpc(ctx, n, "eth_getTransactionCount", []any{p.Account.Address, "latest"}, &used); e != nil {
			return r, e
		}
		v, e := hexInteger(used)
		if e != nil || !v.IsUint64() {
			return r, errors.New("invalid consumed account nonce")
		}
		if v.Uint64() > tx.Nonce() {
			r.Detail = "account nonce consumed; original payment or replacement needs independent discovery"
		} else {
			r.Detail = "identity not observed; absence is not proof of rejection"
		}
		return r, nil
	}
	if !strings.EqualFold(receipt.TransactionHash, s.Identity) || !hashValid(receipt.BlockHash) {
		return r, errors.New("receipt identity mismatch")
	}
	bn, e := hexInteger(receipt.BlockNumber)
	if e != nil || !bn.IsUint64() {
		return r, errors.New("invalid receipt block number")
	}
	var canonical *block
	if e = a.rpc(ctx, n, "eth_getBlockByNumber", []any{receipt.BlockNumber, false}, &canonical); e != nil {
		return r, e
	}
	if canonical == nil || !strings.EqualFold(canonical.Hash, receipt.BlockHash) {
		r.State = "REORGED"
		r.Detail = "receipt block is no longer canonical"
		return r, nil
	}
	r.Block = receipt.BlockHash
	status, e := hexInteger(receipt.Status)
	if e != nil || status.BitLen() > 1 {
		return r, errors.New("invalid execution status")
	}
	execution := "execution succeeded"
	if status.Sign() == 0 {
		execution = "execution reverted; gas was spent"
	}
	sasset, _ := asset(n, p.Asset)
	if sasset.Contract != "" && status.Sign() != 0 {
		event := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
		found := false
		for _, log := range receipt.Logs {
			if !log.Removed && strings.EqualFold(log.Address, sasset.Contract) && len(log.Topics) == 3 && strings.EqualFold(log.Topics[0], event) && strings.EqualFold(log.Topics[1], common.BytesToHash(common.HexToAddress(p.Account.Address).Bytes()).Hex()) && strings.EqualFold(log.Topics[2], common.BytesToHash(common.HexToAddress(p.Recipient).Bytes()).Hex()) {
				v, e := abiInteger(log.Data)
				if e == nil && v.String() == p.Amount {
					found = true
				}
			}
		}
		if !found {
			return r, errors.New("successful receipt lacks the approved token Transfer event")
		}
	}
	var final *block
	if e = a.rpc(ctx, n, "eth_getBlockByNumber", []any{"finalized", false}, &final); e != nil {
		r.State = "SUBMITTED"
		r.Detail = execution + "; finality unavailable"
		return r, nil
	}
	if final == nil {
		r.State = "SUBMITTED"
		r.Detail = execution + "; finality unavailable"
		return r, nil
	}
	height, e := hexInteger(final.Number)
	if e != nil || !height.IsUint64() {
		return r, errors.New("invalid finality block number")
	}
	if height.Cmp(bn) >= 0 {
		if status.Sign() == 0 {
			r.State = "FAILED"
		} else {
			r.State = "CONFIRMED"
		}
		r.Detail = execution + " in canonical finalized block"
	} else {
		r.State = "SUBMITTED"
		r.Detail = execution + "; awaiting finality"
	}
	return r, nil
}

var _ model.Adapter = (*Adapter)(nil)
