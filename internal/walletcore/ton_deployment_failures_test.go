package walletcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
)

type classifiedTONDeployer struct {
	*ton.Adapter
	prepareErr, readErr error
	plan                model.Plan
	prepares, submits   int
}

func (a *classifiedTONDeployer) PrepareDeployment(context.Context, model.Account) (model.Plan, error) {
	a.prepares++
	return a.plan, a.prepareErr
}
func (a *classifiedTONDeployer) WalletState(context.Context, model.Account) (model.TONWalletState, error) {
	return model.TONWalletState{}, a.readErr
}
func (a *classifiedTONDeployer) Submit(context.Context, model.Plan, model.Signed) (model.Receipt, error) {
	a.submits++
	return model.Receipt{}, errors.New("unexpected deployment broadcast")
}
func fundedPointTwoTONDeployment(account model.Account) model.Plan {
	raw, _ := json.Marshal(ton.Payload{Kind: "deployment", Deploy: true, SourceBalance: "200000000", EstimatedFee: "1000000", StorageReserve: "10000000", MasterSeqno: 123})
	plan := model.Plan{Account: account, Asset: account.Network + ":TON", Recipient: account.Address, Amount: "0", MaxFee: "3000000", FeeAsset: account.Network + ":TON", Nonce: "0", Expires: time.Now().Add(time.Minute).Unix(), Payload: raw}
	encoded, _ := json.Marshal(plan)
	digest := sha256.Sum256(encoded)
	plan.ID = hex.EncodeToString(digest[:])
	return plan
}
func TestTONDeploymentFailuresAreSafeAndDoNotPrepareJournalOrBroadcast(t *testing.T) {
	const raw = "CANARY-public-remote-body API-token-path user-supplied-secret"
	for _, r := range []struct {
		name, code   string
		failure      error
		rejectRecord bool
	}{
		{"rate", "TON_RPC_RATE_LIMITED", fmt.Errorf(raw+": %w", ton.ErrRateLimited), false},
		{"general", "TON_DEPLOYMENT_UNAVAILABLE", errors.New(raw), false},
		{"forged-rate-text", "TON_DEPLOYMENT_UNAVAILABLE", errors.New("TON RPC rate limited " + raw), false},
		{"record", "TON_DEPLOYMENT_RECORD_FAILED", nil, true},
	} {
		t.Run(r.name, func(t *testing.T) {
			c, path, account, queries := tonDeploymentCoordinator(t)
			adapter := &classifiedTONDeployer{Adapter: ton.New(), prepareErr: r.failure, plan: fundedPointTwoTONDeployment(account)}
			c.adapters = []model.Adapter{adapter}
			state := readPublicState(t, path)
			if r.rejectRecord {
				state["reject"] = []string{"prepare"}
			}
			writePublicState(t, path, state)
			original := readPublicState(t, path)["ops"]
			result, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
			if err == nil || result != nil || ipc.PublicErrorCode(err) != r.code || err.Error() != r.code || strings.Contains(err.Error(), raw) {
				t.Fatal("raw provider data escaped classified preparation", result, err)
			}
			current := readPublicState(t, path)
			if !reflect.DeepEqual(original, current["ops"]) || adapter.submits != 0 || adapter.prepares != 1 || queries.Load() != 0 {
				t.Fatal("failed deployment wrote journal, broadcast or reached external provider", current)
			}
			for _, value := range current["calls"].([]any) {
				call := value.(string)
				if call == "sign" || call == "send" || call == "update" || call == "prepare" && !r.rejectRecord {
					t.Fatal("failed preparation gained signer authority", call)
				}
			}
		})
	}
}
func TestTONDeploymentStatusRateConditionIsTypedNotProviderText(t *testing.T) {
	const raw = "CANARY-public-hostile-provider-body"
	for _, r := range []struct {
		err  error
		code string
	}{{fmt.Errorf(raw+": %w", ton.ErrRateLimited), "TON_RPC_RATE_LIMITED"}, {errors.New("TON RPC rate limited " + raw), "REQUEST_FAILED_STATE_UNKNOWN"}} {
		c, _, account, _ := tonDeploymentCoordinator(t)
		adapter := &classifiedTONDeployer{Adapter: ton.New(), readErr: r.err}
		c.adapters = []model.Adapter{adapter}
		result, err := c.handle(message("tonWalletState", map[string]string{"accountId": account.ID}))
		if result != nil || ipc.PublicErrorCode(err) != r.code || strings.Contains(ipc.PublicErrorCode(err), raw) || adapter.prepares != 0 || adapter.submits != 0 {
			t.Fatal("status error leaked text or gained deployment authority", result, err)
		}
	}
}
func TestTONDeploymentPointTwoGRAMOnlyRecordsImmutableZeroTransferPlan(t *testing.T) {
	c, path, account, queries := tonDeploymentCoordinator(t)
	adapter := &classifiedTONDeployer{Adapter: ton.New(), plan: fundedPointTwoTONDeployment(account)}
	c.adapters = []model.Adapter{adapter}
	if err := ton.New().ValidatePlan(adapter.plan); err != nil {
		t.Fatal("synthetic0.2GRAM fixture invalid", err)
	}
	result, err := c.handle(message("prepareTonDeployment", map[string]string{"accountId": account.ID}))
	if err != nil || !reflect.DeepEqual(result, adapter.plan) || adapter.submits != 0 || queries.Load() != 0 {
		t.Fatal("funded preparation sent or changed the deployment", result, err)
	}
	ops := readPublicState(t, path)["ops"].([]any)
	if len(ops) != 1 || ops[0].(map[string]any)["state"] != "PREPARED" {
		t.Fatal("deployment was not durably recorded for later exact approval", ops)
	}
}

func TestTONPublicRequestErrorCodeRetainsSafeRateCategory(t *testing.T) {
	if publicRequestErrorCode(fmt.Errorf("private-provider-token: %w", ton.ErrRateLimited)) != "TON_RPC_RATE_LIMITED" {
		t.Fatal("wrapped TON provider limit lost safe category")
	}
	if publicRequestErrorCode(errors.New("TON_RPC_RATE_LIMITED private-provider-token")) != "REQUEST_FAILED_STATE_UNKNOWN" {
		t.Fatal("provider text spoofed a public category")
	}
}
