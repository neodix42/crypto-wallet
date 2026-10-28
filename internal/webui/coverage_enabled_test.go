//go:build walletcoverage

package webui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticCountersRequireOwnedPrivateRegularDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCOVERDIR", directory)
	if got := coveragePromptEnvironment(nil); len(got) != 1 || got[0] != "GOCOVERDIR="+directory {
		t.Fatal("private owned counter directory not inherited")
	}
	flushPromptCoverage()
	// Go's test-main manages its own profile registration. The executable
	// campaign separately requires real counters from accepted private roles.
	link := filepath.Join(t.TempDir(), "counter-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "counter-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "relative", directory + "/../other", link, file, directory + "/missing"} {
		t.Setenv("GOCOVERDIR", value)
		if privateCoverageDirectory() != "" || len(coveragePromptEnvironment(nil)) != 0 {
			t.Fatal("unsafe diagnostic counter location accepted")
		}
	}
	t.Setenv("GOCOVERDIR", directory)
	if err := os.Chmod(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if privateCoverageDirectory() != "" {
		t.Fatal("counter directory exposed to other users")
	}
}
