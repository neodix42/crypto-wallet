package kucoin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
)

// Actual TLS/HTTP with synthetic public credentials; no live account or funds.
type fundingFixtureTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (f fundingFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.URL.Scheme, copy.URL.Host = f.target.Scheme, f.target.Host
	return f.base.RoundTrip(copy)
}
func fundingFixture(t *testing.T, handler func(http.ResponseWriter, *http.Request, string)) *Client {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		prehash := r.Header.Get("KC-API-TIMESTAMP") + r.Method + r.URL.RequestURI() + string(body)
		mac := hmac.New(sha256.New, []byte("public-synthetic-api-secret"))
		mac.Write([]byte(prehash))
		if r.Header.Get("KC-API-SIGN") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) || r.Header.Get("KC-API-KEY") != "public-synthetic-api-key" || r.Header.Get("KC-API-KEY-VERSION") != "3" {
			t.Error("request authentication did not bind exact wire bytes")
		}
		mac.Reset()
		mac.Write([]byte("public-synthetic-api-passphrase"))
		if r.Header.Get("KC-API-PASSPHRASE") != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Error("plaintext or invalid API passphrase header")
		}
		handler(w, r, string(body))
	}))
	t.Cleanup(server.Close)
	client, err := New(endpoint, func(_ context.Context, request bybit.AuthRequest) (bybit.Auth, error) {
		return HMAC("public-synthetic-api-key", []byte("public-synthetic-api-secret"), []byte("public-synthetic-api-passphrase"), 3, request)
	})
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(server.URL)
	client.http.Transport = fundingFixtureTransport{target, server.Client().Transport}
	t.Cleanup(client.Close)
	return client
}

func fixtureWithdrawal() bybit.WithdrawalRequest {
	return bybit.WithdrawalRequest{Coin: "ETH", Chain: "eth", Address: "0x1111111111111111111111111111111111111111", Amount: "0.1", Timestamp: time.Now().UnixMilli(), ForceChain: 1, AccountType: "FUND", FeeType: 0, RequestID: strings.Repeat("a", 32), FeeMode: "kucoin-external"}
}

func TestKuCoinFundingWritesBindExactWireAndAcknowledgementsRemainSeparate(t *testing.T) {
	posts := 0
	client := fundingFixture(t, func(w http.ResponseWriter, r *http.Request, body string) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/ua/v2/account/mode" {
			io.WriteString(w, `{"code":"200000","data":{"selfAccountMode":"CLASSIC"}}`)
			return
		}
		if r.Method != "POST" {
			t.Error("unexpected method", r.Method)
		}
		posts++
		switch r.URL.Path {
		case "/api/v3/accounts/universal-transfer":
			if body != `{"clientOid":"11111111-1111-4111-8111-111111111111","type":"INTERNAL","currency":"ETH","amount":"0.1","fromAccountType":"MAIN","toAccountType":"TRADE"}` {
				t.Error("altered transfer", body)
			}
			io.WriteString(w, `{"code":"200000","data":{"orderId":"exact-transfer-ack"}}`)
		case "/api/v3/withdrawals":
			var wire map[string]any
			json.Unmarshal([]byte(body), &wire)
			if wire["currency"] != "ETH" || wire["amount"] != "0.1" || wire["toAddress"] != fixtureWithdrawal().Address || wire["chain"] != "eth" || wire["feeDeductType"] != "EXTERNAL" || wire["withdrawType"] != "ADDRESS" || wire["isInner"] != false || wire["remark"] != strings.Repeat("a", 32) || len(wire) != 8 {
				t.Error("altered withdrawal", body)
			}
			io.WriteString(w, `{"code":"200000","data":{"withdrawalId":"exact-withdrawal-ack"}}`)
		default:
			t.Error("unapproved endpoint", r.URL.Path)
		}
	})
	transfer := bybit.TransferRequest{TransferID: "11111111-1111-4111-8111-111111111111", Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
	ack, err := client.Transfer(context.Background(), transfer)
	if err != nil || ack.TransferID != "exact-transfer-ack" || ack.Status != "PENDING" {
		t.Fatal("ack became finality", ack, err)
	}
	if row, err := client.TransferStatusFor(context.Background(), transfer, ack.TransferID); err != nil || row != nil {
		t.Fatal("invented transfer history", row, err)
	}
	withdrawal, err := client.Withdraw(context.Background(), fixtureWithdrawal())
	if err != nil || withdrawal.ID != "exact-withdrawal-ack" || posts != 2 {
		t.Fatal(withdrawal, err, posts)
	}
}

func TestKuCoinFundingNeverPostsForInvalidInputOrUnifiedAccount(t *testing.T) {
	posts := 0
	client := fundingFixture(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Method == "POST" {
			posts++
		}
		io.WriteString(w, `{"code":"200000","data":{"selfAccountMode":"UNIFIED"}}`)
	})
	request := fixtureWithdrawal()
	if _, err := client.Withdraw(context.Background(), request); !errors.Is(err, ErrAccountMode) {
		t.Fatal(err)
	}
	request.FeeMode = ""
	if _, err := client.Withdraw(context.Background(), request); !errors.Is(err, ErrRequest) {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatal("invalid/account-mode request moved funds")
	}
	if _, err := client.TransferStatus(context.Background(), "not-an-original-id"); err == nil {
		t.Fatal("invalid transfer identity accepted")
	}
}

