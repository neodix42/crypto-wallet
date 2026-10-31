package hypercore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

func mainnetUSDCFixture(t *testing.T) (*coreNode, model.Account, model.Asset) {
	t.Helper()
	f := fakeCore(t)
	var err error
	f.a, err = NewWithEndpoints(map[string]string{"hypercore-mainnet": f.server.URL, "hypercore-mainnet:explorer": f.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	f.override["spotMeta"] = mainnetSpotMetadata()
	f.override["spotClearinghouseState"] = map[string]any{"balances": []any{
		map[string]any{"coin": "HYPE", "token": 150, "total": "0", "hold": "0"},
		map[string]any{"coin": "USDC", "token": 0, "total": "12.34567891", "hold": "2.00000001"},
	}}
	ac, err := f.a.Derive(syntheticSeed(), 0, "hypercore-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := network(ac.Network)
	return f, ac, n.Assets[1]
}
func mainnetSpotMetadata() map[string]any {
	return map[string]any{"tokens": []any{
		map[string]any{"name": "HYPE", "weiDecimals": 8, "index": 150, "tokenId": strings.TrimPrefix(mainToken, "HYPE:")},
		map[string]any{"name": "USDC", "weiDecimals": 8, "index": 0, "tokenId": strings.TrimPrefix(mainUSDCToken, "USDC:"), "isCanonical": true},
	}}
}
func prepareUSDC(t *testing.T, f *coreNode, ac model.Account, asset model.Asset, amount string) (model.Plan, error) {
	t.Helper()
	return f.a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: asset.ID, Recipient: "0x1111111111111111111111111111111111111111", Amount: amount})
}
func TestCoreUSDCSpotBalanceAndRealActionPipeline(t *testing.T) {
	f, ac, asset := mainnetUSDCFixture(t)
	n, _ := network(ac.Network)
	if len(n.Assets) != 2 || n.Assets[0].Contract != mainToken || asset.Contract != mainUSDCToken || asset.Decimals != 8 || asset.ID != ac.Network+":"+mainUSDCToken {
		t.Fatal("default spot registry changed", n.Assets)
	}
	balances, err := f.a.Balance(context.Background(), ac)
	if err != nil || len(balances) != 2 || balances[0].Atomic != "0" || balances[1].Asset != asset.ID || balances[1].Atomic != "1234567891" || balances[1].State != "provider-observed" {
		t.Fatal(balances, err)
	}
	p, err := prepareUSDC(t, f, ac, asset, "1034567890") // exact total minus hold; no HYPE gas debit
	if err != nil {
		t.Fatal(err)
	}
	var payload planPayload
	if json.Unmarshal(p.Payload, &payload) != nil || payload.Action.Token != mainUSDCToken || payload.Action.Amount != "10.3456789" || payload.Action.Type != "spotSend" || p.MaxFee != "0" || p.FeeAsset != asset.ID {
		t.Fatal("wrong actual USDC action", p)
	}
	signed, err := f.a.Sign(syntheticSeed(), p)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := f.a.Submit(context.Background(), p, signed)
	if err != nil || receipt.State != "SUBMITTED" || f.submitCount != 1 || f.action.Token != mainUSDCToken {
		t.Fatal(receipt, err)
	}
	f.owner = ac.Address
	for _, token := range []string{mainUSDCToken, "USDC"} {
		f.override["userNonFundingLedgerUpdates"] = []any{map[string]any{"time": f.action.Time, "hash": f.hash, "delta": map[string]any{"type": "spotTransfer", "token": token, "amount": f.action.Amount, "user": f.owner, "destination": p.Recipient, "fee": "0", "nativeTokenFee": "0"}}}
		receipt, err = f.a.Reconcile(context.Background(), p, signed)
		if err != nil || receipt.State != "CONFIRMED" {
			t.Fatal("exact USDC ledgers did not reconcile", receipt, err)
		}
	}
	if _, err = prepareUSDC(t, f, ac, asset, "1034567891"); err == nil {
		t.Fatal("held USDC was spendable")
	}
}
func TestCoreUSDCNeverAuthorizesAnotherLedgerTokenOrFee(t *testing.T) {
	f, ac, asset := mainnetUSDCFixture(t)
	p, err := prepareUSDC(t, f, ac, asset, "1")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*model.Plan, *planPayload){
		func(p *model.Plan, x *planPayload) { x.Action.Token = mainToken },
		func(p *model.Plan, x *planPayload) { x.Action.Token = "USDC:0xeb62eee3685fc4c43992febcd9e75443" },
		func(p *model.Plan, x *planPayload) { x.Action.Type = "usdSend" },
		func(p *model.Plan, x *planPayload) { x.Action.Type = "sendAsset" },
		func(p *model.Plan, x *planPayload) { x.Action.HyperliquidChain = "Testnet" },
		func(p *model.Plan, x *planPayload) { x.Action.Amount = "0.000001" }, // 6-decimal ERC-20 confusion
		func(p *model.Plan, x *planPayload) {
			p.Asset = "hyperevm-mainnet:erc20:0xb88339CB7199b77E23DB6E890353E22632Ba630f"
		},
		func(p *model.Plan, x *planPayload) { p.MaxFee = "100000000" },
		func(p *model.Plan, x *planPayload) { p.FeeAsset = "hypercore-mainnet:" + mainToken },
		func(p *model.Plan, x *planPayload) { x.RecipientRole = "missing" },
		func(p *model.Plan, x *planPayload) { x.SourceAbstraction = "unifiedAccount" },
	} {
		q := p
		var x planPayload
		_ = json.Unmarshal(p.Payload, &x)
		mutate(&q, &x)
		q.Payload, _ = json.Marshal(x)
		q.ID = planID(q)
		if f.a.ValidatePlan(q) == nil {
			t.Fatal("unapproved USDC effects accepted", q)
		}
		if _, err = f.a.Sign(syntheticSeed(), q); err == nil {
			t.Fatal("unapproved USDC effects signed")
		}
	}
	f.intercept = func(typ string, req map[string]json.RawMessage) (any, bool) {
		if typ == "userRole" && string(req["user"]) == `"0x1111111111111111111111111111111111111111"` {
			return map[string]string{"role": "missing"}, true
		}
		return nil, false
	}
	if _, err = prepareUSDC(t, f, ac, asset, "1"); err == nil {
		t.Fatal("unpriced new-account activation debit approved")
	}
	if f.submitCount != 0 {
		t.Fatal("preflight broadcast funds")
	}
}
func TestCoreUSDCRegistryAndBalanceFailuresStayUnknownPerAsset(t *testing.T) {
	for _, field := range []string{"tokenId", "index", "name", "weiDecimals", "isCanonical", "duplicate-index", "duplicate-id", "duplicate-ticker"} {
		t.Run(field, func(t *testing.T) {
			f, ac, asset := mainnetUSDCFixture(t)
			metadata := mainnetSpotMetadata()
			tokens := metadata["tokens"].([]any)
			usd := tokens[1].(map[string]any)
			switch field {
			case "tokenId":
				usd[field] = "0xeb62eee3685fc4c43992febcd9e75443"
			case "index":
				usd[field] = 1
			case "name":
				usd[field] = "USDT"
			case "weiDecimals":
				usd[field] = 6
			case "isCanonical":
				usd[field] = false
			case "duplicate-index":
				tokens = append(tokens, map[string]any{"name": "FAKE", "index": 0, "tokenId": "0x111"})
			case "duplicate-id":
				tokens = append(tokens, usd)
			case "duplicate-ticker":
				tokens = append(tokens, map[string]any{"name": "USDC", "index": 9999, "tokenId": "0x111"})
			}
			metadata["tokens"] = tokens
			f.override["spotMeta"] = metadata
			rows, err := f.a.Balance(context.Background(), ac)
			if err != nil || len(rows) != 2 || rows[0].State != "provider-observed" || rows[1].State != "unavailable" || rows[1].Atomic != "" {
				t.Fatal("USDC metadata failure hid HYPE or invented zero", rows, err)
			}
			if _, err = prepareUSDC(t, f, ac, asset, "1"); err == nil {
				t.Fatal("unverified token prepared")
			}
		})
	}
	for _, bad := range []any{
		map[string]any{"coin": "USDT", "token": 0, "total": "10", "hold": "0"},
		map[string]any{"coin": "USDC", "token": 0, "total": "0.000000001", "hold": "0"},
		map[string]any{"coin": "USDC", "token": 0, "total": "1", "hold": "2"},
	} {
		f, ac, asset := mainnetUSDCFixture(t)
		f.override["spotClearinghouseState"] = map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 150, "total": "1", "hold": "0"}, bad}}
		rows, err := f.a.Balance(context.Background(), ac)
		if err != nil || rows[0].Atomic != "100000000" || rows[1].State != "unavailable" {
			t.Fatal(rows, err)
		}
		if _, err = prepareUSDC(t, f, ac, asset, "1"); err == nil {
			t.Fatal("invalid USDC balance prepared")
		}
	}
}
func TestCoreUSDCReceiveAndHistoryUseOwnedAccountAndExactTokenPrecision(t *testing.T) {
	f, ac, asset := mainnetUSDCFixture(t)
	evmAccount, err := f.a.Derive(syntheticSeed(), 0, "hypercore-mainnet")
	if err != nil || evmAccount.Address != ac.Address || f.a.ValidateAddress(ac.Network, ac.Address) != nil || strings.Contains(ac.Address, asset.Contract) {
		t.Fatal("USDC receive address differs from owner", err)
	}
	at := uint64(time.Now().UnixMilli())
	events := []any{}
	for i, token := range []string{"USDC", mainUSDCToken, "HYPE", mainToken} {
		events = append(events, map[string]any{"time": at, "hash": "0x" + strings.Repeat(string(rune('a'+i)), 64), "delta": map[string]any{"type": "spotTransfer", "token": token, "amount": "0.00000001", "user": ac.Address, "destination": "0x1111111111111111111111111111111111111111", "fee": "0", "nativeTokenFee": "0"}})
	}
	f.override["userNonFundingLedgerUpdates"] = events
	rows, err := f.a.History(context.Background(), ac)
	if err != nil || len(rows) != 4 {
		t.Fatal(rows, err)
	}
	for i, row := range rows {
		if row.State != "OBSERVED" || row.Transfers[0].Atomic != "1" || row.Transfers[0].Asset.Decimals != 8 || row.Transfers[0].Source != ac.Address {
			t.Fatal("history atomic/source changed", row)
		}
		if i < 2 && row.Transfers[0].Asset != asset {
			t.Fatal("USDC history relabeled", row)
		}
	}
	// HYPE availability cannot turn missing USDC history metadata into successful emptiness.
	metadata := mainnetSpotMetadata()
	metadata["tokens"] = metadata["tokens"].([]any)[:1]
	f.override["spotMeta"] = metadata
	if _, err = f.a.History(context.Background(), ac); err == nil {
		t.Fatal("unverified USDC history scope accepted")
	}
}
func TestCoreLegacyHYPEPlanAndSignatureSurviveUSDCAddition(t *testing.T) {
	f, _, _ := mainnetUSDCFixture(t)
	p := fixturePlan(t, f.a, "hypercore-mainnet")
	var x planPayload
	_ = json.Unmarshal(p.Payload, &x)
	if x.Action.Token != mainToken || p.FeeAsset != p.Asset {
		t.Fatal("legacy HYPE plan changed")
	}
	signed, err := f.a.Sign(syntheticSeed(), p)
	if err != nil {
		t.Fatal(err)
	}
	// Reopening an old signed HYPE operation does not require new USDC metadata.
	metadata := mainnetSpotMetadata()
	metadata["tokens"] = metadata["tokens"].([]any)[:1]
	f.override["spotMeta"] = metadata
	receipt, err := f.a.Submit(context.Background(), p, signed)
	if err != nil || receipt.State != "SUBMITTED" || f.action != x.Action {
		t.Fatal("old HYPE payload no longer submitted exactly", receipt, err)
	}
}
func TestCoreUSDCOfficialSDKIndependentSignatures(t *testing.T) {
	raw, err := os.ReadFile("testdata/usdc-sdk.json")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Oracle   string `json:"oracle"`
		Fixtures []struct {
			Index     uint32    `json:"index"`
			Action    action    `json:"action"`
			Address   string    `json:"address"`
			Hash      string    `json:"hash"`
			Signature signature `json:"signature"`
		} `json:"fixtures"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Oracle != "hyperliquid-python-sdk 0.24.0" || len(data.Fixtures) != 3 {
		t.Fatal("missing independent SDK provenance")
	}
	for _, f := range data.Fixtures {
		ac, err := New().Derive(syntheticSeed(), f.Index, "hypercore-mainnet")
		if err != nil || ac.Address != f.Address || f.Action.Token != mainUSDCToken || f.Action.Amount != "0.12345678" {
			t.Fatal("independent owner/token mismatch", err)
		}
		hash, err := typedHash(f.Action)
		if err != nil || "0x"+hex.EncodeToString(hash) != f.Hash {
			t.Fatal("independent EIP712 hash differs", err)
		}
		key, err := deriveKey(syntheticSeed(), f.Index)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := crypto.Sign(hash, key)
		destroyKey(key)
		if err != nil {
			t.Fatal(err)
		}
		got := signature{"0x" + new(big.Int).SetBytes(sig[:32]).Text(16), "0x" + new(big.Int).SetBytes(sig[32:64]).Text(16), sig[64] + 27}
		wipe(sig)
		if got != f.Signature {
			t.Fatal("official SDK signature differs", f.Index)
		}
	}
}

func TestCoreUSDCDifferentLedgerEffectsNeverReleaseReservation(t *testing.T) {
	f, ac, asset := mainnetUSDCFixture(t)
	p, err := prepareUSDC(t, f, ac, asset, "12345678")
	if err != nil {
		t.Fatal(err)
	}
	signed, err := f.a.Sign(syntheticSeed(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.Submit(context.Background(), p, signed); err != nil {
		t.Fatal(err)
	}
	f.owner = ac.Address
	for _, field := range []string{"token", "amount", "fee", "nativeTokenFee", "destination"} {
		delta := map[string]any{"type": "spotTransfer", "token": "USDC", "amount": "0.12345678", "user": ac.Address, "destination": p.Recipient, "fee": "0", "nativeTokenFee": "0"}
		switch field {
		case "token":
			delta[field] = "HYPE"
		case "amount":
			delta[field] = "12.345678"
		case "fee", "nativeTokenFee":
			delta[field] = "0.00000001"
		case "destination":
			delta[field] = "0x3333333333333333333333333333333333333333"
		}
		f.override["userNonFundingLedgerUpdates"] = []any{map[string]any{"time": f.action.Time, "hash": f.hash, "delta": delta}}
		receipt, err := f.a.Reconcile(context.Background(), p, signed)
		if err != nil || receipt.State != "UNKNOWN" {
			t.Fatal("different USDC effect confirmed", field, receipt, err)
		}
	}
	if f.submitCount != 1 {
		t.Fatal("reconciliation resubmitted signed action")
	}
}
func TestCoreUSDCAbsenceZeroAndFailureAreDistinct(t *testing.T) {
	f, ac, _ := mainnetUSDCFixture(t)
	f.override["spotClearinghouseState"] = map[string]any{"balances": []any{}}
	rows, err := f.a.Balance(context.Background(), ac)
	if err != nil || len(rows) != 2 || rows[1].Atomic != "0" || rows[1].State != "provider-observed" {
		t.Fatal("verified absent spot token not zero", rows, err)
	}
	f.override["spotClearinghouseState"] = map[string]any{"balances": nil}
	if _, err = f.a.Balance(context.Background(), ac); err == nil {
		t.Fatal("missing state became zero")
	}
	n, _ := network("hypercore-testnet")
	if len(n.Assets) != 1 {
		t.Fatal("mainnet USDC leaked to testnet")
	}
	if _, err = assetByID(n, "hypercore-mainnet:"+mainUSDCToken); err == nil {
		t.Fatal("foreign ledger identity accepted")
	}
}

func TestCoreMaximumSpendableIsExactSelectedAssetAndFailsOnUnknownFunds(t *testing.T) {
	f, ac, asset := mainnetUSDCFixture(t)
	available, err := f.a.SpendableBalance(context.Background(), ac, asset.ID)
	if err != nil || available != "1034567890" {
		t.Fatal("Core order holds remained spendable", available, err)
	}
	for _, id := range []string{"USDC", "hypercore-testnet:" + mainUSDCToken, "hyperevm-mainnet:erc20:USDC"} {
		if _, err = f.a.SpendableBalance(context.Background(), ac, id); err == nil {
			t.Fatal("foreign maximum asset accepted", id)
		}
	}
	bad := ac
	bad.PublicKey = "00"
	if _, err = f.a.SpendableBalance(context.Background(), bad, asset.ID); err == nil {
		t.Fatal("unowned maximum account accepted")
	}
	f.fail = "spotMeta"
	if _, err = f.a.SpendableBalance(context.Background(), ac, asset.ID); err == nil {
		t.Fatal("unverified token provided spendable amount")
	}
	f.fail = "spotClearinghouseState"
	if _, err = f.a.SpendableBalance(context.Background(), ac, asset.ID); err == nil {
		t.Fatal("RPC failure became spendable zero")
	}
	f.fail = ""
	for _, r := range []struct{ total, hold, want string }{{"10", "10", "0"}, {"10", "9.99999999", "1"}} {
		f.override["spotClearinghouseState"] = map[string]any{"balances": []any{map[string]any{"coin": "USDC", "token": 0, "total": r.total, "hold": r.hold}}}
		available, err = f.a.SpendableBalance(context.Background(), ac, asset.ID)
		if err != nil || available != r.want {
			t.Fatal("hold precision or zero altered", available, err)
		}
	}
	f.override["spotClearinghouseState"] = map[string]any{"balances": []any{map[string]any{"coin": "USDC", "token": 0, "total": "1", "hold": "2"}}}
	if _, err = f.a.SpendableBalance(context.Background(), ac, asset.ID); err == nil {
		t.Fatal("invalid held balance accepted")
	}
}

func TestCoreUSDCZeroIndexMustBeExplicitInMetadataAndBalances(t *testing.T) {
	for _, field := range []string{"metadata-missing", "metadata-null", "balance-missing", "balance-null", "balance-foreign-index"} {
		t.Run(field, func(t *testing.T) {
			f, ac, asset := mainnetUSDCFixture(t)
			if strings.HasPrefix(field, "metadata") {
				metadata := mainnetSpotMetadata()
				entry := metadata["tokens"].([]any)[1].(map[string]any)
				if field == "metadata-missing" {
					delete(entry, "index")
				} else {
					entry["index"] = nil
				}
				f.override["spotMeta"] = metadata
			} else {
				entry := map[string]any{"coin": "USDC", "total": "12", "hold": "1"}
				if field == "balance-null" {
					entry["token"] = nil
				}
				if field == "balance-foreign-index" {
					entry["token"] = 999
				}
				f.override["spotClearinghouseState"] = map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 150, "total": "1", "hold": "0"}, entry}}
			}
			balances, err := f.a.Balance(context.Background(), ac)
			if err != nil || len(balances) != 2 || balances[0].State != "provider-observed" || balances[1].State != "unavailable" || balances[1].Atomic != "" {
				t.Fatal("missing/null/foreign USDC index became verified zero or units", balances, err)
			}
			if _, err = f.a.SpendableBalance(context.Background(), ac, asset.ID); err == nil {
				t.Fatal("missing USDC index authorized Max")
			}
			if _, err = prepareUSDC(t, f, ac, asset, "1"); err == nil {
				t.Fatal("missing USDC index authorized actual transfer")
			}
			if f.submitCount != 0 {
				t.Fatal("malformed index query sent funds")
			}
		})
	}
}
