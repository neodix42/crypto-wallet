package near

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"localwallet/internal/model"
)

func tokenPlan(t testing.TB, registration bool) (*Adapter, model.Plan) {
	t.Helper()
	token := tokenPolicy("token.testnet", 6)
	a, err := New().WithTokens([]model.NEP141Token{token})
	if err != nil {
		t.Fatal(err)
	}
	p := fixturePlan(t, a)
	p.Asset = token.Asset.ID
	p.Amount = "1234567"
	p.MaxFee = "60000000000000000000000"
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	d.Gas = TokenGas
	d.Token = &TokenCall{Policy: token, Method: "ft_transfer", Args: `{"receiver_id":"` + p.Recipient + `","amount":"` + p.Amount + `"}`, Gas: TokenGas, Deposit: "1"}
	if registration {
		p.Asset = asset(p.Account.Network)
		p.Recipient = p.Account.Address
		p.Amount = "1250000000000000000000"
		d.Token.Method = "storage_deposit"
		d.Token.Deposit = p.Amount
		d.Token.Args = `{"account_id":"` + p.Account.Address + `","registration_only":true}`
	}
	p.Payload, _ = json.Marshal(d)
	return a, p
}
func TestNEP141IndependentSDKBytesAndSignatures(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/oracles/near/token-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Index                                                                                                                      uint32
			Decimals                                                                                                                   uint8
			Address, PublicKey, Contract, Recipient, Method, Args, Amount, Deposit, Gas, Nonce, BlockHash, CodeHash, Raw, Hash, Signed string
		}
	}
	if json.Unmarshal(raw, &fixture) != nil || len(fixture.Vectors) != 18 {
		t.Fatal("missing SDK vectors")
	}
	for _, v := range fixture.Vectors {
		policy := tokenPolicy(v.Contract, v.Decimals)
		policy.CodeHash = v.CodeHash
		a, _ := New().WithTokens([]model.NEP141Token{policy})
		_, p := tokenPlan(t, v.Method == "storage_deposit")
		p.Account, err = a.Derive(seed(), v.Index, "near-testnet")
		if err != nil || p.Account.Address != v.Address || p.Account.PublicKey != v.PublicKey {
			t.Fatal("independent key mismatch")
		}
		p.Recipient, p.Amount, p.Nonce = v.Recipient, v.Amount, v.Nonce
		p.Asset = policy.Asset.ID
		if v.Method == "storage_deposit" {
			p.Asset = asset(p.Account.Network)
		}
		var d Payload
		_ = json.Unmarshal(p.Payload, &d)
		d.BlockHash = v.BlockHash
		d.Token = &TokenCall{Policy: policy, Method: v.Method, Args: v.Args, Deposit: v.Deposit, Gas: v.Gas}
		p.Payload, _ = json.Marshal(d)
		encoded, err := transaction(p)
		if err != nil || hex.EncodeToString(encoded) != v.Raw {
			t.Fatal("official Borsh mismatch", v.Index, v.Method, err)
		}
		signed, err := a.Sign(seed(), p)
		if err != nil || signed.Identity != v.Hash || hex.EncodeToString(signed.Bytes) != v.Signed || a.verify(p, signed) != nil {
			t.Fatal("official signature mismatch", v.Index, v.Method, err)
		}
	}
}
func TestNEP141SignerRefusesEveryUnapprovedMethodOrSignedField(t *testing.T) {
	for _, registration := range []bool{false, true} {
		a, p := tokenPlan(t, registration)
		if a.ValidatePlan(p) != nil {
			t.Fatal("fixture plan")
		}
		for _, name := range []string{"method", "callback", "allowance", "args", "deposit", "gas", "both-actions", "foreign-contract", "foreign-hash", "foreign-precision", "native-fee", "native-confusion", "negative", "leading-zero", "overflow", "memo", "nonce", "expiry", "unknown", "duplicate", "contract-recipient"} {
			t.Run(name, func(t *testing.T) {
				q := p
				var d Payload
				_ = json.Unmarshal(p.Payload, &d)
				switch name {
				case "method":
					d.Token.Method = "delete_account"
				case "callback":
					d.Token.Method = "ft_transfer_call"
				case "allowance":
					d.Token.Method = "increase_allowance"
				case "args":
					d.Token.Args = `{"account_id":"attacker.testnet","registration_only":false}`
				case "deposit":
					d.Token.Deposit = "2"
				case "gas":
					d.Token.Gas = "300000000000000"
				case "both-actions":
					d.Staking = &StakingCall{Method: "withdraw_all", Args: "{}", Deposit: "0", Gas: StakingGas}
				case "foreign-contract":
					d.Token.Policy.Asset.Contract = "attacker.testnet"
				case "foreign-hash":
					d.Token.Policy.CodeHash = tokenPolicy("x.testnet", 6).CodeHash + "1"
				case "foreign-precision":
					d.Token.Policy.Asset.Decimals++
				case "native-fee":
					q.FeeAsset = d.Token.Policy.Asset.ID
				case "native-confusion":
					if registration {
						q.Asset = d.Token.Policy.Asset.ID
					} else {
						q.Asset = asset(q.Account.Network)
					}
				case "negative":
					q.Amount = "-1"
				case "leading-zero":
					q.Amount = "01"
				case "overflow":
					q.Amount = "340282366920938463463374607431768211456"
				case "memo":
					q.Memo = "unexpected"
				case "nonce":
					q.Nonce = "0"
				case "expiry":
					q.Expires--
				case "contract-recipient":
					q.Recipient = d.Token.Policy.Asset.Contract
				}
				q.Payload, _ = json.Marshal(d)
				if name == "unknown" {
					q.Payload = bytes.Replace(q.Payload, []byte(`"method":`), []byte(`"arbitrary":true,"method":`), 1)
				}
				if name == "duplicate" {
					q.Payload = bytes.Replace(q.Payload, []byte(`"gas":"30000000000000"`), []byte(`"gas":"30000000000000","gas":"1"`), 1)
				}
				if _, err := a.Sign(seed(), q); err == nil {
					t.Fatal("unapproved plan signed")
				}
			})
		}
		if _, err := New().Sign(seed(), p); err == nil {
			t.Fatal("unimported policy authorized token")
		}
	}
}
func TestNEP141ReceiptsNeedExactEventAndCompleteSuccessfulChildren(t *testing.T) {
	a, p := tokenPlan(t, false)
	signed, _ := a.Sign(seed(), p)
	for _, mode := range []string{"valid", "missing", "wrong-sender", "wrong-recipient", "wrong-amount", "wrong-contract", "duplicate", "unknown-field", "false-result", "failed-child", "missing-child"} {
		t.Run(mode, func(t *testing.T) {
			object := tokenRPCExecution(p, signed.Identity)
			raw, _ := json.Marshal(object)
			var r execution
			_ = json.Unmarshal(raw, &r)
			switch mode {
			case "missing":
				r.Receipts[0].Outcome.Logs = nil
			case "wrong-sender":
				r.Receipts[0].Outcome.Logs[0] = strings.Replace(r.Receipts[0].Outcome.Logs[0], p.Account.Address, "attacker.testnet", 1)
			case "wrong-recipient":
				r.Receipts[0].Outcome.Logs[0] = strings.Replace(r.Receipts[0].Outcome.Logs[0], p.Recipient, "attacker.testnet", 1)
			case "wrong-amount":
				r.Receipts[0].Outcome.Logs[0] = strings.Replace(r.Receipts[0].Outcome.Logs[0], p.Amount, "1", 1)
			case "wrong-contract":
				r.Receipts[0].Outcome.Executor = "attacker.testnet"
			case "duplicate":
				r.Receipts[0].Outcome.Logs = append(r.Receipts[0].Outcome.Logs, r.Receipts[0].Outcome.Logs[0])
			case "unknown-field":
				r.Receipts[0].Outcome.Logs[0] = strings.Replace(r.Receipts[0].Outcome.Logs[0], `"standard":`, `"extra":true,"standard":`, 1)
			case "false-result":
				r.Status = []byte(`{"SuccessValue":"ZmFsc2U="}`)
			case "failed-child":
				r.Receipts[0].Outcome.Status = []byte(`{"Failure":{"error":"synthetic"}}`)
			case "missing-child":
				r.Receipts = nil
			}
			receipt, err := tokenReceipt(p, signed.Identity, r)
			if mode == "valid" {
				if err != nil || receipt.State != "CONFIRMED" {
					t.Fatal(receipt, err)
				}
			} else if receipt.State == "CONFIRMED" {
				t.Fatal("unverified transfer confirmed", mode)
			}
		})
	}
}
func FuzzNEP141Policy(f *testing.F) {
	f.Add("token.testnet", "TEST", uint8(6), "1")
	f.Fuzz(func(t *testing.T, contract, symbol string, decimals uint8, quantity string) {
		if len(contract) > 128 || len(symbol) > 256 || len(quantity) > 160 {
			return
		}
		policy := tokenPolicy(contract, decimals)
		policy.Asset.Symbol = symbol
		_ = ValidateTokenPolicy(policy)
		a, p := tokenPlan(t, false)
		var d Payload
		_ = json.Unmarshal(p.Payload, &d)
		d.Token.Policy = policy
		p.Amount = quantity
		p.Payload, _ = json.Marshal(d)
		if a.ValidatePlan(p) == nil && (!tokenSymbol(symbol) || decimals > 77) {
			t.Fatal("unsupported token authorized")
		}
	})
}

func FuzzTokenPlan(f *testing.F) {
	a, p := tokenPlan(f, false)
	f.Add([]byte(p.Payload), p.Amount)
	f.Add([]byte(`{"token":{"method":"ft_transfer_call"}}`), "01")
	f.Fuzz(func(t *testing.T, raw []byte, quantity string) {
		if len(raw) > 65536 || len(quantity) > 160 {
			return
		}
		q := p
		q.Payload = raw
		q.Amount = quantity
		if a.ValidatePlan(q) == nil {
			signed, err := a.Sign(seed(), q)
			if err != nil || a.verify(q, signed) != nil {
				t.Fatal("validated token intent changed during signing")
			}
		}
	})
}
