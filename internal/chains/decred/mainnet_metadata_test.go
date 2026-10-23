package decred

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestMainnetNodeMetadataRequiresExplicitAuthenticationAndRedactsProviderPath(t *testing.T) {
	a := New()
	for _, network := range a.Networks() {
		if network.RPC != "" {
			t.Fatal("Decred advertised unconfigured node access")
		}
	}
	if err := a.Configure("decred-mainnet", Endpoint{URL: "https://rpc.example.org/PUBLIC-PATH-TOKEN", User: "PUBLIC-USER", Password: "PUBLIC-PASSWORD"}); err != nil {
		t.Fatal(err)
	}
	for _, network := range a.Networks() {
		if network.ID == "decred-mainnet" && network.RPC != "https://rpc.example.org" {
			t.Fatal("configured Decred origin missing or credential-bearing")
		}
		body, _ := json.Marshal(network)
		if bytes.Contains(body, []byte("PUBLIC-")) {
			t.Fatal("Decred credentials appeared in public network metadata")
		}
	}
}
