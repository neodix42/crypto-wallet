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
	"time"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

// This deterministic inherited-pipe fixture models the signer's durable states
// for coordinator fault tests. It has no secret, actual signature or network.
// Real client wire/TLS and real signer/vault evidence have separate tests.
const exchangeChildBranches = `
 elif method=='exchangeStatus':
  output={'connected':state.get('exchangeConnected',True),'endpoint':state.get('exchangeEndpoint','https://api.bybit.com'),'credentialId':'fixture-credential','keySuffix':'1234','operations':state.get('exchangeOps',[]),'operationCount':len(state.get('exchangeOps',[]))}
 elif method=='exchangeConnect':state['exchangeConnected']=True;state['exchangeEndpoint']=body['endpoint'];output={}
 elif method=='exchangeDisconnect':state['exchangeConnected']=False;output={}
 elif method=='exchangePrepare':
  kind=body['kind'];key={'swap':'order','transfer':'transfer','withdraw':'withdrawal'}[kind]
  ident=format(len(state.get('exchangeOps',[]))+1,'032x')
  payload=dict(body[key])
  if kind=='swap':payload['orderLinkId']=ident
  elif kind=='transfer':payload['transferId']=ident[:8]+'-'+ident[8:12]+'-'+ident[12:16]+'-'+ident[16:20]+'-'+ident[20:]
  else:payload['requestId']=ident;payload['timestamp']=int(time.time()*1000)
  output={'id':ident,'kind':kind,'endpoint':state.get('exchangeEndpoint','https://api.bybit.com'),'credentialId':'fixture-credential','accountId':body['accountId'],'assetId':body['assetId'],'payload':json.dumps(payload,separators=(',',':')),'state':'PREPARED','createdAt':int(time.time()),'expiresAt':int(time.time())+300,'digest':'fixture-only'}
  for field in ['lastPrice','fee','feeCoin','chainType','contractAddress']:
   if field in body:output[field]=body[field]
  state.setdefault('exchangeOps',[]).append(output)
 elif method=='exchangeAuthorize':
  output=next(o for o in state.get('exchangeOps',[]) if o['id']==body['id']);output['state']='AUTHORIZED'
  if 'authorizationPayloadOverride' in state:output['payload']=state['authorizationPayloadOverride']
  if 'authorizationKindOverride' in state:output['kind']=state['authorizationKindOverride']
 elif method=='exchangeAuth':
  if body['method']=='POST':
   matches=[o for o in state.get('exchangeOps',[]) if o['payload']==body['payload'] and o['state']=='AUTHORIZED']
   if len(matches)!=1:error='SYNTHETIC_DENIED'
   else:matches[0]['state']='SUBMITTING'
  state.setdefault('exchangeAuthRequests',[]).append(body)
  output={'apiKey':'publicFixtureKey','signature':'0'*64}
 elif method=='exchangeOperation':output=next((o for o in state.get('exchangeOps',[]) if o['id']==body['id']),None)
 elif method=='exchangeUpdate':
  output=next(o for o in state.get('exchangeOps',[]) if o['id']==body['id']);output['state']=body['state'];output['externalId']=body['externalId']
 elif method=='exchangeReject':
  output=next(o for o in state.get('exchangeOps',[]) if o['id']==body['id']);output['state']='FAILED'
`

type fixtureExchangeAPI struct {
	endpoint        string
	authorize       bybit.Authorizer
	path            string
	info            bybit.APIKeyInformation
	chain           bybit.Chain
	deposit         bybit.Deposit
	balances        map[string]string
	fail            map[string]error
	order           bybit.SpotOrder
	transfer        bybit.TransferRequest
	withdrawal      bybit.WithdrawalRequest
	posts, closed   int
	orderResult     *bybit.Order
	findOrder       func(string, string) (*bybit.Order, error)
	executions      func(string, string, string) (bybit.ExecutionPage, error)
	transferRecord  *bybit.TransferRecord
	withdrawalPage  *bybit.WithdrawalPage
	instrument      *bybit.Instrument
	ticker          *bybit.Ticker
	fee             *bybit.FeeRate
	coinOverride    *bybit.Coin
	balanceOverride *bybit.Balance
	beforePost      func()
	balanceHook     func(string, string) (bybit.Balance, error)
}

func newFixtureExchangeAPI() *fixtureExchangeAPI {
	a := &fixtureExchangeAPI{balances: map[string]string{}, fail: map[string]error{}, chain: bybit.Chain{Chain: "ETH", ChainType: "Ethereum", ChainDeposit: "1", ChainWithdraw: "1", DepositMin: "0.001", WithdrawMin: "0.001", WithdrawFee: "0.0002", WithdrawPercentageFee: "0", MinAccuracy: "18", WithdrawMax: "10"}, deposit: bybit.Deposit{Chain: "ETH", ChainType: "Ethereum", Address: "0x2222222222222222222222222222222222222222", BatchReleaseLimit: "-1"}}
	a.info.UserID = 123
	a.info.IsMaster = true
	a.info.UTA = 1
	a.info.Permissions.Spot = []string{"SpotTrade"}
	a.info.Permissions.Wallet = []string{"Withdraw", "AccountTransfer"}
	return a
}
func (a *fixtureExchangeAPI) Close() { a.closed++ }
func (a *fixtureExchangeAPI) Endpoint() string {
	if a.endpoint != "" {
		return a.endpoint
	}
	return "https://api.bybit.com"
}
func (a *fixtureExchangeAPI) APIKeyInfo(context.Context) (bybit.APIKeyInformation, error) {
	return a.info, a.fail["key"]
}
func (a *fixtureExchangeAPI) CoinInfo(_ context.Context, coin string) (bybit.Coin, error) {
	if a.coinOverride != nil {
		return *a.coinOverride, a.fail["coin"]
	}
	chain := a.chain
	if coin == "USDT" {
		chain.ContractAddress = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	}
	return bybit.Coin{Coin: coin, Name: coin, Chains: []bybit.Chain{chain}}, a.fail["coin"]
}
func (a *fixtureExchangeAPI) DepositAddress(context.Context, string, string) (bybit.Deposit, error) {
	return a.deposit, a.fail["deposit"]
}
func (a *fixtureExchangeAPI) AccountBalance(_ context.Context, kind, coin string) (bybit.Balance, error) {
	if a.balanceHook != nil {
		return a.balanceHook(kind, coin)
	}
	if a.balanceOverride != nil {
		return *a.balanceOverride, a.fail["balance"]
	}
	value, ok := a.balances[kind+":"+coin]
	if !ok {
		value = "1000"
	}
	var b bybit.Balance
	b.AccountType = kind
	b.AccountID = "1"
	b.MemberID = "123"
	b.Balance.Coin = coin
	b.Balance.WalletBalance = value
	b.Balance.TransferBalance = value
	b.Balance.TransferSafeAmount = value
	return b, a.fail["balance"]
}
func (a *fixtureExchangeAPI) SpotInstrument(_ context.Context, symbol string) (bybit.Instrument, error) {
	if a.instrument != nil {
		return *a.instrument, a.fail["instrument"]
	}
	v := bybit.Instrument{Symbol: symbol, BaseCoin: strings.TrimSuffix(symbol, "USDT"), QuoteCoin: "USDT", Status: "Trading"}
	v.LotSizeFilter.BasePrecision = "0.000001"
	v.LotSizeFilter.QuotePrecision = "0.01"
	v.LotSizeFilter.MinOrderAmt = "5"
	v.LotSizeFilter.MaxMarketOrderQty = "100"
	v.PriceFilter.TickSize = "0.01"
	return v, a.fail["instrument"]
}
func (a *fixtureExchangeAPI) SpotTicker(_ context.Context, symbol string) (bybit.Ticker, error) {
	if a.ticker != nil {
		return *a.ticker, a.fail["ticker"]
	}
	return bybit.Ticker{Symbol: symbol, LastPrice: "2000", Bid1Price: "1999", Ask1Price: "2001"}, a.fail["ticker"]
}
func (a *fixtureExchangeAPI) SpotFeeRate(_ context.Context, symbol string) (bybit.FeeRate, error) {
	if a.fee != nil {
		return *a.fee, a.fail["fee"]
	}
	return bybit.FeeRate{Symbol: symbol, TakerFeeRate: "0.001", MakerFeeRate: "0.001"}, a.fail["fee"]
}
func (a *fixtureExchangeAPI) post(ctx context.Context, path string, payload any) error {
	b, _ := json.Marshal(payload)
	if a.authorize != nil {
		if _, e := a.authorize(ctx, bybit.AuthRequest{Endpoint: a.Endpoint(), Method: "POST", Path: path, Payload: string(b), TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}); e != nil {
			return e
		}
	}
	if a.path != "" {
		state := readExchangeFixtureState(a.path)
		var count int
		for _, op := range state["exchangeOps"].([]any) {
			if op.(map[string]any)["state"] == "SUBMITTING" {
				count++
			}
		}
		if count != 1 {
			return errors.New("network attempt preceded durable SUBMITTING")
		}
	}
	if a.beforePost != nil {
		a.beforePost()
	}
	a.posts++
	return a.fail[path]
}
func (a *fixtureExchangeAPI) CreateOrder(ctx context.Context, o bybit.SpotOrder) (bybit.OrderAcknowledgement, error) {
	a.order = o
	e := a.post(ctx, "/v5/order/create", o)
	return bybit.OrderAcknowledgement{OrderID: "remote-order-1", OrderLinkID: o.OrderLinkID}, e
}
func (a *fixtureExchangeAPI) FindOrder(_ context.Context, symbol, link string) (*bybit.Order, error) {
	if a.findOrder != nil {
		return a.findOrder(symbol, link)
	}
	if e := a.fail["find"]; e != nil {
		return nil, e
	}
	if a.orderResult != nil {
		return a.orderResult, nil
	}
	if a.order.OrderLinkID == "" {
		return nil, nil
	}
	base, quote := "0.1", "200"
	if a.order.Side == "Sell" {
		base = a.order.Qty
	}
	return &bybit.Order{OrderID: "remote-order-1", OrderLinkID: link, Symbol: symbol, Side: a.order.Side, OrderType: "Market", Qty: a.order.Qty, MarketUnit: a.order.MarketUnit, IsLeverage: "0", OrderStatus: "Filled", CumExecQty: base, CumExecValue: quote, CumExecFee: "0.2"}, nil
}
func (a *fixtureExchangeAPI) Executions(_ context.Context, symbol, link, cursor string) (bybit.ExecutionPage, error) {
	if a.executions != nil {
		return a.executions(symbol, link, cursor)
	}
	if e := a.fail["executions"]; e != nil {
		return bybit.ExecutionPage{}, e
	}
	fee, coin := "0.2", "USDT"
	if a.order.Side == "Buy" {
		fee, coin = "0.0001", strings.TrimSuffix(symbol, "USDT")
	}
	return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{{OrderID: "remote-order-1", OrderLinkID: link, Symbol: symbol, Side: a.order.Side, OrderType: "Market", ExecID: "fill-1", ExecQty: "0.1", ExecValue: "200", ExecPrice: "2000", ExecFee: fee, FeeCurrency: coin, ExtraFees: json.RawMessage(`""`)}}}, nil
}
func (a *fixtureExchangeAPI) Transfer(ctx context.Context, v bybit.TransferRequest) (bybit.TransferAcknowledgement, error) {
	a.transfer = v
	e := a.post(ctx, "/v5/asset/transfer/inter-transfer", v)
	return bybit.TransferAcknowledgement{TransferID: v.TransferID, Status: "SUCCESS"}, e
}
func (a *fixtureExchangeAPI) TransferStatus(_ context.Context, id string) (*bybit.TransferRecord, error) {
	if a.transferRecord != nil {
		return a.transferRecord, a.fail["transferstatus"]
	}
	if a.transfer.TransferID == "" {
		return nil, a.fail["transferstatus"]
	}
	a.balances[a.transfer.ToAccountType+":"+a.transfer.Coin] = "1000"
	return &bybit.TransferRecord{TransferID: id, Coin: a.transfer.Coin, Amount: a.transfer.Amount, FromAccountType: a.transfer.FromAccountType, ToAccountType: a.transfer.ToAccountType, Status: "SUCCESS"}, a.fail["transferstatus"]
}
func (a *fixtureExchangeAPI) Withdraw(ctx context.Context, v bybit.WithdrawalRequest) (bybit.WithdrawalAcknowledgement, error) {
	a.withdrawal = v
	e := a.post(ctx, "/v5/asset/withdraw/create", v)
	return bybit.WithdrawalAcknowledgement{ID: "withdrawal-1"}, e
}
func (a *fixtureExchangeAPI) WithdrawalRecords(_ context.Context, coin, id, cursor string) (bybit.WithdrawalPage, error) {
	if a.withdrawalPage != nil {
		return *a.withdrawalPage, a.fail["withdrawstatus"]
	}
	return bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{WithdrawID: id, Coin: coin, Chain: a.withdrawal.Chain, Amount: "0.0998", WithdrawFee: "0.0002", Status: "success", ToAddress: a.withdrawal.Address, Tag: a.withdrawal.Tag, WithdrawType: 0, TxID: "fixture-onchain-hash"}}}, a.fail["withdrawstatus"]
}
func (a *fixtureExchangeAPI) DepositRecords(context.Context, string, string, string) (bybit.DepositPage, error) {
	return bybit.DepositPage{Rows: []bybit.DepositRecord{}}, nil
}
func readExchangeFixtureState(path string) map[string]any {
	b, e := os.ReadFile(path)
	if e != nil {
		panic(e)
	}
	var state map[string]any
	if json.Unmarshal(b, &state) != nil {
		panic("invalid fixture state")
	}
	return state
}

