package evm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const indexedHistoryPageSize = 20
const indexedHistoryMaxPages = 500

// Native and token candidate streams can repeat one transaction. Every indexed
// candidate is interpreted from its full checked receipt, so the coordinator may
// suppress only an identical full observation across pages.
func (*Adapter) HistoryPagesOverlap() bool { return true }

var errHistoryIndexUnavailable = errors.New("address transaction index unavailable")

// These APIs supply candidate transaction identities only. Neither their amounts,
// peers, symbols, decimals, timestamps, execution status nor URLs are authoritative.
func historyIndexEndpoints(network string) []string {
	switch network {
	case "ethereum-mainnet":
		return []string{"https://api.routescan.io/v2/network/mainnet/evm/1/etherscan/api", "https://eth.blockscout.com/api"}
	case "ethereum-sepolia":
		return []string{"https://eth-sepolia.blockscout.com/api"}
	case "base-mainnet":
		return []string{"https://base.blockscout.com/api"}
	case "base-sepolia":
		return []string{"https://base-sepolia.blockscout.com/api"}
	}
	return nil
}

func (a *Adapter) useIndexedHistory(account model.Account) bool {
	if len(historyIndexEndpoints(account.Network)) == 0 {
		return false
	}
	n, err := a.network(account.Network)
	if err != nil {
		return false
	}
	u, err := url.Parse(n.RPC)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	// Disposable/local nodes have no public index. Do not leak their synthetic
	// accounts to a mainnet service or replace real local execution fixtures.
	return u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())
}

func (a *Adapter) History(ctx context.Context, account model.Account) ([]model.Receipt, error) {
	if !a.useIndexedHistory(account) {
		return a.historyRecentRPC(ctx, account)
	}
	page, err := a.HistoryPage(ctx, account, "")
	return page.Entries, err
}

func (a *Adapter) HistoryPage(ctx context.Context, account model.Account, cursor string) (model.HistoryPage, error) {
	if !a.useIndexedHistory(account) {
		return a.historyRecentPage(ctx, account, cursor)
	}
	return a.historyIndexedPage(ctx, account, cursor)
}

type indexedHistoryCursor struct {
	Version    int    `json:"v"`
	Network    string `json:"network"`
	Owner      string `json:"owner"`
	Head       string `json:"head"`
	Hash       string `json:"hash"`
	Final      string `json:"final"`
	NativePage int    `json:"nativePage"`
	TokenPage  int    `json:"tokenPage"`
	NativeDone bool   `json:"nativeDone"`
	TokenDone  bool   `json:"tokenDone"`
	Source     string `json:"source,omitempty"`
	FromKey    string `json:"fromKey,omitempty"`
	ToKey      string `json:"toKey,omitempty"`
}

