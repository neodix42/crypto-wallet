package ton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

type usdtFixture struct {
	MasterCode string `json:"masterCode"`
	MasterData string `json:"masterData"`
	WalletCode string `json:"walletCode"`
}
type jettonRPC struct {
	states       map[string]accountState
	transactions map[string][]map[string]string
	fail         string
	send         int
	ambiguous    bool
	perPage      int
}

func (r *jettonRPC) configure(t *testing.T, a *Adapter) {
	t.Helper()
	info, _ := netInfo("ton-mainnet")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var q struct {
			Method string `json:"method"`
			Params struct {
				Address string `json:"address"`
				LT      string `json:"lt"`
			} `json:"params"`
		}
		_ = json.NewDecoder(req.Body).Decode(&q)
		if q.Method == r.fail {
			w.WriteHeader(503)
			return
		}
		var result any
		switch q.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": info.root, "file_hash": info.file, "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation":
			ad, e := parseAddress("ton-mainnet", q.Params.Address)
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			st, ok := r.states[ad.StringRaw()]
			if !ok {
				st = accountState{State: "uninitialized", Balance: "0"}
			}
			st.SyncUTime = time.Now().Unix()
			result = st
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]int{"in_fwd_fee": 0, "storage_fee": 0, "gas_fee": 1000000, "fwd_fee": 0}}
		case "getTransactions":
			ad, _ := parseAddress("ton-mainnet", q.Params.Address)
			rows := r.transactions[ad.StringRaw()]
			if q.Params.LT != "" {
				offset := len(rows)
				for i, row := range rows {
					c, e := decodeBOC(row["data"])
					if e != nil {
						continue
					}
					sl, _ := c.BeginParse()
					var tx tlb.Transaction
					if tlb.LoadFromCell(&tx, sl) == nil && fmt.Sprint(tx.LT) == q.Params.LT {
						offset = i
						break
					}
				}
				rows = rows[offset:]
			}
			limit := r.perPage
			if limit == 0 {
				limit = 100
			}
			if len(rows) > limit {
				rows = rows[:limit]
			}
			if rows == nil {
				rows = []map[string]string{}
			}
			result = rows
		case "sendBoc":
			r.send++
			if r.ambiguous {
				w.WriteHeader(504)
				return
			}
			result = map[string]any{"@type": "ok"}
		default:
			t.Errorf("unexpected jetton RPC %s", q.Method)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	if e := a.Configure("ton-mainnet", Endpoint{URL: server.URL}); e != nil {
		t.Fatal(e)
	}
}
func jettonFixture(t *testing.T, profile string) (*Adapter, []byte, model.Transfer, model.Account, *jettonRPC) {
	t.Helper()
	_, seed := fixtures(t)
	a := New()
	ac, e := a.DeriveProfile(seed, 0, "ton-mainnet", profile)
	if e != nil {
		t.Fatal(e)
	}
	dest, e := a.DeriveProfile(seed, 1, "ton-mainnet", profile)
	if e != nil {
		t.Fatal(e)
	}
	var artifact usdtFixture
	raw, e := os.ReadFile("testdata/usdt-contracts.json")
	if e != nil || json.Unmarshal(raw, &artifact) != nil {
		t.Fatal("missing public contract fixture", e)
	}
	own, _ := parseAddress(ac.Network, ac.Address)
	wallet, _ := jettonAddress(own)
	master, _ := address.ParseRawAddr(USDTMaster)
	data := cell.BeginCell().MustStoreUInt(0, 4).MustStoreCoins(100000000).MustStoreAddr(own).MustStoreAddr(master).EndCell()
	r := &jettonRPC{states: map[string]accountState{USDTMaster: {State: "active", Balance: "1000000000", Code: artifact.MasterCode, Data: artifact.MasterData}, own.StringRaw(): {State: "uninitialized", Balance: "1000000000"}, wallet.StringRaw(): {State: "active", Balance: "50000000", Code: usdtWalletLibraryBOC, Data: base64.StdEncoding.EncodeToString(data.ToBOC())}}, transactions: map[string][]map[string]string{}}
	r.configure(t, a)
	return a, seed, model.Transfer{AccountID: ac.ID, Asset: usdtAsset().ID, Recipient: dest.Address, Amount: "1234567", Memo: "public USDT fixture"}, ac, r
}
func TestCanonicalUSDTPlansBalanceAndHostileIdentity(t *testing.T) {
	a, seed, transfer, ac, r := jettonFixture(t, ProfileSubwallet42)
	p, e := a.Prepare(context.Background(), ac, transfer)
	if e != nil {
		t.Fatal(e)
	}
	x, _ := payload(p)
	if !IsJettonPlan(p) || p.FeeAsset != "ton-mainnet:TON" || p.MaxFee != "103000000" || !strings.Contains(JettonEffects(p), "0.1 GRAM") {
		t.Fatal("wrong exact jetton effects", p)
	}
	if !strings.Contains(JettonEffects(p), "exact recorded V5R1 StateInit") || !strings.Contains(JettonEffects(p), "initializes a missing recipient jetton wallet") {
		t.Fatal("approval hid first-send contract activation")
	}
	signed, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = a.validateSigned(p, signed); e != nil {
		t.Fatal(e)
	}
	balances, e := a.Balance(context.Background(), ac)
	if e != nil || len(balances) != 2 || balances[1].Atomic != "100000000" {
		t.Fatal("canonical balance absent", balances, e)
	}
	for name, mutate := range map[string]func(*model.Plan, *Payload){"master": func(_ *model.Plan, x *Payload) { x.Jetton.Master = "0:" + strings.Repeat("00", 32) }, "wallet": func(_ *model.Plan, x *Payload) { x.Jetton.Wallet = x.Jetton.RecipientWallet }, "recipientwallet": func(_ *model.Plan, x *Payload) { x.Jetton.RecipientWallet = x.Jetton.Wallet }, "wrong asset": func(p *model.Plan, _ *Payload) { p.Asset = "ton-mainnet:FAKE" }, "wrong decimals amount overbalance": func(p *model.Plan, _ *Payload) { p.Amount = "1234567000" }, "forward": func(_ *model.Plan, x *Payload) { x.Jetton.Forward = "0" }, "gas": func(_ *model.Plan, x *Payload) { x.Jetton.Attached = "1" }, "arbitrary call": func(_ *model.Plan, x *Payload) { x.Kind = "jetton_burn" }, "query overflow": func(_ *model.Plan, x *Payload) { x.Jetton.QueryID = "18446744073709551616" }, "native disguise": func(p *model.Plan, x *Payload) { x.Kind = ""; p.Asset = "ton-mainnet:TON" }} {
		t.Run(name, func(t *testing.T) {
			changed := p
			changedX, _ := payload(changed)
			mutate(&changed, &changedX)
			changed.Payload, _ = json.Marshal(changedX)
			changed.ID = planID(changed)
			if a.ValidatePlan(changed) == nil {
				t.Fatal("hostile token plan accepted")
			}
			if _, _, e = a.validateSigned(changed, signed); e == nil {
				t.Fatal("old signature accepted changed plan")
			}
		})
	}
	bad := r.states[USDTMaster]
	bad.Code = usdtWalletLibraryBOC
	r.states[USDTMaster] = bad
	balances, e = a.Balance(context.Background(), ac)
	if e != nil || balances[0].Atomic != "1000000000" || balances[1].Atomic != "" || balances[1].State != "UNKNOWN" {
		t.Fatal("token failure destroyed native balance or invented zero", balances, e)
	}
	if _, e = a.Submit(context.Background(), p, signed); e == nil || r.send != 0 {
		t.Fatal("changed issuer code sent funds")
	}
	_ = x
}
func TestUSDTSubmitAmbiguityOriginalIdentityAndNoImplicitRetry(t *testing.T) {
	a, seed, tr, ac, r := jettonFixture(t, Profile)
	p, e := a.Prepare(context.Background(), ac, tr)
	if e != nil {
		t.Fatal(e)
	}
	s, e := a.Sign(seed, p)
	if e != nil {
		t.Fatal(e)
	}
	r.ambiguous = true
	receipt, e := a.Submit(context.Background(), p, s)
	if e == nil || receipt.State != "UNKNOWN" || receipt.Identity != s.Identity || r.send != 1 {
		t.Fatal("uncertain submit lost original identity", receipt, e)
	}
	receipt, e = a.Reconcile(context.Background(), p, s)
	if e != nil || receipt.State != "UNKNOWN" || r.send != 1 {
		t.Fatal("absent history caused resend/false delivery", receipt, e)
	}
}

