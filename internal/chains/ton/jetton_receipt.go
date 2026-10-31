package ton

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

func internalOutputs(tx *tlb.Transaction) ([]*tlb.Message, error) {
	if tx.OutMsgCount > 256 {
		return nil, errors.New("TON output count exceeds bound")
	}
	if tx.IO.Out == nil || tx.IO.Out.List == nil {
		if tx.OutMsgCount != 0 {
			return nil, errors.New("TON output dictionary absent")
		}
		return nil, nil
	}
	rows, e := tx.IO.Out.List.LoadAll()
	if e != nil || len(rows) != int(tx.OutMsgCount) {
		return nil, errors.New("TON output count mismatch")
	}
	out := make([]*tlb.Message, 0, len(rows))
	for _, row := range rows {
		s, e := row.Value.LoadRef()
		if e != nil || row.Value.BitsLeft() != 0 || row.Value.RefsNum() != 0 {
			return nil, errors.New("TON output reference invalid")
		}
		m := new(tlb.Message)
		if e = m.LoadFromCell(s); e != nil || m.MsgType != tlb.MsgTypeInternal {
			return nil, errors.New("TON jetton output message type unsupported")
		}
		out = append(out, m)
	}
	return out, nil
}
func (a *Adapter) linkedMessageTransaction(ctx context.Context, network, account string, msg *tlb.Message) (*tlb.Transaction, error) {
	want, e := msg.ToCell()
	if e != nil {
		return nil, e
	}
	tx, _, e := a.findArchivedTransaction(ctx, network, account, func(tx *tlb.Transaction) bool {
		if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeInternal {
			return false
		}
		got, e := tx.IO.In.ToCell()
		return e == nil && sameCell(want, got)
	})
	return tx, e
}
func jettonInternalBody(p model.Plan, x Payload) *cell.Cell {
	query, _ := chainutil.Uint(x.Jetton.QueryID, 64)
	amount, _ := chainutil.Big(p.Amount, 120)
	own, _ := parseAddress(p.Account.Network, p.Account.Address)
	forward := cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice([]byte(p.Memo), uint(len(p.Memo)*8)).EndCell()
	return cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(own).MustStoreAddr(own).MustStoreCoins(jettonForward).MustStoreBoolBit(true).MustStoreRef(forward).EndCell()
}
func (a *Adapter) reconcileJetton(ctx context.Context, p model.Plan, s model.Signed, tx *tlb.Transaction) (model.Receipt, error) {
	x, _ := payload(p)
	r := model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "USDT requires the exact linked owner, source jetton and recipient jetton executions; incomplete traces do not establish delivery"}
	if e := a.verifyUSDT(ctx, p.Account.Network); e != nil {
		return r, e
	}
	// Current code/owner/master must still match the pinned issuer deployment.
	source, e := a.jettonState(ctx, p.Account.Network, p.Account.Address)
	if e != nil {
		return r, e
	}
	dest, e := a.jettonState(ctx, p.Account.Network, p.Recipient)
	if e != nil {
		return r, e
	}
	if !source.Active || !dest.Active {
		return r, nil
	}
	messages, e := internalOutputs(tx)
	if e != nil {
		return r, e
	}
	if len(messages) != 1 {
		return r, nil
	}
	first := messages[0].AsInternal()
	_, expected, e := message(p, x)
	if e != nil {
		return r, e
	}
	own, _ := parseAddress(p.Account.Network, p.Account.Address)
	if first.SrcAddr == nil || !first.SrcAddr.Equals(own) || first.DstAddr == nil || !first.DstAddr.Equals(expected.DstAddr) || first.Bounced || !first.Bounce || first.StateInit != nil || (first.ExtraCurrencies != nil && !first.ExtraCurrencies.IsEmpty()) || first.Amount.Nano().Cmp(expected.Amount.Nano()) != 0 || !sameCell(first.Body, expected.Body) {
		return r, nil
	}
	sourceTx, e := a.linkedMessageTransaction(ctx, p.Account.Network, x.Jetton.Wallet, messages[0])
	if e != nil {
		return r, e
	}
	if sourceTx == nil {
		return r, nil
	}
	if ok, known := success(sourceTx); !known || !ok {
		r.Detail = "Exact jetton transfer request found but source jetton execution failed; no recipient delivery established"
		return r, nil
	}
	outputs, e := internalOutputs(sourceTx)
	if e != nil {
		return r, e
	}
	if len(outputs) != 1 {
		return r, nil
	}
	forward := outputs[0].AsInternal()
	sw, _ := parseAddress(p.Account.Network, source.Address)
	dw, _ := parseAddress(p.Account.Network, dest.Address)
	if forward.SrcAddr == nil || !forward.SrcAddr.Equals(sw) || forward.DstAddr == nil || !forward.DstAddr.Equals(dw) || forward.Bounced || !forward.Bounce || !sameCell(forward.Body, jettonInternalBody(p, x)) || (forward.ExtraCurrencies != nil && !forward.ExtraCurrencies.IsEmpty()) {
		return r, nil
	}
	recipientTx, e := a.linkedMessageTransaction(ctx, p.Account.Network, x.Jetton.RecipientWallet, outputs[0])
	if e != nil {
		return r, e
	}
	if recipientTx == nil {
		return r, nil
	}
	if ok, known := success(recipientTx); !known || !ok {
		r.Detail = "Recipient jetton computation failed or bounced; token delivery is not confirmed"
		return r, nil
	}
	// The pinned recipient wallet can emit this notification only after adding
	// the exact received amount. A standalone lookalike notification is never used.
	notifications, e := internalOutputs(recipientTx)
	if e != nil {
		return r, e
	}
	query, _ := chainutil.Uint(x.Jetton.QueryID, 64)
	amount, _ := chainutil.Big(p.Amount, 120)
	owner, _ := parseAddress(p.Account.Network, p.Recipient)
	want := cell.BeginCell().MustStoreUInt(0x7362d09c, 32).MustStoreUInt(query, 64).MustStoreBigCoins(amount).MustStoreAddr(own).MustStoreBoolBit(true).MustStoreRef(cell.BeginCell().MustStoreUInt(0, 32).MustStoreSlice([]byte(p.Memo), uint(len(p.Memo)*8)).EndCell()).EndCell()
	for _, m := range notifications {
		im := m.AsInternal()
		if im.SrcAddr != nil && im.SrcAddr.Equals(dw) && im.DstAddr != nil && im.DstAddr.Equals(owner) && !im.Bounced && im.Amount.Nano().Cmp(new(big.Int).SetUint64(jettonForward)) == 0 && sameCell(im.Body, want) {
			r.State = "CONFIRMED"
			r.Block = hex.EncodeToString(recipientTx.Hash)
			r.Detail = "Exact signed V5R1 request and hash-linked successful canonical USDT source/recipient wallet executions plus matching transfer notification verified (trusted archival RPC; no independent consensus proof)"
			return r, nil
		}
	}
	return r, nil
}
