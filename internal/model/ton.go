package model

import "context"

// TONWalletState describes the observed contract at an owned, immutable TON
// address. An uninitialized account needs funding before a deployment can pay
// its fees. This is an RPC observation rather than consensus verification.
type TONWalletState struct {
	AccountID string `json:"accountId"`
	Network   string `json:"network"`
	Address   string `json:"address"`
	Status    string `json:"status"`
	Balance   string `json:"balance"`
	Version   string `json:"version"`
	WalletID  string `json:"walletId"`
	Subwallet string `json:"subwallet"`
	CanDeploy bool   `json:"canDeploy"`
	Reason    string `json:"reason"`
	At        string `json:"at"`
}

type TONDeployer interface {
	WalletState(context.Context, Account) (TONWalletState, error)
	PrepareDeployment(context.Context, Account) (Plan, error)
}