func TestKuCoinFundingFailedOrIncompleteAcknowledgementsNeverRetryOrInventAnIdentity(t *testing.T) {
	for _, kind := range []string{"transfer", "withdraw"} {
		for _, reply := range []string{`{"code":"200000","data":{}}`, `{"code":"200000","data":{"orderId":"bad/path","withdrawalId":"bad/path"}}`, `{"code":"400100","msg":"private-provider-canary","data":null}`, `{"code":"200000","data":null}`} {
			t.Run(kind+reply, func(t *testing.T) {
				posts := 0
				client := fundingFixture(t, func(w http.ResponseWriter, r *http.Request, _ string) {
					if r.Method == "GET" {
						io.WriteString(w, `{"code":"200000","data":{"selfAccountMode":"CLASSIC"}}`)
						return
					}
					posts++
					io.WriteString(w, reply)
				})
				var err error
				if kind == "transfer" {
					ack, e := client.Transfer(context.Background(), bybit.TransferRequest{TransferID: "11111111-1111-4111-8111-111111111111", Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"})
					err = e
					if ack.TransferID != "" {
						t.Fatal("invented acknowledgement", ack)
					}
				} else {
					ack, e := client.Withdraw(context.Background(), fixtureWithdrawal())
					err = e
					if ack.ID != "" {
						t.Fatal("invented acknowledgement", ack)
					}
				}
				if err == nil || strings.Contains(err.Error(), "private-provider-canary") || posts != 1 {
					t.Fatal("missing identity, provider error leakage or automatic retry", err, posts)
				}
			})
		}
	}
}

func fundingHistoryRow() map[string]any {
	return map[string]any{"id": "original-ack", "currency": "ETH", "chainId": "eth", "status": "SUCCESS", "address": fixtureWithdrawal().Address, "memo": "", "isInner": false, "amount": "0.1", "fee": "0.0002", "walletTxId": "0x" + strings.Repeat("a", 64), "txId": nil, "remark": strings.Repeat("a", 32), "createdAt": time.Now().Add(-time.Minute).UnixMilli(), "returnStatus": "NOT_RETURN", "taxes": nil}
}

func TestKuCoinWithdrawalHistoryUsesOnlyExactAcknowledgedIdentity(t *testing.T) {
	row := fundingHistoryRow()
	client := fundingFixture(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/withdrawals/original-ack" || r.URL.RawQuery != "" {
			t.Error("unsafe withdrawal search", r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": "200000", "data": row})
	})
	page, err := client.WithdrawalRecords(context.Background(), "ETH", "original-ack", "")
	if err != nil || len(page.Rows) != 1 || page.Rows[0].Status != "success" || page.Rows[0].ToAddress != fixtureWithdrawal().Address || page.Rows[0].WithdrawFee != "0.0002" || page.Rows[0].TxID != row["walletTxId"] || page.NextPageCursor != "" {
		t.Fatal(page, err)
	}
	for _, status := range []string{"PROCESSING", "FAILURE"} {
		row["status"], row["walletTxId"] = status, nil
		if _, err = client.WithdrawalRecords(context.Background(), "ETH", "original-ack", ""); err != nil {
			t.Fatal(status, err)
		}
	}
	for _, args := range [][3]string{{"ETH", "", ""}, {"ETH", "original-ack", "next"}, {"ETH", "../other", ""}} {
		if _, err = client.WithdrawalRecords(context.Background(), args[0], args[1], args[2]); err == nil {
			t.Fatal("broad history lookup accepted", args)
		}
	}
}

func TestKuCoinWithdrawalHistoryRejectsAmbiguousOrIncompleteObservations(t *testing.T) {
	for _, change := range []struct {
		name, key string
		value     any
	}{
		{"identity", "id", "different"}, {"currency", "currency", "USDT"}, {"status", "status", "UNREVIEWED"}, {"internal", "isInner", true}, {"missingFee", "fee", nil}, {"negativeFee", "fee", "-1"}, {"zeroAmount", "amount", "0"}, {"tax", "taxes", []any{map[string]any{"currency": "KCS", "amount": "1"}}}, {"return", "returnStatus", "RETURNED"}, {"future", "createdAt", time.Now().Add(time.Hour).UnixMilli()}, {"noTx", "walletTxId", nil}, {"txConflict", "txId", "different-chain-id"}, {"controlText", "address", "private-canary\n"},
	} {
		t.Run(change.name, func(t *testing.T) {
			row := fundingHistoryRow()
			row[change.key] = change.value
			client := fundingFixture(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				json.NewEncoder(w).Encode(map[string]any{"code": "200000", "data": row})
			})
			if _, err := client.WithdrawalRecords(context.Background(), "ETH", "original-ack", ""); !errors.Is(err, ErrResponse) || strings.Contains(err.Error(), "private-canary") {
				t.Fatal("unbound/incomplete withdrawal accepted", err)
			}
		})
	}
}
