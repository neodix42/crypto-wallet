// Package hypercore implements only owner-signed pinned spot-asset peer transfers.
// HyperCore and HyperEVM intentionally have distinct network and asset identities.
package hypercore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"io"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const Profile = "evm-bip39-bip32-v1"
const mainToken = "HYPE:0x0d01dc56dcaaca66ad901c959b4011ec"
const testToken = "HYPE:0x7317beb7cceed72ef0b346074cc8e7ab"

// Official spotMeta index 0, weiDecimals 8:
// https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint/spot
// This is the Core spot ledger,
// not an ERC-20 contract, a perpetual balance or an implicit bridge route.
const mainUSDCToken = "USDC:0x6d1e7cde53ba9467b783cb7c530ce054"

type Adapter struct {
	endpoints  map[string]string
	HTTPClient *http.Client
	mu         sync.Mutex
	lastNonce  uint64
}

func New() *Adapter { a, _ := NewWithEndpoints(nil); return a }
func NewWithEndpoints(endpoints map[string]string) (*Adapter, error) {
	a := &Adapter{endpoints: map[string]string{}, HTTPClient: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	for id, s := range endpoints {
		if _, e := network(strings.TrimSuffix(id, ":explorer")); e != nil {
			return nil, e
		}
		if e := evm.ValidateEndpoint(s); e != nil {
			return nil, e
		}
		a.endpoints[id] = strings.TrimSuffix(s, "/")
	}
	return a, nil
}
func networks() []model.Network {
	return []model.Network{
		{ID: "hypercore-mainnet", Family: "hypercore", Name: "HyperCore Mainnet spot", ChainID: "HyperCore Mainnet", RPC: "https://api.hyperliquid.xyz", Assets: []model.Asset{{ID: "hypercore-mainnet:" + mainToken, Symbol: "HYPE", Decimals: 8, Contract: mainToken}, {ID: "hypercore-mainnet:" + mainUSDCToken, Symbol: "USDC", Decimals: 8, Contract: mainUSDCToken}}, Trust: "official TLS API and explorer; pinned spot token identities checked; not a consensus proof"},
		{ID: "hypercore-testnet", Family: "hypercore", Name: "HyperCore Testnet spot", ChainID: "HyperCore Testnet", RPC: "https://api.hyperliquid-testnet.xyz", Testnet: true, Assets: []model.Asset{{ID: "hypercore-testnet:" + testToken, Symbol: "HYPE", Decimals: 8, Contract: testToken}}, Trust: "official TLS API and explorer; HYPE token identity checked; not a consensus proof"},
	}
}
func network(id string) (model.Network, error) {
	for _, n := range networks() {
		if n.ID == id {
			return n, nil
		}
	}
	return model.Network{}, errors.New("unsupported HyperCore ledger")
}
func (a *Adapter) Networks() []model.Network {
	ns := networks()
	for i := range ns {
		if ep, ok := a.endpoints[ns[i].ID]; ok {
			ns[i].RPC = ep
		}
	}
	return ns
}
func (a *Adapter) Derive(seed []byte, index uint32, net string) (model.Account, error) {
	if _, e := network(net); e != nil {
		return model.Account{}, e
	}
	ac, e := evm.New().Derive(seed, index, "ethereum-mainnet")
	if e != nil {
		return ac, e
	}
	ac.ID = fmt.Sprintf("hypercore:%d:%s", index, net)
	ac.Network = net
	ac.Family = "hypercore"
	return ac, nil
}
func (a *Adapter) ValidateAddress(net, s string) error {
	if _, e := network(net); e != nil {
		return e
	}
	if e := evm.New().ValidateAddress("ethereum-mainnet", s); e != nil {
		return e
	}
	if strings.EqualFold(s, "0x2222222222222222222222222222222222222222") || strings.HasPrefix(strings.ToLower(s), "0x200000000000000000000000000000000000") {
		return errors.New("Core system/bridge address is not an ordinary recipient")
	}
	return nil
}
func (a *Adapter) validateAccount(ac model.Account) (model.Network, error) {
	n, e := network(ac.Network)
	if e != nil {
		return n, e
	}
	if ac.Profile != Profile || ac.Family != "hypercore" || ac.Index >= 1<<31 || ac.ID != fmt.Sprintf("hypercore:%d:%s", ac.Index, ac.Network) || len(ac.Parameters) != 0 {
		return n, errors.New("invalid Core recovery descriptor")
	}
	if e = a.ValidateAddress(n.ID, ac.Address); e != nil {
		return n, e
	}
	raw, e := hex.DecodeString(strings.TrimPrefix(ac.PublicKey, "0x"))
	if e != nil || len(raw) != 65 {
		return n, errors.New("invalid owner public key")
	}
	pub, e := crypto.UnmarshalPubkey(raw)
	if e != nil || crypto.PubkeyToAddress(*pub).Hex() != ac.Address {
		return n, errors.New("Core owner/address mismatch")
	}
	return n, nil
}

func (a *Adapter) request(ctx context.Context, n model.Network, path string, body any, out any) error {
	base := n.RPC
	key := n.ID
	if path == "/explorer" {
		key += ":explorer"
		if n.Testnet {
			base = "https://rpc.hyperliquid-testnet.xyz"
		} else {
			base = "https://rpc.hyperliquid.xyz"
		}
	}
	if ep, ok := a.endpoints[key]; ok {
		base = ep
	}
	if e := evm.ValidateEndpoint(base); e != nil {
		return e
	}
	raw, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if e != nil {
		return errors.New("Core request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	if a.HTTPClient == nil {
		return errors.New("Core client unavailable")
	}
	res, e := a.HTTPClient.Do(req)
	if e != nil {
		return errors.New("Core API unavailable; submission may be ambiguous")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Core HTTP status %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return errors.New("Core response read failed")
	}
	var checked any
	if e = evm.StrictJSON(b, &checked); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e = d.Decode(out); e != nil {
		return errors.New("invalid Core response schema")
	}
	return nil
}

type meta struct {
	Tokens []struct {
		Name        string  `json:"name"`
		WeiDecimals uint8   `json:"weiDecimals"`
		Index       *uint32 `json:"index"`
		TokenID     string  `json:"tokenId"`
		IsCanonical bool    `json:"isCanonical"`
	} `json:"tokens"`
}

// assetByID never accepts a symbol, arbitrary token ID or another ledger's asset.
func assetByID(n model.Network, id string) (model.Asset, error) {
	for _, asset := range n.Assets {
		if asset.ID == id {
			return asset, nil
		}
	}
	return model.Asset{}, errors.New("unsupported Core spot asset")
}
func registryIndex(n model.Network, asset model.Asset) (uint32, error) {
	pinned, err := assetByID(n, asset.ID)
	if err != nil || pinned != asset {
		return 0, errors.New("Core asset identity mismatch")
	}
	if asset.Contract == mainUSDCToken && n.ID == "hypercore-mainnet" {
		return 0, nil
	}
	if asset.Contract == mainToken && n.ID == "hypercore-mainnet" {
		return 150, nil
	}
	if asset.Contract == testToken && n.ID == "hypercore-testnet" {
		return 1105, nil
	}
	return 0, errors.New("unsupported Core token registry")
}
func metadataIndex(n model.Network, asset model.Asset, m meta) (uint32, error) {
	idx, err := registryIndex(n, asset)
	if err != nil {
		return 0, err
	}
	want := strings.SplitN(asset.Contract, ":", 2)[1]
	found := 0
	for _, token := range m.Tokens {
		// A conflicting index or ticker is ambiguous. History may use the ticker
		// only after its uniqueness against the pinned token has been established.
		if token.TokenID == want || token.Index != nil && *token.Index == idx || token.Name == asset.Symbol {
			if token.TokenID != want || token.Name != asset.Symbol || token.WeiDecimals != asset.Decimals || token.Index == nil || *token.Index != idx || asset.Contract == mainUSDCToken && !token.IsCanonical {
				return 0, errors.New("Core pinned token registry metadata mismatch")
			}
			found++
		}
	}
	if found != 1 {
		return 0, errors.New("Core ledger/token identity mismatch")
	}
	return idx, nil
}
func (a *Adapter) identityState(ctx context.Context, n model.Network) (meta, uint64, error) {
	var m meta
	if err := a.request(ctx, n, "/info", map[string]any{"type": "spotMeta"}, &m); err != nil {
		return m, 0, err
	}
	var status struct {
		Time uint64 `json:"time"`
	}
	if err := a.request(ctx, n, "/info", map[string]any{"type": "exchangeStatus"}, &status); err != nil {
		return m, 0, err
	}
	now := uint64(time.Now().UnixMilli())
	if status.Time+30_000 < now || status.Time > now+30_000 {
		return m, 0, errors.New("Core server time is stale or invalid")
	}
	return m, status.Time, nil
}
func (a *Adapter) assetIdentity(ctx context.Context, n model.Network, asset model.Asset) (uint32, uint64, error) {
	m, at, err := a.identityState(ctx, n)
	if err != nil {
		return 0, 0, err
	}
	index, err := metadataIndex(n, asset, m)
	return index, at, err
}

// identity retains the original HYPE-only check for historical callers/plans.
func (a *Adapter) identity(ctx context.Context, n model.Network) (uint32, uint64, error) {
	return a.assetIdentity(ctx, n, n.Assets[0])
}
func atomic(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 78 || (len(s) > 1 && s[0] == '0') {
		return nil, errors.New("invalid atomic Core spot integer")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, errors.New("invalid atomic Core spot integer")
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.BitLen() > 256 {
		return nil, errors.New("Core spot amount overflow")
	}
	return v, nil
}
func parseUnits(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 88 {
		return nil, errors.New("invalid decimal Core spot amount")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return nil, errors.New("invalid Core spot precision")
	}
	whole := parts[0]
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
		if len(frac) == 0 || len(frac) > 8 {
			return nil, errors.New("excess Core spot precision")
		}
	}
	if whole == "" || (len(whole) > 1 && whole[0] == '0') {
		return nil, errors.New("noncanonical Core spot amount")
	}
	digits := strings.TrimLeft(whole+frac+strings.Repeat("0", 8-len(frac)), "0")
	if digits == "" {
		digits = "0"
	}
	return atomic(digits)
}
func formatUnits(v *big.Int) string {
	s := v.String()
	if len(s) <= 8 {
		s = strings.Repeat("0", 9-len(s)) + s
	}
	result := strings.TrimRight(strings.TrimRight(s[:len(s)-8]+"."+s[len(s)-8:], "0"), ".")
	return result
}
func (a *Adapter) role(ctx context.Context, n model.Network, addr string) (string, error) {
	var r struct {
		Role string `json:"role"`
	}
	if e := a.request(ctx, n, "/info", map[string]any{"type": "userRole", "user": addr}, &r); e != nil {
		return "", e
	}
	switch r.Role {
	case "missing", "user", "agent", "vault", "subAccount":
		return r.Role, nil
	}
	return "", errors.New("unknown Core account role")
}

type spotState struct {
	Balances []struct {
		Coin  string  `json:"coin"`
		Token *uint32 `json:"token"`
		Total string  `json:"total"`
		Hold  string  `json:"hold"`
	} `json:"balances"`
}

func (a *Adapter) funds(ctx context.Context, n model.Network, addr string, index uint32) (*big.Int, *big.Int, error) {
	var state spotState
	if e := a.request(ctx, n, "/info", map[string]any{"type": "spotClearinghouseState", "user": addr}, &state); e != nil {
		return nil, nil, e
	}
	return parseFunds(n, state, index)
}
func parseFunds(n model.Network, state spotState, index uint32) (*big.Int, *big.Int, error) {
	var selected model.Asset
	for _, asset := range n.Assets {
		known, err := registryIndex(n, asset)
		if err == nil && known == index {
			selected = asset
		}
	}
	if selected.ID == "" {
		return nil, nil, errors.New("unsupported Core token index")
	}
	if state.Balances == nil {
		return nil, nil, errors.New("Core balance state is missing")
	}
	total := new(big.Int)
	available := new(big.Int)
	found := false
	for _, b := range state.Balances {
		if b.Coin == selected.Symbol && (b.Token == nil || *b.Token != index) {
			return nil, nil, errors.New("Core selected balance lacks its exact token index")
		}
		if b.Token != nil && *b.Token == index {
			if found || b.Coin != selected.Symbol {
				return nil, nil, errors.New("ambiguous Core token balance")
			}
			found = true
			t, e := parseUnits(b.Total)
			if e != nil {
				return nil, nil, e
			}
			h, e := parseUnits(b.Hold)
			if e != nil {
				return nil, nil, e
			}
			if h.Cmp(t) > 0 {
				return nil, nil, errors.New("invalid Core held balance")
			}
			total.Set(t)
			available.Sub(t, h)
		}
	}
	return total, available, nil
}

// SpendableBalance is used only by maximum-amount planning. Displayed holdings
// remain total balances; ordinary Prepare independently rechecks current holds.
func (a *Adapter) SpendableBalance(ctx context.Context, ac model.Account, assetID string) (string, error) {
	n, err := a.validateAccount(ac)
	if err != nil {
		return "", err
	}
	asset, err := assetByID(n, assetID)
	if err != nil {
		return "", err
	}
	index, _, err := a.assetIdentity(ctx, n, asset)
	if err != nil {
		return "", err
	}
	_, available, err := a.funds(ctx, n, ac.Address, index)
	if err != nil {
		return "", err
	}
	return available.String(), nil
}

func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	n, e := a.validateAccount(ac)
	if e != nil {
		return nil, e
	}
	m, at, e := a.identityState(ctx, n)
	if e != nil {
		return nil, e
	}
	var state spotState
	if err := a.request(ctx, n, "/info", map[string]any{"type": "spotClearinghouseState", "user": ac.Address}, &state); err != nil {
		return nil, err
	}
	out := make([]model.Balance, 0, len(n.Assets))
	var failures []error
	valid := 0
	for _, asset := range n.Assets {
		index, err := metadataIndex(n, asset, m)
		var total *big.Int
		if err == nil {
			total, _, err = parseFunds(n, state, index)
		}
		if err != nil {
			failures = append(failures, err)
			out = append(out, model.Balance{Asset: asset.ID, State: "unavailable"})
			continue
		}
		valid++
		out = append(out, model.Balance{Asset: asset.ID, Atomic: total.String(), State: "provider-observed", At: strconv.FormatUint(at, 10)})
	}
	if valid == 0 {
		return nil, errors.Join(failures...)
	}
	return out, nil
}

// Action is the only action schema accepted by this adapter, never arbitrary typed data.
type action struct {
	Type             string `json:"type"`
	SignatureChainID string `json:"signatureChainId"`
	HyperliquidChain string `json:"hyperliquidChain"`
	Destination      string `json:"destination"`
	Token            string `json:"token"`
	Amount           string `json:"amount"`
	Time             uint64 `json:"time"`
}
type planPayload struct {
	Action            action `json:"action"`
	RecipientRole     string `json:"recipientRole"`
	SourceRole        string `json:"sourceRole"`
	SourceAbstraction string `json:"sourceAbstraction"`
	StateTime         uint64 `json:"stateTime"`
}

func planID(p model.Plan) string {
	p.ID = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return "hypercore:" + hex.EncodeToString(sum[:])
}
func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	fail := func(e error) (model.Plan, error) { return model.Plan{}, e }
	n, e := a.validateAccount(ac)
	if e != nil {
		return fail(e)
	}
	asset, assetErr := assetByID(n, t.Asset)
	if t.AccountID != ac.ID || assetErr != nil || t.Memo != "" {
		return fail(errors.New("unsupported Core asset, account or memo"))
	}
	if e = a.ValidateAddress(n.ID, t.Recipient); e != nil {
		return fail(e)
	}
	recipient := strings.ToLower(common.HexToAddress(t.Recipient).Hex())
	value, e := atomic(t.Amount)
	if e != nil || value.Sign() == 0 {
		return fail(errors.New("Core amount must be a positive eight-decimal atomic integer"))
	}
	index, at, e := a.assetIdentity(ctx, n, asset)
	if e != nil {
		return fail(e)
	}
	sourceRole, e := a.role(ctx, n, ac.Address)
	if e != nil {
		return fail(e)
	}
	if sourceRole != "user" {
		return fail(errors.New("Core source must be an activated owner account; agent/vault/subaccount keys are unsupported"))
	}
	recipientRole, e := a.role(ctx, n, recipient)
	if e != nil {
		return fail(e)
	}
	if recipientRole == "missing" {
		return fail(errors.New("fresh Core recipient requires a one-quote-token activation fee; the documented HYPE action does not identify fee-token selection or its debit, so activate the recipient through a verified quote-token funding flow before retrying"))
	}
	if recipientRole != "user" {
		return fail(errors.New("Core recipient must be an ordinary owner account"))
	}
	var abstraction string
	if e = a.request(ctx, n, "/info", map[string]any{"type": "userAbstraction", "user": ac.Address}, &abstraction); e != nil {
		return fail(e)
	}
	if abstraction != "default" {
		return fail(errors.New("Core unified/portfolio collateral spendability is unsupported"))
	}
	_, available, e := a.funds(ctx, n, ac.Address, index)
	if e != nil {
		return fail(e)
	}
	if available.Cmp(value) < 0 {
		return fail(fmt.Errorf("insufficient available Core %s", asset.Symbol))
	}
	a.mu.Lock()
	nonce := at
	if nonce <= a.lastNonce {
		nonce = a.lastNonce + 1
	}
	a.lastNonce = nonce
	a.mu.Unlock()
	if nonce > at+10_000 {
		return fail(errors.New("Core nonce scheduling limit"))
	}
	env := "Mainnet"
	if n.Testnet {
		env = "Testnet"
	}
	x := planPayload{Action: action{"spotSend", "0x66eee", env, recipient, asset.Contract, formatUnits(value), nonce}, RecipientRole: recipientRole, SourceRole: sourceRole, SourceAbstraction: abstraction, StateTime: at}
	raw, _ := json.Marshal(x)
	p := model.Plan{Account: ac, Asset: asset.ID, Recipient: recipient, Amount: value.String(), MaxFee: "0", FeeAsset: asset.ID, Nonce: strconv.FormatUint(nonce, 10), Expires: time.Now().Add(60 * time.Second).Unix(), Payload: raw}
	p.ID = planID(p)
	if e = a.ValidatePlan(p); e != nil {
		return fail(e)
	}
	return p, nil
}
func (a *Adapter) ValidatePlan(p model.Plan) error {
	n, e := a.validateAccount(p.Account)
	if e != nil {
		return e
	}
	if e = a.ValidateAddress(n.ID, p.Recipient); e != nil {
		return e
	}
	asset, assetErr := assetByID(n, p.Asset)
	if p.Recipient != strings.ToLower(p.Recipient) || assetErr != nil || p.MaxFee != "0" || p.FeeAsset != p.Asset || p.Memo != "" || p.ID != planID(p) || p.Expires <= 0 {
		return errors.New("Core plan identity/effects mismatch")
	}
	value, e := atomic(p.Amount)
	if e != nil || value.Sign() == 0 {
		return errors.New("invalid Core amount")
	}
	nonce, e := strconv.ParseUint(p.Nonce, 10, 64)
	if e != nil || strconv.FormatUint(nonce, 10) != p.Nonce {
		return errors.New("invalid Core nonce")
	}
	var x planPayload
	if e = evm.StrictJSON(p.Payload, &x); e != nil {
		return e
	}
	env := "Mainnet"
	if n.Testnet {
		env = "Testnet"
	}
	want := action{"spotSend", "0x66eee", env, p.Recipient, asset.Contract, formatUnits(value), nonce}
	if x.Action != want || x.SourceRole != "user" || x.RecipientRole != "user" || x.SourceAbstraction != "default" || x.StateTime == 0 || nonce < x.StateTime || nonce > x.StateTime+10_000 {
		return errors.New("Core action/domain/prerequisite mismatch")
	}
	return nil
}
func typedHash(x action) ([]byte, error) {
	chain := ethmath.NewHexOrDecimal256(421614)
	td := apitypes.TypedData{Types: apitypes.Types{"EIP712Domain": {{Name: "name", Type: "string"}, {Name: "version", Type: "string"}, {Name: "chainId", Type: "uint256"}, {Name: "verifyingContract", Type: "address"}}, "HyperliquidTransaction:SpotSend": {{Name: "hyperliquidChain", Type: "string"}, {Name: "destination", Type: "string"}, {Name: "token", Type: "string"}, {Name: "amount", Type: "string"}, {Name: "time", Type: "uint64"}}}, PrimaryType: "HyperliquidTransaction:SpotSend", Domain: apitypes.TypedDataDomain{Name: "HyperliquidSignTransaction", Version: "1", ChainId: chain, VerifyingContract: "0x0000000000000000000000000000000000000000"}, Message: apitypes.TypedDataMessage{"hyperliquidChain": x.HyperliquidChain, "destination": x.Destination, "token": x.Token, "amount": x.Amount, "time": strconv.FormatUint(x.Time, 10)}}
	h, _, e := apitypes.TypedDataAndHash(td)
	if e != nil {
		return nil, errors.New("Core typed action encoding failed")
	}
	return h, nil
}

