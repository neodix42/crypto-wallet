package decred

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/model"
)

type HistoryItem struct {
	Identity string `json:"identity"`
	Height   uint32 `json:"height"`
	Tree     int8   `json:"tree"`
	Received int64  `json:"received"`
	Spent    int64  `json:"spent"`
	State    string `json:"state"`
}
type SyncReport struct {
	Height          uint32           `json:"height"`
	Hash            string           `json:"hash"`
	ScannedFrom     uint32           `json:"scannedFrom"`
	ScannedThrough  uint32           `json:"scannedThrough"`
	AddressLimit    uint32           `json:"addressLimit"`
	HighestReceive  int64            `json:"highestReceive"`
	HighestChange   int64            `json:"highestChange"`
	Spendable       []Input          `json:"spendable"`
	ImmatureAtoms   int64            `json:"immatureAtoms"`
	ImmatureOutputs []ImmatureOutput `json:"immatureOutputs"`
	History         []HistoryItem    `json:"history"`
}
type ImmatureOutput struct {
	TxID                  string `json:"txid"`
	Vout                  uint32 `json:"vout"`
	Tree                  int8   `json:"tree"`
	Atoms                 int64  `json:"atoms"`
	Confirmations         uint64 `json:"confirmations"`
	RequiredConfirmations uint32 `json:"requiredConfirmations"`
	RemainingBlocks       uint32 `json:"remainingBlocks"`
	SpendableFromBlock    uint64 `json:"spendableFromBlock"`
	Reason                string `json:"reason"`
}
type owned struct{ branch, index uint32 }
type txout struct {
	BestBlock     string      `json:"bestblock"`
	Confirmations int64       `json:"confirmations"`
	Value         json.Number `json:"value"`
	Coinbase      bool        `json:"coinbase"`
	Script        struct {
		Hex     string `json:"hex"`
		Version uint16 `json:"version"`
	} `json:"scriptPubKey"`
}

func (a *Adapter) tip(ctx context.Context, network string) (uint32, string, error) {
	cp, e := params(network)
	if e != nil {
		return 0, "", e
	}
	var genesis string
	if e = a.rpc(ctx, network, "getblockhash", []any{0}, &genesis); e != nil {
		return 0, "", e
	}
	if genesis != cp.GenesisHash.String() {
		return 0, "", errors.New("Decred RPC genesis mismatch")
	}
	var info struct {
		Blocks               uint32 `json:"blocks"`
		BestBlockHash        string `json:"bestblockhash"`
		InitialBlockDownload bool   `json:"initialblockdownload"`
		SyncHeight           uint32 `json:"syncheight"`
	}
	if e = a.rpc(ctx, network, "getblockchaininfo", []any{}, &info); e != nil {
		return 0, "", e
	}
	if info.InitialBlockDownload || info.SyncHeight > info.Blocks {
		return 0, "", errors.New("Decred trusted node is synchronizing")
	}
	if info.BestBlockHash == "" {
		return 0, "", errors.New("Decred trusted node has no checkpoint")
	}
	return info.Blocks, info.BestBlockHash, nil
}
func (a *Adapter) unspent(ctx context.Context, network string, in Input, tip string) error {
	var o *txout
	if e := a.rpc(ctx, network, "gettxout", []any{in.TxID, in.Vout, in.Tree, true}, &o); e != nil {
		return e
	}
	if o == nil {
		return errors.New("Decred input is spent, disapproved or unavailable")
	}
	value, e := decimalAtoms(o.Value.String())
	if e != nil || value != in.Value || o.Script.Hex != in.Script || o.Script.Version != 0 || o.BestBlock != tip || o.Confirmations < 1 {
		return errors.New("Decred node input proof disagrees with synchronized transaction")
	}
	return nil
}

// Sync performs a bounded, repeatable trusted-node scan of both transaction
// trees. It derives only public children, never imports keys into dcrwallet, and
// returns an error rather than a zero balance on incomplete scans or reorgs.
// The caller persists public progress and discovery metadata in the main vault.
type SyncProgress struct {
	Scanned      uint32 `json:"scanned"`
	Target       uint32 `json:"target"`
	AddressLimit uint32 `json:"addressLimit"`
}