func fixtureExchangeCoordinator(t *testing.T) (*core, *fixtureExchangeAPI, *publicAdapter, string) {
	t.Helper()
	adapter := &publicAdapter{networks: []model.Network{{ID: "ethereum-mainnet", Family: "evm", Assets: []model.Asset{{ID: "ethereum-mainnet:native", Symbol: "ETH", Decimals: 18}, {ID: "ethereum-mainnet:usdt", Symbol: "USDT", Decimals: 6, Contract: "0xdAC17F958D2ee523a2206206994597C13D831ec7"}}}}, receiptState: "CONFIRMED"}
	c, path := fakeCoordinator(t, adapter, nil)
	c.child.Environment = "mainnet"
	script := strings.Replace(publicChild, " else:output={'locked':False}", exchangeChildBranches+"\n else:output={'locked':False}", 1)
	script = strings.Replace(script, "'recoveryConfirmed':True", "'recoveryConfirmed':state.get('recoveryConfirmed',True)", 1)
	if script == publicChild {
		t.Fatal("fixture injection failed")
	}
	if e := os.WriteFile(c.child.Path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256([]byte(script))
	manifest, _ := json.Marshal(map[string]string{"wallet-signer": hex.EncodeToString(hash[:])})
	if e := os.WriteFile(filepath.Join(filepath.Dir(c.child.Path), "manifest.json"), manifest, 0600); e != nil {
		t.Fatal(e)
	}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{{ID: "exchange-account", Family: "evm", Network: "ethereum-mainnet", Profile: "synthetic-fixture", Address: "0x1111111111111111111111111111111111111111"}}
	writePublicState(t, path, state)
	api := newFixtureExchangeAPI()
	api.path = path
	c.newExchange = func(endpoint string, authorize bybit.Authorizer) (exchangeAPI, error) {
		api.endpoint = endpoint
		api.authorize = authorize
		return api, nil
	}
	return c, api, adapter, path
}
func exchangeMessage(method string, extra map[string]any) ipc.Message {
	body := map[string]any{"accountId": "exchange-account", "assetId": "ethereum-mainnet:native"}
	for k, v := range extra {
		body[k] = v
	}
	return message(method, body)
}
func exchangeResultCall(t *testing.T, c *core, method string, extra map[string]any) exchangeResult {
	t.Helper()
	out, e := c.handle(exchangeMessage(method, extra))
	if e != nil {
		t.Fatal(e)
	}
	result, ok := out.(exchangeResult)
	if !ok {
		t.Fatalf("unexpected %T", out)
	}
	return result
}

func TestExchangeQuoteShowsExactPricesBalancesAndScopedMaintenance(t *testing.T) {
	c, api, _, _ := fixtureExchangeCoordinator(t)
	api.balances["FUND:ETH"] = "1.234567891234567891"
	out, e := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Sell"}))
	if e != nil {
		t.Fatal(e)
	}
	q := out.(exchangeQuote)
	if q.Symbol != "ETHUSDT" || q.LastPrice != "2000" || q.Route.Chain != "ETH" || !q.Route.DepositEnabled || !q.Route.WithdrawEnabled || len(q.Balances) != 4 || q.Balances[0].Available != "1.234567891234567891" || q.ObservedAt == "" {
		t.Fatal(q)
	}
	api.chain.ChainDeposit = "0"
	api.info.ReadOnly = 1
	out, e = c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Buy"}))
	if e != nil {
		t.Fatal(e)
	}
	q = out.(exchangeQuote)
	if q.Route.DepositEnabled || q.Route.WithdrawEnabled {
		t.Fatal("maintenance/read-only capability became enabled")
	}
	api.coinOverride = &bybit.Coin{Coin: "ETH", Chains: []bybit.Chain{{Chain: "BASE", ChainType: "Base"}}}
	out, e = c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Sell"}))
	if e != nil {
		t.Fatal(e)
	}
	q = out.(exchangeQuote)
	if q.Route.Supported || q.Route.Reason == "" || q.Symbol != "ETHUSDT" {
		t.Fatal("missing route silently invented or spot availability lost", q)
	}
	out, e = c.handle(exchangeMessage("exchangeQuote", map[string]any{"assetId": "ethereum-mainnet:usdt", "side": "Sell"}))
	if e != nil {
		t.Fatal(e)
	}
	if q = out.(exchangeQuote); q.Symbol != "" || q.Coin != "USDT" || len(q.Balances) != 2 {
		t.Fatal("USDTUSDT pair invented", q)
	}
}

func TestExchangeSpotBuySellAndDurableUnknownNeverResends(t *testing.T) {
	for _, side := range []string{"Sell", "Buy"} {
		t.Run(side, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			value := "0.1"
			if side == "Buy" {
				value = "200"
			}
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": side, "amount": value, "slippage": "0.5"})
			if result.State != "CONFIRMED" || result.FilledBase != "0.1" || result.FilledQuote != "200" || api.posts != 1 || api.order.IsLeverage != 0 {
				t.Fatal(result, api.order)
			}
			unit := "baseCoin"
			feeCoin := "USDT"
			fee := "0.2"
			if side == "Buy" {
				unit = "quoteCoin"
				feeCoin = "ETH"
				fee = "0.0001"
			}
			if api.order.MarketUnit != unit || result.Fees[feeCoin] != fee {
				t.Fatal("wrong spending unit or fee", api.order, result)
			}
			state := readPublicState(t, path)
			if len(state["exchangeOps"].([]any)) != 1 || len(state["exchangeAuthRequests"].([]any)) != 1 {
				t.Fatal("unexpected authority calls", state)
			}
			if _, e := c.handle(message("exchangeReconcile", map[string]string{"id": result.ID})); e != nil || api.posts != 1 {
				t.Fatal("reconcile resent terminal operation", e, api.posts)
			}
		})
	}
	c, api, _, path := fixtureExchangeCoordinator(t)
	api.fail["/v5/order/create"] = errors.New("synthetic timeout after acceptance")
	result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
	if result.State != "UNKNOWN" || api.posts != 1 {
		t.Fatal(result, api.posts)
	}
	state := readPublicState(t, path)
	if state["exchangeOps"].([]any)[0].(map[string]any)["state"] != "UNKNOWN" {
		t.Fatal("uncertainty not durable")
	}
	delete(api.fail, "/v5/order/create")
	out, e := c.handle(message("exchangeReconcile", map[string]string{"id": result.ID}))
	if e != nil || out.(exchangeResult).State != "CONFIRMED" || api.posts != 1 {
		t.Fatal("reconcile changed original request", out, e, api.posts)
	}
}

