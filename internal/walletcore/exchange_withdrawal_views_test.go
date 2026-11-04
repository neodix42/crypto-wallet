package walletcore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/vault"
)

type withdrawalViewsAPI struct {
	*stablecoinPoolsAPI
	recent       func(string) (bybit.WithdrawalPage, error)
	addresses    func(string, string) (bybit.WithdrawalAddressPage, error)
	historyCalls int
	addressCalls int
}

func (api *withdrawalViewsAPI) RecentWithdrawalRecords(_ context.Context, cursor string) (bybit.WithdrawalPage, error) {
	api.historyCalls++
	return api.recent(cursor)
}
func (api *withdrawalViewsAPI) WithdrawalAddresses(_ context.Context, chain, cursor string) (bybit.WithdrawalAddressPage, error) {
	api.addressCalls++
	return api.addresses(chain, cursor)
}

func withdrawalViewsCoordinator(t *testing.T) (*core, *withdrawalViewsAPI, string) {
	t.Helper()
	c, base, path := stablecoinPoolsCoordinator(t)
	api := &withdrawalViewsAPI{stablecoinPoolsAPI: base}
	api.recent = func(string) (bybit.WithdrawalPage, error) {
		return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{}}, nil
	}
	api.addresses = func(string, string) (bybit.WithdrawalAddressPage, error) {
		return bybit.WithdrawalAddressPage{Rows: []bybit.WithdrawalAddress{}}, nil
	}
	factory := c.newExchange
	c.newExchange = func(endpoint string, auth bybit.Authorizer) (exchangeAPI, error) {
		_, err := factory(endpoint, auth)
		return api, err
	}
	return c, api, path
}

func seedWithdrawalViewOperation(t *testing.T, path, id, external, stateName string, rejection int64, created int64) vault.ExchangeOperation {
	t.Helper()
	request := bybit.WithdrawalRequest{Coin: "USDT", Chain: "TON", Address: "UQ_PUBLIC_OWNER", Amount: "3", Timestamp: created * 1000, ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: id}
	payload, _ := json.Marshal(request)
	op := vault.ExchangeOperation{ID: id, Kind: "withdraw", AccountID: "gram-source", AssetID: "ton-mainnet:USDT", Endpoint: "https://api.bybit.com", CredentialID: "fixture-credential", State: stateName, Payload: string(payload), ExternalID: external, RejectionCode: rejection, CreatedAt: created, Fee: "0.1", FeeCoin: "USDT"}
	stored := readPublicState(t, path)
	ops, _ := stored["exchangeOps"].([]any)
	stored["exchangeOps"] = append(ops, op)
	writePublicState(t, path, stored)
	return op
}

func withdrawalViewProviderRecord(id, status string, created int64) bybit.WithdrawalRecord {
	return bybit.WithdrawalRecord{WithdrawID: id, Coin: "USDT", Chain: "TON", Amount: "2.9", WithdrawFee: "0.1", Status: status, ToAddress: "UQ_PUBLIC_OWNER", Tag: "", CreateTime: strconv.FormatInt(created*1000, 10), UpdateTime: strconv.FormatInt(created*1000+1000, 10), WithdrawType: 0}
}

func TestWithdrawalHistoryShowsLocalRefusalLostAcknowledgementAndManualRequestsWithoutGuessing(t *testing.T) {
	c, api, path := withdrawalViewsCoordinator(t)
	failed := seedWithdrawalViewOperation(t, path, strings.Repeat("1", 32), "", "FAILED", 131093, 1793788805)
	lost := seedWithdrawalViewOperation(t, path, strings.Repeat("2", 32), "", "UNKNOWN", 0, 1793788804)
	linked := seedWithdrawalViewOperation(t, path, strings.Repeat("3", 32), "exact-ack", "PENDING", 0, 1793788803)
	wrong := seedWithdrawalViewOperation(t, path, strings.Repeat("4", 32), "mismatched-address", "PENDING", 0, 1793788802)
	before := readPublicState(t, path)
	manual := withdrawalViewProviderRecord("manual-same-address-and-amount", "Pending", lost.CreatedAt)
	exact := withdrawalViewProviderRecord("exact-ack", "success", linked.CreatedAt)
	exact.TxID = "public-transaction-hash"
	mismatch := withdrawalViewProviderRecord("mismatched-address", "Pending", wrong.CreatedAt)
	mismatch.ToAddress = "UQ_DIFFERENT_PUBLIC_OWNER"
	api.recent = func(string) (bybit.WithdrawalPage, error) {
		return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{mismatch, manual, exact}}, nil
	}
	out, err := c.handle(message("exchangeWithdrawals", struct{}{}))
	if err != nil {
		t.Fatal(err)
	}
	view := out.(exchangeWithdrawalsSnapshot)
	if len(view.Rows) != 6 || view.ReadError != "" || view.Rows[0].OperationID != failed.ID || api.historyCalls != 1 || api.posts != 0 {
		t.Fatal("recent withdrawal view omitted local failure/manual activity or newest ordering", view)
	}
	seen := map[string]exchangeWithdrawalRow{}
	for _, row := range view.Rows {
		seen[row.Source+":"+row.OperationID+":"+row.WithdrawalID] = row
	}
	refused := seen["local:"+failed.ID+":"]
	if refused.State != "REJECTED" || refused.Code != "EXCHANGE_BYBIT_131093" || !strings.Contains(refused.Reason, "whitelist") || refused.AmountBasis != "plannedDebit" {
		t.Fatal("durable safe refusal reason was not exposed", refused)
	}
	unknown := seen["local:"+lost.ID+":"]
	if unknown.State != "UNKNOWN" || unknown.Code != "EXCHANGE_WITHDRAWAL_UNKNOWN" {
		t.Fatal("missing acknowledgement was falsely classified or merged by amount/address/time", unknown)
	}
	if seen["bybit::manual-same-address-and-amount"].OperationID != "" {
		t.Fatal("manual request acquired a guessed original operation ID")
	}
	joined := seen["bybit+local:"+linked.ID+":exact-ack"]
	if joined.State != "SUCCESS" || joined.TransactionID != exact.TxID || joined.AmountBasis != "providerReported" || joined.FeeBasis != "providerReported" {
		t.Fatal("exact acknowledged request lost its checked remote identity/effect", joined)
	}
	if seen["local:"+wrong.ID+":mismatched-address"].OperationID == "" || seen["bybit::mismatched-address"].OperationID != "" {
		t.Fatal("provider ID alone overrode a conflicting destination")
	}
	after := readPublicState(t, path)
	delete(before, "calls")
	delete(after, "calls")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("read-only status history changed signed payloads, states or operation identities")
	}
}

