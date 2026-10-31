package evm

import "errors"

// ErrInsufficientPendingFunds is not a fee quote or retry capability. The
// pending ledger no longer covers the exact native value and verified gas
// reserve, so callers must refresh or resolve the original pending operation.
var ErrInsufficientPendingFunds = errors.New("insufficient pending spendable gas currency balance")

// InsufficientFeeBalanceError is a local preparation result emitted only after
// the adapter has computed and checked its exact fee reserve. No partial plan,
// signature or authority is returned. Max may use this quote to prepare a
// smaller native transfer again; RPC failures never create this error.
type InsufficientFeeBalanceError struct {
	FeeAsset string
	MaxFee   string
}

func (*InsufficientFeeBalanceError) Error() string {
	return "insufficient gas currency balance"
}