func validHistoryRPCKey(key string) bool {
	if len(key) > 512 {
		return false
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

// A custom Alchemy-compatible RPC can provide an address index without any new
// credential field. Its existing encrypted RPC URL remains in the coordinator;
// no demo key, embedded account, API secret or renderer URL is introduced.
func (a *Adapter) historyRPCCandidates(ctx context.Context, n model.Network, account model.Account, head, key string, outgoing bool) ([]string, string, error) {
	if !validHistoryRPCKey(key) {
		return nil, "", errors.New("invalid RPC history continuation")
	}
	query := map[string]any{"fromBlock": "0x0", "toBlock": head, "category": []string{"external", "erc20"}, "maxCount": "0x14", "order": "desc", "excludeZeroValue": false, "withMetadata": false}
	if outgoing {
		query["fromAddress"] = account.Address
	} else {
		query["toAddress"] = account.Address
	}
	if key != "" {
		query["pageKey"] = key
	}
	var result struct {
		Transfers []struct {
			Hash string `json:"hash"`
		} `json:"transfers"`
		PageKey string `json:"pageKey"`
	}
	if a.rpc(ctx, n, "alchemy_getAssetTransfers", []any{query}, &result) != nil {
		return nil, "", errHistoryIndexUnavailable
	}
	if result.Transfers == nil || len(result.Transfers) > indexedHistoryPageSize || !validHistoryRPCKey(result.PageKey) || result.PageKey != "" && result.PageKey == key {
		return nil, "", errors.New("invalid RPC address history result")
	}
	ids := make([]string, 0, len(result.Transfers))
	for _, tx := range result.Transfers {
		if !hashValid(tx.Hash) || tx.Hash != strings.ToLower(tx.Hash) {
			return nil, "", errors.New("invalid RPC history identity")
		}
		ids = append(ids, tx.Hash)
	}
	return ids, result.PageKey, nil
}

func (a *Adapter) historyCandidates(ctx context.Context, endpoint string, account model.Account, head string, page int, tokens bool) ([]string, bool, error) {
	action := "txlist"
	if tokens {
		action = "tokentx"
	}
	end, err := hexInteger(head)
	if err != nil || !end.IsUint64() || page < 1 || page > indexedHistoryMaxPages {
		return nil, false, errors.New("invalid address history position")
	}
	q := url.Values{"module": {"account"}, "action": {action}, "address": {strings.ToLower(account.Address)}, "startblock": {"0"}, "endblock": {end.String()}, "sort": {"desc"}, "page": {strconv.Itoa(page)}, "offset": {strconv.Itoa(indexedHistoryPageSize)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil || a.HTTPClient == nil {
		return nil, false, errHistoryIndexUnavailable
	}
	req.Header.Set("Accept", "application/json")
	client := *a.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, false, errHistoryIndexUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, false, errHistoryIndexUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, false, errors.New("address history response exceeded bound")
	}
	var env struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if StrictJSON(raw, &env) != nil {
		return nil, false, errors.New("invalid address history envelope")
	}
	if env.Status == "0" && env.Message != "No transactions found" {
		return nil, false, errHistoryIndexUnavailable
	}
	var items []struct {
		Hash string `json:"hash"`
	}
	// The duplicate/depth envelope check above also covers this array. Optional
	// provider metadata is deliberately ignored; consumed identities are checked.
	if json.Unmarshal(env.Result, &items) != nil || items == nil || len(items) > indexedHistoryPageSize {
		return nil, false, errors.New("invalid address history candidates")
	}
	if !(env.Status == "1" && env.Message == "OK") && !(env.Status == "0" && env.Message == "No transactions found" && len(items) == 0) {
		return nil, false, errHistoryIndexUnavailable
	}
	ids := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		if !hashValid(item.Hash) || item.Hash != strings.ToLower(item.Hash) {
			return nil, false, errors.New("invalid indexed transaction identity")
		}
		if !seen[item.Hash] {
			ids = append(ids, item.Hash)
			seen[item.Hash] = true
		}
	}
	return ids, len(items) < indexedHistoryPageSize, nil
}

func (a *Adapter) historyIndexedPage(ctx context.Context, account model.Account, rawCursor string) (model.HistoryPage, error) {
	page := model.HistoryPage{Entries: []model.Receipt{}, Scope: "Address-indexed native and tracked ERC-20 history; each transaction, canonical block, receipt and owned effect is checked through the configured chain RPC. Token runtime/precision is checked at the frozen current head; historical upgrades or precision changes are not independently certified. Index coverage, internal calls and bridge effects may be missing. An exhausted index is not proof of complete account history. Powered by Routescan.io APIs (Ethereum) and Blockscout (Base/Sepolia)."}
	n, err := a.validateAccount(account)
	if err != nil {
		return page, err
	}
	position := indexedHistoryCursor{Version: 1, Network: n.ID, Owner: strings.ToLower(account.Address), NativePage: 1, TokenPage: 1, Final: "0x0"}
	if rawCursor == "" {
		head, err := a.current(ctx, n)
		if err != nil {
			return page, err
		}
		position.Head, position.Hash = head.Number, head.Hash
		var final *block
		if a.rpc(ctx, n, "eth_getBlockByNumber", []any{"finalized", false}, &final) == nil && final != nil {
			v, e := hexInteger(final.Number)
			h, _ := hexInteger(head.Number)
			if e != nil || !v.IsUint64() || v.Cmp(h) > 0 || !hashValid(final.Hash) {
				return page, errors.New("invalid indexed history finality")
			}
			position.Final = final.Number
		}
	} else {
		if len(rawCursor) > 4096 || StrictJSON([]byte(rawCursor), &position) != nil || position.Version != 1 || position.Network != n.ID || position.Owner != strings.ToLower(account.Address) || position.NativePage < 1 || position.TokenPage < 1 || position.NativePage > indexedHistoryMaxPages || position.TokenPage > indexedHistoryMaxPages || position.NativeDone && position.TokenDone || !hashValid(position.Hash) || position.Source != "" && position.Source != "rpc-transfers" || !validHistoryRPCKey(position.FromKey) || !validHistoryRPCKey(position.ToKey) || position.Source == "" && (position.FromKey != "" || position.ToKey != "") {
			return page, errors.New("invalid indexed history continuation")
		}
		if err := a.checkChain(ctx, n); err != nil {
			return page, err
		}
		var frozen *block
		if a.rpc(ctx, n, "eth_getBlockByNumber", []any{position.Head, false}, &frozen) != nil || frozen == nil || frozen.Number != position.Head || frozen.Hash != position.Hash {
			return page, errors.New("indexed history snapshot changed; refresh required")
		}
	}
	head, err := hexInteger(position.Head)
	final, finalErr := hexInteger(position.Final)
	if err != nil || !head.IsUint64() || finalErr != nil || !final.IsUint64() || final.Cmp(head) > 0 {
		return page, errors.New("invalid indexed history block bounds")
	}
	var candidates []string
	fetched := false
	var candidateErr error
	for _, endpoint := range historyIndexEndpoints(n.ID) {
		if position.Source == "rpc-transfers" {
			break
		}
		var native, token []string
		nativeDone, tokenDone := position.NativeDone, position.TokenDone
		if !position.NativeDone {
			native, nativeDone, candidateErr = a.historyCandidates(ctx, endpoint, account, position.Head, position.NativePage, false)
		}
		if candidateErr == nil && !position.TokenDone {
			// Routescan's keyless plan is two requests per second. The pause is
			// cancelable and affects only this background read, never a UI prompt.
			if strings.Contains(endpoint, "routescan.io") {
				timer := time.NewTimer(510 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return page, ctx.Err()
				case <-timer.C:
				}
			}
			token, tokenDone, candidateErr = a.historyCandidates(ctx, endpoint, account, position.Head, position.TokenPage, true)
		}
		if candidateErr == nil {
			candidates = append(native, token...)
			position.NativeDone, position.TokenDone = nativeDone, tokenDone
			fetched = true
			break
		}
		if !errors.Is(candidateErr, errHistoryIndexUnavailable) {
			return page, candidateErr
		}
		candidateErr = nil
		// A transport denial may use another reviewed candidate index. Never
		// accept its displayed metadata as a replacement for chain verification.
		candidates = nil
	}
	if !fetched && (rawCursor == "" || position.Source == "rpc-transfers") {
		var outgoing, incoming []string
		fromKey, toKey := position.FromKey, position.ToKey
		candidateErr = nil
		if !position.NativeDone {
			outgoing, fromKey, candidateErr = a.historyRPCCandidates(ctx, n, account, position.Head, position.FromKey, true)
		}
		if candidateErr == nil && !position.TokenDone {
			incoming, toKey, candidateErr = a.historyRPCCandidates(ctx, n, account, position.Head, position.ToKey, false)
		}
		if candidateErr == nil {
			candidates = append(outgoing, incoming...)
			fetched = true
			position.Source, position.FromKey, position.ToKey = "rpc-transfers", fromKey, toKey
			position.NativeDone, position.TokenDone = fromKey == "", toKey == ""
			page.Scope += " Candidate index: the configured RPC's Alchemy-compatible transfer API."
		} else if !errors.Is(candidateErr, errHistoryIndexUnavailable) {
			return page, candidateErr
		}
	}
	if !fetched {
		// Do not turn an unavailable all-time index into a successful empty
		// recent-window result. Existing loaded history remains available in UI.
		return page, errors.New("address history index unavailable; configure a working Alchemy-compatible RPC for address-indexed history or retry the public index later")
	}
	policies := map[string]model.Asset{}
	for _, token := range n.Assets {
		if token.Contract == "" {
			continue
		}
		if a.tokenIdentity(ctx, n, token, position.Head) != nil {
			page.Scope += " Token history unavailable/incomplete for " + token.ID + "."
			continue
		}
		policies[strings.ToLower(token.Contract)] = token
	}
	seen := map[string]bool{}
	for _, id := range candidates {
		if seen[id] {
			continue
		}
		seen[id] = true
		row, err := a.checkedIndexedReceipt(ctx, n, account, id, head, final, policies)
		if err != nil {
			return page, err
		}
		if row != nil {
			page.Entries = append(page.Entries, *row)
		}
	}
	sort.SliceStable(page.Entries, func(i, j int) bool {
		if page.Entries[i].Timestamp == page.Entries[j].Timestamp {
			return page.Entries[i].Identity < page.Entries[j].Identity
		}
		return page.Entries[i].Timestamp > page.Entries[j].Timestamp
	})
	if !position.NativeDone {
		position.NativePage++
	}
	if !position.TokenDone {
		position.TokenPage++
	}
	if !position.NativeDone || !position.TokenDone {
		if position.NativePage > indexedHistoryMaxPages || position.TokenPage > indexedHistoryMaxPages {
			page.Scope += " The 10,000-candidate-per-stream budget was reached; older activity remains unavailable."
			return page, nil
		}
		b, _ := json.Marshal(position)
		page.NextCursor = string(b)
	}
	return page, nil
}

type indexedHistoryLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	TransactionHash string   `json:"transactionHash"`
	BlockHash       string   `json:"blockHash"`
	BlockNumber     string   `json:"blockNumber"`
	LogIndex        string   `json:"logIndex"`
	Removed         bool     `json:"removed"`
}

func (a *Adapter) checkedIndexedReceipt(ctx context.Context, n model.Network, account model.Account, id string, head, final *big.Int, policies map[string]model.Asset) (*model.Receipt, error) {
	var tx *struct {
		historyTx
		BlockHash   string `json:"blockHash"`
		BlockNumber string `json:"blockNumber"`
	}
	if err := a.rpc(ctx, n, "eth_getTransactionByHash", []any{id}, &tx); err != nil {
		return nil, err
	}
	if tx == nil || tx.Hash != id || !hashValid(tx.BlockHash) {
		return nil, errors.New("indexed history transaction binding failed")
	}
	bn, err := hexInteger(tx.BlockNumber)
	if err != nil || !bn.IsUint64() || bn.Cmp(head) > 0 {
		return nil, errors.New("indexed history transaction outside snapshot")
	}
	var b *struct {
		Number       string   `json:"number"`
		Hash         string   `json:"hash"`
		Timestamp    string   `json:"timestamp"`
		Transactions []string `json:"transactions"`
	}
	if err = a.rpc(ctx, n, "eth_getBlockByNumber", []any{tx.BlockNumber, false}, &b); err != nil {
		return nil, err
	}
	if b == nil || b.Number != tx.BlockNumber || b.Hash != tx.BlockHash || len(b.Transactions) > 20000 {
		return nil, errors.New("indexed history canonical block mismatch")
	}
	stamp, err := hexInteger(b.Timestamp)
	if err != nil || !stamp.IsInt64() || stamp.Sign() < 0 || stamp.Int64() > time.Now().Unix()+300 {
		return nil, errors.New("invalid indexed history timestamp")
	}
	included := false
	for _, hash := range b.Transactions {
		if hash == id {
			if included {
				return nil, errors.New("duplicate indexed history block identity")
			}
			included = true
		}
	}
	if !included {
		return nil, errors.New("indexed history transaction absent from canonical block")
	}
	var receipt *struct {
		TransactionHash string              `json:"transactionHash"`
		BlockHash       string              `json:"blockHash"`
		BlockNumber     string              `json:"blockNumber"`
		Status          string              `json:"status"`
		ContractAddress string              `json:"contractAddress"`
		Logs            []indexedHistoryLog `json:"logs"`
	}
	if err = a.rpc(ctx, n, "eth_getTransactionReceipt", []any{id}, &receipt); err != nil {
		return nil, err
	}
	if receipt == nil || receipt.TransactionHash != id || receipt.BlockHash != b.Hash || receipt.BlockNumber != b.Number || receipt.Status != "0x0" && receipt.Status != "0x1" || len(receipt.Logs) > 4096 {
		return nil, errors.New("indexed history receipt binding failed")
	}
	r := &model.Receipt{Identity: id, Block: b.Hash, State: "SUBMITTED", Timestamp: stamp.Int64(), Explorer: chainutil.TransactionExplorer(n.ID, id), Detail: "Chain-RPC checked address history; native transfers and configured ERC-20 receipt events; internal calls and bridge effects are outside this scope."}
	if bn.Cmp(final) <= 0 {
		if receipt.Status == "0x1" {
			r.State = "CONFIRMED"
		} else {
			r.State = "FAILED"
		}
	}
	if receipt.Status == "0x0" {
		r.Detail += " Execution reverted; displayed native amount is attempted, not delivered."
	}
	if a.ValidateAddress(n.ID, tx.From) != nil {
		return nil, errors.New("invalid indexed history source")
	}
	if tx.To != nil && a.ValidateAddress(n.ID, *tx.To) != nil {
		return nil, errors.New("invalid indexed history destination")
	}
	value, err := hexInteger(tx.Value)
	if err != nil {
		return nil, err
	}
	ownedNative := strings.EqualFold(tx.From, account.Address) || tx.To != nil && strings.EqualFold(*tx.To, account.Address)
	if ownedNative && value.Sign() > 0 && tx.To != nil {
		r.Transfers = append(r.Transfers, model.HistoryTransfer{ID: "native", Source: common.HexToAddress(tx.From).Hex(), Destination: common.HexToAddress(*tx.To).Hex(), Asset: n.Assets[0], Atomic: value.String()})
	}
	event := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
	owner := common.BytesToHash(common.HexToAddress(account.Address).Bytes()).Hex()
	indices := map[string]bool{}
	for _, log := range receipt.Logs {
		token, known := policies[strings.ToLower(log.Address)]
		if !known || len(log.Topics) == 0 || log.Topics[0] != event {
			continue
		}
		if len(log.Topics) != 3 || !hashValid(log.Topics[1]) || !hashValid(log.Topics[2]) {
			return nil, errors.New("invalid indexed ERC-20 event topics")
		}
		if log.Topics[1] != owner && log.Topics[2] != owner {
			continue
		}
		if log.Removed || log.TransactionHash != id || log.BlockHash != b.Hash || log.BlockNumber != b.Number || receipt.Status != "0x1" {
			return nil, errors.New("indexed token event inclusion mismatch")
		}
		index, err := hexInteger(log.LogIndex)
		if err != nil || !index.IsUint64() || indices[log.LogIndex] {
			return nil, errors.New("invalid or duplicate indexed token event")
		}
		indices[log.LogIndex] = true
		from, err := historyAddress(log.Topics[1])
		if err != nil {
			return nil, err
		}
		to, err := historyAddress(log.Topics[2])
		if err != nil {
			return nil, err
		}
		amount, err := abiInteger(log.Data)
		if err != nil {
			return nil, err
		}
		if amount.Sign() == 0 {
			continue
		}
		r.Transfers = append(r.Transfers, model.HistoryTransfer{ID: "erc20:" + token.Contract + ":" + log.LogIndex, Source: from, Destination: to, Asset: token, Atomic: amount.String()})
		if len(r.Transfers) > 1000 {
			return nil, errors.New("indexed history effect count exceeded bound")
		}
	}
	if len(r.Transfers) == 0 {
		if !ownedNative {
			return nil, nil
		}
		if tx.To != nil {
			r.Transfers = []model.HistoryTransfer{{ID: "native", Source: common.HexToAddress(tx.From).Hex(), Destination: common.HexToAddress(*tx.To).Hex(), Asset: n.Assets[0], Atomic: value.String()}}
		}
	}
	if !ownedNative && len(r.Transfers) == 0 {
		return nil, errors.New("indexed history candidate has no checked owned effect")
	}
	return r, nil
}
