package bybit

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"localwallet/internal/chains/chainutil"
)

const maxResponseBytes = 1 << 20

type Client struct {
	endpoint  string
	authorize Authorizer
	http      *http.Client
	now       func() time.Time
}

// New never accepts arbitrary endpoints, environment proxies, cookies, or
// redirects. The secret-owning signer supplies only this request's signature.
func New(endpoint string, authorize Authorizer) (*Client, error) {
	if ValidateEndpoint(endpoint) != nil {
		return nil, ErrInvalidRequest
	}
	transport := &http.Transport{
		Proxy:             nil,
		DialContext:       (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxConnsPerHost: 4,
		IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16 << 10,
		DisableCompression: true,
	}
	return &Client{endpoint: endpoint, authorize: authorize, now: time.Now,
		http: &http.Client{Transport: transport, Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close()           { c.http.CloseIdleConnections() }
func (c *Client) Endpoint() string { return c.endpoint }

// APIError contains only numeric status information. Untrusted response text,
// HTTP error bodies and request headers are never propagated into logs or UI.
type APIError struct {
	Code       int64
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.HTTPStatus != 0 {
		return "Bybit rejected request (HTTP " + strconv.Itoa(e.HTTPStatus) + ")"
	}
	return "Bybit rejected request (code " + strconv.FormatInt(e.Code, 10) + ")"
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, value any, private bool, out any) error {
	payload := query.Encode()
	if method == "POST" {
		b, e := json.Marshal(value)
		if e != nil {
			return ErrInvalidRequest
		}
		payload = string(b)
	}
	authRequest := AuthRequest{Endpoint: c.endpoint, Method: method, Path: path, Payload: payload, TimestampMS: c.now().UnixMilli(), ReceiveWindowMS: 5000}
	if ValidateAuthRequest(authRequest) != nil {
		return ErrInvalidRequest
	}
	endpoint := c.endpoint + path
	var body io.Reader
	if method == "GET" && payload != "" {
		endpoint += "?" + payload
	}
	if method == "POST" {
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return ErrInvalidRequest
	}
	req.GetBody = nil // No automatic replay of a state-changing operation.
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	if private {
		if c.authorize == nil {
			return ErrAuthorization
		}
		a, e := c.authorize(ctx, authRequest)
		if e != nil {
			return ErrAuthorization
		}
		if !identifier(a.APIKey, 128) || len(a.Signature) != 64 {
			return ErrAuthorization
		}
		signature, e := hex.DecodeString(a.Signature)
		if e != nil || len(signature) != 32 || strings.ToLower(a.Signature) != a.Signature {
			return ErrAuthorization
		}
		req.Header.Set("X-BAPI-API-KEY", a.APIKey)
		req.Header.Set("X-BAPI-SIGN", a.Signature)
		req.Header.Set("X-BAPI-TIMESTAMP", strconv.FormatInt(authRequest.TimestampMS, 10))
		req.Header.Set("X-BAPI-RECV-WINDOW", "5000")
		defer req.Header.Del("X-BAPI-API-KEY")
		defer req.Header.Del("X-BAPI-SIGN")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{HTTPStatus: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes || !utf8.Valid(b) || chainutil.CheckJSON(b) != nil {
		return ErrInvalidResponse
	}
	var envelope struct {
		RetCode *int64          `json:"retCode"`
		Result  json.RawMessage `json:"result"`
	}
	if json.Unmarshal(b, &envelope) != nil || envelope.RetCode == nil {
		return ErrInvalidResponse
	}
	if *envelope.RetCode != 0 {
		return &APIError{Code: *envelope.RetCode}
	}
	result := bytes.TrimSpace(envelope.Result)
	if len(result) == 0 || result[0] != '{' || json.Unmarshal(result, out) != nil {
		return ErrInvalidResponse
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, private bool, out any) error {
	return c.do(ctx, "GET", path, query, nil, private, out)
}
func pairQuery(symbol string) url.Values { return url.Values{"category": {"spot"}, "symbol": {symbol}} }
func cursorQuery(q url.Values, cursor string) url.Values {
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return q
}
func validCursor(s string) bool { return boundedText(s, 1024, true) }
func required(b []byte, names ...string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return false
	}
	for _, name := range names {
		v, ok := obj[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	return true
}

func (c *Client) APIKeyInfo(ctx context.Context) (APIKeyInformation, error) {
	var raw json.RawMessage
	var info APIKeyInformation
	if e := c.get(ctx, "/v5/user/query-api", nil, true, &raw); e != nil {
		return info, e
	}
	if !required(raw, "userID", "readOnly", "isMaster", "uta", "permissions") || json.Unmarshal(raw, &info) != nil || info.UserID <= 0 || (info.ReadOnly != 0 && info.ReadOnly != 1) || (info.UTA != 0 && info.UTA != 1) || len(info.Permissions.Spot) > 16 || len(info.Permissions.Wallet) > 16 {
		return APIKeyInformation{}, ErrInvalidResponse
	}
	for _, permission := range append(append([]string(nil), info.Permissions.Spot...), info.Permissions.Wallet...) {
		if !identifier(permission, 64) {
			return APIKeyInformation{}, ErrInvalidResponse
		}
	}
	return info, nil
}

func (c *Client) CoinInfo(ctx context.Context, coin string) (Coin, error) {
	var result struct {
		Rows []Coin `json:"rows"`
	}
	if e := c.get(ctx, "/v5/asset/coin/query-info", url.Values{"coin": {coin}}, true, &result); e != nil {
		return Coin{}, e
	}
	if len(result.Rows) != 1 || result.Rows[0].Coin != coin || !boundedText(result.Rows[0].Name, 120, false) || len(result.Rows[0].Chains) == 0 || len(result.Rows[0].Chains) > 128 {
		return Coin{}, ErrInvalidResponse
	}
	seen := map[string]bool{}
	for _, chain := range result.Rows[0].Chains {
		if !identifier(chain.Chain, 40) || seen[chain.Chain] || !boundedText(chain.ChainType, 120, false) || (chain.ChainDeposit != "0" && chain.ChainDeposit != "1") || (chain.ChainWithdraw != "0" && chain.ChainWithdraw != "1") || !decimal(chain.DepositMin, false) || !decimal(chain.WithdrawMin, false) || (chain.WithdrawFee != "" && !decimal(chain.WithdrawFee, false)) || !decimal(chain.WithdrawPercentageFee, false) || !boundedText(chain.ContractAddress, 256, true) {
			return Coin{}, ErrInvalidResponse
		}
		precision, e := strconv.ParseUint(chain.MinAccuracy, 10, 8)
		if e != nil || precision > 36 || strconv.FormatUint(precision, 10) != chain.MinAccuracy || (chain.WithdrawMax != "-1" && !decimal(chain.WithdrawMax, false)) {
			return Coin{}, ErrInvalidResponse
		}
		for _, count := range []string{chain.Confirmation, chain.SafeConfirmNumber} {
			if _, e := chainutil.Uint(count, 64); e != nil {
				return Coin{}, ErrInvalidResponse
			}
		}
		seen[chain.Chain] = true
	}
	return result.Rows[0], nil
}

func (c *Client) DepositAddress(ctx context.Context, coin, chain string) (Deposit, error) {
	var result struct {
		Coin   string    `json:"coin"`
		Chains []Deposit `json:"chains"`
	}
	if e := c.get(ctx, "/v5/asset/deposit/query-address", url.Values{"coin": {coin}, "chainType": {chain}}, true, &result); e != nil {
		return Deposit{}, e
	}
	if result.Coin != coin || len(result.Chains) != 1 {
		return Deposit{}, ErrInvalidResponse
	}
	d := result.Chains[0]
	if d.Chain != chain || !boundedText(d.Address, 256, false) || !boundedText(d.Tag, 120, true) || !boundedText(d.ChainType, 120, false) || !boundedText(d.ContractAddress, 256, true) || (d.BatchReleaseLimit != "-1" && !decimal(d.BatchReleaseLimit, false)) {
		return Deposit{}, ErrInvalidResponse
	}
	return d, nil
}

// AccountBalance reports transferable/safe funding values, not a fabricated
// spendable spot balance. UNIFIED margin, locked funds and debt need separate
// treatment by the caller; every trade explicitly disables leverage.
func (c *Client) AccountBalance(ctx context.Context, kind, coin string) (Balance, error) {
	to := "UNIFIED"
	if kind == "UNIFIED" {
		to = "FUND"
	}
	var result Balance
	if e := c.get(ctx, "/v5/asset/transfer/query-account-coin-balance", url.Values{"accountType": {kind}, "toAccountType": {to}, "coin": {coin}, "withTransferSafeAmount": {"1"}}, true, &result); e != nil {
		return Balance{}, e
	}
	if result.AccountType != kind || result.Balance.Coin != coin || !identifier(result.AccountID, 128) || !identifier(result.MemberID, 32) || !decimal(result.Balance.WalletBalance, false) || !decimal(result.Balance.TransferBalance, false) || !decimal(result.Balance.TransferSafeAmount, false) {
		return Balance{}, ErrInvalidResponse
	}
	return result, nil
}

func (c *Client) SpotInstrument(ctx context.Context, symbol string) (Instrument, error) {
	var r struct {
		Category string       `json:"category"`
		List     []Instrument `json:"list"`
	}
	if e := c.get(ctx, "/v5/market/instruments-info", pairQuery(symbol), false, &r); e != nil {
		return Instrument{}, e
	}
	if r.Category == "spot" && r.List != nil && len(r.List) == 0 {
		return Instrument{}, ErrNotListed
	}
	if r.Category != "spot" || len(r.List) != 1 {
		return Instrument{}, ErrInvalidResponse
	}
	i := r.List[0]
	if i.Symbol != symbol || !coinName(i.BaseCoin) || !SpotQuote(i.QuoteCoin) || i.BaseCoin == i.QuoteCoin || i.BaseCoin+i.QuoteCoin != symbol || i.Status != "Trading" || !decimal(i.LotSizeFilter.BasePrecision, true) || !decimal(i.LotSizeFilter.QuotePrecision, true) || !decimal(i.LotSizeFilter.MinOrderAmt, true) || !decimal(i.LotSizeFilter.MaxMarketOrderQty, true) || !decimal(i.PriceFilter.TickSize, true) {
		return Instrument{}, ErrInvalidResponse
	}
	return i, nil
}
func (c *Client) SpotTicker(ctx context.Context, symbol string) (Ticker, error) {
	var r struct {
		Category string   `json:"category"`
		List     []Ticker `json:"list"`
	}
	if e := c.get(ctx, "/v5/market/tickers", pairQuery(symbol), false, &r); e != nil {
		return Ticker{}, e
	}
	if r.Category != "spot" || len(r.List) != 1 {
		return Ticker{}, ErrInvalidResponse
	}
	t := r.List[0]
	if t.Symbol != symbol || !decimal(t.LastPrice, true) || !decimal(t.Bid1Price, true) || !decimal(t.Ask1Price, true) {
		return Ticker{}, ErrInvalidResponse
	}
	return t, nil
}
func (c *Client) SpotFeeRate(ctx context.Context, symbol string) (FeeRate, error) {
	var r struct {
		List []FeeRate `json:"list"`
	}
	if e := c.get(ctx, "/v5/account/fee-rate", pairQuery(symbol), true, &r); e != nil {
		return FeeRate{}, e
	}
	if len(r.List) != 1 || r.List[0].Symbol != symbol || !decimalRange(r.List[0].TakerFeeRate, 0, 100000000, 8) || !decimalRange(r.List[0].MakerFeeRate, 0, 100000000, 8) {
		return FeeRate{}, ErrInvalidResponse
	}
	return r.List[0], nil
}

func (c *Client) CreateOrder(ctx context.Context, order SpotOrder) (OrderAcknowledgement, error) {
	var r OrderAcknowledgement
	if e := c.do(ctx, "POST", "/v5/order/create", nil, order, true, &r); e != nil {
		return r, e
	}
	if !identifier(r.OrderID, 128) || r.OrderLinkID != order.OrderLinkID {
		return OrderAcknowledgement{}, ErrInvalidResponse
	}
	return r, nil
}
func (c *Client) FindOrder(ctx context.Context, symbol, linkID string) (*Order, error) {
	q := pairQuery(symbol)
	q.Set("orderLinkId", linkID)
	for _, path := range []string{"/v5/order/realtime", "/v5/order/history"} {
		var r struct {
			Category       string  `json:"category"`
			List           []Order `json:"list"`
			NextPageCursor string  `json:"nextPageCursor"`
		}
		if e := c.get(ctx, path, q, true, &r); e != nil {
			return nil, e
		}
		if r.Category != "spot" || r.List == nil || len(r.List) > 1 || r.NextPageCursor != "" {
			return nil, ErrInvalidResponse
		}
		if len(r.List) == 0 {
			continue
		}
		o := r.List[0]
		unit := "baseCoin"
		if o.Side == "Buy" {
			unit = "quoteCoin"
		}
		if o.Symbol != symbol || o.OrderLinkID != linkID || !identifier(o.OrderID, 128) || (o.Side != "Buy" && o.Side != "Sell") || o.OrderType != "Market" || o.MarketUnit != unit || o.IsLeverage != "0" || !decimal(o.Qty, true) || !decimal(o.CumExecQty, false) || !decimal(o.CumExecValue, false) || (o.CumExecFee != "" && !decimal(o.CumExecFee, false)) || (o.AvgPrice != "" && !decimal(o.AvgPrice, false)) || !identifier(o.OrderStatus, 64) || !boundedText(o.RejectReason, 120, true) {
			return nil, ErrInvalidResponse
		}
		return &o, nil
	}
	return nil, nil // Absence is not proof that a submission failed.
}
func (c *Client) Executions(ctx context.Context, symbol, linkID, cursor string) (ExecutionPage, error) {
	q := pairQuery(symbol)
	q.Set("orderLinkId", linkID)
	q.Set("limit", "100")
	var r ExecutionPage
	if e := c.get(ctx, "/v5/execution/list", cursorQuery(q, cursor), true, &r); e != nil {
		return r, e
	}
	if r.Category != "spot" || r.List == nil || len(r.List) > 100 || !validCursor(r.NextPageCursor) || (r.NextPageCursor != "" && r.NextPageCursor == cursor) {
		return ExecutionPage{}, ErrInvalidResponse
	}
	seen := map[string]bool{}
	for _, x := range r.List {
		if x.Symbol != symbol || x.OrderLinkID != linkID || !identifier(x.OrderID, 128) || !identifier(x.ExecID, 128) || seen[x.ExecID] || (x.Side != "Buy" && x.Side != "Sell") || x.OrderType != "Market" || !decimal(x.OrderQty, true) || !decimal(x.ExecPrice, true) || !decimal(x.ExecQty, true) || !decimal(x.ExecValue, true) || !decimal(x.ExecFee, false) || !coinName(x.FeeCurrency) {
			return ExecutionPage{}, ErrInvalidResponse
		}
		seen[x.ExecID] = true
	}
	return r, nil
}

func validTransferStatus(s string) bool {
	return s == "STATUS_UNKNOWN" || s == "SUCCESS" || s == "PENDING" || s == "FAILED"
}
func (c *Client) Transfer(ctx context.Context, transfer TransferRequest) (TransferAcknowledgement, error) {
	var r TransferAcknowledgement
	if e := c.do(ctx, "POST", "/v5/asset/transfer/inter-transfer", nil, transfer, true, &r); e != nil {
		return r, e
	}
	if r.TransferID != transfer.TransferID || !validTransferStatus(r.Status) {
		return TransferAcknowledgement{}, ErrInvalidResponse
	}
	return r, nil
}
func (c *Client) TransferStatus(ctx context.Context, id string) (*TransferRecord, error) {
	var r struct {
		List           []TransferRecord `json:"list"`
		NextPageCursor string           `json:"nextPageCursor"`
	}
	if e := c.get(ctx, "/v5/asset/transfer/query-inter-transfer-list", url.Values{"transferId": {id}, "limit": {"50"}}, true, &r); e != nil {
		return nil, e
	}
	if r.List == nil || len(r.List) > 1 || r.NextPageCursor != "" {
		return nil, ErrInvalidResponse
	}
	if len(r.List) == 0 {
		return nil, nil
	}
	t := r.List[0]
	if ValidateTransfer(TransferRequest{TransferID: t.TransferID, Coin: t.Coin, Amount: t.Amount, FromAccountType: t.FromAccountType, ToAccountType: t.ToAccountType}) != nil || t.TransferID != id || !validTransferStatus(t.Status) {
		return nil, ErrInvalidResponse
	}
	return &t, nil
}
func (c *Client) Withdraw(ctx context.Context, withdrawal WithdrawalRequest) (WithdrawalAcknowledgement, error) {
	var r WithdrawalAcknowledgement
	if e := c.do(ctx, "POST", "/v5/asset/withdraw/create", nil, withdrawal, true, &r); e != nil {
		return r, e
	}
	if !identifier(r.ID, 128) {
		return WithdrawalAcknowledgement{}, ErrInvalidResponse
	}
	return r, nil
}
func (c *Client) WithdrawalRecords(ctx context.Context, coin, id, cursor string) (WithdrawalPage, error) {
	var r WithdrawalPage
	q := url.Values{"coin": {coin}, "withdrawID": {id}, "withdrawType": {"0"}, "limit": {"50"}}
	if e := c.get(ctx, "/v5/asset/withdraw/query-record", cursorQuery(q, cursor), true, &r); e != nil {
		return r, e
	}
	if r.Rows == nil || len(r.Rows) > 1 || !validCursor(r.NextPageCursor) || (r.NextPageCursor != "" && r.NextPageCursor == cursor) {
		return WithdrawalPage{}, ErrInvalidResponse
	}
	for _, w := range r.Rows {
		if w.Coin != coin || w.WithdrawID != id || !identifier(w.Chain, 40) || !boundedText(w.ToAddress, 256, false) || !boundedText(w.Tag, 120, true) || !decimal(w.Amount, true) || !decimal(w.WithdrawFee, false) || !identifier(w.Status, 64) || w.WithdrawType != 0 || !boundedText(w.TxID, 256, true) {
			return WithdrawalPage{}, ErrInvalidResponse
		}
	}
	return r, nil
}
func (c *Client) DepositRecords(ctx context.Context, coin, txID, cursor string) (DepositPage, error) {
	var r DepositPage
	q := url.Values{"coin": {coin}, "txID": {txID}, "limit": {"50"}}
	if e := c.get(ctx, "/v5/asset/deposit/query-record", cursorQuery(q, cursor), true, &r); e != nil {
		return r, e
	}
	if r.Rows == nil || len(r.Rows) > 50 || !validCursor(r.NextPageCursor) || (r.NextPageCursor != "" && r.NextPageCursor == cursor) {
		return DepositPage{}, ErrInvalidResponse
	}
	seen := map[string]bool{}
	for _, d := range r.Rows {
		if d.Coin != coin || d.TxID != txID || !identifier(d.ID, 128) || seen[d.ID] || !identifier(d.Chain, 40) || !decimal(d.Amount, true) || !boundedText(d.ToAddress, 256, false) || !boundedText(d.Tag, 120, true) || d.Status < 0 || d.Status > 100 || (d.DepositFee != "" && !decimal(d.DepositFee, false)) || !boundedText(d.SuccessAt, 32, true) || !boundedText(d.Confirmations, 64, true) || !boundedText(d.DepositType, 16, true) || d.TravelRuleStatus < 0 || d.TravelRuleStatus > 3 {
			return DepositPage{}, ErrInvalidResponse
		}
		seen[d.ID] = true
	}
	return r, nil
}
