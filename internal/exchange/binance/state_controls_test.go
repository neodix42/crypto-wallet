package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// These public synthetic provider responses enforce state and identity
// distinctions. No account, credential, transaction or fund is live.
func TestBinanceNativeWritesRejectIncompleteAcknowledgementsWithoutRetry(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []struct {
		name, path string
		call       func(*Client) error
		malformed  []string
	}{
		{"order", "/api/v3/order", func(c *Client) error {
			ack, err := c.CreateOrder(ctx, spotOrder())
			if err != nil && (ack.OrderID != "" || ack.OrderLinkID != "") {
				t.Fatal("failed order retained an unverified identity")
			}
			return err
		}, []string{`{}`, `{"symbol":"ETHUSDT","orderId":0,"clientOrderId":"fixtureOrder1"}`, `{"symbol":"BTCUSDT","orderId":123,"clientOrderId":"fixtureOrder1"}`, `{"symbol":"ETHUSDT","orderId":123,"clientOrderId":"foreign"}`}},
		{"transfer", "/sapi/v1/asset/transfer", func(c *Client) error {
			ack, err := c.Transfer(ctx, transfer())
			if err != nil && ack.TransferID != "" {
				t.Fatal("failed transfer acquired an identity")
			}
			return err
		}, []string{`{}`, `{"tranId":null}`, `{"tranId":0}`}},
		{"withdrawal", "/sapi/v1/capital/withdraw/apply", func(c *Client) error {
			ack, err := c.Withdraw(ctx, withdrawal())
			if err != nil && ack.ID != "" {
				t.Fatal("failed withdrawal acquired an identity")
			}
			return err
		}, []string{`{}`, `{"id":null}`, `{"id":"foreign/id"}`}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			for _, body := range operation.malformed {
				posts := 0
				c := testClient(t, func(r *http.Request) string {
					if r.Method == "POST" {
						posts++
						if r.URL.Path != operation.path {
							t.Fatal("wrong write path")
						}
						return body
					}
					return wireFixture(r)
				})
				if err := operation.call(c); err != ErrInvalidResponse || posts != 1 {
					t.Fatal("malformed acknowledgement accepted or retried", err, posts)
				}
			}
			posts := 0
			c := testClient(t, wireFixture)
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					posts++
					return nil, errors.New("PRIVATE_PROVIDER_FAILURE")
				}
				return response(wireFixture(r)), nil
			})
			if err := operation.call(c); err != ErrUnavailable || posts != 1 {
				t.Fatal("lost acknowledgement was retried or promoted", err, posts)
			}
		})
	}
}

