package near

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// StakingGas is the explicitly approved prepaid gas for the four supported
// staking-pool methods. It is serialized into the signed FunctionCall action.
const StakingGas = "100000000000000"

type StakingRequest struct {
	AccountID string `json:"accountId"`
	Validator string `json:"validator"`
	Action    string `json:"action"`
	Amount    string `json:"amount"`
}

// StakingState is a final-block provider observation. get_account includes
// compounded rewards in Staked, but cannot identify rewards separately.
type StakingState struct {
	AccountID   string `json:"accountId"`
	Validator   string `json:"validator"`
	Staked      string `json:"staked"`
	Unstaked    string `json:"unstaked"`
	CanWithdraw bool   `json:"canWithdraw"`
	BlockHash   string `json:"blockHash"`
	BlockHeight string `json:"blockHeight"`
	Rewards     string `json:"rewards,omitempty"`
}

// StakingCall is a typed allowlisted operation, never arbitrary contract data.
// Amount in the enclosing plan describes the staking effect, while Deposit is
// the actual signed attached NEAR (zero for every unstake/withdraw operation).
type StakingCall struct {
	Method  string `json:"method"`
	Args    string `json:"args"`
	Deposit string `json:"deposit"`
	Gas     string `json:"gas"`
}

// ValidateStakingPool limits operations to direct children of reviewed staking
// factory namespaces. The sandbox fixture is unavailable on either live ledger.
// A namespace and a compatible response are not an audit of a pool's code.
func ValidateStakingPool(network, pool string) error {
	if New().ValidateAddress(network, pool) != nil {
		return errInvalid
	}
	suffixes := []string{}
	switch network {
	case "near-mainnet":
		suffixes = []string{".poolv1.near", ".pool.near"}
	case "near-testnet":
		suffixes = []string{".pool.f863973.m0"}
	case "near-localnet":
		if pool == "pool.sandbox" {
			return nil
		}
	}
	for _, suffix := range suffixes {
		label := strings.TrimSuffix(pool, suffix)
		if label != pool && label != "" && !strings.Contains(label, ".") {
			return nil
		}
	}
	return errors.New("NEAR staking pool is outside the supported network factory namespaces")
}

func (a *Adapter) stakingAt(ctx context.Context, ac model.Account, pool, hash, height string) (StakingState, error) {
	var z StakingState
	args, _ := json.Marshal(struct {
		Account string `json:"account_id"`
	}{ac.Address})
	var response struct {
		Result json.RawMessage `json:"result"`
		Hash   string          `json:"block_hash"`
		Height json.Number     `json:"block_height"`
	}
	if err := a.rpc(ctx, ac.Network, "query", map[string]string{"request_type": "call_function", "block_id": hash, "account_id": pool, "method_name": "get_account", "args_base64": base64.StdEncoding.EncodeToString(args)}, &response); err != nil {
		return z, err
	}
	if response.Hash != hash || response.Height.String() != height || len(response.Result) > 8192 || len(response.Result) == 0 || response.Result[0] != '[' {
		return z, errInvalid
	}
	var result []uint8
	if err := chainutil.DecodeStrict(response.Result, &result); err != nil || len(result) > 1024 {
		return z, errInvalid
	}
	// Pointers make absent/null fields distinguishable from verified zero/false.
	var observed struct {
		Account     *string `json:"account_id"`
		Unstaked    *string `json:"unstaked_balance"`
		Staked      *string `json:"staked_balance"`
		CanWithdraw *bool   `json:"can_withdraw"`
	}
	if err := chainutil.DecodeStrict(result, &observed); err != nil {
		return z, err
	}
	if observed.Account == nil || observed.Unstaked == nil || observed.Staked == nil || observed.CanWithdraw == nil || *observed.Account != ac.Address {
		return z, errInvalid
	}
	staked, err := chainutil.Big(*observed.Staked, 128)
	if err != nil {
		return z, err
	}
	unstaked, err := chainutil.Big(*observed.Unstaked, 128)
	if err != nil {
		return z, err
	}
	if new(big.Int).Add(staked, unstaked).BitLen() > 128 {
		return z, errInvalid
	}
	return StakingState{AccountID: ac.Address, Validator: pool, Staked: *observed.Staked, Unstaked: *observed.Unstaked, CanWithdraw: *observed.CanWithdraw, BlockHash: hash, BlockHeight: height}, nil
}

// StakingState reads only the selected account's standard staking-pool state.
func (a *Adapter) StakingState(ctx context.Context, ac model.Account, pool string) (StakingState, error) {
	if err := ValidateStakingPool(ac.Network, pool); err != nil {
		return StakingState{}, err
	}
	st, _, err := a.state(ctx, ac)
	if err != nil {
		return StakingState{}, err
	}
	return a.stakingAt(ctx, ac, pool, st.Hash, st.Height.String())
}

