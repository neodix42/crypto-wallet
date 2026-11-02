package walletcore

import (
	"encoding/json"
	"localwallet/internal/model"
	"testing"
)

func TestPublicPresentationRepliesAreScopedToTheirAccountPage(t *testing.T) {
	c := &core{}
	account := model.Account{ID: "owned", Index: 2}
	for _, presentation := range []model.AccountPresentation{
		{WalletLabels: map[string]string{"foreign": "name"}},
		{GroupLabels: map[string]string{"0": "wrong group"}},
		{DeletedGroups: map[string]bool{"02": true}},
	} {
		raw, _ := json.Marshal(snapshot{Accounts: []model.Account{account}, Presentation: presentation})
		if _, err := c.publicSnapshotResultAt(0, "public-test", raw); err == nil {
			t.Fatal("foreign presentation crossed coordinator")
		}
	}
	good := snapshot{Accounts: []model.Account{account}, Presentation: model.AccountPresentation{WalletLabels: map[string]string{"owned": "Savings"}, GroupLabels: map[string]string{"2": "Other"}, DeletedGroups: map[string]bool{"2": true}}}
	raw, _ := json.Marshal(good)
	out, err := c.publicSnapshotResultAt(0, "public-test", raw)
	if err != nil || out.Presentation.WalletLabels["owned"] != "Savings" {
		t.Fatal("valid owned metadata rejected", err)
	}
}