func transferRows(t *testing.T, total uint64, rows []map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"total": total, "rows": rows})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func transferRow(id uint64, status string) map[string]any {
	return map[string]any{"asset": "ETH", "amount": "0.1", "type": "FUNDING_MAIN", "status": status, "tranId": id, "timestamp": uint64(1700000000000)}
}
func TestBinanceTransferStatusRequiresExactServerIdentityThroughBoundedPages(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[string]string{"CONFIRMED": "SUCCESS", "FAILED": "FAILED", "PENDING": "PENDING"} {
		c := testClient(t, func(*http.Request) string { return transferRows(t, 1, []map[string]any{transferRow(456, status)}) })
		record, err := c.TransferStatusFor(ctx, transfer(), "456")
		if err != nil || record == nil || record.TransferID != fixtureUUID || record.Status != want {
			t.Fatal("exact observed transfer state changed", record, err)
		}
	}
	first := []map[string]any{}
	for i := uint64(1000); i < 1100; i++ {
		first = append(first, transferRow(i, "CONFIRMED"))
	}
	calls := 0
	c := testClient(t, func(r *http.Request) string {
		calls++
		q := r.URL.Query()
		if r.Method != "GET" || q.Get("type") != "FUNDING_MAIN" || q.Get("size") != "100" {
			t.Fatal("expanded transfer-history authority")
		}
		if q.Get("current") == "1" {
			return transferRows(t, 101, first)
		}
		if q.Get("current") != "2" {
			t.Fatal("unexpected transfer page")
		}
		return transferRows(t, 101, []map[string]any{transferRow(456, "CONFIRMED")})
	})
	record, err := c.TransferStatusFor(ctx, transfer(), "456")
	if err != nil || record == nil || record.TransferID != fixtureUUID || calls != 2 {
		t.Fatal("matching amount bypassed exact later identity", record, err, calls)
	}
	c = testClient(t, func(*http.Request) string { return transferRows(t, 1, []map[string]any{transferRow(789, "CONFIRMED")}) })
	if record, err = c.TransferStatusFor(ctx, transfer(), "456"); err != nil || record != nil {
		t.Fatal("similar transfer became original result", record, err)
	}
	c = testClient(t, func(*http.Request) string {
		t.Fatal("unacknowledged/invalid transfer reached history search")
		return `{}`
	})
	if record, err = c.TransferStatusFor(ctx, transfer(), ""); err != nil || record != nil {
		t.Fatal("lost acknowledgement guessed", record, err)
	}
	for _, id := range []string{"0", "0456", "remote-id"} {
		if _, err = c.TransferStatusFor(ctx, transfer(), id); err != ErrInvalidRequest {
			t.Fatal("bad server ID", err)
		}
	}
	bad := transfer()
	bad.TransferID = "not-a-uuid"
	if _, err = c.TransferStatusFor(ctx, bad, "456"); err != ErrInvalidRequest {
		t.Fatal(err)
	}
}

func TestBinanceTransferHistoryRejectsHostileTruncatedAndExhaustedEvidence(t *testing.T) {
	ctx := context.Background()
	for _, mutate := range []func(map[string]any){
		func(r map[string]any) { delete(r, "tranId") }, func(r map[string]any) { r["tranId"] = uint64(0) }, func(r map[string]any) { r["asset"] = "BTC" }, func(r map[string]any) { r["amount"] = "0.2" }, func(r map[string]any) { r["amount"] = "-1" }, func(r map[string]any) { r["type"] = "MAIN_FUNDING" }, func(r map[string]any) { r["status"] = "SUCCESS" }, func(r map[string]any) { delete(r, "timestamp") }, func(r map[string]any) { r["timestamp"] = 0 },
	} {
		row := transferRow(456, "CONFIRMED")
		mutate(row)
		c := testClient(t, func(*http.Request) string { return transferRows(t, 1, []map[string]any{row}) })
		if _, err := c.TransferStatusFor(ctx, transfer(), "456"); err != ErrInvalidResponse {
			t.Fatal("foreign/incomplete transfer evidence accepted", err)
		}
	}
	for _, body := range []string{`{"rows":[]}`, `{"total":1,"rows":null}`, transferRows(t, 0, []map[string]any{transferRow(456, "CONFIRMED")}), transferRows(t, 2, []map[string]any{transferRow(456, "CONFIRMED"), transferRow(456, "CONFIRMED")}), transferRows(t, 101, []map[string]any{transferRow(789, "CONFIRMED")})} {
		c := testClient(t, func(*http.Request) string { return body })
		if _, err := c.TransferStatusFor(ctx, transfer(), "456"); err != ErrInvalidResponse {
			t.Fatal("malformed/truncated transfer page accepted", err)
		}
	}
	calls := 0
	c := testClient(t, func(r *http.Request) string {
		calls++
		page, err := strconv.Atoi(r.URL.Query().Get("current"))
		if err != nil || page < 1 || page > 32 {
			t.Fatal("unbounded transfer pagination")
		}
		rows := []map[string]any{}
		for i := 0; i < 100; i++ {
			rows = append(rows, transferRow(uint64(1000+page*100+i), "CONFIRMED"))
		}
		return transferRows(t, 3201, rows)
	})
	if record, err := c.TransferStatusFor(ctx, transfer(), "456"); err != ErrInvalidResponse || record != nil || calls != 32 {
		t.Fatal("search budget became false absence/finality", record, err, calls)
	}
	c = testClient(t, wireFixture)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("PRIVATE_NETWORK_ERROR") })
	if _, err := c.TransferStatusFor(ctx, transfer(), "456"); err != ErrUnavailable {
		t.Fatal("history outage concealed", err)
	}
}

