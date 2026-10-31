// Package ton implements the immutable multichain BIP39/SLIP-0010 Wallet V5R1
// profile. It signs real external-in messages, including first-send StateInit.
package ton

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

const Profile = "ton-bip39-slip10-v5r1-v1"
const CodeHash = "20834b7b72b112147e1b2fb457b84e74d1a30f04f737d4f62a668e9552d2b72f"

// ErrRateLimited is an allowlisted provider condition, never raw RPC text.
// Callers must retain the original signed identity if a broadcast is uncertain.
var ErrRateLimited = errors.New("TON RPC rate limited")

// Contract artifact from ton-blockchain/wallet-contract-v5 commit
// ff2a9cdb5abe3efdba1e773e4d03a53dea1550b1/build/wallet_v5.compiled.json.
//
//go:embed wallet_v5r1.boc
var codeBOC []byte

type Parameters struct {
	Version   string `json:"version"`
	Workchain int8   `json:"workchain"`
	WalletID  uint32 `json:"walletId"`
	GlobalID  int32  `json:"globalId"`
	Subwallet uint16 `json:"subwallet"`
	CodeHash  string `json:"codeHash"`
	StateInit string `json:"stateInit"`
}
type Endpoint struct {
	URL, APIKey string
	Client      *http.Client
}
type Adapter struct {
	mu              sync.RWMutex
	endpoints       map[string]Endpoint
	publicScheduler *publicRateScheduler // private fixture override; nil uses the shared host quota
}
type network struct {
	global          int32
	wallet          uint32
	root, file, url string
}

