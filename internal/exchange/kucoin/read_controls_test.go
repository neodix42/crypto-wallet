package kucoin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"localwallet/internal/exchange/bybit"
)

func TestKuCoinInvalidAssetAndAccountIntentsNeverRequestAuthentication(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		call func(*Client) error
	}{
		{"coinURL", func(c *Client) error { _, err := c.CoinInfo(ctx, "https://foreign.invalid"); return err }},
		{"marginBalance", func(c *Client) error { _, err := c.AccountBalance(ctx, "MARGIN", "ETH"); return err }},
		{"instrumentPair", func(c *Client) error { _, err := c.SpotInstrument(ctx, "ETHBTC"); return err }},
		{"tickerPair", func(c *Client) error { _, err := c.SpotTicker(ctx, "ETHBTC"); return err }},
		{"feePair", func(c *Client) error { _, err := c.SpotFeeRate(ctx, "ETHBTC"); return err }},
		{"transferScope", func(c *Client) error {
			_, err := c.Transfer(ctx, bybit.TransferRequest{TransferID: "11111111-1111-4111-8111-111111111111", Coin: "ETH", Amount: "1", FromAccountType: "MARGIN", ToAccountType: "FUND"})
			return err
		}},
		{"transferLookup", func(c *Client) error { _, err := c.TransferStatusFor(ctx, bybit.TransferRequest{}, "ack"); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			c := testClient(t, func(r *http.Request) string { requests++; return wireFixture(r) })
			if err := test.call(c); err != ErrRequest || requests != 0 {
				t.Fatal("invalid intent reached authentication/network", err, requests)
			}
		})
	}
}

func TestKuCoinFundingAndSpotBalancesAreSeparateExactObservations(t *testing.T) {
	ctx := context.Background()
	reads := 0
	c := testClient(t, func(r *http.Request) string {
		if r.URL.Path == "/api/v1/accounts" {
			reads++
			if r.URL.Query().Get("type") == "trade" {
				return `[{"id":"spot-account","currency":"ETH","type":"trade","balance":"2.5","available":"2","holds":"0.5"}]`
			}
		}
		return wireFixture(r)
	})
	spot, err := c.AccountBalance(ctx, "UNIFIED", "ETH")
	if err != nil {
		t.Fatal(err)
	}
	funding, err := c.AccountBalance(ctx, "FUND", "ETH")
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 || spot.AccountType != "UNIFIED" || spot.AccountID != "spot-account" || spot.Balance.TransferSafeAmount != "2" || funding.AccountType != "FUND" || funding.Balance.TransferSafeAmount != "1.1" {
		t.Fatal("Funding and Spot balances conflated", spot, funding)
	}
}

func TestKuCoinMissingDepositThresholdCannotEnableFunding(t *testing.T) {
	c := testClient(t, func(r *http.Request) string {
		return strings.Replace(wireFixture(r), `"depositMinSize":"0.0001"`, `"depositMinSize":null`, 1)
	})
	value, err := c.CoinInfo(context.Background(), "ETH")
	if err != nil || len(value.Chains) != 1 || value.Chains[0].ChainDeposit != "0" || value.Chains[0].ChainWithdraw != "1" {
		t.Fatal("unknown deposit minimum became supported", value, err)
	}
}

func TestKuCoinClassicReadFailureCannotReuseAnotherAccountOrInventEmptySuccess(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name, path string
		call       func(*Client) error
	}{
		{"address", "/api/v3/deposit-addresses", func(c *Client) error { _, err := c.DepositAddress(ctx, "ETH", "eth"); return err }},
		{"balance", "/api/v1/accounts", func(c *Client) error { _, err := c.AccountBalance(ctx, "FUND", "ETH"); return err }},
	} {
		for _, outage := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "empty", true: "outage"}[outage], func(t *testing.T) {
				c := testClient(t, wireFixture)
				reads := 0
				c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == test.path {
						reads++
						if outage {
							return nil, errors.New("private-provider-canary")
						}
						return response(`[]`), nil
					}
					return response(wireFixture(r)), nil
				})
				err := test.call(c)
				expected := ErrResponse
				if outage {
					expected = ErrUnavailable
				}
				if err != expected || reads != 1 {
					t.Fatal("unknown account became an empty success or retried", err, reads)
				}
			})
		}
	}
	for _, test := range []func(*Client) error{
		func(c *Client) error { _, err := c.SpotFeeRate(ctx, "ETHUSDT"); return err },
		func(c *Client) error {
			_, err := c.Transfer(ctx, bybit.TransferRequest{TransferID: "11111111-1111-4111-8111-111111111111", Coin: "ETH", Amount: "1", FromAccountType: "FUND", ToAccountType: "UNIFIED"})
			return err
		},
	} {
		c := testClient(t, func(*http.Request) string { return `{"selfAccountMode":"UNIFIED"}` })
		if err := test(c); err != ErrAccountMode {
			t.Fatal("unsupported account reached a write or fee assumption", err)
		}
	}
}
