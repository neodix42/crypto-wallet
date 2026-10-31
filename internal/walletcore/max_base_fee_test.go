package walletcore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

func TestMaximumActualBaseNativeValueSizeFeeCanIncreaseAfterSmallProbe(t *testing.T) {
	const balance = int64(1_000_000_000)
	l1Selector := "0x" + hex.EncodeToString(crypto.Keccak256([]byte("getL1FeeUpperBound(uint256)"))[:4])
	operatorSelector := "0x" + hex.EncodeToString(crypto.Keccak256([]byte("getOperatorFee(uint256)"))[:4])
	var estimates, l1Reads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			t.Error("invalid loopback RPC")
			return
		}
		var result any
		switch q.Method {
		case "eth_chainId":
			result = "0x2105"
		case "eth_getBlockByNumber":
			result = map[string]string{"number": "0x10", "hash": "0x" + strings.Repeat("a", 64), "timestamp": fmt.Sprintf("0x%x", time.Now().Unix()), "baseFeePerGas": "0x1"}
		case "eth_getBalance":
			result = fmt.Sprintf("0x%x", balance)
		case "eth_getCode":
			result = "0x" // owned EOA; unrelated token RPC stays unavailable
		case "eth_getTransactionCount":
			result = "0x0"
		case "eth_maxPriorityFeePerGas":
			result = "0x1"
		case "eth_estimateGas":
			estimates++
			result = "0x5208"
		case "eth_call":
			var msg map[string]string
			_ = json.Unmarshal(q.Params[0], &msg)
			switch {
			case strings.HasPrefix(msg["data"], l1Selector):
				l1Reads++
				size, ok := new(big.Int).SetString(msg["data"][10:], 16)
				if !ok {
					t.Error("invalid oracle size")
					return
				}
				result = fmt.Sprintf("0x%064x", new(big.Int).Mul(size, big.NewInt(1000)))
			case strings.HasPrefix(msg["data"], operatorSelector):
				result = fmt.Sprintf("0x%064x", 0)
			default:
				result = "0x"
			}
		default:
			t.Errorf("Max invoked unrelated RPC %s", q.Method)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
	}))
	defer server.Close()
	adapter, err := evm.NewWithEndpoints(map[string]string{"base-mainnet": server.URL})
	if err != nil {
		t.Fatal(err)
	}
	seed := make([]byte, 64)
	seed[0] = 1
	account, err := adapter.Derive(seed, 0, "base-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	p, err := maxPlan(context.Background(), adapter, account, model.Transfer{AccountID: account.ID, Asset: "base-mainnet:native", Recipient: "0x1111111111111111111111111111111111111111"})
	if err != nil {
		t.Fatal("valid Base Max could not adjust a value-size-dependent L1 estimate", err)
	}
	amount, ok := new(big.Int).SetString(p.Amount, 10)
	if !ok {
		t.Fatal("invalid final amount")
	}
	fee, ok := new(big.Int).SetString(p.MaxFee, 10)
	if !ok {
		t.Fatal("invalid final fee")
	}
	if amount.Sign() <= 0 || new(big.Int).Add(amount, fee).Cmp(big.NewInt(balance)) != 0 || estimates < 3 || l1Reads < 3 || adapter.ValidatePlan(p) != nil {
		t.Fatal("Max reused a small-value fee or returned a conservative probe instead of final exact budget", p, estimates, l1Reads)
	}
}

type refusedFeeQuoteAdapter struct {
	*maxAdapter
	failure error
}

func (a *refusedFeeQuoteAdapter) Prepare(ctx context.Context, ac model.Account, t model.Transfer) (model.Plan, error) {
	p, err := a.maxAdapter.Prepare(ctx, ac, t)
	if a.prepares == 2 {
		return model.Plan{}, a.failure
	}
	return p, err
}
func TestMaximumNeverRetriesRPCMetadataOrForeignFeeFailures(t *testing.T) {
	const native = "unit-testnet:native"
	for _, row := range []struct {
		name    string
		failure error
		token   bool
	}{
		{"RPC429", fmt.Errorf("HTTP429 untrusted public provider body"), false},
		{"metadata", fmt.Errorf("runtime metadata changed"), false},
		{"untyped-insufficient", fmt.Errorf("insufficient gas currency balance"), false},
		{"zero-quote", &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "0"}, false},
		{"malformed-quote", &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "01"}, false},
		{"no-budget-progress", &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "10"}, false},
		{"insufficient-after-fee", &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "1000"}, false},
		{"foreign-domain", &evm.InsufficientFeeBalanceError{FeeAsset: "foreign:native", MaxFee: "12"}, false},
		{"token-fees-native", &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "12"}, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			asset, gas := native, native
			if row.token {
				asset = "unit-testnet:erc20:USDC"
			}
			base, ac := maxFeeFixture("evm", asset, gas, "1000", "1000", "10", nil)
			a := &refusedFeeQuoteAdapter{maxAdapter: base, failure: row.failure}
			p, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: asset, Recipient: "0x1111111111111111111111111111111111111111"})
			if err == nil || p.ID != "" || a.prepares != 2 || a.submits != 0 {
				t.Fatal("unverified quote caused a retry, partial plan or broadcast", p, err, a.prepares)
			}
		})
	}
	quoted := &evm.InsufficientFeeBalanceError{FeeAsset: native, MaxFee: "12"}
	if quoted.Error() != "insufficient gas currency balance" {
		t.Fatal("local typed fee quote changed the fixed error category")
	}
}
