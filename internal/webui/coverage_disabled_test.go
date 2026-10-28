//go:build !walletcoverage

package webui

import (
	"reflect"
	"testing"
)

func TestProductionPromptCannotInheritCoverageOutput(t *testing.T) {
	t.Setenv("GOCOVERDIR", t.TempDir())
	env := []string{"PATH=/usr/bin:/bin"}
	if !reflect.DeepEqual(coveragePromptEnvironment(env), env) {
		t.Fatal("normal private environment acquired a diagnostic file-writing capability")
	}
	for _, entry := range promptEnvironment() {
		if len(entry) >= len("GOCOVERDIR=") && entry[:len("GOCOVERDIR=")] == "GOCOVERDIR=" {
			t.Fatal("normal private process inherited coverage output")
		}
	}
	flushPromptCoverage()
}
