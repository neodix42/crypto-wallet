package evm

import (
	"context"

	"localwallet/internal/model"
)

// SpendableBalance excludes pending debits without crediting an unconfirmed
// incoming transfer. Both observations come from the checked selected chain;
// tokens retain the same exact runtime/implementation/precision checks at each
// state. An unsupported or malformed pending query is an error, never zero.
func (a *Adapter) SpendableBalance(ctx context.Context, account model.Account, assetID string) (string, error) {
	network, err := a.validateAccount(account)
	if err != nil {
		return "", err
	}
	selected, err := asset(network, assetID)
	if err != nil {
		return "", err
	}
	current, err := a.current(ctx, network)
	if err != nil {
		return "", err
	}
	confirmed, err := a.balance(ctx, network, account, selected, current.Number)
	if err != nil {
		return "", err
	}
	pending, err := a.balance(ctx, network, account, selected, "pending")
	if err != nil {
		return "", err
	}
	if pending.Cmp(confirmed) < 0 {
		return pending.String(), nil
	}
	return confirmed.String(), nil
}
