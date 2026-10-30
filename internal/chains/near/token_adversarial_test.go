package near

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"localwallet/internal/model"
)

func stringParam(params map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(params[key], &value)
	return value
}
func changeView(result map[string]any, change func(string) string) {
	values := result["result"].([]int)
	raw := make([]byte, len(values))
	for i, v := range values {
		raw[i] = byte(v)
	}
	raw = []byte(change(string(raw)))
	values = make([]int, len(raw))
	for i, v := range raw {
		values[i] = int(v)
	}
	result["result"] = values
}
func TestNEP141HostileRPCBranchesPreserveNoBroadcastAndUnknowns(t *testing.T) {
	cases := []struct {
		name, scope, operation string
		change                 func(map[string]any)
	}{
		{"block-rpc", "block", "inspect", nil},
		{"block-hash", "block", "inspect", func(r map[string]any) { r["header"].(map[string]any)["hash"] = "bad" }},
		{"block-height", "block", "inspect", func(r map[string]any) { r["header"].(map[string]any)["height"] = -1 }},
		{"block-timestamp", "block", "inspect", func(r map[string]any) { r["header"].(map[string]any)["timestamp_nanosec"] = "bad" }},
		{"block-time-overflow", "block", "inspect", func(r map[string]any) {
			r["header"].(map[string]any)["timestamp_nanosec"] = json.Number("18446744073709551615")
		}},
		{"block-future", "block", "inspect", func(r map[string]any) {
			r["header"].(map[string]any)["timestamp_nanosec"] = time.Now().Add(2 * time.Minute).UnixNano()
		}},
		{"contract-rpc", "contract", "inspect", nil},
		{"metadata-rpc", "ft_metadata", "inspect", nil},
		{"metadata-empty", "ft_metadata", "inspect", func(r map[string]any) { changeView(r, func(string) string { return "" }) }},
		{"metadata-no-name", "ft_metadata", "inspect", func(r map[string]any) {
			changeView(r, func(s string) string { return strings.Replace(s, `"name":"Test Token",`, "", 1) })
		}},
		{"metadata-bad-name", "ft_metadata", "inspect", func(r map[string]any) {
			changeView(r, func(s string) string { return strings.Replace(s, "Test Token", `Bad\u0000Name`, 1) })
		}},
		{"metadata-huge-icon", "ft_metadata", "inspect", func(r map[string]any) {
			changeView(r, func(s string) string {
				return strings.Replace(s, `"icon":null`, `"icon":"`+strings.Repeat("x", 8193)+`"`, 1)
			})
		}},
		{"metadata-huge-reference", "ft_metadata", "inspect", func(r map[string]any) {
			changeView(r, func(s string) string {
				return strings.Replace(s, `"reference":null`, `"reference":"`+strings.Repeat("x", 2049)+`"`, 1)
			})
		}},
		{"metadata-huge-referencehash", "ft_metadata", "inspect", func(r map[string]any) {
			changeView(r, func(s string) string {
				return strings.Replace(s, `"reference_hash":null`, `"reference_hash":"`+strings.Repeat("x", 129)+`"`, 1)
			})
		}},
		{"metadata-overall-bound", "ft_metadata", "inspect", func(r map[string]any) { changeView(r, func(string) string { return strings.Repeat(" ", 16385) }) }},
		{"metadata-invalid-byte", "ft_metadata", "inspect", func(r map[string]any) { r["result"] = []any{1, 1.5} }},
		{"metadata-raw-bound", "ft_metadata", "inspect", func(r map[string]any) { r["result"] = strings.Repeat("x", 90000) }},
		{"metadata-bad-height", "ft_metadata", "inspect", func(r map[string]any) { r["block_height"] = 124 }},
		{"storage-bounds-rpc", "storage_balance_bounds", "storage", nil},
		{"storage-balance-rpc", "storage_balance_of", "storage", nil},
		{"storage-bounds-missing", "storage_balance_bounds", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"min":"1"}` }) }},
		{"storage-bounds-invalid-min", "storage_balance_bounds", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"min":"-1","max":null}` }) }},
		{"storage-bounds-invalid-max", "storage_balance_bounds", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"min":"1","max":2}` }) }},
		{"storage-bounds-max-less", "storage_balance_bounds", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"min":"2","max":"1"}` }) }},
		{"storage-balance-missing", "storage_balance_of", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"total":"1"}` }) }},
		{"storage-balance-invalid", "storage_balance_of", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"total":"-1","available":"0"}` }) }},
		{"storage-balance-less-min", "storage_balance_of", "storage", func(r map[string]any) { changeView(r, func(string) string { return `{"total":"1","available":"0"}` }) }},
		{"token-balance-rpc", "ft_balance_of", "prepare", nil},
		{"token-balance-null", "ft_balance_of", "prepare", func(r map[string]any) { changeView(r, func(string) string { return `null` }) }},
		{"token-balance-insufficient", "ft_balance_of", "prepare", func(r map[string]any) { changeView(r, func(string) string { return `"1"` }) }},
		{"protocol-rpc", "EXPERIMENTAL_protocol_config", "prepare", nil},
		{"protocol-chain", "EXPERIMENTAL_protocol_config", "prepare", func(r map[string]any) { r["chain_id"] = "mainnet" }},
		{"protocol-storage", "EXPERIMENTAL_protocol_config", "prepare", func(r map[string]any) { r["runtime_config"].(map[string]any)["storage_amount_per_byte"] = "invalid" }},
		{"protocol-min-price", "EXPERIMENTAL_protocol_config", "prepare", func(r map[string]any) { r["runtime_config"].(map[string]any)["min_gas_purchase_price"] = "-1" }},
		{"protocol-bad-cost", "EXPERIMENTAL_protocol_config", "prepare", func(r map[string]any) {
			r["runtime_config"].(map[string]any)["transaction_costs"].(map[string]any)["action_receipt_creation_config"].(map[string]any)["execution"] = -1
		}},
		{"protocol-huge-cost", "EXPERIMENTAL_protocol_config", "prepare", func(r map[string]any) {
			r["runtime_config"].(map[string]any)["transaction_costs"].(map[string]any)["action_receipt_creation_config"].(map[string]any)["execution"] = json.Number("18446744073709551615")
		}},
		{"gas-rpc", "gas_price", "prepare", nil},
		{"gas-zero", "gas_price", "prepare", func(r map[string]any) { r["gas_price"] = "0" }},
		{"gas-malformed", "gas_price", "prepare", func(r map[string]any) { r["gas_price"] = "bad" }},
		{"nonce-overflow", "key", "prepare", func(r map[string]any) { r["nonce"] = json.Number("18446744073709551615") }},
		{"native-amount-invalid", "owned", "prepare", func(r map[string]any) { r["amount"] = "bad" }},
		{"native-storage-invalid", "owned", "prepare", func(r map[string]any) { r["storage_usage"] = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTokenRPC(t)
			base, _ := f.adapter.WithTokens(nil)
			hit := false
			f.hook = func(method string, params map[string]json.RawMessage, result any, failure *rpcError) (any, *rpcError) {
				scope := method
				if method == "query" {
					scope = stringParam(params, "method_name")
					if stringParam(params, "request_type") == "view_account" {
						scope = "contract"
						if stringParam(params, "account_id") == f.account.Address {
							scope = "owned"
						}
					}
					if stringParam(params, "request_type") == "view_access_key" {
						scope = "key"
					}
				}
				if scope != tc.scope {
					return result, failure
				}
				hit = true
				if tc.change == nil {
					return nil, &rpcError{Code: -1}
				}
				tc.change(result.(map[string]any))
				return result, failure
			}
			var err error
			switch tc.operation {
			case "inspect":
				_, err = base.InspectToken(context.Background(), f.account, "token.testnet")
			case "storage":
				_, err = f.adapter.TokenStorage(context.Background(), f.account, f.policies[0].Asset.ID)
			default:
				_, err = f.adapter.Prepare(context.Background(), f.account, f.transfer())
			}
			if err == nil || !hit || f.sends != 0 {
				t.Fatalf("hostile RPC accepted/not reached/broadcast: %v hit=%v", err, hit)
			}
		})
	}
}
func TestNEP141UnavailableAndRegistrationLifecycleBranches(t *testing.T) {
	f := newTokenRPC(t)
	ctx := context.Background()
	base, _ := f.adapter.WithTokens(nil)
	if _, err := base.InspectToken(ctx, model.Account{}, "token.testnet"); err == nil {
		t.Fatal("unowned inspection")
	}
	if _, err := f.adapter.InspectToken(ctx, f.account, "token.testnet"); err == nil {
		t.Fatal("duplicate inspection")
	}
	if _, err := base.TokenStorage(ctx, f.account, f.policies[0].Asset.ID); err == nil {
		t.Fatal("unapproved storage")
	}
	if _, err := base.PrepareTokenRegistration(ctx, f.account, f.policies[0].Asset.ID); err == nil {
		t.Fatal("unapproved registration")
	}
	if _, err := f.adapter.PrepareTokenRegistration(ctx, f.account, f.policies[0].Asset.ID); err == nil {
		t.Fatal("already registered")
	}
	for _, mode := range []string{"wrong-chain", "changed-code", "large-storage", "restricted"} {
		f.mode = mode
		f.senderRegistered = false
		if _, err := f.adapter.TokenStorage(ctx, f.account, f.policies[0].Asset.ID); mode != "restricted" && err == nil {
			t.Fatal("invalid storage observation", mode)
		}
		if _, err := f.adapter.PrepareTokenRegistration(ctx, f.account, f.policies[0].Asset.ID); err == nil {
			t.Fatal("invalid registration", mode)
		}
	}
	f.mode = ""
	f.senderRegistered = false
	p, err := f.adapter.PrepareTokenRegistration(ctx, f.account, f.policies[0].Asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := f.adapter.Sign(seed(), p)
	if err != nil {
		t.Fatal(err)
	}
	f.plan, f.identity = p, signed.Identity
	receipt, err := f.adapter.Submit(ctx, p, signed)
	if err != nil || receipt.State != "CONFIRMED" {
		t.Fatal("registration receipt", receipt, err)
	}
	f.senderRegistered = true
	if _, err = f.adapter.Submit(ctx, p, signed); err == nil || f.sends != 1 {
		t.Fatal("stale storage repeated", err)
	}
	f.senderRegistered = false
	f.mode = "large-storage"
	if _, err = f.adapter.Submit(ctx, p, signed); err == nil || f.sends != 1 {
		t.Fatal("changed storage repeated", err)
	}
	f.mode = ""
	f.hook = func(method string, params map[string]json.RawMessage, result any, failure *rpcError) (any, *rpcError) {
		if method == "query" && stringParam(params, "account_id") == f.account.Address && stringParam(params, "request_type") == "view_account" {
			result.(map[string]any)["amount"] = "bad"
		}
		return result, failure
	}
	if _, err = f.adapter.Submit(ctx, p, signed); err == nil {
		t.Fatal("bad current native balance")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = base.InspectToken(canceled, f.account, "new.testnet"); err == nil {
		t.Fatal("canceled RPC succeeded")
	}
}
func TestNEP141PureBoundariesAndReceiptUnknowns(t *testing.T) {
	a, p := tokenPlan(t, false)
	for _, invalid := range []model.Plan{{}, fixturePlan(t, New())} {
		if _, ok := TokenAsset(invalid); ok {
			t.Fatal("non-token asset")
		}
		if _, ok := TokenEffects(invalid); ok {
			t.Fatal("non-token effects")
		}
	}
	q := p
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	d.Prepared = math.MaxInt64
	q.Payload, _ = json.Marshal(d)
	if _, ok := TokenAsset(q); ok {
		t.Fatal("overflow historic lifetime")
	}
	q = p
	q.Amount = "bad"
	if _, ok := TokenAsset(q); ok {
		t.Fatal("invalid historic amount")
	}
	if _, err := a.tokenBlock(context.Background(), p.Account.Network, "bad"); err == nil {
		t.Fatal("invalid block reference")
	}
	if _, err := a.tokenView(context.Background(), p.Account.Network, "token.testnet", "ft_transfer", "{}", tokenBlock{}, 1); err == nil {
		t.Fatal("write method reached view transport")
	}
	signed, _ := a.Sign(seed(), p)
	for _, mode := range []string{"invalid-plan", "wrong-receiver", "nonfinal", "bad-result", "bad-storage-return", "huge-log", "many-logs", "unrelated-log", "missing-event"} {
		t.Run(mode, func(t *testing.T) {
			plan := p
			object := tokenRPCExecution(p, signed.Identity)
			raw, _ := json.Marshal(object)
			var r execution
			_ = json.Unmarshal(raw, &r)
			switch mode {
			case "invalid-plan":
				plan.Payload = []byte(`{`)
			case "wrong-receiver":
				r.Transaction.Receiver = "attacker.testnet"
			case "nonfinal":
				r.Final = "EXECUTED"
			case "bad-result":
				r.Status = []byte(`{"SuccessValue":"%%%"}`)
			case "bad-storage-return":
				_, plan = tokenPlan(t, true)
				object = tokenRPCExecution(plan, signed.Identity)
				raw, _ = json.Marshal(object)
				_ = json.Unmarshal(raw, &r)
				r.Status = []byte(`{"SuccessValue":"bnVsbA=="}`)
			case "huge-log":
				r.Receipts[0].Outcome.Logs = []string{strings.Repeat("x", 16385)}
			case "many-logs":
				r.Receipts[0].Outcome.Logs = make([]string, 65)
			case "unrelated-log":
				r.Receipts[0].Outcome.Logs = []string{`EVENT_JSON:{"standard":"nep141","version":"1.0.0","event":"ft_mint","data":[]}`}
			case "missing-event":
				r.Receipts[0].Outcome.Logs = []string{"ordinary bounded log"}
			}
			receipt, _ := tokenReceipt(plan, signed.Identity, r)
			if receipt.State == "CONFIRMED" {
				t.Fatal("nonstandard receipt confirmed")
			}
		})
	}
	if _, _, _, err := storageBalance([]byte(`{"total":"0","available":"-1"}`)); err == nil {
		t.Fatal("invalid storage available")
	}
}