type signature struct {
	R string `json:"r"`
	S string `json:"s"`
	V uint8  `json:"v"`
}
type envelope struct {
	Action    action    `json:"action"`
	Nonce     uint64    `json:"nonce"`
	Signature signature `json:"signature"`
}

func (a *Adapter) Sign(seed []byte, p model.Plan) (model.Signed, error) {
	if e := a.ValidatePlan(p); e != nil {
		return model.Signed{}, e
	}
	if p.Expires < time.Now().Unix() || p.Expires > time.Now().Unix()+120 {
		return model.Signed{}, errors.New("expired Core approval")
	}
	ac, e := a.Derive(seed, p.Account.Index, p.Account.Network)
	if e != nil {
		return model.Signed{}, e
	}
	if ac.Address != p.Account.Address || ac.PublicKey != p.Account.PublicKey {
		return model.Signed{}, errors.New("Core signing owner mismatch")
	}
	key, e := deriveKey(seed, p.Account.Index)
	if e != nil {
		return model.Signed{}, e
	}
	defer destroyKey(key)
	var x planPayload
	_ = evm.StrictJSON(p.Payload, &x)
	now := uint64(time.Now().UnixMilli())
	if x.Action.Time+30_000 < now || x.Action.Time > now+30_000 {
		return model.Signed{}, errors.New("Core nonce timestamp outside signing window")
	}
	h, e := typedHash(x.Action)
	if e != nil {
		return model.Signed{}, e
	}
	sig, e := crypto.Sign(h, key)
	if e != nil {
		return model.Signed{}, errors.New("Core signature failed")
	}
	defer wipe(sig)
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	raw, _ := json.Marshal(envelope{x.Action, x.Action.Time, signature{"0x" + r.Text(16), "0x" + s.Text(16), sig[64] + 27}})
	signed := model.Signed{Identity: "hypercore-eip712:0x" + hex.EncodeToString(h), Bytes: raw}
	if _, e = a.verifySigned(p, signed); e != nil {
		return model.Signed{}, e
	}
	return signed, nil
}
func (a *Adapter) verifySigned(p model.Plan, s model.Signed) (envelope, error) {
	var env envelope
	if e := a.ValidatePlan(p); e != nil {
		return env, e
	}
	if len(s.Bytes) > 4096 || len(s.Auxiliary) != 0 {
		return env, errors.New("invalid Core signed envelope")
	}
	if e := evm.StrictJSON(s.Bytes, &env); e != nil {
		return env, e
	}
	var x planPayload
	_ = evm.StrictJSON(p.Payload, &x)
	if env.Action != x.Action || env.Nonce != x.Action.Time {
		return env, errors.New("signed Core action mismatch")
	}
	h, e := typedHash(x.Action)
	if e != nil {
		return env, e
	}
	if s.Identity != "hypercore-eip712:0x"+hex.EncodeToString(h) {
		return env, errors.New("Core signing identity mismatch")
	}
	decode := func(s string) (*big.Int, error) {
		if !strings.HasPrefix(s, "0x") || len(s) > 66 || len(s) < 3 {
			return nil, errors.New("invalid Core signature scalar")
		}
		v, ok := new(big.Int).SetString(s[2:], 16)
		if !ok || v.Sign() <= 0 || "0x"+v.Text(16) != s {
			return nil, errors.New("invalid Core signature scalar")
		}
		return v, nil
	}
	r, e := decode(env.Signature.R)
	if e != nil {
		return env, e
	}
	ss, e := decode(env.Signature.S)
	if e != nil {
		return env, e
	}
	if env.Signature.V < 27 || env.Signature.V > 28 || !crypto.ValidateSignatureValues(env.Signature.V-27, r, ss, true) {
		return env, errors.New("invalid Core ECDSA signature")
	}
	sig := append(word(r), word(ss)...)
	sig = append(sig, env.Signature.V-27)
	pub, e := crypto.SigToPub(h, sig)
	if e != nil || crypto.PubkeyToAddress(*pub).Hex() != p.Account.Address {
		return env, errors.New("Core signature owner mismatch")
	}
	canon, _ := json.Marshal(env)
	if !bytes.Equal(canon, s.Bytes) {
		return env, errors.New("noncanonical Core signed action")
	}
	return env, nil
}
func word(x *big.Int) []byte { return x.FillBytes(make([]byte, 32)) }
func (a *Adapter) Submit(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	env, e := a.verifySigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	r := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	n, _ := network(p.Account.Network)
	asset, _ := assetByID(n, p.Asset)
	if _, _, e = a.assetIdentity(ctx, n, asset); e != nil {
		return r, e
	}
	if p.Expires < time.Now().Unix() {
		return r, errors.New("Core approval expired; reconcile original action")
	}
	role, e := a.role(ctx, n, p.Recipient)
	if e != nil {
		return r, e
	}
	if role != "user" {
		return r, errors.New("Core recipient prerequisite changed")
	}
	var response struct {
		Status   string          `json:"status"`
		Response json.RawMessage `json:"response"`
	}
	if e = a.request(ctx, n, "/exchange", env, &response); e != nil {
		r.Detail = "submission ambiguous; reconcile exact owner action/nonce before any replacement"
		return r, e
	}
	if response.Status != "ok" {
		r.State = "UNKNOWN"
		r.Detail = "exchange returned an error; reconcile original action before releasing its nonce"
		return r, errors.New("Core action rejected")
	}
	var status struct {
		Type string `json:"type"`
	}
	if e = evm.StrictJSON(response.Response, &status); e != nil || status.Type != "default" {
		return r, errors.New("unrecognized Core execution acknowledgement")
	}
	r.State = "SUBMITTED"
	r.Detail = "Core acknowledged action; exact explorer and both ledger effects remain to be reconciled"
	return r, nil
}

