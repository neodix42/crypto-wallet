package ton

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

type accountState struct {
	Balance string `json:"balance"`
	State   string `json:"state"`
	Code    string `json:"code"`
	Data    string `json:"data"`
	Last    struct {
		LT   string `json:"lt"`
		Hash string `json:"hash"`
	} `json:"last_transaction_id"`
	SyncUTime int64 `json:"sync_utime"`
	Suspended bool  `json:"suspended"`
}
type authority struct {
	Seqno    uint32
	Deploy   bool
	Balance  string
	DataHash string
}

func (a *Adapter) verifyNetwork(ctx context.Context, id string) (uint32, error) {
	n, e := netInfo(id)
	if e != nil {
		return 0, e
	}
	var v struct {
		Init struct {
			Root      string `json:"root_hash"`
			File      string `json:"file_hash"`
			Workchain int32  `json:"workchain"`
		} `json:"init"`
		Last struct {
			Seqno uint32 `json:"seqno"`
		} `json:"last"`
	}
	if e = a.rpc(ctx, id, "getMasterchainInfo", map[string]any{}, &v); e != nil {
		return 0, e
	}
	if v.Init.Root != n.root || v.Init.File != n.file || v.Init.Workchain != -1 {
		return 0, errors.New("TON RPC zerostate/network mismatch")
	}
	if v.Last.Seqno == 0 {
		return 0, errors.New("TON provider has no current masterchain checkpoint")
	}
	return v.Last.Seqno, nil
}
func (a *Adapter) state(ctx context.Context, id, addr string) (accountState, error) {
	var st accountState
	if e := a.rpc(ctx, id, "getAddressInformation", map[string]any{"address": addr}, &st); e != nil {
		return st, e
	}
	if _, e := chainutil.Big(st.Balance, 120); e != nil {
		return st, errors.New("TON balance invalid")
	}
	if st.Suspended {
		return st, errors.New("TON account is suspended")
	}
	switch st.State {
	case "active", "uninitialized", "frozen":
	default:
		return st, errors.New("TON activation state unknown")
	}
	if st.SyncUTime <= 0 || st.SyncUTime < time.Now().Add(-2*time.Minute).Unix() || st.SyncUTime > time.Now().Add(time.Minute).Unix() {
		return st, errors.New("TON provider state is stale")
	}
	return st, nil
}
func (a *Adapter) authority(ctx context.Context, ac model.Account) (authority, error) {
	st, e := a.state(ctx, ac.Network, ac.Address)
	if e != nil {
		return authority{}, e
	}
	return a.authorityFromState(ac, st)
}
func (a *Adapter) authorityFromState(ac model.Account, st accountState) (authority, error) {
	meta, pub, e := descriptor(ac)
	if e != nil {
		return authority{}, e
	}
	auth := authority{Balance: st.Balance}
	if st.State == "uninitialized" {
		auth.Deploy = true
		return auth, nil
	}
	if st.State != "active" {
		return authority{}, errors.New("TON wallet is frozen or unavailable")
	}
	code, e := decodeBOC(st.Code)
	if e != nil || hex.EncodeToString(code.Hash()) != CodeHash {
		return authority{}, errors.New("TON wallet code differs from pinned V5R1")
	}
	data, e := decodeBOC(st.Data)
	if e != nil {
		return authority{}, e
	}
	s, e := data.BeginParse()
	if e != nil {
		return authority{}, e
	}
	allowed, e := s.LoadBoolBit()
	if e != nil || !allowed {
		return authority{}, errors.New("TON wallet signature authority disabled")
	}
	seq, e := s.LoadUInt(32)
	if e != nil {
		return authority{}, e
	}
	id, e := s.LoadUInt(32)
	if e != nil || uint32(id) != meta.WalletID {
		return authority{}, errors.New("TON wallet ID differs from recovery descriptor")
	}
	pk, e := s.LoadSlice(256)
	if e != nil || !bytes.Equal(pk, pub) {
		return authority{}, errors.New("TON wallet public-key authority mismatch")
	}
	hasExtensions, e := s.LoadBoolBit()
	if e != nil || hasExtensions || s.BitsLeft() != 0 || s.RefsNum() != 0 {
		return authority{}, errors.New("TON extension authority is unsupported; explicit recovery required")
	}
	auth.Seqno = uint32(seq)
	auth.DataHash = hex.EncodeToString(data.Hash())
	return auth, nil
}
func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	if _, _, e := descriptor(ac); e != nil {
		return nil, e
	}
	seq, e := a.verifyNetwork(ctx, ac.Network)
	if e != nil {
		return nil, e
	}
	st, e := a.state(ctx, ac.Network, ac.Address)
	if e != nil {
		return nil, e
	}
	return []model.Balance{{Asset: ac.Network + ":TON", Atomic: st.Balance, State: st.State + " (trusted RPC)", At: strconv.FormatUint(uint64(seq), 10)}}, nil
}
func (a *Adapter) estimate(ctx context.Context, p model.Plan, x Payload, body *cell.Cell, ignoreSignature bool) (uint64, error) {
	args := map[string]any{"address": p.Account.Address, "body": base64.StdEncoding.EncodeToString(body.ToBOC()), "ignore_chksig": ignoreSignature, "init_code": "", "init_data": ""}
	if x.Deploy {
		meta, pub, e := descriptor(p.Account)
		if e != nil {
			return 0, e
		}
		init, _, e := stateInitForWalletID(pub, meta.WalletID)
		if e != nil {
			return 0, e
		}
		args["init_code"] = base64.StdEncoding.EncodeToString(init.Code.ToBOC())
		args["init_data"] = base64.StdEncoding.EncodeToString(init.Data.ToBOC())
	}
	var fees struct {
		Source struct {
			In      json.Number `json:"in_fwd_fee"`
			Storage json.Number `json:"storage_fee"`
			Gas     json.Number `json:"gas_fee"`
			Forward json.Number `json:"fwd_fee"`
		} `json:"source_fees"`
	}
	if e := a.rpc(ctx, p.Account.Network, "estimateFee", args, &fees); e != nil {
		return 0, e
	}
	var total uint64
	for _, s := range []json.Number{fees.Source.In, fees.Source.Storage, fees.Source.Gas, fees.Source.Forward} {
		n, e := chainutil.Uint(s.String(), 64)
		if e != nil || n > 249_500_000 || total > 249_500_000-n {
			return 0, errors.New("TON estimated fee exceeds policy")
		}
		total += n
	}
	if total == 0 {
		return 0, errors.New("TON fee estimate unavailable")
	}
	return total, nil
}
func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	if t.AccountID != ac.ID || t.Asset != ac.Network+":TON" {
		return model.Plan{}, errors.New("unsupported TON transfer")
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
	seq, e := a.verifyNetwork(ctx, ac.Network)
	if e != nil {
		return model.Plan{}, e
	}
	auth, e := a.authority(ctx, ac)
	if e != nil {
		return model.Plan{}, e
	}
	dst, e := a.state(ctx, ac.Network, t.Recipient)
	if e != nil {
		return model.Plan{}, e
	}
	if dst.State == "frozen" {
		return model.Plan{}, errors.New("TON recipient is frozen")
	}
	x := Payload{Seqno: auth.Seqno, Deploy: auth.Deploy, Bounce: dst.State == "active", SourceBalance: auth.Balance, StorageReserve: "10000000", SourceDataHash: auth.DataHash, MasterSeqno: seq}
	p := model.Plan{Account: ac, Asset: t.Asset, Recipient: t.Recipient, Amount: t.Amount, Memo: t.Memo, Nonce: strconv.FormatUint(uint64(x.Seqno), 10), FeeAsset: t.Asset, Expires: time.Now().Add(5 * time.Minute).Unix()}
	unsigned, _, e := message(p, x)
	if e != nil {
		return model.Plan{}, e
	}
	dummyBody := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(make([]byte, 64), 512).EndCell()
	fee, e := a.estimate(ctx, p, x, dummyBody, true)
	if e != nil {
		return model.Plan{}, e
	}
	x.EstimatedFee = strconv.FormatUint(fee, 10)
	p.MaxFee = strconv.FormatUint(fee*2+1_000_000, 10)
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if e = a.ValidatePlan(p); e != nil {
		return model.Plan{}, e
	}
	return p, nil
}
func (a *Adapter) Submit(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	_, ext, e := a.validateSigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	if _, e = a.verifyNetwork(ctx, p.Account.Network); e != nil {
		return model.Receipt{}, e
	}
	x, _ := payload(p)
	auth, e := a.authority(ctx, p.Account)
	if e != nil {
		return model.Receipt{}, e
	}
	if auth.Seqno != x.Seqno || auth.Deploy != x.Deploy || auth.DataHash != x.SourceDataHash {
		return model.Receipt{}, errors.New("TON authority or seqno changed since approval")
	}
	if x.Kind != "deployment" {
		dst, e := a.state(ctx, p.Account.Network, p.Recipient)
		if e != nil {
			return model.Receipt{}, e
		}
		if (dst.State == "active") != x.Bounce || dst.State == "frozen" {
			return model.Receipt{}, errors.New("TON recipient activation changed; new approval required")
		}
	}
	fee, e := a.estimate(ctx, p, x, ext.Body, false)
	if e != nil {
		return model.Receipt{}, e
	}
	ceiling, _ := chainutil.Uint(p.MaxFee, 64)
	if fee > ceiling {
		return model.Receipt{}, errors.New("TON current fee exceeds approved ceiling")
	}
	balance, _ := chainutil.Big(auth.Balance, 120)
	amount, _ := chainutil.Big(p.Amount, 120)
	cost := new(big.Int).Add(amount, new(big.Int).SetUint64(ceiling+10_000_000))
	if balance.Cmp(cost) < 0 {
		return model.Receipt{}, errors.New("TON balance no longer covers payment and reserves")
	}
	var result json.RawMessage
	e = a.rpc(ctx, p.Account.Network, "sendBoc", map[string]any{"boc": base64.StdEncoding.EncodeToString(s.Bytes)}, &result)
	if e != nil {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "submission uncertain; reconcile journaled external message before retrying"}, e
	}
	detail := "external message submitted; source action and recipient credit await trace verification"
	if x.Kind == "deployment" {
		detail = "deployment external message submitted; matching source execution and deployed authority await verification"
	}
	return model.Receipt{Identity: s.Identity, State: "SUBMITTED", Detail: detail}, nil
}