func requestedCall(request StakingRequest, state StakingState) (StakingCall, string, error) {
	call := StakingCall{Args: "{}", Deposit: "0", Gas: StakingGas}
	effect := request.Amount
	switch request.Action {
	case "stake":
		call.Method, call.Deposit = "deposit_and_stake", request.Amount
	case "unstake":
		call.Method = "unstake"
		call.Args = `{"amount":"` + request.Amount + `"}`
	case "unstake_all":
		call.Method, effect = "unstake_all", state.Staked
	case "withdraw_all":
		call.Method, effect = "withdraw_all", state.Unstaked
	default:
		return call, "", errInvalid
	}
	all := request.Action == "unstake_all" || request.Action == "withdraw_all"
	if all && request.Amount != "" {
		return call, "", errInvalid
	}
	value, err := chainutil.Big(effect, 128)
	if err != nil || value.Sign() == 0 {
		return call, "", errInvalid
	}
	if request.Action == "unstake" {
		staked, _ := chainutil.Big(state.Staked, 128)
		if value.Cmp(staked) > 0 {
			return call, "", errors.New("insufficient staked NEAR")
		}
	}
	if request.Action == "withdraw_all" && !state.CanWithdraw {
		return call, "", errors.New("NEAR unstaked balance is still locked by the pool")
	}
	return call, effect, nil
}

func (a *Adapter) PrepareStaking(ctx context.Context, ac model.Account, request StakingRequest) (model.Plan, error) {
	var z model.Plan
	if request.AccountID != ac.ID || ValidateStakingPool(ac.Network, request.Validator) != nil {
		return z, errInvalid
	}
	// Reject non-allowlisted intent before any public account query.
	if request.Action != "stake" && request.Action != "unstake" && request.Action != "unstake_all" && request.Action != "withdraw_all" {
		return z, errInvalid
	}
	all := request.Action == "unstake_all" || request.Action == "withdraw_all"
	if all {
		if request.Amount != "" {
			return z, errInvalid
		}
	} else if value, err := chainutil.Big(request.Amount, 128); err != nil || value.Sign() == 0 {
		return z, errInvalid
	}
	st, key, err := a.state(ctx, ac)
	if err != nil {
		return z, err
	}
	state, err := a.stakingAt(ctx, ac, request.Validator, st.Hash, st.Height.String())
	if err != nil {
		return z, err
	}
	call, effect, err := requestedCall(request, state)
	if err != nil {
		return z, err
	}
	nonce, err := chainutil.Uint(key.Nonce.String(), 64)
	if err != nil || nonce == math.MaxUint64 {
		return z, errInvalid
	}
	var cfg protocol
	if err = a.rpc(ctx, ac.Network, "EXPERIMENTAL_protocol_config", map[string]string{"block_id": st.Hash}, &cfg); err != nil {
		return z, err
	}
	if cfg.ChainID != chain(ac.Network) {
		return z, errInvalid
	}
	var gp struct {
		Price string `json:"gas_price"`
	}
	if err = a.rpc(ctx, ac.Network, "gas_price", []string{st.Hash}, &gp); err != nil {
		return z, err
	}
	price, err := chainutil.Big(gp.Price, 128)
	if err != nil || price.Sign() == 0 {
		return z, errInvalid
	}
	if cfg.Runtime.MinPrice != "" {
		minimum, err := chainutil.Big(cfg.Runtime.MinPrice, 128)
		if err != nil {
			return z, err
		}
		if price.Cmp(minimum) < 0 {
			price = minimum
		}
	}
	gas, _ := chainutil.Big(StakingGas, 64)
	for index, c := range []cost{cfg.Runtime.Costs.Receipt, cfg.Runtime.Costs.Actions.FunctionCall, cfg.Runtime.Costs.Actions.FunctionCallByte} {
		for _, raw := range []string{c.Execution.String(), c.Send.String()} {
			n, err := chainutil.Big(raw, 64)
			if err != nil {
				return z, err
			}
			if index == 2 {
				n.Mul(n, big.NewInt(int64(len(call.Method)+len(call.Args))))
			}
			gas.Add(gas, n)
		}
	}
	if gas.Cmp(big.NewInt(110_000_000_000_000)) > 0 {
		return z, errInvalid
	}
	fee := new(big.Int).Mul(gas, price)
	fee.Mul(fee, big.NewInt(2))
	storage, err := chainutil.Big(cfg.Runtime.Storage, 128)
	if err != nil {
		return z, err
	}
	usage, err := chainutil.Big(st.Storage.String(), 64)
	if err != nil {
		return z, err
	}
	reserve := new(big.Int).Mul(storage, usage)
	balance, err := chainutil.Big(st.Amount, 128)
	if err != nil {
		return z, err
	}
	deposit, _ := chainutil.Big(call.Deposit, 128)
	required := new(big.Int).Add(deposit, fee)
	required.Add(required, reserve)
	if required.BitLen() > 128 || balance.Cmp(required) < 0 {
		return z, errors.New("insufficient spendable NEAR including storage and staking fee reserve")
	}
	now := time.Now().Unix()
	d := Payload{Version: 1, BlockHash: st.Hash, BlockHeight: st.Height.String(), Protocol: cfg.Version.String(), Gas: gas.String(), GasPrice: price.String(), StorageReserve: reserve.String(), RecipientExists: true, Prepared: now, FeePolicy: "protocol-assessed fee; estimate reserve is not a signed fee cap", Staking: &call}
	payload, _ := json.Marshal(d)
	z = model.Plan{Account: ac, Asset: asset(ac.Network), FeeAsset: asset(ac.Network), Recipient: request.Validator, Amount: effect, MaxFee: fee.String(), Nonce: strconv.FormatUint(nonce+1, 10), Expires: now + 60, Payload: payload}
	return z, a.ValidatePlan(z)
}