type explorerTx struct {
	Time   uint64          `json:"time"`
	User   string          `json:"user"`
	Action json.RawMessage `json:"action"`
	Block  uint64          `json:"block"`
	Hash   string          `json:"hash"`
	Error  json.RawMessage `json:"error"`
}
type ledger struct {
	Time  uint64 `json:"time"`
	Hash  string `json:"hash"`
	Delta struct {
		Type           string          `json:"type"`
		Token          string          `json:"token"`
		Amount         json.RawMessage `json:"amount"`
		User           string          `json:"user"`
		Destination    string          `json:"destination"`
		Fee            json.RawMessage `json:"fee"`
		NativeTokenFee json.RawMessage `json:"nativeTokenFee"`
	} `json:"delta"`
}

func sameAction(raw json.RawMessage, want action) bool {
	var x action
	if evm.StrictJSON(raw, &x) != nil {
		return false
	}
	return x == want
}
func decimalJSON(raw json.RawMessage) (*big.Int, error) {
	var s string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return nil, errors.New("invalid ledger quantity")
		}
	} else {
		s = string(raw)
	}
	return parseUnits(s)
}
func validHash(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	_, e := hex.DecodeString(s[2:])
	return e == nil
}
func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	env, e := a.verifySigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	r := model.Receipt{Identity: s.Identity, State: "UNKNOWN"}
	n, _ := network(p.Account.Network)
	asset, _ := assetByID(n, p.Asset)
	_, at, e := a.assetIdentity(ctx, n, asset)
	if e != nil {
		return r, e
	}
	var history struct {
		Type string       `json:"type"`
		Txs  []explorerTx `json:"txs"`
	}
	if e = a.request(ctx, n, "/explorer", map[string]any{"type": "userDetails", "user": strings.ToLower(p.Account.Address)}, &history); e != nil {
		return r, e
	}
	if history.Type != "userDetails" || len(history.Txs) > 2000 {
		return r, errors.New("invalid Core explorer history")
	}
	matches := []explorerTx{}
	for _, tx := range history.Txs {
		if strings.EqualFold(tx.User, p.Account.Address) && sameAction(tx.Action, env.Action) {
			matches = append(matches, tx)
		}
	}
	if len(matches) != 1 {
		r.Detail = "exact signed action not uniquely observed; reservation remains held"
		return r, nil
	}
	observed := matches[0]
	if !validHash(observed.Hash) || observed.Block == 0 || len(observed.Error) == 0 {
		return r, errors.New("incomplete Core inclusion evidence")
	}
	var detail struct {
		Type string     `json:"type"`
		Tx   explorerTx `json:"tx"`
	}
	if e = a.request(ctx, n, "/explorer", map[string]any{"type": "txDetails", "hash": observed.Hash}, &detail); e != nil {
		return r, e
	}
	tx := detail.Tx
	if detail.Type != "txDetails" || tx.Hash != observed.Hash || tx.Block != observed.Block || tx.Time != observed.Time || !strings.EqualFold(tx.User, p.Account.Address) || !sameAction(tx.Action, env.Action) || !bytes.Equal(tx.Error, observed.Error) {
		return r, errors.New("Core explorer evidence disagreement")
	}
	r.Block = strconv.FormatUint(tx.Block, 10)
	if string(tx.Error) != "null" {
		r.Detail = "exact action has an execution error; ledger effects require inspection"
		return r, nil
	}
	start := env.Nonce
	if start > 300_000 {
		start -= 300_000
	}
	if tx.Time < start || tx.Time > at || tx.Time == 0 {
		return r, errors.New("Core inclusion time outside verified reconciliation window")
	}
	for _, address := range []string{p.Account.Address, p.Recipient} {
		updates, scanErr := a.ledgerWindow(ctx, n, address, start, at)
		if scanErr != nil {
			r.Detail = "ledger pagination incomplete; original action reservation remains held"
			return r, scanErr
		}
		found := 0
		for _, u := range updates {
			d := u.Delta
			if u.Hash != tx.Hash {
				continue
			}
			if d.Type == "spotTransfer" && (d.Token == env.Action.Token || d.Token == asset.Symbol) && strings.EqualFold(d.User, p.Account.Address) && strings.EqualFold(d.Destination, p.Recipient) {
				v, e := decimalJSON(d.Amount)
				if e != nil {
					return r, e
				}
				fee, e := decimalJSON(d.Fee)
				if e != nil {
					return r, e
				}
				native := new(big.Int)
				if len(d.NativeTokenFee) > 0 {
					native, e = decimalJSON(d.NativeTokenFee)
					if e != nil {
						return r, e
					}
				}
				if v.String() != p.Amount || fee.Sign() != 0 || native.Sign() != 0 {
					r.Detail = "same-hash ledger effects differ from the approved zero-fee spot transfer"
					return r, nil
				}
				found++
			} else {
				r.Detail = "unrecognized same-hash ledger effect; reservation remains held"
				return r, nil
			}
		}
		if found != 1 {
			r.Detail = "both exact sender and recipient transfer effects are not yet established"
			return r, nil
		}
	}
	r.State = "CONFIRMED"
	r.Detail = "exact owner action/nonce included and same-hash zero-fee spot transfer observed in sender and recipient ledgers"
	return r, nil
}

var _ model.Adapter = (*Adapter)(nil)
