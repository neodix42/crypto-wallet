package signer

import (
	"crypto/hmac"
	"fmt"

	"localwallet/internal/chains/near"
	"localwallet/internal/model"
)

// NEP141TokenPolicies contains public authorization metadata, never keys or
// credentials. It is unavailable while the encrypted wallet is locked.
func (e *Engine) NEP141TokenPolicies() []model.NEP141Token {
	if e.data == nil {
		return nil
	}
	return append([]model.NEP141Token(nil), e.data.NEP141Tokens...)
}

func adaptersWithNEP141Tokens(adapters []model.Adapter, tokens []model.NEP141Token) ([]model.Adapter, error) {
	out := append([]model.Adapter(nil), adapters...)
	for i, adapter := range out {
		if chain, ok := adapter.(*near.Adapter); ok {
			updated, err := chain.WithTokens(tokens)
			if err != nil {
				return nil, ErrDenied
			}
			out[i] = updated
		}
	}
	return out, nil
}

func (e *Engine) ImportNEP141Token(accountID string, token model.NEP141Token) error {
	if !e.canAuthorize() || near.ValidateTokenPolicy(token) != nil || len(e.data.NEP141Tokens) >= near.MaxImportedTokens {
		return ErrDenied
	}
	account, err := e.account(accountID)
	if err != nil || account.Family != "near" || account.Network != token.Network {
		return ErrDenied
	}
	for _, existing := range e.data.NEP141Tokens {
		if existing.Asset.ID == token.Asset.ID {
			return ErrDenied
		}
	}
	data, generation := e.data, e.data.Generation
	policies := append(append([]model.NEP141Token(nil), data.NEP141Tokens...), token)
	adapters, err := adaptersWithNEP141Tokens(e.Adapters, policies)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("Network: %s\nOwned receiving account: %s\nNEP-141 token contract: %s\nUntrusted token symbol: %s\nDecimals: %d\nObserved contract code hash: %s\nOnly the exact standard ft_transfer with 1 yoctoNEAR attached and native NEAR fees is authorized by this policy. Self storage registration requires its own exact transaction approval and password; importing sends no funds and registers no account. No ft_transfer_call, arbitrary method, allowance or Bybit route is enabled. Symbols can collide and an upgrade or malicious issuer can change token behavior. Code pinning is not an audit. Recovery phrases restore the account, not this contract policy; import the contract again on another installation.", token.Network, account.Address, token.Asset.Contract, token.Asset.Symbol, token.Asset.Decimals, token.CodeHash)
	if !e.UI.Confirm("Import NEP-141 token", summary) || !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	in, err := e.UI.Input("Confirm app password for NEAR token import", "Storage password")
	defer wipeInputs(in)
	if err != nil || len(in) != 1 || !hmac.Equal(in[0], e.password) || !e.canAuthorize() || e.data != data || data.Generation != generation {
		return ErrDenied
	}
	data.NEP141Tokens = policies
	if err := e.persist(); err != nil {
		return err
	}
	e.Adapters = adapters
	return nil
}

func (e *Engine) nearTokenApproval(plan model.Plan) string {
	for _, token := range e.NEP141TokenPolicies() {
		if token.Network == plan.Account.Network && token.Asset.ID == plan.Asset {
			return fmt.Sprintf("\nImported NEP-141 contract: %s\nToken symbol (untrusted): %s\nToken decimals: %d\nApproved contract code hash: %s\nOnly the exact standard token transfer shown above is authorized. Contract/issuer safety is not independently verified.", token.Asset.Contract, token.Asset.Symbol, token.Asset.Decimals, token.CodeHash)
		}
	}
	return ""
}
