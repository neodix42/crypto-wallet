package near

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/model"
)

func stakingFixture(t *testing.T, method string) model.Plan {
	t.Helper()
	p := fixturePlan(t, New())
	p.Recipient = "oracle.pool.f863973.m0"
	p.Amount = "123456789012345678901234"
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	d.Gas = StakingGas
	d.Staking = &StakingCall{Method: method, Args: "{}", Deposit: "0", Gas: StakingGas}
	if method == "deposit_and_stake" {
		d.Staking.Deposit = p.Amount
	}
	if method == "unstake" {
		d.Staking.Args = `{"amount":"` + p.Amount + `"}`
	}
	p.MaxFee = "200000000000000000000000"
	p.Payload, _ = json.Marshal(d)
	return p
}

func TestStakingIndependentOfficialSDK(t *testing.T) {
	data, err := os.ReadFile("../../../tests/oracles/near/staking-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Index                                                                                                             uint32
			Address, PublicKey, Recipient, Amount, Method, Args, Deposit, Gas, Nonce, BlockHash, Raw, Hash, Signature, Signed string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) != 12 {
		t.Fatal("missing official SDK vectors")
	}
	for _, v := range fixture.Vectors {
		p := stakingFixture(t, v.Method)
		p.Account, err = New().Derive(seed(), v.Index, "near-testnet")
		if err != nil || p.Account.Address != v.Address || p.Account.PublicKey != v.PublicKey {
			t.Fatalf("derivation index %d", v.Index)
		}
		p.Recipient, p.Amount, p.Nonce = v.Recipient, v.Amount, v.Nonce
		var d Payload
		_ = json.Unmarshal(p.Payload, &d)
		d.BlockHash = v.BlockHash
		d.Staking = &StakingCall{Method: v.Method, Args: v.Args, Deposit: v.Deposit, Gas: v.Gas}
		p.Payload, _ = json.Marshal(d)
		raw, err := transaction(p)
		if err != nil || hex.EncodeToString(raw) != v.Raw {
			t.Fatalf("FunctionCall Borsh index%d method%s: %v", v.Index, v.Method, err)
		}
		signed, err := New().Sign(seed(), p)
		if err != nil || signed.Identity != v.Hash || hex.EncodeToString(signed.Bytes) != v.Signed {
			t.Fatalf("FunctionCall signature index%d method%s: %v", v.Index, v.Method, err)
		}
		if err = New().verify(p, signed); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStakingSignerRejectsArbitraryMethodsAndMutations(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		p := stakingFixture(t, method)
		if err := New().ValidatePlan(p); err != nil {
			t.Fatal(err)
		}
		original, err := New().Sign(seed(), p)
		if err != nil {
			t.Fatal(err)
		}
		for _, mutation := range []string{"method", "args", "deposit", "gas", "pool", "network", "amount", "unknown", "double-args", "missing-pool", "under-gas", "over-gas"} {
			t.Run(method+"/"+mutation, func(t *testing.T) {
				q := p
				var d Payload
				_ = json.Unmarshal(p.Payload, &d)
				switch mutation {
				case "method":
					d.Staking.Method = "add_key"
				case "args":
					d.Staking.Args = `{"receiver_id":"attacker.near"}`
				case "deposit":
					d.Staking.Deposit = "1"
				case "gas":
					d.Staking.Gas = "300000000000000"
				case "pool":
					q.Recipient = "attacker.near"
				case "network":
					q.Account.Network = "near-mainnet"
				case "amount":
					q.Amount = "1"
				case "double-args":
					d.Staking.Args = `{"amount":"1","amount":"2"}`
				case "missing-pool":
					d.RecipientExists = false
				case "under-gas":
					d.Gas = "99999999999999"
				case "over-gas":
					d.Gas = "110000000000001"
				}
				q.Payload, _ = json.Marshal(d)
				if mutation == "unknown" {
					q.Payload = bytes.Replace(q.Payload, []byte(`"method":`), []byte(`"arbitrary":true,"method":`), 1)
				}
				// All-method observed amounts are not serialized and need not
				// change bytes. Their immutable approval binding is signer-owned.
				if mutation == "amount" && (method == "unstake_all" || method == "withdraw_all") {
					return
				}
				if New().ValidatePlan(q) == nil && New().verify(q, original) == nil {
					t.Fatal("mutated intent accepted")
				}
			})
		}
		text, ok := StakingEffects(p)
		if !ok || !strings.Contains(text, p.Recipient) || !strings.Contains(text, method) || !strings.Contains(text, "100 Tgas") {
			t.Fatal("missing exact signer approval details")
		}
		if method != "deposit_and_stake" && !strings.Contains(text, "Attached NEAR: 0.") {
			t.Fatal("unstake/withdraw attached NEAR misrepresented")
		}
		if strings.HasSuffix(method, "_all") && !strings.Contains(text, "observed") {
			t.Fatal("all-operation amount represented as guaranteed")
		}
	}
	if IsStakingPlan(fixturePlan(t, New())) {
		t.Fatal("transfer treated as staking")
	}
	if _, ok := StakingEffects(fixturePlan(t, New())); ok {
		t.Fatal("transfer staking summary")
	}
}

func TestStakingPoolNetworkScope(t *testing.T) {
	for _, v := range []struct {
		network, pool string
		valid         bool
	}{
		{"near-mainnet", "astro-stakers.poolv1.near", true}, {"near-mainnet", "validator.pool.near", true},
		{"near-testnet", "aurora.pool.f863973.m0", true}, {"near-localnet", "pool.sandbox", true},
		{"near-testnet", "astro-stakers.poolv1.near", false}, {"near-mainnet", "pool.sandbox", false},
		{"near-mainnet", "sub.fake.pool.near", false}, {"near-mainnet", "pool.near", false},
		{"near-mainnet", "attacker.pool.near.example", false}, {"near-testnet", "pool.testnet", false},
		{"near-mainnet", "unreviewed.near", false}, {"near-localnet", "unreviewed.sandbox", false},
		{"unknown", "test.pool.near", false}, {"near-mainnet", "UPPER.pool.near", false},
	} {
		if (ValidateStakingPool(v.network, v.pool) == nil) != v.valid {
			t.Fatalf("pool scope %s/%s", v.network, v.pool)
		}
	}
}

func stakingProtocolFixture() map[string]any {
	cfg := protocolFixture()
	actions := cfg["runtime_config"].(map[string]any)["transaction_costs"].(map[string]any)["action_creation_config"].(map[string]any)
	actions["function_call_cost"] = map[string]any{"execution": 231986150000, "send_not_sir": 231986150000, "send_sir": 231986150000}
	actions["function_call_cost_per_byte"] = map[string]any{"execution": 2235934, "send_not_sir": 2235934, "send_sir": 2235934}
	return cfg
}

func TestStakingReadAndPrepareAgainstHostileRPC(t *testing.T) {
	for _, mode := range []string{"valid", "unavailable", "wrong-chain", "wrong-account", "wrong-block", "wrong-height", "negative", "overflow", "sum-overflow", "numeric-amount", "null-amount", "missing-availability", "null-availability", "duplicate-field", "unknown-field", "base64-result", "oversized", "fractional-byte", "malformed-byte", "insufficient", "low-balance", "malformed-cost", "large-cost", "bad-price", "nonce-overflow", "locked"} {
		t.Run(mode, func(t *testing.T) {
			ac, _ := New().Derive(seed(), 0, "near-testnet")
			hash := base58.Encode(make([]byte, 32))
			a, srv := nearServer(t, func(method string, params map[string]json.RawMessage) (any, *rpcError) {
				switch method {
				case "status":
					chainID := "testnet"
					if mode == "wrong-chain" {
						chainID = "mainnet"
					}
					return map[string]any{"chain_id": chainID, "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
				case "query":
					var kind string
					_ = json.Unmarshal(params["request_type"], &kind)
					if kind == "view_access_key" {
						nonce := any(41)
						if mode == "nonce-overflow" {
							nonce = json.Number("18446744073709551615")
						}
						return map[string]any{"nonce": nonce, "permission": "FullAccess", "block_hash": hash, "block_height": 123}, nil
					}
					if kind == "view_account" {
						balance := "1000000000000000000000000000"
						if mode == "low-balance" {
							balance = "1"
						}
						return map[string]any{"amount": balance, "locked": "0", "storage_usage": 182, "block_hash": hash, "block_height": 123}, nil
					}
					if kind != "call_function" {
						t.Errorf("unexpected query %s", kind)
					}
					var methodName, args, blockID, pool string
					_ = json.Unmarshal(params["method_name"], &methodName)
					_ = json.Unmarshal(params["args_base64"], &args)
					_ = json.Unmarshal(params["block_id"], &blockID)
					_ = json.Unmarshal(params["account_id"], &pool)
					decoded, _ := base64.StdEncoding.DecodeString(args)
					if methodName != "get_account" || string(decoded) != `{"account_id":"`+ac.Address+`"}` || blockID != hash || pool != "oracle.pool.f863973.m0" {
						t.Error("unbound staking state request")
					}
					if mode == "unavailable" {
						return nil, &rpcError{Code: -32000}
					}
					state := `{"account_id":"` + ac.Address + `","unstaked_balance":"1000000000000000000000000","staked_balance":"2000000000000000000000000","can_withdraw":true}`
					switch mode {
					case "wrong-account":
						state = strings.Replace(state, ac.Address, "foreign.near", 1)
					case "negative":
						state = strings.Replace(state, `"2000000000000000000000000"`, `"-1"`, 1)
					case "overflow":
						state = strings.Replace(state, `"2000000000000000000000000"`, `"340282366920938463463374607431768211456"`, 1)
					case "sum-overflow":
						state = strings.Replace(state, `"2000000000000000000000000"`, `"340282366920938463463374607431768211455"`, 1)
					case "numeric-amount":
						state = strings.Replace(state, `"2000000000000000000000000"`, `2000000000000000000000000`, 1)
					case "null-amount":
						state = strings.Replace(state, `"2000000000000000000000000"`, `null`, 1)
					case "missing-availability":
						state = strings.Replace(state, `,"can_withdraw":true`, "", 1)
					case "null-availability":
						state = strings.Replace(state, `"can_withdraw":true`, `"can_withdraw":null`, 1)
					case "duplicate-field":
						state = strings.Replace(state, `"can_withdraw":true`, `"can_withdraw":false,"can_withdraw":true`, 1)
					case "unknown-field":
						state = strings.Replace(state, `"can_withdraw":true`, `"can_withdraw":true,"extra":1`, 1)
					case "oversized":
						state = strings.Repeat(" ", 1025)
					case "insufficient":
						state = strings.Replace(state, `"2000000000000000000000000"`, `"1"`, 1)
					case "locked":
						state = strings.Replace(state, `"can_withdraw":true`, `"can_withdraw":false`, 1)
					}
					var result any
					ints := make([]int, len(state))
					for i, v := range []byte(state) {
						ints[i] = int(v)
					}
					result = ints
					if mode == "base64-result" {
						result = base64.StdEncoding.EncodeToString([]byte(state))
					}
					if mode == "fractional-byte" {
						result = []any{123, 1.5, 125}
					}
					if mode == "malformed-byte" {
						result = []any{123, 256, 125}
					}
					r := map[string]any{"result": result, "logs": []string{}, "block_hash": hash, "block_height": 123}
					if mode == "wrong-block" {
						r["block_hash"] = base58.Encode(bytes.Repeat([]byte{1}, 32))
					}
					if mode == "wrong-height" {
						r["block_height"] = 124
					}
					return r, nil
				case "EXPERIMENTAL_protocol_config":
					cfg := stakingProtocolFixture()
					actions := cfg["runtime_config"].(map[string]any)["transaction_costs"].(map[string]any)["action_creation_config"].(map[string]any)
					if mode == "malformed-cost" {
						actions["function_call_cost"] = map[string]any{"execution": 1.5, "send_not_sir": 1}
					}
					if mode == "large-cost" {
						actions["function_call_cost"] = map[string]any{"execution": json.Number("18446744073709551615"), "send_not_sir": 1}
					}
					return cfg, nil
				case "gas_price":
					price := "1000000000"
					if mode == "bad-price" {
						price = "-1"
					}
					return map[string]any{"gas_price": price}, nil
				}
				return nil, &rpcError{Code: -32000}
			})
			defer srv.Close()
			state, err := a.StakingState(context.Background(), ac, "oracle.pool.f863973.m0")
			readOK := map[string]bool{"valid": true, "insufficient": true, "low-balance": true, "malformed-cost": true, "large-cost": true, "bad-price": true, "nonce-overflow": true, "locked": true}[mode]
			if readOK {
				if err != nil || state.AccountID != ac.Address || state.BlockHash != hash || state.Rewards != "" {
					t.Fatalf("state %+v %v", state, err)
				}
			} else if err == nil {
				t.Fatal("hostile get_account accepted")
			}
			for _, action := range []string{"stake", "unstake", "unstake_all", "withdraw_all"} {
				amount := "1000000000000000000000000"
				if strings.HasSuffix(action, "_all") {
					amount = ""
				}
				p, err := a.PrepareStaking(context.Background(), ac, StakingRequest{AccountID: ac.ID, Validator: "oracle.pool.f863973.m0", Action: action, Amount: amount})
				wantOK := mode == "valid" || (mode == "insufficient" && action != "unstake") || (mode == "locked" && action != "withdraw_all")
				if !wantOK {
					if err == nil {
						t.Fatalf("hostile%s accepted action%s", mode, action)
					}
					continue
				}
				if err != nil || p.Nonce != "42" || p.Expires <= time.Now().Unix() {
					t.Fatalf("prepare%s: %+v %v", action, p, err)
				}
				var d Payload
				_ = json.Unmarshal(p.Payload, &d)
				if d.Staking.Gas != StakingGas {
					t.Fatal("signed prepaid gas")
				}
				gas, _ := new(big.Int).SetString(d.Gas, 10)
				if gas.Cmp(big.NewInt(100_000_000_000_000)) <= 0 {
					t.Fatal("omitted runtime fee overhead")
				}
				if action != "stake" && d.Staking.Deposit != "0" {
					t.Fatal("unstake or withdrawal attached deposit")
				}
				if _, err = a.Sign(seed(), p); err != nil {
					t.Fatalf("actual staking signature: %v", err)
				}
			}
		})
	}
}

func TestStakingIntentRejectsInvalidAmounts(t *testing.T) {
	s := StakingState{Staked: "100", Unstaked: "100", CanWithdraw: true}
	for _, r := range []StakingRequest{{Action: "call", Amount: "1"}, {Action: "stake", Amount: "0"}, {Action: "unstake", Amount: "101"}, {Action: "stake", Amount: "01"}, {Action: "unstake", Amount: "1.5"}, {Action: "stake", Amount: "-1"}, {Action: "unstake_all", Amount: "1"}, {Action: "withdraw_all", Amount: "1"}} {
		if _, _, err := requestedCall(r, s); err == nil {
			t.Fatalf("invalid intent %+v", r)
		}
	}
	if _, _, err := requestedCall(StakingRequest{Action: "unstake_all"}, StakingState{Staked: "0"}); err == nil {
		t.Fatal("empty stake")
	}
	if _, _, err := requestedCall(StakingRequest{Action: "withdraw_all"}, StakingState{Unstaked: "0", CanWithdraw: true}); err == nil {
		t.Fatal("empty withdrawal")
	}
	a := New()
	account, _ := a.Derive(seed(), 0, "near-testnet")
	for _, r := range []StakingRequest{
		{AccountID: "foreign", Validator: "oracle.pool.f863973.m0", Action: "stake", Amount: "1"},
		{AccountID: account.ID, Validator: "foreign.testnet", Action: "stake", Amount: "1"},
		{AccountID: account.ID, Validator: "oracle.pool.f863973.m0", Action: "arbitrary", Amount: "1"},
		{AccountID: account.ID, Validator: "oracle.pool.f863973.m0", Action: "unstake_all", Amount: "1"},
		{AccountID: account.ID, Validator: "oracle.pool.f863973.m0", Action: "stake", Amount: "0"},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := a.PrepareStaking(ctx, account, r); err == nil {
			t.Fatal("invalid public staking intent")
		}
	}
	if _, err := a.StakingState(context.Background(), account, "foreign.testnet"); err == nil {
		t.Fatal("arbitrary contract state call")
	}
	bad := stakingFixture(t, "unstake_all")
	bad.MaxFee = "0"
	if _, ok := StakingEffects(bad); ok {
		t.Fatal("unvalidated staking approval text")
	}
}

func TestStakingBoundedMalformedStress(t *testing.T) {
	p := stakingFixture(t, "unstake")
	a := New()
	for i := 0; i < 10000; i++ {
		q := p
		var d Payload
		_ = json.Unmarshal(p.Payload, &d)
		switch i % 8 {
		case 0:
			d.Staking.Method = "transfer"
		case 1:
			d.Staking.Deposit = "1"
		case 2:
			d.Staking.Gas = "300000000000001"
		case 3:
			d.Staking.Args = `{"amount":"1","amount":"2"}`
		case 4:
			q.Recipient = "attacker.pool.f863973.m0.example"
		case 5:
			q.Amount = "340282366920938463463374607431768211456"
		case 6:
			q.Amount = "-" + strconv.Itoa(i)
		case 7:
			d.Staking.Args = strings.Repeat("[", 128) + strings.Repeat("]", 128)
		}
		q.Payload, _ = json.Marshal(d)
		if err := a.ValidatePlan(q); err == nil {
			t.Fatalf("malformed staking case %d accepted", i)
		}
	}
}

func TestStakingSubmissionReconciliationPreservesOriginalIdentity(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		for _, mode := range []string{"confirmed", "failed", "pending", "timeout-after-acceptance", "nonce-changed", "gas-spike", "wrong-signer", "wrong-receiver", "wrong-identity"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				p := stakingFixture(t, method)
				signed, err := New().Sign(seed(), p)
				if err != nil {
					t.Fatal(err)
				}
				broadcasts := 0
				a, srv := nearServer(t, func(rpcMethod string, params map[string]json.RawMessage) (any, *rpcError) {
					switch rpcMethod {
					case "status":
						return map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
					case "query":
						var kind string
						_ = json.Unmarshal(params["request_type"], &kind)
						if kind == "view_access_key" {
							nonce := 41
							if mode == "nonce-changed" {
								nonce = 42
							}
							return map[string]any{"nonce": nonce, "permission": "FullAccess", "block_height": 123, "block_hash": base58.Encode(make([]byte, 32))}, nil
						}
						return map[string]any{"amount": "1000000000000000000000000000", "storage_usage": 182, "block_height": 123, "block_hash": base58.Encode(make([]byte, 32))}, nil
					case "gas_price":
						price := "1000000000"
						if mode == "gas-spike" {
							price = "2000000001"
						}
						return map[string]any{"gas_price": price}, nil
					case "send_tx", "tx":
						if rpcMethod == "send_tx" {
							broadcasts++
							var encoded string
							_ = json.Unmarshal(params["signed_tx_base64"], &encoded)
							got, _ := base64.StdEncoding.DecodeString(encoded)
							if !bytes.Equal(got, signed.Bytes) {
								t.Error("broadcast replaced exact approved bytes")
							}
							if mode == "timeout-after-acceptance" {
								return nil, &rpcError{Code: -32000}
							}
						} else {
							var identity string
							_ = json.Unmarshal(params["tx_hash"], &identity)
							if identity != signed.Identity {
								t.Error("reconciliation changed original transaction identity")
							}
						}
						state := map[string]any{"SuccessValue": ""}
						if mode == "failed" {
							state = map[string]any{"Failure": map[string]any{"ActionError": "synthetic pool failure"}}
						}
						final := "FINAL"
						if mode == "pending" {
							final = "EXECUTED"
						}
						signer, receiver, identity := p.Account.Address, p.Recipient, signed.Identity
						if mode == "wrong-signer" {
							signer = "attacker.near"
						}
						if mode == "wrong-receiver" {
							receiver = "attacker.pool.near"
						}
						if mode == "wrong-identity" {
							identity = "foreign"
						}
						block := base58.Encode(make([]byte, 32))
						receiptID := base58.Encode(bytes.Repeat([]byte{1}, 32))
						return map[string]any{"status": state, "final_execution_status": final, "transaction": map[string]any{"hash": identity, "signer_id": signer, "receiver_id": receiver}, "transaction_outcome": map[string]any{"block_hash": block, "outcome": map[string]any{"receipt_ids": []string{receiptID}}}, "receipts_outcome": []any{map[string]any{"id": receiptID, "block_hash": block, "outcome": map[string]any{"executor_id": p.Recipient, "status": map[string]any{"SuccessValue": ""}, "receipt_ids": []string{}}}}}, nil
					}
					return nil, &rpcError{Code: -32000}
				})
				defer srv.Close()
				receipt, err := a.Submit(context.Background(), p, signed)
				want := map[string]string{"confirmed": "CONFIRMED", "failed": "FAILED", "pending": "SUBMITTED"}[mode]
				if want != "" {
					if err != nil || receipt.State != want {
						t.Fatalf("submit %+v %v", receipt, err)
					}
				} else if err == nil {
					t.Fatal("unsafe submission marked successful")
				}
				if (mode == "nonce-changed" || mode == "gas-spike") && broadcasts != 0 {
					t.Fatal("broadcast after lost approval state")
				}
				// Local plan expiry does not discard durable signed identity.
				p.Expires = time.Now().Unix() - 1
				reconciled, reconcileErr := a.Reconcile(context.Background(), p, signed)
				if strings.HasPrefix(mode, "wrong-") {
					if reconcileErr == nil {
						t.Fatal("foreign receipt reconciled")
					}
				} else {
					if want == "" {
						want = "CONFIRMED"
					}
					if reconcileErr != nil || reconciled.State != want || reconciled.Identity != signed.Identity {
						t.Fatalf("original reconciliation %+v %v", reconciled, reconcileErr)
					}
				}
				if broadcasts > 1 {
					t.Fatal("reconciliation broadcast a replacement")
				}
			})
		}
	}
}

