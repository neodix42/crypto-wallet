package bybit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCoinInfoIsolatesUnrelatedIncompleteNetworksWithoutAuthorizingThem(t *testing.T) {
	var fixture struct {
		Rows []Coin `json:"rows"`
	}
	if json.Unmarshal([]byte(methodFixtures()[1].result), &fixture) != nil {
		t.Fatal("invalid independent fixture")
	}
	good := fixture.Rows[0].Chains[0]
	for _, field := range []string{"withdrawal", "precision", "confirmations", "flags", "deposit"} {
		t.Run(field, func(t *testing.T) {
			bad := good
			bad.Chain = "UNRELATED"
			bad.ChainType = "Unrelated network"
			switch field {
			case "withdrawal":
				bad.WithdrawMax = ""
			case "precision":
				bad.MinAccuracy = ""
			case "confirmations":
				bad.SafeConfirmNumber = ""
			case "flags":
				bad.ChainDeposit = ""
			case "deposit":
				bad.DepositMin = ""
			}
			coin := fixture.Rows[0]
			coin.Chains = []Chain{good, bad}
			raw, _ := json.Marshal(map[string]any{"rows": []Coin{coin}})
			parsed, err := testClient(t, string(raw)).CoinInfo(context.Background(), "ETH")
			if err != nil || len(parsed.Chains) != 2 || parsed.InvalidChains[good.Chain] != "" || parsed.InvalidChains[bad.Chain] == "" {
				t.Fatal("bad unrelated network blocked valid route or became valid", err, parsed.InvalidChains)
			}
			// Removing the good network must retain a typed failure; no defaults.
			coin.Chains = []Chain{bad}
			raw, _ = json.Marshal(map[string]any{"rows": []Coin{coin}})
			_, err = testClient(t, string(raw)).CoinInfo(context.Background(), "ETH")
			var typed *CoinMetadataError
			if !errors.Is(err, ErrInvalidResponse) || !errors.As(err, &typed) || typed.Field == "" {
				t.Fatal("unsafe sole route accepted or precise reason lost", err)
			}
		})
	}
	coin := fixture.Rows[0]
	coin.Chains = []Chain{good, good}
	raw, _ := json.Marshal(map[string]any{"rows": []Coin{coin}})
	if _, err := testClient(t, string(raw)).CoinInfo(context.Background(), "ETH"); err == nil {
		t.Fatal("ambiguous duplicate network accepted")
	}
	if strings.Contains((&CoinMetadataError{Field: "PRIVATE_CANARY"}).Error(), "PRIVATE_CANARY") {
		t.Fatal("metadata error exposed response data")
	}
}

func TestDepositCoinInfoNeverInventsOrRequiresWithdrawalFeeAuthority(t *testing.T) {
	fixture := methodFixtures()[1].result
	for _, field := range []string{`"withdrawMax":"-1"`, `"withdrawPercentageFee":"0"`, `"withdrawMin":"0.0004"`} {
		bad := strings.Replace(fixture, field, strings.Split(field, ":")[0]+`:""`, 1)
		coin, err := testClient(t, bad).CoinInfoForDeposit(context.Background(), "ETH")
		if err != nil || len(coin.Chains) != 1 || coin.InvalidChains["ETH"] != "" {
			t.Fatal("withdrawal-only limit prevented independent deposit metadata", field, err)
		}
		if _, err = testClient(t, bad).CoinInfo(context.Background(), "ETH"); err == nil {
			t.Fatal("unknown withdrawal data enabled general withdrawal route")
		}
	}
	for _, field := range []string{`"depositMin":"0.0001"`, `"minAccuracy":"18"`, `"safeConfirmNumber":"64"`} {
		bad := strings.Replace(fixture, field, strings.Split(field, ":")[0]+`:""`, 1)
		if _, err := testClient(t, bad).CoinInfoForDeposit(context.Background(), "ETH"); err == nil {
			t.Fatal("required deposit metadata bypassed", field)
		}
	}
}
