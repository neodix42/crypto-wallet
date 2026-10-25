package near

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// Only a view_account error may begin absence verification. An access-key
// failure must never turn a funded wallet or revoked authority into zero funds.
type accountQueryFailure struct{ cause error }

func (e *accountQueryFailure) Error() string { return "NEAR account query unavailable" }
func (e *accountQueryFailure) Unwrap() error { return e.cause }

func unknownAccount(err error) bool {
	var failure *rpcError
	return errors.As(err, &failure) && failure != nil && failure.Name == "HANDLER_ERROR" && failure.Cause.Name == "UNKNOWN_ACCOUNT"
}

func accountBalance(ac model.Account, st accountState) ([]model.Balance, error) {
	if _, err := chainutil.Big(st.Amount, 128); err != nil {
		return nil, err
	}
	if !blockHash(st.Hash) {
		return nil, errInvalid
	}
	return []model.Balance{{Asset: asset(ac.Network), Atomic: st.Amount, State: "FINAL", At: st.Hash}}, nil
}

func (a *Adapter) balanceAfterMissing(ctx context.Context, ac model.Account) ([]model.Balance, error) {
	// state() already validated the immutable descriptor and the selected chain's
	// synchronized, recent head. Named associations are never inferred absent.
	var parameters Parameters
	_ = json.Unmarshal(ac.Parameters, &parameters)
	if !implicit(ac.Address) || parameters.Named {
		return nil, errInvalid
	}
	var block struct {
		Header struct {
			Hash      string          `json:"hash"`
			Height    json.RawMessage `json:"height"`
			Timestamp json.Number     `json:"timestamp_nanosec"`
		} `json:"header"`
	}
	if err := a.rpc(ctx, ac.Network, "block", map[string]string{"finality": "final"}, &block); err != nil {
		return nil, err
	}
	if !blockHash(block.Header.Hash) {
		return nil, errInvalid
	}
	height := string(block.Header.Height)
	if _, err := chainutil.Uint(height, 64); err != nil {
		return nil, err
	}
	nanoseconds, err := chainutil.Uint(block.Header.Timestamp.String(), 64)
	if err != nil || nanoseconds > math.MaxInt64 {
		return nil, errInvalid
	}
	observed := time.Unix(0, int64(nanoseconds))
	if time.Since(observed) > 2*time.Minute || time.Until(observed) > time.Minute {
		return nil, errors.New("NEAR stale or malformed final block")
	}
	var state accountState
	err = a.rpc(ctx, ac.Network, "query", map[string]string{"request_type": "view_account", "block_id": block.Header.Hash, "account_id": ac.Address}, &state)
	if err == nil {
		// Funding may have appeared between the original query and this final
		// block. Verify its exact observation and normal authority, never return
		// the earlier missing-account response as an invented zero balance.
		if state.Hash != block.Header.Hash || state.Height.String() != height {
			return nil, errInvalid
		}
		if _, err = a.accessKey(ctx, ac, state); err != nil {
			return nil, err
		}
		return accountBalance(ac, state)
	}
	if !unknownAccount(err) {
		return nil, err
	}
	var failure *rpcError
	_ = errors.As(err, &failure)
	var absence struct {
		Account string          `json:"requested_account_id"`
		Height  json.RawMessage `json:"block_height"`
		Hash    string          `json:"block_hash"`
	}
	if err = chainutil.DecodeStrict(failure.Cause.Info, &absence); err != nil {
		return nil, err
	}
	if absence.Account != ac.Address || string(absence.Height) != height || absence.Hash != block.Header.Hash {
		return nil, errInvalid
	}
	return []model.Balance{{Asset: asset(ac.Network), Atomic: "0", State: "NOT_CREATED (final state)", At: block.Header.Hash}}, nil
}