func FuzzStakingCallPolicy(f *testing.F) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all", "add_key", "withdraw"} {
		f.Add(method, "{}", "0", StakingGas, "oracle.pool.f863973.m0", "1")
	}
	f.Fuzz(func(t *testing.T, method, args, deposit, gas, pool, effect string) {
		if len(method)+len(args)+len(deposit)+len(gas)+len(pool)+len(effect) > 4096 {
			return
		}
		p := stakingFixture(t, "unstake_all")
		p.Recipient, p.Amount = pool, effect
		var d Payload
		_ = json.Unmarshal(p.Payload, &d)
		d.Staking = &StakingCall{Method: method, Args: args, Deposit: deposit, Gas: gas}
		p.Payload, _ = json.Marshal(d)
		err := New().ValidatePlan(p)
		if err != nil {
			return
		}
		if ValidateStakingPool(p.Account.Network, pool) != nil || gas != StakingGas {
			t.Fatal("unscoped pool or gas accepted")
		}
		switch method {
		case "deposit_and_stake":
			if deposit != effect || args != "{}" {
				t.Fatal("stake effect/deposit substitution")
			}
		case "unstake":
			if deposit != "0" || args != fmt.Sprintf(`{"amount":"%s"}`, effect) {
				t.Fatal("unstake argument/deposit substitution")
			}
		case "unstake_all", "withdraw_all":
			if deposit != "0" || args != "{}" {
				t.Fatal("all-operation arbitrary data/deposit")
			}
		default:
			t.Fatal("arbitrary contract method accepted")
		}
		if _, err := New().Sign(seed(), p); err != nil {
			t.Fatalf("valid bounded intent did not sign: %v", err)
		}
	})
}

