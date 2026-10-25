package near

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/model"
)

// These observations are public, synthetic RPC data. No fixture invokes a
// signer or a transaction-submission method.
const missingBalanceState = "NOT_CREATED (final state)"
const balanceFixtureHeight = 321

var balanceFixtureHash = func() string {
	hash := sha256.Sum256([]byte("LocalWallet NEAR final-state balance regression"))
	return base58.Encode(hash[:])
}()

type balanceRequest struct {
	Method string
	Params map[string]any
}

type balanceRoundTrip func(*http.Request) (*http.Response, error)

func (f balanceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (q balanceRequest) text(name string) string {
	s, _ := q.Params[name].(string)
	return s
}

func balanceEnvelope(result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": "wallet", "result": result}
}

func missingAccountEnvelope(account string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0", "id": "wallet",
		"error": map[string]any{
			"code": -32000, "message": "Server error", "name": "HANDLER_ERROR",
			"data": "A human-readable message must never establish an account balance.",
			"cause": map[string]any{
				"name": "UNKNOWN_ACCOUNT",
				"info": map[string]any{"requested_account_id": account, "block_height": balanceFixtureHeight, "block_hash": balanceFixtureHash},
			},
		},
	}
}

func fundedAccountEnvelope() map[string]any {
	return balanceEnvelope(map[string]any{"amount": "123456789012345678901234", "locked": "0", "storage_usage": 182, "block_height": balanceFixtureHeight, "block_hash": balanceFixtureHash})
}

// The fixture implements the public wire protocol, including structured error
// metadata, rather than constructing the adapter's private error values.
func balanceRPCFixture(t *testing.T, alter func(balanceRequest, map[string]any) map[string]any) (*Adapter, model.Account, func() []balanceRequest) {
	t.Helper()
	a := New()
	ac, err := a.Derive(seed(), 20261025, "near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	var mutex sync.Mutex
	var calls []balanceRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		q := balanceRequest{Method: request.Method}
		_ = json.Unmarshal(request.Params, &q.Params)
		mutex.Lock()
		calls = append(calls, q)
		mutex.Unlock()
		var response map[string]any
		switch q.Method {
		case "status":
			response = balanceEnvelope(map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}})
		case "block":
			response = balanceEnvelope(map[string]any{"header": map[string]any{"hash": balanceFixtureHash, "height": balanceFixtureHeight, "timestamp_nanosec": strconv.FormatInt(time.Now().UnixNano(), 10)}})
		case "query":
			switch q.text("request_type") {
			case "view_account":
				response = missingAccountEnvelope(q.text("account_id"))
			case "view_access_key":
				response = balanceEnvelope(map[string]any{"nonce": 41, "permission": "FullAccess", "block_height": balanceFixtureHeight, "block_hash": balanceFixtureHash})
			}
		}
		if response == nil {
			t.Errorf("unexpected RPC method %s", q.Method)
			response = balanceEnvelope(nil)
		}
		if alter != nil {
			response = alter(q, response)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	if err := a.SetEndpoint(ac.Network, server.URL); err != nil {
		t.Fatal(err)
	}
	return a, ac, func() []balanceRequest {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]balanceRequest(nil), calls...)
	}
}

func requireBalanceError(t *testing.T, a *Adapter, ac model.Account) {
	t.Helper()
	rows, err := a.Balance(context.Background(), ac)
	if err == nil || len(rows) != 0 {
		t.Fatalf("unverified balance accepted: %+v, %v", rows, err)
	}
}

func TestTP051NEARMissingImplicitBalanceAtFinalState(t *testing.T) {
	a, ac, calls := balanceRPCFixture(t, nil)
	rows, err := a.Balance(context.Background(), ac)
	if err != nil || len(rows) != 1 || rows[0].Asset != asset(ac.Network) || rows[0].Atomic != "0" || rows[0].State != missingBalanceState || rows[0].At != balanceFixtureHash {
		t.Fatalf("missing implicit balance: %+v, %v", rows, err)
	}
	requests := calls()
	if len(requests) != 4 || requests[0].Method != "status" || requests[1].text("request_type") != "view_account" || requests[1].text("finality") != "final" || requests[2].Method != "block" || requests[2].text("finality") != "final" || requests[3].text("request_type") != "view_account" || requests[3].text("block_id") != balanceFixtureHash || requests[3].text("account_id") != ac.Address {
		t.Fatalf("absence was not independently pinned to final state: %+v", requests)
	}
	if _, err := a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: "recipient.testnet", Amount: "1"}); err == nil {
		t.Fatal("missing sender received a spendable plan")
	}
}

