package evm

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"localwallet/internal/model"
)

const MaxImportedTokens = 32

// SupportsTokenImport is intentionally narrower than EVM compatibility. Unknown
// chain identities never inherit ERC-20 approval or discovery authority.
func SupportsTokenImport(network string) bool {
	switch network {
	case "ethereum-mainnet", "ethereum-sepolia", "base-mainnet", "base-sepolia", "arbitrum-mainnet", "arbitrum-sepolia":
		return true
	}
	return false
}

func IsPinnedAsset(networkID, assetID string) bool {
	for _, network := range registry() {
		if network.ID == networkID {
			for _, asset := range network.Assets {
				if asset.ID == assetID {
					return true
				}
			}
		}
	}
	return false
}

func tokenSymbol(symbol string) bool {
	if len(symbol) < 1 || len(symbol) > 24 {
		return false
	}
	for _, c := range symbol {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ValidateStoredTokenPolicy does not vouch for an issuer. Contract + network are its
// identity; the untrusted symbol is only bounded display metadata.
func ValidateStoredTokenPolicy(token model.ERC20Token) error {
	s := token.Asset
	if (!SupportsTokenImport(token.Network) && !IsPromotedDefault(token)) ||
		len(s.Contract) != 42 || !common.IsHexAddress(s.Contract) ||
		common.HexToAddress(s.Contract) == (common.Address{}) || common.HexToAddress(s.Contract).Hex() != s.Contract ||
		s.ID != token.Network+":erc20:"+s.Contract || s.Decimals > 77 || !tokenSymbol(s.Symbol) ||
		!hashValid(token.CodeHash) || token.CodeHash != strings.ToLower(token.CodeHash) {
		return errors.New("unsupported ERC-20 token policy")
	}
	for _, n := range registry() {
		for _, pinned := range n.Assets {
			if n.ID == token.Network && strings.EqualFold(pinned.Contract, s.Contract) && !IsPromotedDefault(token) {
				return errors.New("token is already pinned by the wallet")
			}
		}
	}
	return nil
}

// ValidateTokenPolicy approves new imports only; registry defaults cannot be
// imported again. ValidateStoredTokenPolicy separately preserves inert historical
// approvals when an exact contract is promoted into the default registry.
func ValidateTokenPolicy(token model.ERC20Token) error {
	if err := ValidateStoredTokenPolicy(token); err != nil {
		return err
	}
	if IsPromotedDefault(token) {
		return errors.New("token is already pinned by the wallet")
	}
	return nil
}

// WithTokens returns a fresh immutable adapter, so concurrent observations keep
// their original policy. Only signer-approved policies reach this constructor.
func (a *Adapter) WithTokens(tokens []model.ERC20Token) (*Adapter, error) {
	if len(tokens) > MaxImportedTokens {
		return nil, errors.New("too many imported ERC-20 tokens")
	}
	seen := map[string]bool{}
	for _, token := range tokens {
		if ValidateStoredTokenPolicy(token) != nil || seen[token.Asset.ID] {
			return nil, errors.New("invalid or duplicate imported ERC-20 token")
		}
		seen[token.Asset.ID] = true
	}
	out, err := NewWithEndpoints(a.endpoints)
	if err != nil {
		return nil, err
	}
	out.HTTPClient = a.HTTPClient
	for _, token := range tokens {
		if !IsPromotedDefault(token) {
			out.tokens = append(out.tokens, token)
		}
	}
	return out, nil
}

func decodeTokenSymbol(raw string) (string, error) {
	if !cleanHex(raw) || len(raw) > 2+32*2*4 {
		return "", errors.New("unsupported token symbol metadata")
	}
	bytes, err := hex.DecodeString(raw[2:])
	if err != nil || len(bytes) != 96 {
		return "", errors.New("token symbol must use bounded standard ABI string metadata")
	}
	for _, value := range bytes[:31] {
		if value != 0 {
			return "", errors.New("invalid token symbol offset")
		}
	}
	for _, value := range bytes[32:63] {
		if value != 0 {
			return "", errors.New("invalid token symbol length")
		}
	}
	length := int(bytes[63])
	if bytes[31] != 32 || length < 1 || length > 24 || !tokenSymbol(string(bytes[64:64+length])) {
		return "", errors.New("unsupported token symbol metadata")
	}
	for _, value := range bytes[64+length:] {
		if value != 0 {
			return "", errors.New("noncanonical token symbol padding")
		}
	}
	return string(bytes[64 : 64+length]), nil
}

// InspectToken reads only standard metadata at one checked network/block.
// Missing optional metadata is unsupported, never guessed (especially decimals).
func (a *Adapter) InspectToken(ctx context.Context, account model.Account, contract string) (model.ERC20Token, error) {
	n, err := a.validateAccount(account)
	if err != nil || !SupportsTokenImport(n.ID) || a.ValidateAddress(n.ID, contract) != nil {
		return model.ERC20Token{}, errors.New("token import requires an owned Ethereum, Base or Arbitrum wallet and exact contract")
	}
	contract = common.HexToAddress(contract).Hex()
	for _, asset := range n.Assets {
		if strings.EqualFold(asset.Contract, contract) {
			return model.ERC20Token{}, errors.New("token is already tracked")
		}
	}
	b, err := a.current(ctx, n)
	if err != nil {
		return model.ERC20Token{}, err
	}
	return a.inspectTokenAt(ctx, n, contract, b.Number)
}

func (a *Adapter) inspectTokenAt(ctx context.Context, n model.Network, contract, at string) (model.ERC20Token, error) {
	var code, precision, symbol string
	if err := a.rpc(ctx, n, "eth_getCode", []any{contract, at}, &code); err != nil || len(code) < 4 || len(code) > 2<<20 || !cleanHex(code) {
		return model.ERC20Token{}, errors.New("token contract runtime unavailable")
	}
	if err := a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": contract, "data": callData("decimals()")}, at}, &precision); err != nil {
		return model.ERC20Token{}, err
	}
	decimals, err := abiInteger(precision)
	if err != nil || !decimals.IsUint64() || decimals.Uint64() > 77 {
		return model.ERC20Token{}, errors.New("token decimals are missing or unsupported")
	}
	if err = a.rpc(ctx, n, "eth_call", []any{map[string]string{"to": contract, "data": callData("symbol()")}, at}, &symbol); err != nil {
		return model.ERC20Token{}, err
	}
	label, err := decodeTokenSymbol(symbol)
	if err != nil {
		return model.ERC20Token{}, err
	}
	codeBytes, err := hex.DecodeString(code[2:])
	if err != nil || len(codeBytes) == 0 {
		return model.ERC20Token{}, errors.New("invalid token runtime encoding")
	}
	token := model.ERC20Token{Network: n.ID, Asset: model.Asset{ID: n.ID + ":erc20:" + contract,
		Symbol: label, Decimals: uint8(decimals.Uint64()), Contract: contract}, CodeHash: crypto.Keccak256Hash(codeBytes).Hex()}
	return token, ValidateTokenPolicy(token)
}