func TestUSDTStateAndSpendRechecksFailClosed(t *testing.T) {
	for _, failure := range []string{"source owner", "source master", "source code", "source locked", "recipient locked", "source missing", "token insufficient", "GRAM insufficient", "master library", "stale provider"} {
		t.Run(failure, func(t *testing.T) {
			a, seed, tr, ac, r := jettonFixture(t, ProfileSubwallet42)
			plan, e := a.Prepare(context.Background(), ac, tr)
			if e != nil {
				t.Fatal(e)
			}
			signed, e := a.Sign(seed, plan)
			if e != nil {
				t.Fatal(e)
			}
			x, _ := payload(plan)
			owner, _ := parseAddress(ac.Network, ac.Address)
			master, _ := address.ParseRawAddr(USDTMaster)
			dst, _ := parseAddress(ac.Network, tr.Recipient)
			switch failure {
			case "GRAM insufficient":
				st := r.states[owner.StringRaw()]
				st.Balance = "102999999"
				r.states[owner.StringRaw()] = st
			case "master library":
				st := r.states[USDTMaster]
				data, _ := decodeBOC(st.Data)
				s := data.MustBeginParse()
				s.MustLoadBigCoins()
				admin := s.MustLoadAddr()
				next := s.MustLoadAddr()
				_, _ = s.LoadRefCell()
				meta, _ := s.LoadRefCell()
				st.Data = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreCoins(1).MustStoreAddr(admin).MustStoreAddr(next).MustStoreRef(cell.BeginCell().EndCell()).MustStoreRef(meta).EndCell().ToBOC())
				r.states[USDTMaster] = st
			case "stale provider":
				r.fail = "getAddressInformation"
			case "source missing":
				delete(r.states, x.Jetton.Wallet)
			default:
				key := x.Jetton.Wallet
				recordedOwner := owner
				recordedMaster := master
				status := uint64(0)
				balance := uint64(100000000)
				if failure == "source owner" {
					recordedOwner = dst
				}
				if failure == "source master" {
					recordedMaster = owner
				}
				if failure == "source locked" {
					status = 1
				}
				if failure == "token insufficient" {
					balance = 1
				}
				if failure == "recipient locked" {
					key = x.Jetton.RecipientWallet
					recordedOwner = dst
					status = 2
				}
				st := r.states[key]
				st.State = "active"
				st.Balance = "50000000"
				st.Code = usdtWalletLibraryBOC
				st.Data = base64.StdEncoding.EncodeToString(cell.BeginCell().MustStoreUInt(status, 4).MustStoreCoins(balance).MustStoreAddr(recordedOwner).MustStoreAddr(recordedMaster).EndCell().ToBOC())
				if failure == "source code" {
					st.Code = base64.StdEncoding.EncodeToString(cell.BeginCell().EndCell().ToBOC())
				}
				r.states[key] = st
			}
			if _, e = a.Prepare(context.Background(), ac, tr); e == nil {
				t.Fatal("changed token/GRAM state produced a fresh payable plan")
			}
			if _, e = a.Submit(context.Background(), plan, signed); e == nil || r.send != 0 {
				t.Fatal("changed token or GRAM state reached broadcast", e)
			}
		})
	}
}

