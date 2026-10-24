package signer

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"localwallet/internal/amount"
	"localwallet/internal/exchange"
	"localwallet/internal/exchange/bybit"
	"localwallet/internal/model"
	"localwallet/internal/strictjson"
	"localwallet/internal/vault"
)

type ExchangeIntent struct {
	Kind            string                   `json:"kind"`
	AccountID       string                   `json:"accountId"`
	AssetID         string                   `json:"assetId"`
	Order           *bybit.SpotOrder         `json:"order,omitempty"`
	Transfer        *bybit.TransferRequest   `json:"transfer,omitempty"`
	Withdrawal      *bybit.WithdrawalRequest `json:"withdrawal,omitempty"`
	LastPrice       string                   `json:"lastPrice,omitempty"`
	Fee             string                   `json:"fee,omitempty"`
	FeeCoin         string                   `json:"feeCoin,omitempty"`
	ChainType       string                   `json:"chainType,omitempty"`
	ContractAddress string                   `json:"contractAddress,omitempty"`
}
type ExchangeStatus struct {
	Connected      bool                      `json:"connected"`
	Endpoint       string                    `json:"endpoint,omitempty"`
	KeySuffix      string                    `json:"keySuffix,omitempty"`
	CredentialID   string                    `json:"credentialId,omitempty"`
	Operations     []vault.ExchangeOperation `json:"operations"`
	OperationCount int                       `json:"operationCount"`
}

