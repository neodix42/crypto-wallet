package nativewallet

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"localwallet/internal/chains/ton"
	"localwallet/internal/model"
	"localwallet/internal/webui"
)

func TestRecoverTONProfileOpensExactLegacyIdentityAndDisplaysItsContract(t *testing.T) {
	for _, network := range []string{"ton-mainnet", "ton-testnet"} {
		for _, existing := range []bool{false, true} {
			t.Run(network+strconv.FormatBool(existing), func(t *testing.T) {
				a, ui, core := fixture(t)
				adapter := ton.New()
				seed := bytes.Repeat([]byte{0x71}, 64) // Disposable public fixture only.
				current, _ := adapter.DeriveProfile(seed, 0, network, ton.ProfileSubwallet42)
				legacy, _ := adapter.DeriveProfile(seed, 0, network, ton.Profile)
				a.environment = strings.TrimPrefix(network, "ton-")
				a.state.Environment, a.state.Revision = a.environment, "1"
				a.state.Accounts = []model.Account{current}
				if existing {
					a.state.Accounts = append(a.state.Accounts, legacy)
				}
				a.active, a.tabs = "wallet/"+current.ID, []string{current.ID}
				if existing {
					a.tabs = append(a.tabs, legacy.ID)
				}
				a.fields["tonProfile"], a.fields["tonIndex"] = ton.Profile, "0"
				state := a.state
				state.Accounts, state.TotalAccounts, state.Revision = []model.Account{current, legacy}, 2, "2"
				queried := make(chan string, 1)
				core.handler = func(g uint64, method string, body, out any) error {
					switch method {
					case "addTonAccount":
						if !reflect.DeepEqual(body, map[string]any{"index": uint32(0), "profile": ton.Profile}) {
							return errors.New("wrong recovery intent")
						}
						return answer(out, state)
					case "accounts":
						return answer(out, state)
					case "networks":
						return answer(out, a.networks)
					case "tonWalletState":
						id := body.(map[string]string)["accountId"]
						queried <- id
						var parameters ton.Parameters
						_ = json.Unmarshal(legacy.Parameters, &parameters)
						return answer(out, model.TONWalletState{AccountID: id, Network: network, Address: legacy.Address, Version: parameters.Version, WalletID: strconv.FormatUint(uint64(parameters.WalletID), 10), Subwallet: "0", Balance: "1", At: "1", Status: "active"})
					}
					return errors.New("unexpected recovery authority or request")
				}
				a.event(webui.Event{ID: "addTonAccount"})
				ui.apply(t)
				ui.apply(t)
				if <-queried != legacy.ID || a.active != "wallet/"+legacy.ID || a.busy || len(a.tabs) != 2 || !strings.Contains(a.status, "wallet ID 0") {
					t.Fatal("recovery left the original42 tab selected or duplicated a tab", a.active, a.status)
				}
				if len(a.state.Accounts) != 2 || !reflect.DeepEqual(a.state.Accounts[0], current) || a.state.Accounts[1].Address != legacy.Address {
					t.Fatal("opening recovered profile rewrote the existing funded identity")
				}
				text := viewText(a.walletPage(legacy))
				if !strings.Contains(text, "Contract status: active") || !strings.Contains(text, "wallet ID 0") || strings.Contains(text, "wallet ID 42") || strings.Contains(text, current.Address) {
					t.Fatal("recovered page retained the previous contract observation", text)
				}
			})
		}
	}
}

func TestRecoverTONProfileRejectsMismatchedPublicSnapshotsAndContextualizesErrors(t *testing.T) {
	for _, mode := range []string{"request-failed", "wrong-environment", "missing-profile", "wrong-index", "hidden", "forged-descriptor", "duplicate-match"} {
		t.Run(mode, func(t *testing.T) {
			a, ui, core := fixture(t)
			adapter := ton.New()
			seed := bytes.Repeat([]byte{0x72}, 64)
			current, _ := adapter.DeriveProfile(seed, 0, "ton-mainnet", ton.ProfileSubwallet42)
			legacy, _ := adapter.DeriveProfile(seed, 0, "ton-mainnet", ton.Profile)
			a.state.Accounts, a.state.Environment = []model.Account{current}, "mainnet"
			a.active, a.tabs = "wallet/"+current.ID, []string{current.ID}
			state := a.state
			state.Accounts, state.TotalAccounts, state.Revision = []model.Account{current, legacy}, 2, "2"
			switch mode {
			case "wrong-environment":
				state.Environment = "testnet"
			case "missing-profile":
				state.Accounts, state.TotalAccounts = []model.Account{current}, 1
			case "wrong-index":
				state.Accounts[1].Index = 1
			case "hidden":
				state.Accounts[1].Hidden = true
			case "forged-descriptor":
				state.Accounts[1].Address = current.Address
			case "duplicate-match":
				other := legacy
				other.ID += "-foreign"
				state.Accounts, state.TotalAccounts = append(state.Accounts, other), 3
			}
			core.handler = func(g uint64, method string, body, out any) error {
				if method == "addTonAccount" {
					if mode == "request-failed" {
						return errors.New("private provider diagnostic")
					}
					return answer(out, state)
				}
				if method == "accounts" {
					return answer(out, state)
				}
				return errors.New("invalid recovery reached an unrelated request")
			}
			a.recoverTONProfile(0, ton.Profile)
			ui.apply(t)
			if a.busy || a.active != "wallet/"+current.ID || len(a.state.Accounts) != 1 || !strings.Contains(a.status, "selected TON profile could not be opened") || strings.Contains(a.status, "Balance") || strings.Contains(a.status, "pending") || strings.Contains(a.status, "private") {
				t.Fatal("invalid recovery changed the selected wallet or produced unrelated pending guidance", a.status)
			}
		})
	}
}
