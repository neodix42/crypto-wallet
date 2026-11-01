package evm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func publicIndexHTML(owner string, ids []string) string {
	body := `<html><head><script>doNotExecute()</script></head><body><table><thead><tr><th>Transaction Hash</th><th>From</th><th>To</th></tr></thead><tbody>`
	for _, id := range ids {
		body += `<tr><td><a href="/tx/` + id + `">provider amount 999</a></td><td><span data-full-address="` + owner + `">owner</span></td></tr>`
	}
	if len(ids) == 0 {
		body += `<tr style='visibility:collapse'><td></td></tr><tr><td colspan='10'><div>There are no matching entries</div></td><tr>`
	}
	return body + `</tbody></table></body></html>`
}

func TestPublicHistoryCandidateParserRejectsChallengesMalformedAndUnboundRows(t *testing.T) {
	owner := "0x1111111111111111111111111111111111111111"
	valid := publicIndexHTML(owner, []string{indexedID(1), indexedID(1), indexedID(2)})
	ids, err := publicHistoryIdentities([]byte(valid), owner)
	if err != nil || strings.Join(ids, ",") != strings.Join([]string{indexedID(1), indexedID(1), indexedID(2)}, ",") {
		t.Fatal("exact candidate identities/order were not retained", ids, err)
	}
	ids, err = publicHistoryIdentities([]byte(publicIndexHTML(owner, nil)), owner)
	if err != nil || len(ids) != 0 {
		t.Fatal("explicit empty table rejected", ids, err)
	}
	// The observed native empty table uses a complete row and a secondary hint;
	// the token table above uses a collapsed layout row and implicit terminal row.
	nativeEmpty := `<table><thead><tr><th>Transaction Hash</th><th>From</th><th>To</th></tr></thead><tbody><tr class="border-bottom-0"><td colspan="10"><div><h3>There are no matching entries</h3><p>Please try again later</p></div></td></tr></tbody></table>`
	if ids, err := publicHistoryIdentities([]byte(nativeEmpty), owner); err != nil || len(ids) != 0 {
		t.Fatal("observed native empty placeholder rejected", ids, err)
	}
	for fault, body := range map[string]string{
		"challenge":       `<html><title>Just a moment...</title></html>`,
		"no-empty-proof":  strings.ReplaceAll(publicIndexHTML(owner, nil), "There are no matching entries", ""),
		"wrong-header":    strings.ReplaceAll(valid, "Transaction Hash", "Remote data"),
		"foreign-owner":   strings.ReplaceAll(valid, owner, "0x2222222222222222222222222222222222222222"),
		"invalid-hash":    strings.ReplaceAll(valid, indexedID(1), "bad"),
		"traversal":       strings.ReplaceAll(valid, indexedID(1), indexedID(1)+"/../x"),
		"conflicting-row": strings.Replace(valid, "</td>", `<a href="/tx/`+indexedID(999)+`">bad</a></td>`, 1),
		"unclosed":        strings.ReplaceAll(valid, "</table>", ""),
		"multiple-tables": valid + valid,
		"nested-table":    strings.Replace(valid, "<tbody>", "<tbody><table>", 1),
		"empty-with-ids":  strings.Replace(valid, "<tbody>", "<tbody>There are no matching entries", 1),
		"oversize":        strings.Repeat("x", (2<<20)+1),
	} {
		t.Run(fault, func(t *testing.T) {
			if _, err := publicHistoryIdentities([]byte(body), owner); err == nil {
				t.Fatal("malformed or unbound public source accepted")
			}
		})
	}
	many := make([]string, 26)
	for i := range many {
		many[i] = indexedID(i + 1)
	}
	if _, err := publicHistoryIdentities([]byte(publicIndexHTML(owner, many)), owner); err == nil {
		t.Fatal("public row bound exceeded")
	}
	outside := `<a href="https://other.invalid/tx/` + indexedID(999) + `">outside</a>` + valid
	ids, err = publicHistoryIdentities([]byte(outside), owner)
	if err != nil || len(ids) != 3 {
		t.Fatal("outside link became candidate", ids, err)
	}
}