func TestBinanceWithdrawalRecordStatusesAndDatesRemainExplicit(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[int]string{0: "Pending", 1: "CancelByUser", 2: "Pending", 3: "Reject", 4: "Pending", 5: "Fail", 6: "success"} {
		c := testClient(t, func(*http.Request) string {
			return strings.Replace(fixtureWithdrawal, `"status":6`, `"status":`+strconv.Itoa(status), 1)
		})
		page, err := c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", "")
		if err != nil || len(page.Rows) != 1 || page.Rows[0].Status != want {
			t.Fatal("withdrawal status promoted or erased", status, page, err)
		}
	}
	for _, change := range [][2]string{{`"status":6`, `"status":7`}, {`"applyTime":"2023-11-14 22:13:20"`, `"applyTime":"bad-date"`}, {`"applyTime":"2023-11-14 22:13:20"`, `"applyTime":"1970-01-01 00:00:00"`}, {`"completeTime":"2023-11-14 22:14:20"`, `"completeTime":"bad-date"`}, {`"completeTime":"2023-11-14 22:14:20"`, `"completeTime":"2023-11-14 22:12:20"`}} {
		c := testClient(t, func(*http.Request) string { return changedFixture(t, fixtureWithdrawal, change[0], change[1]) })
		if _, err := c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", ""); err != ErrInvalidResponse {
			t.Fatal("invalid withdrawal status/date accepted", err)
		}
	}
	c := testClient(t, func(*http.Request) string {
		return strings.Replace(fixtureWithdrawal, `"completeTime":"2023-11-14 22:14:20"`, `"completeTime":""`, 1)
	})
	page, err := c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", "")
	if err != nil || page.Rows[0].UpdateTime != "" {
		t.Fatal("missing completion time invented", page, err)
	}
	c = testClient(t, func(*http.Request) string { return `[]` })
	page, err = c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", "")
	if err != nil || page.Rows == nil || len(page.Rows) != 0 {
		t.Fatal("explicit absent row became a completed withdrawal", page, err)
	}
	c = testClient(t, func(*http.Request) string { t.Fatal("malformed cursor reached network"); return `[]` })
	if _, err = c.WithdrawalRecords(ctx, "ETH", "remoteWithdrawal1", "01"); err != ErrInvalidRequest {
		t.Fatal(err)
	}
}

