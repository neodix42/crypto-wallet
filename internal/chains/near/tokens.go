package near

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/decred/base58"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

const MaxImportedTokens = 32
const TokenGas = "30000000000000"
const MaxTokenStorageDeposit = "1000000000000000000000000" // 1 NEAR; registration only.

func SupportsTokenImport(network string) bool { return known(network) }
func tokenSymbol(s string) bool {
	if len(s) < 1 || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func codeHash(s string) bool {
	b := base58.Decode(s)
	return len(b) == 32 && base58.Encode(b) == s && !bytes.Equal(b, make([]byte, 32))
}
func ValidateTokenPolicy(t model.NEP141Token) error {
	if !SupportsTokenImport(t.Network) || New().ValidateAddress(t.Network, t.Asset.Contract) != nil || t.Asset.ID != t.Network+":nep141:"+t.Asset.Contract || !tokenSymbol(t.Asset.Symbol) || t.Asset.Decimals > 77 || !codeHash(t.CodeHash) {
		return errors.New("unsupported NEP-141 token policy")
	}
	return nil
}

// WithTokens copies both policy and endpoint maps so observations/signing retain
// their immutable approved network and contract identities across later imports.
func (a *Adapter) WithTokens(tokens []model.NEP141Token) (*Adapter, error) {
	if len(tokens) > MaxImportedTokens {
		return nil, errors.New("too many imported NEP-141 tokens")
	}
	out := &Adapter{endpoints: make(map[string]string, len(a.endpoints)), client: a.client}
	seen := map[string]bool{}
	for k, v := range a.endpoints {
		out.endpoints[k] = v
	}
	for _, t := range tokens {
		if ValidateTokenPolicy(t) != nil || seen[t.Asset.ID] {
			return nil, errors.New("invalid or duplicate imported NEP-141 token")
		}
		seen[t.Asset.ID] = true
	}
	out.tokens = append([]model.NEP141Token(nil), tokens...)
	return out, nil
}
func (a *Adapter) token(network, id string) (model.NEP141Token, bool) {
	for _, t := range a.tokens {
		if t.Network == network && t.Asset.ID == id {
			return t, true
		}
	}
	return model.NEP141Token{}, false
}
func (a *Adapter) networkTokens(network string) []model.NEP141Token {
	out := []model.NEP141Token{}
	for _, t := range a.tokens {
		if t.Network == network {
			out = append(out, t)
		}
	}
	return out
}

type tokenBlock struct{ Hash, Height string }

func (a *Adapter) tokenBlock(ctx context.Context, network, hash string) (tokenBlock, error) {
	var z tokenBlock
	var r struct {
		Header struct {
			Hash      string      `json:"hash"`
			Height    json.Number `json:"height"`
			Timestamp json.Number `json:"timestamp_nanosec"`
		} `json:"header"`
	}
	params := map[string]string{"finality": "final"}
	if hash != "" {
		if !blockHash(hash) {
			return z, errInvalid
		}
		params = map[string]string{"block_id": hash}
	}
	if err := a.rpc(ctx, network, "block", params, &r); err != nil {
		return z, err
	}
	if !blockHash(r.Header.Hash) || hash != "" && hash != r.Header.Hash {
		return z, errInvalid
	}
	if _, err := chainutil.Uint(r.Header.Height.String(), 64); err != nil {
		return z, err
	}
	stamp, err := chainutil.Uint(r.Header.Timestamp.String(), 64)
	if err != nil || stamp > math.MaxInt64 {
		return z, errInvalid
	}
	at := time.Unix(0, int64(stamp))
	if time.Since(at) > 2*time.Minute || time.Until(at) > time.Minute {
		return z, errors.New("NEAR stale or malformed token observation")
	}
	return tokenBlock{Hash: r.Header.Hash, Height: r.Header.Height.String()}, nil
}

// Only standard bounded read methods reach this helper. Results must identify
// the exact checked block; metadata images/reference URLs are never fetched.
func (a *Adapter) tokenView(ctx context.Context, network, contract, method, args string, block tokenBlock, max int) ([]byte, error) {
	switch method {
	case "ft_metadata", "ft_balance_of", "storage_balance_bounds", "storage_balance_of":
	default:
		return nil, errInvalid
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Hash   string          `json:"block_hash"`
		Height json.Number     `json:"block_height"`
	}
	if err := a.rpc(ctx, network, "query", map[string]string{"request_type": "call_function", "block_id": block.Hash, "account_id": contract, "method_name": method, "args_base64": base64.StdEncoding.EncodeToString([]byte(args))}, &response); err != nil {
		return nil, err
	}
	if response.Hash != block.Hash || response.Height.String() != block.Height || len(response.Result) > max*5+2 || len(response.Result) == 0 || response.Result[0] != '[' {
		return nil, errInvalid
	}
	var raw []uint8
	if chainutil.DecodeStrict(response.Result, &raw) != nil || len(raw) > max || len(raw) == 0 {
		return nil, errInvalid
	}
	return raw, nil
}
func (a *Adapter) inspectTokenAt(ctx context.Context, network, contract string, block tokenBlock) (model.NEP141Token, error) {
	var z model.NEP141Token
	var state struct {
		CodeHash string      `json:"code_hash"`
		Hash     string      `json:"block_hash"`
		Height   json.Number `json:"block_height"`
	}
	if err := a.rpc(ctx, network, "query", map[string]string{"request_type": "view_account", "block_id": block.Hash, "account_id": contract}, &state); err != nil {
		return z, err
	}
	if !codeHash(state.CodeHash) || state.Hash != block.Hash || state.Height.String() != block.Height {
		return z, errors.New("NEAR token contract code unavailable")
	}
	raw, err := a.tokenView(ctx, network, contract, "ft_metadata", "{}", block, 16384)
	if err != nil {
		return z, err
	}
	var meta struct {
		Spec          *string `json:"spec"`
		Name          *string `json:"name"`
		Symbol        *string `json:"symbol"`
		Decimals      *uint8  `json:"decimals"`
		Icon          *string `json:"icon"`
		Reference     *string `json:"reference"`
		ReferenceHash *string `json:"reference_hash"`
	}
	if chainutil.DecodeStrict(raw, &meta) != nil || meta.Spec == nil || *meta.Spec != "ft-1.0.0" || meta.Name == nil || meta.Symbol == nil || meta.Decimals == nil || len(*meta.Name) < 1 || len(*meta.Name) > 128 || strings.IndexFunc(*meta.Name, func(c rune) bool { return c < 32 || c == 127 }) >= 0 {
		return z, errors.New("unsupported NEP-148 token metadata")
	}
	if meta.Icon != nil && len(*meta.Icon) > 8192 || meta.Reference != nil && len(*meta.Reference) > 2048 || meta.ReferenceHash != nil && len(*meta.ReferenceHash) > 128 {
		return z, errInvalid
	}
	z = model.NEP141Token{Network: network, Asset: model.Asset{ID: network + ":nep141:" + contract, Contract: contract, Symbol: *meta.Symbol, Decimals: *meta.Decimals}, CodeHash: state.CodeHash}
	return z, ValidateTokenPolicy(z)
}
func (a *Adapter) InspectToken(ctx context.Context, account model.Account, contract string) (model.NEP141Token, error) {
	var z model.NEP141Token
	if a.account(account) != nil || !SupportsTokenImport(account.Network) || a.ValidateAddress(account.Network, contract) != nil {
		return z, errInvalid
	}
	if _, ok := a.token(account.Network, account.Network+":nep141:"+contract); ok {
		return z, errors.New("token is already tracked")
	}
	if err := a.identity(ctx, account.Network); err != nil {
		return z, err
	}
	block, err := a.tokenBlock(ctx, account.Network, "")
	if err != nil {
		return z, err
	}
	return a.inspectTokenAt(ctx, account.Network, contract, block)
}
func (a *Adapter) verifyTokenAt(ctx context.Context, t model.NEP141Token, block tokenBlock) error {
	got, err := a.inspectTokenAt(ctx, t.Network, t.Asset.Contract, block)
	if err != nil {
		return err
	}
	if got != t {
		return errors.New("NEAR token runtime or metadata changed; fresh exact policy approval required")
	}
	return nil
}
func (a *Adapter) tokenBalanceAt(ctx context.Context, t model.NEP141Token, account string, block tokenBlock) (string, error) {
	raw, err := a.tokenView(ctx, t.Network, t.Asset.Contract, "ft_balance_of", `{"account_id":"`+account+`"}`, block, 128)
	if err != nil {
		return "", err
	}
	var value *string
	if chainutil.DecodeStrict(raw, &value) != nil || value == nil {
		return "", errInvalid
	}
	if _, err := chainutil.Big(*value, 128); err != nil {
		return "", err
	}
	return *value, nil
}
