package walletcore

import (
	"errors"
	"strconv"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/ipc"
)

// Called only before a wallet payment or exchange write is authorized. Keep
// the failing read and numeric service reason without forwarding error text.
func exchangeReadFailure(stage string, err error) error {
	code := publicRequestErrorCode(err)
	if code == "REQUEST_CANCELLED" {
		return ipc.NewRequestError(code)
	}
	var api *bybit.APIError
	var metadata *bybit.CoinMetadataError
	if stage == "COIN" && errors.As(err, &metadata) && metadata != nil {
		code = "METADATA_" + metadata.Field
	}
	if errors.As(err, &api) && api != nil && api.HTTPStatus == 0 && api.Code != 0 {
		code = "BYBIT_" + strconv.FormatInt(api.Code, 10)
	}
	return ipc.NewExchangeReadError(stage, code)
}