func TestUSDTContinuationBoundAndLegacyPayloadIsolation(t *testing.T) {
	a, _, tr, ac, _ := jettonFixture(t, Profile)
	cursor := encodeUSDTPageCursor(usdtHistoryCursor{Version: 2, Account: positionAccount(ac.Network, ac.Address), Token: &transactionPosition{LT: "123", Hash: strings.Repeat("01", 32)}})
	if len(cursor) > 512 {
		t.Fatal("USDT cursor exceeded transport bound")
	}
	decoded, e := decodeUSDTPageCursor(ac, cursor)
	if e != nil || decoded.Token == nil || decoded.Token.LT != "123" {
		t.Fatal("valid token cursor lost", e)
	}
	other := ac
	other.ID = "other"
	other.Address = tr.Recipient
	if _, e = decodeUSDTPageCursor(other, cursor); e == nil {
		t.Fatal("cross-owner token cursor accepted")
	}
	if _, e = a.HistoryPage(context.Background(), ac, strings.Repeat("a", 513)); e == nil {
		t.Fatal("oversized cursor accepted")
	}
	mixed := usdtHistoryCursor{Version: 2, Account: positionAccount(ac.Network, ac.Address), Native: &transactionPosition{LT: "18446744073709551615", Hash: strings.Repeat("01", 32), Account: positionAccount(ac.Network, ac.Address)}, Token: &transactionPosition{LT: "18446744073709551615", Hash: strings.Repeat("02", 32), Account: strings.Repeat("03", 32)}}
	if encoded := encodeUSDTPageCursor(mixed); len(encoded) > 512 {
		t.Fatal("mixed chain cursor exceeds bound", len(encoded))
	} else if _, e := decodeUSDTPageCursor(ac, encoded); e != nil {
		t.Fatal(e)
	}
	legacy, seed, native := testPlan(t)
	original, e := legacy.Sign(seed, native)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(native.Payload), "jetton") {
		t.Fatal("legacy journal payload changed")
	}
	var raw map[string]any
	_ = json.Unmarshal(native.Payload, &raw)
	raw["jetton"] = map[string]any{"master": USDTMaster}
	native.Payload, _ = json.Marshal(raw)
	native.ID = planID(native)
	if legacy.ValidatePlan(native) == nil {
		t.Fatal("native transaction smuggled a jetton payload")
	}
	if _, _, e = legacy.validateSigned(native, original); e == nil {
		t.Fatal("legacy signed bytes accepted altered payload")
	}
}

