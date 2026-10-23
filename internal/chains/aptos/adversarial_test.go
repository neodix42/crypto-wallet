package aptos

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"localwallet/internal/model"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTP051HostileLedgerPrepare(t *testing.T) {
	for _, mode := range []string{"valid", "ledger-outage", "wrong-chain", "stale", "future", "bad-timestamp", "bad-version", "account-outage", "bad-sequence", "sequence-overflow", "balance-outage", "balance-shape", "balance-bad", "insufficient", "estimate-outage", "estimate-zero", "estimate-huge", "simulation-outage", "simulation-shape", "simulation-failure", "simulation-gasbad", "simulation-gaszero", "simulation-gashuge"} {
		t.Run(mode, func(t *testing.T) {
			p := fixturePlan(t, New())
			a, srv := aptosServer(t, func(r *http.Request) any {
				fail := func() any { return map[string]any{"error": "synthetic"} }
				switch {
				case r.URL.Path == "/":
					if mode == "ledger-outage" {
						return []any{}
					}
					id := 2
					if mode == "wrong-chain" {
						id = 1
					}
					ts := time.Now().UnixMicro()
					if mode == "stale" {
						ts -= int64(10 * time.Minute / time.Microsecond)
					}
					if mode == "future" {
						ts += int64(10 * time.Minute / time.Microsecond)
					}
					timestamp := any(stringNumber(ts))
					if mode == "bad-timestamp" {
						timestamp = "-1"
					}
					version := "123"
					if mode == "bad-version" {
						version = "-1"
					}
					return map[string]any{"chain_id": id, "ledger_version": version, "ledger_timestamp": timestamp}
				case strings.HasPrefix(r.URL.Path, "/accounts/"):
					if mode == "account-outage" {
						return []any{}
					}
					seq := p.Nonce
					if mode == "bad-sequence" {
						seq = "1.5"
					}
					if mode == "sequence-overflow" {
						seq = "18446744073709551615"
					}
					return map[string]any{"sequence_number": seq, "authentication_key": p.Account.Address}
				case r.URL.Path == "/view":
					if mode == "balance-outage" {
						return fail()
					}
					if mode == "balance-shape" {
						return []string{}
					}
					balance := "999999999999"
					if mode == "balance-bad" {
						balance = "bad"
					}
					if mode == "insufficient" {
						balance = "1"
					}
					return []string{balance}
				case r.URL.Path == "/estimate_gas_price":
					if mode == "estimate-outage" {
						return []any{}
					}
					price := any(100)
					if mode == "estimate-zero" {
						price = 0
					}
					if mode == "estimate-huge" {
						price = 1000001
					}
					return map[string]any{"gas_estimate": price}
				case r.URL.Path == "/transactions/simulate":
					if mode == "simulation-outage" {
						return fail()
					}
					if mode == "simulation-shape" {
						return []any{}
					}
					gas := "1000"
					if mode == "simulation-gasbad" {
						gas = "bad"
					}
					if mode == "simulation-gaszero" {
						gas = "0"
					}
					if mode == "simulation-gashuge" {
						gas = "100001"
					}
					return []any{map[string]any{"success": mode != "simulation-failure", "gas_used": gas}}
				}
				return fail()
			})
			defer srv.Close()
			q, e := a.Prepare(context.Background(), p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: p.Amount})
			if mode == "valid" {
				if e != nil || q.MaxFee != "160000" {
					t.Fatalf("%+v %v", q, e)
				}
			} else if e == nil {
				t.Fatal("hostile state accepted")
			}
		})
	}
}
func stringNumber(i int64) string { return strconv.FormatInt(i, 10) }
func TestTP042InvalidDescriptorsAndTransactions(t *testing.T) {
	a := New()
	p := fixturePlan(t, a)
	s, _ := a.Sign(seed(), p)
	for _, change := range []func(*model.Account){func(ac *model.Account) { ac.Family = "wrong" }, func(ac *model.Account) { ac.Network = "wrong" }, func(ac *model.Account) { ac.Address = "0x1" }, func(ac *model.Account) { ac.Parameters = []byte(`{}`) }, func(ac *model.Account) { ac.Parameters = []byte(`{"unknown":1}`) }, func(ac *model.Account) {
		ac.Parameters = []byte(`{"path":"wrong","scheme":"single-key","authenticationKey":"0x1","associated":false}`)
	}} {
		ac := p.Account
		change(&ac)
		if a.account(ac) == nil {
			t.Fatal("invalid descriptor")
		}
		if _, e := a.Associate(context.Background(), ac, "0x1"); e == nil {
			t.Fatal("association")
		}
		if _, e := a.Discover(context.Background(), ac); e == nil {
			t.Fatal("discovery")
		}
		q := p
		q.Account = ac
		if _, e := a.Submit(context.Background(), q, s); e == nil {
			t.Fatal("submit")
		}
		if _, e := a.Reconcile(context.Background(), q, s); e == nil {
			t.Fatal("reconcile")
		}
		if _, e := a.authority(context.Background(), ac, "123"); e == nil {
			t.Fatal("authority")
		}
	}
	if _, e := a.Associate(context.Background(), p.Account, "bad"); e == nil {
		t.Fatal("bad association address")
	}
	if a.SetEndpoint("wrong", "https://example.org") == nil {
		t.Fatal("network")
	}
	if a.ValidateAddress("wrong", "0x1") == nil {
		t.Fatal("network")
	}
	if a.request(context.Background(), "wrong", "GET", "", nil, nil) == nil {
		t.Fatal("network")
	}
	if a.request(context.Background(), "aptos-testnet", "POST", "", make(chan int), nil) == nil {
		t.Fatal("unencodable request")
	}
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	for _, field := range []string{"source", "recipient", "amount", "nonce", "gas", "price", "network"} {
		q := p
		data := d
		switch field {
		case "source":
			q.Account.Address = "invalid"
		case "recipient":
			q.Recipient = "invalid"
		case "amount":
			q.Amount = "0"
		case "nonce":
			q.Nonce = "-1"
		case "gas":
			data.GasUnits = "2000001"
		case "price":
			data.GasPrice = "1000001"
		case "network":
			q.Account.Network = "wrong"
		}
		if _, e := raw(q, data); e == nil {
			t.Fatal(field)
		}
	}
	if _, e := signed(nil, []byte{}, make([]byte, 64)); e == nil {
		t.Fatal("invalid pub")
	}
	pub, _ := hex.DecodeString(p.Account.PublicKey)
	if _, e := signed(nil, pub, []byte{}); e == nil {
		t.Fatal("invalid signature")
	}
	for _, change := range []func(*model.Plan){func(p *model.Plan) { p.Payload = []byte(`{}`) }, func(p *model.Plan) { p.Account.PublicKey = "bad" }, func(p *model.Plan) { p.Amount = "-1" }} {
		q := p
		change(&q)
		if a.verify(q, s) == nil {
			t.Fatal("invalid artifact")
		}
	}
	for _, field := range []string{"version", "ledgerVersion", "rawTransactionBCS", "prepared", "maxGasUnits", "gasUnitPrice", "unknown"} {
		q := p
		var data map[string]any
		_ = json.Unmarshal(q.Payload, &data)
		switch field {
		case "version":
			data[field] = 2
		case "ledgerVersion", "maxGasUnits", "gasUnitPrice":
			data[field] = "-1"
		case "rawTransactionBCS":
			data[field] = "deadbeef"
		case "prepared":
			data[field] = 0
		case "unknown":
			data[field] = true
		}
		q.Payload, _ = json.Marshal(data)
		if a.ValidatePlan(q) == nil {
			t.Fatal(field)
		}
	}
	for _, tr := range []model.Transfer{{AccountID: "bad"}, {AccountID: p.Account.ID, Asset: p.Asset, Recipient: "bad", Amount: "1"}, {AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "0"}} {
		if _, e := a.Prepare(context.Background(), p.Account, tr); e == nil {
			t.Fatal("invalid transfer")
		}
	}
	q := p
	q.Account.Family = "bad"
	if _, e := a.Prepare(context.Background(), q.Account, model.Transfer{AccountID: q.Account.ID, Asset: q.Asset, Recipient: q.Recipient, Amount: "1"}); e == nil {
		t.Fatal("prepare source")
	}
	q = p
	q.Account.Index = math.MaxUint32
	if _, e := a.Sign(seed(), q); e == nil {
		t.Fatal("sign index")
	}
}
func TestTP060NetworkSubmissionAndRecoveryFaults(t *testing.T) {
	for _, mode := range []string{"network", "authority", "sequence", "submission", "hash", "pending-no-type", "pending-foreign", "reconcile", "receipt-foreign", "receipt-sequence", "receipt-version", "receipt-unknown"} {
		t.Run(mode, func(t *testing.T) {
			p := fixturePlan(t, New())
			s, _ := New().Sign(seed(), p)
			a, srv := aptosServer(t, func(r *http.Request) any {
				switch {
				case r.URL.Path == "/":
					if mode == "network" {
						return map[string]any{"chain_id": 1}
					}
					return map[string]any{"chain_id": 2, "ledger_version": "123", "ledger_timestamp": stringNumber(time.Now().UnixMicro())}
				case strings.HasPrefix(r.URL.Path, "/accounts/"):
					if mode == "authority" {
						return map[string]any{"authentication_key": "0x1", "sequence_number": "42"}
					}
					seq := "42"
					if mode == "sequence" {
						seq = "43"
					}
					return map[string]any{"authentication_key": p.Account.Address, "sequence_number": seq}
				case r.URL.Path == "/transactions":
					if mode == "submission" {
						return []any{}
					}
					hash := s.Identity
					if mode == "hash" {
						hash = "wrong"
					}
					sender := p.Account.Address
					if mode == "pending-foreign" {
						sender = "0x1"
					}
					return map[string]any{"hash": hash, "sender": sender, "sequence_number": "42"}
				case strings.HasPrefix(r.URL.Path, "/transactions/by_hash/"):
					if mode == "reconcile" {
						return []any{}
					}
					sender := p.Account.Address
					seq := "42"
					version := "124"
					typ := "user_transaction"
					if mode == "receipt-foreign" {
						sender = "0x1"
					}
					if mode == "receipt-sequence" {
						seq = "43"
					}
					if mode == "receipt-version" {
						version = "bad"
					}
					if mode == "receipt-unknown" {
						typ = "other"
					}
					return map[string]any{"type": typ, "hash": s.Identity, "sender": sender, "sequence_number": seq, "success": true, "version": version}
				}
				return map[string]any{}
			})
			defer srv.Close()
			r, e := a.Submit(context.Background(), p, s)
			if mode == "pending-no-type" {
				if e != nil || r.State != "SUBMITTED" {
					t.Fatalf("%+v %v", r, e)
				}
			} else if mode == "network" || mode == "authority" || mode == "sequence" || mode == "submission" || mode == "hash" || mode == "pending-foreign" {
				if e == nil || r.State != "UNKNOWN" {
					t.Fatalf("%+v %v", r, e)
				}
			}
			r, e = a.Reconcile(context.Background(), p, s)
			if mode == "network" || mode == "reconcile" || strings.HasPrefix(mode, "receipt-") {
				if e == nil {
					t.Fatal("invalid reconciliation accepted")
				}
			}
			bad := s
			bad.Bytes = append([]byte{}, s.Bytes...)
			bad.Bytes[len(bad.Bytes)-1] ^= 1
			if _, e = a.Submit(context.Background(), p, bad); e == nil {
				t.Fatal("bad signature")
			}
		})
	}
}
func TestTP022RotationDiscovery(t *testing.T) {
	p := fixturePlan(t, New())
	for _, mode := range []string{"empty", "many", "bad-shape", "network", "view", "associate-network"} {
		t.Run(mode, func(t *testing.T) {
			a, srv := aptosServer(t, func(r *http.Request) any {
				switch {
				case r.URL.Path == "/":
					if mode == "network" || mode == "associate-network" {
						return map[string]any{"chain_id": 1}
					}
					return map[string]any{"chain_id": 2, "ledger_version": "123", "ledger_timestamp": stringNumber(time.Now().UnixMicro())}
				case r.URL.Path == "/view":
					if mode == "view" {
						return map[string]any{}
					}
					if mode == "empty" {
						return []any{map[string]any{"vec": []string{}}}
					}
					if mode == "many" {
						return []any{map[string]any{"vec": []string{"0x1", "0x2"}}}
					}
					return []any{}
				case strings.HasPrefix(r.URL.Path, "/accounts/"):
					return map[string]any{"authentication_key": p.Account.Address, "sequence_number": "0"}
				}
				return map[string]any{}
			})
			defer srv.Close()
			_, e := a.Discover(context.Background(), p.Account)
			if (mode == "empty") != (e == nil) {
				t.Fatalf("discovery %s %v", mode, e)
			}
			if mode == "associate-network" {
				if _, e := a.Associate(context.Background(), p.Account, p.Account.Address); e == nil {
					t.Fatal("association wrong ledger")
				}
			}
		})
	}
}
func TestTP032RecoveryDescriptor(t *testing.T) {
	a := New()
	expected, _ := a.Derive(seed(), 1, "aptos-testnet")
	if ValidateRecovery(expected, expected) != nil {
		t.Fatal("canonical descriptor")
	}
	restored := expected
	var p Parameters
	_ = json.Unmarshal(restored.Parameters, &p)
	p.Associated = true
	restored.Address = "0x" + strings.Repeat("0", 63) + "a"
	restored.Parameters, _ = json.Marshal(p)
	if ValidateRecovery(expected, restored) != nil {
		t.Fatal("original-address association not recoverable")
	}
	restored.ID = "foreign"
	if ValidateRecovery(expected, restored) == nil {
		t.Fatal("account ID substitution")
	}
	restored = expected
	p.Scheme = "single-key"
	restored.Parameters, _ = json.Marshal(p)
	if ValidateRecovery(expected, restored) == nil {
		t.Fatal("authentication scheme substitution")
	}
}

