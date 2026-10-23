// Package chains registers every required family; none is inferred from EVM support.
package chains

import (
	"localwallet/internal/chains/aptos"
	"localwallet/internal/chains/decred"
	"localwallet/internal/chains/evm"
	"localwallet/internal/chains/hypercore"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

func All() []model.Adapter {
	return []model.Adapter{evm.New(), hypercore.New(), near.New(), aptos.New(), decred.New(), ton.New()}
}