func netInfo(id string) (network, error) {
	switch id {
	case "ton-mainnet":
		return network{-239, 0x7fffff11, "F6OpKZKqvqeFp6CQmFomXNMfMj2EnaUSOXN+Mh+wVWk=", "XplPz01CXAps5qeSWUtxcyBfdAo5zVb1N979KLSKD24=", "https://toncenter.com/api/v2/jsonRPC"}, nil
	case "ton-testnet":
		return network{-3, 0x7ffffffd, "gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=", "Z+IKwYS54DmmJmesw/nAD5DzWadnOCMzee+kdgSYDOg=", "https://testnet.toncenter.com/api/v2/jsonRPC"}, nil
	}
	return network{}, errors.New("unsupported TON network")
}
func New() *Adapter {
	a := &Adapter{endpoints: map[string]Endpoint{}}
	for _, id := range []string{"ton-mainnet", "ton-testnet"} {
		n, _ := netInfo(id)
		a.endpoints[id] = Endpoint{URL: n.url, Client: officialPublicHTTP(n.url, "", defaultHTTP())}
	}
	return a
}
func defaultHTTP() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("TON RPC redirect rejected") }}
}
func (a *Adapter) Networks() []model.Network {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []model.Network
	for _, id := range []string{"ton-mainnet", "ton-testnet"} {
		n, _ := netInfo(id)
		name := "TON Mainnet"
		if id == "ton-testnet" {
			name = "TON Testnet"
		}
		assets := []model.Asset{{ID: id + ":TON", Symbol: "GRAM", Decimals: 9}}
		if id == "ton-mainnet" {
			assets = append(assets, usdtAsset())
		}
		out = append(out, model.Network{ID: id, Family: "ton", Name: name, Testnet: id == "ton-testnet", ChainID: strconv.FormatInt(int64(n.global), 10), RPC: a.endpoints[id].URL, Assets: assets, Trust: "Trusted TON Center-compatible RPC; zerostate identity checked; transaction BOCs locally interpreted; no consensus-proof claim"})
	}
	return out
}
func (a *Adapter) Configure(id string, e Endpoint) error {
	if _, er := netInfo(id); er != nil {
		return er
	}
	if er := chainutil.Endpoint(e.URL); er != nil {
		return er
	}
	if e.Client == nil {
		e.Client = defaultHTTP()
	}
	e.Client = officialPublicHTTP(e.URL, e.APIKey, e.Client)
	client := *e.Client
	client.CheckRedirect = defaultHTTP().CheckRedirect
	if client.Timeout <= 0 || client.Timeout > 30*time.Second {
		client.Timeout = 20 * time.Second
	}
	e.Client = &client
	a.mu.Lock()
	a.endpoints[id] = e
	a.mu.Unlock()
	return nil
}
func (a *Adapter) rpc(ctx context.Context, id, method string, args any, out any) error {
	a.mu.RLock()
	e, ok := a.endpoints[id]
	a.mu.RUnlock()
	if !ok {
		return errors.New("TON network is not configured")
	}
	raw, er := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": args})
	if er != nil {
		return errors.New("TON RPC request invalid")
	}
	req, er := http.NewRequestWithContext(ctx, "POST", e.URL, bytes.NewReader(raw))
	if er != nil {
		return errors.New("TON RPC request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	// RPC identity/state/fee responses must never be supplied from a shared
	// intermediary's stale or incorrectly keyed cache. Identity checks remain
	// mandatory even when the intermediary ignores these request directives.
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Pragma", "no-cache")
	if e.APIKey != "" {
		req.Header.Set("X-API-Key", e.APIKey)
	}
	release, er := a.beginPublic(ctx, e)
	if er != nil {
		return er
	}
	defer release()
	res, er := e.Client.Do(req)
	if er != nil {
		return errors.New("TON RPC unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if res.StatusCode != 200 {
		return errors.New("TON RPC HTTP failure")
	}
	raw, er = io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
	if er != nil || len(raw) > 2<<20 || chainutil.CheckJSON(raw) != nil {
		return errors.New("TON RPC response invalid or oversized")
	}
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
		ID     json.RawMessage `json:"id"`
		Code   int             `json:"code"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return errors.New("TON RPC rejected request")
	}
	var remoteError struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(env.Error, &remoteError)
	if env.Code == 429 || remoteError.Code == 429 {
		return ErrRateLimited
	}
	if !env.OK || (len(env.ID) > 0 && string(env.ID) != "1" && string(env.ID) != `"1"`) || len(env.Result) == 0 || (len(env.Error) > 0 && string(env.Error) != "null") {
		return errors.New("TON RPC rejected request")
	}
	if json.Unmarshal(env.Result, out) != nil {
		return errors.New("TON RPC result invalid")
	}
	return nil
}
func parseBOC(raw []byte) (*cell.Cell, error) {
	if len(raw) == 0 || len(raw) > 256<<10 {
		return nil, errors.New("TON BOC exceeds bounds")
	}
	c, e := cell.FromBOCWithOptions(raw, cell.BOCParseOptions{MaxCells: 2048})
	if e != nil {
		return nil, errors.New("invalid TON BOC")
	}
	return c, nil
}
func decodeBOC(s string) (*cell.Cell, error) {
	if len(s) > 350000 {
		return nil, errors.New("TON BOC exceeds bounds")
	}
	raw, e := base64.StdEncoding.DecodeString(s)
	if e != nil {
		return nil, errors.New("invalid TON BOC encoding")
	}
	return parseBOC(raw)
}

// StateInit retains the original V1 contract parameters for fixture and legacy
// recovery compatibility. New accounts use Derive's explicit DefaultProfile;
// sending/deployment reconstruct StateInit from the recorded descriptor.
func StateInit(pub ed25519.PublicKey, id string) (*tlb.StateInit, *cell.Cell, error) {
	n, e := netInfo(id)
	if e != nil {
		return nil, nil, e
	}
	return stateInitForWalletID(pub, n.wallet)
}
func stateInitForWalletID(pub ed25519.PublicKey, walletID uint32) (*tlb.StateInit, *cell.Cell, error) {
	if len(pub) != 32 {
		return nil, nil, errors.New("invalid TON public key")
	}
	code, e := parseBOC(codeBOC)
	if e != nil || hex.EncodeToString(code.Hash()) != CodeHash {
		return nil, nil, errors.New("pinned TON V5R1 code hash mismatch")
	}
	data := cell.BeginCell().MustStoreBoolBit(true).MustStoreUInt(0, 32).MustStoreUInt(uint64(walletID), 32).MustStoreSlice(pub, 256).MustStoreBoolBit(false).EndCell()
	s := &tlb.StateInit{Code: code, Data: data}
	c, e := tlb.ToCell(s)
	return s, c, e
}
func account(pub ed25519.PublicKey, index uint32, id string) (model.Account, error) {
	return accountForProfile(pub, index, id, Profile)
}
func accountForProfile(pub ed25519.PublicKey, index uint32, id, profile string) (model.Account, error) {
	n, e := netInfo(id)
	if e != nil || index >= derivation.MaxIndex {
		return model.Account{}, errors.New("invalid TON account")
	}
	subwallet, walletID, e := profileWalletID(profile, n.global)
	if e != nil {
		return model.Account{}, e
	}
	_, state, e := stateInitForWalletID(pub, walletID)
	if e != nil {
		return model.Account{}, e
	}
	ad := address.NewAddress(0, 0, state.Hash()).Bounce(false).Testnet(id == "ton-testnet")
	p := Parameters{Version: "V5R1", WalletID: walletID, GlobalID: n.global, Subwallet: subwallet, CodeHash: CodeHash, StateInit: base64.StdEncoding.EncodeToString(state.ToBOC())}
	raw, _ := json.Marshal(p)
	accountID := fmt.Sprintf("ton:%s:%d", id, index)
	if profile != Profile {
		accountID += ":subwallet42"
	}
	return model.Account{ID: accountID, Family: "ton", Profile: profile, Index: index, Network: id, Address: ad.String(), PublicKey: hex.EncodeToString(pub), Parameters: raw}, nil
}
func (a *Adapter) Derive(seed []byte, index uint32, id string) (model.Account, error) {
	return a.DeriveProfile(seed, index, id, DefaultProfile)
}
func (a *Adapter) DeriveProfile(seed []byte, index uint32, id, profile string) (model.Account, error) {
	if index >= derivation.MaxIndex {
		return model.Account{}, errors.New("TON account index exceeds bound")
	}
	k, e := derivation.Ed25519(seed, []uint32{44, 607, index})
	if e != nil {
		return model.Account{}, e
	}
	defer clear(k)
	return accountForProfile(k.Public().(ed25519.PublicKey), index, id, profile)
}
func descriptor(ac model.Account) (Parameters, ed25519.PublicKey, error) {
	var p Parameters
	if ac.Family != "ton" || strictjson.Decode(ac.Parameters, &p) != nil {
		return p, nil, errors.New("invalid TON account descriptor")
	}
	pub, e := hex.DecodeString(ac.PublicKey)
	if e != nil || len(pub) != 32 {
		return p, nil, errors.New("invalid TON public key")
	}
	want, e := accountForProfile(pub, ac.Index, ac.Network, ac.Profile)
	canonicalParameters, _ := json.Marshal(p)
	if e != nil || want.ID != ac.ID || want.Address != ac.Address || !bytes.Equal(want.Parameters, canonicalParameters) {
		return p, nil, errors.New("TON StateInit or contract parameters mismatch")
	}
	return p, pub, nil
}
func parseAddress(id, s string) (*address.Address, error) {
	if _, e := netInfo(id); e != nil {
		return nil, e
	}
	if len(s) > 80 || s == "" {
		return nil, errors.New("invalid TON address")
	}
	var ad *address.Address
	var e error
	if strings.Contains(s, ":") {
		ad, e = address.ParseRawAddr(s)
	} else {
		ad, e = address.ParseAddr(s)
		if e == nil {
			raw, er := base64.RawURLEncoding.DecodeString(s)
			if er != nil {
				raw, er = base64.RawStdEncoding.DecodeString(s)
			}
			if er != nil || len(raw) != 36 || (raw[0]&0x7f != 0x11 && raw[0]&0x7f != 0x51) {
				return nil, errors.New("invalid TON address flags")
			}
		}
	}
	if e != nil || ad.Workchain() != 0 {
		return nil, errors.New("invalid or unsupported TON workchain")
	}
	if id == "ton-mainnet" && ad.IsTestnetOnly() {
		return nil, errors.New("TON testnet-only address cannot be used on mainnet")
	}
	return ad, nil
}
func (a *Adapter) ValidateAddress(id, s string) error { _, e := parseAddress(id, s); return e }
func planID(p model.Plan) string {
	p.ID = ""
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type Payload struct {
	Kind           string         `json:"kind,omitempty"`
	Seqno          uint32         `json:"seqno"`
	Deploy         bool           `json:"deploy"`
	Bounce         bool           `json:"bounce"`
	SourceBalance  string         `json:"sourceBalance"`
	EstimatedFee   string         `json:"estimatedFee"`
	StorageReserve string         `json:"storageReserve"`
	SourceDataHash string         `json:"sourceDataHash"`
	MasterSeqno    uint32         `json:"masterSeqno"`
	Jetton         *JettonPayload `json:"jetton,omitempty"`
}

func payload(p model.Plan) (Payload, error) {
	var x Payload
	if strictjson.Decode(p.Payload, &x) != nil {
		return x, errors.New("invalid TON payment payload")
	}
	return x, nil
}
func message(p model.Plan, x Payload) (*cell.Cell, *tlb.InternalMessage, error) {
	meta, _, e := descriptor(p.Account)
	if e != nil {
		return nil, nil, e
	}
	if x.Kind == "deployment" {
		// InnerRequest with neither outgoing messages nor extended actions. The
		// signed external message activates exactly its own StateInit only.
		unsigned := cell.BeginCell().MustStoreUInt(0x7369676e, 32).MustStoreUInt(uint64(meta.WalletID), 32).MustStoreUInt(uint64(p.Expires), 32).MustStoreUInt(uint64(x.Seqno), 32).MustStoreBoolBit(false).MustStoreBoolBit(false).EndCell()
		return unsigned, nil, nil
	}
	ad, e := parseAddress(p.Account.Network, p.Recipient)
	if e != nil {
		return nil, nil, e
	}
	amount, e := chainutil.Big(p.Amount, 120)
	if e != nil || amount.Sign() == 0 {
		return nil, nil, errors.New("invalid GRAM transfer amount")
	}
	body := cell.BeginCell()
	if p.Memo != "" {
		body.MustStoreUInt(0, 32).MustStoreSlice([]byte(p.Memo), uint(len(p.Memo)*8))
	}
	internal := &tlb.InternalMessage{IHRDisabled: true, Bounce: x.Bounce, DstAddr: ad, Amount: tlb.FromNanoTON(amount), Body: body.EndCell()}
	if x.Kind == "jetton_transfer" {
		if err := validateJetton(p, x); err != nil {
			return nil, nil, err
		}
		wallet, _ := parseAddress(p.Account.Network, x.Jetton.Wallet)
		transfer, err := jettonBody(p, x)
		if err != nil {
			return nil, nil, err
		}
		internal = &tlb.InternalMessage{IHRDisabled: true, Bounce: true, DstAddr: wallet, Amount: tlb.FromNanoTONU(jettonAttached), Body: transfer}
	}
	out, e := tlb.ToCell(internal)
	if e != nil {
		return nil, nil, e
	}
	list := cell.BeginCell().MustStoreRef(cell.BeginCell().EndCell()).MustStoreUInt(0x0ec3c86d, 32).MustStoreUInt(3, 8).MustStoreRef(out).EndCell()
	unsigned := cell.BeginCell().MustStoreUInt(0x7369676e, 32).MustStoreUInt(uint64(meta.WalletID), 32).MustStoreUInt(uint64(p.Expires), 32).MustStoreUInt(uint64(x.Seqno), 32).MustStoreBoolBit(true).MustStoreRef(list).MustStoreBoolBit(false).EndCell()
	return unsigned, internal, nil
}
func external(p model.Plan, x Payload, body *cell.Cell) (*cell.Cell, error) {
	meta, pub, e := descriptor(p.Account)
	_ = meta
	if e != nil {
		return nil, e
	}
	ad, e := parseAddress(p.Account.Network, p.Account.Address)
	if e != nil {
		return nil, e
	}
	ext := &tlb.ExternalMessage{DstAddr: ad, Body: body}
	if x.Deploy {
		ext.StateInit, _, e = stateInitForWalletID(pub, meta.WalletID)
		if e != nil {
			return nil, e
		}
	}
	return tlb.ToCell(ext)
}
func (a *Adapter) ValidatePlan(p model.Plan) error {
	if _, _, e := descriptor(p.Account); e != nil {
		return e
	}
	x, e := payload(p)
	if e != nil {
		return e
	}
	jetton := x.Kind == "jetton_transfer"
	if p.ID != planID(p) || (!jetton && p.Asset != p.Account.Network+":TON") || p.FeeAsset != p.Account.Network+":TON" || p.Nonce != strconv.FormatUint(uint64(x.Seqno), 10) || p.Expires <= time.Now().Unix() || p.Expires > time.Now().Add(10*time.Minute).Unix() || p.Expires > 0xffffffff || !utf8.ValidString(p.Memo) || len(p.Memo) > 120 {
		return errors.New("TON plan metadata mismatch or expiry")
	}
	if e = a.ValidateAddress(p.Account.Network, p.Recipient); e != nil {
		return e
	}
	deployment := x.Kind == "deployment"
	if x.Kind != "" && !deployment && !jetton {
		return errors.New("unsupported TON operation")
	}
	if jetton {
		if e = validateJetton(p, x); e != nil {
			return e
		}
	} else if x.Jetton != nil {
		return errors.New("unexpected jetton payload in native operation")
	}
	if deployment && (!x.Deploy || x.Seqno != 0 || x.Bounce || x.SourceDataHash != "" || p.Recipient != p.Account.Address || p.Memo != "" || p.Amount != "0") {
		return errors.New("TON deployment must initialize only its exact owned contract without outgoing actions")
	}
	amount, e := chainutil.Big(p.Amount, 120)
	if e != nil || (!deployment && amount.Sign() == 0) {
		return errors.New("invalid GRAM amount")
	}
	fee, e := chainutil.Uint(p.MaxFee, 64)
	if e != nil || fee > 500_000_000 {
		return errors.New("TON fee exceeds policy")
	}
	estimate, e := chainutil.Uint(x.EstimatedFee, 64)
	attached := uint64(0)
	if jetton {
		attached = jettonAttached
	}
	if e != nil || estimate > 249_500_000 || fee != estimate*2+1_000_000+attached || x.StorageReserve != "10000000" {
		return errors.New("TON fee or storage reserve mismatch")
	}
	balance, e := chainutil.Big(x.SourceBalance, 120)
	if e != nil {
		return e
	}
	cost := new(big.Int).Add(nativeSpend(p, x), new(big.Int).SetUint64(fee+10_000_000))
	if balance.Cmp(cost) < 0 {
		return errors.New("insufficient GRAM for payment, fees and storage reserve")
	}
	if x.Deploy && x.Seqno != 0 {
		return errors.New("TON deployment seqno must be zero")
	}
	if !x.Deploy {
		hash, e := hex.DecodeString(x.SourceDataHash)
		if e != nil || len(hash) != 32 {
			return errors.New("invalid TON authority-state checkpoint")
		}
	}
	_, _, e = message(p, x)
	return e
}
func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	key, e := derivation.Ed25519(seed, []uint32{44, 607, p.Account.Index})
	if e != nil {
		return model.Signed{}, e
	}
	defer clear(key)
	if hex.EncodeToString(key.Public().(ed25519.PublicKey)) != p.Account.PublicKey {
		return model.Signed{}, errors.New("TON key does not belong to account")
	}
	x, _ := payload(p)
	unsigned, _, e := message(p, x)
	if e != nil {
		return model.Signed{}, e
	}
	sig := ed25519.Sign(key, unsigned.Hash())
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), unsigned.Hash(), sig) {
		return model.Signed{}, errors.New("TON signature verification failed")
	}
	body := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(sig, 512).EndCell()
	ext, e := external(p, x, body)
	if e != nil {
		return model.Signed{}, e
	}
	return model.Signed{Identity: hex.EncodeToString(ext.Hash()), Bytes: ext.ToBOC()}, nil
}
func (a *Adapter) validateSigned(p model.Plan, s model.Signed) (*cell.Cell, *tlb.ExternalMessage, error) {
	if e := a.ValidatePlan(p); e != nil {
		return nil, nil, e
	}
	c, e := parseBOC(s.Bytes)
	if e != nil || hex.EncodeToString(c.Hash()) != s.Identity {
		return nil, nil, errors.New("TON signed message identity mismatch")
	}
	slice, e := c.BeginParse()
	if e != nil {
		return nil, nil, e
	}
	var ext tlb.ExternalMessage
	if e = tlb.LoadFromCell(&ext, slice); e != nil {
		return nil, nil, errors.New("invalid TON external message")
	}
	if ext.Body == nil {
		return nil, nil, errors.New("TON external body absent")
	}
	bs, e := ext.Body.BeginParse()
	if e != nil {
		return nil, nil, e
	}
	x, _ := payload(p)
	wantRefs := 1
	if x.Kind == "deployment" {
		wantRefs = 0
	}
	if bs.BitsLeft() != 642 || bs.RefsNum() != wantRefs {
		return nil, nil, errors.New("invalid TON signed body shape")
	}
	head, e := bs.LoadSlice(130)
	if e != nil {
		return nil, nil, e
	}
	sig, e := bs.LoadSlice(512)
	if e != nil {
		return nil, nil, e
	}
	unsigned := cell.BeginCell().MustStoreSlice(head, 130)
	if wantRefs == 1 {
		ref, e := bs.LoadRefCell()
		if e != nil {
			return nil, nil, e
		}
		unsigned.MustStoreRef(ref)
	}
	u := unsigned.EndCell()
	want, _, e := message(p, x)
	if e != nil {
		return nil, nil, e
	}
	_, pub, _ := descriptor(p.Account)
	if !bytes.Equal(u.Hash(), want.Hash()) || !ed25519.Verify(pub, u.Hash(), sig) {
		return nil, nil, errors.New("TON signature or approved payload mismatch")
	}
	expectedBody := cell.BeginCell().MustStoreBuilder(want.ToBuilder()).MustStoreSlice(sig, 512).EndCell()
	expected, e := external(p, x, expectedBody)
	if e != nil || !bytes.Equal(expected.Hash(), c.Hash()) {
		return nil, nil, errors.New("TON external message differs from approved spend effects")
	}
	return c, &ext, nil
}
