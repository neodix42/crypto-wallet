package walletcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

// This fixture exercises real coordinator scheduling and durable inherited-pipe
// state with synthetic service acknowledgements. It does not claim live API,
// HMAC, signer approval or custody acceptance; those have separate tests.
type nativeSchedulingAPI struct {
	*fixtureExchangeAPI
	provider     string
	gate         error
	wrongRequest bool
}

func (a *nativeSchedulingAPI) WithdrawalAllowed(context.Context) error { return a.gate }
func (a *nativeSchedulingAPI) CreateOrder(ctx context.Context, order bybit.SpotOrder) (bybit.OrderAcknowledgement, error) {
	a.order = order
	err := a.post(ctx, "/fixture/native/order", order)
	return bybit.OrderAcknowledgement{OrderID: "native-server-order", OrderLinkID: order.OrderLinkID}, err
}
func (a *nativeSchedulingAPI) FindOrder(_ context.Context, symbol, id string) (*bybit.Order, error) {
	if a.order.OrderLinkID == "" {
		return nil, nil
	}
	base, _ := exchangeAtomic(a.order.BaseQty)
	total := new(big.Int).Mul(base, big.NewInt(2000))
	value, _ := exchangeDecimal(total)
	return &bybit.Order{OrderID: "native-server-order", OrderLinkID: id, Symbol: symbol, Side: a.order.Side, OrderType: "Limit", Qty: a.order.BaseQty, BaseQty: a.order.BaseQty, LimitPrice: a.order.LimitPrice, TimeInForce: "IOC", MarketUnit: "baseCoin", IsLeverage: "0", OrderStatus: "Filled", CumExecQty: a.order.BaseQty, CumExecValue: value}, nil
}
func (a *nativeSchedulingAPI) Executions(ctx context.Context, symbol, id, cursor string) (bybit.ExecutionPage, error) {
	order, _ := a.FindOrder(ctx, symbol, id)
	fee, coin := "0.2", "USDT"
	if a.provider == "binance" && a.order.Side == "Buy" {
		fee, coin = "0.00001", "ETH"
	}
	return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{{OrderID: order.OrderID, OrderLinkID: id, Symbol: symbol, Side: a.order.Side, OrderType: "Limit", OrderQty: a.order.BaseQty, ExecID: "native-fill", ExecQty: a.order.BaseQty, ExecValue: order.CumExecValue, ExecPrice: "2000", ExecFee: fee, FeeCurrency: coin, ExtraFees: json.RawMessage(`[]`)}}}, nil
}
func (a *nativeSchedulingAPI) Transfer(ctx context.Context, r bybit.TransferRequest) (bybit.TransferAcknowledgement, error) {
	a.transfer = r
	err := a.post(ctx, "/fixture/native/transfer", r)
	return bybit.TransferAcknowledgement{TransferID: "native-server-transfer", Status: "PENDING"}, err
}
func (a *nativeSchedulingAPI) TransferStatusFor(_ context.Context, r bybit.TransferRequest, remote string) (*bybit.TransferRecord, error) {
	if remote != "native-server-transfer" {
		return nil, nil
	}
	a.balances[r.ToAccountType+":"+r.Coin] = "1000"
	return &bybit.TransferRecord{TransferID: r.TransferID, Coin: r.Coin, Amount: r.Amount, FromAccountType: r.FromAccountType, ToAccountType: r.ToAccountType, Status: "SUCCESS"}, nil
}
func (a *nativeSchedulingAPI) Withdraw(ctx context.Context, r bybit.WithdrawalRequest) (bybit.WithdrawalAcknowledgement, error) {
	a.withdrawal = r
	err := a.post(ctx, "/fixture/native/withdraw", r)
	return bybit.WithdrawalAcknowledgement{ID: "native-server-withdrawal"}, err
}
func (a *nativeSchedulingAPI) WithdrawalRecordsFor(_ context.Context, r bybit.WithdrawalRequest, remote, cursor string) (bybit.WithdrawalPage, error) {
	if remote == "" && a.provider != "binance" {
		return bybit.WithdrawalPage{}, nil
	}
	id := r.RequestID
	if a.wrongRequest {
		id = "different-original-request"
	}
	return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{WithdrawID: "native-server-withdrawal", RequestID: id, Coin: r.Coin, Chain: r.Chain, Amount: r.Amount, WithdrawFee: "0.0002", Status: "success", ToAddress: r.Address, Tag: r.Tag, TxID: "native-chain-observation"}}}, nil
}
func nativeSchedulingCoordinator(t *testing.T, provider string) (*core, *nativeSchedulingAPI, *publicAdapter, string) {
	t.Helper()
	c, base, adapter, path := fixtureExchangeCoordinator(t)
	script, err := os.ReadFile(c.child.Path)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.Replace(string(script), "output={'connected':state.get('exchangeConnected',True)", "output={'provider':state.get('exchangeProvider','bybit'),'connected':state.get('exchangeConnected',True)", 1)
	if source == string(script) {
		t.Fatal("native fixture status injection failed")
	}
	if err = os.WriteFile(c.child.Path, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(source))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(hash[:])})
	if err = os.WriteFile(filepath.Join(filepath.Dir(c.child.Path), "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	state := readPublicState(t, path)
	state["exchangeProvider"], state["exchangeEndpoint"] = provider, "https://api."+provider+".com"
	writePublicState(t, path, state)
	api := &nativeSchedulingAPI{fixtureExchangeAPI: base, provider: provider}
	if provider == "kucoin" {
		api.chain.Chain, api.deposit.Chain = "eth", "eth"
	}
	c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
		api.endpoint, api.authorize = endpoint, authorize
		return api, nil
	}
	return c, api, adapter, path
}
func TestNativeExchangeCoordinatorApprovesFundingThenExactIOCForEachDirection(t *testing.T) {
	for _, provider := range []string{"binance"} {
		for _, side := range []string{"Buy", "Sell"} {
			t.Run(provider+side, func(t *testing.T) {
				c, api, _, path := nativeSchedulingCoordinator(t, provider)
				value, spend := "0.1", "ETH"
				if side == "Buy" {
					value, spend = "200", "USDT"
				}
				api.balances["UNIFIED:"+spend] = "0"
				result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"provider": provider, "side": side, "amount": value, "slippage": "0.5"})
				if result.State != "CONFIRMED" || api.posts != 2 || api.order.OrderType != "Limit" || api.order.LimitPrice == "" || api.order.BaseQty == "" || api.order.Qty != value || api.transfer.Amount != value || api.transfer.FromAccountType != "FUND" || api.transfer.ToAccountType != "UNIFIED" {
					t.Fatal("native scheduling or exact budget", result, api.order, api.transfer, api.posts)
				}
				state := readPublicState(t, path)
				operations := state["exchangeOps"].([]any)
				if len(operations) != 2 || operations[0].(map[string]any)["kind"] != "transfer" || operations[1].(map[string]any)["kind"] != "swap" {
					t.Fatal("missing separate economic operations", operations)
				}
				approvals := 0
				for _, call := range state["calls"].([]any) {
					if call == "exchangeAuthorize" {
						approvals++
					}
				}
				if approvals != 2 {
					t.Fatal("funding implicitly authorized trade", approvals)
				}
			})
		}
	}
}
func TestNativeExchangeCoordinatorLostAcknowledgementsReconcileOriginalWithoutResend(t *testing.T) {
	for _, provider := range []string{"binance"} {
		t.Run(provider, func(t *testing.T) {
			c, api, _, path := nativeSchedulingCoordinator(t, provider)
			api.fail["/fixture/native/order"] = errors.New("synthetic lost acknowledgement")
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"provider": provider, "side": "Sell", "amount": "0.1", "slippage": "0.5"})
			if result.State != "UNKNOWN" || api.posts != 1 {
				t.Fatal(result, api.posts)
			}
			original := api.order.OrderLinkID
			state := readPublicState(t, path)
			if state["exchangeOps"].([]any)[0].(map[string]any)["state"] != "UNKNOWN" {
				t.Fatal("unknown not durable")
			}
			out, err := c.handle(message("exchangeReconcile", map[string]string{"provider": provider, "id": result.ID}))
			if err != nil || out.(exchangeResult).State != "CONFIRMED" || api.posts != 1 || api.order.OrderLinkID != original {
				t.Fatal("original-only reconciliation failed", out, err, api.posts)
			}
		})
	}
}
func TestNativeExchangeWithdrawalPreflightAndExactOriginalRequestRecovery(t *testing.T) {
	for _, provider := range []string{"binance", "kucoin"} {
		t.Run(provider, func(t *testing.T) {
			c, api, _, _ := nativeSchedulingCoordinator(t, provider)
			api.balances["FUND:ETH"] = "0"
			api.gate = errors.New("synthetic unsupported regional or account-mode requirement")
			if _, err := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"provider": provider, "amount": "0.1"})); err == nil || api.posts != 0 {
				t.Fatal("preflight moved funds", err, api.posts)
			}
			c, api, _, _ = nativeSchedulingCoordinator(t, provider)
			result := exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"provider": provider, "amount": "0.1"})
			mode := "binance-native"
			if provider == "kucoin" {
				mode = "kucoin-external"
			}
			if result.State != "EXCHANGE_COMPLETED" || api.posts != 1 || api.withdrawal.FeeMode != mode || api.withdrawal.FeeType != 0 || api.withdrawal.Amount != "0.1" || api.withdrawal.AccountType != "FUND" || api.withdrawal.Address != "0x1111111111111111111111111111111111111111" {
				t.Fatal("native withdrawal policy", result, api.withdrawal)
			}
			c, api, _, _ = nativeSchedulingCoordinator(t, provider)
			api.fail["/fixture/native/withdraw"] = errors.New("synthetic lost acknowledgement")
			result = exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"provider": provider, "amount": "0.1"})
			if result.State != "UNKNOWN" {
				t.Fatal(result)
			}
			api.wrongRequest = true
			out, err := c.handle(message("exchangeReconcile", map[string]string{"provider": provider, "id": result.ID}))
			if provider == "binance" && err == nil {
				t.Fatal("foreign original withdrawal identity accepted", out)
			}
			api.wrongRequest = false
			out, err = c.handle(message("exchangeReconcile", map[string]string{"provider": provider, "id": result.ID}))
			want := "UNKNOWN"
			if provider == "binance" {
				want = "EXCHANGE_COMPLETED"
			}
			if err != nil || out.(exchangeResult).State != want || api.posts != 1 {
				t.Fatal("lost withdrawal acknowledgement misclassified or resent", out, err, api.posts)
			}
		})
	}
}

