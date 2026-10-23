package hypercore

import (
	"context"
	"errors"
	"fmt"
	"localwallet/internal/chains/evm"
	"localwallet/internal/model"
	"strings"
	"time"
)

func (*Adapter) HistoryScope() string {
	return "Provider-observed spot HYPE transfers in the latest 30 days, at most 500 ledger events. This bounded window does not establish complete historical recovery or prove the outcome of an unresolved signed action."
}

// History is public ledger observation. Reconcile separately binds the exact
// signed action/nonce, explorer inclusion, and both ledger effects for payments.
func (a *Adapter) History(ctx context.Context, ac model.Account) ([]model.Receipt, error) {
	n, e := a.validateAccount(ac)
	if e != nil {
		return nil, e
	}
	_, at, e := a.identity(ctx, n)
	if e != nil {
		return nil, e
	}
	const window = uint64(30 * 24 * time.Hour / time.Millisecond)
	start := uint64(0)
	if at > window {
		start = at - window
	}
	var updates []ledger
	if e = a.request(ctx, n, "/info", map[string]any{"type": "userNonFundingLedgerUpdates", "user": ac.Address, "startTime": start, "endTime": at}, &updates); e != nil {
		return nil, e
	}
	if updates == nil {
		return nil, errors.New("Core history response missing")
	}
	if len(updates) > 500 {
		return nil, errors.New("Core history response limit")
	}
	out := []model.Receipt{}
	seen := map[string]bool{}
	for _, u := range updates {
		d := u.Delta
		if d.Type != "spotTransfer" {
			continue
		}
		if d.Token != "HYPE" && d.Token != n.Assets[0].Contract {
			continue
		}
		if !validHash(u.Hash) || u.Time < start || u.Time > at {
			return nil, errors.New("invalid Core history identity or time")
		}
		if !strings.EqualFold(d.User, ac.Address) && !strings.EqualFold(d.Destination, ac.Address) {
			return nil, errors.New("Core history does not involve account")
		}
		if e = evm.New().ValidateAddress("ethereum-mainnet", d.User); e != nil {
			return nil, e
		}
		if e = evm.New().ValidateAddress("ethereum-mainnet", d.Destination); e != nil {
			return nil, e
		}
		value, e := decimalJSON(d.Amount)
		if e != nil {
			return nil, e
		}
		fee, e := decimalJSON(d.Fee)
		if e != nil {
			return nil, e
		}
		nativeFee := "not provided"
		if len(d.NativeTokenFee) > 0 {
			v, e := decimalJSON(d.NativeTokenFee)
			if e != nil {
				return nil, e
			}
			nativeFee = v.String()
		}
		direction := "received"
		peer := d.User
		if strings.EqualFold(d.User, ac.Address) {
			direction = "sent"
			peer = d.Destination
		}
		if seen[u.Hash] {
			return nil, errors.New("duplicate Core history identity")
		}
		seen[u.Hash] = true
		detail := fmt.Sprintf("provider-observed %s %s atomic %s; peer %s; provider fee %s (fee asset not established), native HYPE fee %s atomic; bounded times %d..%d", direction, value.String(), n.Assets[0].ID, peer, formatUnits(fee), nativeFee, start, at)
		if len(updates) == 500 {
			detail += "; event limit reached, older events may be omitted"
		}
		out = append(out, model.Receipt{Identity: u.Hash, State: "OBSERVED", Detail: detail})
	}
	return out, nil
}
