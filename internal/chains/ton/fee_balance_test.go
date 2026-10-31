package ton

import (
	"context"
	"errors"
	"testing"

	"localwallet/internal/model"
)

func TestNativeTransferInsufficientBudgetQuoteKeepsStorageAndNeverReturnsPartialPlan(t *testing.T) {
	for _, profile := range []string{Profile, ProfileSubwallet42} {
		t.Run(profile, func(t *testing.T) {
			a := New()
			account, err := a.DeriveProfile(make([]byte, 64), 0, "ton-testnet", profile)
			if err != nil {
				t.Fatal(err)
			}
			rpc := &deploymentRPC{state: "uninitialized", balance: "200000000", fee: 1_000_000}
			rpc.configure(t, a, account.Network)
			request := model.Transfer{AccountID: account.ID, Asset: account.Network + ":TON", Recipient: account.Address, Amount: "199000000"}
			p, err := a.Prepare(context.Background(), account, request)
			var quote *InsufficientNativeFeeBalanceError
			if !errors.As(err, &quote) || p.ID != "" || quote.MaxFee != "3000000" || quote.StorageReserve != "10000000" || quote.Error() != "insufficient GRAM for payment, fees and storage reserve" || rpc.sendCount != 0 {
				t.Fatal("native insufficient budget lost exact quote/storage or returned authority", p, err)
			}
			request.Amount = "180000000"
			p, err = a.Prepare(context.Background(), account, request)
			if err != nil || p.Amount != request.Amount || a.ValidatePlan(p) != nil || rpc.sendCount != 0 {
				t.Fatal("affordable native plan changed or sent", p, err)
			}
		})
	}
}
