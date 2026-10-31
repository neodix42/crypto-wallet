package walletcore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

// Simulate the typed native DTO boundary, not a permissive model decoder. The
// public transport deliberately exposes payload as an opaque JSON string.
func decodePublicWirePlan(t *testing.T, raw json.RawMessage) model.Plan {
	t.Helper()
	var plan model.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := json.Unmarshal(plan.Payload, &payload); err != nil {
		t.Fatal("public wire lost its opaque payload-string contract", err)
	}
	plan.Payload = json.RawMessage(payload)
	return plan
}

func TestPublicJSONRealPlansRetainCanonicalIdentityAcrossSerializedEnvelopes(t *testing.T) {
	tonAdapter := ton.New()
	tonAccount, err := tonAdapter.Derive(make([]byte, 64), 0, "ton-testnet")
	if err != nil {
		t.Fatal(err)
	}
	deployment := fundedPointTwoTONDeployment(tonAccount)
	coordinator, _, account, _ := tonDeploymentCoordinator(t)
	recipient, err := tonAdapter.Derive(make([]byte, 64), 1, account.Network)
	if err != nil {
		t.Fatal(err)
	}
	value, err := coordinator.handle(message("quotePayment", quoteRequest(model.Transfer{AccountID: account.ID, Asset: account.Network + ":TON", Recipient: recipient.Address}, true)))
	if err != nil {
		t.Fatal(err)
	}
	transfer := value.(model.Plan)
	nearCoordinator, _, _ := stakingCoordinator(t)
	value, err = nearCoordinator.handle(message("prepareNearStaking", map[string]string{"accountId": "near-testnet:0", "pool": "test.pool.f863973.m0", "action": "stake", "amount": "1.000000000000000000000001"}))
	if err != nil {
		t.Fatal(err)
	}
	staking := value.(model.Plan)
	evmAdapter := evm.New()
	evmAccount, err := evmAdapter.Derive(make([]byte, 64), 0, "ethereum-sepolia")
	if err != nil {
		t.Fatal(err)
	}
	evmPlan := model.Plan{Account: evmAccount, Asset: "ethereum-sepolia:native", Recipient: "0x1111111111111111111111111111111111111111", Amount: "123456789", MaxFee: "50400000025200", FeeAsset: "ethereum-sepolia:native", Nonce: "18446744073709551615", Expires: time.Now().Add(time.Minute).Unix()}
	evmPlan.Payload = json.RawMessage(fmt.Sprintf(`{"chainId":"11155111","to":%q,"value":"123456789","data":"0x","gas":"25200","feeCap":"2000000001","tipCap":"1","dataFeeReserve":"0","operatorFeeReserve":"0","stateBlock":"0x10","stateHash":"0x%s","stateTime":"0x%x"}`, evmPlan.Recipient, strings.Repeat("a", 64), time.Now().Unix()))
	evmPlan.ID = "evm:" + tonDeploymentDigest(evmPlan) // EVM prefixes the canonical ID-empty Plan JSON digest.
	for _, test := range []struct {
		name     string
		plan     model.Plan
		validate func(model.Plan) error
	}{
		{"TON-deployment", deployment, tonAdapter.ValidatePlan},
		{"TON-transfer", transfer, tonAdapter.ValidatePlan},
		{"NEAR-staking", staking, near.New().ValidatePlan},
		{"Ethereum-transfer", evmPlan, evmAdapter.ValidatePlan},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate(test.plan); err != nil {
				t.Fatal("fixture did not pass the actual adapter's policy", err)
			}
			originalJSON, err := json.Marshal(test.plan)
			if err != nil {
				t.Fatal(err)
			}
			var originalParameters bytes.Buffer
			if len(test.plan.Account.Parameters) != 0 && json.Compact(&originalParameters, test.plan.Account.Parameters) != nil {
				t.Fatal("invalid original account parameters")
			}
			operation := journal.Operation{Plan: test.plan, Digest: journal.Digest(test.plan), State: "PREPARED", CreatedAt: time.Now().Unix()}
			for _, shape := range []string{"plan", "journal-page", "pending-payment", "nested-page"} {
				t.Run(shape, func(t *testing.T) {
					var envelope any = test.plan
					switch shape {
					case "journal-page":
						envelope = journalPage{Operations: []journal.Operation{operation}, Complete: true, Revision: "fixture-revision", Total: 1}
					case "pending-payment":
						envelope = struct {
							Operation *journal.Operation `json:"operation"`
						}{&operation}
					case "nested-page":
						envelope = struct {
							Pages []journalPage `json:"pages"`
						}{[]journalPage{{Operations: []journal.Operation{operation}}}}
					}
					wire, err := publicJSON(envelope)
					if err != nil || !json.Valid(wire) {
						t.Fatal("invalid public envelope", err)
					}
					var rawPlan json.RawMessage = wire
					switch shape {
					case "journal-page", "nested-page":
						var page struct {
							Operations []struct {
								Plan json.RawMessage `json:"plan"`
							} `json:"operations"`
						}
						if shape == "nested-page" {
							var nested struct {
								Pages []json.RawMessage `json:"pages"`
							}
							if json.Unmarshal(wire, &nested) != nil || len(nested.Pages) != 1 {
								t.Fatal("nested public page lost")
							}
							wire = nested.Pages[0]
						}
						if json.Unmarshal(wire, &page) != nil || len(page.Operations) != 1 {
							t.Fatal("public operation lost")
						}
						rawPlan = page.Operations[0].Plan
					case "pending-payment":
						var pending struct {
							Operation struct {
								Plan json.RawMessage `json:"plan"`
							} `json:"operation"`
						}
						if json.Unmarshal(wire, &pending) != nil {
							t.Fatal("pending public operation lost")
						}
						rawPlan = pending.Operation.Plan
					}
					decoded := decodePublicWirePlan(t, rawPlan)
					decodedJSON, err := json.Marshal(decoded)
					if err != nil || !bytes.Equal(decodedJSON, originalJSON) || !bytes.Equal(decoded.Payload, test.plan.Payload) || !bytes.Equal(decoded.Account.Parameters, originalParameters.Bytes()) || decoded.ID != test.plan.ID || journal.Digest(decoded) != operation.Digest {
						t.Fatalf("public serialization changed digest-bound transaction bytes or account parameters: marshal=%v plan=%v payload=%v parameters=%v id=%v digest=%v", err, bytes.Equal(decodedJSON, originalJSON), bytes.Equal(decoded.Payload, test.plan.Payload), bytes.Equal(decoded.Account.Parameters, originalParameters.Bytes()), decoded.ID == test.plan.ID, journal.Digest(decoded) == operation.Digest)
					}
					if err := test.validate(decoded); err != nil {
						t.Fatal("wire roundtrip no longer passes actual chain validation", err)
					}
				})
			}
		})
	}
}

