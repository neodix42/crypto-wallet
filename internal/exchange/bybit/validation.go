package bybit

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
)

var (
	ErrInvalidRequest  = errors.New("invalid Bybit request")
	ErrInvalidResponse = errors.New("invalid or incomplete Bybit response")
	ErrUnavailable     = errors.New("Bybit connection unavailable; operation may require reconciliation")
	ErrAuthorization   = errors.New("Bybit authorization unavailable")
	ErrNotListed       = errors.New("Bybit has no active spot listing for this selected pair")
)

var productionEndpoints = []string{
	"https://api.bybit.com", "https://api.bytick.com", "https://api.bybit.tr",
	"https://api.bybit.kz", "https://api.bybitgeorgia.ge", "https://api.bybit.ae",
	"https://api.bybit.eu", "https://api.bybit.id", "https://api.manepa.jp", "https://api.spark-fintech.com",
}

// Endpoints returns only production endpoints listed in Bybit's official guide.
func Endpoints() []string { return append([]string(nil), productionEndpoints...) }
func ValidateEndpoint(endpoint string) error {
	for _, allowed := range productionEndpoints {
		if endpoint == allowed {
			return nil
		}
	}
	return ErrInvalidRequest
}
func boundedText(s string, max int, empty bool) bool {
	if (!empty && s == "") || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func identifier(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func transactionID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_/+=:", c)) {
			return false
		}
	}
	return true
}
func coinName(s string) bool {
	if s == "" || len(s) > 24 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func symbolName(s string) bool {
	_, _, ok := SpotPair(s)
	return ok
}

// SpotQuote restricts this wallet's exchange quote currencies. It does not
// authorize a wallet token, deposit/withdrawal route, or arbitrary trading pair.
func SpotQuote(s string) bool { return s == "USDT" || s == "USDC" }

// SpotPair splits only the two explicitly supported spot quote currencies.
// Callers additionally bind the base coin to the selected pinned wallet asset.
func SpotPair(s string) (base, quote string, ok bool) {
	if !coinName(s) {
		return "", "", false
	}
	for _, quote := range []string{"USDT", "USDC"} {
		if strings.HasSuffix(s, quote) {
			base := strings.TrimSuffix(s, quote)
			if coinName(base) && base != quote {
				return base, quote, true
			}
		}
	}
	return "", "", false
}
func decimal(s string, positive bool) bool {
	n, e := amount.Parse(s, 36)
	return e == nil && (!positive || n != "0")
}
func decimalRange(s string, min, max int64, precision uint8) bool {
	n, e := amount.Parse(s, precision)
	if e != nil {
		return false
	}
	v, _ := new(big.Int).SetString(n, 10)
	return v.Cmp(big.NewInt(min)) >= 0 && v.Cmp(big.NewInt(max)) <= 0
}
func accountType(s string) bool { return s == "FUND" || s == "UNIFIED" }
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
func withdrawalID(s string) bool {
	if !identifier(s, 32) {
		return false
	}
	return !strings.ContainsAny(s, "-_")
}

func ValidateSpotOrder(o SpotOrder) error {
	unit := "baseCoin"
	if o.Side == "Buy" {
		unit = "quoteCoin"
	} else if o.Side != "Sell" {
		return ErrInvalidRequest
	}
	if o.Category != "spot" || !symbolName(o.Symbol) || o.OrderType != "Market" || !decimal(o.Qty, true) || o.MarketUnit != unit || o.IsLeverage != 0 || o.SlippageToleranceType != "Percent" || !decimalRange(o.SlippageTolerance, 1, 1000, 2) || !identifier(o.OrderLinkID, 36) {
		return ErrInvalidRequest
	}
	return nil
}
func ValidateTransfer(t TransferRequest) error {
	if !uuid(t.TransferID) || !coinName(t.Coin) || !decimal(t.Amount, true) || !accountType(t.FromAccountType) || !accountType(t.ToAccountType) || t.FromAccountType == t.ToAccountType {
		return ErrInvalidRequest
	}
	return nil
}
func ValidateWithdrawal(w WithdrawalRequest) error {
	if !coinName(w.Coin) || !identifier(w.Chain, 40) || !boundedText(w.Address, 256, false) || !boundedText(w.Tag, 120, true) || !decimal(w.Amount, true) || w.Timestamp <= 0 || w.ForceChain != 1 || w.AccountType != "FUND" || w.FeeType != 1 || !withdrawalID(w.RequestID) || w.Beneficiary != nil && ValidateUAEBeneficiary(*w.Beneficiary) != nil {
		return ErrInvalidRequest
	}
	return nil
}

// ValidateAuthRequest accepts only the fixed API surface and canonical wire
// encoding used by this package. A signer must additionally match POST payloads
// to an immutable, durably approved operation; this function grants no approval.
func ValidateAuthRequest(r AuthRequest) error {
	if ValidateEndpoint(r.Endpoint) != nil || r.TimestampMS <= 0 || r.ReceiveWindowMS != 5000 || len(r.Payload) > 8192 {
		return ErrInvalidRequest
	}
	if r.Method == "POST" {
		var value any
		switch r.Path {
		case "/v5/order/create":
			var o SpotOrder
			if chainutil.DecodeStrict([]byte(r.Payload), &o) != nil || ValidateSpotOrder(o) != nil {
				return ErrInvalidRequest
			}
			value = o
		case "/v5/asset/transfer/inter-transfer":
			var t TransferRequest
			if chainutil.DecodeStrict([]byte(r.Payload), &t) != nil || ValidateTransfer(t) != nil {
				return ErrInvalidRequest
			}
			value = t
		case "/v5/asset/withdraw/create":
			var w WithdrawalRequest
			if chainutil.DecodeStrict([]byte(r.Payload), &w) != nil || ValidateWithdrawal(w) != nil {
				return ErrInvalidRequest
			}
			if (r.Endpoint == "https://api.bybit.ae") != (w.Beneficiary != nil) {
				return ErrInvalidRequest
			}
			value = w
		default:
			return ErrInvalidRequest
		}
		b, e := json.Marshal(value)
		if e != nil || string(b) != r.Payload {
			return ErrInvalidRequest
		}
		return nil
	}
	if r.Method != "GET" {
		return ErrInvalidRequest
	}
	q, e := url.ParseQuery(r.Payload)
	if e != nil || q.Encode() != r.Payload {
		return ErrInvalidRequest
	}
	for _, values := range q {
		if len(values) != 1 || !boundedText(values[0], 1024, false) {
			return ErrInvalidRequest
		}
	}
	keys := func(required []string, optional ...string) bool {
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
		for k := range q {
			if !allowed[k] {
				return false
			}
		}
		return true
	}
	valid := false
	switch r.Path {
	case "/v5/user/query-api":
		valid = keys(nil)
	case "/v5/asset/coin/query-info":
		valid = keys([]string{"coin"}) && coinName(q.Get("coin"))
	case "/v5/asset/deposit/query-address":
		valid = keys([]string{"coin", "chainType"}) && coinName(q.Get("coin")) && identifier(q.Get("chainType"), 40)
	case "/v5/asset/transfer/query-account-coin-balance":
		valid = keys([]string{"accountType", "coin", "withTransferSafeAmount", "toAccountType"}) && accountType(q.Get("accountType")) && accountType(q.Get("toAccountType")) && q.Get("accountType") != q.Get("toAccountType") && coinName(q.Get("coin")) && q.Get("withTransferSafeAmount") == "1"
	case "/v5/market/instruments-info", "/v5/market/tickers", "/v5/account/fee-rate":
		valid = keys([]string{"category", "symbol"}) && q.Get("category") == "spot" && symbolName(q.Get("symbol"))
	case "/v5/order/realtime", "/v5/order/history":
		valid = keys([]string{"category", "symbol", "orderLinkId"}) && q.Get("category") == "spot" && symbolName(q.Get("symbol")) && identifier(q.Get("orderLinkId"), 36)
	case "/v5/execution/list":
		valid = keys([]string{"category", "symbol", "orderLinkId", "limit"}, "cursor") && q.Get("category") == "spot" && symbolName(q.Get("symbol")) && identifier(q.Get("orderLinkId"), 36) && q.Get("limit") == "100"
	case "/v5/asset/transfer/query-inter-transfer-list":
		valid = keys([]string{"transferId", "limit"}) && uuid(q.Get("transferId")) && q.Get("limit") == "50"
	case "/v5/asset/withdraw/query-record":
		valid = keys([]string{"coin", "withdrawID", "withdrawType", "limit"}, "cursor") && coinName(q.Get("coin")) && identifier(q.Get("withdrawID"), 128) && q.Get("withdrawType") == "0" && q.Get("limit") == "50"
	case "/v5/asset/deposit/query-record":
		valid = keys([]string{"coin", "txID", "limit"}, "cursor") && coinName(q.Get("coin")) && transactionID(q.Get("txID")) && q.Get("limit") == "50"
	}
	if !valid {
		return ErrInvalidRequest
	}
	return nil
}

// HMAC is for the secret-owning signer. Neither Client nor its Authorizer
// requires receiving the API secret, and signatures are never cached.
func HMAC(apiKey string, secret []byte, request AuthRequest) (Auth, error) {
	if ValidateAuthRequest(request) != nil || !identifier(apiKey, 128) || len(secret) < 16 || len(secret) > 512 {
		return Auth{}, ErrInvalidRequest
	}
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte(strconv.FormatInt(request.TimestampMS, 10)))
	_, _ = m.Write([]byte(apiKey))
	_, _ = m.Write([]byte(strconv.FormatInt(request.ReceiveWindowMS, 10)))
	_, _ = m.Write([]byte(request.Payload))
	return Auth{APIKey: apiKey, Signature: hex.EncodeToString(m.Sum(nil))}, nil
}