func TestArbitrumPublicIndexFallbackChecksNativeAndBothDefaultStablecoinEffects(t *testing.T) {
	h := newIndexedHistory(t, "arbitrum-mainnet")
	h.f.rpcError = "alchemy_getAssetTransfers"
	owner := common.BytesToHash(common.HexToAddress(h.account.Address).Bytes()).Hex()
	peer := "0x1111111111111111111111111111111111111111"
	n, _ := h.f.a.network(h.account.Network)
	tokens := n.Assets[2:]
	if len(tokens) != 2 {
		t.Fatal("expected pinned USDT0 and USDC")
	}
	h.receipt = func(id string) any {
		logs := []any{}
		for i, asset := range tokens {
			logs = append(logs, map[string]any{"address": asset.Contract, "topics": []string{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex(), common.BytesToHash(common.HexToAddress(peer).Bytes()).Hex(), owner}, "data": "0x" + hex.EncodeToString(word(big.NewInt(int64(i+1)*100000))), "transactionHash": id, "blockHash": indexedID(500), "blockNumber": "0x1", "logIndex": fmt.Sprintf("0x%x", i), "removed": false})
		}
		return map[string]any{"transactionHash": id, "blockHash": indexedID(500), "blockNumber": "0x1", "status": "0x1", "logs": logs}
	}
	originalIndex := h.index
	h.index = func(r *http.Request) (string, int) {
		if r.URL.Host != "arbiscan.io" {
			return "", 403
		}
		if r.Method != "GET" || r.URL.Query().Get("a") != strings.ToLower(h.account.Address) || r.URL.Query().Get("p") != "1" || r.URL.Query().Get("ps") != "25" || r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "CryptoWallet" || r.URL.Scheme != "https" {
			t.Error("public query changed identity, bounds or authority", r.URL)
		}
		if r.URL.Path != "/txs" && r.URL.Path != "/tokentxns" {
			t.Error("unreviewed explorer path")
		}
		return publicIndexHTML(h.account.Address, []string{indexedID(1)}), 200
	}
	page, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(page.Entries) != 1 || len(page.Entries[0].Transfers) != 3 || page.Complete || page.NextCursor != "" || !strings.Contains(page.Scope, "Arbiscan public") {
		t.Fatal("public fallback lost checked effects", page, err)
	}
	assets := map[string]string{}
	for _, tr := range page.Entries[0].Transfers {
		assets[tr.Asset.ID] = tr.Atomic
	}
	if assets[n.ID+":native"] != "1" || assets[ArbitrumUSDTAsset] != "100000" || assets[ArbitrumUSDCAsset] != "200000" || h.f.calls["eth_getTransactionReceipt"] != 1 || h.f.sent != "" {
		t.Fatal("untrusted HTML metadata or repeated candidate used", assets, h.f.calls)
	}
	// A usable documented API remains preferred; HTML is not contacted in that case.
	h.index = originalIndex
	if _, err = h.f.a.HistoryPage(context.Background(), h.account, ""); err != nil {
		t.Fatal(err)
	}
}

func TestArbitrumPublicIndexPinsContinuationAndRejectsRepeatedPages(t *testing.T) {
	h := newIndexedHistory(t, "arbitrum-mainnet")
	h.f.rpcError = "alchemy_getAssetTransfers"
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = indexedID(i + 1)
	}
	repeated := false
	h.index = func(r *http.Request) (string, int) {
		if r.URL.Host != "arbiscan.io" {
			return "", 403
		}
		if r.URL.Path == "/tokentxns" {
			return publicIndexHTML(h.account.Address, nil), 200
		}
		if r.URL.Query().Get("p") == "1" || repeated {
			return publicIndexHTML(h.account.Address, ids), 200
		}
		return publicIndexHTML(h.account.Address, []string{indexedID(26)}), 200
	}
	first, err := h.f.a.HistoryPage(context.Background(), h.account, "")
	if err != nil || len(first.Entries) != 25 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	var position indexedHistoryCursor
	if json.Unmarshal([]byte(first.NextCursor), &position) != nil || position.Source != publicHistorySource || position.NativeAnchor == "" || !position.TokenDone || position.NativePage != 2 {
		t.Fatal("public source not pinned", first.NextCursor)
	}
	second, err := h.f.a.HistoryPage(context.Background(), h.account, first.NextCursor)
	if err != nil || len(second.Entries) != 1 || second.NextCursor != "" || second.Complete {
		t.Fatal(second, err)
	}
	repeated = true
	if _, err = h.f.a.HistoryPage(context.Background(), h.account, first.NextCursor); err == nil {
		t.Fatal("provider repeated same table forever")
	}
	for _, change := range []func(*indexedHistoryCursor){func(c *indexedHistoryCursor) { c.NativePage = 401 }, func(c *indexedHistoryCursor) { c.NativeAnchor = "malformed" }, func(c *indexedHistoryCursor) { c.FromKey = "foreign-rpc-key" }, func(c *indexedHistoryCursor) { c.Network = "base-mainnet" }, func(c *indexedHistoryCursor) { c.Source = "unreviewed" }} {
		bad := position
		change(&bad)
		raw, _ := json.Marshal(bad)
		if _, err = h.f.a.HistoryPage(context.Background(), h.account, string(raw)); err == nil {
			t.Fatal("invalid public continuation accepted", bad)
		}
	}
}

