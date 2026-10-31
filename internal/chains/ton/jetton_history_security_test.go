package ton

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

func TestUSDTMixedHistoryFollowsBothChainsAndPreservesNativeCursors(t *testing.T) {
	a, _, _, ac, r := jettonFixture(t, Profile)
	_, _, _, base, _ := receiptFixture(t)
	owner, _ := parseAddress(ac.Network, ac.Address)
	wallet, _ := jettonAddress(owner)
	native := cloneHistory(t, base)
	native.AccountAddr = owner.Data()
	native.IO.In = nil
	native.IO.Out = nil
	native.OutMsgCount = 0
	native.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
	token := cloneHistory(t, base)
	token.AccountAddr = wallet.Data()
	token.IO.Out = nil
	token.OutMsgCount = 0
	master, _ := address.ParseRawAddr(USDTMaster)
	body := cell.BeginCell().MustStoreUInt(0x178d4519, 32).MustStoreUInt(7, 64).MustStoreCoins(1000000).MustStoreAddr(address.NewAddressNone()).MustStoreAddr(owner).MustStoreCoins(0).MustStoreBoolBit(false).EndCell()
	token.IO.In = &tlb.Message{MsgType: tlb.MsgTypeInternal, Msg: &tlb.InternalMessage{IHRDisabled: true, SrcAddr: master, DstAddr: wallet, Amount: tlb.FromNanoTONU(1000000), Body: body}}
	nativeChain, tokenChain := archivalChain(t, native, 3), archivalChain(t, token, 3)
	for _, group := range []struct {
		address string
		chain   []*tlb.Transaction
	}{{owner.StringRaw(), nativeChain}, {wallet.StringRaw(), tokenChain}} {
		for _, tx := range group.chain {
			r.transactions[group.address] = append(r.transactions[group.address], map[string]string{"data": txBOC(t, tx)})
		}
	}
	r.perPage = 2
	first, e := a.HistoryPage(context.Background(), ac, "")
	if e != nil || first.Complete || first.NextCursor == "" || len(first.Entries) != 2 {
		t.Fatal("first mixed chain scope invalid", first, e)
	}
	cursor, e := decodeUSDTPageCursor(ac, first.NextCursor)
	if e != nil || cursor.Native == nil || cursor.Token == nil {
		t.Fatal("independent chain continuations lost", cursor, e)
	}
	last, e := a.HistoryPage(context.Background(), ac, first.NextCursor)
	if e != nil || !last.Complete || last.NextCursor != "" || len(last.Entries) != 2 {
		t.Fatal("second mixed chain did not reach both real beginnings", last, e)
	}
	found := false
	for _, row := range last.Entries {
		for _, effect := range row.Transfers {
			if effect.Asset.ID == usdtAsset().ID {
				found = true
				if effect.Atomic != "1000000" || effect.Source != USDTMaster || effect.Destination != owner.StringRaw() {
					t.Fatal("mint credit mislabeled", effect)
				}
			}
		}
	}
	if !found {
		t.Fatal("older token credit vanished")
	}
	old := *cursor.Native
	old.Account = positionAccount(ac.Network, ac.Address)
	raw, _ := json.Marshal(old)
	restored, e := decodeUSDTPageCursor(ac, base64.RawURLEncoding.EncodeToString(raw))
	if e != nil || restored.Native == nil || restored.Native.LT != old.LT || restored.Token != nil {
		t.Fatal("existing native cursor no longer usable", restored, e)
	}
}

