package signer

import (
	"bytes"
	"encoding/json"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/chains/near"
	"localwallet/internal/chains/ton"
	"localwallet/internal/exchange"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/vault"
)

// This capability exists only between private Bybit preparation and signing in
// this unlocked session. It is neither a public request flag nor a vault field.
type bybitDepositAuthorization struct {
	owner                            *vault.Data
	exchange                         *vault.ExchangeData
	generation                       uint64
	id, digest, endpoint, credential string
	environment                      string
	expires                          int64
}

// PrepareBybitDeposit is called by the trusted coordinator only after checking
// the current Bybit deposit route/address/memo. The normal chain validator and
// durable journal still authenticate every transfer field and nonce reservation.
func (e *Engine) PrepareBybitDeposit(plan model.Plan) error {
	if !e.canAuthorize() || e.data.Environment != "mainnet" || e.data.Exchange == nil || e.data.Exchange.Selected() != "bybit" {
		return ErrDenied
	}
	x := e.data.Exchange
	credential := x.Credential("bybit")
	if credential == nil || !exchange.ValidateProviderEndpoint("bybit", credential.Endpoint) {
		return ErrDenied
	}
	if near.IsStakingPlan(plan) || near.IsTokenRegistrationPlan(plan) || ton.IsDeploymentPlan(plan) {
		return ErrDenied
	}
	if _, _, err := e.exchangeAsset(plan.Account.ID, plan.Asset); err != nil {
		return err
	}
	value, err := amount.Atomic(plan.Amount)
	if err != nil || value.Sign() <= 0 {
		return ErrDenied
	}
	if err = e.Prepare(plan); err != nil {
		return err
	}
	if e.bybitDepositPreparations == nil {
		e.bybitDepositPreparations = map[string]*bybitDepositAuthorization{}
	}
	e.bybitDepositPreparations[plan.ID] = &bybitDepositAuthorization{owner: e.data, exchange: x, generation: e.data.Generation, id: plan.ID, digest: journal.Digest(plan), endpoint: credential.Endpoint, credential: vault.ExchangeCredentialID(credential), environment: e.data.Environment, expires: plan.Expires}
	return nil
}

func (e *Engine) bybitDepositOperation(capability *bybitDepositAuthorization, state string, generation uint64) (*journal.Operation, error) {
	if capability == nil || !e.canAuthorize() || e.data != capability.owner || e.data.Generation != generation || e.data.Environment != capability.environment || capability.environment != "mainnet" || e.data.Exchange != capability.exchange || e.data.Exchange.Selected() != "bybit" || capability.expires <= time.Now().Unix() {
		return nil, ErrDenied
	}
	credential := e.data.Exchange.Credential("bybit")
	if credential == nil || credential.Endpoint != capability.endpoint || vault.ExchangeCredentialID(credential) != capability.credential || !exchange.ValidateProviderEndpoint("bybit", credential.Endpoint) {
		return nil, ErrDenied
	}
	for i := range e.data.Operations {
		op := &e.data.Operations[i]
		if op.Plan.ID != capability.id {
			continue
		}
		if op.State != state || op.Signed != nil || op.Digest != capability.digest || journal.Digest(op.Plan) != capability.digest || op.Plan.Expires != capability.expires {
			return nil, ErrDenied
		}
		account, _, err := e.exchangeAsset(op.Plan.Account.ID, op.Plan.Asset)
		want, _ := json.Marshal(account)
		got, _ := json.Marshal(op.Plan.Account)
		if err != nil || !bytes.Equal(want, got) {
			return nil, ErrDenied
		}
		return op, nil
	}
	return nil, ErrDenied
}

// SignBybitDeposit consumes the exact private preparation once. A normally
// prepared wallet Send has no such capability and keeps its password prompt.
func (e *Engine) SignBybitDeposit(id string) (model.Signed, error) {
	capability := e.bybitDepositPreparations[id]
	if capability == nil || e.bybitDepositSigning != nil {
		return model.Signed{}, ErrDenied
	}
	delete(e.bybitDepositPreparations, id)
	if _, err := e.bybitDepositOperation(capability, "PREPARED", capability.generation); err != nil {
		return model.Signed{}, err
	}
	e.bybitDepositSigning = capability
	defer func() {
		if e.bybitDepositSigning == capability {
			e.bybitDepositSigning = nil
		}
	}()
	return e.Sign(id)
}
