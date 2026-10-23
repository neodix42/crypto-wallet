package decred

import (
	"encoding/json"
	"strconv"
)

// MarshalJSON is the public discovery DTO. Atomic quantities cross the desktop
// boundary as decimal strings. Creation proofs stay inside the Go adapter: a
// shared large previous transaction must not be serialized once per UTXO.
func (r SyncReport) MarshalJSON() ([]byte, error) {
	type metadata SyncReport
	type output struct {
		TxID       string `json:"txid"`
		Vout       uint32 `json:"vout"`
		Tree       int8   `json:"tree"`
		Value      string `json:"value"`
		Branch     uint32 `json:"branch"`
		Index      uint32 `json:"index"`
		Height     uint32 `json:"height"`
		BlockIndex uint32 `json:"blockIndex"`
	}
	type historyMetadata HistoryItem
	type history struct {
		historyMetadata
		Received string `json:"received"`
		Spent    string `json:"spent"`
	}
	type immatureMetadata ImmatureOutput
	type immature struct {
		immatureMetadata
		Atoms string `json:"atoms"`
	}
	view := struct {
		metadata
		Spendable       []output   `json:"spendable"`
		ImmatureAtoms   string     `json:"immatureAtoms"`
		ImmatureOutputs []immature `json:"immatureOutputs"`
		History         []history  `json:"history"`
	}{metadata: metadata(r), Spendable: make([]output, 0, len(r.Spendable)), ImmatureAtoms: strconv.FormatInt(r.ImmatureAtoms, 10), ImmatureOutputs: make([]immature, 0, len(r.ImmatureOutputs)), History: make([]history, 0, len(r.History))}
	for _, v := range r.Spendable {
		view.Spendable = append(view.Spendable, output{TxID: v.TxID, Vout: v.Vout, Tree: v.Tree, Value: strconv.FormatInt(v.Value, 10), Branch: v.Branch, Index: v.Index, Height: v.Height, BlockIndex: v.BlockIndex})
	}
	for _, v := range r.ImmatureOutputs {
		view.ImmatureOutputs = append(view.ImmatureOutputs, immature{immatureMetadata: immatureMetadata(v), Atoms: strconv.FormatInt(v.Atoms, 10)})
	}
	for _, v := range r.History {
		view.History = append(view.History, history{historyMetadata: historyMetadata(v), Received: strconv.FormatInt(v.Received, 10), Spent: strconv.FormatInt(v.Spent, 10)})
	}
	return json.Marshal(view)
}
