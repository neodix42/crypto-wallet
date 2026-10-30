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
	Provider       string                    `json:"provider"`
	Providers      []ExchangeConnection      `json:"providers"`
	Connected      bool                      `json:"connected"`
	Endpoint       string                    `json:"endpoint,omitempty"`
	KeySuffix      string                    `json:"keySuffix,omitempty"`
	CredentialID   string                    `json:"credentialId,omitempty"`
	Operations     []vault.ExchangeOperation `json:"operations"`
	OperationCount int                       `json:"operationCount"`
}
type ExchangeConnection struct {
	Provider  string `json:"provider"`
	Connected bool   `json:"connected"`
	Endpoint  string `json:"endpoint,omitempty"`
	KeySuffix string `json:"keySuffix,omitempty"`
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
	s := ExchangeStatus{Provider: x.Selected(), Providers: []ExchangeConnection{}, Operations: []vault.ExchangeOperation{}, OperationCount: len(x.Operations)}
	for _, provider := range exchange.Providers() {
		connection := ExchangeConnection{Provider: provider.ID}
		if credential := x.Credential(provider.ID); credential != nil {
			connection.Connected = true
			connection.Endpoint = credential.Endpoint
			if len(credential.APIKey) >= 4 {
				connection.KeySuffix = string(credential.APIKey[len(credential.APIKey)-4:])
			}
		}
		s.Providers = append(s.Providers, connection)
	}
	if c := x.Credential(x.Selected()); c != nil {
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

// ConnectExchange requests credentials through the trusted masked wallet
// dialog in the same Wails window. An already unlocked, recovery-confirmed session may
// connect without entering its storage password again; spending and disconnect
// keep their own password checks. Neither the public portfolio nor core receives
// the secret outside that transient prompt reply.
func (e *Engine) ConnectExchange(endpoint string) error {
	return e.ConnectExchangeProvider("bybit", endpoint)
}

func (e *Engine) SelectExchange(provider string) error {
	x, err := e.exchangeData()
	service, ok := exchange.ProviderByID(provider)
	if err != nil || !ok || provider == "" || e.data.Environment != "mainnet" {
		return ErrDenied
	}
	if x.Selected() == service.ID {
		return nil
	}
	for _, operation := range x.Operations {
		if exchangeUnresolved(operation.State) || operation.State == "AUTHORIZED" {
			return ErrDenied
		}
	}
	previous := x.SelectedProvider
	x.SelectedProvider = service.ID
	if err = e.persist(); err != nil {
		x.SelectedProvider = previous
		return err
	}
	e.exchangeAuthorizations = nil
	return nil
}

func (e *Engine) ConnectExchangeProvider(provider, endpoint string) error {
	x, err := e.exchangeData()
	service, known := exchange.ProviderByID(provider)
	if err != nil || !known || e.data.Environment != "mainnet" || !exchange.ValidateProviderEndpoint(provider, endpoint) {
		return ErrDenied
	}
	for _, operation := range x.Operations {
		if operation.State == "AUTHORIZED" {
			return ErrDenied
		}
	}
	data, generation := e.data, e.data.Generation
	current := func() bool {
		return e.canAuthorize() && e.data == data && data.Generation == generation && data.Exchange == x
	}
	labels := []string{service.Name + " API key", service.Name + " API secret"}
	if service.RequiresPassphrase {
		labels = append(labels, "KuCoin API passphrase", "KuCoin API key version (2 or 3)")
	}
	in, err := e.UI.Input("Connect "+service.Name+" — enter your API key and matching API secret", labels...)
	if err != nil {
		return err
	}
	defer wipeInputs(in)
	if len(in) != len(labels) || !current() || !exchangeCredentialBytes(in[0], true) || !exchangeCredentialBytes(in[1], false) {
		return ErrDenied
	}
	if service.RequiresPassphrase {
		if len(in[2]) < 1 || len(in[2]) > 256 {
			return ErrDenied
		}
		for _, value := range in[2] {
			if value < 33 || value > 126 {
				return ErrDenied
			}
		}
		if string(in[3]) != "2" && string(in[3]) != "3" {
			return ErrDenied
		}
	}
	c := &vault.ExchangeCredentials{Endpoint: endpoint, APIKey: bytes.Clone(in[0]), Secret: bytes.Clone(in[1])}
	if service.ID != "bybit" {
		c.Provider = service.ID
	}
	if service.RequiresPassphrase {
		c.Passphrase = bytes.Clone(in[2])
		c.KeyVersion = int(in[3][0] - '0')
	}
	keep := false
	defer func() {
		if !keep {
			clear(c.APIKey)
			clear(c.Secret)
			clear(c.Passphrase)
		}
	}()
	for _, o := range x.Operations {
		if exchangeUnresolved(o.State) && o.CredentialID != vault.ExchangeCredentialID(c) {
			return ErrDenied
		}
	}
	if !e.UI.Confirm("Approve "+service.Name+" custody and API access", "Service: "+service.Name+"\nEndpoint: "+endpoint+"\nDeposited assets are held by "+service.Name+", which controls trading, custody, withdrawals, fees and availability. API credentials permit the permissions configured in your "+service.Name+" account. This wallet supports unleveraged spot trades and withdrawals only to its selected owned accounts. No deposit, trade, transfer or withdrawal is sent by connecting. Keep the API key restricted and whitelist your wallet withdrawal addresses on "+service.Name+". The exchange secret is stored only inside the encrypted local vault; recovery phrases do not restore it.") || !current() {
		return ErrDenied
	}
	if old := x.Credential(service.ID); old != nil {
		clear(old.APIKey)
		clear(old.Secret)
		clear(old.Passphrase)
	}
	x.SetCredential(service.ID, c)
	x.SelectedProvider = service.ID
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
	service, _ := exchange.ProviderByID(x.Selected())
	if err = e.exchangePassword("Reauthenticate " + service.Name + " disconnection"); err != nil {
		return err
	}
	if credential := x.Credential(x.Selected()); credential != nil {
		clear(credential.APIKey)
		clear(credential.Secret)
		clear(credential.Passphrase)
		x.SetCredential(x.Selected(), nil)
	}
	e.exchangeAuthorizations = nil
	for i := range x.Operations {
		provider, known := exchange.ProviderForEndpoint(x.Operations[i].Endpoint)
		if known && provider.ID == service.ID && (x.Operations[i].State == "PREPARED" || x.Operations[i].State == "AUTHORIZED") {
			x.Operations[i].State = "CANCELLED"
		}
	}
	return e.persist()
}
func (e *Engine) exchangeAsset(accountID, assetID string) (model.Account, model.Asset, error) {
	for _, token := range e.NEP141TokenPolicies() {
		if token.Asset.ID == assetID {
			return model.Account{}, model.Asset{}, ErrDenied
		}
	}
	for _, token := range e.TokenPolicies() {
		if token.Asset.ID == assetID {
			return model.Account{}, model.Asset{}, ErrDenied
		}
	}
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
func exchangeWithdrawalNetwork(a model.Account, asset model.Asset, w bybit.WithdrawalRequest, chainType, contract string, selected ...string) bool {
	if w.Coin != asset.Symbol || w.Address != a.Address || w.Tag != "" {
		return false
	}
	provider := "bybit"
	if len(selected) == 1 {
		provider = selected[0]
	}
	if len(selected) > 1 {
		return false
	}
	return exchange.ValidateProviderRoute(provider, a, asset, w.Chain, chainType, contract) == nil
}
func (e *Engine) PrepareExchange(in ExchangeIntent) (vault.ExchangeOperation, error) {
	x, err := e.exchangeData()
	if err != nil || x.Credential(x.Selected()) == nil || e.data.Environment != "mainnet" || len(x.Operations) >= 1000 {
		return vault.ExchangeOperation{}, ErrDenied
	}
	credential := x.Credential(x.Selected())
	for _, o := range x.Operations {
		if exchangeUnresolved(o.State) || o.State == "AUTHORIZED" {
			return vault.ExchangeOperation{}, ErrDenied
		}
	}
	a, asset, err := e.exchangeAsset(in.AccountID, in.AssetID)
	if err != nil {
		return vault.ExchangeOperation{}, err
	}
	if in.LastPrice != "" && !exchangeDecimal(in.LastPrice, true) || in.Fee != "" && !exchangeDecimal(in.Fee, false) {
		return vault.ExchangeOperation{}, ErrDenied
	}
	id := ID()
	var payload any
	switch in.Kind {
	case "swap":
		// Classic KuCoin offers no reviewed read-only proof that a later
		// additional KCS fee debit is disabled. Never approve that extra asset.
		if x.Selected() == "kucoin" {
			return vault.ExchangeOperation{}, ErrDenied
		}
		if in.Order == nil || in.Transfer != nil || in.Withdrawal != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		o := *in.Order
		o.OrderLinkID = id
		base, quote, pairOK := bybit.SpotPair(o.Symbol)
		orderErr := bybit.ValidateSpotOrder(o)
		if x.Selected() != "bybit" {
			orderErr = exchange.ValidateIOCOrder(o)
		}
		if !pairOK || base != asset.Symbol || orderErr != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		feeCoin := quote
		if o.Side == "Buy" && x.Selected() != "kucoin" {
			feeCoin = base
		}
		if in.FeeCoin != "" && in.FeeCoin != feeCoin {
			return vault.ExchangeOperation{}, ErrDenied
		}
		b, _ := amount.Parse(o.SlippageTolerance, 2)
		n, _ := new(big.Int).SetString(b, 10)
		if n == nil || n.Cmp(big.NewInt(100)) > 0 {
			return vault.ExchangeOperation{}, ErrDenied
		}
		payload = o
	case "transfer":
		if x.Selected() == "kucoin" {
			// A FlexTransfer acknowledgement has no reviewed exact Classic
			// status lookup. Do not create a fresh unreconcilable reservation.
			return vault.ExchangeOperation{}, ErrDenied
		}
		if in.Transfer == nil || in.Order != nil || in.Withdrawal != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		t := *in.Transfer
		t.TransferID = exchangeUUID(id)
		if (t.Coin != asset.Symbol && !bybit.SpotQuote(t.Coin)) || bybit.ValidateTransfer(t) != nil || in.FeeCoin != "" && in.FeeCoin != t.Coin {
			return vault.ExchangeOperation{}, ErrDenied
		}
		payload = t
	case "withdraw":
		if in.Withdrawal == nil || in.Order != nil || in.Transfer != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		// These entities require separate documented questionnaires which this
		// wallet does not collect. Refuse before creating a remote operation.
		switch credential.Endpoint {
		case "https://api.bybit.tr", "https://api.bybit.kz", "https://api.bybit.eu", "https://api.bybit.id":
			return vault.ExchangeOperation{}, ErrDenied
		}
		w := *in.Withdrawal
		if w.Beneficiary != nil {
			return vault.ExchangeOperation{}, ErrDenied
		}
		w.RequestID, w.Timestamp = id, time.Now().UnixMilli()
		valid := bybit.ValidateWithdrawal(w) == nil
		if x.Selected() != "bybit" {
			encoded, _ := json.Marshal(w)
			_, nativeErr := exchangeOperationRequest(vault.ExchangeOperation{Endpoint: credential.Endpoint, Kind: "withdraw", Payload: string(encoded)}, time.Now().UnixMilli())
			valid = nativeErr == nil
		}
		if !valid || !exchangeWithdrawalNetwork(a, asset, w, in.ChainType, in.ContractAddress, x.Selected()) || in.FeeCoin != "" && in.FeeCoin != asset.Symbol {
			return vault.ExchangeOperation{}, ErrDenied
		}
		if credential.Endpoint == "https://api.bybit.ae" {
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
	o := vault.ExchangeOperation{ID: id, Kind: in.Kind, Endpoint: credential.Endpoint, CredentialID: vault.ExchangeCredentialID(credential), AccountID: a.ID, AssetID: asset.ID, Payload: string(b), State: "PREPARED", CreatedAt: now, ExpiresAt: now + 300, LastPrice: in.LastPrice, Fee: in.Fee, FeeCoin: in.FeeCoin, ChainType: in.ChainType, ContractAddress: in.ContractAddress}
	if _, err = exchangeOperationRequest(o, time.Now().UnixMilli()); err != nil {
		return vault.ExchangeOperation{}, ErrDenied
	}
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
	service, known := exchange.ProviderForEndpoint(o.Endpoint)
	if !known {
		return "", ErrDenied
	}
	if service.ID == "kucoin" && (o.Kind == "swap" || o.Kind == "transfer") {
		return "", ErrDenied
	}
	if _, err := exchangeOperationRequest(o, time.Now().UnixMilli()); err != nil {
		return "", ErrDenied
	}
	summary := fmt.Sprintf("Service: %s\nEndpoint: %s\nWallet network: %s\nOwned wallet address: %s\nAsset identity: %s\nOperation ID: %s\n", service.Name, o.Endpoint, a.Network, a.Address, o.AssetID, o.ID)
	switch o.Kind {
	case "swap":
		var order bybit.SpotOrder
		if strictjson.Decode([]byte(o.Payload), &order) != nil || order.OrderLinkID != o.ID {
			return "", ErrDenied
		}
		base, quote, pairOK := bybit.SpotPair(order.Symbol)
		if !pairOK || base != asset.Symbol {
			return "", ErrDenied
		}
		spend := asset.Symbol
		if order.Side == "Buy" {
			spend = quote
		}
		if service.ID == "bybit" {
			summary += fmt.Sprintf("Spot market order: %s %s\nExact spending quantity: %s %s\nMaximum slippage: %s%% from Bybit's current best ask/bid\nMargin or borrowing: disabled\nLatest observed price (indicative): %s %s per %s\nEstimated trading fee: %s %s\nMarket orders can fill partially or not fill. The last price is not an execution guarantee.\n", order.Side, order.Symbol, order.Qty, spend, order.SlippageTolerance, o.LastPrice, quote, asset.Symbol, o.Fee, o.FeeCoin)
		} else {
			summary += fmt.Sprintf("Price-bounded spot order: %s %s\nMaximum spending quantity: %s %s\nExact base quantity: %s %s\nLimit price: %s %s per %s\nTime in force: IOC (cancel unfilled remainder immediately)\nSelected slippage: %s%% (already incorporated in this fixed price)\nMargin or borrowing: disabled\nLatest observed price (indicative): %s\nEstimated trading fee: %s %s\nThis limit order can fill partially or not fill. Only the exact quantity and fixed price above are authorized.\n", order.Side, order.Symbol, order.Qty, spend, order.BaseQty, base, order.LimitPrice, quote, base, order.SlippageTolerance, o.LastPrice, o.Fee, o.FeeCoin)
		}
	case "transfer":
		var transfer bybit.TransferRequest
		if strictjson.Decode([]byte(o.Payload), &transfer) != nil || transfer.TransferID != exchangeUUID(o.ID) || transfer.Coin != asset.Symbol && !bybit.SpotQuote(transfer.Coin) {
			return "", ErrDenied
		}
		summary += fmt.Sprintf("Transfer inside your %s account\nExact amount: %s %s\nFrom account: %s\nTo account: %s\nThis transfers the selected amount only; it does not place a trade.\n", service.Name, transfer.Amount, transfer.Coin, transfer.FromAccountType, transfer.ToAccountType)
	case "withdraw":
		var withdrawal bybit.WithdrawalRequest
		if strictjson.Decode([]byte(o.Payload), &withdrawal) != nil || withdrawal.RequestID != o.ID || !exchangeWithdrawalNetwork(a, asset, withdrawal, o.ChainType, o.ContractAddress, service.ID) {
			return "", ErrDenied
		}
		if service.ID != "bybit" {
			summary += fmt.Sprintf("Withdraw to your selected owned wallet\nDestination: %s\nNetwork label: %s\nExact service chain identifier: %s\nToken contract: %s\nExact withdrawal API amount: %s %s\nFee policy: %s\nEstimated additional withdrawal fee reserved: %s %s\nFees and actual recipient credit are controlled by the exchange. The API amount is preserved exactly; estimated fees are not a guaranteed maximum. Only on-chain withdrawal to this owned address is authorized.\n", withdrawal.Address, o.ChainType, withdrawal.Chain, o.ContractAddress, withdrawal.Amount, withdrawal.Coin, withdrawal.FeeMode, o.Fee, o.FeeCoin)
			break
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
	if err != nil || o.State != "PREPARED" || o.Digest != vault.ExchangeDigest(*o) || o.ExpiresAt <= time.Now().Unix() || e.data.Exchange.Credential(e.data.Exchange.Selected()) == nil || o.CredentialID != vault.ExchangeCredentialID(e.data.Exchange.Credential(e.data.Exchange.Selected())) {
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
	service, _ := exchange.ProviderForEndpoint(o.Endpoint)
	if !e.UI.Confirm("Authorize exact "+service.Name+" operation", summary) {
		o.State = "CANCELLED"
		if err = e.persist(); err != nil {
			return vault.ExchangeOperation{}, err
		}
		return vault.ExchangeOperation{}, ErrDenied
	}
	if !e.canAuthorize() {
		return vault.ExchangeOperation{}, ErrDenied
	}
	if err = e.exchangePassword("Reauthenticate this " + service.Name + " operation"); err != nil {
		if e.canAuthorize() {
			o.State = "CANCELLED"
			if pErr := e.persist(); pErr != nil {
				return vault.ExchangeOperation{}, pErr
			}
		}
		return vault.ExchangeOperation{}, err
	}
	if o.ExpiresAt <= time.Now().Unix() || o.Digest != vault.ExchangeDigest(*o) || o.CredentialID != vault.ExchangeCredentialID(e.data.Exchange.Credential(e.data.Exchange.Selected())) {
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

// ExchangeAuth releases only a narrow selected-service capability. Every
// funds-moving request is durably SUBMITTING before its signature leaves. It is
// never signed a second time, including after an ambiguous network result.
func (e *Engine) ExchangeAuth(req bybit.AuthRequest) (bybit.Auth, error) {
	x, err := e.exchangeData()
	if err != nil || x.Credential(x.Selected()) == nil || req.Endpoint != x.Credential(x.Selected()).Endpoint {
		return bybit.Auth{}, ErrDenied
	}
	delta := time.Now().UnixMilli() - req.TimestampMS
	if delta < -1000 || delta > 5000 {
		return bybit.Auth{}, ErrDenied
	}
	credential := x.Credential(x.Selected())
	readOnly, policyErr := exchangeRequestPolicy(x.Selected(), req)
	if policyErr != nil {
		return bybit.Auth{}, ErrDenied
	}
	if !readOnly {
		var found *vault.ExchangeOperation
		for i := range x.Operations {
			o := &x.Operations[i]
			expected, err := exchangeOperationRequest(*o, req.TimestampMS)
			if err == nil && expected == req {
				if found != nil {
					return bybit.Auth{}, ErrDenied
				}
				found = o
			}
		}
		if found == nil || found.State != "AUTHORIZED" || found.ExpiresAt <= time.Now().Unix() || found.Digest != vault.ExchangeDigest(*found) || found.CredentialID != vault.ExchangeCredentialID(credential) || e.exchangeAuthorizations[found.ID] != found.Digest {
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
	return exchangeRequestAuth(x.Selected(), credential, req)
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
	provider, ok := exchange.ProviderForEndpoint(o.Endpoint)
	if !ok || provider.ID != "bybit" {
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
