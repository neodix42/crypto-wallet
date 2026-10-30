package decred

import (
	"context"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// historyOutputs reads only already authenticated, locally decoded raw block
// transactions. Missing external prevouts stay unknown instead of attributing
// every input in a multi-party transaction to this wallet.
func historyOutputs(network string, tx *wire.MsgTx, tree int8, scripts map[string]owned, outgoing bool, sources []string, allInputsKnown bool, budget int) ([]model.HistoryTransfer, bool) {
	cp, err := params(network)
	if err != nil {
		return nil, true
	}
	source := ""
	if allInputsKnown && len(sources) > 0 {
		set := map[string]bool{}
		for _, address := range sources {
			set[address] = true
		}
		values := make([]string, 0, len(set))
		for address := range set {
			values = append(values, address)
		}
		sort.Strings(values)
		if joined := strings.Join(values, ", "); len(joined) <= 4096 {
			source = joined
		}
	}
	asset := model.Asset{ID: network + ":DCR", Symbol: "DCR", Decimals: 8}
	var transfers []model.HistoryTransfer
	limited := false
	for index, output := range tx.TxOut {
		_, ours := scripts[hex.EncodeToString(output.PkScript)]
		if !outgoing && !ours || output.Value <= 0 || output.Value > MaxAtoms {
			continue
		}
		if len(transfers) >= 256 || len(transfers) >= budget {
			limited = true
			continue
		}
		destination := ""
		_, addresses := stdscript.ExtractAddrs(output.Version, output.PkScript, cp)
		if len(addresses) == 1 {
			destination = addresses[0].String()
		}
		comment := ""
		if coinbase(tx) {
			comment = "Coinbase"
		}
		transfers = append(transfers, model.HistoryTransfer{ID: "output/" + strconv.Itoa(int(tree)) + "/" + strconv.Itoa(index), Source: source, Destination: destination, Asset: asset, Atomic: strconv.FormatInt(output.Value, 10), Comment: comment})
	}
	return transfers, limited
}

func (*Adapter) HistoryScope() string {
	return "Authenticated dcrd bounded birthday/address scan of native DCR; at most 256 relevant outputs per transaction and 16384 output effects per scan. Input addresses outside scanned wallet outputs remain unknown. Vote-disapproved transactions have no credited effects. Older blocks, unconfirmed activity and addresses beyond the configured discovery limits are outside this scope."
}

func HistoryReceipts(network string, report SyncReport) []model.Receipt {
	entries := make([]model.Receipt, 0, len(report.History))
	for _, item := range report.History {
		detail := "Decoded transaction in authenticated bounded dcrd scan; external input peers can be unknown."
		if item.EffectsLimited {
			detail += " Output-effect display bound reached; this transaction has additional outputs."
		}
		entries = append(entries, model.Receipt{Identity: item.Identity, State: strings.ToUpper(item.State), Detail: detail, Block: strconv.FormatUint(uint64(item.Height), 10), Timestamp: item.Timestamp, Explorer: chainutil.TransactionExplorer(network, item.Identity), Transfers: append([]model.HistoryTransfer(nil), item.Transfers...)})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Timestamp != entries[j].Timestamp {
			return entries[i].Timestamp > entries[j].Timestamp
		}
		return entries[i].Identity < entries[j].Identity
	})
	return entries
}

func (a *Adapter) History(ctx context.Context, account model.Account) ([]model.Receipt, error) {
	report, err := a.Sync(ctx, account)
	if err != nil {
		return nil, err
	}
	return HistoryReceipts(account.Network, report), nil
}
