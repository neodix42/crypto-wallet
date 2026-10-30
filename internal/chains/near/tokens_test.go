package near

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/model"
)

func tokenPolicy(contract string, decimals uint8) model.NEP141Token {
	return model.NEP141Token{Network: "near-testnet", Asset: model.Asset{ID: "near-testnet:nep141:" + contract, Contract: contract, Symbol: "TEST", Decimals: decimals}, CodeHash: base58.Encode(bytes.Repeat([]byte{7}, 32))}
}
func TestNEP141PoliciesAreExactBoundedAndImmutable(t *testing.T) {
	policy := tokenPolicy("token.testnet", 6)
	if ValidateTokenPolicy(policy) != nil {
		t.Fatal("valid policy")
	}
	for _, network := range []string{"near-mainnet", "near-testnet", "near-localnet"} {
		if !SupportsTokenImport(network) {
			t.Fatal(network)
		}
	}
	if SupportsTokenImport("ethereum-mainnet") {
		t.Fatal("foreign ledger")
	}
	for _, change := range []func(*model.NEP141Token){func(t *model.NEP141Token) { t.Network = "foreign" }, func(t *model.NEP141Token) { t.Asset.Contract = "UPPER.near" }, func(t *model.NEP141Token) { t.Asset.ID = "near-testnet:native" }, func(t *model.NEP141Token) { t.Asset.Symbol = "NEAR\nSEND" }, func(t *model.NEP141Token) { t.Asset.Symbol = strings.Repeat("A", 25) }, func(t *model.NEP141Token) { t.Asset.Decimals = 78 }, func(t *model.NEP141Token) { t.CodeHash = base58.Encode(make([]byte, 32)) }, func(t *model.NEP141Token) { t.CodeHash = "bad" }} {
		bad := policy
		change(&bad)
		if ValidateTokenPolicy(bad) == nil {
			t.Fatal("bad policy accepted")
		}
	}
	a := New()
	tokens := []model.NEP141Token{policy}
	tracked, err := a.WithTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	tokens[0].Asset.Symbol = "CHANGED"
	a.endpoints["near-testnet"] = "http://127.0.0.1:9"
	if tracked.Networks()[1].Assets[1] != policy.Asset || tracked.endpoints["near-testnet"] == a.endpoints["near-testnet"] || len(a.Networks()[1].Assets) != 1 {
		t.Fatal("policy or endpoint alias")
	}
	if _, err = a.WithTokens([]model.NEP141Token{policy, policy}); err == nil {
		t.Fatal("duplicate policy")
	}
	if _, err = a.WithTokens(make([]model.NEP141Token, MaxImportedTokens+1)); err == nil {
		t.Fatal("unbounded policy")
	}
}

type tokenRPCFixture struct {
	hook                                  func(string, map[string]json.RawMessage, any, *rpcError) (any, *rpcError)
	adapter                               *Adapter
	account                               model.Account
	policies                              []model.NEP141Token
	mode                                  string
	senderRegistered, recipientRegistered bool
	sends                                 int
	identity                              string
	plan                                  model.Plan
}

