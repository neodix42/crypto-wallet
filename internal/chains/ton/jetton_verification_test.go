package ton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/model"
)

// Exercise malformed provider storage, independently of display metadata and
// symbol spelling. Native observations survive each token-specific failure.
func TestUSDTPinnedMasterRejectsMalformedAndUpgradedStorage(t *testing.T) {
	for _, kind := range []string{"wrong network", "outage", "data encoding", "missing supply", "missing admin", "external admin", "missing next", "external next", "missing wallet library", "missing metadata", "trailing bits", "trailing refs"} {
		t.Run(kind, func(t *testing.T) {
			a, _, _, ac, r := jettonFixture(t, ProfileSubwallet42)
			st := r.states[USDTMaster]
			master, _ := address.ParseRawAddr(USDTMaster)
			library, _ := jettonCode()
			metadata := cell.BeginCell().EndCell()
			b := cell.BeginCell()
			switch kind {
			case "wrong network":
				if e := a.verifyUSDT(context.Background(), "ton-testnet"); e == nil {
					t.Fatal("USDT identity leaked onto testnet")
				}
				return
			case "outage":
				r.fail = "getAddressInformation"
			case "data encoding":
				st.Data = "invalid"
			case "missing supply":
				st.Data = base64.StdEncoding.EncodeToString(b.EndCell().ToBOC())
			case "missing admin":
				st.Data = base64.StdEncoding.EncodeToString(b.MustStoreCoins(0).EndCell().ToBOC())
			case "external admin":
				st.Data = base64.StdEncoding.EncodeToString(b.MustStoreCoins(0).MustStoreAddr(address.NewAddressExt(0, 8, []byte{1})).EndCell().ToBOC())
			default:
				b.MustStoreCoins(0).MustStoreAddr(master)
				if kind != "missing next" {
					next := address.NewAddressNone()
					if kind == "external next" {
						next = address.NewAddressExt(0, 8, []byte{1})
					}
					b.MustStoreAddr(next)
				}
				if kind != "missing next" && kind != "external next" && kind != "missing wallet library" {
					b.MustStoreRef(library)
					if kind != "missing metadata" {
						b.MustStoreRef(metadata)
					}
				}
				if kind == "trailing bits" {
					b.MustStoreBoolBit(true)
				}
				if kind == "trailing refs" {
					b.MustStoreRef(metadata)
				}
				st.Data = base64.StdEncoding.EncodeToString(b.EndCell().ToBOC())
			}
			r.states[USDTMaster] = st
			if e := a.verifyUSDT(context.Background(), ac.Network); e == nil {
				t.Fatal("malformed canonical-master observation accepted")
			}
			if kind != "outage" {
				balances, e := a.Balance(context.Background(), ac)
				if e != nil || len(balances) != 2 || balances[0].Atomic == "" || balances[1].Atomic != "" || balances[1].State != "UNKNOWN" {
					t.Fatal("master failure fabricated a token zero or destroyed native observation", balances, e)
				}
			}
		})
	}
}