func FuzzUSDTTransferCreditBOC(f *testing.F) {
	f.Add([]byte{0xb5, 0xee, 0x9c, 0x72})
	f.Add(cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(1, 64).MustStoreCoins(1).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(address.NewAddressNone()).MustStoreCoins(1).MustStoreBoolBit(false).EndCell().ToBOC())
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			return
		}
		c, e := parseBOC(raw)
		if e != nil {
			return
		}
		credit, recognized, e := parseJettonCredit(c)
		if e == nil && recognized {
			if credit.Amount == "" || credit.From == nil || credit.Response == nil {
				t.Fatal("recognized credit lost required identity")
			}
		}
	})
}
func TestUSDTActualV5R1Emulator(t *testing.T) {
	if os.Getenv("WALLET_TON_EMULATOR") != "1" {
		t.Skip("NOT_RUN: WALLET_TON_EMULATOR=1 with pinned SDK/TVM dependencies required")
	}
	for _, profile := range []string{Profile, ProfileSubwallet42} {
		t.Run(profile, func(t *testing.T) {
			a, seed, tr, ac, r := jettonFixture(t, profile)
			p, e := a.Prepare(context.Background(), ac, tr)
			if e != nil {
				t.Fatal(e)
			}
			signed, e := a.Sign(seed, p)
			if e != nil {
				t.Fatal(e)
			}
			x, _ := payload(p)
			meta, _, _ := descriptor(ac)
			artifacts, _ := filepath.Abs("testdata/usdt-contracts.json")
			request, _ := json.Marshal(map[string]any{"artifacts": artifacts, "address": ac.Address, "index": ac.Index, "subwallet": meta.Subwallet, "expires": p.Expires, "boc": base64.StdEncoding.EncodeToString(signed.Bytes), "master": USDTMaster, "walletLibrary": usdtWalletLibraryBOC, "recipient": tr.Recipient, "sourceJetton": x.Jetton.Wallet, "recipientJetton": x.Jetton.RecipientWallet, "amount": p.Amount, "memo": p.Memo, "queryId": x.Jetton.QueryID})
			dir := t.TempDir()
			input, output := filepath.Join(dir, "request.json"), filepath.Join(dir, "report.json")
			if e = os.WriteFile(input, request, 0600); e != nil {
				t.Fatal(e)
			}
			log, e := exec.Command("node", "../../../tests/oracles/ton/jetton.cjs", input, output).CombinedOutput()
			if e != nil {
				t.Fatalf("actual USDT emulator: %v\n%s", e, log)
			}
			t.Log(strings.TrimSpace(string(log)))
			var report struct {
				Status                            string
				States                            map[string]accountState
				Transactions                      map[string][]map[string]string
				SourceBalance, DestinationBalance string
			}
			raw, e := os.ReadFile(output)
			if e != nil || json.Unmarshal(raw, &report) != nil || report.Status != "PASS" {
				t.Fatal("invalid USDT TVM report", e)
			}
			for address, st := range report.States {
				r.states[address] = st
			}
			r.transactions = report.Transactions
			receipt, e := a.Reconcile(context.Background(), p, signed)
			if e != nil || receipt.State != "CONFIRMED" {
				t.Fatal("real jetton chain not reconciled", receipt, e)
			}
			originalRows := r.transactions[x.Jetton.RecipientWallet]
			for _, failure := range []string{"recipient VM failure", "fake notification"} {
				c, _ := decodeBOC(originalRows[0]["data"])
				sl, _ := c.BeginParse()
				var hostile tlb.Transaction
				if e := tlb.LoadFromCell(&hostile, sl); e != nil {
					t.Fatal(e)
				}
				if failure == "recipient VM failure" {
					ordinary(t, &hostile, func(d *tlb.TransactionDescriptionOrdinary) {
						vm := d.ComputePhase.Phase.(tlb.ComputePhaseVM)
						vm.Success = false
						d.ComputePhase.Phase = vm
					})
				} else {
					dw, _ := parseAddress(ac.Network, x.Jetton.RecipientWallet)
					destOwner, _ := parseAddress(ac.Network, p.Recipient)
					hostile.OutMsgCount = 1
					setOutgoing(t, &hostile, &tlb.InternalMessage{IHRDisabled: true, SrcAddr: dw, DstAddr: destOwner, Amount: tlb.FromNanoTONU(1), Body: cell.BeginCell().MustStoreUInt(0x7362d09c, 32).EndCell()})
				}
				r.transactions[x.Jetton.RecipientWallet] = []map[string]string{{"data": txBOC(t, &hostile)}}
				if rejected, e := a.Reconcile(context.Background(), p, signed); e != nil || rejected.State != "UNKNOWN" {
					t.Fatalf("%s falsely confirmed: %+v %v", failure, rejected, e)
				}
			}
			r.transactions[x.Jetton.RecipientWallet] = originalRows
			combined, e := a.HistoryPage(context.Background(), ac, "")
			foundUSDT := false
			for _, row := range combined.Entries {
				for _, effect := range row.Transfers {
					foundUSDT = foundUSDT || effect.Asset.ID == usdtAsset().ID
				}
			}
			if e != nil || !foundUSDT {
				t.Fatal("latest USDT hidden behind native history", combined, e)
			}
			entries, _, _, e := a.usdtHistoryPart(context.Background(), ac, nil)
			if e != nil || len(entries) != 1 || entries[0].Transfers[0].Asset.ID != usdtAsset().ID || entries[0].Transfers[0].Atomic != p.Amount || entries[0].Transfers[0].Destination == x.Jetton.RecipientWallet {
				t.Fatal("USDT send history failed", entries, e)
			}
			dest, _ := a.DeriveProfile(seed, 1, "ton-mainnet", profile)
			entries, _, _, e = a.usdtHistoryPart(context.Background(), dest, nil)
			if e != nil || len(entries) != 1 || entries[0].Transfers[0].Destination != func() string { d, _ := parseAddress(ac.Network, p.Recipient); return d.StringRaw() }() {
				t.Fatal("USDT receive history failed", entries, e)
			}
			delete(r.transactions, x.Jetton.RecipientWallet)
			receipt, e = a.Reconcile(context.Background(), p, signed)
			if e != nil || receipt.State != "UNKNOWN" {
				t.Fatal("missing recipient trace falsely confirmed", receipt, e)
			}
		})
	}
}
