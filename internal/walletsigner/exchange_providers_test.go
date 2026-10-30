package walletsigner

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"localwallet/internal/exchange/bybit"
	"localwallet/internal/signer"
)

func TestProviderPrivateDispatchKeepsCredentialInputsOffProtocolAndBindsSelection(t *testing.T) {
	e, u := dispatchEngine(t, "mainnet", true)
	u.inputs = append(u.inputs, publicInput(publicPassword))
	if err := e.Unlock(); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []struct {
		id, endpoint string
		private      []string
	}{
		{"binance", "https://api.binance.com", []string{"PUBLIC_BINANCE_API_KEY_87527", "PUBLIC_BINANCE_API_SECRET_7524892"}},
		{"kucoin", "https://api.kucoin.com", []string{"PUBLIC_KUCOIN_API_KEY_87527", "PUBLIC_KUCOIN_API_SECRET_7524892", "PUBLIC_KUCOIN_API_PASSPHRASE_72854", "3"}},
	} {
		u.inputs = append(u.inputs, publicInput(provider.private...))
		raw, err := handle(e, request("exchangeConnect", map[string]string{"provider": provider.id, "endpoint": provider.endpoint}), "mainnet")
		var status signer.ExchangeStatus
		if err != nil || json.Unmarshal(raw, &status) != nil || status.Provider != provider.id || status.Endpoint != provider.endpoint || !status.Connected {
			t.Fatal("provider connect dispatch", err)
		}
		for _, secret := range provider.private[:2] {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("protocol returned credential")
			}
		}
		if provider.id == "kucoin" && bytes.Contains(raw, []byte(provider.private[2])) {
			t.Fatal("protocol returned API passphrase")
		}
		authRequest := bybit.AuthRequest{Endpoint: provider.endpoint, Method: "GET", TimestampMS: time.Now().UnixMilli(), ReceiveWindowMS: 5000, Path: "/sapi/v1/account/apiRestrictions"}
		if provider.id == "kucoin" {
			authRequest.Path = "/api/v1/user/api-key"
		}
		raw, err = handle(e, request("exchangeAuth", authRequest), "mainnet")
		if err != nil || !bytes.Contains(raw, []byte("signature")) {
			t.Fatal("selected service read authorization", err)
		}
		if bytes.Contains(raw, []byte(provider.private[1])) || provider.id == "kucoin" && bytes.Contains(raw, []byte(provider.private[2])) {
			t.Fatal("service plaintext secret escaped auth reply")
		}
	}
	raw, err := handle(e, request("exchangeSelect", map[string]string{"provider": "binance"}), "mainnet")
	var status signer.ExchangeStatus
	if err != nil || json.Unmarshal(raw, &status) != nil || status.Provider != "binance" || status.Endpoint != "https://api.binance.com" {
		t.Fatal("explicit selection not persisted", err)
	}
	for _, body := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{"provider":null}`), json.RawMessage(`{"provider":7}`), json.RawMessage(`{"provider":"kucoin","provider":"binance"}`), json.RawMessage(`{"provider":"unknown"}`), json.RawMessage(`{"provider":"bybit","secret":"public-fixture"}`)} {
		if raw, err := handleExchange(e, "exchangeSelect", body); err == nil || raw != nil {
			t.Fatal("ambiguous provider selection accepted")
		}
	}
	e.Lock()
	if _, err = handle(e, request("exchangeSelect", map[string]string{"provider": "kucoin"}), "mainnet"); err == nil {
		t.Fatal("locked service selection accepted")
	}
}
