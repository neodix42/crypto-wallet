// Package binance implements the bounded global Binance spot/funding API.
// Secrets remain with a caller-owned authorizer; the HTTP client receives only
// a signature for the exact fixed endpoint, operation and encoded parameters.
package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"localwallet/internal/amount"
	"localwallet/internal/chains/chainutil"
	"localwallet/internal/exchange/bybit"
)

const Endpoint = "https://api.binance.com"

var (
	ErrInvalidRequest  = errors.New("invalid Binance request")
	ErrInvalidResponse = errors.New("invalid or incomplete Binance response")
	ErrUnavailable     = errors.New("Binance connection unavailable; reconcile the original operation before retrying")
	ErrAuthorization   = errors.New("Binance authorization unavailable")
	ErrTravelRule      = errors.New("Binance requires an unsupported regional withdrawal questionnaire")
	ErrFeeCurrency     = errors.New("Binance BNB fee payment is enabled; configure supported fee payment before trading")
)

func text(s string, max int, empty bool) bool {
	if (!empty && s == "") || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func identifier(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func coin(s string) bool {
	if !identifier(s, 24) || strings.ContainsAny(s, "-_") || s != strings.ToUpper(s) {
		return false
	}
	return true
}
func pair(s string) bool { _, _, ok := bybit.SpotPair(s); return ok }
func decimal(s string, positive bool) bool {
	v, e := amount.Parse(s, 36)
	return e == nil && (!positive || v != "0")
}
func number(s string, positive bool) bool {
	v, e := strconv.ParseUint(s, 10, 64)
	return e == nil && strconv.FormatUint(v, 10) == s && (!positive || v != 0)
}
func uuid(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func txid(s string) bool {
	if !text(s, 256, false) {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_/+=:", c)) {
			return false
		}
	}
	return true
}
func rat(s string) *big.Rat { r, _ := new(big.Rat).SetString(s); return r }
func validOrder(o bybit.SpotOrder) bool {
	unit := "baseCoin"
	if o.Side == "Buy" {
		unit = "quoteCoin"
	} else if o.Side != "Sell" {
		return false
	}
	if o.Category != "spot" || !pair(o.Symbol) || o.OrderType != "Limit" || o.IsLeverage != 0 || o.MarketUnit != unit || !decimal(o.Qty, true) || !decimal(o.BaseQty, true) || !decimal(o.LimitPrice, true) || !identifier(o.OrderLinkID, 36) || o.SlippageToleranceType != "Percent" || !decimal(o.SlippageTolerance, true) {
		return false
	}
	slippage := rat(o.SlippageTolerance)
	_, slippageError := amount.Parse(o.SlippageTolerance, 2)
	if slippageError != nil || slippage.Cmp(big.NewRat(1, 100)) < 0 || slippage.Cmp(big.NewRat(10, 1)) > 0 {
		return false
	}
	spend := rat(o.BaseQty)
	if o.Side == "Buy" {
		spend.Mul(spend, rat(o.LimitPrice))
	} else {
		return spend.Cmp(rat(o.Qty)) == 0
	}
	return spend.Cmp(rat(o.Qty)) <= 0
}
func orderQuery(o bybit.SpotOrder) (url.Values, error) {
	if !validOrder(o) {
		return nil, ErrInvalidRequest
	}
	return url.Values{"symbol": {o.Symbol}, "side": {strings.ToUpper(o.Side)}, "type": {"LIMIT"}, "timeInForce": {"IOC"}, "quantity": {o.BaseQty}, "price": {o.LimitPrice}, "newClientOrderId": {o.OrderLinkID}, "newOrderRespType": {"ACK"}}, nil
}
func transferQuery(t bybit.TransferRequest) (url.Values, error) {
	if !uuid(t.TransferID) || !coin(t.Coin) || !decimal(t.Amount, true) {
		return nil, ErrInvalidRequest
	}
	kind := ""
	if t.FromAccountType == "FUND" && t.ToAccountType == "UNIFIED" {
		kind = "FUNDING_MAIN"
	}
	if t.FromAccountType == "UNIFIED" && t.ToAccountType == "FUND" {
		kind = "MAIN_FUNDING"
	}
	if kind == "" {
		return nil, ErrInvalidRequest
	}
	return url.Values{"type": {kind}, "asset": {t.Coin}, "amount": {t.Amount}}, nil
}
func withdrawalQuery(w bybit.WithdrawalRequest) (url.Values, error) {
	if !coin(w.Coin) || !identifier(w.Chain, 40) || !text(w.Address, 256, false) || !text(w.Tag, 120, true) || !decimal(w.Amount, true) || w.Timestamp <= 0 || w.ForceChain != 1 || w.AccountType != "FUND" || w.FeeType != 0 || w.FeeMode != "binance-native" || w.Beneficiary != nil || !identifier(w.RequestID, 32) {
		return nil, ErrInvalidRequest
	}
	q := url.Values{"coin": {w.Coin}, "network": {w.Chain}, "address": {w.Address}, "amount": {w.Amount}, "withdrawOrderId": {w.RequestID}, "walletType": {"1"}}
	if w.Tag != "" {
		q.Set("addressTag", w.Tag)
	}
	return q, nil
}

// OperationRequest reconstructs the exact native write from the canonical
// immutable operation stored by the signer. It grants no operation approval.
func OperationRequest(endpoint, kind, payload string, timestampMS int64) (bybit.AuthRequest, error) {
	r := bybit.AuthRequest{Endpoint: endpoint, Method: "POST", TimestampMS: timestampMS, ReceiveWindowMS: 5000}
	var q url.Values
	var err error
	var value any
	switch kind {
	case "swap":
		var o bybit.SpotOrder
		if chainutil.DecodeStrict([]byte(payload), &o) != nil {
			return r, ErrInvalidRequest
		}
		value = o
		q, err = orderQuery(o)
		r.Path = "/api/v3/order"
	case "transfer":
		var t bybit.TransferRequest
		if chainutil.DecodeStrict([]byte(payload), &t) != nil {
			return r, ErrInvalidRequest
		}
		value = t
		q, err = transferQuery(t)
		r.Path = "/sapi/v1/asset/transfer"
	case "withdraw":
		var w bybit.WithdrawalRequest
		if chainutil.DecodeStrict([]byte(payload), &w) != nil {
			return r, ErrInvalidRequest
		}
		value = w
		q, err = withdrawalQuery(w)
		r.Path = "/sapi/v1/capital/withdraw/apply"
	default:
		return r, ErrInvalidRequest
	}
	b, e := json.Marshal(value)
	if err != nil || e != nil || string(b) != payload {
		return r, ErrInvalidRequest
	}
	r.Payload = q.Encode()
	if ValidateAuthRequest(r) != nil {
		return r, ErrInvalidRequest
	}
	return r, nil
}
func queryKeys(q url.Values, required []string, optional ...string) bool {
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
		if q.Get(k) == "" {
			return false
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k, values := range q {
		if !allowed[k] || len(values) != 1 || !text(values[0], 1024, false) {
			return false
		}
	}
	return true
}
func offset(s string) bool {
	n, e := strconv.ParseUint(s, 10, 32)
	return e == nil && strconv.FormatUint(n, 10) == s && n <= 10000
}

// ValidateAuthRequest allows only canonical business parameters for reviewed
// paths. Timestamp/window/signature cannot be smuggled into the signed payload.
func ValidateAuthRequest(r bybit.AuthRequest) error {
	if r.Endpoint != Endpoint || r.TimestampMS <= 0 || r.ReceiveWindowMS != 5000 || len(r.Payload) > 8192 {
		return ErrInvalidRequest
	}
	q, e := url.ParseQuery(r.Payload)
	if e != nil || q.Encode() != r.Payload {
		return ErrInvalidRequest
	}
	valid := false
	if r.Method == "GET" {
		switch r.Path {
		case "/sapi/v1/account/apiRestrictions", "/sapi/v1/capital/config/getall", "/api/v3/account", "/sapi/v1/localentity/questionnaire-requirements":
			valid = queryKeys(q, nil)
		case "/sapi/v1/capital/deposit/address":
			valid = queryKeys(q, []string{"coin", "network"}) && coin(q.Get("coin")) && identifier(q.Get("network"), 40)
		case "/api/v3/exchangeInfo", "/api/v3/ticker/24hr", "/api/v3/account/commission":
			valid = queryKeys(q, []string{"symbol"}) && pair(q.Get("symbol"))
		case "/api/v3/order":
			valid = queryKeys(q, []string{"symbol", "origClientOrderId"}) && pair(q.Get("symbol")) && identifier(q.Get("origClientOrderId"), 36)
		case "/api/v3/myTrades":
			valid = queryKeys(q, []string{"symbol", "orderId", "limit"}, "fromId") && pair(q.Get("symbol")) && number(q.Get("orderId"), true) && q.Get("limit") == "100" && (q.Get("fromId") == "" || number(q.Get("fromId"), false))
		case "/sapi/v1/asset/transfer":
			valid = queryKeys(q, []string{"type", "current", "size"}) && (q.Get("type") == "FUNDING_MAIN" || q.Get("type") == "MAIN_FUNDING") && number(q.Get("current"), true) && len(q.Get("current")) <= 2 && q.Get("size") == "100"
		case "/sapi/v1/capital/withdraw/history":
			valid = queryKeys(q, []string{"coin", "limit", "offset"}, "idList", "withdrawOrderId", "startTime", "endTime") && coin(q.Get("coin")) && q.Get("limit") == "50" && offset(q.Get("offset"))
			if q.Get("idList") != "" {
				valid = valid && identifier(q.Get("idList"), 128) && !q.Has("withdrawOrderId") && !q.Has("startTime") && !q.Has("endTime")
			} else {
				start, startError := strconv.ParseInt(q.Get("startTime"), 10, 64)
				end, endError := strconv.ParseInt(q.Get("endTime"), 10, 64)
				valid = valid && !q.Has("idList") && identifier(q.Get("withdrawOrderId"), 32) && number(q.Get("startTime"), true) && number(q.Get("endTime"), true) && startError == nil && endError == nil && end > start && end-start < 7*24*60*60*1000
			}
		case "/sapi/v1/capital/deposit/hisrec":
			valid = queryKeys(q, []string{"coin", "txId", "limit", "offset"}) && coin(q.Get("coin")) && txid(q.Get("txId")) && q.Get("limit") == "50" && offset(q.Get("offset"))
		}
	} else if r.Method == "POST" {
		switch r.Path {
		case "/sapi/v1/asset/get-funding-asset":
			valid = queryKeys(q, []string{"asset", "needBtcValuation"}) && coin(q.Get("asset")) && q.Get("needBtcValuation") == "false"
		case "/api/v3/order":
			valid = queryKeys(q, []string{"symbol", "side", "type", "timeInForce", "quantity", "price", "newClientOrderId", "newOrderRespType"}) && pair(q.Get("symbol")) && (q.Get("side") == "BUY" || q.Get("side") == "SELL") && q.Get("type") == "LIMIT" && q.Get("timeInForce") == "IOC" && decimal(q.Get("quantity"), true) && decimal(q.Get("price"), true) && identifier(q.Get("newClientOrderId"), 36) && q.Get("newOrderRespType") == "ACK"
		case "/sapi/v1/asset/transfer":
			valid = queryKeys(q, []string{"type", "asset", "amount"}) && (q.Get("type") == "FUNDING_MAIN" || q.Get("type") == "MAIN_FUNDING") && coin(q.Get("asset")) && decimal(q.Get("amount"), true)
		case "/sapi/v1/capital/withdraw/apply":
			valid = queryKeys(q, []string{"coin", "network", "address", "amount", "withdrawOrderId", "walletType"}, "addressTag") && coin(q.Get("coin")) && identifier(q.Get("network"), 40) && text(q.Get("address"), 256, false) && text(q.Get("addressTag"), 120, true) && decimal(q.Get("amount"), true) && identifier(q.Get("withdrawOrderId"), 32) && q.Get("walletType") == "1"
		}
	}
	if !valid {
		return ErrInvalidRequest
	}
	return nil
}
func IsReadOnly(r bybit.AuthRequest) bool {
	return ValidateAuthRequest(r) == nil && (r.Method == "GET" || r.Path == "/sapi/v1/asset/get-funding-asset")
}
func wireQuery(r bybit.AuthRequest) string {
	q, _ := url.ParseQuery(r.Payload)
	q.Set("timestamp", strconv.FormatInt(r.TimestampMS, 10))
	q.Set("recvWindow", "5000")
	return q.Encode()
}
func HMAC(key string, secret []byte, r bybit.AuthRequest) (bybit.Auth, error) {
	if ValidateAuthRequest(r) != nil || !identifier(key, 128) || len(secret) < 16 || len(secret) > 512 {
		return bybit.Auth{}, ErrInvalidRequest
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(wireQuery(r)))
	return bybit.Auth{APIKey: key, Signature: hex.EncodeToString(m.Sum(nil))}, nil
}
