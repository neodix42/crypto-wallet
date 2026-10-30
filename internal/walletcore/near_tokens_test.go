package walletcore

import (
	"bytes"
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

type nearTokenRPCState struct {
	queries                       atomic.Int32
	fail, registered, balanceFail atomic.Bool
	lowGas                        atomic.Bool
}

func nearTokenCoordinator(t *testing.T) (*core, string, model.Account, *nearTokenRPCState) {
	t.Helper()
	chain := near.New()
	account, err := chain.Derive(make([]byte, 64), 0, "near-testnet")
	if err != nil {
		t.Fatal(err)
	}
	state := new(nearTokenRPCState)
	block := base58.Encode(make([]byte, 32))
	hash := base58.Encode(bytes.Repeat([]byte{0x42}, 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid fixture request")
			return
		}
		state.queries.Add(1)
		respond := func(value any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "wallet", "result": value})
		}
		failure := func() {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": "wallet", "error": map[string]any{"code": -1, "message": "PUBLIC-UNAVAILABLE"}})
		}
		if state.fail.Load() {
			failure()
			return
		}
		switch request.Method {
		case "status":
			respond(map[string]any{"chain_id": "testnet", "sync_info": map[string]any{"syncing": false, "latest_block_time": time.Now().UTC().Format(time.RFC3339Nano)}})
		case "block":
			respond(map[string]any{"header": map[string]any{"hash": block, "height": 123, "timestamp_nanosec": time.Now().UnixNano()}})
		case "query":
			var p map[string]string
			_ = json.Unmarshal(request.Params, &p)
			switch p["request_type"] {
			case "view_account":
				amount := "1000000000000000000000000000"
				if state.lowGas.Load() {
					amount = "1000000000000000000001"
				}
				respond(map[string]any{"amount": amount, "locked": "0", "storage_usage": 100, "code_hash": hash, "block_hash": block, "block_height": 123})
			case "view_access_key":
				respond(map[string]any{"nonce": 41, "permission": "FullAccess", "block_hash": block, "block_height": 123})
			case "call_function":
				if p["account_id"] != "token.testnet" {
					t.Error("token query changed exact contract")
				}
				var data string
				switch p["method_name"] {
				case "ft_metadata":
					data = `{"spec":"ft-1.0.0","name":"Public fixture","symbol":"PUBLIC","decimals":6}`
				case "ft_balance_of":
					if state.balanceFail.Load() {
						failure()
						return
					}
					data = `"123456789"`
				case "storage_balance_bounds":
					data = `{"min":"1250000000000000000000","max":"1250000000000000000000"}`
				case "storage_balance_of":
					data = `null`
					if state.registered.Load() {
						data = `{"total":"1250000000000000000000","available":"0"}`
					}
				default:
					t.Error("nonstandard read reached token fixture", p["method_name"])
					failure()
					return
				}
				values := make([]int, len(data))
				for i := range data {
					values[i] = int(data[i])
				}
				respond(map[string]any{"result": values, "block_hash": block, "block_height": 123})
			default:
				t.Error("unsupported token query", p["request_type"])
				failure()
			}
		case "EXPERIMENTAL_protocol_config":
			cost := map[string]any{"execution": 1, "send_not_sir": 1, "send_sir": 1}
			respond(map[string]any{"chain_id": "testnet", "protocol_version": 88, "runtime_config": map[string]any{"storage_amount_per_byte": "10000000000000000000", "min_gas_purchase_price": "1000000000", "transaction_costs": map[string]any{"action_receipt_creation_config": cost, "action_creation_config": map[string]any{"function_call_cost": cost, "function_call_cost_per_byte": cost}}}})
		case "gas_price":
			respond(map[string]string{"gas_price": "1000000000"})
		default:
			t.Error("read/preparation attempted an unapproved or broadcasting RPC", request.Method)
			failure()
		}
	}))
	t.Cleanup(server.Close)
	if err = chain.SetEndpoint(account.Network, server.URL); err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{chain}
	public := readPublicState(t, path)
	public["accounts"] = []model.Account{account}
	public["settings"] = map[string]any{account.Network: map[string]string{"url": server.URL}}
	writePublicState(t, path, public)
	return c, path, account, state
}

func importCoordinatorNEP141(t *testing.T, c *core, account model.Account) string {
	t.Helper()
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "token.testnet"})); err != nil {
		t.Fatal(err)
	}
	return account.Network + ":nep141:token.testnet"
}

