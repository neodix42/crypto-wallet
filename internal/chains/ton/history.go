package ton

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"localwallet/internal/chains/chainutil"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"localwallet/internal/model"
)

func (*Adapter) HistoryScope() string {
	return "Trusted archival RPC, at most 100 account transaction BOCs per page; explicit older-history continuation. Native GRAM message/credit observations and canonical mainnet USDT jettons have separate checked account chains; other jettons are not interpreted. History is not recipient-payment confirmation or independent finality. At most 256 messages per transaction and 1000 per scan. Complete means both selected linked chains reached their beginning within this provider; it is not a consensus proof. An empty window is not proof of no historical funds."
}

// History deliberately returns OBSERVED. Reconcile separately binds a journaled
// signature to source execution and matching recipient credit before confirming.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	if _, _, e := descriptor(ac); e != nil {
		return nil, e
	}
	if _, e := a.verifyNetwork(ctx, ac.Network); e != nil {
		return nil, e
	}
	txs, e := a.transactions(ctx, ac.Network, ac.Address)
	if e != nil {
		return nil, e
	}
	return a.historyReceipts(ac, txs)
}
func (a *Adapter) historyReceipts(ac model.Account, txs []*tlb.Transaction) ([]model.Receipt, error) {
	own, _ := parseAddress(ac.Network, ac.Address)
	sort.Slice(txs, func(i, j int) bool { return txs[i].LT > txs[j].LT })
	out := make([]model.Receipt, 0, len(txs))
	seen := map[string]bool{}
	logicalTimes := map[uint64]bool{}
	effects := 0
	for _, tx := range txs {
		identity := hex.EncodeToString(tx.Hash)
		if tx.LT == 0 || seen[identity] || logicalTimes[tx.LT] {
			return nil, errors.New("TON history contains duplicate or invalid transaction identity/logical time")
		}
		seen[identity] = true
		logicalTimes[tx.LT] = true
		if tx.OutMsgCount > 256 || effects+int(tx.OutMsgCount) > 1000 {
			return nil, errors.New("TON history message range exceeds bound; explicit archival extension required")
		}
		effects += int(tx.OutMsgCount)
		transfers := []model.HistoryTransfer{}
		native := model.Asset{ID: ac.Network + ":TON", Symbol: "GRAM", Decimals: 9}
		var detail strings.Builder
		fmt.Fprintf(&detail, "%s; transaction LT%d; recorded native fees %s nanos; ", a.HistoryScope(), tx.LT, tx.TotalFees.Coins.Nano())
		if tx.IO.In != nil && tx.IO.In.MsgType == tlb.MsgTypeInternal {
			im := tx.IO.In.AsInternal()
			if im.DstAddr == nil || !im.DstAddr.Equals(own) || !historyPeer(im.SrcAddr) {
				return nil, errors.New("TON history inbound native message account/source mismatch")
			}
			comment, e := historyComment(im)
			if e != nil {
				return nil, e
			}
			transfers = append(transfers, model.HistoryTransfer{ID: "in", Source: im.SrcAddr.StringRaw(), Destination: im.DstAddr.StringRaw(), Asset: native, Atomic: im.Amount.Nano().String(), Comment: comment})
			fmt.Fprintf(&detail, "native inbound message %s nanos from %s (bounced=%t); ", im.Amount.Nano(), im.SrcAddr.StringRaw(), im.Bounced)
		}
		if tx.IO.Out == nil || tx.IO.Out.List == nil {
			if tx.OutMsgCount != 0 {
				return nil, errors.New("TON history missing outgoing-message dictionary")
			}
		} else {
			rows, e := tx.IO.Out.List.LoadAll()
			if e != nil || len(rows) != int(tx.OutMsgCount) {
				return nil, errors.New("TON history outgoing-message count mismatch")
			}
			for index, row := range rows {
				ref, e := row.Value.LoadRef()
				if e != nil || row.Value.BitsLeft() != 0 || row.Value.RefsNum() != 0 {
					return nil, errors.New("TON history outgoing-message reference invalid")
				}
				var message tlb.Message
				if message.LoadFromCell(ref) != nil {
					return nil, errors.New("TON history outgoing message malformed")
				}
				if message.MsgType == tlb.MsgTypeExternalOut {
					log := message.AsExternalOut()
					if log.SrcAddr == nil || !log.SrcAddr.Equals(own) {
						return nil, errors.New("TON history external outgoing source mismatch")
					}
					detail.WriteString("external outgoing log (no native credit interpretation); ")
					continue
				}
				if message.MsgType != tlb.MsgTypeInternal {
					return nil, errors.New("TON history outgoing message type invalid")
				}
				im := message.AsInternal()
				if im.SrcAddr == nil || !im.SrcAddr.Equals(own) || !historyPeer(im.DstAddr) {
					return nil, errors.New("TON history outgoing native message account/destination mismatch")
				}
				comment, e := historyComment(im)
				if e != nil {
					return nil, e
				}
				transfers = append(transfers, model.HistoryTransfer{ID: fmt.Sprintf("out:%d", index), Source: im.SrcAddr.StringRaw(), Destination: im.DstAddr.StringRaw(), Asset: native, Atomic: im.Amount.Nano().String(), Comment: comment})
				fmt.Fprintf(&detail, "native outbound message %s nanos to %s (bounce=%t,bounced=%t); recipient credit not queried; ", im.Amount.Nano(), im.DstAddr.StringRaw(), im.Bounce, im.Bounced)
			}
		}
		if d, ok := tx.Description.(tlb.TransactionDescriptionOrdinary); ok {
			if d.CreditPhase != nil {
				fmt.Fprintf(&detail, "account native credit phase %s nanos (observed); ", d.CreditPhase.Credit.Coins.Nano())
			}
			paid, _ := success(tx)
			if paid && d.ActionPhase != nil {
				detail.WriteString("source action succeeded (observed); ")
			} else {
				detail.WriteString("no successful source action established; ")
			}
		} else {
			detail.WriteString("transaction description not interpreted for execution/credit; ")
		}
		out = append(out, model.Receipt{Identity: identity, State: "OBSERVED", Detail: detail.String(), Timestamp: int64(tx.Now), Explorer: chainutil.TransactionExplorer(ac.Network, identity), Transfers: transfers})
	}
	return out, nil
}
func historyPeer(a *address.Address) bool { return a != nil && a.Type() == address.StdAddress }

// Only standard plaintext message comments are interpreted; arbitrary/encrypted
// payloads remain absent. Retain the shared bounded, inert-text contract.
func historyComment(message *tlb.InternalMessage) (string, error) {
	text := message.Comment()
	if len(text) > 4096 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", errors.New("TON history plaintext comment is invalid or exceeds bound")
	}
	return text, nil
}
