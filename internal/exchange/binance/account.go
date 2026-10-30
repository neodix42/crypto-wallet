package binance

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"localwallet/internal/exchange/bybit"
)

func (c *Client) APIKeyInfo(ctx context.Context) (bybit.APIKeyInformation, error) {
	var raw struct {
		Reading  *bool `json:"enableReading"`
		Spot     *bool `json:"enableSpotAndMarginTrading"`
		Withdraw *bool `json:"enableWithdrawals"`
		Transfer *bool `json:"permitsUniversalTransfer"`
	}
	if e := c.get(ctx, "/sapi/v1/account/apiRestrictions", nil, true, &raw); e != nil {
		return bybit.APIKeyInformation{}, e
	}
	if raw.Reading == nil || raw.Spot == nil || raw.Withdraw == nil || raw.Transfer == nil || !*raw.Reading {
		return bybit.APIKeyInformation{}, ErrInvalidResponse
	}
	i := bybit.APIKeyInformation{ReadOnly: 1}
	if *raw.Spot {
		i.ReadOnly = 0
		i.Permissions.Spot = []string{"SpotTrade"}
	}
	if *raw.Withdraw {
		i.ReadOnly = 0
		i.Permissions.Wallet = append(i.Permissions.Wallet, "Withdraw")
	}
	if *raw.Transfer {
		i.ReadOnly = 0
		i.Permissions.Wallet = append(i.Permissions.Wallet, "AccountTransfer")
	}
	// Binance's permission endpoint does not establish UID, master status or
	// Bybit UTA membership. Those unrelated fields deliberately remain absent.
	return i, nil
}

type networkInfo struct {
	Network       string  `json:"network"`
	Coin          string  `json:"coin"`
	Name          string  `json:"name"`
	Deposit       *bool   `json:"depositEnable"`
	Withdraw      *bool   `json:"withdrawEnable"`
	MinConfirm    *uint64 `json:"minConfirm"`
	UnlockConfirm *uint64 `json:"unLockConfirm"`
	WithdrawFee   string  `json:"withdrawFee"`
	WithdrawMin   string  `json:"withdrawMin"`
	WithdrawMax   string  `json:"withdrawMax"`
	Step          string  `json:"withdrawIntegerMultiple"`
	DepositDust   string  `json:"depositDust"`
	Contract      string  `json:"contractAddress"`
	Denomination  *uint64 `json:"denomination"`
}

func stepPrecision(step string) (string, error) {
	if !decimal(step, true) {
		return "", ErrInvalidResponse
	}
	trim := strings.TrimRight(step, "0")
	if !strings.Contains(step, ".") {
		trim = step
	}
	// Precision-only shared DTOs cannot express non-power-of-ten multiples.
	// Reject such routes instead of pretending every fractional value is valid.
	if trim == "1" {
		return "0", nil
	}
	if !strings.HasPrefix(trim, "0.") || !strings.HasSuffix(trim, "1") || strings.Trim(trim[2:len(trim)-1], "0") != "" {
		return "", ErrInvalidResponse
	}
	return strconv.Itoa(len(trim) - 2), nil
}
func (c *Client) CoinInfo(ctx context.Context, selected string) (bybit.Coin, error) {
	if !coin(selected) {
		return bybit.Coin{}, ErrInvalidRequest
	}
	var raw []struct {
		Coin     string        `json:"coin"`
		Name     string        `json:"name"`
		Networks []networkInfo `json:"networkList"`
	}
	if e := c.get(ctx, "/sapi/v1/capital/config/getall", nil, true, &raw); e != nil {
		return bybit.Coin{}, e
	}
	if raw == nil || len(raw) > 4096 {
		return bybit.Coin{}, ErrInvalidResponse
	}
	var result *bybit.Coin
	for _, r := range raw {
		if r.Coin != selected {
			continue
		}
		if result != nil || !text(r.Name, 120, false) || len(r.Networks) == 0 || len(r.Networks) > 128 {
			return bybit.Coin{}, ErrInvalidResponse
		}
		v := bybit.Coin{Coin: r.Coin, Name: r.Name, Chains: []bybit.Chain{}}
		seen := map[string]bool{}
		for _, n := range r.Networks {
			precision, e := stepPrecision(n.Step)
			if e != nil || !identifier(n.Network, 40) || seen[n.Network] || n.Coin != selected || !text(n.Name, 120, false) || n.Deposit == nil || n.Withdraw == nil || n.MinConfirm == nil || n.UnlockConfirm == nil || !decimal(n.WithdrawFee, false) || !decimal(n.WithdrawMin, false) || !decimal(n.WithdrawMax, false) || !decimal(n.DepositDust, false) || !text(n.Contract, 256, true) || n.Denomination != nil && *n.Denomination != 1 {
				return bybit.Coin{}, ErrInvalidResponse
			}
			dep, wd := "0", "0"
			if *n.Deposit {
				dep = "1"
			}
			if *n.Withdraw {
				wd = "1"
			}
			v.Chains = append(v.Chains, bybit.Chain{Chain: n.Network, ChainType: n.Name, Confirmation: strconv.FormatUint(*n.MinConfirm, 10), SafeConfirmNumber: strconv.FormatUint(*n.UnlockConfirm, 10), WithdrawFee: n.WithdrawFee, DepositMin: n.DepositDust, WithdrawMin: n.WithdrawMin, WithdrawMax: n.WithdrawMax, MinAccuracy: precision, ChainDeposit: dep, ChainWithdraw: wd, WithdrawPercentageFee: "0", ContractAddress: n.Contract})
			seen[n.Network] = true
		}
		result = &v
	}
	if result == nil {
		return bybit.Coin{}, ErrInvalidResponse
	}
	return *result, nil
}
func (c *Client) DepositAddress(ctx context.Context, selected, network string) (bybit.Deposit, error) {
	if !coin(selected) || !identifier(network, 40) {
		return bybit.Deposit{}, ErrInvalidRequest
	}
	metadata, e := c.CoinInfo(ctx, selected)
	if e != nil {
		return bybit.Deposit{}, e
	}
	var route *bybit.Chain
	for i := range metadata.Chains {
		if metadata.Chains[i].Chain == network {
			route = &metadata.Chains[i]
		}
	}
	if route == nil || route.ChainDeposit != "1" {
		return bybit.Deposit{}, ErrInvalidResponse
	}
	var raw json.RawMessage
	if e = c.get(ctx, "/sapi/v1/capital/deposit/address", url.Values{"coin": {selected}, "network": {network}}, true, &raw); e != nil {
		return bybit.Deposit{}, e
	}
	var address struct {
		Coin    string `json:"coin"`
		Address string `json:"address"`
		Tag     string `json:"tag"`
	}
	if !required(raw, "coin", "address", "tag") || json.Unmarshal(raw, &address) != nil || address.Coin != selected || !text(address.Address, 256, false) || !text(address.Tag, 120, true) {
		return bybit.Deposit{}, ErrInvalidResponse
	}
	return bybit.Deposit{Chain: network, ChainType: route.ChainType, Address: address.Address, Tag: address.Tag, ContractAddress: route.ContractAddress, BatchReleaseLimit: "-1"}, nil
}