func TestExchangeFundingMovesOnlyMissingAmountAndApprovesEachEconomicOperation(t *testing.T) {
	c, api, _, path := fixtureExchangeCoordinator(t)
	api.balances["UNIFIED:ETH"] = "0.04"
	api.balances["FUND:ETH"] = "0.06"
	result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
	if result.State != "CONFIRMED" || api.transfer.Amount != "0.06" || api.transfer.FromAccountType != "FUND" || api.transfer.ToAccountType != "UNIFIED" || api.posts != 2 {
		t.Fatal(result, api.transfer, api.posts)
	}
	state := readPublicState(t, path)
	if len(state["exchangeOps"].([]any)) != 2 {
		t.Fatal("internal transfer lost from durable operations")
	}
	calls := state["calls"].([]any)
	var approvals int
	for _, call := range calls {
		if call == "exchangeAuthorize" {
			approvals++
		}
	}
	if approvals != 2 {
		t.Fatal("transfer authorized a later trade implicitly", approvals)
	}
	c, api, _, _ = fixtureExchangeCoordinator(t)
	api.balances["UNIFIED:ETH"] = "0"
	api.transferRecord = &bybit.TransferRecord{TransferID: "00000000-0000-0000-0000-000000000001", Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED", Status: "PENDING"}
	result = exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
	if result.Kind != "transfer" || result.State != "PENDING" || api.posts != 1 || api.order.OrderLinkID != "" {
		t.Fatal("trade ran before funding completed", result, api.posts)
	}
}

func TestExchangeWithdrawTargetsOwnedWalletAndDoesNotGuessLostAcknowledgement(t *testing.T) {
	c, api, _, _ := fixtureExchangeCoordinator(t)
	result := exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"amount": "0.1"})
	if result.State != "EXCHANGE_COMPLETED" || result.TransactionID != "fixture-onchain-hash" || api.withdrawal.Address != "0x1111111111111111111111111111111111111111" || api.withdrawal.ForceChain != 1 || api.withdrawal.FeeType != 1 || api.withdrawal.AccountType != "FUND" || api.posts != 1 {
		t.Fatal(result, api.withdrawal)
	}
	c, api, _, _ = fixtureExchangeCoordinator(t)
	api.fail["/v5/asset/withdraw/create"] = errors.New("synthetic timeout")
	result = exchangeResultCall(t, c, "exchangeWithdraw", map[string]any{"amount": "0.1"})
	if result.State != "UNKNOWN" {
		t.Fatal(result)
	}
	api.withdrawalPage = &bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{{WithdrawID: "different-withdrawal", Coin: "ETH", Chain: "ETH", Amount: "0.0998", WithdrawFee: "0.0002", ToAddress: api.withdrawal.Address, Status: "success"}}}
	out, e := c.handle(message("exchangeReconcile", map[string]string{"id": result.ID}))
	if e != nil || out.(exchangeResult).State != "UNKNOWN" || api.posts != 1 {
		t.Fatal("lost ack guessed from address/amount", out, e, api.posts)
	}
}

func TestExchangeDepositsReuseWalletJournalAndTONMemoIsSigned(t *testing.T) {
	c, api, adapter, path := fixtureExchangeCoordinator(t)
	result := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": "0.123456789123456789"})
	if result.Kind != "deposit" || result.State != "CONFIRMED" || adapter.lastTransfer.Amount != "123456789123456789" || adapter.lastTransfer.Recipient != api.deposit.Address || api.posts != 0 || adapter.submits != 1 {
		t.Fatal(result, adapter.lastTransfer)
	}
	state := readPublicState(t, path)
	if len(state["ops"].([]any)) != 1 {
		t.Fatal("deposit absent from original wallet journal")
	}
	if _, e := c.payment("reconcile", result.ID); e != nil || adapter.submits != 1 {
		t.Fatal("deposit reconcile created another payment", e)
	}
	c, api, adapter, path = fixtureExchangeCoordinator(t)
	state = readPublicState(t, path)
	state["accounts"] = []model.Account{{ID: "exchange-account", Family: "ton", Network: "ton-mainnet", Address: "EQpublicSyntheticOwnedTON"}}
	writePublicState(t, path, state)
	adapter.networks = []model.Network{{ID: "ton-mainnet", Family: "ton", Assets: []model.Asset{{ID: "ton-mainnet:TON", Symbol: "TON", Decimals: 9}}}}
	api.chain.Chain = "service-ton"
	api.chain.ChainType = "TON"
	api.chain.MinAccuracy = "9"
	api.deposit = bybit.Deposit{Chain: "service-ton", ChainType: "TON", Address: "EQpublicSyntheticBybitTON", Tag: "1234567"}
	result = exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"assetId": "ton-mainnet:TON", "amount": "1.123456789"})
	if result.State != "CONFIRMED" || adapter.lastTransfer.Memo != "1234567" || adapter.lastTransfer.Amount != "1123456789" || api.posts != 0 {
		t.Fatal("TON deposit memo/atomic amount was lost", result, adapter.lastTransfer)
	}
}

func TestExchangeExactArithmeticAndValidationRejectsUnspendableQuantities(t *testing.T) {
	q := exchangeQuote{Coin: "ETH", Side: "Sell", BasePrecision: "0.000001", QuotePrecision: "0.01", MinOrderAmount: "5", MaxOrderQty: "100", BidPrice: "1999", LastPrice: "2000", FeeRate: "0.001"}
	if e := validateExchangeOrder(q, "0.1", "0.5"); e != nil {
		t.Fatal(e)
	}
	for _, row := range []struct{ value, slip string }{{"0", "0.5"}, {"-1", "0.5"}, {"1e5", "0.5"}, {"0.0000001", "0.5"}, {"0.001", "0.5"}, {"101", "0.5"}, {"0.1", "0"}, {"0.1", "1.01"}, {"0.1", "0.001"}} {
		if validateExchangeOrder(q, row.value, row.slip) == nil {
			t.Fatal("unsafe order admitted", row)
		}
	}
	fee, coin, e := exchangeFeeEstimate(q, "0.1", "Sell")
	if e != nil || fee != "0.2" || coin != "USDT" {
		t.Fatal(fee, coin, e)
	}
	fee, coin, e = exchangeFeeEstimate(q, "200", "Buy")
	if e != nil || fee != "0.0001" || coin != "ETH" {
		t.Fatal(fee, coin, e)
	}
	q.Side = "Buy"
	if validateExchangeOrder(q, "200", "1") != nil {
		t.Fatal("valid quote spending")
	}
	for _, v := range []string{"4.99", "200000", "5.001"} {
		if validateExchangeOrder(q, v, "1") == nil {
			t.Fatal("invalid buy", v)
		}
	}
	for _, field := range []string{"base", "quote", "minimum", "maximum", "bid"} {
		bad := q
		bad.Side = "Sell"
		switch field {
		case "base":
			bad.BasePrecision = "bad"
		case "quote":
			bad.Side = "Buy"
			bad.QuotePrecision = "0"
		case "minimum":
			bad.MinOrderAmount = "bad"
		case "maximum":
			bad.MaxOrderQty = "0"
		case "bid":
			bad.BidPrice = "0"
		}
		if validateExchangeOrder(bad, "0.1", "0.5") == nil {
			t.Fatal("bad metadata admitted", field)
		}
	}
	for _, field := range []string{"qty", "rate", "price"} {
		bad := q
		v := "1"
		switch field {
		case "qty":
			v = "bad"
		case "rate":
			bad.FeeRate = "1"
		case "price":
			bad.LastPrice = "0"
		}
		if _, _, e := exchangeFeeEstimate(bad, v, "Sell"); e == nil {
			t.Fatal("bad fee admitted", field)
		}
	}
	var b bybit.Balance
	b.Balance.WalletBalance = "5"
	b.Balance.TransferBalance = "3.000000000000000001"
	b.Balance.TransferSafeAmount = "4"
	if safe, e := exchangeSafeBalance(b); e != nil || safe != "3.000000000000000001" {
		t.Fatal("safe minimum lost", safe, e)
	}
	b.Balance.TransferSafeAmount = ""
	if _, e := exchangeSafeBalance(b); e == nil {
		t.Fatal("unknown safe amount treated as zero")
	}
	zero, decimalErr := exchangeDecimal(new(big.Int))
	if decimalErr != nil || zero != "0" || sameExchangeAmount("bad", "bad") || !sameExchangeAmount("0.100000", "0.1") || exchangeAtLeast("bad", "1") {
		t.Fatal("incorrect exact amount comparison")
	}
}