func TestStakingRequiresCompleteSuccessfulDownstreamReceipts(t *testing.T) {
	for _, method := range []string{"deposit_and_stake", "unstake", "unstake_all", "withdraw_all"} {
		for _, mode := range []string{"success", "failure", "pending", "absent", "downstream-failure", "unknown-status", "duplicate-receipt", "missing-child", "missing-root-reference", "missing-root-block", "missing-child-metadata", "bad-receipt-id", "bad-receipt-block", "bad-executor", "no-pool-executor", "null-status", "ambiguous-status", "null-success-value", "invalid-base64", "large-value", "null-next-id", "bad-next-id", "missing-next-id", "valid-next-id", "unlinked-receipt", "non-string-root-success"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				p := stakingFixture(t, method)
				block := base58.Encode(make([]byte, 32))
				ids := []string{base58.Encode(bytes.Repeat([]byte{1}, 32)), base58.Encode(bytes.Repeat([]byte{2}, 32)), base58.Encode(bytes.Repeat([]byte{3}, 32))}
				r := execution{Final: "FINAL", Status: json.RawMessage(`{"SuccessValue":""}`)}
				r.Transaction.Hash = "original-transaction"
				r.Outcome.Block = block
				r.Outcome.Outcome.ReceiptIDs = []string{ids[0]}
				first := receiptOutcome{ID: ids[0], Block: block}
				first.Outcome.Executor = p.Recipient
				first.Outcome.Status = json.RawMessage(`{"SuccessValue":""}`)
				first.Outcome.ReceiptIDs = []string{ids[1]}
				second := receiptOutcome{ID: ids[1], Block: block}
				second.Outcome.Executor = p.Account.Address
				second.Outcome.Status = json.RawMessage(`{"SuccessValue":""}`)
				second.Outcome.ReceiptIDs = []string{}
				r.Receipts = []receiptOutcome{first, second}
				want := "UNKNOWN"
				switch mode {
				case "success":
					want = "CONFIRMED"
				case "failure":
					r.Status = json.RawMessage(`{"Failure":{"ActionError":"fixture"}}`)
					want = "FAILED"
				case "pending":
					r.Final = "EXECUTED"
					want = "SUBMITTED"
				case "absent":
					r.Receipts = nil
				case "downstream-failure":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"Failure":{"ActionError":"transfer or stake failed"}}`)
				case "unknown-status":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"Unknown":null}`)
				case "duplicate-receipt":
					r.Receipts[1].ID = ids[0]
				case "missing-child":
					r.Receipts = r.Receipts[:1]
				case "missing-root-reference":
					r.Outcome.Outcome.ReceiptIDs = nil
				case "missing-root-block":
					r.Outcome.Block = ""
				case "missing-child-metadata":
					r.Receipts[1].Outcome.ReceiptIDs = nil
				case "bad-receipt-id":
					r.Receipts[0].ID = "bad"
				case "bad-receipt-block":
					r.Receipts[1].Block = "bad"
				case "bad-executor":
					r.Receipts[1].Outcome.Executor = "UPPER.near"
				case "no-pool-executor":
					r.Receipts[0].Outcome.Executor = p.Account.Address
				case "null-status":
					r.Receipts[1].Outcome.Status = json.RawMessage(`null`)
				case "ambiguous-status":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"SuccessValue":"","SuccessReceiptId":"` + ids[0] + `"}`)
				case "null-success-value":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"SuccessValue":null}`)
				case "invalid-base64":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"SuccessValue":"!"}`)
				case "large-value":
					r.Receipts[1].Outcome.Status = json.RawMessage(`{"SuccessValue":"` + strings.Repeat("A", 2049) + `"}`)
				case "null-next-id":
					r.Receipts[0].Outcome.Status = json.RawMessage(`{"SuccessReceiptId":null}`)
				case "bad-next-id":
					r.Receipts[0].Outcome.Status = json.RawMessage(`{"SuccessReceiptId":"bad"}`)
				case "missing-next-id":
					r.Receipts[0].Outcome.Status = json.RawMessage(`{"SuccessReceiptId":"` + ids[2] + `"}`)
				case "valid-next-id":
					r.Receipts[0].Outcome.Status = json.RawMessage(`{"SuccessReceiptId":"` + ids[1] + `"}`)
					want = "CONFIRMED"
				case "unlinked-receipt":
					r.Receipts[0].Outcome.ReceiptIDs = []string{}
				case "non-string-root-success":
					r.Status = json.RawMessage(`{"SuccessValue":123}`)
				}
				got, err := stakingReceipt(p, "original-transaction", r)
				if err != nil || got.State != want || got.Identity != "original-transaction" {
					t.Fatalf("receipt %+v %v; want%s", got, err, want)
				}
				if want == "UNKNOWN" && !strings.Contains(got.Detail, "committed") {
					t.Fatal("ambiguous downstream result hid committed root effects")
				}
			})
		}
	}
}
