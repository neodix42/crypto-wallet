package ton

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

type jettonCredit struct {
	Query    uint64
	Amount   string
	From     *address.Address
	Response *address.Address
	Forward  string
	Comment  string
}

func parseJettonCredit(body *cell.Cell) (jettonCredit, bool, error) {
	var out jettonCredit
	if body == nil {
		return out, false, nil
	}
	s, e := body.BeginParse()
	if e != nil {
		return out, false, e
	}
	op, e := s.LoadUInt(32)
	if e != nil || op != 0x178d4519 {
		return out, false, nil
	}
	out.Query, e = s.LoadUInt(64)
	if e != nil {
		return out, false, e
	}
	amount, e := s.LoadBigCoins()
	if e != nil || amount.Sign() <= 0 {
		return out, false, errors.New("invalid USDT credit amount")
	}
	out.Amount = amount.String()
	out.From, e = s.LoadAddr()
	if e != nil || out.From == nil || (!out.From.IsAddrNone() && !historyPeer(out.From)) {
		return out, false, errors.New("invalid USDT source owner")
	}
	out.Response, e = s.LoadAddr()
	if e != nil || out.Response == nil {
		return out, false, errors.New("invalid USDT response owner")
	}
	forward, e := s.LoadBigCoins()
	if e != nil {
		return out, false, e
	}
	out.Forward = forward.String()
	ref, e := s.LoadBoolBit()
	if e != nil {
		return out, false, e
	}
	var payload *cell.Cell
	if ref {
		payload, e = s.LoadRefCell()
		if e != nil || s.BitsLeft() != 0 || s.RefsNum() != 0 {
			return out, false, errors.New("USDT forward payload has trailing data")
		}
	} else {
		payload = s.MustToCell()
	}
	out.Comment, e = historyComment(&tlb.InternalMessage{Body: payload})
	return out, true, e
}
func jettonHistoryReceipts(ac model.Account, txs []*tlb.Transaction) ([]model.Receipt, error) {
	own, _ := parseAddress(ac.Network, ac.Address)
	wallet, _ := jettonAddress(own)
	master, _ := address.ParseRawAddr(USDTMaster)
	var out []model.Receipt
	for _, tx := range txs {
		if !bytesEqual(tx.AccountAddr, wallet.Data()) {
			return nil, errors.New("USDT history account mismatch")
		}
		if tx.IO.In == nil || tx.IO.In.MsgType != tlb.MsgTypeInternal {
			continue
		}
		incoming := tx.IO.In.AsInternal()
		if incoming.DstAddr == nil || !incoming.DstAddr.Equals(wallet) || incoming.Bounced {
			continue
		}
		ok, known := success(tx)
		if !known || !ok {
			continue
		}
		transfers := []model.HistoryTransfer{}
		credit, isCredit, e := parseJettonCredit(incoming.Body)
		if e != nil {
			return nil, e
		}
		if isCredit {
			sender := master
			minted := incoming.SrcAddr != nil && incoming.SrcAddr.Equals(master)
			if !minted && !credit.From.IsAddrNone() {
				sender, e = jettonAddress(credit.From)
				if e != nil {
					return nil, e
				}
			}
			if incoming.SrcAddr == nil || !incoming.SrcAddr.Equals(sender) {
				return nil, errors.New("USDT incoming peer does not derive from recorded source owner")
			}
			from := master.StringRaw()
			if !minted && !credit.From.IsAddrNone() {
				from = credit.From.StringRaw()
			}
			transfers = append(transfers, model.HistoryTransfer{ID: "usdt-in", Source: from, Destination: own.StringRaw(), Asset: usdtAsset(), Atomic: credit.Amount, Comment: credit.Comment})
		} else {
			// Only actual canonical outgoing internal_transfer messages establish a
			// debit observation; attempted transfer commands and excesses are not tokens.
			outputs, e := internalOutputs(tx)
			if e != nil {
				return nil, e
			}
			for i, m := range outputs {
				im := m.AsInternal()
				credit, match, e := parseJettonCredit(im.Body)
				if e != nil {
					return nil, e
				}
				if !match {
					continue
				}
				if credit.From == nil || !credit.From.Equals(own) || im.SrcAddr == nil || !im.SrcAddr.Equals(wallet) || im.DstAddr == nil || im.Bounced {
					return nil, errors.New("USDT outgoing source owner mismatch")
				}
				// Recover the destination regular owner from the initiating allowlisted
				// transfer command, then independently derive its jetton wallet.
				dest, e := transferDestination(incoming.Body)
				if e != nil {
					return nil, e
				}
				expected, e := jettonAddress(dest)
				if e != nil || !expected.Equals(im.DstAddr) {
					return nil, errors.New("USDT outgoing recipient wallet mismatch")
				}
				if incoming.SrcAddr == nil || (!incoming.SrcAddr.Equals(own) && !incoming.SrcAddr.Equals(master)) {
					return nil, errors.New("USDT outgoing command authority mismatch")
				}
				transfers = append(transfers, model.HistoryTransfer{ID: fmt.Sprintf("usdt-out:%d", i), Source: own.StringRaw(), Destination: dest.StringRaw(), Asset: usdtAsset(), Atomic: credit.Amount, Comment: credit.Comment})
			}
		}
		if len(transfers) > 0 {
			identity := hex.EncodeToString(tx.Hash)
			out = append(out, model.Receipt{Identity: identity, State: "OBSERVED", Timestamp: int64(tx.Now), Explorer: chainutil.TransactionExplorer(ac.Network, identity), Transfers: transfers, Detail: "Canonical USDT wallet code/owner/master and exact internal-transfer peer observed in a successful transaction BOC; historical observation is not independently proven finality or journal delivery confirmation"})
		}
	}
	return out, nil
}
func transferDestination(body *cell.Cell) (*address.Address, error) {
	if body == nil {
		return nil, errors.New("USDT command absent")
	}
	s, e := body.BeginParse()
	if e != nil {
		return nil, e
	}
	op, e := s.LoadUInt(32)
	if e != nil || op != 0x0f8a7ea5 {
		return nil, errors.New("USDT outgoing command unsupported")
	}
	if _, e = s.LoadUInt(64); e != nil {
		return nil, e
	}
	if _, e = s.LoadBigCoins(); e != nil {
		return nil, e
	}
	dest, e := s.LoadAddr()
	if e != nil || !historyPeer(dest) || dest.Workchain() != 0 {
		return nil, errors.New("USDT recipient owner invalid")
	}
	return dest, nil
}
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