func TestNEP141CoordinatorUsesObservedPolicyAndExistingExactWholeTokenSendPipeline(t *testing.T) {
	c, path, account, rpc := nearTokenCoordinator(t)
	asset := importCoordinatorNEP141(t, c, account)
	updated, _ := c.adapter(account.Network)
	found := false
	for _, network := range updated.Networks() {
		for _, entry := range network.Assets {
			if entry.ID == asset {
				found = entry.Contract == "token.testnet" && entry.Decimals == 6 && entry.Symbol == "PUBLIC"
			}
		}
	}
	if !found {
		t.Fatal("approved exact metadata did not reach dynamic network registry")
	}
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "token.testnet"})); err == nil {
		t.Fatal("duplicate tracked policy reimported")
	}
	if _, _, _, err := c.exchangeAsset(account.ID, asset); err == nil {
		t.Fatal("new NEAR token gained unreviewed Bybit route")
	}
	observed, err := c.handle(message("balances", map[string]string{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	balances := observed.([]model.Balance)
	if len(balances) != 2 || balances[1].Asset != asset || balances[1].Atomic != "123456789" || balances[0].Atomic == "" {
		t.Fatal("native and imported token balance observations not independent")
	}
	rpc.balanceFail.Store(true)
	observed, err = c.handle(message("balances", map[string]string{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	balances = observed.([]model.Balance)
	if balances[0].Atomic == "" || balances[1].Atomic != "" || !strings.Contains(balances[1].State, "UNKNOWN") {
		t.Fatal("failed token RPC hid native balance or fabricated zero")
	}
	rpc.balanceFail.Store(false)
	rpc.registered.Store(true)
	prepared, err := c.handle(message("prepare", model.Transfer{AccountID: account.ID, Asset: asset, Recipient: "receiver.testnet", Amount: "1.234567"}))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.(model.Plan)
	var d near.Payload
	_ = json.Unmarshal(plan.Payload, &d)
	if plan.Amount != "1234567" || plan.FeeAsset != account.Network+":native" || d.Token == nil || d.Token.Deposit != "1" || d.Token.Args != `{"receiver_id":"receiver.testnet","amount":"1234567"}` || d.Token.Gas != near.TokenGas {
		t.Fatal("whole-token public input changed contract call, precision, native fee or attached deposit")
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 1 {
		t.Fatal("token preview bypassed existing durable prepare route")
	}
	if _, err = c.handle(message("prepare", model.Transfer{AccountID: account.ID, Asset: asset, Recipient: "receiver.testnet", Amount: "1.0000001"})); err == nil {
		t.Fatal("excess token precision silently rounded")
	}
}

func TestNEP145CoordinatorSeparatesReadAndExactOwnedRegistrationPreparation(t *testing.T) {
	c, path, account, rpc := nearTokenCoordinator(t)
	asset := importCoordinatorNEP141(t, c, account)
	body := map[string]string{"accountId": account.ID, "asset": asset}
	observed, err := c.handle(message("nearTokenStorage", body))
	if err != nil {
		t.Fatal(err)
	}
	storage := observed.(near.TokenStorageState)
	if storage.Registered || storage.AccountID != account.Address || storage.Asset != asset || storage.Contract != "token.testnet" || storage.MinDeposit != "1250000000000000000000" || storage.Network != account.Network {
		t.Fatal("registration read lost exact wallet/token identity")
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 0 {
		t.Fatal("read-only storage query prepared a payment")
	}
	prepared, err := c.handle(message("prepareNearTokenRegistration", body))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.(model.Plan)
	var d near.Payload
	_ = json.Unmarshal(plan.Payload, &d)
	if plan.ID == "" || plan.Amount != storage.MinDeposit || plan.Asset != account.Network+":native" || plan.FeeAsset != plan.Asset || plan.Recipient != account.Address || d.Token == nil || d.Token.Method != "storage_deposit" || d.Token.Args != `{"account_id":"`+account.Address+`","registration_only":true}` || d.Token.Deposit != plan.Amount {
		t.Fatal("registration preparation introduced caller-selected authority or hid native NEAR spend")
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 1 {
		t.Fatal("registration bypassed signer journal preparation")
	}
	rpc.registered.Store(true)
	if _, err = c.handle(message("prepareNearTokenRegistration", body)); err == nil {
		t.Fatal("already registered wallet prepared redundant storage payment")
	}
}

func TestNEARTokenCoordinatorRejectsPublicMetadataSigningAuthorityAndFailedImport(t *testing.T) {
	c, path, account, rpc := nearTokenCoordinator(t)
	for _, method := range []string{"importToken", "nearTokenStorage", "prepareNearTokenRegistration"} {
		for _, body := range []string{`null`, `{}`, `{"accountId":null}`, `{"accountId":"` + account.ID + `","contract":"token.testnet","decimals":6}`, `{"accountId":"` + account.ID + `","asset":"near-testnet:nep141:token.testnet","gas":"1"}`, `{"accountId":"` + account.ID + `","asset":"near-testnet:nep141:token.testnet","deposit":"1"}`} {
			m := message(method, nil)
			m.Body = []byte(body)
			if _, err := c.handle(m); err == nil {
				t.Fatal("public caller supplied unsupported token signing/metadata authority")
			}
		}
	}
	if rpc.queries.Load() != 0 {
		t.Fatal("malformed public token requests reached RPC")
	}
	state := readPublicState(t, path)
	state["reject"] = []string{"importNearToken"}
	writePublicState(t, path, state)
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "token.testnet"})); err == nil {
		t.Fatal("signer rejection ignored")
	}
	state = readPublicState(t, path)
	delete(state, "reject")
	writePublicState(t, path, state)
	rpc.fail.Store(true)
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "token.testnet"})); err == nil {
		t.Fatal("unverified RPC metadata reached token approval")
	}
	rpc.fail.Store(false)
	state = readPublicState(t, path)
	state["reject"] = []string{"settings"}
	writePublicState(t, path, state)
	if _, err := c.handle(message("importToken", map[string]string{"accountId": account.ID, "contract": "token.testnet"})); err == nil {
		t.Fatal("failed policy installation reported import complete")
	}
}

func TestNEP141CoordinatorMainnetImportDoesNotEnableExchangeRoute(t *testing.T) {
	chain := near.New()
	account, err := chain.Derive(make([]byte, 64), 0, "near-mainnet")
	if err != nil {
		t.Fatal(err)
	}
	policy := model.NEP141Token{Network: account.Network, Asset: model.Asset{ID: account.Network + ":nep141:token.near", Symbol: "PUBLIC", Decimals: 6, Contract: "token.near"}, CodeHash: base58.Encode(bytes.Repeat([]byte{0x42}, 32))}
	chain, err = chain.WithTokens([]model.NEP141Token{policy})
	if err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{chain}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	if _, _, _, err := c.exchangeAsset(account.ID, policy.Asset.ID); err == nil {
		t.Fatal("mainnet token registry granted unreviewed Bybit authority")
	}
	if _, _, _, err := c.exchangeAsset(account.ID, account.Network+":native"); err != nil {
		t.Fatal("imported token disabled native exchange asset", err)
	}
}

func TestNEP141MaximumTransferKeepsSixDecimalTokensSeparateFromNativeFeesAndYoctoDeposit(t *testing.T) {
	c, path, account, rpc := nearTokenCoordinator(t)
	asset := importCoordinatorNEP141(t, c, account)
	rpc.registered.Store(true)
	request := model.Transfer{AccountID: account.ID, Asset: asset, Recipient: "receiver.testnet"}
	prepared, err := c.handle(message("max", request))
	if err != nil {
		t.Fatal(err)
	}
	plan := prepared.(model.Plan)
	var d near.Payload
	_ = json.Unmarshal(plan.Payload, &d)
	if plan.Amount != "123456789" || plan.FeeAsset != account.Network+":native" || plan.Asset != asset || d.Token == nil || d.Token.Deposit != "1" || d.Token.Args != `{"receiver_id":"receiver.testnet","amount":"123456789"}` || plan.MaxFee == "0" {
		t.Fatal("max mixed native fee/deposit with token atomic precision or truncated full token balance")
	}
	before := len(readPublicState(t, path)["ops"].([]any))
	rpc.lowGas.Store(true)
	if _, err = c.handle(message("max", request)); err == nil {
		t.Fatal("full token balance substituted for insufficient native NEAR gas")
	}
	request.Amount = "0.000001"
	if _, err = c.handle(message("prepare", request)); err == nil {
		t.Fatal("one token atomic unit authorized without separate native gas")
	}
	if len(readPublicState(t, path)["ops"].([]any)) != before {
		t.Fatal("native gas refusal prepared a new token operation")
	}
}

func TestNEP145StorageRequestsRejectForeignWalletsAdaptersMethodsAndPrepareFailure(t *testing.T) {
	c, path, account, rpc := nearTokenCoordinator(t)
	asset := importCoordinatorNEP141(t, c, account)
	body := map[string]string{"accountId": account.ID, "asset": asset}
	if _, err := c.handle(message("nearTokenStorage", map[string]string{"accountId": "foreign", "asset": asset})); err == nil {
		t.Fatal("foreign wallet obtained owned storage observation")
	}
	state := readPublicState(t, path)
	state["reject"] = []string{"prepare"}
	writePublicState(t, path, state)
	if _, err := c.handle(message("prepareNearTokenRegistration", body)); err == nil {
		t.Fatal("signer preparation rejection ignored")
	}
	if len(readPublicState(t, path)["ops"].([]any)) != 0 {
		t.Fatal("rejected registration preparation entered journal")
	}
	before := rpc.queries.Load()
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	if _, err := c.nearTokenStorageAt(message("arbitraryCall", body), epoch); err == nil {
		t.Fatal("internal typed storage handler accepted unsupported method")
	}
	if rpc.queries.Load() != before {
		t.Fatal("unsupported method reached network")
	}
	state = readPublicState(t, path)
	altered := account
	altered.Network = "unsupported-network"
	state["accounts"] = []model.Account{altered}
	writePublicState(t, path, state)
	if _, err := c.handle(message("nearTokenStorage", body)); err == nil {
		t.Fatal("missing adapter accepted")
	}
	other, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	if _, err := other.handle(message("nearTokenStorage", map[string]string{"accountId": publicAccount().ID, "asset": "foreign-token"})); err == nil {
		t.Fatal("non-NEAR adapter accepted NEAR storage request")
	}
}