func (a *Adapter) transactions(ctx context.Context, id, addr string) ([]*tlb.Transaction, error) {
	return a.transactionsAt(ctx, id, addr, nil)
}
func (a *Adapter) transactionsAt(ctx context.Context, id, addr string, position *transactionPosition) ([]*tlb.Transaction, error) {
	var entries []struct {
		Data string `json:"data"`
	}
	params := map[string]any{"address": addr, "limit": 100, "archival": true}
	if position != nil {
		params["lt"], params["hash"] = position.LT, position.Hash
	}
	if e := a.rpc(ctx, id, "getTransactions", params, &entries); e != nil {
		return nil, e
	}
	if len(entries) > 100 {
		return nil, errors.New("TON transaction response exceeds bound")
	}
	var out []*tlb.Transaction
	ad, e := parseAddress(id, addr)
	if e != nil {
		return nil, e
	}
	for _, v := range entries {
		c, e := decodeBOC(v.Data)
		if e != nil {
			return nil, e
		}
		s, e := c.BeginParse()
		if e != nil {
			return nil, e
		}
		tx := new(tlb.Transaction)
		if tlb.LoadFromCell(tx, s) != nil || !bytes.Equal(tx.AccountAddr, ad.Data()) {
			return nil, errors.New("TON transaction BOC account mismatch")
		}
		tx.Hash = c.Hash()
		out = append(out, tx)
	}
	return out, nil
}
func success(tx *tlb.Transaction) (bool, bool) {
	d, ok := tx.Description.(tlb.TransactionDescriptionOrdinary)
	if !ok {
		return false, false
	}
	vm, ok := d.ComputePhase.Phase.(tlb.ComputePhaseVM)
	if !ok || !vm.Success || (vm.Details.ExitCode != 0 && vm.Details.ExitCode != 1) || d.Aborted {
		return false, true
	}
	if d.ActionPhase != nil && (!d.ActionPhase.Success || !d.ActionPhase.Valid || d.ActionPhase.NoFunds || d.ActionPhase.ResultCode != 0 || d.ActionPhase.SkippedActions != 0) {
		return false, true
	}
	return true, true
}
func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	if p.ID != planID(p) {
		return model.Receipt{}, errors.New("TON archived plan digest mismatch")
	}
	check := p
	check.Expires = time.Now().Add(time.Minute).Unix()
	check.ID = planID(check)
	// The original expiry is part of the signature. Validate the archived plan's
	// structure using a temporary deadline, then verify its original signed body.
	if e := a.ValidatePlan(check); e != nil {
		return model.Receipt{}, e
	}
	x, _ := payload(p)
	c, e := parseBOC(s.Bytes)
	if e != nil || hex.EncodeToString(c.Hash()) != s.Identity {
		return model.Receipt{}, errors.New("TON archived message identity mismatch")
	}
	originalUnsigned, _, e := message(p, x)
	if e != nil {
		return model.Receipt{}, e
	}
	sl, e := c.BeginParse()
	if e != nil {
		return model.Receipt{}, e
	}
	var ext tlb.ExternalMessage
	if tlb.LoadFromCell(&ext, sl) != nil || ext.Body == nil {
		return model.Receipt{}, errors.New("TON archived message malformed")
	}
	b, e := ext.Body.BeginParse()
	if e != nil || b.BitsLeft() != 642 {
		return model.Receipt{}, errors.New("TON archived body malformed")
	}
	if _, e = b.LoadSlice(130); e != nil {
		return model.Receipt{}, e
	}
	sig, e := b.LoadSlice(512)
	if e != nil {
		return model.Receipt{}, e
	}
	_, pub, _ := descriptor(p.Account)
	if !ed25519.Verify(pub, originalUnsigned.Hash(), sig) {
		return model.Receipt{}, errors.New("TON archived signature mismatch")
	}
	expectedBody := cell.BeginCell().MustStoreBuilder(originalUnsigned.ToBuilder()).MustStoreSlice(sig, 512).EndCell()
	expected, e := external(p, x, expectedBody)
	if e != nil || !bytes.Equal(expected.Hash(), c.Hash()) {
		return model.Receipt{}, errors.New("TON archived spend effects mismatch")
	}
	if _, e = a.verifyNetwork(ctx, p.Account.Network); e != nil {
		return model.Receipt{}, e
	}
	tx, _, e := a.findArchivedTransaction(ctx, p.Account.Network, p.Account.Address, func(tx *tlb.Transaction) bool {
		if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeExternalIn {
			return false
		}
		incoming := tx.IO.In.AsExternalIn()
		return incoming.Body != nil && bytes.Equal(incoming.Body.Hash(), ext.Body.Hash()) && incoming.DstAddr != nil && incoming.DstAddr.Equals(ext.DstAddr)
	})
	if e != nil {
		return model.Receipt{}, e
	}
	if tx != nil {
		ok, known := success(tx)
		if !known {
			return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "unsupported source transaction description"}, nil
		}
		if !ok {
			return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "source computation/action failed", Block: hex.EncodeToString(tx.Hash)}, nil
		}
		if x.Kind == "deployment" {
			return a.reconcileDeployment(ctx, p, s, tx)
		}
		description := tx.Description.(tlb.TransactionDescriptionOrdinary)
		if description.ActionPhase == nil {
			return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "source transfer has no successful action phase", Block: hex.EncodeToString(tx.Hash)}, nil
		}
		if tx.IO.Out == nil || tx.OutMsgCount != 1 {
			return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "approved transfer did not produce exactly one outgoing message"}, nil
		}
		outs, e := tx.IO.Out.List.LoadAll()
		if e != nil || len(outs) != 1 {
			return model.Receipt{}, errors.New("invalid TON outgoing-message dictionary")
		}
		ref, e := outs[0].Value.LoadRef()
		if e != nil {
			return model.Receipt{}, e
		}
		var out tlb.Message
		if out.LoadFromCell(ref) != nil || out.MsgType != tlb.MsgTypeInternal {
			return model.Receipt{}, errors.New("TON outgoing message malformed")
		}
		im := out.AsInternal()
		dest, e := parseAddress(p.Account.Network, p.Recipient)
		if e != nil {
			return model.Receipt{}, e
		}
		amount, _ := chainutil.Big(p.Amount, 120)
		_, expectedMessage, e := message(p, x)
		if e != nil {
			return model.Receipt{}, e
		}
		if !im.DstAddr.Equals(dest) || im.SrcAddr == nil || !im.SrcAddr.Equals(ext.DstAddr) || im.Amount.Nano().Cmp(amount) != 0 || im.Bounced || im.Bounce != x.Bounce || im.Body == nil || !bytes.Equal(im.Body.Hash(), expectedMessage.Body.Hash()) || im.StateInit != nil || (im.ExtraCurrencies != nil && !im.ExtraCurrencies.IsEmpty()) {
			return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "source outgoing spend effects differ from approved transfer"}, nil
		}
		msgCell, e := out.ToCell()
		if e != nil {
			return model.Receipt{}, e
		}
		rt, _, e := a.findArchivedTransaction(ctx, p.Account.Network, p.Recipient, func(rt *tlb.Transaction) bool {
			if rt.IO.In == nil || rt.IO.In.MsgType != tlb.MsgTypeInternal {
				return false
			}
			inc, err := rt.IO.In.ToCell()
			return err == nil && bytes.Equal(inc.Hash(), msgCell.Hash())
		})
		if e != nil {
			return model.Receipt{Identity: s.Identity, State: "SUBMITTED", Detail: "source action succeeded; recipient archival trace unavailable or inconsistent"}, e
		}
		if rt != nil {
			d, ok := rt.Description.(tlb.TransactionDescriptionOrdinary)
			if !ok || d.CreditPhase == nil || d.CreditPhase.Credit.Coins.Nano().Cmp(amount) < 0 {
				return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "recipient trace has no matching credit"}, nil
			}
			if im.Bounce {
				if paid, known := success(rt); !known || !paid {
					return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "recipient computation/action bounced or failed"}, nil
				}
			}
			return model.Receipt{Identity: s.Identity, State: "CONFIRMED", Detail: "matching source external message, successful action, and recipient credit BOCs (trusted RPC; no independent proof of finality)", Block: hex.EncodeToString(rt.Hash)}, nil
		}
		return model.Receipt{Identity: s.Identity, State: "SUBMITTED", Detail: "source action succeeded; recipient credit not observed within bounded archival search (at most 3200 linked transactions); absence does not prove failure"}, nil
	}
	return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "message absent from bounded archival search (at most 32 pages / 3200 linked transactions); explicit history continuation can inspect older records; absence or seqno advancement does not prove payment failure or success"}, nil
}
