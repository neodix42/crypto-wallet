package walletsigner

import (
	"encoding/json"
	"strings"
	"testing"

	"localwallet/internal/journal"
	"localwallet/internal/model"
)

func TestBybitSessionPrivateDepositDispatchKeepsNormalSendAuthenticated(t *testing.T) {
	for _, bybit := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "bybit"}[bybit], func(t *testing.T) {
			e, u := dispatchEngine(t, "mainnet", true)
			u.inputs = append(u.inputs, publicInput(publicPassword))
			if err := e.Unlock(); err != nil {
				t.Fatal(err)
			}
			u.inputs = append(u.inputs, publicInput("PUBLIC_TEST_ONLY_APIKEY_92731", "PUBLIC_TEST_ONLY_SECRET_912735"))
			if _, err := handle(e, request("exchangeConnect", map[string]string{"endpoint": "https://api.bybit.com"}), "mainnet"); err != nil {
				t.Fatal(err)
			}
			var account model.Account
			for _, owned := range e.Snapshot().Accounts {
				if owned.Network == "ethereum-mainnet" {
					account = owned
				}
			}
			p := publicPlan(account)
			p.Payload = []byte(strings.Replace(string(p.Payload), `"chainId":"11155111"`, `"chainId":"1"`, 1))
			p.ID = ""
			p.ID = "evm:" + journal.Digest(p)
			prepare := "prepare"
			if bybit {
				prepare = "prepareBybitDeposit"
			}
			if _, err := handle(e, request(prepare, p), "mainnet"); err != nil {
				t.Fatal("typed preparation", err)
			}
			inputs, confirms := u.inputCalls, u.confirmCalls
			for _, raw := range []json.RawMessage{
				json.RawMessage(`{"id":"` + p.ID + `","skipPassword":true}`),
				json.RawMessage(`{"id":"` + p.ID + `","provider":"bybit"}`),
				json.RawMessage(`{"id":"` + p.ID + `","id":"other"}`),
				json.RawMessage(`{"id":null}`),
			} {
				m := request("signBybitDeposit", struct{}{})
				m.Body = raw
				if out, err := handle(e, m, "mainnet"); err == nil || len(out) != 0 {
					t.Fatal("caller-selected session bypass accepted")
				}
			}
			raw, err := handle(e, request("signBybitDeposit", map[string]string{"id": p.ID}), "mainnet")
			if bybit {
				var signed model.Signed
				if err != nil || json.Unmarshal(raw, &signed) != nil || len(signed.Bytes) == 0 || u.inputCalls != inputs || u.confirmCalls != confirms+1 {
					t.Fatal("private Bybit deposit missed exact confirmation or repeated the password", err)
				}
				if out, err := handle(e, request("signBybitDeposit", map[string]string{"id": p.ID}), "mainnet"); err == nil || len(out) != 0 {
					t.Fatal("private dispatch replayed a consumed capability")
				}
			} else {
				if err == nil || len(raw) != 0 || u.inputCalls != inputs || u.confirmCalls != confirms {
					t.Fatal("normally prepared Send gained private Bybit authority")
				}
				if _, err := handle(e, request("sign", map[string]string{"id": p.ID}), "mainnet"); err == nil || u.inputCalls != inputs+1 || u.confirmCalls != confirms+1 {
					t.Fatal("normal Send stopped requiring its password")
				}
			}
		})
	}
}
