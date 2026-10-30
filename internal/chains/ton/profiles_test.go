package ton

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/xssnick/tonutils-go/tvm/cell"
	"localwallet/internal/derivation"
	"localwallet/internal/model"
)

func TestImmutableSubwallet42IndependentSDKFixturesAndLegacyRecovery(t *testing.T) {
	_, seed := fixtures(t)
	raw, err := os.ReadFile("../../../tests/oracles/ton/subwallet42-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Profile  string
		Accounts []struct {
			Network, PublicKey, Address, StateHash, BodyHash, DeploymentExternalHash string
			Index                                                                    uint32
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || fixture.Profile != ProfileSubwallet42 || len(fixture.Accounts) != 6 {
		t.Fatal("independent subwallet42 fixture invalid", err)
	}
	adapter := New()
	for _, row := range fixture.Accounts {
		t.Run(row.Network+strconv.Itoa(int(row.Index)), func(t *testing.T) {
			account, err := adapter.DeriveProfile(seed, row.Index, row.Network, ProfileSubwallet42)
			if err != nil || account.Address != row.Address || account.PublicKey != row.PublicKey {
				t.Fatal("independent subwallet42 identity differs", err)
			}
			meta, _, err := descriptor(account)
			if err != nil || meta.Subwallet != 42 {
				t.Fatal("recorded subwallet42 descriptor rejected", err)
			}
			if (row.Network == "ton-mainnet" && meta.WalletID != 2147483451) || (row.Network == "ton-testnet" && meta.WalletID != 2147483607) {
				t.Fatal("subwallet number was confused with raw on-chain wallet ID")
			}
			pub, _ := hex.DecodeString(account.PublicKey)
			_, state, err := stateInitForWalletID(pub, meta.WalletID)
			if err != nil || hex.EncodeToString(state.Hash()) != row.StateHash {
				t.Fatal("independent StateInit differs", err)
			}
			plan := model.Plan{Account: account, Recipient: account.Address, Amount: "0", Expires: 2000000000}
			payload := Payload{Kind: "deployment", Deploy: true}
			unsigned, _, err := message(plan, payload)
			if err != nil {
				t.Fatal(err)
			}
			key, err := derivation.Ed25519(seed, []uint32{44, 607, row.Index})
			if err != nil {
				t.Fatal(err)
			}
			signature := ed25519.Sign(key, unsigned.Hash())
			clear(key)
			body := cell.BeginCell().MustStoreBuilder(unsigned.ToBuilder()).MustStoreSlice(signature, 512).EndCell()
			external, err := external(plan, payload, body)
			if err != nil || hex.EncodeToString(body.Hash()) != row.BodyHash || hex.EncodeToString(external.Hash()) != row.DeploymentExternalHash {
				t.Fatal("independent deployment bytes/signature differ", err)
			}
			legacy, err := adapter.DeriveProfile(seed, row.Index, row.Network, Profile)
			if err != nil || legacy.Address == account.Address || legacy.ID == account.ID || ValidateRecovery(seed, legacy) != nil || ValidateRecovery(seed, account) != nil {
				t.Fatal("legacy and subwallet42 identities were collapsed or recovery changed", err)
			}
		})
	}
	account, _ := adapter.DeriveProfile(seed, 0, "ton-mainnet", ProfileSubwallet42)
	otherSeed := append([]byte(nil), seed...)
	otherSeed[0] ^= 1
	if ValidateRecovery(otherSeed, account) == nil {
		t.Fatal("other root accepted registered TON contract")
	}
	account.Profile = "unregistered"
	if ValidateRecovery(seed, account) == nil {
		t.Fatal("unregistered TON recovery profile accepted")
	}
	if _, err := adapter.DeriveProfile(seed, 0, "ton-mainnet", "unregistered"); err == nil {
		t.Fatal("unregistered TON profile derivation accepted")
	}
}
