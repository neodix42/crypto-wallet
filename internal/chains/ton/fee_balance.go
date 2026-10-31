package ton

// InsufficientNativeFeeBalanceError is emitted only after native transfer
// preparation has computed its exact fee estimate and fixed account-storage
// reserve. It exposes no partial plan or signature and is not an RPC error.
// Token preparation never emits this quote: GRAM cannot become USDT units.
type InsufficientNativeFeeBalanceError struct {
	MaxFee         string
	StorageReserve string
}

func (*InsufficientNativeFeeBalanceError) Error() string {
	return "insufficient GRAM for payment, fees and storage reserve"
}