func newTokenRPC(t *testing.T) *tokenRPCFixture {
	t.Helper()
	f := &tokenRPCFixture{policies: []model.NEP141Token{tokenPolicy("token.testnet", 6), tokenPolicy("second.testnet", 18)}, senderRegistered: true, recipientRegistered: true}
	f.account, _ = New().Derive(seed(), 0, "near-testnet")
	hash := base58.Encode(bytes.Repeat([]byte{3}, 32))
	base, server := nearServer(t, func(method string, params map[string]json.RawMessage) (result any, failure *rpcError) {
		defer func() {
			if f.hook != nil {
				result, failure = f.hook(method, params, result, failure)
			}
		}()
		switch method {
		case "status":
			network := "testnet"
			if f.mode == "wrong-chain" {
				network = "mainnet"
			}
			return map[string]any{"chain_id": network, "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}}, nil
		case "block":
			return map[string]any{"header": map[string]any{"hash": hash, "height": 123, "timestamp_nanosec": time.Now().UnixNano()}}, nil
		case "EXPERIMENTAL_protocol_config":
			return stakingProtocolFixture(), nil
		case "gas_price":
			price := "1000000000"
			if f.mode == "gas-spike" {
				price = "3000000000"
			}
			return map[string]string{"gas_price": price}, nil
		case "query":
			var kind, account, view string
			_ = json.Unmarshal(params["request_type"], &kind)
			_ = json.Unmarshal(params["account_id"], &account)
			_ = json.Unmarshal(params["method_name"], &view)
			if kind == "view_access_key" {
				permission := "FullAccess"
				if f.mode == "restricted" {
					permission = "FunctionCall"
				}
				nonce := 41
				if f.mode == "nonce-changed" {
					nonce = 42
				}
				return map[string]any{"permission": permission, "nonce": nonce, "block_hash": hash, "block_height": 123}, nil
			}
			if kind == "view_account" {
				code := f.policies[0].CodeHash
				if f.mode == "changed-code" {
					code = base58.Encode(bytes.Repeat([]byte{8}, 32))
				}
				if f.mode == "no-code" {
					code = base58.Encode(make([]byte, 32))
				}
				balance := "1000000000000000000000000000"
				if f.mode == "low-native" {
					balance = "1"
				}
				return map[string]any{"amount": balance, "locked": "0", "storage_usage": 182, "code_hash": code, "block_height": 123, "block_hash": hash}, nil
			}
			var raw string
			switch view {
			case "ft_metadata":
				if f.mode == "one-token-unavailable" && account == "token.testnet" {
					return nil, &rpcError{Code: -1}
				}
				decimals := uint8(6)
				if account == "second.testnet" {
					decimals = 18
				}
				if f.mode == "changed-decimals" {
					decimals++
				}
				raw = `{"spec":"ft-1.0.0","name":"Test Token","symbol":"TEST","decimals":` + new(big.Int).SetUint64(uint64(decimals)).String() + `,"icon":null,"reference":null,"reference_hash":null}`
				switch f.mode {
				case "missing-decimals":
					raw = strings.Replace(raw, `,"decimals":6`, "", 1)
				case "null-decimals":
					raw = strings.Replace(raw, `"decimals":6`, `"decimals":null`, 1)
				case "duplicate":
					raw = strings.Replace(raw, `"symbol":"TEST"`, `"symbol":"TEST","symbol":"EVIL"`, 1)
				case "unknown-metadata":
					raw = strings.Replace(raw, `"spec":`, `"extra":true,"spec":`, 1)
				case "bad-spec":
					raw = strings.Replace(raw, "ft-1.0.0", "ft-2.0.0", 1)
				case "hostile-symbol":
					raw = strings.Replace(raw, "TEST", `TE\nST`, 1)
				case "overprecision":
					raw = strings.Replace(raw, `"decimals":6`, `"decimals":78`, 1)
				}
			case "ft_balance_of":
				raw = `"900000000000000000000000"`
				if f.mode == "bad-balance" {
					raw = `"01"`
				}
			case "storage_balance_bounds":
				raw = `{"min":"1250000000000000000000","max":"1250000000000000000000"}`
				if f.mode == "large-storage" {
					raw = `{"min":"1000000000000000000000001","max":null}`
				}
				if f.mode == "zero-storage" {
					raw = `{"min":"0","max":null}`
				}
			case "storage_balance_of":
				var encoded string
				_ = json.Unmarshal(params["args_base64"], &encoded)
				args, _ := base64.StdEncoding.DecodeString(encoded)
				registered := f.recipientRegistered
				if strings.Contains(string(args), f.account.Address) {
					registered = f.senderRegistered
				}
				raw = `null`
				if registered {
					raw = `{"total":"1250000000000000000000","available":"0"}`
				}
				if f.mode == "bad-storage" {
					raw = `{"total":"1","available":"2"}`
				}
			default:
				t.Errorf("unexpected token query %s", view)
				return nil, &rpcError{Code: -1}
			}
			values := make([]int, len(raw))
			for i, b := range []byte(raw) {
				values[i] = int(b)
			}
			result := any(values)
			if f.mode == "wrong-result-encoding" {
				result = base64.StdEncoding.EncodeToString([]byte(raw))
			}
			resultHash := hash
			if f.mode == "wrong-token-block" {
				resultHash = base58.Encode(bytes.Repeat([]byte{4}, 32))
			}
			return map[string]any{"result": result, "block_hash": resultHash, "block_height": 123}, nil
		case "send_tx":
			f.sends++
			if f.mode == "lost-ack" {
				return nil, &rpcError{Code: -1}
			}
			return tokenRPCExecution(f.plan, f.identity), nil
		case "tx":
			return tokenRPCExecution(f.plan, f.identity), nil
		}
		t.Errorf("unexpected RPC %s", method)
		return nil, &rpcError{Code: -1}
	})
	t.Cleanup(server.Close)
	f.adapter, _ = base.WithTokens(f.policies)
	return f
}
func tokenRPCExecution(p model.Plan, identity string) map[string]any {
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	block := base58.Encode(bytes.Repeat([]byte{9}, 32))
	child := base58.Encode(bytes.Repeat([]byte{10}, 32))
	raw := ""
	logs := []string{}
	if d.Token.Method == "ft_transfer" {
		logs = []string{`EVENT_JSON:{"standard":"nep141","version":"1.0.0","event":"ft_transfer","data":[{"old_owner_id":"` + p.Account.Address + `","new_owner_id":"` + p.Recipient + `","amount":"` + p.Amount + `"}]}`}
	} else {
		raw = base64.StdEncoding.EncodeToString([]byte(`{"total":"` + d.Token.Deposit + `","available":"0"}`))
	}
	return map[string]any{"status": map[string]string{"SuccessValue": raw}, "final_execution_status": "FINAL", "transaction": map[string]string{"hash": identity, "signer_id": p.Account.Address, "receiver_id": d.Token.Policy.Asset.Contract}, "transaction_outcome": map[string]any{"block_hash": block, "outcome": map[string]any{"receipt_ids": []string{child}}}, "receipts_outcome": []any{map[string]any{"id": child, "block_hash": block, "outcome": map[string]any{"executor_id": d.Token.Policy.Asset.Contract, "status": map[string]string{"SuccessValue": raw}, "receipt_ids": []string{}, "logs": logs}}}}
}
func (f *tokenRPCFixture) transfer() model.Transfer {
	return model.Transfer{AccountID: f.account.ID, Asset: f.policies[0].Asset.ID, Recipient: "receiver.testnet", Amount: "1234567"}
}
func TestNEP141MetadataAndIndependentUnknownBalances(t *testing.T) {
	f := newTokenRPC(t)
	base, _ := f.adapter.WithTokens(nil)
	got, err := base.InspectToken(context.Background(), f.account, "token.testnet")
	if err != nil || got != f.policies[0] {
		t.Fatalf("inspect %+v %v", got, err)
	}
	for _, mode := range []string{"wrong-chain", "no-code", "missing-decimals", "null-decimals", "duplicate", "unknown-metadata", "bad-spec", "hostile-symbol", "overprecision", "wrong-result-encoding", "wrong-token-block"} {
		f.mode = mode
		if _, err = base.InspectToken(context.Background(), f.account, "token.testnet"); err == nil {
			t.Fatal("hostile metadata accepted", mode)
		}
	}
	f.mode = "one-token-unavailable"
	balances, err := f.adapter.Balance(context.Background(), f.account)
	if err != nil || len(balances) != 3 || balances[0].State != "FINAL" || balances[1].Atomic != "" || !strings.HasPrefix(balances[1].State, "UNKNOWN") || balances[2].State != "FINAL" {
		t.Fatalf("isolated failures %+v %v", balances, err)
	}
	f.mode = "changed-code"
	balances, err = f.adapter.Balance(context.Background(), f.account)
	if err != nil || balances[0].State != "FINAL" || balances[1].Atomic != "" {
		t.Fatal("changed code converted to balance", balances, err)
	}
}
func TestNEP141TransferAndSelfRegistrationUseSeparateExactPlans(t *testing.T) {
	f := newTokenRPC(t)
	ctx := context.Background()
	p, err := f.adapter.Prepare(ctx, f.account, f.transfer())
	if err != nil {
		t.Fatal(err)
	}
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	if p.Asset != f.policies[0].Asset.ID || p.FeeAsset != "near-testnet:native" || p.Recipient != "receiver.testnet" || d.Token.Deposit != "1" || d.Token.Method != "ft_transfer" || d.Token.Gas != TokenGas || IsTokenRegistrationPlan(p) {
		t.Fatal("incorrect token plan", p)
	}
	signed, err := f.adapter.Sign(seed(), p)
	if err != nil {
		t.Fatal(err)
	}
	f.plan, f.identity = p, signed.Identity
	receipt, err := f.adapter.Submit(ctx, p, signed)
	if err != nil || receipt.State != "CONFIRMED" || f.sends != 1 {
		t.Fatalf("submit %+v %v", receipt, err)
	}
	if receipt, err = f.adapter.Reconcile(ctx, p, signed); err != nil || receipt.State != "CONFIRMED" {
		t.Fatal("reconcile", receipt, err)
	}
	f.senderRegistered = false
	state, err := f.adapter.TokenStorage(ctx, f.account, p.Asset)
	if err != nil || state.Registered || state.MinDeposit != "1250000000000000000000" || state.AccountID != f.account.Address {
		t.Fatal("storage", state, err)
	}
	register, err := f.adapter.PrepareTokenRegistration(ctx, f.account, p.Asset)
	if err != nil {
		t.Fatal(err)
	}
	if register.Asset != register.FeeAsset || register.Amount != state.MinDeposit || register.Recipient != f.account.Address || !IsTokenRegistrationPlan(register) {
		t.Fatal("incorrect storage plan")
	}
	text, ok := TokenEffects(register)
	if !ok || !strings.Contains(text, "storage_deposit") || !strings.Contains(text, f.policies[0].CodeHash) || !strings.Contains(text, "registration_only") {
		t.Fatal("approval lacks exact effect", text)
	}
	register.Expires -= 120
	_ = json.Unmarshal(register.Payload, &d)
	d.Prepared -= 120
	register.Payload, _ = json.Marshal(d)
	if _, ok = TokenEffects(register); !ok {
		t.Fatal("expired journal lost registration description")
	}
	if _, ok = TokenAsset(register); !ok {
		t.Fatal("expired journal lost token identity")
	}
	if _, err = f.adapter.Sign(seed(), register); err == nil {
		t.Fatal("historical rendering authorized expired signing")
	}
}
func TestNEP141TransferAndRegistrationPreflightFailuresNeverBroadcast(t *testing.T) {
	for _, mode := range []string{"restricted", "changed-code", "changed-decimals", "bad-balance", "bad-storage", "large-storage", "zero-storage", "low-native", "wrong-token-block", "wrong-result-encoding"} {
		t.Run(mode, func(t *testing.T) {
			f := newTokenRPC(t)
			f.mode = mode
			if _, err := f.adapter.Prepare(context.Background(), f.account, f.transfer()); err == nil {
				t.Fatal("hostile preparation accepted")
			}
			if f.sends != 0 {
				t.Fatal("prepare broadcast")
			}
		})
	}
	f := newTokenRPC(t)
	for _, sender := range []bool{true, false} {
		f.senderRegistered = sender
		f.recipientRegistered = !sender
		if _, err := f.adapter.Prepare(context.Background(), f.account, f.transfer()); err == nil || !strings.Contains(err.Error(), "not registered") {
			t.Fatal("missing registration accepted", err)
		}
	}
	f.senderRegistered, f.recipientRegistered = true, true
	for _, recipient := range []string{f.account.Address, f.policies[0].Asset.Contract} {
		transfer := f.transfer()
		transfer.Recipient = recipient
		if _, err := f.adapter.Prepare(context.Background(), f.account, transfer); err == nil {
			t.Fatal("contract/self confusion")
		}
	}
	p, err := f.adapter.Prepare(context.Background(), f.account, f.transfer())
	if err != nil {
		t.Fatal(err)
	}
	signed, _ := f.adapter.Sign(seed(), p)
	for _, mode := range []string{"nonce-changed", "gas-spike", "changed-code", "changed-decimals", "low-native", "bad-storage"} {
		f.mode = mode
		if _, err = f.adapter.Submit(context.Background(), p, signed); err == nil || f.sends != 0 {
			t.Fatal("changed current state broadcast", mode, err)
		}
	}
}
