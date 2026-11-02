package bybit

// CoinMetadataError contains a fixed field category, never response contents.
// It permits a useful diagnostic without exposing an authenticated API body.
type CoinMetadataError struct{ Field string }

func (e *CoinMetadataError) Error() string { return "invalid Bybit coin network metadata" }
func (e *CoinMetadataError) Unwrap() error { return ErrInvalidResponse }
