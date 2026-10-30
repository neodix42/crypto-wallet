package walletcore

import (
	"context"
	"errors"
	"localwallet/internal/marketdata"
	"testing"
)

type marketReaderFunc func(context.Context) (marketdata.Snapshot, error)

func (f marketReaderFunc) Prices(ctx context.Context) (marketdata.Snapshot, error) { return f(ctx) }
func TestMarketPricesRequireCurrentUnlockedSessionAndDiscardLateResults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	c := &core{epoch: 7, sessionCtx: ctx, marketData: marketReaderFunc(func(ctx context.Context) (marketdata.Snapshot, error) { calls++; return marketdata.Snapshot{}, nil })}
	if _, err := c.marketPricesAt(7); err == nil || calls != 0 {
		t.Fatal("locked session queried prices")
	}
	c.active = true
	if _, err := c.marketPricesAt(6); err == nil || calls != 0 {
		t.Fatal("old session queried prices")
	}
	if _, err := c.marketPricesAt(7); err != nil || calls != 1 {
		t.Fatalf("current public read unavailable %v", err)
	}
	c.marketData = marketReaderFunc(func(ctx context.Context) (marketdata.Snapshot, error) {
		c.mu.Lock()
		c.epoch++
		c.active = false
		c.mu.Unlock()
		return marketdata.Snapshot{Quotes: map[string]marketdata.Quote{"ETHUSDT": {Pair: "ETHUSDT", Price: "2000"}}}, nil
	})
	if out, err := c.marketPricesAt(7); err == nil || len(out.Quotes) != 0 {
		t.Fatal("late quote crossed lock/session transition")
	}
}
func TestMarketPriceSessionCancellationReachesTheHTTPReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	c := &core{active: true, sessionCtx: ctx, marketData: marketReaderFunc(func(ctx context.Context) (marketdata.Snapshot, error) {
		close(entered)
		<-ctx.Done()
		return marketdata.Snapshot{}, ctx.Err()
	})}
	done := make(chan error)
	go func() { _, err := c.marketPricesAt(0); done <- err }()
	<-entered
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled HTTP survived session cancellation")
	}
}
func TestMarketPriceErrorsRemainDisplayFailures(t *testing.T) {
	c := &core{active: true, sessionCtx: context.Background(), marketData: marketReaderFunc(func(ctx context.Context) (marketdata.Snapshot, error) {
		return marketdata.Snapshot{}, marketdata.ErrUnavailable
	})}
	if _, err := c.marketPricesAt(0); !errors.Is(err, marketdata.ErrUnavailable) {
		t.Fatal("public quote failure was concealed")
	}
}
