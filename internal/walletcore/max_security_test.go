package walletcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/hypercore"
	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func checkedMaxBalance(asset, value string) model.Balance {
	return model.Balance{Asset: asset, Atomic: value, State: "provider-observed", At: "checked-chain-position"}
}
func maxFeeFixture(family, asset, gas, balance, gasBalance, fee string, payload json.RawMessage) (*maxAdapter, model.Account) {
	ac := publicAccount()
	ac.Family = family
	assets := []model.Asset{{ID: asset}}
	balances := []model.Balance{checkedMaxBalance(asset, balance)}
	if gas != asset {
		assets = append(assets, model.Asset{ID: gas})
		balances = append(balances, checkedMaxBalance(gas, gasBalance))
	}
	base := &publicAdapter{networks: []model.Network{{ID: ac.Network, Family: family, Assets: assets}}}
	return &maxAdapter{publicAdapter: base, balances: balances, feeAsset: gas, fee: func(int) string { return fee }, payload: payload}, ac
}
func TestMaximumRejectsDuplicateUnobservedAndIncompleteSelectedBalances(t *testing.T) {
	for _, kind := range []string{"duplicate", "empty-state", "empty-position", "unknown", "unavailable", "stale", "pending", "incomplete", "failed", "synchronizing", "error", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			a, ac := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
			switch kind {
			case "duplicate":
				a.balances = append(a.balances, a.balances[0])
			case "empty-state":
				a.balances[0].State = ""
			case "empty-position":
				a.balances[0].At = ""
			case "malformed":
				a.balances[0].Atomic = "1e9"
			default:
				a.balances[0].State = strings.ToUpper(kind)
			}
			if _, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"}); err == nil {
				t.Fatal("unsafe balance yielded Max")
			}
			if a.prepares != 0 || a.submits != 0 {
				t.Fatal("invalid observation reached preparation or submission")
			}
		})
	}
}
func TestMaximumStartsWithAffordableAtomicProbeAndReestimatesFinalAmount(t *testing.T) {
	a, ac := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "700", nil)
	// A previous half-balance probe (500 + 700) was unaffordable. One unit is.
	p, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"})
	if err != nil || p.Amount != "300" || a.prepares != 2 {
		t.Fatal("affordable native Max failed", p, err)
	}
	a, ac = maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "1", nil)
	a.fee = func(i int) string {
		if i == 1 {
			return "10"
		}
		return "12"
	}
	p, err = maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"})
	if err != nil || p.Amount != "988" || p.MaxFee != "12" || a.prepares != 3 {
		t.Fatal("amount-dependent fee was not reestimated", p, err)
	}
}
func TestMaximumTokenBalanceUsesSeparateNativeGasAndAttachments(t *testing.T) {
	for _, r := range []struct{ family, token, gas, balance, fee, native, reserve string }{
		{"evm", "unit-testnet:erc20:USDC", "unit-testnet:native", "123456789", "42000000000000", "42000000000000", ""},
		{"evm", "unit-testnet:erc20:USDT0", "unit-testnet:native", "123456789", "42000000000000", "42000000000000", ""},
		{"ton", "unit-testnet:USDT", "unit-testnet:TON", "123456789", "110000000", "120000000", "10000000"},
	} {
		t.Run(r.family+"-"+r.token, func(t *testing.T) {
			var payload json.RawMessage
			if r.reserve != "" {
				payload = json.RawMessage(`{"storageReserve":"` + r.reserve + `","kind":"jetton_transfer"}`)
			}
			a, ac := maxFeeFixture(r.family, r.token, r.gas, r.balance, r.native, r.fee, payload)
			recipient := "public"
			if r.family == "ton" {
				recipient = "0:" + strings.Repeat("1", 64)
			}
			p, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: r.token, Recipient: recipient})
			if err != nil || p.Amount != r.balance || p.FeeAsset != r.gas {
				t.Fatal("native fee or attachment subtracted from token units", p, err)
			}
			a.balances[1].Atomic = "1"
			if _, err = maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: r.token, Recipient: recipient}); err == nil {
				t.Fatal("token balance replaced native gas funds")
			}
			a.balances[1] = checkedMaxBalance(r.gas, r.native)
			a.balances = append(a.balances, a.balances[1])
			if _, err = maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: r.token, Recipient: recipient}); err == nil {
				t.Fatal("duplicate native gas balance accepted")
			}
		})
	}
}
func TestMaximumTokenNativeObservationAndFeeDomainFailClosed(t *testing.T) {
	for _, kind := range []string{"unknown-gas", "missing-gas", "malformed-gas", "unregistered-fee", "malformed-fee"} {
		a, ac := maxFeeFixture("evm", "unit-testnet:erc20:USDC", "unit-testnet:native", "1000000", "1000", "10", nil)
		switch kind {
		case "unknown-gas":
			a.balances[1].State = "unavailable"
		case "missing-gas":
			a.balances = a.balances[:1]
		case "malformed-gas":
			a.balances[1].Atomic = "00"
		case "unregistered-fee":
			a.feeAsset = "foreign:native"
		case "malformed-fee":
			a.fee = func(int) string { return "1e3" }
		}
		if _, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"}); err == nil {
			t.Fatal("unsafe native fee domain yielded token Max", kind)
		}
	}
}

type changedMaxAdapter struct {
	*maxAdapter
	mutate          func(*model.Plan)
	validationError error
	cancel          context.CancelFunc
}

