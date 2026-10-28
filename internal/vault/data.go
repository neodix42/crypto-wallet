package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"localwallet/internal/chains/evm"
	"localwallet/internal/derivation"
	"localwallet/internal/journal"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
)

type Endpoint struct {
	URL      string `json:"url"`
	User     string `json:"user"`
	Password string `json:"password"`
	APIKey   string `json:"apiKey"`
}
type Data struct {
	Version     uint32 `json:"version"`
	WalletID    string `json:"walletId"`
	Generation  uint64 `json:"generation"`
	Scheme      string `json:"scheme"`
	Entropy     []byte `json:"entropy"`
	Passphrase  []byte `json:"passphrase"`
	Environment string `json:"environment"`
	Birthday    int64  `json:"birthday"`
	// DeprecatedBackupVerified exists solely to read existing v1 vaults. It is
	// never an authority to reveal a phrase, receive funds or sign a payment.
	DeprecatedBackupVerified bool                `json:"backupVerified,omitempty"`
	RecoveryConfirmed        bool                `json:"recoveryConfirmed,omitempty"`
	Accounts                 []model.Account     `json:"accounts"`
	Operations               []journal.Operation `json:"operations"`
	Endpoints                map[string]Endpoint `json:"endpoints"`
	Exchange                 *ExchangeData       `json:"exchange,omitempty"`
	ERC20Tokens              []model.ERC20Token  `json:"erc20Tokens,omitempty"`
}

func (d *Data) Destroy() {
	clear(d.Entropy)
	clear(d.Passphrase)
	d.Entropy = nil
	d.Passphrase = nil
	d.Accounts = nil
	d.Operations = nil
	d.Endpoints = nil
	d.ERC20Tokens = nil
	if d.Exchange != nil {
		d.Exchange.Destroy()
		d.Exchange = nil
	}
}
func (d *Data) Encode() ([]byte, error) {
	if e := d.Validate(); e != nil {
		return nil, e
	}
	return json.Marshal(d)
}
func Decode(b []byte) (*Data, error) {
	var d Data
	if strictjson.Decode(b, &d) != nil {
		d.Destroy()
		return nil, ErrFormat
	}
	if e := d.Validate(); e != nil {
		d.Destroy()
		return nil, e
	}
	return &d, nil
}
func (d *Data) Validate() error {
	if d.Version != 1 || len(d.WalletID) != 32 || d.Generation == 0 || d.Scheme != "bip39-english-v1" || (d.Environment != "testnet" && d.Environment != "mainnet") || len(d.Passphrase) > 4096 || len(d.Accounts) > 10000 || len(d.Operations) > 10000 {
		return ErrFormat
	}
	switch len(d.Entropy) {
	case 16, 20, 24, 28, 32:
	default:
		return ErrFormat
	}
	seen := map[string]bool{}
	for _, a := range d.Accounts {
		if a.ID == "" || seen[a.ID] || a.Index >= derivation.MaxIndex || a.Profile == "" || a.Network == "" || a.Address == "" {
			return ErrFormat
		}
		seen[a.ID] = true
	}
	ids := map[string]bool{}
	for _, o := range d.Operations {
		if o.Plan.ID == "" || ids[o.Plan.ID] || !seen[o.Plan.Account.ID] || journal.Digest(o.Plan) != o.Digest {
			return ErrFormat
		}
		ids[o.Plan.ID] = true
		switch o.State {
		case "SIGNED_NOT_SUBMITTED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "CONFIRMED", "FAILED", "REPLACED", "REORGED":
			if o.Signed == nil {
				return ErrFormat
			}
		case "PREPARED", "AWAITING_CONFIRMATION":
			if o.Signed != nil {
				return ErrFormat
			}
		}
		switch o.State {
		case "PREPARED", "AWAITING_CONFIRMATION", "CANCELLED", "SIGNED_NOT_SUBMITTED", "SUBMITTING", "SUBMITTED", "UNKNOWN", "CONFIRMED", "FAILED", "REPLACED", "REORGED":
		default:
			return ErrFormat
		}
		if o.Signed != nil && (len(o.Signed.Bytes) == 0 || len(o.Signed.Bytes) > 1<<20 || o.Signed.Identity == "") {
			return ErrFormat
		}
	}
	if err := validateExchange(d.Exchange, seen); err != nil {
		return err
	}
	if len(d.ERC20Tokens) > evm.MaxImportedTokens {
		return ErrFormat
	}
	tokens := map[string]bool{}
	for _, token := range d.ERC20Tokens {
		if evm.ValidateTokenPolicy(token) != nil || tokens[token.Asset.ID] ||
			(token.Network == "ethereum-sepolia" || token.Network == "base-sepolia") != (d.Environment == "testnet") {
			return ErrFormat
		}
		tokens[token.Asset.ID] = true
	}
	return nil
}

// VerifyRecovery compares encrypted recovery identities, not only write success.
func VerifyRecovery(a, b *Data) error {
	if a.Scheme != b.Scheme || a.WalletID != b.WalletID || a.Environment != b.Environment || !bytes.Equal(a.Entropy, b.Entropy) || !bytes.Equal(a.Passphrase, b.Passphrase) {
		return errors.New("backup recovery identity differs")
	}
	if a.Birthday != b.Birthday {
		return errors.New("backup discovery birthday differs")
	}
	x, _ := json.Marshal(struct {
		Accounts   []model.Account
		Operations []journal.Operation
		Endpoints  map[string]Endpoint
		Exchange   *ExchangeData
		Tokens     []model.ERC20Token
	}{a.Accounts, a.Operations, a.Endpoints, a.Exchange, a.ERC20Tokens})
	y, _ := json.Marshal(struct {
		Accounts   []model.Account
		Operations []journal.Operation
		Endpoints  map[string]Endpoint
		Exchange   *ExchangeData
		Tokens     []model.ERC20Token
	}{b.Accounts, b.Operations, b.Endpoints, b.Exchange, b.ERC20Tokens})
	if sha256.Sum256(x) != sha256.Sum256(y) {
		return errors.New("backup public descriptors differ")
	}
	return nil
}
