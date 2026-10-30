// Package model contains public data only. Secret-bearing objects never cross this boundary.
package model

import (
	"context"
	"encoding/json"
)

type Account struct {
	ID         string          `json:"id"`
	Family     string          `json:"family"`
	Profile    string          `json:"profile"`
	Index      uint32          `json:"index"`
	Network    string          `json:"network"`
	Address    string          `json:"address"`
	PublicKey  string          `json:"publicKey"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	Hidden     bool            `json:"hidden,omitempty"`
}
type Network struct {
	ID      string  `json:"id"`
	Family  string  `json:"family"`
	Name    string  `json:"name"`
	Testnet bool    `json:"testnet"`
	ChainID string  `json:"chainId"`
	RPC     string  `json:"rpc"`
	Assets  []Asset `json:"assets"`
	Trust   string  `json:"trust"`
}
type Asset struct {
	ID       string `json:"id"`
	Symbol   string `json:"symbol"`
	Decimals uint8  `json:"decimals"`
	Contract string `json:"contract,omitempty"`
}

// ERC20Token is an explicitly approved public token policy, not a discovered
// balance or an endorsement of the token issuer's behavior.
type ERC20Token struct {
	Network  string `json:"network"`
	Asset    Asset  `json:"asset"`
	CodeHash string `json:"codeHash"`
}
type Transfer struct {
	AccountID string `json:"accountId"`
	Asset     string `json:"asset"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Memo      string `json:"memo"`
}

// Plan is interpreted and reconstructed by the chain's secure validator. Payload is
// strictly decoded into a chain-specific allowlisted transfer, never signed blindly.
type Plan struct {
	ID        string          `json:"id"`
	Account   Account         `json:"account"`
	Asset     string          `json:"asset"`
	Recipient string          `json:"recipient"`
	Amount    string          `json:"amount"`
	MaxFee    string          `json:"maxFee"`
	FeeAsset  string          `json:"feeAsset"`
	Nonce     string          `json:"nonce"`
	Expires   int64           `json:"expires"`
	Memo      string          `json:"memo"`
	Payload   json.RawMessage `json:"payload"`
}
type Signed struct {
	Identity  string          `json:"identity"`
	Bytes     []byte          `json:"bytes"`
	Auxiliary json.RawMessage `json:"auxiliary,omitempty"`
}
type Balance struct {
	Asset  string `json:"asset"`
	Atomic string `json:"atomic"`
	State  string `json:"state"`
	At     string `json:"at"`
}
type Receipt struct {
	Identity  string            `json:"identity"`
	State     string            `json:"state"`
	Detail    string            `json:"detail"`
	Block     string            `json:"block"`
	Timestamp int64             `json:"timestamp,omitempty"`
	Explorer  string            `json:"explorer,omitempty"`
	Transfers []HistoryTransfer `json:"transfers,omitempty"`
}

// HistoryPage is a bounded observation, not a proof of balance or finality.
// Adapter cursors contain positions only; the coordinator replaces them with
// single-use, session/account-bound capabilities before exposing a page.
type HistoryPage struct {
	Entries    []Receipt `json:"entries"`
	NextCursor string    `json:"nextCursor"`
	Complete   bool      `json:"complete"`
	Scope      string    `json:"scope"`
}

type HistoryPager interface {
	HistoryPage(context.Context, Account, string) (HistoryPage, error)
}

// Adapter's seed methods are called exclusively inside wallet-signer.
type Adapter interface {
	Networks() []Network
	Derive(seed []byte, index uint32, network string) (Account, error)
	ValidateAddress(network, address string) error
	Balance(context.Context, Account) ([]Balance, error)
	Prepare(context.Context, Account, Transfer) (Plan, error)
	ValidatePlan(Plan) error
	Sign(seed []byte, plan Plan) (Signed, error)
	Submit(context.Context, Plan, Signed) (Receipt, error)
	Reconcile(context.Context, Plan, Signed) (Receipt, error)
}