func (a *changedMaxAdapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	p, err := a.maxAdapter.Prepare(ctx, ac, t)
	if a.mutate != nil {
		a.mutate(&p)
	}
	if a.cancel != nil {
		a.cancel()
	}
	return p, err
}
func (a *changedMaxAdapter) ValidatePlan(p model.Plan) error { return a.validationError }
func TestMaximumDoesNotReturnChangedOrInvalidPlans(t *testing.T) {
	for _, mutate := range []func(*model.Plan){func(p *model.Plan) { p.Account.Network = "foreign" }, func(p *model.Plan) { p.Recipient = "changed-recipient" }, func(p *model.Plan) { p.Asset = "foreign" }, func(p *model.Plan) { p.Amount = "999999" }, func(p *model.Plan) { p.Memo = "changed" }} {
		base, ac := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
		a := &changedMaxAdapter{maxAdapter: base, mutate: mutate}
		if _, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"}); err == nil {
			t.Fatal("changed maximum intent accepted")
		}
	}
	base, ac := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
	a := &changedMaxAdapter{maxAdapter: base, validationError: errors.New("independent adapter validation failed")}
	if _, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"}); err == nil {
		t.Fatal("invalid final plan returned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.validationError = nil
	a.balances[0].Atomic = "1"
	a.fee = func(int) string { return "0" }
	a.cancel = cancel
	if _, err := maxPlan(ctx, a, ac, model.Transfer{AccountID: ac.ID, Asset: a.balances[0].Asset, Recipient: "public"}); err == nil {
		t.Fatal("cancelled Max returned a plan")
	}
	if a.submits != 0 {
		t.Fatal("maximum estimate broadcast an operation")
	}
}
func TestMaximumActualCoreSpotAccountsKeepHeldHYPEAndUSDCUnavailable(t *testing.T) {
	const usdc = "USDC:0x6d1e7cde53ba9467b783cb7c530ce054"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&q)
		var typ string
		_ = json.Unmarshal(q["type"], &typ)
		var result any
		switch typ {
		case "spotMeta":
			result = map[string]any{"tokens": []any{map[string]any{"name": "HYPE", "index": 150, "weiDecimals": 8, "tokenId": "0x0d01dc56dcaaca66ad901c959b4011ec"}, map[string]any{"name": "USDC", "index": 0, "weiDecimals": 8, "tokenId": "0x6d1e7cde53ba9467b783cb7c530ce054", "isCanonical": true}}}
		case "exchangeStatus":
			result = map[string]any{"time": time.Now().UnixMilli()}
		case "spotClearinghouseState":
			result = map[string]any{"balances": []any{map[string]any{"coin": "HYPE", "token": 150, "total": "10", "hold": "2.25"}, map[string]any{"coin": "USDC", "token": 0, "total": "123.45678912", "hold": "3.00000001"}}}
		case "userRole":
			result = map[string]string{"role": "user"}
		case "userAbstraction":
			result = "default"
		default:
			t.Errorf("unexpected Max HTTP method %s", typ)
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	adapter, err := hypercore.NewWithEndpoints(map[string]string{"hypercore-mainnet": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, 64)
	for i := range seed {
		seed[i] = byte(i)
	}
	ac, err := adapter.Derive(seed, 0, "hypercore-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ token, want string }{{"HYPE:0x0d01dc56dcaaca66ad901c959b4011ec", "775000000"}, {usdc, "12045678911"}} {
		p, err := maxPlan(context.Background(), adapter, ac, model.Transfer{AccountID: ac.ID, Asset: "hypercore-mainnet:" + r.token, Recipient: "0x1111111111111111111111111111111111111111"})
		if err != nil || p.Amount != r.want || p.MaxFee != "0" || p.FeeAsset != p.Asset {
			t.Fatal(fmt.Sprintf("Core %s held units or decimal precision mixed", r.token), p, err)
		}
	}
	balances, err := adapter.Balance(context.Background(), ac)
	if err != nil || balances[0].Atomic != "1000000000" || balances[1].Atomic != "12345678912" {
		t.Fatal("Max changed displayed total holdings", balances, err)
	}
}

func TestMaximumFailurePreservesUnknownSignedOperationAndReservation(t *testing.T) {
	a, ac := maxFeeFixture("unit", "unit-testnet:native", "unit-testnet:native", "1000", "", "10", nil)
	p := publicPlan()
	signed := &model.Signed{Identity: "original-public-test-only-identity", Bytes: []byte{1, 2, 3}}
	op := journal.Operation{Plan: p, Digest: journal.Digest(p), State: "UNKNOWN", Signed: signed}
	c, path := fakeCoordinator(t, a.publicAdapter, &op)
	c.adapters = []model.Adapter{a}
	a.balances[0].State = "unavailable"
	before, err := json.Marshal(readPublicState(t, path)["ops"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.handle(message("max", model.Transfer{AccountID: ac.ID, Asset: p.Asset, Recipient: p.Recipient})); err == nil {
		t.Fatal("unknown balance maximum authorized")
	}
	state := readPublicState(t, path)
	after, err := json.Marshal(state["ops"])
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed Max changed original signed operation or reservation", err)
	}
	for _, call := range state["calls"].([]any) {
		if call == "prepare" || call == "sign" || call == "update" {
			t.Fatal("failed Max changed signer journal", call)
		}
	}
	if a.submits != 0 || a.prepares != 0 {
		t.Fatal("failed Max submitted or replaced the original operation")
	}
}