func TestWithdrawalHistorySortsBoundedPagesAndRetainsLocalEvidenceOnProviderFailure(t *testing.T) {
	for _, scenario := range []string{"pages", "unavailable", "malformed", "repeated", "bounded"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, path := withdrawalViewsCoordinator(t)
			local := seedWithdrawalViewOperation(t, path, strings.Repeat("5", 32), "", "UNKNOWN", 0, 1793788999)
			api.recent = func(cursor string) (bybit.WithdrawalPage, error) {
				if scenario == "unavailable" {
					return bybit.WithdrawalPage{}, &bybit.APIError{Code: 10005}
				}
				page := bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{}}
				if scenario == "malformed" {
					row := withdrawalViewProviderRecord("bad", "PRIVATE_REASON_CANARY", 1793788800)
					page.Rows = append(page.Rows, row)
					return page, nil
				}
				for i := 0; i < 12; i++ {
					page.Rows = append(page.Rows, withdrawalViewProviderRecord(strconv.Itoa(api.historyCalls)+"-"+strconv.Itoa(i), "Pending", 1793788800+int64(i)))
				}
				if cursor == "" || scenario == "bounded" || scenario == "repeated" {
					page.NextPageCursor = "next-" + strconv.Itoa(api.historyCalls)
					if scenario == "repeated" {
						page.NextPageCursor = "always-same"
					}
				}
				return page, nil
			}
			out, err := c.handle(message("exchangeWithdrawals", struct{}{}))
			if err != nil {
				t.Fatal(err)
			}
			view := out.(exchangeWithdrawalsSnapshot)
			if len(view.Rows) == 0 || view.Rows[0].OperationID != local.ID || api.posts != 0 {
				t.Fatal("local unknown attempt disappeared when remote observations were partial/unavailable", view)
			}
			if scenario == "unavailable" || scenario == "malformed" || scenario == "repeated" {
				if view.ReadError == "" {
					t.Fatal("unavailable or inconsistent provider history was reported complete", view)
				}
			} else if len(view.Rows) != 10 || !view.More {
				t.Fatal("history failed its newest-ten bound", view)
			}
			if api.historyCalls > 4 || scenario == "bounded" && api.historyCalls != 4 {
				t.Fatal("history exceeded its bounded read scan", api.historyCalls)
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "PRIVATE_REASON_CANARY") {
				t.Fatal("untrusted provider reason entered public history")
			}
		})
	}
}