func TestUSDTWalletStorageRequiresExactOwnerMasterStatusAndLayout(t *testing.T) {
	for _, kind := range []string{"invalid owner input", "state outage", "frozen", "data encoding", "missing status", "unsupported status", "missing balance", "truncated balance", "missing owner", "missing master", "trailing bits", "trailing ref", "issuer locked"} {
		t.Run(kind, func(t *testing.T) {
			a, _, tr, ac, r := jettonFixture(t, Profile)
			owner, _ := parseAddress(ac.Network, ac.Address)
			wallet, _ := jettonAddress(owner)
			master, _ := address.ParseRawAddr(USDTMaster)
			st := r.states[wallet.StringRaw()]
			b := cell.BeginCell()
			switch kind {
			case "invalid owner input":
				if _, e := a.jettonState(context.Background(), ac.Network, "invalid"); e == nil {
					t.Fatal("invalid owner accepted")
				}
				return
			case "state outage":
				r.fail = "getAddressInformation"
			case "frozen":
				st.State = "frozen"
			case "data encoding":
				st.Data = "invalid"
			case "missing status":
				st.Data = base64.StdEncoding.EncodeToString(b.EndCell().ToBOC())
			case "unsupported status":
				st.Data = base64.StdEncoding.EncodeToString(b.MustStoreUInt(4, 4).EndCell().ToBOC())
			case "missing balance":
				st.Data = base64.StdEncoding.EncodeToString(b.MustStoreUInt(0, 4).EndCell().ToBOC())
			case "truncated balance":
				st.Data = base64.StdEncoding.EncodeToString(b.MustStoreUInt(0, 4).MustStoreUInt(15, 4).EndCell().ToBOC())
			default:
				status := uint64(0)
				if kind == "issuer locked" {
					status = 3
				}
				b.MustStoreUInt(status, 4).MustStoreCoins(100000000)
				if kind != "missing owner" {
					b.MustStoreAddr(owner)
				}
				if kind != "missing owner" && kind != "missing master" {
					b.MustStoreAddr(master)
				}
				if kind == "trailing bits" {
					b.MustStoreBoolBit(true)
				}
				if kind == "trailing ref" {
					b.MustStoreRef(cell.BeginCell().EndCell())
				}
				st.Data = base64.StdEncoding.EncodeToString(b.EndCell().ToBOC())
			}
			r.states[wallet.StringRaw()] = st
			observed, e := a.jettonState(context.Background(), ac.Network, ac.Address)
			if kind == "issuer locked" {
				if e != nil || observed.Balance != "100000000" || observed.Status != 3 {
					t.Fatal("issuer lock hid owned balance", observed, e)
				}
				balances, e := a.Balance(context.Background(), ac)
				if e != nil || !strings.Contains(balances[1].State, "issuer-locked") {
					t.Fatal("issuer lock not reported", balances, e)
				}
				if _, e = a.Prepare(context.Background(), ac, tr); e == nil {
					t.Fatal("issuer-locked wallet offered a send")
				}
			} else if e == nil {
				t.Fatal("invalid wallet storage returned an observed token balance", observed)
			}
		})
	}
}

func TestUSDTPreparationAndWireBuildersRejectUnboundIntent(t *testing.T) {
	a, _, tr, ac, r := jettonFixture(t, Profile)
	for _, kind := range []string{"wrong account", "wrong asset", "testnet", "descriptor", "recipient", "memo", "network", "fees"} {
		t.Run(kind, func(t *testing.T) {
			account, transfer := ac, tr
			r.fail = ""
			switch kind {
			case "wrong account":
				transfer.AccountID = "other"
			case "wrong asset":
				transfer.Asset = "ton-mainnet:FAKE"
			case "testnet":
				account.Network = "ton-testnet"
			case "descriptor":
				account.Parameters = []byte(`{}`)
			case "recipient":
				transfer.Recipient = "invalid"
			case "memo":
				transfer.Memo = strings.Repeat("a", 121)
			case "network":
				r.fail = "getMasterchainInfo"
			case "fees":
				r.fail = "estimateFee"
			}
			if _, e := a.Prepare(context.Background(), account, transfer); e == nil {
				t.Fatal("unbound transfer prepared")
			}
		})
	}
	r.fail = ""
	p, e := a.Prepare(context.Background(), ac, tr)
	if e != nil {
		t.Fatal(e)
	}
	x, _ := payload(p)
	for _, kind := range []string{"missing payload", "zero amount", "invalid query", "invalid recipient", "invalid source"} {
		t.Run(kind, func(t *testing.T) {
			changed := p
			xp, _ := payload(p)
			switch kind {
			case "missing payload":
				xp.Jetton = nil
			case "zero amount":
				changed.Amount = "0"
			case "invalid query":
				xp.Jetton.QueryID = "-1"
			case "invalid recipient":
				changed.Recipient = "invalid"
			case "invalid source":
				changed.Account.Address = "invalid"
			}
			if _, e := jettonBody(changed, xp); e == nil {
				t.Fatal("unbound wire transfer constructed")
			}
		})
	}
	if JettonEffects(model.Plan{}) != "Unsupported TON jetton transfer" {
		t.Fatal("unknown transfer produced authoritative effects")
	}
	x.Jetton.Balance = "invalid"
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if a.ValidatePlan(p) == nil {
		t.Fatal("invalid token balance entered signing")
	}
}
