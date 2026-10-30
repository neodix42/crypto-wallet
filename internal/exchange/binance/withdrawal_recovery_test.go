package binance

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestWithdrawalRecoveryUsesExactOriginalIDAndBoundedHistoricalWindow(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(strconv.FormatBool(acknowledged), func(t *testing.T) {
			request := withdrawal()
			remote := ""
			if acknowledged {
				remote = "remoteWithdrawal1"
			}
			calls := 0
			c := testClient(t, func(r *http.Request) string {
				calls++
				q := r.URL.Query()
				if r.Method != "GET" || r.URL.Path != "/sapi/v1/capital/withdraw/history" || q.Get("coin") != request.Coin || q.Get("limit") != "50" || q.Get("offset") != "0" {
					t.Fatal("wrong recovery descriptor", r.URL)
				}
				if acknowledged {
					if q.Get("idList") != remote || q.Has("withdrawOrderId") || q.Has("startTime") || q.Has("endTime") {
						t.Fatal("acknowledged server ID replaced", q)
					}
				} else if q.Get("withdrawOrderId") != request.RequestID || q.Has("idList") || q.Get("startTime") != "1699913600000" || q.Get("endTime") != "1700086400000" {
					t.Fatal("original identity or historical interval changed", q)
				}
				var rows []map[string]any
				if json.Unmarshal([]byte(fixtureWithdrawal), &rows) != nil {
					t.Fatal("invalid fixture")
				}
				rows[0]["withdrawOrderId"] = request.RequestID
				body, _ := json.Marshal(rows)
				return string(body)
			})
			page, err := c.WithdrawalRecordsFor(context.Background(), request, remote, "")
			if err != nil || calls != 1 || len(page.Rows) != 1 || page.Rows[0].WithdrawID != "remoteWithdrawal1" || page.Rows[0].RequestID != request.RequestID || page.Rows[0].ToAddress != request.Address {
				t.Fatal("original-only recovery failed", page, err, calls)
			}
		})
	}
}

func TestWithdrawalRecoveryRejectsMissingForeignDuplicateAndUnownedRecords(t *testing.T) {
	for _, mode := range []string{"missingOriginal", "foreignOriginal", "foreignServer", "chain", "address", "memo", "amount", "internal", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			request := withdrawal()
			c := testClient(t, func(*http.Request) string {
				var rows []map[string]any
				if json.Unmarshal([]byte(fixtureWithdrawal), &rows) != nil {
					t.Fatal("invalid fixture")
				}
				row := rows[0]
				row["withdrawOrderId"] = request.RequestID
				switch mode {
				case "missingOriginal":
					delete(row, "withdrawOrderId")
				case "foreignOriginal":
					row["withdrawOrderId"] = "different-original-identity"
				case "foreignServer":
					row["id"] = "different-server"
				case "chain":
					row["network"] = "BASE"
				case "address":
					row["address"] = "0x2222222222222222222222222222222222222222"
				case "memo":
					row["addressTag"] = "foreign-memo"
				case "amount":
					row["amount"] = "0.2"
				case "internal":
					row["transferType"] = 1
				case "duplicate":
					rows = append(rows, row)
				}
				body, _ := json.Marshal(rows)
				return string(body)
			})
			remote := ""
			if mode == "foreignServer" {
				remote = "remoteWithdrawal1"
			}
			if _, err := c.WithdrawalRecordsFor(context.Background(), request, remote, ""); err == nil {
				t.Fatal("foreign or ambiguous withdrawal confirmed")
			}
		})
	}
	request := withdrawal()
	request.Timestamp = math.MaxInt64
	c := testClient(t, func(*http.Request) string { t.Fatal("invalid interval reached network"); return "[]" })
	if _, err := c.WithdrawalRecordsFor(context.Background(), request, "", ""); err == nil {
		t.Fatal("overflowing historical interval accepted")
	}
}

func TestWithdrawalHistoryReadAuthorizationRequiresExactlyOneBoundedIdentity(t *testing.T) {
	base := url.Values{"coin": {"ETH"}, "withdrawOrderId": {fixtureID}, "limit": {"50"}, "offset": {"0"}, "startTime": {"1700000000000"}, "endTime": {"1700086400000"}}
	for _, change := range []func(url.Values){
		func(q url.Values) { q.Set("idList", "remoteWithdrawal1") },
		func(q url.Values) { q.Del("withdrawOrderId") },
		func(q url.Values) { q.Del("startTime") },
		func(q url.Values) { q.Set("endTime", "1700604800000") },
		func(q url.Values) { q.Set("startTime", "1700086400000") },
	} {
		q := url.Values{}
		for key, value := range base {
			q[key] = append([]string(nil), value...)
		}
		change(q)
		request := bybit.AuthRequest{Endpoint: Endpoint, Method: "GET", Path: "/sapi/v1/capital/withdraw/history", Payload: q.Encode(), TimestampMS: 1700000000000, ReceiveWindowMS: 5000}
		if ValidateAuthRequest(request) == nil {
			t.Fatal("broad or ambiguous history request authorized", q)
		}
	}
}
