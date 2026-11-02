package walletsigner

import (
	"bytes"
	"encoding/json"
	"testing"

	"localwallet/internal/exchange"
	"localwallet/internal/ipc"
)

func TestExchangeConnectCancellationFramesPreserveCiphertextAndNeverReturnCredentials(t *testing.T) {
	for _, provider := range exchange.Providers() {
		t.Run(provider.ID, func(t *testing.T) {
			e, ui := dispatchEngine(t, "mainnet", true)
			ui.inputs = append(ui.inputs, publicInput(publicPassword))
			if _, err := handle(e, request("unlock", struct{}{}), "mainnet"); err != nil {
				t.Fatal(err)
			}
			before, err := e.Files.Read("vault.lwv")
			if err != nil {
				t.Fatal(err)
			}
			values := []string{"PUBLIC_TEST_APIKEY_CANCEL_92731", "PUBLIC_TEST_SECRET_CANCEL_912735"}
			if provider.RequiresPassphrase {
				values = append(values, "PUBLIC_TEST_PASSPHRASE_CANCEL_1917", "2")
			}
			ui.inputs = append(ui.inputs, publicInput(values...))
			ui.approve = false
			request := request("exchangeConnect", map[string]string{"provider": provider.ID, "endpoint": provider.Endpoints[0]})
			var wire bytes.Buffer
			err = respond(&ipc.Channel{W: &wire}, request, func() (json.RawMessage, error) {
				return handle(e, request, "mainnet")
			})
			if err != nil {
				t.Fatal(err)
			}
			response, err := (&ipc.Channel{R: &wire}).Read()
			if err != nil || response.Error != "REQUEST_CANCELLED" || string(response.Body) != "null" {
				t.Fatal("exchange cancellation lost its exact safe transport category", err)
			}
			for _, value := range values {
				if bytes.Contains(wire.Bytes(), []byte(value)) {
					t.Fatal("exchange cancellation exposed entered credentials")
				}
			}
			after, err := e.Files.Read("vault.lwv")
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("cancelled exchange consent changed encrypted storage", err)
			}
			status, err := e.ExchangeStatus()
			if err != nil || status.Connected {
				t.Fatal("cancelled connection installed credentials", err)
			}
		})
	}
}
