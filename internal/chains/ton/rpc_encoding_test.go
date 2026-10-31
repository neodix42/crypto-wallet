package ton

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTONProxyBalancesRemainExactCanonicalUint120(t *testing.T) {
	for _, text := range []string{`0`, `1`, `"0"`, `"200000000"`, `1329227995784915872903807060280344575`} {
		var state accountState
		if e := json.Unmarshal([]byte(`{"state":"uninitialized","balance":`+text+`}`), &state); e != nil || state.Balance == "" {
			t.Fatal("valid exact balance rejected", text, e)
		}
	}
	for _, text := range []string{`null`, `true`, `0.0`, `1e2`, `-1`, `"01"`, `"-0"`, `"1.0"`, `"1e2"`, `""`, `1329227995784915872903807060280344576`} {
		var state accountState
		if e := json.Unmarshal([]byte(`{"state":"uninitialized","balance":`+text+`}`), &state); e == nil {
			t.Fatal("lossy/noncanonical balance accepted", text)
		}
	}
}
func TestTONProxyReplyIDBindsExactNumericOrStringOne(t *testing.T) {
	for _, id := range []string{`1`, `"1"`, `2`, `"01"`, `"2"`, `1.0`, `null`, `true`} {
		t.Run(id, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cache-Control") != "no-cache, no-store" || r.Header.Get("Pragma") != "no-cache" {
					t.Error("RPC read permits stale shared-cache responses")
				}
				_, _ = w.Write([]byte(`{"ok":true,"id":` + id + `,"result":{}}`))
			}))
			defer s.Close()
			a := New()
			_ = a.Configure("ton-mainnet", Endpoint{URL: s.URL})
			var out any
			e := a.rpc(context.Background(), "ton-mainnet", "getMasterchainInfo", struct{}{}, &out)
			valid := id == `1` || id == `"1"`
			if (e == nil) != valid {
				t.Fatal("reply ID binding mismatch", id, e)
			}
		})
	}
}