func TestExchangeStatusSchemasPermissionsAndSessionAuthFailClosed(t *testing.T) {
	c, api, _, path := fixtureExchangeCoordinator(t)
	out, e := c.handle(message("exchangeStatus", struct{}{}))
	if e != nil {
		t.Fatal(e)
	}
	status := out.(exchangePublicStatus)
	if !status.Connected || !status.Configured || status.KeySuffix != "1234" || status.Environment != "mainnet" {
		t.Fatal(status)
	}
	api.info.Permissions.Spot = nil
	api.info.IsMaster = false
	out, e = c.handle(message("exchangeStatus", struct{}{}))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.(exchangePublicStatus).Message, "lacks spot") || !strings.Contains(out.(exchangePublicStatus).Message, "master-account") {
		t.Fatal(out)
	}
	api.fail["key"] = errors.New("synthetic unavailable")
	out, e = c.handle(message("exchangeStatus", struct{}{}))
	if e != nil || out.(exchangePublicStatus).Connected || !out.(exchangePublicStatus).Configured {
		t.Fatal("unverified credentials claimed connected", out, e)
	}
	delete(api.fail, "key")
	if _, e = c.handle(message("exchangeConnect", map[string]string{"endpoint": "https://api.bybit.com"})); e != nil {
		t.Fatal(e)
	}
	out, e = c.handle(message("exchangeDisconnect", struct{}{}))
	if e != nil || out.(exchangePublicStatus).Connected || out.(exchangePublicStatus).Configured {
		t.Fatal(out, e)
	}
	state := readPublicState(t, path)
	state["exchangeConnected"] = true
	writePublicState(t, path, state)
	client, _, e := c.exchangeClientAt(0)
	if e != nil {
		t.Fatal(e)
	}
	client.Close()
	request := bybit.AuthRequest{Endpoint: "https://api.bybit.com", Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	if _, e = api.authorize(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	bad := request
	bad.Endpoint = "https://api.bybit.ae"
	if _, e = api.authorize(context.Background(), bad); e == nil {
		t.Fatal("credential endpoint changed")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = api.authorize(canceled, request); e == nil {
		t.Fatal("canceled request reached signer")
	}
	state = readPublicState(t, path)
	state["reject"] = []string{"exchangeAuth"}
	writePublicState(t, path, state)
	if _, e = api.authorize(context.Background(), request); e == nil {
		t.Fatal("rejected signer auth accepted")
	}
	c.mu.Lock()
	c.epoch++
	c.mu.Unlock()
	if _, e = api.authorize(context.Background(), request); e == nil {
		t.Fatal("stale auth accepted")
	}
	for _, method := range []string{"exchangeStatus", "exchangeDisconnect", "exchangeConnect", "exchangeQuote", "exchangeDeposit", "exchangeSwap", "exchangeWithdraw", "exchangeReconcile"} {
		t.Run("schema-"+method, func(t *testing.T) {
			c, _, _, _ := fixtureExchangeCoordinator(t)
			for _, body := range []any{map[string]string{"password": "ordinary-renderer-secret"}, []string{}, nil} {
				if _, e := c.handle(message(method, body)); e == nil {
					t.Fatal("invalid schema reached exchange", method, body)
				}
			}
		})
	}
	for _, scenario := range []string{"testnet", "locked", "recovery", "disconnected", "badEndpoint", "snapshotFailure", "statusFailure", "factory"} {
		t.Run(scenario, func(t *testing.T) {
			c, _, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			switch scenario {
			case "testnet":
				c.child.Environment = "testnet"
			case "locked":
				state["locked"] = true
			case "recovery":
				state["recoveryConfirmed"] = false
			case "disconnected":
				state["exchangeConnected"] = false
			case "badEndpoint":
				state["exchangeEndpoint"] = "https://evil.invalid"
			case "snapshotFailure":
				state["reject"] = []string{"snapshot"}
			case "statusFailure":
				state["reject"] = []string{"exchangeStatus"}
			case "factory":
				c.newExchange = func(string, bybit.Authorizer) (exchangeAPI, error) {
					return nil, errors.New("synthetic factory failure")
				}
			}
			writePublicState(t, path, state)
			if _, _, e := c.exchangeClientAt(0); e == nil {
				t.Fatal("unavailable signer reached exchange")
			}
			if scenario == "testnet" {
				out, e := c.handle(message("exchangeStatus", struct{}{}))
				if e != nil || !strings.Contains(out.(exchangePublicStatus).Message, "testnet") {
					t.Fatal(out, e)
				}
			}
		})
	}
	for _, method := range []string{"exchangeConnect", "exchangeDisconnect"} {
		t.Run("rejected-"+method, func(t *testing.T) {
			c, _, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			state["reject"] = []string{method}
			writePublicState(t, path, state)
			body := any(struct{}{})
			if method == "exchangeConnect" {
				body = map[string]string{"endpoint": "https://api.bybit.com"}
			}
			if _, e := c.handle(message(method, body)); e == nil {
				t.Fatal("signer rejection ignored")
			}
		})
	}
	c, _, _, _ = fixtureExchangeCoordinator(t)
	if _, e := c.exchangeAt(message("unknown", struct{}{}), 0); e == nil {
		t.Fatal("generic exchange method")
	}
}

func TestExchangeQuoteProviderFailuresNeverBecomeZeroOrSupportedRoutes(t *testing.T) {
	for _, stage := range []string{"key", "coin", "balance", "instrument", "ticker", "fee"} {
		t.Run(stage, func(t *testing.T) {
			c, api, _, _ := fixtureExchangeCoordinator(t)
			api.fail[stage] = errors.New("synthetic unknown")
			if _, e := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Sell"})); e == nil {
				t.Fatal("provider failure became quote", stage)
			}
		})
	}
	for _, scenario := range []string{"side", "account", "asset", "foreignNetwork", "wrongBalance", "badSafe", "wrongInstrument", "wrongTicker", "wrongFee", "absentListing", "explicitAbsent"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			body := map[string]any{"side": "Sell"}
			switch scenario {
			case "side":
				body["side"] = "unknown"
			case "account":
				body["accountId"] = "missing"
			case "asset":
				body["assetId"] = "ethereum-mainnet:unknown"
			case "foreignNetwork":
				state := readPublicState(t, path)
				state["accounts"] = []model.Account{{ID: "exchange-account", Network: "unknown", Address: "address"}}
				writePublicState(t, path, state)
			case "wrongBalance":
				api.balanceOverride = &bybit.Balance{}
			case "badSafe":
				api.balances["FUND:ETH"] = "not-an-amount"
			case "wrongInstrument":
				api.instrument = &bybit.Instrument{Symbol: "TONUSDT"}
			case "wrongTicker":
				api.ticker = &bybit.Ticker{Symbol: "TONUSDT"}
			case "wrongFee":
				api.fee = &bybit.FeeRate{Symbol: "TONUSDT"}
			case "absentListing":
				api.fail["instrument"] = &bybit.APIError{Code: 10001}
			case "explicitAbsent":
				api.fail["instrument"] = bybit.ErrNotListed
			}
			_ = adapter
			out, e := c.handle(exchangeMessage("exchangeQuote", body))
			if scenario == "absentListing" || scenario == "explicitAbsent" {
				if e != nil || out.(exchangeQuote).Symbol != "" {
					t.Fatal("explicit missing pair prevented route observation", out, e)
				}
			} else if e == nil {
				t.Fatal("malformed quote accepted", out)
			}
		})
	}
}

func TestExchangeFundsAndOrderPolicyFailuresMakeNoWrites(t *testing.T) {
	for _, scenario := range []string{"noSpotPermission", "precision", "slippage", "belowMinimum", "aboveMaximum", "noTransferPermission", "insufficientSource", "balanceUnavailable"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, _ := fixtureExchangeCoordinator(t)
			body := map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"}
			switch scenario {
			case "noSpotPermission":
				api.info.Permissions.Spot = nil
			case "precision":
				body["amount"] = "0.1000001"
			case "slippage":
				body["slippage"] = "1.01"
			case "belowMinimum":
				body["amount"] = "0.000001"
			case "aboveMaximum":
				body["amount"] = "101"
			case "noTransferPermission":
				api.balances["UNIFIED:ETH"] = "0"
				api.info.Permissions.Wallet = nil
			case "insufficientSource":
				api.balances["UNIFIED:ETH"] = "0"
				api.balances["FUND:ETH"] = "0.01"
			case "balanceUnavailable":
				api.fail["balance"] = errors.New("synthetic unavailable")
			}
			if _, e := c.handle(exchangeMessage("exchangeSwap", body)); e == nil || api.posts != 0 {
				t.Fatal("unsafe swap wrote exchange", e, api.posts)
			}
		})
	}
	if exchangeAvailable(nil, "FUND", "ETH") != "" {
		t.Fatal("missing balance invented")
	}
	q := exchangeQuote{Side: "Buy", BasePrecision: "0.01", QuotePrecision: "0.01", MinOrderAmount: "5", MaxOrderQty: "100", BidPrice: "0"}
	if validateExchangeOrder(q, "10", "1") == nil {
		t.Fatal("bad buy bid")
	}
	tiny, decimalErr := exchangeDecimal(big.NewInt(1))
	if decimalErr != nil || tiny != "0.000000000000000000000000000000000001" {
		t.Fatal("fractional precision lost")
	}
}

