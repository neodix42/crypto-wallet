package near

import (
	"bytes"
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

// TokenCall has only two supported standard methods. The enclosing plan keeps
// the logical recipient; the signed NEAR transaction targets Policy.Contract.
type TokenCall struct {
	Policy  model.NEP141Token `json:"policy"`
	Method  string            `json:"method"`
	Args    string            `json:"args"`
	Deposit string            `json:"deposit"`
	Gas     string            `json:"gas"`
}
type TokenStorageState struct {
	Network     string `json:"network"`
	Asset       string `json:"asset"`
	Contract    string `json:"contract"`
	AccountID   string `json:"accountId"`
	Registered  bool   `json:"registered"`
	MinDeposit  string `json:"minDeposit"`
	Total       string `json:"total"`
	Available   string `json:"available"`
	BlockHash   string `json:"blockHash"`
	BlockHeight string `json:"blockHeight"`
}

func storageBalance(raw []byte) (total, available string, registered bool, err error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "0", "0", false, nil
	}
	var value struct {
		Total     *string `json:"total"`
		Available *string `json:"available"`
	}
	if chainutil.DecodeStrict(raw, &value) != nil || value.Total == nil || value.Available == nil {
		return "", "", false, errInvalid
	}
	t, e := chainutil.Big(*value.Total, 128)
	if e != nil {
		return "", "", false, e
	}
	av, e := chainutil.Big(*value.Available, 128)
	if e != nil || av.Cmp(t) > 0 {
		return "", "", false, errInvalid
	}
	return *value.Total, *value.Available, true, nil
}
func (a *Adapter) storageAt(ctx context.Context, policy model.NEP141Token, account string, block tokenBlock) (TokenStorageState, error) {
	z := TokenStorageState{Network: policy.Network, Asset: policy.Asset.ID, Contract: policy.Asset.Contract, AccountID: account, BlockHash: block.Hash, BlockHeight: block.Height}
	raw, err := a.tokenView(ctx, policy.Network, policy.Asset.Contract, "storage_balance_bounds", "{}", block, 1024)
	if err != nil {
		return z, err
	}
	var bounds struct {
		Min *string         `json:"min"`
		Max json.RawMessage `json:"max"`
	}
	if chainutil.DecodeStrict(raw, &bounds) != nil || bounds.Min == nil || len(bounds.Max) == 0 {
		return z, errInvalid
	}
	minimum, err := chainutil.Big(*bounds.Min, 128)
	cap, _ := chainutil.Big(MaxTokenStorageDeposit, 128)
	if err != nil || minimum.Sign() == 0 || minimum.Cmp(cap) > 0 {
		return z, errors.New("NEAR token storage minimum is unsupported (maximum one NEAR)")
	}
	if !bytes.Equal(bytes.TrimSpace(bounds.Max), []byte("null")) {
		var max *string
		if chainutil.DecodeStrict(bounds.Max, &max) != nil || max == nil {
			return z, errInvalid
		}
		n, e := chainutil.Big(*max, 128)
		if e != nil || n.Cmp(minimum) < 0 {
			return z, errInvalid
		}
	}
	raw, err = a.tokenView(ctx, policy.Network, policy.Asset.Contract, "storage_balance_of", `{"account_id":"`+account+`"}`, block, 1024)
	if err != nil {
		return z, err
	}
	z.Total, z.Available, z.Registered, err = storageBalance(raw)
	if err != nil {
		return z, err
	}
	if z.Registered {
		total, _ := chainutil.Big(z.Total, 128)
		if total.Cmp(minimum) < 0 {
			return z, errInvalid
		}
	}
	z.MinDeposit = minimum.String()
	return z, nil
}

