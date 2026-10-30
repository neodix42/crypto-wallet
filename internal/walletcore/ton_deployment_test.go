package walletcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/uiprompt"
)

func tonDeploymentCoordinator(t *testing.T) (*core, string, model.Account, *atomic.Int32) {
	t.Helper()
	adapter := ton.New()
	account, err := adapter.Derive(make([]byte, 64), 0, "ton-testnet")
	if err != nil {
		t.Fatal(err)
	}
	queries := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var query struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(request.Body).Decode(&query)
		queries.Add(1)
		var result any
		switch query.Method {
		case "getMasterchainInfo":
			result = map[string]any{"init": map[string]any{"root_hash": "gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=", "file_hash": "Z+IKwYS54DmmJmesw/nAD5DzWadnOCMzee+kdgSYDOg=", "workchain": -1}, "last": map[string]any{"seqno": 123}}
		case "getAddressInformation":
			result = map[string]any{"balance": "1000000000", "state": "uninitialized", "sync_utime": time.Now().Unix()}
		case "estimateFee":
			result = map[string]any{"source_fees": map[string]int{"gas_fee": 1000000, "storage_fee": 0, "in_fwd_fee": 0, "fwd_fee": 0}}
		default:
			t.Error("readonly deployment preparation attempted network write", query.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
	}))
	t.Cleanup(server.Close)
	if err := adapter.Configure(account.Network, ton.Endpoint{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	c.adapters = []model.Adapter{adapter}
	state := readPublicState(t, path)
	state["accounts"] = []model.Account{account}
	writePublicState(t, path, state)
	return c, path, account, queries
}

func TestTONProfileRecoveryRelaysItsExactSignerConfirmation(t *testing.T) {
	c, path := fakeCoordinator(t, &publicAdapter{}, nil)
	state := readPublicState(t, path)
	state["promptMethod"] = "addTonAccount"
	writePublicState(t, path, state)
	var wire bytes.Buffer
	c.publicChannel = &ipc.Channel{W: &wire}
	seen := false
	c.child.OnPrompt = func(body json.RawMessage) error {
		if err := c.relayPrompt(body); err != nil {
			return err
		}
		forwarded, err := (&ipc.Channel{R: &wire}).Read()
		if err != nil || forwarded.Method != "prompt" {
			t.Fatal("TON profile confirmation lost original request/body binding", err)
		}
		prompt, err := uiprompt.DecodePrompt(body)
		if err != nil || prompt.Kind != "confirm" {
			t.Fatal("profile recovery did not expose the exact signer confirmation", err)
		}
		delivered, err := uiprompt.DecodePrompt(forwarded.Body)
		if err != nil || !reflect.DeepEqual(delivered, prompt) {
			t.Fatal("framed TON profile confirmation changed signer-authored fields", err)
		}
		answer, _ := json.Marshal(uiprompt.Reply{PromptID: prompt.ID, Accepted: false})
		seen = true
		return c.replyPrompt(ipc.Message{Version: 1, ID: forwarded.ID, Session: forwarded.Session, Method: "promptReply", Body: answer})
	}
	request := message("addTonAccount", map[string]any{"index": 1, "profile": ton.Profile})
	request.Session = "public-ton-profile-protocol-session"
	if _, err := c.handle(request); err != nil || !seen {
		t.Fatal("explicit TON profile route failed its inherited confirmation protocol", err)
	}
	if c.promptPending {
		t.Fatal("completed TON profile confirmation retained prompt authority")
	}
}

type epochTONDeployer struct {
	*ton.Adapter
	read func(context.Context, model.Account) (model.TONWalletState, error)
}

func (a epochTONDeployer) WalletState(ctx context.Context, account model.Account) (model.TONWalletState, error) {
	return a.read(ctx, account)
}

func TestTONDeploymentStateDiscardsLateSessionObservation(t *testing.T) {
	c, _, account, queries := tonDeploymentCoordinator(t)
	c.adapters = []model.Adapter{epochTONDeployer{Adapter: ton.New(), read: func(ctx context.Context, account model.Account) (model.TONWalletState, error) {
		c.mu.Lock()
		c.epoch++
		c.active = false
		if c.sessionCancel != nil {
			c.sessionCancel()
		}
		c.mu.Unlock()
		return model.TONWalletState{AccountID: account.ID, Status: "uninit", CanDeploy: true}, nil
	}}}
	if state, err := c.handle(message("tonWalletState", map[string]any{"accountId": account.ID})); err == nil || state != nil {
		t.Fatal("stale TON deployment observation crossed the lock boundary", state, err)
	}
	if queries.Load() != 0 {
		t.Fatal("epoch fixture contacted a real RPC")
	}
}

func TestTONDeploymentCoordinatorOwnedStateAndDurableExactPlan(t *testing.T) {
	c, path, account, _ := tonDeploymentCoordinator(t)
	value, err := c.handle(message("tonWalletState", map[string]any{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	state, ok := value.(model.TONWalletState)
	if !ok || state.Status != "uninit" || !state.CanDeploy || state.Address != account.Address || state.WalletID != "2147483607" || state.Subwallet != "42" {
		t.Fatal("contract status lost exact owned identity", state)
	}
	value, err = c.handle(message("prepareTonDeployment", map[string]any{"accountId": account.ID}))
	if err != nil {
		t.Fatal(err)
	}
	plan, ok := value.(model.Plan)
	if !ok || plan.Amount != "0" || plan.Recipient != account.Address || !ton.IsDeploymentPlan(plan) || len(plan.ID) != 64 {
		t.Fatal("deployment changed its fixed zero-transfer effects", plan)
	}
	if err := ton.New().ValidatePlan(plan); err != nil {
		t.Fatal("coordinator changed TON canonical plan digest", err)
	}
	operation := readPublicState(t, path)["ops"].([]any)
	if len(operation) != 1 || operation[0].(map[string]any)["state"] != "PREPARED" {
		t.Fatal("deployment was not durably prepared before public preview")
	}
}

func TestTONDeploymentCoordinatorRejectsCallerContractParametersAndUnownedAccounts(t *testing.T) {
	c, _, account, queries := tonDeploymentCoordinator(t)
	for _, method := range []string{"tonWalletState", "prepareTonDeployment"} {
		for _, body := range []map[string]any{
			{}, {"accountId": "unknown"}, {"accountId": strings.Repeat("x", 513)},
			{"accountId": account.ID, "walletId": 42}, {"accountId": account.ID, "recipient": account.Address},
			{"accountId": account.ID, "amount": "1"}, {"accountId": account.ID, "stateInit": "arbitrary"},
		} {
			before := queries.Load()
			if _, err := c.handle(message(method, body)); err == nil {
				t.Fatal("caller-selected contract data or unowned account accepted", method, body)
			}
			if queries.Load() != before {
				t.Fatal("invalid deployment intent reached a network provider")
			}
		}
	}
	unit, _ := fakeCoordinator(t, &publicAdapter{}, nil)
	if _, err := unit.handle(message("tonWalletState", map[string]any{"accountId": publicAccount().ID})); err == nil {
		t.Fatal("non-TON wallet accepted contract deployment")
	}
}
