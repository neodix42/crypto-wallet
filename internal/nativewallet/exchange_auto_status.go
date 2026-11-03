package nativewallet

import (
	"encoding/hex"
	"encoding/json"
	"strings"
)

// These public records describe already persisted exchange requests. They are
// never authority to prepare, authorize, resend or cancel an operation.
type freePublicOperation struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	State     string `json:"state"`
	AccountID string `json:"accountId"`
	AssetID   string `json:"assetId"`
}

type freeTrackingConnection struct {
	Provider, Endpoint, KeySuffix string
	Connected, Configured         bool
}

func (a *App) freeTrackingStatus() (freeTrackingConnection, []freePublicOperation, bool) {
	var status struct {
		Provider, Endpoint, KeySuffix string
		Connected, Configured         bool
		Operations                    []freePublicOperation
	}
	if json.Unmarshal(a.exchange, &status) != nil {
		return freeTrackingConnection{}, nil, false
	}
	if status.Provider == "" {
		status.Provider = "bybit"
	}
	return freeTrackingConnection{status.Provider, status.Endpoint, status.KeySuffix, status.Connected, status.Configured}, status.Operations, true
}

func freeCanPoll(state string) bool {
	return freePending(state) && state != "PREPARED" && state != "AUTHORIZED" && state != "SIGNED_NOT_SUBMITTED"
}

func freeRemoteTerminal(state string) bool {
	switch state {
	case "CONFIRMED", "FAILED", "CANCELLED", "EXCHANGE_COMPLETED":
		return true
	}
	return false
}

func freeRemoteStage(kind string) int {
	switch kind {
	case "transfer":
		return 2
	case "swap":
		return 3
	case "withdraw":
		return 5
	}
	return 0
}

func (a *App) ownedFreePublicOperation(operation freePublicOperation) bool {
	// A remote operation ID must not fall through the core's separate on-chain
	// reconciliation path. Source descriptors must still belong to this profile.
	if operation.Provider != "bybit" || len(operation.ID) != 32 || operation.ID != strings.ToLower(operation.ID) || freeRemoteStage(operation.Kind) == 0 {
		return false
	}
	if _, err := hex.DecodeString(operation.ID); err != nil {
		return false
	}
	account, ok := a.account(operation.AccountID)
	if !ok {
		return false
	}
	network := a.network(account.Network)
	if network.Testnet {
		return false
	}
	for _, asset := range network.Assets {
		if asset.ID == operation.AssetID && exchangeAssetAllowed(account, asset) {
			return true
		}
	}
	return false
}

func (a *App) resumeFreeExchangeOperations() bool {
	connection, operations, valid := a.freeTrackingStatus()
	if !valid || connection.Provider != "bybit" || !connection.Connected {
		return false
	}
	u := a.freeData()
	changed := false
	counts := map[string]int{}
	for _, operation := range operations {
		counts[operation.ID]++
	}
	for _, operation := range operations {
		if counts[operation.ID] != 1 || !a.ownedFreePublicOperation(operation) {
			continue
		}
		existing := false
		for index := range u.operations {
			original := &u.operations[index]
			if original.ID != operation.ID {
				continue
			}
			existing = true
			if original.Kind == operation.Kind && original.AccountID == operation.AccountID && original.AssetID == operation.AssetID && original.State != operation.State {
				if freePending(original.State) && freeRemoteTerminal(operation.State) {
					original.State = operation.State
					original.revision++
					changed = true
				} else if freeRemoteTerminal(original.State) && freePending(operation.State) {
					// A status read started before reconciliation may arrive later.
					// Its cached pending row cannot reopen a verified terminal ID.
					a.recordFreePublicState(original.ID, original.State)
					changed = true
				}
			}
			break
		}
		if !existing && freeCanPoll(operation.State) {
			u.operations = append(u.operations, freeExchangeOperation{Stage: freeRemoteStage(operation.Kind), AccountID: operation.AccountID, AssetID: operation.AssetID, restored: true, wizardResult: wizardResult{ID: operation.ID, Kind: operation.Kind, State: operation.State}})
			changed = true
		}
	}
	return changed
}

func (a *App) recordFreePublicState(id, state string) {
	var status map[string]json.RawMessage
	if json.Unmarshal(a.exchange, &status) != nil {
		return
	}
	var operations []map[string]json.RawMessage
	if json.Unmarshal(status["operations"], &operations) != nil {
		return
	}
	for _, operation := range operations {
		var candidate string
		if json.Unmarshal(operation["id"], &candidate) == nil && candidate == id {
			operation["state"], _ = json.Marshal(state)
		}
	}
	status["operations"], _ = json.Marshal(operations)
	a.exchange, _ = json.Marshal(status)
}

func (a *App) freePublicOperationSnapshot(id string) (freePublicOperation, bool) {
	_, operations, valid := a.freeTrackingStatus()
	if !valid {
		return freePublicOperation{}, false
	}
	var found freePublicOperation
	count := 0
	for _, operation := range operations {
		if operation.ID == id {
			found = operation
			count++
		}
	}
	return found, count == 1
}

func nextFreeTrackedOperation(u *exchangeFreeUI) *freeExchangeOperation {
	start := 0
	for index, operation := range u.operations {
		if operation.ID == u.lastCheckedID {
			start = index + 1
			break
		}
	}
	for offset := 0; offset < len(u.operations); offset++ {
		operation := u.operations[(start+offset)%len(u.operations)]
		if freeCanPoll(operation.State) {
			u.lastCheckedID = operation.ID
			return &operation
		}
	}
	return nil
}