// TokenStorage is read-only, including for a not-yet-created implicit wallet.
// Registration still requires a funded existing account and its full-access key.
func (a *Adapter) TokenStorage(ctx context.Context, account model.Account, assetID string) (TokenStorageState, error) {
	policy, ok := a.token(account.Network, assetID)
	if !ok || a.account(account) != nil {
		return TokenStorageState{}, errInvalid
	}
	if err := a.identity(ctx, account.Network); err != nil {
		return TokenStorageState{}, err
	}
	block, err := a.tokenBlock(ctx, account.Network, "")
	if err != nil {
		return TokenStorageState{}, err
	}
	if err = a.verifyTokenAt(ctx, policy, block); err != nil {
		return TokenStorageState{}, err
	}
	return a.storageAt(ctx, policy, account.Address, block)
}
func (a *Adapter) prepareTokenTransfer(ctx context.Context, account model.Account, t model.Transfer) (model.Plan, error) {
	policy, ok := a.token(account.Network, t.Asset)
	if !ok || t.AccountID != account.ID || t.Memo != "" || t.Recipient == policy.Asset.Contract || t.Recipient == account.Address || a.ValidateAddress(account.Network, t.Recipient) != nil {
		return model.Plan{}, errInvalid
	}
	value, err := chainutil.Big(t.Amount, 128)
	if err != nil || value.Sign() == 0 {
		return model.Plan{}, errInvalid
	}
	st, key, err := a.state(ctx, account)
	if err != nil {
		return model.Plan{}, err
	}
	block := tokenBlock{Hash: st.Hash, Height: st.Height.String()}
	if err = a.verifyTokenAt(ctx, policy, block); err != nil {
		return model.Plan{}, err
	}
	if err = a.checkTransferState(ctx, policy, account.Address, t.Recipient, t.Amount, block); err != nil {
		return model.Plan{}, err
	}
	call := TokenCall{Policy: policy, Method: "ft_transfer", Args: `{"receiver_id":"` + t.Recipient + `","amount":"` + t.Amount + `"}`, Gas: TokenGas, Deposit: "1"}
	return a.prepareTokenCall(ctx, account, t.Asset, t.Recipient, t.Amount, call, st, key)
}
func (a *Adapter) checkTransferState(ctx context.Context, policy model.NEP141Token, sender, receiver, value string, block tokenBlock) error {
	for _, entry := range []struct{ account, role string }{{sender, "sender"}, {receiver, "recipient"}} {
		storage, err := a.storageAt(ctx, policy, entry.account, block)
		if err != nil {
			return err
		}
		if !storage.Registered {
			return fmt.Errorf("NEAR token %s is not registered with this exact contract; register before transferring", entry.role)
		}
	}
	balance, err := a.tokenBalanceAt(ctx, policy, sender, block)
	if err != nil {
		return err
	}
	available, _ := chainutil.Big(balance, 128)
	required, _ := chainutil.Big(value, 128)
	if required == nil || available.Cmp(required) < 0 {
		return errors.New("insufficient NEP-141 token balance")
	}
	return nil
}
func (a *Adapter) PrepareTokenRegistration(ctx context.Context, account model.Account, assetID string) (model.Plan, error) {
	policy, ok := a.token(account.Network, assetID)
	if !ok {
		return model.Plan{}, errInvalid
	}
	st, key, err := a.state(ctx, account)
	if err != nil {
		return model.Plan{}, err
	}
	block := tokenBlock{Hash: st.Hash, Height: st.Height.String()}
	if err = a.verifyTokenAt(ctx, policy, block); err != nil {
		return model.Plan{}, err
	}
	storage, err := a.storageAt(ctx, policy, account.Address, block)
	if err != nil {
		return model.Plan{}, err
	}
	if storage.Registered {
		return model.Plan{}, errors.New("owned wallet is already registered for this NEAR token")
	}
	call := TokenCall{Policy: policy, Method: "storage_deposit", Args: `{"account_id":"` + account.Address + `","registration_only":true}`, Gas: TokenGas, Deposit: storage.MinDeposit}
	return a.prepareTokenCall(ctx, account, asset(account.Network), account.Address, storage.MinDeposit, call, st, key)
}
func (a *Adapter) prepareTokenCall(ctx context.Context, account model.Account, assetID, recipient, value string, call TokenCall, st accountState, key keyState) (model.Plan, error) {
	var z model.Plan
	nonce, err := chainutil.Uint(key.Nonce.String(), 64)
	if err != nil || nonce == math.MaxUint64 {
		return z, errInvalid
	}
	var cfg protocol
	if err = a.rpc(ctx, account.Network, "EXPERIMENTAL_protocol_config", map[string]string{"block_id": st.Hash}, &cfg); err != nil {
		return z, err
	}
	if cfg.ChainID != chain(account.Network) {
		return z, errInvalid
	}
	var gp struct {
		Price string `json:"gas_price"`
	}
	if err = a.rpc(ctx, account.Network, "gas_price", []string{st.Hash}, &gp); err != nil {
		return z, err
	}
	price, err := chainutil.Big(gp.Price, 128)
	if err != nil || price.Sign() == 0 {
		return z, errInvalid
	}
	if cfg.Runtime.MinPrice != "" {
		minimum, e := chainutil.Big(cfg.Runtime.MinPrice, 128)
		if e != nil {
			return z, e
		}
		if price.Cmp(minimum) < 0 {
			price = minimum
		}
	}
	gas, _ := chainutil.Big(TokenGas, 64)
	for index, c := range []cost{cfg.Runtime.Costs.Receipt, cfg.Runtime.Costs.Actions.FunctionCall, cfg.Runtime.Costs.Actions.FunctionCallByte} {
		for _, raw := range []string{c.Execution.String(), c.Send.String()} {
			n, e := chainutil.Big(raw, 64)
			if e != nil {
				return z, e
			}
			if index == 2 {
				n.Mul(n, big.NewInt(int64(len(call.Method)+len(call.Args))))
			}
			gas.Add(gas, n)
		}
	}
	if gas.Cmp(big.NewInt(40_000_000_000_000)) > 0 {
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
	deposit, err := chainutil.Big(call.Deposit, 128)
	if err != nil {
		return z, err
	}
	required := new(big.Int).Add(deposit, fee)
	required.Add(required, reserve)
	if required.BitLen() > 128 || balance.Cmp(required) < 0 {
		return z, errors.New("insufficient spendable NEAR for token deposit, native gas and account storage reserve")
	}
	now := time.Now().Unix()
	d := Payload{Version: 1, BlockHash: st.Hash, BlockHeight: st.Height.String(), Protocol: cfg.Version.String(), Gas: gas.String(), GasPrice: price.String(), StorageReserve: reserve.String(), RecipientExists: true, Prepared: now, FeePolicy: "protocol-assessed fee; estimate reserve is not a signed fee cap", Token: &call}
	payload, _ := json.Marshal(d)
	z = model.Plan{Account: account, Asset: assetID, FeeAsset: asset(account.Network), Recipient: recipient, Amount: value, MaxFee: fee.String(), Nonce: strconv.FormatUint(nonce+1, 10), Expires: now + 60, Payload: payload}
	return z, a.ValidatePlan(z)
}
func validateToken(p model.Plan, d Payload) error {
	call := d.Token
	if call == nil || d.Staking != nil || ValidateTokenPolicy(call.Policy) != nil || call.Policy.Network != p.Account.Network || call.Gas != TokenGas || !d.RecipientExists || p.Memo != "" || p.FeeAsset != asset(p.Account.Network) {
		return errInvalid
	}
	gas, err := chainutil.Uint(d.Gas, 64)
	if err != nil || gas < 30_000_000_000_000 || gas > 40_000_000_000_000 {
		return errInvalid
	}
	switch call.Method {
	case "ft_transfer":
		if p.Asset != call.Policy.Asset.ID || p.Recipient == call.Policy.Asset.Contract || p.Recipient == p.Account.Address || call.Deposit != "1" || call.Args != `{"receiver_id":"`+p.Recipient+`","amount":"`+p.Amount+`"}` {
			return errInvalid
		}
	case "storage_deposit":
		deposit, err := chainutil.Big(call.Deposit, 128)
		cap, _ := chainutil.Big(MaxTokenStorageDeposit, 128)
		if err != nil || deposit.Sign() == 0 || deposit.Cmp(cap) > 0 || p.Asset != asset(p.Account.Network) || p.Recipient != p.Account.Address || p.Amount != call.Deposit || call.Args != `{"account_id":"`+p.Account.Address+`","registration_only":true}` {
			return errInvalid
		}
	default:
		return errInvalid
	}
	return nil
}
func IsTokenPlan(p model.Plan) bool {
	var d Payload
	return chainutil.DecodeStrict(p.Payload, &d) == nil && d.Token != nil
}
func IsTokenRegistrationPlan(p model.Plan) bool {
	var d Payload
	return chainutil.DecodeStrict(p.Payload, &d) == nil && d.Token != nil && d.Token.Method == "storage_deposit"
}

// TokenAsset returns the exact policy asset after complete allowlisted plan
// validation, including registration plans whose spending asset is native NEAR.
func TokenAsset(p model.Plan) (model.Asset, bool) {
	d, valid := tokenDetails(p)
	if !valid {
		return model.Asset{}, false
	}
	return d.Token.Policy.Asset, true
}

// Historical descriptions validate the original fixed lifetime and all signed
// fields, then shift only the validation clock. This never authorizes signing
// or submission; those still call ValidatePlan with the unchanged current time.
func tokenDetails(p model.Plan) (Payload, bool) {
	var d Payload
	if chainutil.DecodeStrict(p.Payload, &d) != nil || d.Token == nil {
		return d, false
	}
	if d.Prepared <= 0 || d.Prepared > math.MaxInt64-60 || p.Expires != d.Prepared+60 {
		return d, false
	}
	a, err := New().WithTokens([]model.NEP141Token{d.Token.Policy})
	if err != nil {
		return d, false
	}
	current := d
	current.Prepared = time.Now().Unix()
	check := p
	check.Expires = current.Prepared + 60
	check.Payload, _ = json.Marshal(current)
	if a.ValidatePlan(check) != nil {
		return d, false
	}
	return d, true
}
func TokenEffects(p model.Plan) (string, bool) {
	d, valid := tokenDetails(p)
	if !valid {
		return "", false
	}
	call := d.Token
	deposit, _ := amount.Format(call.Deposit, 24)
	action := "Transfer exactly " + p.Amount + " atomic token units to " + p.Recipient + ". No receiver callback or allowance is authorized."
	if call.Method == "storage_deposit" {
		action = "Register only your own wallet address " + p.Account.Address + " for receiving this exact token. This spends NEAR for contract storage and transfers no tokens. No unregister or storage withdrawal is authorized."
	}
	return fmt.Sprintf("NEAR NEP-141 token contract: %s\nToken: %s; decimals: %d\nPinned code hash: %s\nMethod: %s\nArguments: %s\nAttached NEAR: %s NEAR (%s yoctoNEAR)\nPrepaid gas: %s gas (30 Tgas)\nExpected action: %s\nGas is paid in native NEAR. The protocol-assessed fee reserve is not a signed maximum fee. Contract identity and code pinning do not prove issuer safety.", call.Policy.Asset.Contract, call.Policy.Asset.Symbol, call.Policy.Asset.Decimals, call.Policy.CodeHash, call.Method, call.Args, deposit, call.Deposit, call.Gas, action), true
}
func (a *Adapter) tokenPreflight(ctx context.Context, p model.Plan, d Payload, current accountState) error {
	policy := d.Token.Policy
	block := tokenBlock{Hash: current.Hash, Height: current.Height.String()}
	if err := a.verifyTokenAt(ctx, policy, block); err != nil {
		return err
	}
	if d.Token.Method == "ft_transfer" {
		if err := a.checkTransferState(ctx, policy, p.Account.Address, p.Recipient, p.Amount, block); err != nil {
			return err
		}
	} else {
		storage, err := a.storageAt(ctx, policy, p.Account.Address, block)
		if err != nil {
			return err
		}
		if storage.Registered || storage.MinDeposit != d.Token.Deposit {
			return errors.New("NEAR token registration state or deposit changed; fresh approval required")
		}
	}
	balance, err := chainutil.Big(current.Amount, 128)
	if err != nil {
		return err
	}
	deposit, _ := chainutil.Big(d.Token.Deposit, 128)
	fee, _ := chainutil.Big(p.MaxFee, 128)
	reserve, _ := chainutil.Big(d.StorageReserve, 128)
	required := new(big.Int).Add(deposit, fee)
	required.Add(required, reserve)
	if required.BitLen() > 128 || balance.Cmp(required) < 0 {
		return errors.New("insufficient current native NEAR for approved token call")
	}
	return nil
}
func tokenReceipt(p model.Plan, id string, r execution) (model.Receipt, error) {
	var d Payload
	if chainutil.DecodeStrict(p.Payload, &d) != nil || d.Token == nil {
		return model.Receipt{Identity: id, State: "UNKNOWN"}, errInvalid
	}
	actual := p
	actual.Recipient = d.Token.Policy.Asset.Contract
	if r.Transaction.Signer != p.Account.Address || r.Transaction.Receiver != actual.Recipient {
		return model.Receipt{Identity: id, State: "UNKNOWN"}, errInvalid
	}
	observed, err := stakingReceipt(actual, id, r)
	observed.Detail = strings.ReplaceAll(strings.ReplaceAll(observed.Detail, "staking-pool", "token-contract"), "Staking or withdrawal funds movement", "Token transfer or storage registration")
	if err != nil || observed.State != "CONFIRMED" {
		return observed, err
	}
	var root struct {
		Value *string `json:"SuccessValue"`
	}
	if chainutil.DecodeStrict(r.Status, &root) != nil || root.Value == nil {
		observed.State = "UNKNOWN"
		return observed, nil
	}
	raw, err := base64.StdEncoding.DecodeString(*root.Value)
	if err != nil {
		observed.State = "UNKNOWN"
		return observed, nil
	}
	if d.Token.Method == "ft_transfer" {
		if len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			observed.State = "UNKNOWN"
			observed.Detail = "Token contract returned a nonstandard transfer result; reconcile the original transaction and inspect token balances."
			return observed, nil
		}
		if !tokenTransferEvent(p, d.Token.Policy.Asset.Contract, r.Receipts) {
			observed.State = "UNKNOWN"
			observed.Detail = "Final token call lacks the exact standard transfer event; reconcile the original transaction and inspect token balances."
			return observed, nil
		}
	} else {
		total, _, registered, e := storageBalance(raw)
		n, _ := chainutil.Big(total, 128)
		deposit, _ := chainutil.Big(d.Token.Deposit, 128)
		if e != nil || !registered || n == nil || n.Cmp(deposit) < 0 {
			observed.State = "UNKNOWN"
			observed.Detail = "Token storage registration result is unavailable or nonstandard; reconcile the original operation."
			return observed, nil
		}
	}
	observed.Detail = "Final exact NEP-141/NEP-145 call and all reported receipts succeeded. Refresh token balances and registration; no arbitrary contract call was authorized."
	return observed, nil
}

