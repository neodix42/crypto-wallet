package walletcore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

func quoteRequest(t model.Transfer, maximum bool) paymentQuoteRequest {
	return paymentQuoteRequest{AccountID: strictjson.Text(t.AccountID), Asset: strictjson.Text(t.Asset), Recipient: strictjson.Text(t.Recipient), Amount: strictjson.Text(t.Amount), Memo: strictjson.Text(t.Memo), Maximum: &maximum}
}

func TestPaymentQuoteAndLegacyMaxNeverReserveAnAccount(t *testing.T) {
	adapter, account := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
	c, path := fakeCoordinator(t, adapter.publicAdapter, nil)
	c.adapters = []model.Adapter{adapter}
	draft := model.Transfer{AccountID: account.ID, Asset: "unit-testnet:native", Recipient: "public-recipient", Amount: "100", Memo: "public draft"}
	var quote model.Plan
	for _, maximum := range []bool{true, true, false, true} {
		value, err := c.handle(message("quotePayment", quoteRequest(draft, maximum)))
		if err != nil {
			t.Fatal("repeat read-only quote failed", err)
		}
		quote = value.(model.Plan)
		want := "100"
		if maximum {
			want = "990"
		}
		if quote.Amount != want || quote.MaxFee != "10" || quote.ID == "" {
			t.Fatal("wrong exact quote", quote)
		}
	}
	for range 2 {
		if _, err := c.handle(message("max", draft)); err != nil {
			t.Fatal("legacy Max still reserves an account", err)
		}
	}
	before := readPublicState(t, path)
	if len(before["ops"].([]any)) != 0 {
		t.Fatal("read-only quote created a durable PREPARED operation")
	}
	for _, call := range before["calls"].([]any) {
		if call == "prepare" || call == "sign" || call == "update" || call == "cancelPrepared" {
			t.Fatal("quote mutated signer state", call)
		}
	}
	if _, err := c.handle(message("send", map[string]string{"id": quote.ID})); err == nil || adapter.submits != 0 {
		t.Fatal("nonpersisted quote acquired signing/submission authority")
	}
	// Explicit Prepare observes current fees again and is the first durable write.
	adapter.fee = func(int) string { return "15" }
	value, err := c.handle(message("prepare", draft))
	if err != nil || value.(model.Plan).MaxFee != "15" {
		t.Fatal("Prepare reused a stale quote", value, err)
	}
	prepares := adapter.prepares
	if _, err := c.handle(message("prepare", draft)); ipc.PublicErrorCode(err) != "PAYMENT_PENDING" || adapter.prepares != prepares {
		t.Fatal("existing prepared operation was replaced or fresh RPC ran", err)
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 1 {
		t.Fatal("explicit Prepare did not create exactly one operation")
	}
}

func TestPaymentQuotePreservesOriginalSignedReservationAndPendingLookupStripsBytes(t *testing.T) {
	adapter, account := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
	plan := publicPlan()
	const canary = "PUBLIC-SYNTHETIC-ORIGINAL-SIGNED-BYTES"
	original := journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: "UNKNOWN", Signed: &model.Signed{Identity: "public-original-identity", Bytes: []byte(canary)}}
	c, path := fakeCoordinator(t, adapter.publicAdapter, &original)
	c.adapters = []model.Adapter{adapter}
	draft := model.Transfer{AccountID: account.ID, Asset: plan.Asset, Recipient: plan.Recipient, Amount: "1"}
	before, _ := json.Marshal(readPublicState(t, path)["ops"])
	if _, err := c.handle(message("quotePayment", quoteRequest(draft, true))); err != nil {
		t.Fatal("existing reservation blocked a read-only estimate", err)
	}
	prepares := adapter.prepares
	if _, err := c.handle(message("prepare", draft)); ipc.PublicErrorCode(err) != "PAYMENT_PENDING" || adapter.prepares != prepares {
		t.Fatal("possibly sent operation was replaced", err)
	}
	value, err := c.handle(message("pendingPayment", map[string]string{"accountId": account.ID}))
	raw, _ := json.Marshal(value)
	var result struct {
		Operation *journal.Operation `json:"operation"`
	}
	if err != nil || json.Unmarshal(raw, &result) != nil || result.Operation == nil || result.Operation.Signed != nil || result.Operation.State != "UNKNOWN" || !equalTONResumeJSON(result.Operation.Plan, original.Plan) || bytes.Contains(raw, []byte(canary)) || bytes.Contains(raw, []byte("public-original-identity")) || bytes.Contains(raw, []byte(`"signed"`)) {
		t.Fatal("pending lookup leaked signed authority or lost original plan", string(raw), err)
	}
	after, _ := json.Marshal(readPublicState(t, path)["ops"])
	if !bytes.Equal(before, after) || adapter.submits != 0 {
		t.Fatal("read-only quote/lookup changed the original journal")
	}
}

