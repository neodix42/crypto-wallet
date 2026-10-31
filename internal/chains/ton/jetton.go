package ton

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// Tether's canonical mainnet USDT master. The code/data layouts are pinned to
// ton-blockchain/stablecoin-contract 5a3b500267b0bdfc6505a08e5ac661c805cab8b0.
// The pinned master getter hard-codes six decimals. Symbols/remote metadata do
// not establish token identity. Issuer upgrades fail closed pending review.
const USDTMaster = "0:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe"
const USDTMasterCodeHash = "18d5b6e780ff0bb451254c2c760d09d6e485638cd1407abb97078752c3c1c9ee"
const USDTWalletCodeHash = "89468f02c78e570802e39979c8516fc38df07ea76a48357e0536f2ba7b3ee37b"
const USDTWalletLibraryHash = "8f452d7a4dfd74066b682365177259ed05734435be76b5fd4bd5d8af2b7c3d68"
const usdtWalletLibraryBOC = "te6cckEBAQEAIwAIQgKPRS16Tf10BmtoI2UXclntBXNENb52tf1L1divK3w9aCBrv3Y="
const jettonAttached = uint64(100_000_000) // 0.1 GRAM, unused excess returns to sender.
const jettonForward = uint64(1)            // One nanogram requests the standard notification.

func usdtAsset() model.Asset {
	return model.Asset{ID: "ton-mainnet:USDT", Symbol: "USDT", Decimals: 6, Contract: USDTMaster}
}

type JettonPayload struct {
	Master          string `json:"master"`
	Wallet          string `json:"wallet"`
	RecipientWallet string `json:"recipientWallet"`
	Balance         string `json:"balance"`
	QueryID         string `json:"queryId"`
	Attached        string `json:"attached"`
	Forward         string `json:"forward"`
	CodeHash        string `json:"codeHash"`
}

func IsJettonPlan(p model.Plan) bool {
	x, e := payload(p)
	return p.Account.Family == "ton" && e == nil && x.Kind == "jetton_transfer"
}

// JettonEffects must follow ValidatePlan, like DeploymentEffects.
func JettonEffects(p model.Plan) string {
	x, e := payload(p)
	if e != nil || x.Jetton == nil || !IsJettonPlan(p) {
		return "Unsupported TON jetton transfer"
	}
	return fmt.Sprintf("Transfer exactly %s atomic USDT units (6 decimals) to owner %s through canonical master %s and owned jetton wallet %s. Allowlisted operation transfer (0x0f8a7ea5), query ID %s; recipient jetton wallet %s. Attach 0.1 GRAM for jetton execution, including a 0.000000001 GRAM notification; unused excess returns to %s. On a first send, this account's exact recorded V5R1 StateInit activates the owned wallet; the canonical token contract initializes a missing recipient jetton wallet. Native fees are paid in GRAM; the displayed reserve is an estimate, not a protocol-enforced ceiling. Tether controls freezing and upgrades of this token.", p.Amount, p.Recipient, USDTMaster, x.Jetton.Wallet, x.Jetton.QueryID, x.Jetton.RecipientWallet, p.Account.Address)
}
func jettonCode() (*cell.Cell, error) {
	c, e := decodeBOC(usdtWalletLibraryBOC)
	if e != nil || hex.EncodeToString(c.Hash()) != USDTWalletCodeHash {
		return nil, errors.New("pinned USDT wallet library mismatch")
	}
	return c, nil
}
func jettonAddress(owner *address.Address) (*address.Address, error) {
	code, e := jettonCode()
	if e != nil {
		return nil, e
	}
	master, _ := address.ParseRawAddr(USDTMaster)
	data := cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(0).MustStoreAddr(owner).MustStoreAddr(master).EndCell()
	state, e := tlb.ToCell(&tlb.StateInit{Code: code, Data: data})
	if e != nil {
		return nil, e
	}
	return address.NewAddress(0, 0, state.Hash()), nil
}

// Verify the exact reviewed minter code and its stored wallet library, without
// trusting provider token symbols, arbitrary getters or off-chain metadata.
func (a *Adapter) verifyUSDT(ctx context.Context, network string) error {
	if network != "ton-mainnet" {
		return errors.New("canonical TON USDT is mainnet only")
	}
	st, e := a.state(ctx, network, USDTMaster)
	if e != nil {
		return e
	}
	code, e := decodeBOC(st.Code)
	if e != nil || st.State != "active" || hex.EncodeToString(code.Hash()) != USDTMasterCodeHash {
		return errors.New("USDT master code changed or unavailable; review required")
	}
	data, e := decodeBOC(st.Data)
	if e != nil {
		return e
	}
	s, e := data.BeginParse()
	if e != nil {
		return e
	}
	if _, e = s.LoadBigCoins(); e != nil {
		return errors.New("invalid USDT master supply")
	}
	admin, e := s.LoadAddr()
	if e != nil || !historyPeer(admin) {
		return errors.New("invalid USDT master administrator")
	}
	next, e := s.LoadAddr()
	if e != nil || next == nil || (!next.IsAddrNone() && !historyPeer(next)) {
		return errors.New("invalid USDT next administrator")
	}
	wallet, e := s.LoadRefCell()
	if e != nil || hex.EncodeToString(wallet.Hash()) != USDTWalletCodeHash {
		return errors.New("USDT master wallet code changed; review required")
	}
	metadata, e := s.LoadRefCell()
	if e != nil || metadata.BitsSize() > 4096 || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return errors.New("invalid USDT master storage layout")
	}
	return nil
}

