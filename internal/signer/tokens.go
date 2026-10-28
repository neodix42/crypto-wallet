package signer

import (
	"crypto/hmac"
	"fmt"

	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
)

// TokenPolicies is public contract metadata on inherited core/signer IPC.
func (e *Engine) TokenPolicies() []model.ERC20Token {
	if e.data == nil {
		return nil
	}
	return append([]model.ERC20Token(nil), e.data.ERC20Tokens...)
}

func adaptersWithTokens(adapters []model.Adapter, tokens []model.ERC20Token) ([]model.Adapter, error) {
	out := append([]model.Adapter(nil), adapters...)
	for i, adapter := range out {
		if ethereum, ok := adapter.(*evm.Adapter); ok {
			updated, err := ethereum.WithTokens(tokens)
			if err != nil {
				return nil, ErrDenied
			}
			out[i] = updated
		}
	}
	return out, nil
}

func (e *Engine) ImportToken(accountID string, token model.ERC20Token) error {
	if !e.canAuthorize() || evm.ValidateTokenPolicy(token) != nil || len(e.data.ERC20Tokens) >= evm.MaxImportedTokens {
		return ErrDenied
	}
	account, err := e.account(accountID)
	if err != nil || account.Family != "evm" || account.Network != token.Network {
		return ErrDenied
	}
	for _, existing := range e.data.ERC20Tokens {
		if existing.Asset.ID == token.Asset.ID {
			return ErrDenied
		}
	}
	data, generation := e.data, e.data.Generation
	policies := append(append([]model.ERC20Token(nil), data.ERC20Tokens...), token)
	adapters, err := adaptersWithTokens(e.Adapters, policies)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("Network: %s\nOwned receiving address: %s\nToken contract: %s\nUntrusted token symbol: %s\nDecimals: %d\nObserved runtime code hash: %s\nOnly standard transfer(address,uint256) is permitted, with ETH paying gas. No approval/allowance or arbitrary contract call is enabled. This imports a watch policy, not funds. The RPC and token issuer can mislead you; token symbols can collide. Runtime code pinning does not prove proxy implementation stability or safe issuer behavior. Tokens with missing metadata, transfer fees, rebasing or nonstandard transfer results are not supported. Recovery phrases restore the address; import this contract again on another installation.", token.Network, account.Address, token.Asset.Contract, token.Asset.Symbol, token.Asset.Decimals, token.CodeHash)
	if !e.UI.Confirm("Import Ethereum ERC-20 token", summary) || !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	in, err := e.UI.Input("Confirm app password for token import", "Storage password")
	defer wipeInputs(in)
	if err != nil || len(in) != 1 || !hmac.Equal(in[0], e.password) || !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	data.ERC20Tokens = policies
	if err := e.persist(); err != nil {
		return err
	}
	e.Adapters = adapters
	return nil
}

func (e *Engine) tokenApproval(plan model.Plan) string {
	for _, token := range e.TokenPolicies() {
		if token.Network == plan.Account.Network && token.Asset.ID == plan.Asset {
			return fmt.Sprintf("\nImported ERC-20 contract: %s\nToken symbol (untrusted): %s\nToken decimals: %d\nApproved runtime code hash: %s\nIssuer/proxy behavior is not independently verified; only the exact standard token transfer shown above is authorized.", token.Asset.Contract, token.Asset.Symbol, token.Asset.Decimals, token.CodeHash)
		}
	}
	return ""
}
