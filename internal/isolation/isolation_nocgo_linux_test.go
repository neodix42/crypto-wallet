//go:build !cgo || portable

package isolation

import "testing"

func TestStaticBuildDoesNotPretendLegacySandboxIsInstalled(t *testing.T) {
	if err := Install(); err == nil || err.Error() != "signer isolation unavailable" {
		t.Fatal("static build misreported an unavailable legacy filter", err)
	}
}
