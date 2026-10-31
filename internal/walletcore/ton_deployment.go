package walletcore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

// The caller selects an owned account only. Its immutable profile determines
// the contract, StateInit, wallet ID, fee policy and zero-outgoing actions.
func (c *core) tonDeploymentAt(message ipc.Message, epoch uint64) (any, error) {
	if message.Method != "tonWalletState" && message.Method != "prepareTonDeployment" {
		return nil, errors.New("unsupported TON deployment operation")
	}
	var request struct {
		AccountID strictjson.Text `json:"accountId"`
	}
	if strictjson.Decode(message.Body, &request) != nil || request.AccountID == "" || len(request.AccountID) > 512 {
		return nil, errors.New("TON deployment requires an owned account")
	}
	account, err := c.account(string(request.AccountID))
	if err != nil {
		return nil, err
	}
	adapter, err := c.adapter(account.Network)
	if err != nil {
		return nil, err
	}
	deployer, ok := adapter.(model.TONDeployer)
	if !ok || account.Family != "ton" {
		return nil, errors.New("contract deployment is available only for supported TON wallets")
	}
	ctx, cancel := c.networkContextAt(45*time.Second, epoch)
	defer cancel()
	if message.Method == "tonWalletState" {
		state, err := deployer.WalletState(ctx, account)
		if err != nil {
			if errors.Is(err, ton.ErrRateLimited) {
				return nil, ipc.NewRequestError("TON_RPC_RATE_LIMITED")
			}
			return nil, err
		}
		c.mu.Lock()
		valid := c.active && c.epoch == epoch && ctx.Err() == nil
		c.mu.Unlock()
		if !valid {
			return nil, errors.New("wallet session changed during TON contract observation")
		}
		return state, nil
	}
	// Preparation reserves the account durably. A lost UI preview must resume
	// that exact unsigned operation instead of creating a conflicting second
	// deployment, and must never replace an artifact which may have been sent.
	if existing, err := c.preparedTONDeploymentAt(epoch, account, adapter); err != nil {
		return nil, err
	} else if existing != nil {
		return *existing, nil
	}
	plan, err := deployer.PrepareDeployment(ctx, account)
	if err != nil {
		if errors.Is(err, ton.ErrRateLimited) {
			return nil, ipc.NewRequestError("TON_RPC_RATE_LIMITED")
		}
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_UNAVAILABLE")
	}
	// TON signs a canonical digest as its Plan.ID. Keep it unchanged; the
	// signer independently validates that digest and records it before consent.
	if err := c.childCallAt(epoch, "prepare", plan, nil); err != nil {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	return plan, nil
}

func (c *core) preparedTONDeploymentAt(epoch uint64, account model.Account, adapter model.Adapter) (*model.Plan, error) {
	cursor, revision := "", ""
	seen := map[string]bool{}
	var reservation *journal.Operation
	for pages := 0; ; pages++ {
		if pages >= 10000 {
			return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
		}
		var page journalPage
		if err := c.childCallAt(epoch, "journalPage", struct {
			AccountID string `json:"accountId"`
			Asset     string `json:"asset"`
			Cursor    string `json:"cursor"`
		}{account.ID, "", cursor}, &page); err != nil {
			return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
		}
		if len(page.Operations) > 128 || page.Total < 0 || page.Total > 10000 || page.Total < len(page.Operations) || page.Revision == "" || revision != "" && revision != page.Revision || page.Complete != (page.NextCursor == "") || !page.Complete && (len(page.Operations) == 0 || page.NextCursor == cursor) {
			return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
		}
		revision = page.Revision
		for _, operation := range page.Operations {
			if operation.Plan.ID == "" || seen[operation.Plan.ID] || operation.Plan.Account.ID != account.ID || operation.Signed != nil || len(seen) >= 10000 {
				return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
			}
			seen[operation.Plan.ID] = true
			if !journal.Terminal(operation.State) {
				if reservation != nil {
					return nil, ipc.NewRequestError("TON_DEPLOYMENT_PENDING")
				}
				original := operation
				reservation = &original
			}
		}
		if page.Complete {
			if len(seen) != page.Total {
				return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
			}
			break
		}
		cursor = page.NextCursor
	}
	if reservation == nil {
		return nil, nil
	}
	// Journal pages omit signed bytes; consult the original private record so
	// inconsistent PREPARED-with-signature data cannot release a reservation.
	var original journal.Operation
	if err := c.childCallAt(epoch, "operation", struct {
		ID string `json:"id"`
	}{reservation.Plan.ID}, &original); err != nil {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	unsignedView := original
	unsignedView.Signed = nil
	if !reflect.DeepEqual(unsignedView, *reservation) || !reflect.DeepEqual(original.Plan.Account, account) || original.Digest != journal.Digest(original.Plan) {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	if original.State != "PREPARED" || original.Signed != nil || !ton.IsDeploymentPlan(original.Plan) {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_PENDING")
	}
	if original.Plan.Expires > time.Now().Unix() {
		if adapter.ValidatePlan(original.Plan) != nil {
			return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
		}
		return &original.Plan, nil
	}
	// Validate the expired artifact's complete original identity/effects before
	// cancelling it. Only the temporary validation copy receives a new deadline;
	// persisted bytes and any signed identity are never modified or reused.
	if original.Plan.ID != tonDeploymentDigest(original.Plan) {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	check := original.Plan
	check.Expires = time.Now().Add(time.Minute).Unix()
	check.ID = tonDeploymentDigest(check)
	if adapter.ValidatePlan(check) != nil {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	if err := c.childCallAt(epoch, "cancelPrepared", struct {
		ID string `json:"id"`
	}{original.Plan.ID}, nil); err != nil {
		return nil, ipc.NewRequestError("TON_DEPLOYMENT_RECORD_FAILED")
	}
	return nil, nil
}

func tonDeploymentDigest(plan model.Plan) string {
	plan.ID = ""
	raw, err := json.Marshal(plan)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