func TestExchangeDepositValidationAndSignerFailuresKeepWalletUnspent(t *testing.T) {
	for _, scenario := range []string{"suspended", "missingRoute", "belowMinimum", "zero", "excessPrecision", "wrongChain", "foreignMemo", "missingTONMemo", "badAddress", "provider", "prepare", "signerPrepare", "signerSign", "unknownSubmission"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, adapter, path := fixtureExchangeCoordinator(t)
			body := map[string]any{"amount": "0.1"}
			switch scenario {
			case "suspended":
				api.chain.ChainDeposit = "0"
			case "missingRoute":
				api.chain.ChainType = "Base"
			case "belowMinimum":
				body["amount"] = "0.0001"
			case "zero":
				body["amount"] = "0"
			case "excessPrecision":
				body["amount"] = "0.1000000000000000001"
			case "wrongChain":
				api.deposit.Chain = "BASE"
			case "foreignMemo":
				api.deposit.Tag = "secret-account-tag"
			case "missingTONMemo":
				state := readPublicState(t, path)
				state["accounts"] = []model.Account{{ID: "exchange-account", Family: "ton", Network: "ethereum-mainnet", Address: "address"}}
				writePublicState(t, path, state)
			case "badAddress": // publicAdapter has no address parser; chain substitution independently rejects this destination.
				api.deposit.ChainType = "unknown"
			case "provider":
				api.fail["deposit"] = errors.New("synthetic unavailable")
			case "prepare":
				adapter.prepareError = true
			case "signerPrepare":
				state := readPublicState(t, path)
				state["reject"] = []string{"prepare"}
				writePublicState(t, path, state)
			case "signerSign":
				state := readPublicState(t, path)
				state["reject"] = []string{"sign"}
				writePublicState(t, path, state)
			case "unknownSubmission":
				adapter.submitError = true
			}
			out, e := c.handle(exchangeMessage("exchangeDeposit", body))
			if scenario == "unknownSubmission" {
				if e != nil || out.(exchangeResult).State != "UNKNOWN" || adapter.submits != 1 {
					t.Fatal("accepted-then-timeout deposit lost", out, e)
				}
			} else if e == nil || adapter.submits != 0 {
				t.Fatal("invalid deposit submitted", scenario, out, e, adapter.submits)
			}
		})
	}
}

func fixtureRemoteOperation(kind string, payload any) vault.ExchangeOperation {
	b, _ := json.Marshal(payload)
	return vault.ExchangeOperation{ID: "00000000000000000000000000000001", Kind: kind, Endpoint: "https://api.bybit.com", CredentialID: "fixture-credential", AccountID: "exchange-account", AssetID: "ethereum-mainnet:native", Payload: string(b), State: "UNKNOWN", CreatedAt: 1700000000, ExpiresAt: 1700000300}
}
func storeFixtureOperation(t *testing.T, path string, op vault.ExchangeOperation) {
	t.Helper()
	state := readPublicState(t, path)
	state["exchangeOps"] = []vault.ExchangeOperation{op}
	writePublicState(t, path, state)
}
func fixtureRemoteOrder() bybit.SpotOrder {
	return bybit.SpotOrder{Category: "spot", Symbol: "ETHUSDT", Side: "Sell", OrderType: "Market", Qty: "0.1", MarketUnit: "baseCoin", IsLeverage: 0, SlippageToleranceType: "Percent", SlippageTolerance: "0.5", OrderLinkID: "00000000000000000000000000000001"}
}
func fixtureOrderRecord(request bybit.SpotOrder) bybit.Order {
	return bybit.Order{OrderID: "remote-order-1", OrderLinkID: request.OrderLinkID, Symbol: request.Symbol, Side: request.Side, OrderType: request.OrderType, Qty: request.Qty, MarketUnit: request.MarketUnit, IsLeverage: "0", OrderStatus: "Filled", CumExecQty: "0.1", CumExecValue: "200"}
}
func fixtureExecution(request bybit.SpotOrder) bybit.Execution {
	return bybit.Execution{OrderID: "remote-order-1", OrderLinkID: request.OrderLinkID, Symbol: request.Symbol, Side: request.Side, OrderType: "Market", ExecID: "fill-1", ExecQty: "0.1", ExecValue: "200", ExecPrice: "2000", ExecFee: "0.2", FeeCurrency: "USDT", ExtraFees: json.RawMessage(`""`)}
}

func TestExchangeExecutionPagesRequireExactAmountsFeesAndNoMissingOrDuplicateFills(t *testing.T) {
	request := fixtureRemoteOrder()
	order := fixtureOrderRecord(request)
	fill := fixtureExecution(request)
	for _, scenario := range []string{"one", "paged", "failure", "category", "foreignOrder", "foreignLink", "foreignSymbol", "foreignSide", "foreignType", "duplicate", "emptyID", "qty", "value", "fee", "price", "feeCoin", "sumQty", "sumValue", "overspend", "underfill", "buyOverspend", "loop", "emptyContinuation", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			a := newFixtureExchangeAPI()
			req, ord := request, order
			calls := 0
			a.executions = func(_ string, _ string, cursor string) (bybit.ExecutionPage, error) {
				calls++
				f := fill
				page := bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{f}}
				switch scenario {
				case "failure":
					return page, errors.New("synthetic unavailable")
				case "category":
					page.Category = "linear"
				case "foreignOrder":
					f.OrderID = "different"
				case "foreignLink":
					f.OrderLinkID = "different"
				case "foreignSymbol":
					f.Symbol = "TONUSDT"
				case "foreignSide":
					f.Side = "Buy"
				case "foreignType":
					f.OrderType = "Limit"
				case "duplicate":
					page.List = append(page.List, f)
					return page, nil
				case "emptyID":
					f.ExecID = ""
				case "qty":
					f.ExecQty = "-1"
				case "value":
					f.ExecValue = "0"
				case "fee":
					f.ExecFee = "bad"
				case "price":
					f.ExecPrice = "0"
				case "feeCoin":
					f.FeeCurrency = "BTC"
				case "overspend":
					f.ExecQty = "0.2"
				case "underfill":
					f.ExecQty = "0.05"
				case "buyOverspend":
					f.Side = "Buy"
					f.ExecValue = "201"
				case "paged":
					f.ExecQty = "0.05"
					f.ExecValue = "100"
					f.ExecFee = "0.1"
					f.ExecID = "fill-" + string(rune('0'+calls))
					if cursor == "" {
						page.NextPageCursor = "page2"
					}
				case "loop":
					f.ExecID = "fill-" + string(rune('0'+calls))
					page.NextPageCursor = "loop"
				case "emptyContinuation":
					page.List = []bybit.Execution{}
					page.NextPageCursor = "next"
					return page, nil
				case "budget":
					f.ExecID = "fill-" + string(rune('0'+calls))
					page.NextPageCursor = "page-" + string(rune('0'+calls))
				}
				page.List = []bybit.Execution{f}
				return page, nil
			}
			switch scenario {
			case "sumQty":
				ord.CumExecQty = "0.2"
			case "sumValue":
				ord.CumExecValue = "201"
			case "overspend":
				ord.CumExecQty = "0.2"
			case "underfill":
				ord.CumExecQty = "0.05"
			case "buyOverspend":
				req.Side = "Buy"
				req.Qty = "200"
				req.MarketUnit = "quoteCoin"
				ord.Side = "Buy"
				ord.CumExecValue = "201"
			}
			base, quote, fees, e := verifiedExchangeExecutions(context.Background(), a, req, ord)
			if scenario == "one" || scenario == "paged" {
				if e != nil || base != "0.1" || quote != "200" || fees["USDT"] != "0.2" {
					t.Fatal(base, quote, fees, e)
				}
			} else if e == nil {
				t.Fatal("incomplete/foreign execution confirmed", scenario, base, quote, fees)
			}
		})
	}
}