func validateStaking(p model.Plan, d Payload) error {
	call := d.Staking
	if call == nil || ValidateStakingPool(p.Account.Network, p.Recipient) != nil || !d.RecipientExists || call.Gas != StakingGas {
		return errInvalid
	}
	gas, err := chainutil.Uint(d.Gas, 64)
	if err != nil || gas < 100_000_000_000_000 || gas > 110_000_000_000_000 {
		return errInvalid
	}
	deposit, err := chainutil.Big(call.Deposit, 128)
	if err != nil {
		return err
	}
	value, err := chainutil.Big(p.Amount, 128)
	if err != nil || value.Sign() == 0 {
		return errInvalid
	}
	switch call.Method {
	case "deposit_and_stake":
		if deposit.String() != p.Amount || call.Args != "{}" {
			return errInvalid
		}
	case "unstake":
		if deposit.Sign() != 0 || call.Args != `{"amount":"`+p.Amount+`"}` {
			return errInvalid
		}
	case "unstake_all", "withdraw_all":
		if deposit.Sign() != 0 || call.Args != "{}" {
			return errInvalid
		}
	default:
		return errInvalid
	}
	return nil
}

func IsStakingPlan(p model.Plan) bool {
	var d Payload
	return chainutil.DecodeStrict(p.Payload, &d) == nil && d.Staking != nil
}

// StakingEffects supplies signer-authored operation details after validating the
// complete plan. All-operation amounts are explicitly observations, not limits.
func StakingEffects(p model.Plan) (string, bool) {
	if New().ValidatePlan(p) != nil {
		return "", false
	}
	var d Payload
	_ = json.Unmarshal(p.Payload, &d)
	if d.Staking == nil {
		return "", false
	}
	call := d.Staking
	deposit, _ := amount.Format(call.Deposit, 24)
	effect, _ := amount.Format(p.Amount, 24)
	action := "Deposit and stake " + effect + " NEAR; funds leave the spendable wallet balance."
	switch call.Method {
	case "unstake":
		action = "Unstake " + effect + " NEAR; funds remain in the pool until withdrawal is available."
	case "unstake_all":
		action = "Unstake ALL staked funds; observed " + effect + " NEAR. The executed amount can change with rewards."
	case "withdraw_all":
		action = "Withdraw ALL available unstaked funds to " + p.Account.Address + "; observed " + effect + " NEAR. The executed amount can change."
	}
	return fmt.Sprintf("NEAR staking pool contract: %s\nMethod: %s\nArguments: %s\nAttached NEAR: %s NEAR (%s yoctoNEAR)\nPrepaid gas: %s gas (100 Tgas)\nExpected action: %s\nUnstaking normally requires four epochs; the pool determines actual withdrawal availability.\nFee is a protocol-assessed estimate reserve, not a signed maximum fee. A successful call does not prove validator performance or future rewards.", p.Recipient, call.Method, call.Args, deposit, call.Deposit, call.Gas, action), true
}

type receiptOutcome struct {
	ID      string `json:"id"`
	Block   string `json:"block_hash"`
	Outcome struct {
		Executor   string          `json:"executor_id"`
		Status     json.RawMessage `json:"status"`
		ReceiptIDs []string        `json:"receipt_ids"`
		Logs       []string        `json:"logs"`
	} `json:"outcome"`
}

