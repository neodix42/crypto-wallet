// Package decred implements the versioned BIP39/Decred Child profile and ordinary
// ECDSA P2PKH payments. Network operations trust an explicitly configured dcrd;
// there is no auxiliary wallet or secret-bearing database.
package decred

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

const Profile = "decred-bip39-64-child-v1"
const MaxAtoms int64 = 21_000_000 * 100_000_000
const maxIndex uint32 = 1_000_000
const maxInputs = 128
const dustAtoms int64 = 6030 // conservative P2PKH dust floor at 10,000 atoms/kB

type Parameters struct {
	AccountXPub  string `json:"accountXpub"`
	ReceiveIndex uint32 `json:"receiveIndex"`
	ChangeIndex  uint32 `json:"changeIndex"`
	Birthday     uint32 `json:"birthdayHeight"`
	GapLimit     uint32 `json:"gapLimit"`
	ScanLimit    uint32 `json:"scanLimit"`
	BlockBudget  uint32 `json:"blockBudget,omitempty"`
}

// Endpoint credentials must be loaded by wallet-core from encrypted settings.
// They are never included in public Network descriptors or errors.
type Endpoint struct {
	URL, User, Password string
	Client              *http.Client
}
type Adapter struct {
	mu        sync.RWMutex
	endpoints map[string]Endpoint
	maxBlocks uint32
}

func New() *Adapter { return &Adapter{endpoints: make(map[string]Endpoint), maxBlocks: 10000} }

// SetScanBudget changes the explicitly selected synchronization work limit.
// Larger scans must be shown with progress and never imply zero funds if cut short.
func (a *Adapter) SetScanBudget(blocks uint32) error {
	if blocks < 1 || blocks > 2_000_000 {
		return errors.New("Decred scan budget is out of bounds")
	}
	a.mu.Lock()
	a.maxBlocks = blocks
	a.mu.Unlock()
	return nil
}

// SetDiscovery preserves the account root and issued address sequence. Phrase
// import must never silently choose a recent birthday to skip historical funds.
func SetDiscovery(ac model.Account, birthday, gap, limit uint32) (model.Account, error) {
	x, _, e := descriptor(ac)
	if e != nil {
		return ac, e
	}
	x.Birthday = birthday
	x.GapLimit = gap
	x.ScanLimit = limit
	ac.Parameters, _ = json.Marshal(x)
	if _, _, e = descriptor(ac); e != nil {
		return model.Account{}, e
	}
	return ac, nil
}

