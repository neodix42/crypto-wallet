package signer

import (
	"localwallet/internal/chains"
	"localwallet/internal/model"
)

// Historical descriptors remain authenticated in ciphertext and retain their
// journal references. They are never offered as active wallets or RPC targets.
func (e *Engine) activeAccounts() []model.Account {
	if e.data == nil {
		return nil
	}
	accounts := make([]model.Account, 0, len(e.data.Accounts))
	for _, account := range e.data.Accounts {
		if !chains.IsRetiredNetwork(account.Network) {
			accounts = append(accounts, account)
		}
	}
	return accounts
}
