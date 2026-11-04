package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func withdrawalViewRecord() map[string]any {
	return map[string]any{"withdrawId": "remote-withdrawal-1", "coin": "USDT", "chain": "TON", "amount": "2.9", "withdrawFee": "0.1", "status": "Pending", "toAddress": "UQ_PUBLIC_SYNTHETIC_OWNER", "tag": "", "txID": "", "createTime": "1793788800000", "updateTime": "1793788801000", "withdrawType": 0}
}

func withdrawalAddressViewRecord() map[string]any {
	return map[string]any{"coin": "baseCoin", "chain": "TON", "address": "UQ_PUBLIC_SYNTHETIC_OWNER", "tag": "", "status": 1, "verified": 0, "addressType": 0, "remark": "PRIVATE_REMARK_CANARY", "createdAt": "1760951195"}
}

func withdrawalViewResponse(row map[string]any, cursor string) string {
	raw, _ := json.Marshal(map[string]any{"rows": []any{row}, "nextPageCursor": cursor})
	return string(raw)
}

func TestWithdrawalViewsUseOnlyBoundedReadOnlyQueriesAndDiscardRemarks(t *testing.T) {
	for _, kind := range []string{"history", "addresses"} {
		t.Run(kind, func(t *testing.T) {
			row := withdrawalViewRecord()
			path, query := "/v5/asset/withdraw/query-record", "limit=50&withdrawType=0"
			if kind == "addresses" {
				row, path, query = withdrawalAddressViewRecord(), "/v5/asset/withdraw/query-address", "addressType=0&chain=TON&limit=50"
			}
			client := testClient(t, "{}")
			calls := 0
			client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != "GET" || request.URL.Path != path || request.URL.RawQuery != query || request.Header.Get("X-BAPI-SIGN") == "" || request.Header.Get("X-BAPI-API-KEY") != fixtureKey {
					t.Fatal("read view changed endpoint/query/authentication or attempted a write", request.Method, request.URL.Path)
				}
				return resultResponse(withdrawalViewResponse(row, "")), nil
			})
			var result any
			var err error
			if kind == "history" {
				result, err = client.RecentWithdrawalRecords(context.Background(), "")
			} else {
				result, err = client.WithdrawalAddresses(context.Background(), "TON", "")
			}
			if err != nil || calls != 1 {
				t.Fatal("valid documented read-only view failed", err, calls)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "PRIVATE_REMARK_CANARY") || strings.Contains(string(raw), "createdAt") {
				t.Fatal("unneeded address-book remark/timestamp was retained")
			}
		})
	}
}

func TestRecentWithdrawalViewRejectsMalformedOrAmbiguousRecords(t *testing.T) {
	for _, scenario := range []string{"missing-time", "null-status", "shadow-status", "shadow-id", "unknown-status", "reversed-time", "noncanonical-time", "offchain", "zero-amount", "invalid-fee", "control-address", "repeated-cursor", "duplicate-id"} {
		t.Run(scenario, func(t *testing.T) {
			row, cursor := withdrawalViewRecord(), ""
			switch scenario {
			case "missing-time":
				delete(row, "createTime")
			case "null-status":
				row["status"] = nil
			case "shadow-status":
				row["Status"] = "success"
			case "shadow-id":
				row["WithdrawId"] = "other-id"
			case "unknown-status":
				row["status"] = "PRIVATE_STATUS_CANARY"
			case "reversed-time":
				row["updateTime"] = "1793788799999"
			case "noncanonical-time":
				row["createTime"] = "01793788800000"
			case "offchain":
				row["withdrawType"] = 1
			case "zero-amount":
				row["amount"] = "0"
			case "invalid-fee":
				row["withdrawFee"] = "NaN"
			case "control-address":
				row["toAddress"] = "private\naddress"
			case "repeated-cursor":
				cursor = "cursor-1"
			}
			body := withdrawalViewResponse(row, cursor)
			if scenario == "duplicate-id" {
				raw, _ := json.Marshal(map[string]any{"rows": []any{row, row}, "nextPageCursor": ""})
				body = string(raw)
			}
			_, err := testClient(t, body).RecentWithdrawalRecords(context.Background(), cursor)
			if !errors.Is(err, ErrInvalidResponse) || strings.Contains(err.Error(), "PRIVATE_") {
				t.Fatal("malformed read record was accepted or provider value entered diagnostics", err)
			}
		})
	}
}

func TestWithdrawalAddressViewRejectsInvalidEligibilityWithoutUsingDefaultZero(t *testing.T) {
	for _, scenario := range []string{"missing-status", "null-status", "shadow-status", "invalid-status", "invalid-verified", "internal-address", "wrong-chain", "null-tag", "wrong-coin", "repeated-cursor"} {
		t.Run(scenario, func(t *testing.T) {
			row, cursor := withdrawalAddressViewRecord(), ""
			switch scenario {
			case "missing-status":
				delete(row, "status")
			case "null-status":
				row["status"] = nil
			case "shadow-status":
				row["Status"] = 0
			case "invalid-status":
				row["status"] = 2
			case "invalid-verified":
				row["verified"] = 2
			case "internal-address":
				row["addressType"] = 1
			case "wrong-chain":
				row["chain"] = "ETH"
			case "null-tag":
				row["tag"] = nil
			case "wrong-coin":
				row["coin"] = "usdt"
			case "repeated-cursor":
				cursor = "cursor-1"
			}
			_, err := testClient(t, withdrawalViewResponse(row, cursor)).WithdrawalAddresses(context.Background(), "TON", cursor)
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatal("malformed whitelist data became normal/unlocked eligibility", err)
			}
		})
	}
}

func TestWithdrawalViewAuthenticationPolicyCannotBroadenIntoOtherRequests(t *testing.T) {
	for _, query := range []string{"limit=50&withdrawType=0", "cursor=next&limit=50&withdrawType=0"} {
		if ValidateAuthRequest(readRequest("/v5/asset/withdraw/query-record", query)) != nil {
			t.Fatal("fixed recent withdrawal query refused", query)
		}
	}
	for _, query := range []string{"addressType=0&chain=TON&limit=50", "addressType=0&chain=TON&cursor=next&limit=50"} {
		if ValidateAuthRequest(readRequest("/v5/asset/withdraw/query-address", query)) != nil {
			t.Fatal("fixed on-chain address query refused", query)
		}
	}
	for _, query := range []string{"limit=10&withdrawType=0", "limit=50&withdrawType=2", "coin=USDT&limit=50&withdrawType=0", "limit=50&requestId=guess&withdrawType=0", "addressType=1&chain=TON&limit=50", "addressType=0&chain=TON&limit=50&remark=private", "addressType=0&chain=TON&limit=50&coin=baseCoin"} {
		path := "/v5/asset/withdraw/query-record"
		if strings.Contains(query, "addressType") {
			path = "/v5/asset/withdraw/query-address"
		}
		if ValidateAuthRequest(readRequest(path, query)) == nil {
			t.Fatal("read-only view allowed an unreviewed query or heuristic request-ID lookup", query)
		}
	}
}