// Pool methods can return successfully while an unreturned child Stake or
// Transfer promise fails. Never interpret that root result as proven staking or
// withdrawal execution. UNKNOWN retains the durable original reservation.
func stakingReceipt(p model.Plan, id string, result execution) (model.Receipt, error) {
	z, err := receipt(id, result)
	if err != nil || z.State != "CONFIRMED" {
		return z, err
	}
	z.State = "UNKNOWN"
	z.Detail = "Final staking-pool call committed; downstream execution and funds movement have not been verified. Reconcile this original operation and inspect pool and wallet state before any new payment."
	var rootStatus struct {
		Value *string `json:"SuccessValue"`
	}
	if chainutil.DecodeStrict(result.Status, &rootStatus) != nil || rootStatus.Value == nil {
		return z, nil
	}
	rootValue, rootErr := base64.StdEncoding.DecodeString(*rootStatus.Value)
	if rootErr != nil || len(rootValue) > 1024 || base64.StdEncoding.EncodeToString(rootValue) != *rootStatus.Value {
		return z, nil
	}
	if len(result.Receipts) == 0 || len(result.Receipts) > 128 || !blockHash(result.Outcome.Block) || len(result.Outcome.Outcome.ReceiptIDs) == 0 || len(result.Outcome.Outcome.ReceiptIDs) > 128 {
		return z, nil
	}
	seen := make(map[string]bool, len(result.Receipts))
	children := make(map[string][]string, len(result.Receipts))
	references := append([]string(nil), result.Outcome.Outcome.ReceiptIDs...)
	poolSeen := false
	for _, receipt := range result.Receipts {
		if !blockHash(receipt.ID) || !blockHash(receipt.Block) || seen[receipt.ID] || New().ValidateAddress(p.Account.Network, receipt.Outcome.Executor) != nil {
			return z, nil
		}
		seen[receipt.ID] = true
		if receipt.Outcome.ReceiptIDs == nil || len(receipt.Outcome.ReceiptIDs) > 128 {
			return z, nil
		}
		children[receipt.ID] = receipt.Outcome.ReceiptIDs
		references = append(references, receipt.Outcome.ReceiptIDs...)
		var status map[string]json.RawMessage
		if chainutil.DecodeStrict(receipt.Outcome.Status, &status) != nil || len(status) != 1 {
			return z, nil
		}
		if failure, ok := status["Failure"]; ok {
			if len(failure) > 0 {
				z.Detail = "Final staking-pool call committed, but a downstream receipt failed. Staking or withdrawal funds movement is not confirmed. Do not automatically repeat this operation; reconcile its original identity and inspect pool and wallet state."
			}
			return z, nil
		}
		if raw, ok := status["SuccessValue"]; ok {
			var value *string
			if json.Unmarshal(raw, &value) != nil || value == nil || len(*value) > 2048 {
				return z, nil
			}
			decoded, err := base64.StdEncoding.DecodeString(*value)
			if err != nil || base64.StdEncoding.EncodeToString(decoded) != *value {
				return z, nil
			}
		} else if raw, ok := status["SuccessReceiptId"]; ok {
			var next *string
			if json.Unmarshal(raw, &next) != nil || next == nil || !blockHash(*next) {
				return z, nil
			}
			references = append(references, *next)
			children[receipt.ID] = append(children[receipt.ID], *next)
		} else {
			return z, nil
		}
		if receipt.Outcome.Executor == p.Recipient {
			poolSeen = true
		}
	}
	if !poolSeen {
		return z, nil
	}
	for _, reference := range references {
		if !seen[reference] {
			return z, nil
		}
	}
	// Receipt dependencies cannot form a cycle. A visited-only traversal would
	// accept a self-reference or back edge as a complete successful execution.
	reachable, visiting := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(current string) bool {
		if visiting[current] {
			return false
		}
		if reachable[current] {
			return true
		}
		visiting[current] = true
		for _, child := range children[current] {
			if !visit(child) {
				return false
			}
		}
		delete(visiting, current)
		reachable[current] = true
		return true
	}
	for _, root := range result.Outcome.Outcome.ReceiptIDs {
		if !visit(root) {
			return z, nil
		}
	}
	if len(reachable) != len(seen) {
		return z, nil
	}
	z.State = "CONFIRMED"
	z.Detail = "Final staking-pool call and all reported downstream receipts succeeded. Refresh pool and wallet balances; validator performance and future rewards are not guaranteed."
	return z, nil
}