func (a *Adapter) ChainTip(ctx context.Context, network string) (uint32, string, error) {
	return a.tip(ctx, network)
}
func (a *Adapter) Sync(ctx context.Context, ac model.Account) (SyncReport, error) {
	return a.SyncWithProgress(ctx, ac, nil)
}
func (a *Adapter) SyncWithProgress(ctx context.Context, ac model.Account, progress func(SyncProgress)) (SyncReport, error) {
	meta, cp, e := descriptor(ac)
	if e != nil {
		return SyncReport{}, e
	}
	height, tip, e := a.tip(ctx, ac.Network)
	if e != nil {
		return SyncReport{}, e
	}
	a.mu.RLock()
	budget := a.maxBlocks
	a.mu.RUnlock()
	if meta.BlockBudget != 0 {
		budget = meta.BlockBudget
	}
	if height < meta.Birthday || height-meta.Birthday > budget {
		return SyncReport{}, errors.New("Decred bounded scan incomplete; choose an explicit birthday/checkpoint or extend the scan")
	}
	xpub, e := hdkeychain.NewKeyFromString(meta.AccountXPub, cp)
	if e != nil {
		return SyncReport{}, e
	}
	defer xpub.Zero()
	scripts := make(map[string]owned, meta.ScanLimit*2)
	for branch := uint32(0); branch < 2; branch++ {
		for j := uint32(0); j < meta.ScanLimit; j++ {
			addr, _, e := publicAt(xpub, cp, branch, j)
			if e != nil {
				return SyncReport{}, errors.New("Decred public child invalid; explicit address-index mapping required")
			}
			_, s, e := paymentScript(ac.Network, addr)
			if e != nil {
				return SyncReport{}, e
			}
			scripts[hex.EncodeToString(s)] = owned{branch, j}
		}
	}
	r := SyncReport{Height: height, Hash: tip, ScannedFrom: meta.Birthday, AddressLimit: meta.ScanLimit, HighestReceive: -1, HighestChange: -1, Spendable: []Input{}, History: []HistoryItem{}}
	utxos := make(map[string]Input)
	var scanErr error
	apply := func(txs []*wire.MsgTx, tree int8, h uint32) {
		for txi, tx := range txs {
			if scanErr != nil {
				return
			}
			id := tx.TxHash().String()
			history := HistoryItem{Identity: id, Height: h, Tree: tree, State: "confirmed"}
			for _, in := range tx.TxIn {
				key := fmt.Sprintf("%s:%d:%d", in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index, in.PreviousOutPoint.Tree)
				if u, ok := utxos[key]; ok {
					if history.Spent > MaxAtoms-u.Value {
						scanErr = errors.New("Decred synchronized input total exceeds monetary bounds")
						return
					}
					history.Spent += u.Value
					delete(utxos, key)
				}
			}
			raw, _ := tx.Bytes()
			previousHex := hex.EncodeToString(raw)
			for j, o := range tx.TxOut {
				own, ok := scripts[hex.EncodeToString(o.PkScript)]
				if !ok || o.Version != 0 || o.Value <= 0 || o.Value > MaxAtoms {
					continue
				}
				if own.branch == 0 && int64(own.index) > r.HighestReceive {
					r.HighestReceive = int64(own.index)
				}
				if own.branch == 1 && int64(own.index) > r.HighestChange {
					r.HighestChange = int64(own.index)
				}
				if len(utxos) >= 10000 {
					scanErr = errors.New("Decred bounded wallet-record scan limit exceeded; incremental persisted synchronization is required")
					return
				}
				in := Input{TxID: id, Vout: uint32(j), Tree: tree, Value: o.Value, Script: hex.EncodeToString(o.PkScript), Previous: previousHex, Branch: own.branch, Index: own.index, Height: h, BlockIndex: uint32(txi)}
				utxos[fmt.Sprintf("%s:%d:%d", id, j, tree)] = in
				if history.Received > MaxAtoms-o.Value {
					scanErr = errors.New("Decred synchronized output total exceeds monetary bounds")
					return
				}
				history.Received += o.Value
			}
			if history.Received > 0 || history.Spent > 0 {
				r.History = append(r.History, history)
			}
			if len(r.History) > 10000 {
				scanErr = errors.New("Decred bounded wallet-record scan limit exceeded; incremental persisted synchronization is required")
				return
			}
		}
	}
	var previousBlock *wire.MsgBlock
	var previousHash string
	for h := meta.Birthday; h <= height; h++ {
		if e = ctx.Err(); e != nil {
			return SyncReport{}, e
		}
		var hash, raw string
		if e = a.rpc(ctx, ac.Network, "getblockhash", []any{h}, &hash); e != nil {
			return SyncReport{}, e
		}
		if e = a.rpc(ctx, ac.Network, "getblock", []any{hash, false}, &raw); e != nil {
			return SyncReport{}, e
		}
		data, e := hex.DecodeString(raw)
		if e != nil || len(data) > 4<<20 {
			return SyncReport{}, errors.New("invalid Decred block bytes")
		}
		block := new(wire.MsgBlock)
		reader := bytes.NewReader(data)
		if !boundedWireBlock(data) || block.Deserialize(reader) != nil || reader.Len() != 0 || block.BlockHash().String() != hash || block.Header.Height != h || (previousHash != "" && block.Header.PrevBlock.String() != previousHash) {
			return SyncReport{}, errors.New("Decred block checkpoint or ancestry mismatch")
		}
		if previousBlock != nil {
			ph := previousBlock.Header.Height
			if int64(h) < cp.StakeValidationHeight || block.Header.VoteBits&1 != 0 {
				apply(previousBlock.Transactions, wire.TxTreeRegular, ph)
			} else {
				for _, tx := range previousBlock.Transactions {
					relevant := false
					for _, out := range tx.TxOut {
						_, ok := scripts[hex.EncodeToString(out.PkScript)]
						relevant = relevant || ok
					}
					for _, in := range tx.TxIn {
						_, ok := utxos[fmt.Sprintf("%s:%d:%d", in.PreviousOutPoint.Hash, in.PreviousOutPoint.Index, in.PreviousOutPoint.Tree)]
						relevant = relevant || ok
					}
					if relevant {
						r.History = append(r.History, HistoryItem{Identity: tx.TxHash().String(), Height: ph, Tree: wire.TxTreeRegular, State: "disapproved"})
					}
				}
			}
			apply(previousBlock.STransactions, wire.TxTreeStake, ph)
		}
		if scanErr != nil {
			return SyncReport{}, scanErr
		}
		previousBlock = block
		previousHash = hash
		r.ScannedThrough = h
		if progress != nil {
			progress(SyncProgress{Scanned: h, Target: height, AddressLimit: meta.ScanLimit})
		}
		if h == height {
			if previousHash != tip {
				return SyncReport{}, errors.New("Decred synchronized checkpoint changed")
			}
			nextHeight, nextTip, e := a.tip(ctx, ac.Network)
			if e != nil {
				return SyncReport{}, e
			}
			if nextHeight < height || nextHeight == height && nextTip != tip {
				return SyncReport{}, errors.New("Decred synchronized checkpoint reorganized")
			}
			if nextHeight == height {
				break
			}
			if nextHeight-meta.Birthday > budget {
				return SyncReport{}, errors.New("Decred catch-up exceeded explicit scan budget")
			}
			height = nextHeight
			tip = nextTip
			r.Height = height
			r.Hash = tip
		}
	}
	if previousBlock != nil {
		if int64(height) < cp.StakeValidationHeight {
			apply(previousBlock.Transactions, wire.TxTreeRegular, height)
		}
		apply(previousBlock.STransactions, wire.TxTreeStake, height)
	}
	if scanErr != nil {
		return SyncReport{}, scanErr
	}
	if previousHash != tip {
		return SyncReport{}, errors.New("Decred chain changed during synchronization")
	}
	newHeight, newTip, e := a.tip(ctx, ac.Network)
	if e != nil {
		return SyncReport{}, e
	}
	if newHeight != height || newTip != tip {
		return SyncReport{}, errors.New("Decred chain changed during synchronization; retry the same bounded scan")
	}
	if r.HighestReceive >= int64(meta.ScanLimit-meta.GapLimit) || r.HighestChange >= int64(meta.ScanLimit-meta.GapLimit) {
		return SyncReport{}, errors.New("Decred discovery range exhausted before gap limit; explicitly extend scan")
	}
	for _, in := range utxos {
		tx, e := previous(in)
		if e != nil {
			return SyncReport{}, e
		}
		confirmations := uint64(height) - uint64(in.Height) + 1
		if (coinbase(tx) || tx.Expiry != 0) && confirmations < uint64(cp.CoinbaseMaturity) {
			if r.ImmatureAtoms > MaxAtoms-in.Value {
				return SyncReport{}, errors.New("Decred immature balance exceeds monetary bounds")
			}
			r.ImmatureAtoms += in.Value
			reason := "transaction-expiry"
			if coinbase(tx) {
				reason = "coinbase"
			}
			r.ImmatureOutputs = append(r.ImmatureOutputs, ImmatureOutput{TxID: in.TxID, Vout: in.Vout, Tree: in.Tree, Atoms: in.Value, Confirmations: confirmations, RequiredConfirmations: uint32(cp.CoinbaseMaturity), RemainingBlocks: uint32(uint64(cp.CoinbaseMaturity) - confirmations), SpendableFromBlock: uint64(in.Height) + uint64(cp.CoinbaseMaturity), Reason: reason})
			continue
		}
		if e = a.unspent(ctx, ac.Network, in, tip); e != nil {
			return SyncReport{}, e
		}
		r.Spendable = append(r.Spendable, in)
	}
	sort.Slice(r.Spendable, func(i, j int) bool {
		l, r := r.Spendable[i], r.Spendable[j]
		if l.Height != r.Height {
			return l.Height < r.Height
		}
		if l.TxID != r.TxID {
			return l.TxID < r.TxID
		}
		if l.Tree != r.Tree {
			return l.Tree < r.Tree
		}
		return l.Vout < r.Vout
	})
	return r, nil
}
func (a *Adapter) Balance(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	r, e := a.Sync(ctx, ac)
	if e != nil {
		return nil, e
	}
	var total int64
	for _, in := range r.Spendable {
		if total > MaxAtoms-in.Value {
			return nil, errors.New("Decred balance overflow")
		}
		total += in.Value
	}
	return []model.Balance{{Asset: ac.Network + ":DCR", Atomic: strconv.FormatInt(total, 10), State: "confirmed-spendable (bounded trusted-node scan)", At: r.Hash}}, nil
}
func (a *Adapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	meta, _, e := descriptor(ac)
	if e != nil {
		return model.Plan{}, e
	}
	if t.AccountID != ac.ID || t.Asset != ac.Network+":DCR" || t.Memo != "" {
		return model.Plan{}, errors.New("unsupported Decred transfer")
	}
	if e = a.ValidateAddress(ac.Network, t.Recipient); e != nil {
		return model.Plan{}, e
	}
	amount, e := atomic(t.Amount, true)
	if e != nil || amount < dustAtoms {
		return model.Plan{}, errors.New("Decred payment is below dust floor")
	}
	r, e := a.Sync(ctx, ac)
	if e != nil {
		return model.Plan{}, e
	}
	var net struct {
		RelayFee json.Number `json:"relayfee"`
	}
	if e = a.rpc(ctx, ac.Network, "getnetworkinfo", []any{}, &net); e != nil {
		return model.Plan{}, e
	}
	rate, e := decimalAtoms(net.RelayFee.String())
	if e != nil {
		return model.Plan{}, e
	}
	if rate < 10000 {
		rate = 10000
	}
	if rate > 1_000_000 {
		return model.Plan{}, errors.New("Decred relay fee exceeds supported limit")
	}
	feeSource := "relay-minimum-no-estimate"
	var quote struct {
		Rate   json.Number `json:"feerate"`
		Errors []string    `json:"errors"`
		Blocks int64       `json:"blocks"`
	}
	if err := a.rpc(ctx, ac.Network, "estimatesmartfee", []any{2, "conservative"}, &quote); err == nil && len(quote.Errors) == 0 && quote.Blocks > 0 {
		estimated, err := decimalAtoms(quote.Rate.String())
		if err != nil {
			return model.Plan{}, errors.New("Decred fee estimate has unsupported precision")
		}
		if estimated > 1_000_000 {
			return model.Plan{}, errors.New("Decred fee estimate exceeds policy")
		}
		if estimated > rate {
			rate = estimated
		}
		feeSource = "smart-conservative-2"
	}
	change, e := AddressAt(ac, 1, meta.ChangeIndex)
	if e != nil {
		return model.Plan{}, e
	}
	x := Payload{Inputs: []Input{}, ChangeAddress: change, ChangeIndex: meta.ChangeIndex, FeeRate: rate, FeeSource: feeSource, TipHeight: r.Height, TipHash: r.Hash, Expiry: r.Height + 12}
	p := model.Plan{Account: ac, Asset: t.Asset, Recipient: t.Recipient, Amount: t.Amount, FeeAsset: t.Asset, Nonce: "utxo:" + r.Hash, Expires: time.Now().Add(5 * time.Minute).Unix()}
	var total int64
	for _, in := range r.Spendable {
		if len(x.Inputs) >= maxInputs {
			break
		}
		if total > MaxAtoms-in.Value {
			return model.Plan{}, errors.New("Decred coin selection overflow")
		}
		total += in.Value
		x.Inputs = append(x.Inputs, in)
		dust := feeFor(603, rate)
		x.Change = dust
		tx, e := makeTx(p, x)
		if e != nil {
			return model.Plan{}, e
		}
		fee := feeFor(signedSize(tx), rate)
		if total < amount+fee {
			continue
		}
		x.Change = total - amount - fee
		x.Fee = fee
		if x.Change < dust {
			x.Change = 0
			tx, e = makeTx(p, x)
			if e != nil {
				return model.Plan{}, e
			}
			minimum := feeFor(signedSize(tx), rate)
			if total-amount > minimum+dust {
				continue
			}
			x.Fee = total - amount
		}
		p.MaxFee = strconv.FormatInt(x.Fee, 10)
		p.Payload, _ = json.Marshal(x)
		p.ID = planID(p)
		if e = a.ValidatePlan(p); e != nil {
			return model.Plan{}, e
		}
		return p, nil
	}
	return model.Plan{}, errors.New("insufficient synchronized mature DCR for amount and fee")
}
func (a *Adapter) Submit(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	_, x, e := a.validateArchivedSigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	height, tip, e := a.tip(ctx, p.Account.Network)
	if e != nil {
		return model.Receipt{}, e
	}
	if height >= x.Expiry {
		return model.Receipt{}, errors.New("Decred transaction expired")
	}
	var checkpoint string
	if e = a.rpc(ctx, p.Account.Network, "getblockhash", []any{x.TipHeight}, &checkpoint); e != nil {
		return model.Receipt{}, e
	}
	if checkpoint != x.TipHash {
		return model.Receipt{}, errors.New("Decred approved checkpoint was reorganized")
	}
	for _, in := range x.Inputs {
		if e = a.unspent(ctx, p.Account.Network, in, tip); e != nil {
			return model.Receipt{}, e
		}
	}
	var id string
	e = a.rpc(ctx, p.Account.Network, "sendrawtransaction", []any{hex.EncodeToString(s.Bytes), false}, &id)
	if e != nil {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "submission uncertain; reconcile the journaled transaction before retrying"}, e
	}
	if id != s.Identity {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN"}, errors.New("Decred node returned another transaction identity")
	}
	return model.Receipt{Identity: id, State: "SUBMITTED", Detail: "mempool acceptance; execution and stake approval pending"}, nil
}
func (a *Adapter) Reconcile(ctx context.Context, p model.Plan, s model.Signed) (model.Receipt, error) {
	if p.ID != planID(p) {
		return model.Receipt{}, errors.New("Decred archived plan digest mismatch")
	}
	// Reconciliation remains available after approval expiry. Only expiry checks
	// are relaxed; the exact signed transaction and spend effects are reverified.
	_, x, e := a.validateArchivedSigned(p, s)
	if e != nil {
		return model.Receipt{}, e
	}
	height, _, e := a.tip(ctx, p.Account.Network)
	if e != nil {
		return model.Receipt{}, e
	}
	var tx struct {
		TxID          string `json:"txid"`
		Hex           string `json:"hex"`
		BlockHash     string `json:"blockhash"`
		Confirmations int64  `json:"confirmations"`
		BlockHeight   uint32 `json:"blockheight"`
	}
	if e = a.rpc(ctx, p.Account.Network, "getrawtransaction", []any{s.Identity, 1}, &tx); e != nil {
		return model.Receipt{Identity: s.Identity, State: "UNKNOWN", Detail: "not available from trusted node; absence is not proof of non-submission"}, e
	}
	if tx.TxID != s.Identity || tx.Hex != hex.EncodeToString(s.Bytes) {
		return model.Receipt{}, errors.New("Decred reconciliation bytes mismatch")
	}
	if tx.Confirmations < 0 {
		return model.Receipt{Identity: s.Identity, State: "REORGED", Block: tx.BlockHash}, nil
	}
	if tx.BlockHash == "" || tx.Confirmations == 0 {
		return model.Receipt{Identity: s.Identity, State: "SUBMITTED"}, nil
	}
	var current string
	if e = a.rpc(ctx, p.Account.Network, "getblockhash", []any{tx.BlockHeight}, &current); e != nil {
		return model.Receipt{}, e
	}
	if current != tx.BlockHash {
		return model.Receipt{Identity: s.Identity, State: "REORGED", Block: tx.BlockHash}, nil
	}
	cp, _ := params(p.Account.Network)
	if int64(tx.BlockHeight+1) >= cp.StakeValidationHeight {
		if height <= tx.BlockHeight {
			return model.Receipt{Identity: s.Identity, State: "SUBMITTED", Detail: "regular tree awaits stake approval", Block: tx.BlockHash}, nil
		}
		var next string
		if e = a.rpc(ctx, p.Account.Network, "getblockhash", []any{tx.BlockHeight + 1}, &next); e != nil {
			return model.Receipt{}, e
		}
		var block struct {
			VoteBits uint16 `json:"votebits"`
		}
		if e = a.rpc(ctx, p.Account.Network, "getblock", []any{next, true}, &block); e != nil {
			return model.Receipt{}, e
		}
		if block.VoteBits&1 == 0 {
			if height >= x.Expiry {
				return model.Receipt{Identity: s.Identity, State: "FAILED", Detail: "disapproved regular-tree transaction has passed its wire expiry", Block: tx.BlockHash}, nil
			}
			return model.Receipt{Identity: s.Identity, State: "REORGED", Detail: "regular transaction tree disapproved; retain reservation because the same signed bytes can be re-included before wire expiry", Block: tx.BlockHash}, nil
		}
	}
	state := "SUBMITTED"
	if tx.Confirmations >= 6 {
		state = "CONFIRMED"
	}
	return model.Receipt{Identity: s.Identity, State: state, Detail: "canonical regular-tree transaction; confirmation depth " + strconv.FormatInt(tx.Confirmations, 10), Block: tx.BlockHash}, nil
}
