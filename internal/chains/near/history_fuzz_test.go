package near

import (
	"encoding/json"
	"reflect"
	"testing"
	"unicode/utf8"

	"localwallet/internal/chains/chainutil"
	"localwallet/internal/model"
)

func FuzzNEARHistoricalNativeActions(f *testing.F) {
	f.Add([]byte(`[{"Transfer":{"deposit":"1000000000000000000000000"}}]`), true)
	f.Add([]byte(`[{"Transfer":{"deposit":"0"}},{"Transfer":{"deposit":"2"}}]`), false)
	f.Add([]byte(`[{"FunctionCall":{"method_name":"deposit_and_stake","args":"e30=","gas":100000000000000,"deposit":"10"}}]`), true)
	f.Add([]byte(`[{"Transfer":{"deposit":"-1"}}]`), false)
	f.Add([]byte(`[{"Transfer":{"deposit":"340282366920938463463374607431768211456"}}]`), true)
	f.Add([]byte(`[{"Transfer":{"deposit":"1","deposit":"2"}}]`), true)
	f.Add([]byte(`[{"CreateAccount":{}}]`), false)
	a := New()
	account := model.Account{Network: "near-testnet", Address: "owned.testnet"}
	f.Fuzz(func(t *testing.T, payload []byte, outgoing bool) {
		if len(payload) > 32768 {
			return
		}
		var actions []json.RawMessage
		if chainutil.DecodeStrict(payload, &actions) != nil || len(actions) > 100 {
			return
		}
		source, destination := account.Address, "peer.testnet"
		if !outgoing {
			source, destination = destination, source
		}
		rows, err := a.historicalNativeActions(account, source, destination, actions, "native:fixture", true)
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, effect := range rows {
			value, err := chainutil.Big(effect.Atomic, 128)
			if err != nil || value.Sign() == 0 || effect.Source != source || effect.Destination != destination || effect.Asset != (model.Asset{ID: "near-testnet:native", Symbol: "NEAR", Decimals: 24}) || seen[effect.ID] || effect.ID == "" || !utf8.ValidString(effect.Comment) || len(effect.Comment) > 512 {
				t.Fatal("unbound/noncanonical native historical effect", effect, err)
			}
			seen[effect.ID] = true
		}
		again, err := a.historicalNativeActions(account, source, destination, actions, "native:fixture", true)
		if err != nil || !reflect.DeepEqual(rows, again) {
			t.Fatal("identical public receipt actions changed historical meaning")
		}
		foreign, err := a.historicalNativeActions(account, "foreign.testnet", "other.testnet", actions, "native:fixture", true)
		if err != nil || len(foreign) != 0 {
			t.Fatal("unowned native actions became owned historical transfers", foreign, err)
		}
	})
}
