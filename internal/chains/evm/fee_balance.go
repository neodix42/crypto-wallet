package evm

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