func TestPublicJSONPreservesOpaquePayloadAndParametersWhileRedactingOnlyPublicRPC(t *testing.T) {
	value := struct {
		Parameters json.RawMessage `json:"parameters"`
		Payload    json.RawMessage `json:"payload"`
		Count      uint64          `json:"count"`
		Nested     []any           `json:"nested"`
	}{json.RawMessage(`{"z":18446744073709551615,"a":[{"y":2,"b":1}]}`), json.RawMessage(`{"z":18446744073709551615,"a":0,"rpc":"opaque-contract-data"}`), ^uint64(0), []any{map[string]any{"rpc": "https://fixture-user:fixture-pass@example.invalid/key?secret=fixture#fragment"}, map[string]any{"rpc": "file:///fixture"}}}
	wire, err := publicJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Parameters json.RawMessage `json:"parameters"`
		Payload    string          `json:"payload"`
		Count      uint64          `json:"count"`
		Nested     []struct {
			RPC string `json:"rpc"`
		} `json:"nested"`
	}
	if json.Unmarshal(wire, &decoded) != nil || !bytes.Equal(decoded.Parameters, value.Parameters) || decoded.Payload != string(value.Payload) || decoded.Count != value.Count || len(decoded.Nested) != 2 || decoded.Nested[0].RPC != "https://example.invalid" || decoded.Nested[1].RPC != "" {
		t.Fatal("opaque bytes, uint64 digits, field order or origin redaction changed")
	}
	if strings.Contains(string(wire), "fixture-pass") || strings.Contains(string(wire), "fixture-user") || strings.Contains(string(wire), "secret=") {
		t.Fatal("endpoint credentials escaped through nested public output")
	}
	if _, err := publicJSON(json.RawMessage(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))); err == nil {
		t.Fatal("unbounded public response nesting accepted")
	}
}
