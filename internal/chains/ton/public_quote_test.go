package ton

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"testing"
	"time"

	"localwallet/internal/model"
)

// This optional read-only probe reconstructs only an active public V5 account's
// public descriptor. It never loads a vault/root, signs, records or submits a
// plan. The target address is supplied explicitly instead of committing user
// account data as a test fixture.
func TestTONPublicActiveNativeMaximumReadOnly(t *testing.T) {
	input := os.Getenv("WALLET_TON_PUBLIC_ADDRESS")
	if os.Getenv("WALLET_TON_PUBLIC_PROBE") != "1" || input == "" {
		t.Skip("NOT_RUN: explicit public-address read-only provider opt-in required")
	}
	a := New()
	network := "ton-mainnet"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := a.verifyNetwork(ctx, network); err != nil {
		t.Fatal("read-only network identity:", err)
	}
	if _, err := parseAddress(network, input); err != nil {
		t.Fatal("invalid public address")
	}
	st, err := a.state(ctx, network, input)
	if err != nil || st.State != "active" {
		t.Fatal("read-only source is not an available active contract:", err)
	}
	code, err := decodeBOC(st.Code)
	if err != nil || hex.EncodeToString(code.Hash()) != CodeHash {
		t.Fatal("public source is not pinned V5R1")
	}
	data, err := decodeBOC(st.Data)
	if err != nil {
		t.Fatal("read-only source data:", err)
	}
	s, err := data.BeginParse()
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := s.LoadBoolBit()
	if err != nil || !allowed {
		t.Fatal("public source signature authority disabled")
	}
	_, _ = s.LoadUInt(32) // authorityFromState below independently validates all fields.
	walletID, err := s.LoadUInt(32)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := s.LoadSlice(256)
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountForProfile(pub, 0, network, Profile)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := parseAddress(network, account.Address)
	actual, _ := parseAddress(network, input)
	if !actual.Equals(expected) {
		account, err = accountForProfile(pub, 0, network, ProfileSubwallet42)
		if err != nil {
			t.Fatal(err)
		}
		expected, _ = parseAddress(network, account.Address)
	}
	if !actual.Equals(expected) {
		t.Fatal("public source does not match either supported immutable wallet profile")
	}
	auth, err := a.authorityFromState(account, st)
	if err != nil || auth.Deploy {
		t.Fatal("public source exact authority:", err)
	}
	_, publicFixtureSeed := fixtures(t)
	recipient, err := a.DeriveProfile(publicFixtureSeed, 127, network, ProfileSubwallet42)
	if err != nil {
		t.Fatal(err)
	}
	balance, ok := new(big.Int).SetString(auth.Balance, 10)
	if !ok || balance.Sign() <= 0 {
		t.Fatal("public source has no observed native balance")
	}
	target, reserveCap := big.NewInt(1), new(big.Int)
	for attempt := 0; attempt < 4; attempt++ {
		request := model.Transfer{AccountID: account.ID, Asset: network + ":TON", Recipient: recipient.Address, Amount: target.String()}
		plan, err := a.Prepare(ctx, account, request)
		maxFee, storage := "", "10000000"
		var quote *InsufficientNativeFeeBalanceError
		if err == nil {
			if a.ValidatePlan(plan) != nil || plan.Account.Address != account.Address || plan.Account.Profile != account.Profile || plan.Amount != target.String() {
				t.Fatal("read-only plan changed the exact public wallet or requested amount")
			}
			maxFee = plan.MaxFee
		} else if errors.As(err, &quote) {
			maxFee, storage = quote.MaxFee, quote.StorageReserve
		} else {
			t.Fatalf("read-only native fee attempt %d: %v", attempt+1, err)
		}
		fee, valid := new(big.Int).SetString(maxFee, 10)
		if !valid || storage != "10000000" {
			t.Fatal("invalid local native budget quote")
		}
		required := new(big.Int).Add(fee, big.NewInt(10_000_000))
		if required.Cmp(reserveCap) > 0 {
			reserveCap.Set(required)
		}
		next := new(big.Int).Sub(balance, reserveCap)
		t.Logf("read-only native attempt=%d activeSeqno=%d profile=%s rawWalletID=%d sourceBalance=%s amount=%s estimatedNativeReserve=%s storage=%s accepted=%t", attempt+1, auth.Seqno, account.Profile, walletID, balance.String(), target.String(), maxFee, storage, err == nil)
		if next.Sign() <= 0 {
			t.Fatal("public source cannot afford the checked fee and storage reserve")
		}
		if next.Cmp(target) == 0 && err == nil {
			t.Log("PASS read-only native Max stabilized; no signer, journal or broadcast API called")
			return
		}
		target = next
	}
	t.Fatal("read-only native maximum could not stabilize within the existing four-attempt budget")
}
