package near

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"localwallet/internal/chains/chainutil"
)

func TestDefaultProviderIsDocumentedFastNearWithoutOverwritingConfiguredEndpoints(t *testing.T) {
	a := New()
	for network, endpoint := range map[string]string{"near-mainnet": "https://rpc.mainnet.fastnear.com", "near-testnet": "https://rpc.testnet.fastnear.com"} {
		if a.endpoints[network] != endpoint {
			t.Fatal("deprecated provider default returned", network)
		}
		custom := "https://owned-node.invalid/private-provider-key"
		if a.SetEndpoint(network, custom) != nil || a.endpoints[network] != custom || historyArchiveEndpoint(network, custom) != "" {
			t.Fatal("custom provider was replaced or leaked to another archive")
		}
	}
}

func TestDefaultNearProviderReadOnlyAcceptance(t *testing.T) {
	if os.Getenv("CRYPTOWALLET_PUBLIC_PROVIDERS") != "1" {
		t.Skip("NOT_RUN: explicit read-only public provider check")
	}
	a := New()
	for _, network := range []string{"near-mainnet", "near-testnet"} {
		t.Run(network, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var status struct {
				ChainID string `json:"chain_id"`
				Sync    struct {
					Syncing bool   `json:"syncing"`
					Height  uint64 `json:"latest_block_height"`
				} `json:"sync_info"`
			}
			if err := a.rpc(ctx, network, "status", []any{}, &status); err != nil || status.ChainID != chain(network) || status.Sync.Syncing || status.Sync.Height == 0 {
				t.Fatal("default NEAR provider identity/readiness unavailable", err)
			}
			seed := make([]byte, 64)
			for index := range seed {
				seed[index] = byte(index)
			}
			account, err := a.Derive(seed, 0, network)
			clear(seed)
			if err != nil {
				t.Fatal(err)
			}
			balances, err := a.Balance(ctx, account)
			if err != nil || len(balances) != 1 || balances[0].Asset != asset(network) || balances[0].State != "NOT_CREATED (final state)" || balances[0].Atomic != "0" || balances[0].At == "" {
				t.Fatal("production missing-implicit-account balance unavailable or not explicitly observed zero", balances, err)
			}
			var head historyBlock
			if err := a.rpc(ctx, network, "block", map[string]string{"finality": "final"}, &head); err != nil || head.Header.Hash == "" {
				t.Fatal("final block unavailable", err)
			}
			var config protocol
			if err := a.rpc(ctx, network, "EXPERIMENTAL_protocol_config", map[string]string{"block_id": head.Header.Hash}, &config); err != nil || config.ChainID != chain(network) {
				t.Fatal("production protocol fee configuration unavailable", err)
			}
			if storage, err := chainutil.Big(config.Runtime.Storage, 128); err != nil || storage.Sign() <= 0 {
				t.Fatal("production storage-price metadata unavailable", err)
			}
			if transfer, err := chainutil.Uint(config.Runtime.Costs.Actions.Transfer.Execution.String(), 64); err != nil || transfer == 0 {
				t.Fatal("production transfer execution costs unavailable", err)
			}
			var price struct {
				Value string `json:"gas_price"`
			}
			if err := a.rpc(ctx, network, "gas_price", []string{head.Header.Hash}, &price); err != nil {
				t.Fatal("production gas-price method unavailable", err)
			}
			if value, err := chainutil.Big(price.Value, 128); err != nil || value.Sign() <= 0 {
				t.Fatal("invalid production gas price", err)
			}
			_, err = a.accessKey(ctx, account, accountState{Hash: head.Header.Hash})
			var missing *rpcError
			if !(errors.As(err, &missing) && missing.Cause.Name == "UNKNOWN_ACCOUNT") && (err == nil || err.Error() != "NEAR derived key lacks full-access authority") {
				t.Fatal("missing synthetic account access-key query did not fail closed as missing authority", err)
			}
			// FASTNEAR currently returns the legacy result.error envelope for
			// a missing key. The production parser denies authority; this is
			// not a successful funded full-access-key/preparation claim.
			t.Log("Production Balance, FINAL block, protocol/storage/transfer fee configuration, gas_price and missing-access-key rejection PASS; full-access funded preparation remains NOT_RUN")
			t.Logf("PASS read-only %s at provider height %d; no consensus or funded transfer claim", status.ChainID, status.Sync.Height)
		})
	}
}
