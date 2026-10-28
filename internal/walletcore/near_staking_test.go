package walletcore

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/near"
	"localwallet/internal/model"
)

// This coordinator fixture uses the real public NEAR adapter and inherited-pipe
// journal scheduling. It deliberately has no signing key or blockchain node;
// signer authorization and actual sandbox execution have separate tests.
func stakingCoordinator(t *testing.T) (*core, string, *atomic.Int32) {
	t.Helper()
	adapter := near.New()
	account, err := adapter.Derive(make([]byte, 64), 0, "near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	block := base58.Encode(make([]byte, 32))
	queries := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("malformed fixture request")
			return
		}
		queries.Add(1)
		var result any
		switch request.Method {
		case "status":
			result = map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}
		case "query":
			var params map[string]string
			_ = json.Unmarshal(request.Params, &params)
			switch params["request_type"] {
			case "view_account":
				result = map[string]any{"amount": "1000000000000000000000000000", "locked": "0", "storage_usage": 100, "block_hash": block, "block_height": 123}
			case "view_access_key":
				result = map[string]any{"nonce": 41, "permission": "FullAccess", "block_hash": block, "block_height": 123}
			case "call_function":
				if params["account_id"] != "test.pool.f863973.m0" || params["method_name"] != "get_account" {
					t.Error("public read escaped the selected pool/get_account")
				}
				raw, err := base64.StdEncoding.DecodeString(params["args_base64"])
				if err != nil || string(raw) != `{"account_id":"`+account.Address+`"}` {
					t.Error("staking query changed the selected wallet address")
				}
				data := `{"account_id":"` + account.Address + `","unstaked_balance":"2000000000000000000000000","staked_balance":"10000000000000000000000000","can_withdraw":true}`
				values := make([]int, len(data))
				for i := range data {
					values[i] = int(data[i])
				}
				result = map[string]any{"result": values, "block_hash": block, "block_height": 123}
			default:
				t.Error("unsupported fixture query", params["request_type"])
			}
		case "EXPERIMENTAL_protocol_config":
			cost := map[string]any{"execution": 1, "send_not_sir": 1, "send_sir": 1}
			result = map[string]any{"chain_id": "testnet", "protocol_version": 88, "runtime_config": map[string]any{
				"storage_amount_per_byte": "10000000000000000000", "min_gas_purchase_price": "1000000000",
				"transaction_costs": map[string]any{"action_receipt_creation_config": cost, "action_creation_config": map[string]any{"function_call_cost": cost, "function_call_cost_per_byte": cost}},
			}}
		case "gas_price":
			result = map[string]any{"gas_price": "1000000000"}
		default:
			t.Error("preparation attempted a non-read request", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "wallet", "result": result})
	}))
	t.Cleanup(server.Close)
	if err = adapter.SetEndpoint("near-testnet", server.URL); err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	return c, path, queries
}

func TestNEARStakingCoordinatorReadAndExactWholeNEARPreparation(t *testing.T) {
	c, path, _ := stakingCoordinator(t)
	observed, err := c.handle(message("nearStakingState", map[string]any{"accountId": "near-testnet:0", "pool": "test.pool.f863973.m0"}))
	if err != nil {
		t.Fatal(err)
	}
	state, ok := observed.(near.StakingState)
	if !ok || state.Staked != "10000000000000000000000000" || state.Unstaked != "2000000000000000000000000" || !state.CanWithdraw || state.Rewards != "" {
		t.Fatal("state/rewards observation was not preserved")
	}
	for _, request := range []struct {
		action, amount, method, effect, deposit string
	}{
		{"stake", "1.000000000000000000000001", "deposit_and_stake", "1000000000000000000000001", "1000000000000000000000001"},
		{"unstake", "1", "unstake", "1000000000000000000000000", "0"},
		{"unstake_all", "", "unstake_all", "10000000000000000000000000", "0"},
		{"withdraw_all", "", "withdraw_all", "2000000000000000000000000", "0"},
	} {
		prepared, err := c.handle(message("prepareNearStaking", map[string]any{"accountId": "near-testnet:0", "pool": "test.pool.f863973.m0", "action": request.action, "amount": request.amount}))
		if err != nil {
			t.Fatal(request.action, err)
		}
		plan := prepared.(model.Plan)
		var payload near.Payload
		if json.Unmarshal(plan.Payload, &payload) != nil || payload.Staking == nil || payload.Staking.Method != request.method || payload.Staking.Deposit != request.deposit || plan.Amount != request.effect || plan.ID == "" {
			t.Fatal("whole NEAR staking intent changed", request.action)
		}
		journal := readPublicState(t, path)["ops"].([]any)
		if len(journal) == 0 || journal[len(journal)-1].(map[string]any)["state"] != "PREPARED" {
			t.Fatal("staking preview did not use existing durable preparation")
		}
	}
}

func TestNEARStakingCoordinatorRejectsPublicSigningAuthority(t *testing.T) {
	c, _, queries := stakingCoordinator(t)
	for _, fields := range []map[string]any{
		{"action": "transfer", "amount": "1"},
		{"action": "stake", "amount": "0"},
		{"action": "stake", "amount": "-1"},
		{"action": "stake", "amount": "1e3"},
		{"action": "stake", "amount": "0.0000000000000000000000001"},
		{"action": "unstake_all", "amount": "1"},
		{"action": "withdraw_all", "amount": "0"},
		{"action": "stake", "amount": "1", "gas": "1"},
		{"action": "stake", "amount": "1", "deposit": "1"},
		{"action": "stake", "amount": "1", "args": "{}"},
		{"action": "stake", "amount": "1", "method": "add_key"},
		{"action": "stake", "amount": "1", "password": "PUBLIC-SYNTHETIC-forbidden"},
		{"action": nil, "amount": "1"},
	} {
		fields["accountId"] = "near-testnet:0"
		fields["pool"] = "test.pool.f863973.m0"
		if _, err := c.handle(message("prepareNearStaking", fields)); err == nil {
			t.Fatal("public request gained unsupported signing authority", fields)
		}
	}
	if queries.Load() != 0 {
		t.Fatal("malformed public intent reached the network")
	}
	for _, fields := range []map[string]any{
		{"accountId": "near-testnet:0", "pool": "test.pool.f863973.m0", "method": "get_private_key"},
		{"accountId": "near-testnet:0", "pool": nil},
		{"accountId": "near-testnet:0", "pool": ""},
		{"accountId": "unknown-wallet", "pool": "test.pool.f863973.m0"},
		{"accountId": "near-testnet:0", "pool": "attacker.near"},
		{"accountId": "near-testnet:0", "pool": strings.Repeat("x", 1024)},
	} {
		if _, err := c.handle(message("nearStakingState", fields)); err == nil {
			t.Fatal("unsupported staking read accepted")
		}
	}
	if queries.Load() != 0 {
		t.Fatal("invalid pool read reached the network")
	}
}