func TestTP042MalformedBoundaryFailures(t *testing.T) {
	a := New()
	p := fixturePlan(t, a)
	s, _ := a.Sign(seed(), p)
	if _, e := a.Sign(make([]byte, 32), p); e == nil {
		t.Fatal("short seed")
	}
	q := p
	q.Payload = []byte(`{`)
	if a.verify(q, s) == nil {
		t.Fatal("malformed codec metadata")
	}
	bad := s
	bad.Bytes = append([]byte{}, s.Bytes...)
	bad.Bytes[len(bad.Bytes)-1] ^= 1
	if _, e := a.Reconcile(context.Background(), p, bad); e == nil {
		t.Fatal("modified recovery artifact")
	}
}
func TestTP060BalanceUnavailableAndForeignHash(t *testing.T) {
	p := fixturePlan(t, New())
	s, _ := New().Sign(seed(), p)
	a, srv := aptosServer(t, func(r *http.Request) any {
		switch {
		case r.URL.Path == "/":
			return map[string]any{"chain_id": 2, "ledger_version": "123", "ledger_timestamp": stringNumber(time.Now().UnixMicro())}
		case strings.HasPrefix(r.URL.Path, "/accounts/"):
			return map[string]any{"authentication_key": p.Account.Address, "sequence_number": "42"}
		case r.URL.Path == "/view":
			return map[string]any{"unavailable": true}
		case strings.HasPrefix(r.URL.Path, "/transactions/by_hash/"):
			return map[string]any{"hash": "foreign", "type": "pending_transaction"}
		}
		return map[string]any{}
	})
	defer srv.Close()
	if _, e := a.Balance(context.Background(), p.Account); e == nil {
		t.Fatal("unavailable balance became zero")
	}
	if _, e := a.Reconcile(context.Background(), p, s); e == nil {
		t.Fatal("foreign transaction identity")
	}
}
