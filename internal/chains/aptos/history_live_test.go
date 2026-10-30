package aptos

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// This opt-in is strictly read-only and uses the repository's well-known public
// BIP39 vector. It never imports a user profile or constructs/signs/submits a
// transaction. Public history availability is not a hermetic CI dependency.
func TestReadOnlyPublicFixtureAccountHistory(t *testing.T) {
	if os.Getenv("WALLET_APTOS_HISTORY_PUBLIC_CHECK") != "1" {
		t.Skip("read-only public provider acceptance requires explicit opt-in")
	}
	for _, network := range []string{"aptos-mainnet", "aptos-testnet"} {
		t.Run(network, func(t *testing.T) {
			a := New()
			ac, err := a.Derive(seed(), 0, network)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			page, err := a.HistoryPage(ctx, ac, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("public fixture %s: checked transactions=%d, continuation=%t, complete=%t, scope=%s", ac.Address, len(page.Entries), page.NextCursor != "", page.Complete, page.Scope)
			if page.Complete {
				t.Fatal("untrusted index became complete archival claim")
			}
			if network == "aptos-mainnet" {
				if len(page.Entries) == 0 {
					t.Fatal("known public mainnet fixture account activity is missing")
				}
				head, err := a.identity(ctx, network)
				if err != nil {
					t.Fatal(err)
				}
				headVersion, _ := strconv.ParseUint(head.Version, 10, 64)
				older := false
				for _, row := range page.Entries {
					version, _ := strconv.ParseUint(row.Block, 10, 64)
					if version+historyVersions < headVersion {
						older = true
					}
					t.Logf("checked historical tx=%s version=%s timestamp=%d effects=%+v status=%s", row.Identity, row.Block, row.Timestamp, row.Transfers, row.State)
				}
				if !older {
					t.Fatal("public check did not exercise activity older than the previous 256-version global window")
				}
			}
		})
	}
}