func TestKuCoinSpotFeeUncertaintyGatesBeforeFundingWithoutHidingWithdrawal(t *testing.T) {
	c, api, _, _ := nativeSchedulingCoordinator(t, "kucoin")
	api.balances["UNIFIED:ETH"] = "0"
	api.balances["UNIFIED:USDT"] = "0"
	for _, side := range []string{"Buy", "Sell"} {
		out, err := c.handle(exchangeMessage("exchangeQuote", map[string]any{"provider": "kucoin", "side": side}))
		if err != nil {
			t.Fatal(err)
		}
		quote := out.(exchangeQuote)
		if quote.Symbol != "" || !strings.Contains(quote.TradingUnavailable, "KCS") || !quote.Route.Supported || !quote.Route.WithdrawEnabled {
			t.Fatal("fee gate hid safe capabilities or allowed a quote", quote, err)
		}
		if _, err = c.handle(exchangeMessage("exchangeSwap", map[string]any{"provider": "kucoin", "side": side, "amount": "0.1", "slippage": "0.5"})); err == nil || api.posts != 0 {
			t.Fatal("KuCoin fee gate moved funds", err, api.posts)
		}
	}
	api.balances["FUND:ETH"] = "0"
	out, err := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"provider": "kucoin", "amount": "0.1"}))
	if err != nil || out.(exchangeResult).State != "FUNDING_REQUIRED" || !strings.Contains(out.(exchangeResult).Message, "KuCoin Funding") || api.posts != 0 {
		t.Fatal("unverifiable KuCoin transfer submitted or manual-funding guidance missing", out, err, api.posts)
	}
}