func TestBinanceDepositStatusesIdentityAndContinuationPreserveUncertainty(t *testing.T) {
	ctx := context.Background()
	for status, want := range map[int]int{0: 0, 1: 3, 2: 2, 6: 0, 7: 2, 8: 0} {
		c := testClient(t, func(*http.Request) string {
			return strings.Replace(fixtureDeposit, `"status":1`, `"status":`+strconv.Itoa(status), 1)
		})
		page, err := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
		if err != nil || len(page.Rows) != 1 || page.Rows[0].Status != want {
			t.Fatal("withheld/pending deposit promoted", status, page, err)
		}
	}
	for _, change := range [][2]string{{`"id":"deposit1"`, `"id":""`}, {`"coin":"ETH"`, `"coin":"BTC"`}, {`"txId":"0xabcdef"`, `"txId":"foreign"`}, {`"status":1`, `"status":4`}, {`"addressTag":""`, `"addressTag":null`}, {`"transferType":0`, `"transferType":1`}, {`"confirmTimes":"64/64"`, `"confirmTimes":""`}, {`"travelRuleStatus":0`, `"travelRuleStatus":2`}, {`"completeTime":1700000001000`, `"completeTime":1699999999999`}} {
		c := testClient(t, func(*http.Request) string { return changedFixture(t, fixtureDeposit, change[0], change[1]) })
		if _, err := c.DepositRecords(ctx, "ETH", "0xabcdef", ""); err != ErrInvalidResponse {
			t.Fatal("foreign/incomplete deposit accepted", err)
		}
	}
	for _, change := range [][2]string{{`"completeTime":1700000001000`, `"completeTime":null`}, {`"completeTime":1700000001000`, `"completeTime":0`}} {
		c := testClient(t, func(*http.Request) string { return changedFixture(t, fixtureDeposit, change[0], change[1]) })
		page, err := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
		if err != nil || page.Rows[0].SuccessAt != "" {
			t.Fatal("completion timestamp fabricated", page, err)
		}
	}
	rows := []string{}
	for i := 0; i < 50; i++ {
		rows = append(rows, strings.Replace(fixtureDeposit[1:len(fixtureDeposit)-1], `"id":"deposit1"`, `"id":"deposit`+fmt.Sprint(i)+`"`, 1))
	}
	c := testClient(t, func(r *http.Request) string {
		q := r.URL.Query()
		if q.Get("coin") != "ETH" || q.Get("txId") != "0xabcdef" || q.Get("limit") != "50" {
			t.Fatal("deposit context changed")
		}
		if q.Get("offset") == "50" {
			return `[]`
		}
		return `[` + strings.Join(rows, ",") + `]`
	})
	page, err := c.DepositRecords(ctx, "ETH", "0xabcdef", "")
	if err != nil || len(page.Rows) != 50 || page.NextPageCursor != "50" {
		t.Fatal("deposit page truncated", page, err)
	}
	page, err = c.DepositRecords(ctx, "ETH", "0xabcdef", page.NextPageCursor)
	if err != nil || page.Rows == nil || len(page.Rows) != 0 || page.NextPageCursor != "" {
		t.Fatal("explicit end-of-window lost", page, err)
	}
	if _, err = c.DepositRecords(ctx, "ETH", "0xabcdef", "10000"); err != ErrInvalidResponse {
		t.Fatal("deposit search budget bypassed", err)
	}
	for _, body := range []string{`null`, `[` + strings.Join(append(rows, rows[0]), ",") + `]`, `[` + rows[0] + `,` + rows[0] + `]`} {
		c := testClient(t, func(*http.Request) string { return body })
		if _, err := c.DepositRecords(ctx, "ETH", "0xabcdef", ""); err != ErrInvalidResponse {
			t.Fatal("oversized/missing/duplicate deposit page", err)
		}
	}
	c = testClient(t, wireFixture)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("PRIVATE_PROVIDER_ERROR") })
	if _, err := c.DepositRecords(ctx, "ETH", "0xabcdef", ""); err != ErrUnavailable {
		t.Fatal("deposit outage erased", err)
	}
}

func TestBinanceReverseTransferKeepsOriginalUUIDAndExplicitAccountDirection(t *testing.T) {
	request := transfer()
	request.FromAccountType, request.ToAccountType = "UNIFIED", "FUND"
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Query().Get("type") != "MAIN_FUNDING" {
			t.Fatal("reverse account direction changed")
		}
		row := transferRow(456, "CONFIRMED")
		row["type"] = "MAIN_FUNDING"
		return transferRows(t, 1, []map[string]any{row})
	})
	record, err := c.TransferStatusFor(context.Background(), request, "456")
	if err != nil || record == nil || record.FromAccountType != "UNIFIED" || record.ToAccountType != "FUND" || record.TransferID != request.TransferID {
		t.Fatal("reversed transfer lost original account binding", record, err)
	}
}
