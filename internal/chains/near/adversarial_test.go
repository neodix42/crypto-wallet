package near

import (
	"context"
	"encoding/json"
	"localwallet/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func protocolFixture() map[string]any {
	return map[string]any{"chain_id": "testnet", "protocol_version": 88, "runtime_config": map[string]any{"storage_amount_per_byte": "10000000000000000000", "min_gas_purchase_price": "1000000000", "transaction_costs": map[string]any{"action_receipt_creation_config": map[string]any{"execution": 108059500000, "send_not_sir": 108059500000, "send_sir": 108059500000}, "action_creation_config": map[string]any{"transfer_cost": map[string]any{"execution": 115123062500, "send_not_sir": 115123062500, "send_sir": 115123062500}, "create_account_cost": map[string]any{"execution": 7200000000000, "send_not_sir": 500000000000, "send_sir": 500000000000}, "add_key_cost": map[string]any{"full_access_cost": map[string]any{"execution": 101765125000, "send_not_sir": 101765125000, "send_sir": 101765125000}}}}}}
}
func TestTP051PrepareHazards(t *testing.T) {
	for _, mode := range []string{"valid", "implicit", "missing-named", "recipient-outage", "source-outage", "key-outage", "revoked", "hash", "nonce-overflow", "nonce-malformed", "protocol-outage", "protocol-chain", "protocol-version", "gas-outage", "gas-malformed", "gas-floor", "storage-malformed", "usage-malformed", "balance-malformed", "insufficient", "cost-malformed", "zero-cost", "head-stale", "head-sync", "head-malformed", "status-outage"} {
		t.Run(mode, func(t *testing.T) {
			a, srv := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
				rpcErr := func() (any, *rpcError) { return nil, &rpcError{Code: -32000} }
				switch method {
				case "status":
					if mode == "status-outage" {
						return rpcErr()
					}
					sync := false
					timestamp := time.Now().UTC().Format(time.RFC3339Nano)
					if mode == "head-sync" {
						sync = true
					}
					if mode == "head-stale" {
						timestamp = time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano)
					}
					if mode == "head-malformed" {
						timestamp = "bad"
					}
					return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"syncing": sync, "latest_block_time": timestamp}}, nil
				case "query":
					var req, id string
					_ = json.Unmarshal(params["request_type"], &req)
					_ = json.Unmarshal(params["account_id"], &id)
					if req == "view_access_key" {
						if mode == "key-outage" {
							return rpcErr()
						}
						nonce := any(41)
						if mode == "nonce-overflow" {
							nonce = json.Number("18446744073709551615")
						}
						if mode == "nonce-malformed" {
							nonce = json.Number("1.5")
						}
						permission := any("FullAccess")
						if mode == "revoked" {
							permission = map[string]any{"FunctionCall": map[string]any{}}
						}
						hash := fixturePlan(t, New())
						var d Payload
						_ = json.Unmarshal(hash.Payload, &d)
						block := d.BlockHash
						if mode == "hash" {
							block = "wrong"
						}
						return map[string]any{"nonce": nonce, "permission": permission, "block_hash": block, "block_height": 123}, nil
					}
					if id == "receiver.testnet" || strings.HasPrefix(id, "000000") {
						if mode == "recipient-outage" {
							return rpcErr()
						}
						if mode == "missing-named" || mode == "implicit" {
							r := &rpcError{Code: -32000}
							r.Cause.Name = "UNKNOWN_ACCOUNT"
							return nil, r
						}
					}
					if mode == "source-outage" {
						return rpcErr()
					}
					amount := "1000000000000000000000000000"
					if mode == "balance-malformed" {
						amount = "-1"
					}
					if mode == "insufficient" {
						amount = "1"
					}
					usage := any(182)
					if mode == "usage-malformed" {
						usage = json.Number("1.5")
					}
					p := fixturePlan(t, New())
					var d Payload
					_ = json.Unmarshal(p.Payload, &d)
					return map[string]any{"amount": amount, "locked": "0", "storage_usage": usage, "block_hash": d.BlockHash, "block_height": 123}, nil
				case "EXPERIMENTAL_protocol_config":
					if mode == "protocol-outage" {
						return rpcErr()
					}
					p := protocolFixture()
					if mode == "protocol-chain" {
						p["chain_id"] = "mainnet"
					}
					if mode == "protocol-version" {
						p["protocol_version"] = 1.5
					}
					rt := p["runtime_config"].(map[string]any)
					if mode == "storage-malformed" {
						rt["storage_amount_per_byte"] = "bad"
					}
					actions := rt["transaction_costs"].(map[string]any)["action_creation_config"].(map[string]any)
					transfer := actions["transfer_cost"].(map[string]any)
					if mode == "cost-malformed" {
						transfer["execution"] = 1.5
					}
					if mode == "zero-cost" {
						transfer["execution"] = json.Number("18446744073709551615")
					}
					return p, nil
				case "gas_price":
					if mode == "gas-outage" {
						return rpcErr()
					}
					price := "1000000000"
					if mode == "gas-floor" {
						price = "100"
					}
					if mode == "gas-malformed" {
						price = "-1"
					}
					return map[string]any{"gas_price": price}, nil
				}
				return rpcErr()
			})
			defer srv.Close()
			ac, _ := a.Derive(seed(), 0, "near-testnet")
			recipient := "receiver.testnet"
			if mode == "implicit" {
				recipient = strings.Repeat("0", 64)
			}
			p, e := a.Prepare(context.Background(), ac, model.Transfer{AccountID: ac.ID, Asset: asset(ac.Network), Recipient: recipient, Amount: "123456789012345678901234"})
			if mode == "valid" || mode == "implicit" || mode == "gas-floor" {
				if e != nil {
					t.Fatal(e)
				}
				if p.Nonce != "42" {
					t.Fatal("nonce")
				}
				if mode != "implicit" && p.MaxFee != "892730250000000000000" {
					t.Fatalf("fee %s", p.MaxFee)
				}
			} else if e == nil {
				t.Fatalf("hazard accepted: %s", mode)
			}
		})
	}
}
func TestTP042BoundedPlanFields(t *testing.T) {
	a := New()
	base := fixturePlan(t, a)
	for _, field := range []string{"version", "blockHash", "blockHeight", "protocol", "gas", "gasPrice", "storageReserve", "recipientExists", "prepared", "feePolicy", "unknown"} {
		p := base
		var data map[string]any
		_ = json.Unmarshal(p.Payload, &data)
		switch field {
		case "version":
			data[field] = 2
		case "blockHash":
			data[field] = "111"
		case "blockHeight", "protocol", "gas", "gasPrice", "storageReserve":
			data[field] = "-1"
		case "recipientExists":
			data[field] = false
		case "prepared":
			data[field] = 0
		case "feePolicy":
			data[field] = "guaranteed"
		case "unknown":
			data[field] = true
		}
		p.Payload, _ = json.Marshal(data)
		if a.ValidatePlan(p) == nil {
			t.Fatal(field)
		}
	}
	for _, mutate := range []func(*model.Plan){func(p *model.Plan) { p.Amount = "0" }, func(p *model.Plan) { p.Nonce = "0" }, func(p *model.Plan) { p.Account.Index = 1 << 31 }, func(p *model.Plan) { p.Account.Family = "other" }, func(p *model.Plan) { p.Account.Parameters = []byte(`{}`) }, func(p *model.Plan) { p.Account.Address = "other.near" }, func(p *model.Plan) { p.Account.PublicKey = "bad" }} {
		p := base
		mutate(&p)
		if a.ValidatePlan(p) == nil {
			t.Fatal("invalid plan accepted")
		}
		if _, e := a.Sign(seed(), p); e == nil {
			t.Fatal("invalid sign")
		}
		if _, e := a.Submit(context.Background(), p, model.Signed{}); e == nil {
			t.Fatal("invalid submit")
		}
		if _, e := a.Reconcile(context.Background(), p, model.Signed{}); e == nil {
			t.Fatal("invalid reconcile")
		}
	}
	if _, e := a.Derive(seed(), 0, "unknown"); e == nil {
		t.Fatal("network")
	}
	if a.SetEndpoint("unknown", "https://example.org") == nil {
		t.Fatal("network")
	}
	for _, r := range []execution{{Transaction: struct {
		Hash     string `json:"hash"`
		Signer   string `json:"signer_id"`
		Receiver string `json:"receiver_id"`
	}{Hash: "wrong"}}, {Final: "FINAL", Status: json.RawMessage(`"Unknown"`)}, {Final: "FINAL", Status: json.RawMessage(`{}`)}} {
		_, _ = receipt("id", r)
	}
	rpcFailure := &rpcError{Code: -32000, Message: "PUBLIC-RPC-CANARY", Data: json.RawMessage(`{"request":"PUBLIC-RPC-CANARY"}`)}
	if strings.Contains(rpcFailure.Error(), "PUBLIC-RPC-CANARY") {
		t.Fatal("untrusted RPC context leaked through error")
	}
}

