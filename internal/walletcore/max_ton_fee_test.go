package walletcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
)

func TestMaximumActualTONNativeValueSizeFeeKeepsFixedStorageAndProfiles(t *testing.T) {
	for _, profile := range []string{ton.Profile, ton.ProfileSubwallet42} {
		for _, rateLimited := range []bool{false, true} {
			name := profile
			if rateLimited {
				name += "-rate-limited"
			}
			t.Run(name, func(t *testing.T) {
				const balance = "200000000"
				var estimates, sendCount int
				var sizes []int
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q struct {
						Method string                     `json:"method"`
						Params map[string]json.RawMessage `json:"params"`
					}
					if json.NewDecoder(r.Body).Decode(&q) != nil {
						t.Error("invalid loopback RPC")
						return
					}
					var result any
					switch q.Method {
					case "getMasterchainInfo":
						result = map[string]any{"init": map[string]any{"root_hash": "gj+B8wb/AmlPk1z1AhVI484rhrUpgSr2oSFIh56VoSg=", "file_hash": "Z+IKwYS54DmmJmesw/nAD5DzWadnOCMzee+kdgSYDOg=", "workchain": -1}, "last": map[string]any{"seqno": 123}}
					case "getAddressInformation":
						result = map[string]any{"balance": balance, "state": "uninitialized", "sync_utime": time.Now().Unix()}
					case "estimateFee":
						estimates++
						if rateLimited && estimates == 2 {
							w.WriteHeader(http.StatusTooManyRequests)
							return
						}
						var body string
						_ = json.Unmarshal(q.Params["body"], &body)
						raw, err := base64.StdEncoding.DecodeString(body)
						if err != nil {
							t.Error("invalid actual transfer BOC")
							return
						}
						sizes = append(sizes, len(raw))
						// A Coins VarUInteger grows with the real payment amount. The provider
						// models that serialization cost; the actual adapter creates each BOC.
						result = map[string]any{"source_fees": map[string]int{"gas_fee": 1_000_000 + len(raw)*10_000, "storage_fee": 0, "in_fwd_fee": 0, "fwd_fee": 0}}
					case "sendBoc":
						sendCount++
						t.Error("Max broadcast a BOC")
						return
					default:
						t.Errorf("Max invoked unrelated TON RPC %s", q.Method)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1, "result": result})
				}))
				defer server.Close()
				adapter := ton.New()
				account, err := adapter.DeriveProfile(make([]byte, 64), 0, "ton-testnet", profile)
				if err != nil {
					t.Fatal(err)
				}
				recipient, err := adapter.DeriveProfile(make([]byte, 64), 1, "ton-testnet", profile)
				if err != nil {
					t.Fatal(err)
				}
				if err = adapter.Configure(account.Network, ton.Endpoint{URL: server.URL}); err != nil {
					t.Fatal(err)
				}
				p, err := maxPlan(context.Background(), adapter, account, model.Transfer{AccountID: account.ID, Asset: account.Network + ":TON", Recipient: recipient.Address})
				if rateLimited {
					if !errors.Is(err, ton.ErrRateLimited) || p.ID != "" || estimates != 2 || sendCount != 0 {
						t.Fatal("TON429 retried, returned authority or broadcast", p, err, estimates)
					}
					return
				}
				if err != nil {
					t.Fatal("valid TON Max failed after actual Coins serialization changed fee", err)
				}
				value, ok := new(big.Int).SetString(p.Amount, 10)
				if !ok {
					t.Fatal("invalid final native amount")
				}
				fee, ok := new(big.Int).SetString(p.MaxFee, 10)
				if !ok {
					t.Fatal("invalid final native fee")
				}
				total := new(big.Int).Add(value, fee)
				total.Add(total, big.NewInt(10_000_000))
				if total.String() != balance || adapter.ValidatePlan(p) != nil || estimates < 3 || len(sizes) < 3 || sizes[0] >= sizes[1] || sendCount != 0 {
					t.Fatal("Max reused small-amount fee, dropped storage or changed profile", p, estimates, sizes)
				}
			})
		}
	}
}
func TestMaximumDoesNotUseTONFeeQuotesForTokensOrChangedStoragePolicy(t *testing.T) {
	for _, row := range []struct{ asset, storage, fee string }{
		{"unit-testnet:USDT", "10000000", "5000000"},
		{"unit-testnet:TON", "0", "5000000"},
		{"unit-testnet:TON", "1", "5000000"},
		{"unit-testnet:TON", "10000000", "bad"},
	} {
		base, ac := maxFeeFixture("ton", row.asset, "unit-testnet:TON", "200000000", "200000000", "3000000", json.RawMessage(`{"storageReserve":"10000000"}`))
		a := &refusedFeeQuoteAdapter{maxAdapter: base, failure: &ton.InsufficientNativeFeeBalanceError{MaxFee: row.fee, StorageReserve: row.storage}}
		p, err := maxPlan(context.Background(), a, ac, model.Transfer{AccountID: ac.ID, Asset: row.asset, Recipient: "0:1111111111111111111111111111111111111111111111111111111111111111"})
		if err == nil || p.ID != "" || a.prepares != 2 || a.submits != 0 {
			t.Fatal("native quote changed token units/storage or retried malformed fee", row, p, err, a.prepares)
		}
	}
}
