package ton

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

// IsDeploymentPlan identifies the only additional operation accepted by TON's
// secure validator. It does not itself validate or authorize a plan.
func IsDeploymentPlan(p model.Plan) bool {
	x, err := payload(p)
	return p.Account.Family == "ton" && err == nil && x.Kind == "deployment"
}

// DeploymentEffects is safe to call only after the secure ValidatePlan check.
func DeploymentEffects(p model.Plan) string {
	meta, _, err := descriptor(p.Account)
	if err != nil || !IsDeploymentPlan(p) {
		return "Unsupported TON deployment"
	}
	return fmt.Sprintf("Deploy Wallet V5R1 at the exact owned address %s. Workchain: %d; subwallet: %d; on-chain wallet ID: %d; code hash: %s. The signed external message includes this account's exact StateInit, seqno 0 and no outgoing transfers or extensions. Attached transfer: 0 GRAM. Network/storage fees are deducted from this funded address. The displayed fee reserve is an estimate, not a protocol-enforced signed ceiling.", p.Account.Address, meta.Workchain, meta.Subwallet, meta.WalletID, meta.CodeHash)
}

func (a *Adapter) WalletState(ctx context.Context, ac model.Account) (model.TONWalletState, error) {
	meta, _, err := descriptor(ac)
	if err != nil {
		return model.TONWalletState{}, err
	}
	checkpoint, err := a.verifyNetwork(ctx, ac.Network)
	if err != nil {
		return model.TONWalletState{}, err
	}
	state, err := a.state(ctx, ac.Network, ac.Address)
	if err != nil {
		return model.TONWalletState{}, err
	}
	result := model.TONWalletState{AccountID: ac.ID, Network: ac.Network, Address: ac.Address, Status: state.State, Balance: state.Balance, Version: meta.Version, WalletID: strconv.FormatUint(uint64(meta.WalletID), 10), Subwallet: strconv.FormatUint(uint64(meta.Subwallet), 10), At: strconv.FormatUint(uint64(checkpoint), 10)}
	switch state.State {
	case "uninitialized":
		result.Status = "uninit"
		balance, _ := chainutil.Big(state.Balance, 120)
		result.CanDeploy = balance.Cmp(big.NewInt(10_000_000)) > 0
		result.Reason = "Fund this non-bounceable receiving address with GRAM for deployment fees plus the separate 0.01 GRAM storage reserve."
		if result.CanDeploy {
			result.Reason = "Funded but contract is not deployed. Review deployment to check exact fees and the storage reserve."
		}
	case "active":
		// Do not label an arbitrary contract active/usable on the strength of
		// the provider's state name alone; validate the pinned code/key/ID.
		if _, err := a.authorityFromState(ac, state); err != nil {
			return model.TONWalletState{}, err
		}
		result.Reason = "Wallet V5R1 contract is deployed; its recorded code, wallet ID and signing authority match."
	case "frozen":
		result.Reason = "Wallet contract is frozen; ordinary deployment is unavailable."
	}
	return result, nil
}

func (a *Adapter) PrepareDeployment(ctx context.Context, ac model.Account) (model.Plan, error) {
	if _, _, err := descriptor(ac); err != nil {
		return model.Plan{}, err
	}
	checkpoint, err := a.verifyNetwork(ctx, ac.Network)
	if err != nil {
		return model.Plan{}, err
	}
	auth, err := a.authority(ctx, ac)
	if err != nil {
		return model.Plan{}, err
	}
	if !auth.Deploy || auth.Seqno != 0 {
		return model.Plan{}, errors.New("TON wallet is already deployed; its contract parameters cannot be changed")
	}
	x := Payload{Kind: "deployment", Deploy: true, SourceBalance: auth.Balance, StorageReserve: "10000000", MasterSeqno: checkpoint}
	p := model.Plan{Account: ac, Asset: ac.Network + ":TON", Recipient: ac.Address, Amount: "0", Nonce: "0", FeeAsset: ac.Network + ":TON", Expires: time.Now().Add(5 * time.Minute).Unix()}
	unsigned, _, err := message(p, x)
	if err != nil {
		return model.Plan{}, err
	}
	body := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(make([]byte, 64), 512).EndCell()
	fee, err := a.estimate(ctx, p, x, body, true)
	if err != nil {
		return model.Plan{}, err
	}
	x.EstimatedFee = strconv.FormatUint(fee, 10)
	p.MaxFee = strconv.FormatUint(fee*2+1_000_000, 10)
	p.Payload, _ = json.Marshal(x)
	p.ID = planID(p)
	if err := a.ValidatePlan(p); err != nil {
		return model.Plan{}, err
	}
	return p, nil
}

func (a *Adapter) reconcileDeployment(ctx context.Context, p model.Plan, signed model.Signed, tx *tlb.Transaction) (model.Receipt, error) {
	result := model.Receipt{Identity: signed.Identity, State: "UNKNOWN", Block: hex.EncodeToString(tx.Hash)}
	if tx.OutMsgCount != 0 || (tx.IO.Out != nil && tx.IO.Out.List != nil && !tx.IO.Out.List.IsEmpty()) {
		result.State, result.Detail = "FAILED", "deployment emitted an outgoing message despite its approved empty action list"
		return result, nil
	}
	// Successful computation alone is not sufficient: the source contract must
	// now be active with the approved V5R1 code/key/ID and no extensions. Its
	// seqno may have advanced further through separately approved operations.
	auth, err := a.authority(ctx, p.Account)
	if err != nil || auth.Deploy || auth.Seqno == 0 {
		result.Detail = "matching deployment execution found; active recorded wallet authority has not been verified"
		return result, err
	}
	result.State = "CONFIRMED"
	result.Detail = "matching signed deployment execution, no outgoing transfers, and active recorded V5R1 authority verified (trusted RPC; no independent consensus proof)"
	return result, nil
}
