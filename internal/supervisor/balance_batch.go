package supervisor

import (
	"context"
	"encoding/json"
	"errors"

	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

// CallBalancesAt keeps one serialized IPC request while independent provider
// workers stream public results. Cancelling it never ends the signer session
// and never grants cancellation authority over a later write.
func (c *Child) CallBalancesAt(ctx context.Context, generation uint64, accountIDs []string, onResult func(model.BalanceBatchResult)) error {
	if ctx == nil || c.SelfRole != "--internal-core" || onResult == nil || len(accountIDs) == 0 || len(accountIDs) > model.BalanceBatchLimit {
		return errors.New("wallet balances require bounded accounts and a core context")
	}
	selected := make(map[string]bool, len(accountIDs))
	for _, id := range accountIDs {
		if id == "" || len(id) > 256 || selected[id] {
			return errors.New("invalid selected balance account")
		}
		selected[id] = true
	}
	seen := make(map[string]bool, len(accountIDs))
	return c.callStreamContext(&generation, "balanceBatch", struct {
		AccountIDs []string `json:"accountIds"`
	}{append([]string(nil), accountIDs...)}, nil, ctx, func(body json.RawMessage) error {
		var result model.BalanceBatchResult
		if strictjson.Decode(body, &result) != nil || !model.ValidBalanceBatchResult(result) || !selected[result.AccountID] || seen[result.AccountID] {
			return errors.New("invalid wallet balance observation")
		}
		seen[result.AccountID] = true
		if ctx.Err() == nil {
			onResult(result)
		}
		return nil
	}, func(final json.RawMessage) error {
		var summary model.BalanceBatchSummary
		if strictjson.Decode(final, &summary) != nil || summary.Completed != len(accountIDs) || len(seen) != len(accountIDs) {
			return errors.New("incomplete wallet balance stream")
		}
		return nil
	})
}