// WithdrawalAllowed is checked before a transfer into the withdrawal wallet,
// and again immediately before withdrawal. It never fabricates questionnaire
// answers or falls back to another region/API when Binance requires them.
func (c *Client) WithdrawalAllowed(ctx context.Context) error {
	var raw struct {
		Country *string `json:"questionnaireCountryCode"`
	}
	if e := c.get(ctx, "/sapi/v1/localentity/questionnaire-requirements", nil, true, &raw); e != nil {
		return e
	}
	if raw.Country == nil || !text(*raw.Country, 8, false) {
		return ErrInvalidResponse
	}
	if *raw.Country != "NIL" {
		return ErrTravelRule
	}
	return nil
}
func (c *Client) AccountBalance(ctx context.Context, kind, selected string) (bybit.Balance, error) {
	if !coin(selected) || kind != "FUND" && kind != "UNIFIED" {
		return bybit.Balance{}, ErrInvalidRequest
	}
	var rows []json.RawMessage
	if kind == "FUND" {
		if e := c.do(ctx, "POST", "/sapi/v1/asset/get-funding-asset", url.Values{"asset": {selected}, "needBtcValuation": {"false"}}, true, &rows); e != nil {
			return bybit.Balance{}, e
		}
	} else {
		var raw struct {
			Balances    []json.RawMessage `json:"balances"`
			AccountType string            `json:"accountType"`
		}
		if e := c.get(ctx, "/api/v3/account", nil, true, &raw); e != nil {
			return bybit.Balance{}, e
		}
		if raw.AccountType != "SPOT" {
			return bybit.Balance{}, ErrInvalidResponse
		}
		rows = raw.Balances
	}
	if rows == nil || len(rows) > 4096 {
		return bybit.Balance{}, ErrInvalidResponse
	}
	result := bybit.Balance{AccountType: kind}
	result.Balance.Coin = selected
	found := false
	for _, r := range rows {
		var v struct {
			Asset       string `json:"asset"`
			Free        string `json:"free"`
			Locked      string `json:"locked"`
			Freeze      string `json:"freeze"`
			Withdrawing string `json:"withdrawing"`
		}
		if json.Unmarshal(r, &v) != nil || !coin(v.Asset) || !decimal(v.Free, false) || !decimal(v.Locked, false) || kind == "FUND" && (!decimal(v.Freeze, false) || !decimal(v.Withdrawing, false)) {
			return bybit.Balance{}, ErrInvalidResponse
		}
		if v.Asset != selected {
			continue
		}
		if found {
			return bybit.Balance{}, ErrInvalidResponse
		}
		found = true
		// Only Binance's explicitly free amount is usable. Locked/frozen funds
		// never become transfer-safe balances through shared DTO projection.
		result.Balance.WalletBalance = v.Free
		result.Balance.TransferBalance = v.Free
		result.Balance.TransferSafeAmount = v.Free
	}
	if !found {
		// A complete explicit empty result is a verified zero; a missing/null
		// balances array was rejected above and remains unavailable.
		result.Balance.WalletBalance = "0"
		result.Balance.TransferBalance = "0"
		result.Balance.TransferSafeAmount = "0"
	}
	return result, nil
}
