package ton

import (
	"context"
	"net/url"
	"time"
)

// TON Center's unauthenticated service permits one request per second. A balance
// lookup already needs both chain identity and account state, so serialise these
// public requests with a margin. This never retries an HTTP request or broadcast.
// The limit is local to this adapter; other applications sharing the provider/IP
// may still exhaust the provider quota. An encrypted API key or custom RPC can be
// configured if the public service is unavailable.
func (a *Adapter) pacePublic(ctx context.Context, endpoint Endpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil || endpoint.APIKey != "" || (u.Hostname() != "toncenter.com" && u.Hostname() != "testnet.toncenter.com") {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case a.publicPermit <- struct{}{}:
	}
	defer func() { <-a.publicPermit }()
	if delay := time.Until(a.lastPublicRequest.Add(1100 * time.Millisecond)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.lastPublicRequest = time.Now()
	return nil
}