func TestExchangePublicRowsNeverExposeCredentialsApprovalPayloadOrPrivateMetadata(t *testing.T) {
	order := fixtureRemoteOrder()
	transfer := bybit.TransferRequest{Coin: "USDT", Amount: "200", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
	withdraw := bybit.WithdrawalRequest{Coin: "ETH", Amount: "0.1", Address: "owned-wallet"}
	rows := []vault.ExchangeOperation{fixtureRemoteOperation("swap", order), fixtureRemoteOperation("transfer", transfer), fixtureRemoteOperation("withdraw", withdraw)}
	rows[2].State = "CONFIRMED"
	for i := range rows {
		rows[i].CredentialID = "SECRET_CANARY_CREDENTIAL"
		rows[i].Digest = "PRIVATE_CANARY_DIGEST"
		rows[i].Endpoint = "PRIVATE_CANARY_ENDPOINT"
		rows[i].Fee = "PRIVATE_CANARY_FEE"
	}
	for _, kind := range []string{"swap", "transfer", "withdraw", "unknown"} {
		bad := fixtureRemoteOperation(kind, nil)
		bad.Payload = "malformed"
		rows = append(rows, bad)
	}
	out := exchangePublicOperations(rows)
	if len(out) != 3 || out[0].Coin != "ETH" || out[0].Symbol != "ETHUSDT" || out[1].Coin != "USDT" || out[2].State != "EXCHANGE_COMPLETED" || out[0].CreatedAt != "1700000000" {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "CANARY") || strings.Contains(string(b), "payload") || strings.Contains(string(b), "credential") {
		t.Fatal("private metadata leaked", string(b))
	}
}

func TestExchangeOrderReconciliationHandlesPartialCancelledMissingAndForeignResults(t *testing.T) {
	for _, scenario := range []string{"pending", "partialPending", "filled", "cancelledEmpty", "cancelledPartial", "unknownStatus", "missing", "provider", "foreign", "executionFailure", "badPayload", "badState", "terminal", "persistFailure", "credentialChanged", "endpointChanged", "lookupFailure"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			request := fixtureRemoteOrder()
			api.order = request
			record := fixtureOrderRecord(request)
			api.orderResult = &record
			op := fixtureRemoteOperation("swap", request)
			op.ExternalID = "remote-order-1"
			want := "CONFIRMED"
			wantError := false
			switch scenario {
			case "pending":
				record.OrderStatus = "New"
				want = "PENDING"
			case "partialPending":
				record.OrderStatus = "PartiallyFilled"
				want = "PENDING"
			case "cancelledEmpty":
				record.OrderStatus = "Cancelled"
				record.CumExecQty = "0"
				record.CumExecValue = "0"
				api.executions = func(string, string, string) (bybit.ExecutionPage, error) {
					return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{}}, nil
				}
				want = "FAILED"
			case "cancelledPartial":
				record.OrderStatus = "PartiallyFilledCanceled"
				record.CumExecQty = "0.05"
				record.CumExecValue = "100"
				api.executions = func(string, string, string) (bybit.ExecutionPage, error) {
					fill := fixtureExecution(request)
					fill.ExecQty = "0.05"
					fill.ExecValue = "100"
					return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{fill}}, nil
				}
			case "unknownStatus":
				record.OrderStatus = "Unrecognized"
				want = "UNKNOWN"
			case "missing":
				api.orderResult = nil
				api.order = bybit.SpotOrder{}
				want = "UNKNOWN"
			case "provider":
				api.fail["find"] = errors.New("unavailable")
				want = "UNKNOWN"
			case "foreign":
				record.OrderID = "different"
				wantError = true
			case "executionFailure":
				api.fail["executions"] = errors.New("unavailable")
				want = "UNKNOWN"
			case "badPayload":
				op.Payload = "null"
				wantError = true
			case "badState":
				op.State = "AUTHORIZED"
				wantError = true
			case "terminal":
				op.State = "FAILED"
				want = "FAILED"
			case "persistFailure":
				state := readPublicState(t, path)
				state["reject"] = []string{"exchangeUpdate"}
				writePublicState(t, path, state)
				wantError = true
			case "credentialChanged":
				op.CredentialID = "different"
				wantError = true
			case "endpointChanged":
				op.Endpoint = "https://api.bybit.ae"
				wantError = true
			case "lookupFailure":
				state := readPublicState(t, path)
				state["reject"] = []string{"exchangeOperation"}
				writePublicState(t, path, state)
				wantError = true
			}
			storeFixtureOperation(t, path, op)
			out, e := c.handle(message("exchangeReconcile", map[string]string{"id": op.ID}))
			if wantError {
				if e == nil {
					t.Fatal("invalid reconciliation accepted", out)
				}
				return
			}
			if e != nil || out.(exchangeResult).State != want || api.posts != 0 {
				t.Fatal(out, e, want, api.posts)
			}
		})
	}
}

func TestExchangeTransferReconciliationBindsExactOperationAndPreservesUncertainty(t *testing.T) {
	request := bybit.TransferRequest{TransferID: "00000000-0000-0000-0000-000000000001", Coin: "ETH", Amount: "0.1", FromAccountType: "FUND", ToAccountType: "UNIFIED"}
	for _, scenario := range []string{"success", "failed", "pending", "unknown", "foreign", "missing", "provider", "badPayload"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			record := bybit.TransferRecord{TransferID: request.TransferID, Coin: request.Coin, Amount: request.Amount, FromAccountType: request.FromAccountType, ToAccountType: request.ToAccountType, Status: "SUCCESS"}
			api.transferRecord = &record
			op := fixtureRemoteOperation("transfer", request)
			want := "CONFIRMED"
			bad := false
			switch scenario {
			case "failed":
				record.Status = "FAILED"
				want = "FAILED"
			case "pending":
				record.Status = "PENDING"
				want = "PENDING"
			case "unknown":
				record.Status = "STATUS_UNKNOWN"
				want = "UNKNOWN"
			case "foreign":
				record.Amount = "0.2"
				bad = true
			case "missing":
				api.transferRecord = nil
				want = "UNKNOWN"
			case "provider":
				api.fail["transferstatus"] = errors.New("unavailable")
				want = "UNKNOWN"
			case "badPayload":
				op.Payload = "[]"
				bad = true
			}
			storeFixtureOperation(t, path, op)
			out, e := c.reconcileExchangeAt(0, api, op)
			if bad {
				if e == nil {
					t.Fatal("foreign transfer confirmed", out)
				}
				return
			}
			if e != nil || out.State != want || api.posts != 0 {
				t.Fatal(out, e, want)
			}
		})
	}
}

func TestExchangeWithdrawalReconciliationUsesKnownIDAndNeverClaimsChainFinality(t *testing.T) {
	request := bybit.WithdrawalRequest{Coin: "ETH", Chain: "ETH", Address: "0x1111111111111111111111111111111111111111", Amount: "0.1", ForceChain: 1, AccountType: "FUND", FeeType: 1, RequestID: "00000000000000000000000000000001"}
	for _, scenario := range []string{"success", "successConfirmed", "SecurityCheck", "Pending", "BlockchainConfirmed", "MoreInformationRequired", "Unknown", "CancelByUser", "Reject", "Fail", "futureStatus", "foreign", "badAmount", "badFee", "missing", "cursor", "provider", "badPayload", "terminal", "unknownKind"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			op := fixtureRemoteOperation("withdraw", request)
			op.ExternalID = "withdrawal-1"
			record := bybit.WithdrawalRecord{WithdrawID: op.ExternalID, Coin: request.Coin, Chain: request.Chain, ToAddress: request.Address, Amount: "0.1", WithdrawFee: "0.0002", Status: scenario, TxID: "onchain-hash"}
			api.withdrawalPage = &bybit.WithdrawalPage{Rows: []bybit.WithdrawalRecord{record}}
			want := "UNKNOWN"
			bad := false
			switch scenario {
			case "success", "successConfirmed":
				want = "EXCHANGE_COMPLETED"
			case "SecurityCheck", "Pending", "BlockchainConfirmed", "MoreInformationRequired", "Unknown":
				want = "PENDING"
			case "CancelByUser", "Reject", "Fail":
				want = "FAILED"
			case "foreign":
				api.withdrawalPage.Rows[0].ToAddress = "attacker"
				bad = true
			case "badAmount":
				api.withdrawalPage.Rows[0].Amount = "bad"
				bad = true
			case "badFee":
				api.withdrawalPage.Rows[0].WithdrawFee = "bad"
				bad = true
			case "missing":
				api.withdrawalPage.Rows = []bybit.WithdrawalRecord{}
			case "cursor":
				api.withdrawalPage.NextPageCursor = "incomplete"
			case "provider":
				api.fail["withdrawstatus"] = errors.New("unavailable")
			case "badPayload":
				op.Payload = "[]"
				bad = true
			case "terminal":
				op.State = "CONFIRMED"
				want = "EXCHANGE_COMPLETED"
			case "unknownKind":
				op.Kind = "unknown"
				bad = true
			}
			storeFixtureOperation(t, path, op)
			out, e := c.reconcileExchangeAt(0, api, op)
			if bad {
				if e == nil {
					t.Fatal("invalid withdrawal accepted", out)
				}
				return
			}
			if e != nil || out.State != want || api.posts != 0 {
				t.Fatal(out, e, want)
			}
			if out.State == "EXCHANGE_COMPLETED" && !strings.Contains(out.Message, "not") && !strings.Contains(out.Message, "does not") {
				t.Fatal("exchange report presented as chain finality", out)
			}
		})
	}
}

