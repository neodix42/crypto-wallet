package walletcore

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/ipc"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

const balanceBatchWorkers = 4

type balanceJob struct {
	account model.Account
	network model.Network
	adapter model.Adapter
}

func (c *core) balanceBatchAt(message ipc.Message, epoch uint64) (model.BalanceBatchSummary, error) {
	var request struct {
		AccountIDs []string `json:"accountIds"`
	}
	if strictjson.Decode(message.Body, &request) != nil || len(request.AccountIDs) == 0 || len(request.AccountIDs) > model.BalanceBatchLimit {
		return model.BalanceBatchSummary{}, errors.New("invalid balance batch")
	}
	selected := make(map[string]bool, len(request.AccountIDs))
	for _, id := range request.AccountIDs {
		if id == "" || len(id) > 256 || selected[id] {
			return model.BalanceBatchSummary{}, errors.New("invalid balance batch accounts")
		}
		selected[id] = true
	}
	// All signer access is serial and finishes before public provider workers
	// start. The signer still owns every descriptor and approved token policy.
	state, err := c.current()
	if err != nil || state.Locked {
		return model.BalanceBatchSummary{}, errors.New("wallet locked")
	}
	owned := make(map[string]model.Account, len(state.Accounts))
	for _, account := range state.Accounts {
		owned[account.ID] = account
	}
	for _, id := range request.AccountIDs {
		if _, exists := owned[id]; exists {
			continue
		}
		if state.AccountsCursor == "" {
			return model.BalanceBatchSummary{}, errors.New("unknown balance account")
		}
		var account model.Account
		if c.childCallAt(epoch, "account", struct {
			AccountID string `json:"accountId"`
		}{id}, &account) != nil || account.ID != id {
			return model.BalanceBatchSummary{}, errors.New("unknown balance account")
		}
		owned[id] = account
	}
	c.mu.Lock()
	adapters := append([]model.Adapter(nil), c.adapters...)
	revision, current := c.adapterRevision, c.epoch == epoch && c.active
	c.mu.Unlock()
	if !current {
		return model.BalanceBatchSummary{}, errors.New("wallet session changed")
	}
	groups := make([][]balanceJob, 0, len(request.AccountIDs))
	hosts := make(map[string]int)
	for _, id := range request.AccountIDs {
		account := owned[id]
		adapter, err := findAdapter(adapters, account.Network)
		if err != nil {
			return model.BalanceBatchSummary{}, err
		}
		var network model.Network
		for _, candidate := range adapter.Networks() {
			if candidate.ID == account.Network {
				network = candidate
				break
			}
		}
		if network.ID == "" || network.Family != account.Family || len(network.Assets) == 0 || len(network.Assets) > 64 {
			return model.BalanceBatchSummary{}, errors.New("invalid owned balance network")
		}
		// A shared configured authority is sequential within this batch. This
		// does not replace provider-specific limits such as TON's shared lease.
		host := network.ID
		if endpoint, err := url.Parse(network.RPC); err == nil && endpoint.Host != "" {
			host = strings.ToLower(endpoint.Host)
		}
		position, exists := hosts[host]
		if !exists {
			position = len(groups)
			hosts[host] = position
			groups = append(groups, nil)
		}
		groups[position] = append(groups[position], balanceJob{account, network, adapter})
	}
	ctx, cancel := c.historyContextAt(2*time.Minute, epoch, message.Session)
	defer cancel()
	jobs := make(chan []balanceJob, len(groups))
	for _, group := range groups {
		jobs <- group
	}
	close(jobs)
	results := make(chan model.BalanceBatchResult, balanceBatchWorkers)
	for worker := 0; worker < balanceBatchWorkers && worker < len(groups); worker++ {
		go func() {
			for group := range jobs {
				for _, job := range group {
					if ctx.Err() != nil {
						return
					}
					accountCtx, stop := context.WithTimeout(ctx, 30*time.Second)
					result := observeBalance(accountCtx, job)
					stop()
					select {
					case results <- result:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	for completed := 0; completed < len(request.AccountIDs); completed++ {
		select {
		case <-ctx.Done():
			return model.BalanceBatchSummary{}, ctx.Err()
		case result := <-results:
			c.mu.Lock()
			valid := c.active && c.epoch == epoch && c.adapterRevision == revision
			channel := c.publicChannel
			c.mu.Unlock()
			if !valid || ctx.Err() != nil {
				return model.BalanceBatchSummary{}, errors.New("wallet balance context changed")
			}
			if channel == nil {
				return model.BalanceBatchSummary{}, errors.New("wallet balance stream unavailable")
			}
			body, err := json.Marshal(result)
			if err != nil {
				return model.BalanceBatchSummary{}, err
			}
			if err = channel.Write(ipc.Message{Version: 1, ID: message.ID, Session: message.Session, Method: "balanceProgress", Body: body}); err != nil {
				return model.BalanceBatchSummary{}, err
			}
		}
	}
	return model.BalanceBatchSummary{Completed: len(request.AccountIDs)}, nil
}

func observeBalance(ctx context.Context, job balanceJob) model.BalanceBatchResult {
	values, err := job.adapter.Balance(ctx, job.account)
	result := model.BalanceBatchResult{AccountID: job.account.ID, Network: job.account.Network, Values: values}
	// A checked native observation remains useful when a later token query
	// times out. Adapters explicitly mark those token values unknown. The
	// enclosing batch separately rejects cancelled/stale publication contexts.
	if err != nil || !validBalanceAssets(result, job.network) {
		result.Values, result.Error = nil, "BALANCE_UNAVAILABLE"
		if errors.Is(err, ton.ErrRateLimited) {
			result.Error = "TON_RPC_RATE_LIMITED"
		}
	}
	return result
}

func validBalanceAssets(result model.BalanceBatchResult, network model.Network) bool {
	if !model.ValidBalanceBatchResult(result) || len(result.Values) != len(network.Assets) {
		return false
	}
	assets := make(map[string]bool, len(network.Assets))
	for _, asset := range network.Assets {
		assets[asset.ID] = true
	}
	for _, value := range result.Values {
		if !assets[value.Asset] {
			return false
		}
	}
	return true
}
