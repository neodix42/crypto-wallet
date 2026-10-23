package stress

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
	"github.com/ethereum/go-ethereum/core/types"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

// These bounded fake nodes independently decode real signed transactions. Their
// ledger is scheduling/fault evidence only; real consensus gates are separate.
type stressTransfer struct {
	id, from, to, value string
	height              uint64
}
type stressNode struct {
	mu             sync.Mutex
	mode           string
	nonce, height  uint64
	source         string
	transfers      map[string]stressTransfer
	byHeight       map[uint64]stressTransfer
	credits        *big.Int
	calls, replays int
	server         *httptest.Server
	adapter        *evm.Adapter
}

func blockIdentity(height uint64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("public stress block %d", height)))
	return "0x" + hex.EncodeToString(sum[:])
}
func newStressNode(t *testing.T) *stressNode {
	t.Helper()
	n := &stressNode{nonce: 7, height: 10, transfers: map[string]stressTransfer{}, byHeight: map[uint64]stressTransfer{}, credits: new(big.Int)}
	n.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
			ID     int               `json:"id"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&q) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		n.calls++
		if n.mode == "outage" {
			http.Error(w, "public synthetic outage", 503)
			return
		}
		if n.mode == "rate-limit" {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "public synthetic rate limit", 429)
			return
		}
		var result any
		fail := func() {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "error": map[string]any{"code": -32000, "message": "public synthetic rejection"}})
		}
		switch q.Method {
		case "eth_chainId":
			result = "0xaa36a7"
		case "eth_getCode":
			result = "0x"
		case "eth_getBalance":
			result = "0x3635c9adc5dea00000"
		case "eth_getTransactionCount":
			result = fmt.Sprintf("0x%x", n.nonce)
		case "eth_estimateGas":
			result = "0x5208"
		case "eth_call":
			result = "0x"
		case "eth_maxPriorityFeePerGas":
			result = "0x1"
		case "eth_getBlockByNumber":
			var tag string
			var full bool
			_ = json.Unmarshal(q.Params[0], &tag)
			_ = json.Unmarshal(q.Params[1], &full)
			height := n.height
			if tag != "latest" && tag != "finalized" {
				var e error
				height, e = strconv.ParseUint(strings.TrimPrefix(tag, "0x"), 16, 64)
				if e != nil {
					fail()
					return
				}
			}
			b := map[string]any{"number": fmt.Sprintf("0x%x", height), "hash": blockIdentity(height), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x3b9aca00"}
			if full {
				txs := []any{}
				if tx, ok := n.byHeight[height]; ok {
					txs = append(txs, map[string]any{"hash": tx.id, "from": tx.from, "to": tx.to, "value": "0x" + mustBig(tx.value).Text(16), "input": "0x"})
				}
				b["transactions"] = txs
			}
			result = b
		case "eth_sendRawTransaction":
			var raw string
			_ = json.Unmarshal(q.Params[0], &raw)
			data, e := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
			var tx types.Transaction
			if e != nil || tx.UnmarshalBinary(data) != nil || tx.ChainId().Cmp(big.NewInt(11155111)) != 0 || tx.To() == nil {
				fail()
				return
			}
			from, e := types.Sender(types.LatestSignerForChainID(big.NewInt(11155111)), &tx)
			if e != nil || !strings.EqualFold(from.Hex(), n.source) {
				fail()
				return
			}
			id := tx.Hash().Hex()
			if _, exists := n.transfers[id]; exists {
				n.replays++
				result = id
				break
			}
			if tx.Nonce() != n.nonce || tx.Value().Sign() <= 0 || len(tx.Data()) != 0 || tx.Gas() != 25200 {
				fail()
				return
			}
			n.height++
			n.nonce++
			entry := stressTransfer{id: id, from: from.Hex(), to: tx.To().Hex(), value: tx.Value().String(), height: n.height}
			n.transfers[id] = entry
			n.byHeight[n.height] = entry
			n.credits.Add(n.credits, tx.Value())
			if n.mode == "accept-timeout" {
				http.Error(w, "public synthetic lost acceptance response", 503)
				return
			}
			result = id
		case "eth_getTransactionReceipt":
			var id string
			_ = json.Unmarshal(q.Params[0], &id)
			if entry, ok := n.transfers[id]; ok {
				result = map[string]any{"transactionHash": id, "blockHash": blockIdentity(entry.height), "blockNumber": fmt.Sprintf("0x%x", entry.height), "status": "0x1", "logs": []any{}}
			}
		case "eth_getTransactionByHash":
			var id string
			_ = json.Unmarshal(q.Params[0], &id)
			if _, ok := n.transfers[id]; ok {
				result = map[string]string{"hash": id}
			}
		case "eth_getLogs":
			result = []any{}
		default:
			t.Errorf("unexpected bounded stress RPC %s", q.Method)
			fail()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	var e error
	n.adapter, e = evm.NewWithEndpoints(map[string]string{"ethereum-sepolia": n.server.URL})
	if e != nil {
		t.Fatal(e)
	}
	n.adapter.HTTPClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{MaxConnsPerHost: 16, MaxIdleConnsPerHost: 16, MaxIdleConns: 16}}
	t.Cleanup(func() { n.adapter.HTTPClient.CloseIdleConnections(); n.server.Close() })
	return n
}
func mustBig(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("invalid public fixture integer")
	}
	return v
}
func (n *stressNode) sourceSet(source string) {
	n.mu.Lock()
	n.source = source
	n.mu.Unlock()
}
func (n *stressNode) modeSet(mode string) { n.mu.Lock(); n.mode = mode; n.mu.Unlock() }
func (n *stressNode) stats() (int, int, string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.transfers), n.calls, n.credits.String()
}
func (n *stressNode) prepare(t *testing.T, ac model.Account, value string) model.Plan {
	t.Helper()
	p, e := n.adapter.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: "ethereum-sepolia:native", Recipient: "0x1111111111111111111111111111111111111111", Amount: value})
	if e != nil {
		t.Fatal(e)
	}
	return p
}

type stressDCR struct {
	mu      sync.Mutex
	blocks  map[uint32]*wire.MsgBlock
	height  uint32
	adapter *decred.Adapter
	server  *httptest.Server
}

func newStressDCR(t *testing.T) *stressDCR {
	t.Helper()
	d := &stressDCR{blocks: map[uint32]*wire.MsgBlock{}, height: 5, adapter: decred.New()}
	cp := chaincfg.SimNetParams()
	previous := cp.GenesisHash
	for h := uint32(1); h <= 64; h++ {
		b := &wire.MsgBlock{Header: wire.BlockHeader{Height: h, PrevBlock: previous, VoteBits: 1}}
		d.blocks[h] = b
		previous = b.BlockHash()
	}
	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		var result any
		switch q.Method {
		case "getblockchaininfo":
			result = map[string]any{"blocks": d.height, "syncheight": d.height, "bestblockhash": d.blocks[d.height].BlockHash().String(), "initialblockdownload": false}
		case "getblockhash":
			var h uint32
			_ = json.Unmarshal(q.Params[0], &h)
			if h == 0 {
				result = cp.GenesisHash.String()
			} else if b := d.blocks[h]; b != nil {
				result = b.BlockHash().String()
			}
		case "getblock":
			var id string
			_ = json.Unmarshal(q.Params[0], &id)
			for _, b := range d.blocks {
				if b.BlockHash().String() == id {
					var data bytes.Buffer
					_ = b.Serialize(&data)
					result = hex.EncodeToString(data.Bytes())
					break
				}
			}
		default:
			t.Errorf("unexpected bounded DCR discovery RPC %s", q.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "result": result})
	}))
	if e := d.adapter.Configure("decred-simnet", decred.Endpoint{URL: d.server.URL, User: "public-stress", Password: "public-stress-password"}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(d.server.Close)
	return d
}