func TestUSDTMixedCursorRejectsOwnerPositionAndStateConfusion(t *testing.T) {
	_, _, _, ac, _ := jettonFixture(t, Profile)
	base := usdtHistoryCursor{Version: 2, Account: positionAccount(ac.Network, ac.Address)}
	for _, kind := range []string{"owner", "native done with position", "token done with position", "native complete before done", "token complete before done", "version", "syntax", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			c := base
			switch kind {
			case "owner":
				c.Account = strings.Repeat("0", 64)
			case "native done with position":
				c.NativeDone = true
				c.Native = &transactionPosition{LT: "1", Hash: strings.Repeat("0", 64)}
			case "token done with position":
				c.TokenDone = true
				c.Token = &transactionPosition{LT: "1", Hash: strings.Repeat("0", 64)}
			case "native complete before done":
				c.NativeComplete = true
			case "token complete before done":
				c.TokenComplete = true
			case "version":
				c.Version = 99
			}
			encoded := encodeUSDTPageCursor(c)
			if kind == "syntax" {
				encoded = "!"
			}
			if kind == "oversize" {
				encoded = strings.Repeat("A", 513)
			}
			if _, e := decodeUSDTPageCursor(ac, encoded); e == nil {
				t.Fatal("unbound mixed history cursor accepted")
			}
		})
	}
	for _, pos := range []*transactionPosition{{LT: "1", Hash: strings.Repeat("0", 64), Account: "other"}, {LT: "0", Hash: strings.Repeat("0", 64)}, {LT: "1", Hash: "bad"}} {
		if _, e := checkedCursorPosition(pos, ac.Network, ac.Address); e == nil {
			t.Fatal("invalid archival position accepted", pos)
		}
	}
	if _, e := decodePosition(strings.Repeat("a", 513), ac.Network, ac.Address); e == nil {
		t.Fatal("legacy cursor oversized")
	}
}

func TestUSDTHistoryTokenFailureRetainsNativeAndNeverFabricatesCompletion(t *testing.T) {
	for _, kind := range []string{"network", "native RPC", "master unavailable", "token frozen", "token malformed history", "unknown token position"} {
		t.Run(kind, func(t *testing.T) {
			a, _, _, ac, r := jettonFixture(t, Profile)
			_, _, _, base, _ := receiptFixture(t)
			owner, _ := parseAddress(ac.Network, ac.Address)
			wallet, _ := jettonAddress(owner)
			base.AccountAddr = owner.Data()
			base.IO.In = nil
			base.IO.Out = nil
			base.OutMsgCount = 0
			base.Description = tlb.TransactionDescriptionStorage{StoragePhase: tlb.StoragePhase{StatusChange: tlb.AccStatusChange{Type: tlb.AccStatusChangeUnchanged}}}
			rows := archivalChain(t, base, 1)
			r.transactions[owner.StringRaw()] = []map[string]string{{"data": txBOC(t, rows[0])}}
			cursor := ""
			switch kind {
			case "network":
				r.fail = "getMasterchainInfo"
			case "native RPC":
				r.fail = "getTransactions"
			case "master unavailable":
				st := r.states[USDTMaster]
				st.Code = "invalid"
				r.states[USDTMaster] = st
			case "token frozen":
				st := r.states[wallet.StringRaw()]
				st.State = "frozen"
				r.states[wallet.StringRaw()] = st
			case "token malformed history":
				r.transactions[wallet.StringRaw()] = []map[string]string{{"data": "invalid"}}
			case "unknown token position":
				cursor = encodeUSDTPageCursor(usdtHistoryCursor{Version: 2, Account: positionAccount(ac.Network, ac.Address), NativeDone: true, NativeComplete: true, Token: &transactionPosition{LT: "1", Hash: strings.Repeat("0", 64)}})
			}
			page, e := a.HistoryPage(context.Background(), ac, cursor)
			if kind == "network" || kind == "native RPC" || kind == "unknown token position" {
				if e == nil {
					t.Fatal("invalid chain response did not fail", page)
				}
			} else {
				if e != nil || len(page.Entries) != 1 || page.Complete || page.NextCursor == "" || !strings.Contains(page.Scope, "USDT history is currently unavailable") {
					t.Fatal("token failure hid native activity or claimed completeness", page, e)
				}
				if _, e = a.HistoryPage(context.Background(), ac, page.NextCursor); e == nil {
					t.Fatal("unavailable token-only continuation became successful complete history")
				}
			}
		})
	}
}