func TestExchangeWithdrawalFeesPrecisionAndFundingRules(t *testing.T) {
	for _, row := range []struct {
		value, fixed, rate string
		precision          uint8
		want               string
	}{{"10", "1", "0", 8, "1"}, {"10", "1", "0.1", 8, "1.9"}, {"1", "0.001", "0.002", 3, "0.003"}, {"0.1", "0.0002", "0", 18, "0.0002"}} {
		got, e := exchangeWithdrawalFee(row.value, row.fixed, row.rate, row.precision)
		if e != nil || got != row.want {
			t.Fatal(row, got, e)
		}
	}
	for _, row := range []struct {
		value, fixed, rate string
		precision          uint8
	}{{"bad", "1", "0", 8}, {"1", "bad", "0", 8}, {"1", "0", "bad", 8}, {"1", "1", "0", 8}, {"1", "0", "1", 8}, {"1", "0", "0", 37}, {"0.1", "0.01", "0.9", 0}} {
		if _, e := exchangeWithdrawalFee(row.value, row.fixed, row.rate, row.precision); e == nil {
			t.Fatal("unsafe fee accepted", row)
		}
	}
	for _, scenario := range []string{"key", "withdrawPermission", "master", "suspended", "unknownRoute", "noFee", "minimum", "precision", "zero", "overMaximum", "feeTooLarge", "rateTooLarge", "needFunding", "pendingFunding", "noFunding"} {
		t.Run(scenario, func(t *testing.T) {
			c, api, _, _ := fixtureExchangeCoordinator(t)
			body := map[string]any{"amount": "0.1"}
			wantError := true
			switch scenario {
			case "key":
				api.fail["key"] = errors.New("unavailable")
			case "withdrawPermission":
				api.info.Permissions.Wallet = nil
			case "master":
				api.info.IsMaster = false
			case "suspended":
				api.chain.ChainWithdraw = "0"
			case "unknownRoute":
				api.chain.ChainType = "Base"
			case "noFee":
				api.chain.WithdrawFee = ""
			case "minimum":
				body["amount"] = "0.0001"
			case "precision":
				api.chain.MinAccuracy = "6"
				body["amount"] = "0.1000001"
			case "zero":
				body["amount"] = "0"
			case "overMaximum":
				body["amount"] = "11"
			case "feeTooLarge":
				api.chain.WithdrawFee = "0.1"
			case "rateTooLarge":
				api.chain.WithdrawPercentageFee = "1"
			case "needFunding":
				api.balances["FUND:ETH"] = "0.04"
				wantError = false
			case "pendingFunding":
				api.balances["FUND:ETH"] = "0"
				api.transferRecord = &bybit.TransferRecord{TransferID: "00000000-0000-0000-0000-000000000001", Coin: "ETH", Amount: "0.1", FromAccountType: "UNIFIED", ToAccountType: "FUND", Status: "PENDING"}
				wantError = false
			case "noFunding":
				api.balances["FUND:ETH"] = "0"
				api.balances["UNIFIED:ETH"] = "0"
			}
			out, e := c.handle(exchangeMessage("exchangeWithdraw", body))
			if wantError {
				if e == nil || api.posts != 0 {
					t.Fatal("invalid withdrawal sent", scenario, out, e, api.posts)
				}
			} else if e != nil {
				t.Fatal(out, e)
			}
		})
	}
	c, api, _, _ := fixtureExchangeCoordinator(t)
	api.chain.MinAccuracy = "36"
	if _, e := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1"})); e != nil {
		t.Fatal("wallet precision bound not applied", e)
	}
}

type exchangeJournalAdapter struct {
	*publicAdapter
	expireUnset  bool
	providedID   string
	addressError bool
}

func (a *exchangeJournalAdapter) ValidateAddress(string, string) error {
	if a.addressError {
		return errors.New("synthetic wrong destination")
	}
	return nil
}
func (a *exchangeJournalAdapter) Prepare(ctx context.Context, account model.Account, transfer model.Transfer) (model.Plan, error) {
	plan, e := a.publicAdapter.Prepare(ctx, account, transfer)
	if a.expireUnset {
		plan.Expires = 0
	}
	if a.providedID != "" {
		plan.ID = a.providedID
	}
	return plan, e
}

func TestExchangeTokenDepositUsesFullRegistryAndMatchingAddressSuffix(t *testing.T) {
	for _, contract := range []string{"31ec7", "D831ec7", "D831ec", "0xdAC17F958D2ee523a2206206994597C13D831ec7", "831ec7"} {
		t.Run(contract, func(t *testing.T) {
			c, api, adapter, _ := fixtureExchangeCoordinator(t)
			api.deposit.ContractAddress = contract
			out, e := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"assetId": "ethereum-mainnet:usdt", "amount": "12.345678"}))
			valid := contract == "0xdAC17F958D2ee523a2206206994597C13D831ec7" || contract == "831ec7"
			if valid {
				if e != nil || out.(exchangeResult).State != "CONFIRMED" || adapter.lastTransfer.Amount != "12345678" || adapter.lastTransfer.Asset != "ethereum-mainnet:usdt" {
					t.Fatal("registered USDT deposit failed", out, e, adapter.lastTransfer)
				}
			} else if e == nil || adapter.submits != 0 {
				t.Fatal("wrong suffix accepted", out, e)
			}
		})
	}
	c, api, adapter, _ := fixtureExchangeCoordinator(t)
	wrapper := &exchangeJournalAdapter{publicAdapter: adapter, expireUnset: true, providedID: "original-wallet-deposit"}
	c.adapters = []model.Adapter{wrapper}
	result := exchangeResultCall(t, c, "exchangeDeposit", map[string]any{"amount": "0.1"})
	if result.ID != "original-wallet-deposit" {
		t.Fatal("original prepared identity replaced")
	}
	if _, e := c.handle(message("exchangeReconcile", map[string]string{"id": result.ID})); e != nil || adapter.submits != 1 || api.posts != 0 {
		t.Fatal("deposit reconciliation failed", e)
	}
	c, api, adapter, _ = fixtureExchangeCoordinator(t)
	c.adapters = []model.Adapter{&exchangeJournalAdapter{publicAdapter: adapter, addressError: true}}
	if _, e := c.handle(exchangeMessage("exchangeDeposit", map[string]any{"amount": "0.1"})); e == nil || adapter.submits != 0 || api.posts != 0 {
		t.Fatal("invalid address reached wallet journal")
	}
}

func TestExchangeDefinitiveRejectionAndDurabilityFailures(t *testing.T) {
	for _, code := range []int64{10001, 10003, 10004, 10005} {
		t.Run("definitive-"+big.NewInt(code).String(), func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			api.fail["/v5/order/create"] = &bybit.APIError{Code: code}
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
			if result.State != "FAILED" || result.Message == "" || api.posts != 1 {
				t.Fatal(result)
			}
			state := readPublicState(t, path)
			if state["exchangeOps"].([]any)[0].(map[string]any)["state"] != "FAILED" {
				t.Fatal("definitive rejection not durable")
			}
		})
	}
	for _, code := range []int64{10000, 10014, 10016, 170141, 110072} {
		t.Run("ambiguous-"+big.NewInt(code).String(), func(t *testing.T) {
			c, api, _, _ := fixtureExchangeCoordinator(t)
			api.fail["/v5/order/create"] = &bybit.APIError{Code: code}
			result := exchangeResultCall(t, c, "exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})
			if result.State != "UNKNOWN" || api.posts != 1 {
				t.Fatal("ambiguous/duplicate result discarded", result)
			}
		})
	}
	for _, stage := range []string{"exchangePrepare", "exchangeAuthorize", "exchangeUpdate", "exchangeReject"} {
		t.Run(stage, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			state["reject"] = []string{stage}
			writePublicState(t, path, state)
			if stage == "exchangeReject" {
				api.fail["/v5/order/create"] = &bybit.APIError{Code: 10005}
			}
			if _, e := c.handle(exchangeMessage("exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})); e == nil {
				t.Fatal("durability/authorization failure ignored")
			}
			if (stage == "exchangePrepare" || stage == "exchangeAuthorize") && api.posts != 0 {
				t.Fatal("write happened before durable authorization")
			}
		})
	}
	for _, kind := range []string{"swap", "transfer", "withdraw", "unknown"} {
		t.Run("bad-private-"+kind, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			if kind == "unknown" {
				state["authorizationKindOverride"] = "unsupported"
			} else {
				state["authorizationPayloadOverride"] = "[]"
			}
			writePublicState(t, path, state)
			intent := exchangeIntent{Kind: "swap", AccountID: "exchange-account", AssetID: "ethereum-mainnet:native", Order: &bybit.SpotOrder{}}
			switch kind {
			case "transfer":
				intent.Kind = "transfer"
				intent.Order = nil
				intent.Transfer = &bybit.TransferRequest{}
			case "withdraw":
				intent.Kind = "withdraw"
				intent.Order = nil
				intent.Withdrawal = &bybit.WithdrawalRequest{}
			}
			if _, e := c.executeExchangeAt(0, api, intent); e == nil || api.posts != 0 {
				t.Fatal("malformed private operation sent", e)
			}
		})
	}
}

