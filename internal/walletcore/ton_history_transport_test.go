package walletcore

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

// Exercise the production TON adapter and coordinator together. The loopback
// provider serves synthetic hash-linked BOCs built from the retained public TVM
// template, not a mocked HistoryPager. This is transport/evidence-policy testing,
// separate from actual contract execution and public-provider completeness.
type tonHistoryTransportTrace struct {
	atomic.Int32
	nativeState, nativeHead atomic.Int32
}

func tonHistoryTransportCoordinator(t *testing.T, failure string) (*core, model.Account, string, *tonHistoryTransportTrace) {
	t.Helper()
	var fixture struct{ Seed string }
	raw, err := os.ReadFile("../../tests/oracles/ton/fixtures.json")
	if err != nil || json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("public interoperability fixture unavailable", err)
	}
	seed, err := hex.DecodeString(fixture.Seed)
	if err != nil {
		t.Fatal(err)
	}
	adapter := ton.New()
	account, err := adapter.DeriveProfile(seed, 0, "ton-mainnet", ton.ProfileSubwallet42)
	if err != nil {
		t.Fatal(err)
	}
	peerAccount, err := adapter.DeriveProfile(seed, 1, "ton-mainnet", ton.ProfileSubwallet42)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := address.ParseAddr(account.Address)
	peer, _ := address.ParseAddr(peerAccount.Address)
	master, _ := address.ParseRawAddr(ton.USDTMaster)
	var contracts struct{ MasterCode, MasterData string }
	raw, err = os.ReadFile("../chains/ton/testdata/usdt-contracts.json")
	if err != nil || json.Unmarshal(raw, &contracts) != nil {
		t.Fatal("public canonical USDT contract fixture unavailable", err)
	}
	decodeCell := func(encoded string) *cell.Cell {
		t.Helper()
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := cell.FromBOC(data)
		if err != nil {
			t.Fatal(err)
		}
		return decoded
	}
	masterData := decodeCell(contracts.MasterData).MustBeginParse()
	masterData.MustLoadCoins()
	masterData.MustLoadAddr()
	masterData.MustLoadAddr()
	library := masterData.MustLoadRef().MustToCell()
	if hex.EncodeToString(library.Hash()) != ton.USDTWalletCodeHash {
		t.Fatal("canonical USDT library fixture hash changed")
	}
	data := cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(0).MustStoreAddr(owner).MustStoreAddr(master).EndCell()
	init, err := tlb.ToCell(&tlb.StateInit{Code: library, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	wallet := address.NewAddress(0, 0, init.Hash())
	var retained struct {
		Transactions []struct{ Data string }
	}
	raw, err = os.ReadFile("../../tests/oracles/ton/ton-emulator.json")
	if err != nil || json.Unmarshal(raw, &retained) != nil || len(retained.Transactions) == 0 {
		t.Fatal("retained actual TVM transaction unavailable", err)
	}
	template := retained.Transactions[0].Data
	chains := map[string][]*cell.Cell{}
	for _, token := range []bool{false, true} {
		ownedAddress, source, atomicAmount, memo := owner, peer, uint64(123456789), "retained native transport comment"
		if token {
			ownedAddress, source, atomicAmount, memo = wallet, master, 1234567, "retained USDT transport comment"
		}
		previousLT, previousHash := uint64(0), make([]byte, 32)
		for i := uint64(1); i <= 2; i++ {
			var tx tlb.Transaction
			if err := tlb.LoadFromCell(&tx, decodeCell(template).MustBeginParse()); err != nil {
				t.Fatal(err)
			}
			comment := cell.BeginCell().MustStoreUInt(0, 32).MustStoreStringSnake(memo).EndCell()
			body := comment
			if token {
				body = cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(i, 64).MustStoreCoins(atomicAmount).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(owner).MustStoreCoins(1).MustStoreBoolBit(true).MustStoreRef(comment).EndCell()
			}
			tx.AccountAddr, tx.LT, tx.PrevTxLT, tx.PrevTxHash = ownedAddress.Data(), 1000*i, previousLT, previousHash
			tx.Now = uint32(time.Now().Add(-time.Duration(3-i) * time.Minute).Unix())
			tx.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{IHRDisabled: true, SrcAddr: source, DstAddr: ownedAddress, Amount: tlb.FromNanoTONU(atomicAmount), Body: body}}
			tx.IO.Out, tx.OutMsgCount = nil, 0
			encoded, err := tlb.ToCell(&tx)
			if err != nil {
				t.Fatal(err)
			}
			chains[ownedAddress.StringRaw()] = append([]*cell.Cell{encoded}, chains[ownedAddress.StringRaw()]...)
			previousLT, previousHash = tx.LT, encoded.Hash()
		}
	}
	queries := new(tonHistoryTransportTrace)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
			Params struct {
				Address  string `json:"address"`
				LT       string `json:"lt"`
				Hash     string `json:"hash"`
				Limit    int    `json:"limit"`
				Archival bool   `json:"archival"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid production TON RPC request")
			w.WriteHeader(400)
			return
		}
		queries.Add(1)
		var result any
		switch request.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": "F6OpKZKqvqeFp6CQmFomXNMfMj2EnaUSOXN+Mh+wVWk=", "file_hash": "XplPz01CXAps5qeSWUtxcyBfdAo5zVb1N979KLSKD24=", "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation", "getTransactions":
			requested, err := address.ParseAddr(request.Params.Address)
			if err != nil {
				requested, err = address.ParseRawAddr(request.Params.Address)
			}
			if err != nil {
				t.Error("malformed account escaped the production adapter")
				w.WriteHeader(400)
				return
			}
			if request.Method == "getAddressInformation" {
				code, data := "", ""
				state, balance := "active", "1000000000"
				lastLT, lastHash := "0", base64.StdEncoding.EncodeToString(make([]byte, 32))
				if len(chains[requested.StringRaw()]) != 0 {
					lastLT, lastHash = "2000", base64.StdEncoding.EncodeToString(chains[requested.StringRaw()][0].Hash())
				}
				switch {
				case requested.Equals(master):
					code, data = contracts.MasterCode, contracts.MasterData
				case requested.Equals(wallet):
					code = base64.StdEncoding.EncodeToString(library.ToBOC())
					data = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(5000000).MustStoreAddr(owner).MustStoreAddr(master).EndCell().ToBOC())
				case !requested.Equals(owner):
					t.Error("history queried an unrelated account")
				}
				if requested.Equals(owner) {
					queries.nativeState.Add(1)
				}
				if failure == "unused" && !requested.Equals(master) {
					state, balance, lastLT, lastHash = "uninitialized", "0", "0", base64.StdEncoding.EncodeToString(make([]byte, 32))
				}
				result = map[string]any{"state": state, "balance": balance, "code": code, "data": data, "sync_utime": time.Now().Unix(), "last_transaction_id": map[string]string{"lt": lastLT, "hash": lastHash}}
			} else {
				if request.Params.Limit != 100 || !request.Params.Archival {
					t.Error("production request lost its bounded archival contract")
				}
				rows := []map[string]string{}
				chain := chains[requested.StringRaw()]
				index := 0
				if request.Params.LT != "" {
					if request.Params.LT == "1000" {
						index = 1
					} else if request.Params.LT == "2000" && requested.Equals(owner) {
						queries.nativeHead.Add(1)
					} else {
						t.Error("history requested a guessed logical time")
					}
					if len(chain) != 2 || request.Params.Hash != hex.EncodeToString(chain[index].Hash()) {
						t.Error("opaque core capability lost the exact BOC-linked provider position")
					}
				}
				omit := failure == "unused" || failure == "missing-continuation" && request.Params.LT != "" || requested.Equals(owner) && strings.HasPrefix(failure, "initial-") && request.Params.LT == "" || failure == "initial-head-omitted" && requested.Equals(owner)
				if !omit {
					if failure == "initial-head-wrong" && requested.Equals(owner) && request.Params.LT == "2000" {
						index = 1
					}
					if len(chain) > index {
						rows = append(rows, map[string]string{"data": base64.StdEncoding.EncodeToString(chain[index].ToBOC())})
					}
				}
				result = rows
				if failure == "initial-null" && requested.Equals(owner) {
					result = nil
				}
			}
		default:
			t.Error("read-only TON history attempted a non-history request", request.Method)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	if err := adapter.Configure(account.Network, ton.Endpoint{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	return c, account, path, queries
}

func TestTONActualAdapterHTTPBOCsSurviveCoreOpaquePagination(t *testing.T) {
	c, account, path, queries := tonHistoryTransportCoordinator(t, "")
	read := func(cursor string) model.HistoryPage {
		t.Helper()
		value, err := c.handle(message("chainHistory", map[string]string{"accountId": account.ID, "cursor": cursor}))
		if err != nil {
			t.Fatal("actual TON adapter history failed through coordinator", err)
		}
		wire, err := publicJSON(value)
		var page model.HistoryPage
		if err != nil || json.Unmarshal(wire, &page) != nil {
			t.Fatal("TON history lost typed public-wire values", err)
		}
		return page
	}
	first := read("")
	if first.Complete || len(first.Entries) != 2 || len(first.NextCursor) != 32 || !lowerHex(first.NextCursor) {
		t.Fatal("real TON BOC rows or core continuation were lost", len(first.Entries), first.Complete)
	}
	last := read(first.NextCursor)
	if !last.Complete || last.NextCursor != "" || len(last.Entries) != 2 {
		t.Fatal("both exact TON account chains did not reach their beginnings")
	}
	seen, native, token := map[string]bool{}, 0, 0
	for _, page := range []model.HistoryPage{first, last} {
		for _, receipt := range page.Entries {
			if receipt.State != "OBSERVED" || len(receipt.Transfers) != 1 || seen[receipt.Identity] || receipt.Explorer != "https://tonviewer.com/transaction/"+receipt.Identity {
				t.Fatal("observed BOC receipt identity, status or effect was changed")
			}
			seen[receipt.Identity] = true
			effect := receipt.Transfers[0]
			switch effect.Asset.ID {
			case account.Network + ":TON":
				native++
				if effect.Asset.Symbol != "GRAM" || effect.Asset.Decimals != 9 || effect.Atomic != "123456789" || effect.Comment != "retained native transport comment" {
					t.Fatal("native BOC values were not retained exactly")
				}
			case "ton-mainnet:USDT":
				token++
				if effect.Asset.Contract != ton.USDTMaster || effect.Asset.Decimals != 6 || effect.Atomic != "1234567" || effect.Comment != "retained USDT transport comment" || effect.Source != ton.USDTMaster {
					t.Fatal("registered canonical token effect was not retained exactly")
				}
			default:
				t.Fatal("unregistered token entered the public history")
			}
		}
	}
	if native != 2 || token != 2 {
		t.Fatal("native or USDT account-chain rows vanished", native, token)
	}
	if queries.nativeState.Load() != 0 || queries.nativeHead.Load() != 0 {
		t.Fatal("healthy nonempty BOC history acquired an unnecessary state/fallback dependency")
	}
	before := queries.Load()
	if _, err := c.handle(message("chainHistory", map[string]string{"accountId": account.ID, "cursor": first.NextCursor})); err == nil || queries.Load() != before {
		t.Fatal("consumed core capability was reusable or contacted the provider")
	}
	for _, method := range readPublicState(t, path)["calls"].([]any) {
		if method != "snapshot" {
			t.Fatal("read-only history requested private payment authority", method)
		}
	}
}

func TestTONActualAdapterEmptyInitialPageUsesOnlyCheckedHeadThroughCore(t *testing.T) {
	for _, mode := range []string{"initial-missing", "initial-head-omitted", "initial-head-wrong", "initial-null", "unused"} {
		t.Run(mode, func(t *testing.T) {
			c, account, _, queries := tonHistoryTransportCoordinator(t, mode)
			value, err := c.handle(message("chainHistory", map[string]string{"accountId": account.ID}))
			if mode == "initial-head-omitted" || mode == "initial-head-wrong" || mode == "initial-null" {
				if err == nil || value != nil {
					t.Fatal("missing, contradictory or null known history became a successful empty page")
				}
				if mode == "initial-null" {
					if queries.nativeState.Load() != 0 || queries.nativeHead.Load() != 0 {
						t.Fatal("malformed history result acquired state-based fallback authority")
					}
				} else if queries.nativeState.Load() != 1 || queries.nativeHead.Load() != 1 {
					t.Fatal("known-head fallback was missing or unbounded")
				}
				if len(c.historyScans) != 0 {
					t.Fatal("failed known-head lookup retained a successful public continuation")
				}
				return
			}
			if err != nil {
				t.Fatal("checked empty-head handling failed through real adapter", err)
			}
			page := value.(model.HistoryPage)
			if mode == "unused" {
				if len(page.Entries) != 0 || page.Complete || page.NextCursor != "" || !strings.Contains(page.Scope, "uninitialized with zero balance") || queries.nativeState.Load() != 1 || queries.nativeHead.Load() != 0 {
					t.Fatal("unused-account observation lost its scoped hint or fabricated complete history")
				}
				return
			}
			if len(page.Entries) != 2 || page.Complete || len(page.NextCursor) != 32 || queries.nativeState.Load() != 1 || queries.nativeHead.Load() != 1 {
				t.Fatal("exact state-recorded native head or separate USDT stream was lost")
			}
			native, token := 0, 0
			for _, row := range page.Entries {
				for _, effect := range row.Transfers {
					switch effect.Asset.ID {
					case "ton-mainnet:TON":
						native++
						if effect.Atomic != "123456789" || effect.Comment != "retained native transport comment" {
							t.Fatal("fallback fabricated or changed the native BOC effect")
						}
					case "ton-mainnet:USDT":
						token++
					}
				}
			}
			if native != 1 || token != 1 {
				t.Fatal("checked native fallback suppressed one of the two account chains")
			}
		})
	}
}

func TestTONActualAdapterMissingLinkedBOCIsErrorThroughCore(t *testing.T) {
	c, account, _, _ := tonHistoryTransportCoordinator(t, "missing-continuation")
	value, err := c.handle(message("chainHistory", map[string]string{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	first := value.(model.HistoryPage)
	if first.NextCursor == "" {
		t.Fatal("fixture did not establish a BOC-linked expected previous transaction")
	}
	if value, err = c.handle(message("chainHistory", map[string]string{"accountId": account.ID, "cursor": first.NextCursor})); err == nil || value != nil {
		t.Fatal("provider omission of a known linked transaction became successful empty history")
	}
}
