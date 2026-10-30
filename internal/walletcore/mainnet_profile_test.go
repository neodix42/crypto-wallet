package walletcore

import (
	"bytes"
	"encoding/json"
	"testing"

	"localwallet/internal/chains"
	"localwallet/internal/model"
	"localwallet/internal/supervisor"
)

func TestPublicNetworkRegistrySeparatesCompleteMainnetAndTestProfiles(t *testing.T) {
	for _, test := range []struct {
		environment      string
		networks, assets int
	}{{"mainnet", 8, 11}, {"testnet", 10, 11}} {
		t.Run(test.environment, func(t *testing.T) {
			c := &core{child: &supervisor.Child{Environment: test.environment}, adapters: chains.All()}
			out, err := c.handle(message("networks", struct{}{}))
			if err != nil {
				t.Fatal(err)
			}
			networks := out.([]model.Network)
			families, assets := map[string]bool{}, 0
			for _, network := range networks {
				if network.Testnet != (test.environment == "testnet") {
					t.Fatal("foreign profile network displayed", network.ID)
				}
				families[network.Family] = true
				assets += len(network.Assets)
				if test.environment == "mainnet" && network.RPC == "" {
					t.Fatal("mainnet public RPC absent", network.ID)
				}
				if network.Family == "decred" {
					t.Fatal("retired Decred network exposed")
				}
			}
			if len(networks) != test.networks || assets != test.assets || len(families) != 5 {
				t.Fatal("profile omitted required mainnet/testnet chain family or asset", len(networks), assets, len(families))
			}
		})
	}
}

func TestPublicRPCMetadataRetainsOnlyOrigin(t *testing.T) {
	for _, endpoint := range []string{
		"https://PUBLIC-USER:PUBLIC-PASSWORD@rpc.example.org:443/v1/PUBLIC-PATH-TOKEN?apiKey=PUBLIC-QUERY#PUBLIC-FRAGMENT",
		"https://rpc.example.org/%zz/PUBLIC-PATH-TOKEN",
		"/PUBLIC-PATH-TOKEN", "PUBLIC-PATH-TOKEN", "javascript:PUBLIC-PATH-TOKEN",
	} {
		body, err := publicJSON(map[string]any{"rpc": endpoint})
		if err != nil || bytes.Contains(body, []byte("PUBLIC-")) {
			t.Fatal("RPC credential-bearing endpoint escaped origin redaction", err)
		}
		var value map[string]string
		if err := json.Unmarshal(body, &value); err != nil {
			t.Fatal(err)
		}
		if value["rpc"] != "" && value["rpc"] != "https://rpc.example.org:443" {
			t.Fatal("public RPC included private provider path")
		}
	}
}