func TestExchangeAdditionalSessionAndFundingFailureBoundaries(t *testing.T) {
	c, api, adapter, path := fixtureExchangeCoordinator(t)
	adapter.networks = append([]model.Network{{ID: "irrelevant", Family: "evm", Testnet: true}}, adapter.networks...)
	if _, _, _, e := c.exchangeAsset("exchange-account", "ethereum-mainnet:native"); e != nil {
		t.Fatal("unrelated registry entry changed exact lookup", e)
	}
	c.newExchange = nil
	client, _, e := c.exchangeClientAt(0)
	if e != nil {
		t.Fatal(e)
	}
	client.Close() // Constructs production transport, performs no request.
	for _, method := range []string{"exchangeQuote", "exchangeSwap", "exchangeDeposit", "exchangeWithdraw", "exchangeReconcile", "exchangeStatus"} {
		t.Run("unavailable-"+method, func(t *testing.T) {
			c, _, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			state["exchangeEndpoint"] = "https://evil.invalid"
			writePublicState(t, path, state)
			body := map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"}
			var msg ipc.Message
			switch method {
			case "exchangeStatus":
				msg = message(method, struct{}{})
			case "exchangeReconcile":
				msg = message(method, map[string]string{"id": "00000000000000000000000000000001"})
			case "exchangeQuote":
				msg = exchangeMessage(method, map[string]any{"side": "Sell"})
			case "exchangeDeposit", "exchangeWithdraw":
				msg = exchangeMessage(method, map[string]any{"amount": "0.1"})
			default:
				msg = exchangeMessage(method, body)
			}
			if _, e := c.handle(msg); e == nil {
				t.Fatal("untrusted endpoint reached exchange")
			}
		})
	}
	c, api, _, path = fixtureExchangeCoordinator(t)
	if _, _, e := c.exchangeClientAt(0); e != nil {
		t.Fatal(e)
	}
	state := readPublicState(t, path)
	state["delayMethod"] = "exchangeAuth"
	writePublicState(t, path, state)
	reply := make(chan error, 1)
	go func() {
		_, e := api.authorize(context.Background(), bybit.AuthRequest{Endpoint: "https://api.bybit.com", Method: "GET", Path: "/v5/user/query-api", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000})
		reply <- e
	}()
	waitPublicFixture(t, filepath.Join(filepath.Dir(path), "native-ready"))
	c.mu.Lock()
	c.epoch++
	c.mu.Unlock()
	if e := os.WriteFile(filepath.Join(filepath.Dir(path), "native-release"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	if e := <-reply; e == nil {
		t.Fatal("late private authorization escaped changed session")
	}
	for _, stage := range []string{"initial", "refresh", "unavailableAfterSuccess", "prepare", "invalidAmount"} {
		t.Run("funding-"+stage, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			api.balances["UNIFIED:ETH"] = "0"
			switch stage {
			case "initial":
				api.fail["balance"] = errors.New("unavailable")
			case "refresh":
				count := 0
				api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
					count++
					if count > 2 {
						return bybit.Balance{}, errors.New("unavailable after transfer")
					}
					hook := api.balanceHook
					api.balanceHook = nil
					b, e := api.AccountBalance(context.Background(), kind, coin)
					api.balanceHook = hook
					return b, e
				}
			case "unavailableAfterSuccess":
				count := 0
				api.balanceHook = func(kind, coin string) (bybit.Balance, error) {
					count++
					hook := api.balanceHook
					api.balanceHook = nil
					b, e := api.AccountBalance(context.Background(), kind, coin)
					api.balanceHook = hook
					if kind == "UNIFIED" {
						b.Balance.WalletBalance = "0"
						b.Balance.TransferBalance = "0"
						b.Balance.TransferSafeAmount = "0"
					}
					return b, e
				}
			case "prepare":
				state := readPublicState(t, path)
				state["reject"] = []string{"exchangePrepare"}
				writePublicState(t, path, state)
			}
			api.authorize = func(ctx context.Context, req bybit.AuthRequest) (bybit.Auth, error) {
				var out bybit.Auth
				err := c.childCallAt(0, "exchangeAuth", req, &out)
				return out, err
			}
			value := "0.1"
			if stage == "invalidAmount" {
				value = "bad"
			}
			result, e := c.ensureExchangeFunds(0, api, api.info, "exchange-account", "ethereum-mainnet:native", "ETH", value, "UNIFIED")
			if stage == "unavailableAfterSuccess" {
				if e != nil || result == nil || result.Kind != "transfer" || result.State != "CONFIRMED" {
					t.Fatal("stale target balance treated as spendable", result, e)
				}
			} else if e == nil {
				t.Fatal("funding failure ignored", stage, result)
			}
		})
	}
	c, api, _, _ = fixtureExchangeCoordinator(t)
	api.fee = &bybit.FeeRate{Symbol: "ETHUSDT", TakerFeeRate: "1"}
	if _, e := c.handle(exchangeMessage("exchangeSwap", map[string]any{"side": "Sell", "amount": "0.1", "slippage": "0.5"})); e == nil || api.posts != 0 {
		t.Fatal("invalid fee made a trade")
	}
}

func TestExchangeWithdrawalRegionPreflightStopsUnsupportedQuestionnairesBeforeAnyTransfer(t *testing.T) {
	for _, endpoint := range []string{"https://api.bybit.com", "https://api.bytick.com", "https://api.bybit.ae", "https://api.bybit.tr", "https://api.bybit.kz", "https://api.bybit.eu", "https://api.bybit.id"} {
		t.Run(endpoint, func(t *testing.T) {
			c, api, _, path := fixtureExchangeCoordinator(t)
			state := readPublicState(t, path)
			state["exchangeEndpoint"] = endpoint
			writePublicState(t, path, state)
			out, e := c.handle(exchangeMessage("exchangeQuote", map[string]any{"side": "Sell"}))
			if e != nil {
				t.Fatal(e)
			}
			quote := out.(exchangeQuote)
			supported := endpoint == "https://api.bybit.com" || endpoint == "https://api.bytick.com" || endpoint == "https://api.bybit.ae"
			if quote.Route.WithdrawEnabled != supported {
				t.Fatal("incorrect regional withdrawal capability", quote.Route)
			}
			if !supported {
				if quote.Route.Reason == "" {
					t.Fatal("blocked regional requirement hidden")
				}
				api.balances["FUND:ETH"] = "0"
				if _, e := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1"})); e == nil || api.posts != 0 {
					t.Fatal("unsupported regional withdrawal moved funds")
				}
			}
		})
	}
	for _, precision := range []string{"bad", "37"} {
		t.Run("precision-"+precision, func(t *testing.T) {
			c, api, _, _ := fixtureExchangeCoordinator(t)
			api.chain.MinAccuracy = precision
			if _, e := c.handle(exchangeMessage("exchangeWithdraw", map[string]any{"amount": "0.1"})); e == nil || api.posts != 0 {
				t.Fatal("invalid precision silently fell back")
			}
		})
	}
}

func TestExchangeExecutionFeesIncludeKnownTaxesAndRejectAdditionalSpending(t *testing.T) {
	for _, scenario := range []string{"VAT", "GST", "VAT-json-string", "buy-VAT", "inputFee", "buyInputFee", "thirdCoin", "feeExceedsReceived", "taxInputCoin", "taxExceedsReceived", "taxUnknown", "taxNull", "taxMissing"} {
		t.Run(scenario, func(t *testing.T) {
			request := fixtureRemoteOrder()
			order := fixtureOrderRecord(request)
			fill := fixtureExecution(request)
			tax := `[{"feeCoin":"USDT","feeType":"VAT","subFeeType":"ARE_VAT","feeRate":"0.05","fee":"0.01"}]`
			wantFees := "0.21"
			wantCoin := "USDT"
			success := scenario == "VAT" || scenario == "GST" || scenario == "VAT-json-string" || scenario == "buy-VAT"
			switch scenario {
			case "GST":
				tax = strings.ReplaceAll(strings.ReplaceAll(tax, "VAT", "GST"), "ARE_GST", "IND_GST")
			case "VAT-json-string":
				b, _ := json.Marshal(tax)
				tax = string(b)
			case "buy-VAT":
				request.Side = "Buy"
				request.MarketUnit = "quoteCoin"
				request.Qty = "200"
				order.Side = "Buy"
				order.MarketUnit = "quoteCoin"
				order.Qty = "200"
				fill.Side = "Buy"
				fill.ExecFee = "0.0001"
				fill.FeeCurrency = "ETH"
				tax = `[{"feeCoin":"ETH","feeType":"VAT","subFeeType":"ARE_VAT","feeRate":"0.05","fee":"0.000005"}]`
				wantFees = "0.000105"
				wantCoin = "ETH"
			case "inputFee":
				fill.FeeCurrency = "ETH"
				fill.ExecFee = "0.0001"
				tax = `[]`
			case "buyInputFee":
				request.Side = "Buy"
				request.MarketUnit = "quoteCoin"
				request.Qty = "200"
				order.Side = "Buy"
				order.MarketUnit = "quoteCoin"
				order.Qty = "200"
				fill.Side = "Buy"
				tax = `[]`
			case "thirdCoin":
				fill.FeeCurrency = "MNT"
				tax = `[]`
			case "feeExceedsReceived":
				fill.ExecFee = "201"
				tax = `[]`
			case "taxInputCoin":
				tax = strings.Replace(tax, `"feeCoin":"USDT"`, `"feeCoin":"ETH"`, 1)
			case "taxExceedsReceived":
				tax = strings.Replace(tax, `"fee":"0.01"`, `"fee":"201"`, 1)
			case "taxUnknown":
				tax = `[{"feeCoin":"USDT","feeType":"UNKNOWN","subFeeType":"UNKNOWN","feeRate":"0.05","fee":"0.01"}]`
			case "taxNull":
				tax = `null`
			}
			fill.ExtraFees = json.RawMessage(tax)
			if scenario == "taxMissing" {
				fill.ExtraFees = nil
			}
			api := newFixtureExchangeAPI()
			api.executions = func(string, string, string) (bybit.ExecutionPage, error) {
				return bybit.ExecutionPage{Category: "spot", List: []bybit.Execution{fill}}, nil
			}
			base, quote, fees, e := verifiedExchangeExecutions(context.Background(), api, request, order)
			if success {
				if e != nil || base != "0.1" || quote != "200" || fees[wantCoin] != wantFees {
					t.Fatal("documented tax omitted from fee totals", base, quote, fees, e)
				}
			} else if e == nil {
				t.Fatal("unapproved spending or incomplete fee metadata confirmed", scenario, fees)
			}
		})
	}
}
