package walletcore

import (
	"context"
	"errors"
	"time"

	"localwallet/internal/marketdata"
)

type marketPriceReader interface {
	Prices(context.Context) (marketdata.Snapshot, error)
}

// marketPricesAt has no account/asset/endpoint arguments. Only public fixed
// ticker requests leave the coordinator; credentials and wallet identities never
// enter this request. Its result is display data and cannot change a payment.
func (c *core) marketPricesAt(epoch uint64) (marketdata.Snapshot, error) {
	c.mu.Lock()
	if !c.active || c.epoch != epoch || c.sessionCtx == nil {
		c.mu.Unlock()
		return marketdata.Snapshot{}, errors.New("wallet session changed")
	}
	if c.marketData == nil {
		c.marketData = marketdata.NewClient()
	}
	reader := c.marketData
	session := c.sessionCtx
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(session, 8*time.Second)
	defer cancel()
	result, err := reader.Prices(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.epoch != epoch || ctx.Err() != nil {
		return marketdata.Snapshot{}, errors.New("wallet session changed")
	}
	return result, err
}