func TestTP012RPCEnvelopeAndRecoveryFaults(t *testing.T) {
	for _, body := range []string{`{"jsonrpc":"2.0","id":"other","result":{}}`, `{"jsonrpc":"1.0","id":"wallet","result":{}}`, `{"jsonrpc":"2.0","id":"wallet","result":null}`, `{"jsonrpc":"2.0","id":"wallet","result":[]}`, `{"jsonrpc":"2.0","id":"wallet","result":{"amount":"bad"}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		a := New()
		_ = a.SetEndpoint("near-testnet", srv.URL)
		var out accountState
		if body == `{"jsonrpc":"2.0","id":"wallet","result":{"amount":"bad"}}` {
			ac, _ := a.Derive(seed(), 0, "near-testnet")
			_, _ = a.Balance(context.Background(), ac)
		} else if a.rpc(context.Background(), "near-testnet", "query", nil, &out) == nil {
			t.Fatal("malformed envelope")
		}
		srv.Close()
	}
	a := New()
	var out accountState
	if a.rpc(context.Background(), "wrong", "query", nil, &out) == nil {
		t.Fatal("network")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.rpc(ctx, "near-testnet", "status", nil, &out) == nil {
		t.Fatal("cancelled request")
	}
	p := fixturePlan(t, a)
	s, _ := a.Sign(seed(), p)
	for _, field := range []string{"payload", "public", "nonce", "amount"} {
		q := p
		switch field {
		case "payload":
			q.Payload = []byte(`{}`)
		case "public":
			q.Account.PublicKey = "invalid"
		case "nonce":
			q.Nonce = "-1"
		case "amount":
			q.Amount = "-1"
		}
		if _, e := transaction(q); e == nil {
			t.Fatal("invalid transaction codec input")
		}
		if a.verify(q, s) == nil {
			t.Fatal("invalid artifact association")
		}
	}
	q := p
	q.Account.Parameters = []byte(`{"path":"other","implicit":"x","named":false}`)
	if a.account(q.Account) == nil {
		t.Fatal("descriptor mismatch")
	}
	var params Parameters
	_ = json.Unmarshal(p.Account.Parameters, &params)
	params.Named = true
	q = p
	q.Account.Parameters, _ = json.Marshal(params)
	if a.account(q.Account) == nil {
		t.Fatal("named implicit confusion")
	}
	q = p
	q.Account.Address = "bad!!"
	if _, e := a.Associate(ctx, q.Account, "ok.near"); e == nil {
		t.Fatal("bad source")
	}
	if _, e := a.Associate(ctx, p.Account, "bad!!"); e == nil {
		t.Fatal("bad destination")
	}
	if _, _, e := a.state(ctx, q.Account); e == nil {
		t.Fatal("bad state account")
	}
	for _, tr := range []model.Transfer{{AccountID: "wrong"}, {AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "0"}} {
		if _, e := a.Prepare(ctx, p.Account, tr); e == nil {
			t.Fatal("invalid transfer")
		}
	}
	_, _ = receipt("id", execution{Final: "FINAL", Status: json.RawMessage(`{}`), Transaction: struct {
		Hash     string `json:"hash"`
		Signer   string `json:"signer_id"`
		Receiver string `json:"receiver_id"`
	}{Hash: "id"}})
	_, _ = receipt("id", execution{Final: "FINAL", Status: json.RawMessage(`{"Unknown":""}`), Transaction: struct {
		Hash     string `json:"hash"`
		Signer   string `json:"signer_id"`
		Receiver string `json:"receiver_id"`
	}{Hash: "id"}})
}
func TestTP060SubmissionFaultsStayUnknown(t *testing.T) {
	for _, mode := range []string{"nonce", "key", "gas-rpc", "gas-parse", "gas-spike", "broadcast", "status", "foreign-receipt", "invalid-balance", "invalid-minimum"} {
		t.Run(mode, func(t *testing.T) {
			p := fixturePlan(t, New())
			s, _ := New().Sign(seed(), p)
			var d Payload
			_ = json.Unmarshal(p.Payload, &d)
			a, srv := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
				switch method {
				case "status":
					if mode == "status" {
						return nil, &rpcError{Code: -1}
					}
					return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
				case "query":
					var req string
					_ = json.Unmarshal(params["request_type"], &req)
					if req == "view_access_key" {
						if mode == "key" {
							return nil, &rpcError{Code: -1}
						}
						nonce := 41
						if mode == "nonce" {
							nonce = 42
						}
						return map[string]any{"nonce": nonce, "permission": "FullAccess", "block_hash": d.BlockHash, "block_height": 123}, nil
					}
					amount := "1000000000000000000000000000"
					if mode == "invalid-balance" {
						amount = "bad"
					}
					return map[string]any{"amount": amount, "storage_usage": 182, "block_height": 123, "block_hash": d.BlockHash}, nil
				case "gas_price":
					if mode == "gas-rpc" {
						return nil, &rpcError{Code: -1}
					}
					price := "1000000000"
					if mode == "gas-parse" {
						price = "bad"
					}
					if mode == "gas-spike" {
						price = "999999999999999"
					}
					return map[string]any{"gas_price": price}, nil
				case "send_tx":
					return nil, &rpcError{Code: -1}
				case "tx":
					if mode == "foreign-receipt" {
						return map[string]any{"transaction": map[string]any{"hash": s.Identity, "signer_id": "foreign", "receiver_id": p.Recipient}}, nil
					}
					return nil, &rpcError{Code: -1}
				case "EXPERIMENTAL_protocol_config":
					cfg := protocolFixture()
					cfg["runtime_config"].(map[string]any)["min_gas_purchase_price"] = "bad"
					return cfg, nil
				}
				return nil, &rpcError{Code: -1}
			})
			defer srv.Close()
			if mode == "invalid-balance" {
				if _, e := a.Balance(context.Background(), p.Account); e == nil {
					t.Fatal("malformed balance")
				}
				return
			}
			if mode == "invalid-minimum" {
				if _, e := a.Prepare(context.Background(), p.Account, model.Transfer{AccountID: p.Account.ID, Asset: p.Asset, Recipient: p.Recipient, Amount: "1"}); e == nil {
					t.Fatal("minimum gas parse")
				}
				return
			}
			r, e := a.Submit(context.Background(), p, s)
			if e == nil || r.State != "UNKNOWN" {
				t.Fatalf("failed preflight/broadcast %+v %v", r, e)
			}
			r, e = a.Reconcile(context.Background(), p, s)
			if e == nil || r.State != "UNKNOWN" {
				t.Fatalf("failed reconciliation %+v %v", r, e)
			}
		})
	}
}
func TestTP032RecoveryDescriptor(t *testing.T) {
	a := New()
	expected, _ := a.Derive(seed(), 1, "near-testnet")
	if ValidateRecovery(expected, expected) != nil {
		t.Fatal("canonical descriptor")
	}
	restored := expected
	var p Parameters
	_ = json.Unmarshal(restored.Parameters, &p)
	p.Named = true
	restored.Address = "associated.testnet"
	restored.Parameters, _ = json.Marshal(p)
	if ValidateRecovery(expected, restored) != nil {
		t.Fatal("named association not recoverable")
	}
	restored.ID = "foreign"
	if ValidateRecovery(expected, restored) == nil {
		t.Fatal("account ID substitution")
	}
	restored = expected
	p.Path = "m/44'/397'/0'"
	restored.Parameters, _ = json.Marshal(p)
	if ValidateRecovery(expected, restored) == nil {
		t.Fatal("path substitution")
	}
}

func TestTP042MalformedBoundaryFailures(t *testing.T) {
	a := New()
	p := fixturePlan(t, a)
	s, _ := a.Sign(seed(), p)
	if _, e := a.Sign(make([]byte, 32), p); e == nil {
		t.Fatal("short seed")
	}
	ac := p.Account
	ac.Parameters = []byte(`{"unknown":true}`)
	if a.account(ac) == nil {
		t.Fatal("unknown descriptor field")
	}
	bad := s
	bad.Bytes = append([]byte{}, s.Bytes...)
	bad.Bytes[len(bad.Bytes)-1] ^= 1
	if _, e := a.Submit(context.Background(), p, bad); e == nil {
		t.Fatal("modified artifact")
	}
	q := p
	q.Payload = []byte(`{`)
	if _, e := transaction(q); e == nil {
		t.Fatal("malformed codec metadata")
	}
}