func exchangeUnresolved(state string) bool {
	return state == "SUBMITTING" || state == "PENDING" || state == "UNKNOWN"
}
func (e *Engine) exchangeData() (*vault.ExchangeData, error) {
	if !e.canAuthorize() {
		return nil, ErrDenied
	}
	if e.data.Exchange == nil {
		e.data.Exchange = &vault.ExchangeData{Operations: []vault.ExchangeOperation{}}
	}
	return e.data.Exchange, nil
}
func (e *Engine) ExchangeStatus() (ExchangeStatus, error) {
	x, err := e.exchangeData()
	if err != nil {
		return ExchangeStatus{}, err
	}
	s := ExchangeStatus{Operations: []vault.ExchangeOperation{}, OperationCount: len(x.Operations)}
	if c := x.Credentials; c != nil {
		s.Connected, s.Endpoint = true, c.Endpoint
		s.CredentialID = vault.ExchangeCredentialID(c)
		if len(c.APIKey) >= 4 {
			s.KeySuffix = string(c.APIKey[len(c.APIKey)-4:])
		}
	}
	start := len(x.Operations) - 50
	if start < 0 {
		start = 0
	}
	for i, o := range x.Operations {
		if i >= start || exchangeUnresolved(o.State) {
			s.Operations = append(s.Operations, o)
		}
	}
	return s, nil
}
func (e *Engine) exchangePassword(title string) error {
	if !e.canAuthorize() {
		return ErrDenied
	}
	in, err := e.UI.Input(title, "App storage password")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != 1 || !e.canAuthorize() || !hmac.Equal(in[0], e.password) {
		return ErrDenied
	}
	return nil
}
func exchangeCredentialBytes(v []byte, key bool) bool {
	min, max := 16, 512
	if key {
		min, max = 8, 128
	}
	if len(v) < min || len(v) > max {
		return false
	}
	for _, c := range v {
		if c < 33 || c > 126 || key && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ConnectExchange requests credentials through the existing masked, isolated
// Electron input channel. Neither the public portfolio nor core receives the
// secret outside that transient prompt reply.
func (e *Engine) ConnectExchange(endpoint string) error {
	x, err := e.exchangeData()
	if err != nil || e.data.Environment != "mainnet" || bybit.ValidateEndpoint(endpoint) != nil {
		return ErrDenied
	}
	if err = e.exchangePassword("Reauthenticate Bybit connection"); err != nil {
		return err
	}
	in, err := e.UI.Input("Connect Bybit — API credentials", "System-generated HMAC API key (masked)", "API secret (masked; stored encrypted locally)")
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != 2 || !e.canAuthorize() || !exchangeCredentialBytes(in[0], true) || !exchangeCredentialBytes(in[1], false) {
		return ErrDenied
	}
	c := &vault.ExchangeCredentials{Endpoint: endpoint, APIKey: bytes.Clone(in[0]), Secret: bytes.Clone(in[1])}
	keep := false
	defer func() {
		if !keep {
			clear(c.APIKey)
			clear(c.Secret)
		}
	}()
	for _, o := range x.Operations {
		if exchangeUnresolved(o.State) && o.CredentialID != vault.ExchangeCredentialID(c) {
			return ErrDenied
		}
	}
	if !e.UI.Confirm("Approve Bybit custody and API access", "Service: Bybit\nEndpoint: "+endpoint+"\nDeposited assets are held by Bybit, which controls trading, custody, withdrawals, fees and availability. API credentials permit the permissions configured in your Bybit account. This wallet supports unleveraged spot trades and withdrawals only to its selected owned accounts. No deposit, trade, transfer or withdrawal is sent by connecting. Keep the API key restricted and whitelist your wallet withdrawal addresses on Bybit. The exchange secret is stored only inside the encrypted local vault; recovery phrases do not restore it.") || !e.canAuthorize() {
		return ErrDenied
	}
	if x.Credentials != nil {
		clear(x.Credentials.APIKey)
		clear(x.Credentials.Secret)
	}
	x.Credentials = c
	if err = e.persist(); err != nil {
		return err
	}
	keep = true
	return nil
}
func (e *Engine) DisconnectExchange() error {
	x, err := e.exchangeData()
	if err != nil {
		return err
	}
	if err = e.exchangePassword("Reauthenticate Bybit disconnection"); err != nil {
		return err
	}
	if x.Credentials != nil {
		clear(x.Credentials.APIKey)
		clear(x.Credentials.Secret)
		x.Credentials = nil
	}
	e.exchangeAuthorizations = nil
	for i := range x.Operations {
		if x.Operations[i].State == "PREPARED" || x.Operations[i].State == "AUTHORIZED" {
			x.Operations[i].State = "CANCELLED"
		}
	}
	return e.persist()
}
func (e *Engine) exchangeAsset(accountID, assetID string) (model.Account, model.Asset, error) {
	a, err := e.account(accountID)
	if err != nil {
		return model.Account{}, model.Asset{}, ErrDenied
	}
	adapter, err := e.adapter(a.Network)
	if err != nil {
		return model.Account{}, model.Asset{}, ErrDenied
	}
	for _, n := range adapter.Networks() {
		if n.ID == a.Network && !n.Testnet {
			for _, asset := range n.Assets {
				if asset.ID == assetID {
					return a, asset, nil
				}
			}
		}
	}
	return model.Account{}, model.Asset{}, ErrDenied
}
func exchangeDecimal(s string, positive bool) bool {
	n, err := amount.Parse(s, 36)
	return err == nil && (!positive || n != "0")
}
func exchangeUUID(id string) string {
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
func exchangePath(kind string) string {
	switch kind {
	case "swap":
		return "/v5/order/create"
	case "transfer":
		return "/v5/asset/transfer/inter-transfer"
	case "withdraw":
		return "/v5/asset/withdraw/create"
	}
	return ""
}
func exchangeWithdrawalNetwork(a model.Account, asset model.Asset, w bybit.WithdrawalRequest, chainType, contract string) bool {
	if w.Coin != asset.Symbol || w.Address != a.Address || w.Tag != "" {
		return false
	}
	return exchange.ValidateRoute(a, asset, w.Chain, chainType, contract) == nil
}
func (e *Engine) PrepareExchange(in ExchangeIntent) (vault.ExchangeOperation, error) {
	x, err := e.exchangeData()
	if err != nil || x.Credentials == nil || e.data.Environment != "mainnet" || len(x.Operations) >= 1000 {
		return vault.ExchangeOperation{}, ErrDenied
	}
	for _, o := range x.Operations {
		if exchangeUnresolved(o.State) || o.State == "AUTHORIZED" {
			return vault.ExchangeOperation{}, ErrDenied
		}
	}
	a, asset, err := e.exchangeAsset(in.AccountID, in.AssetID)
	if err != nil {
		return vault.ExchangeOperation{}, err
	}
	if in.LastPrice != "" && !exchangeDecimal(in.LastPrice, true) || in.Fee != "" && !exchangeDecimal(in.Fee, false) || in.FeeCoin != "" && in.FeeCoin != asset.Symbol && in.FeeCoin != "USDT" {
		return vault.ExchangeOperation{}, ErrDenied
	}
	id := ID()
	var payload any
	switch in.Kind {
	case "swap":
		if in.Order == nil || in.Transfer != nil || in.Withdrawal != nil || asset.Symbol == "USDT" {
			return vault.ExchangeOperation{}, ErrDenied
		}
		o := *in.Order
		o.OrderLinkID = id
		if o.Symbol != asset.Symbol+"USDT" || bybit.ValidateSpotOrder(o) != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		b, _ := amount.Parse(o.SlippageTolerance, 2)
		n, _ := new(big.Int).SetString(b, 10)
		if n == nil || n.Cmp(big.NewInt(100)) > 0 {
			return vault.ExchangeOperation{}, ErrDenied
		}
		payload = o
	case "transfer":
		if in.Transfer == nil || in.Order != nil || in.Withdrawal != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		t := *in.Transfer
		t.TransferID = exchangeUUID(id)
		if (t.Coin != asset.Symbol && t.Coin != "USDT") || bybit.ValidateTransfer(t) != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		payload = t
	case "withdraw":
		if in.Withdrawal == nil || in.Order != nil || in.Transfer != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		// These entities require separate documented questionnaires which this
		// wallet does not collect. Refuse before creating a remote operation.
		switch x.Credentials.Endpoint {
		case "https://api.bybit.tr", "https://api.bybit.kz", "https://api.bybit.eu", "https://api.bybit.id":
			return vault.ExchangeOperation{}, ErrDenied
		}
		w := *in.Withdrawal
		if w.Beneficiary != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		w.RequestID, w.Timestamp = id, time.Now().UnixMilli()
		if bybit.ValidateWithdrawal(w) != nil || !exchangeWithdrawalNetwork(a, asset, w, in.ChainType, in.ContractAddress) {
			return vault.ExchangeOperation{}, ErrDenied
		}
		if x.Credentials.Endpoint == "https://api.bybit.ae" {
			w.Beneficiary, err = e.exchangeUAEBeneficiary()
			if err != nil {
				return vault.ExchangeOperation{}, err
			}
		}
		payload = w
	default:
		return vault.ExchangeOperation{}, ErrDenied
	}
	// Each branch assigns one of three fixed structs containing only strings
	// and integers; JSON encoding cannot fail for these types.
	b, _ := json.Marshal(payload)
	now := time.Now().Unix()
	o := vault.ExchangeOperation{ID: id, Kind: in.Kind, Endpoint: x.Credentials.Endpoint, CredentialID: vault.ExchangeCredentialID(x.Credentials), AccountID: a.ID, AssetID: asset.ID, Payload: string(b), State: "PREPARED", CreatedAt: now, ExpiresAt: now + 300, LastPrice: in.LastPrice, Fee: in.Fee, FeeCoin: in.FeeCoin, ChainType: in.ChainType, ContractAddress: in.ContractAddress}
	o.Digest = vault.ExchangeDigest(o)
	x.Operations = append(x.Operations, o)
	if err = e.persist(); err != nil {
		return vault.ExchangeOperation{}, err
	}
	return o, nil
}
func (e *Engine) exchangeOperation(id string) (*vault.ExchangeOperation, error) {
	x, err := e.exchangeData()
	if err != nil {
		return nil, err
	}
	for i := range x.Operations {
		if x.Operations[i].ID == id {
			return &x.Operations[i], nil
		}
	}
	return nil, ErrDenied
}
func (e *Engine) ExchangeOperation(id string) (vault.ExchangeOperation, error) {
	o, err := e.exchangeOperation(id)
	if err != nil {
		return vault.ExchangeOperation{}, err
	}
	return *o, nil
}
func exchangeApproval(o vault.ExchangeOperation, a model.Account, asset model.Asset) (string, error) {
	request := bybit.AuthRequest{Endpoint: o.Endpoint, Method: "POST", Path: exchangePath(o.Kind), Payload: o.Payload, TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000}
	if bybit.ValidateAuthRequest(request) != nil {
		return "", ErrDenied
	}
	summary := fmt.Sprintf("Service: Bybit\nEndpoint: %s\nWallet network: %s\nOwned wallet address: %s\nAsset identity: %s\nOperation ID: %s\n", o.Endpoint, a.Network, a.Address, o.AssetID, o.ID)
	switch o.Kind {
	case "swap":
		var order bybit.SpotOrder
		if strictjson.Decode([]byte(o.Payload), &order) != nil || order.Symbol != asset.Symbol+"USDT" || order.OrderLinkID != o.ID {
			return "", ErrDenied
		}
		spend := asset.Symbol
		if order.Side == "Buy" {
			spend = "USDT"
		}
		summary += fmt.Sprintf("Spot market order: %s %s\nExact spending quantity: %s %s\nMaximum slippage: %s%% from Bybit's current best ask/bid\nMargin or borrowing: disabled\nLatest observed price (indicative): %s USDT per %s\nEstimated trading fee: %s %s\nMarket orders can fill partially or not fill. The last price is not an execution guarantee.\n", order.Side, order.Symbol, order.Qty, spend, order.SlippageTolerance, o.LastPrice, asset.Symbol, o.Fee, o.FeeCoin)
	case "transfer":
		var transfer bybit.TransferRequest
		if strictjson.Decode([]byte(o.Payload), &transfer) != nil || transfer.TransferID != exchangeUUID(o.ID) || transfer.Coin != asset.Symbol && transfer.Coin != "USDT" {
			return "", ErrDenied
		}
		summary += fmt.Sprintf("Transfer inside your Bybit account\nExact amount: %s %s\nFrom account: %s\nTo account: %s\nThis transfers the selected amount only; it does not place a trade.\n", transfer.Amount, transfer.Coin, transfer.FromAccountType, transfer.ToAccountType)
	case "withdraw":
		var withdrawal bybit.WithdrawalRequest
		if strictjson.Decode([]byte(o.Payload), &withdrawal) != nil || withdrawal.RequestID != o.ID || !exchangeWithdrawalNetwork(a, asset, withdrawal, o.ChainType, o.ContractAddress) {
			return "", ErrDenied
		}
		net := "unknown"
		if o.Fee != "" && o.FeeCoin == withdrawal.Coin {
			gross, e1 := amount.Parse(withdrawal.Amount, 36)
			fee, e2 := amount.Parse(o.Fee, 36)
			if e1 != nil || e2 != nil {
				return "", ErrDenied
			}
			difference, err := amount.Max(gross, fee)
			if err != nil {
				return "", ErrDenied
			}
			net, _ = amount.Format(difference, 36)
			net = strings.TrimRight(strings.TrimRight(net, "0"), ".")
		}
		summary += fmt.Sprintf("Withdraw to your selected owned wallet\nFrom Bybit account: FUND\nDestination: %s\nNetwork label reported by Bybit: %s\nExact Bybit chain identifier: %s\nToken contract: %s\nTotal debit including withdrawal fee: %s %s\nEstimated withdrawal fee: %s %s\nEstimated amount received: %s %s\nThe service deducts its current fee from this total debit; the final recipient amount may differ from this estimate. Only on-chain withdrawal is permitted.\n", withdrawal.Address, o.ChainType, withdrawal.Chain, o.ContractAddress, withdrawal.Amount, withdrawal.Coin, o.Fee, o.FeeCoin, net, withdrawal.Coin)
		summary += exchangeBeneficiarySummary(withdrawal.Beneficiary)
	default:
		return "", ErrDenied
	}
	return summary + "Only this operation is authorized. A timeout requires reconciliation of this original request before another operation.", nil
}
func (e *Engine) AuthorizeExchange(id string) (vault.ExchangeOperation, error) {
	o, err := e.exchangeOperation(id)
	if err != nil || o.State != "PREPARED" || o.Digest != vault.ExchangeDigest(*o) || o.ExpiresAt <= time.Now().Unix() || e.data.Exchange.Credentials == nil || o.CredentialID != vault.ExchangeCredentialID(e.data.Exchange.Credentials) {
		return vault.ExchangeOperation{}, ErrDenied
	}
	for _, other := range e.data.Exchange.Operations {
		if other.ID != id && (exchangeUnresolved(other.State) || other.State == "AUTHORIZED") {
			return vault.ExchangeOperation{}, ErrDenied
		}
	}
	a, asset, err := e.exchangeAsset(o.AccountID, o.AssetID)
	if err != nil {
		return vault.ExchangeOperation{}, err
	}
	summary, err := exchangeApproval(*o, a, asset)
	if err != nil {
		return vault.ExchangeOperation{}, err
	}
	if !e.UI.Confirm("Authorize exact Bybit operation", summary) {
		o.State = "CANCELLED"
		if err = e.persist(); err != nil {
			return vault.ExchangeOperation{}, err
		}
		return vault.ExchangeOperation{}, ErrDenied
	}
	if !e.canAuthorize() {
		return vault.ExchangeOperation{}, ErrDenied
	}
	if err = e.exchangePassword("Reauthenticate this Bybit operation"); err != nil {
		if e.canAuthorize() {
			o.State = "CANCELLED"
			if pErr := e.persist(); pErr != nil {
				return vault.ExchangeOperation{}, pErr
			}
		}
		return vault.ExchangeOperation{}, err
	}
	if o.ExpiresAt <= time.Now().Unix() || o.Digest != vault.ExchangeDigest(*o) {
		return vault.ExchangeOperation{}, ErrDenied
	}
	if o.Kind == "withdraw" {
		var w bybit.WithdrawalRequest
		if strictjson.Decode([]byte(o.Payload), &w) != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		// The anti-replay timestamp is generated immediately after password
		// authentication. All economic fields and the durable request ID stay
		// exactly as displayed; callers receive this final canonical payload.
		w.Timestamp = time.Now().UnixMilli()
		b, _ := json.Marshal(w)
		o.Payload = string(b)
		o.Digest = vault.ExchangeDigest(*o)
	}
	o.State = "AUTHORIZED"
	if err = e.persist(); err != nil {
		return vault.ExchangeOperation{}, err
	}
	if e.exchangeAuthorizations == nil {
		e.exchangeAuthorizations = map[string]string{}
	}
	e.exchangeAuthorizations[o.ID] = o.Digest
	return *o, nil
}

// ExchangeAuth releases only a narrow Bybit authentication capability. A POST
// is durably SUBMITTING before the signature can leave this process. It is
// never signed a second time, including after an ambiguous network result.
func (e *Engine) ExchangeAuth(req bybit.AuthRequest) (bybit.Auth, error) {
	x, err := e.exchangeData()
	if err != nil || x.Credentials == nil || req.Endpoint != x.Credentials.Endpoint || bybit.ValidateAuthRequest(req) != nil {
		return bybit.Auth{}, ErrDenied
	}
	delta := time.Now().UnixMilli() - req.TimestampMS
	if delta < -1000 || delta > 5000 {
		return bybit.Auth{}, ErrDenied
	}
	if req.Method == "POST" {
		var found *vault.ExchangeOperation
		for i := range x.Operations {
			o := &x.Operations[i]
			if o.Payload == req.Payload && exchangePath(o.Kind) == req.Path {
				found = o
				break
			}
		}
		if found == nil || found.State != "AUTHORIZED" || found.ExpiresAt <= time.Now().Unix() || found.Digest != vault.ExchangeDigest(*found) || found.CredentialID != vault.ExchangeCredentialID(x.Credentials) || e.exchangeAuthorizations[found.ID] != found.Digest {
			return bybit.Auth{}, ErrDenied
		}
		account, asset, err := e.exchangeAsset(found.AccountID, found.AssetID)
		if err != nil {
			return bybit.Auth{}, ErrDenied
		}
		if _, err = exchangeApproval(*found, account, asset); err != nil {
			return bybit.Auth{}, ErrDenied
		}
		delete(e.exchangeAuthorizations, found.ID)
		found.State = "SUBMITTING"
		if err = e.persist(); err != nil {
			return bybit.Auth{}, err
		}
		if !e.canAuthorize() {
			return bybit.Auth{}, ErrDenied
		}
	}
	return bybit.HMAC(string(x.Credentials.APIKey), x.Credentials.Secret, req)
}
func (e *Engine) UpdateExchange(id, state, externalID string) error {
	o, err := e.exchangeOperation(id)
	if err != nil || !exchangeUnresolved(o.State) || len(externalID) > 128 || strings.ContainsAny(externalID, "\x00\r\n") || externalID != "" && o.ExternalID != "" && externalID != o.ExternalID {
		return ErrDenied
	}
	switch state {
	case "UNKNOWN", "PENDING":
	case "CONFIRMED", "FAILED":
		if externalID == "" && o.ExternalID == "" {
			return ErrDenied
		}
	default:
		return ErrDenied
	}
	if externalID != "" {
		o.ExternalID = externalID
	}
	o.State = state
	return e.persist()
}

// RejectExchange records only a definitive response to this first POST. It
// cannot turn a previous timeout/UNKNOWN or a known remote record into failure.
// The trusted network core supplies the fixed numeric REST code; free-form
// service messages, which can echo private fields, are never persisted.
func (e *Engine) RejectExchange(id string, code int64) error {
	o, err := e.exchangeOperation(id)
	if err != nil || o.State != "SUBMITTING" || o.ExternalID != "" || !vault.ExchangeRejectionAllowed(code) {
		return ErrDenied
	}
	o.State, o.RejectionCode = "FAILED", code
	return e.persist()
}

func (e *Engine) CancelExchange(id string) error {
	o, err := e.exchangeOperation(id)
	if err != nil || o.State != "PREPARED" && o.State != "AUTHORIZED" {
		return ErrDenied
	}
	o.State = "CANCELLED"
	delete(e.exchangeAuthorizations, id)
	return e.persist()
}

// SignExchangeRequest retains a descriptive alias for internal callers.
func (e *Engine) SignExchangeRequest(req bybit.AuthRequest) (bybit.Auth, error) {
	return e.ExchangeAuth(req)
}
