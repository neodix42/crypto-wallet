package nativewallet

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

func TestTONDeploymentSafeCodesReachFixedStatusWithoutRawProviderMessages(t *testing.T) {
	const raw = "CANARY-public-provider-body secret-key-path /private/wallet"
	for _, r := range []struct{ code, want string }{
		{"TON_RPC_RATE_LIMITED", "rate-limited"},
		{"TON_DEPLOYMENT_UNAVAILABLE", "No deployment was sent"},
		{"TON_DEPLOYMENT_RECORD_FAILED", "transaction history"},
		{"TON_DEPLOYMENT_PENDING", "Check status"},
		{"TON_DEPLOYMENT_INVALID_PLAN", "recorded contract"},
	} {
		t.Run(r.code, func(t *testing.T) {
			a, ui, core, account := tonFixture(t)
			a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "200000000", true)}}
			previous := &model.Plan{ID: "preserve-unrelated-prepared-operation"}
			a.plan = previous
			var calls atomic.Int32
			core.handler = func(_ uint64, method string, body, out any) error {
				calls.Add(1)
				if method != "prepareTonDeployment" || viewText(body) != `{"accountId":"`+account.ID+`"}` {
					t.Errorf("failed deployment changed typed route %s", method)
				}
				// Even a populated response must not become a preview after an error.
				_ = answer(out, model.Plan{ID: raw, Recipient: raw})
				return fmt.Errorf(raw+": %w", ipc.NewRequestError(r.code))
			}
			a.tonEvent("tonDeploy", account.ID)
			ui.apply(t)
			if !strings.Contains(a.status, r.want) || strings.Contains(a.status, raw) || strings.Contains(viewText(a.walletPage(account)), raw) {
				t.Fatal("provider response escaped fixed status", a.status)
			}
			if len(a.tonPending) != 0 || a.plan != previous || calls.Load() != 1 || ui.cancels.Load() == 0 || a.busy {
				t.Fatal("failed preparation installed a preview, changed unrelated plan or sent funds")
			}
		})
	}
}
func TestTONDeploymentInvalidSuccessfulResponseNeverCreatesSpendablePreview(t *testing.T) {
	for _, kind := range []string{"account", "outgoing-value", "not-deployment", "corrupt-payload", "expired"} {
		t.Run(kind, func(t *testing.T) {
			a, ui, core, account := tonFixture(t)
			a.tonStates = map[string]tonObservation{account.ID: {State: tonStateFixture(t, account, "uninit", "200000000", true)}}
			plan := nativeTONDeploymentPlan(account)
			switch kind {
			case "account":
				plan.Account.Address = "CANARY-public-provider-owned-address"
			case "outgoing-value":
				plan.Amount = "1"
			case "not-deployment":
				plan.Asset = "ton-mainnet:USDT"
			case "corrupt-payload":
				plan.Payload = json.RawMessage(`{"CANARY-public-provider-body":"bad"}`)
			case "expired":
				plan.Expires = 1
			}
			routeFixture(t, a, core, "prepareTonDeployment", map[string]string{"accountId": account.ID}, plan)
			a.tonEvent("tonDeploy", account.ID)
			ui.apply(t)
			if len(a.tonPending) != 0 || !strings.Contains(a.status, "recorded contract") || strings.Contains(a.status, "CANARY") || a.busy {
				t.Fatal("invalid response was presented for spending or leaked input", a.status)
			}
		})
	}
}
func TestTONStatusRateLimitIsActionableAndGeneralFailureRemainsSafe(t *testing.T) {
	const raw = "CANARY-public-provider-body with API token"
	for _, rate := range []bool{true, false} {
		a, ui, core, account := tonFixture(t)
		var calls atomic.Int32
		core.handler = func(_ uint64, method string, body, out any) error {
			calls.Add(1)
			if method != "tonWalletState" {
				t.Errorf("failed observation attempted deployment %s", method)
			}
			if rate {
				return fmt.Errorf(raw+": %w", ipc.NewRequestError("TON_RPC_RATE_LIMITED"))
			}
			return errors.New(raw)
		}
		a.readTONState(account)
		ui.apply(t)
		observed := a.tonStates[account.ID]
		before := observed
		if !observed.Failed || observed.Loading || strings.Contains(viewText(observed), raw) {
			t.Fatal("failed observation retained raw response", observed)
		}
		page := viewText(a.tonNodes(account))
		if strings.Contains(page, raw) || !strings.Contains(page, "Contract status: unavailable") {
			t.Fatal("RPC failure lost safe status", page)
		}
		if rate && (!strings.Contains(page, "rate limit") || !strings.Contains(page, "trusted RPC/API key")) {
			t.Fatal("rate limit does not explain safe next step", page)
		}
		a.tonEvent("tonDeploy", account.ID)
		if calls.Load() != 1 || len(a.tonPending) != 0 || !reflect.DeepEqual(a.tonStates[account.ID], before) {
			t.Fatal("failed observation enabled deployment")
		}
	}
}
