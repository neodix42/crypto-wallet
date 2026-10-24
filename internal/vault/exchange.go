package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ExchangeData is authenticated by the same encrypted envelope as the wallet
// root. API credentials and remote-operation history are not derived from the
// recovery phrase and are never included in public wallet snapshots.
type ExchangeData struct {
	Credentials *ExchangeCredentials `json:"credentials,omitempty"`
	Operations  []ExchangeOperation  `json:"operations"`
}
type ExchangeCredentials struct {
	Endpoint string `json:"endpoint"`
	APIKey   []byte `json:"apiKey"`
	Secret   []byte `json:"secret"`
}
type ExchangeOperation struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Endpoint        string `json:"endpoint"`
	CredentialID    string `json:"credentialId"`
	AccountID       string `json:"accountId"`
	AssetID         string `json:"assetId"`
	Payload         string `json:"payload"`
	Digest          string `json:"digest"`
	State           string `json:"state"`
	CreatedAt       int64  `json:"createdAt"`
	ExpiresAt       int64  `json:"expiresAt"`
	ExternalID      string `json:"externalId,omitempty"`
	RejectionCode   int64  `json:"rejectionCode,omitempty"`
	LastPrice       string `json:"lastPrice,omitempty"`
	Fee             string `json:"fee,omitempty"`
	FeeCoin         string `json:"feeCoin,omitempty"`
	ChainType       string `json:"chainType,omitempty"`
	ContractAddress string `json:"contractAddress,omitempty"`
}

func (d *ExchangeData) Destroy() {
	if d.Credentials != nil {
		clear(d.Credentials.APIKey)
		clear(d.Credentials.Secret)
		d.Credentials = nil
	}
	d.Operations = nil
}

// ExchangeDigest authenticates the immutable approval intent. Status and the
// independently obtained remote identifier are the only mutable fields.
func ExchangeDigest(o ExchangeOperation) string {
	o.Digest, o.State, o.ExternalID = "", "", ""
	o.RejectionCode = 0
	b, _ := json.Marshal(o)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func exchangeText(s string, limit int, empty bool) bool {
	return (empty || s != "") && len(s) <= limit && utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func exchangeHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}
func ExchangeCredentialID(c *ExchangeCredentials) string {
	if c == nil {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(c.Endpoint))
	h.Write([]byte{0})
	h.Write(c.APIKey)
	return hex.EncodeToString(h.Sum(nil))
}
func validateExchange(d *ExchangeData, accounts map[string]bool) error {
	if d == nil {
		return nil
	}
	if len(d.Operations) > 1000 {
		return ErrFormat
	}
	if c := d.Credentials; c != nil {
		if !exchangeText(c.Endpoint, 128, false) || len(c.APIKey) < 8 || len(c.APIKey) > 128 || len(c.Secret) < 16 || len(c.Secret) > 512 {
			return ErrFormat
		}
	}
	seen := map[string]bool{}
	for _, o := range d.Operations {
		if !exchangeHex(o.ID, 32) || seen[o.ID] || !accounts[o.AccountID] || !exchangeText(o.AssetID, 256, false) || !exchangeText(o.Endpoint, 128, false) || !exchangeHex(o.CredentialID, 64) || len(o.Payload) == 0 || len(o.Payload) > 8192 || !utf8.ValidString(o.Payload) || o.CreatedAt <= 0 || o.ExpiresAt <= o.CreatedAt || o.ExpiresAt-o.CreatedAt > 600 || !exchangeText(o.ExternalID, 128, true) || !exchangeText(o.LastPrice, 80, true) || !exchangeText(o.Fee, 80, true) || !exchangeText(o.FeeCoin, 24, true) || !exchangeText(o.ChainType, 80, true) || !exchangeText(o.ContractAddress, 256, true) || o.Digest != ExchangeDigest(o) {
			return ErrFormat
		}
		seen[o.ID] = true
		if o.RejectionCode != 0 && (o.State != "FAILED" || o.ExternalID != "" || !ExchangeRejectionAllowed(o.RejectionCode)) {
			return ErrFormat
		}
		switch o.Kind {
		case "swap", "transfer", "withdraw":
		default:
			return ErrFormat
		}
		switch o.State {
		case "PREPARED", "AUTHORIZED", "SUBMITTING", "PENDING", "UNKNOWN", "CONFIRMED", "FAILED", "CANCELLED":
		default:
			return ErrFormat
		}
	}
	return nil
}

// ExchangeRejectionAllowed is intentionally limited to documented REST
// parameter/authentication/permission failures before acceptance. A duplicate,
// timeout, HTTP failure, rate limit or undocumented code grants no release.
func ExchangeRejectionAllowed(code int64) bool {
	switch code {
	case 10001, 10003, 10004, 10005:
		return true
	}
	return false
}