func TestTP051NEARAbsenceRequiresExactStructuredContext(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(map[string]any)
	}{
		{"wrong account", func(info map[string]any) { info["requested_account_id"] = strings.Repeat("1", 64) }},
		{"missing account", func(info map[string]any) { delete(info, "requested_account_id") }},
		{"wrong hash", func(info map[string]any) { info["block_hash"] = base58.Encode(make([]byte, 32)) }},
		{"missing hash", func(info map[string]any) { delete(info, "block_hash") }},
		{"short hash", func(info map[string]any) { info["block_hash"] = "111" }},
		{"wrong height", func(info map[string]any) { info["block_height"] = balanceFixtureHeight - 1 }},
		{"missing height", func(info map[string]any) { delete(info, "block_height") }},
		{"negative height", func(info map[string]any) { info["block_height"] = -1 }},
		{"fractional height", func(info map[string]any) { info["block_height"] = 321.5 }},
		{"overflow height", func(info map[string]any) { info["block_height"] = json.Number("18446744073709551616") }},
		{"exponent height", func(info map[string]any) { info["block_height"] = json.Number("3.21e2") }},
		{"string height", func(info map[string]any) { info["block_height"] = "321" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, ac, _ := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
				if q.Method == "query" && q.text("block_id") != "" {
					info := response["error"].(map[string]any)["cause"].(map[string]any)["info"].(map[string]any)
					test.alter(info)
				}
				return response
			})
			requireBalanceError(t, a, ac)
		})
	}
}

func TestTP051NEARAbsenceRejectsErrorAmbiguity(t *testing.T) {
	for _, stage := range []string{"initial", "pinned"} {
		for _, test := range []struct {
			name  string
			alter func(map[string]any)
		}{
			{"wrong handler", func(re map[string]any) { re["name"] = "INTERNAL_ERROR" }},
			{"missing handler", func(re map[string]any) { delete(re, "name") }},
			{"wrong cause", func(re map[string]any) { re["cause"].(map[string]any)["name"] = "UNKNOWN_ACCESS_KEY" }},
			{"missing cause", func(re map[string]any) { delete(re, "cause") }},
			{"message only", func(re map[string]any) {
				delete(re, "name")
				delete(re, "cause")
				re["message"] = "UNKNOWN_ACCOUNT does not exist"
			}},
			{"legacy data only", func(re map[string]any) {
				delete(re, "name")
				delete(re, "cause")
				re["data"] = "account does not exist while viewing"
			}},
		} {
			t.Run(stage+"/"+test.name, func(t *testing.T) {
				a, ac, calls := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
					if q.Method == "query" && q.text("request_type") == "view_account" && ((stage == "initial" && q.text("block_id") == "") || (stage == "pinned" && q.text("block_id") != "")) {
						test.alter(response["error"].(map[string]any))
					}
					return response
				})
				requireBalanceError(t, a, ac)
				if stage == "initial" && len(calls()) != 2 {
					t.Fatal("ambiguous initial error triggered absence fallback")
				}
			})
		}
	}
	// Numeric legacy error codes are deprecated by NEAR. Structured semantics,
	// exact context and a separately observed final block carry this decision.
	t.Run("legacy numeric code is not authority", func(t *testing.T) {
		a, ac, _ := balanceRPCFixture(t, func(_ balanceRequest, response map[string]any) map[string]any {
			if re, ok := response["error"].(map[string]any); ok {
				re["code"] = -12345
				re["message"] = "different provider wording"
			}
			return response
		})
		rows, err := a.Balance(context.Background(), ac)
		if err != nil || len(rows) != 1 || rows[0].State != missingBalanceState {
			t.Fatalf("structured absence rejected for legacy prose/code: %+v, %v", rows, err)
		}
	})
}

func TestTP051NEARAbsenceRequiresRecentCanonicalFinalBlock(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(map[string]any)
	}{
		{"missing hash", func(h map[string]any) { delete(h, "hash") }},
		{"malformed hash", func(h map[string]any) { h["hash"] = "bad0hash" }},
		{"short hash", func(h map[string]any) { h["hash"] = "111" }},
		{"missing height", func(h map[string]any) { delete(h, "height") }},
		{"negative height", func(h map[string]any) { h["height"] = -1 }},
		{"fractional height", func(h map[string]any) { h["height"] = 321.5 }},
		{"exponent height", func(h map[string]any) { h["height"] = json.Number("3.21e2") }},
		{"overflow height", func(h map[string]any) { h["height"] = json.Number("18446744073709551616") }},
		{"missing time", func(h map[string]any) { delete(h, "timestamp_nanosec") }},
		{"stale time", func(h map[string]any) {
			h["timestamp_nanosec"] = strconv.FormatInt(time.Now().Add(-3*time.Minute).UnixNano(), 10)
		}},
		{"future time", func(h map[string]any) {
			h["timestamp_nanosec"] = strconv.FormatInt(time.Now().Add(2*time.Minute).UnixNano(), 10)
		}},
		{"negative time", func(h map[string]any) { h["timestamp_nanosec"] = "-1" }},
		{"leading zero time", func(h map[string]any) { h["timestamp_nanosec"] = "0" + strconv.FormatInt(time.Now().UnixNano(), 10) }},
		{"fractional time", func(h map[string]any) { h["timestamp_nanosec"] = "1792928916002010756.0" }},
		{"overflow time", func(h map[string]any) { h["timestamp_nanosec"] = "18446744073709551616" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, ac, calls := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
				if q.Method == "block" {
					test.alter(response["result"].(map[string]any)["header"].(map[string]any))
				}
				return response
			})
			requireBalanceError(t, a, ac)
			if len(calls()) != 3 {
				t.Fatal("invalid final block reached a pinned account query")
			}
		})
	}
}