func TestPaymentQuoteRejectsNullUnknownFieldsAndInvalidDraftsBeforeRPC(t *testing.T) {
	adapter, account := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
	c, _ := fakeCoordinator(t, adapter.publicAdapter, nil)
	c.adapters = []model.Adapter{adapter}
	for _, body := range []string{
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":"public","amount":"1","memo":""}`,
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":"public","amount":"1","memo":"","maximum":null}`,
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":"public","amount":"1","memo":"","maximum":"true"}`,
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":"public","amount":"1","memo":"","maximum":true,"seed":"forbidden"}`,
		`{"accountId":null,"asset":"unit-testnet:native","recipient":"public","amount":"1","memo":"","maximum":true}`,
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":" ","amount":"1","memo":"","maximum":true}`,
		`{"accountId":"public-account","asset":"unit-testnet:native","recipient":"public","amount":"1e9","memo":"","maximum":false}`,
	} {
		if _, err := c.handle(message("quotePayment", json.RawMessage(body))); ipc.PublicErrorCode(err) != "PAYMENT_QUOTE_INVALID_INPUT" {
			t.Fatal("invalid quote input accepted", body, err)
		}
	}
	if adapter.prepares != 0 || adapter.submits != 0 {
		t.Fatal("invalid input reached RPC/signing", adapter.prepares)
	}
	_, _ = account, adapter
}

func TestPaymentQuoteSafeFailureCodesNeverTrustProviderText(t *testing.T) {
	for _, row := range []struct {
		err  error
		want string
	}{
		{errQuoteBalance, "PAYMENT_QUOTE_BALANCE_UNAVAILABLE"},
		{errQuoteInsufficient, "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"},
		{&evm.InsufficientFeeBalanceError{FeeAsset: "ethereum-mainnet:native", MaxFee: "123"}, "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"},
		{&ton.InsufficientNativeFeeBalanceError{MaxFee: "123", StorageReserve: "10000000"}, "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"},
		{errQuoteUnstable, "PAYMENT_QUOTE_UNSTABLE"},
		{fmt.Errorf("private provider body: %w", ton.ErrRateLimited), "TON_RPC_RATE_LIMITED"},
		{errors.New("PAYMENT_QUOTE_INSUFFICIENT_FUNDS public-untrusted-body"), "PAYMENT_QUOTE_UNAVAILABLE"},
	} {
		if err := paymentQuoteError(row.err); ipc.PublicErrorCode(err) != row.want || err.Error() != row.want {
			t.Fatal("unsafe/misleading quote classification", err, row.want)
		}
	}
}