// NEP-297/NEP-141 logs must bind the emitting contract, original sender,
// approved recipient and canonical u128 amount. A successful call alone is not
// sufficient evidence of a fungible-token transfer.
func tokenTransferEvent(p model.Plan, contract string, receipts []receiptOutcome) bool {
	matches, logs := 0, 0
	for _, outcome := range receipts {
		if len(outcome.Outcome.Logs) > 64 {
			return false
		}
		for _, line := range outcome.Outcome.Logs {
			logs++
			if logs > 256 || len(line) > 16384 {
				return false
			}
			if outcome.Outcome.Executor != contract || !strings.HasPrefix(line, "EVENT_JSON:") {
				continue
			}
			var event struct {
				Standard string `json:"standard"`
				Version  string `json:"version"`
				Event    string `json:"event"`
				Data     []struct {
					Old    string  `json:"old_owner_id"`
					New    string  `json:"new_owner_id"`
					Amount string  `json:"amount"`
					Memo   *string `json:"memo,omitempty"`
				} `json:"data"`
			}
			if chainutil.DecodeStrict([]byte(strings.TrimSpace(strings.TrimPrefix(line, "EVENT_JSON:"))), &event) != nil {
				return false
			}
			if event.Standard != "nep141" || event.Version != "1.0.0" || event.Event != "ft_transfer" || len(event.Data) != 1 {
				return false
			}
			item := event.Data[0]
			if item.Old != p.Account.Address || item.New != p.Recipient || item.Amount != p.Amount || item.Memo != nil {
				return false
			}
			matches++
		}
	}
	return matches == 1
}