func TestTP051NEARAbsenceRejectsNamedAndInvalidDescriptors(t *testing.T) {
	for _, kind := range []string{"named", "changed implicit address", "changed key", "bad parameters"} {
		t.Run(kind, func(t *testing.T) {
			a, ac, calls := balanceRPCFixture(t, nil)
			switch kind {
			case "named":
				var parameters Parameters
				_ = json.Unmarshal(ac.Parameters, &parameters)
				parameters.Named = true
				ac.Parameters, _ = json.Marshal(parameters)
				ac.Address = "missing.testnet"
			case "changed implicit address":
				ac.Address = strings.Repeat("1", 64)
			case "changed key":
				ac.PublicKey = "ed25519:" + base58.Encode(make([]byte, 32))
			case "bad parameters":
				ac.Parameters = json.RawMessage(`{"named":false}`)
			}
			requireBalanceError(t, a, ac)
			for _, q := range calls() {
				if q.Method == "block" {
					t.Fatal("noncanonical implicit descriptor reached absence fallback")
				}
			}
		})
	}
}

func TestTP051NEARFundingRaceStillRequiresPinnedAuthority(t *testing.T) {
	for _, variant := range []string{"valid", "wrong account hash", "wrong account height", "bad amount", "restricted key", "missing key", "wrong key hash", "wrong key height"} {
		t.Run(variant, func(t *testing.T) {
			a, ac, calls := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
				if q.Method == "query" && q.text("block_id") != "" && q.text("request_type") == "view_account" {
					response = fundedAccountEnvelope()
					state := response["result"].(map[string]any)
					switch variant {
					case "wrong account hash":
						state["block_hash"] = base58.Encode(make([]byte, 32))
					case "wrong account height":
						state["block_height"] = balanceFixtureHeight + 1
					case "bad amount":
						state["amount"] = "01"
					}
				}
				if q.Method == "query" && q.text("request_type") == "view_access_key" {
					key := response["result"].(map[string]any)
					switch variant {
					case "restricted key":
						key["permission"] = map[string]any{"FunctionCall": map[string]any{"receiver_id": "contract.testnet", "method_names": []string{"call"}}}
					case "missing key":
						return missingAccountEnvelope(q.text("account_id"))
					case "wrong key hash":
						key["block_hash"] = base58.Encode(make([]byte, 32))
					case "wrong key height":
						key["block_height"] = balanceFixtureHeight + 1
					}
				}
				return response
			})
			if variant != "valid" {
				requireBalanceError(t, a, ac)
				return
			}
			rows, err := a.Balance(context.Background(), ac)
			if err != nil || len(rows) != 1 || rows[0].Atomic != "123456789012345678901234" || rows[0].State != "FINAL" || rows[0].At != balanceFixtureHash {
				t.Fatalf("funding race was lost or treated as zero: %+v, %v", rows, err)
			}
			requests := calls()
			last := requests[len(requests)-1]
			if last.Method != "query" || last.text("request_type") != "view_access_key" || last.text("block_id") != balanceFixtureHash || last.text("public_key") != ac.PublicKey {
				t.Fatal("funded race bypassed pinned derived-key authority")
			}
		})
	}
}

func TestTP051NEARAccessKeyFailureCannotBecomeMissingBalance(t *testing.T) {
	a, ac, calls := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
		if q.Method == "query" && q.text("request_type") == "view_account" {
			return fundedAccountEnvelope()
		}
		if q.Method == "query" && q.text("request_type") == "view_access_key" {
			// Even a malicious provider's UNKNOWN_ACCOUNT on a key request must
			// never erase an already observed funded account.
			return missingAccountEnvelope(q.text("account_id"))
		}
		return response
	})
	requireBalanceError(t, a, ac)
	for _, q := range calls() {
		if q.Method == "block" {
			t.Fatal("access-key failure entered account-absence fallback")
		}
	}
}