func TestPaymentQuoteActualEthereumAdapterRepeatedMaxDoesNotPersistOrBroadcast(t *testing.T) {
	const funds = int64(1_000_000_000_000_000_000)
	pendingFunds := funds
	var rpcCalls, estimates, broadcasts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(request.Body).Decode(&q) != nil {
			t.Error("invalid actual-adapter request")
			return
		}
		rpcCalls++
		var value any
		switch q.Method {
		case "eth_chainId":
			value = "0x1"
		case "eth_getBlockByNumber":
			value = map[string]string{"number": "0x10", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x1"}
		case "eth_getBalance":
			var at string
			_ = json.Unmarshal(q.Params[1], &at)
			balance := funds
			if at == "pending" {
				balance = pendingFunds
			}
			value = fmt.Sprintf("0x%x", balance)
		case "eth_getCode":
			value = "0x"
		case "eth_getTransactionCount":
			value = "0x0"
		case "eth_maxPriorityFeePerGas":
			value = "0x1"
		case "eth_estimateGas":
			estimates++
			value = "0x5208"
		case "eth_call":
			value = "0x"
		case "eth_sendRawTransaction":
			broadcasts++
			t.Error("quote broadcast a transaction")
			w.WriteHeader(500)
			return
		default:
			t.Errorf("unexpected quote RPC %s", q.Method)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": value})
	}))
	defer server.Close()
	adapter, err := evm.NewWithEndpoints(map[string]string{"ethereum-mainnet": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	account, err := adapter.Derive(make([]byte, 64), 0, "ethereum-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	draft := model.Transfer{AccountID: account.ID, Asset: "ethereum-mainnet:native", Recipient: "0x1111111111111111111111111111111111111111"}
	for _, pending := range []int64{funds, funds / 2, funds * 2, funds / 2} {
		pendingFunds = pending
		result, err := c.handle(message("quotePayment", quoteRequest(draft, true)))
		if err != nil {
			t.Fatal("real Ethereum Max adapter path failed", err)
		}
		p := result.(model.Plan)
		amount, ok := new(big.Int).SetString(p.Amount, 10)
		if !ok {
			t.Fatal("invalid quote amount")
		}
		fee, ok := new(big.Int).SetString(p.MaxFee, 10)
		if !ok {
			t.Fatal("invalid quote fee")
		}
		budget := funds
		if pending < budget {
			budget = pending
		}
		if new(big.Int).Add(amount, fee).Cmp(big.NewInt(budget)) != 0 || adapter.ValidatePlan(p) != nil || p.FeeAsset != draft.Asset {
			t.Fatal("Max did not preserve exact native fee budget", p)
		}
	}
	pendingFunds = 0
	if result, err := c.handle(message("quotePayment", quoteRequest(draft, true))); result != nil || ipc.PublicErrorCode(err) != "PAYMENT_QUOTE_INSUFFICIENT_FUNDS" {
		t.Fatal("empty pending balance invented a spendable quote", result, err)
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 0 || broadcasts != 0 || estimates < 6 || rpcCalls == 0 {
		t.Fatal("actual adapter quote persisted or bypassed exact estimates", estimates, broadcasts)
	}
}

func TestPaymentQuoteActualTONAdapterRepeatedMaxLeavesNoPreparedReservation(t *testing.T) {
	c, path, account, queries := tonDeploymentCoordinator(t)
	recipient, err := ton.New().Derive(make([]byte, 64), 1, account.Network)
	if err != nil {
		t.Fatal(err)
	}
	draft := model.Transfer{AccountID: account.ID, Asset: account.Network + ":TON", Recipient: recipient.Address}
	for range 3 {
		result, err := c.handle(message("quotePayment", quoteRequest(draft, true)))
		if err != nil {
			t.Fatal("repeat actual TON Max adapter path failed", err)
		}
		plan := result.(model.Plan)
		if plan.Amount != "987000000" || plan.MaxFee != "3000000" || plan.FeeAsset != draft.Asset || ton.New().ValidatePlan(plan) != nil {
			t.Fatal("TON quote changed fee/storage budget or immutable profile", plan)
		}
	}
	state := readPublicState(t, path)
	if len(state["ops"].([]any)) != 0 || queries.Load() == 0 {
		t.Fatal("TON Max left a blocking prepared operation")
	}
	for _, call := range state["calls"].([]any) {
		if call == "prepare" || call == "sign" || call == "update" || call == "cancelPrepared" {
			t.Fatal("TON estimate changed signer state", call)
		}
	}
}

func TestPaymentQuoteRejectsChangedPlansAndBalancesWithoutPreparing(t *testing.T) {
	for _, kind := range []string{"account", "recipient", "asset", "amount", "memo", "fee-domain", "fee-value", "expired", "adapter-validation", "unknown-balance", "zero-balance"} {
		t.Run(kind, func(t *testing.T) {
			base, account := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
			adapter := &changedMaxAdapter{maxAdapter: base}
			c, path := fakeCoordinator(t, base.publicAdapter, nil)
			c.adapters = []model.Adapter{adapter}
			maximum, want := false, "PAYMENT_QUOTE_INVALID_PLAN"
			switch kind {
			case "account":
				adapter.mutate = func(p *model.Plan) { p.Account.Profile = "different-profile" }
			case "recipient":
				adapter.mutate = func(p *model.Plan) { p.Recipient = "different-recipient" }
			case "asset":
				adapter.mutate = func(p *model.Plan) { p.Asset = "foreign:token" }
			case "amount":
				adapter.mutate = func(p *model.Plan) { p.Amount = "11" }
			case "memo":
				adapter.mutate = func(p *model.Plan) { p.Memo = "changed" }
			case "fee-domain":
				adapter.mutate = func(p *model.Plan) { p.FeeAsset = "foreign:gas" }
			case "fee-value":
				adapter.mutate = func(p *model.Plan) { p.MaxFee = "01" }
			case "expired":
				adapter.mutate = func(p *model.Plan) { p.Expires = 1 }
			case "adapter-validation":
				adapter.validationError = errors.New("CANARY private adapter failure")
			case "unknown-balance":
				maximum = true
				base.balances[0].State = "unavailable"
				want = "PAYMENT_QUOTE_BALANCE_UNAVAILABLE"
			case "zero-balance":
				maximum = true
				base.balances[0].Atomic = "0"
				want = "PAYMENT_QUOTE_INSUFFICIENT_FUNDS"
			}
			draft := model.Transfer{AccountID: account.ID, Asset: "unit-testnet:native", Recipient: "public-recipient", Amount: "10"}
			if value, err := c.handle(message("quotePayment", quoteRequest(draft, maximum))); value != nil || ipc.PublicErrorCode(err) != want {
				t.Fatal("invalid quote changed authority or leaked error", value, err, want)
			}
			if len(readPublicState(t, path)["ops"].([]any)) != 0 || adapter.submits != 0 {
				t.Fatal("failed quote mutated journal or sent funds")
			}
		})
	}
}

func TestPendingPaymentRequiresUniqueCurrentOwnedAuthenticatedRecord(t *testing.T) {
	for _, kind := range []string{"empty", "wrong-account", "bad-digest", "changed-profile", "multiple", "journal-failure", "operation-failure"} {
		t.Run(kind, func(t *testing.T) {
			plan := publicPlan()
			original := journal.Operation{Plan: plan, Digest: journal.Digest(plan), State: "PREPARED"}
			c, path := fakeCoordinator(t, &publicAdapter{}, &original)
			state := readPublicState(t, path)
			request := map[string]string{"accountId": plan.Account.ID}
			if kind == "empty" {
				state["ops"] = []journal.Operation{}
			}
			if kind == "wrong-account" {
				request["accountId"] = "foreign-account"
			}
			if kind == "bad-digest" {
				original.Digest = "invalid"
				state["ops"] = []journal.Operation{original}
			}
			if kind == "changed-profile" {
				original.Plan.Account.Profile = "changed"
				original.Digest = journal.Digest(original.Plan)
				state["ops"] = []journal.Operation{original}
			}
			if kind == "multiple" {
				other := original
				other.Plan.ID = "second-active"
				other.Digest = journal.Digest(other.Plan)
				state["ops"] = []journal.Operation{original, other}
			}
			if kind == "journal-failure" {
				state["reject"] = []string{"journalPage"}
			}
			if kind == "operation-failure" {
				state["reject"] = []string{"operation"}
			}
			writePublicState(t, path, state)
			before, _ := json.Marshal(readPublicState(t, path)["ops"])
			result, err := c.handle(message("pendingPayment", request))
			if kind == "empty" {
				raw, _ := json.Marshal(result)
				if err != nil || string(raw) != `{"operation":null}` {
					t.Fatal("empty pending lookup lost truthful null", string(raw), err)
				}
			} else if result != nil || ipc.PublicErrorCode(err) != "PAYMENT_PENDING_UNAVAILABLE" {
				t.Fatal("ambiguous/unowned record became review authority", result, err)
			}
			after, _ := json.Marshal(readPublicState(t, path)["ops"])
			if !bytes.Equal(before, after) {
				t.Fatal("read-only pending lookup changed the ledger")
			}
		})
	}
}