func TestPublicIndexUnavailableAndRedirectNeverBecomeEmptyHistory(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h := newIndexedHistory(t, "arbitrum-mainnet")
			h.f.rpcError = "alchemy_getAssetTransfers"
			h.index = func(*http.Request) (string, int) { return "PRIVATE-REMOTE-DETAIL", status }
			p, err := h.f.a.HistoryPage(context.Background(), h.account, "")
			if err == nil || len(p.Entries) != 0 || p.Complete || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("public outage became empty success", p, err)
			}
		})
	}
	h := newIndexedHistory(t, "base-mainnet")
	if _, _, _, err := h.f.a.historyPublicCandidates(context.Background(), h.account, 1, false, ""); err == nil || h.indexRequests != 0 {
		t.Fatal("public fallback leaked another network")
	}
	// A redirecting client cannot follow an attacker-controlled Location.
	h = newIndexedHistory(t, "arbitrum-mainnet")
	followed := false
	h.f.a.HTTPClient.Transport = discoveryTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "arbiscan.io" {
			followed = true
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.invalid/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, _, _, err := h.f.a.historyPublicCandidates(context.Background(), h.account, 1, false, "")
	if err == nil || followed {
		t.Fatal("public source followed redirect", err)
	}
}

func TestPublicIndexDoesNotInheritRPCURLCredentialsOrCookies(t *testing.T) {
	h := newIndexedHistory(t, "arbitrum-mainnet")
	h.f.a.endpoints[h.account.Network] = "https://history-rpc.invalid/RPC-CREDENTIAL?apikey=RPC-SECRET"
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse("https://arbiscan.io")
	jar.SetCookies(origin, []*http.Cookie{{Name: "RPC_SESSION", Value: "SECRET", Path: "/"}})
	h.f.a.HTTPClient.Jar = jar
	h.index = func(r *http.Request) (string, int) {
		if r.URL.Host != "arbiscan.io" || strings.Contains(r.URL.String(), "RPC-") || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" || r.Header.Get("Referer") != "" || r.Body != nil || len(r.URL.Query()) != 3 {
			t.Error("RPC authority or session leaked into public index", r.URL, r.Header)
		}
		return publicIndexHTML(h.account.Address, nil), 200
	}
	if _, _, _, err := h.f.a.historyPublicCandidates(context.Background(), h.account, 1, false, ""); err != nil {
		t.Fatal(err)
	}
}

func FuzzPublicHistoryCandidateTable(f *testing.F) {
	owner := "0x1111111111111111111111111111111111111111"
	for _, body := range []string{publicIndexHTML(owner, []string{indexedID(1)}), publicIndexHTML(owner, nil), "<html>Just a moment...</html>", "<table><tbody><tr><td>", "<table><table>", "<script><a href='/tx/bad'>ignored</a></script>"} {
		f.Add([]byte(body))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			return
		}
		ids, err := publicHistoryIdentities(raw, owner)
		if err != nil {
			return
		}
		if len(ids) > 25 {
			t.Fatal("public parser exceeded page bound")
		}
		for _, id := range ids {
			if !hashValid(id) || id != strings.ToLower(id) {
				t.Fatal("public parser accepted invalid identity")
			}
		}
	})
}

func TestPublicHistoryRejectsOwnedRowsWithoutIdentity(t *testing.T) {
	owner := "0x1111111111111111111111111111111111111111"
	valid := publicIndexHTML(owner, []string{indexedID(1)})
	for fault, row := range map[string]string{
		"owned-no-hash":       `<tr><td><span data-full-address="` + owner + `">owner</span></td><td>unrecognized transaction</td></tr>`,
		"owned-absolute-link": `<tr><td><a href="https://other.invalid/tx/` + indexedID(2) + `">tx</a><span data-full-address="` + owner + `">owner</span></td></tr>`,
		"nonempty-no-hash":    `<tr><td>nonempty unknown transaction row</td></tr>`,
		"owned-unterminated":  `<tr><td><span data-full-address="` + owner + `">owner</span></td>`,
	} {
		t.Run(fault, func(t *testing.T) {
			body := strings.Replace(valid, "</tbody>", row+"</tbody>", 1)
			ids, err := publicHistoryIdentities([]byte(body), owner)
			if err == nil {
				t.Fatalf("malformed row silently omitted: returned %d identities, so source reports a short exhausted page", len(ids))
			}
		})
	}
	if ids, err := publicHistoryIdentities([]byte(publicIndexHTML(owner, nil)), owner); err != nil || len(ids) != 0 {
		t.Fatal("actual observed explicit-empty shim broken", ids, err)
	}
}
