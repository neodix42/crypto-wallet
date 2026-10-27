package walletcore

import (
	"encoding/json"
	"testing"
)

func TestLockedSnapshotForwardsFirstRunAndUnavailableStorage(t *testing.T) {
	for _, tc := range []struct {
		state  string
		exists bool
	}{{"missing", false}, {"present", true}, {"unavailable", true}} {
		t.Run(tc.state, func(t *testing.T) {
			c, path := fakeCoordinator(t, &publicAdapter{}, nil)
			writePublicState(t, path, map[string]any{"locked": true, "walletExists": tc.exists, "vaultState": tc.state})
			s, err := c.current()
			if err != nil || !s.Locked || s.WalletExists != tc.exists || s.VaultState != tc.state {
				t.Fatalf("presence lost in coordinator: %+v %v", s, err)
			}
			encoded, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var public map[string]any
			if err := json.Unmarshal(encoded, &public); err != nil {
				t.Fatal(err)
			}
			if public["walletExists"] != tc.exists || public["vaultState"] != tc.state || public["locked"] != true {
				t.Fatalf("public presence fields absent: %s", encoded)
			}
		})
	}
}