type jettonState struct {
	Address string
	Balance string
	Status  uint64
	Active  bool
}

func (a *Adapter) jettonState(ctx context.Context, network, owner string) (jettonState, error) {
	own, e := parseAddress(network, owner)
	if e != nil {
		return jettonState{}, e
	}
	wallet, e := jettonAddress(own)
	if e != nil {
		return jettonState{}, e
	}
	out := jettonState{Address: wallet.StringRaw(), Balance: "0"}
	st, e := a.state(ctx, network, out.Address)
	if e != nil {
		return out, e
	}
	if st.State == "uninitialized" {
		return out, nil
	}
	if st.State != "active" {
		return out, errors.New("USDT jetton wallet frozen")
	}
	code, e := decodeBOC(st.Code)
	if e != nil || hex.EncodeToString(code.Hash()) != USDTWalletCodeHash {
		return out, errors.New("USDT jetton wallet code mismatch")
	}
	data, e := decodeBOC(st.Data)
	if e != nil {
		return out, e
	}
	s, e := data.BeginParse()
	if e != nil {
		return out, e
	}
	status, e := s.LoadUInt(4)
	if e != nil || status > 3 {
		return out, errors.New("USDT status unsupported")
	}
	balance, e := s.LoadBigCoins()
	if e != nil || balance.Sign() < 0 || balance.BitLen() > 120 {
		return out, errors.New("invalid USDT balance")
	}
	recordedOwner, e := s.LoadAddr()
	if e != nil || recordedOwner == nil || !recordedOwner.Equals(own) {
		return out, errors.New("USDT wallet owner mismatch")
	}
	master, e := s.LoadAddr()
	want, _ := address.ParseRawAddr(USDTMaster)
	if e != nil || master == nil || !master.Equals(want) || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return out, errors.New("USDT wallet master/storage mismatch")
	}
	out.Balance, out.Status, out.Active = balance.String(), status, true
	return out, nil
}
func jettonBody(p model.Plan, x Payload) (*cell.Cell, error) {
	if x.Jetton == nil {
		return nil, errors.New("missing jetton payload")
	}
	j := x.Jetton
	amount, e := chainutil.Big(p.Amount, 120)
	if e != nil || amount.Sign() == 0 {
		return nil, errors.New("invalid USDT amount")
	}
	query, e := chainutil.Uint(j.QueryID, 64)
	if e != nil {
		return nil, e
	}
	dest, e := parseAddress(p.Account.Network, p.Recipient)
	if e != nil {
		return nil, e
	}
	own, e := parseAddress(p.Account.Network, p.Account.Address)
	if e != nil {
		return nil, e
	}
	forward := cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice([]byte(p.Memo), uint(len(p.Memo)*8)).EndCell()
	return cell.BeginCell().MustStoreUInt(0x0f8a7ea5, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(dest).MustStoreAddr(own).MustStoreBoolBit(false).MustStoreCoins(jettonForward).MustStoreBoolBit(true).MustStoreRef(forward).EndCell(), nil
}
func validateJetton(p model.Plan, x Payload) error {
	j := x.Jetton
	if j == nil || x.Kind != "jetton_transfer" || p.Account.Network != "ton-mainnet" || p.Asset != usdtAsset().ID || p.FeeAsset != "ton-mainnet:TON" || !x.Bounce || j.Master != USDTMaster || j.CodeHash != USDTWalletCodeHash || j.Attached != strconv.FormatUint(jettonAttached, 10) || j.Forward != "1" {
		return errors.New("USDT identity or operation mismatch")
	}
	own, _ := parseAddress(p.Account.Network, p.Account.Address)
	dst, e := parseAddress(p.Account.Network, p.Recipient)
	if e != nil {
		return e
	}
	source, e := jettonAddress(own)
	if e != nil {
		return e
	}
	dest, e := jettonAddress(dst)
	if e != nil {
		return e
	}
	if j.Wallet != source.StringRaw() || j.RecipientWallet != dest.StringRaw() {
		return errors.New("USDT wallet derivation mismatch")
	}
	balance, e := chainutil.Big(j.Balance, 120)
	amount, ae := chainutil.Big(p.Amount, 120)
	if e != nil || ae != nil || amount.Sign() == 0 || balance.Cmp(amount) < 0 {
		return errors.New("insufficient or invalid USDT")
	}
	if _, e = chainutil.Uint(j.QueryID, 64); e != nil {
		return e
	}
	return nil
}
func (a *Adapter) prepareJetton(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	if t.AccountID != ac.ID || t.Asset != usdtAsset().ID || ac.Network != "ton-mainnet" {
		return model.Plan{}, errors.New("unsupported TON jetton")
	}
	if _, _, e := descriptor(ac); e != nil {
		return model.Plan{}, e
	}
	if e := a.ValidateAddress(ac.Network, t.Recipient); e != nil {
		return model.Plan{}, e
	}
	if len(t.Memo) > 120 {
		return model.Plan{}, errors.New("TON comment exceeds 120 bytes")
	}
	checkpoint, e := a.verifyNetwork(ctx, ac.Network)
	if e != nil {
		return model.Plan{}, e
	}
	auth, e := a.authority(ctx, ac)
	if e != nil {
		return model.Plan{}, e
	}
	if e = a.verifyUSDT(ctx, ac.Network); e != nil {
		return model.Plan{}, e
	}
	source, e := a.jettonState(ctx, ac.Network, ac.Address)
	if e != nil {
		return model.Plan{}, e
	}
	dest, e := a.jettonState(ctx, ac.Network, t.Recipient)
	if e != nil {
		return model.Plan{}, e
	}
	if !source.Active || source.Status&1 != 0 || dest.Status&2 != 0 {
		return model.Plan{}, errors.New("USDT wallet absent or issuer-locked")
	}
	var randomness [8]byte
	if _, e = rand.Read(randomness[:]); e != nil {
		return model.Plan{}, errors.New("jetton query identity unavailable")
	}
	x := Payload{Kind: "jetton_transfer", Seqno: auth.Seqno, Deploy: auth.Deploy, Bounce: true, SourceBalance: auth.Balance, StorageReserve: "10000000", SourceDataHash: auth.DataHash, MasterSeqno: checkpoint, Jetton: &JettonPayload{Master: USDTMaster, Wallet: source.Address, RecipientWallet: dest.Address, Balance: source.Balance, QueryID: strconv.FormatUint(binary.BigEndian.Uint64(randomness[:]), 10), Attached: strconv.FormatUint(jettonAttached, 10), Forward: "1", CodeHash: USDTWalletCodeHash}}
	p := model.Plan{Account: ac, Asset: t.Asset, Recipient: t.Recipient, Amount: t.Amount, Memo: t.Memo, Nonce: strconv.FormatUint(uint64(auth.Seqno), 10), FeeAsset: ac.Network + ":TON", Expires: time.Now().Add(5 * time.Minute).Unix()}
	if e = validateJetton(p, x); e != nil {
		return model.Plan{}, e
	}
	unsigned, _, e := message(p, x)
	if e != nil {
		return model.Plan{}, e
	}
	body := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(make([]byte, 64), 512).EndCell()
	fee, e := a.estimate(ctx, p, x, body, true)
	if e != nil {
		return model.Plan{}, e
	}
	x.EstimatedFee = strconv.FormatUint(fee, 10)
	p.MaxFee = strconv.FormatUint(fee*2+1_000_000+jettonAttached, 10)
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if e = a.ValidatePlan(p); e != nil {
		return model.Plan{}, e
	}
	return p, nil
}
func (a *Adapter) recheckJetton(ctx context.Context, p model.Plan, x Payload) error {
	if e := a.verifyUSDT(ctx, p.Account.Network); e != nil {
		return e
	}
	source, e := a.jettonState(ctx, p.Account.Network, p.Account.Address)
	if e != nil {
		return e
	}
	dest, e := a.jettonState(ctx, p.Account.Network, p.Recipient)
	if e != nil {
		return e
	}
	amount, _ := chainutil.Big(p.Amount, 120)
	balance, _ := chainutil.Big(source.Balance, 120)
	if !source.Active || source.Status&1 != 0 || dest.Status&2 != 0 || balance.Cmp(amount) < 0 || source.Address != x.Jetton.Wallet || dest.Address != x.Jetton.RecipientWallet {
		return errors.New("USDT authority, balance or recipient changed")
	}
	return nil
}

func sameCell(a, b *cell.Cell) bool { return a != nil && b != nil && bytes.Equal(a.Hash(), b.Hash()) }
func nativeSpend(p model.Plan, x Payload) *big.Int {
	if x.Kind == "jetton_transfer" {
		return new(big.Int)
	}
	n, _ := chainutil.Big(p.Amount, 120)
	return n
}