func TestTP051NEARBalanceRejectsWrongChainAndStaleHead(t *testing.T) {
	for _, variant := range []string{"wrong chain", "syncing", "stale", "future", "bad time"} {
		t.Run(variant, func(t *testing.T) {
			a, ac, calls := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
				if q.Method == "status" {
					status := response["result"].(map[string]any)
					sync := status["sync_info"].(map[string]any)
					switch variant {
					case "wrong chain":
						status["chain_id"] = "mainnet"
					case "syncing":
						sync["syncing"] = true
					case "stale":
						sync["latest_block_time"] = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
					case "future":
						sync["latest_block_time"] = time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
					case "bad time":
						sync["latest_block_time"] = "not a timestamp"
					}
				}
				return response
			})
			requireBalanceError(t, a, ac)
			if len(calls()) != 1 {
				t.Fatal("invalid network status reached an account query")
			}
		})
	}
}

func TestTP051NEARBalanceRejectsAmbiguousRPCEnvelope(t *testing.T) {
	for _, method := range []string{"status", "block", "query"} {
		for _, variant := range []string{"wrong id", "wrong version", "result plus error", "null result", "missing result"} {
			t.Run(method+"/"+variant, func(t *testing.T) {
				a, ac, _ := balanceRPCFixture(t, func(q balanceRequest, response map[string]any) map[string]any {
					if q.Method != method {
						return response
					}
					switch variant {
					case "wrong id":
						response["id"] = "another request"
					case "wrong version":
						response["jsonrpc"] = "1.0"
					case "result plus error":
						if _, exists := response["error"]; exists {
							response["result"] = map[string]any{"amount": "0"}
						} else {
							response["error"] = missingAccountEnvelope(q.text("account_id"))["error"]
						}
					case "null result":
						delete(response, "error")
						response["result"] = nil
					case "missing result":
						delete(response, "error")
						delete(response, "result")
					}
					return response
				})
				requireBalanceError(t, a, ac)
			})
		}
	}
}

func TestTP051NEARBalanceContextAndHTTPFailureStayUnknown(t *testing.T) {
	t.Run("cancelled context", func(t *testing.T) {
		a, ac, calls := balanceRPCFixture(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rows, err := a.Balance(ctx, ac)
		if err == nil || len(rows) != 0 || len(calls()) != 0 {
			t.Fatalf("cancelled query returned or requested a balance: %+v, %v", rows, err)
		}
	})
	for _, stage := range []struct {
		name string
		call int
	}{{"initial account", 2}, {"final block", 3}, {"pinned account", 4}} {
		for _, mode := range []string{"transport failure", "context cancelled"} {
			t.Run(stage.name+"/"+mode, func(t *testing.T) {
				a, ac, calls := balanceRPCFixture(t, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				attempt := 0
				a.client.HTTP.Transport = balanceRoundTrip(func(request *http.Request) (*http.Response, error) {
					attempt++
					if attempt == stage.call {
						if mode == "context cancelled" {
							cancel()
							return nil, request.Context().Err()
						}
						return nil, errors.New("public synthetic transport failure")
					}
					return http.DefaultTransport.RoundTrip(request)
				})
				rows, err := a.Balance(ctx, ac)
				if err == nil || len(rows) != 0 || attempt != stage.call || len(calls()) != stage.call-1 {
					t.Fatalf("failed observation became a balance or retried: %+v, %v, attempts %d", rows, err, attempt)
				}
			})
		}
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(missingAccountEnvelope(strings.Repeat("1", 64)))
			}))
			defer server.Close()
			a := New()
			ac, _ := a.Derive(seed(), 20261025, "near-testnet")
			if err := a.SetEndpoint(ac.Network, server.URL); err != nil {
				t.Fatal(err)
			}
			requireBalanceError(t, a, ac)
		})
	}
}

// This opt-in read-only probe uses a PUBLIC standard BIP39 test vector and a
// disposable index. It never uses a user's profile or signs/submits/funds
// anything. Its output is a provider observation, not public funded acceptance.
func TestNEARPublicBalanceReadOnly(t *testing.T) {
	if os.Getenv("WALLET_NEAR_BALANCE_READONLY") != "1" {
		t.Skip("NOT_RUN: explicit public read-only NEAR balance probe not requested")
	}
	a := New()
	ac, err := a.Derive(seed(), 20261025, "near-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rows, err := a.Balance(ctx, ac)
	if err != nil || len(rows) != 1 || rows[0].Asset != asset(ac.Network) || rows[0].Atomic != "0" || rows[0].State != missingBalanceState || len(base58.Decode(rows[0].At)) != 32 {
		t.Fatalf("public disposable implicit account observation: %+v, %v", rows, err)
	}
	t.Logf("PUBLIC READ-ONLY OBSERVATION: address=%s block=%s state=%s; no transfer or funding requested", ac.Address, rows[0].At, rows[0].State)
}