// SetDiscoveryBudget selects the durable account-specific historical scan bound.
// Zero preserves the adapter's default; explicit bounds must remain finite.
func SetDiscoveryBudget(ac model.Account, blocks uint32) (model.Account, error) {
	x, _, e := descriptor(ac)
	if e != nil {
		return model.Account{}, e
	}
	if blocks > 2_000_000 {
		return model.Account{}, errors.New("Decred scan budget is out of bounds")
	}
	x.BlockBudget = blocks
	ac.Parameters, _ = json.Marshal(x)
	return ac, nil
}
func params(network string) (*chaincfg.Params, error) {
	switch network {
	case "decred-mainnet":
		return chaincfg.MainNetParams(), nil
	case "decred-testnet3":
		return chaincfg.TestNet3Params(), nil
	case "decred-simnet":
		return chaincfg.SimNetParams(), nil
	}
	return nil, errors.New("unsupported Decred network")
}
func (a *Adapter) Networks() []model.Network {
	var out []model.Network
	for _, id := range []string{"decred-mainnet", "decred-testnet3", "decred-simnet"} {
		p, _ := params(id)
		out = append(out, model.Network{ID: id, Family: "decred", Name: p.Name, Testnet: id != "decred-mainnet", ChainID: p.GenesisHash.String(), Assets: []model.Asset{{ID: id + ":DCR", Symbol: "DCR", Decimals: 8}}, Trust: "Authenticated trusted dcrd; bounded full-block scan; no SPV proof claim"})
	}
	return out
}
func (a *Adapter) Configure(network string, e Endpoint) error {
	if _, err := params(network); err != nil {
		return err
	}
	u, err := url.Parse(e.URL)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid Decred endpoint")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") && network == "decred-simnet") {
		return errors.New("Decred RPC requires authenticated TLS; plaintext is restricted to loopback simnet")
	}
	if e.User == "" || e.Password == "" {
		return errors.New("Decred RPC authentication required")
	}
	if e.Client == nil {
		e.Client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RPC redirect rejected") }}
	}
	client := *e.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("RPC redirect rejected") }
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		client.Timeout = 20 * time.Second
	}
	e.Client = &client
	a.mu.Lock()
	a.endpoints[network] = e
	a.mu.Unlock()
	return nil
}
func (a *Adapter) rpc(ctx context.Context, network, method string, args []any, out any) error {
	a.mu.RLock()
	e, ok := a.endpoints[network]
	a.mu.RUnlock()
	if !ok {
		return errors.New("Decred trusted node is not configured")
	}
	b, err := json.Marshal(map[string]any{"jsonrpc": "1.0", "id": 1, "method": method, "params": args})
	if err != nil {
		return errors.New("Decred RPC request invalid")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.URL, bytes.NewReader(b))
	if err != nil {
		return errors.New("Decred RPC request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(e.User, e.Password)
	res, err := e.Client.Do(req)
	if err != nil {
		return errors.New("Decred RPC unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("Decred RPC HTTP failure")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if err != nil || len(data) > 2<<20 || chainutil.CheckJSON(data) != nil {
		return errors.New("Decred RPC response exceeds limit")
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
		ID int `json:"id"`
	}
	if json.Unmarshal(data, &env) != nil || env.ID != 1 || env.Error != nil || len(env.Result) == 0 {
		return errors.New("Decred RPC response rejected")
	}
	if err = json.Unmarshal(env.Result, out); err != nil {
		return errors.New("Decred RPC result invalid")
	}
	return nil
}
func strict(data []byte, out any) error {
	if len(data) > 2<<20 {
		return errors.New("Decred input exceeds limit")
	}
	return strictjson.Decode(data, out)
}
func descriptor(ac model.Account) (Parameters, *chaincfg.Params, error) {
	p, e := params(ac.Network)
	if e != nil {
		return Parameters{}, nil, e
	}
	var x Parameters
	if ac.Family != "decred" || ac.Profile != Profile || ac.Index >= maxIndex || strict(ac.Parameters, &x) != nil || x.GapLimit < 1 || x.GapLimit > 100 || x.ScanLimit < x.GapLimit || x.ScanLimit > 10000 || x.ReceiveIndex >= maxIndex || x.ChangeIndex >= maxIndex || x.BlockBudget > 2_000_000 {
		return x, nil, errors.New("invalid Decred account descriptor")
	}
	k, e := hdkeychain.NewKeyFromString(x.AccountXPub, p)
	if e != nil {
		return x, nil, errors.New("invalid Decred public descriptor")
	}
	defer k.Zero()
	if k.IsPrivate() || k.Depth() != 3 || k.ChildNum() != hdkeychain.HardenedKeyStart+ac.Index {
		return x, nil, errors.New("invalid Decred account public key")
	}
	addr, pub, e := publicAt(k, p, 0, x.ReceiveIndex)
	if e != nil || addr != ac.Address || hex.EncodeToString(pub) != ac.PublicKey {
		return x, nil, errors.New("Decred receive descriptor mismatch")
	}
	return x, p, nil
}
func childPath(seed []byte, index uint32, p *chaincfg.Params) (*hdkeychain.ExtendedKey, error) {
	if len(seed) != 64 || index >= maxIndex {
		return nil, errors.New("Decred profile requires a complete 64-byte BIP39 seed and bounded account")
	}
	k, e := hdkeychain.NewMaster(seed, p)
	if e != nil {
		return nil, errors.New("Decred master derivation failed")
	}
	coin := uint32(42)
	if p.Net != wire.MainNet {
		coin = 1
	}
	for _, n := range []uint32{44 + hdkeychain.HardenedKeyStart, coin + hdkeychain.HardenedKeyStart, index + hdkeychain.HardenedKeyStart} {
		next, e := k.Child(n)
		k.Zero()
		if e != nil {
			return nil, errors.New("Decred child invalid; account index must be explicitly advanced")
		}
		k = next
	}
	return k, nil
}
func publicAt(k *hdkeychain.ExtendedKey, p *chaincfg.Params, branch, index uint32) (string, []byte, error) {
	if branch > 1 || index >= maxIndex {
		return "", nil, errors.New("invalid Decred address index")
	}
	b, e := k.Child(branch)
	if e != nil {
		return "", nil, e
	}
	defer b.Zero()
	c, e := b.Child(index)
	if e != nil {
		return "", nil, e
	}
	defer c.Zero()
	pub := append([]byte(nil), c.SerializedPubKey()...)
	addr, e := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(stdaddr.Hash160(pub), p)
	if e != nil {
		return "", nil, e
	}
	return addr.String(), pub, nil
}
func (a *Adapter) Derive(seed []byte, index uint32, network string) (model.Account, error) {
	p, e := params(network)
	if e != nil {
		return model.Account{}, e
	}
	k, e := childPath(seed, index, p)
	if e != nil {
		return model.Account{}, e
	}
	defer k.Zero()
	pub := k.Neuter()
	defer pub.Zero()
	addr, pk, e := publicAt(pub, p, 0, 0)
	if e != nil {
		return model.Account{}, e
	}
	x := Parameters{AccountXPub: pub.String(), GapLimit: 20, ScanLimit: 1000}
	raw, _ := json.Marshal(x)
	return model.Account{ID: fmt.Sprintf("decred:%s:%d", network, index), Family: "decred", Profile: Profile, Index: index, Network: network, Address: addr, PublicKey: hex.EncodeToString(pk), Parameters: raw}, nil
}
func AddressAt(ac model.Account, branch, index uint32) (string, error) {
	x, p, e := descriptor(ac)
	if e != nil {
		return "", e
	}
	k, e := hdkeychain.NewKeyFromString(x.AccountXPub, p)
	if e != nil {
		return "", e
	}
	defer k.Zero()
	s, _, e := publicAt(k, p, branch, index)
	return s, e
}

// AdvanceReceive returns metadata the coordinator must durably persist before
// displaying the newly issued address. A hidden account is never renumbered.
func AdvanceReceive(ac model.Account) (model.Account, error) {
	x, p, e := descriptor(ac)
	if e != nil {
		return ac, e
	}
	if x.ReceiveIndex+1 >= maxIndex {
		return ac, errors.New("Decred address range exhausted")
	}
	x.ReceiveIndex++
	k, e := hdkeychain.NewKeyFromString(x.AccountXPub, p)
	if e != nil {
		return ac, e
	}
	defer k.Zero()
	addr, pub, e := publicAt(k, p, 0, x.ReceiveIndex)
	if e != nil {
		return ac, e
	}
	ac.Address = addr
	ac.PublicKey = hex.EncodeToString(pub)
	ac.Parameters, _ = json.Marshal(x)
	return ac, nil
}

// AdvanceChange must be committed in the same transaction as the operation's
// input reservations. Reopening an old backup requires a bounded rescan.
func AdvanceChange(ac model.Account) (model.Account, error) {
	x, _, e := descriptor(ac)
	if e != nil {
		return ac, e
	}
	if x.ChangeIndex+1 >= maxIndex {
		return ac, errors.New("Decred change range exhausted")
	}
	x.ChangeIndex++
	ac.Parameters, _ = json.Marshal(x)
	return ac, nil
}

// ValidateRecovery verifies a restored public descriptor against the freshly
// derived account root while preserving issued receive/change counters. Labels,
// scan ranges and birthday never choose a different recovery root.
func ValidateRecovery(expected, restored model.Account) error {
	base, _, e := descriptor(expected)
	if e != nil {
		return e
	}
	saved, _, e := descriptor(restored)
	if e != nil {
		return e
	}
	if expected.ID != restored.ID || expected.Family != restored.Family || expected.Profile != restored.Profile || expected.Network != restored.Network || expected.Index != restored.Index || base.AccountXPub != saved.AccountXPub {
		return errors.New("restored Decred account does not match recovery root")
	}
	return nil
}
func (a *Adapter) ValidateAddress(network, address string) error {
	p, e := params(network)
	if e != nil {
		return e
	}
	if len(address) > 128 {
		return errors.New("invalid Decred address")
	}
	ad, e := stdaddr.DecodeAddress(address, p)
	if e != nil {
		return errors.New("invalid Decred address or network")
	}
	if _, ok := ad.(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0); !ok {
		return errors.New("only ordinary Decred ECDSA P2PKH recipients are supported")
	}
	return nil
}
func paymentScript(network, address string) (uint16, []byte, error) {
	p, e := params(network)
	if e != nil {
		return 0, nil, e
	}
	ad, e := stdaddr.DecodeAddress(address, p)
	if e != nil {
		return 0, nil, e
	}
	if _, ok := ad.(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0); !ok {
		return 0, nil, errors.New("unsupported Decred script")
	}
	v, s := ad.PaymentScript()
	return v, s, nil
}
func atomic(s string, positive bool) (int64, error) {
	if s == "" || len(s) > 16 || (len(s) > 1 && s[0] == '0') {
		return 0, errors.New("invalid Decred atoms")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid Decred atoms")
		}
	}
	v, e := strconv.ParseInt(s, 10, 64)
	if e != nil || v > MaxAtoms || (positive && v == 0) {
		return 0, errors.New("invalid Decred atoms")
	}
	return v, nil
}
func decimalAtoms(s string) (int64, error) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return 0, errors.New("invalid DCR decimal")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 8 {
		return 0, errors.New("DCR precision exceeds 8 decimals")
	}
	whole, e := atomic(parts[0], false)
	if e != nil || whole > 21_000_000 {
		return 0, errors.New("invalid DCR amount")
	}
	for _, c := range frac {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid DCR amount")
		}
	}
	f, _ := strconv.ParseInt(frac+strings.Repeat("0", 8-len(frac)), 10, 64)
	return whole*1e8 + f, nil
}
func planID(p model.Plan) string {
	p.ID = ""
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Input struct {
	TxID       string `json:"txid"`
	Vout       uint32 `json:"vout"`
	Tree       int8   `json:"tree"`
	Value      int64  `json:"value"`
	Script     string `json:"script"`
	Previous   string `json:"previous"`
	Branch     uint32 `json:"branch"`
	Index      uint32 `json:"index"`
	Height     uint32 `json:"height"`
	BlockIndex uint32 `json:"blockIndex"`
}
type Payload struct {
	Inputs        []Input `json:"inputs"`
	ChangeAddress string  `json:"changeAddress"`
	ChangeIndex   uint32  `json:"changeIndex"`
	Change        int64   `json:"change"`
	Fee           int64   `json:"fee"`
	FeeRate       int64   `json:"feeRate"`
	FeeSource     string  `json:"feeSource"`
	TipHeight     uint32  `json:"tipHeight"`
	TipHash       string  `json:"tipHash"`
	Expiry        uint32  `json:"expiry"`
}

func decodePlan(p model.Plan) (Payload, error) {
	var x Payload
	if strict(p.Payload, &x) != nil {
		return x, errors.New("invalid Decred payment payload")
	}
	return x, nil
}
func coinbase(tx *wire.MsgTx) bool {
	return len(tx.TxIn) > 0 && tx.TxIn[0].PreviousOutPoint.Hash == (chainhash.Hash{}) && tx.TxIn[0].PreviousOutPoint.Index == math.MaxUint32
}
func previous(in Input) (*wire.MsgTx, error) {
	if len(in.Previous) > 1<<20 {
		return nil, errors.New("previous Decred transaction too large")
	}
	b, e := hex.DecodeString(in.Previous)
	if e != nil {
		return nil, errors.New("invalid previous Decred transaction")
	}
	tx := wire.NewMsgTx()
	r := bytes.NewReader(b)
	if !boundedWireTransaction(b) || tx.Deserialize(r) != nil || r.Len() != 0 || tx.TxHash().String() != in.TxID || in.Vout >= uint32(len(tx.TxOut)) {
		return nil, errors.New("Decred previous outpoint mismatch")
	}
	o := tx.TxOut[in.Vout]
	if o.Value != in.Value || o.Version != 0 || hex.EncodeToString(o.PkScript) != in.Script {
		return nil, errors.New("Decred previous input value or script mismatch")
	}
	return tx, nil
}
func makeTx(p model.Plan, x Payload) (*wire.MsgTx, error) {
	tx := wire.NewMsgTx()
	tx.Expiry = x.Expiry
	for _, in := range x.Inputs {
		h, e := chainhash.NewHashFromStr(in.TxID)
		if e != nil {
			return nil, e
		}
		ti := wire.NewTxIn(wire.NewOutPoint(h, in.Vout, in.Tree), in.Value, nil)
		ti.BlockHeight = in.Height
		ti.BlockIndex = in.BlockIndex
		tx.AddTxIn(ti)
	}
	v, script, e := paymentScript(p.Account.Network, p.Recipient)
	if e != nil {
		return nil, e
	}
	amount, e := atomic(p.Amount, true)
	if e != nil {
		return nil, e
	}
	tx.AddTxOut(&wire.TxOut{Value: amount, Version: v, PkScript: script})
	if x.Change > 0 {
		v, s, e := paymentScript(p.Account.Network, x.ChangeAddress)
		if e != nil {
			return nil, e
		}
		tx.AddTxOut(&wire.TxOut{Value: x.Change, Version: v, PkScript: s})
	}
	return tx, nil
}
func signedSize(tx *wire.MsgTx) int {
	copyTx := tx.Copy()
	for _, in := range copyTx.TxIn {
		in.SignatureScript = make([]byte, 108)
	}
	return copyTx.SerializeSize()
}
func feeFor(size int, rate int64) int64 { return (int64(size)*rate + 999) / 1000 }
func (a *Adapter) ValidatePlan(p model.Plan) error {
	x, e := decodePlan(p)
	if e != nil {
		return e
	}
	meta, cp, e := descriptor(p.Account)
	if e != nil {
		return e
	}
	if p.ID != planID(p) || p.Asset != p.Account.Network+":DCR" || p.FeeAsset != p.Asset || p.Memo != "" || p.Nonce != "utxo:"+x.TipHash || p.Expires <= time.Now().Unix() || p.Expires > time.Now().Add(10*time.Minute).Unix() {
		return errors.New("Decred plan metadata mismatch or expiry")
	}
	amount, e := atomic(p.Amount, true)
	if e != nil || amount < dustAtoms {
		return errors.New("Decred recipient is below dust floor")
	}
	maxFee, e := atomic(p.MaxFee, true)
	if e != nil {
		return e
	}
	if len(x.Inputs) == 0 || len(x.Inputs) > maxInputs || x.FeeRate < 10000 || x.FeeRate > 1_000_000 || x.Fee <= 0 || x.Fee != maxFee || x.Fee > 100_000_000 || x.Change < 0 || x.Change > MaxAtoms || (x.Change > 0 && x.Change < dustAtoms) || x.ChangeIndex != meta.ChangeIndex || x.Expiry != x.TipHeight+12 || x.Expiry < x.TipHeight {
		return errors.New("invalid Decred fees, inputs, change or expiry")
	}
	if x.FeeSource != "relay-minimum-no-estimate" && x.FeeSource != "smart-conservative-2" {
		return errors.New("Decred fee provenance is missing")
	}
	if amount < feeFor(603, x.FeeRate) || (x.Change > 0 && x.Change < feeFor(603, x.FeeRate)) {
		return errors.New("Decred output is dust at the approved relay fee rate")
	}
	if _, e = chainhash.NewHashFromStr(x.TipHash); e != nil {
		return errors.New("invalid Decred sync checkpoint")
	}
	change, e := AddressAt(p.Account, 1, x.ChangeIndex)
	if e != nil || change != x.ChangeAddress {
		return errors.New("foreign Decred change output")
	}
	seen := make(map[string]bool)
	var total int64
	for _, in := range x.Inputs {
		key := fmt.Sprintf("%s:%d:%d", in.TxID, in.Vout, in.Tree)
		if seen[key] || in.Tree < 0 || in.Tree > 1 || in.Value <= 0 || in.Value > MaxAtoms || in.Height > x.TipHeight || in.Branch > 1 || in.Index >= maxIndex || total > MaxAtoms-in.Value {
			return errors.New("invalid or duplicate Decred input")
		}
		seen[key] = true
		total += in.Value
		prev, e := previous(in)
		if e != nil {
			return e
		}
		if coinbase(prev) && uint64(x.TipHeight)-uint64(in.Height)+1 < uint64(cp.CoinbaseMaturity) {
			return errors.New("immature Decred input")
		}
		addr, e := AddressAt(p.Account, in.Branch, in.Index)
		if e != nil {
			return e
		}
		_, s, e := paymentScript(p.Account.Network, addr)
		if e != nil || hex.EncodeToString(s) != in.Script {
			return errors.New("Decred input is not owned by this account")
		}
	}
	if amount > MaxAtoms-x.Fee || amount+x.Fee > MaxAtoms-x.Change || total != amount+x.Fee+x.Change {
		return errors.New("Decred spend effects do not balance")
	}
	tx, e := makeTx(p, x)
	if e != nil {
		return e
	}
	required := feeFor(signedSize(tx), x.FeeRate)
	if x.Fee < required || x.Fee > required+feeFor(603, x.FeeRate) {
		return errors.New("Decred fee policy mismatch")
	}
	return nil
}
func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	meta, cp, _ := descriptor(p.Account)
	k, e := childPath(seed, p.Account.Index, cp)
	if e != nil {
		return model.Signed{}, e
	}
	defer k.Zero()
	pub := k.Neuter()
	defer pub.Zero()
	if pub.String() != meta.AccountXPub {
		return model.Signed{}, errors.New("Decred account does not belong to this recovery root")
	}
	x, _ := decodePlan(p)
	tx, e := makeTx(p, x)
	if e != nil {
		return model.Signed{}, e
	}
	for i, in := range x.Inputs {
		branch, e := k.Child(in.Branch)
		if e != nil {
			return model.Signed{}, errors.New("Decred child derivation failed")
		}
		key, e := branch.Child(in.Index)
		branch.Zero()
		if e != nil {
			return model.Signed{}, errors.New("Decred child derivation failed")
		}
		borrowed, e := key.SerializedPrivKey()
		raw := append([]byte(nil), borrowed...)
		key.Zero()
		if e != nil {
			return model.Signed{}, errors.New("Decred key unavailable")
		}
		script, _ := hex.DecodeString(in.Script)
		sig, e := sign.SignatureScript(tx, i, script, txscript.SigHashAll, raw, dcrec.STEcdsaSecp256k1, true)
		clear(raw)
		if e != nil {
			return model.Signed{}, errors.New("Decred signing failed")
		}
		tx.TxIn[i].SignatureScript = sig
	}
	if e = verify(tx, x); e != nil {
		return model.Signed{}, e
	}
	b, e := tx.Bytes()
	if e != nil {
		return model.Signed{}, e
	}
	return model.Signed{Identity: tx.TxHash().String(), Bytes: b}, nil
}
func verify(tx *wire.MsgTx, x Payload) error {
	for i, in := range x.Inputs {
		script, e := hex.DecodeString(in.Script)
		if e != nil {
			return errors.New("invalid Decred input script")
		}
		vm, e := txscript.NewEngine(script, tx, i, txscript.ScriptVerifyCleanStack|txscript.ScriptVerifySigPushOnly|txscript.ScriptVerifyCheckLockTimeVerify|txscript.ScriptVerifyCheckSequenceVerify|txscript.ScriptVerifySHA256|txscript.ScriptVerifyTreasury, 0, nil)
		if e != nil {
			return errors.New("Decred signature engine failed")
		}
		if e = vm.Execute(); e != nil {
			return errors.New("Decred signature verification failed")
		}
	}
	return nil
}
func (a *Adapter) validateSigned(p model.Plan, s model.Signed) (*wire.MsgTx, Payload, error) {
	if e := a.ValidatePlan(p); e != nil {
		return nil, Payload{}, e
	}
	x, _ := decodePlan(p)
	if len(s.Bytes) > 1<<20 {
		return nil, x, errors.New("signed Decred transaction exceeds bounds")
	}
	tx := wire.NewMsgTx()
	r := bytes.NewReader(s.Bytes)
	if !boundedWireTransaction(s.Bytes) || tx.Deserialize(r) != nil || r.Len() != 0 || tx.TxHash().String() != s.Identity {
		return nil, x, errors.New("signed Decred transaction identity mismatch")
	}
	want, e := makeTx(p, x)
	if e != nil {
		return nil, x, e
	}
	unsigned := tx.Copy()
	for _, in := range unsigned.TxIn {
		in.SignatureScript = nil
	}
	b, _ := unsigned.Bytes()
	wb, _ := want.Bytes()
	if !bytes.Equal(b, wb) {
		return nil, x, errors.New("signed Decred transaction differs from approved plan")
	}
	if e = verify(tx, x); e != nil {
		return nil, x, e
	}
	return tx, x, nil
}

// A journaled signature remains a public network artifact after its UI approval
// deadline. Retransmitting those exact bytes needs no second signature; the wire
// transaction's own block expiry still limits network acceptance.
func (a *Adapter) validateArchivedSigned(p model.Plan, s model.Signed) (*wire.MsgTx, Payload, error) {
	if p.ID != planID(p) {
		return nil, Payload{}, errors.New("Decred archived plan digest mismatch")
	}
	check := p
	check.Expires = time.Now().Add(time.Minute).Unix()
	check.ID = planID(check)
	return a.validateSigned(check, s)
}