func TestWithdrawalAddressStatusRequiresExactOwnedIdentityAndExhaustedPages(t *testing.T) {
	for _, scenario := range []string{"normal", "unverified", "locked", "universal", "absent", "case", "memo", "second-page", "later-failure", "loop", "bounded"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, path := withdrawalViewsCoordinator(t)
			owned := "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ"
			api.addresses = func(chain, cursor string) (bybit.WithdrawalAddressPage, error) {
				if chain != "TON" {
					t.Fatal("selected TON token used another address book chain", chain)
				}
				page := bybit.WithdrawalAddressPage{Rows: []bybit.WithdrawalAddress{}}
				row := bybit.WithdrawalAddress{Coin: "USDT", Chain: "TON", Address: owned, Tag: "", Status: 0, Verified: 1, AddressType: 0}
				switch scenario {
				case "normal":
				case "unverified":
					row.Verified = 0
				case "locked":
					row.Status = 1
				case "universal":
					row.Coin = "baseCoin"
				case "absent":
					row.Address = "UQ_DIFFERENT_PUBLIC_OWNER"
				case "case":
					row.Address = strings.ToLower(owned)
				case "memo":
					row.Tag = "required-different-tag"
				case "second-page", "later-failure":
					if cursor == "" {
						page.NextPageCursor = "next"
						if scenario == "second-page" {
							return page, nil
						}
					} else if scenario == "later-failure" {
						return page, errors.New("PRIVATE_BOOK_ERROR_CANARY")
					}
				case "loop":
					page.NextPageCursor = "same"
				case "bounded":
					page.NextPageCursor = strconv.Itoa(api.addressCalls)
				}
				page.Rows = append(page.Rows, row)
				return page, nil
			}
			out, err := c.handle(message("exchangeWithdrawalAddressStatus", map[string]string{"accountId": "gram-source", "assetId": "ton-mainnet:USDT"}))
			if err != nil {
				t.Fatal(err)
			}
			view := out.(exchangeWithdrawalAddressStatus)
			if view.Coin != "USDT" || view.Chain != "TON" || view.Address != owned || api.posts != 0 || independentExchangeOperationCount(t, path) != 0 {
				t.Fatal("address status changed owned token/network/destination or created authority", view)
			}
			switch scenario {
			case "locked":
				if view.State != "locked24h" || view.Verified == nil || !*view.Verified || strings.Contains(view.Reason, "at 20") {
					t.Fatal("provider new-address lock was lost or assigned an invented unlock time", view)
				}
			case "absent", "case", "memo":
				if view.State != "notListed" || view.Verified != nil {
					t.Fatal("another address representation or memo was accepted as exact whitelist entry", view)
				}
			case "later-failure", "loop", "bounded":
				if view.State != "unknown" || view.ReadError == "" || view.Verified != nil {
					t.Fatal("incomplete address-book scan invented eligibility/absence", view)
				}
			default:
				if view.State != "listed" || view.Verified == nil || *view.Verified != (scenario != "unverified") {
					t.Fatal("normal/verified/universal exact entry was not reported precisely", view)
				}
			}
			if api.addressCalls > 20 {
				t.Fatal("address-book scan exceeded its bound")
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "PRIVATE_") {
				t.Fatal("provider private text entered public address status")
			}
		})
	}
}

func TestWithdrawalReadViewsRejectCallerSelectedRequestsAndNeverAuthorizeWrites(t *testing.T) {
	for _, request := range []struct {
		method string
		body   any
	}{
		{"exchangeWithdrawals", map[string]string{"withdrawID": "guess"}},
		{"exchangeWithdrawals", map[string]int{"limit": 500}},
		{"exchangeWithdrawalAddressStatus", map[string]string{"accountId": "foreign", "assetId": "ton-mainnet:USDT"}},
		{"exchangeWithdrawalAddressStatus", map[string]string{"accountId": "gram-source", "assetId": "ton-mainnet:USDT", "address": "caller-address"}},
		{"exchangeWithdrawalAddressStatus", map[string]string{"accountId": "gram-source", "assetId": "unsupported"}},
	} {
		c, api, path := withdrawalViewsCoordinator(t)
		if _, err := c.handle(message(request.method, request.body)); err == nil || api.posts != 0 || api.historyCalls != 0 || api.addressCalls != 0 || independentExchangeOperationCount(t, path) != 0 {
			t.Fatal("read view accepted caller-controlled provider/request/foreign destination authority", request, err)
		}
	}
}

func TestWithdrawalDefinitiveWhitelistRefusalsExposeDurableFixedReasonsOnlyForWithdrawals(t *testing.T) {
	for _, code := range []int64{131093, 131098, 131089} {
		for _, kind := range []string{"withdraw", "swap"} {
			t.Run(strconv.FormatInt(code, 10)+"/"+kind, func(t *testing.T) {
				c, api, _, path := fixtureExchangeCoordinator(t)
				method, endpoint := "exchangeWithdraw", "/v5/asset/withdraw/create"
				body := map[string]any{"amount": "0.1", "existingFunds": true, "fundingOnly": true}
				if kind == "swap" {
					method, endpoint = "exchangeSwap", "/v5/order/create"
					body = map[string]any{"amount": "0.1", "side": "Sell", "slippage": "0.5", "unifiedOnly": true}
				}
				api.fail[endpoint] = &bybit.APIError{Code: code}
				result := exchangeResultCall(t, c, method, body)
				op := readPublicState(t, path)["exchangeOps"].([]any)[0].(map[string]any)
				if kind == "withdraw" {
					if result.State != "FAILED" || result.Code != "EXCHANGE_BYBIT_"+strconv.FormatInt(code, 10) || result.Message != exchangeRejectionMessage(code) || op["state"] != "FAILED" || op["rejectionCode"] != float64(code) {
						t.Fatal("documented first withdrawal refusal lost its durable fixed cause", result, op)
					}
				} else if result.State != "UNKNOWN" || op["state"] != "UNKNOWN" || op["rejectionCode"] != nil {
					t.Fatal("withdrawal-only refusal released an unrelated ambiguous spot operation", result, op)
				}
				if api.posts != 1 {
					t.Fatal("refusal handling retried a payment", api.posts)
				}
			})
		}
	}
}
